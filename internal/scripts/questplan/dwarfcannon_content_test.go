package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, bit, roll, reward, spawn and hunting rule Dwarf Cannon rests on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run DwarfCannonContent ./internal/scripts/questplan/
//
// (The loc index the fleet runs on is baked from the production maps, which hold locs this checkout's maps do not, such as the tower's
// ladders; those are pinned in the questmods real-data tests.)

const cannonDir = "scripts/quests/quest_mcannon/"

// The pack ids the plan carries are the pack's.
func TestDwarfCannonContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "lawgof2", NpcCannonCommander}, {"npc", npc, "nulodion", NpcNulodion}, {"npc", npc, "dwarfchildtw1", NpcCannonChild},
		{"npc", npc, "mcannonguard", NpcCannonGuard}, {"npc", npc, "bat", NpcGiantBat},
		{"obj", obj, "mcannonremains", ObjCannonRemains}, {"obj", obj, "mcannontoolkit", ObjCannonToolkit}, {"obj", obj, "nulodions_notes", ObjCannonNotes},
		{"obj", obj, "ammo_mould", ObjCannonMould}, {"obj", obj, "mcannonrailing1_obj", ObjCannonRailing}, {"obj", obj, "shark", ObjShark},
		{"loc", loc, "mcannoncrateboy", LocCannonCrate}, {"loc", loc, "mcannoncave", LocCannonCave}, {"loc", loc, "mcannondoor", LocCannonNulodionDoor},
		{"loc", loc, "mcannondoor1", LocCannonShedDoor}, {"loc", loc, "broken_multicannon", LocCannonBroken}, {"loc", loc, "mcanmudpile", LocCannonMudPile},
		{"loc", loc, "ladder", LocCannonLadderUp}, {"loc", loc, "laddertop", LocCannonLadderDown},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	for i := 0; i < CannonRailings; i++ {
		if got := loc[fmt.Sprintf("mcannonrailing%d", i+1)]; got != CannonRailingLoc(i) {
			t.Errorf("mcannonrailing%d is %d, the plan says %d", i+1, got, CannonRailingLoc(i))
		}
	}
}

// The stages, the points, the reward and the varps.
func TestDwarfCannonContentStagesAndReward(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^mcannon_complete = %d", McComplete), "^mcannon_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, cannonDir+"configs/quest_mcannon.constant")
	for name, v := range map[string]int{"not_started": McNotStarted, "tasked_with_fixing_railings": McRailings, "tasked_with_checking_guard_tower": McTower,
		"tasked_with_finding_goblin_cave": McGoblinCave, "tasked_with_finding_gilobs_son": McFindChild, "return_to_dwarf_commander": McChildRescued,
		"tasked_with_fixing_cannon": McFixCannon, "inspected_cannon_first_time": 7, "has_repaired_cannon": McCannonFixed,
		"tasked_with_speaking_to_nulodion": McSeeNulodion, "return_to_dwarf_commander_with_notes": McReturnNotes} {
		if want := fmt.Sprintf("^mcannon_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_mcannon.constant lacks %q", want)
		}
	}
	reward := csContent(t, cannonDir+"scripts/quest_mcannon.rs2")
	for _, want := range []string{"%mcannon = ^mcannon_complete;", "stat_advance(crafting, 7500);", "^mcannon_questpoints"} {
		if !strings.Contains(reward, want) {
			t.Errorf("quest_mcannon.rs2 lacks %q", want)
		}
	}
	// Non-transmitted: the journal's prose is the only reader.
	varps := csContent(t, cannonDir+"configs/quest_mcannon.varp")
	if strings.Contains(varps, "transmit") || !strings.Contains(varps, "[mcannon]") || !strings.Contains(varps, "[mcannonmulti]") {
		t.Errorf("the varps are %q", varps)
	}
}

