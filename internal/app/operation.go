package app

import (
	"context"
	"fmt"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/metrics"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// ApplyOperation atomically advances a position to its next stage. It enforces
// idempotency, generation match, single-step prefix, stage-specific domain
// rules and, within one transaction, performs component binding, material
// deduction, lease acquisition and evidence appending.
func (s *Service) ApplyOperation(ctx context.Context, unit, positionID string, req domain.OperationRequest) (domain.OperationResult, error) {
	gen, err := s.resolvePositionGeneration(ctx, unit, positionID, req.ExpectedGeneration)
	if err != nil {
		return domain.OperationResult{}, err
	}
	row, err := s.store.StageFor(ctx, unit, positionID, gen)
	if err != nil {
		return domain.OperationResult{}, mapErr(err, req.OperationID, "position not found")
	}
	if !req.Stage.Valid() {
		return domain.OperationResult{}, domain.NewBusinessError(domain.CodeInvalidRequest, "invalid stage", req.OperationID, "stage")
	}
	requestDigest := digest(req)

	// Idempotency replay is resolved before the strict prefix check so a
	// repeated successful operation replays rather than failing on "stage
	// already reached".
	if rec, err := s.store.LookupIdempotency(ctx, scopeKey(unit, positionID), req.OperationID); err == nil {
		if rec.RequestDigest == requestDigest {
			return domain.OperationResult{
				OperationID: req.OperationID, Unit: unit, PositionID: positionID,
				Generation: gen, Stage: req.Stage, StageName: req.Stage.String(),
				ComponentID: row.ComponentID, Replayed: true,
			}, nil
		}
		return domain.OperationResult{}, domain.NewBusinessError(domain.CodeIdempotencyConflict, "operation id reused with different content", req.OperationID, "content mismatch")
	}

	if row.Progress > int(req.Stage) {
		return domain.OperationResult{}, domain.NewBusinessError(domain.CodeGenerationConflict, "stage already reached", req.OperationID, "stage prefix")
	}
	if row.Progress < int(req.Stage) {
		return domain.OperationResult{}, domain.NewBusinessError(domain.CodeGenerationConflict, "stage skipped", req.OperationID, "stage prefix")
	}

	snapGen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return domain.OperationResult{}, err
	}
	snap, err := s.store.Snapshot(ctx, unit, snapGen)
	if err != nil {
		return domain.OperationResult{}, mapErr(err, req.OperationID, "snapshot")
	}
	pos := findPosition(snap, positionID)
	if pos == nil {
		return domain.OperationResult{}, domain.NewBusinessError(domain.CodeNotFound, "position not in snapshot", req.OperationID, positionID)
	}
	// Read-only stage validation happens before the transaction so no partial
	// state can be produced by a rejection.
	if err := s.validateStage(ctx, pos, req); err != nil {
		return domain.OperationResult{}, err
	}

	var result domain.OperationResult
	err = s.store.Tx(ctx, func(tx store.DBTx) error {
		componentID, destination, err := s.commitStage(ctx, tx, unit, positionID, gen, row, req)
		if err != nil {
			return err
		}
		if err := store.AdvanceStageTx(ctx, tx, unit, positionID, gen, req.Stage, componentID, destination); err != nil {
			return err
		}
		if err := store.AppendEvidenceTx(ctx, tx, domain.StageEvidence{
			Unit: unit, PositionID: positionID, Generation: gen, Stage: req.Stage,
			Holder: req.Holder, LogicalTime: req.LogicalTime, PayloadDigest: requestDigest,
		}); err != nil {
			return err
		}
		if _, err := store.AppendEventTx(ctx, tx, domain.DomainEvent{
			Unit: unit, Type: "stage." + req.Stage.String(), PayloadDigest: requestDigest, LogicalTime: req.LogicalTime,
		}); err != nil {
			return err
		}
		if err := store.InsertIdempotencyTx(ctx, tx, domain.IdempotencyRecord{
			Scope: scopeKey(unit, positionID), OperationID: req.OperationID,
			RequestDigest: requestDigest, ResponseDigest: requestDigest, LogicalTime: req.LogicalTime,
		}); err != nil {
			return err
		}
		result = domain.OperationResult{
			OperationID: req.OperationID, Unit: unit, PositionID: positionID,
			Generation: gen, Stage: req.Stage, StageName: req.Stage.String(),
			ComponentID: componentID,
		}
		return nil
	})
	if err != nil {
		return domain.OperationResult{}, mapErr(err, req.OperationID, "operation rejected")
	}
	return result, nil
}

