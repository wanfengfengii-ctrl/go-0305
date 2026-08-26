// Package httpapi exposes the Go HTTP API: JSON endpoints, the stable error
// response structure, idempotent operation entries and the static frontend
// console. It is the transport boundary for every domain component.
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"hospital-isolation-bearing-unlock-closure/internal/app"
	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// Server wires the application service behind the HTTP boundary and serves the
// built frontend console.
type Server struct {
	svc       *app.Service
	staticDir string
}

// New returns a Server bound to the given application service and the directory
// containing the built frontend assets.
func New(svc *app.Service, staticDir string) *Server {
	return &Server{svc: svc, staticDir: staticDir}
}

// Handler builds the HTTP routing table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health and readiness.
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Design locking and inventory intake.
	mux.HandleFunc("POST /api/v1/isolation-units", s.handleCreateIsolationUnit)
	mux.HandleFunc("POST /api/v1/components", s.handleRegisterComponent)
	mux.HandleFunc("POST /api/v1/lots", s.handleRegisterLot)

	// Position stage operations.
	mux.HandleFunc("POST /api/v1/units/{unit}/positions/{position}/operations", s.handleOperation)

	// Leases.
	mux.HandleFunc("POST /api/v1/leases/acquire", s.handleAcquireLease)
	mux.HandleFunc("POST /api/v1/leases/{lease}/release", s.handleReleaseLease)

	// Scripted instruments.
	mux.HandleFunc("POST /api/v1/instrument-calls", s.handleInstrumentCall)
	mux.HandleFunc("POST /api/v1/instrument-calls/{call}/retry", s.handleRetryInstrument)

	// Impacts and replacements.
	mux.HandleFunc("POST /api/v1/units/{unit}/impacts", s.handleImpact)
	mux.HandleFunc("POST /api/v1/units/{unit}/replacements", s.handleReplacement)

	// Review, unlock and terminal.
	mux.HandleFunc("POST /api/v1/units/{unit}/reviews", s.handleReview)
	mux.HandleFunc("POST /api/v1/units/{unit}/unlock", s.handleUnlock)
	mux.HandleFunc("POST /api/v1/units/{unit}/terminal", s.handleTerminal)

	// Read models.
	mux.HandleFunc("GET /api/v1/units/{unit}", s.handleGetUnit)
	mux.HandleFunc("GET /api/v1/units/{unit}/lineage", s.handleGetLineage)

	// Frontend console.
	mux.Handle("/", s.handleFrontend())
	return logRequests(mux)
}

// errorResponse is the uniform JSON error body shared by every business
// rejection.
type errorResponse struct {
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	OrderedReasons []string `json:"ordered_reasons,omitempty"`
	OperationID    string   `json:"operation_id,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFrontend() http.Handler {
	if s.staticDir == "" {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.Dir(s.staticDir))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// statusFor maps a stable business error code to an HTTP status.
func statusFor(err error) int {
	be, ok := err.(*domain.BusinessError)
	if !ok {
		return http.StatusInternalServerError
	}
	switch be.Code {
	case domain.CodeNotFound:
		return http.StatusNotFound
	case domain.CodeInvalidRequest:
		return http.StatusBadRequest
	case domain.CodeInvalidGeometry, domain.CodeArithmeticOverflow:
		return http.StatusUnprocessableEntity
	case domain.CodeLeaseBusy, domain.CodeLeaseExpired, domain.CodeComponentAlreadyBound,
		domain.CodeGenerationConflict, domain.CodeTerminalAlreadyDecided, domain.CodeIdempotencyConflict,
		domain.CodeStaleSummary:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	be, ok := err.(*domain.BusinessError)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Code:    domain.CodeInternal,
			Message: "internal error",
		})
		return
	}
	writeJSON(w, status, errorResponse{
		Code:           be.Code,
		Message:        be.Message,
		OrderedReasons: be.OrderedReasons,
		OperationID:    be.OperationID,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}
