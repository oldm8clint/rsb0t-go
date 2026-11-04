package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward, gate and spawn Tribal Totem and the Fishing Contest rest on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run TotemFishContent ./internal/scripts/questplan/
//
// It uses the cook/sheep content test's readers (cooksheep_content_test.go).

// The ids the plans carry are the pack's, the interface's components included.
func TestTotemFishContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "tribal_totem", ObjTribalTotem}, {"obj", obj, "tribal_totem_label", ObjTotemLabel},
		{"obj", obj, "red_vine_worm", ObjRedVineWorm}, {"obj", obj, "hemenster_fishing_trophy", ObjFishingTrophy},
		{"obj", obj, "fishing_competition_pass", ObjFishingPass}, {"obj", obj, "fishing_rod", ObjFishingRod},
		{"obj", obj, "fishing_bait", ObjFishingBait}, {"obj", obj, "raw_sardine", ObjRawSardine}, {"obj", obj, "raw_giant_carp", ObjRawGiantCarp},
		{"obj", obj, "garlic", ObjGarlic}, {"obj", obj, "spade", ObjSpade},
		{"npc", npc, "kangai_mau", NpcKangaiMau}, {"npc", npc, "rpdt_employee", NpcRPDTEmployee}, {"npc", npc, "ardounge_wizard", NpcWizardCromperty},
		{"npc", npc, "tunnel_dwarf", NpcMountainDwarf}, {"npc", npc, "bonzo", NpcBonzo}, {"npc", npc, "morris", NpcMorris},
		{"npc", npc, "sinister_stranger", NpcSinisterStranger}, {"npc", npc, "0_41_53_compofishspot", NpcWillowSpot},
		{"npc", npc, "0_41_53_sinisterfishspot", NpcPipesSpot}, {"npc", npc, "harry", NpcHarry}, {"npc", npc, "guarddog", NpcGuardDog},
		{"loc", loc, "horncrate", LocHornCrate}, {"loc", loc, "teleportcrate", LocTeleportCrate}, {"loc", loc, "combodoor", LocComboDoor},
		{"loc", loc, "tribaltotemdoor", LocMansionDoor}, {"loc", loc, "totemtrapstairs", LocTrapStairs}, {"loc", loc, "totemshutchest", LocTotemChestShut},
		{"loc", loc, "totemopenchest", LocTotemChestOpen}, {"loc", loc, "garlicpipe", LocWallPipe}, {"loc", loc, "fishinggateclosedl", LocHemensterGateLeft},
		{"loc", loc, "fishinggateclosedr", LocHemensterGateRight}, {"loc", loc, "mcgruborlooserailing", LocMcGruborRailing},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	// The seven diggable vines, by the pack's own names.
	for i, name := range []string{"red_worm_vine", "red_worm_end", "red_worm_junction", "red_worm_diag1", "red_worm_diag3", "red_worm_diagfiller", "red_worm_end_diag"} {
		if loc[name] != FcRedVineLocs[i] {
			t.Errorf("vine %s is %d in the pack, the plan says %d", name, loc[name], FcRedVineLocs[i])
		}
	}
	// The lock's interface: tribal_door2 and the components the plan clicks (content/pack/interface.pack, `id=name`).
	ifs := csPackIDs(t, "interface")
	if ifs["tribal_door2"] != TotemLockInterface {
		t.Errorf("tribal_door2 is %d, the plan says %d", ifs["tribal_door2"], TotemLockInterface)
	}
	for i, n := range []int{43, 44, 45, 46} {
		if got := ifs[fmt.Sprintf("tribal_door2:com_%d", n)]; got != TotemDialText(i) {
			t.Errorf("dial %d text is com_%d = %d, the plan says %d", i+1, n, got, TotemDialText(i))
		}
	}
	for i, n := range []int{47, 49, 51, 53} {
		if got := ifs[fmt.Sprintf("tribal_door2:com_%d", n)]; got != TotemDialDown(i) {
			t.Errorf("dial %d down arrow is com_%d = %d, the plan says %d", i+1, n, got, TotemDialDown(i))
		}
	}
	for i, n := range []int{48, 50, 52, 54} {
		if got := ifs[fmt.Sprintf("tribal_door2:com_%d", n)]; got != TotemDialUp(i) {
			t.Errorf("dial %d up arrow is com_%d = %d, the plan says %d", i+1, n, got, TotemDialUp(i))
		}
	}
	if ifs["tribal_door2:com_57"] != TotemLockConfirm {
		t.Errorf("the confirm button is com_57 = %d, the plan says %d", ifs["tribal_door2:com_57"], TotemLockConfirm)
	}
}

