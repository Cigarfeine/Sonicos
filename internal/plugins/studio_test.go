package plugins

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func genSamplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := range 128 {
		for x := range 128 {
			img.SetRGBA(x, y, color.RGBA{R: 37, G: 211, B: 102, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func genSampleMP3(t *testing.T) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.mp3")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:a", "libmp3lame", p).CombinedOutput(); err != nil {
		t.Fatalf("gen mp3: %v\n%s", err, string(out))
	}
	b, err := readTestFile(t, p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStudioCommands_DubStealToImgToAudioPtvSreact(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	store := newFakeModerationStore()
	pngData := genSamplePNG(t)
	mp3Data := genSampleMP3(t)

	// 1. !dub (visual first -> audio second)
	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}
	baseMsg := command.Message{
		Chat:            "g@g.us",
		Sender:          "15550001@s.whatsapp.net",
		Group:           true,
		ModerationStore: store,
	}

	msg1 := baseMsg
	msg1.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngData}}
	if err := dubStickerCmd(ctx, msg1, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Visual staged") {
		t.Fatalf("expected visual staged prompt, got %+v", replies)
	}

	replies = nil
	msg2 := baseMsg
	msg2.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: mp3Data}}
	if err := dubStickerCmd(ctx, msg2, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Sticker == nil || !replies[0].Sticker.Audio || len(replies[0].Sticker.AudioData) == 0 {
		t.Fatalf("expected dubbed sound sticker, got %+v", replies)
	}
	dubbed := *replies[0].Sticker

	// 2. !steal on dubbed sound sticker
	replies = nil
	stealMsg := baseMsg
	stealMsg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	stealMsg.StickerAudioSource = stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/ogg; codecs=opus", Data: dubbed.AudioData}}
	if err := stealStickerCmd(ctx, stealMsg, []string{"CustomPack", "|", "Me"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Sticker == nil || !replies[0].Sticker.Audio || replies[0].Sticker.ExifOffset == 0 {
		t.Fatalf("expected rebranded sound sticker, got %+v", replies)
	}

	// 3. !toimg on sticker
	replies = nil
	toImgMsg := baseMsg
	toImgMsg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	if err := stickerToImgCmd(ctx, toImgMsg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Image == nil || len(replies[0].Image.Data) == 0 {
		t.Fatalf("expected PNG image reply, got %+v", replies)
	}

	// 4. !toaudio on sound sticker
	replies = nil
	toAudMsg := baseMsg
	toAudMsg.StickerAudioSource = stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/ogg", Data: dubbed.AudioData}}
	if err := toAudioCmd(ctx, toAudMsg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Audio == nil || len(replies[0].Audio.Data) == 0 {
		t.Fatalf("expected extracted audio reply, got %+v", replies)
	}

	// 5. !ptv on image
	replies = nil
	ptvMsg := baseMsg
	ptvMsg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngData}}
	if err := videoNoteCmd(ctx, ptvMsg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].VideoNote == nil || len(replies[0].VideoNote.Data) == 0 {
		t.Fatalf("expected video note reply, got %+v", replies)
	}

	// 6. !sreact set + reply
	replies = nil
	sreactSet := baseMsg
	sreactSet.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	if err := stickerReactCmd(ctx, sreactSet, []string{"set"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "saved") {
		t.Fatalf("expected sreact set confirmation, got %+v", replies)
	}

	replies = nil
	sreactSend := baseMsg
	sreactSend.QuotedMessageID = "TARGET-MSG-42"
	sreactSend.QuotedSender = "15550002@s.whatsapp.net"
	if err := stickerReactCmd(ctx, sreactSend, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].StickerReaction == nil || replies[0].StickerReaction.TargetStanzaID != "TARGET-MSG-42" {
		t.Fatalf("expected StickerReaction reply, got %+v", replies)
	}

	// 7. !tomp4 on sound sticker -> video reply; on static silent sticker -> !toimg hint
	replies = nil
	toMP4Msg := baseMsg
	toMP4Msg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	toMP4Msg.StickerAudioSource = stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/ogg", Data: dubbed.AudioData}}
	if err := stickerToMP4Cmd(ctx, toMP4Msg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Video == nil || len(replies[0].Video.Data) == 0 || replies[0].Video.Seconds < 1 {
		t.Fatalf("expected MP4 video reply, got %+v", replies)
	}

	replies = nil
	silentMP4Msg := baseMsg
	silentMP4Msg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	if err := stickerToMP4Cmd(ctx, silentMP4Msg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "toimg") {
		t.Fatalf("expected static silent hint mentioning toimg, got %+v", replies)
	}

	// Stripped quoted sound sticker (IsAudioSticker=true, StickerAudioSource=nil) must NOT fall through to silent video.
	replies = nil
	strippedAudioMsg := baseMsg
	strippedAudioMsg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	strippedAudioMsg.QuotedSticker = &command.StickerInfo{IsAudioSticker: true}
	if err := stickerToMP4Cmd(ctx, strippedAudioMsg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Video != nil || !strings.Contains(replies[0].Text, "stripped the audio") {
		t.Fatalf("expected stripped audio warning without silent video, got %+v", replies)
	}

	// Audio download error must NOT fall through to silent video.
	replies = nil
	errAudioMsg := baseMsg
	errAudioMsg.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: dubbed.Data}}
	errAudioMsg.StickerAudioSource = stubMediaSource{err: errors.New("expired direct path")}
	if err := stickerToMP4Cmd(ctx, errAudioMsg, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].Video != nil || !strings.Contains(replies[0].Text, "Couldn't download that sound sticker's audio track") {
		t.Fatalf("expected audio download failure message without silent video, got %+v", replies)
	}
}

func TestStickerReactLibrary_GlobalEmojiCRUDAndCrossChat(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := newFakeModerationStore()
	pngData := genSamplePNG(t)

	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	groupMsg := command.Message{
		Chat:            "group1@g.us",
		Sender:          "15550001@s.whatsapp.net",
		Group:           true,
		ModerationStore: store,
	}

	// 1. Save ❤️ (with U+FE0F variation selector) in group1.
	addHeart := groupMsg
	addHeart.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngData}}
	if err := stickerReactCmd(ctx, addHeart, []string{"add", "❤️"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Saved sticker under ❤️") {
		t.Fatalf("expected save confirmation for ❤️, got %+v", replies)
	}

	// 2. Save 🔥 via `set` alias in group1.
	replies = nil
	addFire := groupMsg
	addFire.MediaSource = stubMediaSource{att: command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngData}}
	if err := stickerReactCmd(ctx, addFire, []string{"set", "🔥"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Saved sticker under 🔥") {
		t.Fatalf("expected save confirmation for 🔥, got %+v", replies)
	}

	// 3. List from a completely different DM chat (global library).
	dmMsg := command.Message{
		Chat:            "15559999@s.whatsapp.net",
		Sender:          "15550001@s.whatsapp.net",
		Group:           false,
		ModerationStore: store,
	}
	replies = nil
	if err := stickerReactCmd(ctx, dmMsg, []string{"list"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "❤️") || !strings.Contains(replies[0].Text, "🔥") {
		t.Fatalf("expected global library list with ❤️ and 🔥, got %+v", replies)
	}

	// 4. React in DM with ❤ (without U+FE0F) -> matches ❤️ entry!
	replies = nil
	reactHeart := dmMsg
	reactHeart.QuotedMessageID = "DM-TARGET-77"
	reactHeart.QuotedSender = "15559999@s.whatsapp.net"
	if err := stickerReactCmd(ctx, reactHeart, []string{"❤"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].StickerReaction == nil {
		t.Fatalf("expected StickerReaction reply, got %+v", replies)
	}
	sr := replies[0].StickerReaction
	if sr.TargetStanzaID != "DM-TARGET-77" || len(sr.Sticker.Data) == 0 || sr.Sticker.ExifOffset == 0 || len(sr.Sticker.Emojis) == 0 {
		t.Fatalf("unexpected StickerReaction payload: %+v", sr)
	}

	// 5. React with unsaved emoji -> helpful guidance, no reaction sent.
	replies = nil
	if err := stickerReactCmd(ctx, reactHeart, []string{"💀"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].StickerReaction != nil || !strings.Contains(replies[0].Text, "No sticker saved for 💀") {
		t.Fatalf("expected missing emoji prompt, got %+v", replies)
	}

	// 6. Delete 🔥 and verify it is no longer usable.
	replies = nil
	if err := stickerReactCmd(ctx, dmMsg, []string{"del", "🔥"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Removed 🔥") {
		t.Fatalf("expected delete confirmation, got %+v", replies)
	}

	replies = nil
	if err := stickerReactCmd(ctx, reactHeart, []string{"🔥"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].StickerReaction != nil || !strings.Contains(replies[0].Text, "No sticker saved for 🔥") {
		t.Fatalf("expected 🔥 to be gone after delete, got %+v", replies)
	}
}
