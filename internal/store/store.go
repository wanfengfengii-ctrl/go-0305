// Package store provides the embedded relational persistence layer. It uses
// SQLite (modernc.org/sqlite, a pure-Go driver) with foreign keys, unique
// indexes, check constraints and immediate transactions to protect component
// occupancy, operation ids, impact digests and terminal uniqueness. On
// restart, projections are rebuilt from snapshots and append-only events.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, cross-arch (arm64/amd64)
)

// Store wraps the SQL database handle.
type Store struct {
	db *sql.DB
}

// Open opens (and creates if necessary) the database at path, applies the
// schema and verifies connectivity. Pass ":memory:" for an in-memory database.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		// Enable WAL and a busy timeout for safe concurrent use.
		dsn = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	} else {
		dsn = "file::memory:?_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer keeps SQLite transactions serialized
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// dbtx abstracts *sql.DB and *sql.Tx so a single method set works inside and
// outside transactions.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DBTx is the exported transaction/connection interface consumed by the
// application layer so writes can run atomically inside store transactions.
type DBTx = dbtx

// ErrExpired signals a logical-time lease or material expiry rejection.
var ErrExpired = errors.New("store: expired")

// ErrBusy signals a mutual-exclusion rejection (a competing lease holder).
var ErrBusy = errors.New("store: busy")

// Tx runs fn inside a single immediate transaction, rolling back on any error
// and returning the error unchanged. This is the single boundary that
// guarantees identity, inventory, lease, stage and uniqueness atomicity.
func (s *Store) Tx(ctx context.Context, fn func(tx dbtx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrate applies the full schema. Every statement is idempotent.
func (s *Store) migrate(ctx context.Context) error {
	for _, stmt := range schema {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// schema is the ordered list of DDL statements.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS design_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		generation INTEGER NOT NULL,
		building TEXT NOT NULL,
		unit TEXT NOT NULL,
		summary_version TEXT NOT NULL,
		transform_json TEXT NOT NULL,
		lock_digest TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		UNIQUE(unit, generation)
	)`,

	`CREATE TABLE IF NOT EXISTS design_positions (
		unit TEXT NOT NULL,
		generation INTEGER NOT NULL,
		position_id TEXT NOT NULL,
		position_json TEXT NOT NULL,
		PRIMARY KEY (unit, generation, position_id)
	)`,

	`CREATE TABLE IF NOT EXISTS design_adjacency (
		unit TEXT NOT NULL,
		generation INTEGER NOT NULL,
		a TEXT NOT NULL,
		b TEXT NOT NULL,
		PRIMARY KEY (unit, generation, a, b)
	)`,

	`CREATE TABLE IF NOT EXISTS sync_unlock_groups (
		unit TEXT NOT NULL,
		generation INTEGER NOT NULL,
		group_index INTEGER NOT NULL,
		position_id TEXT NOT NULL,
		PRIMARY KEY (unit, generation, group_index, position_id)
	)`,

	`CREATE TABLE IF NOT EXISTS components (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		model TEXT NOT NULL DEFAULT '',
		manufacture_batch TEXT NOT NULL,
		construction_summary TEXT NOT NULL,
		status TEXT NOT NULL,
		current_unit TEXT NOT NULL DEFAULT '',
		current_position TEXT NOT NULL DEFAULT '',
		destination TEXT NOT NULL DEFAULT '',
		thickness_micron INTEGER NOT NULL DEFAULT 0,
		shim_count INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS lineage_events (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		unit TEXT NOT NULL,
		position_id TEXT NOT NULL,
		component_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		generation INTEGER NOT NULL,
		logical_time INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS inventory_lots (
		id TEXT PRIMARY KEY,
		unit TEXT NOT NULL,
		initial_grams INTEGER NOT NULL,
		expiry_logical_time INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS inventory_movements (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		lot_id TEXT NOT NULL,
		delta_grams INTEGER NOT NULL,
		logical_time INTEGER NOT NULL,
		FOREIGN KEY (lot_id) REFERENCES inventory_lots(id)
	)`,

	`CREATE TABLE IF NOT EXISTS resource_leases (
		id TEXT PRIMARY KEY,
		resource TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		holder TEXT NOT NULL,
		position_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		acquired_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		release_reason TEXT NOT NULL DEFAULT ''
	)`,

	`CREATE TABLE IF NOT EXISTS instrument_calls (
		id TEXT PRIMARY KEY,
		instrument TEXT NOT NULL,
		script_step TEXT NOT NULL,
		logical_time INTEGER NOT NULL,
		attempt INTEGER NOT NULL,
		status TEXT NOT NULL,
		fault_code TEXT NOT NULL DEFAULT '',
		raw_digest TEXT NOT NULL DEFAULT '',
		next_retry_at INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS position_stages (
		unit TEXT NOT NULL,
		position_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		current_stage INTEGER NOT NULL,
		component_id TEXT NOT NULL DEFAULT '',
		destination TEXT NOT NULL DEFAULT '',
		grout_lot_id TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (unit, position_id, generation)
	)`,

	`CREATE TABLE IF NOT EXISTS stage_evidence (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		unit TEXT NOT NULL,
		position_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		stage INTEGER NOT NULL,
		holder TEXT NOT NULL,
		logical_time INTEGER NOT NULL,
		payload_digest TEXT NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS impact_cases (
		id TEXT PRIMARY KEY,
		unit TEXT NOT NULL,
		trigger_position TEXT NOT NULL,
		reason TEXT NOT NULL,
		digest TEXT NOT NULL,
		isolated INTEGER NOT NULL DEFAULT 0,
		UNIQUE(unit, digest)
	)`,

	`CREATE TABLE IF NOT EXISTS impact_positions (
		case_id TEXT NOT NULL,
		position_id TEXT NOT NULL,
		PRIMARY KEY (case_id, position_id)
	)`,

	`CREATE TABLE IF NOT EXISTS replacement_generations (
		unit TEXT NOT NULL,
		generation INTEGER NOT NULL,
		position_id TEXT NOT NULL,
		old_component_id TEXT NOT NULL,
		old_destination TEXT NOT NULL,
		review_result TEXT NOT NULL,
		PRIMARY KEY (unit, generation, position_id)
	)`,

	`CREATE TABLE IF NOT EXISTS reviews (
		id TEXT PRIMARY KEY,
		unit TEXT NOT NULL,
		reviewer_id TEXT NOT NULL,
		qualification TEXT NOT NULL,
		opinion TEXT NOT NULL,
		logical_time INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS unlock_events (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		unit TEXT NOT NULL,
		group_index INTEGER NOT NULL,
		position_id TEXT NOT NULL,
		lock_dest TEXT NOT NULL,
		logical_time INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS terminal_decisions (
		unit TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		version INTEGER NOT NULL,
		credential_digest TEXT NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS idempotency_records (
		scope TEXT NOT NULL,
		operation_id TEXT NOT NULL,
		request_digest TEXT NOT NULL,
		response_digest TEXT NOT NULL,
		event_sequence INTEGER NOT NULL,
		logical_time INTEGER NOT NULL,
		PRIMARY KEY (scope, operation_id)
	)`,

	`CREATE TABLE IF NOT EXISTS domain_events (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		unit TEXT NOT NULL,
		type TEXT NOT NULL,
		payload_digest TEXT NOT NULL,
		logical_time INTEGER NOT NULL
	)`,
}
