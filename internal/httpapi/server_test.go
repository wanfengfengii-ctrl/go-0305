package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hospital-isolation-bearing-unlock-closure/internal/app"
	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(app.New(st), "")
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func lockBody(transform string) string {
	return `{
		"operation_id":"op-1","building":"A","unit":"U1","summary_version":"v1",
		"transform":` + transform + `,
		"adjacency":[["P1","P2"]],
		"sync_unlock_group":[["P1"],["P2"]],
		"positions":[{
			"building":"A","unit":"U1","axis_grid":"1-A","position_id":"P1",
			"design_center":{"x":0,"y":0,"z":0},
			"orientation":{"x":0,"y":0,"z":1,"scale":1},
			"bearing_model":"LRB-500",
			"upper":{"id":"u1","orientation":{"x":0,"y":0,"z":1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
			"lower":{"id":"l1","orientation":{"x":0,"y":0,"z":-1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
			"allowed_eccentricity":5000,"allowed_tilt":1000,"tilt_scale":3,
			"max_shim_thickness":20000,"max_shim_layers":4
		},{
			"building":"A","unit":"U1","axis_grid":"1-B","position_id":"P2",
			"design_center":{"x":0,"y":0,"z":0},
			"orientation":{"x":0,"y":0,"z":1,"scale":1},
			"bearing_model":"LRB-500",
			"upper":{"id":"u2","orientation":{"x":0,"y":0,"z":1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
			"lower":{"id":"l2","orientation":{"x":0,"y":0,"z":-1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
			"allowed_eccentricity":5000,"allowed_tilt":1000,"tilt_scale":3,
			"max_shim_thickness":20000,"max_shim_layers":4
		}]
	}`
}

func TestCreateIsolationUnit(t *testing.T) {
	srv := newTestServer(t)
	body := lockBody(`{"a":1,"b":0,"c":0,"d":0,"e":1,"f":0,"scale":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/isolation-units", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snap domain.DesignSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.LockDigest == "" || snap.Generation != 1 {
		t.Fatalf("bad snapshot: %+v", snap)
	}
}

func TestCreateIsolationUnitRejectsInvalidGeometry(t *testing.T) {
	srv := newTestServer(t)
	body := `{"operation_id":"op-2","building":"A","unit":"U1","summary_version":"v1",
		"transform":{"a":1,"b":1,"c":0,"d":1,"e":1,"f":0,"scale":1},
		"positions":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/isolation-units", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var e errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if e.Code != domain.CodeInvalidGeometry {
		t.Fatalf("code = %s", e.Code)
	}
}
