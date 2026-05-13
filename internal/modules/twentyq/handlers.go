package twentyq

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot/internal/ai"
	"github.com/tiennm99/miti99bot/internal/keylock"
	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot/internal/storage"
)

const (
	upstreamFail = "⚠️ AI service hiccup — try again in a few seconds."
	notConfig    = "⚠️ Twentyq is not configured (missing GEMINI_API_KEY)."
	rateLimited  = "⚠️ AI is rate-limited. Try again in a minute."
	noRound      = "No active round. Send <code>/twentyq</code> to start one."
)

type state struct {
	kv      storage.KVStore
	chatter ai.Chatter
	limiter *ai.PerUserLimiter

	rngMu sync.Mutex
	rng   *rand.Rand

	locks keylock.Map
}

func (s *state) randomSeed() string {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return seeds[s.rng.IntN(len(seeds))]
}

// fallbackRoundStart matches JS roundstart fallback when the model fails.
func fallbackRoundStart() (string, string) {
	return "object", "it is something you might encounter in everyday life"
}

func (s *state) startFreshGame(ctx context.Context) (*GameState, error) {
	target := s.randomSeed()
	prompt := buildStartRoundPrompt(target)
	resp, err := s.chatter.Generate(ctx, prompt, "begin")
	if err != nil {
		return nil, err
	}
	cat, hint, ok := parseRoundStart(parseJSON(resp))
	if !ok {
		log.Warn("twentyq roundstart unparseable", "preview", truncate(resp, 200))
		cat, hint = fallbackRoundStart()
	}
	hint = redactSecret(hint, target)
	now := chathelper.NowMillis()
	return &GameState{
		Category:    cat,
		Target:      target,
		InitialHint: hint,
		StartedAt:   &now,
		Solved:      false,
		Turns:       []Turn{},
	}, nil
}

func (s *state) handleTwentyq(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	if s.chatter == nil {
		return chathelper.Reply(ctx, b, msg.Chat.ID, notConfig)
	}
	defer s.locks.Acquire(subject)()

	arg := chathelper.ArgAfterCommand(msg.Text)

	game, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	// Solved-but-lingering rounds → start fresh transparently (JS-parity).
	if game != nil && game.Solved {
		_ = clearGame(ctx, s.kv, subject)
		game = nil
	}

	if game == nil {
		if s.limiter != nil && !s.limiter.Allow(subject) {
			return chathelper.Reply(ctx, b, msg.Chat.ID, "⏳ Slow down — too many requests in a short window.")
		}
		fresh, err := s.startFreshGame(ctx)
		if err != nil {
			if errors.Is(err, ai.ErrRateLimited) {
				return chathelper.Reply(ctx, b, msg.Chat.ID, rateLimited)
			}
			log.Warn("twentyq roundstart failed", "err", err)
			return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
		}
		if err := saveGame(ctx, s.kv, subject, fresh); err != nil {
			return err
		}
		if arg == "" {
			return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, formatIntro(*fresh))
		}
		// Fresh round + immediate question — show intro then process turn.
		if err := chathelper.ReplyHTML(ctx, b, msg.Chat.ID, formatIntro(*fresh)); err != nil {
			return err
		}
		return s.submitTurn(ctx, b, msg, subject, fresh, arg)
	}

	if arg == "" {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, formatBoard(*game))
	}
	return s.submitTurn(ctx, b, msg, subject, game, arg)
}

func (s *state) submitTurn(ctx context.Context, b *bot.Bot, msg *models.Message, subject string, game *GameState, raw string) error {
	v := validateQuestion(raw)
	if !v.OK {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, v.Reason)
	}
	lower := strings.ToLower(v.Normalized)
	for _, t := range game.Turns {
		if strings.ToLower(t.Text) == lower {
			return chathelper.Reply(ctx, b, msg.Chat.ID,
				"🔁 You already asked that exact question — try a new angle.")
		}
	}

	if s.limiter != nil && !s.limiter.Allow(subject) {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "⏳ Slow down — too many turns in a short window.")
	}

	system := buildSystemPrompt(*game)
	resp, err := s.chatter.Generate(ctx, system, v.Normalized)
	if err != nil {
		if errors.Is(err, ai.ErrRateLimited) {
			return chathelper.Reply(ctx, b, msg.Chat.ID, rateLimited)
		}
		log.Warn("twentyq judge failed", "err", err)
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	payload := parseJSON(resp)
	if payload == nil {
		log.Warn("twentyq judge unparseable", "preview", truncate(resp, 200))
	}
	jud := normalizeJudgement(payload)
	jud.Hint = redactSecret(jud.Hint, game.Target)

	turn := Turn{
		Text:    v.Normalized,
		IsGuess: jud.IsGuess,
		Answer:  jud.Answer,
		Hint:    jud.Hint,
		TS:      chathelper.NowMillis(),
	}
	game.Turns = append(game.Turns, turn)

	won := turn.IsGuess && turn.Answer == "yes"
	if won {
		game.Solved = true
		count := len(game.Turns)
		if _, err := recordResult(ctx, s.kv, subject, true, count, chathelper.NowMillis()); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			formatTurnReply(turn, true, game.Target, count))
	}

	if err := saveGame(ctx, s.kv, subject, game); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
		formatTurnReply(turn, false, game.Target, len(game.Turns)))
}

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
	game, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	if game == nil {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, noRound)
	}
	if _, err := recordResult(ctx, s.kv, subject, false, len(game.Turns), chathelper.NowMillis()); err != nil {
		return err
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, formatGiveup(*game))
}

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
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, formatStats(*st))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return fmt.Sprintf("%s…", s[:n])
}
