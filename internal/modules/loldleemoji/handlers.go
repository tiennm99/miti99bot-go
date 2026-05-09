package loldleemoji

import (
	"context"
	"fmt"
	"html"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const newRoundHint = "🆕 Send <code>/loldle_emoji</code> or <code>/loldle_emoji &lt;champion&gt;</code> to start a new round."

// state captures everything a loldle-emoji handler needs at runtime. Built
// once per Factory call and shared across the four command closures.
type state struct {
	kv    storage.KVStore
	pool  []EmojiChampion
	locks keylock.Map // serialises Get→mutate→Put per subject
}

// subjectFor: group/supergroup → chat ID, otherwise → user ID. Matches the
// JS source (loldle-emoji/handlers.js getSubject) and classic loldle. Same
// helper exists in classic loldle's handlers.go — duplication accepted at
// 2 callers; will extract once a third loldle variant lands (5 total are
// expected per Phase 6).
func subjectFor(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	switch msg.Chat.Type {
	case models.ChatTypeGroup, models.ChatTypeSupergroup:
		return strconv.FormatInt(msg.Chat.ID, 10)
	default:
		if msg.From != nil {
			return strconv.FormatInt(msg.From.ID, 10)
		}
	}
	return ""
}

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

func (s *state) pickRandom() *EmojiChampion {
	return &s.pool[rand.Intn(len(s.pool))]
}

func nowMillis() int64 { return time.Now().UTC().UnixMilli() }

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

func reply(ctx context.Context, b *bot.Bot, chatID int64, text string) error {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

func replyHTML(ctx context.Context, b *bot.Bot, chatID int64, text string) error {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	return err
}

// handleEmoji is /loldle_emoji [champion] — show clue if no arg, else guess.
func (s *state) handleEmoji(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()
	arg := argAfterCommand(msg.Text)

	maxGuesses, err := getMaxGuesses(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	game, err := s.getOrInitGame(ctx, subject, maxGuesses)
	if err != nil {
		return err
	}
	target := findByExactName(s.pool, game.Target)
	if target == nil {
		// Pool refreshed mid-round and the target is gone — start over.
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return replyHTML(ctx, b, msg.Chat.ID,
			"Emoji data was updated since this round started. "+newRoundHint)
	}

	if arg == "" {
		return replyHTML(ctx, b, msg.Chat.ID, renderBoard(target.Emojis, game.Guesses, maxGuesses))
	}

	guess := findChampion(s.pool, arg)
	if guess == nil {
		return reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Champion not found: %q.", arg))
	}

	for _, prior := range game.Guesses {
		if prior == guess.ChampionName {
			return replyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
				"🔁 <b>%s</b> was already guessed this round — try another champion.",
				html.EscapeString(guess.ChampionName)))
		}
	}

	if game.StartedAt == nil {
		now := nowMillis()
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
		return replyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"🎉 Got it! <b>%s</b> — solved in %d/%d\n🔥 Streak: %d\n%s",
			answer, len(game.Guesses), maxGuesses, st.Streak, newRoundHint))

	case len(game.Guesses) >= maxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return replyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n❌ Out of guesses. Answer was <b>%s</b>.\n%s",
			renderBoard(target.Emojis, game.Guesses, maxGuesses), answer, newRoundHint))

	default:
		if err := saveGame(ctx, s.kv, subject, game); err != nil {
			return err
		}
		return replyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n❌ Not <b>%s</b>. Guess %d/%d.",
			renderBoard(target.Emojis, game.Guesses, maxGuesses),
			html.EscapeString(guess.ChampionName), len(game.Guesses), maxGuesses))
	}
}

// handleGiveup is /loldle_emoji_giveup — reveal answer + clear round.
func (s *state) handleGiveup(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	defer s.locks.Acquire(subject)()

	existing, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	if existing == nil {
		return replyHTML(ctx, b, msg.Chat.ID, "No active round. "+newRoundHint)
	}
	if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
		return err
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return replyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"🏳️ Answer was <b>%s</b>.\n%s", html.EscapeString(existing.Target), newRoundHint))
}

// handleStats is /loldle_emoji_stats — lifetime score.
func (s *state) handleStats(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	st, err := loadStats(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	winRate := 0
	if st.Played > 0 {
		// math.Round matches JS Math.round; int(...) truncation would render
		// 2/3 as 66% where the JS source shows 67%.
		winRate = int(math.Round(float64(st.Wins) / float64(st.Played) * 100))
	}
	scope := "group"
	if msg.Chat.Type == models.ChatTypePrivate {
		scope = "your"
	}
	return reply(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"📊 Loldle Emoji %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, st.Played, st.Wins, winRate, st.Streak, st.BestStreak))
}

// handleSetMax is /loldle_emoji_setmax <n> — private; per-subject override.
func (s *state) handleSetMax(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := subjectFor(msg)
	if subject == "" {
		return reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	arg := argAfterCommand(msg.Text)
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > MaxGuessesCap {
		return reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /loldle_emoji_setmax <1-%d>", MaxGuessesCap))
	}
	if err := setMaxGuesses(ctx, s.kv, subject, n); err != nil {
		return err
	}
	return reply(ctx, b, msg.Chat.ID, fmt.Sprintf("✅ Loldle emoji max guesses set to %d (applies to the next round).", n))
}
