package questplan

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, answer, stage, level, reward and hazard the Digsite Quest rests on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run DigsiteContent ./internal/scripts/questplan/

const (
	digDir     = "scripts/quests/quest_itexam/"
	digScripts = digDir + "scripts/"
)

// digMapRows are the rows of one section ("NPC", "OBJ") of a map file: its level, tile and type id.
type digMapRow struct {
	level, x, z, id int
}

func digMapRows(t *testing.T, square, section string) []digMapRow {
	t.Helper()
	mx, mz, ok := strings.Cut(strings.TrimPrefix(square, "m"), "_")
	if !ok {
		t.Fatalf("bad square %q", square)
	}
	sx, _ := strconv.Atoi(mx)
	sz, _ := strconv.Atoi(mz)
	text := csContent(t, "maps/"+square+".jm2")
	i := strings.Index(text, "==== "+section+" ====")
	if i < 0 {
		t.Fatalf("%s has no %s section", square, section)
	}
	body := text[i+len(section)+10:]
	if j := strings.Index(body, "\n===="); j >= 0 {
		body = body[:j]
	}
	var rows []digMapRow
	row := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)`)
	for _, line := range strings.Split(body, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		l, _ := strconv.Atoi(m[1])
		x, _ := strconv.Atoi(m[2])
		z, _ := strconv.Atoi(m[3])
		id, _ := strconv.Atoi(m[4])
		rows = append(rows, digMapRow{level: l, x: sx*64 + x, z: sz*64 + z, id: id})
	}
	return rows
}

func digRowsOf(rows []digMapRow, id int) []tile.Tile {
	var out []tile.Tile
	for _, r := range rows {
		if r.id == id {
			out = append(out, tile.Tile{X: r.x, Z: r.z, Level: r.level})
		}
	}
	return out
}

// The ids the plan carries are the pack's.
func TestDigsiteContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "specimen_jar", DigObjSpecimenJar}, {"obj", obj, "specimen_brush", DigObjSpecimenBrush}, {"obj", obj, "rock_sample1", DigObjSampleGreen},
		{"obj", obj, "rock_sample2", DigObjSampleOrange}, {"obj", obj, "rock_sample3", DigObjSamplePurple}, {"obj", obj, "trowel", DigObjTrowel},
		{"obj", obj, "tray_empty", DigObjTrayEmpty}, {"obj", obj, "tray_mud", DigObjTrayMud}, {"obj", obj, "digtalisman", DigObjTalisman},
		{"obj", obj, "digplainletter", DigObjPlainLetter}, {"obj", obj, "recommendedletter", DigObjStampedLetter},
		{"obj", obj, "level1certificate", DigObjCert1}, {"obj", obj, "level2certificate", DigObjCert2}, {"obj", obj, "level3certificate", DigObjCert3},
		{"obj", obj, "digexpertscroll", DigObjExpertScroll}, {"obj", obj, "zarosstonetablet", DigObjStoneTablet}, {"obj", obj, "unidentified_powder", DigObjPowder},
		{"obj", obj, "ammonium_nitrate", DigObjNitrate}, {"obj", obj, "unidentified_liquid", DigObjLiquid}, {"obj", obj, "nitroglycerin", DigObjNitro},
		{"obj", obj, "ground_charcoal", DigObjGroundCharcoal}, {"obj", obj, "precharcoalmixture", DigObjPreMix}, {"obj", obj, "postcharcoalmixture", DigObjPostMix},
		{"obj", obj, "digcompound", DigObjCompound}, {"obj", obj, "arcenia_root", DigObjArceniaRoot}, {"obj", obj, "digchestkey", DigObjChestKey},
		{"obj", obj, "vial_empty", DigObjVial}, {"obj", obj, "pestle_and_mortar", DigObjPestle}, {"obj", obj, "tinderbox", DigObjTinderbox},
		{"obj", obj, "charcoal", DigObjCharcoal}, {"obj", obj, "rope", DigObjRope}, {"obj", obj, "chisel", DigObjChisel}, {"obj", obj, "opal", DigObjOpal},
		{"obj", obj, "uncut_opal", DigObjUncutOpal}, {"obj", obj, "cup_of_tea", DigObjTea},
		{"npc", npc, "digworkman1", DigNpcWorkman}, {"npc", npc, "digworkman2", DigNpcCaveWorkman}, {"npc", npc, "student1", DigNpcGreen},
		{"npc", npc, "student3", DigNpcOrange}, {"npc", npc, "student2", DigNpcPurple}, {"npc", npc, "examiner", DigNpcExaminer},
		{"npc", npc, "archaeological_expert", DigNpcExpert}, {"npc", npc, "panning_guide", DigNpcGuide}, {"npc", npc, "curator", DigNpcCurator},
		{"npc", npc, "tea_seller", DigNpcTeaSeller}, {"npc", npc, "jatix", PlagueNpcJatix}, {"npc", npc, "skeleton_unarmed", NpcManorSkeleton},
		{"loc", loc, "digwinch1", DigLocMainWinch}, {"loc", loc, "digwinch2", DigLocPrivateWinch}, {"loc", loc, "winchladder1", DigLocRopeWest},
		{"loc", loc, "winchladder2", DigLocRopeEast}, {"loc", loc, "digsamplesacks", DigLocSacks}, {"loc", loc, "digsitebushsample", DigLocSampleBush},
		{"loc", loc, "digbarrelclosed", DigLocBarrel}, {"loc", loc, "digchestopen", DigLocChestOpen}, {"loc", loc, "digchestclosed", DigLocChestShut},
		{"loc", loc, "digblastbrick", DigLocBrick}, {"loc", loc, "panning_point", DigLocPanning}, {"loc", loc, "digdugupsoil1", DigLocSoil1},
		{"loc", loc, "digdugupsoil2", DigLocSoil2}, {"loc", loc, "digdugupsoil3", DigLocSoil3},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	// The students: npc student1 is the green one, student3 the orange one, student2 the purple one (the pack numbers them 615, 617, 616).
	if npc["student1"] != 615 || npc["student2"] != 617 || npc["student3"] != 616 {
		t.Errorf("the students are %d %d %d in the pack", npc["student1"], npc["student2"], npc["student3"])
	}
	// The loot the module may drop is the pack's: every id of the junk list names an object.
	byID := map[int]string{}
	for name, id := range obj {
		byID[id] = name
	}
	for _, id := range digJunk {
		if _, ok := byID[id]; !ok {
			t.Errorf("junk id %d is no object in the pack", id)
		}
	}
	for _, c := range []struct {
		name string
		want int
	}{{"spade", 952}, {"leather_gloves", 1059}, {"leather_boots", 1061}, {"bucket_empty", 1925}, {"bones", 526}, {"digsitearrow", 687}, {"digsitepottery", 694},
		{"digsitebuttons", 688}, {"digsitevase", 710}, {"digsiteglass", 690}, {"digsitesword", 686}, {"digsitestaff", 689}, {"digsitearmour1", 697},
		{"digsitearmour2", 698}, {"digsitebuckle", 684}, {"old_tooth", 695}, {"old_boot", 685}, {"cracked_sample", 674}, {"nuggets", 680}} {
		if obj[c.name] != c.want {
			t.Errorf("object %s is %d in the pack, the junk list has %d", c.name, obj[c.name], c.want)
		}
	}
}

// The stages, the points and the rewards.
func TestDigsiteContentStagesAndRewards(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^itexam_complete = %d", DigComplete), "^itexam_questpoints = 2"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, digDir+"configs/quest_itexam.constant")
	for name, v := range map[string]int{"not_started": DigNotStarted, "stamping": DigStamping, "first_exam": DigFirstExam, "second_exam": DigSecondExam,
		"third_exam": DigThirdExam, "impress_archeological_expert": DigImpressExpert, "mineshaft_permit": DigMineshaftPermit,
		"poured_compound_on_bricks": DigPouredCompound, "removed_blockage": DigRemovedBlockage} {
		if want := fmt.Sprintf("^itexam_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_itexam.constant lacks %q", want)
		}
	}
	// The varps: the stage is bits 0..3 of a perm varp that is never transmitted, which is why the journal is the only reader.
	varp := csContent(t, digDir+"configs/quest_itexam.varp")
	if !strings.Contains(varp, "[itexamlevel]\nscope=perm") || strings.Contains(varp, "transmit") {
		t.Errorf("the varp file is %q: the stage was meant to be unreadable by the client", varp)
	}
	main := csContent(t, digScripts+"quest_itexam.rs2")
	for _, want := range []string{"getbit_range(%itexamlevel, 0, 3)", "if ($progress < %itexamlevel) {", "stat_advance(mining, 153000);", "stat_advance(herblore, 20000);",
		"inv_add(inv, gold_bar, 2);"} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_itexam.rs2 lacks %q", want)
		}
	}
	if LadderFacts[DigsiteID].XP["mining"] != 15300 || LadderFacts[DigsiteID].XP["herblore"] != 2000 {
		t.Errorf("the row pays %v: stat_advance(mining, 153000) and (herblore, 20000) are 15,300 and 2,000 at 1x", LadderFacts[DigsiteID].XP)
	}
	// The tab's name.
	if !strings.Contains(csContent(t, "scripts/player/interfaces/questlist.if"), "[itexam]") ||
		!strings.Contains(csContent(t, "scripts/player/interfaces/questlist.if"), "text=Digsite Quest") {
		t.Error("the quest tab does not say Digsite Quest")
	}
}

// Every level the plan states is a check in the scripts: the pocket, the winches and the mixes.
func TestDigsiteContentLevels(t *testing.T) {
	workman := csContent(t, digScripts+"digsite_workman.rs2")
	for _, want := range []string{
		"if (stat(thieving) < 25) {", "You need to be at level 25 Thieving to pick the workman's pocket.", "mes(\"You fail to pick the workman's pocket.\");",
		"~damage_self(1);", "%stunned = calc(map_clock + 8);",
		// The loot: the green student's sample only at the first exam and only while the pack has none, then rope and brush.
		"if (~itexam_progress = ^itexam_first_exam & inv_total(inv, rock_sample1) = 0) {", "def_int $rand = random(11);", "} else if ($rand < 7) {",
		"mes(\"You steal some rope.\");", "mes(\"You find a specimen brush.\");", "} else if ($rand < 10) {",
		// The permit: a workman takes the letter at the right stage.
		"if (last_useitem = digexpertscroll) {", "if(%itexamlevel = ^itexam_impress_archeological_expert) ~itexam_set_progress(^itexam_mineshaft_permit);",
	} {
		if !strings.Contains(workman, want) {
			t.Errorf("digsite_workman.rs2 lacks %q", want)
		}
	}
	site := csContent(t, digScripts+"area_digsite.rs2")
	if n := strings.Count(site, "if (stat(agility) < 10) {"); n != 2 {
		t.Errorf("%d winches check Agility 10, want both", n)
	}
	for _, want := range []string{"You need an Agility level of 10 to squeeze down the shaft.", "if (~itexam_progress < ^itexam_mineshaft_permit) {",
		"mes(\"The bucket descends, but does not reach the bottom.\");", "mes(\"You tie the rope to the bucket...\");", "inv_del(inv, rope, 1);",
		"p_teleport(0_52_153_42_36);", "p_teleport(0_52_153_25_26);", "p_teleport(0_52_152_42_36);", "p_teleport(0_52_152_25_26);",
		"p_teleport(0_52_53_42_35);", "p_teleport(0_52_53_26_25);"} {
		if !strings.Contains(site, want) {
			t.Errorf("area_digsite.rs2 lacks %q", want)
		}
	}
	chem := csContent(t, digScripts+"itexam_chemistry.rs2")
	if n := strings.Count(chem, "if (stat(herblore) < 10) {"); n != 3 {
		t.Errorf("%d chemistry scripts check Herblore 10, want the postmixture's own check and the second and third mixes'", n)
	}
	// The first mix and the grinding ask for no level.
	for _, label := range []string{"[label,itexam_mixed_chemicals1]", "[label,grind_charcoal]"} {
		i := strings.Index(chem, label)
		if i < 0 {
			t.Fatalf("no %s", label)
		}
		end := strings.Index(chem[i:], "\n[")
		body := chem[i:]
		if end > 0 {
			body = chem[i : i+end]
		}
		if strings.Contains(body, "stat(herblore)") {
			t.Errorf("%s checks Herblore: the plan says the first mix and the grinding need no level", label)
		}
	}
	if DigThieving != 25 || DigAgility != 10 || DigHerblore != 10 {
		t.Errorf("levels %d %d %d", DigThieving, DigAgility, DigHerblore)
	}
}

// The exam answers, derived from the examiner's script itself: for each exam and question, the option the answered errand adds is the plan's
// right answer, the wrong-only menu holds none of them and one of its lines is a fallback.
func TestDigsiteContentExamAnswers(t *testing.T) {
	script := csContent(t, digScripts+"examiner.rs2")
	labels := []string{"itexam_examiner_first_exam", "itexam_examiner_second_exam", "itexam_examiner_third_exam"}
	choice := regexp.MustCompile(`\$q(\d)choice = ~p_choice3\("([^"]*)", 1, "([^"]*)", 2, "([^"]*)", 3\);`)
	score := regexp.MustCompile(`if \(~itexam_errand_progress\(\^itexam_errand_student(\d)\) = 2 & \$q(\d)choice = (\d)\) \{`)
	var rights []string
	for i, label := range labels {
		start := strings.Index(script, "[label,"+label+"]")
		if start < 0 {
			t.Fatalf("no label %s", label)
		}
		body := script[start+1:]
		if end := strings.Index(body, "\n[label,"); end >= 0 {
			body = body[:end]
		}
		menus := map[int][][3]string{}
		for _, m := range choice.FindAllStringSubmatch(body, -1) {
			n, _ := strconv.Atoi(m[1])
			menus[n] = append(menus[n], [3]string{m[2], m[3], m[4]})
		}
		right := map[int]int{}
		for _, m := range score.FindAllStringSubmatch(body, -1) {
			n, _ := strconv.Atoi(m[2])
			k, _ := strconv.Atoi(m[3])
			right[n] = k
		}
		if len(right) != 3 {
			t.Fatalf("%s scores %d questions", label, len(right))
		}
		for q := 1; q <= 3; q++ {
			ms := menus[q]
			if len(ms) != 2 {
				t.Fatalf("%s question %d has %d menus, want the answered and the wrong-only", label, q, len(ms))
			}
			answered, wrongOnly := ms[0], ms[1]
			correct := answered[right[q]-1]
			if want := DigExamAnswers[i*3+q-1]; correct != want {
				t.Errorf("%s question %d: the right answer is %q in the script, the plan has %q", label, q, correct, want)
			}
			rights = append(rights, correct)
			for _, o := range wrongOnly {
				if slices.Contains(DigExamAnswers, o) {
					t.Errorf("%s question %d: the wrong-only menu offers the right answer %q", label, q, o)
				}
			}
			hit := false
			for _, f := range digExamFallbacks {
				for _, o := range wrongOnly {
					if strings.Contains(strings.ToLower(o), strings.ToLower(f)) {
						hit = true
					}
				}
				// A fallback must not match any option of an answered menu either (it would pre-empt nothing there, being after the answers, but a
				// fallback that is also a wrong answer of an answered menu is read as a coincidence worth failing on).
				for _, o := range answered {
					if o != correct && strings.Contains(strings.ToLower(o), strings.ToLower(f)) && !slices.Contains(wrongOnly[:], o) {
						t.Errorf("%s question %d: fallback %q also matches %q of the answered menu", label, q, f, o)
					}
				}
			}
			if !hit {
				t.Errorf("%s question %d: no fallback matches the wrong-only menu %q", label, q, wrongOnly)
			}
			// Exactly one preference of the exam stops matches each menu the account will meet: the answer in an answered menu.
			prefer := DigExaminerExamStop.Prefer
			if pick, ok := PickPreferred(answered[:], prefer); !ok || pick != correct {
				t.Errorf("%s question %d: the exam stop picks %q (%v) from the answered menu, want %q", label, q, pick, ok, correct)
			}
			if _, ok := PickPreferred(wrongOnly[:], prefer); !ok {
				t.Errorf("%s question %d: the exam stop picks nothing from the wrong-only menu %q and the exam would stall", label, q, wrongOnly)
			}
		}
	}
	if !slices.Equal(rights, DigExamAnswers) {
		t.Errorf("the nine right answers in the script are %q, the plan has %q", rights, DigExamAnswers)
	}
	// Which errand each question rides on (the plan's student numbering in the file comment): exam 1 and 2 ask student1, student3, student2 in that
	// order; exam 3 student2, student1, student3.
	order := regexp.MustCompile(`if \(~itexam_errand_progress\(\^itexam_errand_student(\d)\) = 2\) \{\n\s+\$q(\d)choice`)
	var got []string
	for _, label := range labels {
		start := strings.Index(script, "[label,"+label+"]")
		body := script[start+1:]
		if end := strings.Index(body, "\n[label,"); end >= 0 {
			body = body[:end]
		}
		var perQ [4]string
		for _, m := range order.FindAllStringSubmatch(body, -1) {
			n, _ := strconv.Atoi(m[2])
			perQ[n] = m[1]
		}
		got = append(got, strings.Join(perQ[1:], ""))
	}
	if !slices.Equal(got, []string{"132", "132", "213"}) {
		t.Errorf("the errands the exams ride on are %v, want exam 1 and 2 on students 1, 3, 2 and exam 3 on 2, 1, 3", got)
	}
}

