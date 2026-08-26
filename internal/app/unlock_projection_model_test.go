package app

import (
	"context"
	"fmt"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_UnlockKeepsEventsStagesAndAuditConsistent(t *testing.T) {
	tests := []struct {
		name             string
		positionCount    int
		stopBefore       domain.Stage
		reviewOpinions   []string
		wantErrorCode    string
		wantUnlockEvents int
	}{
		{
			name:             "successful unlock advances every released position",
			positionCount:    2,
			stopBefore:       domain.StageUnlocked,
			reviewOpinions:   []string{"pass", "pass"},
			wantUnlockEvents: 2,
		},
		{
			name:           "position not remeasured",
			positionCount:  1,
			stopBefore:     domain.StageRemeasured,
			reviewOpinions: []string{"pass", "pass"},
			wantErrorCode:  domain.CodeGenerationConflict,
		},
		{
			name:           "fewer than two reviewers",
			positionCount:  1,
			stopBefore:     domain.StageUnlocked,
			reviewOpinions: []string{"pass"},
			wantErrorCode:  domain.CodeInvalidRequest,
		},
		{
			name:           "second review did not pass",
			positionCount:  1,
			stopBefore:     domain.StageUnlocked,
			reviewOpinions: []string{"pass", "fail"},
			wantErrorCode:  domain.CodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newTestService(t)
			lockUnit(t, svc, "U1", tt.positionCount)

			for i := 1; i <= tt.positionCount; i++ {
				position := fmt.Sprintf("P%d", i)
				bearing := fmt.Sprintf("B%d", i)
				shim := fmt.Sprintf("S%d", i)
				lot := fmt.Sprintf("L%d", i)
				registerBearing(t, svc, bearing, "LRB-500", "batch-"+position)
				registerShim(t, svc, shim, 1000)
				registerLot(t, svc, lot, "U1", 1000, 100000)

				logicalTime := int64(0)
				stages := []domain.Stage{
					domain.StageIncomingAccepted,
					domain.StageBaseAccepted,
					domain.StagePlaced,
					domain.StageLeveled,
					domain.StageGrouted,
					domain.StageInitialTightened,
					domain.StageFinalTightened,
					domain.StageCured,
					domain.StageRemeasured,
				}
				for _, stage := range stages {
					if stage == tt.stopBefore {
						break
					}
					logicalTime += 10
					req := op(stage, fmt.Sprintf("%s-%s", stage, position), logicalTime)
					switch stage {
					case domain.StageIncomingAccepted:
						req.ComponentID = bearing
					case domain.StagePlaced:
						req.LeaseKind = domain.ResourceHoist
						req.LeaseResourceID = "H-" + position
						req.LeaseExpiry = logicalTime + 100
					case domain.StageLeveled:
						req.ShimIDs = []string{shim}
					case domain.StageGrouted:
						req.GroutLotID = lot
						req.GroutGrams = 100
						req.LeaseKind = domain.ResourceGroutStation
						req.LeaseResourceID = "G-" + position
						req.LeaseExpiry = logicalTime + 100
					case domain.StageInitialTightened, domain.StageFinalTightened:
						req.LeaseKind = domain.ResourceTorqueTool
						req.LeaseResourceID = fmt.Sprintf("T-%d-%s", stage, position)
						req.LeaseExpiry = logicalTime + 100
					}
					if _, err := svc.ApplyOperation(ctx, "U1", position, req); err != nil {
						t.Fatalf("advance %s to %s: %v", position, stage, err)
					}
				}
			}

			for i, opinion := range tt.reviewOpinions {
				_, err := svc.SubmitReview(ctx, "U1", domain.ReviewRequest{
					OperationID:   fmt.Sprintf("review-%d", i+1),
					ReviewerID:    fmt.Sprintf("R%d", i+1),
					Qualification: "qualified",
					Opinion:       opinion,
					LogicalTime:   int64(1000 + i),
				})
				if err != nil {
					t.Fatalf("submit review %d: %v", i+1, err)
				}
			}

			beforeEvents, err := svc.Events(ctx)
			if err != nil {
				t.Fatalf("count events before unlock: %v", err)
			}
			events, err := svc.Unlock(ctx, "U1", "unlock-U1", 2000)
			if tt.wantErrorCode != "" {
				if !domain.IsBusinessError(err, tt.wantErrorCode) {
					t.Fatalf("unlock error = %v, want %s", err, tt.wantErrorCode)
				}
				view, viewErr := svc.Unit(ctx, "U1")
				if viewErr != nil {
					t.Fatalf("get unit after rejected unlock: %v", viewErr)
				}
				if len(view.Unlocks) != 0 {
					t.Fatalf("rejected unlock persisted events: %+v", view.Unlocks)
				}
				afterEvents, countErr := svc.Events(ctx)
				if countErr != nil {
					t.Fatalf("count events after rejected unlock: %v", countErr)
				}
				if afterEvents != beforeEvents {
					t.Fatalf("rejected unlock changed audit event count from %d to %d", beforeEvents, afterEvents)
				}
				return
			}

			if err != nil {
				t.Fatalf("unlock: %v", err)
			}
			if len(events) != tt.wantUnlockEvents {
				t.Fatalf("unlock response has %d events, want %d", len(events), tt.wantUnlockEvents)
			}
			view, err := svc.Unit(ctx, "U1")
			if err != nil {
				t.Fatalf("get unit after unlock: %v", err)
			}
			if len(view.Unlocks) != tt.wantUnlockEvents {
				t.Fatalf("unit view has %d unlock events, want %d", len(view.Unlocks), tt.wantUnlockEvents)
			}
			for _, position := range view.Positions {
				if position.Stage != domain.StageUnlocked || position.StageName != "unlocked" {
					t.Errorf("position %s stage = %s (%d), want unlocked", position.PositionID, position.StageName, position.Stage)
				}
				last := position.Evidence[len(position.Evidence)-1]
				if last.Stage != domain.StageUnlocked || last.Holder != "unlock" || last.LogicalTime != 2000 {
					t.Errorf("position %s final evidence = %+v, want unlock audit evidence", position.PositionID, last)
				}
			}
			afterEvents, err := svc.Events(ctx)
			if err != nil {
				t.Fatalf("count events after unlock: %v", err)
			}
			if want := beforeEvents + tt.positionCount + 1; afterEvents != want {
				t.Fatalf("audit event count = %d, want %d", afterEvents, want)
			}
		})
	}
}
