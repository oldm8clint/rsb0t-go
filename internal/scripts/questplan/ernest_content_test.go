package questplan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, message and rule Ernest the Chicken's plan rests on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run ErnestContent ./internal/scripts/questplan/

const (
	ernestScript   = "scripts/quests/quest_haunted/scripts/quest_haunted.rs2"
	ernestJournal  = "scripts/quests/quest_haunted/scripts/haunted_journal.rs2"
	ernestLocs     = "scripts/quests/quest_haunted/configs/quest_haunted.loc"
	ernestObjs     = "scripts/quests/quest_haunted/configs/quest_haunted.obj"
	ernestVarps    = "scripts/quests/quest_haunted/configs/quest_haunted.varp"
	ernestConsts   = "scripts/quests/quest_haunted/configs/quest_haunted.constant"
	ernestVeronica = "scripts/areas/area_draynor/scripts/veronica.rs2"
	ernestProf     = "scripts/areas/area_draynor/scripts/professor_oddenstein.rs2"
)

// section is the text of a script block from its [header] to the next one.
func scriptBlock(src, header string) string {
	i := strings.Index(src, "["+header+"]")
	if i < 0 {
		return ""
	}
	rest := src[i+len(header)+2:]
	if j := strings.Index(rest, "\n["); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestErnestContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "veronica", NpcVeronica}, {"npc", npc, "professor_oddenstein", NpcOddenstein}, {"npc", npc, "ernest", NpcErnestMan},
		{"npc", npc, "questchicken", NpcErnestChicken}, {"npc", npc, "skeleton_unarmed", NpcManorSkeleton}, {"npc", npc, "nasty_tree", NpcManorTree},
		{"obj", obj, "pressure_gauge", ObjPressureGauge}, {"obj", obj, "fish_food", ObjFishFood}, {"obj", obj, "poison", ObjPoison},
		{"obj", obj, "poisoned_fish_food", ObjPoisonedFishFood}, {"obj", obj, "closet_key", ObjClosetKey}, {"obj", obj, "rubber_tube", ObjRubberTube},
		{"obj", obj, "oil_can", ObjOilCan}, {"obj", obj, "spade", ObjSpade}, {"obj", obj, "coins", CoinsID},
		{"loc", loc, "hauntedcompostheap", LocManorCompost}, {"loc", loc, "hauntedfountain", LocManorFountain}, {"loc", loc, "closet_door", LocClosetDoor},
		{"loc", loc, "hauntedbookcasel", LocManorBookcaseL}, {"loc", loc, "hauntedbookcaser", LocManorBookcaseR},
		{"loc", loc, "puzzle_ladder_top", LocPuzzleLadderDown}, {"loc", loc, "puzzle_ladder", LocPuzzleLadderUp}, {"loc", loc, "hauntedleverup", LocAlcoveLever},
		{"loc", loc, "levera", 146}, {"loc", loc, "leverb", 147}, {"loc", loc, "leverc", 148}, {"loc", loc, "leverd", 149}, {"loc", loc, "levere", 150},
		{"loc", loc, "leverf", 151}, {"loc", loc, "haunteddoorl", LocManorDoorL}, {"loc", loc, "haunteddoorr", 135}, {"loc", loc, "hauntedbackdoor", 136},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s: the pack says %d (present %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	for _, d := range MazeDoors {
		if got := loc[d.Name]; got != d.LocID {
			t.Errorf("door %s: the pack says loc %d, the table %d", d.Name, got, d.LocID)
		}
	}
	for _, l := range MazeLevers {
		if got := loc["lever"+strings.ToLower(string(rune(l.Name)))]; got != l.LocID {
			t.Errorf("lever %c: the pack says loc %d, the table %d", l.Name, got, l.LocID)
		}
	}
}

