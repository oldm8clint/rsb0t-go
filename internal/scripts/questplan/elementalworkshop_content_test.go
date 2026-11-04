package questplan

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, rule, message and number the Elemental Workshop rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run ElementalWorkshopContent ./internal/scripts/questplan/
//
// (What the loc index the fleet runs on holds, and the stands of each loc, is pinned in the questmods real-data tests.)

const ewDir = "scripts/quests/quest_elemental_workshop/"

// ewScripts is every script and config of the quest in one string, for the messages the module reads.
func ewScripts(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, f := range []string{"scripts/quest_elemental_workshop.rs2", "scripts/elemental_workshop_shield_book.rs2", "scripts/elemental_earth.rs2", "scripts/elemental_workshop_journal.rs2"} {
		b.WriteString(csContent(t, ewDir+f))
		b.WriteString("\n")
	}
	return b.String()
}

// The pack ids the plan carries are the pack's.
func TestElementalWorkshopContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "earth_elemental_rock_version", NpcEwEarthRock},
		{"obj", obj, "elemental_workshop_shield_book", ObjEwBook}, {"obj", obj, "elemental_workshop_key", ObjEwKey}, {"obj", obj, "elemental_workshop_lava_bowl", ObjEwBowl},
		{"obj", obj, "elemental_workshop_lava_bowl_full", ObjEwBowlFull}, {"obj", obj, "elemental_shield", ObjEwShield}, {"obj", obj, "elemental_workshop_ore", ObjEwOre},
		{"obj", obj, "elemental_workshop_bar", ObjEwBar}, {"obj", obj, "needle", ObjEwNeedle}, {"obj", obj, "thread", ObjEwThread}, {"obj", obj, "leather", ObjEwLeather},
		{"obj", obj, "coal", ObjEwCoal}, {"obj", obj, "hammer", ObjHammer}, {"obj", obj, "knife", ObjKnife}, {"obj", obj, "bronze_pickaxe", ObjBronzePickaxe},
		{"loc", loc, "elemental_workshop_bookcase", LocEwBookcase}, {"loc", loc, "elemental_workshop_oddwall_r", LocEwWallRight},
		{"loc", loc, "elemental_workshop_oddwall_l", LocEwWallLeft}, {"loc", loc, "elemental_workshop_spiralstairstop", LocEwStairsDown},
		{"loc", loc, "elemental_workshop_spiralstairs", LocEwStairsUp}, {"loc", loc, "elemental_workshop_box_1", LocEwBoxBowl},
		{"loc", loc, "elemental_workshop_box_2", LocEwBoxNeedle}, {"loc", loc, "elemental_workshop_box_4", LocEwBoxLeather},
		{"loc", loc, "elemental_workshop_workbench", LocEwWorkbench}, {"loc", loc, "elemental_workshop_rock", LocEwRock},
		{"loc", loc, "elemental_workshop_valve_1", LocEwValveFirst}, {"loc", loc, "elemental_workshop_valve_2", LocEwValveSecond},
		{"loc", loc, "elemental_workshop_water_lever", LocEwWaterLever}, {"loc", loc, "elemental_workshop_air_lever", LocEwAirLever},
		{"loc", loc, "elemental_workshop_bellows_l", LocEwBellowsL}, {"loc", loc, "elemental_workshop_bellows_r", LocEwBellowsR},
		{"loc", loc, "elemental_workshop_furnace", LocEwFurnace}, {"loc", loc, "elemental_workshop_trough", LocEwTrough},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	for i, name := range []string{"elemental_fire", "elemental_earth", "elemental_air", "elemental_water"} {
		if npc[name] != 1019+i {
			t.Errorf("npc %s is %d, want %d", name, npc[name], 1019+i)
		}
	}
	for _, id := range pickaxeIDs() {
		found := false
		for _, v := range obj {
			found = found || v == id
		}
		if !found {
			t.Errorf("pickaxe %d is in no pack entry", id)
		}
	}
}

