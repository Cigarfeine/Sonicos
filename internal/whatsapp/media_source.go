package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

const maxIncomingMediaBytes = 20 << 20

type mediaSource struct {
	client   *whatsmeow.Client
	media    whatsmeow.DownloadableMessage
	kind     command.MediaKind
	mimeType string
}

func newMediaSource(client *whatsmeow.Client, evtMessage *waE2E.Message) command.MediaSource {
	media, kind, mimeType := findMedia(evtMessage)
	if media == nil && evtMessage != nil {
		media, kind, mimeType = findMedia(quotedMessage(evtMessage))
	}
	if media == nil {
		return nil
	}
	return &mediaSource{client: client, media: media, kind: kind, mimeType: mimeType}
}

func (s *mediaSource) Download(ctx context.Context) (command.Attachment, error) {
	if sized, ok := s.media.(interface{ GetFileLength() uint64 }); ok && sized.GetFileLength() > maxIncomingMediaBytes {
		return command.Attachment{}, fmt.Errorf("media exceeds %d MiB limit", maxIncomingMediaBytes>>20)
	}
	data, err := s.client.Download(ctx, s.media)
	if err != nil {
		return command.Attachment{}, fmt.Errorf("download WhatsApp media: %w", err)
	}
	if len(data) == 0 {
		return command.Attachment{}, fmt.Errorf("downloaded media is empty")
	}
	if len(data) > maxIncomingMediaBytes {
		return command.Attachment{}, fmt.Errorf("media exceeds %d MiB limit", maxIncomingMediaBytes>>20)
	}
	return command.Attachment{Kind: s.kind, MIMEType: s.mimeType, Data: data}, nil
}

func findMedia(message *waE2E.Message) (whatsmeow.DownloadableMessage, command.MediaKind, string) {
	if message == nil {
		return nil, "", ""
	}
	if media := message.GetImageMessage(); media != nil {
		return media, command.MediaImage, media.GetMimetype()
	}
	if media := message.GetVideoMessage(); media != nil {
		return media, command.MediaVideo, media.GetMimetype()
	}
	if media := message.GetPtvMessage(); media != nil {
		return media, command.MediaVideo, media.GetMimetype()
	}
	if media := message.GetAudioMessage(); media != nil {
		return media, command.MediaAudio, media.GetMimetype()
	}
	if media := message.GetDocumentMessage(); media != nil {
		mimeType := strings.ToLower(media.GetMimetype())
		kind := command.MediaKind("")
		switch {
		case strings.HasPrefix(mimeType, "image/"):
			kind = command.MediaImage
		case strings.HasPrefix(mimeType, "video/"):
			kind = command.MediaVideo
		case strings.HasPrefix(mimeType, "audio/"):
			kind = command.MediaAudio
		}
		if kind != "" {
			return media, kind, mimeType
		}
	}
	return nil, "", ""
}
