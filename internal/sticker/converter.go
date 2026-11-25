package sticker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

const (
	MaxInputBytes     = 20 << 20
	MaxStaticBytes    = 100 << 10
	MaxAnimatedBytes  = 500 << 10
	MaxAudioBytes     = 1 << 20
	MaxStickerSeconds = 6
	MaxAudioSeconds   = 10
)

type Converter struct {
	ffmpeg string
}

func NewConverter(ffmpeg string) *Converter {
	if strings.TrimSpace(ffmpeg) == "" {
		ffmpeg = "ffmpeg"
	}
	return &Converter{ffmpeg: ffmpeg}
}

func (c *Converter) ToWebP(ctx context.Context, attachment command.Attachment) (command.Sticker, error) {
	if len(attachment.Data) == 0 {
		return command.Sticker{}, errors.New("media attachment is empty")
	}
	if len(attachment.Data) > MaxInputBytes {
		return command.Sticker{}, fmt.Errorf("input exceeds %d MiB", MaxInputBytes>>20)
	}
	if attachment.Kind != command.MediaImage && attachment.Kind != command.MediaVideo {
		return command.Sticker{}, errors.New("sticker command requires an image or video")
	}

	inputPath, cleanup, err := writeInput(attachment.Data, suffixFor(attachment.MIMEType, attachment.Kind))
	if err != nil {
		return command.Sticker{}, err
	}
	defer cleanup()

	animated := attachment.Kind == command.MediaVideo || strings.HasPrefix(strings.ToLower(attachment.MIMEType), "image/gif")
	limit := MaxStaticBytes
	if animated {
		limit = MaxAnimatedBytes
	}
	attempts := []struct {
		quality int
		fps     int
		size    int
	}{
		{72, 15, 512},
		{58, 12, 512},
		{45, 10, 448},
		{34, 8, 384},
	}
	for _, attempt := range attempts {
		outputPath, removeOutput, err := tempPath(".webp")
		if err != nil {
			return command.Sticker{}, err
		}
		err = c.encodeWebP(ctx, inputPath, outputPath, animated, attempt.quality, attempt.fps, attempt.size)
		if err != nil {
			removeOutput()
			return command.Sticker{}, err
		}
		data, err := os.ReadFile(outputPath)
		removeOutput()
		if err != nil {
			return command.Sticker{}, fmt.Errorf("read converted WebP: %w", err)
		}
		if len(data) <= limit {
			return command.Sticker{Data: data, MIMEType: "image/webp", Animated: animated}, nil
		}
	}
	return command.Sticker{}, fmt.Errorf("converted sticker exceeds WhatsApp's %d KiB size limit", limit>>10)
}

func (c *Converter) ToOpus(ctx context.Context, attachment command.Attachment) (command.Sticker, error) {
	if len(attachment.Data) == 0 {
		return command.Sticker{}, errors.New("media attachment is empty")
	}
	if len(attachment.Data) > MaxInputBytes {
		return command.Sticker{}, fmt.Errorf("input exceeds %d MiB", MaxInputBytes>>20)
	}
	if attachment.Kind != command.MediaAudio && attachment.Kind != command.MediaVideo {
		return command.Sticker{}, errors.New("audio sticker command requires audio or video with an audio track")
	}

	inputPath, cleanup, err := writeInput(attachment.Data, suffixFor(attachment.MIMEType, attachment.Kind))
	if err != nil {
		return command.Sticker{}, err
	}
	defer cleanup()

	outputPath, removeOutput, err := tempPath(".ogg")
	if err != nil {
		return command.Sticker{}, err
	}
	defer removeOutput()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", inputPath,
		"-map", "0:a:0", "-vn", "-t", fmt.Sprint(MaxAudioSeconds),
		"-ac", "1", "-ar", "48000", "-c:a", "libopus", "-b:a", "48k",
		"-application", "audio", "-vbr", "on", "-f", "ogg", outputPath,
	}
	if err := c.run(ctx, args...); err != nil {
		return command.Sticker{}, fmt.Errorf("convert audio to Opus: %w", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return command.Sticker{}, fmt.Errorf("read converted Opus audio: %w", err)
	}
	if len(data) > MaxAudioBytes {
		return command.Sticker{}, fmt.Errorf("converted Opus audio exceeds %d KiB", MaxAudioBytes>>10)
	}
	seconds, err := c.duration(ctx, outputPath)
	if err != nil {
		return command.Sticker{}, fmt.Errorf("read Opus duration: %w", err)
	}
	return command.Sticker{Data: data, MIMEType: "audio/ogg; codecs=opus", Audio: true, Seconds: seconds}, nil
}

func (c *Converter) encodeWebP(ctx context.Context, inputPath, outputPath string, animated bool, quality, fps, size int) error {
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,format=rgba,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=0x00000000", size, size, size, size)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", inputPath}
	if animated {
		args = append(args, "-t", fmt.Sprint(MaxStickerSeconds), "-an", "-vf", fmt.Sprintf("fps=%d,%s", fps, filter), "-loop", "0", "-c:v", "libwebp_anim")
	} else {
		args = append(args, "-frames:v", "1", "-an", "-vf", filter, "-c:v", "libwebp")
	}
	args = append(args, "-quality", fmt.Sprint(quality), "-compression_level", "6", outputPath)
	return c.run(ctx, args...)
}

func (c *Converter) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, c.ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func (c *Converter) duration(ctx context.Context, path string) (uint32, error) {
	ffprobe := "ffprobe"
	if filepath.Base(c.ffmpeg) == "ffmpeg" {
		ffprobe = filepath.Join(filepath.Dir(c.ffmpeg), "ffprobe")
	}
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("run ffprobe: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", strings.TrimSpace(string(output)), err)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("converted audio duration %.2fs is outside the supported range", seconds)
	}
	if seconds > MaxAudioSeconds {
		// Opus frame alignment/lookahead can push a -t trimmed stream slightly past MaxAudioSeconds (e.g. 10.0065s).
		if seconds <= float64(MaxAudioSeconds)+0.5 {
			return MaxAudioSeconds, nil
		}
		return 0, fmt.Errorf("converted audio duration %.2fs is outside the supported range", seconds)
	}
	return uint32(math.Ceil(seconds)), nil
}

func writeInput(data []byte, suffix string) (string, func(), error) {
	path, cleanup, err := tempPath(suffix)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write media input: %w", err)
	}
	return path, cleanup, nil
}

func tempPath(suffix string) (string, func(), error) {
	file, err := os.CreateTemp("", "sonicos-media-*")
	if err != nil {
		return "", nil, fmt.Errorf("create media temp file: %w", err)
	}
	path := file.Name() + suffix
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", nil, fmt.Errorf("close media temp file: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		return "", nil, fmt.Errorf("prepare media temp file: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func suffixFor(mimeType string, kind command.MediaKind) string {
	if strings.Contains(mimeType, "/") {
		subtype := strings.TrimSpace(strings.SplitN(strings.SplitN(mimeType, ";", 2)[0], "/", 2)[1])
		switch subtype {
		case "jpeg":
			return ".jpg"
		case "quicktime":
			return ".mov"
		case "x-matroska":
			return ".mkv"
		default:
			return "." + subtype
		}
	}
	switch kind {
	case command.MediaImage:
		return ".img"
	case command.MediaVideo:
		return ".video"
	case command.MediaAudio:
		return ".audio"
	default:
		return filepath.Ext(mimeType)
	}
}
