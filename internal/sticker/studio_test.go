package sticker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func TestConverter_LiveFFmpeg_StudioSuite(t *testing.T) {
	c := requireFFmpeg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pngBytes, err := renderAudioCoverPNG(make([]byte, 64))
	if err != nil {
		t.Fatal(err)
	}
	mp3Bytes := genTestMedia(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:a", "libmp3lame", "-f", "mp3")
	mp4Anim := genTestMedia(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=20:duration=3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mp4")
	mp4WithAudio := genTestMedia(t,
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=20:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-f", "mp4",
	)

	t.Run("dub_png_plus_mp3", func(t *testing.T) {
		st, err := c.DubSoundSticker(
			ctx,
			command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngBytes},
			command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: mp3Bytes},
		)
		if err != nil {
			t.Fatalf("DubSoundSticker png+mp3: %v", err)
		}
		if !st.Audio || st.Animated || len(st.Data) == 0 || len(st.AudioData) == 0 || st.Seconds != 3 || st.ExifOffset == 0 {
			t.Fatalf("unexpected dubbed sound sticker: audio=%v anim=%v sec=%d exif=%d", st.Audio, st.Animated, st.Seconds, st.ExifOffset)
		}
	})

	t.Run("dub_animated_webp_plus_mp3", func(t *testing.T) {
		silentAnim, err := c.ToWebP(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4Anim})
		if err != nil {
			t.Fatal(err)
		}
		st, err := c.DubSoundSticker(
			ctx,
			command.Attachment{Kind: command.MediaSticker, MIMEType: "image/webp", Data: silentAnim.Data},
			command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: mp3Bytes},
		)
		if err != nil {
			t.Fatalf("DubSoundSticker webp+mp3: %v", err)
		}
		if !st.Audio || !st.Animated || len(st.AudioData) == 0 || st.ExifOffset == 0 {
			t.Fatalf("expected animated dubbed sound sticker, got audio=%v anim=%v exif=%d", st.Audio, st.Animated, st.ExifOffset)
		}
	})

	t.Run("rebrand_sticker_and_extract_toimg_toaudio", func(t *testing.T) {
		st, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4WithAudio})
		if err != nil {
			t.Fatal(err)
		}
		rebranded, err := c.RebrandSticker(ctx, st.Data, st.AudioData, "MyPack", "MyAuthor", nil)
		if err != nil {
			t.Fatalf("RebrandSticker: %v", err)
		}
		if !rebranded.Audio || !rebranded.Animated || rebranded.ExifOffset == 0 || len(rebranded.AudioData) == 0 {
			t.Fatalf("unexpected rebranded sound sticker: %+v", rebranded)
		}
		exifLen := binary.LittleEndian.Uint32(rebranded.Data[rebranded.ExifOffset+4 : rebranded.ExifOffset+8])
		var meta ExifMetadata
		if err := json.Unmarshal(rebranded.Data[rebranded.ExifOffset+8+26:rebranded.ExifOffset+8+exifLen], &meta); err != nil {
			t.Fatalf("unmarshal rebranded EXIF: %v", err)
		}
		if meta.PackName != "MyPack" || meta.Publisher != "MyAuthor" {
			t.Fatalf("expected MyPack/MyAuthor, got %+v", meta)
		}

		extractedPNG, err := c.StickerToPNG(ctx, rebranded.Data)
		if err != nil {
			t.Fatalf("StickerToPNG: %v", err)
		}
		img, err := png.Decode(bytes.NewReader(extractedPNG))
		if err != nil || img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
			t.Fatalf("expected 512x512 PNG, got err=%v bounds=%v", err, img.Bounds())
		}

		extractedAudio, err := c.ExtractAudio(ctx, command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/ogg", Data: rebranded.AudioData})
		if err != nil || len(extractedAudio.Data) == 0 || extractedAudio.Seconds != 3 {
			t.Fatalf("ExtractAudio failed: err=%v sec=%d len=%d", err, extractedAudio.Seconds, len(extractedAudio.Data))
		}
	})

	t.Run("video_note_from_video_and_image", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			att  command.Attachment
		}{
			{"video_with_audio", command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4WithAudio}},
			{"video_without_audio", command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4Anim}},
			{"still_image", command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngBytes}},
		} {
			note, err := c.ToVideoNote(ctx, tc.att)
			if err != nil {
				t.Fatalf("ToVideoNote(%s): %v", tc.name, err)
			}
			if len(note.Data) == 0 || len(note.Thumb) == 0 || note.Width != 384 || note.Height != 384 || note.Seconds < 1 {
				t.Fatalf("unexpected video note (%s): len=%d thumb=%d %dx%d sec=%d",
					tc.name, len(note.Data), len(note.Thumb), note.Width, note.Height, note.Seconds)
			}
		}
	})

	t.Run("measure_loudness_quiet_vs_earrape", func(t *testing.T) {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("ffmpeg missing")
		}
		quietOgg := genTestMedia(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-af", "volume=-20dB", "-c:a", "libopus", "-f", "ogg")
		loudOgg := genTestMedia(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-af", "volume=25dB,alimiter=limit=0.99", "-c:a", "libopus", "-f", "ogg")

		qMean, _, err := c.MeasureLoudness(ctx, quietOgg)
		if err != nil {
			t.Fatalf("MeasureLoudness quiet: %v", err)
		}
		lMean, lMax, err := c.MeasureLoudness(ctx, loudOgg)
		if err != nil {
			t.Fatalf("MeasureLoudness loud: %v", err)
		}
		if qMean > -10.0 {
			t.Fatalf("expected quiet mean < -10 dB, got %.1f dB", qMean)
		}
		if lMean < -5.0 || lMax < -2.0 {
			t.Fatalf("expected earrape mean >= -5 dB and max >= -2 dB, got mean=%.1f max=%.1f", lMean, lMax)
		}
	})

	t.Run("sticker_to_mp4_all_modes", func(t *testing.T) {
		dir := t.TempDir()
		probeOut := func(name string, data []byte) mediaProbe {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := c.probeMedia(ctx, p)
			if err != nil {
				t.Fatalf("probe %s: %v", name, err)
			}
			return info
		}

		// 1. Animated silent sticker -> MP4 without audio
		silentAnim, err := c.ToWebP(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4Anim})
		if err != nil {
			t.Fatal(err)
		}
		vid1, err := c.StickerToMP4(ctx, silentAnim.Data, nil)
		if err != nil {
			t.Fatalf("StickerToMP4(anim_silent): %v", err)
		}
		if len(vid1.Data) == 0 || len(vid1.Thumb) == 0 || vid1.Width != 512 || vid1.Height != 512 || vid1.Seconds < 1 {
			t.Fatalf("unexpected vid1 metadata: len=%d thumb=%d %dx%d sec=%d", len(vid1.Data), len(vid1.Thumb), vid1.Width, vid1.Height, vid1.Seconds)
		}
		if info := probeOut("anim_silent.mp4", vid1.Data); !info.hasVideo || info.hasAudio {
			t.Fatalf("expected video-only output for anim_silent, got %+v", info)
		}

		// 2. Animated sound sticker -> MP4 with audio
		animSound, err := c.ToSoundSticker(ctx, command.Attachment{Kind: command.MediaVideo, MIMEType: "video/mp4", Data: mp4WithAudio})
		if err != nil {
			t.Fatal(err)
		}
		vid2, err := c.StickerToMP4(ctx, animSound.Data, animSound.AudioData)
		if err != nil {
			t.Fatalf("StickerToMP4(anim_sound): %v", err)
		}
		if len(vid2.Data) == 0 || len(vid2.Thumb) == 0 || vid2.Seconds != 3 {
			t.Fatalf("unexpected vid2 metadata: len=%d thumb=%d sec=%d", len(vid2.Data), len(vid2.Thumb), vid2.Seconds)
		}
		if info := probeOut("anim_sound.mp4", vid2.Data); !info.hasVideo || !info.hasAudio {
			t.Fatalf("expected video+audio output for anim_sound, got %+v", info)
		}

		// 3. Static sound sticker -> MP4 with audio
		staticSound, err := c.DubSoundSticker(
			ctx,
			command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngBytes},
			command.Attachment{Kind: command.MediaAudio, MIMEType: "audio/mpeg", Data: mp3Bytes},
		)
		if err != nil {
			t.Fatal(err)
		}
		vid3, err := c.StickerToMP4(ctx, staticSound.Data, staticSound.AudioData)
		if err != nil {
			t.Fatalf("StickerToMP4(static_sound): %v", err)
		}
		if len(vid3.Data) == 0 || len(vid3.Thumb) == 0 || vid3.Seconds != 3 {
			t.Fatalf("unexpected vid3 metadata: len=%d thumb=%d sec=%d", len(vid3.Data), len(vid3.Thumb), vid3.Seconds)
		}
		if info := probeOut("static_sound.mp4", vid3.Data); !info.hasVideo || !info.hasAudio {
			t.Fatalf("expected video+audio output for static_sound, got %+v", info)
		}

		// 4. Static silent sticker -> ErrStaticSilentSticker
		staticSilent, err := c.ToWebP(ctx, command.Attachment{Kind: command.MediaImage, MIMEType: "image/png", Data: pngBytes})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.StickerToMP4(ctx, staticSilent.Data, nil); !errors.Is(err, ErrStaticSilentSticker) {
			t.Fatalf("expected ErrStaticSilentSticker, got %v", err)
		}
	})
}
