package domain

import "testing"

func TestNewBusinessErrorSortsReasons(t *testing.T) {
	e := NewBusinessError(CodeInvalidGeometry, "bad", "op-9", "zeta", "alpha", "mid")
	if len(e.OrderedReasons) != 3 {
		t.Fatalf("reasons = %v", e.OrderedReasons)
	}
	if e.OrderedReasons[0] != "alpha" || e.OrderedReasons[2] != "zeta" {
		t.Fatalf("reasons not sorted: %v", e.OrderedReasons)
	}
	if e.Code != CodeInvalidGeometry || e.OperationID != "op-9" {
		t.Fatalf("bad error fields: %+v", e)
	}
}

func TestIsBusinessError(t *testing.T) {
	e := NewBusinessError(CodeLeaseBusy, "busy", "", "r1")
	if !IsBusinessError(e, CodeLeaseBusy) {
		t.Fatalf("expected LEASE_BUSY match")
	}
	if IsBusinessError(e, CodeLeaseExpired) {
		t.Fatalf("unexpected match")
	}
	if IsBusinessError(nil, CodeLeaseBusy) {
		t.Fatalf("nil must not match")
	}
}

func TestBusinessErrorString(t *testing.T) {
	e := NewBusinessError(CodeStaleSummary, "stale", "", "")
	if e.Error() == "" {
		t.Fatalf("empty error string")
	}
}
