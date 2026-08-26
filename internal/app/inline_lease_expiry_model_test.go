package app

import (
	"context"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_InlineLeaseExpiryIsAtomic(t *testing.T) {
	tests := []struct {
		name         string
		stage        domain.Stage
		expiryOffset int64
		standalone   bool
		wantReject   bool
	}{
		{name: "placed_expiry_before_logical_time", stage: domain.StagePlaced, expiryOffset: -1, wantReject: true},
		{name: "grouted_expiry_equal_to_logical_time", stage: domain.StageGrouted, expiryOffset: 0, wantReject: true},
		{name: "initial_tightened_expiry_before_logical_time", stage: domain.StageInitialTightened, expiryOffset: -1, wantReject: true},
		{name: "final_tightened_expiry_equal_to_logical_time", stage: domain.StageFinalTightened, expiryOffset: 0, wantReject: true},
		{name: "valid_inline_lease_after_non_overlapping_lease", stage: domain.StagePlaced, expiryOffset: 20},
		{name: "standalone_acquire_keeps_expiry_boundary_rejection", expiryOffset: 0, standalone: true, wantReject: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newTestService(t)

			if tt.standalone {
				_, err := svc.AcquireLease(ctx, domain.LeaseAcquireRequest{
					OperationID: "standalone-expired", Kind: domain.ResourceHoist, ResourceID: "H1",
					Holder: "operator", PositionID: "P1", Generation: 1, LogicalTime: 100, Expiry: 100 + tt.expiryOffset,
				})
				if !domain.IsBusinessError(err, domain.CodeInvalidRequest) {
					t.Fatalf("standalone acquire error = %v, want %s", err, domain.CodeInvalidRequest)
				}
				return
			}

			lockUnit(t, svc, "U1", 1)
			registerBearing(t, svc, "B1", "LRB-500", "batch-1")
			registerShim(t, svc, "S1", 100)
			registerLot(t, svc, "L1", "U1", 1000, 10000)

			for stage := domain.StageIncomingAccepted; stage < tt.stage; stage++ {
				req := op(stage, "setup-"+stage.String(), int64(stage+1)*10)
				switch stage {
				case domain.StageIncomingAccepted:
					req.ComponentID = "B1"
				case domain.StageLeveled:
					req.ShimIDs = []string{"S1"}
				case domain.StageGrouted:
					req.GroutLotID = "L1"
					req.GroutGrams = 100
				}
				if _, err := svc.ApplyOperation(ctx, "U1", "P1", req); err != nil {
					t.Fatalf("setup stage %s: %v", stage, err)
				}
			}

			logicalTime := int64(tt.stage+1) * 10
			resourceKind := domain.ResourceTorqueTool
			if tt.stage == domain.StagePlaced {
				resourceKind = domain.ResourceHoist
			} else if tt.stage == domain.StageGrouted {
				resourceKind = domain.ResourceGroutStation
			}
			if !tt.wantReject {
				if _, err := svc.AcquireLease(ctx, domain.LeaseAcquireRequest{
					OperationID: "prior-lease", Kind: resourceKind, ResourceID: "resource-1",
					Holder: "prior", PositionID: "P1", Generation: 1,
					LogicalTime: 1, Expiry: logicalTime,
				}); err != nil {
					t.Fatalf("acquire prior non-overlapping lease: %v", err)
				}
			}

			before, err := svc.Unit(ctx, "U1")
			if err != nil {
				t.Fatalf("unit before operation: %v", err)
			}
			beforeEvents, err := svc.Events(ctx)
			if err != nil {
				t.Fatalf("events before operation: %v", err)
			}
			beforeLot, err := svc.store.LotBalance(ctx, "L1")
			if err != nil {
				t.Fatalf("lot before operation: %v", err)
			}

			req := op(tt.stage, "target-operation", logicalTime)
			req.LeaseKind = resourceKind
			req.LeaseResourceID = "resource-1"
			req.LeaseExpiry = logicalTime + tt.expiryOffset
			if tt.stage == domain.StageGrouted {
				req.GroutLotID = "L1"
				req.GroutGrams = 100
			}
			_, applyErr := svc.ApplyOperation(ctx, "U1", "P1", req)

			after, err := svc.Unit(ctx, "U1")
			if err != nil {
				t.Fatalf("unit after operation: %v", err)
			}
			afterEvents, err := svc.Events(ctx)
			if err != nil {
				t.Fatalf("events after operation: %v", err)
			}
			afterLot, err := svc.store.LotBalance(ctx, "L1")
			if err != nil {
				t.Fatalf("lot after operation: %v", err)
			}

			if tt.wantReject {
				if !domain.IsBusinessError(applyErr, domain.CodeInvalidRequest) {
					t.Fatalf("operation error = %v, want stable %s", applyErr, domain.CodeInvalidRequest)
				}
				if after.Positions[0].Stage != before.Positions[0].Stage || after.Positions[0].StageName != before.Positions[0].StageName {
					t.Fatalf("position advanced from %s to %s", before.Positions[0].StageName, after.Positions[0].StageName)
				}
				if len(after.Positions[0].Evidence) != len(before.Positions[0].Evidence) {
					t.Fatalf("evidence count changed from %d to %d", len(before.Positions[0].Evidence), len(after.Positions[0].Evidence))
				}
				if len(after.Leases) != len(before.Leases) {
					t.Fatalf("lease count changed from %d to %d", len(before.Leases), len(after.Leases))
				}
				if afterEvents != beforeEvents {
					t.Fatalf("domain event count changed from %d to %d", beforeEvents, afterEvents)
				}
				if afterLot.Remaining != beforeLot.Remaining {
					t.Fatalf("lot balance changed from %d to %d", beforeLot.Remaining, afterLot.Remaining)
				}
				return
			}

			if applyErr != nil {
				t.Fatalf("valid inline lease operation: %v", applyErr)
			}
			if after.Positions[0].Stage != tt.stage || len(after.Positions[0].Evidence) != len(before.Positions[0].Evidence)+1 {
				t.Fatalf("stage/evidence not committed together: stage=%s evidence=%d", after.Positions[0].StageName, len(after.Positions[0].Evidence))
			}
			if len(after.Leases) != len(before.Leases)+1 || afterEvents != beforeEvents+1 {
				t.Fatalf("lease/event not committed together: leases=%d events=%d", len(after.Leases), afterEvents)
			}
		})
	}
}