// The quest's script: the stages, the label, the crates, the lock's combination and its wheel, the trap and the reward.
func TestTotemFishContentTotemScript(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^totem_complete = 5", "^totem_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_totem/configs/quest_totem.constant")
	for _, want := range []string{"^totem_not_started = 0", "^totem_started = 1", "^totem_crate_marked = 2", "^totem_crate_delivered = 3", "^totem_teleported = 4",
		"^totem_stairs_fail_coord = 0_41_151_16_55"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_totem.constant lacks %q", want)
		}
	}
	if TotemNotStarted != 0 || TotemStarted != 1 || TotemCrateMarked != 2 || TotemCrateDelivered != 3 || TotemTeleported != 4 || TotemComplete != 5 {
		t.Error("the plan's stages are not the content's")
	}
	src := csContent(t, "scripts/quests/quest_totem/scripts/quest_totem.rs2")
	for _, want := range []string{
		// The label: op 2 on the mansion's crate, handed over after a message box, again only while the pack holds none.
		"[oploc2,horncrate]", "if(inv_total(inv, tribal_totem_label) = 0) {", "inv_add(inv, tribal_totem_label, 1);",
		// The label used on the other crate: after its message box, consumed, the stage moved, the player's line.
		"[oplocu,teleportcrate]", "case tribal_totem_label :", "inv_del(inv, tribal_totem_label, 1);", "%totemquest = ^totem_crate_marked;",
		// The lock: four values, the word it asks for, the message each answer gives.
		"~get_tribal_door_lock(1) = 10 & ~get_tribal_door_lock(2) = 20 & ~get_tribal_door_lock(3) = 17 & ~get_tribal_door_lock(4) = 19",
		`mes("The combination seems correct!");`, `mes("This combination is incorrect.");`, "[if_button,tribal_door2:com_57]\nif_close;",
		"[if_button,tribal_door2:com_47] @tribal_door_combo(1, -1);", "[if_button,tribal_door2:com_48] @tribal_door_combo(1, 1);",
		"[if_button,tribal_door2:com_53] @tribal_door_combo(4, -1);", "[if_button,tribal_door2:com_54] @tribal_door_combo(4, 1);",
		"$stage > 25", "$stage = 0;", "$stage < 0", "$stage = 25;", "~clear_tribaldoor_inter;", "if_openmain(tribal_door2);",
		// The stairs: op 2 notes the trap at Thieving 21, op 1 climbs a noted trap and drops an un-noted one, both after a walk to 3 north.
		"[oploc1,totemtrapstairs]", "[oploc2,totemtrapstairs]", "def_coord $start = movecoord(loc_coord, 0, 0, 3);", "if(stat(thieving) < 21) {",
		`mes("You don't find anything interesting");`, "%handelmort_traps_disabled = setbit(%handelmort_traps_disabled, 21);",
		"trap in these stairs", "p_telejump(1_41_51_7_57);", `mes("As you climb the stairs you hear a click...");`, "p_telejump(^totem_stairs_fail_coord);",
		"~damage_self(calc(((stat(hitpoints) * 20) / 100) + 1));",
		// The chest: shut to open, search for the totem, none to a player who holds one in the pack or the bank.
		"[oploc1,totemshutchest]", "[oploc1,totemopenchest]", "if(inv_total(inv, tribal_totem) = 0 & inv_total(bank, tribal_totem) = 0) {",
		`mes("The chest is empty.");`,
		// The front door opens from the inside only.
		"[oploc1,tribaltotemdoor]", "if(coordz(coord) > coordz(loc_coord)) {", `mes("This door is securely locked.");`,
		// The reward.
		"stat_advance(thieving, 17750);", "inv_add(inv, swordfish, 5);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_totem.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(src, "mesbox(\"There is a label on this crate.") || !strings.Contains(src, "mesbox(\"Your trained senses as a thief enable you|to see that there is a trap in these stairs.") {
		t.Error("the label's and the trap's message boxes are not as the plan reads them")
	}
	if TotemThieving != 21 || LadderFactsXP("totem", "thieving") != 1775 {
		t.Errorf("Thieving gate %d, reward %d: stat_advance 17750 at 10x is 1,775", TotemThieving, LadderFactsXP("totem", "thieving"))
	}
	// The wheel: A to Z in order, so a letter's value is its place, and the combination is the letters at 10, 20, 17 and 19.
	alphabet := csContent(t, "scripts/quests/quest_totem/configs/alphabet.enum")
	for i := 0; i < 26; i++ {
		if want := fmt.Sprintf("val=%d,%c", i, 'A'+i); !strings.Contains(alphabet, want) {
			t.Errorf("alphabet.enum lacks %q", want)
		}
	}
	// The varps are perm and not transmitted: the journal is the only reader of the lock.
	for _, f := range []string{"scripts/quests/quest_totem/configs/quest_totem.varp"} {
		if v := csContent(t, f); strings.Contains(v, "transmit") || !strings.Contains(v, "[totemquest]\nscope=perm") || !strings.Contains(v, "[handelmort_traps_disabled]\nscope=perm") {
			t.Errorf("%s: %q", f, v)
		}
	}
	// The lock's interface text starts at A: the components are text with no default letter the plan could mistake for a reading.
	// (tribal_door2.if com_43 to com_46 are the dial texts the server writes.)
	door := csContent(t, "scripts/quests/quest_totem/interfaces/tribal_door2.if")
	for _, com := range []string{"com_43", "com_44", "com_45", "com_46"} {
		if !strings.Contains(csConfigBlock(door, com), "type=text") {
			t.Errorf("%s of the lock is not a text component: %q", com, csConfigBlock(door, com))
		}
	}
}

