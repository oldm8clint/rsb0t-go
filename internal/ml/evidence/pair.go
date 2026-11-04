package evidence

import "rsb0t-go/internal/ml/evaluation"

// FromPair converts the canonical matched evaluation into the bounded evidence
// record used by future refinement systems. It exposes aggregate outcomes,
// not source code, credentials, packets, or live handles.
func FromPair(task string, result evaluation.PairResult) Record {
	return Build(task,
		Metrics{AverageReward: result.Baseline.AverageReward, AverageSteps: result.Baseline.AverageSteps},
		Metrics{AverageReward: result.Candidate.AverageReward, AverageSteps: result.Candidate.AverageSteps},
		result.Report,
	)
}
