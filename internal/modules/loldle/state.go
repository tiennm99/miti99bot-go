package loldle

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

// MaxGuesses is the default round length. Per-subject overrides land via
// /loldle_setmax (capped by MaxGuessesCap).
const (
	MaxGuesses    = 8
	MaxGuessesCap = 10
)

// gameState is the per-subject KV record. Field tags match JS exactly so a
// JS-written round decodes cleanly. StartedAt is *int64 because the JS
// source initialises it to `null` (timer doesn't start until first guess) —
// using time.Time would marshal as "0001-01-01T00:00:00Z" instead of null
// and break wire-format parity.
//
// Guesses is just championNames; comparison rows are recomputed at render
// time against current champions.json so a weekly data refresh updates
// historical board displays without migrating saved rounds.
type gameState struct {
	Target    string   `json:"target"`
	Guesses   []string `json:"guesses"`
	StartedAt *int64   `json:"startedAt"` // ms-since-epoch | null
}

// stats lifetime score. JS shape — note no LastResultAt field (differs from
// wordle's stats; the JS loldle source omits it, parity dictates we do too).
type stats struct {
	Played     int `json:"played"`
	Wins       int `json:"wins"`
	Streak     int `json:"streak"`
	BestStreak int `json:"bestStreak"`
}

// roundConfig stores the per-subject MaxGuesses override. Stored only when
// /loldle_setmax has been run; the absence of this record means "use default".
type roundConfig struct {
	MaxGuesses int `json:"maxGuesses"`
}

func gameKey(subject string) string   { return "game:" + subject }
func statsKey(subject string) string  { return "stats:" + subject }
func configKey(subject string) string { return "config:" + subject }

// loadGame returns the active round, or (nil, nil) if none exists.
func loadGame(ctx context.Context, kv storage.KVStore, subject string) (*gameState, error) {
	var g gameState
	err := kv.GetJSON(ctx, gameKey(subject), &g)
	switch {
	case err == nil:
		return &g, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("loldle loadGame: %w", err)
	}
}

func saveGame(ctx context.Context, kv storage.KVStore, subject string, g *gameState) error {
	if err := kv.PutJSON(ctx, gameKey(subject), g); err != nil {
		return fmt.Errorf("loldle saveGame: %w", err)
	}
	return nil
}

// clearGame removes the round so the next /loldle starts fresh. Used after
// win / loss / giveup; the new round's timer should start on the player's
// next interaction, not at the moment the previous round ended.
func clearGame(ctx context.Context, kv storage.KVStore, subject string) error {
	if err := kv.Delete(ctx, gameKey(subject)); err != nil {
		return fmt.Errorf("loldle clearGame: %w", err)
	}
	return nil
}

// loadStats returns lifetime score; missing → fresh-zero record (matches
// the JS `?? {…}` fallback).
func loadStats(ctx context.Context, kv storage.KVStore, subject string) (*stats, error) {
	var s stats
	err := kv.GetJSON(ctx, statsKey(subject), &s)
	switch {
	case err == nil:
		return &s, nil
	case errors.Is(err, storage.ErrNotFound):
		return &stats{}, nil
	default:
		return nil, fmt.Errorf("loldle loadStats: %w", err)
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
		return nil, fmt.Errorf("loldle recordResult: %w", err)
	}
	return s, nil
}

// getMaxGuesses returns the effective round length: the per-subject override
// if set and in range, otherwise MaxGuesses. Out-of-range values are
// silently ignored (matches JS).
func getMaxGuesses(ctx context.Context, kv storage.KVStore, subject string) (int, error) {
	var cfg roundConfig
	err := kv.GetJSON(ctx, configKey(subject), &cfg)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return MaxGuesses, nil
		}
		return 0, fmt.Errorf("loldle getMaxGuesses: %w", err)
	}
	if cfg.MaxGuesses < 1 || cfg.MaxGuesses > MaxGuessesCap {
		return MaxGuesses, nil
	}
	return cfg.MaxGuesses, nil
}

// setMaxGuesses validates and persists the per-subject override.
func setMaxGuesses(ctx context.Context, kv storage.KVStore, subject string, n int) error {
	if n < 1 || n > MaxGuessesCap {
		return fmt.Errorf("loldle: maxGuesses must be in [1, %d], got %d", MaxGuessesCap, n)
	}
	if err := kv.PutJSON(ctx, configKey(subject), roundConfig{MaxGuesses: n}); err != nil {
		return fmt.Errorf("loldle setMaxGuesses: %w", err)
	}
	return nil
}
