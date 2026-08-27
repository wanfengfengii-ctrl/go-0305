package httpapi

import (
	"encoding/json"
	"net/http"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
)

// decode decodes a JSON request body, returning a stable INVALID_REQUEST error
// on malformed input.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewBusinessError(
			domain.CodeInvalidRequest, "malformed JSON body", "", "body not valid JSON",
		))
		return false
	}
	return true
}

func (s *Server) handleCreateIsolationUnit(w http.ResponseWriter, r *http.Request) {
	var req domain.LockRequest
	if !decode(w, r, &req) {
		return
	}
	snap, err := s.svc.LockDesign(r.Context(), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) handleRegisterComponent(w http.ResponseWriter, r *http.Request) {
	var c domain.PhysicalComponent
	if !decode(w, r, &c) {
		return
	}
	if err := s.svc.RegisterComponent(r.Context(), c); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleRegisterLot(w http.ResponseWriter, r *http.Request) {
	var lot domain.ConsumableLot
	if !decode(w, r, &lot) {
		return
	}
	if err := s.svc.RegisterLot(r.Context(), lot); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, lot)
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	var req domain.OperationRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := s.svc.ApplyOperation(r.Context(), r.PathValue("unit"), r.PathValue("position"), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAcquireLease(w http.ResponseWriter, r *http.Request) {
	var req domain.LeaseAcquireRequest
	if !decode(w, r, &req) {
		return
	}
	lease, err := s.svc.AcquireLease(r.Context(), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

func (s *Server) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.svc.ReleaseLease(r.Context(), r.PathValue("lease"), req.Reason); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (s *Server) handleInstrumentCall(w http.ResponseWriter, r *http.Request) {
	var req domain.InstrumentCallRequest
	if !decode(w, r, &req) {
		return
	}
	call, err := s.svc.RecordInstrument(r.Context(), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, call)
}

func (s *Server) handleRetryInstrument(w http.ResponseWriter, r *http.Request) {
	var req domain.RetryInstrumentRequest
	if !decode(w, r, &req) {
		return
	}
	call, err := s.svc.RetryInstrument(r.Context(), r.PathValue("call"), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, call)
}

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	var req domain.ImpactRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := s.svc.Impact(r.Context(), r.PathValue("unit"), req.TriggerPosition, req.Reason)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleReplacement(w http.ResponseWriter, r *http.Request) {
	var req domain.ReplacementRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := s.svc.Replace(r.Context(), r.PathValue("unit"), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req domain.ReviewRequest
	if !decode(w, r, &req) {
		return
	}
	review, err := s.svc.SubmitReview(r.Context(), r.PathValue("unit"), req)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationID string `json:"operation_id"`
		LogicalTime int64  `json:"logical_time"`
	}
	if !decode(w, r, &req) {
		return
	}
	events, err := s.svc.Unlock(r.Context(), r.PathValue("unit"), req.OperationID, req.LogicalTime)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	var req domain.TerminalRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := s.svc.DecideTerminal(r.Context(), r.PathValue("unit"), req.Kind, req.OperationID)
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleGetUnit(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Unit(r.Context(), r.PathValue("unit"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetLineage(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Lineage(r.Context(), r.PathValue("unit"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
