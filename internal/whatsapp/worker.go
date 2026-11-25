package whatsapp

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow/types/events"
)

func (b *Bot) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-b.queue:
			if evt == nil || evt.Message == nil {
				continue
			}
			if err := b.processMessage(ctx, evt); err != nil {
				b.logger.Error("command processing failed", "chat", evt.Info.Chat.String(), "message_id", evt.Info.ID, "error", err)
			}
		}
	}
}

func (b *Bot) processMessage(ctx context.Context, evt *events.Message) error {
	message, ok := DecodeMessage(evt)
	if !ok {
		return nil
	}

	reply := func(ctx context.Context, text string) error {
		_, err := b.client.SendMessage(ctx, evt.Info.Chat, EncodeQuotedReply(evt, text))
		if err != nil {
			return fmt.Errorf("send quoted reply: %w", err)
		}
		return nil
	}
	return b.processor.Handle(ctx, message, reply)
}
