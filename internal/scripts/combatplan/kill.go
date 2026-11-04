package combatplan

// Kill detection, and where a kill's drops land.
//
// The problem this solves is the one docs/trustworthy_harnesses.md lists as trap 4: an NPC
// that is no longer in the client's NPC list may have died, or walked out of render range,
// or despawned, or been killed by somebody else. Counting that absence as a kill is how a
// harness passes with no confirmed death, and it is equally how a training script would
// mis-count XP, mis-attribute loot and walk to a corpse that was never there.
//
// So this file deliberately separates a confirmed kill from a merely likely one. The only
// evidence this client actually has is:
//
//   - The NPC's health, which arrives on a hitmark in NPC_INFO's extended block and is what
//     gamestate.NPC.Health/TotalHealth carry. TotalHealth stays 0 until a hitmark has ever
//     landed on that NPC, so "Health == 0" is meaningless on its own — an NPC nobody has hit
//     reads exactly the same as a dead one.
//   - Whether the NPC is still listed at all.
//
// Health reaching 0 on an NPC we were fighting is a real death. Disappearance alone is not,
// and JudgeKill says so rather than guessing.
//
// A loot filter already exists and is NOT rewritten here: internal/scripts/fighterplan's
// WantsLoot/IsBone/LootFilter port the TypeScript fighter's own filter, and a combat script
// should reuse it. What was missing, and is here, is the death signal and the rule tying a
// drop on the ground to the kill that produced it.

// TargetState is one observation of a target NPC, taken from gamestate.NPC.
type TargetState struct {
	// Index is the NPC's own list index, stable while it is visible.
	Index int
	// Present is whether the NPC is still in the client's NPC list at all.
	Present bool
	// Health and TotalHealth come from the last hitmark seen on this NPC. TotalHealth is 0
	// until one has arrived, which is what HealthKnown reports.
	Health, TotalHealth int
	// FightingUs is whether this NPC was facing us — the difference between "our target
	// died" and "something died nearby".
	FightingUs bool
}

// HealthKnown reports whether a hitmark has ever landed on this NPC, which is the only thing
// that makes its Health value mean anything.
func (t TargetState) HealthKnown() bool { return t.TotalHealth > 0 }

// Verdict is what one before/after pair of observations proves about a target.
type Verdict string

const (
	// Alive means the target is still listed with health above zero.
	Alive Verdict = "alive"
	// Killed means health was observed at zero: a real, confirmed death.
	Killed Verdict = "killed"
	// LikelyKilled means the NPC vanished while we were fighting it and its last known
	// health was low, but no zero-health reading was ever seen. Good enough to move on
	// and look for loot; NOT good enough for a harness to report a kill.
	LikelyKilled Verdict = "likely-killed"
	// Vanished means the NPC is simply gone with no evidence either way — out of range,
	// despawned, or taken by another player.
	Vanished Verdict = "vanished"
	// Unknown means there is nothing to judge yet.
	Unknown Verdict = "unknown"
)

// Confirmed reports whether a verdict is one a live proof may count as a kill.
func (v Verdict) Confirmed() bool { return v == Killed }

// lowHealthFraction is how far a target's health must have fallen for its disappearance to
// count as LikelyKilled rather than Vanished. A quarter is deliberately conservative: an NPC
// at 80% health that vanishes almost certainly walked away.
const lowHealthFraction = 4

// JudgeKill decides what happened to a target between two observations.
//
// before is the last reading taken while the NPC was known to be there; after is the current
// one. The two must describe the same NPC index — a reused index is a different NPC, and the
// caller is responsible for not pairing them.
func JudgeKill(before, after TargetState) Verdict {
	if after.Present {
		if after.HealthKnown() && after.Health <= 0 {
			return Killed
		}
		if after.HealthKnown() {
			return Alive
		}
		// Present but never hit: nothing is proven, and it is certainly not dead.
		return Unknown
	}
	// Gone. The only honest evidence is what the last reading said.
	if !before.Present {
		return Unknown
	}
	if before.HealthKnown() && before.Health <= 0 {
		// Zero health was actually observed before it left the list.
		return Killed
	}
	if before.FightingUs && before.HealthKnown() && before.Health*lowHealthFraction <= before.TotalHealth {
		return LikelyKilled
	}
	return Vanished
}

// DropOffset is how far south and west of an NPC's reported tile its drops can land.
//
// The client reports an NPC at the tile under its centre. For a 2x2 or 3x3 NPC that centre is
// one tile north-east of the south-west corner where drops actually appear, and two tiles for
// a 4x4 or 5x5 — the same allowance internal/scripts/fighterplan.BoneFromKill makes, ported
// from the TypeScript fighter's own comment on this.
const DropOffset = 2

// KillSite is where and when a target died, so later drops can be attributed to it.
type KillSite struct {
	X, Z, Level int
	// AtMs is the wall-clock millisecond the death was judged, for windowing.
	AtMs int64
}

// DropFromKill reports whether a ground item at (x, z, level) plausibly came from one of the
// recent kills: it must lie within DropOffset tiles south and west of the recorded tile, on
// the same level, within windowMs of the death.
//
// Why it is not a plain radius: the offset is directional. Allowing the same slack north and
// east would sweep in another player's drops on the far side of the corpse.
func DropFromKill(x, z, level int, kills []KillSite, nowMs, windowMs int64) bool {
	for _, k := range kills {
		if k.Level != level || nowMs-k.AtMs > windowMs {
			continue
		}
		dx, dz := k.X-x, k.Z-z
		if dx >= 0 && dx <= DropOffset && dz >= 0 && dz <= DropOffset {
			return true
		}
	}
	return false
}

// ApproachDecision says what a runner should do about a ground item it wants.
type ApproachDecision string

const (
	// TakeNow means we are close enough that the server will honour the Take.
	TakeNow ApproachDecision = "take"
	// WalkCloser means walk toward it first — OpObj does not walk on its own.
	WalkCloser ApproachDecision = "walk"
	// TooFar means the item is outside the leash and should be left alone.
	TooFar ApproachDecision = "too-far"
)

// DecideApproach chooses between taking a ground item and walking to it first.
//
// This is the "ground-item taking has no approach walk" gap: internal/actions.OpObj sends the
// take for a tile the account may be nowhere near, and the server simply ignores it, so a
// caller that never walks silently picks nothing up. distance is Chebyshev tiles to the item,
// takeRadius is how close the server will accept the click from (1 is adjacent), and leash is
// how far the script is willing to travel for it at all.
func DecideApproach(distance, takeRadius, leash int) ApproachDecision {
	if distance <= takeRadius {
		return TakeNow
	}
	if leash > 0 && distance > leash {
		return TooFar
	}
	return WalkCloser
}
