package questplan

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, bit, gate, hunter and reward the Watch Tower rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped without
// one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run WatchtowerContent ./internal/scripts/questplan/

const wtDir = "scripts/quests/quest_itwatchtower/"

// The pack ids the plan carries are the pack's.
func TestWatchtowerContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	type row struct {
		kind string
		ids  map[string]int
		name string
		want int
	}
	var rows []row
	for name, want := range map[string]int{
		"coins": WtObjCoins, "rope": WtObjRope, "lit_candle": WtObjLitCandle, "lit_black_candle": WtObjLitBlackCandle, "torch_lit": WtObjLitTorch,
		"vial_water": WtObjVialWater, "vial_empty": WtObjVialEmpty, "jangerberries": WtObjJangerberries, "guam_leaf": WtObjGuamLeaf, "guamvial": WtObjGuamVial,
		"pestle_and_mortar": WtObjPestle, "bronze_pickaxe": WtObjPickaxe, "bat_bones": WtObjBatBones, "dragon_bones": WtObjDragonBones, "deathrune": WtObjDeathRune,
		"gold_bar": WtObjGoldBar, "shark": WtObjShark, "ogrerelic": WtObjRelic, "relicpart1": WtObjPart1, "relicpart2": WtObjPart2, "relicpart3": WtObjPart3,
		"skavidmap": WtObjMap, "ogretooth": WtObjTooth, "toban_key": WtObjTobanKey, "rockcake": WtObjRockCake, "powering_crystal1": WtObjCrystal1,
		"powering_crystal2": WtObjCrystal2, "powering_crystal3": WtObjCrystal3, "powering_crystal4": WtObjCrystal4, "fingernails": WtObjFingernails,
		"watchtowerrobe": WtObjDecoyRobe, "watchtowerarmour": WtObjDecoyArmour, "watchtowerdagger": WtObjDecoyDagger, "watch_eye_patch": WtObjDecoyEyePatch,
		"jangervial": WtObjJangerVial, "guamjangervial": WtObjGuamJangerVial, "ground_bat_bones": WtObjGroundBones, "stolen_gold": WtObjStolenGold,
		"ogre_potion": WtObjOgrePotion, "magic_ogre_potion": WtObjMagicPotion, "watchtowerspell": WtObjSpellScroll, "shaman_robe": WtObjShamanRobe,
		"nightshade": WtObjNightshade,
	} {
		rows = append(rows, row{"obj", obj, name, want})
	}
	for name, want := range map[string]int{
		"watchtower_wizard": WtNpcWizard, "tower_guard": WtNpcTowerGuard, "og": WtNpcOg, "grew": WtNpcGrew, "toban": WtNpcToban, "gorad": WtNpcGorad,
		"ogre_guard1": WtNpcGuardEast, "ogre_guard2": WtNpcGuardRelic, "ogre_guard3": WtNpcGuardWall, "ogre_guard4": WtNpcGuardChasm, "city_guard": WtNpcCityGuard,
		"scared_skavid": WtNpcScared, "mad_skavid": WtNpcMad, "skavidtalker1": WtNpcTalker1, "skavidtalker2": WtNpcTalker2, "skavidtalker3": WtNpcTalker3,
		"skavidtalker4": WtNpcTalker4, "enclave_guard": WtNpcEnclave, "ogre_shaman": WtNpcShaman, "ogre_trader2": WtNpcTrader, "ogre": WtNpcOgre,
		"ogre2": WtNpcOgre2, "ogre_chieftan": WtNpcChieftain,
	} {
		rows = append(rows, row{"npc", npc, name, want})
	}
	for name, want := range map[string]int{
		"loc_2299": LocWtClimbRocks, "towerladder": LocWtTowerLadder, "laddertop": LocWtLadderTop, "watchladderup": LocWtLadderUp, "watchladderdown": LocWtLadderDown,
		"watchleverup": LocWtLever, "watchtowerbushnail": LocWtNailBush, "tree_ropeswing4_norope": LocWtBranch, "tree_ropeswing3": LocWtSwingBack,
		"tobancave": LocWtTobanCave, "tobanladderdown": LocWtTobanLadder, "tobanchest": LocWtTobanChest, "ogreguardgate2": LocWtGateRelic, "ogreguardgate1": LocWtGateEast,
		"rockcounter_withcakes": LocWtCakeStall, "ganothbattlement": LocWtBattlement, "tanothjump1": LocWtJumpIn, "tanothjump2": LocWtJumpOut,
		"enclavecave": LocWtEnclaveExit, "darkcave_escape": LocWtDarkRope, "rock_of_dalgroth": LocWtRock,
	} {
		rows = append(rows, row{"loc", loc, name, want})
	}
	for i := 0; i < 6; i++ {
		rows = append(rows, row{"loc", loc, fmt.Sprintf("skavid_cave%d", i+1), LocWtCaveIn[i]}, row{"loc", loc, fmt.Sprintf("cave%dexit", i+1), LocWtCaveOut[i]})
	}
	for _, c := range rows {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the bits, the points and the reward.
func TestWatchtowerContentStagesAndReward(t *testing.T) {
	consts := csContent(t, wtDir+"configs/quest_itwatchtower.constant")
	for name, v := range map[string]int{"not_started": WtNotStarted, "started": WtStarted, "given_fingernails": WtGivenFingernails, "made_relic": WtMadeRelic,
		"given_relic": WtGivenRelic, "given_riddle": WtGivenRiddle, "solved_riddle": WtSolvedRiddle, "skavid_crystal": WtSkavidCrystal,
		"fed_nightshade": WtFedNightshade, "learned_potion": WtLearnedPotion, "made_potion": WtMadePotion, "found_all_crystals": WtFoundAll, "complete_read_scroll": WtReadScroll} {
		if want := fmt.Sprintf("^itwatchtower_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_itwatchtower.constant lacks %q", want)
		}
	}
	quest := csContent(t, "scripts/general/configs/quest.constant")
	if !strings.Contains(quest, fmt.Sprintf("^itwatchtower_complete = %d", WtComplete)) || !strings.Contains(quest, "^itwatchtower_questpoints = 4") {
		t.Errorf("quest.constant: complete %d and 4 points are expected", WtComplete)
	}
	// The bits the flags stand for, by position.
	for name, v := range map[string]int{"looking_relic": 0, "spoken_toban": 1, "helped_toban": 2, "spoken_grew": 3, "helped_grew": 4, "spoken_og": 5, "helped_og": 6,
		"relic1": 7, "relic2": 8, "relic3": 9, "market_lower": 10, "market_upper": 11, "learning_skavid": 12, "learned_ar": 13, "learned_ig": 14, "learned_cur": 15,
		"learned_nod": 16, "shaman_kills_lower": 17, "shaman_kills_upper": 19} {
		if want := fmt.Sprintf("^itwatchtower_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_itwatchtower.constant lacks %q", want)
		}
	}
	main := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	hasAll(t, "quest_itwatchtower.rs2", main, "%itwatchtower = ^itwatchtower_complete;", "stat_advance(magic, 153000);", "inv_add(inv, coins, 5000);",
		"inv_add(inv, watchtowerspell, 1);", "%qp = add(%qp, ^itwatchtower_questpoints);", "p_teleport(2_45_73_48_43);",
		"if(~watchtower_has_all_crystals = true & %itwatchtower = ^itwatchtower_found_all_crystals) {")
	if WtMirrorLanding != herdCoord(t, "2_45_73_48_43") {
		t.Errorf("the mirror landing %v", WtMirrorLanding)
	}
	// Not transmitted: the journal's prose is the only reader.
	varps := csContent(t, wtDir+"configs/quest_itwatchtower.varp")
	if strings.Contains(varps, "transmit") || !strings.Contains(varps, "[itwatchtower]") || !strings.Contains(varps, "[itwatchtower_bits]") || !strings.Contains(varps, "[gutanoth_gold]") {
		t.Errorf("the varps are %q", varps)
	}
}