func TestErnestContentStagesAndReward(t *testing.T) {
	consts := csContent(t, ernestConsts)
	for name, want := range map[string]int{"haunted_not_started": ErnestNotStarted, "haunted_started": ErnestStarted, "haunted_spoken_to_oddenstein": ErnestSpokeOddenstein} {
		if !strings.Contains(consts, fmt.Sprintf("^%s = %d", name, want)) {
			t.Errorf("quest_haunted.constant does not say ^%s = %d", name, want)
		}
	}
	general := csContent(t, "scripts/general/configs/quest.constant")
	if !strings.Contains(general, fmt.Sprintf("^haunted_complete = %d", ErnestComplete)) {
		t.Errorf("quest.constant does not say ^haunted_complete = %d", ErnestComplete)
	}
	if !strings.Contains(general, fmt.Sprintf("^haunted_questpoints = %d", ErnestChickenRecord.QuestPoints)) {
		t.Errorf("quest.constant does not say ^haunted_questpoints = %d", ErnestChickenRecord.QuestPoints)
	}
	if LadderFacts[ErnestChickenID].QP != ErnestChickenRecord.QuestPoints {
		t.Errorf("the ladder row pays %d quest points, the record %d", LadderFacts[ErnestChickenID].QP, ErnestChickenRecord.QuestPoints)
	}
	// The varps are perm and not transmitted: the stage is the journal's to read, never the wire's.
	varps := csContent(t, ernestVarps)
	for _, name := range []string{"haunted", "ernestlever", "haunted_manor_fountain_poisoned"} {
		b := csConfigBlock(varps, name)
		if !strings.Contains(b, "scope=perm") || strings.Contains(b, "transmit") {
			t.Errorf("varp %s is not a plain perm varp: %q", name, b)
		}
	}
	script := csContent(t, ernestScript)
	if !regexp.MustCompile(`inv_add\(inv, coins, 300\)`).MatchString(script) {
		t.Error("the quest no longer pays 300 coins")
	}
	if strings.Contains(script, "stat_advance") {
		t.Error("the quest pays experience: the ladder row has none")
	}
	// The quest's whole tab entry: the name the quest tab is read by.
	if !strings.Contains(csContent(t, ernestJournal), `~quest_journal("Ernest the Chicken"`) {
		t.Error("the journal is not titled Ernest the Chicken")
	}
}

// The journal's four renderings contain the needles, and only the started stage has "He went into".
func TestErnestContentJournalNeedles(t *testing.T) {
	src := csContent(t, ernestJournal)
	norm := func(s string) string { return strings.ToLower(NormaliseLines([]string{s})) }
	branches := map[int]string{}
	// The four branches in order: not started, started, spoken to Oddenstein, else (complete).
	re := regexp.MustCompile(`(?s)\$text = "(.*?)";`)
	all := re.FindAllStringSubmatch(src, -1)
	if len(all) < 4 {
		t.Fatalf("%d journal texts in the script, want 4 or more", len(all))
	}
	for i, stage := range []int{ErnestNotStarted, ErnestStarted, ErnestSpokeOddenstein} {
		branches[stage] = norm(all[i][1])
	}
	branches[ErnestComplete] = norm(all[len(all)-1][1])
	for _, n := range ernestJournalNeedles {
		if !strings.Contains(branches[n.stage], n.needle) {
			t.Errorf("stage %d: the needle %q is not in its journal text %q", n.stage, n.needle, branches[n.stage])
		}
		for other, text := range branches {
			if other < n.stage && strings.Contains(text, n.needle) {
				t.Errorf("the needle %q of stage %d is in stage %d's text too: an earlier stage would read as the later one", n.needle, n.stage, other)
			}
		}
	}
	// And each branch reads back as its own stage through the parser.
	for stage, text := range branches {
		if got, ok := ParseErnestChickenJournal([]string{text}); !ok || got != stage {
			t.Errorf("stage %d reads as %d (ok %v)", stage, got, ok)
		}
	}
}

