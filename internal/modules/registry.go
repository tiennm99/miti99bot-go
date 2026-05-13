package modules

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/go-telegram/bot"

	"github.com/tiennm99/miti99bot/internal/ai"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// moduleNameRe is intentionally looser than commandNameRe — it allows hyphen
// so module names can carry hyphenated suffixes (e.g. "loldle-classic"). The
// crucial constraint is "no `:`" so the storage Prefixed wrapper's `:`
// delimiter cannot be subverted; everything else is style.
//
// Telegram command names still need the stricter [a-z0-9_]{1,32} alphabet
// (commandNameRe in validate.go). Cron route segments use their own regex in
// internal/server/router.go and stay strict for log-injection safety.
var moduleNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Registry holds the resolved set of modules selected by the MODULES env var.
// It is built once at startup; Build fails fast on validation or conflict.
//
// Read-only after Build returns. Callers must not mutate any field —
// dispatchers and handlers capture *Registry by pointer and assume the maps
// are stable. A future hot-reload feature would need an explicit mutation API.
type Registry struct {
	Modules     []Module           // in MODULES-env order
	AllCommands map[string]Command // name → Command, deduped across modules
	publicCmds  map[string]Command
	protected   map[string]Command
	private     map[string]Command
	crons       map[string]Cron // name → Cron, unique across modules
	cronDeps    map[string]Deps // cron name → owning module's prefixed Deps
}

// PublicCommands returns commands tagged VisibilityPublic, sorted by name.
func (r *Registry) PublicCommands() []Command { return sortedCommands(r.publicCmds) }

// ProtectedCommands returns commands tagged VisibilityProtected, sorted by name.
func (r *Registry) ProtectedCommands() []Command { return sortedCommands(r.protected) }

// PrivateCommands returns commands tagged VisibilityPrivate, sorted by name.
func (r *Registry) PrivateCommands() []Command { return sortedCommands(r.private) }

// Cron looks up a cron by global name across all loaded modules.
func (r *Registry) Cron(name string) (Cron, bool) {
	c, ok := r.crons[name]
	return c, ok
}

// CronDeps returns the per-module-prefixed Deps the cron's owning module
// received. The cron dispatcher uses this to pass scoped Deps to the handler.
func (r *Registry) CronDeps(name string) (Deps, bool) {
	d, ok := r.cronDeps[name]
	return d, ok
}

// Crons returns all loaded crons, sorted by name. Allocates a fresh slice on
// every call — fine for startup-time logging, not for hot paths.
func (r *Registry) Crons() []Cron {
	out := make([]Cron, 0, len(r.crons))
	for _, c := range r.crons {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Build constructs a Registry from the requested module names. The KVProvider
// supplies a per-module-isolated KVStore (MemoryProvider key-prefixes a shared
// store; FirestoreProvider hands out one collection per module). Build validates
// every command/cron and aborts on duplicate command names across the union of
// all visibilities.
//
// Names not present in factories are reported as a single error so a typo in
// MODULES does not silently load a smaller bot than intended. Duplicate names
// in MODULES are also a hard error to keep startup deterministic.
// BuildOptions bundles the optional dependencies threaded into every Module
// Factory's Deps. Adding new optional deps here keeps Build's signature
// stable as the dep list grows.
type BuildOptions struct {
	Chatter ai.Chatter
	Bot     *bot.Bot
}

func Build(enabled []string, factories map[string]Factory, kv storage.KVProvider, opts BuildOptions) (*Registry, error) {
	if kv == nil {
		return nil, fmt.Errorf("modules: KVProvider is required")
	}

	reg := &Registry{
		AllCommands: map[string]Command{},
		publicCmds:  map[string]Command{},
		protected:   map[string]Command{},
		private:     map[string]Command{},
		crons:       map[string]Cron{},
		cronDeps:    map[string]Deps{},
	}

	owners := map[string]string{} // command name → module that registered it
	cronOwners := map[string]string{}
	seenModule := map[string]bool{}
	var unknown []string

	for _, name := range enabled {
		if !moduleNameRe.MatchString(name) {
			return nil, fmt.Errorf("modules: invalid name %q in MODULES env (must match %s)", name, moduleNameRe)
		}
		if seenModule[name] {
			return nil, fmt.Errorf("modules: duplicate name %q in MODULES env", name)
		}
		seenModule[name] = true

		factory, ok := factories[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}

		moduleDeps := Deps{
			KV:       kv.For(name),
			Registry: reg,
			Chatter:  opts.Chatter,
			Bot:      opts.Bot,
		}
		mod := factory(moduleDeps)
		// A factory that hardcodes its own Name is a bug: the registry key is
		// the source of truth and a mismatch means the catalog and module
		// disagree about identity. Surface the conflict rather than silently
		// overwriting it.
		if mod.Name != "" && mod.Name != name {
			return nil, fmt.Errorf("module %q: factory returned mismatched Name=%q", name, mod.Name)
		}
		mod.Name = name

		for _, cmd := range mod.Commands {
			if err := validateCommand(cmd); err != nil {
				return nil, fmt.Errorf("module %q: %w", name, err)
			}
			if prev, dup := owners[cmd.Name]; dup {
				return nil, fmt.Errorf("command conflict: /%s defined in %q and %q", cmd.Name, prev, name)
			}
			owners[cmd.Name] = name
			reg.AllCommands[cmd.Name] = cmd
			switch cmd.Visibility {
			case VisibilityPublic:
				reg.publicCmds[cmd.Name] = cmd
			case VisibilityProtected:
				reg.protected[cmd.Name] = cmd
			case VisibilityPrivate:
				reg.private[cmd.Name] = cmd
			}
		}

		for _, cron := range mod.Crons {
			if err := validateCron(cron); err != nil {
				return nil, fmt.Errorf("module %q: %w", name, err)
			}
			if prev, dup := cronOwners[cron.Name]; dup {
				return nil, fmt.Errorf("cron conflict: %q defined in %q and %q", cron.Name, prev, name)
			}
			cronOwners[cron.Name] = name
			reg.crons[cron.Name] = cron
			reg.cronDeps[cron.Name] = moduleDeps
		}

		reg.Modules = append(reg.Modules, mod)
	}

	if len(unknown) > 0 {
		return nil, fmt.Errorf("modules: unknown name(s) in MODULES env: %v", unknown)
	}

	return reg, nil
}

func sortedCommands(m map[string]Command) []Command {
	out := make([]Command, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
