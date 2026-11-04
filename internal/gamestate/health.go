// Health/death tracking, added for the combat-state primitive.
//
// This client never decodes a dedicated "hitmark"/health-update opcode family, and
// there isn't one to decode: this engine's full opcode table (internal/protocol/
// serverprot, generated from the engine itself, "verified to match ... exactly, zero
// drift") has no HP/HIT/HEALTH/DAMAGE-shaped entry at all. Classic RS folds live HP
// into the same UPDATE_STAT family every other skill uses — Hitpoints is stat index 3
// (see HitpointsStat below), and its "current level" byte IS current HP: like Prayer's
// current-points-vs-max split, a monster hit or a fall lowers the reported level without
// touching XP, and healing/food raises it back, capped at the real max HP. So HP()/MaxHP()
// below are thin readers over the Skill() decode skills.go already has — no new packet
// decode needed.
//
// MaxHP is NOT simply "the level implied by accumulated HP XP" despite that being the
// textbook classic-RS relationship (and this task's own initial framing) — live-tested
// against the local engine (cmd/combatstatetest, account loctest1) and found broken on
// this content build: a fresh login reported current level 10 (i.e. 10 HP) in the very
// same UPDATE_STAT packet whose XP field decoded (via the shared skills.go tenths-of-XP
// convention, and internal/scripts/levels' standard level curve) to level 2. XP-derived
// "max HP" cannot be authoritative when it comes out lower than the current value it is
// supposed to bound. This matches a sibling TypeScript bot project's own already-fixed
// gap for the same server family ("Fix HP threshold — baseLevel is bugged on 2004.plus,
// track real max HP from observed values"): the fix here is the same one, applied at the
// decode layer instead of a script layer — skills.go's applyUpdateStat tracks
// State.hpMaxSeen, a monotonic high-water mark of the Hitpoints stat's observed current
// level, and MaxHP() below just reads that back. It starts accurate as soon as the
// player has been seen at full health even once (guaranteed at least at login/spawn in
// practice) and only grows from there, exactly tracking real Hitpoints level-ups —
// unlike the XP curve, it is never wrong in the direction that matters (never reports a
// max lower than a current value already observed).
//
// Per-hit combat feedback (an actual hitmark/health bar rendered over an entity's head)
// is a real, separate gap: it lives in PLAYER_INFO/NPC_INFO's per-entity extended info
// block, which position.go/npc.go each deliberately abandon after reading position (see
// position.go's own "abandoning the bit cursor here mid-packet is safe" comment). Decoding
// that block is out of scope here — this file covers the local player's own live HP value
// and death detection only, per this task's scope-down.
package gamestate

import (
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/levels"
)

// HitpointsStat is UPDATE_STAT's index for Hitpoints, cross-checked against two
// independent sources in this repo: cmd/barbtest's own AgilityStat citation
// (engine/src/engine/entity/PlayerStat.ts and the client's Skill.ts, which agree:
// attack,defence,strength,hitpoints,ranged,prayer,magic,cooking,woodcutting,
// fletching,fishing,firemaking,crafting,smithing,mining,herblore,agility,...) and
// internal/scripts/fighterfood's own doc comment describing this same stat family.
const HitpointsStat = 3

// HP returns the local player's current hitpoints — UPDATE_STAT's live "level" byte
// for HitpointsStat, which drops on damage and rises on healing without any XP change
// (the same current/max split every other skill's boost/drain uses; HP just repurposes
// it to mean "how hurt am I" instead of "how buffed am I"). ok is false until at least
// one UPDATE_STAT for Hitpoints has actually been seen.
func (s *State) HP() (hp int, ok bool) {
	sk, _ := s.Skill(HitpointsStat)
	return sk.Level, sk.Generation > 0
}

// MaxHP returns the higher of two floors: the highest Hitpoints "current level" ever
// observed for this session (State.hpMaxSeen, updated by skills.go's applyUpdateStat —
// the real max HP, immune to any single current-damage dip) and the level the account's
// current Hitpoints XP implies (levels.LevelFromXP). See this file's package doc comment
// for why hpMaxSeen alone, not the XP-implied level, was the original fix on this
// content build — that direction (XP saying a level too low) is still handled correctly
// here since hpMaxSeen wins whenever it's the higher of the two. Taking the max also
// closes the opposite gap: a Hitpoints level-up that happens while the player is
// damaged (not at full health) raises the XP-implied level immediately, but hpMaxSeen
// only catches up once a packet is observed at that new, higher current value — which a
// player who then stays damaged for a while might not produce for some time. ok is
// false until at least one UPDATE_STAT for Hitpoints has been seen.
func (s *State) MaxHP() (max int, ok bool) {
	s.mu.RLock()
	seen := s.hpMaxSeen
	hp := s.skills[HitpointsStat]
	s.mu.RUnlock()
	if seen == 0 && hp.Generation == 0 {
		return 0, false
	}
	if xpLevel := levels.LevelFromXP(hp.XP); xpLevel > seen {
		return xpLevel, true
	}
	return seen, true
}

// checkDeathLine flags death when text (a decoded MESSAGE_GAME line — see state.go's
// Handle, which already decodes the jstring once for EventChatMessage and passes it
// here rather than having this file read the packet a second time) is the game's own
// death line.
func (s *State) checkDeathLine(text string) bool {
	if !fighterfood.IsDeathLine(text) {
		return false
	}
	s.Dead = true
	s.recordDeath()
	return true
}
