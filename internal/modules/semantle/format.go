package semantle

import "math"

// Calibration constants — tuned empirically for bge-m3 on the JS side.
// text-embedding-004 (768d, this port) lives in a similar narrow cone, so
// the same sigmoid mapping holds well enough that retuning is not blocking
// for v1. Phase 11 soak data may justify a re-fit; until then, JS parity.
const (
	floor  = 0.4
	center = 0.6
	scale  = 8.0
)

var (
	floorSig = sigmoid(scale * (floor - center))
	oneSig   = sigmoid(scale * (1 - center))
	sigRange = oneSig - floorSig
)

func sigmoid(x float64) float64 { return 1.0 / (1.0 + math.Exp(-x)) }

// calibrate maps raw cosine ∈ [-1, 1] → display score ∈ [0, 100]. Mirrors
// JS format.js calibrate(). Returns 0 below floor, 100 at exact match.
func calibrate(raw float64) float64 {
	if raw >= 1 {
		return 100
	}
	if raw <= floor {
		return 0
	}
	s := sigmoid(scale * (raw - center))
	v := ((s - floorSig) / sigRange) * 100
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

// formatWarmth: zero-padded percent, width 2 ("07", "54", "100").
func formatWarmth(score float64) string {
	pct := int(math.Round(score))
	if pct >= 100 {
		return "100"
	}
	if pct < 10 {
		return "0" + itoa(pct)
	}
	return itoa(pct)
}

// warmthEmoji: bucket emoji by calibrated score, JS-parity thresholds.
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

// itoa is a tiny stdlib-free int→string for the 0-99 range above. strconv
// works too; this is a perf nit borrowed from wordle/render. Either is fine.
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
