package evaluation

// Comparison reports whether a learned policy improved over a baseline.
type Comparison struct {
	Baseline    Summary
	Learned     Summary
	DeltaReward float64
	Improved    bool
}

func Compare(baseline, learned Summary) Comparison {
	return Comparison{Baseline: baseline, Learned: learned, DeltaReward: learned.AverageReward - baseline.AverageReward, Improved: learned.AverageReward > baseline.AverageReward}
}
