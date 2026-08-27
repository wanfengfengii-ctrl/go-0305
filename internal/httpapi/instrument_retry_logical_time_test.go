package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_InstrumentRetryHonorsLogicalTimeGate(t *testing.T) {
	type retryStep struct {
		logicalTime int64
		wantCode    int
		wantAttempt int
		wantStatus  string
		wantNext    int64
	}
	cases := []struct {
		name   string
		script string
		steps  []retryStep
	}{
		{
			name:   "success cannot be created before scheduled time",
			script: "timeout|ok",
			steps: []retryStep{
				{logicalTime: 1009, wantCode: http.StatusBadRequest},
				{logicalTime: 1010, wantCode: http.StatusOK, wantAttempt: 2, wantStatus: "success"},
			},
		},
		{
			name:   "transient retry keeps deterministic next schedule",
			script: "timeout|disconnect|ok",
			steps: []retryStep{
				{logicalTime: 1010, wantCode: http.StatusOK, wantAttempt: 2, wantStatus: "retry", wantNext: 2010},
				{logicalTime: 2009, wantCode: http.StatusBadRequest},
				{logicalTime: 2010, wantCode: http.StatusOK, wantAttempt: 3, wantStatus: "success"},
			},
		},
		{
			name:   "fault is produced at a time after scheduled time",
			script: "timeout|reject",
			steps: []retryStep{
				{logicalTime: 1011, wantCode: http.StatusOK, wantAttempt: 2, wantStatus: "fault"},
			},
		},
	}

	for caseIndex, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			body := fmt.Sprintf(`{"operation_id":"retry-gate-%d","instrument":"total_station","script_step":%q,"logical_time":10}`, caseIndex, tc.script)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/instrument-calls", bytes.NewBufferString(body))
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
			}

			var call domain.InstrumentCall
			if err := json.Unmarshal(rec.Body.Bytes(), &call); err != nil {
				t.Fatalf("decode initial call: %v", err)
			}
			if call.Attempt != 1 || call.Status != "retry" || call.NextRetryAt != 1010 {
				t.Fatalf("initial call = %+v, want attempt=1 status=retry next_retry_at=1010", call)
			}

			for stepIndex, step := range tc.steps {
				retryBody := fmt.Sprintf(`{"logical_time":%d}`, step.logicalTime)
				rec = httptest.NewRecorder()
				req = httptest.NewRequest(http.MethodPost, "/api/v1/instrument-calls/"+call.ID+"/retry", bytes.NewBufferString(retryBody))
				srv.Handler().ServeHTTP(rec, req)
				if rec.Code != step.wantCode {
					t.Fatalf("step %d at logical time %d: status = %d, body = %s, want %d", stepIndex, step.logicalTime, rec.Code, rec.Body.String(), step.wantCode)
				}

				if step.wantCode != http.StatusOK {
					var apiErr errorResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
						t.Fatalf("step %d: decode rejection: %v", stepIndex, err)
					}
					if apiErr.Code != domain.CodeInvalidRequest {
						t.Fatalf("step %d: error code = %q, want %q", stepIndex, apiErr.Code, domain.CodeInvalidRequest)
					}
					successful, err := srv.svc.SuccessfulCall(req.Context(), call.ID)
					if err != nil {
						t.Fatalf("step %d: inspect rejected call: %v", stepIndex, err)
					}
					if successful {
						t.Fatalf("step %d: rejected early retry created a success eligible for instrument_call_id evidence", stepIndex)
					}
					continue
				}

				if err := json.Unmarshal(rec.Body.Bytes(), &call); err != nil {
					t.Fatalf("step %d: decode call: %v", stepIndex, err)
				}
				if call.Attempt != step.wantAttempt || call.Status != step.wantStatus || call.NextRetryAt != step.wantNext {
					t.Fatalf("step %d: call = %+v, want attempt=%d status=%s next_retry_at=%d", stepIndex, call, step.wantAttempt, step.wantStatus, step.wantNext)
				}
			}
		})
	}
}
