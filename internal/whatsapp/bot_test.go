package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestDecodeMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *waE2E.Message
		want    string
	}{
		{
			name:    "conversation",
			message: &waE2E.Message{Conversation: proto.String("  !ping  ")},
			want:    "!ping",
		},
		{
			name:    "extended text",
			message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("!help")}},
			want:    "!help",
		},
		{
			name:    "image caption",
			message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("!ping")}},
			want:    "!ping",
		},
		{
			name: "empty message",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evt := &events.Message{
				Message: test.message,
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:   types.NewJID("12345", types.DefaultUserServer),
						Sender: types.NewJID("67890", types.DefaultUserServer),
					},
					ID: "test-message",
				},
			}
			got, ok := DecodeMessage(evt)
			if test.want != "" && !ok {
				t.Fatal("expected message to decode")
			}
			if test.want == "" && ok {
				t.Fatal("expected empty message to be ignored")
			}
			if got.Text != test.want {
				t.Fatalf("DecodeMessage().Text = %q, want %q", got.Text, test.want)
			}
		})
	}
}

func TestDecodeMessageAllowsSelfMessages(t *testing.T) {
	selfMessage := &events.Message{
		Message: &waE2E.Message{Conversation: proto.String("!ping")},
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				IsFromMe: true,
				Chat:     types.NewJID("12345", types.DefaultUserServer),
			},
		},
	}
	if _, ok := DecodeMessage(selfMessage); !ok {
		t.Fatal("self-authored message should be available for command processing")
	}
	if _, ok := DecodeMessage(nil); ok {
		t.Fatal("nil message should be ignored")
	}
}

func TestQuotedReplyIncludesSourceMessage(t *testing.T) {
	chat := types.NewJID("12345", types.DefaultUserServer)
	sender := types.NewJID("67890", types.DefaultUserServer)
	source := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   chat,
				Sender: sender,
			},
			ID: "SOURCE-MESSAGE",
		},
		Message: &waE2E.Message{Conversation: proto.String("!ping")},
	}

	reply := EncodeQuotedReply(source, "Pong")
	text := reply.GetExtendedTextMessage()
	if text.GetText() != "Pong" {
		t.Fatalf("reply text = %q, want %q", text.GetText(), "Pong")
	}
	context := text.GetContextInfo()
	if context.GetStanzaID() != "SOURCE-MESSAGE" {
		t.Fatalf("quoted stanza ID = %q, want %q", context.GetStanzaID(), "SOURCE-MESSAGE")
	}
	if context.GetParticipant() != sender.String() {
		t.Fatalf("quoted participant = %q, want %q", context.GetParticipant(), sender.String())
	}
	if context.GetRemoteJID() != chat.String() {
		t.Fatalf("quoted chat = %q, want %q", context.GetRemoteJID(), chat.String())
	}
	if context.GetQuotedMessage().GetConversation() != "!ping" {
		t.Fatalf("quoted message = %q, want %q", context.GetQuotedMessage().GetConversation(), "!ping")
	}
}
