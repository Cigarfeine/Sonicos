package sticker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func buildSyntheticWebp(chunkFourcc string, payloadLen int) []byte {
	vp8x := make([]byte, 18)
	copy(vp8x[0:4], "VP8X")
	binary.LittleEndian.PutUint32(vp8x[4:8], 10)
	var flags uint32
	if chunkFourcc == "ANMF" || chunkFourcc == "ANIM" {
		flags |= 0x02
	}
	binary.LittleEndian.PutUint32(vp8x[8:12], flags)

	payload := bytes.Repeat([]byte{0x55}, payloadLen)
	paddedPayloadLen := len(payload) + (len(payload) & 1)
	chunk := make([]byte, 8+paddedPayloadLen)
	copy(chunk[0:4], chunkFourcc)
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(payload)))
	copy(chunk[8:], payload)

	body := append(vp8x, chunk...)
	riff := make([]byte, 12)
	copy(riff[0:4], "RIFF")
	binary.LittleEndian.PutUint32(riff[4:8], uint32(4+len(body)))
	copy(riff[8:12], "WEBP")
	return append(riff, body...)
}

func TestInjectWebpExifAndFindOffset(t *testing.T) {
	raw := buildSyntheticWebp("ANMF", 77) // odd payload length to test even padding
	meta := ExifMetadata{
		PackID:    "com.sonicos.sound.1234abcd",
		PackName:  "Sonicos Sound",
		Publisher: "Sonicos",
		Emojis:    []string{"🔊"},
	}

	out, off, err := InjectWebpExif(raw, meta, 512, 512)
	if err != nil {
		t.Fatalf("InjectWebpExif: %v", err)
	}
	expectedOffset := uint32(12 + 18 + 8 + 78)
	if off != expectedOffset {
		t.Fatalf("expected EXIF offset %d, got %d", expectedOffset, off)
	}
	if got := FindWebpExifOffset(out); got != off {
		t.Fatalf("FindWebpExifOffset = %d, want %d", got, off)
	}
	// Verify VP8X has EXIF (0x08) and ANIM (0x02) bits set
	if out[12+8]&0x0a != 0x0a {
		t.Fatalf("expected VP8X EXIF+ANIM flags, got 0x%02x", out[12+8])
	}
	// Verify TIFF header + JSON payload at exifOffset
	exifLen := binary.LittleEndian.Uint32(out[off+4 : off+8])
	exifPayload := out[off+8 : off+8+exifLen]
	if string(exifPayload[:2]) != "MM" {
		t.Fatalf("expected TIFF MM header, got %q", exifPayload[:2])
	}
	var decoded ExifMetadata
	if err := json.Unmarshal(exifPayload[26:], &decoded); err != nil {
		t.Fatalf("unmarshal EXIF JSON: %v", err)
	}
	if decoded.PackID != meta.PackID || decoded.PackName != meta.PackName || len(decoded.Emojis) != 1 || decoded.Emojis[0] != "🔊" {
		t.Fatalf("unexpected decoded metadata: %+v", decoded)
	}
}

func TestSuffixForSanitization(t *testing.T) {
	cases := []struct {
		mime string
		kind command.MediaKind
		want string
	}{
		{"image/jpeg", command.MediaImage, ".jpg"},
		{"video/quicktime", command.MediaVideo, ".mov"},
		{"video/x-matroska", command.MediaVideo, ".mkv"},
		{"audio/ogg; codecs=opus", command.MediaAudio, ".ogg"},
		{"audio/foo/bar", command.MediaAudio, ".audio"},
		{"audio/..", command.MediaAudio, ".audio"},
		{"", command.MediaVideo, ".video"},
	}
	for _, tc := range cases {
		if got := suffixFor(tc.mime, tc.kind); got != tc.want {
			t.Errorf("suffixFor(%q, %q) = %q, want %q", tc.mime, tc.kind, got, tc.want)
		}
	}
}

func TestFFprobePathResolution(t *testing.T) {
	cases := []struct {
		ffmpeg string
		want   string
	}{
		{"ffmpeg", "ffprobe"},
		{"/usr/bin/ffmpeg", "/usr/bin/ffprobe"},
		{"./ffmpeg", "." + string(filepath.Separator) + "ffprobe"},
		{"/opt/custom/ffmpeg-static", "ffprobe"},
	}
	for _, tc := range cases {
		c := NewConverter(tc.ffmpeg)
		if got := c.ffprobePath(); got != tc.want {
			t.Errorf("NewConverter(%q).ffprobePath() = %q, want %q", tc.ffmpeg, got, tc.want)
		}
	}
}

func TestRenderAudioCoverPNG(t *testing.T) {
	wave := make([]byte, 64)
	for i := range wave {
		wave[i] = byte((i * 7) % 100)
	}
	pngBytes, err := renderAudioCoverPNG(wave)
	if err != nil {
		t.Fatalf("renderAudioCoverPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
		t.Fatalf("expected 512x512 cover, got %v", img.Bounds())
	}
}

func requireFFmpeg(t *testing.T) *Converter {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed in PATH")
	}
	return NewConverter("ffmpeg")
}

func genTestMedia(t *testing.T, args ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "sample")
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	full = append(full, out)
	cmd := exec.Command("ffmpeg", full...)
	if outLog, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg sample generation failed: %v\n%s", err, string(outLog))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return data
}

