package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"hospital-isolation-bearing-unlock-closure/internal/domain"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

// retryInterval is the deterministic logical-time spacing between instrument
// retry attempts, defined by the locking policy.
const retryInterval int64 = 1000

// scriptOutcome is the deterministic result of running one instrument script
// step at a given attempt number.
type scriptOutcome struct {
	status    string
	faultCode string
	reading   string
}

// runScript interprets a script step. A step may be a single outcome token or a
// pipe-separated sequence consumed attempt-by-attempt (attempt is 1-indexed).
// Recognised tokens: ok, reject, disconnect, timeout, malformed. Unknown tokens
// are treated as a stable fault so malformed scripts fail deterministically.
func runScript(step string, attempt int) scriptOutcome {
	tokens := strings.Split(step, "|")
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(tokens) {
		idx = len(tokens) - 1
	}
	token := strings.TrimSpace(tokens[idx])
	switch token {
	case "ok":
		return scriptOutcome{status: "success", reading: fmt.Sprintf("reading-%d", attempt)}
	case "reject":
		return scriptOutcome{status: "fault", faultCode: "REJECTED"}
	case "disconnect":
		return scriptOutcome{status: "retry", faultCode: "DISCONNECTED"}
	case "timeout":
		return scriptOutcome{status: "retry", faultCode: "TIMEOUT"}
	case "malformed":
		return scriptOutcome{status: "retry", faultCode: "MALFORMED"}
	default:
		return scriptOutcome{status: "fault", faultCode: "UNKNOWN_SCRIPT"}
	}
}

// RecordInstrument drives a scripted instrument invocation. Successful calls
// are recorded once; transient failures become deterministic pending-retry
// records. Instrument failures never become business readings.
func (s *Service) RecordInstrument(ctx context.Context, req domain.InstrumentCallRequest) (domain.InstrumentCall, error) {
	call := domain.InstrumentCall{
		ID:          callID(req.Instrument, req.OperationID),
		Instrument:  req.Instrument,
		ScriptStep:  req.ScriptStep,
		LogicalTime: req.LogicalTime,
		Attempt:     1,
	}
	out := runScript(req.ScriptStep, 1)
	call.Status = out.status
	call.FaultCode = out.faultCode
	call.RawDigest = digestString(out.reading + out.faultCode)
	if out.status == "retry" {
		call.NextRetryAt = req.LogicalTime + retryInterval
	}
	if err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.InsertCallTx(ctx, tx, call)
	}); err != nil {
		return domain.InstrumentCall{}, mapErr(err, req.OperationID, "call id conflict")
	}
	return call, nil
}

// RetryInstrument re-runs a pending instrument call, incrementing its attempt
// and advancing its deterministic retry schedule. Only calls in the 'retry'
// state may be retried, and only once the request's logical time has reached
// the call's scheduled next_retry_at, so a retry can never succeed before its
// planned time.
func (s *Service) RetryInstrument(ctx context.Context, callID string, req domain.RetryInstrumentRequest) (domain.InstrumentCall, error) {
	existing, err := s.store.Call(ctx, callID)
	if err != nil {
		return domain.InstrumentCall{}, mapErr(err, "", "call not found")
	}
	if existing.Status != "retry" {
		return domain.InstrumentCall{}, domain.NewBusinessError(domain.CodeInvalidRequest, "call is not retryable", "", "call status")
	}
	if req.LogicalTime < existing.NextRetryAt {
		return domain.InstrumentCall{}, domain.NewBusinessError(domain.CodeInvalidRequest, "retry called before scheduled retry time", "", "logical_time", "next_retry_at")
	}
	out := runScript(existing.ScriptStep, existing.Attempt+1)
	existing.Attempt++
	existing.Status = out.status
	existing.FaultCode = out.faultCode
	existing.RawDigest = digestString(out.reading + out.faultCode)
	if out.status == "retry" {
		existing.NextRetryAt = existing.LogicalTime + int64(existing.Attempt)*retryInterval
	} else {
		existing.NextRetryAt = 0
	}
	if err := s.store.Tx(ctx, func(tx store.DBTx) error {
		return store.UpdateCallTx(ctx, tx, existing)
	}); err != nil {
		return domain.InstrumentCall{}, mapErr(err, "", "call update failed")
	}
	return existing, nil
}

// SuccessfulCall reports whether an instrument call id references a successful,
// well-formed invocation. Only such calls may be referenced by evidence.
func (s *Service) SuccessfulCall(ctx context.Context, id string) (bool, error) {
	c, err := s.store.Call(ctx, id)
	if err != nil {
		return false, mapErr(err, "", "call not found")
	}
	return c.Status == "success", nil
}

func callID(kind domain.InstrumentKind, opID string) string {
	h := sha256.Sum256([]byte(opID))
	return fmt.Sprintf("%s-%s", kind, hex.EncodeToString(h[:8]))
}

func digestString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
