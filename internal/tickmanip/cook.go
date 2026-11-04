package tickmanip

// Cooking cadence. content skill_cooking/scripts/cooking.rs2 `[label,cook_item]` does `p_delay(1)` before it removes the
// raw item and adds the cooked or burnt one, so a use (OPLOCU of a raw fish on a range) read in tick T:
//
//   - is accepted only if the player is not delayed when the packet handlers run (OpLocUHandler refuses while
//     player.delayed, like every op handler);
//   - sets delayedUntil = T+2 (PlayerOps P_DELAY: tick + 1 + n), and the handlers of tick T+2 still run before
//     processPlayers clears `delayed`, so uses read in T+1 and T+2 are refused;
//   - resumes in tick T+2 (processPlayers: "resume suspended script" runs once delayed is cleared), where the raw fish is
//     removed and the cooked or burnt one added.
//
// So the earliest accepted next use is T+3, and one cooked item per 3 ticks is the ceiling. CookUse is the step.
const (
	// CookDelay is the p_delay argument of cook_item.
	CookDelay = 1
)

// CookTrace is the model's answer for a plan of CookUse steps.
type CookTrace struct {
	// Accepted are the ticks a use was accepted, Refused the ticks a use was read and refused, Done the ticks the raw
	// fish was consumed (accepted + CookDelay + 1).
	Accepted, Refused, Done []int
}

// ReplayCook runs a plan of CookUse steps (any other step is ignored) over ticks arm ticks.
func ReplayCook(plan Plan, ticks int) CookTrace {
	var tr CookTrace
	delayedUntil := -1 << 30
	for t := 0; t < ticks; t++ {
		for _, st := range plan(t) {
			if st != CookUse {
				continue
			}
			if t <= delayedUntil {
				tr.Refused = append(tr.Refused, t)
				continue
			}
			tr.Accepted = append(tr.Accepted, t)
			tr.Done = append(tr.Done, t+CookDelay+1)
			delayedUntil = t + 1 + CookDelay
		}
	}
	return tr
}

// EveryPlan writes step every period ticks from first.
func EveryPlan(first, period int, steps ...Step) Plan {
	return func(t int) []Step {
		if period > 0 && t >= first && (t-first)%period == 0 {
			return steps
		}
		return nil
	}
}
