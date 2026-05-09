package loldlesplash

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

const newRoundHint = "🆕 Send <code>/loldle_splash</code> or <code>/loldle_splash &lt;champion&gt;</code> to start a new round."

// state captures everything a loldle-splash handler needs at runtime. Built
// once per Factory call and shared across the four command closures.
type state struct {
	kv    storage.KVStore
	pool  []SplashChampion
	locks keylock.Map // serialises Get→mutate→Put per subject
}

// championName extracts the comparable name field for champname helpers.
func championName(c *SplashChampion) string { return c.ChampionName }

func (s *state) pickRandomChampion() *SplashChampion {
	return &s.pool[rand.Intn(len(s.pool))]
}

func pickRandomSkin(c *SplashChampion) *Skin {
	return &c.Skins[rand.Intn(len(c.Skins))]
}

func (s *state) startFreshGame(ctx context.Context, subject string) (*gameState, error) {
	target := s.pickRandomChampion()
	skin := pickRandomSkin(target)
	g := &gameState{
		Target:    target.ChampionName,
		SkinID:    skin.ID,
		Guesses:   []string{},
		StartedAt: nil,
	}
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

// caption is the photo caption shown above each round-in-progress splash.
func caption(guesses, maxGuesses int) string {
	return fmt.Sprintf("🎨 Guess the champion from this splash art. %d/%d guesses so far.", guesses, maxGuesses)
}

// sendSplash dispatches sendPhoto with the splash URL. Returns the bot
// library's error verbatim — caller decides whether to log/ignore.
func sendSplash(ctx context.Context, b *bot.Bot, chatID int64, skin *Skin, captionText string) error {
	_, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileString{Data: skin.URL},
		Caption: captionText,
	})
	return err
}

// handleSplash is /loldle_splash [champion] — show splash if no arg, else guess.
func (s *state) handleSplash(ctx context.Context, b *bot.Bot, update *models.Update) error {
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
	var skin *Skin
	if target != nil {
		skin = skinByID(target, game.SkinID)
	}
	if target == nil || skin == nil {
		// Pool was refreshed mid-round and the skin is gone — drop the round.
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"Splash data was updated since this round started. "+newRoundHint)
	}

	if arg == "" {
		return sendSplash(ctx, b, msg.Chat.ID, skin, caption(len(game.Guesses), maxGuesses))
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
	skinLabel := fmt.Sprintf("<i>%s</i> skin", html.EscapeString(skin.Name))

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
			"🎉 Got it! That was <b>%s</b> in %s. Solved in %d/%d\n🔥 Streak: %d\n%s",
			answer, skinLabel, len(game.Guesses), maxGuesses, st.Streak, newRoundHint))

	case len(game.Guesses) >= maxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"❌ Out of guesses. Answer was <b>%s</b> in %s.\n%s",
			answer, skinLabel, newRoundHint))

	default:
		if err := saveGame(ctx, s.kv, subject, game); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"❌ Not <b>%s</b>. Guess %d/%d.",
			html.EscapeString(guess.ChampionName), len(game.Guesses), maxGuesses))
	}
}

// handleGiveup is /loldle_splash_giveup — reveal answer + clear round.
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
	target := champname.FindByExactName(s.pool, existing.Target, championName)
	var label string
	if target != nil {
		if sk := skinByID(target, existing.SkinID); sk != nil {
			label = fmt.Sprintf(" in <i>%s</i> skin", html.EscapeString(sk.Name))
		}
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"🏳️ Answer was <b>%s</b>%s.\n%s",
		html.EscapeString(existing.Target), label, newRoundHint))
}

// handleStats is /loldle_splash_stats — lifetime score.
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
		"📊 Loldle Splash %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, st.Played, st.Wins, chathelper.WinRate(st.Wins, st.Played), st.Streak, st.BestStreak))
}

// handleSetMax is /loldle_splash_setmax <n> — private; per-subject override.
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
		return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /loldle_splash_setmax <1-%d>", MaxGuessesCap))
	}
	if err := setMaxGuesses(ctx, s.kv, subject, n); err != nil {
		return err
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("✅ Loldle splash max guesses set to %d (applies to the next round).", n))
}