func TestErnestContentDialogue(t *testing.T) {
	ver, prof := csContent(t, ernestVeronica), csContent(t, ernestProf)
	for _, opt := range VeronicaStop.Prefer {
		if !strings.Contains(ver, `"`+opt+`"`) {
			t.Errorf("veronica.rs2 has no option %q", opt)
		}
	}
	for _, opt := range OddensteinStop.Prefer {
		if !strings.Contains(prof, `"`+opt+`"`) {
			t.Errorf("professor_oddenstein.rs2 has no option %q", opt)
		}
	}
	// Veronica's start is her first menu's first option, at the quest's stage 0 only.
	start := scriptBlock(ver, "label,haunted_start")
	if !strings.Contains(start, `~p_choice2("Aha, sounds like a quest. I'll help.", 1,`) || !strings.Contains(start, "%haunted = ^haunted_started") {
		t.Errorf("Veronica's start is not as the plan has it: %q", start)
	}
	// The Professor: stage 1 opens a three option menu whose first option leads to the two option menu, both of whose answers
	// reach oddenstein_not_easy (which sets stage 2); stage 2 has no menu and a hand-in that wants all three parts.
	looking := scriptBlock(prof, "label,oddenstein_looking")
	if !strings.Contains(looking, `~p_choice3("I'm looking for a guy called Ernest.", 1,`) ||
		!strings.Contains(looking, `@multi2("I'm glad Veronica didn't actually get engaged to a chicken.", oddenstein_chicken, "Change him back this instant!", oddenstein_change_back)`) {
		t.Errorf("the Professor's menus are not as the plan has them: %q", looking)
	}
	for _, label := range []string{"oddenstein_chicken", "oddenstein_change_back"} {
		if !strings.Contains(scriptBlock(prof, "label,"+label), "@oddenstein_not_easy") {
			t.Errorf("%s no longer reaches oddenstein_not_easy", label)
		}
	}
	if !strings.Contains(scriptBlock(prof, "label,oddenstein_not_easy"), "%haunted = ^haunted_spoken_to_oddenstein") {
		t.Error("oddenstein_not_easy no longer sets stage 2")
	}
	items := scriptBlock(prof, "label,oddenstein_items")
	if strings.Contains(items, "p_choice") || strings.Contains(items, "multi") {
		t.Error("the Professor's stage 2 conversation has a menu now")
	}
	for _, want := range []string{"inv_total(inv, pressure_gauge) > 0 & inv_total(inv, oil_can) > 0 & inv_total(inv, rubber_tube) > 0",
		"if_close;", "inv_del(inv, pressure_gauge, 1);", "inv_del(inv, oil_can, 1);", "inv_del(inv, rubber_tube, 1);", "%haunted = ^haunted_complete;"} {
		if !strings.Contains(items, want) {
			t.Errorf("the hand-in lost %q", want)
		}
	}
	// The hand-in closes the dialogue and plays a scene of 1, 2, 2 and 1 ticks' delay before Ernest speaks: the reason the
	// module drives it with a custom step.
	if n := strings.Count(items, "p_delay("); n != 4 {
		t.Errorf("the hand-in scene has %d delays, the module's comment says 4", n)
	}
	// Outside stages 1 and 2 the Professor's menu is the pre-quest one, whose options the plan never prefers.
	non := scriptBlock(prof, "label,oddenstein_nonquest")
	for _, opt := range OddensteinStop.Prefer {
		if strings.Contains(non, opt) {
			t.Errorf("the pre-quest menu offers %q: a talk at the wrong stage would pick it", opt)
		}
	}
}

