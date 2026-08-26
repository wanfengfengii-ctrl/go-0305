package app

import (
	"context"
	"fmt"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// AcquireLease acquires a time-limited mutual-exclusion lease for a resource,
// keyed on logical time (never machine wall clock). Competing holders receive
// LEASE_BUSY; expired leases cannot be reused.
func (s *Service) AcquireLease(ctx context.Context, req domain.LeaseAcquireRequest) (domain.ResourceLease, error) {
	if req.Expiry <= req.LogicalTime {
		return domain.ResourceLease{}, domain.NewBusinessError(domain.CodeInvalidRequest, "expiry must be after logical time", req.OperationID, "expiry")
	}
	lease := domain.ResourceLease{
		ID:         fmt.Sprintf("lease-%s-%s-%d", req.Kind, req.ResourceID, req.LogicalTime),
		Resource:   req.Kind,
		ResourceID: req.ResourceID,
		Holder:     req.Holder,
		PositionID: req.PositionID,
		Generation: req.Generation,
		AcquiredAt: req.LogicalTime,
		ExpiresAt:  req.Expiry,
	}
	err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.InsertLeaseTx(ctx, tx, lease)
	})
	if err != nil {
		return domain.ResourceLease{}, mapErr(err, req.OperationID, "lease busy")
	}
	return lease, nil
}

// ReleaseLease releases a previously acquired lease with a reason.
func (s *Service) ReleaseLease(ctx context.Context, leaseID, reason string) error {
	err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.ReleaseLeaseTx(ctx, tx, leaseID, reason)
	})
	if err != nil {
		return mapErr(err, "", "lease already released")
	}
	return nil
}

// ActiveLease reports the current active lease for a resource at a logical
// time, or NOT_FOUND when the resource is free.
func (s *Service) ActiveLease(ctx context.Context, kind domain.ResourceKind, resourceID string, logicalTime int64) (domain.ResourceLease, error) {
	l, err := s.store.ActiveLease(ctx, kind, resourceID, logicalTime)
	if err != nil {
		return domain.ResourceLease{}, mapErr(err, "", "no active lease")
	}
	return l, nil
}
