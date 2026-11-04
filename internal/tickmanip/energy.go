package tickmanip

// Run energy, as the engine spends it. Everything here is read from Player.updateEnergy
// (engine/src/engine/entity/Player.ts, "updateEnergy"), which World.processPlayers calls once a tick
// AFTER processInteraction has moved the player:
//
//	if (this.delayed) return;                                  // p_delay: nothing happens this tick
//	if (this.stepsTaken < 2) recover (agility/6 | 0) + 8, capped at 10000
//	else lose (67 + 67 * clamp(weightKg, 0, 64) / 64) | 0, floored at 0
//
// `delayed` is set only by p_delay and friends (PlayerOps.ts P_DELAY: delayedUntil = tick + 1 + n) and
// cleared at the top of processPlayers on the first tick >= delayedUntil, so a p_delay(n) issued during
// tick T freezes energy on ticks T .. T+n, n+1 ticks. The client cannot act on the tick that clears it
// either: the packet handlers run BEFORE processPlayers and refuse everything while `delayed` is still
// set (OpHeldHandler.ts, OpHeldTHandler.ts, OpLocHandler.ts, OpNpcHandler.ts, MoveClickHandler.ts), so
// the earliest recast is tick T + n + 2.
//
// The energy value the client sees is UPDATE_RUNENERGY: p1(energy / 100 | 0), a whole percent.

// MaxEnergy is 100% in engine units.
const MaxEnergy = 10000

// RunLoss is the energy one running tick costs at a given carried weight in grams.
func RunLoss(weightGrams int) int {
	kg := float64(weightGrams) / 1000
	if kg < 0 {
		kg = 0
	}
	if kg > 64 {
		kg = 64
	}
	return int(67 + 67*kg/64)
}

// Recovery is the energy a tick that took fewer than two steps gives back at an Agility level.
func Recovery(agility int) int { return agility/6 + 8 }

// EnergyTick is one engine cycle as updateEnergy sees it.
type EnergyTick struct {
	// Steps is stepsTaken: 2 while running, 1 on a walk step or the last step of a leg, 0 standing.
	Steps int
	// Frozen is `this.delayed` at the time updateEnergy runs.
	Frozen bool
}

// ApplyTick applies one tick to an energy value in units.
func ApplyTick(units int, t EnergyTick, agility, weightGrams int) int {
	if t.Frozen {
		return units
	}
	if t.Steps < 2 {
		units += Recovery(agility)
		if units > MaxEnergy {
			units = MaxEnergy
		}
		return units
	}
	units -= RunLoss(weightGrams)
	if units < 0 {
		units = 0
	}
	return units
}

// Percent is what the client is told: whole percent, truncated.
func Percent(units int) int { return units / 100 }

// Trajectory is the percent the client sees after each tick, from a starting value in units.
func Trajectory(start int, ticks []EnergyTick, agility, weightGrams int) []int {
	out := make([]int, 0, len(ticks))
	u := start
	for _, t := range ticks {
		u = ApplyTick(u, t, agility, weightGrams)
		out = append(out, Percent(u))
	}
	return out
}

// FitStart asks which starting energies (in units) reproduce an observed percent series exactly. The
// first observation `before` is the percent seen before the first tick, which pins the start to a
// 100-unit band; every later observation must then equal the model's. It returns how many of the 100
// candidate starts survive: 0 means the tick pattern given does not describe what the engine did.
//
// This is the energy analogue of "the observed roll ticks equal the modelled ones": a total-energy
// figure can agree with a wrong pattern by luck, a per-tick percent series cannot.
func FitStart(before int, observed []int, ticks []EnergyTick, agility, weightGrams int) (survivors int, first int) {
	first = -1
	if len(observed) != len(ticks) {
		return 0, -1
	}
	for off := 0; off < 100; off++ {
		start := before*100 + off
		if start > MaxEnergy {
			continue
		}
		ok := true
		u := start
		for i, t := range ticks {
			u = ApplyTick(u, t, agility, weightGrams)
			if Percent(u) != observed[i] {
				ok = false
				break
			}
		}
		if ok {
			survivors++
			if first < 0 {
				first = start
			}
		}
	}
	return survivors, first
}

// DelayVariant describes a content action that ends in p_delay, for the prediction table.
type DelayVariant struct {
	Name string
	// DelayArg is n in p_delay(n).
	DelayArg int
	// StopsWalk is true when the script runs p_stopaction, which clears the walk queue: the account
	// stands still until the next move click.
	StopsWalk bool
	// File cites the script.
	File string
}

// Period is the shortest gap between two accepted uses: delay + 2 ticks (see the file comment).
func (v DelayVariant) Period() int { return v.DelayArg + 2 }

