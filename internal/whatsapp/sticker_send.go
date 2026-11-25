package whatsapp

import (
	"context"
	"fmt"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func (b *Bot) encodeSticker(ctx context.Context, sticker command.Sticker, source *events.Message) (*waE2E.Message, error) {
	mediaType := whatsmeow.MediaImage
	if sticker.Audio {
		mediaType = whatsmeow.MediaAudio
	}
	upload, err := b.client.Upload(ctx, sticker.Data, mediaType)
	if err != nil {
		return nil, fmt.Errorf("upload sticker media: %w", err)
	}
	return buildStickerMessage(sticker, upload, source), nil
}

func buildStickerMessage(sticker command.Sticker, upload whatsmeow.UploadResponse, source *events.Message) *waE2E.Message {
	var contextInfo *waE2E.ContextInfo
	var mediaKeyTimestamp *int64
	if source != nil {
		contextInfo = quotedContext(source)
		mediaKeyTimestamp = proto.Int64(source.Info.Timestamp.Unix())
	}
	if sticker.Audio {
		return &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:               proto.String(upload.URL),
				Mimetype:          proto.String(sticker.MIMEType),
				FileSHA256:        upload.FileSHA256,
				FileLength:        proto.Uint64(upload.FileLength),
				Seconds:           proto.Uint32(sticker.Seconds),
				MediaKey:          upload.MediaKey,
				FileEncSHA256:     upload.FileEncSHA256,
				DirectPath:        proto.String(upload.DirectPath),
				MediaKeyTimestamp: mediaKeyTimestamp,
				PTT:               proto.Bool(false),
				ContextInfo:       contextInfo,
			},
		}
	}
	message := &waE2E.StickerMessage{
		URL:                proto.String(upload.URL),
		FileSHA256:         upload.FileSHA256,
		FileEncSHA256:      upload.FileEncSHA256,
		MediaKey:           upload.MediaKey,
		Mimetype:           proto.String(sticker.MIMEType),
		DirectPath:         proto.String(upload.DirectPath),
		FileLength:         proto.Uint64(upload.FileLength),
		MediaKeyTimestamp:  mediaKeyTimestamp,
		ContextInfo:        contextInfo,
		AccessibilityLabel: proto.String("Sticker"),
		Width:              proto.Uint32(512),
		Height:             proto.Uint32(512),
		IsAnimated:         proto.Bool(sticker.Animated),
	}
	return &waE2E.Message{StickerMessage: message}
}

func quotedContext(evt *events.Message) *waE2E.ContextInfo {
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(string(evt.Info.ID)),
		Participant:   proto.String(evt.Info.Sender.String()),
		RemoteJID:     proto.String(evt.Info.Chat.String()),
		QuotedMessage: evt.Message,
	}
}
