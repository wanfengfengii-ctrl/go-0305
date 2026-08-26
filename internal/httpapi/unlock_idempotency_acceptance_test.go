package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

func TestModel_UnlockRetryIsIdempotent(t *testing.T) {
	cases := []struct {
		name    string
		retries int
	}{
		{name: "single retry after disconnect", retries: 1},
		{name: "repeated retransmissions", retries: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			srv := newTestServer(t)
			handler := srv.Handler()

			lockReq := httptest.NewRequest(http.MethodPost, "/api/v1/isolation-units", bytes.NewBufferString(
				lockBody(`{"a":1,"b":0,"c":0,"d":0,"e":1,"f":0,"scale":1}`),
			))
			lockRec := httptest.NewRecorder()
			handler.ServeHTTP(lockRec, lockReq)
			if lockRec.Code != http.StatusCreated {
				t.Fatalf("lock status = %d body=%s", lockRec.Code, lockRec.Body.String())
			}

			for _, position := range []string{"P1", "P2"} {
				bearingID := "bearing-" + position
				lotID := "lot-" + position
				if err := srv.svc.RegisterComponent(ctx, domain.PhysicalComponent{
					ID: bearingID, Kind: domain.KindBearing, Model: "LRB-500", Status: "intake",
				}); err != nil {
					t.Fatalf("register bearing for %s: %v", position, err)
				}
				if err := srv.svc.RegisterLot(ctx, domain.ConsumableLot{
					ID: lotID, Unit: "U1", InitialGrams: 100, ExpiryLogicalTime: 1000,
				}); err != nil {
					t.Fatalf("register lot for %s: %v", position, err)
				}

				for stage := domain.StageIncomingAccepted; stage <= domain.StageUnlocked; stage++ {
					op := domain.OperationRequest{
						OperationID:        fmt.Sprintf("%s-%s", position, stage.String()),
						ExpectedGeneration: 1,
						LogicalTime:        int64(stage) + 1,
						Holder:             "qualified-operator",
						Stage:              stage,
					}
					if stage == domain.StageIncomingAccepted {
						op.ComponentID = bearingID
					}
					if stage == domain.StageGrouted {
						op.GroutLotID = lotID
						op.GroutGrams = 1
					}
					if _, err := srv.svc.ApplyOperation(ctx, "U1", position, op); err != nil {
						t.Fatalf("advance %s to %s: %v", position, stage, err)
					}
				}
			}

			for i, reviewer := range []string{"reviewer-a", "reviewer-b"} {
				if _, err := srv.svc.SubmitReview(ctx, "U1", domain.ReviewRequest{
					OperationID:   "review-" + reviewer,
					ReviewerID:    reviewer,
					Qualification: "qualified",
					Opinion:       "pass",
					LogicalTime:   int64(100 + i),
				}); err != nil {
					t.Fatalf("submit review %s: %v", reviewer, err)
				}
			}

			beforeUnlock, err := srv.svc.Events(ctx)
			if err != nil {
				t.Fatalf("count events before unlock: %v", err)
			}
			unlockBody := []byte(`{"operation_id":"unlock-disconnect-1","logical_time":200}`)
			postUnlock := func() []domain.UnlockEvent {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, "/api/v1/units/U1/unlock", bytes.NewReader(unlockBody))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("unlock status = %d body=%s", rec.Code, rec.Body.String())
				}
				var events []domain.UnlockEvent
				if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
					t.Fatalf("decode unlock response: %v", err)
				}
				return events
			}

			first := postUnlock()
			if len(first) != 2 || first[0].GroupIndex != 0 || first[0].PositionID != "P1" ||
				first[1].GroupIndex != 1 || first[1].PositionID != "P2" {
				t.Fatalf("first unlock batch has wrong synchronized order: %+v", first)
			}
			for retry := 0; retry < tc.retries; retry++ {
				replayed := postUnlock()
				if len(replayed) != len(first) {
					t.Fatalf("retry %d returned %d unlocks, want %d", retry+1, len(replayed), len(first))
				}
				for i := range first {
					if replayed[i].Unit != first[i].Unit || replayed[i].GroupIndex != first[i].GroupIndex ||
						replayed[i].PositionID != first[i].PositionID || replayed[i].LockDest != first[i].LockDest ||
						replayed[i].LogicalTime != first[i].LogicalTime {
						t.Fatalf("retry %d changed committed unlock at index %d: first=%+v replay=%+v", retry+1, i, first[i], replayed[i])
					}
				}
			}

			viewReq := httptest.NewRequest(http.MethodGet, "/api/v1/units/U1", nil)
			viewRec := httptest.NewRecorder()
			handler.ServeHTTP(viewRec, viewReq)
			if viewRec.Code != http.StatusOK {
				t.Fatalf("unit view status = %d body=%s", viewRec.Code, viewRec.Body.String())
			}
			var view domain.UnitView
			if err := json.Unmarshal(viewRec.Body.Bytes(), &view); err != nil {
				t.Fatalf("decode unit view: %v", err)
			}
			if len(view.Unlocks) != len(first) {
				t.Fatalf("unit view contains %d unlock events after retries, want %d: %+v", len(view.Unlocks), len(first), view.Unlocks)
			}
			afterRetries, err := srv.svc.Events(ctx)
			if err != nil {
				t.Fatalf("count events after retries: %v", err)
			}
			if afterRetries != beforeUnlock+1 {
				t.Fatalf("unlock appended %d DomainEvents, want exactly 1", afterRetries-beforeUnlock)
			}
		})
	}
}
