package loot

import (
	"strings"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/cachecfg"
)

// Prayer: every bone the fleet buries, on one card (owner, 2026-09-23: "not sure if giants and the
// prayer section is hooked either"). It was not: four scripts bury bones (ScarabSwarm after every
// training kill, CombatTrainer on its giant rungs, AdvancedFighter with buryBones on, and the
// BoneBurier script itself) and none of them reported a single one.
//
// WHY PRODUCTION, NOT A KIND OF ITS OWN
//
// Burying has exactly the shape production.go describes for Fletching: an item in the account's
// own pack is CONSUMED, and the source is keyed on that input. The only difference is that the
// output is experience rather than an item, so a bury Drop has no Items and carries the Prayer XP
// it earned instead (Drop.XP). Being KindProduction buys two things with no new code:
//
//   - GroupKey is "production|prayer", one card for every bone kind, separate from Fletching's
//     "production|fletching" because the skill is part of the key.
//   - Source.Consumes prices one bone per event as the card's cost (cost.go). That is the same
//     rule that nets a log off the Fletching card, and it is the right accounting here for the same
//     reason: the bones a kill dropped are counted as that kill's loot on its own card, and once
//     they are buried the fleet no longer holds them. Without the netting, the fleet's net loot
//     value would keep counting bones that are in the ground.
//
// WHAT RECORDS IT
//
// Not the four scripts. internal/bot watches every running account for the one thing burying does
// that nothing else in content 289 does: a bone leaves the pack in the same server tick as the
// Prayer XP rises (content/scripts/skill_prayer/scripts/bury_bone.rs2 is inv_delslot then
// stat_advance(prayer, ...) in one script step). See internal/bot/lootprayer.go.

// SkillPrayer is spelled as gamestate.SkillNames spells it.
const SkillPrayer = "Prayer"

// ActionBury is what one event of burying is, for the panel's verb: one bone buried.
const ActionBury = "bury"

// PrayerStat is Prayer's stat index (gamestate.SkillNames).
const PrayerStat = 5

// buryableBones is every obj content 289 lets an account bury, by id, with its name and the
// experience one burial is worth in TENTHS of a point — derived from internal/bankxp's
// content-generated route table rather than typed out, exactly as production.go derives the
// fletching products, so regenerating that table from a content checkout carries this with it.
var buryableBones = func() map[int]bankxp.Route {
	m := map[int]bankxp.Route{}
	for _, r := range bankxp.Routes {
		if r.Skill != SkillPrayer || !strings.EqualFold(r.Via, "Bury") || len(r.Inputs) == 0 {
			continue
		}
		m[r.Inputs[0].ID] = r
	}
	return m
}()

// IsBuryable reports whether obj id is a bone content lets an account bury.
func IsBuryable(id int) bool {
	_, ok := buryableBones[id]
	return ok
}

// Bury is a bury source for one bone kind, named through cfg when it can be so the card shows the
// cache's own wording, then through the route table, then fallback.
func Bury(cfg *cachecfg.Config, objID int, fallback string) Source {
	name := fallback
	if r, ok := buryableBones[objID]; ok && r.Inputs[0].Name != "" {
		name = r.Inputs[0].Name
	}
	return Production(SkillPrayer, ActionBury, objID, objName(cfg, objID, name))
}