// The examiner's, the curator's, the students', the guide's and the expert's menus the stops steer, and the order of the cave workman's pleas.
func TestDigsiteContentDialogue(t *testing.T) {
	examiner := csContent(t, digScripts+"examiner.rs2")
	for _, want := range []string{
		"~p_choice2(\"Can I take an exam?\", 1, \"Interesting...\", 2)", "inv_add(inv, digplainletter, 1);", "~itexam_set_progress(^itexam_stamping);",
		"~p_choice2(\"I have lost the letter you gave me.\", 1, \"Alright I'll try and get it.\", 2)", "inv_del(inv, recommendedletter, ^max_32bit_int);",
		"~itexam_set_progress(^itexam_first_exam);\n    @itexam_examiner_first_exam;", "That was foolish. Take this one and don't lose it!",
		"~p_choice2(\"Yes, I certainly am.\", 1, \"No, not at the moment.\", 2)",
		"~p_choice4(\"I am ready for the next exam.\", 1, \"I am stuck on a question.\", 2, \"Sorry, I didn't mean to disturb you.\", 3, \"I have lost my trowel.\", 4)",
		"~p_choice4(\"I am ready for the last exam...\", 1,", "inv_add(inv, trowel, 1);", "inv_add(inv, level1certificate, 1);", "inv_add(inv, level2certificate, 1);",
		"inv_add(inv, level3certificate, 1);", "~itexam_set_progress(^itexam_second_exam);", "~itexam_set_progress(^itexam_third_exam);",
		"~itexam_set_progress(^itexam_impress_archeological_expert);", "~itexam_reset_student_errands;", "if ($correct_answer_count = 3) {",
		// From stage 5 on the Examiner replaces a trowel with no menu at all.
		"[label,itexam_examiner_go_digging]", "Well, what are you doing here? Get digging!", "if (inv_total(inv, trowel) = 0) {\n    @itexam_examiner_lost_trowel;",
	} {
		if !strings.Contains(examiner, want) {
			t.Errorf("examiner.rs2 lacks %q", want)
		}
	}
	// The stop's preferences are lines of those menus, in the script.
	for _, stop := range []NpcStop{DigExaminerStartStop, DigExaminerLostLetterStop, DigExaminerTrowelStop} {
		for _, p := range stop.Prefer {
			if !strings.Contains(examiner, `"`+p+`"`) {
				t.Errorf("examiner.rs2 has no option %q", p)
			}
		}
	}
	for _, p := range []string{"Yes, I certainly am.", "I am ready for the next exam.", "I am ready for the last exam"} {
		if !strings.Contains(examiner, `"`+p) {
			t.Errorf("examiner.rs2 has no option starting %q", p)
		}
	}
	// The curator stamps the letter before any menu, and the stamped letter replaces it.
	curator := csContent(t, "scripts/areas/area_varrock/scripts/curator.rs2")
	for _, want := range []string{"if (inv_total(inv, digplainletter) > 0) @curator_stamp_letter;", "inv_del(inv, digplainletter, 1);", "inv_add(inv, recommendedletter, 1);",
		"Welcome to the museum of Varrock."} {
		if !strings.Contains(curator, want) {
			t.Errorf("curator.rs2 lacks %q", want)
		}
	}
	// Students: a sample used on a student answers the errand whatever its state; nobody has a menu. (The files are named for the other student:
	// student2.rs2 holds student3's script and student3.rs2 student2's, so the scripts are found by their labels.)
	files := map[string]string{}
	for _, f := range []string{"student1.rs2", "student2.rs2", "student3.rs2"} {
		s := csContent(t, digScripts+f)
		if strings.Contains(s, "p_choice") || strings.Contains(s, "multi") {
			t.Errorf("%s has a menu: the student talks are driven with a placeholder preference", f)
		}
		for n := 1; n <= 3; n++ {
			if strings.Contains(s, fmt.Sprintf("[label,itexam_student%d_deliver_sample]", n)) {
				files[fmt.Sprint(n)] = s
				if !strings.Contains(s, fmt.Sprintf("[opnpcu,student%d]", n)) {
					t.Errorf("%s has no use-on for student%d", f, n)
				}
			}
		}
	}
	if len(files) != 3 {
		t.Errorf("found the delivery of %d students, want 3", len(files))
	}
	s2 := files["2"]
	for _, want := range []string{"[label,itexam_student2_third_exam_tips]", "Oh, hi again. Did you bring me the opal?", "if (inv_total(inv, opal) > 0) {", "inv_del(inv, opal, 1);",
		"~chatnpc(\"<p,happy>I'll tell you what I would like: a precious stone."} {
		if !strings.Contains(s2, want) {
			t.Errorf("student2's script lacks %q", want)
		}
	}
	// The guide: the gate offers one of two lines, the tea is taken when it is in the pack, and the same talk is a plain greeting once given.
	guide := csContent(t, digScripts+"panning_guide.rs2")
	for _, want := range []string{"~p_choice2(\"OK, forget it.\", 1, \"So how do I become invited then?\", 2)", "~chatplayer(\"<p,happy>I've some here that you can have.\");",
		"inv_del(inv, cup_of_tea, 1);", "~itexam_setbit(^itexam_bit_panning_guide_tea_given);"} {
		if !strings.Contains(guide, want) {
			t.Errorf("panning_guide.rs2 lacks %q", want)
		}
	}
	for _, p := range DigGuideStop.Prefer {
		if !strings.Contains(guide, `"`+p+`"`) {
			t.Errorf("panning_guide.rs2 has no option %q", p)
		}
	}
	site := csContent(t, digScripts+"area_digsite.rs2")
	for _, want := range []string{"if (npc_find(coord, panning_guide, 10, 0) = true) {\n        @panning_guide_intervene;", "mes(\"You need a panning tray to pan here!\");",
		"@panning_action;", "[label,search_panning_tray]", "def_int $rand = random(40);", "if ($rand < 3) {", "} else if ($rand < 6) {"} {
		if !strings.Contains(site, want) {
			t.Errorf("area_digsite.rs2 lacks %q", want)
		}
	}
	// The expert: the lost-letter line exists only once he has seen the talisman and the pack holds no letter, and he names each find.
	expert := csContent(t, digScripts+"arcaeological_expert.rs2")
	for _, want := range []string{"if (~itexam_testbit(^itexam_bit_shown_ancient_symbol) = ^true & inv_total(inv, digexpertscroll) = 0) {", "\"I lost the letter you gave me.\", archaeological_expert_lost_letter",
		"\"No thanks.\", archaeological_expert_no_thanks", "if ($item = digtalisman) {", "inv_add(inv, digexpertscroll, 1);", "} else if ($item = zarosstonetablet) {",
		"queue(itexam_complete, 0, 0);", "} else if ($item = unidentified_liquid) {", "inv_add(inv, nitroglycerin, 1);", "} else if ($item = unidentified_powder) {",
		"inv_add(inv, ammonium_nitrate, 1);"} {
		if !strings.Contains(expert, want) {
			t.Errorf("arcaeological_expert.rs2 lacks %q", want)
		}
	}
	for _, p := range DigExpertLetterStop.Prefer {
		if !strings.Contains(expert, `"`+p+`"`) {
			t.Errorf("arcaeological_expert.rs2 has no option %q", p)
		}
	}
	// The cave workman: the pleas in the order the content asks them, each menu matched by exactly one of the plan's lines.
	cave := csContent(t, digScripts+"private_dig_worker.rs2")
	menu := regexp.MustCompile(`@multi3\("([^"]*)", \w+, "([^"]*)", \w+, "([^"]*)", \w+\);`)
	var menus [][]string
	for _, m := range menu.FindAllStringSubmatch(cave, -1) {
		menus = append(menus, []string{m[1], m[2], m[3]})
	}
	// The first menu ("I have been invited ..."), the second (p_choice2) and the five pleas' menus.
	menus = append(menus, []string{"Do you know where to find a specimen jar?", "Do you know where to find a chest key?"})
	var asked []string
	for _, m := range menus {
		pick, ok := PickPreferred(m, DigKeyPleas)
		if !ok {
			continue
		}
		asked = append(asked, pick)
		for _, o := range m {
			if o != pick && slices.ContainsFunc(DigKeyPleas, func(p string) bool { return strings.Contains(strings.ToLower(o), strings.ToLower(p)) }) {
				t.Errorf("menu %q: the option %q also matches a plea", m, o)
			}
		}
	}
	wantAsked := []string{"I have been invited to research here.", "I don't suppose I could use it?", "Please?", "Aww... go on.", "Pretty please!", "Pretty please with sugar on top!", "Do you know where to find a chest key?"}
	slices.Sort(asked)
	slices.Sort(wantAsked)
	if !slices.Equal(asked, wantAsked) {
		t.Errorf("the plan's pleas pick %q from the cave workman's menus, want %q", asked, wantAsked)
	}
	if len(menus) != 7 {
		t.Errorf("the cave workman has %d menus, want 7 (one entry, one for the key, five pleas' worth)", len(menus))
	}
	for _, want := range []string{"inv_add(inv, digchestkey, 1);", "[label,digworkman1_please3]"} {
		if !strings.Contains(cave, want) {
			t.Errorf("private_dig_worker.rs2 lacks %q", want)
		}
	}
}

