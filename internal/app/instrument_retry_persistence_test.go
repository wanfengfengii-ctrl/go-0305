package app

import (
	"context"
	"path/filepath"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_InstrumentRetryPersistence(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "successful retry survives restart and authorizes evidence",
			run: func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "instrument.db")
				st, err := storeOpen(ctx, path)
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				svc := New(st)

				first, err := svc.RecordInstrument(ctx, domain.InstrumentCallRequest{
					OperationID: "survey-1", Instrument: domain.InstrumentTotalStation,
					ScriptStep: "timeout|ok", LogicalTime: 700,
				})
				if err != nil {
					t.Fatalf("record instrument: %v", err)
				}
				if first.Attempt != 1 || first.Status != "retry" || first.FaultCode != "TIMEOUT" || first.NextRetryAt != 1700 {
					t.Fatalf("initial call = %+v, want attempt 1 TIMEOUT retry at 1700", first)
				}

				retried, err := svc.RetryInstrument(ctx, first.ID)
				if err != nil {
					t.Fatalf("retry instrument: %v", err)
				}
				if retried.Attempt != 2 || retried.Status != "success" || retried.FaultCode != "" || retried.RawDigest != digestString("reading-2") || retried.NextRetryAt != 0 {
					t.Fatalf("retry result = %+v, want fully populated attempt 2 success", retried)
				}
				if err := svc.Close(); err != nil {
					t.Fatalf("close before restart: %v", err)
				}

				reopened, err := storeOpen(ctx, path)
				if err != nil {
					t.Fatalf("reopen store: %v", err)
				}
				t.Cleanup(func() { _ = reopened.Close() })
				svc = New(reopened)
				persisted, err := reopened.Call(ctx, first.ID)
				if err != nil {
					t.Fatalf("load call after restart: %v", err)
				}
				if persisted.Attempt != retried.Attempt || persisted.Status != retried.Status || persisted.FaultCode != retried.FaultCode || persisted.RawDigest != retried.RawDigest || persisted.NextRetryAt != retried.NextRetryAt {
					t.Fatalf("persisted call = %+v, retry response = %+v", persisted, retried)
				}

				lockUnit(t, svc, "U-retry", 1)
				registerBearing(t, svc, "B-retry", "LRB-500", "batch-retry")
				req := op(domain.StageIncomingAccepted, "install-with-survey", 1800)
				req.ComponentID = "B-retry"
				req.InstrumentCallID = first.ID
				if _, err := svc.ApplyOperation(ctx, "U-retry", "P1", req); err != nil {
					t.Fatalf("successful retried call rejected as evidence: %v", err)
				}
				if _, err := svc.RetryInstrument(ctx, first.ID); !domain.IsBusinessError(err, domain.CodeInvalidRequest) {
					t.Fatalf("retrying persisted success: got %v, want INVALID_REQUEST", err)
				}
			},
		},
		{
			name: "repeated transient failure advances deterministic persisted schedule",
			run: func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "instrument.db")
				st, err := storeOpen(ctx, path)
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				svc := New(st)
				call, err := svc.RecordInstrument(ctx, domain.InstrumentCallRequest{
					OperationID: "survey-transient", Instrument: domain.InstrumentTotalStation,
					ScriptStep: "timeout|disconnect", LogicalTime: 25,
				})
				if err != nil {
					t.Fatalf("record instrument: %v", err)
				}
				call, err = svc.RetryInstrument(ctx, call.ID)
				if err != nil {
					t.Fatalf("retry instrument: %v", err)
				}
				if call.Attempt != 2 || call.Status != "retry" || call.FaultCode != "DISCONNECTED" || call.RawDigest != digestString("DISCONNECTED") || call.NextRetryAt != 2025 {
					t.Fatalf("second transient result = %+v, want persisted retry at 2025", call)
				}
				persisted, err := st.Call(ctx, call.ID)
				if err != nil {
					t.Fatalf("load retry record: %v", err)
				}
				if persisted.Attempt != call.Attempt || persisted.Status != call.Status || persisted.FaultCode != call.FaultCode || persisted.RawDigest != call.RawDigest || persisted.NextRetryAt != call.NextRetryAt {
					t.Fatalf("persisted call = %+v, retry response = %+v", persisted, call)
				}
				_ = svc.Close()
			},
		},
		{
			name: "terminal fault is neither a reading nor retryable",
			run: func(t *testing.T) {
				svc := newTestService(t)
				call, err := svc.RecordInstrument(ctx, domain.InstrumentCallRequest{
					OperationID: "survey-rejected", Instrument: domain.InstrumentTotalStation,
					ScriptStep: "reject", LogicalTime: 90,
				})
				if err != nil {
					t.Fatalf("record instrument: %v", err)
				}
				if call.Status != "fault" || call.FaultCode != "REJECTED" || call.NextRetryAt != 0 {
					t.Fatalf("terminal fault = %+v", call)
				}
				if ok, err := svc.SuccessfulCall(ctx, call.ID); err != nil || ok {
					t.Fatalf("SuccessfulCall(fault) = %v, %v; want false, nil", ok, err)
				}
				if _, err := svc.RetryInstrument(ctx, call.ID); !domain.IsBusinessError(err, domain.CodeInvalidRequest) {
					t.Fatalf("retrying fault: got %v, want INVALID_REQUEST", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
