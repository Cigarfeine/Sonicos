package plugins

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func init() {
	command.Register(command.Command{
		Name:        "help",
		Aliases:     []string{"menu", "commands"},
		Category:    "General",
		Description: "Show this categorized command menu",
		Handler:     help,
	})
}

var categoryOrder = map[string]int{
	"General":          1,
	"Media & Stickers": 2,
	"Group (Members)":  3,
	"Group (Admins)":   4,
}

var categoryIcons = map[string]string{
	"General":          "⚡",
	"Media & Stickers": "🎨",
	"Group (Members)":  "👥",
	"Group (Admins)":   "🛡️",
}

func help(ctx context.Context, _ command.Message, _ []string, reply command.ReplyFunc) error {
	registry, err := command.Default()
	if err != nil {
		return err
	}
	prefix := strings.TrimSpace(os.Getenv("BOT_PREFIX"))
	if prefix == "" {
		prefix = "!"
	}

	grouped := make(map[string][]command.Command)
	for _, cmd := range registry.Commands() {
		cat := strings.TrimSpace(cmd.Category)
		if cat == "" {
			cat = "General"
		}
		grouped[cat] = append(grouped[cat], cmd)
	}

	categories := make([]string, 0, len(grouped))
	for cat := range grouped {
		categories = append(categories, cat)
		sort.Slice(grouped[cat], func(i, j int) bool {
			return grouped[cat][i].Name < grouped[cat][j].Name
		})
	}
	sort.Slice(categories, func(i, j int) bool {
		oi, okI := categoryOrder[categories[i]]
		oj, okJ := categoryOrder[categories[j]]
		if okI && okJ && oi != oj {
			return oi < oj
		}
		if okI != okJ {
			return okI
		}
		return categories[i] < categories[j]
	})

	var out strings.Builder
	out.WriteString("╔══════════════════════╗\n")
	out.WriteString("   🤖 *SONICOS BOT MENU*\n")
	out.WriteString("╚══════════════════════╝\n")
	fmt.Fprintf(&out, "Prefix: `%s`  •  Total: *%d* commands\n", prefix, len(registry.Commands()))

	for _, cat := range categories {
		icon := categoryIcons[cat]
		if icon == "" {
			icon = "📌"
		}
		fmt.Fprintf(&out, "\n%s *%s*\n", icon, strings.ToUpper(cat))
		out.WriteString("──────────────────────\n")
		for _, cmd := range grouped[cat] {
			aliasText := ""
			if len(cmd.Aliases) > 0 {
				prefixed := make([]string, len(cmd.Aliases))
				for i, a := range cmd.Aliases {
					prefixed[i] = prefix + a
				}
				aliasText = fmt.Sprintf(" _(%s)_", strings.Join(prefixed, ", "))
			}
			fmt.Fprintf(&out, "▸ `%s%s`%s\n   _%s_\n", prefix, cmd.Name, aliasText, cmd.Description)
		}
	}

	out.WriteString("\n💡 _Tip: Reply to media or messages when using sticker or moderation commands._")
	return reply(ctx, command.Reply{Text: out.String()})
}
