package wordle

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// state captures everything a wordle command needs at handler-time. Built
// once in New and shared across all four handlers via closure.
type state struct {
	kv    storage.KVStore
	words []string
	set   map[string]struct{}
	locks keylock.Map // per-subject mutex; serialises Get→mutate→Put
}

// subjectFor mirrors JS getSubject: per-user in DMs, per-chat in groups,
// per-user fallback for channels and unknown types. Returns an empty string
// when no usable subject id is present (caller replies with an error).
func subjectFor(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	switch msg.Chat.Type {
	case models.ChatTypePrivate:
		if msg.From != nil {
			return strconv.FormatInt(msg.From.ID, 10)
		}
	case models.ChatTypeGroup, models.ChatTypeSupergroup:
		return strconv.FormatInt(msg.Chat.ID, 10)
	default:
		if msg.From != nil {
			return strconv.FormatInt(msg.From.ID, 10)
		}
	}
	return ""
}

// argAfterCommand returns everything after the first space in text, trimmed.
// JS-parity. Works for `/wordle apple`, `/wordle@bot apple`, etc.
func argAfterCommand(text string) string {
	if text == "" {
		return ""
	}
	idx := strings.IndexByte(text, ' ')
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}

// rejectMessage maps a validation failure into the user-facing reply. JS
// parity word-for-word.
func rejectMessage(reason rejectReason) string {
	switch reason {
	case reasonEmpty:
		return fmt.Sprintf("Please provide a %d-letter word.", WordLength)
	case reasonLength:
		return fmt.Sprintf("Word must be exactly %d letters.", WordLength)
	default:
		return "Not in the word list."
	}
}

// reply is a tiny helper so the four handlers don't all repeat the same
// SendMessage incantation. Returns the SendMessage error to the dispatcher.
func reply(ctx context.Context, b *bot.Bot, msg *models.Message, text string) error {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
	})
	return err
}

// nowMillis returns current UTC ms-since-epoch — single source of "now" so
// tests can substitute via the kv-state path if needed.
func nowMillis() int64 { return time.Now().UTC().UnixMilli() }

// startFresh writes a brand-new round and returns it. Errors propagate to
// the caller (not swallowed — a KV failure means subsequent ops will lie).
func (s *state) startFresh(ctx context.Context, subject string) (*GameState, error) {
	target, err := pickRandom(s.words, nil)
	if err != nil {
		return nil, fmt.Errorf("wordle startFresh: %w", err)
	}
	g := &GameState{
		Target:    target,
		Guesses:   []GuessRecord{},
		Solved:    false,
		Giveup:    false,
		StartedAt: nowMillis(),
	}
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *state) getOrInit(ctx context.Context, subject string) (*GameState, error) {
	g, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return nil, err
	}
	if g != nil {
		return g, nil
	}
	return s.startFresh(ctx, subject)
}

// handleWordle is /wordle [word] — show board if no arg, else submit a guess.
func (s *state) handleWordle(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()
	arg := argAfterCommand(msg.Text)

	g, err := s.getOrInit(ctx, subject)
	if err != nil {
		return err
	}

	if arg == "" {
		var header string
		switch {
		case g.Solved:
			header = fmt.Sprintf("🎉 Solved in %d/%d. /wordle_new for another.", len(g.Guesses), MaxGuesses)
		case g.Giveup:
			header = fmt.Sprintf("🏳️ Gave up. Answer was %s. /wordle_new for another.", strings.ToUpper(g.Target))
		default:
			header = fmt.Sprintf("Guess %d/%d. Use `/wordle <word>`.", len(g.Guesses), MaxGuesses)
		}
		return reply(ctx, b, msg, header+"\n\n"+renderBoard(g.Guesses))
	}

	if isFinished(g) {
		return reply(ctx, b, msg,
			fmt.Sprintf("Current round is over. Use /wordle_new to start another. Answer was %s.", strings.ToUpper(g.Target)))
	}

	v := validateGuess(s.set, arg)
	if !v.OK {
		return reply(ctx, b, msg, rejectMessage(v.Reason))
	}

	results := CompareWords(v.Word, g.Target)
	g.Guesses = append(g.Guesses, GuessRecord{Word: v.Word, Results: results})
	won := v.Word == g.Target
	if won {
		g.Solved = true
	}
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return err
	}

	rendered := renderGuess(v.Word, results)
	switch {
	case won:
		stats, err := recordResult(ctx, s.kv, subject, true, nowMillis())
		if err != nil {
			return err
		}
		return reply(ctx, b, msg, fmt.Sprintf("%s\n\n🎉 Solved in %d/%d! Streak: %d. /wordle_new for another.",
			rendered, len(g.Guesses), MaxGuesses, stats.Streak))
	case len(g.Guesses) >= MaxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false, nowMillis()); err != nil {
			return err
		}
		return reply(ctx, b, msg, fmt.Sprintf("%s\n\n❌ Out of guesses. Answer was %s. /wordle_new to retry.",
			rendered, strings.ToUpper(g.Target)))
	default:
		return reply(ctx, b, msg, fmt.Sprintf("%s\n\nGuess %d/%d.", rendered, len(g.Guesses), MaxGuesses))
	}
}

// handleNew is /wordle_new — abandons any in-progress round (counts as
// giveup → stats hit) and starts fresh.
func (s *state) handleNew(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()

	prelude := ""
	prior, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	if prior != nil && !isFinished(prior) {
		if _, err := recordResult(ctx, s.kv, subject, false, nowMillis()); err != nil {
			return err
		}
		prelude = fmt.Sprintf("🏳️ Previous round abandoned (auto-giveup). Answer was %s.\n\n",
			strings.ToUpper(prior.Target))
	}

	if _, err := s.startFresh(ctx, subject); err != nil {
		return err
	}
	return reply(ctx, b, msg, prelude+"🆕 New round started. Use `/wordle <word>` to guess.")
}

// handleGiveup is /wordle_giveup — reveals answer for the current round.
// Idempotent on already-finished rounds (parrots the same answer back).
func (s *state) handleGiveup(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()
	g, err := s.getOrInit(ctx, subject)
	if err != nil {
		return err
	}
	if g.Solved {
		return reply(ctx, b, msg, fmt.Sprintf("Already solved — %s.", strings.ToUpper(g.Target)))
	}
	if g.Giveup {
		return reply(ctx, b, msg, fmt.Sprintf("Already gave up — %s.", strings.ToUpper(g.Target)))
	}
	g.Giveup = true
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return err
	}
	if _, err := recordResult(ctx, s.kv, subject, false, nowMillis()); err != nil {
		return err
	}
	return reply(ctx, b, msg, fmt.Sprintf("🏳️ Answer was %s. /wordle_new for another.", strings.ToUpper(g.Target)))
}

// handleStats is /wordle_stats — shows lifetime score for the subject.
func (s *state) handleStats(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg, "Cannot identify chat.")
	}
	stats, err := loadStats(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	winRate := 0
	if stats.Played > 0 {
		// math.Round matches JS Math.round (round half away from zero for
		// positive inputs); int(...) would truncate 66.66 to 66 where JS
		// shows 67.
		winRate = int(math.Round(float64(stats.Wins) / float64(stats.Played) * 100))
	}
	scope := "group"
	if msg.Chat.Type == models.ChatTypePrivate {
		scope = "your"
	}
	return reply(ctx, b, msg, fmt.Sprintf(
		"📊 Wordle %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, stats.Played, stats.Wins, winRate, stats.Streak, stats.BestStreak,
	))
}
