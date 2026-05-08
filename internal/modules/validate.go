package modules

import (
	"fmt"
	"regexp"
)

var commandNameRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func validateCommand(c Command) error {
	if !commandNameRe.MatchString(c.Name) {
		return fmt.Errorf("command name %q must match %s", c.Name, commandNameRe)
	}
	switch c.Visibility {
	case VisibilityPublic, VisibilityProtected, VisibilityPrivate:
	default:
		return fmt.Errorf("command %q: unknown visibility %d", c.Name, c.Visibility)
	}
	if c.Description == "" {
		return fmt.Errorf("command %q: description is required", c.Name)
	}
	if c.Handler == nil {
		return fmt.Errorf("command %q: handler is nil", c.Name)
	}
	return nil
}

func validateCron(c Cron) error {
	if c.Name == "" {
		return fmt.Errorf("cron: name is required")
	}
	if c.Handler == nil {
		return fmt.Errorf("cron %q: handler is nil", c.Name)
	}
	return nil
}
