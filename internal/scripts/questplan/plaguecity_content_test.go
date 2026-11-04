package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward, rule, shop and foe Plague City rests on, read from a Content 289 checkout (RSB0T_CONTENT,
// skipped without one) so that none of them can drift away from the scripts and maps they were taken from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run PlagueContent ./internal/scripts/questplan/
//
// It uses the cook/sheep content test's readers (cooksheep_content_test.go) and the Ernest test's scriptBlock.

const (
	plagueDir     = "scripts/quests/quest_elena/"
	plagueScripts = plagueDir + "scripts/"
	plagueConfigs = plagueDir + "configs/"
	plagueWest    = "scripts/areas/area_ardougne_west/"
)

// plagueConsts reads the quest's constants (quest_elena.constant and the two in general/configs/quest.constant).
func plagueConsts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	re := regexp.MustCompile(`^\^(\w+)\s*=\s*(\d+)\s*$`)
	for _, f := range []string{plagueConfigs + "quest_elena.constant", "scripts/general/configs/quest.constant"} {
		for _, line := range strings.Split(csContent(t, f), "\n") {
			if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				n, _ := strconv.Atoi(m[2])
				out[m[1]] = n
			}
		}
	}
	return out
}

// plagueRenderJournal runs elena_journal.rs2 for one value of %elenaquest the way the script reads: nested ifs and else-ifs over
// `%elenaquest OP ^constant` conditions joined by `|`, each active `append` adding its string, and the first ~quest_journal call
// that is reached ending the script. The result is the text the client would split at `|`.
func plagueRenderJournal(t *testing.T, src string, stage int, consts map[string]int) string {
	t.Helper()
	type frame struct{ parent, taken, active bool }
	var stack []frame
	active := func() bool { return len(stack) == 0 || stack[len(stack)-1].active }
	condRe := regexp.MustCompile(`%elenaquest\s*(>=|<=|!=|=|>|<|!)\s*\^(\w+)`)
	eval := func(cond string) bool {
		for _, clause := range strings.Split(cond, " | ") {
			m := condRe.FindStringSubmatch(clause)
			if m == nil {
				t.Fatalf("a journal condition the renderer does not read: %q", clause)
			}
			v, ok := consts[m[2]]
			if !ok {
				t.Fatalf("constant ^%s not found", m[2])
			}
			var r bool
			switch m[1] {
			case "=":
				r = stage == v
			case ">=":
				r = stage >= v
			case ">":
				r = stage > v
			case "<":
				r = stage < v
			case "<=":
				r = stage <= v
			case "!", "!=":
				r = stage != v
			}
			if r {
				return true
			}
		}
		return false
	}
	strRe := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	text := ""
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "//"):
		case strings.HasPrefix(line, "if (") && strings.HasSuffix(line, ") {"):
			v := active() && eval(line[len("if ("):len(line)-len(") {")])
			stack = append(stack, frame{parent: active(), taken: v, active: v})
		case strings.HasPrefix(line, "} else if (") && strings.HasSuffix(line, ") {"):
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			v := top.parent && !top.taken && eval(line[len("} else if ("):len(line)-len(") {")])
			stack = append(stack, frame{parent: top.parent, taken: top.taken || v, active: v})
		case line == "} else {":
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			v := top.parent && !top.taken
			stack = append(stack, frame{parent: top.parent, taken: true, active: v})
		case line == "}":
			stack = stack[:len(stack)-1]
		case active() && strings.HasPrefix(line, "$text = append($text, "):
			text += strRe.FindStringSubmatch(line)[1]
		case active() && strings.HasPrefix(line, "$text = "):
			if m := strRe.FindStringSubmatch(line); m != nil {
				text = m[1]
			}
		case active() && strings.HasPrefix(line, "~quest_journal("):
			return text
		}
	}
	t.Fatalf("stage %d: the journal script never reached ~quest_journal", stage)
	return ""
}

func TestPlagueContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "edmond", PlagueNpcEdmond}, {"npc", npc, "alrena", PlagueNpcAlrena}, {"npc", npc, "jethick", PlagueNpcJethick},
		{"npc", npc, "ted_rehnison", PlagueNpcTed}, {"npc", npc, "martha_rehnison", PlagueNpcMartha}, {"npc", npc, "milli", PlagueNpcMilli},
		{"npc", npc, "clerk", PlagueNpcClerk}, {"npc", npc, "bravek", PlagueNpcBravek}, {"npc", npc, "elenap", PlagueNpcElena},
		{"npc", npc, "mournertwb", PlagueNpcMourner}, {"npc", npc, "jatix", PlagueNpcJatix}, {"npc", npc, "guarddog", PlagueNpcGuardDog},
		{"npc", npc, "hobgoblin_unarmed", PlagueNpcHobgob}, {"npc", npc, "hobgoblin_armed", PlagueNpcHobgobSp}, {"npc", npc, "cow", NpcDairyCow},
		{"npc", npc, "aemad", AemadStore.NpcID}, {"npc", npc, "wydin", WydinShop.NpcID},
		{"obj", obj, "dwellberries", PlagueObjDwellberries}, {"obj", obj, "gasmask", PlagueObjGasMask}, {"obj", obj, "elena_picture", PlagueObjPicture},
		{"obj", obj, "turnip_book", PlagueObjBook}, {"obj", obj, "scruffy_note", PlagueObjNote}, {"obj", obj, "hangover_cure", PlagueObjHangoverCure},
		{"obj", obj, "warrant", PlagueObjWarrant}, {"obj", obj, "elenakey", PlagueObjKey}, {"obj", obj, "ardougnescroll", PlagueObjScroll},
		{"obj", obj, "pestle_and_mortar", PlagueObjPestle}, {"obj", obj, "snape_grass", PlagueObjSnapeGrass}, {"obj", obj, "chocolate_bar", PlagueObjChocolateBar},
		{"obj", obj, "chocolate_dust", PlagueObjChocolateDust}, {"obj", obj, "chocolaty_milk", PlagueObjChocolateMilk}, {"obj", obj, "shark", PlagueObjShark},
		{"obj", obj, "spade", ObjSpade}, {"obj", obj, "rope", ObjRope}, {"obj", obj, "bucket_empty", ObjBucket}, {"obj", obj, "bucket_water", ObjBucketOfWater},
		{"obj", obj, "bucket_milk", ObjBucketOfMilk}, {"obj", obj, "coins", CoinsID},
		{"loc", loc, "plaguemudpatch1", PlagueLocMudPatch1}, {"loc", loc, "plaguemudpatch2", PlagueLocMudPatch2}, {"loc", loc, "plaguemudpile", PlagueLocMudPile},
		{"loc", loc, "plaguesewerpipe", PlagueLocSewerPipe}, {"loc", loc, "plaguemanholeclosed", PlagueLocManholeShut}, {"loc", loc, "plaguemanholeopen", PlagueLocManholeOpen},
		{"loc", loc, "alrenascupboardshut", PlagueLocCupboardShut}, {"loc", loc, "alrenascupboardopen", PlagueLocCupboardOpen},
		{"loc", loc, "rehnisondoorshut", PlagueLocRehnisonDoor}, {"loc", loc, "rehnisonstairs", PlagueLocRehnisonStairs}, {"loc", loc, "rehnisonstairstop", PlagueLocRehnisonTop},
		{"loc", loc, "loc_2535", PlagueLocHouseDoor}, {"loc", loc, "plaguekeybarrel", PlagueLocBarrel}, {"loc", loc, "plaguehousestairsdown", PlagueLocHouseStairs},
		{"loc", loc, "plaguehousestairsup", PlagueLocCellarStairs}, {"loc", loc, "elenagateshut", PlagueLocElenaGate}, {"loc", loc, "bravekdoorshut", PlagueLocBravekDoor},
		{"loc", loc, "fountain", PlagueLocFountain},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s: the pack says %d (present %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

func TestPlagueContentStagesAndRewards(t *testing.T) {
	consts := plagueConsts(t)
	for name, want := range map[string]int{
		"quest_elena_not_started": PlagueNotStarted, "quest_elena_started": PlagueStarted, "quest_elena_gasmask": PlagueGasMask,
		"quest_elena_started_mud_patch": PlagueWater, "quest_elena_mud_patch4": PlagueSoft, "quest_elena_opened_tunnel": PlagueTunnel,
		"quest_elena_tied_rope": PlagueRope, "quest_elena_opened_pipe": PlaguePipeOpen, "quest_elena_shown_picture": PlagueShown,
		"quest_elena_spoken_martha_ted": PlagueSpokenParents, "quest_elena_spoke_to_milli": PlagueSpokenMilli,
		"quest_elena_spoke_to_plague_house": PlagueNeedClearance, "quest_elena_spoke_to_bravek": PlagueSpokenBravek,
		"quest_elena_spoke_cured_bravek": PlagueCured, "quest_elena_freed_elena": PlagueFreed, "elena_complete": PlagueComplete,
		"elena_complete_read_scroll": PlagueCompleteScroll, "elena_questpoints": 1,
	} {
		if consts[name] != want {
			t.Errorf("^%s is %d in the content, the plan says %d", name, consts[name], want)
		}
	}
	// The intermediate stages the plan folds into groups.
	if consts["quest_elena_mud_patch1"] != 4 || consts["quest_elena_mud_patch3"] != 6 || consts["quest_elena_returned_book"] != 21 ||
		consts["quest_elena_spoke_to_clerk"] != 25 {
		t.Errorf("the grouped stages moved: %v", consts)
	}
	if PlagueCityRecord.QuestPoints != consts["elena_questpoints"] || LadderFacts["elena"].XP["mining"] != 2425 || LadderFacts["elena"].QP != 1 {
		t.Errorf("record %+v ladder row %+v: 24,250 Mining XP at 1x is 2,425 and the quest is worth 1 point", PlagueCityRecord, LadderFacts["elena"])
	}
	src := csContent(t, plagueScripts+"quest_elena.rs2")
	hasAll(t, "quest_elena.rs2", src,
		"stat_advance(mining, 24250);", "inv_add(inv, ardougnescroll, 1);", "[queue,quest_elena_complete]",
		"inv_del(inv, warrant, ^max_32bit_int);", "inv_del(inv, elenakey, ^max_32bit_int);", "inv_del(inv, turnip_book, ^max_32bit_int);")
	// The varp is perm and not transmitted: the tab's colour and the journal are the only readers.
	if v := csContent(t, "scripts/_unpack/225/all.varp"); !strings.Contains(csConfigBlock(v, "elenaquest"), "scope=perm") || strings.Contains(csConfigBlock(v, "elenaquest"), "transmit") {
		t.Errorf("%%elenaquest: %q", csConfigBlock(v, "elenaquest"))
	}
	// The teleport coordinates: the dig's landing, the mud pile's, the pipe's and the manhole's.
	for _, c := range []struct {
		name string
		want tile.Tile
	}{
		{"quest_elena_sewer_mud_pile_coord", PlagueSewerLanding}, {"quest_elena_garden_coord", tile.Tile{X: 2566, Z: 3331}},
		{"quest_elena_west_ardy_manhole_tele_coord", PlagueWestLanding}, {"quest_elena_west_ardy_manhole_coord", PlagueManhole},
		{"quest_elena_sewer_sewerpipe_coord", PlagueSewerPipeEnd},
	} {
		re := regexp.MustCompile(`\^` + c.name + `\s*=\s*(\d+)_(\d+)_(\d+)_(\d+)_(\d+)`)
		m := re.FindStringSubmatch(csContent(t, plagueConfigs+"quest_elena.constant"))
		if m == nil {
			t.Errorf("no ^%s", c.name)
			continue
		}
		n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
		got := tile.Tile{X: n(2)*64 + n(4), Z: n(3)*64 + n(5), Level: n(1)}
		if got != c.want {
			t.Errorf("^%s is %v, the plan has %v", c.name, got, c.want)
		}
	}
}

// The journal script, run for every stage, parses to the stage the plan says (the lowest of each group the journal renders alike),
// and the lines the parser keys on are really in the stage's text and really absent from every earlier stage's.
func TestPlagueContentJournalRendersToTheStageTheParserReads(t *testing.T) {
	consts := plagueConsts(t)
	src := csContent(t, plagueScripts+"elena_journal.rs2")
	want := func(stage int) int {
		switch {
		case stage == 0:
			return PlagueNotStarted
		case stage >= 3 && stage <= 6:
			return PlagueWater
		case stage == 20 || stage == 21:
			return PlagueShown
		case stage == 24 || stage == 25:
			return PlagueNeedClearance
		case stage >= 29:
			return PlagueComplete
		}
		return stage
	}
	for _, stage := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30} {
		text := plagueRenderJournal(t, src, stage, consts)
		if text == "" {
			t.Errorf("stage %d rendered nothing", stage)
			continue
		}
		got, ok := ParsePlagueJournal(strings.Split(text, "|"))
		if !ok || got != want(stage) {
			t.Errorf("stage %d (%q): parsed %d (ok %v), want %d", stage, text, got, ok, want(stage))
		}
	}
	// The groups really are rendered alike (the reason the plan folds them): 3-6, 20-21 and 24-25 render the same text.
	for _, g := range [][]int{{3, 4, 5, 6}, {20, 21}, {24, 25}} {
		first := plagueRenderJournal(t, src, g[0], consts)
		for _, s := range g[1:] {
			if got := plagueRenderJournal(t, src, s, consts); got != first {
				t.Errorf("stages %d and %d render differently, so the plan could tell them apart and does not:\n %q\n %q", g[0], s, first, got)
			}
		}
	}
	// And every other pair differs, so the parser has nothing it could not tell apart: the stages it reports are all distinct texts.
	seen := map[string]int{}
	for _, stage := range []int{0, 1, 2, 3, 7, 8, 9, 10, 20, 22, 23, 24, 26, 27, 28, 29} {
		text := plagueRenderJournal(t, src, stage, consts)
		if other, dup := seen[text]; dup {
			t.Errorf("stages %d and %d render the same text", other, stage)
		}
		seen[text] = stage
	}
	// The tab's complete state ends the quest (the in-progress text never holds it).
	for _, stage := range []int{0, 1, 10, 20, 28} {
		if strings.Contains(strings.ToLower(plagueRenderJournal(t, src, stage, consts)), "quest complete") {
			t.Errorf("stage %d's journal says QUEST COMPLETE", stage)
		}
	}
}

