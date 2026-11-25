package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Message struct {
	ID              string
	Chat            string
	Sender          string
	Text            string
	Group           bool
	MentionedJIDs   []string
	QuotedSender    string
	QuotedMessageID string
	MediaSource     MediaSource
	GroupService    GroupService
}

type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
	MediaAudio MediaKind = "audio"
)

type Attachment struct {
	Kind     MediaKind
	MIMEType string
	Data     []byte
}

type MediaSource interface {
	Download(context.Context) (Attachment, error)
}

type Sticker struct {
	Data     []byte
	MIMEType string
	Animated bool
	Audio    bool
	Seconds  uint32
}

type Reply struct {
	Text     string
	Mentions []string
	Sticker  *Sticker
}

type ParticipantAction string

const (
	ParticipantAdd     ParticipantAction = "add"
	ParticipantRemove  ParticipantAction = "remove"
	ParticipantPromote ParticipantAction = "promote"
	ParticipantDemote  ParticipantAction = "demote"
)

type GroupParticipant struct {
	JID          string
	PhoneNumber  string
	IsAdmin      bool
	IsSuperAdmin bool
}

type GroupInfo struct {
	JID          string
	Name         string
	Topic        string
	Owner        string
	BotJID       string
	Locked       bool
	Announce     bool
	Approval     bool
	EphemeralSec uint32
	MemberAddAll bool
	Participants []GroupParticipant
}

type GroupRequest struct {
	JID         string
	RequestedAt int64
}

type GroupService interface {
	GetInfo(ctx context.Context, groupJID string) (GroupInfo, error)
	UpdateParticipants(ctx context.Context, groupJID string, targets []string, action ParticipantAction) error
	SetName(ctx context.Context, groupJID, name string) error
	SetDescription(ctx context.Context, groupJID, description string) error
	SetPhoto(ctx context.Context, groupJID string, image []byte) error
	SetAnnounce(ctx context.Context, groupJID string, announce bool) error
	SetLocked(ctx context.Context, groupJID string, locked bool) error
	SetJoinApproval(ctx context.Context, groupJID string, requireApproval bool) error
	SetMemberAddMode(ctx context.Context, groupJID string, allowAll bool) error
	SetDisappearing(ctx context.Context, groupJID string, seconds uint32) error
	GetInviteLink(ctx context.Context, groupJID string, reset bool) (string, error)
	GetJoinRequests(ctx context.Context, groupJID string) ([]GroupRequest, error)
	UpdateJoinRequests(ctx context.Context, groupJID string, targets []string, approve bool) error
	RevokeMessage(ctx context.Context, chatJID, senderJID, messageID string) error
	Leave(ctx context.Context, groupJID string) error
}

type Handler func(context.Context, Message, []string, ReplyFunc) error

type ReplyFunc func(context.Context, Reply) error

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Handler     Handler
}

type Registry struct {
	mu       sync.RWMutex
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

func (r *Registry) Register(cmd Command) error {
	cmd.Name = normalize(cmd.Name)
	if cmd.Name == "" || cmd.Handler == nil {
		return errors.New("command name and handler are required")
	}

	keys := append([]string{cmd.Name}, cmd.Aliases...)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range keys {
		key = normalize(key)
		if key == "" {
			return errors.New("command aliases cannot be empty")
		}
		if _, exists := r.commands[key]; exists {
			return fmt.Errorf("command %q is already registered", key)
		}
	}
	for _, key := range keys {
		key = normalize(key)
		r.commands[key] = cmd
	}
	return nil
}

func (r *Registry) Dispatch(ctx context.Context, prefix string, msg Message, reply ReplyFunc) (bool, error) {
	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, prefix) {
		return false, nil
	}
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(text, prefix)))
	if len(parts) == 0 {
		return false, nil
	}

	r.mu.RLock()
	cmd, exists := r.commands[normalize(parts[0])]
	r.mu.RUnlock()
	if !exists {
		return false, nil
	}
	return true, cmd.Handler(ctx, msg, parts[1:], reply)
}

func (r *Registry) Commands() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	commands := make([]Command, 0, len(r.commands))
	seen := make(map[string]struct{}, len(r.commands))
	for _, cmd := range r.commands {
		if _, exists := seen[cmd.Name]; exists {
			continue
		}
		seen[cmd.Name] = struct{}{}
		commands = append(commands, cmd)
	}
	return commands
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
