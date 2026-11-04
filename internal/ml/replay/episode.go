package replay

import (
	"encoding/json"
	"io"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

type Step struct {
	Observation observation.State `json:"observation"`
	Action      policy.Action     `json:"action"`
	Confidence  float32           `json:"confidence"`
	Reward      float32           `json:"reward"`
}

type Episode struct {
	SchemaVersion uint16  `json:"schema_version"`
	EpisodeID     uint64  `json:"episode_id"`
	Steps         []Step  `json:"steps"`
	TotalReward   float32 `json:"total_reward"`
}

func (e *Episode) Add(s Step) { e.Steps = append(e.Steps, s); e.TotalReward += s.Reward }

// WriteJSONL emits one complete episode per line for streaming/offline datasets.
func WriteJSONL(w io.Writer, episodes []Episode) error {
	enc := json.NewEncoder(w)
	for _, e := range episodes {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