// The railings: the roll, the cut, the offer, the three answers and the bits.
func TestDwarfCannonContentRailings(t *testing.T) {
	src := csContent(t, cannonDir+"scripts/locs/railings.rs2")
	for _, want := range []string{
		"[oploc1,mcannonrailing1]\n@damaged_railings(5);", "[oploc1,mcannonrailing6]\n@damaged_railings(10);",
		"if (testbit(%mcannonmulti, $railing_num) = ^true) {\n    mes(\"You have already fixed this railing.\");",
		"mes(\"One railing is broken and needs to be replaced.\");", "~p_choice2(\"Try to replace the railing.\", 1, \"Leave it be.\", 2)",
		"if (stat_random(crafting, 60, 150) = true) {", "mes(\"You replace the railing with no problems.\");", "inv_del(inv, mcannonrailing1_obj, 1);",
		"mes(\"but fail and cut yourself trying.\");", "def_int $damage = calc(stat_base(hitpoints) / 10);", "if ($damage > 3) {\n            $damage = 3;", "~damage_self($damage);",
		"%mcannonmulti = setbit(%mcannonmulti, $railing_num);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("railings.rs2 lacks %q", want)
		}
	}
	// The cut is min(3, hitpoints / 10) and the module's number is the same.
	for hp, want := range map[int]int{9: 0, 10: 1, 25: 2, 30: 3, 99: 3} {
		if got := CannonRailingCut(hp); got != want {
			t.Errorf("a failed railing at %d hitpoints cuts %d, the content cuts %d", hp, got, want)
		}
	}
	// Health regenerates one point every hundred ticks (login.rs2 settimer(health_regen, 100); stat.rs2 stat_heal(hitpoints, 1, 0)).
	login := csContent(t, "scripts/login_logout/login.rs2")
	stat := csContent(t, "scripts/player/scripts/stat.rs2")
	if !strings.Contains(login, "settimer(health_regen, 100);") || !strings.Contains(stat, "[timer,health_regen]\nstat_heal(hitpoints, 1, 0);") {
		t.Error("health no longer regenerates one point in a hundred ticks")
	}
	// The loc ids: the bit of loc 15 + i is 5 + i.
	for i, spot := range CannonRailingSpots {
		rows := csMapRows(t, "LOC", spot)
		if !csHasRow(rows, CannonRailingLoc(i)) {
			t.Errorf("railing %d: tile %v holds %v, want loc %d", i+1, spot, rows, CannonRailingLoc(i))
		}
	}
}

// The tower: the ladder's special case, the remains and their pickup.
func TestDwarfCannonContentTower(t *testing.T) {
	ladders := csContent(t, "scripts/ladders+stairs/scripts/ladders.rs2")
	if !strings.Contains(ladders, "case 0_40_53_10_49 : @ladder_to_dwarf_remains;") {
		t.Error("the tower's ladder special case moved")
	}
	if got := herdCoord(t, "0_40_53_10_49"); got != (tile.Tile{X: 2570, Z: 3441}) {
		t.Errorf("0_40_53_10_49 is %v", got)
	}
	for _, want := range []string{"~climb_ladder(movecoord(coord(), 0, 1, 0), true);", "~climb_ladder(movecoord(coord(), 0, -1, 0), false);"} {
		if !strings.Contains(ladders, want) {
			t.Errorf("ladders.rs2 lacks %q", want)
		}
	}
	trap := csContent(t, cannonDir+"scripts/locs/mcannon_ladders.rs2")
	for _, want := range []string{"if (%mcannon < ^mcannon_tasked_with_checking_guard_tower) {", "but the trap door will not open.", "~climb_ladder(movecoord(coord(), 0, 1, 0), true);"} {
		if !strings.Contains(trap, want) {
			t.Errorf("mcannon_ladders.rs2 lacks %q", want)
		}
	}
	remains := csContent(t, cannonDir+"scripts/objs/dwarf_remains.rs2")
	for _, want := range []string{"[opobj3,mcannonremains]", "if(%mcannon = ^mcannon_tasked_with_checking_guard_tower) {", "if (inv_total(inv, mcannonremains) > 0) {", "@pickup_obj;"} {
		if !strings.Contains(remains, want) {
			t.Errorf("dwarf_remains.rs2 lacks %q", want)
		}
	}
	if rows := csMapRows(t, "OBJ", CannonRemainsSpawn); !csHasRow(rows, ObjCannonRemains) {
		t.Errorf("the top floor's tile %v holds %v, want the remains", CannonRemainsSpawn, rows)
	}
	// The ground-floor ladder is in the map too (the first floors' are in the baked index: the questmods real-data test).
	if rows := csMapRows(t, "LOC", tile.Tile{X: 2570, Z: 3441}); !csHasRow(rows, LocCannonLadderUp) {
		t.Errorf("the tower's ground floor ladder is %v", rows)
	}
	// The remains are obj 0, which the client's inventory model reads as an empty slot (readObjID in gamestate/inventory.go).
	if ObjCannonRemains != 0 {
		t.Errorf("the remains are obj %d", ObjCannonRemains)
	}
}

