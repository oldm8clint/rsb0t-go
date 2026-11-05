package questplan

import "strings"

// NeedGap is a skill stretch no trainer row covers yet: a NAMED, COUNTED gap, never a livelock (plan 3.1, rule 6). A
// need that runs into one is reported as "unreachable: no trainer for <skill> <level>" and is never planned for.
//
// The list is held to the catalogue from both sides. TestEveryRequirementHasAnAcquirerOrAReason fails when a quest's
// hard skill need runs past what the catalogue covers and no gap names it (a requirement was added with no trainer and no
// reason), and TestNoGapIsStale fails when a gap is listed for a level the catalogue now covers (a trainer lane landed
// and left its gap behind), so the list is exactly what is missing.
type NeedGap struct {
	Skill string
	// From is the first level no row trains: the gap runs from it upward.
	From   int
	Lane   string
	Reason string
}

// SkillGaps are the gaps, by the plan's lanes (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.5 and wave 9).
var SkillGaps = []NeedGap{
	{Skill: "mining", From: 1, Lane: "T1", Reason: "no Mining trainer: the ore ladder (copper and tin, iron, coal) is not built"},
	{Skill: "fishing", From: 1, Lane: "T3", Reason: "no Fishing trainer: net, bait and fly fishing are not built"},
	{Skill: "firemaking", From: 1, Lane: "T4", Reason: "no Firemaking trainer: the log-burning rung is not built"},
	{Skill: "herblore", From: 1, Lane: "T5", Reason: "no Herblore trainer: cleaning and potion making are not built"},
	{Skill: "ranged", From: 1, Lane: "T6", Reason: "no Ranged trainer: the CombatTrainer's Ranged discipline is pinned to 0 (Stage5TrainerSettingsFor)"},
	{Skill: "runecraft", From: 1, Lane: "-", Reason: "no quest asks for Runecrafting, so nothing trains it for one"},
	{Skill: "slayer", From: 1, Lane: "-", Reason: "disabled in content 289"},
	{Skill: "farming", From: 1, Lane: "-", Reason: "disabled in content 289"},
	{Skill: "agility", From: 35, Lane: "T8", Reason: "the Gnome course stops at 35: the Barbarian Outpost course rung is not built"},
	{Skill: "smithing", From: 34, Lane: "T2", Reason: "the dagger rung stops at 34: Crest's 40 and Legends' 50 are not planned"},
	{Skill: "crafting", From: 40, Lane: "T9", Reason: "the flax rung stops at 40: Legends' 50 is not planned"},
	{Skill: "woodcutting", From: 40, Lane: "T9", Reason: "the Seers camps rung stops at 40: Legends' 50 is not planned"},
	{Skill: "fletching", From: 25, Lane: "T9", Reason: "the fletching rung stops at 25: nothing above is planned"},
	{Skill: "prayer", From: 45, Lane: "T7", Reason: "the bones rung stops at 45: Prayer past it needs the bone pool (T7)"},
}

// GapFor is the gap that names skill's level `level` (the first level the catalogue cannot train through), if any.
func GapFor(skill string, level int) (NeedGap, bool) {
	skill = strings.ToLower(skill)
	for _, g := range SkillGaps {
		if g.Skill == skill && level >= g.From {
			return g, true
		}
	}
	return NeedGap{}, false
}
