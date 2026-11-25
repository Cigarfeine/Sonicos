package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sonicos/whatsapp-bot/internal/application"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Bot struct {
	client    *whatsmeow.Client
	processor *application.Processor
	workers   int
	queue     chan *events.Message
	logger    *slog.Logger
}

func NewBot(store *sqlstore.Container, processor *application.Processor, workers, queueSize int, logger *slog.Logger) (*Bot, error) {
	device, err := store.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load WhatsApp device: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Stdout("whatsmeow", "INFO", false))
	client.EnableAutoReconnect = true
	client.InitialAutoReconnect = true

	return &Bot{
		client:    client,
		processor: processor,
		workers:   workers,
		queue:     make(chan *events.Message, queueSize),
		logger:    logger,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
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

	qrChannel, err := b.client.GetQRChannel(ctx)
	if err != nil && b.client.Store.ID == nil {
		return fmt.Errorf("prepare WhatsApp pairing: %w", err)
	}
	b.client.AddEventHandler(b.handleEvent(workerCtx))

	if err := b.client.ConnectContext(ctx); err != nil {
		return fmt.Errorf("connect to WhatsApp: %w", err)
	}
	if qrChannel != nil {
		go b.showQR(ctx, qrChannel)
	}

	<-ctx.Done()
	b.client.Disconnect()
	return nil
}

func (b *Bot) handleEvent(ctx context.Context) whatsmeow.EventHandler {
	return func(raw any) {
		switch evt := raw.(type) {
		case *events.Message:
			if ShouldIgnoreMessage(evt) {
				return
			}
			b.logger.Debug("received WhatsApp message", "chat", evt.Info.Chat.String(), "sender", evt.Info.Sender.String(), "message_id", evt.Info.ID)
			select {
			case b.queue <- evt:
			case <-ctx.Done():
			default:
				b.logger.Warn("message queue full; dropping incoming message", "chat", evt.Info.Chat.String(), "message_id", evt.Info.ID)
			}
		case *events.Connected:
			b.logger.Info("WhatsApp connected")
		case *events.Disconnected:
			b.logger.Warn("WhatsApp disconnected")
		case *events.LoggedOut:
			b.logger.Error("WhatsApp session logged out", "reason", evt.Reason.String())
		}
	}
}