// The levels each step is gated on are the scripts', each at its own step.
func TestWatchtowerContentGates(t *testing.T) {
	main := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	trader := csContent(t, "scripts/areas/area_yanille/scripts/ogre_trader.rs2")
	potion := csContent(t, wtDir+"scripts/ogre_potion.rs2")
	shaman := csContent(t, wtDir+"scripts/ogre_shaman.rs2")
	shortcuts := csContent(t, "scripts/skill_agility/scripts/shortcuts.rs2")
	journal := csContent(t, wtDir+"scripts/itwatchtower_journal.rs2")
	hasAll(t, "the wall", main, "[oploc1,loc_2299]\nif(stat(agility) < 18) {", "~agility_climb_up(310, 1_39_48_52_45);")
	hasAll(t, "the chasm", main, "[oploc1,tanothjump1]\nif(stat(agility) < 25) {", "inv_del(inv, coins, 20);", "p_teleport(0_39_47_34_21);", "p_teleport(0_39_47_35_18);")
	hasAll(t, "the rock", main, "if(stat(mining) < 40) {", "def_obj $pickaxe = ~pickaxe_checker;", "inv_add(inv, powering_crystal4, 1);",
		"getbit_range(%itwatchtower_bits, ^itwatchtower_shaman_kills_lower, ^itwatchtower_shaman_kills_upper) < 6")
	hasAll(t, "the stall", trader, "if (stat(thieving) < 15) {", "if(npc_find(coord, ogre_trader2, 3, 2) = true) {", "~npc_retaliate(0);", "loc_change(empty_rock_cake_counter, 12);")
	hasAll(t, "the potion", potion, "if(stat(herblore) < 14) {", "case guam_leaf : ~potion_explosion(guam_leaf, jangervial", "case guamvial : ~potion_explosion(ground_bat_bones, jangervial",
		"inv_del(inv, ground_bat_bones, 1);\ninv_del(inv, guamjangervial, 1);\ninv_add(inv, ogre_potion, 1);", "~damage_self(5);")
	hasAll(t, "the shamans", shaman, "if(stat(magic) < 14) {", "inv_del(inv, magic_ogre_potion, 1);\n            inv_add(inv, vial_empty, 1);\n            inv_add(inv, powering_crystal3, 1);",
		"[opnpc1,ogre_shaman]\nmes(\"A magic blast comes from the shaman.\");", "queue(damage_player, 0, 20);", "~damage_self(30);")
	// The potion is spent by the SIXTH dissolve only: the case 6 branch is the one that deletes it.
	if strings.Count(shaman, "inv_del(inv, magic_ogre_potion, 1);") != 1 {
		t.Error("the magic ogre potion is deleted somewhere other than the sixth shaman")
	}
	// The swing back off the island asks Agility 10 (the island's rope is tree_ropeswing3, not the one the check leaves out).
	hasAll(t, "the island swing", shortcuts, "loc_type ! tree_ropeswing2 & stat(agility) < 10", "case 0_39_48_15_19 : return (%ogre_ropeswing_used);")
	hasAll(t, "the journal's list", journal, "stat_base(magic) < 14", "stat_base(mining) < 40", "stat_base(herblore) < 14", "stat_base(thieving) < 15", "stat_base(agility) < 25")
	// The rope tied to the branch is used up and the rope tree changes for five ticks; the exit is the swing.
	hasAll(t, "the branch", main, "[oplocu,tree_ropeswing4_norope]\nif(last_useitem = rope) {", "inv_del(inv, rope, 1);", "~forcewalk2(0_39_48_5_15);",
		"movecoord(loc_coord, 6, 0, 0)")
	if got := herdCoord(t, "0_39_48_5_15"); got != WtBranchStand {
		t.Errorf("the forced walk ends at %v, the plan's branch stand is %v", got, WtBranchStand)
	}
	if got := (tile.Tile{X: WtBranch.X + 6, Z: WtBranch.Z}); got != WtIslandLanding {
		t.Errorf("the swing lands at %v, the plan says %v", got, WtIslandLanding)
	}
	if got := herdCoord(t, "0_39_48_15_19"); got != WtSwingStand {
		t.Errorf("the island's swing starts at %v, the plan's stand is %v", got, WtSwingStand)
	}
	if got := herdCoord(t, "0_39_48_15_24"); got != WtSwingLanding {
		t.Errorf("the island's swing ends at %v, the plan says %v", got, WtSwingLanding)
	}
}

