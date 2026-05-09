package loldleability

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

const newRoundHint = "🆕 Send <code>/loldle_ability</code> or <code>/loldle_ability &lt;champion&gt;</code> to start a new round."

// state captures everything a loldle-ability handler needs at runtime.
// Built once per Factory call and shared across the four command closures.
type state struct {
	kv    storage.KVStore
	pool  []AbilityChampion
	locks keylock.Map // serialises Get→mutate→Put per subject
}

// championName extracts the comparable name field for champname helpers.
func championName(c *AbilityChampion) string { return c.ChampionName }

func (s *state) pickRandomChampion() *AbilityChampion {
	return &s.pool[rand.Intn(len(s.pool))]
}

func pickRandomAbility(c *AbilityChampion) *Ability {
	return &c.Abilities[rand.Intn(len(c.Abilities))]
}

func (s *state) startFreshGame(ctx context.Context, subject string) (*gameState, error) {
	target := s.pickRandomChampion()
	ability := pickRandomAbility(target)
	g := &gameState{
		Target:    target.ChampionName,
		Slot:      ability.Slot,
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

// caption is the photo caption shown above each round-in-progress icon.
func caption(guesses, maxGuesses int) string {
	return fmt.Sprintf("🔮 Guess the champion from this ability. %d/%d guesses so far.", guesses, maxGuesses)
}

// sendAbilityIcon dispatches a sendPhoto with the ability icon URL. Returns
// the bot library's error verbatim — caller decides whether to log/ignore.
func sendAbilityIcon(ctx context.Context, b *bot.Bot, chatID int64, ability *Ability, captionText string) error {
	_, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileString{Data: ability.Icon},
		Caption: captionText,
	})
	return err
}

// handleAbility is /loldle_ability [champion] — show icon if no arg, else guess.
func (s *state) handleAbility(ctx context.Context, b *bot.Bot, update *models.Update) error {
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
	var ability *Ability
	if target != nil {
		ability = abilityBySlot(target, game.Slot)
	}
	if target == nil || ability == nil {
		// Pool was refreshed mid-round and the slot is gone — drop the round.
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"Ability data was updated since this round started. "+newRoundHint)
	}

	if arg == "" {
		return sendAbilityIcon(ctx, b, msg.Chat.ID, ability, caption(len(game.Guesses), maxGuesses))
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
	abilityLabel := fmt.Sprintf("<i>%s</i> (%s)", html.EscapeString(ability.Name), ability.Slot)

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
			"🎉 Got it! That was <b>%s</b> — %s. Solved in %d/%d\n🔥 Streak: %d\n%s",
			answer, abilityLabel, len(game.Guesses), maxGuesses, st.Streak, newRoundHint))

	case len(game.Guesses) >= maxGuesses:
		if _, err := recordResult(ctx, s.kv, subject, false); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"❌ Out of guesses. Answer was <b>%s</b> — %s.\n%s",
			answer, abilityLabel, newRoundHint))

	default:
		if err := saveGame(ctx, s.kv, subject, game); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
			"❌ Not <b>%s</b>. Guess %d/%d.",
			html.EscapeString(guess.ChampionName), len(game.Guesses), maxGuesses))
	}
}

// handleGiveup is /loldle_ability_giveup — reveal answer + clear round.
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
		if a := abilityBySlot(target, existing.Slot); a != nil {
			label = fmt.Sprintf(" — <i>%s</i> (%s)", html.EscapeString(a.Name), a.Slot)
		}
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, fmt.Sprintf(
		"🏳️ Answer was <b>%s</b>%s.\n%s",
		html.EscapeString(existing.Target), label, newRoundHint))
}

// handleStats is /loldle_ability_stats — lifetime score.
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
		"📊 Loldle Ability %s stats\nPlayed: %d\nWins: %d (%d%%)\nCurrent streak: %d\nBest streak: %d",
		scope, st.Played, st.Wins, chathelper.WinRate(st.Wins, st.Played), st.Streak, st.BestStreak))
}

// handleSetMax is /loldle_ability_setmax <n> — private; per-subject override.
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
		return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("Usage: /loldle_ability_setmax <1-%d>", MaxGuessesCap))
	}
	if err := setMaxGuesses(ctx, s.kv, subject, n); err != nil {
		return err
	}
	return chathelper.Reply(ctx, b, msg.Chat.ID, fmt.Sprintf("✅ Loldle ability max guesses set to %d (applies to the next round).", n))
}
