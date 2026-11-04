package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward and rule Hazeel Cult rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped without one)
// so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run HazeelContent ./internal/scripts/questplan/
//
// (The loc index the fleet runs on is baked from the production maps; what it holds at these tiles, and the stands of each loc, are pinned in
// the questmods real-data tests.)

const hzDir = "scripts/quests/quest_hazeelcult/"

// jm2Entry is one line of a map file's LOC, NPC or OBJ section: "level x z: id [shape rotation]", in world coordinates.
type jm2Entry struct {
	Level, X, Z, ID, Shape, Rot int
}

var jm2Line = regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)(?: (\d+)(?: (\d+))?)?\s*$`)

// jm2Entries reads a section ("LOC", "NPC" or "OBJ") of maps/m<sx>_<sz>.jm2, the map square's origin added.
func jm2Entries(t *testing.T, sx, sz int, section string) []jm2Entry {
	t.Helper()
	src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", sx, sz))
	in := false
	var out []jm2Entry
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = strings.HasPrefix(line, "==== "+section+" ")
			continue
		}
		if !in {
			continue
		}
		m := jm2Line.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
		out = append(out, jm2Entry{Level: n(1), X: sx*64 + n(2), Z: sz*64 + n(3), ID: n(4), Shape: n(5), Rot: n(6)})
	}
	if len(out) == 0 {
		t.Fatalf("no %s entries in m%d_%d", section, sx, sz)
	}
	return out
}

// jm2Has reports an entry of id at the tile.
func jm2Has(es []jm2Entry, id int, at tile.Tile) bool {
	for _, e := range es {
		if e.ID == id && e.X == at.X && e.Z == at.Z && e.Level == at.Level {
			return true
		}
	}
	return false
}

// The pack ids the plan carries are the pack's.
func TestHazeelContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "sir_ceril_carnillean", NpcHzCeril}, {"npc", npc, "claus_carnillean", NpcHzClaus}, {"npc", npc, "butler_jones_hazeel_cultist", NpcHzJones},
		{"npc", npc, "alomone_hazeel_cultist", NpcHzAlomone}, {"npc", npc, "hazeel", NpcHzHazeel}, {"npc", npc, "clivet_hazeel_cultist", NpcHzClivet},
		{"npc", npc, "hazeel_cultist", NpcHzCultist},
		{"obj", obj, "hazeel_scroll", ObjHzScroll}, {"obj", obj, "carnilleanchestkey", ObjHzKey}, {"obj", obj, "carnillean_armour", ObjHzArmour},
		{"obj", obj, "mark_of_hazeel", ObjHzMark}, {"obj", obj, "poison", ObjPoison},
		{"loc", loc, "sewervalve1", LocHzValve1}, {"loc", loc, "sewervalve2", LocHzValve2}, {"loc", loc, "sewervalve3", LocHzValve3},
		{"loc", loc, "sewervalve4", LocHzValve4}, {"loc", loc, "sewervalve5", LocHzValve5}, {"loc", loc, "hazeelsewerraft", LocHzRaft},
		{"loc", loc, "hazeelcultcave", LocHzCave}, {"loc", loc, "hazeelcultstairs", LocHzStairs}, {"loc", loc, "loc_2854", LocHzSecretWall},
		{"loc", loc, "carnilleanshutchest", LocHzChestShut}, {"loc", loc, "carnilleanopenchest", LocHzChestOpen}, {"loc", loc, "carnilleancrate", LocHzCrate},
		{"loc", loc, "carnilleanrange", LocHzRange}, {"loc", loc, "ladder_from_cellar", LocHzKitchenLadder}, {"loc", loc, "ladder", LocHzAtticLadderUp},
		{"loc", loc, "laddertop", LocHzAtticLadderDn},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the quest points and the rewards.
func TestHazeelContentStagesAndRewards(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^hazeelcult_complete = %d", HzComplete), "^hazeelcult_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	varp := csContent(t, hzDir+"configs/quest_hazeelcult.constant")
	for name, v := range map[string]int{"not_started": HzNotStarted, "started": HzStarted, "spoken_clivet": HzSpokenClivet, "clivet_decision": HzClivetDecision,
		"poured_poison": HzPoured, "finished_side_task": HzFinishedSide, "given_armour_or_scroll": HzScroll} {
		if want := fmt.Sprintf("^hazeelcult_%s = %d", name, v); !strings.Contains(varp, want) {
			t.Errorf("quest_hazeelcult.constant lacks %q", want)
		}
	}
	for _, want := range []string{"^hazeelcult_goodside = 0", "^hazeelcult_evilside = 1"} {
		if !strings.Contains(varp, want) {
			t.Errorf("quest_hazeelcult.constant lacks %q", want)
		}
	}
	quests := csContent(t, hzDir+"scripts/quest_hazeelcult.rs2")
	for _, want := range []string{
		// The reward: the experience (1x: 1,500), the coins, the quest point and the scroll taken.
		"[queue,hazeelcult_quest_complete]", "%hazeelcultquest = ^hazeelcult_complete;", "inv_del(inv, hazeel_scroll, 1);", "stat_advance(thieving, 15000);",
		"inv_add(inv, coins, 2000);", "^hazeelcult_questpoints",
	} {
		if !strings.Contains(quests, want) {
			t.Errorf("quest_hazeelcult.rs2 lacks %q", want)
		}
	}
}

// Ceril's and Clivet's menus, in the order they come, and what the answers do.
func TestHazeelContentTheTalks(t *testing.T) {
	ceril := csContent(t, hzDir+"scripts/ceril_carnillean.rs2")
	for _, want := range []string{
		"case ^hazeelcult_not_started :", `~p_choice3("What's wrong?", 1, "You probably deserve it.", 2, "You seem uptight, I'll leave you alone.", 3)`,
		`~p_choice2("No thanks. I've got other plans.", 1, "Yes, of course, I'd be happy to help.", 2)`, "%hazeelcultquest = ^hazeelcult_started;",
	} {
		if !strings.Contains(ceril, want) {
			t.Errorf("ceril_carnillean.rs2 lacks %q", want)
		}
	}
	clivet := csContent(t, hzDir+"scripts/clivet.rs2")
	for _, want := range []string{
		// Stage 2 sets stage 3 before its first menu; stage 3 asks the same menu again.
		"%hazeelcultquest = ^hazeelcult_spoken_clivet;",
		`@multi2("What do you mean?", clivet_hazeel_cultist_what_do_you_mean, "I've heard enough of your rubbish.", clivet_hazeel_cultist_rubbish);`,
		// The two further menus.
		`switch_int(~p_choice2("You're crazy, I'd never help you.", 1, "So what would I have to do?", 2))`,
		`switch_int(~p_choice2("No. I won't do it.", 1, "Ok, count me in.", 2))`,
		// The cult's answer: the vial, the side and the stage.
		"inv_add(inv, poison, 1);", "%hazeelcult_side = ^hazeelcult_evilside;", "%hazeelcultquest = ^hazeelcult_clivet_decision;",
		// The Carnillean side's three refusals all lock the other side in and remove Clivet.
		"%hazeelcult_side = ^hazeelcult_goodside;", "npc_del;",
		// At stage 4 on the cult's side Clivet hands a lost vial out again, with no menu and no bank check.
		"if(inv_total(inv, poison) = 0) { // no bank check", "inv_add(inv, poison, 1);",
	} {
		if !strings.Contains(clivet, want) {
			t.Errorf("clivet.rs2 lacks %q", want)
		}
	}
	// The order of the preferences is the order of the menus in the script.
	last := -1
	for _, line := range HzClivetStop.Prefer {
		i := strings.Index(clivet, `"`+line+`"`)
		if i < 0 {
			t.Errorf("clivet.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	last = -1
	for _, line := range []string{"What's wrong?", "Yes, of course, I'd be happy to help."} {
		i := strings.Index(ceril, `"`+line+`"`)
		if i < 0 {
			t.Errorf("ceril_carnillean.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	// The words of the quest the stage-5 talk must not be taken for: Alomone's first talk (no menu) sets the stage, and the cult's other
	// stages have no menu either.
	alomone := csContent(t, hzDir+"scripts/alomone.rs2")
	for _, want := range []string{
		"case ^hazeelcult_poured_poison :", "%hazeelcultquest = ^hazeelcult_finished_side_task;",
		// The ritual: the scroll in the pack, the strong queue that ends it.
		"if(inv_total(inv, hazeel_scroll) = 0) {", "queue*(hazeel_leave, 0)(npc_uid);", "queue(hazeelcult_quest_complete, 0, 0);",
	} {
		if !strings.Contains(alomone+csContent(t, hzDir+"scripts/quest_hazeelcult.rs2"), want) {
			t.Errorf("alomone.rs2 lacks %q", want)
		}
	}
	// None of Alomone's talks on the cult's side retaliates: only the "default" case, the Carnillean side's and the first-stage talks do.
	cultCase := alomone[strings.Index(alomone, "case ^hazeelcult_poured_poison :"):strings.Index(alomone, "case ^hazeelcult_finished_side_task :")]
	if strings.Contains(cultCase, "npc_retaliate") {
		t.Error("Alomone's stage-5 talk retaliates")
	}
}

// The poison goes on the range, the valves set bits, the raft counts them, the crate and the chest and the secret wall.
func TestHazeelContentTheWorldsRules(t *testing.T) {
	soup := csContent(t, hzDir+"scripts/claus_the_chef.rs2")
	for _, want := range []string{
		"[oplocu,carnilleanrange]", "if(last_useitem = poison) {", "if(%hazeelcultquest = ^hazeelcult_clivet_decision & %hazeelcult_side = ^hazeelcult_evilside) {",
		"%hazeelcultquest = ^hazeelcult_poured_poison;", "inv_del(inv, poison, 1);",
	} {
		if !strings.Contains(soup, want) {
			t.Errorf("claus_the_chef.rs2 lacks %q", want)
		}
	}
	q := csContent(t, hzDir+"scripts/quest_hazeelcult.rs2")
	for _, want := range []string{
		// The valves: the third sets its bit on Turn-left, every other on Turn-right, and an op writes the bit, it does not toggle it.
		"[oploc1,_hazeelcult_valve]", "if(loc_type = sewervalve3) {", "%hazeelcult_valves = setbit(%hazeelcult_valves, loc_param(hazeelcult_valve_idx));",
		"%hazeelcult_valves = clearbit(%hazeelcult_valves, loc_param(hazeelcult_valve_idx));", "[oploc2,_hazeelcult_valve]",
		// The raft counts the set bits from the first valve and stops at the first clear one; five rides to the hideout, the landing's raft only.
		"[proc,count_correct_valves]()(int)", "if(testbit(%hazeelcult_valves, $i) = ^true) {", "} else if($valves_value = 5) {", "p_teleport(0_40_151_46_28);",
		"if(loc_coord ! 0_40_151_7_15) {", "p_teleport(0_40_151_7_16);", "The current is flowing against the raft. It will not move.",
		// The cave and the stairs.
		"p_teleport(0_40_151_10_18);", "p_teleport(0_40_50_27_37);",
		// The secret wall: stages 6 and 7 on the cult's side.
		"[oploc1,loc_2854]", "%hazeelcultquest >= ^hazeelcult_finished_side_task & %hazeelcultquest < ^hazeelcult_complete & %hazeelcult_side = ^hazeelcult_evilside",
		"~open_and_close_door2(loc_2855, ~check_axis(coord, loc_coord, loc_angle), door_open);",
		// The chest: the key unlocks, the scroll only for a pack with none, the stage bump only at stage 6.
		"[oplocu,carnilleanshutchest]", "if(last_useitem = carnilleanchestkey) {", "[oploc1,carnilleanopenchest]", "if(inv_total(inv, hazeel_scroll) > 0) {", "The chest is empty.",
		"if(%hazeelcultquest = ^hazeelcult_finished_side_task & %hazeelcult_side = ^hazeelcult_evilside) {", "%hazeelcultquest = ^hazeelcult_given_armour_or_scroll;",
		// The crate: the key only for a pack with none, no stage asked.
		"[oploc1,carnilleancrate]", "if(inv_total(inv, carnilleanchestkey) > 0) {", "inv_add(inv, carnilleanchestkey, 1);",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("quest_hazeelcult.rs2 lacks %q", want)
		}
	}
	// Both of the world's message boxes that carry an item come BEFORE the item is added: continued, never closed.
	for _, c := range []struct{ box, add string }{
		{`~mesbox("You search the crate. Under various food package you find an old rusty key.");`, "inv_add(inv, carnilleanchestkey, 1);"},
		{`~mesbox("Inside the chest you find the Scroll of Hazeel.");`, "inv_add(inv, hazeel_scroll, 1);"},
	} {
		b, a := strings.Index(q, c.box), strings.LastIndex(q, c.add)
		if b < 0 || a < b {
			t.Errorf("the box %q does not come before %q (%d, %d)", c.box, c.add, b, a)
		}
	}
	// The valve's and the range's boxes come AFTER the change: closing them loses nothing.
	if v := strings.Index(q, "%hazeelcult_valves = setbit"); v < 0 || v > strings.Index(q, `~mesbox("You turn the large metal valve to the left.`) {
		t.Error("the valve's box comes before its bit")
	}
	if p, b := strings.Index(soup, "inv_del(inv, poison, 1);"), strings.Index(soup, `~mesbox("You pour the poison`); p < 0 || b < p {
		t.Error("the range's box comes before the poison is poured")
	}
}