// Every menu option the plan's stops choose is in the content, in the order the conversation reaches it.
func TestPlagueContentDialogue(t *testing.T) {
	check := func(file string, stop NpcStop) {
		t.Helper()
		src := csContent(t, file)
		last := -1
		for _, line := range stop.Prefer {
			i := strings.Index(src, `"`+line+`"`)
			if i < 0 && line != "No problem." {
				t.Errorf("%s (%s) has no option %q", file, stop.Npc, line)
			}
			if i >= 0 && i < last && stop.Npc != "Edmond" {
				t.Errorf("%s: option %q comes before the previous preference", file, line)
			}
			if i >= 0 {
				last = i
			}
		}
	}
	check(plagueScripts+"edmond.rs2", PlagueEdmondStart)
	check(plagueScripts+"jethick.rs2", PlagueJethickStop)
	check(plagueScripts+"doors.rs2", PlagueMournerStop)
	check(plagueWest+"scripts/clerk.rs2", PlagueClerkStop)
	check(plagueScripts+"bravek.rs2", PlagueBravekRecipe)
	check(plagueScripts+"bravek.rs2", PlagueBravekWarrant)
	// Edmond's first conversation: the first menu is the two-option one that opens the second.
	edmond := csContent(t, plagueScripts+"edmond.rs2")
	hasAll(t, "edmond.rs2", edmond,
		`@multi2("What's happened to her?", edmond_what_happened_to_elena, "Well, good luck finding her.", edmond_goodluck_finding_elena);`,
		`@multi3("Tell me more about the plague.", edmond_tell_me_more, "Can I help find her?", edmond_can_i_help_find_elena, "I'm sorry, I have to go.", edmond_sorry_i_have_to_go);`,
		"~quest_elena_set_progress(^quest_elena_started);")
	// No other talk with Edmond has a menu until the quest is over (so the stops' "No problem." is only the post-quest line).
	for _, stage := range []string{"quest_elena_gasmask", "quest_elena_tied_rope", "quest_elena_freed_elena"} {
		if !strings.Contains(edmond, "^"+stage) {
			t.Errorf("edmond.rs2 no longer handles ^%s", stage)
		}
	}
	// Mourner at the plague house door: the menu is reached only with a mourner within 14 tiles, and "I fear not a mere plague." goes
	// straight to the clearance question.
	doors := csContent(t, plagueScripts+"doors.rs2")
	hasAll(t, "doors.rs2", doors,
		`@multi3("But I think a kidnap victim is in here.", plague_house_but_i_think_a_kidnap_victim_is_in_here,`,
		`"I fear not a mere plague.", plague_house_i_fear_not_a_mere_plague,`,
		"npc_find(coord, mournertwb, 14, 0) = true", "case ^quest_elena_spoke_to_milli :", "case ^quest_elena_spoke_cured_bravek :",
		"inv_total(inv, warrant) > 0 & npc_find(coord, mournertwb, 14, 0) = true", "~open_and_close_door_fast(loc_2536, true, door_open);",
		"You wait until the mourner's back is turned and sneak into the building.",
		"if (~check_axis(coord, loc_coord, loc_angle) = false) {", "~quest_elena_set_progress(^quest_elena_spoke_to_plague_house);")
	// The clerk calls Bravek in only with Bravek within seven tiles.
	clerk := csContent(t, plagueWest+"scripts/clerk.rs2")
	hasAll(t, "clerk.rs2", clerk, `@multi3("I need permission to enter a plague house.", clerk_i_need_permission,`,
		`"This is urgent though!", clerk_this_is_urgent_though);`, ".npc_find(coord, bravek, 7, 0) = true", "~quest_elena_set_progress(^quest_elena_spoke_to_clerk);")
	// Bravek: his door opens from stage 25 on, the recipe is the third option of his second menu, the warrant the third of the last.
	westDoors := csContent(t, plagueWest+"scripts/doors.rs2")
	hasAll(t, "doors.rs2", westDoors, "[oploc1,bravekdoorshut]",
		"case ^quest_elena_spoke_to_clerk, ^quest_elena_spoke_to_bravek, ^quest_elena_spoke_cured_bravek, ^quest_elena_freed_elena, ^elena_complete, ^elena_complete_read_scroll : @bravek_enter_room;",
		"~open_and_close_door2(loc_param(next_loc_stage), ~check_axis(coord, loc_coord, loc_angle), door_open);",
		"ardougnewalldoor", "...But they will not open.")
	bravek := csContent(t, plagueScripts+"bravek.rs2")
	hasAll(t, "bravek.rs2", bravek,
		`@multi2("This is really important though!", bravek_this_is_really_important_though, "Ok, goodbye.", bravek_okay_goodbye);`,
		`"Do you know what is in the cure?", bravek_do_you_know_what_is_in_the_cure);`,
		`@multi3("Ok, I'll go speak to them.", bravek_okay_ill_go_speak_to_them, "Is that all anyone says around here?", bravek_is_that_all_anyone_says_around_here, "They won't listen to me!", bravek_they_wont_listen_to_me);`,
		"~quest_elena_set_progress(^quest_elena_spoke_to_bravek);", "inv_add(inv, scruffy_note, 1);", "inv_add(inv, warrant, 1);",
		"~quest_elena_set_progress(^quest_elena_spoke_cured_bravek);", "if (inv_total(inv, hangover_cure) > 0) {", "inv_del(inv, hangover_cure, 1);",
		"if (inv_total(inv, warrant) > 0) {")
	// Jethick: the picture is shown, the book given only while none is held anywhere.
	jethick := csContent(t, plagueScripts+"jethick.rs2")
	hasAll(t, "jethick.rs2", jethick, `@multi2("I'm looking for a woman from East Ardougne.", jethick_im_looking_for_a_woman,`,
		"if (inv_total(inv, elena_picture) > 0) {", "%elenaquest = ^quest_elena_shown_picture;", "if (~obj_gettotal(turnip_book) = 0) {", "inv_add(inv, turnip_book, 1);",
		"case ^quest_elena_opened_pipe, ^quest_elena_shown_picture :")
	if p := csContent(t, "scripts/general/scripts/misc/inv_procs.rs2"); !strings.Contains(scriptBlock(p, "proc,obj_gettotal"), "inv_total(inv, $obj) + inv_total(bank, $obj) + inv_total(worn, $obj)") {
		t.Error("obj_gettotal no longer counts the pack, the bank and the worn items")
	}
	// The Rehnisons: the door acts only with Ted within seven tiles (before stage 21), the chat is plain pages, the stairs land at 2527,3331.
	hasAll(t, "doors.rs2", doors, "def_loc $next_loc = loc_param(next_loc_stage);", "if (%elenaquest >= ^quest_elena_returned_book) {", "if (npc_find(coord, ted_rehnison, 7, 0) = false) {",
		"case ^quest_elena_shown_picture :", "if (inv_total(inv, turnip_book) > 0) {", "~quest_elena_set_progress(^quest_elena_returned_book);", "inv_del(inv, turnip_book, 1);",
		"[oploc1,rehnisondoorshut] @rehnissons_enter_house;")
	reh := csContent(t, plagueScripts+"rehnisons.rs2")
	hasAll(t, "rehnisons.rs2", reh, "if(%elenaquest = ^quest_elena_returned_book) ~quest_elena_set_progress(^quest_elena_spoken_martha_ted);",
		"~quest_elena_set_progress(^quest_elena_spoke_to_milli);", "p_telejump(1_39_52_31_3);", "p_telejump(0_39_52_31_3);",
		"[opnpc1,ted_rehnison] @rehnisons_i_hear_a_woman_is_staying_here;", "[opnpc1,martha_rehnison] @rehnisons_i_hear_a_woman_is_staying_here;")
	// Elena at stage 27 and the plague house's barrel, stairs and cell.
	house := csContent(t, plagueScripts+"plaguehouse.rs2")
	hasAll(t, "plaguehouse.rs2", house, "[oploc1,plaguekeybarrel]", "case ^quest_elena_spoke_cured_bravek :", "if (inv_total(inv, elenakey) > 0) {", "inv_add(inv, elenakey, 1);",
		"p_telejump(0_39_151_41_6);", "p_telejump(0_39_51_40_7);", "[oplocu,elenagateshut]", "case elenakey :",
		"case ^quest_elena_freed_elena, ^elena_complete, ^elena_complete_read_scroll : ~open_and_close_door(loc_param(next_loc_stage), ~check_axis(coord, loc_coord, loc_angle), false);",
		"if (inv_total(inv, elenakey) > 0) {", "~mesbox(\"You unlock the door.\");")
	hasAll(t, "elena.rs2", csContent(t, plagueScripts+"elena.rs2"), "[opnpc1,elenap]", "~quest_elena_set_progress(^quest_elena_freed_elena);")
}

