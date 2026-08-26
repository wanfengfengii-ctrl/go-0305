package store

import (
	"context"
	"database/sql"
	"errors"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// InsertLot registers a grout lot with an initial integer gram quantity and a
// logical availability deadline.
func (s *Store) InsertLot(ctx context.Context, lot domain.ConsumableLot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO inventory_lots (id, unit, initial_grams, expiry_logical_time) VALUES (?, ?, ?, ?)`,
		lot.ID, lot.Unit, lot.InitialGrams, lot.ExpiryLogicalTime)
	return err
}

// Lot returns a grout lot by id, or ErrNotFound.
func (s *Store) Lot(ctx context.Context, id string) (domain.ConsumableLot, error) {
	var lot domain.ConsumableLot
	err := s.db.QueryRowContext(ctx,
		`SELECT id, unit, initial_grams, expiry_logical_time FROM inventory_lots WHERE id = ?`, id).
		Scan(&lot.ID, &lot.Unit, &lot.InitialGrams, &lot.ExpiryLogicalTime)
	if err == sql.ErrNoRows {
		return lot, ErrNotFound
	}
	return lot, err
}

// LotRemaining returns the remaining grams of a lot: initial minus the sum of
// all movements.
func (s *Store) LotRemaining(ctx context.Context, id string) (int64, error) {
	var moved int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(delta_grams), 0) FROM inventory_movements WHERE lot_id = ?`, id).Scan(&moved)
	if err != nil {
		return 0, err
	}
	lot, err := s.Lot(ctx, id)
	if err != nil {
		return 0, err
	}
	return lot.InitialGrams - moved, nil
}

// DeductTx deducts grams from a lot inside a transaction, enforcing that the
// remaining balance never goes negative and the lot has not expired. It returns
// ErrConflict when the deduction would overdraw or when the lot is past its
// logical expiry.
func DeductTx(ctx context.Context, tx dbtx, lotID string, grams int64, logicalTime int64) error {
	if grams < 0 {
		return errors.New("store: negative deduction")
	}
	var initial, expiry int64
	err := tx.QueryRowContext(ctx,
		`SELECT initial_grams, expiry_logical_time FROM inventory_lots WHERE id = ?`, lotID).Scan(&initial, &expiry)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if logicalTime > expiry {
		return ErrExpired
	}
	var moved int64
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(delta_grams), 0) FROM inventory_movements WHERE lot_id = ?`, lotID).Scan(&moved)
	if err != nil {
		return err
	}
	if initial-moved < grams {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO inventory_movements (lot_id, delta_grams, logical_time) VALUES (?, ?, ?)`,
		lotID, grams, logicalTime)
	return err
}

// LotBalance returns the read-model balance for a lot.
func (s *Store) LotBalance(ctx context.Context, id string) (domain.LotBalance, error) {
	remaining, err := s.LotRemaining(ctx, id)
	if err != nil {
		return domain.LotBalance{}, err
	}
	lot, err := s.Lot(ctx, id)
	if err != nil {
		return domain.LotBalance{}, err
	}
	return domain.LotBalance{ID: id, Remaining: remaining, Expiry: lot.ExpiryLogicalTime}, nil
}
