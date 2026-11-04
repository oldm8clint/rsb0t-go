package questplan

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward, rule and foe Biohazard rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were taken from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run BiohazardContent ./internal/scripts/questplan/
//
// It uses the cook/sheep content test's readers (cooksheep_content_test.go), the Ernest test's scriptBlock and hasAll.

const (
	bioDir     = "scripts/quests/quest_biohazard/"
	bioScripts = bioDir + "scripts/"
	bioConfigs = bioDir + "configs/"
	bioWestDir = "scripts/areas/area_ardougne_west/"
	bioEastDir = "scripts/areas/area_ardougne_east/scripts/"
)

// bioConsts reads the quest's constants (quest_biohazard.constant and the two in general/configs/quest.constant).
func bioConsts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	re := regexp.MustCompile(`^\^(\w+)\s*=\s*(\d+)\s*$`)
	for _, f := range []string{bioConfigs + "quest_biohazard.constant", "scripts/general/configs/quest.constant"} {
		for _, line := range strings.Split(csContent(t, f), "\n") {
			if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				n, _ := strconv.Atoi(m[2])
				out[m[1]] = n
			}
		}
	}
	return out
}

// bioRenderJournal runs biohazard_journal.rs2 for one value of %biohazard the way the script reads: nested ifs and else-ifs over
// `%biohazard OP ^constant` and `inv_total(inv, ITEM) > 0` conditions, each active `$text =` or `append` adding its string, and the
// first ~quest_journal call that is reached ending the script. held names the items the pack holds. The result is the text the client
// would split at `|`.
func bioRenderJournal(t *testing.T, src string, stage int, held map[string]bool, consts map[string]int) string {
	t.Helper()
	type frame struct{ parent, taken, active bool }
	var stack []frame
	active := func() bool { return len(stack) == 0 || stack[len(stack)-1].active }
	stageRe := regexp.MustCompile(`%biohazard\s*(>=|<=|!=|=|>|<)\s*\^(\w+)`)
	invRe := regexp.MustCompile(`inv_total\(inv, (\w+)\)\s*>\s*0`)
	eval := func(cond string) bool {
		if m := invRe.FindStringSubmatch(cond); m != nil {
			return held[m[1]]
		}
		m := stageRe.FindStringSubmatch(cond)
		if m == nil {
			t.Fatalf("a journal condition the renderer does not read: %q", cond)
		}
		v, ok := consts[m[2]]
		if !ok {
			t.Fatalf("constant ^%s not found", m[2])
		}
		switch m[1] {
		case "=":
			return stage == v
		case ">=":
			return stage >= v
		case ">":
			return stage > v
		case "<":
			return stage < v
		case "<=":
			return stage <= v
		case "!=":
			return stage != v
		}
		return false
	}
	strRe := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	text := ""
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "//") || line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "def_string"):
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

// bioMenus reads every menu a script offers: the quoted option texts of each @multiN(...) and ~p_choiceN(...) call.
func bioMenus(src string) [][]string {
	call := regexp.MustCompile(`(?:@multi\d|~p_choice\d)\((.*)\)`)
	str := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	var out [][]string
	for _, line := range strings.Split(src, "\n") {
		if m := call.FindStringSubmatch(line); m != nil {
			var opts []string
			for _, o := range str.FindAllStringSubmatch(m[1], -1) {
				opts = append(opts, o[1])
			}
			if len(opts) > 1 {
				out = append(out, opts)
			}
		}
	}
	return out
}

func TestBiohazardContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "elena2", BioNpcElena}, {"npc", npc, "jerico", BioNpcJerico}, {"npc", npc, "omart", BioNpcOmart}, {"npc", npc, "kilron", BioNpcKilron},
		{"npc", npc, "chemist", BioNpcChemist}, {"npc", npc, "drunk1", BioNpcHopsRim}, {"npc", npc, "drunk2", BioNpcHopsVar}, {"npc", npc, "artist1", BioNpcDeVinciRim},
		{"npc", npc, "artist2", BioNpcDeVinciVar}, {"npc", npc, "gambler1", BioNpcChancyRim}, {"npc", npc, "gambler2", BioNpcChancyVar},
		{"npc", npc, "guidor", BioNpcGuidor}, {"npc", npc, "guidors_wife", BioNpcGuidorsWife}, {"npc", npc, "bioguard1", BioNpcGuard},
		{"npc", npc, "thessalia", BioNpcThessalia}, {"npc", npc, "kinglathas", BioNpcLathas}, {"npc", npc, "mournerstew2", NpcSickMourner},
		{"npc", npc, "mournerstew3", BioNpcMournerDown}, {"npc", npc, "mournerstew1", BioNpcMournerLow}, {"npc", npc, "mourner_armed", BioNpcMournerArm},
		{"npc", npc, "zombie_unarmed", BioNpcZombie}, {"npc", npc, "zombie_armed", BioNpcZombieArmed}, {"npc", npc, "ghost", BioNpcGhost},
		{"obj", obj, "ethenea", BioObjEthenea}, {"obj", obj, "liquid_honey", BioObjLiquidHoney}, {"obj", obj, "sulphuric_broline", BioObjBroline},
		{"obj", obj, "plaguesample", BioObjSample}, {"obj", obj, "touch_paper", BioObjTouchPaper}, {"obj", obj, "distillator", BioObjDistillator},
		{"obj", obj, "birdfeed", BioObjBirdfeed}, {"obj", obj, "mournerkeytw", BioObjKey}, {"obj", obj, "pigeons", BioObjPigeons},
		{"obj", obj, "pigeoncage", BioObjEmptyCage}, {"obj", obj, "priest_gown", BioObjPriestGown}, {"obj", obj, "priest_robe", BioObjPriestRobe},
		{"obj", obj, "doctor_gown", BioObjDoctorGown}, {"obj", obj, "rottenapples", BioObjRottenApples}, {"obj", obj, "coins", CoinsID},
		{"obj", obj, "shark", PlagueObjShark},
		{"loc", loc, "jericoscupboardshut", BioLocJericoCupboardShut}, {"loc", loc, "jericoscupboardopen", BioLocJericoCupboardOpen},
		{"loc", loc, "biowatchtower", BioLocWatchtower}, {"loc", loc, "mournercauldron", BioLocCauldron},
		{"loc", loc, "bionursescupboardshut", BioLocNurseCupboardShut}, {"loc", loc, "bionursescupboardopen", BioLocNurseCupboardOpen},
		{"loc", loc, "mournerstewdoor", BioLocHQDoor}, {"loc", loc, "mournerstewdoorup", BioLocHQUpDoor}, {"loc", loc, "mournerquaters_gater", BioLocHQGateRight},
		{"loc", loc, "mournerquaters_gatel", BioLocHQGateLeft}, {"loc", loc, "mournercrateup", BioLocHQCrate}, {"loc", loc, "spiralstairs", BioLocHQStairs},
		{"loc", loc, "mournerstewfence", BioLocYardFence}, {"loc", loc, "guidorgatelclosed", BioLocVarrockGateA}, {"loc", loc, "guidorgaterclosed", BioLocVarrockGateB},
		{"loc", loc, "guidordoor", BioLocGuidorDoor},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s: the pack says %d (present %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

func TestBiohazardContentStagesAndRewards(t *testing.T) {
	consts := bioConsts(t)
	for name, want := range map[string]int{
		"biohazard_not_started": BioNotStarted, "biohazard_started": BioStarted, "biohazard_spoken_jerico": BioSpokenJerico,
		"biohazard_used_birdfeed": BioUsedBirdfeed, "biohazard_released_pigeons": BioReleasedPigeons, "biohazard_climbed_ladder": BioClimbedLadder,
		"biohazard_poisoned_stew": BioPoisonedStew, "biohazard_found_distillator": BioFoundDistillator, "biohazard_given_distillator": BioGivenDistillator,
		"biohazard_spoken_chemist": BioSpokenChemist, "biohazard_found_secret": BioFoundSecret, "biohazard_reported_elena": BioReportedElena,
		"biohazard_complete": BioComplete, "biohazard_questpoints": 3,
	} {
		if consts[name] != want {
			t.Errorf("^%s is %d in the content, the plan says %d", name, consts[name], want)
		}
	}
	if BiohazardRecord.QuestPoints != consts["biohazard_questpoints"] || LadderFacts[BiohazardID].XP["thieving"] != 1250 || LadderFacts[BiohazardID].QP != 3 {
		t.Errorf("record %+v ladder row %+v: 12,500 Thieving experience at 1x is 1,250 and the quest is worth 3 points", BiohazardRecord, LadderFacts[BiohazardID])
	}
	src := csContent(t, bioScripts+"quest_biohazard.rs2")
	hasAll(t, "quest_biohazard.rs2", src, "[queue,quest_biohazard_complete]", "stat_advance(thieving, 12500);", "%biohazard = ^biohazard_complete;")
	// The varp is perm and not transmitted: the tab's colour and the journal are the only readers.
	if v := csContent(t, bioConfigs+"quest_biohazard.varp"); !strings.Contains(csConfigBlock(v, "biohazard"), "scope=perm") || strings.Contains(csConfigBlock(v, "biohazard"), "transmit") {
		t.Errorf("%%biohazard: %q", csConfigBlock(v, "biohazard"))
	}
	// The three errand varps' bits are per player.
	for _, name := range []string{"bioerrand"} {
		if !strings.Contains(csConfigBlock(csContent(t, bioConfigs+"quest_biohazard.varp"), name), "scope=perm") {
			t.Errorf("%%%s is not perm", name)
		}
	}
	// Plague City is the prerequisite the plan keeps, and its tab is "Plague City".
	if len(BiohazardRecord.Requirements.Quests) != 1 || BiohazardRecord.Requirements.Quests[0] != PlagueCityID {
		t.Errorf("record requirements %+v", BiohazardRecord.Requirements)
	}
	// The pigeon box is the two corners opheld1,pigeons names: 0_39_51_63_35 and 0_40_51_5_43.
	if BioPigeonZone != (Rect{MinX: 39*64 + 63, MaxX: 40*64 + 5, MinZ: 51*64 + 35, MaxZ: 51*64 + 43}) {
		t.Errorf("the pigeon box is %+v", BioPigeonZone)
	}
	hasAll(t, "quest_biohazard.rs2", src, "inzone(0_39_51_63_35, 0_40_51_5_43, coord) = true")
	// The ladder lands: Omart's script teleports to 0_39_51_60_3 and Kilron's to 0_39_51_63_3.
	if BioWestLanding != (tile.Tile{X: 39*64 + 60, Z: 51*64 + 3}) || BioEastLanding != (tile.Tile{X: 39*64 + 63, Z: 51*64 + 3}) {
		t.Errorf("the landings are %v and %v", BioWestLanding, BioEastLanding)
	}
	omart, kilron := csContent(t, bioWestDir+"scripts/omart.rs2"), csContent(t, bioWestDir+"scripts/kilron.rs2")
	hasAll(t, "omart.rs2", omart, "@biohazard_climb_ladder(0_39_51_60_3);")
	hasAll(t, "kilron.rs2", kilron, "@biohazard_climb_ladder(0_39_51_63_3);", "if(%biohazard >= ^biohazard_climbed_ladder) {")
}

// The journal script, run for every stage, parses to that stage, and no two stages' texts are alike.
func TestBiohazardContentJournalRendersToTheStageTheParserReads(t *testing.T) {
	consts := bioConsts(t)
	src := csContent(t, bioScripts+"biohazard_journal.rs2")
	seen := map[string]int{}
	for _, stage := range []int{0, 1, 2, 3, 4, 5, 6, 7, 10, 12, 14, 15, 16} {
		for _, held := range []map[string]bool{{}, {"distillator": true, "plaguesample": true, "liquid_honey": true, "sulphuric_broline": true, "ethenea": true}} {
			text := bioRenderJournal(t, src, stage, held, consts)
			if text == "" {
				t.Errorf("stage %d rendered nothing", stage)
				continue
			}
			got, ok := ParseBiohazardJournal(strings.Split(text, "|"))
			if !ok || got != stage {
				t.Errorf("stage %d (%q) parsed %d (ok %v)", stage, text, got, ok)
			}
			// A journal that lists what the pack holds (stage 10, 7) is the same stage either way; stages differ by text.
			if len(held) == 0 {
				if other, dup := seen[text]; dup {
					t.Errorf("stages %d and %d render the same text", other, stage)
				}
				seen[text] = stage
			}
		}
	}
	// The tab's complete state ends the quest: only the last stage's text says QUEST COMPLETE.
	for _, stage := range []int{0, 1, 2, 7, 10, 12, 14, 15} {
		if strings.Contains(strings.ToLower(bioRenderJournal(t, src, stage, nil, consts)), "quest complete") {
			t.Errorf("stage %d's journal says QUEST COMPLETE", stage)
		}
	}
	// The needles really are in the text of their own stage and absent from every earlier stage's, so the order is only a tie-break.
	for _, n := range bioJournalNeedles {
		for _, stage := range []int{0, 1, 2, 3, 4, 5, 6, 7, 10, 12, 14, 15, 16} {
			has := strings.Contains(NormaliseLines(strings.Split(bioRenderJournal(t, src, stage, nil, consts), "|")), n.needle)
			if stage < n.stage && has {
				t.Errorf("needle %q (stage %d) is in the text of the earlier stage %d", n.needle, n.stage, stage)
			}
			if stage == n.stage && !has {
				t.Errorf("needle %q is not in the text of its own stage %d", n.needle, n.stage)
			}
		}
	}
}

// Every menu option the plan's stops choose is in the content, and the preferences pick no other option of any menu of the file.
func TestBiohazardContentDialogue(t *testing.T) {
	check := func(file string, stop NpcStop, intended ...string) {
		t.Helper()
		src := csContent(t, file)
		last := -1
		for _, line := range stop.Prefer {
			i := strings.Index(src, line)
			if i < 0 {
				t.Errorf("%s (%s) has no option %q", file, stop.Npc, line)
				continue
			}
			if i < last {
				t.Errorf("%s: option %q comes before the previous preference", file, line)
			}
			last = i
		}
		// The options the preferences choose, over every menu of the file, are the intended ones and no others.
		picked := map[string]bool{}
		for _, menu := range bioMenus(src) {
			if pick, ok := PickPreferred(menu, stop.Prefer); ok {
				picked[pick] = true
			}
		}
		if len(picked) != len(intended) {
			t.Errorf("%s (%s): the preferences choose %v, want only the options containing %v", file, stop.Npc, picked, intended)
		}
		for _, want := range intended {
			found := false
			for p := range picked {
				if strings.Contains(p, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s (%s): no menu offers an option containing %q to the preferences %v", file, stop.Npc, want, stop.Prefer)
			}
		}
	}
	check(bioEastDir+"elena.rs2", BioElenaStart, "I'll try to retrieve it for you.")
	check(bioEastDir+"elena.rs2", BioElenaReplace, "I'm afraid I've lost some of the stuff")
	check(bioWestDir+"scripts/omart.rs2", BioOmartStop, "Ok, lets do it.")
	check(bioWestDir+"scripts/kilron.rs2", BioKilronStop, "Yes I do.")
	check(bioWestDir+"scripts/mourner.rs2", BioMournerStop, "There's nothing I can do, it's fatal.")
	check(bioScripts+"chemist.rs2", BioChemistStop, "It's ok, I'm Elena's friend.", "I just need some touch paper for a guy called Guidor.")
	check(bioScripts+"guidor.rs2", BioGuidorStop, "I've come to ask your assistance in stopping a plague.", "I've been sent by your old pupil Elena.",
		"So what does that mean exactly?")
	check(bioEastDir+"king_lathas.rs2", BioLathasStop, "I don't understand...")
	for _, er := range BioErrands {
		file := bioScripts + map[string]string{"Hops": "hops.rs2", "DeVinci": "devinci.rs2", "Chancy": "chancy.rs2"}[er.Name]
		check(file, er.Rim, "You give him the "+er.Give+"...")
	}
	// The Varrock side's only menu is a spoilt vial's (Chancy's and DeVinci's; Hops has none), and the collect preferences are the two lines
	// that are in those files, both harmless.
	for _, line := range bioCollectPrefer {
		in := 0
		for _, f := range []string{"chancy.rs2", "devinci.rs2"} {
			if strings.Contains(csContent(t, bioScripts+f), `"`+line+`"`) {
				in++
			}
		}
		if in != 1 {
			t.Errorf("the collect option %q is in %d of Chancy's and DeVinci's scripts, want one", line, in)
		}
	}
	// The pairing is the correct one in the content: Hops the broline, Chancy the honey, DeVinci the ethenea.
	for name, want := range map[string][2]string{
		"hops.rs2": {"hops_sulphuricbroline", "^hops_correct"}, "chancy.rs2": {"chancy_liquidhoney", "^chancy_correct"}, "devinci.rs2": {"devinci_ethenea", "^devinci_correct"}} {
		src := csContent(t, bioScripts+name)
		block := scriptBlock(src, "label,"+want[0])
		if !strings.Contains(block, want[1]) {
			t.Errorf("%s: the correct pairing is not %s", name, want[0])
		}
	}
	// Elena at stage 0 has the two-line menu, Omart and Kilron their two, the sick mourner his three, the Chemist his two in a row.
	elena := csContent(t, bioEastDir+"elena.rs2")
	hasAll(t, "elena.rs2", elena, `switch_int(~p_choice2("I'll try to retrieve it for you.", 1, "Well, good luck.", 2))`,
		`switch_int(~p_choice3("I just find it hard to say goodbye sometimes.", 1, "I'm afraid I've lost some of the stuff that you gave me...", 2, "I've forgotten what I need to do.", 3))`)
	mourner := csContent(t, bioWestDir+"scripts/mourner.rs2")
	hasAll(t, "mourner.rs2", mourner, `~p_choice3("Just hold your breath and count to ten.", 1, "The best I can do is pray for you.", 2, "There's nothing I can do, it's fatal.", 3)`)
	chem := csContent(t, bioScripts+"chemist.rs2")
	hasAll(t, "chemist.rs2", chem, `~p_choice2("This can't wait, I'm carrying a plague sample.", 1, "It's ok, I'm Elena's friend.", 2)`,
		`@multi2("I need some more touch paper for this plague sample.", chemist_touchpaperplague, "I just need some touch paper for a guy called Guidor.", chemist_touchpaperguidor);`)
}

// The rules the plan decides by, as the scripts say them.
func TestBiohazardContentRules(t *testing.T) {
	main := csContent(t, bioScripts+"quest_biohazard.rs2")
	// The bird feed: Jerico's cupboard, only from stage 2, one at a time, and the watch tower takes it only at stage 2.
	hasAll(t, "quest_biohazard.rs2", main,
		"if(%biohazard = ^biohazard_spoken_jerico & last_useitem = birdfeed) {", "%biohazard = ^biohazard_used_birdfeed;",
		"if(inv_total(inv, birdfeed) > 0 | %biohazard >= ^biohazard_used_birdfeed) {", "if(inv_total(bank, birdfeed) > 0) {", "inv_add(inv, birdfeed, 1);",
		"if(%biohazard = ^biohazard_used_birdfeed & inzone(0_39_51_63_35, 0_40_51_5_43, coord) = true) {", "inv_add(inv, pigeoncage, 1);",
		"if(%biohazard = ^biohazard_released_pigeons) %biohazard = ^biohazard_climbed_ladder;")
	// The cauldron deletes the apples whatever the stage and moves the stage only from stage 5.
	hasAll(t, "quest_biohazard.rs2", main, "if(last_useitem = rottenapples) {", "inv_del(inv, rottenapples, 1);", "if(%biohazard = ^biohazard_climbed_ladder) {",
		"%biohazard = ^biohazard_poisoned_stew;")
	// The nurse's cupboard: the gown from stage 6 on, only while none is owned anywhere.
	hasAll(t, "quest_biohazard.rs2", main, "if(~obj_gettotal(doctor_gown) = 0 & %biohazard >= ^biohazard_poisoned_stew) {", "inv_add(inv, doctor_gown, 1);")
	// The gate: locked to an account without the key on the entering side, its key on a leaf opens it; the crate gives the distillator.
	hasAll(t, "quest_biohazard.rs2", main, `if($entering = true & inv_total(inv, mournerkeytw) = 0 & $used_key = false) { // cant enter with oploc1`, `mes("The gate is locked.");`,
		"[oplocu,mournerquaters_gatel]", "if(last_useitem = mournerkeytw) @open_mournerhq_gate(^left, true);", "[oplocu,mournerquaters_gater]",
		"~open_and_close_double_door3($entering, $loc_type, $loc_shape, $loc_angle, $loc_coord, $side, grate_open);",
		"if(~obj_gettotal(distillator) > 0 | %biohazard > ^biohazard_found_distillator) {", "if(%biohazard = ^biohazard_poisoned_stew) %biohazard = ^biohazard_found_distillator;",
		"inv_add(inv, distillator, 1);")
	// The sick mourner: talks only at stage 6 to an account wearing the gown, turns on it whatever it answers, drops the key to the account
	// he is aggressive to; the key comes only when the pack holds none.
	mourner := csContent(t, bioWestDir+"scripts/mourner.rs2")
	sick := scriptBlock(mourner, "opnpc1,mournerstew2")
	hasAll(t, "mourner.rs2 [opnpc1,mournerstew2]", sick, "if(%biohazard = ^biohazard_poisoned_stew & inv_total(worn, doctor_gown) > 0) {",
		`mes("The mourner is sick.");`, `mes("He doesn't feel like talking.");`)
	if n := strings.Count(sick, "~npc_retaliate(0);"); n != 3 {
		t.Errorf("the sick mourner turns on the account %d times, once in each of the three answers", n)
	}
	hasAll(t, "mourner.rs2", mourner, "[ai_queue3,mournerstew2]", "if(finduid(%npc_aggressive_player) = true) {", "queue(defeat_biohazard_mourner, 0, 0);",
		"[queue,defeat_biohazard_mourner]", "if(inv_total(inv, mournerkeytw) = 0) {", "inv_add(inv, mournerkeytw, 1);")
	// The dinner downstairs talks to a false doctor angrily and sets the mourner on it: the plan never talks to them.
	hasAll(t, "mourner.rs2", scriptBlock(mourner, "opnpc1,mournerstew3"), "if(inv_total(worn, doctor_gown) = 0) {", "~npc_retaliate(0);")
	// The doors: the headquarters opens for a doctor at stages 6 and 7 only; the door upstairs refuses an account without the gown.
	doors := csContent(t, bioWestDir+"scripts/doors.rs2")
	hasAll(t, "doors.rs2", doors, "case ^biohazard_poisoned_stew, ^biohazard_found_distillator :", "if(inv_total(worn, doctor_gown) > 0) {", `"<p,neutral>In you go doc."`,
		"[oploc1,mournerstewdoorup]", "if(~check_axis(coord, loc_coord, loc_angle) = false & inv_total(worn, doctor_gown) = 0) {", `mes("The mourner is refusing to open the door.");`,
		"[oploc1,mournerstewdoor]", "if(~check_axis(coord, loc_coord, loc_angle) = false) {", "@west_ardougne_mourner_headquarters_doors;",
		"if(%biohazard = ^biohazard_complete) {", "...But they will not open.")
	// The doors open and move the account: entering teleports to the loc's tile and across, leaving to the loc's tile.
	opens := csContent(t, "scripts/doors/scripts/open_and_close_doors.rs2")
	hasAll(t, "open_and_close_doors.rs2", scriptBlock(opens, "proc,open_and_close_door2"), "if ($entering = true) {", "p_teleport($loc_coord);", "$dest = movecoord(loc_coord, $x, 0, $z);", "p_teleport($dest);")
	// check_axis: true on the loc's own column (east and west doors) or row (north and south ones).
	axis := scriptBlock(csContent(t, "scripts/doors/scripts/door_procs.rs2"), "proc,check_axis")
	hasAll(t, "door_procs.rs2", axis, "case ^loc_north, ^loc_south :", "if (coordz($coord) = coordz($loc_coord)) {", "case ^loc_west, ^loc_east :", "if (coordx($coord) = coordx($loc_coord)) {")
	// Elena: the distillator is taken, then each of the four items added only with two slots free; the lost-items option adds what the
	// pack lacks; she repeats the errand at stage 12 too.
	elena := csContent(t, bioEastDir+"elena.rs2")
	hasAll(t, "elena.rs2", elena, "case ^biohazard_found_distillator :", "if(inv_total(inv, distillator) = 0) {", "inv_del(inv, distillator, 1);", "%biohazard = ^biohazard_given_distillator;",
		"case ^biohazard_given_distillator, ^biohazard_spoken_chemist :", "if(inv_total(inv, liquid_honey) = 0) {", "if(inv_total(inv, plaguesample) = 0) {",
		"case ^biohazard_found_secret :", "%biohazard = ^biohazard_reported_elena;")
	if n := strings.Count(elena, "if(inv_freespace(inv) > 1) {"); n != 4 {
		t.Errorf("Elena skips an add unless two slots are free %d times, want four (one for each of the three vials and the sample)", n)
	}
	if BioHandbackSlots < 5+1 {
		t.Errorf("the hand-back wants %d free slots, the four adds need five once the distillator has gone", BioHandbackSlots)
	}
	// The Chemist: he keeps the sample from the wrong menu line, gives the paper for Guidor and moves the stage; at stage 12 he gives another
	// sheet when none is owned anywhere.
	chem := csContent(t, bioScripts+"chemist.rs2")
	hasAll(t, "chemist.rs2", chem, "case ^biohazard_spoken_chemist, ^biohazard_found_secret :", "if(~obj_gettotal(touch_paper) = 0) {", "inv_del(inv, plaguesample, 1);",
		"%biohazard = ^biohazard_spoken_chemist;", "inv_add(inv, touch_paper, 1);", "if(inv_total(inv, plaguesample) = 0) {")
	// The gate: the search applies from the sample (stage 10) until the secret (stage 14), takes every vial and needs the guard within two
	// tiles; a player on the gate's own column is not searched.
	gate := csContent(t, "scripts/areas/area_varrock/scripts/east_gate.rs2")
	hasAll(t, "east_gate.rs2", gate, "%biohazard >= ^biohazard_given_distillator & %biohazard < ^biohazard_found_secret & npc_find(coord, bioguard1, 2, 0) = true",
		"inv_del(inv, ethenea, 1);", "inv_del(inv, liquid_honey, 1);", "inv_del(inv, sulphuric_broline, 1);", "~forcemove(0_50_53_63_13);",
		"p_teleport(movecoord(coord, 1, 0, 0));", "p_teleport(loc_coord);", "p_teleport(movecoord(loc_coord, -1, 0, 0));",
		"if(~check_axis(coord, loc_coord, loc_angle) = false) {")
	if BioGateOutside != (tile.Tile{X: 50*64 + 63, Z: 3406}) || BioGateInside.X != 3265 {
		t.Errorf("the gate's two sides are %v and %v", BioGateOutside, BioGateInside)
	}
	// Guidor's door lets in a priest (both halves worn) from stage 12; his wife's talk is a chat that cannot be eaten in.
	hasAll(t, "quest_biohazard.rs2", main, "[oploc1,guidordoor]", "if(map_members = ^false | %biohazard < ^biohazard_spoken_chemist) {",
		"if(inv_total(worn, priest_gown) > 0 & inv_total(worn, priest_robe) > 0) {", "mes(\"Guidor's wife allows you to go in.\");")
	hasAll(t, "guidor.rs2", csContent(t, bioScripts+"guidor.rs2"), "inv_del(inv, plaguesample, 1);", "inv_del(inv, touch_paper, 1);", "%biohazard = ^biohazard_found_secret;")
	hasAll(t, "guidor.rs2", csContent(t, bioScripts+"guidor.rs2"), "if(inv_total(inv, sulphuric_broline) = 0 | inv_total(inv, liquid_honey) = 0 | inv_total(inv, ethenea) = 0) {",
		"if(inv_total(inv, touch_paper) = 0) {")
	// Lathas: the first answer queues the completion, the second does not.
	lathas := csContent(t, bioEastDir+"king_lathas.rs2")
	hasAll(t, "king_lathas.rs2", lathas, "if(%biohazard = ^biohazard_reported_elena) {", "queue(quest_biohazard_complete, 0, 0);")
	if n := strings.Count(lathas, "queue(quest_biohazard_complete, 0, 0);"); n != 1 {
		t.Errorf("King Lathas queues the completion %d times, want once (the first answer)", n)
	}
	// The items' properties the plan keys on: the quest items cannot be traded, the priest's suit can, the cage and the apples have ops.
	objs := csContent(t, bioConfigs+"quest_biohazard.obj")
	for _, name := range []string{"ethenea", "liquid_honey", "sulphuric_broline", "plaguesample", "touch_paper", "distillator", "birdfeed", "mournerkeytw", "pigeons", "pigeoncage", "doctor_gown"} {
		if !strings.Contains(csConfigBlock(objs, name), "tradeable=no") {
			t.Errorf("%s is tradeable", name)
		}
	}
	for _, name := range []string{"priest_gown", "priest_robe"} {
		b := csConfigBlock(objs, name)
		if strings.Contains(b, "tradeable=no") || !strings.Contains(b, "iop2=Wear") {
			t.Errorf("%s: %q", name, b)
		}
	}
	hasAll(t, "pigeons", csConfigBlock(objs, "pigeons"), "iop1=Open")
	hasAll(t, "rotten apples", csConfigBlock(csContent(t, "scripts/skill_cooking/configs/cooking_generic/cooking.obj"), "rottenapples"), "iop1=Eat")
	// The shop: Thessalia's clothes shop keeps three of each half of the suit at five coins each, back after a hundred ticks.
	inv := csContent(t, "scripts/areas/area_varrock/configs/varrock.inv")
	shop := csConfigBlock(inv, "clotheshop")
	hasAll(t, "clotheshop", shop, "restock=yes", "stock11=priest_robe,3,100", "stock12=priest_gown,3,100")
	hasAll(t, "thessalia", csConfigBlock(csContent(t, "scripts/areas/area_varrock/configs/varrock.npc"), "thessalia"), "owned_shop,clotheshop", "op3=Trade")
	hasAll(t, "obj", csConfigBlock(csContent(t, bioConfigs+"quest_biohazard.obj"), "priest_gown"), "cost=5")
	hasAll(t, "obj", csConfigBlock(csContent(t, bioConfigs+"quest_biohazard.obj"), "priest_robe"), "cost=5")
}

// The spawns the plan stands on or looks for are the maps'.
func TestBiohazardContentSpawns(t *testing.T) {
	for _, c := range []struct {
		what string
		at   tile.Tile
		id   int
	}{
		{"Elena", BioElenaAnchor, BioNpcElena}, {"Jerico", BioJericoAnchor, BioNpcJerico}, {"Omart", BioOmartAnchor, BioNpcOmart}, {"Kilron", BioKilronAnchor, BioNpcKilron},
		{"the Chemist", BioChemist, BioNpcChemist}, {"Hops", BioHopsRim, BioNpcHopsRim}, {"DeVinci", BioDeVinciRim, BioNpcDeVinciRim}, {"Chancy", BioChancyRim, BioNpcChancyRim},
		{"Thessalia", BioThessalia, BioNpcThessalia}, {"the gate guard", tile.Tile{X: 3263, Z: 3407}, BioNpcGuard}, {"Hops at the inn", BioHopsVar, BioNpcHopsVar},
		{"Chancy at the inn", BioChancyVar, BioNpcChancyVar}, {"DeVinci at the inn", BioDeVinciVar, BioNpcDeVinciVar}, {"Guidor", BioGuidor, BioNpcGuidor},
		{"Guidor's wife", tile.Tile{X: 3281, Z: 3383}, BioNpcGuidorsWife}, {"King Lathas", BioLathas, BioNpcLathas},
		{"the first sick mourner", tile.Tile{X: 2551, Z: 3324, Level: 1}, NpcSickMourner}, {"the second sick mourner", tile.Tile{X: 2551, Z: 3327, Level: 1}, NpcSickMourner},
	} {
		if !csHasRow(csMapRows(t, "NPC", c.at), c.id) {
			t.Errorf("%s: no npc %d at %v", c.what, c.id, c.at)
		}
	}
	// Both sick mourners stand west of the double gate (x 2551, the gate on the east edge) and the crate is east of it.
	if BioSickRoom.X >= BioGate.X+1 || BioCrate.X <= BioGate.X {
		t.Errorf("the sick room %v, the gate %v and the crate %v are not in that order", BioSickRoom, BioGate, BioCrate)
	}
	// The pigeon cages, the apples.
	for _, s := range BioPigeonSpawns {
		if !csHasRow(csMapRows(t, "OBJ", s), BioObjPigeons) {
			t.Errorf("no full pigeon cage at %v", s)
		}
	}
	if !csHasRow(csMapRows(t, "OBJ", BioApples), BioObjRottenApples) {
		t.Errorf("no rotten apples at %v", BioApples)
	}
	// The locs.
	for _, c := range []struct {
		what string
		at   tile.Tile
		id   int
	}{
		{"Jerico's cupboard", BioJericoCupboard, BioLocJericoCupboardShut}, {"the nurse's cupboard", BioNurseCupboard, BioLocNurseCupboardShut},
		{"the cauldron", BioCauldron, BioLocCauldron}, {"the yard's fence", tile.Tile{X: 2541, Z: 3331}, BioLocYardFence},
		{"the street door", BioHQDoorOutside, BioLocHQDoor}, {"the back door", tile.Tile{X: 2551, Z: 3328}, BioLocHQDoor},
		{"the door upstairs", tile.Tile{X: 2547, Z: 3325, Level: 1}, BioLocHQUpDoor}, {"the left gate", BioGate, BioLocHQGateLeft},
		{"the right gate", tile.Tile{X: 2551, Z: 3325, Level: 1}, BioLocHQGateRight}, {"the crate", BioCrate, BioLocHQCrate},
		{"the stairs up", BioHQStairs, BioLocHQStairs}, {"Varrock's gate, south leaf", tile.Tile{X: 3264, Z: 3405}, BioLocVarrockGateA},
		{"Varrock's gate, north leaf", tile.Tile{X: 3264, Z: 3406}, BioLocVarrockGateB}, {"Guidor's door", BioGuidorDoor, BioLocGuidorDoor},
	} {
		if !csHasRow(csMapRows(t, "LOC", c.at), c.id) {
			t.Errorf("%s: no loc %d at %v", c.what, c.id, c.at)
		}
	}
	// The watch tower is nine wall pieces: the plan stands at its east wall (x 2563).
	if n := csNearRows(t, "LOC", BioLocWatchtower, BioWatchtower, 2); n < 3 {
		t.Errorf("%d watch tower wall pieces within two tiles of %v", n, BioWatchtower)
	}
	// The sick mourners' neighbours: the dinner downstairs is mournerstew1 and mournerstew3 and nobody of those two kinds stands upstairs.
	for _, at := range []tile.Tile{{X: 2552, Z: 3326}, {X: 2545, Z: 3324}} {
		rows := csMapRows(t, "NPC", at)
		if !csHasRow(rows, BioNpcMournerDown) {
			t.Errorf("no dinner mourner (npc %d) at %v", BioNpcMournerDown, at)
		}
	}
}

// The hunters of West Ardougne's west end, from the map: they are not on any walk of the quest, and these are their wander boxes.
func TestBiohazardContentWestHunters(t *testing.T) {
	for _, h := range BioWestHunters {
		if !csHasRow(csMapRows(t, "NPC", h.Spawn), h.NPC) {
			t.Errorf("no npc %d at %v", h.NPC, h.Spawn)
		}
	}
	// And their configs: cowardly, one tile of notice, the wander the plan says.
	cfg := csContent(t, "scripts/_unpack/225/all.npc")
	for name, wander := range map[string]int{"zombie_unarmed": 9, "zombie_armed": 9, "ghost": 8} {
		b := csConfigBlock(cfg, name)
		hasAll(t, name, b, "huntmode=cowardly", "huntrange=1", "wanderrange="+strconv.Itoa(wander))
	}
	// No mourner hunts: the headquarters' mourners have no hunt mode, so they attack only what a script sets them on.
	mourners := csContent(t, bioWestDir+"configs/mourner.npc")
	if strings.Contains(mourners, "huntmode") {
		t.Error("a mourner has a hunt mode now")
	}
}