func TestErnestContentThePartsAndWhereTheyAre(t *testing.T) {
	script := csContent(t, ernestScript)
	// The compost heap: its Search refuses, the spade digs, and the key comes only while the pack holds none.
	if !strings.Contains(scriptBlock(script, "oploc1,hauntedcompostheap"), "I'm not looking through that with my hands!") {
		t.Error("the compost heap's Search no longer refuses")
	}
	dig := scriptBlock(script, "oplocu,hauntedcompostheap")
	for _, want := range []string{"last_useitem ! spade", "inv_total(inv, closet_key) = 0", "inv_add(inv, closet_key, 1)"} {
		if !strings.Contains(dig, want) {
			t.Errorf("the compost heap's dig lost %q", want)
		}
	}
	// The closet door: locked to Open, opened by the key from either side (open_and_close_door2 decides the direction).
	if !strings.Contains(scriptBlock(script, "oploc1,closet_door"), "The door is locked.") ||
		!strings.Contains(scriptBlock(script, "oplocu,closet_door"), "last_useitem ! closet_key") ||
		!strings.Contains(scriptBlock(script, "label,ernest_open_key_door"), "open_and_close_door2") {
		t.Error("the closet door is not as the plan has it")
	}
	// The front doors refuse an account inside, the way out is the back door.
	if !strings.Contains(scriptBlock(script, "label,open_manor_entrance"), "The doors won't open.") {
		t.Error("the manor's front doors no longer refuse an account from inside")
	}
	// The fountain: the bite message, the poisoned branch and the pour.
	f := scriptBlock(script, "oploc1,hauntedfountain")
	for _, want := range []string{"%haunted_manor_fountain_poisoned = 1", "Something in the water bites you.", "inv_add(inv, pressure_gauge, 1)", "~damage_self(1)"} {
		if !strings.Contains(f, want) {
			t.Errorf("the fountain's search lost %q", want)
		}
	}
	pour := scriptBlock(script, "oplocu,hauntedfountain")
	for _, want := range []string{"last_useitem ! poisoned_fish_food", "... then die and float to the surface.", "%haunted_manor_fountain_poisoned = 1"} {
		if !strings.Contains(pour, want) {
			t.Errorf("the fountain's pour lost %q", want)
		}
	}
	if !strings.Contains(script, "[opheldu,poison]") || !strings.Contains(script, "[opheldu,fish_food]") || !strings.Contains(script, "inv_add(inv, poisoned_fish_food, 1)") {
		t.Error("poison and fish food no longer combine into the poisoned fish food")
	}
	// The bookcase, the alcove's lever and the ladders: where they send an account, and what resets the levers.
	if !strings.Contains(scriptBlock(script, "oploc1,_manor_bookcase"), "coordx(coord) < coordx(loc_coord)") {
		t.Error("the bookcases no longer refuse an account west of them")
	}
	locs := csContent(t, ernestLocs)
	for name, end := range map[string]string{"hauntedbookcasel": "0_48_52_24_30", "hauntedbookcaser": "0_48_52_24_31"} {
		if !strings.Contains(csConfigBlock(locs, name), "param=end_coord,"+end) {
			t.Errorf("%s no longer lands at %s", name, end)
		}
	}
	if !strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder_top"), "0_48_152_44_26") || !strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder"), "0_48_52_20_33") {
		t.Error("the ladders' landings moved")
	}
	// The levers reset on the way UP and not the way down (the TS def says otherwise), and on any teleport spell.
	if !strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder"), "~reset_haunted_levers") {
		t.Error("the ladder up no longer resets the levers")
	}
	if strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder_top"), "reset_haunted_levers") {
		t.Error("the ladder down resets the levers now: the module's reset cycle is no longer needed")
	}
	if !strings.Contains(csContent(t, "scripts/skill_magic/scripts/spells/teleport.rs2"), "~reset_haunted_levers") {
		t.Error("a teleport spell no longer resets the levers")
	}
	lever := scriptBlock(script, "oploc1,_haunted_lever")
	if !strings.Contains(lever, `mes("You pull lever <substring(loc_name, calc($name_idx - 1), $name_idx)> <$dir>.");`) {
		t.Errorf("the lever message changed: %q", lever)
	}
	if !strings.Contains(script, "This door is locked.") {
		t.Error("the locked door message changed")
	}
}