// The fixtures of watchtower_test.go are the journal's, line for line, and every needle the parser reads is inside one of the journal's strings.
func TestWatchtowerContentJournal(t *testing.T) {
	src := csContent(t, wtDir+"scripts/itwatchtower_journal.rs2")
	for name, page := range map[string]string{"started": wtPages.started, "tribes": wtPages.tribes, "tribesAllGiven": wtPages.tribesAllGiven, "riddle": wtPages.riddle,
		"cave": wtPages.cave, "crystal": wtPages.crystal, "potionToMake": wtPages.potionToMake, "potionMade": wtPages.potionMade, "shamans": wtPages.shamans,
		"shamansDone": wtPages.shamansDone, "rock": wtPages.rock, "crystals": wtPages.crystals, "lever": wtPages.lever} {
		for _, line := range strings.Split(page, "|") {
			if line == "" {
				continue
			}
			// The kills line carries a computed count; the rest are literal.
			if strings.Contains(line, "kill 4 ogre") {
				line = strings.Split(line, "kill 4")[0]
			}
			if !strings.Contains(src, line) {
				t.Errorf("page %s: the journal script has no string %q", name, line)
			}
		}
	}
	hasAll(t, "the kills line", src, "kill <tostring(calc(6 - $current_kills))> ogre shaman(s).", "I killed all the ogre shamans.")
	// Every literal of the script, as the client renders it: a needle must sit inside one of them.
	var lits []string
	for _, m := range regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(src, -1) {
		lits = append(lits, NormaliseLines([]string{strings.ReplaceAll(m[1], "|", " ")}))
	}
	inScript := func(needle string) bool {
		for _, l := range lits {
			if strings.Contains(l, needle) {
				return true
			}
		}
		return false
	}
	for _, m := range wtStageMarkers {
		if m.text == "quest complete!" {
			continue
		}
		if !inScript(m.text) {
			t.Errorf("stage %d needle %q is in no journal string", m.stage, m.text)
		}
	}
	for _, m := range wtFlagMarkers {
		if !inScript(m.text) {
			t.Errorf("flag %#x needle %q is in no journal string", m.flag, m.text)
		}
	}
	if !strings.Contains(src, "QUEST COMPLETE!") {
		t.Error("the journal no longer says QUEST COMPLETE!")
	}
	// The "wants" lines are dropped once a tribe is helped, which is why helped implies spoken.
	hasAll(t, "the tribes' lines", src, "if (testbit(%itwatchtower_bits, ^itwatchtower_helped_og) = ^true) {", "} else if (testbit(%itwatchtower_bits, ^itwatchtower_spoken_og) = ^true) {")
}

// The menus the module prefers are in the scripts, and the lines that set the guards on the account are not preferred anywhere.
func TestWatchtowerContentTheTalks(t *testing.T) {
	scripts := map[string]string{}
	for _, f := range []string{"watchtower_wizard", "og", "grew", "toban", "city_guard", "skavid", "ogre_guard", "gorad", "enclave_guard"} {
		scripts[f] = csContent(t, wtDir+"scripts/"+f+".rs2")
	}
	main := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	has := func(file, line string) {
		t.Helper()
		src := scripts[file]
		if file == "main" {
			src = main
		}
		if !strings.Contains(src, "\""+line+"\"") {
			t.Errorf("%s offers no %q", file, line)
		}
	}
	for _, l := range WtStartPrefer {
		has("watchtower_wizard", l)
	}
	for _, l := range WtNailsPrefer {
		has("watchtower_wizard", l)
	}
	has("watchtower_wizard", "I have lost the relic you gave me.")
	for _, l := range WtOgStop.Prefer {
		has("og", l)
	}
	for _, l := range WtGrewStop.Prefer {
		has("grew", l)
	}
	for _, l := range WtGrewLostStop.Prefer {
		has("grew", l)
	}
	for _, l := range WtTobanStop.Prefer {
		has("toban", l)
	}
	for _, l := range WtCityGuardRiddleStop.Prefer {
		has("city_guard", l)
	}
	for _, l := range WtCityGuardMapStop.Prefer {
		has("city_guard", l)
	}
	has("ogre_guard", "But I am a friend to ogres...")
	has("main", "Okay, I'll pay it.")
	has("skavid", "Okay, okay, I'm not going to hurt you.")
	has("skavid", "But I've lost it!")
	for _, w := range []string{"Cur.", "Ar.", "Ig.", "Nod.", "Gor.", "Bidith.", "Tanath."} {
		has("skavid", w)
	}
	// What each prefer list never picks: the options that set the guards on the account or cost hitpoints.
	for _, bad := range []struct{ file, line string }{{"og", "I have come to kill you."}, {"toban", "Die, creature!"}, {"grew", "You will have to kill me first."},
		{"city_guard", "I am an ogre killer come to destroy you!"}} {
		has(bad.file, bad.line)
		for _, stop := range []NpcStop{WtOgStop, WtGrewStop, WtGrewLostStop, WtTobanStop, WtCityGuardRiddleStop, WtCityGuardMapStop} {
			for _, p := range stop.Prefer {
				if strings.Contains(strings.ToLower(bad.line), strings.ToLower(p)) || strings.Contains(strings.ToLower(p), strings.ToLower(bad.line)) {
					t.Errorf("%s prefers %q, which matches the dangerous option %q", stop.Npc, p, bad.line)
				}
			}
		}
	}
	// The wizard's menu at the start has the lines the three-menu preference covers, and the stage-8 talk has none.
	hasAll(t, "the wizard at stage 8", scripts["watchtower_wizard"], "case ^itwatchtower_fed_nightshade :\n        ~chatplayer(\"<p,sad>I have found the cave of ogre shaman, but I cannot touch them!\");",
		"%itwatchtower = ^itwatchtower_learned_potion;")
	// The words each talker understands, by cave; the mad skavid's four phrases and their answers.
	sk := scripts["skavid"]
	hasAll(t, "the talkers", sk, "[label,skavid_saycur]\n~chatplayer(\"<p,neutral>Cur.\");\nif(npc_type = skavidtalker3) {", "[label,skavid_sayar]\n~chatplayer(\"<p,neutral>Ar.\");\nif(npc_type = skavidtalker2) {",
		"[label,skavid_sayig]\n~chatplayer(\"<p,neutral>Ig.\");\nif(npc_type = skavidtalker1) {", "[label,skavid_saynod]\n~chatplayer(\"<p,neutral>Nod.\");\nif(npc_type = skavidtalker4) {")
	// caves 1 to 4 are talker 4, 1, 2 and 3 (the module's order), by where each stands against the landing.
	talker := [4]string{"skavidtalker4", "skavidtalker1", "skavidtalker2", "skavidtalker3"}
	npcIDs := csPackIDs(t, "npc")
	for i, name := range talker {
		rows := csMapRows(t, "NPC", WtCaveTalker[i])
		if !csHasRow(rows, npcIDs[name]) {
			t.Errorf("cave %d: %v holds %v, want %s", i+1, WtCaveTalker[i], rows, name)
		}
	}
	for i, want := range []uint32{WtLearnedNod, WtLearnedIg, WtLearnedAr, WtLearnedCur} {
		if WtCaveFlag[i] != want {
			t.Errorf("cave %d teaches flag %#x, want %#x", i+1, WtCaveFlag[i], want)
		}
	}
	hasAll(t, "the mad skavid", sk, "case 0 : ~chatnpc(\"<p,bored>Ar cur...\");", "case 1 : ~chatnpc(\"<p,bored>Bidith ig...\");", "case 2 : ~chatnpc(\"<p,bored>Cur tanath...\");",
		"case 3 : ~chatnpc(\"<p,bored>Gor nod...\");", "if($mes_type = 1) @mad_skavid_correct;", "if($mes_type = 2) @mad_skavid_correct;", "if($mes_type = 3) @mad_skavid_correct;",
		"if($mes_type = 0) @mad_skavid_correct;")
	for _, p := range wtMadAnswerRows {
		if !strings.Contains(sk, p) {
			t.Errorf("skavid.rs2 lacks %q", p)
		}
	}
	// Gorad's tooth: the fight is an attack, the tooth the death's (a free slot, no tooth held, before the relic is given).
	g := scripts["gorad"]
	hasAll(t, "gorad.rs2", g, "if(%itwatchtower < ^itwatchtower_given_relic) queue(defeat_gorad, 0, 0);", "if(inv_freespace(inv) = 0 | ~obj_gettotal(ogretooth) > 0) return;",
		"inv_add(inv, ogretooth, 1);", "~npc_retaliate(0);")
	// The enclave guard: the nightshade run past him, and both of his own options make him attack.
	eg := scripts["enclave_guard"]
	hasAll(t, "enclave_guard.rs2", eg, "[opnpcu,enclave_guard]\nif(last_useitem = nightshade) {", "if(%itwatchtower = ^itwatchtower_skavid_crystal) %itwatchtower = ^itwatchtower_fed_nightshade;",
		"p_teleport(0_40_147_28_2);")
	if got := herdCoord(t, "0_40_147_28_2"); got != WtEnclaveArrive {
		t.Errorf("the enclave arrival %v, the plan says %v", got, WtEnclaveArrive)
	}
	if strings.Count(eg, "~npc_retaliate(0);") != 2 {
		t.Error("the enclave guard's two talk options no longer both set him on the account")
	}
}

