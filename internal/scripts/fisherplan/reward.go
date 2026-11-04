package fisherplan

// TraceProgress is the small set of deltas the ML observation bridge (see
// internal/scripts/fisherrunner/tracebridge.go and
// docs/ml_lab_real_observation_bridge.md) measures around one live Loop call to score
// its outcome. It carries no I/O and no internal/ml package types, matching this
// package's own "pure, no client/actions/fleet/bot imports" rule from its doc comment —
// the mapping to internal/ml/policy.Action and internal/ml/observation.State happens
// entirely in fisherrunner, the impure half.
type TraceProgress struct {
	// FishCaught is raw fish gained this Loop call — only ever nonzero coming out of
	// PhaseFish (fisherBot.fishStep's own before/after CurrentRawFish delta).
	FishCaught int
	// BankTrip is true once a real catch actually reached the bank this call — mirrors
	// fisherBot.bankStep's own cyclesDone counter (see its doc comment for why an
	// incidental coin-only deposit right after PhaseBuy must not count).
	BankTrip bool
	// ToolBought is true once PhaseBuy actually bought the ladder's next tool.
	ToolBought bool
	// SoldGP is coins gained from selling (the starter kit, or fish) this call.
	SoldGP int
}

// PhaseReward scores one live Loop call's outcome on the same scale
// internal/ml/environment.FisherEnv's synthetic Step already uses for its own six
// actions (+1 a real catch, +.5 a real bank trip, -.01 an ordinary walk/travel leg, -.05
// an idle wait, -1 stuck with nothing to do and no tool), so a real trace.Decision and a
// synthetic one stay comparable to the same evaluator rather than merely similarly
// shaped. This is the ProgressiveFisher reward definition
// docs/ml_lab_real_observation_bridge.md's "Reward semantics" section asks be documented
// and kept stable for a given experiment version — bump the experiment id passed to
// fisherrunner.NewTraceRecorder if this mapping ever changes, so old and new evidence are
// never silently compared as if equivalent.
func PhaseReward(phase FishPhase, p TraceProgress) float32 {
	switch phase {
	case PhaseFish:
		if p.FishCaught > 0 {
			return float32(p.FishCaught)
		}
		return -0.1
	case PhaseBank:
		if p.BankTrip {
			return 0.5
		}
		return -0.01
	case PhaseBuy:
		if p.ToolBought {
			return 1
		}
		return -0.1
	case PhaseSell:
		if p.SoldGP > 0 {
			// Scaled down from raw gp so one expensive sale doesn't dwarf a whole
			// session's worth of +1-per-fish rewards.
			return float32(p.SoldGP) / 100
		}
		return -0.1
	case PhaseFund, PhaseTravel:
		return -0.01
	case PhaseWait:
		return -0.05
	case PhaseStuck:
		return -1
	default:
		// PhaseAccept, PhaseTutorial, PhaseBright, PhaseStore: one-time new-account
		// setup, not part of the fish/bank/buy/sell loop this reward scores.
		return 0
	}
}
