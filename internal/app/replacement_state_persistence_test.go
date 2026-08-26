package app

import (
	"context"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_ReplacementPersistsNewBearingAcrossReadModels(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	lockUnit(t, svc, "U1", 1)
	registerBearing(t, svc, "B1", "LRB-500", "batch-1")
	registerBearing(t, svc, "B2", "LRB-500", "batch-2")

	bind := op(domain.StageIncomingAccepted, "bind-B1", 1)
	bind.ComponentID = "B1"
	if _, err := svc.ApplyOperation(ctx, "U1", "P1", bind); err != nil {
		t.Fatalf("bind old bearing: %v", err)
	}

	replacements, err := svc.Replace(ctx, "U1", domain.ReplacementRequest{
		OperationID: "replace-B1-with-B2",
		Positions: []domain.ReplacementItem{{
			PositionID:             "P1",
			NewComponentID:         "B2",
			NewManufactureBatch:    "batch-2",
			NewConstructionSummary: "summary",
			NewModel:               "LRB-500",
			OldDestination:         "scrap",
		}},
	})
	if err != nil {
		t.Fatalf("replace bearing: %v", err)
	}

	unit, err := svc.Unit(ctx, "U1")
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	lineage, err := svc.Lineage(ctx, "U1")
	if err != nil {
		t.Fatalf("read lineage: %v", err)
	}
	oldBearing, err := svc.Store().Component(ctx, "B1")
	if err != nil {
		t.Fatalf("read old bearing: %v", err)
	}
	stale := op(domain.StageBaseAccepted, "late-generation-one-receipt", 2)
	stale.ExpectedGeneration = 1
	_, staleErr := svc.ApplyOperation(ctx, "U1", "P1", stale)

	findGeneration := func(positions []domain.PositionView, generation int) (domain.PositionView, bool) {
		for _, position := range positions {
			if position.PositionID == "P1" && position.Generation == generation {
				return position, true
			}
		}
		return domain.PositionView{}, false
	}

	cases := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "replacement returns the new generation and old destination",
			check: func(t *testing.T) {
				if len(replacements) != 1 || replacements[0].Generation != 2 || replacements[0].OldComponentID != "B1" || replacements[0].OldDestination != "scrap" {
					t.Fatalf("replacement = %+v", replacements)
				}
			},
		},
		{
			name: "component balance binds B2 to P1",
			check: func(t *testing.T) {
				for _, component := range unit.ComponentBalance {
					if component.ID == "B2" && component.PositionID == "P1" {
						return
					}
				}
				t.Fatalf("B2 binding missing from component balance: %+v", unit.ComponentBalance)
			},
		},
		{
			name: "unit projects B2 at P1 in generation two",
			check: func(t *testing.T) {
				position, ok := findGeneration(unit.Positions, 2)
				if !ok || position.ComponentID != "B2" {
					t.Fatalf("generation two position = %+v, found = %v", position, ok)
				}
			},
		},
		{
			name: "lineage records and projects the replacement",
			check: func(t *testing.T) {
				hasEvent := false
				for _, event := range lineage.Events {
					if event.PositionID == "P1" && event.Generation == 2 && event.ComponentID == "B2" && event.Kind == "replaced" {
						hasEvent = true
						break
					}
				}
				position, hasPosition := findGeneration(lineage.Positions, 2)
				if !hasEvent || !hasPosition || position.ComponentID != "B2" {
					t.Fatalf("replacement event = %v, generation two position = %+v, found = %v", hasEvent, position, hasPosition)
				}
			},
		},
		{
			name: "new generation remains unstarted",
			check: func(t *testing.T) {
				position, ok := findGeneration(unit.Positions, 2)
				if !ok || position.StageName != "unstarted" || len(position.Evidence) != 0 {
					t.Fatalf("generation two progress = %+v, found = %v", position, ok)
				}
			},
		},
		{
			name: "old generation receipt conflicts",
			check: func(t *testing.T) {
				if !domain.IsBusinessError(staleErr, domain.CodeGenerationConflict) {
					t.Fatalf("late receipt error = %v", staleErr)
				}
			},
		},
		{
			name: "old bearing keeps scrap destination",
			check: func(t *testing.T) {
				if oldBearing.CurrentPosition != "" || oldBearing.Destination != "scrap" {
					t.Fatalf("old bearing = %+v", oldBearing)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.check)
	}
}
