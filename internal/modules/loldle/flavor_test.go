package loldle

import "testing"

func TestAttemptFlavor(t *testing.T) {
	const maxAttempts = 8
	cases := map[int]string{
		1: "First try!",
		2: "Sharp!",
		3: "Nice.",
		4: "Nice.",
		5: "Nice.",
		6: "Close call!",
		7: "Close call!",
		8: "Phew — last one!",
		9: "Phew — last one!", // attempt > max — defensive (`>=` branch)
	}
	for attempt, want := range cases {
		if got := attemptFlavor(attempt, maxAttempts); got != want {
			t.Errorf("attemptFlavor(%d, %d) = %q, want %q", attempt, maxAttempts, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{500, "1s"}, // round-half-up
		{499, "0s"},
		{42_000, "42s"},
		{60_000, "1m"},
		{75_500, "1m 16s"}, // 75.5s → 76s → 1m 16s
		{90_000, "1m 30s"},
		{60 * 60 * 1000, "1h"},
		{60 * 60 * 1000 * 2, "2h"},
		{(60*60 + 12*60) * 1000, "1h 12m"},
		{-500, "0s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.ms); got != c.want {
			t.Errorf("formatDuration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}
