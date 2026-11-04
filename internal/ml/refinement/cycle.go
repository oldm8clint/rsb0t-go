// Package refinement connects offline evaluation, bounded evidence, and an
// LLM advisor without giving the advisor access to the live client.
package refinement

import (
	"context"
	"fmt"

	"rsb0t-go/internal/ml/evaluation"
	"rsb0t-go/internal/ml/evidence"
	"rsb0t-go/internal/ml/llm"
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

type CycleResult struct {
	Prior          evaluation.PairResult
	Evidence       evidence.Record
	Recommendation policy.Recommendation
	Proposal       evaluation.PairResult
}

type advisorPolicy struct {
	advisor  llm.Advisor
	evidence evidence.Record
}

func (p advisorPolicy) Recommend(s observation.State) policy.Recommendation {
	r, err := p.advisor.Recommend(p.evidence, s)
	if err != nil {
		return policy.Recommendation{Action: policy.Recover, Confidence: 0}
	}
	return r
}

// Run evaluates a prior, converts the result to bounded evidence, asks the
// advisor for a structured recommendation, then evaluates the advisor policy.
// It never executes generated source or exposes a live client handle.
func Run(ctx context.Context, env replay.Environment, baseline, prior policy.Advisor, advisor llm.Advisor, task string, cfg evaluation.PairConfig) (CycleResult, error) {
	if advisor == nil {
		return CycleResult{}, fmt.Errorf("llm advisor is required")
	}
	priorResult, err := evaluation.RunPair(ctx, env, baseline, prior, cfg)
	if err != nil {
		return CycleResult{}, fmt.Errorf("prior evaluation: %w", err)
	}

	e := evidence.FromPair(task, priorResult)
	initial := observation.State{SchemaVersion: 1, Health: 100, InventorySize: 28}
	recommendation, err := advisor.Recommend(e, initial)
	if err != nil {
		return CycleResult{}, fmt.Errorf("advisor recommendation: %w", err)
	}
	if recommendation.Confidence < 0 || recommendation.Confidence > 1 {
		return CycleResult{}, fmt.Errorf("advisor returned invalid confidence")
	}

	proposalResult, err := evaluation.RunPair(ctx, env, baseline, advisorPolicy{advisor: advisor, evidence: e}, cfg)
	if err != nil {
		return CycleResult{}, fmt.Errorf("proposal evaluation: %w", err)
	}
	return CycleResult{Prior: priorResult, Evidence: e, Recommendation: recommendation, Proposal: proposalResult}, nil
}
