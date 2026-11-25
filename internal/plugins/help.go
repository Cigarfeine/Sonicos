package plugins

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func init() {
	command.Register(command.Command{
		Name:        "help",
		Aliases:     []string{"commands"},
		Description: "List available commands",
		Handler:     help,
	})
}

func help(ctx context.Context, _ command.Message, _ []string, reply command.ReplyFunc) error {
	registry, err := command.Default()
	if err != nil {
		return err
	}
	commands := registry.Commands()
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })

	var output strings.Builder
	output.WriteString("Available commands:")
	for _, cmd := range commands {
		fmt.Fprintf(&output, "\n%s — %s", cmd.Name, cmd.Description)
	}
	return reply(ctx, command.Reply{Text: output.String()})
}