// resolvePositionGeneration determines the effective replacement generation for
// an operation on a position. A stale generation (an old receipt after a
// replacement) is rejected with GENERATION_CONFLICT.
func (s *Service) resolvePositionGeneration(ctx context.Context, unit, positionID string, expected int) (int, error) {
	latest, err := s.store.PositionLatestGeneration(ctx, unit, positionID)
	if err != nil {
		return 0, err
	}
	if latest == 0 {
		return 0, domain.NewBusinessError(domain.CodeNotFound, "position not found", "", "position")
	}
	if expected == 0 {
		return latest, nil
	}
	if expected != latest {
		return 0, domain.NewBusinessError(domain.CodeGenerationConflict, "generation mismatch", "", "generation")
	}
	return expected, nil
}

// resolveSnapshotGeneration returns the design snapshot generation for a unit.
func (s *Service) resolveSnapshotGeneration(ctx context.Context, unit string) (int, error) {
	latest, err := s.store.LatestGeneration(ctx, unit)
	if err != nil {
		return 0, err
	}
	if latest == 0 {
		return 0, domain.NewBusinessError(domain.CodeNotFound, "unit not locked", "", "unit")
	}
	return latest, nil
}

// validateStage performs the read-only, stage-specific domain checks.
func (s *Service) validateStage(ctx context.Context, pos *domain.DesignPosition, req domain.OperationRequest) error {
	if req.InstrumentCallID != "" {
		ok, err := s.SuccessfulCall(ctx, req.InstrumentCallID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.NewBusinessError(domain.CodeInvalidRequest, "instrument call is not successful", req.OperationID, "instrument_call_id")
		}
	}
	// An inline lease carried by a stage operation must not already be expired
	// at the operation's logical time, mirroring the standalone AcquireLease
	// guard. Rejecting before the transaction keeps the position un-advanced.
	if req.LeaseKind != "" && req.LeaseExpiry <= req.LogicalTime {
		return domain.NewBusinessError(domain.CodeInvalidRequest, "expiry must be after logical time", req.OperationID, "expiry")
	}
	switch req.Stage {
	case domain.StageIncomingAccepted:
		if req.ComponentID == "" {
			return domain.NewBusinessError(domain.CodeInvalidRequest, "component id required", req.OperationID, "component_id")
		}
		comp, err := s.store.Component(ctx, req.ComponentID)
		if err != nil {
			return mapErr(err, req.OperationID, "component not found")
		}
		if comp.Kind != domain.KindBearing {
			return domain.NewBusinessError(domain.CodeInvalidRequest, "component is not a bearing", req.OperationID, "component kind")
		}
		if comp.Model != pos.BearingModel {
			return domain.NewBusinessError(domain.CodeInvalidGeometry, "bearing model mismatch", req.OperationID, "model")
		}
	case domain.StageLeveled:
		var total int64
		for _, id := range req.ShimIDs {
			comp, err := s.store.Component(ctx, id)
			if err != nil {
				return mapErr(err, req.OperationID, "shim not found")
			}
			if comp.Kind != domain.KindShim {
				return domain.NewBusinessError(domain.CodeInvalidRequest, "component is not a shim", req.OperationID, "shim kind")
			}
			total, err = addChecked(total, comp.ThicknessMicron)
			if err != nil {
				return domain.NewBusinessError(domain.CodeArithmeticOverflow, "shim thickness overflow", req.OperationID, "shim thickness")
			}
		}
		if len(req.ShimIDs) > pos.MaxShimLayers {
			return domain.NewBusinessError(domain.CodeInvalidGeometry, "too many shim layers", req.OperationID, "shim layers")
		}
		if total > pos.MaxShimThickness {
			return domain.NewBusinessError(domain.CodeInvalidGeometry, "shim stack exceeds limit", req.OperationID, "shim thickness")
		}
	case domain.StageGrouted:
		if req.GroutLotID == "" {
			return domain.NewBusinessError(domain.CodeInvalidRequest, "grout lot required", req.OperationID, "grout_lot_id")
		}
	case domain.StageRemeasured:
		return validateMetrics(pos, req)
	}
	return nil
}