func pickaxeIDs() []int {
	var out []int
	for _, p := range doricPickaxes {
		out = append(out, p.ID)
	}
	return out
}

// The stages' constants, the quest point and the reward.
func TestElementalWorkshopContentRewardsAndBits(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^elemental_workshop_complete = 20", "^elemental_workshop_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, ewDir+"configs/quest_elemental_workshop.constant")
	for _, want := range []string{"^elemental_workshop_read_book = 1", "^elemental_workshop_slashed_book = 2", "^elemental_workshop_water_controls_left = 3",
		"^elemental_workshop_water_controls_right = 4", "^elemental_workshop_water_flowing = 5", "^elemental_workshop_bellows_repaired = 6",
		"^elemental_workshop_furnace_lit = 7", "^elemental_workshop_air_blowing = 9", "^elemental_workshop_entered = 13", "^elemental_workshop_leather_found = 14",
		"^elemental_workshop_needle_found = 15"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_elemental_workshop.constant lacks %q", want)
		}
	}
	src := csContent(t, ewDir+"scripts/quest_elemental_workshop.rs2")
	for _, want := range []string{
		// The reward: 5,000 Crafting and 5,000 Smithing (1x), the bar's 8 and the shield's 20 Smithing.
		"stat_advance(crafting, 50000);", "stat_advance(smithing, 50000);", "stat_advance(smithing, 80);", "stat_advance(smithing, 200);",
		"if(testbit(%elemental_workshop_bits, ^elemental_workshop_complete) = ^false) queue(elemental_workshop_quest_complete, 0, 0);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_elemental_workshop.rs2 lacks %q", want)
		}
	}
	// The row of the ladder says what the script pays.
	if f := LadderFacts[ElementalWorkshopID]; f.XP["crafting"] != 5000 || f.XP["smithing"] != 5000+8+20 {
		t.Errorf("the ladder row pays %v", f.XP)
	}
}

