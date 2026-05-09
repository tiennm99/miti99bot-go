package semantle

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/ai"
	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const (
	upstreamFail = "⚠️ Upstream hiccup — try again in a few seconds."
	notConfig    = "⚠️ Semantle is not configured (missing GEMINI_API_KEY)."
	rateLimited  = "⚠️ AI is rate-limited. Try again in a minute."
)

// state is what every handler captures. Loaded once in New.
type state struct {
	kv       storage.KVStore
	embedder ai.Embedder
	limiter  *ai.PerUserLimiter
	words    []string
	vocab    map[string]struct{}

	rngMu sync.Mutex
	rng   *rand.Rand // overridable by tests via newWithRNG; defaults to crypto-seeded

	locks keylock.Map
}

// pickTarget returns a random word from the pool. Lock-protected so
// concurrent handlers see deterministic behavior under a fixed-seed test.
func (s *state) pickTarget() string {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return s.words[s.rng.IntN(len(s.words))]
}

func (s *state) startFresh(ctx context.Context, subject string) (*GameState, error) {
	target := s.pickTarget()
	g := &GameState{Target: target, StartedAt: nil, Solved: false, Guesses: []Guess{}}
	if err := saveGame(ctx, s.kv, subject, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *state) getOrInit(ctx context.Context, subject string) (*GameState, error) {
	existing, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return nil, err
	}
	if existing != nil && !existing.Solved {
		return existing, nil
	}
	return s.startFresh(ctx, subject)
}

// handleSemantle: /semantle [word] — show board if no arg, else submit guess.
func (s *state) handleSemantle(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	if s.embedder == nil {
		return chathelper.Reply(ctx, b, msg.Chat.ID, notConfig)
	}
	defer s.locks.Acquire(subject)()

	arg := chathelper.ArgAfterCommand(msg.Text)
	game, err := s.getOrInit(ctx, subject)
	if err != nil {
		return err
	}
	if arg == "" {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, renderBoard(game.Guesses, ""))
	}
	return s.submitGuess(ctx, b, msg, subject, game, arg)
}

func (s *state) submitGuess(ctx context.Context, b *bot.Bot, msg *models.Message, subject string, game *GameState, arg string) error {
	guess := normalize(arg)
	if !isValidShape(guess) {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Please provide a single letter-only word.")
	}
	// Fast-path dedup: same raw or same canonical → no upstream call.
	for _, g := range game.Guesses {
		if g.Word == guess || g.Canonical == guess {
			return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
				fmt.Sprintf("🔁 <b>%s</b> was already guessed this round — try another word.",
					html.EscapeString(guess)))
		}
	}
	// OOV cheap-check before spending a Gemini call.
	if _, ok := s.vocab[guess]; !ok {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			fmt.Sprintf("🤔 <code>%s</code> isn't in the vocabulary.", html.EscapeString(guess)))
	}
	// Per-user rate limit. Bucket key = subject so DM and group are scoped.
	if s.limiter != nil && !s.limiter.Allow(subject) {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "⏳ Slow down — too many guesses in a short window.")
	}

	vecs, err := s.embedder.Embed(ctx, []string{game.Target, guess})
	if err != nil {
		if errors.Is(err, ai.ErrRateLimited) {
			return chathelper.Reply(ctx, b, msg.Chat.ID, rateLimited)
		}
		log.Warn("semantle embed failed", "err", err)
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	if len(vecs) != 2 {
		log.Warn("semantle embed: bad vec count", "got", len(vecs))
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	sim, ok := cosine(vecs[0], vecs[1])
	if !ok {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			fmt.Sprintf("🤔 <code>%s</code> isn't in the vocabulary.", html.EscapeString(guess)))
	}

	entry := Guess{Word: guess, Canonical: guess, Similarity: sim}
	game.Guesses = append(game.Guesses, entry)
	if game.StartedAt == nil {
		now := chathelper.NowMillis()
		game.StartedAt = &now
	}

	if entry.Canonical == game.Target {
		game.Solved = true
		count := len(game.Guesses)
		if _, err := recordResult(ctx, s.kv, subject, true, count, chathelper.NowMillis()); err != nil {
			return err
		}
		if err := clearGame(ctx, s.kv, subject); err != nil {
			return err
		}
		board := renderBoard(game.Guesses, entry.Canonical)
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			fmt.Sprintf("%s\n✅ Solved in %d guess%s!", board, count, plural(count)))
	}

	if err := saveGame(ctx, s.kv, subject, game); err != nil {
		return err
	}
	body := renderGuess(entry) + "\n" + renderBoard(game.Guesses, entry.Canonical)
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, body)
}

// handleGiveup: /semantle_giveup — reveal target + end round + record loss.
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
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"No active round. Send <code>/semantle</code> to start one.")
	}
	if _, err := recordResult(ctx, s.kv, subject, false, len(game.Guesses), chathelper.NowMillis()); err != nil {
		return err
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
		fmt.Sprintf("🏳️ The target was <b>%s</b>. Send <code>/semantle</code> for a new round.",
			html.EscapeString(game.Target)))
}

// handleStats: /semantle_stats — lifetime score.
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
	if st.Played == 0 {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "No semantle games played yet.")
	}
	solveRate := chathelper.WinRate(st.Solved, st.Played)
	avg := "—"
	if st.Played > 0 {
		avg = fmt.Sprintf("%d", roundDiv(st.TotalGuesses, st.Played))
	}
	best := "—"
	if st.BestGuessCount != nil {
		best = fmt.Sprintf("%d", *st.BestGuessCount)
	}
	body := fmt.Sprintf(
		"🎯 <b>Semantle stats</b>\nPlayed: %d\nSolved: %d (%d%%)\nTotal guesses: %d\nFewest to solve: %s\nAvg per round: %s",
		st.Played, st.Solved, solveRate, st.TotalGuesses, best, avg,
	)
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, body)
}

// roundDiv rounds (a/b) half-away-from-zero, JS Math.round parity for non-negative inputs.
func roundDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a*2 + b) / (2 * b)
}
