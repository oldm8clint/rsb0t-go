package sandbox

import (
	"context"
	"rsb0t-go/internal/ml/environment"
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/scriptcontract"
	"testing"
)

// fisherAdapter satisfies replay.Environment (Reset/Step returning the tuple shape) over
// *environment.FisherEnv, which exposes Observe/Step(StepResult) instead. This mirrors the
// identically-shaped adapter cmd/mlrun/main.go already defines for the same reason: FisherEnv is
// a plain, adapter-agnostic environment reused by more than one caller, each of which wraps it its
// own way rather than the environment itself depending on replay's interface (that one lives in
// package main and can't be imported here, so it's duplicated rather than shared).
type fisherAdapter struct{ env *environment.FisherEnv }

func (a fisherAdapter) Reset(seed int64) observation.State {
	_ = seed
	*a.env = *environment.NewFisherEnv()
	return a.env.Observe()
}

func (a fisherAdapter) Step(action policy.Action) (observation.State, float32, bool) {
	r := a.env.Step(action)
	return r.Observation, r.Reward, r.Done
}

func TestRunUsesDeclaredActionsAndStepBudget(t *testing.T) {
	candidate := scriptcontract.Candidate{SchemaVersion: 1, Name: "test", Source: "ignored", Actions: []policy.Action{policy.Wait}, Tests: []string{"offline"}}
	env := fisherAdapter{env: environment.NewFisherEnv()}
	result := Run(context.Background(), env, candidate, Config{MaxSteps: 3})
	if result.Steps != 3 {
		t.Fatalf("steps=%d", result.Steps)
	}
	if result.Valid {
		t.Fatal("expected step-budget exhaustion to fail validation")
	}
}

func TestExecuteSourceAlwaysDisabled(t *testing.T) {
	if err := ExecuteSource("package main"); err != ErrExecutionDisabled {
		t.Fatalf("err=%v", err)
	}
}
