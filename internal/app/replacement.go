package app

import (
	"context"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// Replace creates a new replacement generation for the affected positions. It
// unbinds the old bearing (recording its destination), binds a new bearing and
// restarts the position's stage projection at the new generation. Late receipts
// from the old generation are rejected because the position generation has
// advanced.
func (s *Service) Replace(ctx context.Context, unit string, req domain.ReplacementRequest) ([]domain.ReplacementGeneration, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return nil, err
	}
	snap, err := s.store.Snapshot(ctx, unit, gen)
	if err != nil {
		return nil, mapErr(err, req.OperationID, "snapshot")
	}

	// Read-only pre-validation: positions exist, old components bound, new
	// components registered and model-matching.
	type plan struct {
		item      domain.ReplacementItem
		pos       *domain.DesignPosition
		oldCompID string
		newGen    int
	}
	var plans []plan
	for _, item := range req.Positions {
		pos := findPosition(snap, item.PositionID)
		if pos == nil {
			return nil, domain.NewBusinessError(domain.CodeNotFound, "position not in snapshot", req.OperationID, item.PositionID)
		}
		curGen, err := s.store.PositionLatestGeneration(ctx, unit, item.PositionID)
		if err != nil {
			return nil, mapErr(err, req.OperationID, "position generation")
		}
		row, err := s.store.StageFor(ctx, unit, item.PositionID, curGen)
		if err != nil || row.ComponentID == "" {
			return nil, domain.NewBusinessError(domain.CodeGenerationConflict, "position has no bound bearing", req.OperationID, item.PositionID)
		}
		nc, err := s.store.Component(ctx, item.NewComponentID)
		if err != nil {
			return nil, mapErr(err, req.OperationID, "new component not found")
		}
		if nc.Kind != domain.KindBearing || nc.Model != pos.BearingModel {
			return nil, domain.NewBusinessError(domain.CodeInvalidGeometry, "new bearing model mismatch", req.OperationID, item.PositionID)
		}
		plans = append(plans, plan{item: item, pos: pos, oldCompID: row.ComponentID, newGen: curGen + 1})
	}

	var out []domain.ReplacementGeneration
	err = s.store.Tx(ctx, func(tx store.DBTx) error {
		for _, p := range plans {
			// Unbind old bearing and record its destination.
			if err := store.UnbindComponentTx(ctx, tx, p.oldCompID, p.item.OldDestination, "removed"); err != nil {
				return mapErr(err, req.OperationID, "old bearing unbind")
			}
			if err := store.AppendLineageTx(ctx, tx, domain.LineageEvent{
				Unit: unit, PositionID: p.item.PositionID, ComponentID: p.oldCompID,
				Kind: "removed", Generation: p.newGen, LogicalTime: 0,
			}); err != nil {
				return err
			}
			// Bind the new bearing and start a fresh projection.
			if err := store.BindComponentTx(ctx, tx, p.item.NewComponentID, p.item.PositionID, "bound"); err != nil {
				return mapErr(err, req.OperationID, "new bearing bind")
			}
			if err := store.AppendLineageTx(ctx, tx, domain.LineageEvent{
				Unit: unit, PositionID: p.item.PositionID, ComponentID: p.item.NewComponentID,
				Kind: "replaced", Generation: p.newGen, LogicalTime: 0,
			}); err != nil {
				return err
			}
			if err := store.InsertPositionStageTx(ctx, tx, unit, p.item.PositionID, p.newGen, p.item.NewComponentID, ""); err != nil {
				return err
			}
			r := domain.ReplacementGeneration{
				Unit: unit, Generation: p.newGen, PositionID: p.item.PositionID,
				OldComponentID: p.oldCompID, OldDestination: p.item.OldDestination,
				ReviewResult: "pending",
			}
			if err := store.InsertReplacementTx(ctx, tx, r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
