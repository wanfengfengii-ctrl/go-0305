package app

import (
	"context"
	"fmt"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// newTestService builds an in-memory store-backed service for tests.
func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

// testUnit builds a LockRequest for a unit with n positions P1..Pn, a chained
// force-transfer adjacency and one sync-unlock group per position.
func testUnit(unit string, n int) domain.LockRequest {
	up := domain.Orientation{X: 0, Y: 0, Z: 1, Scale: 1}
	down := domain.Orientation{X: 0, Y: 0, Z: -1, Scale: 1}
	var positions []domain.DesignPosition
	for i := 1; i <= n; i++ {
		positions = append(positions, domain.DesignPosition{
			Building: "A", Unit: unit, AxisGrid: fmt.Sprintf("%d-A", i), PositionID: fmt.Sprintf("P%d", i),
			DesignCenter:        domain.Point3{X: 0, Y: 0, Z: 0},
			Orientation:         up,
			BearingModel:        "LRB-500",
			Upper:               domain.ConnectionInterface{ID: "u", Orientation: up, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
			Lower:               domain.ConnectionInterface{ID: "l", Orientation: down, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
			AllowedEccentricity: 5000, AllowedTilt: 1000, TiltScale: 3,
			MaxShimThickness: 20000, MaxShimLayers: 4,
		})
	}
	var adjacency [][2]string
	for i := 1; i < n; i++ {
		adjacency = append(adjacency, [2]string{fmt.Sprintf("P%d", i), fmt.Sprintf("P%d", i+1)})
	}
	var groups [][]string
	for i := 1; i <= n; i++ {
		groups = append(groups, []string{fmt.Sprintf("P%d", i)})
	}
	return domain.LockRequest{
		OperationID: "op-lock-" + unit, Building: "A", Unit: unit, SummaryVersion: "v1",
		Transform:       domain.PlanTransform{A: 1, B: 0, C: 0, D: 0, E: 1, F: 0, Scale: 1},
		Positions:       positions,
		Adjacency:       adjacency,
		SyncUnlockGroup: groups,
	}
}

// lockUnit locks a test unit and returns the snapshot.
func lockUnit(t *testing.T, svc *Service, unit string, n int) domain.DesignSnapshot {
	t.Helper()
	snap, err := svc.LockDesign(context.Background(), testUnit(unit, n))
	if err != nil {
		t.Fatalf("lock unit %s: %v", unit, err)
	}
	return snap
}

// registerBearing registers a bearing component.
func registerBearing(t *testing.T, svc *Service, id, model, batch string) {
	t.Helper()
	err := svc.RegisterComponent(context.Background(), domain.PhysicalComponent{
		ID: id, Kind: domain.KindBearing, Model: model, ManufactureBatch: batch,
		ConstructionSummary: "summary", Status: "intake",
	})
	if err != nil {
		t.Fatalf("register bearing %s: %v", id, err)
	}
}

// registerShim registers a shim component.
func registerShim(t *testing.T, svc *Service, id string, thickness int64) {
	t.Helper()
	err := svc.RegisterComponent(context.Background(), domain.PhysicalComponent{
		ID: id, Kind: domain.KindShim, ThicknessMicron: thickness, Status: "intake",
	})
	if err != nil {
		t.Fatalf("register shim %s: %v", id, err)
	}
}

// registerLot registers a grout lot.
func registerLot(t *testing.T, svc *Service, id, unit string, grams, expiry int64) {
	t.Helper()
	err := svc.RegisterLot(context.Background(), domain.ConsumableLot{
		ID: id, Unit: unit, InitialGrams: grams, ExpiryLogicalTime: expiry,
	})
	if err != nil {
		t.Fatalf("register lot %s: %v", id, err)
	}
}

// op builds an operation request for a given stage with default values.
func op(stage domain.Stage, opID string, logicalTime int64) domain.OperationRequest {
	return domain.OperationRequest{
		OperationID: opID, ExpectedGeneration: 1, Stage: stage, LogicalTime: logicalTime, Holder: "op",
	}
}

// advanceFullPosition advances a single position through all ten stages using a
// pre-registered bearing, shim and lot.
func advanceFullPosition(t *testing.T, svc *Service, unit, position, bearingID, shimID, lotID string) {
	t.Helper()
	lt := int64(0)
	step := func(stage domain.Stage, mutate func(*domain.OperationRequest)) {
		lt += 10
		r := op(stage, stage.String()+"-"+position, lt)
		if mutate != nil {
			mutate(&r)
		}
		if _, err := svc.ApplyOperation(context.Background(), unit, position, r); err != nil {
			t.Fatalf("advance %s %s: %v", position, stage, err)
		}
	}
	step(domain.StageIncomingAccepted, func(r *domain.OperationRequest) { r.ComponentID = bearingID })
	step(domain.StageBaseAccepted, nil)
	step(domain.StagePlaced, func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceHoist
		r.LeaseResourceID = "H-" + position
		r.LeaseExpiry = lt + 100
	})
	step(domain.StageLeveled, func(r *domain.OperationRequest) { r.ShimIDs = []string{shimID} })
	step(domain.StageGrouted, func(r *domain.OperationRequest) {
		r.GroutLotID = lotID
		r.GroutGrams = 100
		r.LeaseKind = domain.ResourceGroutStation
		r.LeaseResourceID = "G-" + position
		r.LeaseExpiry = lt + 100
	})
	step(domain.StageInitialTightened, func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceTorqueTool
		r.LeaseResourceID = "T1-" + position
		r.LeaseExpiry = lt + 100
	})
	step(domain.StageFinalTightened, func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceTorqueTool
		r.LeaseResourceID = "T2-" + position
		r.LeaseExpiry = lt + 100
	})
	step(domain.StageCured, nil)
	step(domain.StageRemeasured, nil)
	step(domain.StageUnlocked, nil)
}

// storeOpen opens a file-based store (used by restart-recovery tests).
func storeOpen(ctx context.Context, path string) (*store.Store, error) {
	return store.Open(ctx, path)
}