// The ground spawns the module walks to are the maps'.
func TestErnestContentSpawns(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		id   int
	}{
		{"poison", ErnestPoisonSpawn, ObjPoison}, {"fish food", ErnestFishFoodSpawn, ObjFishFood}, {"spade", ErnestSpadeSpawn, ObjSpade},
		{"rubber tube", ErnestTubeSpawn, ObjRubberTube}, {"oil can", ErnestOilCanSpawn, ObjOilCan},
	} {
		if !csHasRow(csMapRows(t, "OBJ", c.at), c.id) {
			t.Errorf("no %s (obj %d) spawns at %v", c.name, c.id, c.at)
		}
	}
	for _, c := range []struct {
		name string
		at   tile.Tile
		id   int
	}{
		{"Veronica", VeronicaAnchor, NpcVeronica}, {"Professor Oddenstein", OddensteinAnchor, NpcOddenstein},
		{"the closet skeleton", tile.Tile{X: 3110, Z: 3367}, NpcManorSkeleton},
		{"the chicken", tile.Tile{X: 3110, Z: 3366, Level: 2}, NpcErnestChicken},
	} {
		if !csHasRow(csMapRows(t, "NPC", c.at), c.id) {
			t.Errorf("no %s (npc %d) spawns at %v", c.name, c.id, c.at)
		}
	}
	// The garden's trees: fourteen nasty trees in the manor's two squares, ten of them in the garden between Veronica and the
	// front door (the rectangle the walks cross), every one on level 0.
	trees, garden := 0, 0
	for _, sq := range [][2]int{{48, 52}, {49, 52}} {
		src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", sq[0], sq[1]))
		in := false
		for _, line := range strings.Split(src, "\n") {
			if strings.HasPrefix(line, "==== ") {
				in = line == "==== NPC ===="
				continue
			}
			head, tail, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !in || !ok || strings.TrimSpace(tail) != strconv.Itoa(NpcManorTree) {
				continue
			}
			var lvl, lx, lz int
			if n, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); n != 3 {
				continue
			}
			x, z := sq[0]*64+lx, sq[1]*64+lz
			trees++
			if lvl != 0 {
				t.Errorf("a nasty tree on level %d at %d,%d", lvl, x, z)
			}
			if x >= 3100 && x <= 3125 && z >= 3330 && z <= 3350 {
				garden++
			}
		}
	}
	if trees != 14 || garden != 10 {
		t.Errorf("%d nasty trees in the manor's squares, %d in the garden; the plan's comments say 14 and 10", trees, garden)
	}
	// The locs the legs operate sit where the plan puts them.
	for _, c := range []struct {
		name string
		at   tile.Tile
		id   int
	}{
		{"compost heap", tile.Tile{X: 3084, Z: 3360}, LocManorCompost}, {"fountain", tile.Tile{X: 3087, Z: 3334}, LocManorFountain},
		{"closet door", tile.Tile{X: 3107, Z: 3367}, LocClosetDoor}, {"alcove lever", ErnestAlcoveLever, LocAlcoveLever},
		{"bookcase L", tile.Tile{X: 3097, Z: 3358}, LocManorBookcaseL}, {"bookcase R", tile.Tile{X: 3097, Z: 3359}, LocManorBookcaseR},
		{"ladder down", tile.Tile{X: 3092, Z: 3362}, LocPuzzleLadderDown}, {"ladder up", tile.Tile{X: 3117, Z: 9754}, LocPuzzleLadderUp},
		{"front door L", tile.Tile{X: 3108, Z: 3353}, LocManorDoorL}, {"front door R", tile.Tile{X: 3109, Z: 3353}, 135},
		{"back door", tile.Tile{X: 3123, Z: 3360}, 136},
	} {
		if !csHasRow(csMapRows(t, "LOC", c.at), c.id) {
			t.Errorf("no %s (loc %d) at %v", c.name, c.id, c.at)
		}
	}
	for _, l := range MazeLevers {
		if !csHasRow(csMapRows(t, "LOC", l.Tile), l.LocID) {
			t.Errorf("lever %c (loc %d) is not at %v", l.Name, l.LocID, l.Tile)
		}
	}
	for _, d := range MazeDoors {
		if !csHasRow(csMapRows(t, "LOC", d.Near), d.LocID) {
			t.Errorf("door %s (loc %d) is not at %v", d.Name, d.LocID, d.Near)
		}
	}
}

