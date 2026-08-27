package store

import (
	"context"
	"database/sql"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// InsertCallTx inserts an instrument call inside a transaction.
func InsertCallTx(ctx context.Context, tx dbtx, c domain.InstrumentCall) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO instrument_calls (id, instrument, script_step, logical_time, attempt, status, fault_code, raw_digest, next_retry_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, string(c.Instrument), c.ScriptStep, c.LogicalTime, c.Attempt, c.Status, c.FaultCode, c.RawDigest, c.NextRetryAt)
	return err
}

// Call returns an instrument call by id, or ErrNotFound.
func (s *Store) Call(ctx context.Context, id string) (domain.InstrumentCall, error) {
	return CallTx(ctx, s.db, id)
}

// CallTx returns an instrument call by id inside a transaction, or ErrNotFound.
// Reading inside the transaction guarantees RetryInstrument observes a
// consistent row and its advancement is committed atomically with the read.
func CallTx(ctx context.Context, tx dbtx, id string) (domain.InstrumentCall, error) {
	var c domain.InstrumentCall
	var instrument, status string
	err := tx.QueryRowContext(ctx,
		`SELECT id, instrument, script_step, logical_time, attempt, status, fault_code, raw_digest, next_retry_at
		 FROM instrument_calls WHERE id = ?`, id).
		Scan(&c.ID, &instrument, &c.ScriptStep, &c.LogicalTime, &c.Attempt, &status, &c.FaultCode, &c.RawDigest, &c.NextRetryAt)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	c.Instrument = domain.InstrumentKind(instrument)
	c.Status = status
	return c, err
}

// UpdateCallTx updates an instrument call's attempt, status and retry schedule
// inside a transaction.
func UpdateCallTx(ctx context.Context, tx dbtx, c domain.InstrumentCall) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE instrument_calls SET attempt = ?, status = ?, fault_code = ?, raw_digest = ?, next_retry_at = ? WHERE id = ?`,
		c.Attempt, c.Status, c.FaultCode, c.RawDigest, c.NextRetryAt, c.ID)
	return err
}

// PendingCalls returns calls whose status is 'retry' (awaiting a retry),
// ordered deterministically by logical time then id.
func (s *Store) PendingCalls(ctx context.Context) ([]domain.InstrumentCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instrument, script_step, logical_time, attempt, status, fault_code, raw_digest, next_retry_at
		 FROM instrument_calls WHERE status = 'retry' ORDER BY next_retry_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InstrumentCall
	for rows.Next() {
		var c domain.InstrumentCall
		var instrument, status string
		if err := rows.Scan(&c.ID, &instrument, &c.ScriptStep, &c.LogicalTime, &c.Attempt, &status, &c.FaultCode, &c.RawDigest, &c.NextRetryAt); err != nil {
			return nil, err
		}
		c.Instrument = domain.InstrumentKind(instrument)
		c.Status = status
		out = append(out, c)
	}
	return out, rows.Err()
}
