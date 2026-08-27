package domain

// OperationRequest atomically advances a position to its next stage within a
// single database transaction. It carries the idempotency key, the expected
// generation, the logical time, the lease request, component identities,
// material usage and the stage-specific evidence payload.
type OperationRequest struct {
	OperationID        string `json:"operation_id"`
	ExpectedGeneration int    `json:"expected_generation"`
	LogicalTime        int64  `json:"logical_time"`
	Holder             string `json:"holder"` // qualified operator submitting the evidence
	Stage              Stage  `json:"stage"`  // the target stage being reached

	// Component identity for binding (incoming acceptance) or reuse.
	ComponentID         string `json:"component_id,omitempty"`
	ManufactureBatch    string `json:"manufacture_batch,omitempty"`
	ConstructionSummary string `json:"construction_summary,omitempty"`
	Model               string `json:"model,omitempty"`

	// Lease request for resource-consuming stages.
	LeaseKind       ResourceKind `json:"lease_kind,omitempty"`
	LeaseResourceID string       `json:"lease_resource_id,omitempty"`
	LeaseExpiry     int64        `json:"lease_expiry,omitempty"`

	// Shim and grout usage.
	ShimIDs    []string `json:"shim_ids,omitempty"`
	GroutLotID string   `json:"grout_lot_id,omitempty"`
	GroutGrams int64    `json:"grout_grams,omitempty"`

	// Measured values for metric-bearing stages.
	MeasuredX        int64 `json:"measured_x,omitempty"`
	MeasuredY        int64 `json:"measured_y,omitempty"`
	MeasuredZ        int64 `json:"measured_z,omitempty"`
	TiltX            int64 `json:"tilt_x,omitempty"`
	TiltY            int64 `json:"tilt_y,omitempty"`
	Scale            int   `json:"scale,omitempty"`
	BearingArea      int64 `json:"bearing_area,omitempty"`
	NominalArea      int64 `json:"nominal_area,omitempty"`
	Pretension       int64 `json:"pretension,omitempty"`
	TargetPretension int64 `json:"target_pretension,omitempty"`
	Force            int64 `json:"force,omitempty"`
	Displacement     int64 `json:"displacement,omitempty"`

	// Instrument reference for evidence sourced from a scripted call.
	InstrumentCallID string `json:"instrument_call_id,omitempty"`

	Payload string `json:"payload,omitempty"`
}

// OperationResult is the committed result of an operation, either a fresh
// advancement or an idempotent replay of a previous one.
type OperationResult struct {
	OperationID   string `json:"operation_id"`
	Unit          string `json:"unit"`
	PositionID    string `json:"position_id"`
	Generation    int    `json:"generation"`
	Stage         Stage  `json:"stage"`
	StageName     string `json:"stage_name"`
	EventSequence int64  `json:"event_sequence"`
	Replayed      bool   `json:"replayed"`
	ComponentID   string `json:"component_id,omitempty"`
	LeaseID       string `json:"lease_id,omitempty"`
}

// LeaseAcquireRequest asks for a time-limited mutual-exclusion lease keyed on
// logical time.
type LeaseAcquireRequest struct {
	OperationID string       `json:"operation_id"`
	Kind        ResourceKind `json:"kind"`
	ResourceID  string       `json:"resource_id"`
	Holder      string       `json:"holder"`
	PositionID  string       `json:"position_id"`
	Generation  int          `json:"generation"`
	LogicalTime int64        `json:"logical_time"`
	Expiry      int64        `json:"expiry"`
}

// InstrumentCallRequest drives a scripted instrument invocation. The script
// step deterministically reproduces rejection, disconnect, timeout and format
// errors so tests can rely on reproducible behaviour.
type InstrumentCallRequest struct {
	OperationID string         `json:"operation_id"`
	Instrument  InstrumentKind `json:"instrument"`
	ScriptStep  string         `json:"script_step"`
	LogicalTime int64          `json:"logical_time"`
}

