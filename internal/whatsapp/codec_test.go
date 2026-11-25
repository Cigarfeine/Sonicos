package whatsapp

import (
	"bytes"
	"encoding/hex"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestDecodeMessage_StickersAndQuotedSoundStickers(t *testing.T) {
	webpHash := bytes.Repeat([]byte{0xAA}, 32)
	audioHash := bytes.Repeat([]byte{0xBB}, 32)
	wantWebpHex := hex.EncodeToString(webpHash)
	wantAudioHex := hex.EncodeToString(audioHash)

	soundStickerProto := &waE2E.Message{
		AudioStickerMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				StickerMessage: &waE2E.StickerMessage{
					FileSHA256: webpHash,
					IsAnimated: proto.Bool(true),
					Audio: &waE2E.StickerMessage_AudioMessage{
						AudioMessage: &waE2E.AudioMessage{
							FileSHA256: audioHash,
						},
					},
				},
			},
		},
	}

	baseInfo := types.MessageInfo{
		ID: types.MessageID("MSG-1"),
		MessageSource: types.MessageSource{
			Chat:     types.NewJID("120363000000111", types.GroupServer),
			Sender:   types.NewJID("15550001111", types.DefaultUserServer),
			IsGroup:  true,
			IsFromMe: false,
		},
	}

	t.Run("top_level_audio_sticker", func(t *testing.T) {
		msg, ok := DecodeMessage(nil, &events.Message{Info: baseInfo, Message: soundStickerProto})
		if !ok {
			t.Fatal("expected DecodeMessage ok=true for AudioStickerMessage")
		}
		if msg.Text != "" || msg.Sticker == nil {
			t.Fatalf("expected empty text and non-nil Sticker, got text=%q sticker=%+v", msg.Text, msg.Sticker)
		}
		if !msg.Sticker.IsAudioSticker || !msg.Sticker.IsAnimated {
			t.Fatalf("expected IsAudioSticker=true and IsAnimated=true, got %+v", msg.Sticker)
		}
		if msg.Sticker.FileSHA256 != wantWebpHex || msg.Sticker.AudioSHA256 != wantAudioHex {
			t.Fatalf("hash mismatch: got file=%s audio=%s", msg.Sticker.FileSHA256, msg.Sticker.AudioSHA256)
		}
	})

	t.Run("ephemeral_wrapped_audio_sticker", func(t *testing.T) {
		wrapped := &waE2E.Message{
			EphemeralMessage: &waE2E.FutureProofMessage{
				Message: soundStickerProto,
			},
		}
		msg, ok := DecodeMessage(nil, &events.Message{Info: baseInfo, Message: wrapped})
		if !ok || msg.Sticker == nil || !msg.Sticker.IsAudioSticker || msg.Sticker.AudioSHA256 != wantAudioHex {
			t.Fatalf("failed to unwrap ephemeral audio sticker: ok=%v sticker=%+v", ok, msg.Sticker)
		}
	})

	t.Run("reply_bansticker_quoting_audio_sticker", func(t *testing.T) {
		replyEvt := &events.Message{
			Info: baseInfo,
			Message: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String("!bansticker loud 18+"),
					ContextInfo: &waE2E.ContextInfo{
						StanzaID:      proto.String("QUOTED-1"),
						Participant:   proto.String("15550002222@s.whatsapp.net"),
						QuotedMessage: soundStickerProto,
					},
				},
			},
		}
		msg, ok := DecodeMessage(nil, replyEvt)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if msg.Text != "!bansticker loud 18+" || msg.Sticker != nil || msg.QuotedSticker == nil {
			t.Fatalf("unexpected decode: text=%q sticker=%+v quoted=%+v", msg.Text, msg.Sticker, msg.QuotedSticker)
		}
		if msg.QuotedSticker.FileSHA256 != wantWebpHex || msg.QuotedSticker.AudioSHA256 != wantAudioHex {
			t.Fatalf("quoted sticker hash mismatch: %+v", msg.QuotedSticker)
		}
	})

	t.Run("silent_sticker", func(t *testing.T) {
		silent := &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				FileSHA256: webpHash,
				IsAnimated: proto.Bool(false),
			},
		}
		msg, ok := DecodeMessage(nil, &events.Message{Info: baseInfo, Message: silent})
		if !ok || msg.Sticker == nil || msg.Sticker.IsAudioSticker || msg.Sticker.FileSHA256 != wantWebpHex {
			t.Fatalf("unexpected silent sticker decode: ok=%v sticker=%+v", ok, msg.Sticker)
		}
	})

	t.Run("dm_image_without_caption_ignored_but_group_decoded", func(t *testing.T) {
		img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}
		dmInfo := baseInfo
		dmInfo.IsGroup = false
		if _, ok := DecodeMessage(nil, &events.Message{Info: dmInfo, Message: img}); ok {
			t.Fatal("expected ok=false for DM image without caption")
		}
		if _, ok := DecodeMessage(nil, &events.Message{Info: baseInfo, Message: img}); !ok {
			t.Fatal("expected ok=true for group message so group shields can inspect it")
		}
	})

	t.Run("ephemeral_wrapped_quoted_reply_extracts_text_and_sound_sticker", func(t *testing.T) {
		downloadableSound := proto.Clone(soundStickerProto).(*waE2E.Message)
		au := downloadableSound.GetAudioStickerMessage().GetMessage().GetStickerMessage().GetAudioMessage()
		au.DirectPath = proto.String("/v/t62.7114-24/a.enc")
		au.MediaKey = bytes.Repeat([]byte{0x12}, 32)
		au.FileEncSHA256 = bytes.Repeat([]byte{0x34}, 32)

		ephemeralReply := &waE2E.Message{
			EphemeralMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: proto.String("!tomp4"),
						ContextInfo: &waE2E.ContextInfo{
							StanzaID:      proto.String("EPHEM-QUOTED-1"),
							Participant:   proto.String("15550002222@s.whatsapp.net"),
							QuotedMessage: downloadableSound,
						},
					},
				},
			},
		}
		msg, ok := DecodeMessage(nil, &events.Message{Info: baseInfo, Message: ephemeralReply})
		if !ok {
			t.Fatal("expected ok=true for EphemeralMessage-wrapped reply")
		}
		if msg.Text != "!tomp4" || msg.QuotedMessageID != "EPHEM-QUOTED-1" || msg.QuotedSticker == nil || !msg.QuotedSticker.IsAudioSticker {
			t.Fatalf("unexpected ephemeral reply decode: %+v", msg)
		}
		if msg.StickerAudioSource == nil {
			t.Fatal("expected StickerAudioSource decoded inside EphemeralMessage reply")
		}
	})
}