func TestErnestContentItemsAndTheSkeleton(t *testing.T) {
	objs := csContent(t, ernestObjs)
	for name, untradeable := range map[string]bool{"pressure_gauge": true, "fish_food": false, "poison": false, "poisoned_fish_food": true,
		"closet_key": true, "rubber_tube": true, "oil_can": true} {
		if got := strings.Contains(csConfigBlock(objs, name), "tradeable=no"); got != untradeable {
			t.Errorf("%s untradeable in the content: %v, the kit says %v", name, got, untradeable)
		}
	}
	// The kit's Untradeable flags are those.
	untrade := map[int]bool{}
	for _, l := range ernestKit(Snapshot{InvIDs: map[int]int{}}) {
		untrade[l.ID] = l.Untradeable
	}
	for id, want := range map[int]bool{ObjPressureGauge: true, ObjPoisonedFishFood: true, ObjClosetKey: true, ObjRubberTube: true, ObjOilCan: true,
		ObjFishFood: false, ObjPoison: false, ObjSpade: false} {
		if got, ok := untrade[id]; !ok || got != want {
			t.Errorf("kit line %d: untradeable %v (present %v), want %v", id, got, ok, want)
		}
	}
	// The closet skeleton hunts like the module says: cowardly, range 1, level 25.
	npcs := npcBlocks(t, contentRoot(t))
	sk := npcs["skeleton_unarmed"]
	if sk == nil || sk["huntmode"] != "cowardly" || sk["huntrange"] != "1" || sk["vislevel"] != strconv.Itoa(ErnestSkeletonLevel) {
		t.Errorf("the skeleton's block is not as the plan has it: %v", sk)
	}
	tree := npcs["nasty_tree"]
	if tree == nil || tree["huntmode"] != "aggressive_melee" || tree["huntrange"] != "1" || tree["moverestrict"] != "nomove" {
		t.Errorf("the garden tree's block is not as the plan has it: %v", tree)
	}
	// The rule "cowardly skips a player above twice its level" is the engine's.
	hunt := csContent(t, "scripts/_unpack/225/all.hunt")
	if !strings.Contains(csConfigBlock(hunt, "cowardly"), "check_nottoostrong=outside_wilderness") {
		t.Error("cowardly hunting no longer skips a player that is too strong")
	}
	if got, want := FoeManorSkeleton.MaxHit(), ErnestSkeletonPeakHit; got != want {
		t.Errorf("the skeleton's maximum hit is %d, the plan says %d", got, want)
	}
}

// ---- the door script, evaluated ----

// scriptCond is a tiny evaluator for the boolean expression of [oploc1,_haunted_door]: testbit(%ernestlever, N) = ^true/^false,
// loc_type = name, & and | and parentheses, nothing else.
type scriptCond struct {
	toks []string
	i    int
	door string
	bits uint8
}

var scriptTok = regexp.MustCompile(`\(|\)|&|\||=|,|%ernestlever|\^true|\^false|[A-Za-z0-9_]+`)

func (c *scriptCond) peek() string {
	if c.i < len(c.toks) {
		return c.toks[c.i]
	}
	return ""
}

func (c *scriptCond) next() string { s := c.peek(); c.i++; return s }

func (c *scriptCond) expect(s string) {
	if got := c.next(); got != s {
		panic(fmt.Sprintf("script condition: want %q, got %q at token %d", s, got, c.i-1))
	}
}

func (c *scriptCond) or() bool {
	v := c.and()
	for c.peek() == "|" {
		c.next()
		r := c.and()
		v = v || r
	}
	return v
}