// validateMetrics computes the derived fixed-point metrics and rejects any
// out-of-bound result. It is pure and side-effect free.
func validateMetrics(pos *domain.DesignPosition, req domain.OperationRequest) error {
	ecc, err := metrics.Eccentricity(pos.DesignCenter.X, pos.DesignCenter.Y, req.MeasuredX, req.MeasuredY, 0)
	if err != nil {
		return domain.NewBusinessError(domain.CodeArithmeticOverflow, "eccentricity overflow", req.OperationID, "eccentricity")
	}
	if ecc > pos.AllowedEccentricity {
		return domain.NewBusinessError(domain.CodeInvalidGeometry, "eccentricity exceeds allowance", req.OperationID, "eccentricity")
	}
	tilt, err := metrics.CompositeTilt(req.TiltX, req.TiltY, pos.TiltScale)
	if err != nil {
		return domain.NewBusinessError(domain.CodeArithmeticOverflow, "tilt overflow", req.OperationID, "tilt")
	}
	if tilt > pos.AllowedTilt {
		return domain.NewBusinessError(domain.CodeInvalidGeometry, "tilt exceeds allowance", req.OperationID, "tilt")
	}
	if req.NominalArea != 0 {
		ratio, err := metrics.BearingRatio(req.BearingArea, req.NominalArea, 0)
		if err != nil {
			return domain.NewBusinessError(domain.CodeArithmeticOverflow, "bearing ratio error", req.OperationID, "bearing ratio")
		}
		if ratio > 100 {
			return domain.NewBusinessError(domain.CodeInvalidGeometry, "bearing ratio out of range", req.OperationID, "bearing ratio")
		}
	}
	return nil
}

// commitStage performs the write-only side effects for a stage inside the
// transaction, returning the component id and destination to persist.
func (s *Service) commitStage(ctx context.Context, tx store.DBTx, unit, positionID string, gen int, row store.StageRow, req domain.OperationRequest) (string, string, error) {
	switch req.Stage {
	case domain.StageIncomingAccepted:
		if err := store.BindComponentTx(ctx, tx, req.ComponentID, positionID, "bound"); err != nil {
			return "", "", mapErr(err, req.OperationID, "component already bound")
		}
		if err := store.AppendLineageTx(ctx, tx, domain.LineageEvent{
			Unit: unit, PositionID: positionID, ComponentID: req.ComponentID,
			Kind: "bound", Generation: gen, LogicalTime: req.LogicalTime,
		}); err != nil {
			return "", "", err
		}
		return req.ComponentID, "", nil
	case domain.StageGrouted:
		if err := store.DeductTx(ctx, tx, req.GroutLotID, req.GroutGrams, req.LogicalTime); err != nil {
			return "", "", mapErr(err, req.OperationID, "grout deduction failed")
		}
		if err := store.SetGroutLotTx(ctx, tx, unit, positionID, gen, req.GroutLotID); err != nil {
			return "", "", err
		}
		if req.LeaseKind != "" {
			if _, err := s.acquireLeaseTx(ctx, tx, unit, positionID, gen, req); err != nil {
				return "", "", err
			}
		}
		return row.ComponentID, row.Destination, nil
	case domain.StagePlaced, domain.StageInitialTightened, domain.StageFinalTightened:
		if req.LeaseKind != "" {
			if _, err := s.acquireLeaseTx(ctx, tx, unit, positionID, gen, req); err != nil {
				return "", "", err
			}
		}
		return row.ComponentID, row.Destination, nil
	default:
		return row.ComponentID, row.Destination, nil
	}
}

// acquireLeaseTx acquires a lease inside the transaction with a deterministic
// id derived from scope, resource and stage.
func (s *Service) acquireLeaseTx(ctx context.Context, tx store.DBTx, unit, positionID string, gen int, req domain.OperationRequest) (domain.ResourceLease, error) {
	leaseID := fmt.Sprintf("%s:%s:%s:%d", scopeKey(unit, positionID), req.LeaseKind, req.LeaseResourceID, int(req.Stage))
	l := domain.ResourceLease{
		ID: leaseID, Resource: req.LeaseKind, ResourceID: req.LeaseResourceID,
		Holder: req.Holder, PositionID: positionID, Generation: gen,
		AcquiredAt: req.LogicalTime, ExpiresAt: req.LeaseExpiry,
	}
	if err := store.InsertLeaseTx(ctx, tx, l); err != nil {
		return domain.ResourceLease{}, mapErr(err, req.OperationID, "lease conflict")
	}
	return l, nil
}

func addChecked(a, b int64) (int64, error) {
	r := a + b
	if (a > 0 && b > 0 && r < 0) || (a < 0 && b < 0 && r >= 0) {
		return 0, fmt.Errorf("overflow")
	}
	return r, nil
}

func findPosition(snap domain.DesignSnapshot, positionID string) *domain.DesignPosition {
	for i := range snap.Positions {
		if snap.Positions[i].PositionID == positionID {
			return &snap.Positions[i]
		}
	}
	return nil
}
