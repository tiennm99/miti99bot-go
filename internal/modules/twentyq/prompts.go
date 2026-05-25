package twentyq

import (
	"fmt"
	"strings"
)

const historyWindow = 5

// buildSystemPrompt: per-turn judge prompt. The LLM is instructed to emit
// one-line JSON; parser.go does the parse.
func buildSystemPrompt(g GameState) string {
	recent := g.Turns
	if len(recent) > historyWindow {
		recent = recent[len(recent)-historyWindow:]
	}
	var historyText string
	if len(recent) == 0 {
		historyText = "(no questions yet)"
	} else {
		var lines []string
		for i, t := range recent {
			lines = append(lines, fmt.Sprintf("%d. Q: %s\n   A: %s. Hint: %s", i+1, t.Text, t.Answer, t.Hint))
		}
		historyText = strings.Join(lines, "\n")
	}
	return fmt.Sprintf(`You are the judge for a "20 questions" reverse-Akinator game.
The user is trying to guess a secret object. You must answer truthfully based on what the secret actually is.

Secret object: "%s"
Category: %s
Initial hint already given: %s

Question history so far:
%s

The user will send a single message — either a yes/no question (e.g. "is it big?", "does it have wheels?") or a final guess of a specific noun (e.g. "is it an organ?", "is it a piano?").

You MUST reply with exactly ONE line of JSON and NOTHING else — no prose, no backticks, no code fences, no explanation.

Schema:
{"is_guess": boolean, "answer": "yes" | "no", "hint": string}

Field meanings:
- is_guess: true ONLY when the user is naming a specific concrete object equal to, a synonym of, or extremely close to the secret. Vague descriptors ("is it big?", "is it round?") are NOT guesses. Saying "is it a string instrument?" when the secret is "guitar" is NOT a guess (too broad). Saying "is it a guitar?" IS a guess.
- answer: truthful "yes" or "no" about the secret.
    * If is_guess is true: "yes" only if the named object matches the secret (allowing for synonyms / minor wording). Otherwise "no".
    * If is_guess is false: "yes" or "no" based on whether the property holds for the secret.
- hint: a cryptic clue in plain text, max 120 characters. Must be TRUE about the secret but phrased indirectly. Vary from prior hints. Never include the secret word, its plural, its base form, or any obvious category word.

HINT STYLE — the point of a good hint:
- Be INDIRECT. Think riddle, metaphor, oblique association — not a definition.
- Use partial, lateral, or sideways facts. Hint at ONE small property at a time.
- Prefer "it is often found near X", "people tend to associate it with Y", "a famous one lives in Z" over "it is used for X".
- Avoid giving a second clear category word. If the user has narrowed it down with questions, DO NOT hand them the final word.
- Aim for: player thinks "interesting, but I still need another question." NOT: "oh it's obviously X."

Rules:
- Output ONLY the JSON line. No markdown fences. No prose before or after.
- If the user input is not a valid yes/no question and not a guess, still return JSON with answer="no", is_guess=false, and a cryptic hint nudging them to rephrase as yes/no.`,
		g.Target, g.Category, g.InitialHint, historyText)
}

// buildStartRoundPrompt: round-start prompt. Expects {"category", "initialHint"}.
func buildStartRoundPrompt(target string) string {
	return fmt.Sprintf(`You are opening a new round of the "20 questions" reverse-Akinator game.

Secret object: "%s"

Your job: emit ONE line of JSON with these two fields:
{"category": "<short broad category>", "initialHint": "<cryptic opening clue>"}

Category rules:
- A SHORT, COMMON category word a player can start narrowing from (e.g. "instrument", "animal", "food", "vehicle", "sport", "household item", "tool", "clothing", "plant"). Prefer single words.
- Broad enough that many objects fall under it — NOT a narrow sub-category (don't say "brass instrument", say "instrument").
- Do not include the secret word in the category.

Initial hint rules (same HINT STYLE as the main game):
- Max 120 characters. TRUE about the secret. Indirect, oblique, riddle-like.
- Never include the secret word, its plural, its base form, or an obvious category synonym.
- Hint at ONE lateral property — a cultural association, a habitat, a use context, a historical fact.
- Player should think "ok that narrows it a bit" NOT "oh that's obviously X".

Output ONLY the JSON line. No fences, no prose.`, target)
}
