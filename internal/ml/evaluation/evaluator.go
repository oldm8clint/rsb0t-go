package evaluation

import "rsb0t-go/internal/ml/replay"

// Summary contains model-independent episode metrics suitable for comparing policies.
type Summary struct {
	Episodes      int
	AverageReward float64
	AverageSteps  float64
	MinReward     float64
	MaxReward     float64
}

func Evaluate(episodes []*replay.Episode) Summary {
	if len(episodes) == 0 {
		return Summary{}
	}
	min, max := float64(episodes[0].TotalReward), float64(episodes[0].TotalReward)
	var reward float64
	var steps int
	for _, e := range episodes {
		r := float64(e.TotalReward)
		reward += r
		steps += len(e.Steps)
		if r < min {
			min = r
		}
		if r > max {
			max = r
		}
	}
	n := float64(len(episodes))
	return Summary{Episodes: len(episodes), AverageReward: reward / n, AverageSteps: float64(steps) / n, MinReward: min, MaxReward: max}
}
