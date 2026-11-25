package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/application"
	"github.com/sonicos/whatsapp-bot/internal/command"
	"github.com/sonicos/whatsapp-bot/internal/config"
	"github.com/sonicos/whatsapp-bot/internal/idempotency"
	_ "github.com/sonicos/whatsapp-bot/internal/plugins"
	"github.com/sonicos/whatsapp-bot/internal/whatsapp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)
	registry, err := command.Default()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessionStore, err := whatsapp.OpenStore(ctx, cfg.DatabaseDriver, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer sessionStore.Close()

	deduper, err := idempotency.New(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer deduper.Close()

	processor, err := application.NewProcessor(registry, deduper, cfg.Prefix, 30*time.Second, logger)
	if err != nil {
		return err
	}
	bot, err := whatsapp.NewBot(sessionStore, processor, cfg.Workers, cfg.QueueSize, logger)
	if err != nil {
		return err
	}
	logger.Info("starting WhatsApp bot", "workers", cfg.Workers, "queue_size", cfg.QueueSize, "prefix", cfg.Prefix)
	return bot.Run(ctx)
}

func logLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