// The dig: its zones, its requirements and its loot, and every object the tables hand out is something the module keeps or may drop.
func TestDigsiteContentDigSites(t *testing.T) {
	site := csContent(t, digScripts+"area_digsite.rs2")
	for _, want := range []string{
		"inzone(0_52_53_24_4, 0_52_53_29_8, loc_coord) = true", "inzone(0_52_53_22_12, 0_52_53_29_20, loc_coord) = true",
		"if (~itexam_progress < ^itexam_impress_archeological_expert) {", "if (inv_total(inv, specimen_jar) = 0) {", "if (inv_total(inv, specimen_brush) = 0) {",
		"def_int $rand = random(20);", "} else if ($rand < 3) {\n    if (~itexam_testbit(^itexam_bit_shown_ancient_symbol) = ^false & inv_total(inv, digtalisman) = 0) {",
		"[oplocu,_digsite_soil]", "if ($item ! trowel) {", "mes(\"You dig through the earth...\");", "p_delay(6);", "inv_add(inv, charcoal, 1);",
		"def_int $rand = random(13);", "if (inv_freespace(inv) = 0) {\n    mes(\"You find nothing of interest.\");",
	} {
		if !strings.Contains(site, want) {
			t.Errorf("area_digsite.rs2 lacks %q", want)
		}
	}
	// The zones' coordinates.
	for _, c := range []struct {
		z          DigZone
		sw, ne     string
		x0, z0, x1 int
		z1         int
	}{
		{DigTrainingZone, "0_52_53_24_4", "0_52_53_29_8", 52*64 + 24, 53*64 + 4, 52*64 + 29, 53*64 + 8},
		{DigLevel3Zone, "0_52_53_22_12", "0_52_53_29_20", 52*64 + 22, 53*64 + 12, 52*64 + 29, 53*64 + 20},
	} {
		if c.z.MinX != c.x0 || c.z.MinZ != c.z0 || c.z.MaxX != c.x1 || c.z.MaxZ != c.z1 {
			t.Errorf("zone %s..%s is x %d..%d z %d..%d, the plan has %+v", c.sw, c.ne, c.x0, c.x1, c.z0, c.z1, c.z)
		}
	}
	// The loot tables: every object any of them adds is one the module keeps (a find it wants), drops (junk) or does not mind (coins).
	obj := csPackIDs(t, "obj")
	known := map[int]bool{CoinsID: true}
	for _, id := range append(append(append([]int(nil), digTools...), digJunk...), digFoodIDs...) {
		known[id] = true
	}
	add := regexp.MustCompile(`inv_add\(inv, (\w+),`)
	for _, file := range []string{"area_digsite.rs2", "digsite_workman.rs2"} {
		for _, m := range add.FindAllStringSubmatch(csContent(t, digScripts+file), -1) {
			id, ok := obj[m[1]]
			if !ok {
				t.Errorf("%s adds %s, which is no object", file, m[1])
				continue
			}
			if !known[id] {
				t.Errorf("%s adds %s (%d), which is neither a tool nor junk to drop: it would sit in the pack for good", file, m[1], id)
			}
		}
	}
	// The tray and the opal both come out of the river, the sample out of the tray 3 times in 40 (and only at the first exam).
	if !strings.Contains(site, "if (~itexam_progress = ^itexam_first_exam & inv_total(inv, rock_sample2) = 0) {") {
		t.Error("the orange sample is not gated on the first exam")
	}
	if !strings.Contains(site, "inv_add(inv, uncut_opal, 1);\n    ~objbox(uncut_opal, \"You find a gem within the mud!\"") {
		t.Error("the river's opal is not an uncut one")
	}
}

