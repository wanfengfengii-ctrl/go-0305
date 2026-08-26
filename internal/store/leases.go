package store

import (
	"context"
	"database/sql"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// InsertLeaseTx inserts a lease inside a transaction, rejecting any overlap
// with an existing active lease for the same resource whose validity window
// [acquired_at, expires_at) intersects the requested window. It returns
// ErrBusy on conflict.
func InsertLeaseTx(ctx context.Context, tx dbtx, l domain.ResourceLease) error {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_leases
		 WHERE resource = ? AND resource_id = ? AND release_reason = ''
		   AND acquired_at < ? AND expires_at > ?`,
		string(l.Resource), l.ResourceID, l.ExpiresAt, l.AcquiredAt).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrBusy
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO resource_leases (id, resource, resource_id, holder, position_id, generation, acquired_at, expires_at, release_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')`,
		l.ID, string(l.Resource), l.ResourceID, l.Holder, l.PositionID, l.Generation, l.AcquiredAt, l.ExpiresAt)
	return err
}

// ActiveLease returns the currently active lease for a resource, or ErrNotFound.
func (s *Store) ActiveLease(ctx context.Context, kind domain.ResourceKind, resourceID string, logicalTime int64) (domain.ResourceLease, error) {
	var l domain.ResourceLease
	var resource, reason string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, resource, resource_id, holder, position_id, generation, acquired_at, expires_at, release_reason
		 FROM resource_leases
		 WHERE resource = ? AND resource_id = ? AND release_reason = ''
		   AND acquired_at <= ? AND expires_at > ?
		 ORDER BY acquired_at DESC LIMIT 1`,
		string(kind), resourceID, logicalTime, logicalTime).Scan(
		&l.ID, &resource, &l.ResourceID, &l.Holder, &l.PositionID, &l.Generation, &l.AcquiredAt, &l.ExpiresAt, &reason)
	if err == sql.ErrNoRows {
		return l, ErrNotFound
	}
	l.Resource = domain.ResourceKind(resource)
	l.ReleaseReason = reason
	return l, err
}

// ReleaseLeaseTx marks a lease as released inside a transaction. It returns
// ErrConflict when the lease is already released or unknown.
func ReleaseLeaseTx(ctx context.Context, tx dbtx, leaseID, reason string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE resource_leases SET release_reason = ? WHERE id = ? AND release_reason = ''`, reason, leaseID)
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

// Leases returns all leases for a unit's positions, ordered by resource and id.
func (s *Store) Leases(ctx context.Context, unit string) ([]domain.ResourceLease, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, resource, resource_id, holder, position_id, generation, acquired_at, expires_at, release_reason
		 FROM resource_leases ORDER BY resource, resource_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ResourceLease
	for rows.Next() {
		var l domain.ResourceLease
		var resource string
		if err := rows.Scan(&l.ID, &resource, &l.ResourceID, &l.Holder, &l.PositionID, &l.Generation, &l.AcquiredAt, &l.ExpiresAt, &l.ReleaseReason); err != nil {
			return nil, err
		}
		l.Resource = domain.ResourceKind(resource)
		out = append(out, l)
	}
	return out, rows.Err()
}
