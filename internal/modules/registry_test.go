package modules

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/storage"
)

func noopCmd(name string) Command {
	return Command{
		Name:        name,
		Visibility:  VisibilityPublic,
		Description: "test " + name,
		Handler:     func(_ context.Context, _ *bot.Bot, _ *models.Update) error { return nil },
	}
}

func noopCron(name string) Cron {
	return Cron{
		Schedule: "@every 24h",
		Name:     name,
		Handler:  func(_ context.Context, _ Deps) error { return nil },
	}
}

func factory(name string, cmds []Command, crons []Cron) Factory {
	return func(_ Deps) Module {
		return Module{Name: name, Commands: cmds, Crons: crons}
	}
}

func newProvider() storage.KVProvider { return storage.NewMemoryProvider() }

func TestBuild_EmptyModulesBootsCleanly(t *testing.T) {
	reg, err := Build(nil, map[string]Factory{}, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build empty: %v", err)
	}
	if len(reg.AllCommands) != 0 {
		t.Errorf("expected 0 commands, got %d", len(reg.AllCommands))
	}
}

func TestBuild_LoadsRequestedModules(t *testing.T) {
	factories := map[string]Factory{
		"alpha": factory("alpha", []Command{noopCmd("a1")}, nil),
		"beta":  factory("beta", []Command{noopCmd("b1")}, []Cron{noopCron("daily")}),
	}
	reg, err := Build([]string{"alpha", "beta"}, factories, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(reg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(reg.Modules))
	}
	if _, ok := reg.AllCommands["a1"]; !ok {
		t.Error("missing command a1")
	}
	if _, ok := reg.Cron("daily"); !ok {
		t.Error("missing cron daily")
	}
}

func TestBuild_SkipsModulesNotInEnv(t *testing.T) {
	factories := map[string]Factory{
		"alpha": factory("alpha", []Command{noopCmd("a1")}, nil),
		"beta":  factory("beta", []Command{noopCmd("b1")}, nil),
	}
	reg, err := Build([]string{"alpha"}, factories, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := reg.AllCommands["b1"]; ok {
		t.Error("beta should not have been loaded")
	}
}

func TestBuild_RejectsUnknownModule(t *testing.T) {
	_, err := Build([]string{"ghost"}, map[string]Factory{}, newProvider(), BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected error mentioning ghost, got %v", err)
	}
}

func TestBuild_DetectsCommandConflict(t *testing.T) {
	factories := map[string]Factory{
		"alpha": factory("alpha", []Command{noopCmd("ping")}, nil),
		"beta":  factory("beta", []Command{noopCmd("ping")}, nil),
	}
	_, err := Build([]string{"alpha", "beta"}, factories, newProvider(), BuildOptions{})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "command conflict") {
		t.Errorf("error should mention conflict, got %v", err)
	}
}

func TestBuild_DetectsCronConflict(t *testing.T) {
	factories := map[string]Factory{
		"alpha": factory("alpha", nil, []Cron{noopCron("daily")}),
		"beta":  factory("beta", nil, []Cron{noopCron("daily")}),
	}
	_, err := Build([]string{"alpha", "beta"}, factories, newProvider(), BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "cron conflict") {
		t.Errorf("expected cron conflict, got %v", err)
	}
}

func TestBuild_RequiresProvider(t *testing.T) {
	_, err := Build(nil, map[string]Factory{}, nil, BuildOptions{})
	if err == nil {
		t.Error("expected error when KVProvider is nil")
	}
}

func TestBuild_ValidationErrorsMentionModule(t *testing.T) {
	bad := Command{Name: "BAD-NAME", Visibility: VisibilityPublic, Description: "x", Handler: noopCmd("x").Handler}
	factories := map[string]Factory{
		"alpha": factory("alpha", []Command{bad}, nil),
	}
	_, err := Build([]string{"alpha"}, factories, newProvider(), BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Errorf("expected error mentioning module 'alpha', got %v", err)
	}
}