// The cave and its crate.
func TestDwarfCannonContentCave(t *testing.T) {
	cave := csContent(t, cannonDir+"scripts/locs/mcannon_cave.rs2")
	for _, want := range []string{"[oploc1,mcannoncave]", "%mcannon = ^mcannon_tasked_with_finding_gilobs_son;", "p_telejump(0_40_153_60_5);",
		"[oploc1,mcanmudpile]", "p_telejump(0_40_52_63_63);"} {
		if !strings.Contains(cave, want) {
			t.Errorf("mcannon_cave.rs2 lacks %q", want)
		}
	}
	for c, want := range map[string]tile.Tile{"0_40_153_60_5": CannonCaveArrive, "0_40_52_63_63": CannonCaveExit} {
		if got := herdCoord(t, c); got != want {
			t.Errorf("%s is %v, the plan says %v", c, got, want)
		}
	}
	crate := csContent(t, cannonDir+"scripts/locs/mcannon_crate.rs2")
	for _, want := range []string{"[oploc1,mcannoncrateboy]", "if (%mcannon = ^mcannon_tasked_with_finding_gilobs_son) {", "if (npc_find(coord, dwarfchildtw1, 14, 0) = true) {",
		"mes(\"You search the crate but find nothing.\");", "npc_add(0_40_153_11_59, dwarfchildtw1, 120);", "@dwarfchildtw1_dialogue;"} {
		if !strings.Contains(crate, want) {
			t.Errorf("mcannon_crate.rs2 lacks %q", want)
		}
	}
	child := csContent(t, cannonDir+"scripts/npcs/dwarfchildtw1.rs2")
	for _, want := range []string{"if (%mcannon = ^mcannon_tasked_with_finding_gilobs_son) {\n    %mcannon = ^mcannon_return_to_dwarf_commander;"} {
		if !strings.Contains(child, want) {
			t.Errorf("dwarfchildtw1.rs2 lacks %q", want)
		}
	}
	for _, c := range []struct {
		what string
		at   tile.Tile
		id   int
		sec  string
	}{
		{"the Cave Entrance", CannonCaveEntrance, LocCannonCave, "LOC"}, {"the crate", CannonCrate, LocCannonCrate, "LOC"}, {"the mud pile", CannonMudPile, LocCannonMudPile, "LOC"},
		{"the Commander", CannonCommanderAnchor, NpcCannonCommander, "NPC"}, {"Nulodion", NulodionAnchor, NpcNulodion, "NPC"},
		{"the shed's door", tile.Tile{X: 2576, Z: 3461}, LocCannonShedDoor, "LOC"}, {"the broken cannon", CannonShedCannon, LocCannonBroken, "LOC"},
		{"Nulodion's door", tile.Tile{X: 3015, Z: 3453}, LocCannonNulodionDoor, "LOC"},
	} {
		if rows := csMapRows(t, c.sec, c.at); !csHasRow(rows, c.id) {
			t.Errorf("%s: tile %v holds %v, want %d", c.what, c.at, rows, c.id)
		}
	}
	// The cave's map square holds goblins (levels 2, 5 and 13, no hunt mode), rats, and the hunters: bats and dungeon rats. Nothing else.
	npc := csPackIDs(t, "npc")
	byID := map[int]string{}
	for name, id := range npc {
		byID[id] = name
	}
	blocks := npcBlocks(t, contentRoot(t))
	src := csContent(t, "maps/m40_153.jm2")
	in := false
	seen := map[string]int{}
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		_, tail, ok := strings.Cut(line, ":")
		if !in || !ok {
			continue
		}
		var id int
		fmt.Sscanf(strings.TrimSpace(tail), "%d", &id)
		seen[byID[id]]++
	}
	for name, n := range seen {
		switch name {
		case "goblin", "goblin_armed", "goblin_helmet", "rat":
			if blocks[name]["huntmode"] != "" {
				t.Errorf("%s (%d in the cave) has hunt mode %s: it would attack an account that attacks nobody", name, n, blocks[name]["huntmode"])
			}
		case "bat", "dungeon_rat":
			if blocks[name]["huntmode"] != "cowardly" || blocks[name]["huntrange"] != "2" {
				t.Errorf("%s: hunt mode %q range %q", name, blocks[name]["huntmode"], blocks[name]["huntrange"])
			}
		default:
			t.Errorf("the cave holds %d %s: the plan does not price it", n, name)
		}
	}
	if seen["bat"] == 0 || seen["dungeon_rat"] == 0 || seen["goblin"] == 0 {
		t.Errorf("the cave census %v is not the plan's", seen)
	}
}

