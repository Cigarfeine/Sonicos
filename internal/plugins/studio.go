package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"github.com/sonicos/whatsapp-bot/internal/sticker"
)

const stageTTLSeconds = 600

func init() {
	for _, cmd := range []command.Command{
		{
			Name:        "dub",
			Aliases:     []string{"dubsticker"},
			Category:    "Media & Stickers",
			Description: "Combine any image/sticker/video with separate audio into a Sound Sticker",
			Handler:     dubStickerCmd,
		},
		{
			Name:        "steal",
			Aliases:     []string{"take", "wm"},
			Category:    "Media & Stickers",
			Description: "Rebrand a replied sticker or sound sticker with custom pack | author",
			Handler:     stealStickerCmd,
		},
		{
			Name:        "toimg",
			Aliases:     []string{"sticker2img"},
			Category:    "Media & Stickers",
			Description: "Convert a replied sticker back into a PNG image",
			Handler:     stickerToImgCmd,
		},
		{
			Name:        "tomp4",
			Aliases:     []string{"tovideo", "tovid"},
			Category:    "Media & Stickers",
			Description: "Convert a replied animated sticker or sound sticker into an MP4 video",
			Handler:     stickerToMP4Cmd,
		},
		{
			Name:        "toaudio",
			Aliases:     []string{"tomp3", "extractaudio"},
			Category:    "Media & Stickers",
			Description: "Extract playable audio from a sound sticker or video",
			Handler:     toAudioCmd,
		},
		{
			Name:        "ptv",
			Aliases:     []string{"round", "videonote"},
			Category:    "Media & Stickers",
			Description: "Convert a video or image into a circular WhatsApp video note",
			Handler:     videoNoteCmd,
		},
		{
			Name:        "sreact",
			Aliases:     []string{"stickerreact"},
			Category:    "Media & Stickers",
			Description: "Sticker reaction library: !sreact add <emoji>, !sreact <emoji>, !sreact list, !sreact del <emoji>",
			Handler:     stickerReactCmd,
		},
	} {
		command.Register(cmd)
	}
}

func stageKeyFor(kind string, msg command.Message) string {
	return kind + ":" + msg.Chat + ":" + jidUser(msg.Sender)
}

func dubStickerCmd(ctx context.Context, msg command.Message, args []string, reply command.ReplyFunc) error {
	if msg.ModerationStore == nil {
		return reply(ctx, command.Reply{Text: "Staging storage is unavailable right now."})
	}
	key := stageKeyFor("dub", msg)

	if len(args) > 0 && (strings.EqualFold(args[0], "clear") || strings.EqualFold(args[0], "reset")) {
		if err := msg.ModerationStore.ClearStage(ctx, key); err != nil {
			slog.Warn("clear dub stage", "error", err)
		}
		return reply(ctx, command.Reply{Text: "Cleared staged dub media."})
	}

	if msg.MediaSource == nil {
		return reply(ctx, command.Reply{
			Text: fmt.Sprintf("Step 1: Send or reply to an image/sticker/video (or audio) with %sdub.\nStep 2: Send or reply to the matching audio (or visual) with %sdub.", botPrefix(), botPrefix()),
		})
	}

	att, err := msg.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("download dub media", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that media. Try again."})
	}

	staged, hasStaged, err := msg.ModerationStore.LoadStage(ctx, key)
	if err != nil {
		slog.Error("load dub stage", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't access staged media right now."})
	}

	isVisualKind := func(k command.MediaKind) bool {
		return k == command.MediaImage || k == command.MediaVideo || k == command.MediaSticker
	}

	if hasStaged {
		stagedAtt := command.Attachment{Kind: staged.Kind, MIMEType: staged.MIMEType, Data: staged.Data}
		switch {
		case isVisualKind(staged.Kind) && (att.Kind == command.MediaAudio || att.Kind == command.MediaVideo):
			st, err := stickerConverter.DubSoundSticker(ctx, stagedAtt, att)
			if err != nil {
				slog.Error("dub sound sticker (visual+audio)", "error", err)
				return reply(ctx, command.Reply{Text: "Couldn't combine that visual and audio into a sound sticker."})
			}
			_ = msg.ModerationStore.ClearStage(ctx, key)
			return reply(ctx, command.Reply{Sticker: &st})
		case staged.Kind == command.MediaAudio && isVisualKind(att.Kind):
			st, err := stickerConverter.DubSoundSticker(ctx, att, stagedAtt)
			if err != nil {
				slog.Error("dub sound sticker (audio+visual)", "error", err)
				return reply(ctx, command.Reply{Text: "Couldn't combine that audio and visual into a sound sticker."})
			}
			_ = msg.ModerationStore.ClearStage(ctx, key)
			return reply(ctx, command.Reply{Sticker: &st})
		}
	}

	if err := msg.ModerationStore.SaveStage(ctx, key, command.StagedMedia{
		Kind:     att.Kind,
		MIMEType: att.MIMEType,
		Data:     att.Data,
	}, stageTTLSeconds); err != nil {
		slog.Error("save dub stage", "error", err)
		return reply(ctx, command.Reply{Text: "Media is too large to stage (max 10 MiB). Try a shorter clip."})
	}

	if att.Kind == command.MediaAudio {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("🎙️ Audio staged! Now reply to an image, sticker, or video with %sdub.", botPrefix())})
	}
	return reply(ctx, command.Reply{Text: fmt.Sprintf("🖼️ Visual staged! Now reply to a voice note or audio file with %sdub.", botPrefix())})
}

