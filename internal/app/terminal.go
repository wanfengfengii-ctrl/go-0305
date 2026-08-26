package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// SubmitReview records an independent re-check by a qualified person for a
// unit. A review is accepted when the reviewer is qualified; opinions are
// appended in logical time order.
func (s *Service) SubmitReview(ctx context.Context, unit string, req domain.ReviewRequest) (domain.Review, error) {
	if req.ReviewerID == "" || req.Qualification == "" {
		return domain.Review{}, domain.NewBusinessError(domain.CodeInvalidRequest, "reviewer and qualification required", req.OperationID, "reviewer")
	}
	if req.Qualification != "qualified" {
		return domain.Review{}, domain.NewBusinessError(domain.CodeInvalidRequest, "reviewer not qualified", req.OperationID, "qualification")
	}
	r := domain.Review{
		ID:            fmt.Sprintf("review-%s-%d-%s", req.ReviewerID, req.LogicalTime, digest(req.Opinion)[:8]),
		Unit:          unit,
		ReviewerID:    req.ReviewerID,
		Qualification: req.Qualification,
		Opinion:       req.Opinion,
		LogicalTime:   req.LogicalTime,
	}
	err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.InsertReviewTx(ctx, tx, r)
	})
	if err != nil {
		return domain.Review{}, mapErr(err, req.OperationID, "review conflict")
	}
	return r, nil
}

// Unlock releases transport locks and temporary constraints in the locked
// sync-group order. It requires every position to have reached the remeasured
// stage and two distinct qualified reviewers to have passed. A re-sent unlock
// (for example after a client disconnect) replays the committed batch rather
// than appending a second set of events, so the outcome stays idempotent.
func (s *Service) Unlock(ctx context.Context, unit string, operationID string, logicalTime int64) ([]domain.UnlockEvent, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return nil, err
	}
	requestDigest := digest(unlockRequest{Unit: unit, OperationID: operationID, LogicalTime: logicalTime})

	// Idempotency replay is resolved before the precondition checks so a
	// repeated successful unlock replays its committed batch instead of
	// appending a duplicate set of events.
	if rec, err := s.store.LookupIdempotency(ctx, unlockScope(unit), operationID); err == nil {
		if rec.RequestDigest == requestDigest {
			return s.replayUnlock(ctx, unit, rec.LogicalTime)
		}
		return nil, domain.NewBusinessError(domain.CodeIdempotencyConflict, "unlock id reused with different content", operationID, "content mismatch")
	}

	snap, err := s.store.Snapshot(ctx, unit, gen)
	if err != nil {
		return nil, mapErr(err, operationID, "snapshot")
	}

	// All positions must have reached the remeasured stage (or beyond).
	rows, err := s.store.PositionsByUnit(ctx, unit)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		reached, _ := r.Reached()
		if reached < domain.StageRemeasured {
			return nil, domain.NewBusinessError(domain.CodeGenerationConflict, "not all positions remeasured", operationID, r.PositionID)
		}
	}

	// Two distinct qualified reviewers with a passing opinion.
	reviews, err := s.store.Reviews(ctx, unit)
	if err != nil {
		return nil, err
	}
	passers := map[string]bool{}
	for _, r := range reviews {
		if r.Qualification == "qualified" && r.Opinion == "pass" {
			passers[r.ReviewerID] = true
		}
	}
	if len(passers) < 2 {
		return nil, domain.NewBusinessError(domain.CodeInvalidRequest, "dual independent review required", operationID, "reviewers")
	}

	var events []domain.UnlockEvent
	err = s.store.Tx(ctx, func(tx store.DBTx) error {
		for gi, group := range snap.SyncUnlockGroup {
			for _, pos := range group {
				e := domain.UnlockEvent{
					Unit: unit, GroupIndex: gi, PositionID: pos,
					LockDest: "recycled", LogicalTime: logicalTime,
				}
				if err := store.InsertUnlockTx(ctx, tx, e); err != nil {
					return err
				}
				events = append(events, e)
			}
		}
		if _, err := store.AppendEventTx(ctx, tx, domain.DomainEvent{
			Unit: unit, Type: "unlock", PayloadDigest: digest(snap.SyncUnlockGroup), LogicalTime: logicalTime,
		}); err != nil {
			return err
		}
		if err := store.InsertIdempotencyTx(ctx, tx, domain.IdempotencyRecord{
			Scope: unlockScope(unit), OperationID: operationID,
			RequestDigest: requestDigest, ResponseDigest: requestDigest, LogicalTime: logicalTime,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(err, operationID, "unlock failed")
	}
	return events, nil
}

// replayUnlock returns the previously committed unlock batch for an idempotent
// re-send. The batch is identified by the logical time recorded in the
// idempotency record, which is part of the matched request digest.
func (s *Service) replayUnlock(ctx context.Context, unit string, logicalTime int64) ([]domain.UnlockEvent, error) {
	all, err := s.store.Unlocks(ctx, unit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.UnlockEvent, 0, len(all))
	for _, e := range all {
		if e.LogicalTime == logicalTime {
			out = append(out, e)
		}
	}
	return out, nil
}

// unlockRequest is the idempotency input for an unlock call.
type unlockRequest struct {
	Unit        string
	OperationID string
	LogicalTime int64
}

// unlockScope builds the idempotency scope for a unit-level unlock. The
// colon-delimited form avoids colliding with the unit/position scope keys used
// by stage operations.
func unlockScope(unit string) string {
	return fmt.Sprintf("unlock:%s", unit)
}

// DecideTerminal competes for the single-writer terminal outcome. Handover
// additionally verifies unlock completion and produces a deterministic,
// rebuildable credential digest. Concurrent handover/quarantine/cancel requests
// resolve to exactly one winner.
func (s *Service) DecideTerminal(ctx context.Context, unit string, kind domain.TerminalKind, operationID string) (domain.TerminalDecision, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return domain.TerminalDecision{}, err
	}
	snap, err := s.store.Snapshot(ctx, unit, gen)
	if err != nil {
		return domain.TerminalDecision{}, mapErr(err, operationID, "snapshot")
	}

	if kind == domain.TerminalHandover {
		unlocks, err := s.store.Unlocks(ctx, unit)
		if err != nil {
			return domain.TerminalDecision{}, err
		}
		if len(unlocks) == 0 {
			return domain.TerminalDecision{}, domain.NewBusinessError(domain.CodeGenerationConflict, "unlock not complete", operationID, "unlock")
		}
	}

	credential := terminalCredential(unit, kind, snap.LockDigest)
	d := domain.TerminalDecision{
		Unit: unit, Kind: kind, Version: 1, CredentialDigest: credential,
	}
	err = s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.InsertTerminalTx(ctx, tx, d)
	})
	if err != nil {
		// The single-writer barrier lost: report the already-decided outcome.
		existing, e := s.store.Terminal(ctx, unit)
		if e != nil {
			return domain.TerminalDecision{}, mapErr(err, operationID, "terminal conflict")
		}
		return existing, nil
	}
	return d, nil
}

// Terminal returns the current terminal decision for a unit, if any.
func (s *Service) Terminal(ctx context.Context, unit string) (*domain.TerminalDecision, error) {
	t, err := s.store.Terminal(ctx, unit)
	if err == store.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func terminalCredential(unit string, kind domain.TerminalKind, lockDigest string) string {
	h := sha256.New()
	h.Write([]byte(unit))
	h.Write([]byte{0})
	h.Write([]byte(string(kind)))
	h.Write([]byte{0})
	h.Write([]byte(lockDigest))
	return hex.EncodeToString(h.Sum(nil))
}
