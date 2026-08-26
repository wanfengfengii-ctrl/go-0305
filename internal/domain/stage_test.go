package domain

import "testing"

func TestStageNextAndPrefix(t *testing.T) {
	s := StageIncomingAccepted
	for i := 0; i < 9; i++ {
		next, ok := s.Next()
		if !ok {
			t.Fatalf("expected next stage after %s", s)
		}
		if !IsNextStage(s, next) {
			t.Fatalf("IsNextStage(%s,%s) should be true", s, next)
		}
		s = next
	}
	if s != StageUnlocked {
		t.Fatalf("expected final stage unlocked, got %s", s)
	}
	if _, ok := s.Next(); ok {
		t.Fatalf("unlocked must not have a next stage")
	}
}

func TestStageSkippingRejected(t *testing.T) {
	if IsNextStage(StageIncomingAccepted, StageLeveled) {
		t.Fatalf("jumping from incoming to leveled must be rejected")
	}
	if IsNextStage(StageLeveled, StageGrouted) != true {
		t.Fatalf("leveled -> grouted must be accepted")
	}
	if IsNextStage(StageUnlocked, StageUnlocked) {
		t.Fatalf("unlocked -> unlocked must be rejected")
	}
}

func TestStageString(t *testing.T) {
	if StageIncomingAccepted.String() != "incoming_accepted" {
		t.Fatalf("unexpected name: %s", StageIncomingAccepted.String())
	}
	if StageUnlocked.String() != "unlocked" {
		t.Fatalf("unexpected name: %s", StageUnlocked.String())
	}
}

func TestStagePrefixMax(t *testing.T) {
	p := StagePrefix{StageIncomingAccepted, StageBaseAccepted, StagePlaced}
	if p.Max() != StagePlaced {
		t.Fatalf("max = %s", p.Max())
	}
	if !p.Has(StageBaseAccepted) || p.Has(StageGrouted) {
		t.Fatalf("Has mismatch")
	}
}
