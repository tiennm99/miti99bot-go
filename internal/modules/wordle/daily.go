package wordle

import (
	"errors"
	"math/rand"
	"time"
)

// errEmptyWordList is returned by pickers when the dictionary is empty —
// callers (Init) should fail fast rather than spin a game with no answers.
var errEmptyWordList = errors.New("wordle: word list is empty")

// todayUTC returns the current date as YYYY-MM-DD in UTC. Mirrors JS
// `new Date().toISOString().slice(0, 10)`.
func todayUTC(now time.Time) string {
	return now.UTC().Format("2006-01-02")
}

// hashDJB2 is the same djb2 variant the JS source uses, with an explicit
// 32-bit mask at the end.
func hashDJB2(s string) uint32 {
	h := uint32(5381)
	for i := 0; i < len(s); i++ {
		h = (h * 33) ^ uint32(s[i])
	}
	return h
}

// pickDaily returns a deterministic pick keyed by a string seed (defaulting
// to today's UTC date). Same word for everyone on the same UTC day.
//
// Currently unused by handlers (which use pickRandom for variety per round)
// but kept for parity with the JS source — Phase 5c may switch to a daily.
func pickDaily(words []string, seed string) (string, error) {
	if len(words) == 0 {
		return "", errEmptyWordList
	}
	if seed == "" {
		seed = todayUTC(time.Now())
	}
	idx := int(hashDJB2(seed) % uint32(len(words)))
	return words[idx], nil
}

// pickRandom is the picker handlers actually use today. Uniform random pick.
// rng allows tests to inject a deterministic source. When rng is nil we fall
// through to math/rand's package-level Intn, which IS goroutine-safe via an
// internal mutex on the global Source — important because the bot dispatcher
// runs each Telegram update in its own goroutine and concurrent /wordle_new
// calls would otherwise race on a shared *rand.Rand.
func pickRandom(words []string, rng *rand.Rand) (string, error) {
	if len(words) == 0 {
		return "", errEmptyWordList
	}
	if rng != nil {
		return words[rng.Intn(len(words))], nil
	}
	return words[rand.Intn(len(words))], nil
}
