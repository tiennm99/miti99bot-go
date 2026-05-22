// Package deploynotify sends a one-shot Telegram DM to the bot owner when
// the binary starts up with a git SHA that hasn't been notified before.
//
// Wiring: cmd/server/main.go calls Run after modules.Install. The package-
// level gitSHA variable in main is populated via -ldflags at build time
// (see Makefile). An empty gitSHA — e.g. `go run` or a build without the
// ldflags — is treated as a signal to stay silent.
//
// Dedup: a single KV record (key=last_notified_sha) holds the most recently
// notified SHA. On match we return early; on miss we send first, then write
// — so a transient Telegram failure doesn't permanently silence retries.
package deploynotify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-telegram/bot"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// kvKey is the single KV slot this package owns.
const kvKey = "last_notified_sha"

// defaultTimeout caps the whole Run path (KV read + Telegram send + KV write)
// so a misbehaving network can never block Lambda init past its 10s budget.
const defaultTimeout = 3 * time.Second

// Config bundles the runtime dependencies. Sender is a seam for tests; when
// nil, Run falls back to bot.SendMessage.
type Config struct {
	Bot     *bot.Bot
	KV      storage.KVStore
	OwnerID int64
	GitSHA  string
	Timeout time.Duration
	// Sender is the indirection used by tests. Production wiring leaves it
	// nil and Run uses cfg.Bot.SendMessage.
	Sender func(ctx context.Context, chatID int64, text string) error
}

// notifyRecord is the KV value shape. At is informational only — useful for
// eyeballing in the DynamoDB console; not consulted by the code path.
type notifyRecord struct {
	SHA string `json:"sha"`
	At  int64  `json:"at"`
}

// Run is the entry point. Fire-and-forget — never returns an error and
// never panics. Designed to be called once during process init.
func Run(ctx context.Context, cfg Config) {
	if reason := skipReason(cfg); reason != "" {
		log.Info("deploynotify skip", "reason", reason)
		return
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	notify, err := shouldNotify(ctx, cfg.KV, cfg.GitSHA)
	if err != nil {
		// Treat KV errors as "not notified yet": worst case is one extra
		// DM on the next cold start, which is far better than going
		// silent on every deploy because DynamoDB threw a transient.
		log.Warn("deploynotify kv read failed; will attempt send anyway", "err", err)
		notify = true
	}
	if !notify {
		return
	}

	if err := sendMessage(ctx, cfg, renderMessage(cfg.GitSHA)); err != nil {
		log.Warn("deploynotify telegram send failed", "err", err, "owner", cfg.OwnerID)
		return
	}
	if err := markNotified(ctx, cfg.KV, cfg.GitSHA); err != nil {
		log.Warn("deploynotify kv write failed (owner was notified)", "err", err)
		return
	}
	log.Info("deploynotify sent", "sha", cfg.GitSHA, "owner", cfg.OwnerID)
}

// skipReason returns a non-empty short string when Run should no-op without
// touching KV or Telegram. Empty string ⇒ proceed.
func skipReason(cfg Config) string {
	switch {
	case cfg.GitSHA == "":
		return "empty gitSHA (build without -ldflags)"
	case cfg.OwnerID == 0:
		return "no BOT_OWNER_ID configured"
	case cfg.KV == nil:
		return "no KV configured"
	case cfg.Bot == nil && cfg.Sender == nil:
		return "no bot or sender configured"
	}
	return ""
}

// shouldNotify reports whether sha differs from the last notified value.
// A missing record (ErrNotFound) is treated as "yes, notify".
func shouldNotify(ctx context.Context, kv storage.KVStore, sha string) (bool, error) {
	var prev notifyRecord
	err := kv.GetJSON(ctx, kvKey, &prev)
	if errors.Is(err, storage.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return prev.SHA != sha, nil
}

// markNotified writes the SHA + current timestamp to KV.
func markNotified(ctx context.Context, kv storage.KVStore, sha string) error {
	return kv.PutJSON(ctx, kvKey, notifyRecord{
		SHA: sha,
		At:  time.Now().UTC().UnixMilli(),
	})
}

// renderMessage is exposed for tests; keep the format stable enough that the
// owner can grep their Telegram history by SHA.
func renderMessage(sha string) string {
	return fmt.Sprintf("🚀 miti99bot deployed: %s", sha)
}

// sendMessage routes through Config.Sender when set (tests); otherwise it
// calls bot.SendMessage directly. Plain text — no parse_mode — so the SHA
// renders verbatim even if it ever contained Markdown-special characters.
func sendMessage(ctx context.Context, cfg Config, text string) error {
	if cfg.Sender != nil {
		return cfg.Sender(ctx, cfg.OwnerID, text)
	}
	_, err := cfg.Bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: cfg.OwnerID,
		Text:   text,
	})
	return err
}