// The chemicals that explode on Drop are exactly the plan's volatile list, each answering opheld5 with damage; nothing else in the quest does.
func TestDigsiteContentVolatileChemicals(t *testing.T) {
	site := csContent(t, digScripts+"area_digsite.rs2")
	obj := csPackIDs(t, "obj")
	re := regexp.MustCompile(`\[opheld5,(\w+)\]((?s:.*?))\n\n`)
	var got []int
	for _, m := range re.FindAllStringSubmatch(site, -1) {
		if !strings.Contains(m[2], "~damage_self(") {
			continue
		}
		id, ok := obj[m[1]]
		if !ok {
			t.Errorf("%s is no object", m[1])
		}
		got = append(got, id)
	}
	slices.Sort(got)
	want := DigVolatileIDs()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the objects that explode on Drop are %v, the plan's volatile list is %v", got, want)
	}
	for name, dmg := range map[string]int{"unidentified_liquid": 25, "nitroglycerin": 35, "precharcoalmixture": 45, "postcharcoalmixture": 55, "digcompound": 65} {
		i := strings.Index(site, "[opheld5,"+name+"]")
		if i < 0 || !strings.Contains(site[i:min(len(site), i+500)], fmt.Sprintf("~damage_self(%d);", dmg)) {
			t.Errorf("%s does not explode for %d", name, dmg)
		}
	}
	// The chemistry's recipes: the plan's mix table is the script's.
	chem := csContent(t, digScripts+"itexam_chemistry.rs2")
	for _, want := range []string{
		"inv_del(inv, charcoal, 1);\ninv_add(inv, ground_charcoal, 1);", "inv_del(inv, ammonium_nitrate, 1);\ninv_del(inv, nitroglycerin, 1);\ninv_add(inv, precharcoalmixture, 1);",
		"inv_del(inv, ground_charcoal, 1);\ninv_del(inv, precharcoalmixture, 1);\ninv_add(inv, postcharcoalmixture, 1);",
		"inv_del(inv, arcenia_root, 1);\ninv_del(inv, postcharcoalmixture, 1);\ninv_add(inv, digcompound, 1);",
		// The last mix adds its product only after a chat line, which is why the executor drives the dialogue.
		"~chatplayer(\"<p,happy>Excellent! This looks just right!\");\ninv_del(inv, arcenia_root, 1);",
	} {
		if !strings.Contains(strings.ReplaceAll(chem, "\n    ", "\n    "), want) {
			t.Errorf("itexam_chemistry.rs2 lacks %q", want)
		}
	}
}

