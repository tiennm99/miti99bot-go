package wordle

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// MaxGuesses is the standard wordle round length.
const MaxGuesses = 6

// Cloud Firestore has no native per-document TTL equivalent to Cloudflare KV
// — saved games linger until manually cleaned. Out of scope today; tracked
// in port plan as a future cron.

// GuessRecord is one entry in a game's history. JSON shape locks JS parity:
//
//	{ "word": "crane", "results": [{"letter":"c","result":"correct"}, ...] }
type GuessRecord struct {
	Word    string        `json:"word"`
	Results []LetterScore `json:"results"`
}

// GameState is the per-subject KV record for an in-progress (or finished)
// round. Field tags match JS exactly so a JS-written round decodes cleanly.
//
// `giveup` is always emitted (initialized to false on /wordle_new). Do NOT
// add omitempty — the JS source serializes the field unconditionally and
// cross-runtime migration depends on shape parity.
type GameState struct {
	Target    string        `json:"target"`
	Guesses   []GuessRecord `json:"guesses"`
	Solved    bool          `json:"solved"`
	Giveup    bool          `json:"giveup"`
	StartedAt int64         `json:"startedAt"` // ms-since-epoch (Date.now())
}

// Stats is the lifetime score record. lastResultAt is *int64 so an unplayed
// account marshals as `"lastResultAt": null` matching JS's initial shape.
type Stats struct {
	Played       int    `json:"played"`
	Wins         int    `json:"wins"`
	Streak       int    `json:"streak"`
	BestStreak   int    `json:"bestStreak"`
	LastResultAt *int64 `json:"lastResultAt"` // ms-since-epoch | null
}

func gameKey(subject string) string  { return "game:" + subject }
func statsKey(subject string) string { return "stats:" + subject }

// loadGame returns the active round, or (nil, nil) if none exists.
func loadGame(ctx context.Context, kv storage.KVStore, subject string) (*GameState, error) {
	var g GameState
	err := kv.GetJSON(ctx, gameKey(subject), &g)
	switch {
	case err == nil:
		return &g, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("wordle loadGame: %w", err)
	}
}

// saveGame writes the round.
func saveGame(ctx context.Context, kv storage.KVStore, subject string, g *GameState) error {
	if err := kv.PutJSON(ctx, gameKey(subject), g); err != nil {
		return fmt.Errorf("wordle saveGame: %w", err)
	}
	return nil
}

// loadStats returns lifetime stats; missing → fresh-zero record (with
// LastResultAt=nil), matching the JS `?? {…}` fallback.
func loadStats(ctx context.Context, kv storage.KVStore, subject string) (*Stats, error) {
	var s Stats
	err := kv.GetJSON(ctx, statsKey(subject), &s)
	switch {
	case err == nil:
		return &s, nil
	case errors.Is(err, storage.ErrNotFound):
		return &Stats{}, nil
	default:
		return nil, fmt.Errorf("wordle loadStats: %w", err)
	}
}

// recordResult bumps stats with the round outcome (won true → win + streak,
// false → reset streak). Returns the updated stats so callers can show the
// new streak in the win message.
func recordResult(ctx context.Context, kv storage.KVStore, subject string, won bool, nowMillis int64) (*Stats, error) {
	s, err := loadStats(ctx, kv, subject)
	if err != nil {
		return nil, err
	}
	s.Played++
	if won {
		s.Wins++
		s.Streak++
		if s.Streak > s.BestStreak {
			s.BestStreak = s.Streak
		}
	} else {
		s.Streak = 0
	}
	now := nowMillis
	s.LastResultAt = &now
	if err := kv.PutJSON(ctx, statsKey(subject), s); err != nil {
		return nil, fmt.Errorf("wordle recordResult: %w", err)
	}
	return s, nil
}

// isFinished is true when the round can no longer accept guesses: solved,
// gave up, or out of guesses.
func isFinished(g *GameState) bool {
	return g.Solved || g.Giveup || len(g.Guesses) >= MaxGuesses
}
