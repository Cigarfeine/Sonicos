package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SafwanGanz/Sonicos/internal/application"
	"github.com/SafwanGanz/Sonicos/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const incomingSkewTolerance = 2 * time.Second

type Bot struct {
	client     *whatsmeow.Client
	processor  *application.Processor
	moderation command.ModerationStore
	audioCache *stickerAudioCache
	workers    int
	queue      chan *events.Message
	logger     *slog.Logger

	sessionOpMu sync.Mutex
	pairMu      sync.RWMutex
	runCtx      context.Context
	qrCancel    context.CancelFunc
	qrSnap      QRSnapshot
	qrReady     chan struct{}
	connectedAt time.Time

	syncMu          sync.RWMutex
	acceptAfter     time.Time
	offlineSyncing  bool
	awaitingConnect bool
	now             func() time.Time

	listenerMu sync.RWMutex
	listener   EventListener
}

func (b *Bot) SetModerationStore(store command.ModerationStore) {
	b.moderation = store
}

// SetEventListener registers a callback invoked on WhatsApp connection, QR, and pairing state changes.
func (b *Bot) SetEventListener(fn EventListener) {
	b.listenerMu.Lock()
	b.listener = fn
	b.listenerMu.Unlock()
}

func (b *Bot) emitEvent(event string, data any) {
	b.listenerMu.RLock()
	fn := b.listener
	b.listenerMu.RUnlock()
	if fn != nil {
		fn(event, data)
	}
}

func (b *Bot) baseCtx() context.Context {
	b.pairMu.RLock()
	ctx := b.runCtx
	b.pairMu.RUnlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (b *Bot) currentTime() time.Time {
	if b != nil && b.now != nil {
		return b.now().UTC()
	}
	return time.Now().UTC()
}

func (b *Bot) markConnecting() {
	now := b.currentTime()
	b.syncMu.Lock()
	b.acceptAfter = now
	b.offlineSyncing = false
	b.awaitingConnect = true
	b.syncMu.Unlock()
}

func (b *Bot) markConnected() {
	now := b.currentTime()
	b.syncMu.Lock()
	if b.awaitingConnect {
		if now.After(b.acceptAfter) {
			b.acceptAfter = now
		}
		b.awaitingConnect = false
	}
	b.syncMu.Unlock()
}

func (b *Bot) markDisconnected() {
	b.syncMu.Lock()
	b.offlineSyncing = false
	b.awaitingConnect = true
	b.syncMu.Unlock()
}

func (b *Bot) markOfflineSyncStarted(total, messages int) {
	if total <= 0 && messages <= 0 {
		return
	}
	now := b.currentTime()
	b.syncMu.Lock()
	b.offlineSyncing = true
	if now.After(b.acceptAfter) {
		b.acceptAfter = now
	}
	b.syncMu.Unlock()
}

func (b *Bot) markOfflineSyncCompleted() {
	b.syncMu.Lock()
	b.offlineSyncing = false
	b.syncMu.Unlock()
}

func (b *Bot) shouldIgnoreIncomingMessage(evt *events.Message) (string, bool) {
	if ShouldIgnoreMessage(evt) {
		return "ignored_event", true
	}

	b.syncMu.RLock()
	offlineSyncing := b.offlineSyncing
	awaitingConnect := b.awaitingConnect
	acceptAfter := b.acceptAfter
	b.syncMu.RUnlock()

	if offlineSyncing {
		return "offline_sync", true
	}

	cutoff := acceptAfter
	if awaitingConnect {
		if now := b.currentTime(); now.After(cutoff) {
			cutoff = now
		}
	}

	if !cutoff.IsZero() && !evt.Info.Timestamp.IsZero() {
		earliestAccepted := cutoff.Truncate(time.Second).Add(-incomingSkewTolerance)
		if evt.Info.Timestamp.UTC().Before(earliestAccepted) {
			return "before_connect", true
		}
	}

	return "", false
}

func NewBot(store *sqlstore.Container, processor *application.Processor, workers, queueSize int, logger *slog.Logger) (*Bot, error) {
	device, err := store.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load WhatsApp device: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Stdout("whatsmeow", "INFO", false))
	client.EnableAutoReconnect = true
	client.InitialAutoReconnect = true

	now := time.Now().UTC()
	return &Bot{
		client:          client,
		processor:       processor,
		audioCache:      newStickerAudioCache(),
		workers:         workers,
		queue:           make(chan *events.Message, queueSize),
		logger:          logger,
		qrSnap:          QRSnapshot{State: "disconnected"},
		acceptAfter:     now,
		awaitingConnect: true,
		now:             time.Now,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.pairMu.Lock()
	b.runCtx = ctx
	b.pairMu.Unlock()

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for range b.workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			b.runWorker(workerCtx)
		}()
	}

	b.client.AddEventHandler(b.handleEvent(workerCtx))

	if b.client.Store.ID == nil {
		if err := b.startPairingSession(ctx); err != nil {
			return fmt.Errorf("prepare WhatsApp pairing: %w", err)
		}
	} else {
		b.markConnecting()
		if err := b.client.ConnectContext(ctx); err != nil {
			return fmt.Errorf("connect to WhatsApp: %w", err)
		}
	}

	<-ctx.Done()
	b.cancelPairingSession()
	b.client.Disconnect()
	return nil
}