// The machine's rules the plan orders its steps by.
func TestElementalWorkshopContentTheMachine(t *testing.T) {
	src := csContent(t, ewDir+"scripts/quest_elemental_workshop.rs2")
	for _, want := range []string{
		// The book, the key, the wall and the stairs.
		"[oploc1,elemental_workshop_bookcase]", "inv_add(inv, elemental_workshop_shield_book, 1);", "[oplocu,elemental_workshop_oddwall_l] @use_elemental_workshop_wall(^left);",
		"if(last_useitem = elemental_workshop_key) {", "You see a small hole in the wall but no way to open it.", "if(~check_axis_locactive(coord) = false) {",
		"p_teleport(0_42_154_28_32);", "%elemental_workshop_bits = setbit(%elemental_workshop_bits, ^elemental_workshop_entered);", "p_teleport(0_42_54_21_41);",
		// The crates: the bowl whenever the pack holds none, the needle and the leather once per account.
		"[oploc1,elemental_workshop_box_1]", "inv_total(inv, elemental_workshop_lava_bowl) = 0", "inv_total(inv, needle) > 0 | testbit(%elemental_workshop_bits, ^elemental_workshop_needle_found) = ^true",
		"inv_total(inv, leather) > 0 | testbit(%elemental_workshop_bits, ^elemental_workshop_leather_found) = ^true",
		// The valves: the first toggles the right bit only while the left is off, the second the left; the lever needs both and otherwise resets.
		"if(testbit(%elemental_workshop_bits, ^elemental_workshop_water_controls_left) = ^false) { // cant toggle this side if left side is on",
		"%elemental_workshop_bits = togglebit(%elemental_workshop_bits, ^elemental_workshop_water_controls_right);",
		"%elemental_workshop_bits = togglebit(%elemental_workshop_bits, ^elemental_workshop_water_controls_left);",
		"if(getbit_range(%elemental_workshop_bits, ^elemental_workshop_water_controls_left, ^elemental_workshop_water_controls_right) = 3) {",
		"%elemental_workshop_bits = clearbit_range(%elemental_workshop_bits, ^elemental_workshop_water_controls_left, ^elemental_workshop_water_controls_right);",
		// The air: needs the wheel and the bellows.
		"if(getbit_range(%elemental_workshop_bits, ^elemental_workshop_water_flowing, ^elemental_workshop_bellows_repaired) = 3) {",
		// The bellows: Crafting 20, a needle, a leather and a thread, the last two used up.
		"if(stat(crafting) < 20) {", "inv_total(inv, needle) = 0 | inv_total(inv, leather) = 0 | inv_total(inv, thread) = 0", "inv_del(inv, thread, 1);", "inv_del(inv, leather, 1);",
		// The rock: Mining 20 and a usable pickaxe; the elemental is added ON the account's coord and set on it.
		"if(stat(mining) < 20) {", "def_obj $pickaxe = ~pickaxe_checker;", "npc_add(coord, earth_elemental_rock_version, 500);", "~npc_retaliate(0);",
		// The furnace: lit by the full bowl; the ore needs the air and four coal.
		"if(last_useitem = elemental_workshop_lava_bowl_full) {", "if(testbit(%elemental_workshop_bits, ^elemental_workshop_air_blowing) = ^false) {",
		"if(inv_total(inv, coal) < 4) {", "inv_del(inv, coal, 4);", "inv_add(inv, elemental_workshop_bar, 1);",
		// The workbench: a hammer, Smithing 20 and the book.
		"if(inv_total(inv, hammer) = 0) {", "if(stat(smithing) < 20) {", "if(inv_total(inv, elemental_workshop_shield_book) = 0) {", "inv_add(inv, elemental_shield, 1);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_elemental_workshop.rs2 lacks %q", want)
		}
	}
	// The rock's elemental drops the ore for the hero; the standing elementals drop none.
	earth := csContent(t, ewDir+"scripts/elemental_earth.rs2")
	rockDrop := earth[strings.Index(earth, "[ai_queue3,earth_elemental_rock_version]"):strings.Index(earth, "[ai_queue3,elemental_earth]")]
	if !strings.Contains(rockDrop, "obj_add(npc_coord, elemental_workshop_ore, 1, ^lootdrop_duration);") || !strings.Contains(rockDrop, "if (npc_findhero = ^false) {") {
		t.Errorf("the rock elemental's death script is %q", rockDrop)
	}
	if strings.Contains(earth[strings.Index(earth, "[ai_queue3,elemental_earth]"):], "elemental_workshop_ore") {
		t.Error("the standing earth elemental drops the ore")
	}
}

// The messages the module reads are the content's: each fragment is in the scripts, and in the module's glue.
func TestElementalWorkshopContentTheMessagesTheGlueReads(t *testing.T) {
	scripts := strings.ToLower(ewScripts(t))
	glueBytes, err := os.ReadFile("../questmods/workshop.go")
	if err != nil {
		t.Skipf("no glue source: %v", err)
	}
	glue := strings.ToLower(strings.ReplaceAll(string(glueBytes), "\r\n", "\n"))
	for _, frag := range []string{
		"you turn the handle", "locked off", "water wheel starting up", "coming to a standstill", "flow gates resetting",
		"you stitch the leather", "already look fixed", "crafting level of 20", "piece of leather",
		"bursts in to life", "makes little difference", "bellows pump air", "bellows stop pumping", "nothing happens",
		"needs to be hotter", "four heaps of coal", "cold and useless", "it's empty", "nothing of interest", "enough room", "don't want to damage",
	} {
		if !strings.Contains(scripts, frag) {
			t.Errorf("the content says nothing like %q", frag)
		}
		if !strings.Contains(glue, `"`+frag+`"`) {
			t.Errorf("the glue (questmods/workshop.go) does not read %q", frag)
		}
	}
	// The messages the glue sends its waits by are the plan's: nothing else in the glue's cookSaw calls is unlisted.
	for _, m := range regexp.MustCompile(`cookSaw\(d, mark, ([^)]*)\)`).FindAllStringSubmatch(string(glueBytes), -1) {
		for _, lit := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
			if !strings.Contains(scripts, strings.ToLower(lit[1])) {
				t.Errorf("the glue reads %q, which is in no script of the quest", lit[1])
			}
		}
	}
}

