package doantu

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/keylock"
	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/modules/util/chathelper"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const upstreamFail = "⚠️ Upstream hiccup — try again in a few seconds."

// JS-parity rank band: keep targets in the top-frequency band so the game
// stays guessable.
var randomFilters = map[string]string{"min_rank": "100", "max_rank": "1000"}

type state struct {
	kv     storage.KVStore
	api    SimAPI
	rngMu  sync.Mutex
	rng    *rand.Rand
	locks  keylock.Map
}

func (s *state) startFresh(ctx context.Context, subject string) (*GameState, error) {
	picked, err := s.api.RandomWord(ctx, randomFilters)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(picked.Word)
	if target == "" {
		return nil, &UpstreamError{Msg: "empty target from RandomWord"}
	}
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

func (s *state) handleDoantu(ctx context.Context, b *bot.Bot, update *models.Update) error {
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
	game, err := s.getOrInit(ctx, subject)
	if err != nil {
		log.Warn("doantu random failed", "err", err)
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	if arg == "" {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, renderBoard(game.Guesses, ""))
	}
	return s.submitGuess(ctx, b, msg, subject, game, arg)
}

func (s *state) submitGuess(ctx context.Context, b *bot.Bot, msg *models.Message, subject string, game *GameState, arg string) error {
	guess := normalize(arg)
	if !isValidShape(guess) {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Please provide a Vietnamese word (letters + optional single spaces).")
	}
	for _, g := range game.Guesses {
		if g.Word == guess || g.Canonical == guess {
			return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
				fmt.Sprintf("🔁 <b>%s</b> was already guessed this round — try another word.",
					html.EscapeString(guess)))
		}
	}

	res, err := s.api.Similarity(ctx, game.Target, guess)
	if err != nil {
		log.Warn("doantu similarity failed", "err", err)
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	// Target OOV — JS-parity reset behaviour. The recorded round was seeded
	// pre-vocab-change; let player start fresh instead of fighting a ghost.
	if !res.InVocabA {
		log.Warn("doantu target OOV", "target", game.Target)
		_ = clearGame(ctx, s.kv, subject)
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"⚠️ This round's target is no longer valid (upstream vocabulary changed). "+
				"Send <code>/doantu</code> again to start a fresh round.")
	}
	if !res.InVocabB || res.Similarity == nil {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			fmt.Sprintf("🤔 <code>%s</code> isn't in the vocabulary.", html.EscapeString(guess)))
	}
	canonical := guess
	if res.CanonicalB != nil && *res.CanonicalB != "" {
		canonical = strings.ToLower(*res.CanonicalB)
	}

	for _, g := range game.Guesses {
		if g.Canonical == canonical {
			return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
				fmt.Sprintf("🔁 <b>%s</b> was already guessed this round — try another word.",
					html.EscapeString(canonical)))
		}
	}

	entry := Guess{Word: guess, Canonical: canonical, Similarity: *res.Similarity}
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

// playableWord matches lowercase Unicode letter / mark / underscore tokens.
// Used to filter neighbor responses that include foreign place names.
var playableWord = regexp.MustCompile(`^[\p{Ll}\p{M}_]+$`)

func looksVietnamese(word string) bool {
	if strings.Contains(word, "_") {
		return true
	}
	for _, r := range word {
		if r > 0x7f {
			return true
		}
	}
	return false
}

func (s *state) pickHintWords(target string, neighbors []Neighbor, alreadyGuessed []string, count int) []Neighbor {
	guessedSet := make(map[string]struct{}, len(alreadyGuessed))
	for _, g := range alreadyGuessed {
		guessedSet[g] = struct{}{}
	}
	var playable []Neighbor
	for _, n := range neighbors {
		if !playableWord.MatchString(n.Word) || !looksVietnamese(n.Word) {
			continue
		}
		if strings.Contains(n.Word, target) || strings.Contains(target, n.Word) {
			continue
		}
		if _, dup := guessedSet[n.Word]; dup {
			continue
		}
		playable = append(playable, n)
	}
	// Skip top 20% so hints stay "warm but not hot" (JS-parity).
	skip := len(playable) / 5
	if skip > 20 {
		skip = 20
	}
	if skip >= len(playable) {
		return nil
	}
	pool := playable[skip:]
	want := count
	if want > len(pool) {
		want = len(pool)
	}
	if want <= 0 {
		return nil
	}
	// Reservoir-style sample without replacement.
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	idx := s.rng.Perm(len(pool))[:want]
	out := make([]Neighbor, want)
	for i, j := range idx {
		out[i] = pool[j]
	}
	return out
}

func (s *state) handleHint(ctx context.Context, b *bot.Bot, update *models.Update) error {
	msg := update.Message
	if msg == nil {
		return nil
	}
	subject := chathelper.SubjectFor(msg)
	if subject == "" {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "Cannot identify chat.")
	}
	game, err := loadGame(ctx, s.kv, subject)
	if err != nil {
		return err
	}
	if game == nil || game.Solved {
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"No active round. Send <code>/doantu</code> to start one.")
	}
	res, err := s.api.Neighbors(ctx, game.Target, 100)
	if err != nil {
		log.Warn("doantu neighbors failed", "err", err)
		return chathelper.Reply(ctx, b, msg.Chat.ID, upstreamFail)
	}
	already := make([]string, 0, len(game.Guesses))
	for _, g := range game.Guesses {
		already = append(already, g.Canonical)
	}
	picks := s.pickHintWords(game.Target, res.Neighbors, already, 3)
	if len(picks) == 0 {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "🤷 No usable hints available for this round.")
	}
	var lines []string
	for _, p := range picks {
		lines = append(lines, fmt.Sprintf("• <code>%s</code>", html.EscapeString(p.Word)))
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
		"💡 <b>Hints</b> — related words (not the answer):\n"+strings.Join(lines, "\n"))
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
		return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
			"No active round. Send <code>/doantu</code> to start one.")
	}
	if _, err := recordResult(ctx, s.kv, subject, false, len(game.Guesses), chathelper.NowMillis()); err != nil {
		return err
	}
	if err := clearGame(ctx, s.kv, subject); err != nil {
		return err
	}
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID,
		fmt.Sprintf("🏳️ The target was <b>%s</b>. Send <code>/doantu</code> for a new round.",
			html.EscapeString(game.Target)))
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
	if st.Played == 0 {
		return chathelper.Reply(ctx, b, msg.Chat.ID, "No doantu games played yet.")
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
		"🇻🇳 <b>Đoán từ stats</b>\nPlayed: %d\nSolved: %d (%d%%)\nTotal guesses: %d\nFewest to solve: %s\nAvg per round: %s",
		st.Played, st.Solved, solveRate, st.TotalGuesses, best, avg,
	)
	return chathelper.ReplyHTML(ctx, b, msg.Chat.ID, body)
}

// roundDiv rounds (a/b) half-away-from-zero (JS Math.round parity for non-neg).
func roundDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a*2 + b) / (2 * b)
}

// asUpstream is a helper for tests that want to assert specific error types
// without leaking internals. Currently unused outside the package.
var _ = errors.As
