package loldleability

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// Round-length defaults. Mirror JS: 5 default, capped at 10 via
// /loldle_ability_setmax.
const (
	MaxGuesses    = 5
	MaxGuessesCap = 10
)

// gameState differs from emoji/quote: it locks the chosen ability slot at
// round start so the SAME icon shows across every turn until the round ends.
// Field tags match JS.
type gameState struct {
	Target    string   `json:"target"`
	Slot      string   `json:"slot"` // ability slot — P, Q, W, E, R
	Guesses   []string `json:"guesses"`
	StartedAt *int64   `json:"startedAt"`
}

type stats struct {
	Played     int `json:"played"`
	Wins       int `json:"wins"`
	Streak     int `json:"streak"`
	BestStreak int `json:"bestStreak"`
}

type roundConfig struct {
	MaxGuesses int `json:"maxGuesses"`
}

func gameKey(subject string) string   { return "game:" + subject }
func statsKey(subject string) string  { return "stats:" + subject }
func configKey(subject string) string { return "config:" + subject }

func loadGame(ctx context.Context, kv storage.KVStore, subject string) (*gameState, error) {
	var g gameState
	err := kv.GetJSON(ctx, gameKey(subject), &g)
	switch {
	case err == nil:
		return &g, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("loldleability loadGame: %w", err)
	}
}

func saveGame(ctx context.Context, kv storage.KVStore, subject string, g *gameState) error {
	if err := kv.PutJSON(ctx, gameKey(subject), g); err != nil {
		return fmt.Errorf("loldleability saveGame: %w", err)
	}
	return nil
}

func clearGame(ctx context.Context, kv storage.KVStore, subject string) error {
	if err := kv.Delete(ctx, gameKey(subject)); err != nil {
		return fmt.Errorf("loldleability clearGame: %w", err)
	}
	return nil
}

func loadStats(ctx context.Context, kv storage.KVStore, subject string) (*stats, error) {
	var s stats
	err := kv.GetJSON(ctx, statsKey(subject), &s)
	switch {
	case err == nil:
		return &s, nil
	case errors.Is(err, storage.ErrNotFound):
		return &stats{}, nil
	default:
		return nil, fmt.Errorf("loldleability loadStats: %w", err)
	}
}

func recordResult(ctx context.Context, kv storage.KVStore, subject string, won bool) (*stats, error) {
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
	if err := kv.PutJSON(ctx, statsKey(subject), s); err != nil {
		return nil, fmt.Errorf("loldleability recordResult: %w", err)
	}
	return s, nil
}

func getMaxGuesses(ctx context.Context, kv storage.KVStore, subject string) (int, error) {
	var cfg roundConfig
	err := kv.GetJSON(ctx, configKey(subject), &cfg)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MaxGuesses, nil
		}
		return 0, fmt.Errorf("loldleability getMaxGuesses: %w", err)
	}
	if cfg.MaxGuesses < 1 || cfg.MaxGuesses > MaxGuessesCap {
		return MaxGuesses, nil
	}
	return cfg.MaxGuesses, nil
}

func setMaxGuesses(ctx context.Context, kv storage.KVStore, subject string, n int) error {
	if n < 1 || n > MaxGuessesCap {
		return fmt.Errorf("loldleability: maxGuesses must be in [1, %d], got %d", MaxGuessesCap, n)
	}
	if err := kv.PutJSON(ctx, configKey(subject), roundConfig{MaxGuesses: n}); err != nil {
		return fmt.Errorf("loldleability setMaxGuesses: %w", err)
	}
	return nil
}