// wtMadAnswerRows are the mad skavid's answer-to-phrase rows in skavid.rs2, in the module's table order.
var wtMadAnswerRows = []string{
	"case 5 :\n            ~chatplayer(\"<p,neutral>Gor.\");\n            if($mes_type = 0) @mad_skavid_correct;",
	"case 1 :\n            ~chatplayer(\"<p,neutral>Cur.\");\n            if($mes_type = 1) @mad_skavid_correct;",
	"case 3 :\n            ~chatplayer(\"<p,neutral>Bidith.\");\n            if($mes_type = 2) @mad_skavid_correct;",
	"case 4 :\n            ~chatplayer(\"<p,neutral>Tanath.\");\n            if($mes_type = 3) @mad_skavid_correct;",
}

// The teleports the module's tiles are: the scripts' own, read as coordinates.
func TestWatchtowerContentTeleports(t *testing.T) {
	main := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	ladders := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	for _, c := range []struct {
		src, snippet string
		want         tile.Tile
	}{
		{main, "~agility_climb_up(310, 1_39_48_52_45);", WtWallLanding},
		{ladders, "[oploc1,watchladderup]\nif(%itwatchtower >= ^itwatchtower_complete) {\n    ~climb_ladder(2_45_73_53_40, true);\n    return;\n}\n~climb_ladder(2_39_48_53_40, true);", WtWizardAnchor},
		{main, "[oploc1,watchladderdown]\n~climb_ladder(1_39_48_53_40, true);", WtFirstFloorStand},
		{main, "[oploc1,tobancave]\nmes(\"You enter the cave.\");\np_teleport(0_40_47_16_21);", WtCampArrival},
		{main, "[oploc1,tobanladderdown]\nmes(\"You climb down the ladder.\");\np_teleport(0_39_46_4_44);", tile.Tile{X: 2500, Z: 2988}},
		{main, "[oploc1,enclavecave]\np_teleport(0_39_47_44_46);", tile.Tile{X: 2540, Z: 3054}},
		{main, "[oploc1,darkcave_escape]\np_teleport(0_39_47_44_46);", tile.Tile{X: 2540, Z: 3054}},
		{main, "[label,teleport_dark_skavid_cave]\np_teleport(0_39_147_9_52);", WtDarkLanding},
	} {
		if !strings.Contains(main, c.snippet) && !strings.Contains(c.src, c.snippet) {
			t.Errorf("quest_itwatchtower.rs2 lacks %q", c.snippet)
			continue
		}
		var coord string
		for _, f := range regexp.MustCompile(`\d_\d+_\d+_\d+_\d+`).FindAllString(c.snippet, -1) {
			coord = f
		}
		if c.snippet == ladders && strings.Contains(c.snippet, "2_39_48_53_40") {
			coord = "2_39_48_53_40"
		}
		if got := herdCoord(t, coord); got != c.want {
			t.Errorf("%s is %v, the plan has %v", coord, got, c.want)
		}
	}
	// The six caves: mouths (landing with the map and a lit light), exits.
	for i := 0; i < 6; i++ {
		in := fmt.Sprintf("[oploc1,skavid_cave%d]", i+1)
		blk := main[strings.Index(main, in):]
		blk = blk[:strings.Index(blk, "\n\n")]
		var coords []string
		for _, f := range regexp.MustCompile(`0_\d+_\d+_\d+_\d+`).FindAllString(blk, -1) {
			coords = append(coords, f)
		}
		found := false
		for _, c := range coords {
			if herdCoord(t, c) == WtCaveLandings[i] {
				found = true
			}
		}
		if !found {
			t.Errorf("cave %d: no case of %s lands at %v (coordinates %v)", i+1, in, WtCaveLandings[i], coords)
		}
		out := fmt.Sprintf("[oploc1,cave%dexit]\np_teleport(", i+1)
		k := strings.Index(main, out)
		if k < 0 {
			t.Errorf("no exit script for cave %d", i+1)
			continue
		}
		coord := regexp.MustCompile(`\d_\d+_\d+_\d+_\d+`).FindString(main[k:])
		if got := herdCoord(t, coord); got != WtCaveExits[i] {
			t.Errorf("cave %d's exit is %v, the plan says %v", i+1, got, WtCaveExits[i])
		}
	}
	// The relic gate, the east gate, the hill they throw the account to, the lower hill.
	guards := csContent(t, wtDir+"scripts/ogre_guard.rs2")
	hasAll(t, "ogre_guard.rs2", guards, "if(loc_find(0_39_47_8_54, ogreguardgate2) = true) @enter_gutanoth(movecoord(loc_coord, -1, 0, 0));",
		"if(loc_find(0_39_47_53_20, ogreguardgate1right) = true) @enter_gutanoth(movecoord(loc_coord, 0, 0, -1));")
	consts := csContent(t, wtDir+"configs/quest_itwatchtower.constant")
	hasAll(t, "the constants", consts, "^gutanoth_hill = 0_39_47_50_57", "^gutanoth_hill_lower = 0_39_46_27_54")
	if got := herdCoord(t, "0_39_47_8_54"); got != (tile.Tile{X: 2504, Z: 3062}) {
		t.Errorf("the relic gate loc is at %v", got)
	}
	if got := herdCoord(t, "0_39_47_50_57"); got != (tile.Tile{X: 2546, Z: 3065}) {
		t.Errorf("the hill is %v", got)
	}
}

