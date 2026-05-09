package util_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/modules/util"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// helpTestNoop is a stand-in handler used only to satisfy the registry's
// non-nil-handler validator.
func helpTestNoop(_ context.Context, _ *bot.Bot, _ *models.Update) error { return nil }

// fakeFactory builds a module that exposes the supplied commands. Used to
// drive RenderHelp without touching the real util/misc factories (avoids a
// dependency back into the package under test).
func fakeFactory(name string, cmds []modules.Command) modules.Factory {
	return func(_ modules.Deps) modules.Module {
		return modules.Module{Name: name, Commands: cmds}
	}
}

func TestRenderHelp_GroupsByModuleAndSkipsPrivate(t *testing.T) {
	cmd := func(name string, vis modules.Visibility, desc string) modules.Command {
		return modules.Command{Name: name, Visibility: vis, Description: desc, Handler: helpTestNoop}
	}
	factories := map[string]modules.Factory{
		"alpha": fakeFactory("alpha", []modules.Command{
			cmd("a_pub", modules.VisibilityPublic, "alpha public"),
			cmd("a_prot", modules.VisibilityProtected, "alpha protected"),
			cmd("a_priv", modules.VisibilityPrivate, "alpha private — must not appear"),
		}),
		"beta": fakeFactory("beta", []modules.Command{
			cmd("b_pub", modules.VisibilityPublic, "beta <i>desc</i>"),
			cmd("b_amp", modules.VisibilityPublic, `Tom & "Jerry"`),
		}),
	}
	reg, err := modules.Build([]string{"alpha", "beta"}, factories, storage.NewMemoryProvider(), nil, modules.BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	out := util.RenderHelp(reg)

	for _, want := range []string{
		"<b>alpha</b>",
		"<b>beta</b>",
		"/a_pub — alpha public",
		"/a_prot — alpha protected (protected)",
		// HTML in user descriptions must be escaped.
		"beta &lt;i&gt;desc&lt;/i&gt;",
		// Locks html.EscapeString contract: & → &amp;, " → &#34;.
		"Tom &amp; &#34;Jerry&#34;",
		// Support footer always present.
		"github.com/tiennm99/miti99bot-go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---output---\n%s", want, out)
		}
	}
	if strings.Contains(out, "a_priv") {
		t.Errorf("output leaked private command\n---output---\n%s", out)
	}
}

func TestRenderHelp_ModuleOrderMatchesEnvOrder(t *testing.T) {
	cmd := func(name string) modules.Command {
		return modules.Command{Name: name, Visibility: modules.VisibilityPublic, Description: name, Handler: helpTestNoop}
	}
	factories := map[string]modules.Factory{
		"first":  fakeFactory("first", []modules.Command{cmd("f1")}),
		"second": fakeFactory("second", []modules.Command{cmd("s1")}),
	}

	// MODULES order: second,first → expect "second" section before "first".
	reg, err := modules.Build([]string{"second", "first"}, factories, storage.NewMemoryProvider(), nil, modules.BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out := util.RenderHelp(reg)
	iSecond := strings.Index(out, "<b>second</b>")
	iFirst := strings.Index(out, "<b>first</b>")
	if iSecond < 0 || iFirst < 0 {
		t.Fatalf("missing sections; output:\n%s", out)
	}
	if iSecond >= iFirst {
		t.Errorf("expected 'second' before 'first'; got second=%d first=%d\n%s", iSecond, iFirst, out)
	}
}

func TestRenderHelp_OmitsModulesWithNoVisibleCommands(t *testing.T) {
	cmd := func(name string, vis modules.Visibility) modules.Command {
		return modules.Command{Name: name, Visibility: vis, Description: name, Handler: helpTestNoop}
	}
	factories := map[string]modules.Factory{
		"shadow": fakeFactory("shadow", []modules.Command{
			cmd("hidden", modules.VisibilityPrivate),
		}),
		"visible": fakeFactory("visible", []modules.Command{
			cmd("seen", modules.VisibilityPublic),
		}),
	}
	reg, err := modules.Build([]string{"shadow", "visible"}, factories, storage.NewMemoryProvider(), nil, modules.BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out := util.RenderHelp(reg)
	if strings.Contains(out, "<b>shadow</b>") {
		t.Errorf("module with only private commands should not render a section\n%s", out)
	}
	if !strings.Contains(out, "<b>visible</b>") {
		t.Errorf("visible module section missing\n%s", out)
	}
}

func TestRenderHelp_NilRegistryReturnsFooterOnly(t *testing.T) {
	out := util.RenderHelp(nil)
	if !strings.Contains(out, "no commands registered") {
		t.Errorf("nil registry should render placeholder; got:\n%s", out)
	}
	if !strings.Contains(out, "github.com/tiennm99/miti99bot-go") {
		t.Errorf("footer missing; got:\n%s", out)
	}
}
