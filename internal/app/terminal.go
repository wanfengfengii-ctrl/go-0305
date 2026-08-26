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
// stage and two distinct qualified reviewers to have passed.
func (s *Service) Unlock(ctx context.Context, unit string, operationID string, logicalTime int64) ([]domain.UnlockEvent, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return nil, err
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

	// Index the latest generation of each position so the unlock advance
	// operates on the current projection. A position may carry several
	// generations after a replacement; only the newest is unlockable. The
	// rows are read outside the transaction because the single-writer
	// connection is held by the tx below, so reads on s.db would stall.
	latest := map[string]store.StageRow{}
	for _, r := range rows {
		cur, ok := latest[r.PositionID]
		if !ok || r.Generation > cur.Generation {
			latest[r.PositionID] = r
		}
	}
	unlockDigest := digest(snap.SyncUnlockGroup)

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
		// Advance each synced position from remeasured to unlocked inside the
		// same transaction that records the unlock events, so the stage
		// projection cannot lag behind the unlock records. The prefix guard
		// above guarantees every position is at least remeasured; positions
		// already unlocked (for instance when an operation advanced them) are
		// skipped so the unlock stays idempotent with respect to the stage.
		for _, group := range snap.SyncUnlockGroup {
			for _, pos := range group {
				row, ok := latest[pos]
				if !ok {
					continue
				}
				reached, _ := row.Reached()
				if reached >= domain.StageUnlocked {
					continue
				}
				if err := store.AdvanceStageTx(ctx, tx, unit, pos, row.Generation,
					domain.StageUnlocked, row.ComponentID, row.Destination); err != nil {
					return err
				}
				if err := store.AppendEvidenceTx(ctx, tx, domain.StageEvidence{
					Unit: unit, PositionID: pos, Generation: row.Generation,
					Stage: domain.StageUnlocked, Holder: "unlock",
					LogicalTime: logicalTime, PayloadDigest: unlockDigest,
				}); err != nil {
					return err
				}
				if _, err := store.AppendEventTx(ctx, tx, domain.DomainEvent{
					Unit: unit, Type: "stage.unlocked", PayloadDigest: unlockDigest,
					LogicalTime: logicalTime,
				}); err != nil {
					return err
				}
			}
		}
		if _, err := store.AppendEventTx(ctx, tx, domain.DomainEvent{
			Unit: unit, Type: "unlock", PayloadDigest: unlockDigest, LogicalTime: logicalTime,
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