// The hunters: the ogres and the chieftains are cowardly hunters of range 2 that wander 3; the shamans, Gorad and the guards hunt nothing.
func TestWatchtowerContentHunters(t *testing.T) {
	blocks := npcBlocks(t, contentRoot(t))
	for _, name := range []string{"ogre", "ogre2", "ogre_chieftan"} {
		b := blocks[name]
		if b == nil || b["huntmode"] != "cowardly" || b["huntrange"] != "2" || b["wanderrange"] != "3" {
			t.Errorf("[%s] is %v: want a cowardly hunter of range 2 that wanders 3", name, b)
		}
	}
	if blocks["ogre_chieftan"]["vislevel"] != "81" || blocks["ogre"]["vislevel"] != "53" || blocks["gorad"]["vislevel"] != "68" {
		t.Errorf("the levels are %v %v %v", blocks["ogre_chieftan"]["vislevel"], blocks["ogre"]["vislevel"], blocks["gorad"]["vislevel"])
	}
	for _, name := range []string{"ogre_shaman", "gorad", "enclave_guard", "ogre_guard1", "ogre_guard2", "ogre_guard3", "ogre_guard4", "city_guard", "ogre_trader2", "skavid", "og", "grew", "toban"} {
		b := blocks[name]
		if b == nil {
			t.Errorf("no [%s] block", name)
			continue
		}
		if b["huntmode"] != "" {
			t.Errorf("[%s] has a hunt mode %q: the module takes it for a non-hunter", name, b["huntmode"])
		}
	}
	// The spawns the module walks round or prices: the chieftains of the surface and of the enclave are the plan's lists.
	npc := csPackIDs(t, "npc")
	for _, c := range append(append([]tile.Tile(nil), WtChieftainSpawns...), WtEnclaveChieftains...) {
		if !csHasRow(csMapRows(t, "NPC", c), npc["ogre_chieftan"]) {
			t.Errorf("no ogre chieftain at %v", c)
		}
	}
	for _, c := range WtShamanClear {
		if !csHasRow(csMapRows(t, "NPC", c), npc["ogre_shaman"]) {
			t.Errorf("no ogre shaman at %v", c)
		}
	}
	for _, c := range WtShamanNear {
		if !csHasRow(csMapRows(t, "NPC", c), npc["ogre_shaman"]) {
			t.Errorf("no ogre shaman at %v", c)
		}
	}
	for _, c := range []struct {
		name string
		at   tile.Tile
	}{{"og", WtOgAnchor}, {"grew", WtGrewAnchor}, {"toban", WtTobanAnchor}, {"gorad", WtGoradAnchor}, {"ogre_guard2", tile.Tile{X: 2505, Z: 3062}},
		{"ogre_guard2", tile.Tile{X: 2507, Z: 3062}}, {"ogre_guard1", tile.Tile{X: 2549, Z: 3029}}, {"ogre_guard3", WtWallGuardAnchor}, {"ogre_guard4", tile.Tile{X: 2529, Z: 3019}},
		{"city_guard", WtCityGuardAnchor}, {"enclave_guard", tile.Tile{X: 2507, Z: 3036}}, {"mad_skavid", WtCaveTalker[5]}, {"scared_skavid", WtCaveTalker[4]},
		{"ogre_trader2", tile.Tile{X: 2506, Z: 3020}}} {
		if !csHasRow(csMapRows(t, "NPC", c.at), npc[c.name]) {
			t.Errorf("no %s at %v", c.name, c.at)
		}
	}
	// The wizard stands on his floor (four of him).
	if !csHasRow(csMapRows(t, "NPC", tile.Tile{X: 2549, Z: 3112, Level: 2}), npc["watchtower_wizard"]) {
		t.Error("no wizard at the top of the ladder")
	}
	// Ground spawns the legs take: the candle, the berries, the nightshade, the robe.
	obj := csPackIDs(t, "obj")
	if !csHasRow(csMapRows(t, "OBJ", WtCandle), obj["lit_candle"]) {
		t.Errorf("no lit candle at %v", WtCandle)
	}
	for _, b := range WtBerries {
		if !csHasRow(csMapRows(t, "OBJ", b), obj["jangerberries"]) {
			t.Errorf("no jangerberries at %v", b)
		}
	}
	for _, n := range []tile.Tile{WtNightshade2, WtNightshade6} {
		if !csHasRow(csMapRows(t, "OBJ", n), obj["nightshade"]) {
			t.Errorf("no nightshade at %v", n)
		}
	}
	if !csHasRow(csMapRows(t, "OBJ", WtRobe), obj["shaman_robe"]) {
		t.Errorf("no shaman robe at %v", WtRobe)
	}
}

// The kit's sources: the shops hold what the lines say, and the bank-only lines are the ones nobody sells.
func TestWatchtowerContentShops(t *testing.T) {
	adv := csConfigBlock(csContent(t, "scripts/areas/area_ardougne_east/configs/ardougne_east.inv"), "adventurershop")
	hasAll(t, "[adventurershop]", adv, "stock1=vial_water,500,30", "stock2=bronze_pickaxe,2,100", "stock8=rope,20,100")
	herb := csConfigBlock(csContent(t, "scripts/areas/area_taverly/configs/taverly.inv"), "herbloreshop")
	hasAll(t, "[herbloreshop]", herb, "stock2=pestle_and_mortar,3,100")
	port := csContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.inv")
	hasAll(t, "port_sarim.inv", port, "stock8=deathrune,1000,150")
	// Nothing sells the bones, the guam leaf or the gold bar (the dwarven mine's bar stock is zero): they are bank and mule lines only.
	for _, item := range []string{"dragon_bones", "bat_bones", "guam_leaf"} {
		for _, f := range []string{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "scripts/areas/area_taverly/configs/taverly.inv", "scripts/areas/area_port_sarim/configs/port_sarim.inv",
			"scripts/areas/area_yanille/configs/gutanoth.inv", "scripts/areas/area_varrock/configs/varrock.inv"} {
			if strings.Contains(csContent(t, f), "="+item+",") {
				t.Errorf("%s sells %s", f, item)
			}
		}
	}
	for _, c := range []struct {
		line KitLine
		id   int
	}{{wtScarceLine(WtObjDragonBones, "Dragon bones"), WtObjDragonBones}, {wtScarceLine(WtObjBatBones, "Bat bones"), WtObjBatBones}, {wtScarceLine(WtObjGuamLeaf, "Guam leaf"), WtObjGuamLeaf}} {
		if len(c.line.Sources) != 0 || c.line.ID != c.id {
			t.Errorf("scarce line %+v has sources", c.line)
		}
	}
	for _, l := range []KitLine{wtRopeLine(), wtVialLine(), wtPickaxeLine(), wtPestleLine(), wtDeathRuneLine(), wtCandleLine()} {
		if ValidateKitLine(l) != "" {
			t.Errorf("kit line %+v: %s", l, ValidateKitLine(l))
		}
		if len(l.Sources) == 0 {
			t.Errorf("kit line %s has no source", l.Name)
		}
	}
	// The light is a lit source: three objects are in the category the mouths check.
	hasAll(t, "quest_arthur.obj", csContent(t, "scripts/quests/quest_arthur/configs/quest_arthur.obj"), "[lit_black_candle]", "[lit_candle]", "category=light_source_lit")
	hasAll(t, "firemaking.obj", csContent(t, "scripts/skill_firemaking/configs/firemaking.obj"), "[torch_lit]")
	hasAll(t, "the mouths", csContent(t, wtDir+"scripts/quest_itwatchtower.rs2"), "if(inv_totalcat(inv, light_source_lit) < 1) {", "if(inv_total(inv, skavidmap) > 0) {")
}

