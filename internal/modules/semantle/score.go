package semantle

import "math"

// cosine returns the cosine similarity of two float32 vectors. nil/empty
// inputs and length mismatch return (0, false) — caller should treat as OOV.
//
// math.Sqrt is float64 internally; cast at the boundary, accumulate in
// float64 to avoid 32-bit precision loss on long vectors (text-embedding-004
// is 768-dim, so the dot product easily exceeds 2^24 mantissa precision).
func cosine(a, b []float32) (float64, bool) {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot, nA, nB float64
	for i := range a {
		da := float64(a[i])
		db := float64(b[i])
		dot += da * db
		nA += da * da
		nB += db * db
	}
	denom := math.Sqrt(nA) * math.Sqrt(nB)
	if denom == 0 {
		return 0, false
	}
	return dot / denom, true
}
