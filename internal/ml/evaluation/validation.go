package evaluation

import (
	"fmt"

	"rsb0t-go/internal/ml/replay"
)

// ValidateEpisodes checks the minimum evidence invariants required before an
// evaluation summary is treated as valid. It intentionally does not alter or
// score the episodes; callers can keep the validation step separate from
// Evaluate so malformed evidence cannot silently become a zero-valued metric.
func ValidateEpisodes(episodes []*replay.Episode) error {
	if len(episodes) == 0 {
		return fmt.Errorf("evaluation requires at least one episode")
	}
	for i, episode := range episodes {
		if episode == nil {
			return fmt.Errorf("episode %d is nil", i)
		}
	}
	return nil
}