// The gates of the walks that the walk graph cannot know: the north-west gate is a door edge in the graph and a guard in the content, and the battlement a
// shortcut edge; the module crosses both by hand.
func TestWatchtowerContentGatesAndWall(t *testing.T) {
	main := csContent(t, wtDir+"scripts/quest_itwatchtower.rs2")
	hasAll(t, "the gates", main, "if(($guard = ogre_guard1 & %gutanoth_gold < ^gutanoth_found_gold) |  ($guard = ogre_guard2 & %itwatchtower < 4)) {",
		"[oploc1,ganothbattlement]\nif(getbit_range(%itwatchtower_bits, ^itwatchtower_market_lower, ^itwatchtower_market_upper) < 2) {")
	g := csContent(t, wtDir+"scripts/ogre_guard.rs2")
	hasAll(t, "ogre_guard.rs2", g, "case ^gutanoth_found_gold :", "~chatnpc(\"<p,neutral>I know you, creature; you may pass.\");", "%gutanoth_gold = ^gutanoth_found_gold;",
		"%itwatchtower_bits = setbit_range_toint(%itwatchtower_bits, 2, ^itwatchtower_market_lower, ^itwatchtower_market_upper);")
	// The wizard's own lost-relic menu is the module's recovery, and the crystal use is the content's last step.
	w := csContent(t, wtDir+"scripts/watchtower_wizard.rs2")
	hasAll(t, "watchtower_wizard.rs2", w, "~chatplayer(\"<p,happy>This is the last one.\");\n        @watchwiz_all_crystals;", "%itwatchtower = ^itwatchtower_found_all_crystals;",
		"} else if(last_useitem = ogre_potion) {", "@make_magic_ogre_potion;")
}

// ---------------------------------------------------------------------------------------------------------------------
// The hunters of the maps
// ---------------------------------------------------------------------------------------------------------------------

// wtSpawn is one npc row of a map square's NPC section, in world coordinates.
type wtSpawn struct{ ID, X, Z, Level int }

// wtMapNPCs lists every npc spawn of map square (mx, mz).
func wtMapNPCs(t *testing.T, mx, mz int) []wtSpawn {
	t.Helper()
	src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", mx, mz))
	var out []wtSpawn
	in := false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		if !in {
			continue
		}
		head, tail, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		var lvl, lx, lz int
		if n, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); n != 3 {
			continue
		}
		f := strings.Fields(tail)
		if len(f) == 0 {
			continue
		}
		id, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		out = append(out, wtSpawn{ID: id, X: mx<<6 + lx, Z: mz<<6 + lz, Level: lvl})
	}
	return out
}

// Every npc with a hunt mode that stands in a map square the module visits is a hunter the module knows: priced, fenced, too weak to matter, out of reach of
// every tile the plan names, or sealed in a region of its own. The squares are listed from the files, not from the plan's belief about them, so a hunter the
// price does not know fails here.
func TestWatchtowerContentEveryHunterOfTheMapsIsKnown(t *testing.T) {
	root := contentRoot(t)
	names := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	blocks := npcBlocks(t, root)
	plan := WtPlanTiles()
	seen := map[int]int{}
	for _, sq := range WtMapSquares {
		for _, n := range wtMapNPCs(t, sq[0], sq[1]) {
			name := names[n.ID]
			b := blocks[name]
			if b == nil || b["huntmode"] == "" {
				continue
			}
			seen[n.ID]++
			h, ok := WtHunterTable[n.ID]
			if !ok {
				t.Errorf("m%d_%d: npc %d (%s) at %d,%d,%d hunts (%s, level %s, huntrange %s, wanderrange %s) and WtHunterTable does not know it", sq[0], sq[1], n.ID, name,
					n.X, n.Z, n.Level, b["huntmode"], b["vislevel"], b["huntrange"], b["wanderrange"])
				continue
			}
			wander, _ := strconv.Atoi(b["wanderrange"])
			hunt, _ := strconv.Atoi(b["huntrange"])
			if b["huntmode"] != "cowardly" {
				t.Errorf("npc %d (%s) hunts as %q: the module's reach rules are the cowardly mode's", n.ID, name, b["huntmode"])
			}
			// A far hunter's reach is exactly where the engine can find an account (wander + hunt); every other is priced with a tile of slack on it.
			if floor := wander + hunt + map[bool]int{true: 0, false: 1}[h.Kind == WtHunterFar]; h.Reach < floor {
				t.Errorf("npc %d (%s): reach %d is under wander %d + hunt %d (+ a tile unless it is far)", n.ID, name, h.Reach, wander, hunt)
			}
			if h.Kind == WtHunterFar {
				for _, p := range plan {
					if p.Level == n.Level && tile.Chebyshev(p, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}) <= h.Reach {
						t.Errorf("the plan's tile %v is within %d of %s at %d,%d: it is not far", p, h.Reach, name, n.X, n.Z)
					}
				}
			}
		}
	}
	// The far hunters are exactly the plan's list (WtFarSpawns), which the pack test walks the routes past.
	listed := map[wtSpawn]bool{}
	for _, f := range WtFarSpawns {
		listed[wtSpawn{f.ID, f.At.X, f.At.Z, 0}] = true
	}
	for _, sq := range WtMapSquares {
		for _, n := range wtMapNPCs(t, sq[0], sq[1]) {
			if h, ok := WtHunterTable[n.ID]; ok && h.Kind == WtHunterFar {
				if !listed[n] {
					t.Errorf("the far hunter npc %d at %d,%d,%d is not in WtFarSpawns", n.ID, n.X, n.Z, n.Level)
				}
				delete(listed, n)
			}
		}
	}
	for n := range listed {
		t.Errorf("WtFarSpawns lists npc %d at %d,%d, which is not in the maps", n.ID, n.X, n.Z)
	}
	for id, h := range WtHunterTable {
		if seen[id] == 0 {
			t.Errorf("WtHunterTable lists npc %d (%s), which stands in none of the squares", id, h.Name)
		}
	}
}