// The cult and its place are peaceful on the cult's side: none of the npcs the route passes has a hunt mode, and the quest's own two
// that offer Attack are only attacked by the Carnillean side's route.
func TestHazeelContentNothingHuntsTheCultsRoute(t *testing.T) {
	root := contentRoot(t)
	blocks := npcBlocks(t, root)
	for _, name := range []string{"sir_ceril_carnillean", "claus_carnillean", "guard_carnillean", "philipe_carnillean", "carnillean_wife", "butler_jones_hazeel_cultist",
		"alomone_hazeel_cultist", "hazeel", "clivet_hazeel_cultist", "hazeel_cultist", "rat"} {
		b := blocks[name]
		if b == nil {
			t.Errorf("no [%s] block", name)
			continue
		}
		if mode, hunts := b["huntmode"]; hunts {
			t.Errorf("[%s] has huntmode=%s: the cult's road would be walked past a hunter", name, mode)
		}
	}
	// Alomone, Clivet and the cultists are level 13 with 25 hitpoints (the Carnillean side's fight, which this module never starts).
	for _, name := range []string{"alomone_hazeel_cultist", "clivet_hazeel_cultist", "hazeel_cultist"} {
		if blocks[name]["hitpoints"] != "25" {
			t.Errorf("[%s] hitpoints %q", name, blocks[name]["hitpoints"])
		}
	}
}