// The hunters: the cowardly mode skips an account stronger than twice the monster's level, the bats are level 27, and the camp's goblins
// and guards have no hunt mode.
func TestDwarfCannonContentHunters(t *testing.T) {
	blocks := npcBlocks(t, contentRoot(t))
	bat := blocks["bat"]
	if bat["vislevel"] != "27" || bat["huntmode"] != "cowardly" || bat["huntrange"] != "2" || bat["wanderrange"] != "8" || bat["hitpoints"] != "32" {
		t.Errorf("the giant bat is %v", bat)
	}
	if CannonBatCeiling != 2*27 {
		t.Errorf("CannonBatCeiling is %d, twice the bats' level is %d", CannonBatCeiling, 2*27)
	}
	if got := FoeGiantBat; got.Attack != 22 || got.Strength != 22 || got.Defence != 22 || got.Hitpoints != 32 {
		t.Errorf("the table's bat %+v", got)
	}
	hunt := csContent(t, "scripts/_unpack/225/all.hunt")
	if b := csConfigBlock(hunt, "cowardly"); !strings.Contains(b, "check_nottoostrong=outside_wilderness") || !strings.Contains(b, "type=player") {
		t.Errorf("the cowardly hunt mode is %q", b)
	}
	for _, name := range []string{"goblin", "goblin_armed", "goblin_helmet", "mcannonguard", "lawgof2", "nulodion", "dwarfchildtw1"} {
		if blocks[name]["huntmode"] != "" {
			t.Errorf("%s has hunt mode %s", name, blocks[name]["huntmode"])
		}
	}
	if g := blocks["goblin"]; g["vislevel"] != "2" || g["wanderrange"] != "15" {
		t.Errorf("the goblin is %v", g)
	}
	// The camp's bats: seven in the square, the nearest a few tiles from the Commander's anchor.
	npc := csPackIDs(t, "npc")
	src := csContent(t, "maps/m40_54.jm2")
	in := false
	bats := 0
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		_, tail, ok := strings.Cut(line, ":")
		if !in || !ok {
			continue
		}
		var id int
		fmt.Sscanf(strings.TrimSpace(tail), "%d", &id)
		if id == npc["bat"] {
			bats++
		}
	}
	if bats != 7 {
		t.Errorf("%d bats near the camp, the plan says seven", bats)
	}
}

