package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SafwanGanz/Sonicos/internal/api"
	"github.com/SafwanGanz/Sonicos/internal/application"
	"github.com/SafwanGanz/Sonicos/internal/command"
	"github.com/SafwanGanz/Sonicos/internal/config"
	"github.com/SafwanGanz/Sonicos/internal/idempotency"
	"github.com/SafwanGanz/Sonicos/internal/moderation"
	_ "github.com/SafwanGanz/Sonicos/internal/plugins"
	"github.com/SafwanGanz/Sonicos/internal/whatsapp"
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
	modStore := moderation.NewRedisStore(deduper.Client())
	bot.SetModerationStore(modStore)

	// Create API server
	apiServer := api.NewServer(bot, registry, modStore, logger)

	// Start HTTP server in background
	apiPort := os.Getenv("PORT")
	if apiPort == "" {
		apiPort = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + apiPort,
		Handler: apiServer.Handler(),
	}

	go func() {
		logger.Info("HTTP server starting", "port", apiPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server failed", "error", err)
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

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