package app

import (
	"context"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// LockDesign validates and persists a locked isolation-unit design snapshot.
// It returns the immutable snapshot or a stable business error. Locking is
// idempotent only in the sense that identical requests produce identical
// digests; a second lock of an existing unit/generation is a conflict.
func (s *Service) LockDesign(ctx context.Context, req domain.LockRequest) (domain.DesignSnapshot, error) {
	snap, err := s.catalog.Lock(ctx, req)
	if err != nil {
		return domain.DesignSnapshot{}, err
	}
	if err := s.store.InsertSnapshot(ctx, snap, snap.Adjacency, snap.SyncUnlockGroup); err != nil {
		return domain.DesignSnapshot{}, mapErr(err, req.OperationID, "unit already locked")
	}
	return snap, nil
}

// RegisterComponent records a one-shot physical component (bearing, transport
// lock, anchor bolt or shim) in inventory. The id is globally unique.
func (s *Service) RegisterComponent(ctx context.Context, c domain.PhysicalComponent) error {
	if c.ID == "" || c.Kind == "" {
		return domain.NewBusinessError(domain.CodeInvalidRequest, "component id and kind required", "", "component identity")
	}
	if err := s.store.InsertComponent(ctx, c); err != nil {
		return mapErr(err, "", "component id conflict")
	}
	return nil
}

// RegisterLot records a grout consumable lot with an integer gram quantity and
// a logical availability deadline.
func (s *Service) RegisterLot(ctx context.Context, lot domain.ConsumableLot) error {
	if lot.ID == "" || lot.InitialGrams < 0 {
		return domain.NewBusinessError(domain.CodeInvalidRequest, "lot id and non-negative grams required", "", "lot identity")
	}
	if err := s.store.InsertLot(ctx, lot); err != nil {
		return mapErr(err, "", "lot id conflict")
	}
	return nil
}
