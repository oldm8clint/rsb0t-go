package promotion

import (
	"rsb0t-go/internal/ml/evaluation"
	"testing"
)

func TestDecidePromotesPassingReport(t *testing.T) {
	d := Decide(evaluation.Report{PassesGate: true})
	if d.Status != Promote {
		t.Fatalf("status = %q, want %q", d.Status, Promote)
	}
}

func TestDecideRejectsWithFirstReason(t *testing.T) {
	d := Decide(evaluation.Report{PassesGate: false, Reasons: []string{"reward regression", "completion regression"}})
	if d.Status != Reject || d.Reason != "reward regression" {
		t.Fatalf("unexpected decision: %+v", d)
	}
}

func TestDecideRejectsWithoutReason(t *testing.T) {
	d := Decide(evaluation.Report{})
	if d.Status != Reject || d.Reason == "" {
		t.Fatalf("unexpected decision: %+v", d)
	}
}
