package tickmanip

import "fmt"

// Plan says which steps the client writes for the server tick t of an arm (0 = the first tick the
// server processes the arm's first batch). A batch is written in ONE socket write right after the
// client sees the previous tick's PLAYER_INFO, so every step of Plan(t) is read by the server in the
// same cycle (as long as there are at most UserEventLimit of them).
type Plan func(t int) []Step

// NormalPlan is the baseline: click once, then leave the interaction to repeat itself.
func NormalPlan() Plan {
	return func(t int) []Step {
		if t == 0 {
			return []Step{Click}
		}
		return nil
	}
}

// ReclickPlan is the control for "does merely re-clicking help": Click every period ticks from offset,
// with no knife and so no %action_delay write.
func ReclickPlan(period, offset int) Plan {
	return func(t int) []Step {
		if period > 0 && t >= offset && (t-offset)%period == 0 {
			return []Step{Click}
		}
		return nil
	}
}

// ManipOptions shapes a knife-on-log manipulation.
type ManipOptions struct {
	// Period is the number of ticks between batches. 3 is the shortest cycle the mechanism allows: the
	// knife sets %action_delay to map_clock + 2 only when it was below map_clock, so the earliest a
	// batch can act is the tick AFTER a roll, and the roll it sets up lands two ticks later.
	Period int
	// Offset is the arm tick of the first batch.
	Offset int
	// Prefix are steps written before the knife pair in every batch (an extra interrupt).
	Prefix []Step
	// NoClick leaves the click out of the knife batch.
	NoClick bool
	// LoneClick, when positive, writes a Click alone that many ticks after each batch. Net and harpoon
	// spots need it at exactly 2: their scripts roll only when the click is read on the tick where
	// %action_delay = map_clock, and end the interaction otherwise.
	LoneClick int
}

// ManipPlan builds the periodic knife batch.
func ManipPlan(o ManipOptions) Plan {
	if o.Period <= 0 {
		o.Period = 3
	}
	return func(t int) []Step {
		if t < o.Offset {
			return nil
		}
		var out []Step
		if (t-o.Offset)%o.Period == 0 {
			out = append(out, o.Prefix...)
			out = append(out, Knife()...)
			if !o.NoClick {
				out = append(out, Click)
			}
		}
		// The lone click is written LoneClick ticks after each batch. When that is as late as the next
		// batch (LoneClick >= Period) it lands in the same cycle as that batch, after its steps, which is
		// exactly what an open-loop sender with those two timers would write.
		if o.LoneClick > 0 && t-o.Offset >= o.LoneClick && (t-o.Offset-o.LoneClick)%o.Period == 0 {
			out = append(out, Click)
		}
		return out
	}
}

// Recommended is the manipulation each skill's script rewards, as read from the content: a three-tick
// batch for trees and fly spots (their op script carries the interaction on by itself), and a batch
// plus a lone click exactly two ticks later for net and harpoon spots.
func Recommended(sk Skill) Plan {
	switch sk {
	case NetFishing, HarpoonFishing:
		return ManipPlan(ManipOptions{Period: 3, LoneClick: 2})
	}
	return ManipPlan(ManipOptions{Period: 3})
}

// Shift delays every step of p by k ticks (a client whose packets reach the server k cycles late).
func Shift(p Plan, k int) Plan {
	return func(t int) []Step {
		if t-k < 0 {
			return nil
		}
		return p(t - k)
	}
}

// Jittered applies a per-tick shift: late(t) is how many cycles the batch the plan wrote for tick t
// arrives late (0 = on time). Batches that land in the same cycle are concatenated in the order
// written. It models a connection whose delay moves around more than a tick.
func Jittered(p Plan, horizon int, late func(t int) int) Plan {
	arrive := map[int][]Step{}
	for t := 0; t < horizon; t++ {
		steps := p(t)
		if len(steps) == 0 {
			continue
		}
		at := t + late(t)
		arrive[at] = append(arrive[at], steps...)
	}
	return func(t int) []Step { return arrive[t] }
}

// Describe prints a plan's first n ticks, one line per tick that does anything, for the report.
func Describe(p Plan, n int) []string {
	var out []string
	for t := 0; t < n; t++ {
		if steps := p(t); len(steps) > 0 {
			out = append(out, fmt.Sprintf("t+%d: %s", t, StepsString(steps)))
		}
	}
	return out
}

// StepsString names a batch in packet order.
func StepsString(steps []Step) string {
	s := ""
	for i, st := range steps {
		if i > 0 {
			s += " , "
		}
		s += st.String()
	}
	return s
}

// UserPackets is how many user-event packets a batch is (every step is one).
func UserPackets(steps []Step) int { return len(steps) }

// PerHundred scales a count over a number of ticks to a rate per 100 ticks.
func PerHundred(count, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return 100 * float64(count) / float64(ticks)
}
