// Package trading is a paper-trading module for VN stocks. Per-user
// portfolio + buy/sell at market price + stats with P&L. SQL-based trade
// history and a retention cron are out of scope today; the current
// implementation keeps only the live portfolio in KV.
package trading

import (
	"math"
	"strconv"
	"strings"
)

// FormatVND renders an integer-rounded amount with dot-thousands separators
// and a "VND" suffix (e.g. 15000000 -> "15.000.000 VND"). Manual to avoid
// locale-dependent formatting.
func FormatVND(n float64) string {
	rounded := int64(math.Round(n))
	abs := strconv.FormatInt(absInt64(rounded), 10)
	var sb strings.Builder
	if rounded < 0 {
		sb.WriteByte('-')
	}
	for i := 0; i < len(abs); i++ {
		if i > 0 && (len(abs)-i)%3 == 0 {
			sb.WriteByte('.')
		}
		sb.WriteByte(abs[i])
	}
	sb.WriteString(" VND")
	return sb.String()
}

// FormatStock renders an integer share quantity (always whole shares).
func FormatStock(n float64) string {
	return strconv.FormatInt(int64(math.Floor(n)), 10)
}

// FormatPnL renders a signed VND delta + percentage line, e.g.
// "+1.234 VND (+12.34%)" or "-500.000 VND (-5.00%)". When invested is zero
// the percentage is reported as 0.00 to avoid division-by-zero.
func FormatPnL(currentValue, invested float64) string {
	diff := currentValue - invested
	pct := 0.0
	if invested > 0 {
		pct = (diff / invested) * 100
	}
	sign := ""
	if diff >= 0 {
		sign = "+"
	}
	return sign + FormatVND(diff) + " (" + sign + strconv.FormatFloat(pct, 'f', 2, 64) + "%)"
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
