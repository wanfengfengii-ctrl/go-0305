// Package fixed provides overflow-checked signed fixed-point arithmetic used
// across the hospital isolation-bearing domain.
//
// All lengths, coordinates and elevations are stored as signed integer
// microns. Angles, stiffness, damping, compression ratios, bearing ratios and
// deviations are stored as signed integers with an explicit decimal scale
// (value = mantissa / 10^scale). Every arithmetic primitive pre-checks
// overflow, minimum-integer negation and division by zero, and rounds towards
// the nearest value with ties rounded away from zero (round half away from
// zero), as required by the domain rules.
package fixed

import "errors"

// Errors returned by the checked arithmetic primitives.
var (
	ErrOverflow     = errors.New("fixed: arithmetic overflow")
	ErrDivideByZero = errors.New("fixed: division by zero")
)

// Add returns a+b or ErrOverflow when the result cannot be represented.
func Add(a, b int64) (int64, error) {
	r := a + b
	if (a > 0 && b > 0 && r < 0) || (a < 0 && b < 0 && r >= 0) {
		return 0, ErrOverflow
	}
	return r, nil
}

// Sub returns a-b or ErrOverflow when the result cannot be represented.
func Sub(a, b int64) (int64, error) {
	r := a - b
	if (a >= 0 && b < 0 && r < 0) || (a < 0 && b > 0 && r >= 0) {
		return 0, ErrOverflow
	}
	return r, nil
}

// Mul returns a*b or ErrOverflow when the result cannot be represented.
func Mul(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	r := a * b
	if r/b != a {
		return 0, ErrOverflow
	}
	// MinInt64 * -1 overflows even though the division check above passes for
	// the product of MinInt64 and -1 in some compilers; guard explicitly.
	if a == -1 && b == -1<<63 || b == -1 && a == -1<<63 {
		return 0, ErrOverflow
	}
	return r, nil
}

// Abs returns the absolute value of a or ErrOverflow for the minimum integer.
func Abs(a int64) (int64, error) {
	if a == -1<<63 {
		return 0, ErrOverflow
	}
	if a < 0 {
		return -a, nil
	}
	return a, nil
}

// Sqrt returns the floor of the integer square root of v. It returns
// ErrOverflow when v is negative. The result is the largest integer whose
// square is less than or equal to v.
func Sqrt(v int64) (int64, error) {
	if v < 0 {
		return 0, ErrOverflow
	}
	if v < 2 {
		return v, nil
	}
	x := v
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + v/x) / 2
	}
	return x, nil
}

// DivRoundHalfAway divides a by b, rounding ties away from zero. It returns
// ErrDivideByZero when b is zero and ErrOverflow when MinInt64 is divided by -1.
func DivRoundHalfAway(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	if a == -1<<63 && b == -1 {
		return 0, ErrOverflow
	}
	q := a / b
	r := a % b
	if r == 0 {
		return q, nil
	}
	// Round half away from zero.
	absR, _ := Abs(r)
	absB, _ := Abs(b)
	twice := absR * 2
	if twice < absB {
		return q, nil
	}
	if twice > absB {
		return roundAway(q, a, b), nil
	}
	// Exact half: round away from zero.
	if q >= 0 {
		q++
	} else {
		q--
	}
	return q, nil
}

func roundAway(q, a, b int64) int64 {
	// q truncated towards zero; move one step away from zero.
	if (a < 0) != (b < 0) {
		return q - 1
	}
	return q + 1
}

// Rescale converts a mantissa from fromScale to toScale, multiplying or
// dividing by powers of ten with round-half-away-from-zero behaviour and full
// overflow checking. A negative scale is treated as an error.
func Rescale(v int64, fromScale, toScale int) (int64, error) {
	if fromScale < 0 || toScale < 0 {
		return 0, ErrOverflow
	}
	diff := toScale - fromScale
	if diff == 0 {
		return v, nil
	}
	if diff > 0 {
		// Multiply by 10^diff.
		for i := 0; i < diff; i++ {
			nv, err := Mul(v, 10)
			if err != nil {
				return 0, err
			}
			v = nv
		}
		return v, nil
	}
	// Divide by 10^(-diff).
	for i := 0; i < -diff; i++ {
		nv, err := DivRoundHalfAway(v, 10)
		if err != nil {
			return 0, err
		}
		v = nv
	}
	return v, nil
}

// Q is a signed fixed-point number: value = Mantissa / 10^Scale.
type Q struct {
	Mantissa int64
	Scale    int
}

// Add returns the sum of two quantities rescaled to a common scale.
func (q Q) Add(o Q) (Q, error) {
	scale := q.Scale
	if o.Scale > scale {
		scale = o.Scale
	}
	a, err := Rescale(q.Mantissa, q.Scale, scale)
	if err != nil {
		return Q{}, err
	}
	b, err := Rescale(o.Mantissa, o.Scale, scale)
	if err != nil {
		return Q{}, err
	}
	s, err := Add(a, b)
	if err != nil {
		return Q{}, err
	}
	return Q{Mantissa: s, Scale: scale}, nil
}

// Mul returns the product of two quantities.
func (q Q) Mul(o Q) (Q, error) {
	m, err := Mul(q.Mantissa, o.Mantissa)
	if err != nil {
		return Q{}, err
	}
	return Q{Mantissa: m, Scale: q.Scale + o.Scale}, nil
}

// Div returns q/o with round-half-away-from-zero behaviour.
func (q Q) Div(o Q) (Q, error) {
	if o.Mantissa == 0 {
		return Q{}, ErrDivideByZero
	}
	// Align scales so the mantissa division yields scale = q.Scale - o.Scale.
	n, err := Rescale(q.Mantissa, q.Scale, o.Scale)
	if err != nil {
		return Q{}, err
	}
	d, err := DivRoundHalfAway(n, o.Mantissa)
	if err != nil {
		return Q{}, err
	}
	return Q{Mantissa: d, Scale: 0}, nil
}