// The chest, the barrel, the bricks and the run: each loc's script and each coordinate the plan stands on.
func TestDigsiteContentLocsAndTiles(t *testing.T) {
	site := csContent(t, digScripts+"area_digsite.rs2")
	for _, want := range []string{
		// The chest: the key opens it and is used up, the open chest gives the powder at any time.
		"[oplocu,digchestclosed]", "inv_del(inv, digchestkey, 1);", "loc_change(digchestopen, 300);", "[oploc1,digchestopen]", "inv_add(inv, unidentified_powder, 1);",
		// The barrel: a trowel opens the lid, a vial takes the liquid only when it is open.
		"[oplocu,digbarrelclosed]", "if ($item = trowel) {", "~itexam_setbit(^itexam_bit_barrel_open);", "} else if ($item = vial_empty) {",
		"inv_add(inv, unidentified_liquid, 1);", "~chatplayer(\"<p,quiz>It's not open!\");",
		// The bricks: the compound, then the tinderbox at the right stage and from the right tile.
		"[oplocu,digblastbrick]", "if ($item = digcompound) {", "~itexam_set_progress(^itexam_poured_compound_on_bricks);", "} else if ($item = tinderbox) {",
		"if (~itexam_progress ! ^itexam_poured_compound_on_bricks) {", "if (coord ! 0_52_153_51_34) {", "mes(\"Eep! Eep! Unexpected player coord!\");",
		"p_teleport(0_52_152_40_39);", "~itexam_set_progress(^itexam_removed_blockage);",
		// The sample bush, the sacks and the specimen jar.
		"[oploc1,digsitebushsample]", "inv_add(inv, rock_sample3, 1);", "[oploc1,digsamplesacks]", "inv_add(inv, specimen_jar, 1);",
	} {
		if !strings.Contains(site, want) {
			t.Errorf("area_digsite.rs2 lacks %q", want)
		}
	}
	at := func(x, z int) tile.Tile { return tile.Tile{X: x, Z: z} }
	for _, c := range []struct {
		what string
		got  tile.Tile
		want tile.Tile
	}{
		{"the brick stand 0_52_153_51_34", DigBrickStand, at(52*64+51, 153*64+34)},
		{"the east winch's landing 0_52_153_42_36", DigEastLanding, at(52*64+42, 153*64+36)},
		{"the west winch's landing 0_52_153_25_26", DigWestLanding, at(52*64+25, 153*64+26)},
		{"the blast's landing 0_52_152_40_39", DigAltarLanding, at(52*64+40, 152*64+39)},
		{"the east winch's landing past the blast 0_52_152_42_36", DigAltarEastLanding, at(52*64+42, 152*64+36)},
		{"the west winch's landing past the blast 0_52_152_25_26", DigAltarWestLanding, at(52*64+25, 152*64+26)},
	} {
		if c.got != c.want {
			t.Errorf("%s is %v, the plan has %v", c.what, c.want, c.got)
		}
	}
	// The spawns on the maps.
	rows52 := digMapRows(t, "m52_52", "NPC")
	rows53 := digMapRows(t, "m52_53", "NPC")
	rows153 := digMapRows(t, "m52_153", "NPC")
	if got := digRowsOf(rows52, DigNpcExaminer); !slices.Contains(got, DigExaminerAnchor) || len(got) != 3 {
		t.Errorf("the Examiner's spawns are %v, the anchor is %v", got, DigExaminerAnchor)
	}
	if got := digRowsOf(rows52, DigNpcExpert); !slices.Equal(got, []tile.Tile{DigExpertAnchor}) {
		t.Errorf("the expert's spawns are %v, the anchor is %v", got, DigExpertAnchor)
	}
	if got := digRowsOf(rows52, DigNpcGuide); !slices.Equal(got, []tile.Tile{DigGuideAnchor}) {
		t.Errorf("the guide's spawns are %v, the anchor is %v", got, DigGuideAnchor)
	}
	if got := digRowsOf(rows153, DigNpcCaveWorkman); !slices.Equal(got, []tile.Tile{DigCaveWorkmanAnchor}) {
		t.Errorf("the cave workman's spawns are %v, the anchor is %v", got, DigCaveWorkmanAnchor)
	}
	if got := digRowsOf(digMapRows(t, "m50_53", "NPC"), DigNpcCurator); !slices.Equal(got, []tile.Tile{DigCuratorAnchor}) {
		t.Errorf("the Curator's spawns are %v, the anchor is %v", got, DigCuratorAnchor)
	}
	if got := digRowsOf(digMapRows(t, "m51_53", "NPC"), DigNpcTeaSeller); !slices.Equal(got, []tile.Tile{DigTeaSellerAnchor}) {
		t.Errorf("the tea seller's spawns are %v, the anchor is %v", got, DigTeaSellerAnchor)
	}
	// The students stand all over the camp: each colour has spawns within the stop's leash of its anchor, and the closest is near.
	for _, c := range []struct {
		id   int
		stop NpcStop
	}{{DigNpcGreen, DigGreenStop}, {DigNpcOrange, DigOrangeStop}, {DigNpcPurple, DigPurpleStop}} {
		near := 0
		for _, sp := range digRowsOf(rows53, c.id) {
			if tile.Chebyshev(sp, c.stop.Anchor) <= c.stop.Leash {
				near++
			}
		}
		if near < 2 {
			t.Errorf("student %d has %d spawns within %d of %v", c.id, near, c.stop.Leash, c.stop.Anchor)
		}
	}
	// The tray and the tablet are one spawn each, the roots two in the west shaft.
	if got := digRowsOf(digMapRows(t, "m52_52", "OBJ"), DigObjTrayEmpty); !slices.Equal(got, []tile.Tile{DigTraySpawn}) {
		t.Errorf("the tray spawns at %v, the plan has %v", got, DigTraySpawn)
	}
	if got := digRowsOf(digMapRows(t, "m52_152", "OBJ"), DigObjStoneTablet); !slices.Equal(got, []tile.Tile{DigTabletSpawn}) {
		t.Errorf("the tablet spawns at %v, the plan has %v", got, DigTabletSpawn)
	}
	roots := digRowsOf(digMapRows(t, "m52_153", "OBJ"), DigObjArceniaRoot)
	for _, r := range DigRootSpawns {
		if !slices.Contains(roots, r) {
			t.Errorf("no arcenia root spawns at %v (the map has %v)", r, roots)
		}
		if !InDigShaftWest(r) {
			t.Errorf("root spawn %v is not in the west shaft", r)
		}
	}
	// The two roots of the east shaft are not the plan's (the key is in the west one, and the trip is made there for both).
	for _, r := range roots {
		if !slices.Contains(DigRootSpawns, r) && InDigShaftWest(r) {
			t.Errorf("a root spawns in the west shaft at %v and the plan does not list it", r)
		}
	}
}

