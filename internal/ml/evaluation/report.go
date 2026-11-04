package evaluation

import "fmt"

// GateConfig defines conservative thresholds for deciding whether a learned
// candidate is worth sending to the next validation stage. It is intentionally
// independent of the live client.
type GateConfig struct {
	MinRewardDelta       float64
	MaxStepIncreaseRatio float64
}

// Report is a compact, machine-readable explanation of a baseline/candidate
// comparison. It is suitable for the ML Lab UI and for constructing bounded LLM
// context without exposing raw client state.
type Report struct {
	Comparison
	RewardDeltaRatio float64  `json:"reward_delta_ratio"`
	StepDeltaRatio   float64  `json:"step_delta_ratio"`
	PassesGate       bool     `json:"passes_gate"`
	Reasons          []string `json:"reasons,omitempty"`
}

// BuildReport applies explicit quality gates. A candidate must improve reward
// by at least MinRewardDelta and must not increase average steps beyond the
// configured ratio. If no step limit is supplied, step count is informational.
func BuildReport(baseline, learned Summary, cfg GateConfig) Report {
	cmp := Compare(baseline, learned)
	r := Report{Comparison: cmp}
	if baseline.AverageReward != 0 {
		r.RewardDeltaRatio = cmp.DeltaReward / baseline.AverageReward
	}
	if baseline.AverageSteps != 0 {
		r.StepDeltaRatio = (learned.AverageSteps - baseline.AverageSteps) / baseline.AverageSteps
	}

	passReward := cmp.DeltaReward >= cfg.MinRewardDelta
	passSteps := cfg.MaxStepIncreaseRatio <= 0 || r.StepDeltaRatio <= cfg.MaxStepIncreaseRatio
	r.PassesGate = passReward && passSteps
	if !passReward {
		r.Reasons = append(r.Reasons, fmt.Sprintf("reward delta %.4f is below required %.4f", cmp.DeltaReward, cfg.MinRewardDelta))
	}
	if !passSteps {
		r.Reasons = append(r.Reasons, fmt.Sprintf("average steps increased by %.2f%%", r.StepDeltaRatio*100))
	}
	if r.PassesGate {
		r.Reasons = append(r.Reasons, "candidate meets configured evaluation gates")
	}
	return r
}
