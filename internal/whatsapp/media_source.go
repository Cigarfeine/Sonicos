package whatsapp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/SafwanGanz/Sonicos/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
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

func normalizeAudioForDownload(audio *waE2E.AudioMessage) *waE2E.AudioMessage {
	if audio == nil {
		return nil
	}
	clone, ok := proto.Clone(audio).(*waE2E.AudioMessage)
	if !ok || clone == nil {
		return nil
	}
	if strings.TrimSpace(clone.GetDirectPath()) == "" && strings.TrimSpace(clone.GetURL()) != "" {
		if u, err := url.Parse(strings.TrimSpace(clone.GetURL())); err == nil && u.RequestURI() != "" {
			clone.DirectPath = proto.String(u.RequestURI())
		}
	}
	if len(clone.GetMediaKey()) == 0 {
		if key := clone.GetContextInfo().GetMediaDomainInfo().GetE2EeMediaKey(); len(key) > 0 {
			clone.MediaKey = append([]byte(nil), key...)
		}
	}
	return clone
}

func isDownloadableAudio(audio *waE2E.AudioMessage) bool {
	return audio != nil &&
		strings.TrimSpace(audio.GetDirectPath()) != "" &&
		len(audio.GetMediaKey()) > 0 &&
		len(audio.GetFileEncSHA256()) > 0
}

func newAudioMediaSource(client *whatsmeow.Client, audio *waE2E.AudioMessage) command.MediaSource {
	normalized := normalizeAudioForDownload(audio)
	if !isDownloadableAudio(normalized) {
		return nil
	}
	mime := normalized.GetMimetype()
	if strings.TrimSpace(mime) == "" {
		mime = "audio/ogg; codecs=opus"
	}
	return &mediaSource{client: client, media: normalized, kind: command.MediaAudio, mimeType: mime}
}

func newStickerAudioSource(client *whatsmeow.Client, evtMessage *waE2E.Message) command.MediaSource {
	if src := newAudioMediaSource(client, findStickerAudio(evtMessage)); src != nil {
		return src
	}
	if evtMessage != nil {
		return newAudioMediaSource(client, findStickerAudio(quotedMessage(evtMessage)))
	}
	return nil
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

func unwrapMessage(message *waE2E.Message, depth int) *waE2E.Message {
	if message == nil || depth > 6 {
		return message
	}
	for _, inner := range []*waE2E.Message{
		message.GetAudioStickerMessage().GetMessage(),
		message.GetEphemeralMessage().GetMessage(),
		message.GetViewOnceMessage().GetMessage(),
		message.GetViewOnceMessageV2().GetMessage(),
		message.GetViewOnceMessageV2Extension().GetMessage(),
		message.GetDocumentWithCaptionMessage().GetMessage(),
		message.GetLottieStickerMessage().GetMessage(),
		message.GetDeviceSentMessage().GetMessage(),
	} {
		if inner != nil {
			return unwrapMessage(inner, depth+1)
		}
	}
	return message
}

func findStickerAudio(message *waE2E.Message) *waE2E.AudioMessage {
	unwrapped := unwrapMessage(message, 0)
	if unwrapped == nil {
		return nil
	}
	if st := unwrapped.GetStickerMessage(); st != nil {
		return st.GetAudioMessage()
	}
	return nil
}

func findMedia(message *waE2E.Message) (whatsmeow.DownloadableMessage, command.MediaKind, string) {
	message = unwrapMessage(message, 0)
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
	if media := message.GetStickerMessage(); media != nil {
		mime := media.GetMimetype()
		if strings.TrimSpace(mime) == "" {
			mime = "image/webp"
		}
		return media, command.MediaSticker, mime
	}
	if media := message.GetDocumentMessage(); media != nil {
		mimeType := strings.ToLower(media.GetMimetype())
		kind := command.MediaKind("")
		switch {
		case strings.HasPrefix(mimeType, "image/"):
			kind = command.MediaImage
		case strings.HasPrefix(mimeType, "video/"):
			kind = command.MediaVideo
		case strings.HasPrefix(mimeType, "audio/"), strings.HasPrefix(mimeType, "application/ogg"), strings.HasPrefix(mimeType, "application/x-ogg"):
			kind = command.MediaAudio
		}
		if kind != "" {
			return media, kind, mimeType
		}
	}
	return nil, "", ""
}