// The journal's markers are in the journal script, each in the branch the plan reads it in: the cult's side before the else of the good
// side, the Carnillean side's three after it.
func TestHazeelContentTheJournalMarkers(t *testing.T) {
	src := csContent(t, hzDir+"scripts/hazeelcult_journal.rs2")
	split := strings.Index(src, "} else {//good side")
	if split < 0 {
		t.Fatal("the journal script has no good-side branch marker")
	}
	literal := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	norm := func(s string) string { return NormaliseLines(strings.Split(s, "|")) }
	for _, m := range hzMarkers {
		if m.stage == HzComplete {
			continue // the shared "QUEST COMPLETE!" is a @red@ line in both branches
		}
		found, at := false, -1
		for _, loc := range literal.FindAllStringSubmatchIndex(src, -1) {
			if strings.Contains(norm(src[loc[2]:loc[3]]), m.text) {
				found, at = true, loc[0]
				break
			}
		}
		if !found {
			t.Errorf("marker %q (stage %d) is in no string of the journal script", m.text, m.stage)
			continue
		}
		good := m.stage >= HzGoodSide
		switch {
		case m.stage == HzNotStarted || m.stage == HzStarted:
			// before both branches
		case m.stage == HzSpokenClivet:
			// stage 3 is the one page both sides print, before the fork; its text is also struck on the good side after the fork.
		case good && at < split:
			t.Errorf("the Carnillean marker %q is read from before the good branch", m.text)
		case !good && at > split:
			t.Errorf("the cult's marker %q is read from the good branch", m.text)
		}
	}
	if !strings.Contains(src, `@red@QUEST COMPLETE!`) {
		t.Error("the journal has no QUEST COMPLETE! line")
	}
	// The page of stage 3 appears once, unstruck, before the fork.
	if strings.Count(src, "@dbl@He told me a pack of lies about the Carnilleans, then asked me to join the cult.") != 1 {
		t.Error("stage 3's own page is not the only unstruck pack-of-lies line")
	}
}

