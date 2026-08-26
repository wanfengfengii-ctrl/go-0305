// Package app wires the domain components together into a single application
// service backed by the embedded relational store. It implements the full
// business flows: design locking, position stage advancement, component
// binding, material conservation, mutual-exclusion leasing, scripted
// instrument calls with deterministic retry, impact closure, replacement
// generations, dual review, ordered unlock and the single-writer terminal
// competition. Every mutation is transactional; any failure rolls back.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"hospital-isolation-bearing-unlock-closure/internal/catalog"
	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// Compile-time assertions that Service implements every component interface
// from the approved project document.
var (
	_ domain.PositionOperator = (*Service)(nil)
	_ domain.LeaseManager     = (*Service)(nil)
	_ domain.EvidenceRecorder = (*Service)(nil)
	_ domain.Arbiter          = (*Service)(nil)
)

// Service is the application service. It owns the design catalog, the
// relational store and the scripted instrument adapter.
type Service struct {
	catalog *catalog.Catalog
	store   *store.Store
}

// New constructs a Service over the given store.
func New(st *store.Store) *Service {
	return &Service{catalog: catalog.New(), store: st}
}

// Store exposes the underlying store for the HTTP transport and tests.
func (s *Service) Store() *store.Store { return s.store }

// Close closes the underlying store.
func (s *Service) Close() error { return s.store.Close() }

// mapErr converts a store-level sentinel error into a stable business error.
// It is called outside transactions so ordered reasons are always derived from
// domain keys, never from database internals.
func mapErr(err error, opID string, reasons ...string) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return domain.NewBusinessError(domain.CodeNotFound, "resource not found", opID, reasons...)
	case errors.Is(err, store.ErrBusy):
		return domain.NewBusinessError(domain.CodeLeaseBusy, "resource lease is busy", opID, reasons...)
	case errors.Is(err, store.ErrExpired):
		return domain.NewBusinessError(domain.CodeLeaseExpired, "resource lease or material is expired", opID, reasons...)
	case errors.Is(err, store.ErrConflict):
		return domain.NewBusinessError(domain.CodeGenerationConflict, "state conflict", opID, reasons...)
	default:
		var be *domain.BusinessError
		if errors.As(err, &be) {
			return be
		}
		return domain.NewBusinessError(domain.CodeInternal, err.Error(), opID, reasons...)
	}
}

// digest computes a deterministic SHA-256 hex digest of a value's JSON form.
// Identical normalized content always yields the same digest, which powers
// idempotency and impact-set deduplication.
func digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// scopeKey builds the idempotency scope for a unit-position operation.
func scopeKey(unit, position string) string {
	return fmt.Sprintf("%s/%s", unit, position)
}
