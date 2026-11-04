// Package flinchplan is MeleeFlincher's own decisions, ported from rsb0t-scripts'
// flinchPlan.ts: which stage of the hit-and-hide cycle the run is in, whether the target's
// combat timer has reset, how the settle (the margin held past the reset cue) adapts to what
// swings actually cost, and how clean the run has been. It is pure: no client, actions,
// fleet or bot imports and no I/O.
//
// Target choice (pickTarget), eating (shouldEat) and chat-line classification (isDeathLine)
// stay in internal/scripts/fighterplan/fighterfood, exactly as flinchPlan.ts imports them
// from fighterPlan.ts rather than duplicating them — this package imports fighterplan for
// the same reason.
//
// Deliberately not ported here, and why:
//   - flinchHud.ts / hudPanel.ts — the Electron wall's on-screen overlay; no headless
//     equivalent.
//   - prayer flicking (bestFlickPrayers, FLICK_PRAYERS/ATTACK_PRAYERS/STRENGTH_PRAYERS) —
//     this Go client has no Prayer scripting surface at all (see fighterplan's own doc
//     comment on the same gap). The run simply fights unprayed; a future port can add this
//     once a Prayer primitive exists.
package flinchplan

import "regexp"

// LEASH is how far from the safe tile a target may stand and still be worth flinching.
const LEASH = 8

// RESET_TICKS_TYPICAL is what the reset cue costs in ticks, for logging only — the client
// shows a health bar for six seconds after the last combat event (ten ticks), which sits
// inside the eight-to-fifteen monsters vary over, so the bar going out is the cue itself.
const RESET_TICKS_TYPICAL = 10

// RESET_MAX_TICKS is how many ticks the bars are given to clear before the run stops
// waiting and swings anyway.
const RESET_MAX_TICKS = 25

// SETTLE_TICKS is the ticks held after both bars have cleared, before stepping out — the
// margin covering the gap between the client's fixed six-second bar window and a monster's
// own eight-to-fifteen-tick timer.
const SETTLE_TICKS = 2

// SETTLE_MAX is the most the settle grows to when swings keep costing hits.
const SETTLE_MAX = 6

// SETTLE_EASE_AFTER is how many clean swings in a row before the settle eases back down by
// a tick.
const SETTLE_EASE_AFTER = 5

// SettleState is ticks currently held after the bars clear, and how many swings in a row
// have cost nothing since it last moved.
type SettleState struct {
	Ticks    int
	CleanRun int
}

// FreshSettle is the starting settle for a new run or a new target.
var FreshSettle = SettleState{Ticks: SETTLE_TICKS, CleanRun: 0}

// NextSettle ports nextSettle: the settle after one swing, found by feel since a monster's
// timer cannot be read directly — a swing that cost a hit buys a tick, and a run of clean
// ones gives a tick back, settling on the shortest wait that particular target actually
// needs.
func NextSettle(s SettleState, tookHit bool) SettleState {
	if tookHit {
		ticks := s.Ticks + 1
		if ticks > SETTLE_MAX {
			ticks = SETTLE_MAX
		}
		return SettleState{Ticks: ticks, CleanRun: 0}
	}
	cleanRun := s.CleanRun + 1
	if cleanRun >= SETTLE_EASE_AFTER && s.Ticks > SETTLE_TICKS {
		return SettleState{Ticks: s.Ticks - 1, CleanRun: 0}
	}
	return SettleState{Ticks: s.Ticks, CleanRun: cleanRun}
}

// ---- has the target's combat timer reset ----

// ResetVerdict is what waiting on the safe tile has found.
type ResetVerdict string

const (
	ResetWaiting ResetVerdict = "waiting"
	ResetReady   ResetVerdict = "ready"
	ResetTimeout ResetVerdict = "timeout"
)

// ResetInput is what ResetVerdictFrom reads — ported from flinchPlan.ts's ResetInput.
type ResetInput struct {
	// MyBar is our own health bar showing (Game.inCombat()).
	MyBar bool
	// TargetBar is the target's health bar showing.
	TargetBar bool
	// TicksWaited is ticks spent on the safe tile since the last swing.
	TicksWaited int
	// TicksClear is ticks both bars have been gone for, unbroken.
	TicksClear int
	// SettleTicks is ticks to hold past the cue; 0 uses SETTLE_TICKS.
	SettleTicks int
	// MaxTicks is the timeout; 0 uses RESET_MAX_TICKS.
	MaxTicks int
}