// Only the rock's elemental is fought: none of the workshop's other npcs has a hunt mode, and the rock's has the stats the foe table says.
func TestElementalWorkshopContentOnlyTheRocksElementalFights(t *testing.T) {
	blocks := npcBlocks(t, contentRoot(t))
	for _, name := range []string{"elemental_fire", "elemental_earth", "elemental_air", "elemental_water", "earth_elemental_rock_version"} {
		b := blocks[name]
		if b == nil {
			t.Errorf("no [%s] block", name)
			continue
		}
		if mode, hunts := b["huntmode"]; hunts {
			t.Errorf("[%s] has huntmode=%s: the workshop would not be a safe place to walk in", name, mode)
		}
		if b["op2"] != "Attack" {
			t.Errorf("[%s] op2 is %q", name, b["op2"])
		}
	}
	rock := blocks["earth_elemental_rock_version"]
	if rock["vislevel"] != "35" || rock["hitpoints"] != "35" || rock["param:attackrate"] != "6" || rock["param:death_drop"] != "swamprocks1" {
		t.Errorf("the rock's elemental is %v", rock)
	}
	// The foe table is the content's (TestQuestFoesMatchTheContent pins every stat): the plan's own pins.
	if FoeEarthElementalRock.Hitpoints != 35 || FoeEarthElementalRock.Defence != 35 || FoeEarthElementalRock.Attack != 20 || FoeEarthElementalRock.Strength != 35 ||
		FoeEarthElementalRock.AttackTicks != 6 || FoeEarthElementalRock.StrengthBonus != 0 || FoeEarthElementalRock.AttackBonus != 0 {
		t.Errorf("the foe is %+v", FoeEarthElementalRock)
	}
}

// The journal's markers are in the journal script, each in the branch the parser reads it in.
func TestElementalWorkshopContentTheJournalMarkers(t *testing.T) {
	src := csContent(t, ewDir+"scripts/elemental_workshop_journal.rs2")
	literal := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	norm := func(s string) string { return NormaliseLines(strings.Split(s, "|")) }
	// occursIn reports a string of the journal script, between the offsets from and to, that holds the marker (a page repeats its earlier
	// paragraphs, so the same marker is in several branches; the parser's order is what tells them apart).
	occursIn := func(marker string, from, to int) bool {
		for _, loc := range literal.FindAllStringSubmatchIndex(src, -1) {
			if loc[0] >= from && loc[0] < to && strings.Contains(norm(src[loc[2]:loc[3]]), marker) {
				return true
			}
		}
		return false
	}
	entered := strings.Index(src, "else if (testbit(%elemental_workshop_bits, ^elemental_workshop_entered) = ^true) {")
	slashed := strings.Index(src, "} else if (testbit(%elemental_workshop_bits, ^elemental_workshop_slashed_book) = ^true) {")
	read := strings.Index(src, "} else if (testbit(%elemental_workshop_bits, ^elemental_workshop_read_book) = ^true) {")
	fresh := strings.Index(src, "} else if (testbit(%elemental_workshop_bits, ^elemental_workshop_read_book) = ^false) {")
	if entered < 0 || slashed < entered || read < slashed || fresh < read {
		t.Fatalf("the journal's branches are at %d, %d, %d, %d", entered, slashed, read, fresh)
	}
	for _, c := range []struct {
		marker   string
		from, to int
	}{
		{"secret door in the seers village smithy", entered, slashed}, {"cutting open the spine of the book", slashed, read},
		{"where is this workshop and how do i get in", slashed, fresh}, {"where is this workshop and how do i get in", read, fresh},
		{"i can start this quest by reading a", fresh, len(src)},
	} {
		if !occursIn(c.marker, c.from, c.to) {
			t.Errorf("marker %q is in no string of its branch %d..%d", c.marker, c.from, c.to)
		}
	}
	// The newer markers are NOT in the older branches (the parser tests them first and relies on it).
	if occursIn("secret door in the seers village smithy", slashed, len(src)) || occursIn("cutting open the spine of the book", read, len(src)) {
		t.Error("a newer stage's marker is in an older branch")
	}
	for _, m := range ewFlagMarkers {
		if !occursIn(m.text, entered, slashed) || occursIn(m.text, slashed, len(src)) {
			t.Errorf("flag marker %q is not confined to the entered branch %d..%d", m.text, entered, slashed)
		}
	}
	// "quest complete!" is the first branch.
	if !occursIn("quest complete!", 0, entered) {
		t.Errorf("quest complete! is not before the entered branch at %d", entered)
	}
}

