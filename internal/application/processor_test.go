package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

type memoryIdempotency struct {
	claimed bool
	begins  int
	finish  int
	release int
}

func (m *memoryIdempotency) Begin(context.Context, string) (string, bool, error) {
	m.begins++
	return "token", m.claimed, nil
}

func (m *memoryIdempotency) Complete(context.Context, string, string) error {
	m.finish++
	return nil
}

func (m *memoryIdempotency) Release(context.Context, string, string) error {
	m.release++
	return nil
}

func testProcessor(t *testing.T, registry *command.Registry, store Idempotency) *Processor {
	t.Helper()
	processor, err := NewProcessor(registry, store, "!", time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func TestProcessorDispatchesAndCompletes(t *testing.T) {
	registry := command.NewRegistry()
	var replied string
	if err := registry.Register(command.Command{
		Name: "ping",
		Handler: func(ctx context.Context, _ command.Message, _ []string, reply command.ReplyFunc) error {
			return reply(ctx, "Pong")
		},
	}); err != nil {
		t.Fatal(err)
	}

	store := &memoryIdempotency{claimed: true}
	processor := testProcessor(t, registry, store)
	err := processor.Handle(context.Background(), command.Message{ID: "1", Chat: "chat", Text: "!ping"}, func(_ context.Context, text string) error {
		replied = text
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if replied != "Pong" || store.finish != 1 || store.release != 0 {
		t.Fatalf("reply=%q complete=%d release=%d", replied, store.finish, store.release)
	}
}

func TestProcessorReleasesClaimOnHandlerFailure(t *testing.T) {
	registry := command.NewRegistry()
	if err := registry.Register(command.Command{
		Name: "fail",
		Handler: func(context.Context, command.Message, []string, command.ReplyFunc) error {
			return errors.New("failed")
		},
	}); err != nil {
		t.Fatal(err)
	}
	store := &memoryIdempotency{claimed: true}
	processor := testProcessor(t, registry, store)
	if err := processor.Handle(context.Background(), command.Message{ID: "1", Chat: "chat", Text: "!fail"}, func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("expected handler error")
	}
	if store.release != 1 || store.finish != 0 {
		t.Fatalf("complete=%d release=%d", store.finish, store.release)
	}
}

func TestProcessorSkipsClaimedMessage(t *testing.T) {
	registry := command.NewRegistry()
	store := &memoryIdempotency{claimed: false}
	processor := testProcessor(t, registry, store)
	if err := processor.Handle(context.Background(), command.Message{ID: "1", Chat: "chat", Text: "!missing"}, func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if store.finish != 0 || store.release != 0 {
		t.Fatalf("complete=%d release=%d", store.finish, store.release)
	}
}
