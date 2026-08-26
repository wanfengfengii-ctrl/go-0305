package app

import (
	"context"
	"sort"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// Unit returns the read model for a unit: the locked snapshot, per-position
// stage matrix, component balance, lot balances, leases, pending instrument
// calls, impact cases, reviews, unlocks and the terminal decision. All
// collections are deterministically ordered by domain keys.
func (s *Service) Unit(ctx context.Context, unit string) (domain.UnitView, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	snap, err := s.store.Snapshot(ctx, unit, gen)
	if err != nil {
		return domain.UnitView{}, mapErr(err, "", "snapshot")
	}

	rows, err := s.store.PositionsByUnit(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	positions := make([]domain.PositionView, 0, len(rows))
	for _, r := range rows {
		reached, ok := r.Reached()
		stage := domain.StageIncomingAccepted
		stageName := "unstarted"
		if ok {
			stage = reached
			stageName = reached.String()
		}
		evidence, err := s.store.Evidence(ctx, unit, r.PositionID, r.Generation)
		if err != nil {
			return domain.UnitView{}, err
		}
		positions = append(positions, domain.PositionView{
			PositionID: r.PositionID, Generation: r.Generation,
			Stage: stage, StageName: stageName,
			ComponentID: r.ComponentID, Destination: r.Destination,
			Evidence: evidence,
		})
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].PositionID < positions[j].PositionID })

	components, err := s.store.ComponentsByUnit(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	leases, err := s.store.Leases(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	pending, err := s.store.PendingCalls(ctx)
	if err != nil {
		return domain.UnitView{}, err
	}
	impacts, err := s.store.ImpactCases(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	reviews, err := s.store.Reviews(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	unlocks, err := s.store.Unlocks(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}
	terminal, err := s.Terminal(ctx, unit)
	if err != nil {
		return domain.UnitView{}, err
	}

	var lots []domain.LotBalance
	for _, r := range rows {
		if r.GroutLotID == "" {
			continue
		}
		lb, err := s.store.LotBalance(ctx, r.GroutLotID)
		if err == nil {
			lots = append(lots, lb)
		}
	}

	return domain.UnitView{
		Unit: unit, Generation: gen, Snapshot: snap,
		Positions: positions, ComponentBalance: components, Lots: lots,
		Leases: leases, PendingCalls: pending, ImpactCases: impacts,
		Reviews: reviews, Unlocks: unlocks, Terminal: terminal,
	}, nil
}

// Lineage returns the append-only lineage view for a unit: every binding,
// removal and replacement event plus the current position projections.
func (s *Service) Lineage(ctx context.Context, unit string) (domain.LineageView, error) {
	events, err := s.store.Lineage(ctx, unit)
	if err != nil {
		return domain.LineageView{}, err
	}
	view, err := s.Unit(ctx, unit)
	if err != nil {
		return domain.LineageView{}, err
	}
	return domain.LineageView{Unit: unit, Events: events, Positions: view.Positions}, nil
}

// Events returns the total number of append-only domain events, used to assert
// reconstruction after restart.
func (s *Service) Events(ctx context.Context) (int, error) {
	return s.store.EventCount(ctx)
}
