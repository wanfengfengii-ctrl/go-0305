package domain

import "hospital-isolation-bearing-unlock-closure/internal/fixed"

// ApplyPlanTransform applies the two-dimensional affine plan transform to the
// X/Y components of a point, leaving Z untouched. The result is rounded with
// round-half-away-from-zero and every multiply/add is overflow checked.
//
//	x' = (A*x + B*y)/Scale + C
//	y' = (D*x + E*y)/Scale + F
func ApplyPlanTransform(t PlanTransform, p Point3) (Point3, error) {
	x, err := applyXY(t.A, t.B, t.C, p.X, p.Y, t.Scale)
	if err != nil {
		return Point3{}, err
	}
	y, err := applyXY(t.D, t.E, t.F, p.X, p.Y, t.Scale)
	if err != nil {
		return Point3{}, err
	}
	return Point3{X: x, Y: y, Z: p.Z}, nil
}

func applyXY(a, b, c, x, y int64, scale int) (int64, error) {
	if scale <= 0 {
		return 0, fixed.ErrOverflow
	}
	ax, err := fixed.Mul(a, x)
	if err != nil {
		return 0, err
	}
	by, err := fixed.Mul(b, y)
	if err != nil {
		return 0, err
	}
	sum, err := fixed.Add(ax, by)
	if err != nil {
		return 0, err
	}
	div, err := fixed.DivRoundHalfAway(sum, int64(scale))
	if err != nil {
		return 0, err
	}
	return fixed.Add(div, c)
}