// The tiles of the plan are the maps': spawns, locs and the teleports' targets.
func TestHazeelContentTheMapsAgreeWithThePlan(t *testing.T) {
	sewerNpcs := jm2Entries(t, 40, 151, "NPC")
	sewerLocs := jm2Entries(t, 40, 151, "LOC")
	forestLocs := jm2Entries(t, 40, 50, "LOC")
	houseNpcs := jm2Entries(t, 40, 51, "NPC")
	houseLocs := jm2Entries(t, 40, 51, "LOC")
	for _, c := range []struct {
		what string
		es   []jm2Entry
		id   int
		at   tile.Tile
	}{
		{"Clivet's spawn", sewerNpcs, NpcHzClivet, HzClivetAnchor}, {"Alomone's spawn", sewerNpcs, NpcHzAlomone, HzAlomoneAnchor},
		{"Ceril's spawn", houseNpcs, NpcHzCeril, HzCerilAnchor},
		{"the landing's raft", sewerLocs, LocHzRaft, HzRaftMouth}, {"the hideout's raft", sewerLocs, LocHzRaft, HzHideoutRaft},
		{"the stairs", sewerLocs, LocHzStairs, HzStairsAt}, {"the range", sewerLocs, LocHzRange, HzRangeAt}, {"the crate", sewerLocs, LocHzCrate, HzCrateAt},
		{"the cave", forestLocs, LocHzCave, HzCaveAt},
		{"the secret wall", houseLocs, LocHzSecretWall, HzWallAt}, {"the chest", houseLocs, LocHzChestShut, HzChestAt},
	} {
		if !jm2Has(c.es, c.id, c.at) {
			t.Errorf("%s: no %d at %v in the map", c.what, c.id, c.at)
		}
	}
	for i, v := range HzValves {
		if !jm2Has(forestLocs, v.Loc, v.At) {
			t.Errorf("valve %d: no loc %d at %v in the map", i+1, v.Loc, v.At)
		}
	}
	// The teleports' targets: the raft to the hideout, the raft back, the cave, the stairs.
	for _, c := range []struct {
		what   string
		packed [4]int // the content's map square coordinates 0_<sx>_<sz>_<x>_<z>
		want   tile.Tile
	}{
		{"the cave's landing", [4]int{40, 151, 10, 18}, HzLanding}, {"the stairs' top", [4]int{40, 50, 27, 37}, HzStairsTop},
		{"the hideout's landing", [4]int{40, 151, 46, 28}, HzHideoutStand}, {"the raft back", [4]int{40, 151, 7, 16}, HzRaftMouthStand},
	} {
		got := tile.Tile{X: c.packed[0]*64 + c.packed[2], Z: c.packed[1]*64 + c.packed[3]}
		if got != c.want {
			// The stairs' top is 2587,3237; the others are the plan's tile or its neighbour (the landing is a tile the cave sets the account on).
			t.Errorf("%s: the content says %v, the plan %v", c.what, got, c.want)
		}
	}
}

// The ids of the content's chat on the cult's side that the module may meet: nothing in the cult's talks is an Attack.
func TestHazeelContentTheCultSideNeverFights(t *testing.T) {
	for _, file := range []string{"clivet.rs2", "alomone.rs2", "hazeel_cultist.rs2"} {
		src := csContent(t, hzDir+"scripts/"+file)
		// Each retaliate is under a Carnillean-side or default branch: none is on a line that mentions the evil side.
		for i, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "npc_retaliate") && strings.Contains(line, "evilside") {
				t.Errorf("%s line %d retaliates on the cult's side: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
	// The Carnillean branch's retaliates: Alomone's at stage 4 and the default, the cultists' at 4 and after completion.
	if n := strings.Count(csContent(t, hzDir+"scripts/alomone.rs2"), "~npc_retaliate(0);"); n != 2 {
		t.Errorf("Alomone retaliates in %d places, the plan knows two (the Carnillean side's stage 4 and the default)", n)
	}
}
