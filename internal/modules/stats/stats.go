// Package stats tracks per-command invocation counts persistently in KV and
// exposes /stats to display them sorted by popularity.
package stats

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot/internal/storage"
)

const countPrefix = "count:"

type countEntry struct {
	N int64 `json:"n"`
}

type counter struct {
	kv storage.KVStore
}

func countKey(name string) string { return countPrefix + name }

// Inc increments the persistent invocation count for the named command.
// Errors are logged and swallowed and concurrent invocations of the same
// command may lose updates — stats are best-effort. A future atomic
// increment (e.g. DynamoDB UpdateItem ADD) would close the race.
func (c *counter) Inc(ctx context.Context, name string) {
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

func statsCommand(c *counter) modules.Command {
	return modules.Command{
		Name:        "stats",
		Visibility:  modules.VisibilityPublic,
		Description: "Show command usage statistics",
		Handler: func(ctx context.Context, b *bot.Bot, update *models.Update) error {
			if update.Message == nil {
				return nil
			}
			keys, err := c.kv.List(ctx, countPrefix)
			if err != nil {
				log.Error("stats: kv list failed", "err", err)
				return chathelper.Reply(ctx, b, update.Message, "Could not load stats. Try again later.")
			}
			if len(keys) == 0 {
				return chathelper.Reply(ctx, b, update.Message, "No command stats yet.")
			}

			// Fan-out the per-key GetJSONs concurrently. Sequential reads make
			// /stats latency O(N) round-trips to DynamoDB; on a cold Lambda
			// container with 20+ commands, that can push the synchronous
			// handler past the 10s webhook deadline and the trailing Reply
			// SendMessage then fails on a cancelled ctx — the user sees no
			// reply at all. Fanning out collapses wall-clock latency to one
			// round-trip while keeping the same per-key error isolation.
			type row struct {
				name string
				n    int64
				ok   bool
			}
			rows := make([]row, len(keys))
			var wg sync.WaitGroup
			for i, k := range keys {
				wg.Add(1)
				go func(i int, k string) {
					defer wg.Done()
					name := strings.TrimPrefix(k, countPrefix)
					var entry countEntry
					if err := c.kv.GetJSON(ctx, k, &entry); err != nil {
						log.Error("stats: kv get failed during render", "key", k, "err", err)
						rows[i] = row{name: name}
						return
					}
					rows[i] = row{name: name, n: entry.N, ok: true}
				}(i, k)
			}
			wg.Wait()
			kept := rows[:0]
			for _, r := range rows {
				if r.ok {
					kept = append(kept, r)
				}
			}
			rows = kept
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].n != rows[j].n {
					return rows[i].n > rows[j].n
				}
				return rows[i].name < rows[j].name
			})

			var sb strings.Builder
			sb.WriteString("Command usage:\n")
			for _, r := range rows {
				fmt.Fprintf(&sb, "/%s: %d\n", r.name, r.n)
			}
			const telegramMaxLen = 4000 // leave margin below Telegram's 4096-byte hard limit
			output := strings.TrimSuffix(sb.String(), "\n")
			if len(output) > telegramMaxLen {
				cutoff := strings.LastIndexByte(output[:telegramMaxLen], '\n')
				if cutoff <= 0 {
					cutoff = telegramMaxLen
				}
				output = output[:cutoff] + "\n…(truncated)"
			}
			return chathelper.Reply(ctx, b, update.Message, output)
		},
	}
}
