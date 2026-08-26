package app

import (
	"context"
	"fmt"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestImpactClosureMergesAdjacencyBatchAndGroup(t *testing.T) {
	svc := newTestService(t)

	// Custom topology: P1-P2 adjacent, P3-P4 share a sync group, P1 and P3 share
	// a manufacture batch.
	up := domain.Orientation{X: 0, Y: 0, Z: 1, Scale: 1}
	down := domain.Orientation{X: 0, Y: 0, Z: -1, Scale: 1}
	pos := func(id string) domain.DesignPosition {
		return domain.DesignPosition{
			Building: "A", Unit: "U1", AxisGrid: id, PositionID: id,
			DesignCenter: domain.Point3{}, Orientation: up, BearingModel: "LRB-500",
			Upper:               domain.ConnectionInterface{ID: "u", Orientation: up, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
			Lower:               domain.ConnectionInterface{ID: "l", Orientation: down, PlateWidth: 600000, PlateLength: 600000, HoleCount: 4, HolePattern: "square-200"},
			AllowedEccentricity: 5000, AllowedTilt: 1000, TiltScale: 3,
			MaxShimThickness: 20000, MaxShimLayers: 4,
		}
	}
	req := domain.LockRequest{
		OperationID: "op-lock", Building: "A", Unit: "U1", SummaryVersion: "v1",
		Transform:       domain.PlanTransform{A: 1, B: 0, C: 0, D: 0, E: 1, F: 0, Scale: 1},
		Positions:       []domain.DesignPosition{pos("P1"), pos("P2"), pos("P3"), pos("P4")},
		Adjacency:       [][2]string{{"P1", "P2"}},
		SyncUnlockGroup: [][]string{{"P1"}, {"P2"}, {"P3", "P4"}},
	}
	if _, err := svc.LockDesign(context.Background(), req); err != nil {
		t.Fatalf("lock: %v", err)
	}
	registerBearing(t, svc, "B1", "LRB-500", "shared")
	registerBearing(t, svc, "B2", "LRB-500", "b2")
	registerBearing(t, svc, "B3", "LRB-500", "shared")
	registerBearing(t, svc, "B4", "LRB-500", "b4")
	// Bind components to make batches observable.
	for _, c := range []struct{ pos, id string }{{"P1", "B1"}, {"P2", "B2"}, {"P3", "B3"}, {"P4", "B4"}} {
		r := op(domain.StageIncomingAccepted, "bind-"+c.pos, 1)
		r.ComponentID = c.id
		if _, err := svc.ApplyOperation(context.Background(), "U1", c.pos, r); err != nil {
			t.Fatalf("bind %s: %v", c.pos, err)
		}
	}

	c1, err := svc.Impact(context.Background(), "U1", "P1", "batch performance failure")
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	got := map[string]bool{}
	for _, p := range c1.Positions {
		got[p] = true
	}
	for _, want := range []string{"P1", "P2", "P3", "P4"} {
		if !got[want] {
			t.Fatalf("closure missing %s: %v", want, c1.Positions)
		}
	}
	// Re-triggering produces the same digest (deduplicated).
	c2, err := svc.Impact(context.Background(), "U1", "P1", "batch performance failure")
	if err != nil {
		t.Fatalf("second impact: %v", err)
	}
	if c1.Digest != c2.Digest {
		t.Fatalf("digests differ: %s vs %s", c1.Digest, c2.Digest)
	}
}

func TestReplacementNewGenerationRejectsOldReceipt(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerBearing(t, svc, "B2", "LRB-500", "batch-2")

	// Bind B1.
	r := op(domain.StageIncomingAccepted, "op-in", 1)
	r.ComponentID = "B1"
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Replace P1 with B2.
	reps, err := svc.Replace(context.Background(), "U1", domain.ReplacementRequest{
		OperationID: "op-replace",
		Positions: []domain.ReplacementItem{{
			PositionID: "P1", NewComponentID: "B2", NewManufactureBatch: "batch-2",
			NewConstructionSummary: "summary", NewModel: "LRB-500", OldDestination: "scrap",
		}},
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(reps) != 1 || reps[0].Generation != 2 {
		t.Fatalf("replacement = %+v", reps)
	}

	// Old-generation receipt (gen 1) is rejected.
	old := op(domain.StageFinalTightened, "old-receipt", 5)
	old.ExpectedGeneration = 1
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", old); !domain.IsBusinessError(err, domain.CodeGenerationConflict) {
		t.Fatalf("expected GENERATION_CONFLICT for old receipt, got %v", err)
	}

	// Old component B1 recorded its destination; new generation is unstarted.
	comp, err := svc.store.Component(context.Background(), "B1")
	if err != nil {
		t.Fatalf("component B1: %v", err)
	}
	if comp.Destination != "scrap" {
		t.Fatalf("B1 destination = %s", comp.Destination)
	}
	row, err := svc.store.StageFor(context.Background(), "U1", "P1", 2)
	if err != nil {
		t.Fatalf("stage for gen 2: %v", err)
	}
	if row.Progress != 0 {
		t.Fatalf("gen 2 progress = %d, expected 0", row.Progress)
	}
}

func TestDualReviewRequiredForUnlock(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerShim(t, svc, "S1", 1000)
	registerLot(t, svc, "L1", "U1", 1000, 100000)
	advanceFullPosition(t, svc, "U1", "P1", "B1", "S1", "L1")

	// Same reviewer twice is not dual review.
	svc.SubmitReview(context.Background(), "U1", domain.ReviewRequest{
		OperationID: "r1", ReviewerID: "R1", Qualification: "qualified", Opinion: "pass", LogicalTime: 1,
	})
	svc.SubmitReview(context.Background(), "U1", domain.ReviewRequest{
		OperationID: "r2", ReviewerID: "R1", Qualification: "qualified", Opinion: "pass", LogicalTime: 2,
	})
	if _, err := svc.Unlock(context.Background(), "U1", "op-unlock", 3); err == nil {
		t.Fatalf("expected unlock rejection with single reviewer")
	}

	// A second distinct qualified reviewer allows unlock.
	svc.SubmitReview(context.Background(), "U1", domain.ReviewRequest{
		OperationID: "r3", ReviewerID: "R2", Qualification: "qualified", Opinion: "pass", LogicalTime: 3,
	})
	events, err := svc.Unlock(context.Background(), "U1", "op-unlock-2", 4)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("unlock events = %d", len(events))
	}
}

func TestTerminalSingleWinner(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerShim(t, svc, "S1", 1000)
	registerLot(t, svc, "L1", "U1", 1000, 100000)
	advanceFullPosition(t, svc, "U1", "P1", "B1", "S1", "L1")
	svc.SubmitReview(context.Background(), "U1", domain.ReviewRequest{OperationID: "r1", ReviewerID: "R1", Qualification: "qualified", Opinion: "pass", LogicalTime: 1})
	svc.SubmitReview(context.Background(), "U1", domain.ReviewRequest{OperationID: "r2", ReviewerID: "R2", Qualification: "qualified", Opinion: "pass", LogicalTime: 2})
	if _, err := svc.Unlock(context.Background(), "U1", "op-unlock", 3); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	// Competing terminal decisions: only the first wins, the rest see the same
	// already-decided outcome.
	first, err := svc.DecideTerminal(context.Background(), "U1", domain.TerminalHandover, "op-t1")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	second, err := svc.DecideTerminal(context.Background(), "U1", domain.TerminalQuarantine, "op-t2")
	if err != nil {
		t.Fatalf("second terminal: %v", err)
	}
	if first.Kind != second.Kind || first.CredentialDigest != second.CredentialDigest {
		t.Fatalf("terminal not single-winner: %+v vs %+v", first, second)
	}
	if first.Kind != domain.TerminalHandover || first.CredentialDigest == "" {
		t.Fatalf("bad terminal: %+v", first)
	}
}

func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	dbPath := fmt.Sprintf("%s/benzhi.db", dir)
	ctx := context.Background()

	// First run.
	st1, err := storeOpen(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	svc1 := New(st1)
	lockUnit(t, svc1, "U1", 1)
	registerBearing(t, svc1, "B1", "LRB-500", "batch-1")
	registerShim(t, svc1, "S1", 1000)
	registerLot(t, svc1, "L1", "U1", 1000, 100000)
	advanceFullPosition(t, svc1, "U1", "P1", "B1", "S1", "L1")
	svc1.SubmitReview(ctx, "U1", domain.ReviewRequest{OperationID: "r1", ReviewerID: "R1", Qualification: "qualified", Opinion: "pass", LogicalTime: 1})
	svc1.SubmitReview(ctx, "U1", domain.ReviewRequest{OperationID: "r2", ReviewerID: "R2", Qualification: "qualified", Opinion: "pass", LogicalTime: 2})
	if _, err := svc1.Unlock(ctx, "U1", "op-unlock", 3); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	term, err := svc1.DecideTerminal(ctx, "U1", domain.TerminalHandover, "op-t")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	eventsBefore, _ := svc1.Events(ctx)
	st1.Close()

	// Reopen and verify reconstruction.
	st2, err := storeOpen(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	svc2 := New(st2)
	view, err := svc2.Unit(ctx, "U1")
	if err != nil {
		t.Fatalf("view after restart: %v", err)
	}
	if view.Positions[0].Stage != domain.StageUnlocked {
		t.Fatalf("stage after restart = %s", view.Positions[0].StageName)
	}
	if view.Positions[0].ComponentID != "B1" {
		t.Fatalf("component after restart = %s", view.Positions[0].ComponentID)
	}
	lb, _ := svc2.store.LotBalance(ctx, "L1")
	if lb.Remaining != 900 {
		t.Fatalf("lot after restart = %d", lb.Remaining)
	}
	got, err := svc2.Terminal(ctx, "U1")
	if err != nil || got == nil || got.CredentialDigest != term.CredentialDigest {
		t.Fatalf("terminal after restart = %+v err=%v", got, err)
	}
	eventsAfter, _ := svc2.Events(ctx)
	if eventsAfter != eventsBefore {
		t.Fatalf("events mismatch after restart: %d vs %d", eventsAfter, eventsBefore)
	}
}
