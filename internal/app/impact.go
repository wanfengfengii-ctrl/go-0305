package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// Impact computes the deterministic closure of positions affected by a single
// nonconformance. Starting from the trigger position it expands along
// force-transfer adjacency and merges positions sharing a manufacture batch, a
// grout pour or a sync-unlock group, until the closure is stable. The member
// set is sorted by domain key and digested, so re-triggering an equivalent
// nonconformance yields the same unique case.
func (s *Service) Impact(ctx context.Context, unit, triggerPosition, reason string) (domain.ImpactCase, error) {
	gen, err := s.resolveSnapshotGeneration(ctx, unit)
	if err != nil {
		return domain.ImpactCase{}, err
	}
	snap, err := s.store.Snapshot(ctx, unit, gen)
	if err != nil {
		return domain.ImpactCase{}, mapErr(err, "", "snapshot")
	}
	rows, err := s.store.PositionsByUnit(ctx, unit)
	if err != nil {
		return domain.ImpactCase{}, err
	}
	// Keep only the latest generation per position. A replacement opens a newer
	// generation with a new bearing; the superseded generation's bearing is no
	// longer the active one. Propagating from the current binding prevents the
	// closure from diffusing along the old bearing's manufacture batch (or grout
	// lot) after a swap-out.
	rows = latestRows(rows)

	// Build the propagation indexes.
	adj := indexAdjacency(snap)
	batchIndex := map[string][]string{}
	lotIndex := map[string][]string{}
	groupIndex := map[string][]string{}
	for _, r := range rows {
		if r.ComponentID != "" {
			comp, err := s.store.Component(ctx, r.ComponentID)
			if err == nil {
				batchIndex[comp.ManufactureBatch] = append(batchIndex[comp.ManufactureBatch], r.PositionID)
			}
		}
		if r.GroutLotID != "" {
			lotIndex[r.GroutLotID] = append(lotIndex[r.GroutLotID], r.PositionID)
		}
	}
	for _, g := range snap.SyncUnlockGroup {
		for _, pos := range g {
			groupIndex[pos] = append(groupIndex[pos], g...)
		}
	}

	// Deterministic closure.
	members := map[string]bool{triggerPosition: true}
	changed := true
	for changed {
		changed = false
		for pos := range members {
			for _, n := range adj[pos] {
				if !members[n] {
					members[n] = true
					changed = true
				}
			}
		}
		// Batch / lot / group merges depend on the current member set.
		for pos := range members {
			if r, ok := findRow(rows, pos); ok && r.ComponentID != "" {
				if comp, err := s.store.Component(ctx, r.ComponentID); err == nil {
					for _, p := range batchIndex[comp.ManufactureBatch] {
						if !members[p] {
							members[p] = true
							changed = true
						}
					}
				}
			}
			if r, ok := findRow(rows, pos); ok && r.GroutLotID != "" {
				for _, p := range lotIndex[r.GroutLotID] {
					if !members[p] {
						members[p] = true
						changed = true
					}
				}
			}
			for _, p := range groupIndex[pos] {
				if !members[p] {
					members[p] = true
					changed = true
				}
			}
		}
	}

	ordered := sortedKeys(members)
	digestValue := impactDigest(unit, triggerPosition, reason, ordered)

	// Deduplicate: an equivalent closure returns the existing case.
	if existing, err := s.store.ImpactByDigest(ctx, unit, digestValue); err == nil {
		return existing, nil
	}

	c := domain.ImpactCase{
		ID:              fmt.Sprintf("impact-%s", digestValue[:16]),
		Unit:            unit,
		TriggerPosition: triggerPosition,
		Reason:          reason,
		Positions:       ordered,
		Digest:          digestValue,
		Isolated:        true,
	}
	if err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.InsertImpactTx(ctx, tx, c)
	}); err != nil {
		return domain.ImpactCase{}, mapErr(err, "", "impact already recorded")
	}
	return c, nil
}

func indexAdjacency(snap domain.DesignSnapshot) map[string][]string {
	m := map[string][]string{}
	for _, e := range snap.Adjacency {
		m[e[0]] = append(m[e[0]], e[1])
		m[e[1]] = append(m[e[1]], e[0])
	}
	return m
}

// latestRows keeps only the highest-generation row per position. Rows arrive
// ordered by (position_id, generation), so the last row seen for a position is
// the current one; older rows belong to superseded replacement generations
// whose bindings must no longer drive the closure.
func latestRows(rows []store.StageRow) []store.StageRow {
	out := make([]store.StageRow, 0, len(rows))
	by := map[string]int{} // position_id -> index in out
	for _, r := range rows {
		if i, ok := by[r.PositionID]; ok {
			out[i] = r
		} else {
			by[r.PositionID] = len(out)
			out = append(out, r)
		}
	}
	return out
}

func findRow(rows []store.StageRow, positionID string) (store.StageRow, bool) {
	for _, r := range rows {
		if r.PositionID == positionID {
			return r, true
		}
	}
	return store.StageRow{}, false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func impactDigest(unit, trigger, reason string, positions []string) string {
	h := sha256.New()
	h.Write([]byte(unit))
	h.Write([]byte{0})
	h.Write([]byte(trigger))
	h.Write([]byte{0})
	h.Write([]byte(reason))
	h.Write([]byte{0})
	for _, p := range positions {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
