package catalog

import (
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func validReq() domain.LockRequest {
	up := domain.Orientation{X: 0, Y: 0, Z: 1, Scale: 1}
	down := domain.Orientation{X: 0, Y: 0, Z: -1, Scale: 1}
	return domain.LockRequest{
		OperationID:    "op-1",
		Building:       "A",
		Unit:           "U1",
		SummaryVersion: "v3",
		Transform: domain.PlanTransform{
			A: 1, B: 0, C: 0, D: 0, E: 1, F: 0, Scale: 1,
		},
		Positions: []domain.DesignPosition{
			{
				Building: "A", Unit: "U1", AxisGrid: "1-A", PositionID: "P1",
				DesignCenter:        domain.Point3{X: 0, Y: 0, Z: 0},
				Orientation:         up,
				BearingModel:        "LRB-500",
				Upper:               domain.ConnectionInterface{ID: "u1", Orientation: up, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
				Lower:               domain.ConnectionInterface{ID: "l1", Orientation: down, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
				AllowedEccentricity: 5000, AllowedTilt: 1000, TiltScale: 3,
			},
		},
	}
}

func TestLockHappyPath(t *testing.T) {
	c := New()
	snap, err := c.Lock(nil, validReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Generation != 1 || snap.LockDigest == "" {
		t.Fatalf("bad snapshot: %+v", snap)
	}
	if snap.Positions[0].PositionID != "P1" {
		t.Fatalf("position not sorted/present")
	}
	// Deterministic digest: same request yields the same digest.
	snap2, _ := c.Lock(nil, validReq())
	if snap.LockDigest != snap2.LockDigest {
		t.Fatalf("lock digest not deterministic")
	}
}

func TestLockStaleSummary(t *testing.T) {
	req := validReq()
	req.SummaryVersion = ""
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeStaleSummary) {
		t.Fatalf("expected STALE_SUMMARY, got %v", err)
	}
}

func TestLockEmptyPositions(t *testing.T) {
	req := validReq()
	req.Positions = nil
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockDuplicatePosition(t *testing.T) {
	req := validReq()
	req.Positions = append(req.Positions, req.Positions[0])
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockDegeneratePlate(t *testing.T) {
	req := validReq()
	req.Positions[0].Upper.PlateWidth = 0
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockIncompatibleHoleGroup(t *testing.T) {
	req := validReq()
	req.Positions[0].Lower.HolePattern = "square-300"
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockInvertedInterface(t *testing.T) {
	req := validReq()
	// Make upper and lower point the same direction (reversed interface).
	req.Positions[0].Lower.Orientation = req.Positions[0].Upper.Orientation
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockNonInvertibleTransform(t *testing.T) {
	req := validReq()
	req.Transform = domain.PlanTransform{A: 1, B: 1, C: 0, D: 1, E: 1, F: 0, Scale: 1}
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected INVALID_GEOMETRY, got %v", err)
	}
}

func TestLockTransformOverflow(t *testing.T) {
	req := validReq()
	req.Transform = domain.PlanTransform{
		A: 1 << 62, B: 0, C: 0, D: 0, E: 1 << 62, F: 0, Scale: 1,
	}
	_, err := New().Lock(nil, req)
	if !domain.IsBusinessError(err, domain.CodeArithmeticOverflow) {
		t.Fatalf("expected ARITHMETIC_OVERFLOW, got %v", err)
	}
}
