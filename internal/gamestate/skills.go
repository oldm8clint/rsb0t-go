package gamestate

import "rsb0t-go/internal/packet"

// SkillCount is the number of skills in the protocol's stat table.
// UPDATE_STAT addresses skills by their zero-based stat index.
const SkillCount = 21

// Skill is the latest server-reported state for one skill.
type Skill struct {
	Level int
	XP    int
	// Generation increments whenever this skill receives an UPDATE_STAT packet.
	Generation int
}

// Skills returns a snapshot of all known skills. Unknown skills have Generation 0.
type Skills [SkillCount]Skill

// applyUpdateStat decodes UPDATE_STAT: stat:u8, xp:u32, level:u8.
// The wire XP value is already whole XP, not tenths: the engine stores XP internally
// as xp*10 (engine/src/engine/entity/Player.ts's levelExperience table is itself
// built as `Math.floor(acc / 4) * 10`), but UpdateStatEncoder.encode divides that
// internal value by 10 before writing it (`buf.p4((message.exp / 10) | 0)`) — so by
// the time it's on the wire, the /10 has already happened once. An earlier version of
// this function divided by 10 again on decode, silently under-reporting every skill's
// XP by 10x (e.g. a real 13,034,431 XP shown as 1,303,443) — confirmed live against
// this account's own real Cooking level (99, which requires ~13M XP) reading back a
// implausible ~1.3M here before the fix; cross-checked against the engine source above
// before changing this, not just against the symptom.
//
// Returns an EventSkillXP when the XP changed, then an EventLevelUp when this update is
// a real level-up — the new level is higher than the previously known one for this
// stat. The first UPDATE_STAT ever seen for a stat (Generation == 0 beforehand) reports
// neither: there is no real previous value yet, only the zero value, so treating that as
// "leveled up from 0" would fire a bogus event the moment a script connects and its
// skills first sync.
func (s *State) applyUpdateStat(r *packet.Packet) []Event {
	stat := int(r.G1())
	xp := int(r.G4())
	level := int(r.G1())
	if stat < 0 || stat >= SkillCount {
		return nil
	}
	previous := s.skills[stat]
	s.skills[stat] = Skill{Level: level, XP: xp, Generation: previous.Generation + 1}

	// Track the Hitpoints high-water mark here, colocated with the write it depends on.
	// See health.go's MaxHP doc comment: this engine's HP XP total does not reliably
	// imply the real max-HP level via the standard level-from-XP curve (live-proof found
	// XP=115 decoding to level 2 while the same packet's own current-level byte was 10),
	// so the observed current level is the only value proven trustworthy on this content
	// build — matching a sibling TS bot project's own prior "baseLevel is bugged on
	// 2004.plus, track real max HP from observed values" fix for the same server family.
	if stat == HitpointsStat && level > s.hpMaxSeen {
		s.hpMaxSeen = level
	}
	if stat == HitpointsStat {
		s.deaths.noteHP(s.scene.tick, level)
	}

	if previous.Generation == 0 {
		return nil
	}
	var events []Event
	if xp != previous.XP {
		events = append(events, Event{Kind: EventSkillXP, Skill: stat, Level: level, XP: xp, XPDelta: xp - previous.XP})
	}
	if level > previous.Level {
		events = append(events, Event{Kind: EventLevelUp, Skill: stat, Level: level, PreviousLevel: previous.Level, XP: xp})
	}
	return events
}

// Skills returns an immutable snapshot of the latest stat values received from the server.
func (s *State) Skills() Skills {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.skills
}

// Skill returns one skill snapshot and false when the index is outside the protocol table.
func (s *State) Skill(stat int) (Skill, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if stat < 0 || stat >= SkillCount {
		return Skill{}, false
	}
	return s.skills[stat], true
}

// SkillNames maps a stat index (see SkillCount) to its display name, in the exact order
// UPDATE_STAT addresses them — the engine's own PlayerStat enum
// (engine/src/engine/entity/PlayerStat.ts) and the client's Skill.ts, per
// barbarianrunner.AgilityStat's own citation of the same source. Cross-checked here
// against the two indices already independently proven live elsewhere in this repo:
// index 3 ("Hitpoints") matches HitpointsStat (health.go), and index 16 ("Agility")
// matches barbarianrunner.AgilityStat — both land on the expected name below, so this
// isn't just trusting the comment blind.
//
// Indices 18 and 19 are deliberately blank, not "Slayer"/"Farming": this engine's own
// PlayerStat enum (the same source cited above) literally names them STAT18/STAT19 and
// PlayerStatEnabled marks both false — this revision (the account owner dates it to late
// 2004) predates both skills' real release, and the server still reserves the wire slots
// without ever using them (confirmed live: both always report level 1, xp 0). An earlier
// version of this table filled them in with the modern skill names anyway, which is what
// made a genuinely-nonexistent "Farming" show up in the control panel's skills grid — a
// blank name here is what SkillName()'s callers already treat as "skip this slot" (see
// internal/fleet.Snapshot), so leaving these blank is enough to hide them without any
// caller-side change. Slayer *did* ship in this era on real RS but is stubbed out on this
// specific content build; re-add it here once Lost City's own target revision enables it
// (PlayerStatEnabled[17] flips true), not before.
var SkillNames = [SkillCount]string{
	"Attack", "Defence", "Strength", "Hitpoints", "Ranged", "Prayer", "Magic",
	"Cooking", "Woodcutting", "Fletching", "Fishing", "Firemaking", "Crafting",
	"Smithing", "Mining", "Herblore", "Agility", "Thieving", "", "",
	"Runecraft",
}

// SkillName returns the display name for stat, or "" when stat is outside the protocol
// table (mirrors Skill/applyUpdateStat's own bounds check).
func SkillName(stat int) string {
	if stat < 0 || stat >= SkillCount {
		return ""
	}
	return SkillNames[stat]
}
