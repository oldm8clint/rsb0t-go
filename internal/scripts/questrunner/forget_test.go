package questrunner

import "testing"

// ForgetFailure clears the named steps' failure flags (and a kit's blocked flag) and nothing else; the zero engine and unknown keys are fine.
func TestForgetFailureClearsOnlyTheNamedFlags(t *testing.T) {
	e := &Engine{failed: map[string]bool{"a": true, "b": true, "provision:kit": true}, blocked: map[string]bool{"provision:kit": true}}
	e.ForgetFailure("a", "provision:kit", "nothing")
	if e.failed["a"] || e.failed["provision:kit"] || e.blocked["provision:kit"] {
		t.Errorf("flags left after forgetting: failed %v blocked %v", e.failed, e.blocked)
	}
	if !e.failed["b"] {
		t.Error("a flag that was not named was cleared")
	}
	(&Engine{}).ForgetFailure("x") // nil maps
}
