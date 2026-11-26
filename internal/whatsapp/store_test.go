package whatsapp

import (
	"context"
	"testing"

	"github.com/SafwanGanz/Sonicos/internal/command"
	"github.com/SafwanGanz/Sonicos/internal/moderation"
)

func TestOpenStoreWithDBSharedSQLModeration(t *testing.T) {
	ctx := context.Background()
	container, db, err := OpenStoreWithDB(ctx, "sqlite3", "file::memory:?_foreign_keys=on&cache=shared")
	if err != nil {
		t.Fatalf("OpenStoreWithDB: %v", err)
	}
	defer container.Close()

	if _, err := container.GetFirstDevice(ctx); err != nil {
		t.Fatalf("GetFirstDevice: %v", err)
	}

	modStore, err := moderation.NewSQLStore(ctx, db, "sqlite3")
	if err != nil {
		t.Fatalf("NewSQLStore on shared db: %v", err)
	}
	if err := modStore.SaveReactionSticker(ctx, "global", "🔥", command.SavedReactionSticker{Emoji: "🔥", Data: []byte("webp")}); err != nil {
		t.Fatalf("SaveReactionSticker: %v", err)
	}
	got, ok, err := modStore.GetReactionSticker(ctx, "global", "🔥")
	if err != nil || !ok || string(got.Data) != "webp" {
		t.Fatalf("GetReactionSticker = %+v ok=%v err=%v", got, ok, err)
	}
}
