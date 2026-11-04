package context

import (
	"encoding/json"
	"fmt"
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

// Request is the bounded, structured context supplied to an LLM or script generator.
// It intentionally contains no credentials, raw packets, mutable client objects, or
// unrestricted filesystem/network instructions.
type Request struct {
	SchemaVersion uint16            `json:"schema_version"`
	Task          string            `json:"task"`
	Observation   observation.State `json:"observation"`
	Actions       []policy.Action   `json:"allowed_actions"`
	Constraints   []string          `json:"constraints"`
	FailureCases  []string          `json:"known_failure_cases,omitempty"`
}

func New(task string, state observation.State) Request {
	return Request{SchemaVersion: 1, Task: task, Observation: state, Actions: []policy.Action{
		policy.WalkToFish, policy.Fish, policy.WalkToBank, policy.Bank, policy.Wait, policy.Recover,
	}, Constraints: []string{
		"Use only the allowed actions.",
		"Do not access raw protocol or credentials.",
		"Return deterministic, testable decisions.",
	}}
}

func (r Request) MarshalJSON() ([]byte, error) {
	if r.Task == "" {
		return nil, fmt.Errorf("task is required")
	}
	return json.Marshal(struct {
		SchemaVersion uint16            `json:"schema_version"`
		Task          string            `json:"task"`
		Observation   observation.State `json:"observation"`
		Actions       []policy.Action   `json:"allowed_actions"`
		Constraints   []string          `json:"constraints"`
		FailureCases  []string          `json:"known_failure_cases,omitempty"`
	}{r.SchemaVersion, r.Task, r.Observation, r.Actions, r.Constraints, r.FailureCases})
}
