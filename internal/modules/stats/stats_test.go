package stats

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/testutil"
)

func TestNew_RegistersExpectedCommands(t *testing.T) {
	deps := modules.Deps{KV: storage.NewMemoryKVStore()}
	mod := New(deps)

	if len(mod.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1", len(mod.Commands))
	}
	cmd := mod.Commands[0]
	if cmd.Name != "stats" {
		t.Errorf("command name = %q, want %q", cmd.Name, "stats")
	}
	if cmd.Visibility != modules.VisibilityPublic {
		t.Errorf("command visibility = %d, want Public", cmd.Visibility)
	}
	if cmd.Handler == nil {
		t.Error("command handler is nil")
	}
	if mod.CommandHook == nil {
		t.Error("CommandHook is nil")
	}
}

// updateFrom builds a minimal update carrying the sender ID + optional
// username. Pass username="" to exercise the "no Telegram username" branch.
func updateFrom(id int64, username string) *models.Update {
	return &models.Update{
		Message: &models.Message{
			From: &models.User{ID: id, Username: username},
		},
	}
}

func TestInc_PersistsCountInKV(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}

	c.Inc(ctx, "ping", nil)
	c.Inc(ctx, "ping", nil)
	c.Inc(ctx, "wordle", nil)

	var entry countEntry
	if err := kv.GetJSON(ctx, countKey("ping"), &entry); err != nil {
		t.Fatalf("GetJSON ping: %v", err)
	}
	if entry.N != 2 {
		t.Errorf("ping count = %d, want 2", entry.N)
	}

	entry = countEntry{}
	if err := kv.GetJSON(ctx, countKey("wordle"), &entry); err != nil {
		t.Fatalf("GetJSON wordle: %v", err)
	}
	if entry.N != 1 {
		t.Errorf("wordle count = %d, want 1", entry.N)
	}
}

func TestInc_WithUsernameWritesAllThreeKeys(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}

	c.Inc(ctx, "ping", updateFrom(42, "alice"))
	c.Inc(ctx, "ping", updateFrom(42, "alice"))

	var ce countEntry
	if err := kv.GetJSON(ctx, countKey("ping"), &ce); err != nil {
		t.Fatalf("count:ping: %v", err)
	}
	if ce.N != 2 {
		t.Errorf("count:ping N = %d, want 2", ce.N)
	}

	var ue userEntry
	if err := kv.GetJSON(ctx, userKey(42), &ue); err != nil {
		t.Fatalf("user:42: %v", err)
	}
	if ue.N != 2 || ue.Username != "alice" {
		t.Errorf("user:42 = {%q, %d}, want {alice, 2}", ue.Username, ue.N)
	}

	var pe countEntry
	if err := kv.GetJSON(ctx, pairKey("ping", 42), &pe); err != nil {
		t.Fatalf("pair:ping:42: %v", err)
	}
	if pe.N != 2 {
		t.Errorf("pair:ping:42 N = %d, want 2", pe.N)
	}
}

func TestInc_EmptyUsernameSkipsUserAndPair(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}

	c.Inc(ctx, "ping", updateFrom(42, ""))

	var ce countEntry
	if err := kv.GetJSON(ctx, countKey("ping"), &ce); err != nil {
		t.Fatalf("count:ping: %v", err)
	}
	if ce.N != 1 {
		t.Errorf("count:ping N = %d, want 1", ce.N)
	}

	if _, err := kv.Get(ctx, userKey(42)); err != storage.ErrNotFound {
		t.Errorf("user:42 should be absent, got err=%v", err)
	}
	if _, err := kv.Get(ctx, pairKey("ping", 42)); err != storage.ErrNotFound {
		t.Errorf("pair:ping:42 should be absent, got err=%v", err)
	}
}

func TestInc_RefreshesUsernameOnRename(t *testing.T) {
	ctx := context.Background()
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}

	c.Inc(ctx, "ping", updateFrom(42, "alice"))
	c.Inc(ctx, "ping", updateFrom(42, "alice2"))

	var ue userEntry
	if err := kv.GetJSON(ctx, userKey(42), &ue); err != nil {
		t.Fatalf("user:42: %v", err)
	}
	if ue.Username != "alice2" {
		t.Errorf("user:42 Username = %q, want %q", ue.Username, "alice2")
	}
	if ue.N != 2 {
		t.Errorf("user:42 N = %d, want 2", ue.N)
	}
}

func installStats(t *testing.T) (*testutil.RecordingBot, *counter) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	kv := storage.NewMemoryKVStore()
	c := &counter{kv: kv}
	mod := modules.Module{
		Commands: []modules.Command{statsCommand(c)},
	}
	reg := &modules.Registry{
		AllCommands: map[string]modules.Command{},
	}
	for _, cmd := range mod.Commands {
		reg.AllCommands[cmd.Name] = cmd
	}
	modules.Install(rb.Bot, reg, modules.Auth{})
	return rb, c
}