// The ogres round each site are the maps' own: the count of npc 114 and 115 whose reach (plus the tile of a stand beside an npc) covers a stand of the site, in the
// site's own region, is the Ogres the price uses.
func TestWatchtowerContentSiteOgreCounts(t *testing.T) {
	var ogres []wtSpawn
	for _, sq := range WtMapSquares {
		for _, n := range wtMapNPCs(t, sq[0], sq[1]) {
			if (n.ID == WtNpcOgre || n.ID == WtNpcOgre2) && n.Level == 0 {
				ogres = append(ogres, n)
			}
		}
	}
	for _, site := range WtSites() {
		got := 0
		for _, o := range ogres {
			at := tile.Tile{X: o.X, Z: o.Z}
			if WtAreaOf(at) != site.Area {
				continue
			}
			for _, st := range site.Stands {
				if tile.Chebyshev(at, st) <= WtOgreReach+1 {
					got++
					break
				}
			}
		}
		if got != site.Ogres {
			t.Errorf("%s: the maps put %d ogres in reach of its stands, the plan says %d", site.Name, got, site.Ogres)
		}
		// Gorad's corner is priced as the fight it is (WtGoradOdds), not as a visit of so many ticks.
		if (site.Ticks <= 0 && site.Name != WtSiteGorad.Name) || site.Chat < 0 || site.Chat > site.Ticks {
			t.Errorf("%s: %d ticks, %d of them chat", site.Name, site.Ticks, site.Chat)
		}
	}
}

// The enclave's creatures and fire are the content's: the configs the engine reads (scripts/_unpack/225/all.npc), dragon.rs2's fire and the hunt rule.
func TestWatchtowerContentDragonsAndFire(t *testing.T) {
	root := contentRoot(t)
	blocks := npcBlocks(t, root)
	for _, c := range []struct {
		name string
		want map[string]string
	}{
		{"blue_dragon", map[string]string{"vislevel": "111", "size": "4", "attack": "95", "defence": "95", "strength": "95", "hitpoints": "105", "huntmode": "cowardly",
			"huntrange": "4", "wanderrange": "4", "param:damagetype": "^slash_style"}},
		{"greater_demon", map[string]string{"vislevel": "92", "attack": "76", "defence": "81", "strength": "78", "hitpoints": "87", "huntmode": "cowardly", "huntrange": "3", "wanderrange": "6"}},
		{"skeleton_unarmed", map[string]string{"vislevel": "25", "huntmode": "cowardly", "huntrange": "1", "wanderrange": "8"}},
		{"bat", map[string]string{"vislevel": "27", "huntmode": "cowardly", "huntrange": "2", "wanderrange": "8"}},
	} {
		b := blocks[c.name]
		for k, v := range c.want {
			if b[k] != v {
				t.Errorf("[%s] %s is %q, the plan says %q", c.name, k, b[k], v)
			}
		}
	}
	for _, f := range []struct {
		foe  MeleeFoe
		name string
	}{{FoeBlueDragon, "blue_dragon"}, {FoeGreaterDemon, "greater_demon"}} {
		b := blocks[f.name]
		atoi := func(k string) int { n, _ := strconv.Atoi(b[k]); return n }
		if f.foe.Attack != atoi("attack") || f.foe.Strength != atoi("strength") || f.foe.Defence != atoi("defence") || f.foe.Hitpoints != atoi("hitpoints") ||
			f.foe.AttackBonus != 0 || f.foe.StrengthBonus != 0 || b["param:attackrate"] != "" || f.foe.AttackTicks != 4 {
			t.Errorf("%s: the plan has %+v, the config %v", f.name, f.foe, b)
		}
	}
	dragon := csContent(t, "scripts/npc/scripts/dragon.rs2")
	hasAll(t, "dragon.rs2", dragon, "[ai_opplayer2,blue_dragon] @dragon_ai_opplayer2;", "// 1/4 chance to dragon fire (same as pre reworked elvarg)\nif (random(4) = 0) {\n    ~dragon_fire;",
		"def_int $attack_roll = ~npc_magic_attack_roll;", "def_int $defence_roll = ~player_defence_roll_specific(npc_param(damagetype));", "def_int $maxhit = 30;",
		"if (inv_total(worn, antidragonbreathshield) > 0) {\n    $maxhit = 5;", "} else if (%prayer12 = ^true) {\n    $maxhit = 10;",
		"} else if (randominc($attack_roll) > randominc($defence_roll)) {\n    $maxhit = add($maxhit, 20);", "def_int $damage = randominc(max($maxhit, 0));",
		"%npc_action_delay = add(map_clock, 4);")
	if WtDragonFireMax != 30 || WtDragonFireBonus != 20 || WtDragonShieldFire != 5 || WtDragonFireOneIn != 4 {
		t.Errorf("fire constants %d %d %d %d", WtDragonFireMax, WtDragonFireBonus, WtDragonShieldFire, WtDragonFireOneIn)
	}
	hasAll(t, "npc_combat_magic.rs2", csContent(t, "scripts/skill_combat/scripts/npc/npc_combat_magic.rs2"), "def_int $effective_magic = ~combat_effective_stat(npc_stat(magic), 100);",
		"$effective_magic = add($effective_magic, 9);", "return(~combat_stat($effective_magic, $magicattack));")
	if got := csPackIDs(t, "obj")["antidragonbreathshield"]; got != WtObjAntiDragonShield {
		t.Errorf("antidragonbreathshield is %d in the pack, the plan says %d", got, WtObjAntiDragonShield)
	}
	hasAll(t, "all.hunt", csContent(t, "scripts/_unpack/225/all.hunt"), "[cowardly]\ntype=player\ncheck_vis=lineofsight\ncheck_nottoostrong=outside_wilderness\ncheck_notcombat=%lastcombat",
		"check_notcombat_self=%npc_lastcombat\ncheck_notbusy=off\nfind_keephunting=off\nfind_newmode=opplayer2")
	// The spawns the plan's enclave tables list are the map's.
	npc := csPackIDs(t, "npc")
	for _, c := range []struct {
		name  string
		spots []tile.Tile
	}{{"blue_dragon", WtEnclaveDragons}, {"greater_demon", append(append([]tile.Tile(nil), WtEnclaveDemons...), WtEnclaveSealedDemons...)},
		{"skeleton_unarmed", WtEnclaveSkeletons}, {"bat", WtEnclaveSealedBats}} {
		for _, at := range c.spots {
			if !csHasRow(csMapRows(t, "NPC", at), npc[c.name]) {
				t.Errorf("no %s at %v", c.name, at)
			}
		}
	}
	// And every dragon, demon and skeleton of the enclave's square is in the plan's tables (none is missing).
	listed := map[wtSpawn]bool{}
	for _, at := range WtEnclaveDragons {
		listed[wtSpawn{npc["blue_dragon"], at.X, at.Z, 0}] = true
	}
	for _, at := range append(append([]tile.Tile(nil), WtEnclaveDemons...), WtEnclaveSealedDemons...) {
		listed[wtSpawn{npc["greater_demon"], at.X, at.Z, 0}] = true
	}
	for _, at := range WtEnclaveSkeletons {
		listed[wtSpawn{npc["skeleton_unarmed"], at.X, at.Z, 0}] = true
	}
	for _, at := range WtEnclaveSealedBats {
		listed[wtSpawn{npc["bat"], at.X, at.Z, 0}] = true
	}
	for _, n := range wtMapNPCs(t, 40, 147) {
		switch n.ID {
		case npc["blue_dragon"], npc["greater_demon"], npc["skeleton_unarmed"], npc["bat"]:
			if !listed[n] {
				t.Errorf("the enclave's npc %d at %d,%d,%d is in none of the plan's tables", n.ID, n.X, n.Z, n.Level)
			}
		}
	}
}

