package domain

import "context"

// LockRequest carries the raw proposal submitted by the quality lead to lock an
// isolation unit design.
type LockRequest struct {
	OperationID     string           `json:"operation_id"`
	Building        string           `json:"building"`
	Unit            string           `json:"unit"`
	SummaryVersion  string           `json:"summary_version"`
	Transform       PlanTransform    `json:"transform"`
	Positions       []DesignPosition `json:"positions"`
	Adjacency       [][2]string      `json:"adjacency"`
	SyncUnlockGroup [][]string       `json:"sync_unlock_group"`
	Sampling        SamplingMap      `json:"sampling"`
	Thresholds      Thresholds       `json:"thresholds"`
}

// DesignCatalog validates and locks an isolation unit design snapshot. It
// corresponds to the "隔震设计与材料规则目录" component (acceptance 1, 2, 5).
type DesignCatalog interface {
	Lock(ctx context.Context, req LockRequest) (DesignSnapshot, error)
}

// PositionOperator advances a position through the strict ten-stage prefix,
// binding components, deducting materials and acquiring leases atomically. It
// corresponds to the "安装任务及身份谱系聚合" component (acceptance 3, 4, 7).
type PositionOperator interface {
	ApplyOperation(ctx context.Context, unit, position string, req OperationRequest) (OperationResult, error)
	Lineage(ctx context.Context, unit string) (LineageView, error)
}

// LeaseManager maintains one-shot component conservation and logical-time
// mutual-exclusion leases for hoist positions, measure channels, torque tools,
// grout stations and test machines (acceptance 3, 4).
type LeaseManager interface {
	AcquireLease(ctx context.Context, req LeaseAcquireRequest) (ResourceLease, error)
	ReleaseLease(ctx context.Context, leaseID, reason string) error
}

// EvidenceRecorder accepts scripted instrument results, validates stage, lease,
// generation, material freshness and fixed-point metrics, and appends valid
// evidence, failed calls and deterministic retry plans (acceptance 5, 6).
type EvidenceRecorder interface {
	RecordInstrument(ctx context.Context, req InstrumentCallRequest) (InstrumentCall, error)
	RetryInstrument(ctx context.Context, callID string) (InstrumentCall, error)
}

// Arbiter computes unique impact closures, establishes replacement generations,
// performs dual review, ordered unlock and the single-writer terminal
// competition (acceptance 7, 8).
type Arbiter interface {
	Impact(ctx context.Context, unit, triggerPosition, reason string) (ImpactCase, error)
	Replace(ctx context.Context, unit string, req ReplacementRequest) ([]ReplacementGeneration, error)
	DecideTerminal(ctx context.Context, unit string, kind TerminalKind, operationID string) (TerminalDecision, error)
}