func TestStats_NoDataRepliesEmpty(t *testing.T) {
	rb, _ := installStats(t)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/stats"))

	got := rb.LastSent().Text()
	if got != "No command stats yet." {
		t.Errorf("empty stats reply = %q, want 'No command stats yet.'", got)
	}
}

func TestStats_ShowsCountsSortedByPopularity(t *testing.T) {
	ctx := context.Background()
	rb, c := installStats(t)

	c.Inc(ctx, "ping", nil)
	c.Inc(ctx, "wordle", nil)
	c.Inc(ctx, "wordle", nil)
	c.Inc(ctx, "wordle", nil)
	c.Inc(ctx, "loldle", nil)
	c.Inc(ctx, "loldle", nil)

	rb.Bot.ProcessUpdate(ctx, testutil.NewPrivateMessage(1, "/stats"))
	got := rb.LastSent().Text()

	if !strings.HasPrefix(got, "Command usage:") {
		t.Errorf("reply missing header: %q", got)
	}
	wordlePos := strings.Index(got, "/wordle:")
	loLdlePos := strings.Index(got, "/loldle:")
	pingPos := strings.Index(got, "/ping:")
	if wordlePos < 0 || loLdlePos < 0 || pingPos < 0 {
		t.Fatalf("reply missing expected commands: %q", got)
	}
	if wordlePos >= loLdlePos || loLdlePos >= pingPos {
		t.Errorf("commands not in descending count order: wordle=%d loldle=%d ping=%d in %q",
			wordlePos, loLdlePos, pingPos, got)
	}
}

func TestCommandHook_FiredThroughModulesBuild(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMemoryProvider()

	reg, err := modules.Build(
		[]string{"stats"},
		map[string]modules.Factory{"stats": New},
		provider,
		modules.BuildOptions{},
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(reg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(reg.Modules))
	}

	rb := testutil.NewRecordingBot(t)
	modules.Install(rb.Bot, reg, modules.Auth{})

	reg.RunCommandHooks(ctx, "ping", nil)

	statsKV := provider.For("stats")
	var entry countEntry
	if err := statsKV.GetJSON(ctx, countKey("ping"), &entry); err != nil {
		t.Fatalf("expected count:ping in KV after hook: %v", err)
	}
	if entry.N != 1 {
		t.Errorf("count:ping = %d, want 1", entry.N)
	}
}

// renderStats is the asserted-on surface for the subcommand views, since it
// returns the reply string without touching *bot.Bot. seedFixture below mirrors
// what Inc would have written across several users + commands.
func seedFixture(t *testing.T, c *counter) {
	t.Helper()
	ctx := context.Background()
	// alice (id=1): /ping x3, /wordle x1
	// bob   (id=2): /ping x1, /wordle x2
	// carol (id=3): /ping x1                        (username later cleared to test skip)
	for i := 0; i < 3; i++ {
		c.Inc(ctx, "ping", updateFrom(1, "alice"))
	}
	c.Inc(ctx, "wordle", updateFrom(1, "alice"))
	c.Inc(ctx, "ping", updateFrom(2, "bob"))
	c.Inc(ctx, "wordle", updateFrom(2, "bob"))
	c.Inc(ctx, "wordle", updateFrom(2, "bob"))
	c.Inc(ctx, "ping", updateFrom(3, "carol"))
}

func TestRenderStats_TopCommands(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "")
	if !strings.HasPrefix(got, "Command usage:\n") {
		t.Errorf("missing header: %q", got)
	}
	// totals: ping=5, wordle=3
	pingPos := strings.Index(got, "/ping: 5")
	wordlePos := strings.Index(got, "/wordle: 3")
	if pingPos < 0 || wordlePos < 0 {
		t.Fatalf("missing totals: %q", got)
	}
	if pingPos >= wordlePos {
		t.Errorf("ping should sort before wordle: %q", got)
	}
}

func TestRenderStats_Users(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "users")
	if !strings.HasPrefix(got, "Top users:\n") {
		t.Errorf("missing header: %q", got)
	}
	// totals: alice=4, bob=3, carol=1
	alicePos := strings.Index(got, "@alice: 4")
	bobPos := strings.Index(got, "@bob: 3")
	carolPos := strings.Index(got, "@carol: 1")
	if alicePos < 0 || bobPos < 0 || carolPos < 0 {
		t.Fatalf("missing users in %q", got)
	}
	if alicePos >= bobPos || bobPos >= carolPos {
		t.Errorf("not sorted desc: alice=%d bob=%d carol=%d in %q", alicePos, bobPos, carolPos, got)
	}
}

