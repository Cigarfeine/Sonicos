package whatsapp

import (
	"context"
	"encoding/hex"
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

func (b *Bot) rememberSoundSticker(ctx context.Context, chatJID, messageID string, msg *waE2E.Message) {
	if b.audioCache == nil || msg == nil {
		return
	}
	b.audioCache.Remember(ctx, b.moderation, chatJID, messageID, msg)
}

func (b *Bot) hydrateQuotedSoundSticker(ctx context.Context, msg *command.Message) {
	if b.audioCache == nil || msg == nil || msg.StickerAudioSource != nil {
		return
	}
	if msg.QuotedMessageID == "" && msg.QuotedSticker == nil {
		return
	}
	var quotedSHA string
	if msg.QuotedSticker != nil {
		quotedSHA = msg.QuotedSticker.FileSHA256
	}
	cachedAudio := b.audioCache.Lookup(ctx, b.moderation, msg.Chat, msg.QuotedMessageID, quotedSHA)
	if cachedAudio == nil {
		return
	}
	src := newAudioMediaSource(b.client, cachedAudio)
	if src == nil {
		return
	}
	msg.StickerAudioSource = src
	if msg.QuotedSticker != nil {
		msg.QuotedSticker.IsAudioSticker = true
		if msg.QuotedSticker.AudioSHA256 == "" && len(cachedAudio.GetFileSHA256()) > 0 {
			msg.QuotedSticker.AudioSHA256 = hex.EncodeToString(cachedAudio.GetFileSHA256())
		}
	}
}

func (b *Bot) processMessage(ctx context.Context, evt *events.Message) error {
	b.rememberSoundSticker(ctx, evt.Info.Chat.String(), string(evt.Info.ID), evt.Message)

	message, ok := DecodeMessage(b.client, evt)
	if !ok {
		return nil
	}
	message.ModerationStore = b.moderation
	b.hydrateQuotedSoundSticker(ctx, &message)

	reply := func(ctx context.Context, response command.Reply) error {
		var outgoing *waE2E.Message
		var err error
		switch {
		case response.StickerReaction != nil:
			outgoing, err = b.encodeStickerReaction(ctx, *response.StickerReaction, evt.Info.Chat)
		case response.Video != nil:
			outgoing, err = b.encodeVideoReply(ctx, *response.Video, evt)
		case response.VideoNote != nil:
			outgoing, err = b.encodeVideoNoteReply(ctx, *response.VideoNote, evt)
		case response.Image != nil:
			outgoing, err = b.encodeImageReply(ctx, *response.Image, evt)
		case response.Audio != nil:
			outgoing, err = b.encodeAudioReply(ctx, *response.Audio, evt)
		case response.Sticker != nil:
			outgoing, err = b.encodeSticker(ctx, *response.Sticker, evt)
		default:
			if response.Text == "" {
				return fmt.Errorf("reply must contain text or media")
			}
			outgoing = EncodeQuotedReply(evt, response.Text, response.Mentions...)
		}
		if err != nil {
			return err
		}
		resp, err := b.client.SendMessage(ctx, evt.Info.Chat, outgoing)
		if err != nil {
			return fmt.Errorf("send quoted reply: %w", err)
		}
		b.rememberSoundSticker(ctx, evt.Info.Chat.String(), string(resp.ID), outgoing)
		return nil
	}
	return b.processor.Handle(ctx, message, reply)
}
