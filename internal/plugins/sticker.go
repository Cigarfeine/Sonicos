package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"github.com/sonicos/whatsapp-bot/internal/sticker"
)

var stickerConverter = sticker.NewConverter(os.Getenv("FFMPEG_PATH"))

func init() {
	command.Register(command.Command{
		Name:        "sticker",
		Aliases:     []string{"s"},
		Category:    "Media & Stickers",
		Description: "Create a sticker from an image or short video",
		Handler:     makeSticker,
	})
	command.Register(command.Command{
		Name:        "stdio",
		Aliases:     []string{"audiosticker"},
		Category:    "Media & Stickers",
		Description: "Create a playable WhatsApp sound sticker from audio or video",
		Handler:     makeAudioSticker,
	})
}

func makeSticker(ctx context.Context, message command.Message, _ []string, reply command.ReplyFunc) error {
	if message.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Attach or reply to an image or video, then send %ssticker.", botPrefix())})
	}
	media, err := message.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("load sticker source", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download the image or video. Try a different message and retry."})
	}
	converted, err := stickerConverter.ToWebP(ctx, media)
	if err != nil {
		slog.Error("create WebP sticker", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that media. Try a shorter video or another image."})
	}
	return reply(ctx, command.Reply{Sticker: &converted})
}

func makeAudioSticker(ctx context.Context, message command.Message, _ []string, reply command.ReplyFunc) error {
	if message.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Attach or reply to audio or a video with audio, then send %sstdio.", botPrefix())})
	}
	media, err := message.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("load audio sticker source", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download the audio or video. Try a different message and retry."})
	}
	converted, err := stickerConverter.ToSoundSticker(ctx, media)
	if err != nil {
		slog.Error("create sound sticker", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that media into a sound sticker. It must be audio or a video with an audio track."})
	}
	var peak byte
	for _, v := range converted.Waveform {
		if v > peak {
			peak = v
		}
	}
	slog.Debug("sound sticker ready",
		"webp_bytes", len(converted.Data),
		"audio_bytes", len(converted.AudioData),
		"seconds", converted.Seconds,
		"animated", converted.Animated,
		"exif_offset", converted.ExifOffset,
		"waveform_peak", peak,
	)
	return reply(ctx, command.Reply{Sticker: &converted})
}

func botPrefix() string {
	if prefix := strings.TrimSpace(os.Getenv("BOT_PREFIX")); prefix != "" {
		return prefix
	}
	return "!"
}