// FrozenTicks is how many ticks of each period updateEnergy is skipped: n + 1.
func (v DelayVariant) FrozenTicks() int { return v.DelayArg + 1 }

// UnitsPerTile predicts the energy cost per tile of a steady run that repeats the action as fast as it
// is accepted, for an action that does NOT stop the walk: one drain per period (the unfrozen tick),
// two tiles per tick on a run. A walk-stopping action is not a steady run at all; see PredictStopped.
func (v DelayVariant) UnitsPerTile(weightGrams int) float64 {
	loss := float64(RunLoss(weightGrams))
	return loss / float64(v.Period()) / 2
}

// BaselineUnitsPerTile is a plain run: a loss every tick, two tiles a tick.
func BaselineUnitsPerTile(weightGrams int) float64 { return float64(RunLoss(weightGrams)) / 2 }

// PredictStopped predicts the cost per tile of repeating a walk-stopping delay action while trying to
// keep walking, with the best cycle the client can write: a move click on the first tick the engine
// accepts one, the action on the next tick. For p_delay(n) that cycle is n+3 ticks: one running tick
// (two tiles, a drain), then the action's n+1 frozen ticks (no change), then ONE standing tick in which
// the delay has already cleared (it clears at the top of processPlayers, after the packet handlers
// refused the click) so energy recovers instead of freezing. Tiles per tick is 2 / (n+3).
func PredictStopped(v DelayVariant, agility, weightGrams int) (unitsPerTile float64, tilesPerTick float64) {
	cycle := v.DelayArg + 3
	net := float64(RunLoss(weightGrams)) - float64(Recovery(agility))
	return net / 2, 2 / float64(cycle)
}

// FrozenFromCasts marks the ticks of a window of n ticks that updateEnergy skipped: a p_delay(delayArg) accepted
// in tick c freezes ticks c .. c+delayArg (delayArg+1 ticks). Cast ticks outside the window are ignored.
func FrozenFromCasts(n int, casts []int, delayArg int) []bool {
	frozen := make([]bool, n)
	for _, c := range casts {
		for t := c; t <= c+delayArg; t++ {
			if t >= 0 && t < n {
				frozen[t] = true
			}
		}
	}
	return frozen
}

// StepsOf turns a tile series into the per-tick step counts updateEnergy reads: how far the player moved in the tick,
// capped at 2 (a run). The first entry is the tile before the window, so the result has len(tiles)-1 entries.
func StepsOf(xs, zs []int) []int {
	if len(xs) < 2 {
		return nil
	}
	out := make([]int, len(xs)-1)
	for i := 1; i < len(xs); i++ {
		dx, dz := xs[i]-xs[i-1], zs[i]-zs[i-1]
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		d := dx
		if dz > d {
			d = dz
		}
		if d > 2 {
			d = 2
		}
		out[i-1] = d
	}
	return out
}

// applyLoss is ApplyTick with the per-tick loss given directly: the client reads only whole kilograms of carried
// weight, so a fit tries each plausible loss rather than trusting a figure that is off by up to a kilogram.
func applyLoss(units int, t EnergyTick, agility, loss int) int {
	if t.Frozen {
		return units
	}
	if t.Steps < 2 {
		units += Recovery(agility)
		if units > MaxEnergy {
			units = MaxEnergy
		}
		return units
	}
	units -= loss
	if units < 0 {
		units = 0
	}
	return units
}

// FitLoss reports, for each candidate per-tick loss, how many of the 100 starting energies a whole percent allows
// reproduce the observed percent series exactly. A loss with survivors is consistent with the engine; one without
// is ruled out. `before` is the percent seen before the first tick.
func FitLoss(before int, observed []int, ticks []EnergyTick, agility int, losses []int) map[int]int {
	out := map[int]int{}
	if len(observed) != len(ticks) {
		return out
	}
	for _, loss := range losses {
		n := 0
		for off := 0; off < 100; off++ {
			start := before*100 + off
			if start > MaxEnergy {
				continue
			}
			ok := true
			u := start
			for i, t := range ticks {
				u = applyLoss(u, t, agility, loss)
				if Percent(u) != observed[i] {
					ok = false
					break
				}
			}
			if ok {
				n++
			}
		}
		out[loss] = n
	}
	return out
}

// UnitsUsed replays ticks from an exact starting energy with a fixed loss and returns the energy at the end and how
// many ticks drained (took the loss), recovered (a standing or one-step tick that was not frozen) and froze.
func UnitsUsed(start int, ticks []EnergyTick, agility, loss int) (end, drained, recovered, frozen int) {
	u := start
	for _, t := range ticks {
		switch {
		case t.Frozen:
			frozen++
		case t.Steps < 2:
			recovered++
		default:
			drained++
		}
		u = applyLoss(u, t, agility, loss)
	}
	return u, drained, recovered, frozen
}
