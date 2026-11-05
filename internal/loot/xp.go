package loot

import (
	"strings"

	"rsb0t-go/internal/gamestate"
)

// EXPERIENCE ON THE LOOT TAB (a user request: "XP per hour on Woodcutting, Fishing, Fletching,
// Thieving, Crafting, Magic and Prayer panels (Prayer from buried bones); NPC panels show XP/h per
// NPC's kills (Att/Str/Def/Ranged/Magic/HP attributed per kill)").
//
// Experience is measured, never looked up: it is the skill's own reading rising (gamestate's
// EventSkillXP), collected by internal/bot's lootxp.go and handed to the source that earned it.
//
//   - A SKILL CARD (Woodcutting, Fishing, Fletching, Thieving, Crafting, Smithing, Cooking, Herblore,
//     Magic) takes the experience of its own skill that arrived since its last report, as Drop.XP.
//     The Prayer card takes it from the burial that paid it (lootprayer.go), as it always has.
//   - An NPC's KILL takes the experience of the combat skills that arrived since the last kill, split
//     by skill, as Drop.Skills.
//
// Both land on the same fields the Prayer card already used, so the control panel's per-hour figures
// are the ones it already computes for it.

// CombatStats are the skills a fight pays: Attack, Defence, Strength, Hitpoints, Ranged and Magic
// (gamestate.SkillNames indexes).
var CombatStats = []int{0, 1, 2, 3, 4, 6}

// StatOf is the stat index of a skill named as gamestate.SkillNames spells it, case-insensitively.
// False for a name that is no skill (the Teleports and Consumables cards' titles).
func StatOf(skill string) (int, bool) {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return 0, false
	}
	for i, n := range gamestate.SkillNames {
		if strings.EqualFold(n, skill) {
			return i, true
		}
	}
	return 0, false
}

// SkillXP is an experience amount as a Drop.Skills item.
func SkillXP(stat int, xp int64) Item {
	name := ""
	if stat >= 0 && stat < len(gamestate.SkillNames) {
		name = gamestate.SkillNames[stat]
	}
	return Item{ID: stat, Name: name, Count: xp}
}

// ClaimsExperience reports whether a drop's source is a skill card that takes the skill's
// experience as its XP: a gathering, production or thieving source that names a skill the game has,
// except the Prayer card (its burials carry theirs) and the Combat spells mirror row (the npc it
// repeats takes the experience). The Superheat Item mirror DOES claim: the cast's Magic experience is
// its own, and the bars' Smithing experience goes to the Smithing row.
func ClaimsExperience(s Source) (stat int, ok bool) {
	if s.Skill == "" || (s.MirrorsCost() && s.Action != ActionSuperheatCast) || strings.EqualFold(s.Skill, SkillPrayer) {
		return 0, false
	}
	switch s.Kind {
	case KindGathering, KindProduction, KindThieving:
		return StatOf(s.Skill)
	}
	return 0, false
}

// PaysCombatExperience reports whether a drop is a kill that takes the fight's experience.
func PaysCombatExperience(d Drop) bool {
	return d.Source.Kind == KindNPC && d.Source.Action == ActionKill && d.Events > 0
}
