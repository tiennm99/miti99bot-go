package misc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/testutil"
)

// installMisc wires the misc module to a recording bot with a fresh
// in-memory KV. Returns the bot, the kv (so tests can pre-seed or read),
// and an Auth that permits Owner + Admin so /mstats /fortytwo dispatch.
func installMisc(t *testing.T, ownerID int64) (*testutil.RecordingBot, storage.KVStore) {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	provider := storage.NewMemoryProvider()
	kv := provider.For("misc")
	mod := New(modules.Deps{KV: kv})

	reg := &modules.Registry{
		Modules:     []modules.Module{{Name: "misc", Commands: mod.Commands}},
		AllCommands: map[string]modules.Command{},
	}
	for _, c := range mod.Commands {
		reg.AllCommands[c.Name] = c
	}
	auth := modules.Auth{BotOwnerID: ownerID}
	modules.Install(rb.Bot, reg, auth)
	return rb, kv
}

func TestPing_RepliesPongAndWritesKV(t *testing.T) {
	rb, kv := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/ping"))

	if got := rb.LastSent().Text(); got != "pong" {
		t.Errorf("ping reply = %q, want %q", got, "pong")
	}
	var stored lastPing
	if err := kv.GetJSON(context.Background(), lastPingKey, &stored); err != nil {
		t.Fatalf("expected lastPing in KV: %v", err)
	}
	if stored.At <= 0 {
		t.Errorf("lastPing.At = %d, want positive", stored.At)
	}
	// Sanity: timestamp is within a minute of now (rules out stale fixture).
	if delta := time.Now().UTC().UnixMilli() - stored.At; delta > 60_000 || delta < 0 {
		t.Errorf("lastPing.At delta from now = %dms, want within 60s", delta)
	}
}

func TestMstats_NeverWhenKVEmpty(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/mstats"))

	if got := rb.LastSent().Text(); got != "last ping: never" {
		t.Errorf("mstats reply = %q, want 'last ping: never'", got)
	}
}

func TestMstats_AfterPing(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/ping"))
	rb.Reset()
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/mstats"))

	got := rb.LastSent().Text()
	if !strings.HasPrefix(got, "last ping: ") {
		t.Errorf("mstats reply = %q, want 'last ping: ...'", got)
	}
	if strings.Contains(got, "never") {
		t.Errorf("mstats still says 'never' after /ping: %q", got)
	}
}

func TestMstats_DeniedToNonAdmin(t *testing.T) {
	rb, _ := installMisc(t, 999) // owner = 999
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/mstats"))

	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-admin /mstats produced replies: %+v", calls)
	}
}

// trongTruongHopUpdate is the inline counterpart of testutil.NewPrivateMessage
// for cases that need control over From (username, names). The dispatcher
// requires a bot_command entity, so we lift that from the helper API by reusing
// NewPrivateMessage and overwriting From.
func trongTruongHopUpdate(t *testing.T, text string, from *models.User) *models.Update {
	t.Helper()
	u := testutil.NewPrivateMessage(from.ID, text)
	u.Message.From = from
	return u
}

func TestTrongTruongHop_DefaultArgUsesVNG(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), trongTruongHopUpdate(t, "/trongtruonghop",
		&models.User{ID: 7, Username: "boss", FirstName: "Boss"}))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "VNG") {
		t.Errorf("reply missing default target VNG: %q", got)
	}
	if n := strings.Count(got, "@boss"); n != 2 {
		t.Errorf("reply mentions @boss %d times, want 2: %q", n, got)
	}
}

func TestTrongTruongHop_CustomArg(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), trongTruongHopUpdate(t, "/trongtruonghop Acme Corp",
		&models.User{ID: 7, Username: "boss", FirstName: "Boss"}))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Acme Corp") {
		t.Errorf("reply missing custom arg Acme Corp: %q", got)
	}
	if strings.Contains(got, "VNG") {
		t.Errorf("reply unexpectedly contains default VNG: %q", got)
	}
}

func TestTrongTruongHop_HTMLEscapesArg(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), trongTruongHopUpdate(t, "/trongtruonghop <script>",
		&models.User{ID: 7, Username: "boss", FirstName: "Boss"}))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("reply did not HTML-escape arg: %q", got)
	}
	if strings.Contains(got, "<script>") {
		t.Errorf("reply leaked raw <script>: %q", got)
	}
}

func TestTrongTruongHop_NoUsernameFallsBackToLink(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), trongTruongHopUpdate(t, "/trongtruonghop",
		&models.User{ID: 42, FirstName: "Anh"})) // no Username

	got := rb.LastSent().Text()
	wantLink := `<a href="tg://user?id=42">Anh</a>`
	if n := strings.Count(got, wantLink); n != 2 {
		t.Errorf("reply contains link %q %d times, want 2: %q", wantLink, n, got)
	}
}

func TestTrongTruongHop_EmptyDisplayNameFallsBackToThanhVien(t *testing.T) {
	rb, _ := installMisc(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), trongTruongHopUpdate(t, "/trongtruonghop",
		&models.User{ID: 42})) // no Username, no FirstName/LastName

	got := rb.LastSent().Text()
	wantLink := `<a href="tg://user?id=42">thành viên</a>`
	if n := strings.Count(got, wantLink); n != 2 {
		t.Errorf("reply contains fallback link %d times, want 2: %q", n, got)
	}
}

func TestFortytwo_OwnerOnly(t *testing.T) {
	rb, _ := installMisc(t, 999)

	// Non-owner: silent denial
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/fortytwo"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /fortytwo replied: %+v", calls)
	}

	// Owner: reply
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/fortytwo"))
	if got := rb.LastSent().Text(); got != "The answer." {
		t.Errorf("owner /fortytwo reply = %q, want 'The answer.'", got)
	}
}
