package doantu

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot-go/internal/storage"
)

type Guess struct {
	Word       string  `json:"word"`
	Canonical  string  `json:"canonical"`
	Similarity float64 `json:"similarity"`
}

type GameState struct {
	Target    string  `json:"target"`
	StartedAt *int64  `json:"startedAt"`
	Solved    bool    `json:"solved"`
	Guesses   []Guess `json:"guesses"`
}

type Stats struct {
	Played         int    `json:"played"`
	Solved         int    `json:"solved"`
	TotalGuesses   int    `json:"totalGuesses"`
	BestGuessCount *int   `json:"bestGuessCount"`
	LastResultAt   *int64 `json:"lastResultAt"`
}

func gameKey(subject string) string  { return "game:" + subject }
func statsKey(subject string) string { return "stats:" + subject }

func loadGame(ctx context.Context, kv storage.KVStore, subject string) (*GameState, error) {
	var g GameState
	err := kv.GetJSON(ctx, gameKey(subject), &g)
	switch {
	case err == nil:
		return &g, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("doantu loadGame: %w", err)
	}
}

func saveGame(ctx context.Context, kv storage.KVStore, subject string, g *GameState) error {
	if err := kv.PutJSON(ctx, gameKey(subject), g); err != nil {
		return fmt.Errorf("doantu saveGame: %w", err)
	}
	return nil
}

func clearGame(ctx context.Context, kv storage.KVStore, subject string) error {
	if err := kv.Delete(ctx, gameKey(subject)); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("doantu clearGame: %w", err)
	}
	return nil
}

func loadStats(ctx context.Context, kv storage.KVStore, subject string) (*Stats, error) {
	var s Stats
	err := kv.GetJSON(ctx, statsKey(subject), &s)
	switch {
	case err == nil:
		return &s, nil
	case errors.Is(err, storage.ErrNotFound):
		return &Stats{}, nil
	default:
		return nil, fmt.Errorf("doantu loadStats: %w", err)
	}
}

func recordResult(ctx context.Context, kv storage.KVStore, subject string, solved bool, guessCount int, nowMillis int64) (*Stats, error) {
	s, err := loadStats(ctx, kv, subject)
	if err != nil {
		return nil, err
	}
	s.Played++
	s.TotalGuesses += guessCount
	if solved {
		s.Solved++
		if s.BestGuessCount == nil || guessCount < *s.BestGuessCount {
			gc := guessCount
			s.BestGuessCount = &gc
		}
	}
	now := nowMillis
	s.LastResultAt = &now
	if err := kv.PutJSON(ctx, statsKey(subject), s); err != nil {
		return nil, fmt.Errorf("doantu recordResult: %w", err)
	}
	return s, nil
}
