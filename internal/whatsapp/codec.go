package whatsapp

import (
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func ShouldIgnoreMessage(evt *events.Message) bool {
	return evt == nil || evt.Info.Chat.IsBroadcastList()
}

func DecodeMessage(client *whatsmeow.Client, evt *events.Message) (command.Message, bool) {
	if ShouldIgnoreMessage(evt) || evt.Message == nil {
		return command.Message{}, false
	}
	text := extractText(evt.Message)
	if text == "" {
		return command.Message{}, false
	}
	ctxInfo := messageContextInfo(evt.Message)
	var mentioned []string
	var quotedSender, quotedID string
	if ctxInfo != nil {
		mentioned = append([]string(nil), ctxInfo.GetMentionedJID()...)
		quotedSender = ctxInfo.GetParticipant()
		quotedID = ctxInfo.GetStanzaID()
	}
	return command.Message{
		ID:              string(evt.Info.ID),
		Chat:            evt.Info.Chat.String(),
		Sender:          evt.Info.Sender.ToNonAD().String(),
		Text:            text,
		Group:           evt.Info.IsGroup,
		MentionedJIDs:   mentioned,
		QuotedSender:    quotedSender,
		QuotedMessageID: quotedID,
		MediaSource:     newMediaSource(client, evt.Message),
		GroupService:    newGroupService(client),
	}, true
}

func EncodeQuotedReply(evt *events.Message, text string, mentions ...string) *waE2E.Message {
	ctxInfo := quotedContext(evt)
	if len(mentions) > 0 {
		ctxInfo.MentionedJID = mentions
	}
	return &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: ctxInfo,
		},
	}
}

func quotedMessage(message *waE2E.Message) *waE2E.Message {
	if info := messageContextInfo(message); info != nil {
		return info.GetQuotedMessage()
	}
	return nil
}

func messageContextInfo(message *waE2E.Message) *waE2E.ContextInfo {
	if message == nil {
		return nil
	}
	for _, info := range []*waE2E.ContextInfo{
		message.GetExtendedTextMessage().GetContextInfo(),
		message.GetImageMessage().GetContextInfo(),
		message.GetVideoMessage().GetContextInfo(),
		message.GetPtvMessage().GetContextInfo(),
		message.GetAudioMessage().GetContextInfo(),
		message.GetDocumentMessage().GetContextInfo(),
	} {
		if info != nil {
			return info
		}
	}
	return nil
}

func extractText(message *waE2E.Message) string {
	for _, text := range []string{
		message.GetConversation(),
		message.GetExtendedTextMessage().GetText(),
		message.GetImageMessage().GetCaption(),
		message.GetVideoMessage().GetCaption(),
		message.GetPtvMessage().GetCaption(),
		message.GetDocumentMessage().GetCaption(),
	} {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}