// The garden, the pipe and the cupboard: what each op needs, as the scripts say it.
func TestPlagueContentTheDigThePipeAndTheCupboard(t *testing.T) {
	patch := csContent(t, plagueScripts+"mud_patch.rs2")
	hasAll(t, "mud_patch.rs2", patch, "[oplocu,plaguemudpatch1]", "[oplocu,plaguemudpatch2]", "case spade : @elena_dig_mud_patch;", "case bucket_water : @elena_bucket_mud_patch(last_useslot);",
		"if (%elenaquest < ^quest_elena_mud_patch4) {", "if (%elenaquest < ^quest_elena_opened_tunnel) ~quest_elena_set_progress(^quest_elena_opened_tunnel);",
		"p_telejump(^quest_elena_sewer_mud_pile_coord);",
		"case ^quest_elena_started_mud_patch : @elena_soften_mud_patch($slot, ^quest_elena_mud_patch1);",
		"case ^quest_elena_mud_patch1 : @elena_soften_mud_patch($slot, ^quest_elena_mud_patch2);",
		"case ^quest_elena_mud_patch2 : @elena_soften_mud_patch($slot, ^quest_elena_mud_patch3);",
		"case ^quest_elena_mud_patch3 : @elena_soften_mud_patch($slot, ^quest_elena_mud_patch4);",
		"inv_delslot(inv, $slot);", "inv_add(inv, bucket_empty, 1);", "You don't need to pour any more water. The soil is soft enough already.",
		"The soil is now soft enough to dig into.", "You see no reason to do that at the moment.")
	if !strings.Contains(csContent(t, plagueScripts+"mud_pile.rs2"), "p_telejump(^quest_elena_garden_coord);") {
		t.Error("the mud pile no longer lands in the garden")
	}
	pipe := csContent(t, plagueScripts+"sewerpipe.rs2")
	hasAll(t, "sewerpipe.rs2", pipe, "inv_getobj(worn, ^wearpos_hat) = gasmask", "[oplocu,plaguesewerpipe]", "case ^quest_elena_opened_tunnel :", "case rope :",
		"~quest_elena_set_progress(^quest_elena_tied_rope);", "inv_del(inv, rope, 1);", "p_telejump(^quest_elena_west_ardy_manhole_tele_coord);",
		"loc_change(plaguemanholeopen, 500);", "%elena_pipe_used = calc(map_clock + 6);", "I can't let you enter the city without your gas mask on.",
		"case ^quest_elena_opened_pipe, ^quest_elena_shown_picture, ^quest_elena_returned_book, ^quest_elena_spoken_martha_ted, ^quest_elena_spoke_to_milli, ^quest_elena_spoke_to_plague_house, ^quest_elena_spoke_to_clerk, ^quest_elena_spoke_to_bravek, ^quest_elena_spoke_cured_bravek, ^quest_elena_freed_elena :")
	// The pipe's tmp varp is per player (no scope): not a fleet-wide gate.
	if v := csContent(t, plagueConfigs+"elena.vars"); strings.Contains(v, "scope=") {
		t.Errorf("elena_pipe_used has a scope: %q", v)
	}
	manhole := csContent(t, plagueWest+"scripts/manhole.rs2")
	hasAll(t, "manhole.rs2", manhole, "[oploc1,plaguemanholeclosed] @elena_open_manhole;", "[oploc1,plaguemanholeopen] @elena_enter_manhole;",
		"p_telejump(^quest_elena_sewer_sewerpipe_coord);", "loc_change(loc_param(next_loc_stage), 500);")
	cfg := csContent(t, plagueWest+"configs/manhole.loc")
	hasAll(t, "manhole.loc", cfg, "[plaguemanholeclosed]", "op1=Open", "[plaguemanholeopen]", "op1=Climb-down")
	// The cupboard: opened and searched from the east (forceapproach), the mask handed over only below the stage ... and only while none is owned anywhere.
	cup := csContent(t, plagueScripts+"cupboard.rs2")
	hasAll(t, "cupboard.rs2", cup, "if (%elenaquest < ^quest_elena_gasmask) {", "if (~obj_gettotal(gasmask) = 0) {", "inv_add(inv, gasmask, 1);",
		"inv_total(bank, gasmask) > 0 & (inv_total(inv, gasmask) = 0 & inv_total(worn, gasmask) = 0)")
	if !strings.Contains(csContent(t, "scripts/general_use/scripts/cupboards.rs2"), "case 0_40_52_14_6 : @elena_search_cupboard;") {
		t.Error("Alrena's cupboard is no longer at 2574,3334")
	}
	hasAll(t, "cupboard.loc", csContent(t, plagueConfigs+"cupboard.loc"), "[alrenascupboardshut]", "op1=Open", "forceapproach=east", "[alrenascupboardopen]", "op1=Search")
	// Alrena makes the mask from one berry and the gas mask is the hat slot's.
	alrena := csContent(t, plagueScripts+"alrena.rs2")
	hasAll(t, "alrena.rs2", alrena, "inv_del(inv, dwellberries, 1);", "inv_add(inv, gasmask, 1);", "~quest_elena_set_progress(^quest_elena_gasmask);")
	hasAll(t, "gasmask.obj", csContent(t, plagueConfigs+"gasmask.obj"), "wearpos=hat", "tradeable=no")
	// The water is a fountain, the sink or a well: one loc category.
	water := csContent(t, "scripts/general_use/scripts/water_sources.rs2")
	hasAll(t, "water_sources.rs2", water, "[oplocu,_watersource]", "case bucket_empty, jug_empty", "inv_add(inv, oc_param($container, next_obj_stage), 1);")
	if f := csConfigBlock(csContent(t, "scripts/general_use/configs/water_sources.loc"), "fountain"); !strings.Contains(f, "category=watersource") || !strings.Contains(f, "width=2") {
		t.Errorf("the fountain: %q", f)
	}
}

