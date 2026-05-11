package loldle

import (
	"context"
	"fmt"
	"html"
	"math/rand"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const newRoundHint = "🆕 Send <code>/loldle</code> or <code>/loldle &lt;champion&gt;</code> to start a new round."

// state captures everything a loldle handler needs at runtime. Built once
// per Factory call and shared across the four command closures.
type state struct {
	kv        storage.KVStore
	champions []Champion
	locks     keylock.Map // serialises Get→mutate→Put per subject
}

// pickRandomChampion uses math/rand's mutex-protected globals so concurrent
// /loldle handlers don't race on a shared *rand.Rand.
func (s *state) pickRandomChampion() *Champion {
	return &s.champions[rand.Intn(len(s.champions))]
}

func (s *state) findByName(name string) *Champion {
	return findChampionByExactName(s.champions, name)
}

// rehydrateGuesses recomputes board rows from the stored championNames.
// Champions removed from champions.json since the round started are skipped
// (returns the surviving prefix), matching JS.
func (s *state) rehydrateGuesses(g *gameState) []boardEntry {
	target := s.findByName(g.Target)
	if target == nil {
		return nil
	}
	out := make([]boardEntry, 0, len(g.Guesses))
	for _, name := range g.Guesses {
		guess := s.findByName(name)
		if guess == nil {
			continue
		}
		out = append(out, boardEntry{Champion: name, Results: CompareChampions(guess, target)})
	}
	return out
}

// startFreshGame writes a new round with no startedAt clock yet.
func (s *state) startFreshGame(ctx context.Context, subject string) (*gameState, error) {
	target := s.pickRandomChampion()
	g := &gameState{
		Target:    target.ChampionName,
		Guesses:   []string{},
		StartedAt: nil,
	}
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return nil, err
	}
	return g, nil
}

// getOrInitGame loads the active round; if absent (or the saved round has
// already exhausted its budget — defensive), starts a fresh one.
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

// trySendSticker sends a sticker, swallowing errors. A bad/expired file_id
// must never block the text reply that carries the round outcome.
func trySendSticker(ctx context.Context, b *bot.Bot, chatID int64, pool []string) {
	id := pickSticker(pool)
	if id == "" {
		return
	}
	_, _ = b.SendSticker(ctx, &bot.SendStickerParams{
		ChatID:  chatID,
		Sticker: &models.InputFileString{Data: id},
	})
}

// handleLoldle is /loldle [champion] — show board if no arg, else submit guess.
func (s *state) handleLoldle(ctx context.Context, b *bot.Bot, update *models.Update) error {
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

	if arg == "" {
		header := fmt.Sprintf("Guess %d/%d. Use <code>/loldle &lt;champion&gt;</code>.",
			len(game.Guesses), maxGuesses)
		board := renderBoard(s.rehydrateGuesses(game))
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, header+"\n\n"+board)
	}

	guess := findChampion(s.champions, arg)
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

	target := s.findByName(game.Target)
	if target == nil {
		// champions.json was refreshed and the target is gone. Drop the
		// round so the next /loldle starts cleanly.
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"Champion data was updated since this round started. "+newRoundHint)
	}

	results := CompareChampions(guess, target)
	if game.StartedAt == nil {
		now := chathelper.NowMillis()
		game.StartedAt = &now
	}
	game.Guesses = append(game.Guesses, guess.ChampionName)
	won := guess.ChampionName == target.ChampionName

	rendered := renderGuess(guess.ChampionName, results)
	champ := html.EscapeString(target.ChampionName)
	elapsed := formatDuration(chathelper.NowMillis() - *game.StartedAt)

	switch {
	case won:
		st, err := recordResult(ctx, s.kv, subject, true)
		if err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		trySendSticker(ctx, b, msg.Chat.ID, winStickers)
		flavor := attemptFlavor(len(game.Guesses), maxGuesses)
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n🎉 %s %s\n⏱ %s · 🔥 Streak: %d (%d/%d)\n%s",
			rendered, flavor, champ, elapsed, st.Streak, len(game.Guesses), maxGuesses, newRoundHint))

	case len(game.Guesses) >= maxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		trySendSticker(ctx, b, msg.Chat.ID, loseStickers)
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\n❌ Out of guesses. Answer was %s.\n%s", rendered, champ, newRoundHint))

	default:
		if err := saveGame(ctx, s.kv, subject, game); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"%s\n\nGuess %d/%d.", rendered, len(game.Guesses), maxGuesses))
	}
}

// handleGiveup is /loldle_giveup — reveal answer + clear round.
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
	target := s.findByName(existing.Target)
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	trySendSticker(ctx, b, msg.Chat.ID, giveupStickers)
	answer := existing.Target
	if target != nil {
		answer = target.ChampionName
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
		fmt.Sprintf("🏳️ Answer was %s.\n%s", html.EscapeString(answer), newRoundHint))
}

// handleStats is /loldle_stats — lifetime score.
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
		"📊 Loldle %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, st.Played, st.Wins, chathelper.WinRate(st.Wins, st.Played), st.Streak, st.BestStreak))
}

// handleSetMax is /loldle_setmax <n> — private command, sets the per-subject
// MaxGuesses override (1..MaxGuessesCap). Takes effect on the next round.
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
		return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /loldle_setmax <1-%d>", MaxGuessesCap))
	}
	if err := setMaxGuesses(ctx, s.kv, subject, n); err != nil {
		return err
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("✅ Loldle max guesses set to %d (applies to the next round).", n))
}
