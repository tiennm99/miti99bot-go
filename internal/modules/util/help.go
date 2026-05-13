package util

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/modules"
)

const repoURL = "https://github.com/tiennm99/miti99bot"

var supportFooter = fmt.Sprintf(
	`Enjoying the bot? Support me by starring the repo: <a href="%s">%s</a>`,
	repoURL, repoURL,
)

// RenderHelp produces the body of /help: each module's public + protected
// commands grouped under a bold module name, followed by the support footer.
// Modules in MODULES-env order. Modules with no visible commands are omitted.
// Private commands are always skipped.
//
// Exposed (capitalised) so tests can assert on the string without spinning up
// a bot context.
func RenderHelp(reg *modules.Registry) string {
	if reg == nil {
		return "no commands registered\n\n" + supportFooter
	}

	type entry struct {
		name        string
		description string
		protected   bool
	}
	byModule := make(map[string][]entry, len(reg.Modules))

	for _, c := range reg.PublicCommands() {
		byModule[ownerOf(reg, c.Name)] = append(byModule[ownerOf(reg, c.Name)], entry{
			name: c.Name, description: c.Description, protected: false,
		})
	}
	for _, c := range reg.ProtectedCommands() {
		byModule[ownerOf(reg, c.Name)] = append(byModule[ownerOf(reg, c.Name)], entry{
			name: c.Name, description: c.Description, protected: true,
		})
	}

	var sections []string
	for _, mod := range reg.Modules {
		es := byModule[mod.Name]
		if len(es) == 0 {
			continue
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "<b>%s</b>", html.EscapeString(mod.Name))
		for _, e := range es {
			suffix := ""
			if e.protected {
				suffix = " (protected)"
			}
			fmt.Fprintf(&sb, "\n/%s — %s%s", e.name, html.EscapeString(e.description), suffix)
		}
		sections = append(sections, sb.String())
	}

	body := "no commands registered"
	if len(sections) > 0 {
		body = strings.Join(sections, "\n\n")
	}
	return body + "\n\n" + supportFooter
}

// ownerOf finds the module that registered the named command. Linear scan
// (modules are few; commands per module are few). Returns "" if not found —
// callers treat that as "skip".
func ownerOf(reg *modules.Registry, cmdName string) string {
	for _, m := range reg.Modules {
		for _, c := range m.Commands {
			if c.Name == cmdName {
				return m.Name
			}
		}
	}
	return ""
}

// helpCommand returns /help — pure renderer over the registry.
func helpCommand(reg *modules.Registry) modules.Command {
	return modules.Command{
		Name:        "help",
		Visibility:  modules.VisibilityPublic,
		Description: "Show all available commands",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil {
				return nil
			}
			text := RenderHelp(reg)
			_, err := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:             update.Message.Chat.ID,
				Text:               text,
				ParseMode:          models.ParseModeHTML,
				LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
			})
			return err
		},
	}
}
