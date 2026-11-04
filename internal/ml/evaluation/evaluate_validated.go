package evaluation

import (
	"fmt"

	"rsb0t-go/internal/ml/replay"
)

// EvaluateValidated is the safe entry point for evaluation pipelines that need
// malformed input to be reported rather than causing a panic in Evaluate.
// The existing Evaluate function remains unchanged for compatibility.
func EvaluateValidated(episodes []*replay.Episode) (Summary, error) {
	if len(episodes) == 0 {
		return Summary{}, fmt.Errorf("evaluation requires at least one episode")
	}
	for i, episode := range episodes {
		if episode == nil {
			return Summary{}, fmt.Errorf("evaluation episode %d is nil", i)
		}
	}
	return Evaluate(episodes), nil
}