func TestDispatchScheduled_RunsHandler(t *testing.T) {
	called := false
	factories := map[string]Factory{
		"alpha": factory("alpha", nil, []Cron{{
			Name: "tick",
			Handler: func(_ context.Context, _ Deps) error {
				called = true
				return nil
			},
		}}),
	}
	reg, err := Build([]string{"alpha"}, factories, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := DispatchScheduled(context.Background(), "tick", reg); err != nil {
		t.Fatalf("DispatchScheduled: %v", err)
	}
	if !called {
		t.Error("cron handler not invoked")
	}
}

func TestDispatchScheduled_UnknownReturnsErrCronNotFound(t *testing.T) {
	reg, err := Build(nil, map[string]Factory{}, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = DispatchScheduled(context.Background(), "missing", reg)
	if !errors.Is(err, ErrCronNotFound) {
		t.Errorf("expected ErrCronNotFound, got %v", err)
	}
}

func TestDispatchScheduled_PassesPrefixedDeps(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMemoryProvider()

	factories := map[string]Factory{
		"alpha": func(d Deps) Module {
			return Module{Crons: []Cron{{
				Name: "tick_a",
				Handler: func(ctx context.Context, deps Deps) error {
					return deps.KV.Put(ctx, "last", []byte("A"))
				},
			}}}
		},
		"beta": func(d Deps) Module {
			return Module{Crons: []Cron{{
				Name: "tick_b",
				Handler: func(ctx context.Context, deps Deps) error {
					return deps.KV.Put(ctx, "last", []byte("B"))
				},
			}}}
		},
	}
	reg, err := Build([]string{"alpha", "beta"}, factories, provider, BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := DispatchScheduled(ctx, "tick_a", reg); err != nil {
		t.Fatalf("tick_a: %v", err)
	}
	if err := DispatchScheduled(ctx, "tick_b", reg); err != nil {
		t.Fatalf("tick_b: %v", err)
	}

	// Underlying base store should hold each module's prefixed key separately.
	gotA, err := provider.Base().Get(ctx, "alpha:last")
	if err != nil || string(gotA) != "A" {
		t.Errorf("alpha:last = %q (err=%v), want A", gotA, err)
	}
	gotB, err := provider.Base().Get(ctx, "beta:last")
	if err != nil || string(gotB) != "B" {
		t.Errorf("beta:last = %q (err=%v), want B", gotB, err)
	}
}

func TestBuild_RejectsInvalidModuleName(t *testing.T) {
	// `-` is intentionally allowed so modules can carry hyphenated names. `:`
	// must stay rejected — it's the storage prefix delimiter and a
	// hyphen-allowing regex must not let it through.
	for _, name := range []string{"BadName", "a:b", "", "with space", "with.dot", "with/slash"} {
		t.Run(name, func(t *testing.T) {
			_, err := Build([]string{name}, map[string]Factory{}, newProvider(), BuildOptions{})
			if err == nil {
				t.Errorf("name %q: expected error", name)
			}
		})
	}
}

func TestBuild_RejectsFactoryNameMismatch(t *testing.T) {
	// A factory that hardcodes its own name disagreeing with the registry key
	// is a programming bug — surface it instead of silently overwriting.
	factories := map[string]Factory{
		"alpha": func(_ Deps) Module {
			return Module{Name: "imposter", Commands: []Command{noopCmd("a1")}}
		},
	}
	_, err := Build([]string{"alpha"}, factories, newProvider(), BuildOptions{})
	if err == nil {
		t.Fatal("expected error for factory Name mismatch")
	}
	if !strings.Contains(err.Error(), "imposter") {
		t.Errorf("error should mention mismatched name: %v", err)
	}
}

func TestBuild_AllowsFactoryWithBlankName(t *testing.T) {
	// Factory leaves Name blank; registry fills it from the key. Common,
	// non-buggy pattern.
	factories := map[string]Factory{
		"alpha": func(_ Deps) Module {
			return Module{Commands: []Command{noopCmd("a1")}}
		},
	}
	reg, err := Build([]string{"alpha"}, factories, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if reg.Modules[0].Name != "alpha" {
		t.Errorf("blank-name factory: registered Name = %q, want 'alpha'", reg.Modules[0].Name)
	}
}

func TestBuild_AcceptsHyphenatedModuleName(t *testing.T) {
	factories := map[string]Factory{
		"demo-mod": factory("demo-mod", []Command{noopCmd("demo_cmd")}, nil),
	}
	reg, err := Build([]string{"demo-mod"}, factories, newProvider(), BuildOptions{})
	if err != nil {
		t.Fatalf("hyphenated name should be allowed: %v", err)
	}
	if len(reg.Modules) != 1 || reg.Modules[0].Name != "demo-mod" {
		t.Errorf("module not registered correctly: %+v", reg.Modules)
	}
}

func TestBuild_RejectsDuplicateModuleInEnv(t *testing.T) {
	factories := map[string]Factory{
		"alpha": factory("alpha", []Command{noopCmd("a1")}, nil),
	}
	_, err := Build([]string{"alpha", "alpha"}, factories, newProvider(), BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-module error, got %v", err)
	}
}

func TestBuild_PerModulePrefixedKV(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMemoryProvider()

	// Each module writes a value to the same key; with per-module prefixing
	// they must not collide.
	captured := map[string]Deps{}
	factories := map[string]Factory{
		"alpha": func(d Deps) Module {
			captured["alpha"] = d
			return Module{Commands: []Command{noopCmd("a")}}
		},
		"beta": func(d Deps) Module {
			captured["beta"] = d
			return Module{Commands: []Command{noopCmd("b")}}
		},
	}
	if _, err := Build([]string{"alpha", "beta"}, factories, provider, BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := captured["alpha"].KV.Put(ctx, "score", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := captured["beta"].KV.Put(ctx, "score", []byte("2")); err != nil {
		t.Fatal(err)
	}

	got, _ := captured["alpha"].KV.Get(ctx, "score")
	if string(got) != "1" {
		t.Errorf("alpha.KV.score = %q, want 1", got)
	}
	got, _ = captured["beta"].KV.Get(ctx, "score")
	if string(got) != "2" {
		t.Errorf("beta.KV.score = %q, want 2", got)
	}
}
