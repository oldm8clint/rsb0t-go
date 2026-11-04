package learning

import "rsb0t-go/internal/ml/observation"

// EpisodeStats contains compact, experiment-friendly measurements from one episode.
// It deliberately stores aggregates rather than the full observation history.
type EpisodeStats struct {
	Episode       int
	Steps         int
	TotalReward   float32
	MeanReward    float32
	Terminal      bool
	StatesVisited int
}

// TrainingStats aggregates learning progress without retaining every transition.
type TrainingStats struct {
	Episodes          int
	Steps             int
	TotalReward       float32
	MeanEpisodeReward float32
	BestEpisodeReward float32
	StatesLearned     int
}

// ObserveStats is a small helper for callers that want to measure an episode
// without coupling the learner to a trace format.
type ObserveStats struct {
	Steps       int
	TotalReward float32
}

func (s *ObserveStats) Add(_ observation.State, reward float32) {
	s.Steps++
	s.TotalReward += reward
}

func (s ObserveStats) MeanReward() float32 {
	if s.Steps == 0 {
		return 0
	}
	return s.TotalReward / float32(s.Steps)
}
