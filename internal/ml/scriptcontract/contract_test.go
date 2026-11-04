package scriptcontract

import (
	"rsb0t-go/internal/ml/policy"
	"testing"
)

func TestValidateAcceptsBoundedCandidate(t *testing.T) {
	v := Validate(Candidate{SchemaVersion: 1, Name: "fisher", Source: "return Fish", Actions: []policy.Action{policy.Fish}, Tests: []string{"fish_when_at_spot"}})
	if !v.Valid {
		t.Fatalf("unexpected errors: %v", v.Errors)
	}
}

func TestValidateRejectsEmptyAndUnknownActions(t *testing.T) {
	v := Validate(Candidate{SchemaVersion: 1, Name: "x", Actions: []policy.Action{"EXEC_RAW"}, Tests: []string{"x"}})
	if v.Valid || len(v.Errors) < 2 {
		t.Fatalf("expected source/action errors, got %#v", v.Errors)
	}
}