func stealStickerCmd(ctx context.Context, msg command.Message, args []string, reply command.ReplyFunc) error {
	if msg.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to a sticker or sound sticker with %ssteal [pack | author].", botPrefix())})
	}
	media, err := msg.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("download sticker for steal", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that sticker."})
	}
	if media.Kind != command.MediaSticker {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to a sticker with %ssteal [pack | author].", botPrefix())})
	}

	var audioData []byte
	if msg.StickerAudioSource != nil {
		if audioAtt, aerr := msg.StickerAudioSource.Download(ctx); aerr == nil {
			audioData = audioAtt.Data
		} else {
			slog.Warn("download sound sticker audio for steal", "error", aerr)
		}
	}

	packName := "Sonicos"
	author := "Sonicos"
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw != "" {
		parts := strings.SplitN(raw, "|", 2)
		if p := strings.TrimSpace(parts[0]); p != "" {
			packName = p
		}
		if len(parts) == 2 {
			if a := strings.TrimSpace(parts[1]); a != "" {
				author = a
			}
		}
	}

	st, err := stickerConverter.RebrandSticker(ctx, media.Data, audioData, packName, author, nil)
	if err != nil {
		slog.Error("rebrand sticker", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't rebrand that sticker."})
	}
	return reply(ctx, command.Reply{Sticker: &st})
}

func stickerToImgCmd(ctx context.Context, msg command.Message, _ []string, reply command.ReplyFunc) error {
	if msg.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to a sticker with %stoimg.", botPrefix())})
	}
	media, err := msg.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("download sticker for toimg", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that sticker."})
	}
	if media.Kind != command.MediaSticker {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to a sticker with %stoimg.", botPrefix())})
	}
	pngData, err := stickerConverter.StickerToPNG(ctx, media.Data)
	if err != nil {
		slog.Error("convert sticker to png", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that sticker to an image."})
	}
	return reply(ctx, command.Reply{
		Image: &command.ImageReply{Data: pngData, MIMEType: "image/png"},
	})
}