// The hangover cure's recipe, and the items' properties the plan keys on.
func TestPlagueContentTheRecipe(t *testing.T) {
	cakes := csContent(t, "scripts/skill_cooking/scripts/cooking_inv/scripts/cakes/cakes.rs2")
	hasAll(t, "cakes.rs2", cakes,
		"[opheldu,chocolate_bar]", "case pestle_and_mortar : ~attempt_grind_ingredient(last_useslot, last_slot);",
		"[opheldu,chocolate_dust]", "case bucket_milk : @make_chocolate_milk(last_slot);",
		"[opheldu,snape_grass]", "case chocolaty_milk : @make_hangover_cure(last_slot);",
		"[opheldu,chocolaty_milk]", "case snape_grass : @make_hangover_cure(last_useslot);",
		"inv_add(inv, chocolaty_milk, 1);", "inv_add(inv, hangover_cure, 1);", "inv_del(inv, bucket_milk, 1);", "inv_del(inv, chocolaty_milk, 1);")
	objs := csContent(t, "scripts/skill_cooking/configs/cooking_inv/configs/cakes/cakes.obj")
	hasAll(t, "cakes.obj", csConfigBlock(objs, "chocolate_bar"), "param=grindable_ingredient_refined,chocolate_dust", "iop1=Eat")
	hasAll(t, "dwellberries", csConfigBlock(csContent(t, "scripts/skill_cooking/configs/gnome_cooking/gnome_cooking.obj"), "dwellberries"), "iop1=Eat")
	hasAll(t, "hangover_cure.obj", csContent(t, plagueConfigs+"hangover_cure.obj"), "name=Hangover cure", "tradeable=no")
	if s := csContent(t, "scripts/skill_herblore/scripts/herblore.rs2"); !strings.Contains(scriptBlock(s, "proc,grind_ingredient"), "inv_add(inv, $refined, 1);") {
		t.Error("the grinding no longer adds the refined ingredient")
	}
	// The pestle's grind has no level gate: attempt_grind_ingredient asks the item's params and members only.
	grind := scriptBlock(csContent(t, "scripts/skill_herblore/scripts/grinding/grind_ingredient.rs2"), "proc,attempt_grind_ingredient")
	if strings.Contains(grind, "stat(") || strings.Contains(grind, "~herblore_levelup") {
		t.Errorf("grinding gained a level gate: %q", grind)
	}
	// The cow: [opnpcu,cow] with an empty bucket gives milk with no delay.
	hasAll(t, "cow_milking.rs2", csContent(t, "scripts/npc/scripts/cow_milking.rs2"), "[opnpcu,cow]", "case bucket_empty : @milk_cow;", "inv_add(inv, bucket_milk, 1);")
}

