package store

import (
	"context"
	"database/sql"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// InsertImpactTx inserts an impact case and its ordered position set inside a
// transaction. The UNIQUE(unit, digest) constraint rejects duplicate equivalent
// closures.
func InsertImpactTx(ctx context.Context, tx dbtx, c domain.ImpactCase) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO impact_cases (id, unit, trigger_position, reason, digest, isolated)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Unit, c.TriggerPosition, c.Reason, c.Digest, boolToInt(c.Isolated)); err != nil {
		return err
	}
	for _, p := range c.Positions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO impact_positions (case_id, position_id) VALUES (?, ?)`, c.ID, p); err != nil {
			return err
		}
	}
	return nil
}

// ImpactCases returns all impact cases for a unit with their positions, ordered
// deterministically by digest.
func (s *Store) ImpactCases(ctx context.Context, unit string) ([]domain.ImpactCase, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, unit, trigger_position, reason, digest, isolated
		 FROM impact_cases WHERE unit = ? ORDER BY digest`, unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cases []domain.ImpactCase
	for rows.Next() {
		var c domain.ImpactCase
		var isolated int
		if err := rows.Scan(&c.ID, &c.Unit, &c.TriggerPosition, &c.Reason, &c.Digest, &isolated); err != nil {
			return nil, err
		}
		c.Isolated = isolated != 0
		cases = append(cases, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range cases {
		positions, err := s.impactPositions(ctx, cases[i].ID)
		if err != nil {
			return nil, err
		}
		cases[i].Positions = positions
	}
	return cases, nil
}

func (s *Store) impactPositions(ctx context.Context, caseID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT position_id FROM impact_positions WHERE case_id = ? ORDER BY position_id`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ImpactByDigest returns an impact case by (unit, digest), or ErrNotFound.
func (s *Store) ImpactByDigest(ctx context.Context, unit, digest string) (domain.ImpactCase, error) {
	var c domain.ImpactCase
	var isolated int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, unit, trigger_position, reason, digest, isolated
		 FROM impact_cases WHERE unit = ? AND digest = ?`, unit, digest).
		Scan(&c.ID, &c.Unit, &c.TriggerPosition, &c.Reason, &c.Digest, &isolated)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	c.Isolated = isolated != 0
	positions, err := s.impactPositions(ctx, c.ID)
	if err != nil {
		return c, err
	}
	c.Positions = positions
	return c, err
}

// InsertReplacementTx records a replacement generation for a position inside a
// transaction.
func InsertReplacementTx(ctx context.Context, tx dbtx, r domain.ReplacementGeneration) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO replacement_generations (unit, generation, position_id, old_component_id, old_destination, review_result)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.Unit, r.Generation, r.PositionID, r.OldComponentID, r.OldDestination, r.ReviewResult)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
