package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// InsertSnapshot persists a locked design snapshot, its positions, adjacency
// edges and sync-unlock groups in one transaction. It fails if a snapshot for
// the same unit and generation already exists.
func (s *Store) InsertSnapshot(ctx context.Context, snap domain.DesignSnapshot, adjacency [][2]string, groups [][]string) error {
	return s.Tx(ctx, func(tx dbtx) error {
		transform, err := json.Marshal(snap.Transform)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO design_snapshots (generation, building, unit, summary_version, transform_json, lock_digest, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			snap.Generation, snap.Building, snap.Unit, snap.SummaryVersion, string(transform), snap.LockDigest, now()); err != nil {
			return err
		}
		for _, p := range snap.Positions {
			pj, err := json.Marshal(p)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO design_positions (unit, generation, position_id, position_json)
				 VALUES (?, ?, ?, ?)`,
				snap.Unit, snap.Generation, p.PositionID, string(pj)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO position_stages (unit, position_id, generation, current_stage, component_id, destination)
				 VALUES (?, ?, ?, 0, '', '')`,
				snap.Unit, p.PositionID, snap.Generation); err != nil {
				return err
			}
		}
		for _, e := range adjacency {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO design_adjacency (unit, generation, a, b) VALUES (?, ?, ?, ?)`,
				snap.Unit, snap.Generation, e[0], e[1]); err != nil {
				return err
			}
		}
		for i, g := range groups {
			for _, pos := range g {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO sync_unlock_groups (unit, generation, group_index, position_id) VALUES (?, ?, ?, ?)`,
					snap.Unit, snap.Generation, i, pos); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// LatestGeneration returns the highest locked generation for a unit, or 0 when
// none exists.
func (s *Store) LatestGeneration(ctx context.Context, unit string) (int, error) {
	var g int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(generation), 0) FROM design_snapshots WHERE unit = ?`, unit).Scan(&g)
	return g, err
}

// Snapshot loads a full snapshot including positions, adjacency and sync groups
// for a unit generation.
func (s *Store) Snapshot(ctx context.Context, unit string, generation int) (domain.DesignSnapshot, error) {
	var snap domain.DesignSnapshot
	var transform string
	err := s.db.QueryRowContext(ctx,
		`SELECT generation, building, unit, summary_version, transform_json, lock_digest
		 FROM design_snapshots WHERE unit = ? AND generation = ?`, unit, generation).
		Scan(&snap.Generation, &snap.Building, &snap.Unit, &snap.SummaryVersion, &transform, &snap.LockDigest)
	if err == sql.ErrNoRows {
		return snap, ErrNotFound
	}
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal([]byte(transform), &snap.Transform); err != nil {
		return snap, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT position_json FROM design_positions WHERE unit = ? AND generation = ? ORDER BY position_id`, unit, generation)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			rows.Close()
			return snap, err
		}
		var p domain.DesignPosition
		if err := json.Unmarshal([]byte(pj), &p); err != nil {
			rows.Close()
			return snap, err
		}
		snap.Positions = append(snap.Positions, p)
	}
	if err := rows.Close(); err != nil {
		return snap, err
	}
	if err := rows.Err(); err != nil {
		return snap, err
	}
	snap.Adjacency, err = s.Adjacency(ctx, unit, generation)
	if err != nil {
		return snap, err
	}
	snap.SyncUnlockGroup, err = s.SyncGroups(ctx, unit, generation)
	return snap, err
}

// UnitExists reports whether any snapshot exists for the unit.
func (s *Store) UnitExists(ctx context.Context, unit string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM design_snapshots WHERE unit = ?`, unit).Scan(&n)
	return n > 0, err
}

// Adjacency returns the locked force-transfer edges for a unit generation.
func (s *Store) Adjacency(ctx context.Context, unit string, generation int) ([][2]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a, b FROM design_adjacency WHERE unit = ? AND generation = ? ORDER BY a, b`, unit, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var e [2]string
		if err := rows.Scan(&e[0], &e[1]); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SyncGroups returns the ordered sync-unlock groups for a unit generation.
func (s *Store) SyncGroups(ctx context.Context, unit string, generation int) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_index, position_id FROM sync_unlock_groups WHERE unit = ? AND generation = ? ORDER BY group_index, position_id`,
		unit, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[int][]string{}
	var order []int
	for rows.Next() {
		var gi int
		var pos string
		if err := rows.Scan(&gi, &pos); err != nil {
			return nil, err
		}
		if _, ok := groups[gi]; !ok {
			order = append(order, gi)
		}
		groups[gi] = append(groups[gi], pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(order))
	for _, gi := range order {
		out = append(out, groups[gi])
	}
	return out, nil
}