// The shops the plan buys from keep the items it says and are run by the keepers it aims at.
func TestPlagueContentShops(t *testing.T) {
	adv := csConfigBlock(csContent(t, "scripts/areas/area_ardougne_east/configs/ardougne_east.inv"), "adventurershop")
	hasAll(t, "[adventurershop]", adv, "stock8=rope,20,100", "restock=yes")
	aemad := csConfigBlock(csContent(t, "scripts/areas/area_ardougne_east/configs/ardougne_east.npc"), "aemad")
	hasAll(t, "[aemad]", aemad, "param=owned_shop,adventurershop", "param=shop_sell_multiplier,1300", "op3=Trade")
	if AemadStore.NpcID != PlagueNpcAlrena-120 && AemadStore.Npc != "Aemad" {
		t.Errorf("AemadStore %+v", AemadStore)
	}
	herb := csConfigBlock(csContent(t, "scripts/areas/area_taverly/configs/taverly.inv"), "herbloreshop")
	hasAll(t, "[herbloreshop]", herb, "stock2=pestle_and_mortar,3,100", "restock=yes")
	jatix := csConfigBlock(csContent(t, "scripts/areas/area_taverly/configs/taverly.npc"), "jatix")
	hasAll(t, "[jatix]", jatix, "param=owned_shop,herbloreshop", "op3=Trade")
	food := csConfigBlock(csContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.inv"), "wydinstore")
	hasAll(t, "[wydinstore]", food, "stock8=chocolate_bar,1,100", "restock=yes")
	wydin := csConfigBlock(csContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.npc"), "wydin")
	hasAll(t, "[wydin]", wydin, "param=owned_shop,wydinstore", "param=shop_sell_multiplier,1000")
	khazard := csConfigBlock(csContent(t, "scripts/areas/area_ardougne_east/configs/port_khazard/port_khazard.inv"), "khazardshop")
	hasAll(t, "[khazardshop]", khazard, "stock5=bucket_empty,2,100")
	// Prices: the kit's per-item prices cover the base cost times the keeper's multiplier.
	for _, c := range []struct {
		name  string
		price int
		cost  int
		mult  int
	}{{"rope", 24, 18, 1300}, {"chocolate bar", 10, 10, 1000}} {
		if c.price < c.cost*c.mult/1000 {
			t.Errorf("the kit prices %s at %d, below %d x %d/1000", c.name, c.price, c.cost, c.mult)
		}
	}
	for _, l := range plagueBasics(Snapshot{}, PlagueWater) {
		if l.ID == ObjRope {
			if got := l.Sources[0].Price; got < 18*1300/1000 {
				t.Errorf("the rope is priced at %d", got)
			}
		}
	}
}

// The tiles the plan stands on and walks to are the maps': every npc, loc and ground spawn.
func TestPlagueContentTiles(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"Edmond", "NPC", PlagueEdmondAnchor, PlagueNpcEdmond}, {"Alrena", "NPC", PlagueAlrenaAnchor, PlagueNpcAlrena},
		{"Edmond in the sewer", "NPC", PlagueSewerEdmond, PlagueNpcEdmond}, {"Jethick", "NPC", PlagueJethickAnchor, PlagueNpcJethick},
		{"Ted", "NPC", PlagueTedAnchor, PlagueNpcTed}, {"Martha", "NPC", tile.Tile{X: 2531, Z: 3333}, PlagueNpcMartha},
		{"Milli", "NPC", PlagueMilliAnchor, PlagueNpcMilli}, {"the clerk", "NPC", PlagueClerkAnchor, PlagueNpcClerk},
		{"Bravek", "NPC", PlagueBravekAnchor, PlagueNpcBravek}, {"Elena", "NPC", PlagueElenaAnchor, PlagueNpcElena},
		{"a mourner at the door", "NPC", tile.Tile{X: 2531, Z: 3272}, PlagueNpcMourner},
		{"Aemad", "NPC", AemadStore.Anchor, AemadStore.NpcID}, {"Jatix", "NPC", PlagueJatixShop.Anchor, PlagueNpcJatix}, {"Wydin", "NPC", WydinShop.Anchor, WydinShop.NpcID},
		{"the spade", "OBJ", PlagueSpadeSpawn, ObjSpade}, {"the picture", "OBJ", PlaguePictureSpawn, PlagueObjPicture},
		{"snape grass 1", "OBJ", PlagueGrassSpawns[0], PlagueObjSnapeGrass}, {"snape grass 2", "OBJ", PlagueGrassSpawns[1], PlagueObjSnapeGrass},
		{"snape grass 3", "OBJ", PlagueGrassSpawns[2], PlagueObjSnapeGrass}, {"the bucket by Cedric's cart", "OBJ", CogBucketSpawn, ObjBucket},
		{"the dwellberries", "OBJ", PlagueBerrySpots[0], PlagueObjDwellberries},
		{"the mud patch (south)", "LOC", tile.Tile{X: 2566, Z: 3331}, PlagueLocMudPatch1}, {"the mud patch (middle)", "LOC", tile.Tile{X: 2566, Z: 3332}, PlagueLocMudPatch2},
		{"the mud patch (north)", "LOC", tile.Tile{X: 2566, Z: 3333}, PlagueLocMudPatch1}, {"the mud pile", "LOC", tile.Tile{X: 2561, Z: 9738}, PlagueLocMudPile},
		{"the sewer pipe", "LOC", tile.Tile{X: 2530, Z: 9701}, PlagueLocSewerPipe}, {"the manhole", "LOC", PlagueManhole, PlagueLocManholeShut},
		{"Alrena's cupboard", "LOC", PlagueCupboard, PlagueLocCupboardShut}, {"the Rehnisons' door", "LOC", PlagueRehnisonDoor, PlagueLocRehnisonDoor},
		{"the Rehnisons' stairs", "LOC", PlagueRehnisonStairs, PlagueLocRehnisonStairs}, {"the stairs' top", "LOC", PlagueRehnisonTop, PlagueLocRehnisonTop},
		{"the plague house's door", "LOC", PlagueHouseDoor, PlagueLocHouseDoor}, {"the barrel", "LOC", PlagueBarrel, PlagueLocBarrel},
		{"the house's stairs down", "LOC", PlagueHouseStairs, PlagueLocHouseStairs}, {"the cellar's stairs up", "LOC", PlagueCellarStairs, PlagueLocCellarStairs},
		{"Elena's gate", "LOC", PlagueElenaGate, PlagueLocElenaGate}, {"Bravek's door", "LOC", PlagueBravekDoor, PlagueLocBravekDoor},
		{"the fountain", "LOC", PlagueFountain, PlagueLocFountain},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("no %s (%s %d) at %v", c.what, c.section, c.id, c.at)
		}
	}
	// The five dwellberry spawns, the ten guard dogs of the wood and the four of the market, the cows of the field.
	for _, at := range []tile.Tile{{X: 2633, Z: 3496}, {X: 2638, Z: 3479}, {X: 2645, Z: 3498}, {X: 2664, Z: 3476}, {X: 2667, Z: 3494}} {
		if !csHasRow(csMapRows(t, "OBJ", at), PlagueObjDwellberries) {
			t.Errorf("no dwellberries at %v", at)
		}
	}
	for _, at := range append(append([]tile.Tile(nil), PlagueGuardDogs...), PlagueMarketDogs...) {
		if !csHasRow(csMapRows(t, "NPC", at), PlagueNpcGuardDog) {
			t.Errorf("no guard dog at %v", at)
		}
	}
	if csNearRows(t, "NPC", NpcDairyCow, PlagueCowField, 12) < 6 {
		t.Errorf("fewer than six cows within 12 tiles of %v", PlagueCowField)
	}
	// Each berry spot but the plan's lies in a gap's dogs' reach: the others are at most five tiles from a dog (the notes say why
	// only one is used).
	if got := len(PlagueGuardDogs); got != 10 {
		t.Errorf("%d guard dogs in the wood, the plan says ten", got)
	}
	for _, spot := range PlagueBerrySpots {
		for _, d := range PlagueGuardDogs {
			if tile.Chebyshev(spot, d) < PlagueDogBerth {
				t.Errorf("berry spot %v is %d from the dog at %v", spot, tile.Chebyshev(spot, d), d)
			}
		}
	}
	// The mud patch, the dig's start and the pipe's end are all inside the areas the plan names.
	if PlagueAreaOf(PlagueSewerLanding) != PlagueSewer || PlagueAreaOf(PlagueSewerPipeEnd) != PlagueSewer || PlagueAreaOf(PlagueWestLanding) != PlagueWest ||
		PlagueAreaOf(PlagueCellarLanding) != PlagueCellar || PlagueAreaOf(PlagueMilliAnchor) != PlagueUpstairs || PlagueAreaOf(PlagueGarden) != PlagueEast {
		t.Error("an anchor is in the wrong area")
	}
}

