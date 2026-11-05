package loot

import "strings"

// NPCS THAT DROP NOTHING (a user request: "if khazard troopers dont drop anything but bones ensure the loot tracker only displays
// that and other loot goes into the correct sections").
//
// Content 289 gives the Khazard trooper (khazard_troop 475 and khazard_troop_spearmen 476, scripts/quests/quest_tree/configs/
// gnome_battlefield.npc) no `death_drop` param and no [ai_queue3,...] script of its own, so it dies through npc_default_death
// (scripts/skill_combat/scripts/npc/npc_combat.rs2), which adds only an npc's `death_drop` and nothing else: it drops NOTHING, not even
// bones. The tracker nevertheless showed small rune counts on it. Those were never its drops: a pile taken at the pen was credited to
// the account's newest kill when no kill within KillNearTiles could claim it (KillLog.SourceFor's fall-backs, PickupSource), and the
// newest kill at that pen is a trooper. They are other accounts' casts' piles (a rune pen's chaos and nature, a dead player's pack),
// bones another npc dropped, and so on.
//
// DropsNothing is the one place that says which kills cannot own a pile. The kill log skips them when it attributes a pile, and the panel
// moves what an older build filed on one back off it (controlpanel.unfileNoDropKills), so a card shows only what its npc can drop.

// noDropNPCNames is the lowercased names of the npcs whose content drops nothing.
var noDropNPCNames = map[string]bool{
	"khazard trooper": true,
}

// DropsNothing reports whether s is a kill of an npc whose content table drops nothing, so nothing taken off the ground can be its loot.
func DropsNothing(s Source) bool {
	return s.Kind == KindNPC && s.Action == ActionKill && noDropNPCNames[strings.ToLower(strings.TrimSpace(s.Name))]
}
