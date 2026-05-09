package doantu

import "math"

// calibrate: phow2sim cosines already span a wide useful range — JS just
// scales linearly to 0-100 with negative clamp. No sigmoid here.
func calibrate(raw float64) float64 {
	v := raw * 100
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

func formatWarmth(score float64) string {
	pct := int(math.Round(score))
	switch {
	case pct >= 100:
		return "100"
	case pct < 10:
		return "0" + itoa(pct)
	default:
		return itoa(pct)
	}
}

func warmthEmoji(score float64) string {
	switch {
	case score >= 90:
		return "🎯"
	case score >= 70:
		return "🔥"
	case score >= 40:
		return "🌡️"
	case score >= 15:
		return "😐"
	default:
		return "🥶"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