// The foes the plan prices are the content's, and the content's hunt rule is the one the plan relies on.
func TestPlagueContentTheFoes(t *testing.T) {
	npcs := csContent(t, "scripts/_unpack/225/all.npc")
	stat := func(block, key string) int {
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == key {
				n, _ := strconv.Atoi(v)
				return n
			}
		}
		return -1
	}
	for _, c := range []struct {
		name  string
		block string
		foe   MeleeFoe
		hunt  int
		wand  int
		level int
	}{
		{"hobgoblin_unarmed", "hobgoblin_unarmed", FoePlagueHobgoblin, 1, 15, 28},
		{"guarddog", "guarddog", FoePlagueGuardDog, 1, 3, 44},
	} {
		b := csConfigBlock(npcs, c.block)
		if b == "" {
			t.Fatalf("no [%s]", c.block)
		}
		f := c.foe
		if stat(b, "attack") != f.Attack || stat(b, "strength") != f.Strength || stat(b, "defence") != f.Defence || stat(b, "hitpoints") != f.Hitpoints {
			t.Errorf("%s: %d/%d/%d/%d, the model %d/%d/%d/%d", c.name, stat(b, "attack"), stat(b, "strength"), stat(b, "defence"), stat(b, "hitpoints"), f.Attack, f.Strength, f.Defence, f.Hitpoints)
		}
		if strings.Contains(b, "attackrate") || strings.Contains(b, "attackbonus") || strings.Contains(b, "strengthbonus") {
			t.Errorf("%s names an attack rate or bonus the model does not carry: %q", c.name, b)
		}
		if !strings.Contains(b, "huntmode=cowardly") || stat(b, "huntrange") != c.hunt || stat(b, "wanderrange") != c.wand || stat(b, "vislevel") != c.level {
			t.Errorf("%s hunting: %q", c.name, b)
		}
	}
	if PlagueHobgoblinHunts != 2*28 {
		t.Errorf("the hobgoblins hunt up to %d", PlagueHobgoblinHunts)
	}
	// Cowardly hunting leaves a stronger account alone outside the Wilderness (twice the level).
	if b := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "cowardly"); !strings.Contains(b, "check_nottoostrong=outside_wilderness") {
		t.Errorf("cowardly hunt: %q", b)
	}
	// The wood's gaps hold one dog each: 2659,3478 and 2669,3488 are spawns of the list.
	has := func(at tile.Tile) bool {
		for _, d := range PlagueGuardDogs {
			if d == at {
				return true
			}
		}
		return false
	}
	if !has(tile.Tile{X: 2659, Z: 3478}) || !has(tile.Tile{X: 2669, Z: 3488}) {
		t.Error("the wood's two gap dogs are not in the list")
	}
}

