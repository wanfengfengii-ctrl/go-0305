package metrics

import (
	"errors"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/fixed"
)

func TestEccentricityPythagorean(t *testing.T) {
	// 3-4-5 triangle at integer microns: dx=3, dy=4 -> eccentricity 5.
	e, err := Eccentricity(0, 0, 3, 4, 0)
	if err != nil || e != 5 {
		t.Fatalf("Eccentricity(0,0,3,4)=%d,%v want 5", e, err)
	}
}

func TestCompositeTiltMidpoint(t *testing.T) {
	// Orthogonal components 3 and 4 at scale 0 give tilt 5.
	tilt, err := CompositeTilt(3, 4, 0)
	if err != nil || tilt != 5 {
		t.Fatalf("CompositeTilt(3,4)=%d,%v want 5", tilt, err)
	}
}

func TestBearingRatioRoundHalfAway(t *testing.T) {
	// 1/2 at scale 100 -> 50 (exact). 2/3 at scale 100 -> 67 (round half away).
	r, err := BearingRatio(1, 2, 2)
	if err != nil || r != 50 {
		t.Fatalf("BearingRatio(1,2)=%d,%v want 50", r, err)
	}
	r, err = BearingRatio(2, 3, 2)
	if err != nil || r != 67 {
		t.Fatalf("BearingRatio(2,3)=%d,%v want 67", r, err)
	}
}

func TestPretensionDeviationClosedBound(t *testing.T) {
	// measured=105, target=100 -> deviation 5/100 = 5 at scale 100.
	d, err := PretensionDeviation(105, 100, 2)
	if err != nil || d != 5 {
		t.Fatalf("PretensionDeviation=%d,%v want 5", d, err)
	}
	// Exactly at the boundary is accepted (closed interval).
	if !WithinClosed(d, 0, 5) {
		t.Fatalf("boundary value %d should be within [0,5]", d)
	}
	if WithinClosed(6, 0, 5) {
		t.Fatalf("6 should not be within [0,5]")
	}
}

func TestZeroDenominatorRejected(t *testing.T) {
	if _, err := BearingRatio(1, 0, 2); !errors.Is(err, fixed.ErrDivideByZero) {
		t.Fatalf("expected divide-by-zero, got %v", err)
	}
	if _, err := PretensionDeviation(105, 0, 2); !errors.Is(err, fixed.ErrDivideByZero) {
		t.Fatalf("expected divide-by-zero, got %v", err)
	}
}

func TestOversizedStiffnessRejected(t *testing.T) {
	// Force near MaxInt64 with tiny displacement overflows the scale multiply.
	if _, err := HorizontalStiffness(1<<62, 1, 6); !errors.Is(err, fixed.ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
}

func TestDampingZeroMassRejected(t *testing.T) {
	if _, err := Damping(1, 100, 0, 100); !errors.Is(err, fixed.ErrDivideByZero) {
		t.Fatalf("expected divide-by-zero for zero mass, got %v", err)
	}
}
