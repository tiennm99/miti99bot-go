// Package stats tracks per-command and per-user invocation counts persistently
// in KV and exposes /stats subcommands to display them sorted by popularity.
package stats

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/storage"
)

// Sort-key shapes inside the stats module's KV partition:
//   count:<cmd>            → countEntry — total per command
//   user:<userID>          → userEntry  — cached username + total per user
//   pair:<cmd>:<userID>    → countEntry — per (command, user) pair
const (
	countPrefix = "count:"
	userPrefix  = "user:"
	pairPrefix  = "pair:"
	topK        = 20
)

type countEntry struct {
	N int64 `json:"n"`
}

type userEntry struct {
	Username string `json:"username"`
	N        int64  `json:"n"`
}

type counter struct {
	kv storage.KVStore
}

func countKey(name string) string { return countPrefix + name }

func userKey(id int64) string { return userPrefix + strconv.FormatInt(id, 10) }

func pairKey(cmd string, id int64) string {
	return pairPrefix + cmd + ":" + strconv.FormatInt(id, 10)
}

// Inc fans out persistent counter writes for one command invocation.
// Always increments count:<cmd>. When the originating user has a Telegram
// username, also increments user:<id> (refreshing the cached username) and
// pair:<cmd>:<id>. Errors are logged and swallowed; concurrent invocations of
// the same (cmd, user) may lose updates — stats are best-effort. A future
// atomic increment (e.g. DynamoDB UpdateItem ADD) would close the race.
func (c *counter) Inc(ctx context.Context, name string, update *models.Update) {
	var (
		userID   int64
		username string
		hasUser  bool
	)
	if update != nil && update.Message != nil && update.Message.From != nil && update.Message.From.Username != "" {
		userID = update.Message.From.ID
		username = update.Message.From.Username
		hasUser = true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.incCount(ctx, name)
	}()
	if hasUser {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.incUser(ctx, userID, username)
		}()
		go func() {
			defer wg.Done()
			c.incPair(ctx, name, userID)
		}()
	}
	wg.Wait()
}

func (c *counter) incCount(ctx context.Context, name string) {
	key := countKey(name)
	var entry countEntry
	if err := c.kv.GetJSON(ctx, key, &entry); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Error("stats: kv get failed", "key", key, "err", err)
		return
	}
	entry.N++
	if err := c.kv.PutJSON(ctx, key, entry); err != nil {
		log.Error("stats: kv put failed", "key", key, "err", err)
	}
}

func (c *counter) incUser(ctx context.Context, id int64, username string) {
	key := userKey(id)
	var entry userEntry
	if err := c.kv.GetJSON(ctx, key, &entry); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Error("stats: kv get failed", "key", key, "err", err)
		return
	}
	entry.Username = username
	entry.N++
	if err := c.kv.PutJSON(ctx, key, entry); err != nil {
		log.Error("stats: kv put failed", "key", key, "err", err)
	}
}

func (c *counter) incPair(ctx context.Context, name string, id int64) {
	key := pairKey(name, id)
	var entry countEntry
	if err := c.kv.GetJSON(ctx, key, &entry); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Error("stats: kv get failed", "key", key, "err", err)
		return
	}
	entry.N++
	if err := c.kv.PutJSON(ctx, key, entry); err != nil {
		log.Error("stats: kv put failed", "key", key, "err", err)
	}
}

// New is the module Factory. Registers a CommandHook that persists counts and
// a /stats command that displays them.
func New(deps modules.Deps) modules.Module {
	c := &counter{kv: deps.KV}
	return modules.Module{
		CommandHook: c.Inc,
		Commands: []modules.Command{
			statsCommand(c),
		},
	}
}
