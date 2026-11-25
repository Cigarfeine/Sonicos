package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

type stubMediaSource struct {
	att command.Attachment
	err error
}

func (s stubMediaSource) Download(context.Context) (command.Attachment, error) {
	return s.att, s.err
}

func TestMakeAudioSticker_MissingAndDownloadErrors(t *testing.T) {
	t.Setenv("BOT_PREFIX", ".")

	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	if err := makeAudioSticker(context.Background(), command.Message{}, nil, reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, ".stdio") {
		t.Fatalf("expected custom prefix .stdio in prompt, got %+v", replies)
	}

	replies = nil
	msg := command.Message{MediaSource: stubMediaSource{err: errors.New("boom")}}
	if err := makeAudioSticker(context.Background(), msg, nil, reply); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(replies) != 1 || replies[0].Sticker != nil {
		t.Fatalf("expected friendly text reply on download error, got %+v", replies)
	}
}

func TestMakeAudioSticker_ProducesSoundStickerFromVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}

	samplePath := filepath.Join(t.TempDir(), "sample.mp4")
	cmd := exec.Command(
		"ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=20:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", samplePath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate mp4: %v\n%s", err, string(out))
	}
	mp4, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read mp4: %v", err)
	}

	var got command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		got = r
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg := command.Message{
		MediaSource: stubMediaSource{
			att: command.Attachment{
				Kind:     command.MediaVideo,
				MIMEType: "video/mp4",
				Data:     mp4,
			},
		},
	}
	if err := makeAudioSticker(ctx, msg, nil, reply); err != nil {
		t.Fatalf("makeAudioSticker: %v", err)
	}
	if got.Sticker == nil || !got.Sticker.Audio || !got.Sticker.Animated {
		t.Fatalf("expected animated sound sticker, got %+v", got)
	}
	if len(got.Sticker.Data) == 0 || len(got.Sticker.AudioData) == 0 || len(got.Sticker.Waveform) != 64 || got.Sticker.ExifOffset == 0 {
		t.Fatalf("incomplete sound sticker payload: webp=%d audio=%d wave=%d exif=%d",
			len(got.Sticker.Data), len(got.Sticker.AudioData), len(got.Sticker.Waveform), got.Sticker.ExifOffset)
	}
}