// None of the module's map squares is multi-combat (maps/multiway.csv): combat there is single-way, and a hunter skips an account that was in combat in the last
// eight ticks. The prices count every hunter at once all the same, as a ceiling (the first contact is in one tick for all of them); this pins the ground they
// ceiling.
func TestWatchtowerContentTheCountryIsSingleWay(t *testing.T) {
	multi := csContent(t, "maps/multiway.csv")
	for _, sq := range WtMapSquares {
		for lvl := 0; lvl < 4; lvl++ {
			if strings.Contains("\n"+multi, fmt.Sprintf("\n%d_%d_%d_", lvl, sq[0], sq[1])) {
				t.Errorf("map square m%d_%d level %d is in multiway.csv", sq[0], sq[1], lvl)
			}
		}
	}
}

// The three tribes' lost-piece menus, the wizard's, and Gorad's independence of Toban: the facts the recovery and the camp's order rest on.
func TestWatchtowerContentLostPiecesAndTheCampOrder(t *testing.T) {
	scripts := map[string]string{}
	for _, n := range []string{"og", "grew", "toban", "watchtower_wizard", "gorad"} {
		scripts[n] = csContent(t, wtDir+"scripts/"+n+".rs2")
	}
	// Og: the menu at stage 2 only, once helped, with no check of what the wizard holds; the piece is added whatever the wizard has.
	hasAll(t, "og.rs2", scripts["og"], "if(%itwatchtower >= ^itwatchtower_made_relic | %itwatchtower < ^itwatchtower_given_fingernails) {\n    mes(\"He's busy; try him another time.\");",
		"} else if(testbit(%itwatchtower_bits, ^itwatchtower_helped_og) = ^true) {", "\"I have lost the relic part you gave me.\", 2",
		"if(inv_total(inv, relicpart1) > 0) {", "inv_add(inv, relicpart1, 1);")
	// Grew: the option is hidden once the wizard holds his piece (relic2), which is the guard against a spare.
	hasAll(t, "grew.rs2", scripts["grew"], "if(testbit(%itwatchtower_bits, ^itwatchtower_relic2) = ^true) {\n        $op = ~p_choice2(\"Can I do anything else for you?\", 1, \"I've lost the crystal you gave me.\", 3);",
		"$op = ~p_choice3(\"Can I do anything else for you?\", 1, \"I've lost the relic part you gave me.\", 2, \"I've lost the crystal you gave me.\", 3);", "inv_add(inv, relicpart2, 1);")
	hasAll(t, "toban.rs2", scripts["toban"], "if(%itwatchtower >= ^itwatchtower_made_relic) {\n    mes(\"The ogre is not interested in you anymore.\");", "\"I can't find the relic part you gave me.\", 2",
		"inv_add(inv, relicpart3, 1);")
	// The stops carry those words.
	for _, c := range []struct {
		src  string
		stop NpcStop
	}{{scripts["og"], WtOgLostPartStop}, {scripts["grew"], WtGrewLostPartStop}, {scripts["toban"], WtTobanLostPartStop}} {
		if len(c.stop.Prefer) != 1 || !strings.Contains(c.src, "\""+c.stop.Prefer[0]+"\"") {
			t.Errorf("%s's lost-piece option %v is not in his script", c.stop.Npc, c.stop.Prefer)
		}
	}
	// The wizard: the lost-relic menu is the made-relic case's (stage 3); at stage 2 he can do nothing; a piece he holds is refused and stays in the pack.
	w := scripts["watchtower_wizard"]
	hasAll(t, "watchtower_wizard.rs2", w, "case ^itwatchtower_made_relic :", "switch_int(~p_choice2(\"I have lost the relic you gave me.\", 1, \"I will find my way in, no problem.\", 2)) {",
		"case ^itwatchtower_given_fingernails :", "\"I have lost something the ogres gave to me.\"", "~chatnpc(\"<p,neutral>Oh, deary me!|Well, there's nothing I can do about it.|You will have to go back to them, I'm afraid.\");",
		"if(testbit(%itwatchtower_bits, ^itwatchtower_relic1) = ^true) {\n        ~chatnpc(\"<p,confused>I already have that part...\"); // rs3, rsc matches\n        return;",
		"if(testbit(%itwatchtower_bits, ^itwatchtower_relic2) = ^true) {\n        ~chatnpc(\"<p,confused>I already have that part...\");\n        return;",
		"if(testbit(%itwatchtower_bits, ^itwatchtower_relic3) = ^true) {\n        ~chatnpc(\"<p,confused>I already have that part...\");\n        return;")
	// Gorad: his tooth needs Grew's task and nothing of Toban's, so the fight may come before Toban's talk.
	g := scripts["gorad"]
	if strings.Contains(strings.ToLower(g), "toban") {
		t.Error("gorad.rs2 mentions Toban: the camp's order (chest, Gorad, Toban) rests on the tooth not depending on him")
	}
	hasAll(t, "gorad.rs2", g, "getbit_range(%itwatchtower_bits, ^itwatchtower_spoken_grew, ^itwatchtower_helped_grew) = 1", "if(%itwatchtower < ^itwatchtower_given_relic) queue(defeat_gorad, 0, 0);")
}
