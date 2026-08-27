package app

import (
	"context"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestLeaseCompetitionAndExpiry(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)

	a, err := svc.AcquireLease(context.Background(), domain.LeaseAcquireRequest{
		OperationID: "l1", Kind: domain.ResourceHoist, ResourceID: "H1",
		Holder: "op-a", PositionID: "P1", Generation: 1, LogicalTime: 100, Expiry: 200,
	})
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	// Overlapping window is busy.
	if _, err := svc.AcquireLease(context.Background(), domain.LeaseAcquireRequest{
		OperationID: "l2", Kind: domain.ResourceHoist, ResourceID: "H1",
		Holder: "op-b", PositionID: "P1", Generation: 1, LogicalTime: 150, Expiry: 250,
	}); !domain.IsBusinessError(err, domain.CodeLeaseBusy) {
		t.Fatalf("expected LEASE_BUSY, got %v", err)
	}
	// After expiry a new holder can acquire.
	b, err := svc.AcquireLease(context.Background(), domain.LeaseAcquireRequest{
		OperationID: "l3", Kind: domain.ResourceHoist, ResourceID: "H1",
		Holder: "op-c", PositionID: "P1", Generation: 1, LogicalTime: 250, Expiry: 350,
	})
	if err != nil {
		t.Fatalf("acquire after expiry: %v", err)
	}
	_ = a
	_ = b
}

func TestStageSkipRejected(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")

	// Jump straight from unstarted to leveled.
	r := op(domain.StageLeveled, "op-skip", 1)
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); !domain.IsBusinessError(err, domain.CodeGenerationConflict) {
		t.Fatalf("expected GENERATION_CONFLICT (skip), got %v", err)
	}
}

func TestExpiredGroutRejected(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	// Lot expires at logical time 50.
	registerLot(t, svc, "L1", "U1", 1000, 50)

	// Advance to grouted stage.
	steps := []struct {
		stage  domain.Stage
		mutate func(*domain.OperationRequest)
	}{
		{domain.StageIncomingAccepted, func(r *domain.OperationRequest) { r.ComponentID = "B1" }},
		{domain.StageBaseAccepted, nil},
		{domain.StagePlaced, nil},
		{domain.StageLeveled, nil},
	}
	lt := int64(0)
	for _, s := range steps {
		lt += 10
		r := op(s.stage, "op-"+s.stage.String(), lt)
		if s.mutate != nil {
			s.mutate(&r)
		}
		if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
			t.Fatalf("advance %s: %v", s.stage, err)
		}
	}
	// Grout at logical time 60 (past expiry 50).
	r := op(domain.StageGrouted, "op-grout", 60)
	r.GroutLotID = "L1"
	r.GroutGrams = 100
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err == nil {
		t.Fatalf("expected expired grout rejection")
	}
	lb, _ := svc.store.LotBalance(context.Background(), "L1")
	if lb.Remaining != 1000 {
		t.Fatalf("lot remaining = %d, expected 1000", lb.Remaining)
	}
}

func TestShimStackExceedsLimit(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	// Max shim layers is 4; register 5 shims.
	for i := 1; i <= 5; i++ {
		registerShim(t, svc, string(rune('A'+i)), 100)
	}
	// Advance to leveled stage.
	steps := []struct {
		stage  domain.Stage
		mutate func(*domain.OperationRequest)
	}{
		{domain.StageIncomingAccepted, func(r *domain.OperationRequest) { r.ComponentID = "B1" }},
		{domain.StageBaseAccepted, nil},
		{domain.StagePlaced, nil},
	}
	lt := int64(0)
	for _, s := range steps {
		lt += 10
		r := op(s.stage, "op-"+s.stage.String(), lt)
		if s.mutate != nil {
			s.mutate(&r)
		}
		if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); err != nil {
			t.Fatalf("advance %s: %v", s.stage, err)
		}
	}
	// Leveled with 5 shims exceeds the 4-layer limit.
	r := op(domain.StageLeveled, "op-level", lt+10)
	r.ShimIDs = []string{"B", "C", "D", "E", "F"}
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected shim-limit rejection, got %v", err)
	}
}

func TestModelMismatchRejected(t *testing.T) {
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-700", "batch-1") // wrong model

	r := op(domain.StageIncomingAccepted, "op-in", 1)
	r.ComponentID = "B1"
	if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", r); !domain.IsBusinessError(err, domain.CodeInvalidGeometry) {
		t.Fatalf("expected model-mismatch rejection, got %v", err)
	}
}