func stickerToMP4Cmd(ctx context.Context, msg command.Message, _ []string, reply command.ReplyFunc) error {
	if msg.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to an animated sticker or sound sticker with %stomp4.", botPrefix())})
	}
	media, err := msg.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("download sticker for tomp4", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that sticker."})
	}
	if media.Kind != command.MediaSticker {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to an animated sticker or sound sticker with %stomp4.", botPrefix())})
	}

	var audioData []byte
	if msg.StickerAudioSource != nil {
		audioAtt, aerr := msg.StickerAudioSource.Download(ctx)
		if aerr != nil {
			slog.Error("download sound sticker audio for tomp4", "error", aerr)
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("Couldn't download that sound sticker's audio track. Forward or resend the sound sticker once, then reply to it with %stomp4.", botPrefix()),
			})
		}
		audioData = audioAtt.Data
	} else if msg.QuotedSticker != nil && msg.QuotedSticker.IsAudioSticker {
		slog.Warn("quoted sound sticker missing downloadable audio source", "chat", msg.Chat, "quoted_id", msg.QuotedMessageID)
		return reply(ctx, command.Reply{
			Text: fmt.Sprintf("WhatsApp stripped the audio from that quoted sound sticker. Forward or resend the sound sticker once, then reply to it with %stomp4.", botPrefix()),
		})
	}

	videoReply, err := stickerConverter.StickerToMP4(ctx, media.Data, audioData)
	if err != nil {
		if errors.Is(err, sticker.ErrStaticSilentSticker) {
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("That's a static sticker with no audio. Use %stoimg to convert it to an image.", botPrefix()),
			})
		}
		slog.Error("convert sticker to mp4", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that sticker to a video."})
	}
	return reply(ctx, command.Reply{Video: &videoReply})
}

