package stats

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
)

const telegramMaxLen = 4000 // leave margin below Telegram's 4096-byte hard limit

const statsUsage = `Usage:
/stats
/stats users
/stats user <username>
/stats cmd <name>`

type row struct {
	display string
	n       int64
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
			text := renderStats(ctx, c, parseSubargs(update))
			return chathelper.Reply(ctx, b, update.Message, text)
		},
	}
}

// renderStats dispatches to the requested view. Exposed as a pure function
// (no *bot.Bot) so tests can assert on output without driving a recording bot
// through the full command-routing path.
func renderStats(ctx context.Context, c *counter, args string) string {
	fields := strings.Fields(args)
	switch {
	case len(fields) == 0:
		return viewTopCommands(ctx, c)
	case fields[0] == "users":
		return viewTopUsers(ctx, c)
	case fields[0] == "user":
		if len(fields) < 2 {
			return statsUsage
		}
		return viewUserCommands(ctx, c, strings.TrimPrefix(fields[1], "@"))
	case fields[0] == "cmd":
		if len(fields) < 2 {
			return statsUsage
		}
		return viewCmdUsers(ctx, c, fields[1])
	default:
		return statsUsage
	}
}

// parseSubargs returns the message text after the /stats command token,
// stripped of the @botname suffix and leading whitespace. Mirrors the
// entity-stripping logic in dispatcher.matchCommand so /stats@miti99bot users
// behaves like /stats users in groups.
func parseSubargs(update *models.Update) string {
	msg := update.Message
	for _, e := range msg.Entities {
		if e.Type != models.MessageEntityTypeBotCommand {
			continue
		}
		end := e.Offset + e.Length
		if e.Offset < 0 || end > len(msg.Text) || e.Length < 1 {
			continue
		}
		return strings.TrimLeft(msg.Text[end:], " \t")
	}
	return ""
}

func viewTopCommands(ctx context.Context, c *counter) string {
	keys, err := c.kv.List(ctx, countPrefix)
	if err != nil {
		log.Error("stats: kv list failed", "err", err)
		return "Could not load stats. Try again later."
	}
	if len(keys) == 0 {
		return "No command stats yet."
	}
	rows := fanOutCountRows(ctx, c, keys, func(k string) string {
		return "/" + strings.TrimPrefix(k, countPrefix)
	})
	if len(rows) == 0 {
		return "No command stats yet."
	}
	sortRows(rows)
	return renderTopN("Command usage:", rows, topK)
}

func viewTopUsers(ctx context.Context, c *counter) string {
	users := loadUserRowsWithID(ctx, c)
	rows := make([]row, 0, len(users))
	for _, u := range users {
		if u.Username == "" {
			continue
		}
		rows = append(rows, row{display: "@" + u.Username, n: u.N})
	}
	if len(rows) == 0 {
		return "No user stats yet."
	}
	sortRows(rows)
	return renderTopN("Top users:", rows, topK)
}

