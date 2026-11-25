package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Message struct {
	ID     string
	Chat   string
	Sender string
	Text   string
	Group  bool
}

type Handler func(context.Context, Message, []string, ReplyFunc) error

type ReplyFunc func(context.Context, string) error

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
