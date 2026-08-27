package store

import (
	"context"
	"database/sql"
	"errors"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// ErrConflict signals a unique-constraint violation detected by SQLite.
var ErrConflict = errors.New("store: conflict")

// InsertComponent registers a one-shot physical component. It fails when the
// component id already exists.
func (s *Store) InsertComponent(ctx context.Context, c domain.PhysicalComponent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO components (id, kind, model, manufacture_batch, construction_summary, status, current_unit, current_position, destination, thickness_micron, shim_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, string(c.Kind), c.Model, c.ManufactureBatch, c.ConstructionSummary, c.Status, c.CurrentUnit, c.CurrentPosition, c.Destination, c.ThicknessMicron, c.ShimCount)
	return err
}

// Component returns a physical component by id, or ErrNotFound.
func (s *Store) Component(ctx context.Context, id string) (domain.PhysicalComponent, error) {
	var c domain.PhysicalComponent
	var kind, status string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, kind, model, manufacture_batch, construction_summary, status, current_unit, current_position, destination, thickness_micron, shim_count
		 FROM components WHERE id = ?`, id).
		Scan(&c.ID, &kind, &c.Model, &c.ManufactureBatch, &c.ConstructionSummary, &status, &c.CurrentUnit, &c.CurrentPosition, &c.Destination, &c.ThicknessMicron, &c.ShimCount)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	c.Kind = domain.ComponentKind(kind)
	c.Status = status
	return c, err
}

// BindComponentTx binds a component to a unit+position within a transaction,
// enforcing that the component is free. A component is considered free only
// when it holds no current unit/position at all; the same position id may
// legitimately recur in different units, so the unit must qualify the check.
// It returns ErrConflict when the component is already bound elsewhere (or
// the id does not exist).
func BindComponentTx(ctx context.Context, tx dbtx, componentID, unit, positionID, status string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE components SET current_unit = ?, current_position = ?, status = ?
		 WHERE id = ? AND (current_unit = '' OR (current_unit = ? AND current_position = ?))`,
		unit, positionID, status, componentID, unit, positionID)
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

// UnbindComponentTx clears a component's current unit and position within a
// transaction.
func UnbindComponentTx(ctx context.Context, tx dbtx, componentID, destination, status string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE components SET current_unit = '', current_position = '', destination = ?, status = ?
		 WHERE id = ? AND current_unit <> ''`,
		destination, status, componentID)
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

// AppendLineageTx appends a lineage event inside a transaction.
func AppendLineageTx(ctx context.Context, tx dbtx, e domain.LineageEvent) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO lineage_events (unit, position_id, component_id, kind, generation, logical_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.Unit, e.PositionID, e.ComponentID, e.Kind, e.Generation, e.LogicalTime)
	return err
}

// Lineage returns the append-only lineage events for a unit, ordered by
// sequence.
func (s *Store) Lineage(ctx context.Context, unit string) ([]domain.LineageEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sequence, unit, position_id, component_id, kind, generation, logical_time
		 FROM lineage_events WHERE unit = ? ORDER BY sequence`, unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LineageEvent
	for rows.Next() {
		var e domain.LineageEvent
		if err := rows.Scan(&e.Sequence, &e.Unit, &e.PositionID, &e.ComponentID, &e.Kind, &e.Generation, &e.LogicalTime); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ComponentsByUnit returns all components currently bound within a unit.
func (s *Store) ComponentsByUnit(ctx context.Context, unit string) ([]domain.ComponentBalance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.kind, c.destination, c.current_position
		 FROM components c WHERE c.current_unit = ? AND c.current_position <> '' ORDER BY c.id`,
		unit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ComponentBalance
	for rows.Next() {
		var b domain.ComponentBalance
		var kind string
		if err := rows.Scan(&b.ID, &kind, &b.Destination, &b.PositionID); err != nil {
			return nil, err
		}
		b.Kind = domain.ComponentKind(kind)
		b.Unit = unit
		out = append(out, b)
	}
	return out, rows.Err()
}
