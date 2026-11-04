package fishing

// Metrics summarizes outcomes from one deterministic fishing episode.
type Metrics struct {
	Steps      int
	Catches    int
	Failures   int
	Recoveries int
	Completed  bool
	Reward     float64
}

// Run executes a policy against the fixture and records observable outcomes.
// The policy remains independent of Q-learning so future LLM candidates can
// use the same measurement boundary.
func Run(f Fixture, policy func(Observation) Action, maxSteps int) Metrics {
	if maxSteps <= 0 {
		maxSteps = 1
	}
	o := f.Reset()
	var m Metrics
	for m.Steps < maxSteps {
		a := policy(o)
		n, reward, done := f.Step(o, a)
		m.Steps++
		m.Reward += reward
		if a == ActionFish && n.Inventory > o.Inventory {
			m.Catches += n.Inventory - o.Inventory
		}
		if a == ActionFish && reward < 0 {
			m.Failures++
		}
		if a == ActionRecover {
			m.Recoveries++
		}
		o = n
		if done {
			m.Completed = true
			return m
		}
	}
	return m
}