func toAudioCmd(ctx context.Context, msg command.Message, _ []string, reply command.ReplyFunc) error {
	src := msg.StickerAudioSource
	if src == nil {
		src = msg.MediaSource
	}
	if src == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Reply to a sound sticker or video with %stoaudio.", botPrefix())})
	}
	media, err := src.Download(ctx)
	if err != nil {
		slog.Error("download media for toaudio", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that media."})
	}
	audioReply, err := stickerConverter.ExtractAudio(ctx, media)
	if err != nil {
		slog.Error("extract audio", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't extract audio. Reply to a sound sticker, audio, or video with sound."})
	}
	return reply(ctx, command.Reply{Audio: &audioReply})
}

func videoNoteCmd(ctx context.Context, msg command.Message, _ []string, reply command.ReplyFunc) error {
	if msg.MediaSource == nil {
		return reply(ctx, command.Reply{Text: fmt.Sprintf("Attach or reply to a video or image with %sptv.", botPrefix())})
	}
	media, err := msg.MediaSource.Download(ctx)
	if err != nil {
		slog.Error("download media for ptv", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't download that media."})
	}
	note, err := stickerConverter.ToVideoNote(ctx, media)
	if err != nil {
		slog.Error("convert to video note", "error", err)
		return reply(ctx, command.Reply{Text: "Couldn't convert that media into a circular video note."})
	}
	return reply(ctx, command.Reply{VideoNote: &note})
}

func stickerReactCmd(ctx context.Context, msg command.Message, args []string, reply command.ReplyFunc) error {
	ownerID := jidUser(msg.Sender)
	defaultKey := "sreact:global:" + ownerID
	prefix := botPrefix()

	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch sub {
	case "list", "ls", "lib":
		if msg.ModerationStore == nil {
			return reply(ctx, command.Reply{Text: "Reaction library storage is unavailable right now."})
		}
		items, err := msg.ModerationStore.ListReactionStickers(ctx, ownerID)
		if err != nil {
			slog.Error("list sreact library", "error", err)
			return reply(ctx, command.Reply{Text: "Couldn't load your reaction sticker library right now."})
		}
		if len(items) == 0 {
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("Your sticker reaction library is empty.\nReply to any sticker with %ssreact add 😂 to save it.", prefix),
			})
		}
		emojis := make([]string, 0, len(items))
		for _, item := range items {
			emojis = append(emojis, item.Emoji)
		}
		return reply(ctx, command.Reply{
			Text: fmt.Sprintf("🎭 *Sticker Reaction Library (%d):*\n%s\n\nReply to any message with `%ssreact <emoji>` to react, or `%ssreact del <emoji>` to remove.", len(items), strings.Join(emojis, "  "), prefix, prefix),
		})

	case "del", "delete", "rm", "remove":
		if msg.ModerationStore == nil {
			return reply(ctx, command.Reply{Text: "Reaction library storage is unavailable right now."})
		}
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return reply(ctx, command.Reply{Text: fmt.Sprintf("Specify which emoji to remove, e.g. %ssreact del 😂.", prefix)})
		}
		emoji := strings.TrimSpace(args[1])
		removed, err := msg.ModerationStore.DeleteReactionSticker(ctx, ownerID, emoji)
		if err != nil {
			slog.Error("delete sreact sticker", "error", err)
			return reply(ctx, command.Reply{Text: "Couldn't remove that sticker right now."})
		}
		if !removed {
			return reply(ctx, command.Reply{Text: fmt.Sprintf("No sticker was saved for %s.", emoji)})
		}
		return reply(ctx, command.Reply{Text: fmt.Sprintf("🗑️ Removed %s from your reaction library.", emoji)})

	case "clear":
		if msg.ModerationStore != nil {
			_ = msg.ModerationStore.ClearStage(ctx, defaultKey)
		}
		return reply(ctx, command.Reply{Text: "Cleared staged reaction sticker."})

	case "add", "save", "set":
		if msg.ModerationStore == nil {
			return reply(ctx, command.Reply{Text: "Reaction library storage is unavailable right now."})
		}
		if msg.MediaSource == nil {
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("Reply to a sticker, image, or video with %ssreact add <emoji> (e.g. %ssreact add 😂).", prefix, prefix),
			})
		}
		media, err := msg.MediaSource.Download(ctx)
		if err != nil {
			slog.Error("download sreact sticker", "error", err)
			return reply(ctx, command.Reply{Text: "Couldn't download that sticker or media."})
		}

		emoji := ""
		if len(args) >= 2 {
			emoji = strings.TrimSpace(args[1])
		}
		var emojis []string
		if emoji != "" {
			emojis = []string{emoji}
		}

		st, err := mediaToReactionSticker(ctx, media, emojis)
		if err != nil {
			slog.Error("prepare sreact sticker", "error", err)
			return reply(ctx, command.Reply{Text: "Couldn't prepare that sticker for reactions."})
		}

		if emoji != "" {
			if err := msg.ModerationStore.SaveReactionSticker(ctx, ownerID, emoji, command.SavedReactionSticker{
				Emoji:    emoji,
				Data:     st.Data,
				Animated: st.Animated,
			}); err != nil {
				slog.Error("save sreact library entry", "error", err)
				return reply(ctx, command.Reply{Text: "Couldn't save that sticker to your library (max 1 MiB / 100 entries)."})
			}
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("✅ Saved sticker under %s! Reply to any message in any chat with %ssreact %s.", emoji, prefix, emoji),
			})
		}

		if err := msg.ModerationStore.SaveStage(ctx, defaultKey, command.StagedMedia{
			Kind:     command.MediaSticker,
			MIMEType: "image/webp",
			Data:     st.Data,
			Animated: st.Animated,
		}, stageTTLSeconds); err != nil {
			return reply(ctx, command.Reply{Text: "Reaction sticker is too large to stage."})
		}
		return reply(ctx, command.Reply{Text: fmt.Sprintf("✅ Reaction sticker saved! Reply to any message with %ssreact to react with it (or use %ssreact add 😂 for your library).", prefix, prefix)})
	}

	if len(args) > 0 {
		emoji := strings.TrimSpace(args[0])
		if msg.QuotedMessageID == "" {
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("Reply to a message with %ssreact %s to react.\nManage library: %ssreact add <emoji>, %ssreact list, %ssreact del <emoji>.", prefix, emoji, prefix, prefix, prefix),
			})
		}
		if msg.ModerationStore == nil {
			return reply(ctx, command.Reply{Text: "Reaction library storage is unavailable right now."})
		}
		saved, ok, err := msg.ModerationStore.GetReactionSticker(ctx, ownerID, emoji)
		if err != nil {
			slog.Error("get sreact library entry", "error", err)
			return reply(ctx, command.Reply{Text: "Couldn't access your reaction sticker library right now."})
		}
		if !ok || len(saved.Data) == 0 {
			return reply(ctx, command.Reply{
				Text: fmt.Sprintf("No sticker saved for %s. Reply to a sticker with %ssreact add %s first, or check %ssreact list.", emoji, prefix, emoji, prefix),
			})
		}
		reactEmoji := saved.Emoji
		if reactEmoji == "" {
			reactEmoji = emoji
		}
		return reply(ctx, command.Reply{
			StickerReaction: &command.StickerReaction{
				Sticker: command.Sticker{
					Data:       saved.Data,
					MIMEType:   "image/webp",
					Animated:   saved.Animated || sticker.CountWebpAnimFrames(saved.Data) > 1,
					ExifOffset: sticker.FindWebpExifOffset(saved.Data),
					Emojis:     []string{reactEmoji},
				},
				TargetStanzaID:    msg.QuotedMessageID,
				TargetParticipant: msg.QuotedSender,
				TargetFromMe:      msg.QuotedFromMe,
			},
		})
	}

	if msg.QuotedMessageID == "" {
		return reply(ctx, command.Reply{
			Text: fmt.Sprintf("Reply to a message with %ssreact <emoji> (or %ssreact for your default).\nSave a sticker with %ssreact add 😂, view all with %ssreact list.", prefix, prefix, prefix, prefix),
		})
	}

	var reactSticker command.Sticker
	var haveSticker bool
	if msg.MediaSource != nil && msg.QuotedSticker == nil {
		if media, err := msg.MediaSource.Download(ctx); err == nil {
			if st, serr := mediaToReactionSticker(ctx, media, nil); serr == nil {
				reactSticker = st
				haveSticker = true
			}
		}
	}
	if !haveSticker && msg.ModerationStore != nil {
		if staged, ok, err := msg.ModerationStore.LoadStage(ctx, defaultKey); err == nil && ok && len(staged.Data) > 0 {
			reactSticker = command.Sticker{
				Data:       staged.Data,
				MIMEType:   "image/webp",
				Animated:   staged.Animated || sticker.CountWebpAnimFrames(staged.Data) > 1,
				ExifOffset: sticker.FindWebpExifOffset(staged.Data),
			}
			haveSticker = true
		}
	}
	if !haveSticker {
		return reply(ctx, command.Reply{
			Text: fmt.Sprintf("No reaction sticker set. Reply to a sticker with %ssreact add 😂 (or %ssreact set) first.", prefix, prefix),
		})
	}

	return reply(ctx, command.Reply{
		StickerReaction: &command.StickerReaction{
			Sticker:           reactSticker,
			TargetStanzaID:    msg.QuotedMessageID,
			TargetParticipant: msg.QuotedSender,
			TargetFromMe:      msg.QuotedFromMe,
		},
	})
}

func mediaToReactionSticker(ctx context.Context, media command.Attachment, emojis []string) (command.Sticker, error) {
	if len(emojis) == 0 {
		emojis = []string{"✨"}
	}
	if media.Kind == command.MediaSticker {
		return stickerConverter.RebrandSticker(ctx, media.Data, nil, "Sonicos Reactions", "Sonicos", emojis)
	}
	st, err := stickerConverter.ToWebP(ctx, media)
	if err != nil {
		return command.Sticker{}, err
	}
	st.Emojis = emojis
	return st, nil
}
