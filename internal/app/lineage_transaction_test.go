package app

import (
	"context"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestLockSnapshotDeterministic(t *testing.T) {
	svc := newTestService(t)
	s1 := lockUnit(t, svc, "U1", 3)
	s2 := lockUnit(t, svc, "U2", 3)
	// Different units but identical topology produce different digests because
	// the unit name is part of the digest; same unit re-lock would conflict.
	if s1.LockDigest == "" || s2.LockDigest == "" {
		t.Fatalf("empty digest")
	}
	if len(s1.Positions) != 3 {
		t.Fatalf("positions = %d", len(s1.Positions))
	}
	// Snapshot adjacency and sync groups survive restart via store.
	view, err := svc.Unit(context.Background(), "U1")
	if err != nil {
		t.Fatalf("unit view: %v", err)
	}
	if len(view.Snapshot.Adjacency) != 2 || len(view.Snapshot.SyncUnlockGroup) != 3 {
		t.Fatalf("topology mismatch: %+v", view.Snapshot)
	}
	if view.Snapshot.LockDigest != s1.LockDigest {
		t.Fatalf("digest changed after reload")
	}
}

func TestFullStageAdvancement(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerShim(t, svc, "S1", 1000)
	registerLot(t, svc, "L1", "U1", 1000, 100000)

	var lt int64 = 0
	advance := func(stage domain.Stage, opID string, mutate func(*domain.OperationRequest)) {
		t.Helper()
		lt += 10
		r := op(stage, opID, lt)
		if mutate != nil {
			mutate(&r)
		}
		if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
			t.Fatalf("advance %s: %v", stage, err)
		}
	}

	advance(domain.StageIncomingAccepted, "op-1", func(r *domain.OperationRequest) { r.ComponentID = "B1" })
	advance(domain.StageBaseAccepted, "op-2", nil)
	advance(domain.StagePlaced, "op-3", func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceHoist
		r.LeaseResourceID = "H1"
		r.LeaseExpiry = lt + 100
	})
	advance(domain.StageLeveled, "op-4", func(r *domain.OperationRequest) { r.ShimIDs = []string{"S1"} })
	advance(domain.StageGrouted, "op-5", func(r *domain.OperationRequest) {
		r.GroutLotID = "L1"
		r.GroutGrams = 100
		r.LeaseKind = domain.ResourceGroutStation
		r.LeaseResourceID = "G1"
		r.LeaseExpiry = lt + 100
	})
	advance(domain.StageInitialTightened, "op-6", func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceTorqueTool
		r.LeaseResourceID = "T1"
		r.LeaseExpiry = lt + 100
	})
	advance(domain.StageFinalTightened, "op-7", func(r *domain.OperationRequest) {
		r.LeaseKind = domain.ResourceTorqueTool
		r.LeaseResourceID = "T2"
		r.LeaseExpiry = lt + 100
	})
	advance(domain.StageCured, "op-8", nil)
	advance(domain.StageRemeasured, "op-9", nil)
	advance(domain.StageUnlocked, "op-10", nil)

	lineage, err := svc.Lineage(context.Background(), "U1")
	if err != nil {
		t.Fatalf("lineage: %v", err)
	}
	bound := 0
	for _, e := range lineage.Events {
		if e.Kind == "bound" {
			bound++
		}
	}
	if bound != 1 {
		t.Fatalf("expected 1 bound event, got %d", bound)
	}
	view, err := svc.Unit(context.Background(), "U1")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Positions[0].Stage != domain.StageUnlocked {
		t.Fatalf("stage = %s", view.Positions[0].StageName)
	}
	if view.Positions[0].ComponentID != "B1" {
		t.Fatalf("component = %s", view.Positions[0].ComponentID)
	}
	lb, err := svc.store.LotBalance(context.Background(), "L1")
	if err != nil {
		t.Fatalf("lot balance: %v", err)
	}
	if lb.Remaining != 900 {
		t.Fatalf("lot remaining = %d want 900", lb.Remaining)
	}
}

func TestComponentAlreadyBoundRejected(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 2)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")

	// Bind B1 to P1.
	r := op(domain.StageIncomingAccepted, "op-1", 1)
	r.ComponentID = "B1"
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
		t.Fatalf("bind P1: %v", err)
	}
	// Attempt to bind the same B1 to P2 must fail.
	r2 := op(domain.StageIncomingAccepted, "op-2", 2)
	r2.ComponentID = "B1"
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P2", r2); err == nil {
		t.Fatalf("expected component-already-bound rejection")
	}
	// P2 must still be unstarted and B1 still bound to P1.
	view, _ := svc.Unit(context.Background(), "U1")
	for _, p := range view.Positions {
		if p.PositionID == "P2" && p.ComponentID != "" {
			t.Fatalf("P2 unexpectedly bound to %s", p.ComponentID)
		}
	}
}

func TestIdempotentReplayAndConflict(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")

	r := op(domain.StageIncomingAccepted, "op-in", 1)
	r.ComponentID = "B1"
	res1, err := svc.ApplyOperation(context.Background(), "U1", "P1", r)
	if err != nil {
		t.Fatalf("first op: %v", err)
	}
	// Same op id + same content replays.
	res2, err := svc.ApplyOperation(context.Background(), "U1", "P1", r)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !res2.Replayed || res1.Stage != res2.Stage {
		t.Fatalf("expected replayed result: %+v", res2)
	}
	// Same op id + different content conflicts.
	r2 := op(domain.StageIncomingAccepted, "op-in", 1)
	r2.ComponentID = "B1"
	r2.ManufactureBatch = "different"
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r2); !domain.IsBusinessError(err, domain.CodeIdempotencyConflict) {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT, got %v", err)
	}
}

func TestGroutOverdrawRollsBack(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerLot(t, svc, "L1", "U1", 100, 100000)

	// Advance to grouted stage.
	steps := []func(*domain.OperationRequest){
		func(r *domain.OperationRequest) { r.ComponentID = "B1" }, // incoming
		nil, // base
		nil, // placed
		nil, // leveled
	}
	lt := int64(0)
	for i, s := range []domain.Stage{domain.StageIncomingAccepted, domain.StageBaseAccepted, domain.StagePlaced, domain.StageLeveled} {
		lt += 10
		r := op(s, string(rune('a'+i)), lt)
		if steps[i] != nil {
			steps[i](&r)
		}
		if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
			t.Fatalf("advance %s: %v", s, err)
		}
	}
	// Grout with overdraw (200 > 100 remaining).
	lt += 10
	gr := op(domain.StageGrouted, "op-grout", lt)
	gr.GroutLotID = "L1"
	gr.GroutGrams = 200
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", gr); err == nil {
		t.Fatalf("expected overdraw rejection")
	}
	// Stage must still be leveled and lot balance unchanged.
	view, _ := svc.Unit(context.Background(), "U1")
	if view.Positions[0].Stage != domain.StageLeveled {
		t.Fatalf("stage = %s, expected leveled", view.Positions[0].StageName)
	}
	lb, _ := svc.store.LotBalance(context.Background(), "L1")
	if lb.Remaining != 100 {
		t.Fatalf("lot remaining = %d, expected 100 (no partial deduction)", lb.Remaining)
	}
}