func viewUserCommands(ctx context.Context, c *counter, username string) string {
	users := loadUserRowsWithID(ctx, c)
	// Telegram allows username reuse after one user changes theirs, so two
	// distinct user IDs may briefly share a username. First match wins; map
	// iteration order is nondeterministic, so the choice is not stable across
	// renders. Accepted limitation — disambiguating would need historical
	// (timestamp, ID) data we don't store.
	var (
		foundID int64
		ok      bool
	)
	for id, u := range users {
		if u.Username == username {
			foundID = id
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Sprintf("User @%s not found.", username)
	}

	keys, err := c.kv.List(ctx, pairPrefix)
	if err != nil {
		log.Error("stats: kv list failed", "err", err)
		return "Could not load stats. Try again later."
	}
	// Leading colon disambiguates: ":2" does not match a key ending in "12"
	// because IDs are bare decimal integers with no internal punctuation.
	suffix := ":" + strconv.FormatInt(foundID, 10)
	var matching []string
	for _, k := range keys {
		if strings.HasSuffix(k, suffix) {
			matching = append(matching, k)
		}
	}
	if len(matching) == 0 {
		return fmt.Sprintf("No commands recorded for @%s.", username)
	}

	rows := fanOutCountRows(ctx, c, matching, func(k string) string {
		// k looks like "pair:<cmd>:<id>" — drop the prefix and trailing :<id>.
		rest := strings.TrimPrefix(k, pairPrefix)
		idx := strings.LastIndexByte(rest, ':')
		if idx < 0 {
			return "/" + rest
		}
		return "/" + rest[:idx]
	})
	if len(rows) == 0 {
		return fmt.Sprintf("No commands recorded for @%s.", username)
	}
	sortRows(rows)
	return renderTopN(fmt.Sprintf("Commands by @%s:", username), rows, topK)
}

func viewCmdUsers(ctx context.Context, c *counter, cmd string) string {
	prefix := pairPrefix + cmd + ":"
	keys, err := c.kv.List(ctx, prefix)
	if err != nil {
		log.Error("stats: kv list failed", "err", err)
		return "Could not load stats. Try again later."
	}
	if len(keys) == 0 {
		return fmt.Sprintf("Command /%s has no users yet.", cmd)
	}

	type result struct {
		r  row
		ok bool
	}
	results := make([]result, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		go func(i int, k string) {
			defer wg.Done()
			var ce countEntry
			if err := c.kv.GetJSON(ctx, k, &ce); err != nil {
				log.Error("stats: kv get failed", "key", k, "err", err)
				return
			}
			idStr := strings.TrimPrefix(k, prefix)
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return
			}
			var ue userEntry
			if err := c.kv.GetJSON(ctx, userKey(id), &ue); err != nil || ue.Username == "" {
				return
			}
			results[i] = result{r: row{display: "@" + ue.Username, n: ce.N}, ok: true}
		}(i, k)
	}
	wg.Wait()

	rows := make([]row, 0, len(results))
	for _, r := range results {
		if r.ok {
			rows = append(rows, r.r)
		}
	}
	if len(rows) == 0 {
		return fmt.Sprintf("Command /%s has no named users yet.", cmd)
	}
	sortRows(rows)
	return renderTopN(fmt.Sprintf("Users of /%s:", cmd), rows, topK)
}

// fanOutCountRows runs GetJSON on each key in parallel. displayFor maps the
// raw sort key to the human-readable label that gets rendered. Missing /
// errored keys are skipped (logged at error level).
func fanOutCountRows(ctx context.Context, c *counter, keys []string, displayFor func(k string) string) []row {
	type result struct {
		r  row
		ok bool
	}
	results := make([]result, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		go func(i int, k string) {
			defer wg.Done()
			var entry countEntry
			if err := c.kv.GetJSON(ctx, k, &entry); err != nil {
				log.Error("stats: kv get failed during render", "key", k, "err", err)
				return
			}
			results[i] = result{r: row{display: displayFor(k), n: entry.N}, ok: true}
		}(i, k)
	}
	wg.Wait()

	rows := make([]row, 0, len(results))
	for _, r := range results {
		if r.ok {
			rows = append(rows, r.r)
		}
	}
	return rows
}

// loadUserRowsWithID returns userID → userEntry for every user:* row. Used by
// viewTopUsers (renders) and viewUserCommands (resolves a username to its ID).
func loadUserRowsWithID(ctx context.Context, c *counter) map[int64]userEntry {
	keys, err := c.kv.List(ctx, userPrefix)
	if err != nil {
		log.Error("stats: kv list failed", "err", err)
		return nil
	}
	type pair struct {
		id    int64
		entry userEntry
		ok    bool
	}
	results := make([]pair, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		go func(i int, k string) {
			defer wg.Done()
			idStr := strings.TrimPrefix(k, userPrefix)
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return
			}
			var entry userEntry
			if err := c.kv.GetJSON(ctx, k, &entry); err != nil {
				log.Error("stats: kv get failed", "key", k, "err", err)
				return
			}
			results[i] = pair{id: id, entry: entry, ok: true}
		}(i, k)
	}
	wg.Wait()

	out := make(map[int64]userEntry, len(results))
	for _, p := range results {
		if p.ok {
			out[p.id] = p.entry
		}
	}
	return out
}

func sortRows(rows []row) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].display < rows[j].display
	})
}

func renderTopN(header string, rows []row, k int) string {
	if k > 0 && len(rows) > k {
		rows = rows[:k]
	}
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteByte('\n')
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s: %d\n", r.display, r.n)
	}
	output := strings.TrimSuffix(sb.String(), "\n")
	if len(output) > telegramMaxLen {
		cutoff := strings.LastIndexByte(output[:telegramMaxLen], '\n')
		if cutoff <= 0 {
			cutoff = telegramMaxLen
		}
		output = output[:cutoff] + "\n…(truncated)"
	}
	return output
}
