package twentyq

import (
	"fmt"
	"html"
	"strings"
)

const maxTurnRows = 10

func emojiFor(answer string) string {
	switch answer {
	case "yes":
		return "✅"
	case "no":
		return "❌"
	default:
		return "❓"
	}
}

func formatIntro(g GameState) string {
	return strings.Join([]string{
		fmt.Sprintf("🎯 I'm thinking of <b>a %s</b>.", html.EscapeString(g.Category)),
		"Hint: " + html.EscapeString(g.InitialHint),
		"",
		"Ask yes/no questions with <code>/twentyq is it ...?</code>",
	}, "\n")
}

func formatTurnReply(t Turn, solved bool, target string, turnCount int) string {
	if solved {
		return fmt.Sprintf("🎉 Correct! It was <b>%s</b>.\nSolved in %d question%s.",
			html.EscapeString(target), turnCount, plural(turnCount))
	}
	emoji := emojiFor(t.Answer)
	if t.IsGuess {
		return fmt.Sprintf("%s Not quite. Hint: %s", emoji, html.EscapeString(t.Hint))
	}
	yn := "No"
	if t.Answer == "yes" {
		yn = "Yes"
	}
	return fmt.Sprintf("%s %s. Hint: %s", emoji, yn, html.EscapeString(t.Hint))
}

func formatBoard(g GameState) string {
	header := fmt.Sprintf("🎯 Category: <b>%s</b>", html.EscapeString(g.Category))
	intro := "Initial hint: " + html.EscapeString(g.InitialHint)
	if len(g.Turns) == 0 {
		return strings.Join([]string{header, intro, "", "<i>No questions yet — go ahead and ask one.</i>"}, "\n")
	}
	recent := g.Turns
	startNo := 1
	if len(recent) > maxTurnRows {
		startNo = len(g.Turns) - maxTurnRows + 1
		recent = recent[len(recent)-maxTurnRows:]
	}
	var lines []string
	for i, t := range recent {
		num := fmt.Sprintf("%2d", startNo+i)
		lines = append(lines, fmt.Sprintf("%s. %s <b>%s</b>\n     %s",
			num, emojiFor(t.Answer), html.EscapeString(t.Text), html.EscapeString(t.Hint)))
	}
	body := strings.Join([]string{header, intro, "", strings.Join(lines, "\n")}, "\n")
	if hidden := len(g.Turns) - len(recent); hidden > 0 {
		body += fmt.Sprintf("\n…%d earlier turn%s hidden.", hidden, pluralS(hidden))
	}
	return body
}

func formatGiveup(g GameState) string {
	return strings.Join([]string{
		fmt.Sprintf("🏳️ Gave up. The answer was <b>%s</b>.", html.EscapeString(g.Target)),
		"Send <code>/twentyq</code> to start a fresh round.",
	}, "\n")
}

func formatStats(s Stats) string {
	if s.Played == 0 {
		return "No twentyq games played yet."
	}
	solveRate := 0
	if s.Played > 0 {
		solveRate = (s.Solved*200 + s.Played) / (2 * s.Played)
	}
	avg := "—"
	if s.Played > 0 {
		avg = fmt.Sprintf("%d", (s.TotalTurns*2+s.Played)/(2*s.Played))
	}
	best := "—"
	if s.BestTurnCount != nil {
		best = fmt.Sprintf("%d", *s.BestTurnCount)
	}
	return fmt.Sprintf("🎯 <b>Twentyq stats</b>\nPlayed: %d\nSolved: %d (%d%%)\nTotal questions: %d\nFewest to solve: %s\nAvg per round: %s",
		s.Played, s.Solved, solveRate, s.TotalTurns, best, avg)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