// The skeletons: the altar cave holds ten of the level 25 unarmed ones, which hunt only accounts of combat 50 and under within a tile; the
// pre-blast map's ten are the same rows (checked on the pack for reach in questmods).
func TestDigsiteContentSkeletons(t *testing.T) {
	cave := digRowsOf(digMapRows(t, "m52_152", "NPC"), NpcManorSkeleton)
	if len(cave) != 10 {
		t.Fatalf("%d skeletons in the altar cave's map, want 10", len(cave))
	}
	for _, sp := range cave {
		if !InDigAltarEast(sp) {
			t.Errorf("a skeleton stands at %v, outside the altar cave's rectangle", sp)
		}
	}
	nearTablet := 0
	for _, sp := range cave {
		if tile.Chebyshev(sp, DigTabletSpawn) <= 5 {
			nearTablet++
		}
	}
	if nearTablet < 3 {
		t.Errorf("%d skeletons within 5 tiles of the tablet", nearTablet)
	}
	npcCfg := csContent(t, "scripts/_unpack/225/all.npc")
	block := csConfigBlock(npcCfg, "skeleton_unarmed")
	for _, want := range []string{"vislevel=25", "attack=24", "defence=24", "strength=24", "hitpoints=17", "huntrange=1", "huntmode=cowardly", "op2=Attack"} {
		if !strings.Contains(block, want) {
			t.Errorf("[skeleton_unarmed] lacks %q", want)
		}
	}
	// The foe the plan prices is that row.
	f := FoeManorSkeleton
	if f.Attack != 24 || f.Strength != 24 || f.Defence != 24 || f.Hitpoints != 17 || f.AttackBonus != 0 || f.StrengthBonus != 0 {
		t.Errorf("FoeManorSkeleton %+v is not [skeleton_unarmed]", f)
	}
	if DigSkeletonCeiling != 2*25 {
		t.Errorf("the skeletons' ceiling %d is not twice vislevel 25", DigSkeletonCeiling)
	}
	hunt := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "cowardly")
	for _, want := range []string{"type=player", "check_nottoostrong=outside_wilderness"} {
		if !strings.Contains(hunt, want) {
			t.Errorf("[cowardly] lacks %q", want)
		}
	}
}