// ResetVerdictFrom ports resetVerdict: both bars gone is the cue, and the settle is the
// margin on it — the target is idle, so it owes a tick of reaction delay when we step back
// out, and that tick is the one the flinch spends. A timeout is not a reset: it means the
// setup is not holding, and the caller says so rather than pretending.
func ResetVerdictFrom(i ResetInput) ResetVerdict {
	settleTicks := i.SettleTicks
	if settleTicks <= 0 {
		settleTicks = SETTLE_TICKS
	}
	maxTicks := i.MaxTicks
	if maxTicks <= 0 {
		maxTicks = RESET_MAX_TICKS
	}
	if !i.MyBar && !i.TargetBar {
		if i.TicksClear >= settleTicks {
			return ResetReady
		}
		return ResetWaiting
	}
	if i.TicksWaited >= maxTicks {
		return ResetTimeout
	}
	return ResetWaiting
}

// ---- the cycle ----

// FlinchStage is one stage of the hit-and-hide cycle.
type FlinchStage string

const (
	StageEat     FlinchStage = "eat"
	StageRetreat FlinchStage = "retreat"
	StageReset   FlinchStage = "reset"
	StageSeek    FlinchStage = "seek"
	StageStrike  FlinchStage = "strike"
)

// StageInput is what DecideStage reads — ported from flinchPlan.ts's StageInput.
type StageInput struct {
	// HasTarget is true when a live target is held.
	HasTarget bool
	// OnSafeTile is true when we are standing on the safe tile.
	OnSafeTile bool
	// Swung is true when the weapon has swung and the step back has not happened yet.
	Swung bool
	// Reset is true when the target's combat timer has reset, or the wait for it has been
	// given up on.
	Reset bool
	// NeedEat is true when health is low enough to eat and there is food to eat.
	NeedEat bool
	// Urgent is true when health is low enough that the bite comes before the step back.
	Urgent bool
}

// DecideStage ports decideStage: one stage per pass. Getting back to the safe tile
// outranks everything but a bite at urgent health — the swing is only free because the step
// back beats the target's reaction delay, and every tick spent out in the open is a tick
// that delay can finish in.
func DecideStage(i StageInput) FlinchStage {
	mustRetreat := i.Swung || !i.OnSafeTile
	if i.NeedEat && (i.Urgent || !mustRetreat) {
		return StageEat
	}
	if mustRetreat {
		return StageRetreat
	}
	if !i.HasTarget {
		return StageSeek
	}
	if !i.Reset {
		return StageReset
	}
	return StageStrike
}

// ---- how well it is going ----

// FlinchTally is what FlinchQuality reads.
type FlinchTally struct {
	Swings    int
	HitsTaken int
}

// FlinchQuality ports flinchQuality: the share of swings that came away without a hit
// landing on us, 0-1.
func FlinchQuality(t FlinchTally) float64 {
	if t.Swings <= 0 {
		return 1
	}
	q := 1 - float64(t.HitsTaken)/float64(t.Swings)
	if q < 0 {
		return 0
	}
	if q > 1 {
		return 1
	}
	return q
}

// ---- settings ----

var tileRe = regexp.MustCompile(`^\s*(-?\d{1,5})\s*[,\s]\s*(-?\d{1,5})\s*(?:[,\s]\s*([0-3]))?\s*$`)

// ParseTile ports parseTile: the safe tile typed as "x,z" or "x,z,level". ok is false for
// blank (meaning the tile the run is started on) and for anything that is not a pair of
// coordinates, which the caller reports rather than quietly standing somewhere else.
func ParseTile(raw string) (x, z, level int, ok bool) {
	m := tileRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, 0, 0, false
	}
	x = atoiSafe(m[1])
	z = atoiSafe(m[2])
	if m[3] != "" {
		level = atoiSafe(m[3])
	}
	return x, z, level, true
}

func atoiSafe(s string) int {
	neg := false
	i := 0
	if len(s) > 0 && s[0] == '-' {
		neg = true
		i = 1
	}
	n := 0
	for ; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n
}
