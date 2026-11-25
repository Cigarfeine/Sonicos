package whatsapp

import (
	"context"
	"fmt"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow/proto/waE2E"
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
	message, ok := DecodeMessage(b.client, evt)
	if !ok {
		return nil
	}

	reply := func(ctx context.Context, response command.Reply) error {
		var outgoing *waE2E.Message
		if response.Sticker != nil {
			var err error
			outgoing, err = b.encodeSticker(ctx, *response.Sticker, evt)
			if err != nil {
				return err
			}
		} else {
			if response.Text == "" {
				return fmt.Errorf("reply must contain text or sticker media")
			}
			outgoing = EncodeQuotedReply(evt, response.Text, response.Mentions...)
		}
		_, err := b.client.SendMessage(ctx, evt.Info.Chat, outgoing)
		if err != nil {
			return fmt.Errorf("send quoted reply: %w", err)
		}
		return nil
	}
	return b.processor.Handle(ctx, message, reply)
}