// The journal fixtures are the script's own lines: every |-separated fragment of every page the tests render is in itexam_journal.rs2.
func TestDigsiteContentJournalFixtures(t *testing.T) {
	script := csContent(t, digScripts+"itexam_journal.rs2")
	for progress := DigStamping; progress <= DigComplete; progress++ {
		for _, st := range []digJournalState{
			{progress: progress}, {progress: progress, stamped: true}, {progress: progress, e1: 1}, {progress: progress, e1: 1, s1: true},
			{progress: progress, e2: 1}, {progress: progress, e2: 1, s2: true}, {progress: progress, e3: 1}, {progress: progress, e3: 1, s3: true},
			{progress: progress, e1: 2, e2: 2, e3: 2},
		} {
			mcFragmentsIn(t, script, fmt.Sprintf("stage %d %+v", progress, st), digRenderJournal(st))
		}
	}
	// And the needles the parser looks for are in the script's text (once its colour tags are gone and its lines joined).
	normal := strings.ToLower(NormaliseLines(strings.Split(strings.ReplaceAll(strings.ReplaceAll(script, "\"", ""), "\\n", " "), "|")))
	for _, m := range digMarkers {
		if m.stage == DigComplete {
			continue
		}
		if !strings.Contains(normal, m.text) {
			t.Errorf("the journal script has no text %q for stage %d", m.text, m.stage)
		}
	}
}

