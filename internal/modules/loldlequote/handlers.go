package loldlequote

import (
	"context"
	"fmt"
	"html"
	"math/rand"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/champname"
	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const newRoundHint = "🆕 Send <code>/loldle_quote</code> or <code>/loldle_quote &lt;champion&gt;</code> to start a new round."

// state captures everything a loldle-quote handler needs at runtime. Built
// once per Factory call and shared across the four command closures.
type state struct {
	kv    storage.KVStore
	pool  []QuoteChampion
	locks keylock.Map // serialises Get→mutate→Put per subject
}

// championName extracts the comparable name field for champname helpers.
func championName(c *QuoteChampion) string { return c.ChampionName }

func (s *state) pickRandom() *QuoteChampion {
	return &s.pool[rand.Intn(len(s.pool))]
}

func (s *state) startFreshGame(ctx context.Context, subject string) (*gameState, error) {
	target := s.pickRandom()
	g := &gameState{Target: target.ChampionName, Guesses: []string{}, StartedAt: nil}
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *state) getOrInitGame(ctx context.Context, subject string, maxGuesses int) (*gameState, error) {
	existing, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return nil, err
	}
	if existing != nil && len(existing.Guesses) < maxGuesses {
		return existing, nil
	}
	return s.startFreshGame(ctx, subject)
}

// handleQuote is /loldle_quote [champion] — show clue if no arg, else guess.
func (s *state) handleQuote(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()
	arg := chathelper.ArgAfterCommand(msg.Text)

	maxGuesses, err := getMaxGuesses(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	game, err := s.getOrInitGame(ctx, subject, maxGuesses)
	if err != nil {
		return err
	}
	target := champname.FindByExactName(s.pool, game.Target, championName)
	if target == nil {
		// Pool refreshed mid-round and the target is gone — start over.
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"Quote data was updated since this round started. "+newRoundHint)
	}

	if arg == "" {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, renderBoard(target.Quote, game.Guesses, maxGuesses))
	}

	guess := champname.Find(s.pool, arg, championName)
	if guess == nil {
		return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Champion not found: %q.", arg))
	}

	for _, prior := range game.Guesses {
		if prior == guess.ChampionName {
			return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
				"🔁 <b>%s</b> was already guessed this round — try another champion.",
				html.EscapeString(guess.ChampionName)))
		}
	}

	if game.StartedAt == nil {
		now := chathelper.NowMillis()
		game.StartedAt = &now
	}
	game.Guesses = append(game.Guesses, guess.ChampionName)
	won := guess.ChampionName == target.ChampionName
	answer := html.EscapeString(target.ChampionName)

	switch {
	case won:
		st, err := recordResult(ctx, s.kv, subject, true)
		if err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"🎉 Nailed it! <b>%s</b> — solved in %d/%d\n🔥 Streak: %d\n%s",
			answer, len(game.Guesses), maxGuesses, st.Streak, newRoundHint))

	case len(game.Guesses) >= maxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n❌ Out of guesses. Answer: <b>%s</b>.\n%s",
			renderBoard(target.Quote, game.Guesses, maxGuesses), answer, newRoundHint))

	default:
		if err := saveGame(ctx, s.kv, subject, game); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n❌ Not <b>%s</b>. Guess %d/%d.",
			renderBoard(target.Quote, game.Guesses, maxGuesses),
			html.EscapeString(guess.ChampionName), len(game.Guesses), maxGuesses))
	}
}

// handleGiveup is /loldle_quote_giveup — reveal answer + clear round.
func (s *state) handleGiveup(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()

	existing, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	if existing == nil {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, "No active round. "+newRoundHint)
	}
	if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
		return err
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"🏳️ Answer: <b>%s</b>.\n%s", html.EscapeString(existing.Target), newRoundHint))
}

// handleStats is /loldle_quote_stats — lifetime score.
func (s *state) handleStats(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	st, err := loadStats(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	scope := "group"
	if msg.Chat.Type == models.ChatTypePrivate {
		scope = "your"
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"📊 Loldle Quote %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, st.Played, st.Wins, chathelper.WinRate(st.Wins, st.Played), st.Streak, st.BestStreak))
}

// handleSetMax is /loldle_quote_setmax <n> — private; per-subject override.
func (s *state) handleSetMax(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	arg := chathelper.ArgAfterCommand(msg.Text)
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > MaxGuessesCap {
		return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /loldle_quote_setmax <1-%d>", MaxGuessesCap))
	}
	if err := setMaxGuesses(ctx, s.kv, subject, n); err != nil {
		return err
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("✅ Loldle quote max guesses set to %d (applies to the next round).", n))
}
