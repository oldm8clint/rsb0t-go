package bot

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatplan"
)

// Kill judgement for the two scripts whose fight loop does NOT go through
// combatrunner.FightOne: AdvancedFighter (advancedfighter.go) and MeleeFlincher
// (meleeflincher.go). Both hold one target across Loop calls and run their own wait, so
// combatrunner.WaitForKill's OnKill hook — the one every FightOne caller inherits, and the one
// CombatTrainer, the Fishing Guild fighter and ScarabSwarm's Stage 3 are wired to — never fires
// for either of them.
//
// WHY A SHARED WATCH RATHER THAN A SECOND INLINE TEST IN EACH. Both scripts already decide a
// target died with the same expression, `!found || (TotalHealth > 0 && Health == 0)`, and both
// were then stuck for the same reason: once an NPC leaves the client's NPC list, its type id
// leaves with it. gamestate.NPC zero-values on a miss, so `live.Type` in the vanished branch is
// 0, not a Cow, and reporting it would file every such kill under npc type 0.
//
// What that cost, read off the code on 2026-09-22:
//
//   - AdvancedFighter counted the kill in b.kills and then reported it to the loot panel only
//     `if found`. Every kill whose corpse left the list between two 600 ms polls was counted by
//     the run and missing from the panel, so the panel's kill count was quietly lower than the
//     script's own.
//   - MeleeFlincher reported nothing at all: it incremented b.kills in held() and no line in
//     that file ever touched the loot sink.
//
// So this keeps the last reading in which the target was PRESENT and weighs the pair with
// combatplan.JudgeKill — the same function, the same lastPresent/npcType pair and the same
// reason as combatrunner.WaitForKill, which is the behaviour the other combat scripts already
// report through. Pure: it takes readings, holds no session, and records nothing itself.
type killWatch struct {
	// index is the NPC list index being watched, or -1 for nothing.
	index int
	// npcType, x, z and level come from the last reading in which the target was present —
	// the only place a kill's identity and tile can be read from once it is gone.
	npcType     int
	x, z, level int
	// last is that same reading in combatplan's shape, JudgeKill's `before`.
	last combatplan.TargetState
	// reported is set once killed() has handed this target's death out, so a fight loop that
	// asks twice cannot double-count it.
	reported bool
}

// observe feeds one poll of the target a script is holding and returns what this reading and
// the last present one prove about it.
//
// present is whether the NPC was still in the client's list; n is that reading and is ignored
// when present is false (it is the zero NPC then, not the target). fightingUs is whether it was
// facing us, which is the only thing separating combatplan's LikelyKilled from its Vanished.
//
// A different index, or an index whose death has already been reported, starts a fresh watch:
// NPC list indexes are reused, so the next NPC to arrive under one must be judged on its own
// readings and must still be countable.
func (w *killWatch) observe(index int, n gamestate.NPC, present, fightingUs bool) combatplan.Verdict {
	if index != w.index || w.reported {
		*w = killWatch{index: index, npcType: -1}
	}
	after := combatplan.TargetState{Index: index, Present: present}
	if present {
		after.Health, after.TotalHealth, after.FightingUs = n.Health, n.TotalHealth, fightingUs
		w.last = after
		w.npcType, w.x, w.z, w.level = n.Type, n.X, n.Z, n.Level
	}
	return combatplan.JudgeKill(w.last, after)
}

// killed reports the npc type and tile to attribute a death to, and ok=false when this verdict
// is not a death or nothing was ever read that could name one.
//
// WHICH VERDICTS COUNT, AND WHAT EVIDENCE EACH ACTUALLY IS. Killed is a zero-health reading:
// a confirmed death. LikelyKilled and Vanished are NOT confirmed deaths — they are "the NPC we
// were holding left the list", which combatplan's own doc comment is explicit about (it may
// have died, walked out of range, despawned, or been killed by somebody else).
//
// LikelyKilled is counted anyway, and deliberately, for three reasons:
//
//   - It is the same choice combatrunner.WaitForKill's OnKill already makes, and the panel has
//     to agree with itself across scripts rather than count a CombatTrainer kill and drop the
//     identical event from a flincher.
//   - Both callers fight inside a leash — AdvancedFighter within cfg.leashRadius of its anchor,
//     MeleeFlincher within 8 tiles of a safe tile it returns to after every single swing — so
//     the account is not the thing that moved out of view, and both hold exactly the NPC they
//     have been swinging at (MeleeFlincher additionally counts only a target it has landed at
//     least one swing on).
//   - The alternative undercounts every fight that ends on the tick the corpse is removed,
//     which is what AdvancedFighter was already doing.
//
// Vanished is counted only when a hitmark had ever landed on that NPC (TargetState.HealthKnown
// — any hitmark, ours or another player's, which is the only reading this client has). This is
// deliberately STRICTER than combatrunner.WaitForKill, which counts a bare Vanished. A held
// target that nothing ever hit and that then left the list is the exact case combatplan's doc
// comment calls out as no evidence at all: it walked out of range, despawned, or was another
// player's. Counting it would put a kill on the panel for a monster this account never touched.
// The visible consequence is intended: AdvancedFighter's own b.kills can sit one or two above
// the panel on a run that loses an undamaged target, and the panel is the one that is right.
//
// A caller that needs a confirmed-only count must filter on the verdict itself; this is loot
// panel attribution, not a harness verdict, and nothing here is presented as proof of a death.
func (w *killWatch) killed(v combatplan.Verdict) (npcType, x, z, level int, ok bool) {
	switch v {
	case combatplan.Killed, combatplan.LikelyKilled:
	case combatplan.Vanished:
		if !w.last.HealthKnown() {
			return -1, 0, 0, 0, false
		}
	default:
		return -1, 0, 0, 0, false
	}
	if w.reported || w.npcType < 0 {
		return -1, 0, 0, 0, false
	}
	w.reported = true
	return w.npcType, w.x, w.z, w.level, true
}
