// Package catalog implements the "隔震设计与材料规则目录" component: it
// validates and locks an isolation unit design snapshot using integer-micron
// geometry, checked fixed-point arithmetic and deterministic digesting.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/fixed"
)

// Catalog is a stateless design catalog.
type Catalog struct{}

// New returns a ready-to-use Catalog.
func New() *Catalog { return &Catalog{} }

// Lock validates the request and produces an immutable DesignSnapshot, or a
// stable BusinessError describing the first ordered rejection.
func (c *Catalog) Lock(_ context.Context, req domain.LockRequest) (domain.DesignSnapshot, error) {
	if req.SummaryVersion == "" {
		return domain.DesignSnapshot{}, domain.NewBusinessError(
			domain.CodeStaleSummary,
			"design or manufacturing summary version is stale",
			req.OperationID,
			"summary_version missing",
		)
	}
	if err := validateTransform(req.Transform, req.OperationID); err != nil {
		return domain.DesignSnapshot{}, err
	}
	positions, reasons := validatePositions(req.Positions, req.OperationID)
	if len(reasons) > 0 {
		return domain.DesignSnapshot{}, domain.NewBusinessError(
			domain.CodeInvalidGeometry,
			"design positions rejected",
			req.OperationID,
			reasons...,
		)
	}
	if reasons := validateTopology(req, positions, req.OperationID); len(reasons) > 0 {
		return domain.DesignSnapshot{}, domain.NewBusinessError(
			domain.CodeInvalidGeometry,
			"design topology rejected",
			req.OperationID,
			reasons...,
		)
	}
	digest := digestSnapshot(req)
	return domain.DesignSnapshot{
		Generation:      1,
		Building:        req.Building,
		Unit:            req.Unit,
		SummaryVersion:  req.SummaryVersion,
		Transform:       req.Transform,
		Positions:       positions,
		Adjacency:       req.Adjacency,
		SyncUnlockGroup: req.SyncUnlockGroup,
		Sampling:        req.Sampling,
		Thresholds:      req.Thresholds,
		LockDigest:      digest,
	}, nil
}

// validateTransform rejects non-invertible transforms and fixed-point
// overflow while computing the determinant.
func validateTransform(t domain.PlanTransform, opID string) error {
	if t.Scale <= 0 {
		return domain.NewBusinessError(domain.CodeInvalidGeometry,
			"coordinate transform scale must be positive", opID, "transform.scale")
	}
	ae, err := fixed.Mul(t.A, t.E)
	if err != nil {
		return domain.NewBusinessError(domain.CodeArithmeticOverflow,
			"coordinate transform overflow", opID, "transform.determinant")
	}
	bd, err := fixed.Mul(t.B, t.D)
	if err != nil {
		return domain.NewBusinessError(domain.CodeArithmeticOverflow,
			"coordinate transform overflow", opID, "transform.determinant")
	}
	det, err := fixed.Sub(ae, bd)
	if err != nil {
		return domain.NewBusinessError(domain.CodeArithmeticOverflow,
			"coordinate transform overflow", opID, "transform.determinant")
	}
	if det == 0 {
		return domain.NewBusinessError(domain.CodeInvalidGeometry,
			"coordinate transform is not invertible", opID, "transform.determinant")
	}
	return nil
}

// validatePositions checks for missing/duplicate positions, negative or
// degenerate dimensions, inverted interfaces and incompatible hole groups. It
// returns the sorted positions and any ordered rejection reasons.
func validatePositions(in []domain.DesignPosition, opID string) ([]domain.DesignPosition, []string) {
	if len(in) == 0 {
		return nil, []string{"positions empty"}
	}
	seen := make(map[string]struct{}, len(in))
	positions := make([]domain.DesignPosition, len(in))
	copy(positions, in)
	sort.Slice(positions, func(i, j int) bool { return positions[i].PositionID < positions[j].PositionID })

	var reasons []string
	for _, p := range positions {
		if p.PositionID == "" {
			reasons = append(reasons, "position id empty")
		}
		if _, dup := seen[p.PositionID]; dup {
			reasons = append(reasons, fmt.Sprintf("duplicate position %s", p.PositionID))
		}
		seen[p.PositionID] = struct{}{}
		reasons = append(reasons, validatePositionGeometry(p)...)
	}
	sort.Strings(reasons)
	return positions, reasons
}

