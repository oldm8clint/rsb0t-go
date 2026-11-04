package bot

// primarySkills maps a registered script name to the one skill (display name, matching
// gamestate.SkillNames) it is built to train, so a dashboard can make that skill's level/xp
// prominent instead of burying it in an alphabetical dump of every skill the account has ever
// reported. Checked directly against each script's own file, not guessed from its name:
//   - "BarbarianAgility" (barbarian.go) runs the Barbarian Outpost agility course lap after
//     lap — its entire loop is built around barbarianplan's Agility-course stage machine.
//   - "ProgressiveFisher" (fisher.go) fishes fisherplan's rung ladder (small net, big net, ...)
//     and banks the catch — every rung trains Fishing.
//   - "MuleReceiver" (mule.go) only accepts trades and (per COLLAB.md's audit notes) will bank
//     or idle-alch what it receives; it has no skill it is "training" the way the other two
//     scripts do, so it deliberately has no entry here rather than a guessed one.
//
// A script with no entry is not an error: PrimarySkill's second return value is how callers
// tell "no single skill" apart from a typo, so add new scripts here as they're registered
// rather than assuming every script belongs.
var primarySkills = map[string]string{
	"BarbarianAgility":  "Agility",
	"ProgressiveFisher": "Fishing",
	// "GemCutter" (gemcutter.go) chisels uncut gems at a bank for Crafting XP and nothing
	// else — every stage of its loop exists to feed that one skill, checked against the
	// script's own file rather than inferred from its name.
	"GemCutter": "Crafting",
	// "HerbCleaner" (herbcleaner.go) identifies unidentified herbs at a bank. Identifying is
	// the only thing it ever does, and content's ~identify_herb advances exactly one stat
	// (`stat_advance(herblore, $experience)`), checked in the content checkout rather than
	// inferred from the script's name.
	"HerbCleaner": "Herblore",
	// "SmelterBot" (smelterbot.go) withdraws ore and smelts bars at the Al Kharid furnace.
	// Smelting is the only thing it ever does, and content's [queue,smelting_ore] advances
	// exactly one stat (`stat_advance(smithing, $xp)`), checked in the content checkout rather
	// than inferred from the script's name.
	"SmelterBot": "Smithing",
	// "CombatTrainer" (combattrainer.go) deliberately trains no single melee stat: it rotates
	// whichever of Attack, Strength and Defence is furthest from its goal, so featuring any one
	// of the three would be wrong for most of a run. Hitpoints is the honest answer, and it is
	// checked in content rather than guessed: content/scripts/skill_combat/scripts/combat.rs2's
	// give_combat_experience awards 4 XP per damage to the chosen melee stat AND 1.33 to
	// Hitpoints on every hit whatever style is selected, so Hitpoints is the one stat every rung
	// of the ladder advances without exception.
	"CombatTrainer": "Hitpoints",
	// "ProgressiveWoodcutter" (woodcutter.go) chops the Seers tree ladder. Checked in content
	// rather than inferred: content/scripts/skill_woodcutting/scripts/woodcut.rs2 advances
	// exactly one stat per log, `stat_advance(woodcutting, ...productexp...)`. The Draynor
	// start camp also fletches its logs into arrow shafts, which pays Fletching — but that is
	// one stage below the oak gate, and woodcutting is the only skill EVERY stage advances.
	//
	// Reported missing by the fleet owner (2026-09-21): twelve live woodcutters showed no XP on
	// the accounts panel at all, because a script with no entry here has no skill for the panel
	// to feature. The API carried the figures the whole time; nothing was asking for them.
	"ProgressiveWoodcutter": "Woodcutting",
	// "ProgressiveWCFletch" (wcfletch.go) is the same ladder with a knife, and FLETCHING is the
	// honest answer where its sibling's is Woodcutting. Not a preference — the script's tree
	// choice is gated on the Fletching level (wcfletchplan.TierIndex), so Fletching is the stat
	// that decides what it does next, and it is the only one every stage advances: below the
	// oak gate it cuts arrow shafts, and from oak up it cuts bows, both
	// `stat_advance(fletching, ...)` in content/scripts/skill_fletching/scripts/cut_logs.rs2.
	// Woodcutting advances too, but an account whose fletching stalls stops climbing whatever
	// its woodcutting does, which is exactly what the panel should be showing.
	"ProgressiveWCFletch": "Fletching",
	// "EssenceMiner" (essence.go) mines rune essence. Checked:
	// content/scripts/skill_mining/scripts/mining.rs2 advances `stat_advance(mining, ...)`.
	"EssenceMiner": "Mining",
	// "ProgressiveCrafter" (crafter.go) spins flax into bow strings at the Seers wheel, which
	// is Crafting and nothing else -- the same single-skill shape as GemCutter above.
	"ProgressiveCrafter": "Crafting",
	// "ProgressiveThiever" (thiever.go) pickpockets. Thieving is the only stat it advances.
	"ProgressiveThiever": "Thieving",
	// "BankFletcher" (bankfletcher.go) fletches logs at a bank.
	"BankFletcher": "Fletching",
	// "BoneBurier" (boneburier.go) buries bones, which pays Prayer and nothing else.
	"BoneBurier": "Prayer",
	// "Alcher" (alcher.go) casts High Alchemy, which pays Magic.
	"Alcher": "Magic",
	// "LootChestFisher" (lootchestfisher.go) opens the crystal chest, which pays NO experience
	// at all -- the reward is items. Fishing is the honest answer because the script's own
	// fallback, and what it does whenever a trip is not worth making, is fish.
	"LootChestFisher": "Fishing",
	// "BassOysterCaskets" (bassoyster.go) is ProgressiveFisher on the Big net only: Fishing.
	"BassOysterCaskets": "Fishing",
	// "MuleReceiver" (mule.go) exists to receive trades and earns no experience doing it. Its
	// idle-time cooking of banked raw fish is the only skill it ever advances, and on the live
	// fleet that is a real number (the mule, +194k Cooking in one run).
	"MuleReceiver": "Cooking",
	// "MasterCrafter" (mastercrafter.go) runs ScarabSwarm and, when the mule holds a pile of uncut
	// gems, cuts them: Crafting is the skill its own rounds exist to raise (the bows it strings pay
	// Fletching, the other round), and the one the panel should show beside the swarm's stages.
	"MasterCrafter": "Crafting",
}

// PrimarySkill returns the skill name script is built to train and whether one is defined at
// all for it. ok is false both for an unregistered script and for one (like MuleReceiver) that
// genuinely has no single trained skill — callers should treat both the same way: nothing to
// feature.
func PrimarySkill(scriptName string) (skill string, ok bool) {
	skill, ok = primarySkills[scriptName]
	return
}