// Every npc of the places the quest works that hunts is one the plan knows. The sewer's component, the city, the cellar, Edmond's
// garden and house, the market between the bank and the fountain, Aemad's, the cow field: none hunts but the market's four guard
// dogs (which the water walks round). The wood holds the ten dogs and the trail the hobgoblins, which are what the plan prices.
func TestPlagueContentTheHuntersOfThePlacesTheQuestWorks(t *testing.T) {
	root := contentRoot(t)
	blocks := npcBlocks(t, root)
	names := packNames(t, root+"/pack/npc.pack")
	type box struct {
		name                   string
		minX, maxX, minZ, maxZ int
		level                  int
		known                  map[string]int // hunting type -> how many may stand in it
	}
	boxes := []box{
		{"the sewer under the garden", 2525, 2572, 9701, 9739, 0, nil},
		{"the cellar", 2535, 2543, 9668, 9674, 0, nil},
		{"the Rehnisons' upper floor", 2526, 2534, 3328, 3334, 1, nil},
		{"Edmond's garden and house", 2557, 2582, 3322, 3345, 0, nil},
		{"the road from the garden to the fountain", 2582, 2640, 3300, 3340, 0, map[string]int{"guarddog": 3}},
		{"Aemad's", 2605, 2625, 3288, 3300, 0, nil},
		{"the cow field", 2650, 2680, 3335, 3360, 0, nil},
	}
	// The city holds a graveyard's zombies and ghosts in its west streets (x 2497-2511, z 3283-3294), all of them wandering nine tiles or
	// fewer and noticing a player one tile off, so ten tiles; the quest goes no nearer than fifteen.
	west := []tile.Tile{PlagueWestLanding, PlagueJethickAnchor, PlagueRehnisonDoor, PlagueTedAnchor, PlagueHouseDoor, PlagueClerkAnchor, PlagueBravekAnchor,
		PlagueManhole, PlagueBarrel}
	hunters := plagueHuntersIn(t, root, blocks, names, 2433, 2556, 3266, 3334, 0)
	if len(hunters) < 5 {
		t.Errorf("only %d hunters in the city: the graveyard moved", len(hunters))
	}
	for _, h := range hunters {
		for _, a := range west {
			if d := tile.Chebyshev(h.at, a); d < 15 {
				t.Errorf("%s (huntmode %s) at %v is only %d from %v, a place the quest goes", h.name, h.mode, h.at, d, a)
			}
		}
	}
	for _, b := range boxes {
		seen := map[string]int{}
		for mx := b.minX >> 6; mx <= b.maxX>>6; mx++ {
			for mz := b.minZ >> 6; mz <= b.maxZ>>6; mz++ {
				src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", mx, mz))
				in := false
				for _, line := range strings.Split(src, "\n") {
					if strings.HasPrefix(line, "==== ") {
						in = line == "==== NPC ===="
						continue
					}
					head, tail, ok := strings.Cut(strings.TrimSpace(line), ":")
					if !in || !ok {
						continue
					}
					id, err := strconv.Atoi(strings.TrimSpace(tail))
					if err != nil {
						continue
					}
					var lvl, lx, lz int
					if n, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); n != 3 {
						continue
					}
					x, z := mx*64+lx, mz*64+lz
					if lvl != b.level || x < b.minX || x > b.maxX || z < b.minZ || z > b.maxZ {
						continue
					}
					if mode := blocks[names[id]]["huntmode"]; mode != "" && mode != "none" {
						seen[names[id]]++
					}
				}
			}
		}
		for name, n := range seen {
			if b.known[name] < n {
				t.Errorf("%s holds %d x %s that hunts (the plan allows %d): a hunter the module's walks were not written for", b.name, n, name, b.known[name])
			}
		}
	}
}

type plagueHunter struct {
	name, mode string
	at         tile.Tile
}

// plagueHuntersIn lists the hunting npc spawns of a box on one level.
func plagueHuntersIn(t *testing.T, root string, blocks map[string]map[string]string, names map[int]string, minX, maxX, minZ, maxZ, level int) []plagueHunter {
	t.Helper()
	var out []plagueHunter
	for mx := minX >> 6; mx <= maxX>>6; mx++ {
		for mz := minZ >> 6; mz <= maxZ>>6; mz++ {
			in := false
			for _, line := range strings.Split(csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", mx, mz)), "\n") {
				if strings.HasPrefix(line, "==== ") {
					in = line == "==== NPC ===="
					continue
				}
				head, tail, ok := strings.Cut(strings.TrimSpace(line), ":")
				if !in || !ok {
					continue
				}
				id, err := strconv.Atoi(strings.TrimSpace(tail))
				if err != nil {
					continue
				}
				var lvl, lx, lz int
				if n, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); n != 3 {
					continue
				}
				x, z := mx*64+lx, mz*64+lz
				if lvl != level || x < minX || x > maxX || z < minZ || z > maxZ {
					continue
				}
				if mode := blocks[names[id]]["huntmode"]; mode != "" && mode != "none" {
					out = append(out, plagueHunter{names[id], mode, tile.Tile{X: x, Z: z, Level: lvl}})
				}
			}
		}
	}
	return out
}
