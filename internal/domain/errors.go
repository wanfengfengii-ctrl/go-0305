package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Stable business error codes returned by the HTTP API and used by clients to
// branch on failure without parsing messages.
const (
	CodeStaleSummary           = "STALE_SUMMARY"
	CodeInvalidGeometry        = "INVALID_GEOMETRY"
	CodeArithmeticOverflow     = "ARITHMETIC_OVERFLOW"
	CodeIdempotencyConflict    = "IDEMPOTENCY_CONFLICT"
	CodeLeaseBusy              = "LEASE_BUSY"
	CodeLeaseExpired           = "LEASE_EXPIRED"
	CodeComponentAlreadyBound  = "COMPONENT_ALREADY_BOUND"
	CodeGenerationConflict     = "GENERATION_CONFLICT"
	CodeTerminalAlreadyDecided = "TERMINAL_ALREADY_DECIDED"
	CodeNotFound               = "NOT_FOUND"
	CodeInvalidRequest         = "INVALID_REQUEST"
	CodeInternal               = "INTERNAL"
)

// BusinessError is a stable, machine-readable rejection. It carries a code, a
// human message, an ordered list of field-level reasons and the operation id
// that triggered the failure, matching the uniform JSON error structure
// described by the public interface contract.
type BusinessError struct {
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	OrderedReasons []string `json:"ordered_reasons,omitempty"`
	OperationID    string   `json:"operation_id,omitempty"`
}

func (e *BusinessError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewBusinessError builds a BusinessError with the given stable code and
// message. Reasons are sorted by the domain key ordering so that identical
// failures always serialize identically.
func NewBusinessError(code, message, operationID string, reasons ...string) *BusinessError {
	sort.Strings(reasons)
	return &BusinessError{
		Code:           code,
		Message:        message,
		OrderedReasons: reasons,
		OperationID:    operationID,
	}
}

// IsBusinessError reports whether err is (or wraps) a BusinessError with the
// given stable code.
func IsBusinessError(err error, code string) bool {
	be, ok := err.(*BusinessError)
	if !ok {
		return false
	}
	return be.Code == code
}

// JoinReasons concatenates ordered reasons into a single deterministic string,
// primarily for tests and log output.
func JoinReasons(reasons []string) string {
	return strings.Join(reasons, "; ")
}