// RetryInstrumentRequest carries the logical time at which a pending instrument
// call is retried. The retry is only honoured once the logical time reaches the
// call's scheduled next_retry_at, so a retry can never succeed before its
// planned time.
type RetryInstrumentRequest struct {
	LogicalTime int64 `json:"logical_time"`
}

// ImpactRequest registers a nonconformance and derives the deterministic
// affected-position closure.
type ImpactRequest struct {
	OperationID     string `json:"operation_id"`
	TriggerPosition string `json:"trigger_position"`
	Reason          string `json:"reason"`
}

// ReplacementRequest establishes a new generation for affected positions,
// binding a new bearing and recording the destination of the removed one.
type ReplacementRequest struct {
	OperationID string            `json:"operation_id"`
	Positions   []ReplacementItem `json:"positions"`
}

// ReplacementItem is one position's replacement in a new generation.
type ReplacementItem struct {
	PositionID             string `json:"position_id"`
	NewComponentID         string `json:"new_component_id"`
	NewManufactureBatch    string `json:"new_manufacture_batch"`
	NewConstructionSummary string `json:"new_construction_summary"`
	NewModel               string `json:"new_model"`
	OldDestination         string `json:"old_destination"`
}

// ReviewRequest submits an independent re-check by a qualified person.
type ReviewRequest struct {
	OperationID   string `json:"operation_id"`
	ReviewerID    string `json:"reviewer_id"`
	Qualification string `json:"qualification"`
	Opinion       string `json:"opinion"`
	LogicalTime   int64  `json:"logical_time"`
}

// TerminalRequest competes for the single-writer terminal outcome.
type TerminalRequest struct {
	OperationID string       `json:"operation_id"`
	Kind        TerminalKind `json:"kind"`
}

// UnitView is the read model returned by GET /api/v1/units/{unit}.
type UnitView struct {
	Unit             string             `json:"unit"`
	Generation       int                `json:"generation"`
	Snapshot         DesignSnapshot     `json:"snapshot"`
	Positions        []PositionView     `json:"positions"`
	ComponentBalance []ComponentBalance `json:"component_balance"`
	Lots             []LotBalance       `json:"lots"`
	Leases           []ResourceLease    `json:"leases"`
	PendingCalls     []InstrumentCall   `json:"pending_calls"`
	ImpactCases      []ImpactCase       `json:"impact_cases"`
	Reviews          []Review           `json:"reviews"`
	Unlocks          []UnlockEvent      `json:"unlocks"`
	Terminal         *TerminalDecision  `json:"terminal,omitempty"`
}

// PositionView is the per-position stage matrix and current binding.
type PositionView struct {
	PositionID  string          `json:"position_id"`
	Generation  int             `json:"generation"`
	Stage       Stage           `json:"stage"`
	StageName   string          `json:"stage_name"`
	ComponentID string          `json:"component_id,omitempty"`
	Destination string          `json:"destination,omitempty"`
	Evidence    []StageEvidence `json:"evidence"`
}

// StageEvidence is the append-only record of a single accepted stage.
type StageEvidence struct {
	Sequence      int64  `json:"sequence"`
	Unit          string `json:"unit"`
	PositionID    string `json:"position_id"`
	Generation    int    `json:"generation"`
	Stage         Stage  `json:"stage"`
	Holder        string `json:"holder"`
	LogicalTime   int64  `json:"logical_time"`
	PayloadDigest string `json:"payload_digest"`
}

// ComponentBalance reports the current destination and count status of a
// one-shot component.
type ComponentBalance struct {
	ID          string        `json:"id"`
	Kind        ComponentKind `json:"kind"`
	Destination string        `json:"destination"`
	PositionID  string        `json:"position_id,omitempty"`
}

// LotBalance reports a grout lot's remaining grams and expiry.
type LotBalance struct {
	ID        string `json:"id"`
	Remaining int64  `json:"remaining_grams"`
	Expiry    int64  `json:"expiry_logical_time"`
}

// LineageView is returned by GET /api/v1/units/{unit}/lineage.
type LineageView struct {
	Unit      string         `json:"unit"`
	Events    []LineageEvent `json:"events"`
	Positions []PositionView `json:"positions"`
}