func (c *scriptCond) and() bool {
	v := c.factor()
	for c.peek() == "&" {
		c.next()
		r := c.factor()
		v = v && r
	}
	return v
}

func (c *scriptCond) factor() bool {
	if c.peek() == "(" {
		c.next()
		v := c.or()
		c.expect(")")
		return v
	}
	switch c.next() {
	case "loc_type":
		c.expect("=")
		return c.next() == c.door
	case "testbit":
		c.expect("(")
		c.expect("%ernestlever")
		c.expect(",")
		n, err := strconv.Atoi(c.next())
		if err != nil {
			panic(err)
		}
		c.expect(")")
		c.expect("=")
		want := c.next() == "^true"
		return (c.bits>>uint(n)&1 == 1) == want
	}
	panic(fmt.Sprintf("script condition: unexpected token %q", c.toks[c.i-1]))
}

// Every door opens for exactly the lever states the content's own condition says, over all 64 states: the table is the script.
func TestErnestContentDoorRulesAreTheScriptsCondition(t *testing.T) {
	script := csContent(t, ernestScript)
	block := scriptBlock(script, "oploc1,_haunted_door")
	start := strings.Index(block, "if(")
	end := strings.Index(block, ") {")
	if start < 0 || end < start {
		t.Fatalf("cannot find the door condition in %q", block)
	}
	cond := block[start+len("if(") : end]
	toks := scriptTok.FindAllString(cond, -1)
	for _, d := range MazeDoors {
		for bits := uint8(0); bits < 64; bits++ {
			c := &scriptCond{toks: toks, door: d.Name, bits: bits}
			got := c.or()
			if c.i != len(c.toks) {
				t.Fatalf("door %s: the condition parser stopped at token %d of %d (%q)", d.Name, c.i, len(c.toks), c.peek())
			}
			if got != d.Opens(bits) {
				t.Errorf("door %s with levers %s: the script says %v, the table %v", d.Name, MazeBitsString(bits), got, d.Opens(bits))
			}
		}
	}
	// The condition names exactly the nine doors.
	seen := map[string]bool{}
	for i := 0; i+2 < len(toks); i++ {
		if toks[i] == "loc_type" {
			seen[toks[i+2]] = true
		}
	}
	if len(seen) != len(MazeDoors) {
		t.Errorf("the script's condition names %d doors, the table has %d", len(seen), len(MazeDoors))
	}
}

// Every NPC the quest's places hold, checked against the content: the manor and its grounds (x 3060 to 3140, z 3320 to 3392 of maps
// m48_52 and m49_52) and its basement (m48_152) hold exactly the hostiles the plan knows about, and nothing else with a hunt mode.
// A new aggressive spawn would be one the module's HP floors, no-retaliate rule and fight choices were not written for. (The walks
// to and from the banks cross Draynor Village, whose jail guards the walker's danger zone keeps clear of.)
func TestErnestContentTheManorsOnlyHostilesAreTheOnesThePlanKnows(t *testing.T) {
	root := contentRoot(t)
	blocks := npcBlocks(t, root)
	names := packNames(t, root+"/pack/npc.pack")
	// type name -> why the plan lives with it.
	known := map[string]string{
		"nasty_tree":       "the garden's trees: aggressive_melee, huntrange 1, a hit of 3: no route avoids them, the quest does not retaliate",
		"skeleton_unarmed": "the closet's skeleton: cowardly (combat 50 and under), huntrange 1, level 25: fought with the shared melee loop, HP floor first",
		"giantspider1":     "two giant spiders, level 2 and cowardly: they hunt only accounts of combat 4 and under, which the ladder never runs",
	}
	seen := map[string]int{}
	for _, sq := range [][2]int{{48, 52}, {49, 52}, {48, 152}} {
		src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", sq[0], sq[1]))
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
			// The manor and its grounds: the walks never go east of the garden's last tree or north of the manor's wall.
			if x, z := sq[0]*64+lx, sq[1]*64+lz; sq[1] == 52 && (x < 3060 || x > 3140 || z < 3320 || z > 3392) {
				continue
			}
			seen[names[id]]++
		}
	}
	if len(seen) == 0 {
		t.Fatal("no npc spawns read")
	}
	for name, n := range seen {
		cfg := blocks[name]
		mode := cfg["huntmode"]
		if mode == "" || mode == "none" {
			continue
		}
		if _, ok := known[name]; !ok {
			t.Errorf("%d x %s in the quest's squares hunts (huntmode %s, vislevel %s, huntrange %s): the plan does not know it", n, name, mode, cfg["vislevel"], cfg["huntrange"])
		}
	}
	for name := range known {
		if seen[name] == 0 {
			t.Errorf("the plan lives with %s, which no longer spawns in the squares", name)
		}
	}
	// The passive ones the route passes: ghosts, a witch, rats, spiders, ducks. None has a hunt mode.
	for _, name := range []string{"house_ghost", "witch1", "rat", "spider", "duck"} {
		if seen[name] == 0 {
			continue
		}
		if mode := blocks[name]["huntmode"]; mode != "" && mode != "none" {
			t.Errorf("%s hunts now (huntmode %s)", name, mode)
		}
	}
}

