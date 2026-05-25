package loldle

import "fmt"

// attemptFlavor returns a one-word reaction to a winning attempt:
// 1 → "First try!", 2 → "Sharp!", final → "Phew — last one!",
// final-2 → "Close call!", else "Nice.".
func attemptFlavor(attempt, max int) string {
	if attempt <= 1 {
		return "First try!"
	}
	if attempt == 2 {
		return "Sharp!"
	}
	if attempt >= max {
		return "Phew — last one!"
	}
	if attempt >= max-2 {
		return "Close call!"
	}
	return "Nice."
}

// formatDuration renders an elapsed-ms span as a compact human string.
//
//	< 60s     → "42s"
//	< 60min   → "3m 14s" (or "3m" when seconds == 0)
//	otherwise → "1h 12m" (or "1h" when remaining minutes == 0)
//
// Negative inputs clamp to 0.
func formatDuration(ms int64) string {
	total := ms / 1000
	if ms%1000 >= 500 {
		total++ // round-half-up
	}
	if total < 0 {
		total = 0
	}
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	minutes := total / 60
	seconds := total % 60
	if minutes < 60 {
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := minutes / 60
	remMin := minutes % 60
	if remMin == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remMin)
}
