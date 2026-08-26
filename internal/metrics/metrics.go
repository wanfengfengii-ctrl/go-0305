// Package metrics centralises the auditable fixed-point computations used to
// judge isolation-bearing installation quality. Every derived quantity —
// eccentricity, composite tilt, interface height difference, grout bearing
// ratio, anchor pretension deviation, vertical compression, horizontal
// equivalent stiffness and damping — is computed with overflow-checked
// fixed-point arithmetic, round-half-away-from-zero rounding and closed-bound
// threshold comparison, exactly as the domain rules require.
package metrics

import (
	"hospital-isolation-bearing-unlock-closure/internal/fixed"
)

// Eccentricity returns the plan eccentricity between the transformed design
// centre and the measured centre. All coordinates are signed integer microns
// and the result is reported at the given scale. The eccentricity is the
// Euclidean distance sqrt(dx^2 + dy^2).
func Eccentricity(designX, designY, measuredX, measuredY int64, scale int) (int64, error) {
	dx, err := fixed.Sub(measuredX, designX)
	if err != nil {
		return 0, err
	}
	dy, err := fixed.Sub(measuredY, designY)
	if err != nil {
		return 0, err
	}
	return hypotFixed(dx, dy, scale)
}

// CompositeTilt returns the resultant tilt from two orthogonal fixed-point
// components tx and ty at the given scale: sqrt(tx^2 + ty^2).
func CompositeTilt(tx, ty int64, scale int) (int64, error) {
	return hypotFixed(tx, ty, scale)
}

// hypotFixed computes sqrt(a^2 + b^2) for two fixed-point quantities at the
// same scale, returning the result at that same scale.
func hypotFixed(a, b int64, scale int) (int64, error) {
	if scale < 0 {
		return 0, fixed.ErrOverflow
	}
	a2, err := fixed.Mul(a, a)
	if err != nil {
		return 0, err
	}
	b2, err := fixed.Mul(b, b)
	if err != nil {
		return 0, err
	}
	sum, err := fixed.Add(a2, b2)
	if err != nil {
		return 0, err
	}
	// sum is at scale 2*scale; its square root is at scale 'scale'.
	root, err := fixed.Sqrt(sum)
	if err != nil {
		return 0, err
	}
	// The square root of a value at scale 2*scale is already at scale 'scale';
	// round-half-away from zero normalises any fractional residue.
	return fixed.Rescale(root, scale, scale)
}

// Ratio returns numerator/denominator expressed at the given scale with
// round-half-away-from-zero rounding. A zero denominator is rejected.
func Ratio(numerator, denominator int64, scale int) (int64, error) {
	if denominator == 0 {
		return 0, fixed.ErrDivideByZero
	}
	if scale < 0 {
		return 0, fixed.ErrOverflow
	}
	scaled, err := fixed.Mul(numerator, pow10(scale))
	if err != nil {
		return 0, err
	}
	return fixed.DivRoundHalfAway(scaled, denominator)
}

// BearingRatio reports the effective grout bearing ratio as a fixed-point
// value: actualArea/nominalArea at the given scale.
func BearingRatio(actualArea, nominalArea int64, scale int) (int64, error) {
	return Ratio(actualArea, nominalArea, scale)
}

// PretensionDeviation reports the anchor pretension deviation relative to the
// target pretension: |measured-target|/target at the given scale.
func PretensionDeviation(measured, target int64, scale int) (int64, error) {
	if target == 0 {
		return 0, fixed.ErrDivideByZero
	}
	diff, err := fixed.Sub(measured, target)
	if err != nil {
		return 0, err
	}
	absDiff, err := fixed.Abs(diff)
	if err != nil {
		return 0, err
	}
	return Ratio(absDiff, target, scale)
}

// InterfaceHeightDiff returns the difference between the measured upper/lower
// interface elevation and the locked reference, in integer microns.
func InterfaceHeightDiff(measured, reference int64) (int64, error) {
	return fixed.Sub(measured, reference)
}

// VerticalCompression returns the vertical compression of the bearing under
// load as a fixed-point ratio of the observed settlement to the bearing height
// at the given scale.
func VerticalCompression(settlement, height int64, scale int) (int64, error) {
	return Ratio(settlement, height, scale)
}

// HorizontalStiffness returns the horizontal equivalent stiffness force /
// displacement at the given scale. A zero displacement is rejected and the
// multiplication detects overflow, which surfaces oversized-stiffness inputs.
func HorizontalStiffness(force, displacement int64, scale int) (int64, error) {
	return Ratio(force, displacement, scale)
}

// Damping returns the equivalent viscous damping ratio c / (2*sqrt(k*m)) at
// the given scale. Zero stiffness or mass is rejected.
func Damping(c, k, m int64, scale int) (int64, error) {
	if k == 0 || m == 0 {
		return 0, fixed.ErrDivideByZero
	}
	km, err := fixed.Mul(k, m)
	if err != nil {
		return 0, err
	}
	root, err := fixed.Sqrt(km)
	if err != nil {
		return 0, err
	}
	twoRoot, err := fixed.Mul(2, root)
	if err != nil {
		return 0, err
	}
	return Ratio(c, twoRoot, scale)
}

// WithinClosed reports whether value lies in the inclusive range [lo, hi].
// Used to enforce closed-bound threshold acceptance: a value exactly equal to
// a bound is accepted.
func WithinClosed(value, lo, hi int64) bool {
	return value >= lo && value <= hi
}

// pow10 returns 10^n for n >= 0, or 1 for n == 0.
func pow10(n int) int64 {
	if n <= 0 {
		return 1
	}
	v := int64(1)
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
