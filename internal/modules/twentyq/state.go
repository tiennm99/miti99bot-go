package twentyq

import (
	"context"
	"errors"
	"fmt"

	"github.com/tiennm99/miti99bot/internal/storage"
)

// Turn is one Q&A entry stored in the game's history.
type Turn struct {
	Text    string `json:"text"`
	IsGuess bool   `json:"isGuess"`
	Answer  string `json:"answer"` // "yes" | "no"
	Hint    string `json:"hint"`
	TS      int64  `json:"ts"`
}

type GameState struct {
	Category    string `json:"category"`
	Target      string `json:"target"`
	InitialHint string `json:"initialHint"`
	StartedAt   *int64 `json:"startedAt"`
	Solved      bool   `json:"solved"`
	Turns       []Turn `json:"turns"`
}

type Stats struct {
	Played        int    `json:"played"`
	Solved        int    `json:"solved"`
	TotalTurns    int    `json:"totalTurns"`
	BestTurnCount *int   `json:"bestTurnCount"`
	LastResultAt  *int64 `json:"lastResultAt"`
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
		return nil, fmt.Errorf("twentyq loadGame: %w", err)
	}
}

func saveGame(ctx context.Context, kv storage.KVStore, subject string, g *GameState) error {
	if err := kv.PutJSON(ctx, gameKey(subject), g); err != nil {
		return fmt.Errorf("twentyq saveGame: %w", err)
	}
	return nil
}

func clearGame(ctx context.Context, kv storage.KVStore, subject string) error {
	if err := kv.Delete(ctx, gameKey(subject)); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("twentyq clearGame: %w", err)
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
		return nil, fmt.Errorf("twentyq loadStats: %w", err)
	}
}

func recordResult(ctx context.Context, kv storage.KVStore, subject string, solved bool, turnCount int, nowMillis int64) (*Stats, error) {
	s, err := loadStats(ctx, kv, subject)
	if err != nil {
		return nil, err
	}
	s.Played++
	s.TotalTurns += turnCount
	if solved {
		s.Solved++
		if s.BestTurnCount == nil || turnCount < *s.BestTurnCount {
			tc := turnCount
			s.BestTurnCount = &tc
		}
	}
	now := nowMillis
	s.LastResultAt = &now
	if err := kv.PutJSON(ctx, statsKey(subject), s); err != nil {
		return nil, fmt.Errorf("twentyq recordResult: %w", err)
	}
	return s, nil
}
