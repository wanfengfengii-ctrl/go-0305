package app

import (
	"context"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_ComponentOccupancyUsesCompleteBusinessPosition(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "same component cannot be rebound at a reused position id in another unit",
			run: func(t *testing.T) {
				svc := newTestService(t)
				lockUnit(t, svc, "U1", 1)
				lockUnit(t, svc, "U2", 1)
				registerBearing(t, svc, "B1", "LRB-500", "batch-1")

				first := op(domain.StageIncomingAccepted, "bind-u1-p1", 1)
				first.ComponentID = "B1"
				if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", first); err != nil {
					t.Fatalf("bind B1 to U1/P1: %v", err)
				}

				second := op(domain.StageIncomingAccepted, "bind-u2-p1", 2)
				second.ComponentID = "B1"
				if _, err := svc.ApplyOperation(context.Background(), "U2", "P1", second); err == nil {
					t.Fatal("binding B1 to U2/P1 succeeded after it was already bound to U1/P1")
				}

				lineage, err := svc.Lineage(context.Background(), "U2")
				if err != nil {
					t.Fatalf("read U2 lineage: %v", err)
				}
				if len(lineage.Events) != 0 {
					t.Fatalf("rejected binding left U2 lineage events: %+v", lineage.Events)
				}
				view, err := svc.Unit(context.Background(), "U2")
				if err != nil {
					t.Fatalf("read U2: %v", err)
				}
				if got := view.Positions[0].ComponentID; got != "" {
					t.Fatalf("rejected binding left B1 projected at U2/P1: %q", got)
				}
			},
		},
		{
			name: "different free components may use the same position id in different units",
			run: func(t *testing.T) {
				svc := newTestService(t)
				lockUnit(t, svc, "U1", 1)
				lockUnit(t, svc, "U2", 1)
				registerBearing(t, svc, "B1", "LRB-500", "batch-1")
				registerBearing(t, svc, "B2", "LRB-500", "batch-2")

				for _, binding := range []struct {
					unit, component, operation string
				}{{"U1", "B1", "bind-b1"}, {"U2", "B2", "bind-b2"}} {
					req := op(domain.StageIncomingAccepted, binding.operation, 1)
					req.ComponentID = binding.component
					if _, err := svc.ApplyOperation(context.Background(), binding.unit, "P1", req); err != nil {
						t.Fatalf("bind %s to %s/P1: %v", binding.component, binding.unit, err)
					}
				}
			},
		},
		{
			name: "identical operation replay remains idempotent",
			run: func(t *testing.T) {
				svc := newTestService(t)
				lockUnit(t, svc, "U1", 1)
				registerBearing(t, svc, "B1", "LRB-500", "batch-1")

				req := op(domain.StageIncomingAccepted, "bind-b1", 1)
				req.ComponentID = "B1"
				if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", req); err != nil {
					t.Fatalf("initial binding: %v", err)
				}
				result, err := svc.ApplyOperation(context.Background(), "U1", "P1", req)
				if err != nil {
					t.Fatalf("idempotent replay: %v", err)
				}
				if !result.Replayed {
					t.Fatalf("replay result was not marked replayed: %+v", result)
				}
			},
		},
		{
			name: "replacement may bind a genuinely free component",
			run: func(t *testing.T) {
				svc := newTestService(t)
				lockUnit(t, svc, "U1", 1)
				registerBearing(t, svc, "B1", "LRB-500", "batch-1")
				registerBearing(t, svc, "B2", "LRB-500", "batch-2")

				incoming := op(domain.StageIncomingAccepted, "bind-b1", 1)
				incoming.ComponentID = "B1"
				if _, err := svc.ApplyOperation(context.Background(), "U1", "P1", incoming); err != nil {
					t.Fatalf("initial binding: %v", err)
				}
				result, err := svc.Replace(context.Background(), "U1", domain.ReplacementRequest{
					OperationID: "replace-with-b2",
					Positions: []domain.ReplacementItem{{
						PositionID: "P1", NewComponentID: "B2", NewModel: "LRB-500", OldDestination: "quarantine",
					}},
				})
				if err != nil {
					t.Fatalf("replace with free B2: %v", err)
				}
				if len(result) != 1 || result[0].Generation != 2 {
					t.Fatalf("replacement result: %+v", result)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
