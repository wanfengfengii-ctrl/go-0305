package fixed

import (
	"errors"
	"math"
	"testing"
)

func TestAddOverflow(t *testing.T) {
	if _, err := Add(math.MaxInt64, 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if _, err := Add(-1<<63, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := Add(2, 3); err != nil || got != 5 {
		t.Fatalf("Add(2,3) = %d,%v", got, err)
	}
}

func TestSubOverflow(t *testing.T) {
	if _, err := Sub(-1<<63, 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := Sub(10, 3); err != nil || got != 7 {
		t.Fatalf("Sub(10,3) = %d,%v", got, err)
	}
}

func TestMulOverflowAndMinIntNegation(t *testing.T) {
	if _, err := Mul(math.MaxInt64, 2); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if _, err := Mul(-1<<63, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow for MinInt64 * -1, got %v", err)
	}
	if got, err := Mul(6, 7); err != nil || got != 42 {
		t.Fatalf("Mul(6,7) = %d,%v", got, err)
	}
}

func TestAbsMinInt(t *testing.T) {
	if _, err := Abs(-1 << 63); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := Abs(-42); err != nil || got != 42 {
		t.Fatalf("Abs(-42) = %d,%v", got, err)
	}
}

func TestDivRoundHalfAway(t *testing.T) {
	cases := []struct {
		a, b, want int64
	}{
		{5, 2, 3},   // 2.5 -> 3
		{-5, 2, -3}, // -2.5 -> -3
		{7, 2, 4},   // 3.5 -> 4
		{-7, 2, -4}, // -3.5 -> -4
		{4, 2, 2},
		{-4, 2, -2},
	}
	for _, c := range cases {
		got, err := DivRoundHalfAway(c.a, c.b)
		if err != nil {
			t.Fatalf("DivRoundHalfAway(%d,%d) err=%v", c.a, c.b, err)
		}
		if got != c.want {
			t.Fatalf("DivRoundHalfAway(%d,%d)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := DivRoundHalfAway(1, 0); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("expected divide by zero, got %v", err)
	}
	if _, err := DivRoundHalfAway(-1<<63, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}
}

func TestRescale(t *testing.T) {
	if got, err := Rescale(1234, 3, 0); err != nil || got != 1 {
		t.Fatalf("Rescale(1234,3,0)=%d,%v", got, err)
	}
	if got, err := Rescale(1250, 3, 0); err != nil || got != 1 {
		t.Fatalf("half away from zero: Rescale(1250,3,0)=%d,%v", got, err)
	}
	if got, err := Rescale(2, 0, 3); err != nil || got != 2000 {
		t.Fatalf("Rescale(2,0,3)=%d,%v", got, err)
	}
}

func TestQArithmetic(t *testing.T) {
	a := Q{Mantissa: 2500, Scale: 3} // 2.500
	b := Q{Mantissa: 1500, Scale: 3} // 1.500
	sum, err := a.Add(b)
	if err != nil || sum.Mantissa != 4000 || sum.Scale != 3 {
		t.Fatalf("Add=%+v err=%v", sum, err)
	}
	prod, err := a.Mul(b)
	if err != nil || prod.Mantissa != 3750000 || prod.Scale != 6 {
		t.Fatalf("Mul=%+v err=%v", prod, err)
	}
	quot, err := a.Div(b)
	if err != nil {
		t.Fatalf("Div err=%v", err)
	}
	// 2.5 / 1.5 = 1.666... -> 2 (scale 0)
	if quot.Mantissa != 2 {
		t.Fatalf("Div=%+v want 2", quot)
	}
	if _, err := a.Div(Q{}); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("expected divide by zero, got %v", err)
	}
}