// LadderFactsXP is a ladder row's reward in a skill.
func LadderFactsXP(id, skill string) int { return LadderFacts[id].XP[skill] }

// Kangai Mau's, the employee's and the Wizard's menus: the options the stops choose are in their scripts, in the order the stops list them.
func TestTotemFishContentTotemMenus(t *testing.T) {
	kangai := csContent(t, "scripts/areas/area_brimhaven/scripts/kangai_mau.rs2")
	last := -1
	for _, line := range KangaiStartStop.Prefer {
		i := strings.Index(kangai, `"`+line+`"`)
		if i < 0 {
			t.Errorf("kangai_mau.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	for _, want := range []string{"%totemquest = ^totem_started;", "if(inv_total(inv, tribal_totem) > 0 & %totemquest = 4) {", "inv_del(inv, tribal_totem, 1);",
		"queue(totem_quest_complete, 0, 0);", `"And what are you doing in Brimhaven?", 1, "I'm in search of adventure!", 2`} {
		if !strings.Contains(kangai, want) {
			t.Errorf("kangai_mau.rs2 lacks %q", want)
		}
	}
	rpdt := csContent(t, "scripts/areas/area_ardougne_east/scripts/rdpt_employee.rs2")
	for _, want := range []string{"if(%totemquest = ^totem_crate_marked) {", RPDTStop.Prefer[0], "%totemquest = ^totem_crate_delivered;"} {
		if !strings.Contains(rpdt, want) {
			t.Errorf("rdpt_employee.rs2 lacks %q", want)
		}
	}
	crom := csContent(t, "scripts/areas/area_ardougne_east/scripts/wizard_cromperty.rs2")
	last = -1
	for _, line := range CrompertyStop.Prefer {
		i := strings.Index(crom, `"`+line+`"`)
		if i < 0 {
			t.Errorf("wizard_cromperty.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	for _, want := range []string{"def_coord $coord = 0_41_51_25_7;", "$coord = 0_41_51_14_57;", "%totemquest = ^totem_teleported;", "p_delay(4);",
		"if(%totemquest < ^totem_complete) {"} {
		if !strings.Contains(crom, want) {
			t.Errorf("wizard_cromperty.rs2 lacks %q", want)
		}
	}
	// 0_41_51_14_57 is level 0, square 41_51, local 14,57: 2638,3321, the plan's arrival tile; 1_41_51_7_57 is 2631,3321 on level 1.
	if MansionArrival != (tile.Tile{X: 41*64 + 14, Z: 51*64 + 57}) || StairsLanding != (tile.Tile{X: 41*64 + 7, Z: 51*64 + 57, Level: 1}) {
		t.Errorf("arrival %v landing %v", MansionArrival, StairsLanding)
	}
	// The journal's own words, the ones the parser reads.
	journal := csContent(t, "scripts/quests/quest_totem/scripts/totem_journal.rs2")
	for _, want := range []string{"I can start this quest by speaking to @dre@Kangai Mau@dbl@", "I need to get into @dre@Lord Handelmorts Mansion@dbl@",
		"I should get the @dre@R.P.D.T. men@dbl@ to deliver the crate now.", "I should get @dre@Wizard Cromperty@dbl@ to teleport me inside now.",
		"Handelmort and teleported myself inside.", "I worked out the combination for the door.", "I need to find the combination for the @dre@security door@dbl@.",
		"@red@QUEST COMPLETE!", `~quest_journal("Tribal Totem", $text);`} {
		if !strings.Contains(journal, want) {
			t.Errorf("totem_journal.rs2 lacks %q", want)
		}
	}
}

// The spawns and locs of Tribal Totem are where the plan stands.
func TestTotemFishContentTotemMaps(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"Kangai Mau", "NPC", KangaiAnchor, NpcKangaiMau},
		{"an R.P.D.T. employee", "NPC", RPDTAnchor, NpcRPDTEmployee}, {"an R.P.D.T. employee", "NPC", tile.Tile{X: 2644, Z: 3274}, NpcRPDTEmployee},
		{"the Wizard Cromperty", "NPC", CrompertyAnchor, NpcWizardCromperty},
		{"the mansion's crate at the depot", "LOC", tile.Tile{X: 2650, Z: 3273}, LocHornCrate},
		{"the Wizards' Tower crate at the depot", "LOC", tile.Tile{X: 2650, Z: 3271}, LocTeleportCrate},
		{"the crate in the mansion", "LOC", tile.Tile{X: 2638, Z: 3320}, LocTeleportCrate},
		{"the security door", "LOC", tile.Tile{X: 2634, Z: 3323}, LocComboDoor},
		{"the front door", "LOC", tile.Tile{X: 2635, Z: 3321}, LocMansionDoor},
		{"the trapped stairs", "LOC", tile.Tile{X: 2631, Z: 3322}, LocTrapStairs},
		{"the chest", "LOC", TotemChest, LocTotemChestShut},
		{"the staircase down upstairs", "LOC", tile.Tile{X: 2631, Z: 3322, Level: 1}, LocMansionStairsDn},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	// The garden's dogs stand outside the mansion's boxes, and none within five tiles of the walk out along x 2635 from the front door
	// (a dog hunts within a tile of itself and wanders three from its spawn).
	for _, dog := range []tile.Tile{{X: 2641, Z: 3315}, {X: 2646, Z: 3325}, {X: 2624, Z: 3319}, {X: 2624, Z: 3327}} {
		if !csHasRow(csMapRows(t, "NPC", dog), NpcGuardDog) {
			t.Errorf("no guard dog at %v", dog)
		}
		if InTotemMansion(dog) {
			t.Errorf("the dog at %v is in the mansion's boxes", dog)
		}
		for z := 3321; z >= 3308; z-- {
			if d := tile.Chebyshev(dog, tile.Tile{X: 2635, Z: z}); d <= 4 {
				t.Errorf("the walk out at 2635,%d is %d from the dog at %v, inside its reach (wander 3, hunt 1)", z, d, dog)
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Fishing Contest
// ---------------------------------------------------------------------------------------------------------------------

func TestTotemFishContentFishingScripts(t *testing.T) {
	consts := csContent(t, "scripts/quests/quest_fishingcompo/configs/quest_fishingcompo.constant")
	for _, want := range []string{"^fishingcompo_not_started = 0", "^fishingcompo_started = 1", "^fishingcompo_in_comp = 2", "^fishingcompo_garlic_comp = 3",
		"^fishingcompo_won_comp = 4", "^hemenster_comp_not_entered = 0", "^hemenster_comp_paidfee = 1", "^hemenster_comp_all_fish_caught = 4"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_fishingcompo.constant lacks %q", want)
		}
	}
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^fishingcompo_complete = 5", "^fishingcompo_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	if FcNotStarted != 0 || FcStarted != 1 || FcInComp != 2 || FcGarlicComp != 3 || FcWonComp != 4 || FcComplete != 5 {
		t.Error("the plan's stages are not the content's")
	}
	main := csContent(t, "scripts/quests/quest_fishingcompo/scripts/quest_fishingcompo.rs2")
	for _, want := range []string{
		// The gate: a pass for Morris inbound, Bonzo's prompt outbound while a contest runs, the right half asks at stage 1 as well.
		"[oploc1,_hemenster_gate]", "if(coordx(coord) <= coordx(loc_coord)) {", "if(%fishingcompo >= ^fishingcompo_in_comp & %fishingcompo < ^fishingcompo_won_comp | loc_type = fishinggateclosedr & %fishingcompo < ^fishingcompo_won_comp) {",
		"if(npc_find(coord, bonzo, 12, 0) = true) {", "@bonzo_quits;", "if(inv_total(inv, fishing_competition_pass) > 0) {", "p_teleport(loc_coord);",
		"p_teleport(movecoord(loc_coord, 1, 0, 0));",
		// The garlic: used on the wall pipe, the stash is a varp, a paid player is seated at once.
		"[oplocu,garlicpipe]", "case garlic : @stash_garlic;", "%hemenster_pipe_stashed = true;", "if(%hemenster_comp_stage = ^hemenster_comp_paidfee) {",
		"%fishingcompo = ^fishingcompo_garlic_comp;", "if(npc_find(coord, sinister_stranger, 8, 0) = true) {", "npc_tele(0_41_53_7_43);",
		// The reward.
		"stat_advance(fishing, 24370);", "queue(fishingcompo_quest_complete, 0, 0)",
	} {
		if !strings.Contains(main, want) {
			if want == "queue(fishingcompo_quest_complete, 0, 0)" {
				continue // spelt in mountain_dwarf.rs2
			}
			t.Errorf("quest_fishingcompo.rs2 lacks %q", want)
		}
	}
	if LadderFactsXP("fishingcompo", "fishing") != 2437 {
		t.Errorf("reward %d: stat_advance 24370 at 10x is 2,437", LadderFactsXP("fishingcompo", "fishing"))
	}
	fish := csContent(t, "scripts/quests/quest_fishingcompo/scripts/hemenster_fishing.rs2")
	for _, want := range []string{
		// The spots: sardines at the willow tree whatever the bait, a giant carp for a worm at the pipes.
		"[opnpc1,0_41_53_compofishspot] @attempt_fish_hemenster;", "[opnpc1,0_41_53_sinisterfishspot] @attempt_fish_hemenster;",
		"if (npc_type = 0_41_53_sinisterfishspot) { // Using red vine worm + north spot", "inv_add(inv, raw_giant_carp, 1);", "inv_add(inv, raw_sardine, 1);",
		// The bait: a worm first, plain bait after; the level; the one cast a click (four ticks).
		"if(inv_total(inv, red_vine_worm) > 0) {", "return (red_vine_worm);", "return (fishing_bait);", "if (stat(fishing) < 10) {", "p_delay(4); ",
		// The judgement: the third catch, a carp among them wins, the prompt's fee unpaid.
		"%hemenster_comp_stage = calc(%hemenster_comp_stage + 1);", "if (%hemenster_comp_stage = ^hemenster_comp_all_fish_caught) { // 3 fish caught",
		"if (inv_freespace(inv) < 1 & inv_total(inv, $bait) > 1) {",
		// The vampire check, the other way round: the willow spot with the garlic in the pipe.
		"(npc_type = 0_41_53_sinisterfishspot & %fishingcompo = ^fishingcompo_in_comp) | (npc_type = 0_41_53_compofishspot & %fishingcompo = ^fishingcompo_garlic_comp)",
	} {
		if !strings.Contains(fish, want) {
			t.Errorf("hemenster_fishing.rs2 lacks %q", want)
		}
	}
	bonzo := csContent(t, "scripts/areas/area_seers/scripts/hemenster/bonzo.rs2")
	for _, want := range []string{FcBonzoStop.Prefer[0], "if(inv_total(inv, coins) < 5) {", "inv_del(inv, coins, 5);", "%hemenster_comp_stage = ^hemenster_comp_paidfee;",
		"if(%hemenster_pipe_stashed ! null & %hemenster_pipe_stashed = true) {", "~move_hemenster_pipe;", "%fishingcompo = ^fishingcompo_in_comp;",
		"if(inv_total(inv, raw_giant_carp) > 0) {", "inv_del(inv, raw_sardine, 3);", "inv_del(inv, raw_giant_carp, 3);", "%fishingcompo = ^fishingcompo_won_comp;",
		"inv_add(inv, hemenster_fishing_trophy, 1);", "%fishingcompo = ^fishingcompo_started;", "%hemenster_comp_stage = ^hemenster_comp_not_entered;",
		`"` + FcQuitContest + `"`, "if(%fishingcompo = ^fishingcompo_won_comp & inv_total(inv, hemenster_fishing_trophy) = 0) {", "Is that all chump? Don't worry, I have a spare!"} {
		if !strings.Contains(bonzo, want) {
			t.Errorf("bonzo.rs2 lacks %q", want)
		}
	}
	// "I'll enter the competition please." is a line, not a menu, once the fee is paid (the retry the plan relies on).
	if !strings.Contains(bonzo, "if(%hemenster_comp_stage = ^hemenster_comp_paidfee) {\n        ~chatplayer(\"<p,neutral>I'll enter the competition please.\");\n        ~bonzo_set_places;") {
		t.Error("Bonzo's paid-and-unseated branch is not the free retry the plan relies on")
	}
	dwarf := csContent(t, "scripts/areas/area_white_wolf_mountain/scripts/mountain_dwarf.rs2")
	last := -1
	for _, line := range FcDwarfStartStop.Prefer {
		i := strings.Index(dwarf, `"`+line+`"`)
		if i < 0 {
			t.Errorf("mountain_dwarf.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	for _, want := range []string{FcDwarfPassStop.Prefer[0], "inv_add(inv, fishing_competition_pass, 1);", "%fishingcompo = ^fishingcompo_started;",
		"if(inv_total(inv, fishing_competition_pass) = 0 & inv_total(bank, fishing_competition_pass) = 0) {", "inv_del(inv, hemenster_fishing_trophy, 1);",
		"queue(fishingcompo_quest_complete, 0, 0);", "if(inv_total(inv, hemenster_fishing_trophy) > 0) {"} {
		if !strings.Contains(dwarf, want) {
			t.Errorf("mountain_dwarf.rs2 lacks %q", want)
		}
	}
	// Morris's gate is a pass check and a teleport; the cupboard's garlic is unlimited.
	cup := csContent(t, "scripts/general_use/scripts/cupboards.rs2")
	if !strings.Contains(cup, "case 1_48_51_24_4 : mes(\"You search the cupboard.\"); @cupboard_take_garlic;") {
		t.Error("Morgan's cupboard is not the garlic source the plan relies on")
	}
	// The vines.
	vines := csContent(t, "scripts/areas/area_seers/scripts/mcgrubors_wood.rs2")
	for _, want := range []string{"[oploc1,_red_vine] @dig_red_vine;", "if(inv_total(inv, spade) = 0) {", "inv_add(inv, red_vine_worm, 1);", "[oploc1,mcgruborlooserailing]"} {
		if !strings.Contains(vines, want) {
			t.Errorf("mcgrubors_wood.rs2 lacks %q", want)
		}
	}
	locs := csContent(t, "scripts/areas/area_seers/configs/mcgrubors_wood.loc")
	for _, name := range []string{"red_worm_vine", "red_worm_end", "red_worm_junction", "red_worm_diag1", "red_worm_diag3", "red_worm_diagfiller", "red_worm_end_diag"} {
		if b := csConfigBlock(locs, name); !strings.Contains(b, "category=red_vine") || !strings.Contains(b, "op1=Check") {
			t.Errorf("%s: %q", name, b)
		}
	}
	if b := csConfigBlock(locs, "mcgruborlooserailing"); !strings.Contains(b, "op1=Squeeze-through") {
		t.Errorf("the loose railing: %q", b)
	}
	// The quest items are untradeable (so the mule can never hold a worm, a pass or a trophy), the worm stacks.
	objs := csContent(t, "scripts/quests/quest_fishingcompo/configs/quest_fishingcompo.obj")
	for _, name := range []string{"red_vine_worm", "hemenster_fishing_trophy", "fishing_competition_pass"} {
		if b := csConfigBlock(objs, name); !strings.Contains(b, "tradeable=no") {
			t.Errorf("%s is tradeable: %q", name, b)
		}
	}
	if b := csConfigBlock(objs, "red_vine_worm"); !strings.Contains(b, "stackable=yes") {
		t.Errorf("the worm does not stack: %q", b)
	}
	// The journal's own words.
	journal := csContent(t, "scripts/quests/quest_fishingcompo/scripts/fishingcompo_journal.rs2")
	for _, want := range []string{"I can start this quest by speaking to the @dre@Dwarves@dbl@", "I must have level 10 fishing.", "@dre@Fishing Contest Pass@dbl@",
		"@dre@next to the willow tree @dbl@", "@dre@next to the pipes @dbl@", "I easily won the contest by catching some Giant Carp.", "@red@QUEST COMPLETE!",
		`~quest_journal("Fishing Contest", $text);`} {
		if !strings.Contains(journal, want) {
			t.Errorf("fishingcompo_journal.rs2 lacks %q", want)
		}
	}
}

// The spawns and locs of the Fishing Contest are where the plan stands, and the stand in the wood is outside every dog's reach.
func TestTotemFishContentFishingMaps(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"the west dwarf", "NPC", FcDwarfAnchor, NpcMountainDwarf}, {"the west dwarf's twin", "NPC", tile.Tile{X: 2821, Z: 3487}, NpcMountainDwarf},
		{"the east dwarf", "NPC", tile.Tile{X: 2876, Z: 3483}, NpcMountainDwarf},
		{"Bonzo", "NPC", FcBonzoAnchor, NpcBonzo}, {"Morris", "NPC", tile.Tile{X: 2643, Z: 3440}, NpcMorris},
		{"the Stranger", "NPC", tile.Tile{X: 2637, Z: 3440}, NpcSinisterStranger},
		{"the willow spot", "NPC", tile.Tile{X: 2630, Z: 3435}, NpcWillowSpot}, {"the pipes' spot", "NPC", FcPipesSpot, NpcPipesSpot},
		{"Harry", "NPC", FcHarry.Anchor, NpcHarry},
		{"the gate's left half", "LOC", FcGateInside, LocHemensterGateLeft}, {"the gate's right half", "LOC", tile.Tile{X: 2642, Z: 3441}, LocHemensterGateRight},
		{"a wall pipe", "LOC", FcPipeStand, LocWallPipe}, {"a wall pipe", "LOC", tile.Tile{X: 2637, Z: 3446}, LocWallPipe}, {"a wall pipe", "LOC", tile.Tile{X: 2636, Z: 3446}, LocWallPipe},
		{"the loose railing", "LOC", tile.Tile{X: 2662, Z: 3500}, LocMcGruborRailing},
		{"a vine", "LOC", tile.Tile{X: 2631, Z: 3497}, 2990}, {"a vine", "LOC", tile.Tile{X: 2632, Z: 3496}, 2991},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	for _, spade := range FcSpadeSpawns {
		if !csHasRow(csMapRows(t, "OBJ", spade), ObjSpade) {
			t.Errorf("no spade at %v", spade)
		}
	}
	// Every guard dog of the wood is where the plan has it, with the hunt config the plan prices (huntmode cowardly, hunt range 1,
	// wander range 3).
	for _, dog := range FcWoodDogs {
		if !csHasRow(csMapRows(t, "NPC", dog), NpcGuardDog) {
			t.Errorf("no guard dog at %v", dog)
		}
	}
	block := csConfigBlock(csContent(t, "scripts/_unpack/225/all.npc"), "guarddog")
	for _, want := range []string{"vislevel=44", "huntrange=1", "wanderrange=3", "huntmode=cowardly", "attack=35", "strength=36", "defence=37", "hitpoints=49"} {
		if !strings.Contains(block, want) {
			t.Errorf("[guarddog] lacks %q: %q", want, block)
		}
	}
	hunt := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "cowardly")
	if !strings.Contains(hunt, "check_nottoostrong=outside_wilderness") {
		t.Errorf("[cowardly]: %q", hunt)
	}
	// The vine patch: the stand is three tiles or more from every tile a dog can stand on (spawn +- 3), so a dog is never within the
	// hunt range of an account digging there.
	for _, dog := range FcWoodDogs {
		reach := tile.Tile{X: min(max(FcVineStand.X, dog.X-3), dog.X+3), Z: min(max(FcVineStand.Z, dog.Z-3), dog.Z+3)}
		if d := tile.Chebyshev(FcVineStand, reach); d < 3 {
			t.Errorf("the vine stand %v is %d from the nearest tile of the dog at %v's wander box", FcVineStand, d, dog)
		}
	}
	// The wood's outside tile is the railing's west side, the only door.
	if FcWoodOutside != (tile.Tile{X: 2661, Z: 3500}) {
		t.Errorf("the tile outside the railing is %v", FcWoodOutside)
	}
}
