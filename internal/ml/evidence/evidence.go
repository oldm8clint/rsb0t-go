// Package evidence converts experiment results into a small, bounded record that
// can be supplied to an offline script-refinement/LLM process.
package evidence

import (
	"fmt"
	"sort"

	"rsb0t-go/internal/ml/evaluation"
)

const (
	SchemaVersion      uint16 = 1
	MaxReasons                = 8
	MaxRecommendations        = 8
)

// Record is intentionally outcome-focused. It does not contain credentials,
// raw packets, source files, or unrestricted execution instructions.
type Record struct {
	SchemaVersion   uint16            `json:"schema_version"`
	Task            string            `json:"task"`
	Baseline        Metrics           `json:"baseline"`
	Candidate       Metrics           `json:"candidate"`
	Comparison      evaluation.Report `json:"comparison"`
	Observations    []Finding         `json:"findings,omitempty"`
	Recommendations []string          `json:"recommendations,omitempty"`
}

type Metrics struct {
	AverageReward float64 `json:"average_reward"`
	AverageSteps  float64 `json:"average_steps"`
}

type Finding struct {
	Kind    string  `json:"kind"`
	Message string  `json:"message"`
	Weight  float64 `json:"weight,omitempty"`
}

// Build turns a bounded evaluation report into evidence suitable for a future
// LLM prompt. Recommendations are deterministic and deliberately generic.
func Build(task string, baseline, candidate Metrics, report evaluation.Report) Record {
	r := Record{
		SchemaVersion: SchemaVersion,
		Task:          task,
		Baseline:      baseline,
		Candidate:     candidate,
		Comparison:    report,
	}
	if task == "" {
		r.Task = "unspecified"
	}
	if report.PassesGate {
		r.Observations = append(r.Observations, Finding{Kind: "improvement", Message: "candidate passed configured quality gates", Weight: 1})
		r.Recommendations = append(r.Recommendations, "preserve the candidate behaviour and validate it against additional seeded scenarios")
	} else {
		r.Observations = append(r.Observations, Finding{Kind: "regression", Message: "candidate failed one or more configured quality gates", Weight: 1})
		r.Recommendations = append(r.Recommendations, "do not promote the candidate; inspect the reported regression before generating another revision")
	}
	if report.RewardDeltaRatio > 0 {
		r.Observations = append(r.Observations, Finding{Kind: "reward", Message: fmt.Sprintf("reward changed by %.2f%%", report.RewardDeltaRatio*100), Weight: report.RewardDeltaRatio})
	}
	if report.StepDeltaRatio < 0 {
		r.Observations = append(r.Observations, Finding{Kind: "efficiency", Message: fmt.Sprintf("average steps changed by %.2f%%", report.StepDeltaRatio*100), Weight: -report.StepDeltaRatio})
	}
	for _, reason := range report.Reasons {
		if len(r.Observations) >= MaxReasons+2 {
			break
		}
		r.Observations = append(r.Observations, Finding{Kind: "gate", Message: reason})
	}
	if len(r.Recommendations) > MaxRecommendations {
		r.Recommendations = r.Recommendations[:MaxRecommendations]
	}
	sort.SliceStable(r.Observations, func(i, j int) bool { return r.Observations[i].Weight > r.Observations[j].Weight })
	if len(r.Observations) > MaxReasons+2 {
		r.Observations = r.Observations[:MaxReasons+2]
	}
	return r
}
