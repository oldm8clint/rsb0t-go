package replay

import (
	"bytes"
	"strings"
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

func TestEpisodeAccumulatesReward(t *testing.T) {
	var e Episode
	e.Add(Step{Action: policy.Fish, Reward: 1.5})
	e.Add(Step{Action: policy.Wait, Reward: -.5})
	if e.TotalReward != 1 {
		t.Fatalf("got %v", e.TotalReward)
	}
}

func TestWriteJSONL(t *testing.T) {
	var b bytes.Buffer
	err := WriteJSONL(&b, []Episode{{SchemaVersion: 1, EpisodeID: 7, Steps: []Step{{Observation: observation.State{SchemaVersion: 1}, Action: policy.Fish}}}})
	if err != nil || !strings.Contains(b.String(), "episode_id") {
		t.Fatalf("unexpected output: %v %q", err, b.String())
	}
}
