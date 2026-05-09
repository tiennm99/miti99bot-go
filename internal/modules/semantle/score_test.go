package semantle

import (
	"math"
	"testing"
)

func TestCosine_IdenticalVectors(t *testing.T) {
	a := []float32{1, 0, 0}
	got, ok := cosine(a, a)
	if !ok {
		t.Fatal("ok=false for identical vectors")
	}
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("identical: got %v, want 1.0", got)
	}
}

func TestCosine_Orthogonal(t *testing.T) {
	got, ok := cosine([]float32{1, 0}, []float32{0, 1})
	if !ok {
		t.Fatal("ok=false")
	}
	if math.Abs(got) > 1e-6 {
		t.Errorf("orthogonal: got %v, want 0", got)
	}
}

func TestCosine_Opposite(t *testing.T) {
	got, ok := cosine([]float32{1, 0}, []float32{-1, 0})
	if !ok {
		t.Fatal("ok=false")
	}
	if math.Abs(got+1) > 1e-6 {
		t.Errorf("opposite: got %v, want -1", got)
	}
}

func TestCosine_LengthMismatch(t *testing.T) {
	if _, ok := cosine([]float32{1}, []float32{1, 0}); ok {
		t.Errorf("length-mismatch: want ok=false")
	}
}

func TestCosine_Empty(t *testing.T) {
	if _, ok := cosine(nil, nil); ok {
		t.Errorf("nil: want ok=false")
	}
}

func TestCalibrate_FloorAndCeiling(t *testing.T) {
	if v := calibrate(-0.5); v != 0 {
		t.Errorf("below floor: got %v, want 0", v)
	}
	if v := calibrate(1.0); v != 100 {
		t.Errorf("at ceiling: got %v, want 100", v)
	}
	// Mid-range stays in bounds.
	for _, raw := range []float64{0.5, 0.7, 0.9} {
		v := calibrate(raw)
		if v < 0 || v > 100 {
			t.Errorf("calibrate(%v) = %v out of [0,100]", raw, v)
		}
	}
}

func TestCalibrate_Monotonic(t *testing.T) {
	prev := -1.0
	for _, raw := range []float64{0.45, 0.55, 0.65, 0.75, 0.85, 0.95} {
		v := calibrate(raw)
		if v < prev {
			t.Errorf("calibrate non-monotonic at raw=%v: %v < %v", raw, v, prev)
		}
		prev = v
	}
}
