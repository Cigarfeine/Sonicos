package whatsapp

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{now: t.UTC()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.UTC()
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestBot(clock *fakeClock) *Bot {
	now := clock.Now()
	return &Bot{
		queue:           make(chan *events.Message, 64),
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		qrSnap:          QRSnapshot{State: "disconnected"},
		acceptAfter:     now,
		awaitingConnect: true,
		now:             clock.Now,
	}
}

func makeGroupMessage(id string, ts time.Time) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:    types.NewJID("120363000000000000", types.GroupServer),
				Sender:  types.NewJID("15551234567", types.DefaultUserServer),
				IsGroup: true,
			},
			ID:        types.MessageID(id),
			Timestamp: ts,
		},
		Message: &waE2E.Message{},
	}
}

func drainQueueIDs(q chan *events.Message) []string {
	var ids []string
	for {
		select {
		case evt := <-q:
			ids = append(ids, string(evt.Info.ID))
		default:
			return ids
		}
	}
}

func TestHandleEvent_IgnoresMessagesBeforeConnectAndAcceptsAfterConnect(t *testing.T) {
	bootTime := time.Date(2026, 11, 26, 12, 0, 0, 500_000_000, time.UTC)
	clock := newFakeClock(bootTime)
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	// Connect 5 seconds after NewBot.
	clock.Advance(5 * time.Second)
	connectTime := clock.Now()
	handler(&events.Connected{})

	// Old messages sent before connect (beyond skew tolerance) must be ignored.
	handler(makeGroupMessage("old-1", bootTime.Add(-10*time.Minute)))
	handler(makeGroupMessage("old-2", connectTime.Add(-10*time.Second)))

	// Live message sent right after connect must be accepted.
	clock.Advance(1 * time.Second)
	handler(makeGroupMessage("live-1", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "live-1" {
		t.Fatalf("expected [live-1], got %v", got)
	}
}

func TestHandleEvent_IgnoresOfflineSyncWindowAndAcceptsAfterCompleted(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	handler(&events.OfflineSyncPreview{Total: 3, Messages: 3})

	// Even if a message delivered during offline sync has a timestamp >= acceptAfter,
	// it must still be ignored until OfflineSyncCompleted arrives.
	handler(makeGroupMessage("sync-old", clock.Now().Add(-5*time.Minute)))
	handler(makeGroupMessage("sync-boundary", clock.Now()))

	handler(&events.Connected{})
	// Connected alone must not clear an active offline sync.
	handler(makeGroupMessage("sync-still-active", clock.Now()))

	handler(&events.OfflineSyncCompleted{Count: 3})
	clock.Advance(1 * time.Second)
	handler(makeGroupMessage("live-after-sync", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "live-after-sync" {
		t.Fatalf("expected [live-after-sync], got %v", got)
	}
}

func TestHandleEvent_ConnectedBeforeOfflineSyncPreview(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	handler(&events.Connected{})
	handler(&events.OfflineSyncPreview{Total: 2, Messages: 2})
	handler(makeGroupMessage("sync-msg", clock.Now()))
	handler(&events.OfflineSyncCompleted{Count: 2})

	clock.Advance(2 * time.Second)
	handler(makeGroupMessage("live-msg", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "live-msg" {
		t.Fatalf("expected [live-msg], got %v", got)
	}
}

func TestHandleEvent_SubSecondAndSmallSkewTolerance(t *testing.T) {
	// Simulate connect at 12:00:05.900; WhatsApp timestamps have 1-second resolution (12:00:05 or 12:00:04).
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 5, 900_000_000, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())
	handler(&events.Connected{})

	// 1 second before truncated connect time (12:00:04) is within 2s skew tolerance -> accepted.
	handler(makeGroupMessage("skew-ok", time.Date(2026, 11, 26, 12, 0, 4, 0, time.UTC)))
	// 3 seconds before truncated connect time (12:00:02) exceeds 2s skew tolerance -> ignored.
	handler(makeGroupMessage("skew-too-old", time.Date(2026, 11, 26, 12, 0, 2, 0, time.UTC)))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "skew-ok" {
		t.Fatalf("expected [skew-ok], got %v", got)
	}
}

func TestHandleEvent_RetriedOldOfflineMessageAfterSyncCompletedIsIgnored(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	handler(&events.Connected{})
	handler(&events.OfflineSyncPreview{Total: 1, Messages: 1})
	handler(&events.OfflineSyncCompleted{Count: 1})

	// Suppose an undecryptable message from 15 minutes ago gets retried later with RetryCount > 0.
	retried := makeGroupMessage("retried-old", clock.Now().Add(-15*time.Minute))
	retried.RetryCount = 1
	handler(retried)

	got := drainQueueIDs(bot.queue)
	if len(got) != 0 {
		t.Fatalf("expected retried old message to be ignored, got %v", got)
	}
}

func TestShouldIgnoreMessage_StatelessFilters(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())
	handler(&events.Connected{})

	// Nil message event must not panic or queue.
	handler((*events.Message)(nil))

	// Broadcast list message.
	broadcast := makeGroupMessage("broadcast-1", now)
	broadcast.Info.Chat = types.NewJID("123456", types.BroadcastServer)
	handler(broadcast)

	// HistorySync-derived message.
	historyMsg := makeGroupMessage("history-1", now)
	historyMsg.SourceWebMsg = &waWeb.WebMessageInfo{}
	handler(historyMsg)

	// Unavailable placeholder resend message.
	placeholderMsg := makeGroupMessage("placeholder-1", now)
	placeholderMsg.UnavailableRequestID = "req-123"
	handler(placeholderMsg)

	got := drainQueueIDs(bot.queue)
	if len(got) != 0 {
		t.Fatalf("expected all filtered events to be ignored, got %v", got)
	}
}

func TestHandleEvent_ReconnectAfterDowntimeDropsOfflineMessagesBeforeConnected(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	// Initial connection.
	handler(&events.Connected{})
	handler(makeGroupMessage("before-disconnect", clock.Now()))

	// Disconnect for 30 minutes.
	handler(&events.Disconnected{})
	sentWhileOffline := clock.Now().Add(10 * time.Minute)

	clock.Advance(30 * time.Minute)
	bot.markConnecting()

	// Offline message arrives before Connected without OfflineSyncPreview; awaitingConnect advances cutoff to current time.
	handler(makeGroupMessage("offline-while-down", sentWhileOffline))

	handler(&events.Connected{})
	handler(makeGroupMessage("fresh-after-reconnect", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 2 || got[0] != "before-disconnect" || got[1] != "fresh-after-reconnect" {
		t.Fatalf("expected [before-disconnect fresh-after-reconnect], got %v", got)
	}
}

func TestHandleEvent_DisconnectMidOfflineSyncResetsState(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	handler(&events.OfflineSyncPreview{Total: 5, Messages: 5})
	handler(&events.Disconnected{})

	clock.Advance(5 * time.Second)
	handler(&events.Connected{})
	handler(makeGroupMessage("after-reset", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "after-reset" {
		t.Fatalf("expected [after-reset], got %v", got)
	}
}

func TestHandleEvent_ZeroOfflineSyncPreviewDoesNotBlock(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	handler := bot.handleEvent(context.Background())

	handler(&events.OfflineSyncPreview{Total: 0, Messages: 0})
	handler(&events.Connected{})
	handler(makeGroupMessage("live-zero-preview", clock.Now()))

	got := drainQueueIDs(bot.queue)
	if len(got) != 1 || got[0] != "live-zero-preview" {
		t.Fatalf("expected [live-zero-preview], got %v", got)
	}
}

func TestHandleEvent_ConcurrentRaceFree(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	bot := newTestBot(clock)
	bot.queue = make(chan *events.Message, 256)
	handler := bot.handleEvent(context.Background())

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := range 25 {
				switch j % 5 {
				case 0:
					handler(&events.Connected{})
				case 1:
					handler(&events.OfflineSyncPreview{Total: 1, Messages: 1})
				case 2:
					handler(&events.OfflineSyncCompleted{Count: 1})
				case 3:
					handler(&events.Disconnected{})
				default:
					handler(makeGroupMessage("msg", clock.Now()))
				}
				if worker%2 == 0 {
					clock.Advance(10 * time.Millisecond)
				}
			}
		}(i)
	}
	wg.Wait()
}
