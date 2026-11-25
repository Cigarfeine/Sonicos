package whatsapp

import (
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func ShouldIgnoreMessage(evt *events.Message) bool {
	return evt == nil || evt.Info.Chat.IsBroadcastList()
}

func DecodeMessage(evt *events.Message) (command.Message, bool) {
	if ShouldIgnoreMessage(evt) || evt.Message == nil {
		return command.Message{}, false
	}
	text := extractText(evt.Message)
	if text == "" {
		return command.Message{}, false
	}
	return command.Message{
		ID:     string(evt.Info.ID),
		Chat:   evt.Info.Chat.String(),
		Sender: evt.Info.Sender.String(),
		Text:   text,
		Group:  evt.Info.IsGroup,
	}, true
}

func EncodeQuotedReply(evt *events.Message, text string) *waE2E.Message {
	return &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(string(evt.Info.ID)),
				Participant:   proto.String(evt.Info.Sender.String()),
				RemoteJID:     proto.String(evt.Info.Chat.String()),
				QuotedMessage: evt.Message,
			},
		},
	}
}

func extractText(message *waE2E.Message) string {
	for _, text := range []string{
		message.GetConversation(),
		message.GetExtendedTextMessage().GetText(),
		message.GetImageMessage().GetCaption(),
		message.GetVideoMessage().GetCaption(),
		message.GetDocumentMessage().GetCaption(),
	} {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}