func TestConverter_LiveFFmpeg_ToWebP_And_ToSoundSticker(t *testing.T) {
	c := requireFFmpeg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("static_png_to_webp", func(t *testing.T) {
		pngData, err := renderAudioCoverPNG(make([]byte, 64))
		if err != nil {
			t.Fatal(err)
		}
		st, err := c.ToWebP(ctx, command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngData})
		if err != nil {
			t.Fatalf("ToWebP static: %v", err)
		}
		if st.Animated || len(st.Data) == 0 || len(st.Data) > MaxStaticBytes || st.ExifOffset == 0 {
			t.Fatalf("unexpected static sticker: animated=%v len=%d exif=%d", st.Animated, len(st.Data), st.ExifOffset)
		}
	})

	t.Run("video_to_animated_webp", func(t *testing.T) {
		mp4 := genTestMedia(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=24:duration=4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mp4")
		st, err := c.ToWebP(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4})
		if err != nil {
			t.Fatalf("ToWebP video: %v", err)
		}
		if !st.Animated || len(st.Data) == 0 || len(st.Data) > MaxAnimatedBytes || st.ExifOffset == 0 {
			t.Fatalf("unexpected animated sticker: animated=%v len=%d exif=%d", st.Animated, len(st.Data), st.ExifOffset)
		}
	})

	t.Run("video_with_audio_to_sound_sticker", func(t *testing.T) {
		mp4 := genTestMedia(t,
			"-f", "lavfi", "-i", "testsrc=size=480x360:rate=24:duration=4",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=4",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-f", "mp4",
		)
		st, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4})
		if err != nil {
			t.Fatalf("ToSoundSticker video: %v", err)
		}
		if !st.Audio || !st.Animated || len(st.Data) == 0 || len(st.Data) > MaxAnimatedBytes {
			t.Fatalf("unexpected video sound sticker visual: audio=%v anim=%v len=%d", st.Audio, st.Animated, len(st.Data))
		}
		if len(st.AudioData) == 0 || len(st.AudioData) > MaxAudioBytes || st.Seconds != 4 {
			t.Fatalf("unexpected video sound sticker audio: len=%d sec=%d", len(st.AudioData), st.Seconds)
		}
		if st.ExifOffset == 0 || len(st.Waveform) != 64 {
			t.Fatalf("expected exifOffset > 0 and 64B waveform, got exif=%d wave=%d", st.ExifOffset, len(st.Waveform))
		}
	})

	t.Run("long_video_trims_to_25s", func(t *testing.T) {
		mp4 := genTestMedia(t,
			"-f", "lavfi", "-i", "color=c=blue:s=320x240:r=15:d=27",
			"-f", "lavfi", "-i", "sine=frequency=523:duration=27",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-f", "mp4",
		)
		st, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4})
		if err != nil {
			t.Fatalf("ToSoundSticker long video: %v", err)
		}
		if st.Seconds != MaxSoundStickerSeconds {
			t.Fatalf("expected duration capped at %ds, got %ds", MaxSoundStickerSeconds, st.Seconds)
		}
	})

	t.Run("audio_only_mp3_generates_cover_sticker", func(t *testing.T) {
		mp3 := genTestMedia(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:a", "libmp3lame", "-f", "mp3")
		st, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: mp3})
		if err != nil {
			t.Fatalf("ToSoundSticker mp3: %v", err)
		}
		if !st.Audio || st.Animated || len(st.Data) == 0 || len(st.Data) > MaxStaticBytes {
			t.Fatalf("unexpected audio-only sound sticker: audio=%v anim=%v len=%d", st.Audio, st.Animated, len(st.Data))
		}
		if st.Seconds != 3 || len(st.Waveform) != 64 || st.ExifOffset == 0 {
			t.Fatalf("unexpected audio-only metadata: sec=%d wave=%d exif=%d", st.Seconds, len(st.Waveform), st.ExifOffset)
		}
	})

	t.Run("mp3_with_embedded_cover_art", func(t *testing.T) {
		dir := t.TempDir()
		rawMp3 := filepath.Join(dir, "raw.mp3")
		coverPng := filepath.Join(dir, "cover.png")
		outMp3 := filepath.Join(dir, "cover.mp3")
		if err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:a", "libmp3lame", rawMp3).Run(); err != nil {
			t.Fatal(err)
		}
		var pngBuf bytes.Buffer
		_ = png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 128, 128)))
		if err := os.WriteFile(coverPng, pngBuf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", rawMp3, "-i", coverPng, "-map", "0:a", "-map", "1:v", "-c:a", "copy", "-c:v", "mjpeg", "-id3v2_version", "3", "-disposition:v:0", "attached_pic", outMp3).CombinedOutput(); err != nil {
			t.Fatalf("build cover mp3: %v\n%s", err, string(out))
		}
		data, err := os.ReadFile(outMp3)
		if err != nil {
			t.Fatal(err)
		}
		st, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: data})
		if err != nil {
			t.Fatalf("ToSoundSticker cover mp3: %v", err)
		}
		if !st.Audio || st.Animated || st.Seconds != 3 || st.ExifOffset == 0 {
			t.Fatalf("unexpected cover mp3 sound sticker: audio=%v anim=%v sec=%d exif=%d", st.Audio, st.Animated, st.Seconds, st.ExifOffset)
		}
	})

	t.Run("video_without_audio_fails", func(t *testing.T) {
		mp4 := genTestMedia(t, "-f", "lavfi", "-i", "color=c=red:s=128x128:d=2", "-c:v", "libx264", "-f", "mp4")
		if _, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4}); err == nil {
			t.Fatal("expected error for video without audio stream")
		}
	})

	t.Run("image_fails_sound_sticker", func(t *testing.T) {
		if _, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: []byte("fake")}); err == nil {
			t.Fatal("expected error for image input to ToSoundSticker")
		}
	})
}
