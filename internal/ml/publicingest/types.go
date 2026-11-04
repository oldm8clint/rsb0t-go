package publicingest

import "time"

// Lesson is the portable, privacy-filtered result produced by the public
// TypeScript laboratory. It contains evidence, not executable code.
type Lesson struct {
	SchemaVersion string     `json:"schema_version"`
	Source        string     `json:"source"`
	ExperimentID  string     `json:"experiment_id"`
	StrategyID    string     `json:"strategy_id"`
	Seed          int64      `json:"seed"`
	CreatedAt     time.Time  `json:"created_at"`
	Goal          string     `json:"goal"`
	Episodes      int        `json:"episodes"`
	Metrics       Metrics    `json:"metrics"`
	Evidence      []Evidence `json:"evidence"`
}

type Metrics struct {
	SuccessRate float64 `json:"success_rate"`
	MeanReward  float64 `json:"mean_reward"`
	MeanSteps   float64 `json:"mean_steps"`
}

type Evidence struct {
	Claim      string  `json:"claim"`
	Confidence float64 `json:"confidence"`
	SampleSize int     `json:"sample_size"`
}
