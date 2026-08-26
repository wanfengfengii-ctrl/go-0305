package app

import (
	"context"
	"reflect"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_OperationIdempotencyPrecedesReplacementGeneration(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Service, domain.OperationRequest)
	}{
		{
			name: "identical retry replays generation one result",
			run: func(t *testing.T, svc *Service, original domain.OperationRequest) {
				got, err := svc.ApplyOperation(context.Background(), "U1", "P1", original)
				if err != nil {
					t.Fatalf("retry original operation: %v", err)
				}
				if !got.Replayed || got.Generation != 1 || got.Stage != domain.StageIncomingAccepted || got.ComponentID != "B1" {
					t.Fatalf("replayed result = %+v, want committed generation 1 B1 result", got)
				}
			},
		},
		{
			name: "changed retry remains an idempotency conflict",
			run: func(t *testing.T, svc *Service, original domain.OperationRequest) {
				changed := original
				changed.Payload = "different evidence"
				if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", changed); !domain.IsBusinessError(err, domain.CodeIdempotencyConflict) {
					t.Fatalf("changed retry error = %v, want IDEMPOTENCY_CONFLICT", err)
				}
			},
		},
		{
			name: "unrecorded old generation receipt remains rejected",
			run: func(t *testing.T, svc *Service, original domain.OperationRequest) {
				oldReceipt := original
				oldReceipt.OperationID = "op-not-recorded"
				if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", oldReceipt); !domain.IsBusinessError(err, domain.CodeGenerationConflict) {
					t.Fatalf("old receipt error = %v, want GENERATION_CONFLICT", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			lockUnit(t, svc, "U1", 1)
			registerBearing(t, svc, "B1", "LRB-500", "batch-1")
			registerBearing(t, svc, "B2", "LRB-500", "batch-2")

			original := op(domain.StageIncomingAccepted, "op-incoming", 1)
			original.ComponentID = "B1"
			if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", original); err != nil {
				t.Fatalf("commit original operation: %v", err)
			}
			if _, err := svc.Replace(context.Background(), "U1", domain.ReplacementRequest{
				OperationID: "op-replace",
				Positions: []domain.ReplacementItem{{
					PositionID: "P1", NewComponentID: "B2", NewManufactureBatch: "batch-2",
					NewConstructionSummary: "summary", NewModel: "LRB-500", OldDestination: "scrap",
				}},
			}); err != nil {
				t.Fatalf("replace B1 with B2: %v", err)
			}
			before, err := svc.Unit(context.Background(), "U1")
			if err != nil {
				t.Fatalf("read unit before retry: %v", err)
			}

			tc.run(t, svc, original)

			after, err := svc.Unit(context.Background(), "U1")
			if err != nil {
				t.Fatalf("read unit after retry: %v", err)
			}
			if !reflect.DeepEqual(after.Positions, before.Positions) {
				t.Fatalf("position projections changed by retry:\nbefore=%+v\nafter=%+v", before.Positions, after.Positions)
			}
			latest := after.Positions[len(after.Positions)-1]
			if latest.Generation != 2 || latest.StageName != "unstarted" {
				t.Fatalf("current generation advanced by retry: %+v", latest)
			}
		})
	}
}
