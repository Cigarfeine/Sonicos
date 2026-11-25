package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BOT_PREFIX", "")
	t.Setenv("WORKERS", "")
	t.Setenv("QUEUE_SIZE", "")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseDriver != "sqlite3" || cfg.Prefix != "!" || cfg.Workers != 8 || cfg.QueueSize != 512 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadRejectsQueueSmallerThanWorkerPool(t *testing.T) {
	t.Setenv("WORKERS", "4")
	t.Setenv("QUEUE_SIZE", "3")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid queue configuration to fail")
	}
}
