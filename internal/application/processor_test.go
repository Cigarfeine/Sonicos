package application

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

type fakeIdempotency struct {
	mu      sync.Mutex
	begins  int
	done    map[string]bool
	inFly   map[string]string
}

func newFakeIdempotency() *fakeIdempotency {
	return &fakeIdempotency{
		done:  make(map[string]bool),
		inFly: make(map[string]string),
	}
}

func (f *fakeIdempotency) Begin(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins++
	if f.done[key] || f.inFly[key] != "" {
		return "", false, nil
	}
	f.inFly[key] = "tok"
	return "tok", true, nil
}

func (f *fakeIdempotency) Complete(_ context.Context, key, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFly[key] == token {
		delete(f.inFly, key)
		f.done[key] = true
	}
	return nil
}

func (f *fakeIdempotency) Release(_ context.Context, key, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFly[key] == token {
		delete(f.inFly, key)
	}
	return nil
}

func TestProcessor_WatchersAndIdempotency(t *testing.T) {
	reg := command.NewRegistry()
	var cmdCalls, watchCalls int
	if err := reg.Register(command.Command{
		Name: "ping",
		Handler: func(ctx context.Context, msg command.Message, args []string, reply command.ReplyFunc) error {
			cmdCalls++
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	reg.Watch(func(ctx context.Context, msg command.Message, reply command.ReplyFunc) error {
		watchCalls++
		return nil
	})

	idem := newFakeIdempotency()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proc, err := NewProcessor(reg, idem, "!", 5*time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	noopReply := func(context.Context, command.Reply) error { return nil }

	// Normal DM chat text must not hit Redis or watchers.
	if err := proc.Handle(context.Background(), command.Message{ID: "1", Chat: "1555@s.whatsapp.net", Group: false, Text: "hello"}, noopReply); err != nil {
		t.Fatal(err)
	}
	if idem.begins != 0 || watchCalls != 0 || cmdCalls != 0 {
		t.Fatalf("unexpected activity for DM text: begins=%d watch=%d cmd=%d", idem.begins, watchCalls, cmdCalls)
	}

	// Group message triggers watcher once; duplicate delivery is skipped.
	stMsg := command.Message{
		ID:      "2",
		Chat:    "g@g.us",
		Group:   true,
		Sticker: &command.StickerInfo{FileSHA256: "abc", IsAudioSticker: true},
	}
	if err := proc.Handle(context.Background(), stMsg, noopReply); err != nil {
		t.Fatal(err)
	}
	if err := proc.Handle(context.Background(), stMsg, noopReply); err != nil {
		t.Fatal(err)
	}
	if watchCalls != 1 || idem.begins != 2 {
		t.Fatalf("expected watchCalls=1 and idem.begins=2, got watch=%d begins=%d", watchCalls, idem.begins)
	}

	// Command runs command handler and does not invoke passive sticker watcher.
	if err := proc.Handle(context.Background(), command.Message{ID: "3", Chat: "g@g.us", Text: "!ping"}, noopReply); err != nil {
		t.Fatal(err)
	}
	if cmdCalls != 1 || watchCalls != 1 {
		t.Fatalf("expected cmdCalls=1 watchCalls=1, got cmd=%d watch=%d", cmdCalls, watchCalls)
	}
}
