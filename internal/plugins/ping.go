package plugins

import (
	"context"

	"github.com/SafwanGanz/Sonicos/internal/command"
)

func init() {
	command.Register(command.Command{
		Name:        "ping",
		Category:    "General",
		Description: "Check whether the bot is responding",
		Handler:     ping,
	})
}

func ping(ctx context.Context, _ command.Message, _ []string, reply command.ReplyFunc) error {
	return reply(ctx, command.Reply{Text: "Pong"})
}
