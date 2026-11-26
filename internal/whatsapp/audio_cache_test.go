package whatsapp

import (
	"bytes"
	"context"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type stubStageStore struct {
	mu     sync.Mutex
	stages map[string]command.StagedMedia
}

func (s *stubStageStore) BanSticker(context.Context, string, command.BannedSticker) error { return nil }
func (s *stubStageStore) UnbanSticker(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (s *stubStageStore) UnbanStickerByPrefix(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *stubStageStore) IsStickerBanned(context.Context, string, string, string) (command.BannedSticker, bool, error) {
	return command.BannedSticker{}, false, nil
}
func (s *stubStageStore) ListBannedStickers(context.Context, string) ([]command.BannedSticker, error) {
	return nil, nil
}
func (s *stubStageStore) AddStrike(context.Context, string, string) (int, error) { return 0, nil }
func (s *stubStageStore) GetStrikes(context.Context, string, string) (int, error) { return 0, nil }
func (s *stubStageStore) ResetStrikes(context.Context, string, string) error      { return nil }
func (s *stubStageStore) RememberSticker(context.Context, string, string, command.StickerInfo) error {
	return nil
}
func (s *stubStageStore) LookupSticker(context.Context, string, string) (command.StickerInfo, bool, error) {
	return command.StickerInfo{}, false, nil
}
func (s *stubStageStore) GetShields(context.Context, string) (command.GroupShields, error) {
	return command.GroupShields{}, nil
}
func (s *stubStageStore) SetShield(context.Context, string, string, string) error { return nil }
func (s *stubStageStore) IncrementRate(context.Context, string, string, string, int) (int, error) {
	return 0, nil
}
func (s *stubStageStore) SaveStage(_ context.Context, scopeKey string, media command.StagedMedia, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stages == nil {
		s.stages = make(map[string]command.StagedMedia)
	}
	s.stages[scopeKey] = media
	return nil
}
func (s *stubStageStore) LoadStage(_ context.Context, scopeKey string) (command.StagedMedia, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.stages[scopeKey]
	return m, ok, nil
}
func (s *stubStageStore) ClearStage(_ context.Context, scopeKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stages, scopeKey)
	return nil
}
func (s *stubStageStore) SaveReactionSticker(context.Context, string, string, command.SavedReactionSticker) error {
	return nil
}
func (s *stubStageStore) GetReactionSticker(context.Context, string, string) (command.SavedReactionSticker, bool, error) {
	return command.SavedReactionSticker{}, false, nil
}
func (s *stubStageStore) DeleteReactionSticker(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *stubStageStore) ListReactionStickers(context.Context, string) ([]command.SavedReactionSticker, error) {
	return nil, nil
}

func TestNormalizeAudioForDownload(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	encSha := bytes.Repeat([]byte{0x99}, 32)

	// Missing DirectPath & top-level MediaKey -> derived from URL and MediaDomainInfo.
	partial := &waE2E.AudioMessage{
		URL:           proto.String("https://mmg.whatsapp.net/v/t62.7114-24/12345.enc?ccb=11-4&oh=abc"),
		FileEncSHA256: encSha,
		ContextInfo: &waE2E.ContextInfo{
			MediaDomainInfo: &waE2E.MediaDomainInfo{
				MediaKeyDomain: waE2E.MediaKeyDomain_MEDIA_KEY_DOMAIN_E2EE.Enum(),
				E2EeMediaKey:   key,
			},
		},
	}
	norm := normalizeAudioForDownload(partial)
	if !isDownloadableAudio(norm) {
		t.Fatalf("expected normalized audio to be downloadable, got %+v", norm)
	}
	if got := norm.GetDirectPath(); got != "/v/t62.7114-24/12345.enc?ccb=11-4&oh=abc" {
		t.Fatalf("unexpected derived DirectPath: %q", got)
	}
	if !bytes.Equal(norm.GetMediaKey(), key) {
		t.Fatal("expected MediaKey hydrated from MediaDomainInfo.E2EeMediaKey")
	}

	// Stripped audio without FileEncSHA256 or path should be rejected so cache can take over.
	stripped := &waE2E.AudioMessage{
		FileSHA256: bytes.Repeat([]byte{0x11}, 32),
		Seconds:    proto.Uint32(4),
	}
	if isDownloadableAudio(normalizeAudioForDownload(stripped)) {
		t.Fatal("expected stripped partial AudioMessage to be non-downloadable")
	}
}

func TestStickerAudioCache_RememberAndLookup(t *testing.T) {
	ctx := context.Background()
	webpHash := bytes.Repeat([]byte{0xAA}, 32)
	audioHash := bytes.Repeat([]byte{0xBB}, 32)
	mediaKey := bytes.Repeat([]byte{0xCC}, 32)
	encHash := bytes.Repeat([]byte{0xDD}, 32)
	chatJID := "120363000000111@g.us"

	soundMsg := &waE2E.Message{
		AudioStickerMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				StickerMessage: &waE2E.StickerMessage{
					URL:           proto.String("https://mmg.whatsapp.net/v/t62.15575-24/sticker.enc"),
					DirectPath:    proto.String("/v/t62.15575-24/sticker.enc"),
					MediaKey:      bytes.Repeat([]byte{0xEE}, 32),
					FileSHA256:    webpHash,
					FileEncSHA256: bytes.Repeat([]byte{0xFF}, 32),
					IsAnimated:    proto.Bool(true),
					Audio: &waE2E.StickerMessage_AudioMessage{
						AudioMessage: &waE2E.AudioMessage{
							URL:           proto.String("https://mmg.whatsapp.net/v/t62.7114-24/audio.enc"),
							DirectPath:    proto.String("/v/t62.7114-24/audio.enc"),
							MediaKey:      mediaKey,
							FileSHA256:    audioHash,
							FileEncSHA256: encHash,
							Mimetype:      proto.String("audio/ogg; codecs=opus"),
							Seconds:       proto.Uint32(5),
						},
					},
				},
			},
		},
	}

	store := &stubStageStore{}
	cache := newStickerAudioCache()
	cache.Remember(ctx, store, chatJID, "ORIG-SOUND-1", soundMsg)

	// Simulate quoted reply where WhatsApp stripped AudioStickerMessage + AudioMessage down to plain StickerMessage.
	strippedReply := &events.Message{
		Info: types.MessageInfo{
			ID: types.MessageID("CMD-1"),
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("120363000000111", types.GroupServer),
				Sender: types.NewJID("15550001111", types.DefaultUserServer),
			},
		},
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("!tomp4"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String("ORIG-SOUND-1"),
					QuotedMessage: &waE2E.Message{
						StickerMessage: &waE2E.StickerMessage{
							DirectPath:    proto.String("/v/t62.15575-24/sticker.enc"),
							MediaKey:      bytes.Repeat([]byte{0xEE}, 32),
							FileSHA256:    webpHash,
							FileEncSHA256: bytes.Repeat([]byte{0xFF}, 32),
							IsAnimated:    proto.Bool(true),
						},
					},
				},
			},
		},
	}

	bot := &Bot{audioCache: cache, moderation: store}
	decoded, ok := DecodeMessage(nil, strippedReply)
	if !ok {
		t.Fatal("expected DecodeMessage ok=true")
	}
	if decoded.StickerAudioSource != nil {
		t.Fatal("expected nil StickerAudioSource before hydration on stripped quote")
	}
	bot.hydrateQuotedSoundSticker(ctx, &decoded)
	if decoded.StickerAudioSource == nil {
		t.Fatal("expected hydrateQuotedSoundSticker to recover StickerAudioSource from message-ID cache")
	}
	if decoded.QuotedSticker == nil || !decoded.QuotedSticker.IsAudioSticker || decoded.QuotedSticker.AudioSHA256 != hex.EncodeToString(audioHash) {
		t.Fatalf("expected QuotedSticker hydrated with audio metadata, got %+v", decoded.QuotedSticker)
	}

	// Simulate fresh bot restart (empty memory cache, unknown quoted stanza ID) -> fallback via Redis + FileSHA256.
	freshBot := &Bot{audioCache: newStickerAudioCache(), moderation: store}
	decoded.QuotedMessageID = "UNKNOWN-OLD-ID"
	decoded.StickerAudioSource = nil
	decoded.QuotedSticker.IsAudioSticker = false
	decoded.QuotedSticker.AudioSHA256 = ""

	freshBot.hydrateQuotedSoundSticker(ctx, &decoded)
	if decoded.StickerAudioSource == nil {
		t.Fatal("expected fallback lookup via FileSHA256 + ModerationStore stage")
	}
	if !decoded.QuotedSticker.IsAudioSticker || decoded.QuotedSticker.AudioSHA256 != hex.EncodeToString(audioHash) {
		t.Fatalf("expected QuotedSticker audio fields restored from stage, got %+v", decoded.QuotedSticker)
	}
}
