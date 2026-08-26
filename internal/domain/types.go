package domain

// All lengths, coordinates, elevations and effective dimensions are stored as
// signed integer microns. Angles, stiffness, damping, compression ratios,
// bearing ratios and deviations are stored as signed fixed-point values with an
// explicit decimal scale (see the fixed package).

// Point3 is a three-dimensional point in signed integer microns.
type Point3 struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
	Z int64 `json:"z"`
}

// Orientation is a unit normal direction in integer fixed-point components.
// The scale describes the fixed-point denominator of the three components.
type Orientation struct {
	X     int64 `json:"x"`
	Y     int64 `json:"y"`
	Z     int64 `json:"z"`
	Scale int   `json:"scale"`
}

// PlanTransform is a two-dimensional affine transform in the plan (X/Y) plane:
//
//	x' = (A*x + B*y)/Scale + C
//	y' = (D*x + E*y)/Scale + F
//
// A transform is invertible when its determinant A*E - B*D is non-zero.
type PlanTransform struct {
	A     int64 `json:"a"`
	B     int64 `json:"b"`
	C     int64 `json:"c"`
	D     int64 `json:"d"`
	E     int64 `json:"e"`
	F     int64 `json:"f"`
	Scale int   `json:"scale"`
}

// ConnectionInterface describes one side (upper or lower pier) of a design
// position: the interface orientation, embedded plate dimensions and anchor
// bolt hole group.
type ConnectionInterface struct {
	ID          string      `json:"id"`
	Orientation Orientation `json:"orientation"`
	PlateWidth  int64       `json:"plate_width"`  // microns
	PlateLength int64       `json:"plate_length"` // microns
	HoleCount   int         `json:"hole_count"`
	HolePattern string      `json:"hole_pattern"`
}

// DesignPosition is a single bearing seat within an isolation unit. Position
// identities are stable within a snapshot and unique across the snapshot.
type DesignPosition struct {
	Building            string              `json:"building"`
	Unit                string              `json:"unit"`
	AxisGrid            string              `json:"axis_grid"`
	PositionID          string              `json:"position_id"`
	DesignCenter        Point3              `json:"design_center"`
	Orientation         Orientation         `json:"orientation"`
	SupportElevation    int64               `json:"support_elevation"` // microns
	BearingModel        string              `json:"bearing_model"`
	Upper               ConnectionInterface `json:"upper"`
	Lower               ConnectionInterface `json:"lower"`
	AllowedEccentricity int64               `json:"allowed_eccentricity"` // microns
	AllowedTilt         int64               `json:"allowed_tilt"`         // fixed-point
	TiltScale           int                 `json:"tilt_scale"`
	MaxShimThickness    int64               `json:"max_shim_thickness"` // microns, total shim stack
	MaxShimLayers       int                 `json:"max_shim_layers"`    // number of shim plates
}

// SamplingMap associates a manufactured batch with the positions it must be
// sampled from.
type SamplingMap map[string][]string

// Thresholds holds the fixed-point acceptance bounds for derived metrics.
type Thresholds struct {
	MaxEccentricity  int64 `json:"max_eccentricity"`
	MaxTilt          int64 `json:"max_tilt"`
	MaxBearingRatio  int64 `json:"max_bearing_ratio"`
	MaxPretensionDev int64 `json:"max_pretension_dev"`
	Scale            int   `json:"scale"`
}

// DesignSnapshot is the immutable result of locking an isolation unit. Once
// locked, it can only be corrected by creating a new generation, never by
// overwriting history in place.
type DesignSnapshot struct {
	Generation      int              `json:"generation"`
	Building        string           `json:"building"`
	Unit            string           `json:"unit"`
	SummaryVersion  string           `json:"summary_version"`
	Transform       PlanTransform    `json:"transform"`
	Positions       []DesignPosition `json:"positions"`
	Adjacency       [][2]string      `json:"adjacency"`         // force-transfer edges between PositionID pairs
	SyncUnlockGroup [][]string       `json:"sync_unlock_group"` // ordered groups of PositionID for unlock ordering
	Sampling        SamplingMap      `json:"sampling"`
	Thresholds      Thresholds       `json:"thresholds"`
	LockDigest      string           `json:"lock_digest"`
}

// ComponentKind classifies the one-shot physical components tracked by the
// inventory manager.
type ComponentKind string

const (
	KindBearing       ComponentKind = "bearing"
	KindTransportLock ComponentKind = "transport_lock"
	KindAnchorBolt    ComponentKind = "anchor_bolt"
	KindShim          ComponentKind = "shim"
)