// The cannon: the rolls, the menu, the bits, and the four answers.
func TestDwarfCannonContentCannon(t *testing.T) {
	src := csContent(t, cannonDir+"scripts/locs/mcannon_broken_cannon.rs2")
	for _, want := range []string{
		"[oploc1,broken_multicannon]", "if (%mcannon = ^mcannon_tasked_with_fixing_cannon) {", "%mcannon = ^mcannon_inspected_cannon_first_time;",
		"if (getbit_range(%mcannonmulti, 0, 3) = 15) {", "%mcannon = ^mcannon_has_repaired_cannon;", "mes(\"It's a strange dwarf contraption.\");",
		"mes(\"You try, but can't quite find the problem.\");",
		"\"Pipe\", fix_cannon_pipe,", "\"Barrel\", fix_cannon_barrel,", "\"Axle\", fix_cannon_axle,", "\"Shaft\", fix_cannon_shaft,", "\"None\", fix_cannon_none,",
		"@attempt_repair(\"pipe\", 0);", "@attempt_repair(\"barrel\", 1);", "@attempt_repair(\"axle\", 2);", "@attempt_repair(\"shaft\", 3);",
		"mes(\"You've already fixed this part of the cannon.\");", "mes(\"You need a tool kit to repair the cannon.\");",
		"mes(\"After some tinkering you manage to fix it.\");", "mes(\"It's too hard you fail to fix it.\");",
		"def_boolean $smithing = stat_random(smithing, 80, 300);", "def_boolean $crafting = stat_random(crafting, 80, 350);", "if ($smithing = true | $crafting = true) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("mcannon_broken_cannon.rs2 lacks %q", want)
		}
	}
	// The module's four parts are those bits in that order, and their menu words are the script's.
	for i, part := range CannonParts {
		if !strings.Contains(src, fmt.Sprintf("@attempt_repair(\"%s\", %d);", strings.ToLower(part), i)) || !strings.Contains(src, fmt.Sprintf("\"%s\", fix_cannon_", part)) {
			t.Errorf("part %d (%s) is not the script's", i, part)
		}
	}
	// The shed's door opens from stage 6 (the Commander's door from 9 is Nulodion's), and Nulodion's from 9.
	doors := csContent(t, cannonDir+"scripts/locs/mcannon_doors.rs2")
	for _, want := range []string{"[oploc1,mcannondoor]\nif (%mcannon < ^mcannon_tasked_with_speaking_to_nulodion) {", "[oploc1,mcannondoor1]\nif (%mcannon >= ^mcannon_tasked_with_fixing_cannon) {"} {
		if !strings.Contains(doors, want) {
			t.Errorf("mcannon_doors.rs2 lacks %q", want)
		}
	}
}