// The tiles of the plan are the maps': the workshop's locs and npcs, the smithy's wall and stairs, the bookcase, and the teleports' targets.
func TestElementalWorkshopContentTheMapsAgreeWithThePlan(t *testing.T) {
	workshopLocs := jm2Entries(t, 42, 154, "LOC")
	workshopNpcs := jm2Entries(t, 42, 154, "NPC")
	seersLocs := jm2Entries(t, 42, 54, "LOC")
	for _, c := range []struct {
		what string
		es   []jm2Entry
		id   int
		at   tile.Tile
	}{
		{"the water lever", workshopLocs, LocEwWaterLever, EwWaterLeverAt}, {"the air lever", workshopLocs, LocEwAirLever, EwAirLeverAt},
		{"the first valve", workshopLocs, LocEwValveFirst, EwValveFirstAt}, {"the second valve", workshopLocs, LocEwValveSecond, EwValveSecondAt},
		{"the bellows", workshopLocs, LocEwBellowsL, EwBellowsAt}, {"the furnace", workshopLocs, LocEwFurnace, EwFurnaceAt},
		{"the trough", workshopLocs, LocEwTrough, EwTroughAt}, {"the workbench", workshopLocs, LocEwWorkbench, EwWorkbenchAt}, {"the rock", workshopLocs, LocEwRock, EwRockAt},
		{"the bowl's crate", workshopLocs, LocEwBoxBowl, EwBoxBowlAt}, {"the needle's crate", workshopLocs, LocEwBoxNeedle, EwBoxNeedleAt},
		{"the leather's crate", workshopLocs, LocEwBoxLeather, EwBoxLeatherAt}, {"the stairs up", workshopLocs, LocEwStairsUp, EwStairsUpAt},
		{"the bookcase", seersLocs, LocEwBookcase, EwBookcaseAt}, {"the wall's east half", seersLocs, LocEwWallRight, EwWallAt},
		{"the wall's west half", seersLocs, LocEwWallLeft, tile.Tile{X: 2709, Z: 3495}}, {"the stairs down", seersLocs, LocEwStairsDown, EwStairsDownAt},
	} {
		if !jm2Has(c.es, c.id, c.at) {
			t.Errorf("%s: no %d at %v in the map", c.what, c.id, c.at)
		}
	}
	// The standing elementals: 5 earth, 5 fire (two kinds of spawn), none of the rock's kind (it is added by the Mine op alone).
	count := map[int]int{}
	for _, e := range workshopNpcs {
		count[e.ID]++
	}
	if count[NpcEwEarthRock] != 0 || count[1020] == 0 {
		t.Errorf("the map's npcs: %v", count)
	}
	// The teleports' targets: the stairs down land in the workshop, the stairs up in the secret room.
	if got := (tile.Tile{X: 42*64 + 28, Z: 154*64 + 32}); got != EwArrive {
		t.Errorf("the stairs down land at %v, the plan says %v", got, EwArrive)
	}
	if got := (tile.Tile{X: 42*64 + 21, Z: 54*64 + 41}); got != EwSecretArrive {
		t.Errorf("the stairs up land at %v, the plan says %v", got, EwSecretArrive)
	}
	if !jm2Has(workshopLocs, LocEwStairsUp, EwStairsUpAt) {
		t.Error("the workshop has no stairs up where the plan puts them")
	}
	_ = fmt.Sprint
}