// PhysicalComponent is a uniquely identified one-shot component.
type PhysicalComponent struct {
	ID                  string
	Kind                ComponentKind
	Model               string
	ManufactureBatch    string
	ConstructionSummary string
	Status              string
	CurrentPosition     string
	Destination         string
	ThicknessMicron     int64
	ShimCount           int
}

// LineageEvent records a binding, removal or replacement. History is
// append-only and may never be updated or deleted.
type LineageEvent struct {
	Sequence    int64
	Unit        string
	PositionID  string
	ComponentID string
	Kind        string // bound | removed | replaced
	Generation  int
	LogicalTime int64
}

// ConsumableLot is a grout pour with an initial integer gram quantity and a
// logical availability deadline.
type ConsumableLot struct {
	ID                string
	Unit              string
	InitialGrams      int64
	ExpiryLogicalTime int64
}

// InventoryMovement is an append-only deduction or return of consumables.
type InventoryMovement struct {
	Sequence    int64
	LotID       string
	DeltaGrams  int64
	LogicalTime int64
}

// ResourceKind enumerates the mutually exclusive construction resources that
// can be leased.
type ResourceKind string

const (
	ResourceHoist        ResourceKind = "hoist_position"
	ResourceMeasure      ResourceKind = "measure_channel"
	ResourceTorqueTool   ResourceKind = "torque_tool"
	ResourceGroutStation ResourceKind = "grout_station"
	ResourceTestMachine  ResourceKind = "test_machine"
)

// ResourceLease is a time-limited mutual-exclusion lease keyed on logical time.
type ResourceLease struct {
	ID            string
	Resource      ResourceKind
	ResourceID    string
	Holder        string
	PositionID    string
	Generation    int
	AcquiredAt    int64
	ExpiresAt     int64
	ReleaseReason string
}

// InstrumentKind enumerates the scripted instruments.
type InstrumentKind string

const (
	InstrumentTotalStation    InstrumentKind = "total_station"
	InstrumentDigitalLevel    InstrumentKind = "digital_level"
	InstrumentGapGauge        InstrumentKind = "gap_gauge"
	InstrumentTorqueCollector InstrumentKind = "torque_collector"
	InstrumentTestMachine     InstrumentKind = "test_machine"
)

// InstrumentCall is a scripted instrument invocation with a deterministic retry
// schedule. Only successful, well-formed calls may be referenced by evidence.
type InstrumentCall struct {
	ID          string
	Instrument  InstrumentKind
	ScriptStep  string
	LogicalTime int64
	Attempt     int
	Status      string // pending | retry | success | fault
	FaultCode   string
	RawDigest   string
	NextRetryAt int64
}

// ImpactCase is the deterministic closure of positions affected by a single
// nonconformance, plus its unique digest.
type ImpactCase struct {
	ID              string
	Unit            string
	TriggerPosition string
	Reason          string
	Positions       []string
	Digest          string
	Isolated        bool
}

// ReplacementGeneration records a new generation for affected positions and
// the destination of the removed old bearing.
type ReplacementGeneration struct {
	Unit           string
	Generation     int
	PositionID     string
	OldComponentID string
	OldDestination string
	ReviewResult   string
}

// Review is an independent re-check by a qualified person.
type Review struct {
	ID            string
	Unit          string
	ReviewerID    string
	Qualification string
	Opinion       string
	LogicalTime   int64
}

// UnlockEvent records the release of transport locks in deterministic order.
type UnlockEvent struct {
	Sequence    int64
	Unit        string
	GroupIndex  int
	PositionID  string
	LockDest    string
	LogicalTime int64
}

// TerminalKind is the final arbitration outcome.
type TerminalKind string

const (
	TerminalHandover   TerminalKind = "handover"
	TerminalQuarantine TerminalKind = "quarantine"
	TerminalCancel     TerminalKind = "cancel"
)

// TerminalDecision is the single winning terminal event for a unit.
type TerminalDecision struct {
	Unit             string
	Kind             TerminalKind
	Version          int64
	CredentialDigest string
}

// IdempotencyRecord replays a committed response for a repeated operation id,
// or detects content conflicts. Generation and ComponentID capture the original
// committed outcome so a replay can return it verbatim even after the position
// has advanced to a later replacement generation.
type IdempotencyRecord struct {
	Scope          string
	OperationID    string
	RequestDigest  string
	ResponseDigest string
	EventSequence  int64
	LogicalTime    int64
	Generation     int
	ComponentID    string
}

// DomainEvent is the append-only audit event envelope.
type DomainEvent struct {
	Sequence      int64
	Unit          string
	Type          string
	PayloadDigest string
	LogicalTime   int64
}
