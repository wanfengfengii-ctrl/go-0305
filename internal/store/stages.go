package store

import (
	"context"
	"database/sql"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// StageRow is the current projection of a single position within a generation.
// Progress is the number of completed stages (0..10); a fresh position has 0.
type StageRow struct {
	Unit        string
	PositionID  string
	Generation  int
	Progress    int // number of completed stages (0 = unstarted)
	ComponentID string
	Destination string
	GroutLotID  string
}

// Reached returns the furthest stage reached, or false when the position has
// not yet completed any stage.
func (r StageRow) Reached() (domain.Stage, bool) {
	if r.Progress == 0 {
		return 0, false
	}
	return domain.Stage(r.Progress - 1), true
}

// StageFor returns the current stage projection for a position generation.
func (s *Store) StageFor(ctx context.Context, unit, positionID string, generation int) (StageRow, error) {
	var r StageRow
	err := s.db.QueryRowContext(ctx,
		`SELECT unit, position_id, generation, current_stage, component_id, destination, grout_lot_id
		 FROM position_stages WHERE unit = ? AND position_id = ? AND generation = ?`,
		unit, positionID, generation).
		Scan(&r.Unit, &r.PositionID, &r.Generation, &r.Progress, &r.ComponentID, &r.Destination, &r.GroutLotID)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	return r, err
}

// AdvanceStageTx advances a position by exactly one stage inside a transaction,
// enforcing the strict prefix (no skipping). It requires the position's
// progress to equal int(target) and increments it. It returns ErrConflict when
// the stage would be skipped, repeated or the generation/position is stale.
func AdvanceStageTx(ctx context.Context, tx dbtx, unit, positionID string, generation int, target domain.Stage, componentID, destination string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE position_stages SET current_stage = current_stage + 1, component_id = ?, destination = ?
		 WHERE unit = ? AND position_id = ? AND generation = ? AND current_stage = ?`,
		componentID, destination, unit, positionID, generation, int(target))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}

// InsertPositionStageTx inserts a fresh position-stage row (progress 0) for a
// new replacement generation inside a transaction.
func InsertPositionStageTx(ctx context.Context, tx dbtx, unit, positionID string, generation int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO position_stages (unit, position_id, generation, current_stage, component_id, destination, grout_lot_id)
		 VALUES (?, ?, ?, 0, '', '', '')`,
		unit, positionID, generation)
	return err
}

// SetGroutLotTx records the grout lot consumed by a position inside a
// transaction (used by impact-closure propagation).
func SetGroutLotTx(ctx context.Context, tx dbtx, unit, positionID string, generation int, lotID string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE position_stages SET grout_lot_id = ? WHERE unit = ? AND position_id = ? AND generation = ?`,
		lotID, unit, positionID, generation)
	return err
}

// AppendEvidenceTx appends a stage evidence record inside a transaction.
func AppendEvidenceTx(ctx context.Context, tx dbtx, e domain.StageEvidence) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO stage_evidence (unit, position_id, generation, stage, holder, logical_time, payload_digest)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Unit, e.PositionID, e.Generation, int(e.Stage), e.Holder, e.LogicalTime, e.PayloadDigest)
	return err
}

// Evidence returns the stage evidence for a position generation, ordered by
// sequence.
func (s *Store) Evidence(ctx context.Context, unit, positionID string, generation int) ([]domain.StageEvidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sequence, stage, holder, logical_time, payload_digest
		 FROM stage_evidence WHERE unit = ? AND position_id = ? AND generation = ? ORDER BY sequence`,
		unit, positionID, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StageEvidence
	for rows.Next() {
		var e domain.StageEvidence
		var stage int
		if err := rows.Scan(&e.Sequence, &stage, &e.Holder, &e.LogicalTime, &e.PayloadDigest); err != nil {
			return nil, err
		}
		e.Unit = unit
		e.PositionID = positionID
		e.Generation = generation
		e.Stage = domain.Stage(stage)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PositionLatestGeneration returns the highest generation recorded for a
// position (its replacement generation), or 0 when the position is unknown.
func (s *Store) PositionLatestGeneration(ctx context.Context, unit, positionID string) (int, error) {
	var g int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(generation), 0) FROM position_stages WHERE unit = ? AND position_id = ?`, unit, positionID).Scan(&g)
	return g, err
}

// PositionsByUnit returns the stage projections for every position in a unit,
// ordered by position id.
func (s *Store) PositionsByUnit(ctx context.Context, unit string) ([]StageRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT unit, position_id, generation, current_stage, component_id, destination, grout_lot_id
		 FROM position_stages WHERE unit = ? ORDER BY position_id, generation`, unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StageRow
	for rows.Next() {
		var r StageRow
		if err := rows.Scan(&r.Unit, &r.PositionID, &r.Generation, &r.Progress, &r.ComponentID, &r.Destination, &r.GroutLotID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