// The shops: the tea seller's stall, Varrock's general store and Jatix sell what the plan buys there.
func TestDigsiteContentShops(t *testing.T) {
	varrock := csContent(t, "scripts/areas/area_varrock/configs/varrock.inv")
	if !strings.Contains(csConfigBlock(varrock, "teashop"), "stock1=cup_of_tea,20,50") {
		t.Error("the tea shop does not stock cup_of_tea")
	}
	general := csConfigBlock(varrock, "generalshop2")
	for _, want := range []string{"stock5=tinderbox,2,100", "stock6=chisel,2,100"} {
		if !strings.Contains(general, want) {
			t.Errorf("Varrock's general store lacks %q", want)
		}
	}
	taverley := csContent(t, "scripts/areas/area_taverly/configs/taverly.inv")
	if !strings.Contains(taverley, "stock1=vial_empty,800,100") || !strings.Contains(taverley, "stock2=pestle_and_mortar,3,100") {
		t.Error("Jatix's store does not stock the vial and the pestle")
	}
	tea := csContent(t, "scripts/areas/area_varrock/scripts/tea_seller.rs2")
	if !strings.Contains(tea, "[opnpc3,tea_seller]") || !strings.Contains(tea, "~openshop_activenpc;") {
		t.Error("the tea seller's Trade option (op 3) is gone")
	}
}
