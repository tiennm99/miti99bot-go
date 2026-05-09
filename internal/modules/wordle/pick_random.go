package wordle

import (
	"errors"
	"math/rand"
)

// errEmptyWordList is returned by pickers when the dictionary is empty —
// callers (Init) should fail fast rather than spin a game with no answers.
var errEmptyWordList = errors.New("wordle: word list is empty")

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
