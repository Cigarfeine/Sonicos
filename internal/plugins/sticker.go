package plugins

import (
	"context"
	"log/slog"
	"os"

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
		Description: "Create an Opus audio sticker from audio or video",
		Handler:     makeAudioSticker,
	})
}

func makeSticker(ctx context.Context, message command.Message, _ []string, reply command.ReplyFunc) error {
	if message.MediaSource == nil {
		return reply(ctx, command.Reply{Text: "Attach or reply to an image or video, then send !sticker."})
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
		return reply(ctx, command.Reply{Text: "Attach or reply to audio or a video with audio, then send !stdio."})
	}
	media, err := message.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("load audio sticker source", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download the audio. Try a different message and retry."})
	}
	converted, err := stickerConverter.ToOpus(ctx, media)
	if err != nil {
		slog.Error("create Opus audio sticker", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that audio. It must be audio or a video with an audio track."})
	}
	return reply(ctx, command.Reply{Sticker: &converted})
}