// What the search for the way out relies on, read from the script: an Inspect (op 2) answers the lever's state and changes
// nothing, a pull answers the state it left (so its message names the bit afterwards), the levers are cleared by the ladder up and
// by a teleport and by nothing else in the content, and the way down touches nothing.
func TestErnestContentWhatALeverAnswersAndWhatClearsTheLevers(t *testing.T) {
	script := csContent(t, ernestScript)
	inspect := scriptBlock(script, "oploc2,_haunted_lever")
	if !strings.Contains(inspect, `def_string $dir = "up";`) || !strings.Contains(inspect, `$dir = "down";`) || !strings.Contains(inspect, `mes("The lever is <$dir>.");`) {
		t.Errorf("the lever's Inspect no longer answers \"The lever is up/down.\": %q", inspect)
	}
	if strings.Contains(inspect, "togglebit") || strings.Contains(inspect, "%ernestlever =") {
		t.Errorf("the lever's Inspect changes the levers: %q", inspect)
	}
	if got, ok := MazeLeverInspected("The lever is down."); !ok || !got {
		t.Errorf("MazeLeverInspected(down) = %v %v", got, ok)
	}
	if got, ok := MazeLeverInspected("The lever is up."); !ok || got {
		t.Errorf("MazeLeverInspected(up) = %v %v", got, ok)
	}
	pull := scriptBlock(script, "oploc1,_haunted_lever")
	if !strings.Contains(pull, "togglebit(%ernestlever, loc_param(lever_index))") || !strings.Contains(pull, `mes("You pull lever `) {
		t.Errorf("the lever's pull is not a toggle that names the lever: %q", pull)
	}
	if !strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder"), "~reset_haunted_levers;") {
		t.Error("the ladder up no longer resets the levers")
	}
	if strings.Contains(scriptBlock(script, "oploc1,puzzle_ladder_top"), "reset_haunted_levers") {
		t.Error("the way down resets the levers: the module's reset cycle is no longer needed")
	}
	// Every script that calls the reset, content-wide: the ladder up and the teleports' post-checks only.
	root := os.Getenv("RSB0T_CONTENT")
	var callers []string
	_ = filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".rs2") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(b), "~reset_haunted_levers") {
			rel, _ := filepath.Rel(root, path)
			callers = append(callers, filepath.ToSlash(rel))
		}
		return nil
	})
	want := map[string]bool{ernestScript: true, "scripts/skill_magic/scripts/spells/teleport.rs2": true}
	for _, c := range callers {
		if !want[c] {
			t.Errorf("%s also resets the levers: a move outside the puzzle can put an account in a state it did not believe", c)
		}
		delete(want, c)
	}
	for missing := range want {
		t.Errorf("%s no longer resets the levers", missing)
	}
}
