package store

import (
	"context"
	"database/sql"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// InsertReviewTx inserts an independent review inside a transaction.
func InsertReviewTx(ctx context.Context, tx dbtx, r domain.Review) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO reviews (id, unit, reviewer_id, qualification, opinion, logical_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Unit, r.ReviewerID, r.Qualification, r.Opinion, r.LogicalTime)
	return err
}

// Reviews returns all reviews for a unit, ordered by logical time then id.
func (s *Store) Reviews(ctx context.Context, unit string) ([]domain.Review, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, unit, reviewer_id, qualification, opinion, logical_time
		 FROM reviews WHERE unit = ? ORDER BY logical_time, id`, unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		var r domain.Review
		if err := rows.Scan(&r.ID, &r.Unit, &r.ReviewerID, &r.Qualification, &r.Opinion, &r.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertUnlockTx appends an unlock event inside a transaction.
func InsertUnlockTx(ctx context.Context, tx dbtx, e domain.UnlockEvent) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO unlock_events (unit, group_index, position_id, lock_dest, logical_time)
		 VALUES (?, ?, ?, ?, ?)`,
		e.Unit, e.GroupIndex, e.PositionID, e.LockDest, e.LogicalTime)
	return err
}

// Unlocks returns all unlock events for a unit, ordered by group index then
// position id.
func (s *Store) Unlocks(ctx context.Context, unit string) ([]domain.UnlockEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sequence, unit, group_index, position_id, lock_dest, logical_time
		 FROM unlock_events WHERE unit = ? ORDER BY group_index, position_id`, unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UnlockEvent
	for rows.Next() {
		var e domain.UnlockEvent
		if err := rows.Scan(&e.Sequence, &e.Unit, &e.GroupIndex, &e.PositionID, &e.LockDest, &e.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Terminal returns the winning terminal decision for a unit, or ErrNotFound.
func (s *Store) Terminal(ctx context.Context, unit string) (domain.TerminalDecision, error) {
	var t domain.TerminalDecision
	var kind string
	err := s.db.QueryRowContext(ctx,
		`SELECT unit, kind, version, credential_digest FROM terminal_decisions WHERE unit = ?`, unit).
		Scan(&t.Unit, &kind, &t.Version, &t.CredentialDigest)
	if err == sql.ErrNoRows {
		return t, ErrNotFound
	}
	t.Kind = domain.TerminalKind(kind)
	return t, err
}

// InsertTerminalTx writes the single winning terminal decision inside a
// transaction. The primary key on unit enforces single-writer semantics: a
// concurrent second write fails.
func InsertTerminalTx(ctx context.Context, tx dbtx, t domain.TerminalDecision) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO terminal_decisions (unit, kind, version, credential_digest) VALUES (?, ?, ?, ?)`,
		t.Unit, string(t.Kind), t.Version, t.CredentialDigest)
	return err
}

// idempotencyColumns is the shared select/insert column list so the committed
// generation and component id travel with every record and a replay can return
// the original committed outcome even after the position advances to a later
// replacement generation.
const idempotencyColumns = `scope, operation_id, request_digest, response_digest, event_sequence, logical_time, generation, component_id`

// LookupIdempotency returns an idempotency record for a scope/operation, or
// ErrNotFound, using a non-transactional read.
func (s *Store) LookupIdempotency(ctx context.Context, scope, operationID string) (domain.IdempotencyRecord, error) {
	var r domain.IdempotencyRecord
	err := s.db.QueryRowContext(ctx,
		`SELECT `+idempotencyColumns+`
		 FROM idempotency_records WHERE scope = ? AND operation_id = ?`, scope, operationID).
		Scan(&r.Scope, &r.OperationID, &r.RequestDigest, &r.ResponseDigest, &r.EventSequence, &r.LogicalTime, &r.Generation, &r.ComponentID)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	return r, err
}

// LookupIdempotencyTx returns an idempotency record for a scope/operation, or
// ErrNotFound.
func LookupIdempotencyTx(ctx context.Context, tx dbtx, scope, operationID string) (domain.IdempotencyRecord, error) {
	var r domain.IdempotencyRecord
	err := tx.QueryRowContext(ctx,
		`SELECT `+idempotencyColumns+`
		 FROM idempotency_records WHERE scope = ? AND operation_id = ?`, scope, operationID).
		Scan(&r.Scope, &r.OperationID, &r.RequestDigest, &r.ResponseDigest, &r.EventSequence, &r.LogicalTime, &r.Generation, &r.ComponentID)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	return r, err
}

// InsertIdempotencyTx records an idempotency record inside a transaction.
func InsertIdempotencyTx(ctx context.Context, tx dbtx, r domain.IdempotencyRecord) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO idempotency_records (`+idempotencyColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Scope, r.OperationID, r.RequestDigest, r.ResponseDigest, r.EventSequence, r.LogicalTime, r.Generation, r.ComponentID)
	return err
}

// AppendEventTx appends a domain audit event inside a transaction and returns
// its sequence number.
func AppendEventTx(ctx context.Context, tx dbtx, e domain.DomainEvent) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO domain_events (unit, type, payload_digest, logical_time) VALUES (?, ?, ?, ?)`,
		e.Unit, e.Type, e.PayloadDigest, e.LogicalTime)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EventCount returns the total number of domain events (used to assert
// append-only reconstruction on restart).
func (s *Store) EventCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domain_events`).Scan(&n)
	return n, err
}
