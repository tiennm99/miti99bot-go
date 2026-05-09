package trading

import "testing"

func TestFormatVND(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0 VND"},
		{1, "1 VND"},
		{100, "100 VND"},
		{1000, "1.000 VND"},
		{15_000_000, "15.000.000 VND"},
		{1_234_567_890, "1.234.567.890 VND"},
		{-500, "-500 VND"},
		{-1_500_000, "-1.500.000 VND"},
		// rounding: half away from zero (math.Round)
		{1.5, "2 VND"},
		{-1.5, "-2 VND"},
	}
	for _, c := range cases {
		if got := FormatVND(c.in); got != c.want {
			t.Errorf("FormatVND(%v): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatStock(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{100, "100"},
		{99.9, "99"}, // floor for safety; we never store fractional shares anyway
	}
	for _, c := range cases {
		if got := FormatStock(c.in); got != c.want {
			t.Errorf("FormatStock(%v): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatPnL(t *testing.T) {
	cases := []struct {
		current, invested float64
		want              string
	}{
		{1_500_000, 1_000_000, "+500.000 VND (+50.00%)"},
		{800_000, 1_000_000, "-200.000 VND (-20.00%)"},
		{1_000_000, 1_000_000, "+0 VND (+0.00%)"},
		{500_000, 0, "+500.000 VND (+0.00%)"}, // invested=0 → pct=0, no NaN
	}
	for _, c := range cases {
		if got := FormatPnL(c.current, c.invested); got != c.want {
			t.Errorf("FormatPnL(%v,%v): got %q, want %q", c.current, c.invested, got, c.want)
		}
	}
}