func TestRenderStats_UserCommands(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "user @alice")
	if !strings.HasPrefix(got, "Commands by @alice:\n") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "/ping: 3") {
		t.Errorf("missing /ping: 3 in %q", got)
	}
	if !strings.Contains(got, "/wordle: 1") {
		t.Errorf("missing /wordle: 1 in %q", got)
	}
	// bob should not appear in alice's view
	if strings.Contains(got, "@bob") {
		t.Errorf("bob leaked into alice view: %q", got)
	}
}

// Pins the leading-colon disambiguation in viewUserCommands: a user ID
// suffix like ":2" must not falsely match a pair key for a different user
// whose ID happens to end in "2" (e.g. 12, 22, 42, 142). Both reviewers
// flagged this as a potential bug; this test proves the absence of the bug.
func TestRenderStats_UserCommands_IDSuffixDoesNotFalseMatch(t *testing.T) {
	ctx := context.Background()
	c := &counter{kv: storage.NewMemoryKVStore()}
	// user 2 ("two") calls /ping; user 12 ("twelve") calls /wordle.
	c.Inc(ctx, "ping", updateFrom(2, "two"))
	c.Inc(ctx, "wordle", updateFrom(12, "twelve"))

	got := renderStats(ctx, c, "user @two")
	if !strings.HasPrefix(got, "Commands by @two:\n") {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "/ping: 1") {
		t.Errorf("missing /ping for user 2: %q", got)
	}
	if strings.Contains(got, "/wordle") {
		t.Errorf("user 12's /wordle leaked into user 2's view via :2 suffix-match: %q", got)
	}
}

func TestRenderStats_UserCommands_BareNameAlsoWorks(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "user alice")
	if !strings.HasPrefix(got, "Commands by @alice:\n") {
		t.Errorf("missing header for bare-name lookup: %q", got)
	}
}

func TestRenderStats_UserNotFound(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "user @ghost")
	if got != "User @ghost not found." {
		t.Errorf("got %q, want not-found", got)
	}
}

func TestRenderStats_CmdUsers(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "cmd wordle")
	if !strings.HasPrefix(got, "Users of /wordle:\n") {
		t.Errorf("missing header: %q", got)
	}
	// /wordle: bob=2, alice=1
	bobPos := strings.Index(got, "@bob: 2")
	alicePos := strings.Index(got, "@alice: 1")
	if bobPos < 0 || alicePos < 0 {
		t.Fatalf("missing users in %q", got)
	}
	if bobPos >= alicePos {
		t.Errorf("not sorted desc: bob=%d alice=%d in %q", bobPos, alicePos, got)
	}
	if strings.Contains(got, "@carol") {
		t.Errorf("carol leaked into /wordle view (never invoked it): %q", got)
	}
}

func TestRenderStats_CmdNotFound(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	seedFixture(t, c)

	got := renderStats(context.Background(), c, "cmd nonexistent")
	if got != "Command /nonexistent has no users yet." {
		t.Errorf("got %q, want not-found", got)
	}
}

func TestRenderStats_UnknownSubcommand(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	got := renderStats(context.Background(), c, "bogus")
	if !strings.HasPrefix(got, "Usage:") {
		t.Errorf("expected usage string, got %q", got)
	}
}

func TestRenderStats_MissingRequiredArg(t *testing.T) {
	c := &counter{kv: storage.NewMemoryKVStore()}
	if got := renderStats(context.Background(), c, "user"); !strings.HasPrefix(got, "Usage:") {
		t.Errorf("user (no arg) should return usage, got %q", got)
	}
	if got := renderStats(context.Background(), c, "cmd"); !strings.HasPrefix(got, "Usage:") {
		t.Errorf("cmd (no arg) should return usage, got %q", got)
	}
}

func TestRenderTopN_Truncates(t *testing.T) {
	rows := make([]row, 0, 400)
	for i := 0; i < 400; i++ {
		rows = append(rows, row{display: fmt.Sprintf("/cmd%04d", i), n: int64(400 - i)})
	}
	got := renderTopN("Header:", rows, 0) // k=0 means no top-K cap
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("expected truncation marker, got tail %q", tail(got))
	}
	if len(got) > telegramMaxLen+len("\n…(truncated)") {
		t.Errorf("output length %d exceeds telegramMaxLen+marker", len(got))
	}
}

func TestRenderTopN_RespectsK(t *testing.T) {
	rows := []row{
		{display: "/a", n: 10},
		{display: "/b", n: 9},
		{display: "/c", n: 8},
	}
	got := renderTopN("Header:", rows, 2)
	if strings.Contains(got, "/c:") {
		t.Errorf("k=2 should drop /c, got %q", got)
	}
	if !strings.Contains(got, "/a:") || !strings.Contains(got, "/b:") {
		t.Errorf("k=2 should keep /a and /b, got %q", got)
	}
}

func tail(s string) string {
	const n = 40
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