func (b *Bot) handleEvent(ctx context.Context) whatsmeow.EventHandler {
	return func(raw any) {
		switch evt := raw.(type) {
		case *events.Message:
			if reason, ignore := b.shouldIgnoreIncomingMessage(evt); ignore {
				if evt != nil && b.logger != nil {
					b.logger.Debug("ignoring incoming WhatsApp message",
						"reason", reason,
						"chat", evt.Info.Chat.String(),
						"message_id", evt.Info.ID,
						"timestamp", evt.Info.Timestamp.UTC(),
					)
				}
				return
			}
			if b.logger != nil {
				b.logger.Debug("received WhatsApp message", "chat", evt.Info.Chat.String(), "sender", evt.Info.Sender.String(), "message_id", evt.Info.ID)
			}
			select {
			case b.queue <- evt:
			case <-ctx.Done():
			default:
				if b.logger != nil {
					b.logger.Warn("message queue full; dropping incoming message", "chat", evt.Info.Chat.String(), "message_id", evt.Info.ID)
				}
			}
		case *events.OfflineSyncPreview:
			if evt == nil {
				return
			}
			if b.logger != nil {
				b.logger.Info("WhatsApp offline sync started", "total", evt.Total, "messages", evt.Messages)
			}
			b.markOfflineSyncStarted(evt.Total, evt.Messages)
		case *events.OfflineSyncCompleted:
			if evt == nil {
				return
			}
			if b.logger != nil {
				b.logger.Info("WhatsApp offline sync completed", "count", evt.Count)
			}
			b.markOfflineSyncCompleted()
		case *events.Connected:
			if b.logger != nil {
				b.logger.Info("WhatsApp connected")
			}
			b.markConnected()
			connectedAt := b.currentTime()
			b.pairMu.Lock()
			b.connectedAt = connectedAt
			b.qrSnap = QRSnapshot{State: "connected"}
			b.pairMu.Unlock()
			b.emitEvent("status", map[string]any{"event": "connected"})
		case *events.Disconnected:
			if b.logger != nil {
				b.logger.Warn("WhatsApp disconnected")
			}
			b.markDisconnected()
			b.pairMu.Lock()
			b.connectedAt = time.Time{}
			if b.client != nil && b.client.Store != nil && b.client.Store.ID != nil {
				b.qrSnap = QRSnapshot{State: "disconnected"}
			}
			b.pairMu.Unlock()
			b.emitEvent("status", map[string]any{"event": "disconnected"})
		case *events.LoggedOut:
			reason := ""
			if evt != nil {
				reason = evt.Reason.String()
			}
			if b.logger != nil {
				b.logger.Error("WhatsApp session logged out", "reason", reason)
			}
			b.markDisconnected()
			b.pairMu.Lock()
			b.connectedAt = time.Time{}
			b.qrSnap = QRSnapshot{State: "disconnected"}
			b.pairMu.Unlock()
			b.emitEvent("status", map[string]any{"event": "logged_out", "reason": reason})
			if ctx.Err() == nil {
				go func() {
					if err := b.startPairingSession(b.baseCtx()); err != nil && b.logger != nil {
						b.logger.Warn("failed to restart pairing session after logout", "error", err)
					}
				}()
			}
		}
	}
}
