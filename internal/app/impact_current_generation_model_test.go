package app

import (
	"context"
	"slices"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_ImpactClosureUsesOnlyCurrentPositionGeneration(t *testing.T) {
	tests := []struct {
		name     string
		relation string
	}{
		{name: "manufacture batch", relation: "batch"},
		{name: "grout lot", relation: "grout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newTestService(t)
			lock := testUnit("U1", 3)
			lock.Adjacency = nil
			lock.SyncUnlockGroup = [][]string{{"P1"}, {"P2"}, {"P3"}}
			if _, err := svc.LockDesign(ctx, lock); err != nil {
				t.Fatalf("lock design: %v", err)
			}

			batches := map[string]string{
				"P1-old": "old-p1", "P2": "p2", "P3": "p3", "P1-new": "new-p1",
			}
			if tt.relation == "batch" {
				batches["P1-old"], batches["P2"] = "superseded-batch", "superseded-batch"
				batches["P1-new"], batches["P3"] = "current-batch", "current-batch"
			}
			for _, id := range []string{"P1-old", "P2", "P3", "P1-new"} {
				registerBearing(t, svc, id, "LRB-500", batches[id])
			}

			apply := func(position string, generation int, stage domain.Stage, componentID, lotID string) {
				req := op(stage, position+"-g"+string(rune('0'+generation))+"-"+stage.String(), int64(generation*100+int(stage)))
				req.ExpectedGeneration = generation
				req.ComponentID = componentID
				if stage == domain.StageGrouted {
					req.GroutLotID = lotID
					req.GroutGrams = 1
				}
				if _, err := svc.ApplyOperation(ctx, "U1", position, req); err != nil {
					t.Fatalf("apply %s generation %d stage %s: %v", position, generation, stage, err)
				}
			}
			advanceToGrout := func(position string, generation int, componentID, lotID string) {
				for _, stage := range []domain.Stage{
					domain.StageIncomingAccepted,
					domain.StageBaseAccepted,
					domain.StagePlaced,
					domain.StageLeveled,
					domain.StageGrouted,
				} {
					apply(position, generation, stage, componentID, lotID)
				}
			}

			if tt.relation == "grout" {
				registerLot(t, svc, "superseded-lot", "U1", 10, 10000)
				registerLot(t, svc, "current-lot", "U1", 10, 10000)
				advanceToGrout("P1", 1, "P1-old", "superseded-lot")
				advanceToGrout("P2", 1, "P2", "superseded-lot")
				advanceToGrout("P3", 1, "P3", "current-lot")
			} else {
				apply("P1", 1, domain.StageIncomingAccepted, "P1-old", "")
				apply("P2", 1, domain.StageIncomingAccepted, "P2", "")
				apply("P3", 1, domain.StageIncomingAccepted, "P3", "")
			}

			if _, err := svc.Replace(ctx, "U1", domain.ReplacementRequest{
				OperationID: "replace-p1",
				Positions: []domain.ReplacementItem{{
					PositionID:             "P1",
					NewComponentID:         "P1-new",
					NewManufactureBatch:    batches["P1-new"],
					NewConstructionSummary: "replacement",
					NewModel:               "LRB-500",
					OldDestination:         "quarantine",
				}},
			}); err != nil {
				t.Fatalf("replace P1: %v", err)
			}
			if tt.relation == "grout" {
				advanceToGrout("P1", 2, "P1-new", "current-lot")
			} else {
				apply("P1", 2, domain.StageIncomingAccepted, "P1-new", "")
			}

			impact, err := svc.Impact(ctx, "U1", "P1", "replacement reinspection failed")
			if err != nil {
				t.Fatalf("impact: %v", err)
			}
			want := []string{"P1", "P3"}
			if !slices.Equal(impact.Positions, want) {
				t.Fatalf("impact positions = %v, want %v", impact.Positions, want)
			}

			replayed, err := svc.Impact(ctx, "U1", "P1", "replacement reinspection failed")
			if err != nil {
				t.Fatalf("repeat impact: %v", err)
			}
			if replayed.ID != impact.ID || replayed.Digest != impact.Digest {
				t.Fatalf("repeat impact was not deduplicated: first=%+v repeat=%+v", impact, replayed)
			}

			lineage, err := svc.Lineage(ctx, "U1")
			if err != nil {
				t.Fatalf("lineage: %v", err)
			}
			generations := map[int]bool{}
			oldRecorded := false
			for _, position := range lineage.Positions {
				if position.PositionID == "P1" {
					generations[position.Generation] = true
				}
			}
			for _, event := range lineage.Events {
				if event.PositionID == "P1" && event.ComponentID == "P1-old" && event.Kind == "removed" {
					oldRecorded = true
				}
			}
			if !generations[1] || !generations[2] || !oldRecorded {
				t.Fatalf("replacement history missing: generations=%v old removal recorded=%v", generations, oldRecorded)
			}
		})
	}
}
