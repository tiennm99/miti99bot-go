package loldleemoji

import "strings"

// findChampion: exact normalised match first, then unique-prefix fallback.
// Ambiguous prefix or no hit → nil. JS-faithful (lookup.js).
func findChampion(pool []EmojiChampion, input string) *EmojiChampion {
	q := normalize(input)
	if q == "" {
		return nil
	}
	for i := range pool {
		if normalize(pool[i].ChampionName) == q {
			return &pool[i]
		}
	}
	var hit *EmojiChampion
	for i := range pool {
		if strings.HasPrefix(normalize(pool[i].ChampionName), q) {
			if hit != nil {
				return nil // ambiguous
			}
			hit = &pool[i]
		}
	}
	return hit
}

// findByExactName looks up a champion by literal championName. Used to
// rehydrate the target from a stored game. Returns nil if the pool was
// refreshed and the target is no longer present.
func findByExactName(pool []EmojiChampion, name string) *EmojiChampion {
	for i := range pool {
		if pool[i].ChampionName == name {
			return &pool[i]
		}
	}
	return nil
}