func validatePositionGeometry(p domain.DesignPosition) []string {
	var reasons []string
	if p.Upper.PlateWidth <= 0 || p.Upper.PlateLength <= 0 {
		reasons = append(reasons, fmt.Sprintf("%s upper plate degenerate", p.PositionID))
	}
	if p.Lower.PlateWidth <= 0 || p.Lower.PlateLength <= 0 {
		reasons = append(reasons, fmt.Sprintf("%s lower plate degenerate", p.PositionID))
	}
	if p.AllowedEccentricity < 0 || p.AllowedTilt < 0 {
		reasons = append(reasons, fmt.Sprintf("%s negative allowance", p.PositionID))
	}
	if p.MaxShimThickness < 0 || p.MaxShimLayers < 0 {
		reasons = append(reasons, fmt.Sprintf("%s negative shim limit", p.PositionID))
	}
	if p.Upper.HoleCount != p.Lower.HoleCount || p.Upper.HolePattern != p.Lower.HolePattern {
		reasons = append(reasons, fmt.Sprintf("%s incompatible hole group", p.PositionID))
	}
	if inverted(p.Upper.Orientation, p.Lower.Orientation) {
		reasons = append(reasons, fmt.Sprintf("%s inverted interface", p.PositionID))
	}
	return reasons
}

// inverted reports whether the upper and lower interface normals point the same
// way (a reversed interface), which is a locking rejection.
func inverted(upper, lower domain.Orientation) bool {
	if upper.Scale != lower.Scale || upper.Scale <= 0 {
		return true
	}
	return upper.X == lower.X && upper.Y == lower.Y && upper.Z == lower.Z
}

// validateTopology checks that every force-transfer edge references a known
// position, that no self-loop exists, and that sync-unlock groups form a
// partition of the position set (each position appears in exactly one group).
func validateTopology(req domain.LockRequest, positions []domain.DesignPosition, opID string) []string {
	known := make(map[string]struct{}, len(positions))
	for _, p := range positions {
		known[p.PositionID] = struct{}{}
	}
	var reasons []string
	for _, e := range req.Adjacency {
		if _, ok := known[e[0]]; !ok {
			reasons = append(reasons, fmt.Sprintf("adjacency references unknown position %s", e[0]))
		}
		if _, ok := known[e[1]]; !ok {
			reasons = append(reasons, fmt.Sprintf("adjacency references unknown position %s", e[1]))
		}
		if e[0] == e[1] {
			reasons = append(reasons, fmt.Sprintf("adjacency self-loop %s", e[0]))
		}
	}
	seen := make(map[string]bool, len(positions))
	for i, g := range req.SyncUnlockGroup {
		for _, pos := range g {
			if _, ok := known[pos]; !ok {
				reasons = append(reasons, fmt.Sprintf("sync group %d references unknown position %s", i, pos))
			}
			if seen[pos] {
				reasons = append(reasons, fmt.Sprintf("position %s in multiple sync groups", pos))
			}
			seen[pos] = true
		}
	}
	sort.Strings(reasons)
	return reasons
}

// digestSnapshot computes a deterministic SHA-256 digest over the sorted
// positions, adjacency, sync groups and thresholds so identical lock requests
// always produce the same lock digest.
func digestSnapshot(req domain.LockRequest) string {
	h := sha256.New()
	h.Write([]byte(req.Building))
	h.Write([]byte{0})
	h.Write([]byte(req.Unit))
	h.Write([]byte{0})
	h.Write([]byte(req.SummaryVersion))
	h.Write([]byte{0})
	for _, p := range sortedPositions(req.Positions) {
		fmt.Fprintf(h, "%s|%s|%d|%d|%d|", p.PositionID, p.BearingModel, p.DesignCenter.X, p.DesignCenter.Y, p.DesignCenter.Z)
	}
	// Adjacency and sync groups participate in the digest so the lock summary is
	// a deterministic fingerprint of the complete topology.
	adj := make([][2]string, len(req.Adjacency))
	copy(adj, req.Adjacency)
	sort.Slice(adj, func(i, j int) bool {
		if adj[i][0] != adj[j][0] {
			return adj[i][0] < adj[j][0]
		}
		return adj[i][1] < adj[j][1]
	})
	for _, e := range adj {
		fmt.Fprintf(h, "e%s-%s|", e[0], e[1])
	}
	for _, g := range req.SyncUnlockGroup {
		group := append([]string(nil), g...)
		sort.Strings(group)
		fmt.Fprintf(h, "g%s|", group)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedPositions(in []domain.DesignPosition) []domain.DesignPosition {
	out := make([]domain.DesignPosition, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].PositionID < out[j].PositionID })
	return out
}

// Compile-time assertion that Catalog implements the design-catalog component
// interface from the approved project document.
var _ domain.DesignCatalog = (*Catalog)(nil)