// The conversations: the menus the Commander's stop steers, the hand-overs, and Nulodion's.
func TestDwarfCannonContentTheTalks(t *testing.T) {
	cmd := csContent(t, cannonDir+"scripts/npcs/lawgof2.rs2")
	for _, want := range []string{
		"\"I'm sorry, I'm too busy mining.\", im_too_busy_mining,\n    \"Yeah, I'd love to help.\", id_love_to_help",
		"%mcannon = ^mcannon_tasked_with_fixing_railings;", "inv_add(inv, mcannonrailing1_obj, 6);",
		"if (getbit_range(%mcannonmulti, 5, 10) = 63) {", "%mcannon = ^mcannon_tasked_with_checking_guard_tower;",
		"if (inv_total(inv, mcannonrailing1_obj) < 1) {", "inv_add(inv, mcannonrailing1_obj, 1);",
		"%mcannon = ^mcannon_tasked_with_finding_goblin_cave;\n        inv_del(inv, mcannonremains, 1);",
		"\"Ok, I'll see what I can do.\", mcannon_ill_see_what_i_can_do,\n    \"Sorry, I've done enough for today.\", mcannon_ive_done_enough_for_today",
		"inv_add(inv, mcannontoolkit, 1);", "%mcannon = ^mcannon_tasked_with_fixing_cannon;", "if (inv_total(inv, mcannontoolkit) < 1) {",
		"if_close;\np_delay(1);\nmes(\"The Dwarf Commander pops into the shed to take a closer look.\");\np_delay(1);",
		"\"Sorry, I've really done enough.\", mcannon_sorry_ive_really_done_enough,\n    \"Ok then, just for you!\", mcannon_ok_then_just_for_you",
		"%mcannon = ^mcannon_tasked_with_speaking_to_nulodion;", "if (inv_total(inv, ammo_mould) < 1 | inv_total(inv, nulodions_notes) < 1) {",
		"inv_del(inv, ammo_mould, 1);", "inv_del(inv, nulodions_notes, 1);", "queue(mcannon_complete, 0, 0);",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("lawgof2.rs2 lacks %q", want)
		}
	}
	nul := csContent(t, cannonDir+"scripts/npcs/nulodion.rs2")
	for _, want := range []string{"case ^mcannon_tasked_with_speaking_to_nulodion : @mcannon_speak_with_nulodion_first_time;", "inv_add(inv, nulodions_notes, 1);",
		"inv_add(inv, ammo_mould, 1);", "%mcannon = ^mcannon_return_to_dwarf_commander_with_notes;", "if (inv_total(inv, nulodions_notes) = 0) {", "if (inv_total(inv, ammo_mould) = 0) {"} {
		if !strings.Contains(nul, want) {
			t.Errorf("nulodion.rs2 lacks %q", want)
		}
	}
	// The Commander's stop carries the three accept lines, each of them an option of exactly one of his menus.
	for _, line := range CannonCommanderStop.Prefer {
		if strings.Count(cmd, `"`+line+`"`) < 1 {
			t.Errorf("no option %q in lawgof2.rs2", line)
		}
	}
	for _, line := range []string{"I'm sorry, I'm too busy mining.", "Sorry, I've done enough for today.", "Sorry, I've really done enough."} {
		for _, p := range CannonCommanderStop.Prefer {
			if strings.Contains(strings.ToLower(line), strings.ToLower(p)) {
				t.Errorf("the refusal %q contains the preference %q", line, p)
			}
		}
	}
	// Six railings need six pack slots, the start's room is more than that, and Nulodion's two items need two.
	if CannonStartRoom < 6+1 {
		t.Errorf("the start's room is %d", CannonStartRoom)
	}
}

// The journal: every needle the parser reads is in the script's text.
func TestDwarfCannonContentJournal(t *testing.T) {
	src := csContent(t, cannonDir+"scripts/mcannon_journal.rs2")
	text := NormaliseLines([]string{src})
	for _, m := range cannonMarkers {
		if !strings.Contains(text, m.text) {
			t.Errorf("mcannon_journal.rs2 has no %q (stage %d)", m.text, m.stage)
		}
	}
	for _, want := range []string{"if (getbit_range(%mcannonmulti, 5, 10) = 63) {", "if (inv_total(inv, mcannonremains) > 0) {"} {
		if !strings.Contains(src, want) {
			t.Errorf("mcannon_journal.rs2 lacks %q", want)
		}
	}
	// Stages 6 and 7 print the same text.
	if !strings.Contains(src, "case ^mcannon_tasked_with_fixing_cannon :") || !strings.Contains(src, "case ^mcannon_inspected_cannon_first_time :") {
		t.Error("stages 6 and 7 no longer have a page each")
	}
}
