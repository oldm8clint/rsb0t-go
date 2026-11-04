package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, bit, reward and rule Sheep Herder rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run SheepHerderContent ./internal/scripts/questplan/

const herdDir = "scripts/quests/quest_sheepherder/"

// herdCoord reads a content coordinate "level_mx_mz_lx_lz" as a tile.
func herdCoord(t *testing.T, s string) tile.Tile {
	t.Helper()
	var lvl, mx, mz, lx, lz int
	if n, err := fmt.Sscanf(s, "%d_%d_%d_%d_%d", &lvl, &mx, &mz, &lx, &lz); n != 5 || err != nil {
		t.Fatalf("coordinate %q: %v", s, err)
	}
	return tile.Tile{X: mx<<6 + lx, Z: mz<<6 + lz, Level: lvl}
}

// The pack ids the plan carries are the pack's.
func TestSheepHerderContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "councillor_halgrive", NpcHalgrive}, {"npc", npc, "doctor_orbon", NpcOrbon}, {"npc", npc, "farmer_brumty", NpcBrumty},
		{"npc", npc, "plaguesheep_1", NpcPlagueSheep(1)}, {"npc", npc, "plaguesheep_2", NpcPlagueSheep(2)},
		{"npc", npc, "plaguesheep_3", NpcPlagueSheep(3)}, {"npc", npc, "plaguesheep_4", NpcPlagueSheep(4)},
		{"obj", obj, "cattleprod", ObjCattleProd}, {"obj", obj, "poisoned_feed", ObjSheepFeed},
		{"obj", obj, "sheepbonesa", ObjSheepBones(1)}, {"obj", obj, "sheepbonesb", ObjSheepBones(2)},
		{"obj", obj, "sheepbonesc", ObjSheepBones(3)}, {"obj", obj, "sheepbonesd", ObjSheepBones(4)},
		{"obj", obj, "plague_jacket", ObjPlagueJacket}, {"obj", obj, "plague_trousers", ObjPlagueTrousers}, {"obj", obj, "coins", CoinsID},
		{"loc", loc, "plaguesheep_furnace", LocPlagueFurnace}, {"loc", loc, "plaguesheep_gatel", LocPlagueGateLeft}, {"loc", loc, "plaguesheep_gater", LocPlagueGateRight},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the points and the reward.
func TestSheepHerderContentStagesAndReward(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^sheepherder_complete = 3", "^sheepherder_questpoints = 4"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, herdDir+"configs/quest_sheepherder.constant")
	for _, want := range []string{"^sheepherder_not_started = 0", "^sheepherder_tasked_with_talking_to_dr_orbon = 1", "^sheepherder_tasked_with_disposing_of_sheep = 2"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_sheepherder.constant lacks %q", want)
		}
	}
	reward := csContent(t, herdDir+"scripts/quest_sheepherder.rs2")
	for _, want := range []string{"%sheepherderquest = ^sheepherder_complete;", "inv_add(inv, coins, 3100);", "^sheepherder_questpoints"} {
		if !strings.Contains(reward, want) {
			t.Errorf("quest_sheepherder.rs2 lacks %q", want)
		}
	}
	// The quest's two varps are not transmitted (no `transmit=yes`), which is why the journal's prose is the only reader.
	varps := csContent(t, herdDir+"configs/quest_sheepherder.varp")
	if strings.Contains(varps, "transmit") || !strings.Contains(varps, "[sheepherderquest]") || !strings.Contains(varps, "[sheepherdervar]") {
		t.Errorf("the varps are %q", varps)
	}
}

// The enclosure, the gate zone, the sheep's in-pen tiles and their bits.
func TestSheepHerderContentAreasAndSheepData(t *testing.T) {
	areas := csContent(t, herdDir+"configs/sheepherder_area.dbrow")
	pen := csConfigBlock(areas, "sheepherder_in_pen")
	gate := csConfigBlock(areas, "sheepherder_pen_gate")
	boxOf := func(block string) HerdBox {
		_, pair, ok := strings.Cut(block, "data=coord_pair,")
		if !ok {
			t.Fatalf("no coord_pair in %q", block)
		}
		a, b, _ := strings.Cut(strings.TrimSpace(strings.SplitN(pair, "\n", 2)[0]), ",")
		lo, hi := herdCoord(t, a), herdCoord(t, b)
		return HerdBox{X0: lo.X, X1: hi.X, Z0: lo.Z, Z1: hi.Z}
	}
	if got := boxOf(pen); got != HerdPen {
		t.Errorf("sheepherder_in_pen is %+v, the plan says %+v", got, HerdPen)
	}
	if got := boxOf(gate); got != HerdGateZone {
		t.Errorf("sheepherder_pen_gate is %+v, the plan says %+v", got, HerdGateZone)
	}
	data := csContent(t, herdDir+"configs/sheepherder_sheep_data.dbrow")
	for n := 1; n <= 4; n++ {
		block := csConfigBlock(data, fmt.Sprintf("herder_plaguesheep_%d", n))
		if !strings.Contains(block, fmt.Sprintf("data=sheep_type,plaguesheep_%d", n)) ||
			!strings.Contains(block, fmt.Sprintf("data=sheep_start_bit,%d", 3*n-2)) ||
			!strings.Contains(block, "data=sheep_bones,sheepbones"+string(rune('a'+n-1))) {
			t.Errorf("sheep %d's row is %q", n, block)
		}
		_, spot, _ := strings.Cut(block, "data=sheep_in_pen_coord,")
		if got := herdCoord(t, strings.TrimSpace(strings.SplitN(spot, "\n", 2)[0])); got != HerdPenSpots[n-1] {
			t.Errorf("sheep %d is set down on %v, the plan says %v", n, got, HerdPenSpots[n-1])
		}
		if !InHerdPen(HerdPenSpots[n-1]) {
			t.Errorf("sheep %d's pen spot %v is outside the pen", n, HerdPenSpots[n-1])
		}
	}
}

// What prod_sheep, poison_sheep and the sheep's timer do.
func TestSheepHerderContentSheepScripts(t *testing.T) {
	sheep := csContent(t, herdDir+"scripts/npcs/diseased_sheep.rs2")
	for _, want := range []string{
		// The refusals the module is built to never trigger.
		"inv_total(worn, plague_jacket) < 1 | inv_total(worn, plague_trousers)  < 1", "$rhand ! cattleprod", "The sheep is already in the enclosure.",
		"You've already got bones from a sheep like this.", "You have already disposed of this sheep;",
		// The push: one tile along the direction away from the account, a jump read at the sheep's own coordinate, the sheep set down for 150 ticks.
		"npc_setmode(none);", "npc_walk(~movecoord_indirection(npc_coord, ~coord_direction(coord, npc_coord), 1));",
		"if (~inzone_coord_pair_table(sheepherder_pen_gate, npc_coord) = true) {", "npc_tele(0_40_52_35_33);", "npc_add($npc_coord, $npc, 150);",
		"%sheepherdervar = setbit(%sheepherdervar, $npc_start_bit);", "The sheep obligingly jumps over the gate and into the enclosure.",
		// The kill: inside the enclosure only, the bits replaced by 2, a message box, the remains dropped where the sheep stood.
		"%sheepherdervar = setbit_range_toint(%sheepherdervar, 2, $npc_start_bit, calc($npc_start_bit + 2));",
		"You watch as the sheep collapses dead onto the floor.", "obj_add(npc_coord, $npc_bones, 1, calc(^lootdrop_duration / 2));",
		"I don't think I should kill the sheep outside the|enclosure.",
		// The timer returns the sheep to its default mode every 10 to 29 ticks.
		"npc_settimer(calc(10 + random(20)));", "npc_setmode(null);",
		// The feed is never used up.
		"[opnpcu,plaguesheep_1]", "case poisoned_feed : @poison_sheep(npc_type);",
	} {
		if !strings.Contains(sheep, want) {
			t.Errorf("diseased_sheep.rs2 lacks %q", want)
		}
	}
	if strings.Contains(sheep, "inv_del(inv, poisoned_feed") {
		t.Error("the poisoned feed is used up by a kill: the module asks for one feed")
	}
	// coord_direction: the longer axis of the offset, east or west on a tie (the loop's stands assume exactly this).
	procs := csContent(t, "scripts/general/scripts/misc/coord_procs.rs2")
	i := strings.Index(procs, "[proc,coord_direction](coord $coord1, coord $coord2)(int)")
	if i < 0 {
		t.Fatal("no coord_direction proc")
	}
	body := procs[i:]
	body = body[:strings.Index(body, "[proc,coord_direction2]")]
	for _, want := range []string{"if ($dx > 0) {", "if (abs($dz) > abs($dx)) {", "return(^exact_north);", "return(^exact_south);", "return(^exact_east);", "return(^exact_west);"} {
		if !strings.Contains(body, want) {
			t.Errorf("coord_direction lacks %q", want)
		}
	}
	if strings.Count(body, "abs($dz) > abs($dx)") != 2 {
		t.Error("coord_direction no longer breaks a tie east or west")
	}
	// The sheep's config: Prod is op 1, wander range 3, a 25 tick first timer, and no hunt mode (nothing in the quest attacks).
	blocks := npcBlocks(t, contentRoot(t))
	for n := 1; n <= 4; n++ {
		b := blocks[fmt.Sprintf("plaguesheep_%d", n)]
		if b["op1"] != "Prod" || b["wanderrange"] != "3" || b["timer"] != "25" || b["huntmode"] != "" || b["op2"] != "" {
			t.Errorf("plaguesheep_%d is %v", n, b)
		}
	}
}

// The furnace, the gate, the barn.
func TestSheepHerderContentFurnaceAndGate(t *testing.T) {
	furnace := csContent(t, herdDir+"scripts/locs/sheepherder_furnace.rs2")
	for _, want := range []string{
		"(last_useitem = sheepbonesa) {\n    @incinerate_bones(1);", "(last_useitem = sheepbonesb) {\n    @incinerate_bones(4);",
		"(last_useitem = sheepbonesc) {\n    @incinerate_bones(7);", "(last_useitem = sheepbonesd) {\n    @incinerate_bones(10);",
		"inv_del(inv, last_useitem, 1);\n    p_delay(3);\n    mes(\"The remains burn to dust.\");",
		"%sheepherdervar = setbit_range_toint(%sheepherdervar, 6, $starting_bit, calc($starting_bit + 2));",
	} {
		if !strings.Contains(furnace, want) {
			t.Errorf("sheepherder_furnace.rs2 lacks %q", want)
		}
	}
	loc := csContent(t, herdDir+"configs/quest_sheepherder.loc")
	if b := csConfigBlock(loc, "plaguesheep_furnace"); !strings.Contains(b, "width=3") || !strings.Contains(b, "length=3") || !strings.Contains(b, "forceapproach=east") {
		t.Errorf("the furnace is %q", b)
	}
	for _, name := range []string{"plaguesheep_gatel", "plaguesheep_gater"} {
		if b := csConfigBlock(loc, name); !strings.Contains(b, "op1=Open") || !strings.Contains(b, "active=yes") {
			t.Errorf("%s is %q", name, b)
		}
	}
	gate := csContent(t, herdDir+"scripts/locs/sheepherder_gate.rs2")
	for _, want := range []string{
		"if(~check_axis(coord, loc_coord, loc_angle) = true) {", "inv_total(worn, plague_jacket) < 1 | inv_total(worn, plague_trousers) < 1",
		"if(coordx(coord) <= coordx(loc_coord)) {", "p_teleport(movecoord(loc_coord, 1, 0, 0));", "p_teleport(movecoord(loc_coord, 0, 0, 0));",
	} {
		if !strings.Contains(gate, want) {
			t.Errorf("sheepherder_gate.rs2 lacks %q", want)
		}
	}
	// check_axis for an east/west gate (angle 2) is the account's column equalling the gate's: only the gate's own tile checks the suit.
	axis := csContent(t, "scripts/doors/scripts/door_procs.rs2")
	if !strings.Contains(axis, "case ^loc_west, ^loc_east :\n        if (coordx($coord) = coordx($loc_coord)) {\n            return(true);") {
		t.Error("check_axis no longer reads the column for an east/west gate")
	}
	// The map: the gate's halves at 2594,3361 and 2594,3362 (angle 2), the incinerator at 2605,3360, the barn's prod at 2604,3357.
	if rows := csMapRows(t, "LOC", HerdGateOutside); !csHasRow(rows, LocPlagueGateRight) || rows[0][2] != "2" {
		t.Errorf("the gate's tile holds %v", rows)
	}
	if rows := csMapRows(t, "LOC", tile.Tile{X: 2594, Z: 3362}); !csHasRow(rows, LocPlagueGateLeft) {
		t.Errorf("the gate's other half is %v", rows)
	}
	if rows := csMapRows(t, "LOC", HerdFurnaceAnchor); !csHasRow(rows, LocPlagueFurnace) {
		t.Errorf("the incinerator's tile holds %v", rows)
	}
	if rows := csMapRows(t, "OBJ", HerdProdSpawn); !csHasRow(rows, ObjCattleProd) {
		t.Errorf("the barn's tile holds %v", rows)
	}
	if rows := csMapRows(t, "NPC", BrumtyAnchor); !csHasRow(rows, NpcBrumty) {
		t.Errorf("Brumty's tile holds %v", rows)
	}
}

// Halgrive's and Orbon's conversations: the menus the stops steer, the hand-overs, the 100 coins and the 7020.
func TestSheepHerderContentTheTalks(t *testing.T) {
	halgrive := csContent(t, herdDir+"scripts/npcs/councillor_halgrive.rs2")
	orbon := csContent(t, herdDir+"scripts/npcs/doctor_orbon.rs2")
	for _, want := range []string{
		"\"What's wrong?\", halgrive_whats_wrong", "\"I can do that for you.\", halgrive_i_can_do_that_for_you",
		"inv_add(inv, poisoned_feed, 1);", "%sheepherderquest = ^sheepherder_tasked_with_talking_to_dr_orbon;",
		"if (getbit_range(%sheepherdervar, 0, 12) = 7020) {", "queue(sheepherder_complete, 0, 0);",
		"} else if (inv_total(inv, poisoned_feed) < 1) {\n    @halgrive_need_more_poisoned_feed;",
	} {
		if !strings.Contains(halgrive, want) {
			t.Errorf("councillor_halgrive.rs2 lacks %q", want)
		}
	}
	for _, want := range []string{
		"\"Ok, I'll take it.\", doctor_orbon_ill_take_it", "if (inv_total(inv, coins) < 100) {", "inv_del(inv, coins, 100);",
		"inv_add(inv, plague_jacket, 1);", "inv_add(inv, plague_trousers, 1);", "%sheepherderquest = ^sheepherder_tasked_with_disposing_of_sheep;",
		"\"Ok, I'll take it.\", doctor_orbon_ill_take_it,\n        \"No, I don't need anymore.\"",
		"((inv_total(inv, plague_trousers) > 0) | (inv_total(worn, plague_trousers) > 0))", "((inv_total(inv, plague_jacket) > 0) | (inv_total(worn, plague_jacket) > 0))",
	} {
		if !strings.Contains(orbon, want) {
			t.Errorf("doctor_orbon.rs2 lacks %q", want)
		}
	}
	if HerdSuitGP != 100 {
		t.Errorf("the suit costs %d in the plan", HerdSuitGP)
	}
	// Every preference of the two stops is an option of their menus, in the script's order.
	for _, c := range []struct {
		stop NpcStop
		src  string
	}{{HalgriveStop, halgrive}, {OrbonStop, orbon}} {
		last := -1
		for _, line := range c.stop.Prefer {
			i := strings.Index(c.src, `"`+line+`"`)
			if i < 0 {
				t.Errorf("%s: no option %q in its script", c.stop.Npc, line)
			}
			if i < last {
				t.Errorf("%s: option %q comes before the previous preference in the script", c.stop.Npc, line)
			}
			last = i
		}
	}
	// The conversations' map spawns.
	if rows := csMapRows(t, "NPC", HalgriveAnchor); !csHasRow(rows, NpcHalgrive) {
		t.Errorf("Halgrive's tile holds %v", rows)
	}
	if rows := csMapRows(t, "NPC", OrbonAnchor); !csHasRow(rows, NpcOrbon) {
		t.Errorf("Orbon's tile holds %v", rows)
	}
}

// The journal: every needle the parser reads is in the script's text, and the bit values its pages are printed from are the ones the
// sheep data and the furnace set.
func TestSheepHerderContentJournal(t *testing.T) {
	src := csContent(t, herdDir+"scripts/sheepherder_journal.rs2")
	text := NormaliseLines([]string{src})
	needles := []string{"i can start this quest by speaking to", "i need to get some protective clothing", "i bought some protective clothing", "i should return to",
		"quest complete!"}
	for n := 0; n < 4; n++ {
		ord := herdOrdinals[n]
		needles = append(needles, "killed the "+ord+" sheep and incinerated its bones", "killed the "+ord+" sheep. now i must incinerate",
			"herded the "+ord+" sheep to the pen")
	}
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Errorf("sheepherder_journal.rs2 has no %q", needle)
		}
	}
	for _, want := range []string{"def_int $sheep1 = getbit_range(%sheepherdervar, 1, 3);", "def_int $sheep2 = getbit_range(%sheepherdervar, 4, 6);",
		"def_int $sheep3 = getbit_range(%sheepherdervar, 7, 9);", "def_int $sheep4 = getbit_range(%sheepherdervar, 10, 12);",
		"if ($sheep1 = 6 & $sheep2 = 6 & $sheep3 = 6 & $sheep4 = 6) {", "if (inv_total(inv, sheepbonesa) > 0) {"} {
		if !strings.Contains(src, want) {
			t.Errorf("sheepherder_journal.rs2 lacks %q", want)
		}
	}
	// 7020 is every sheep's bits at 6.
	if got := 6<<1 + 6<<4 + 6<<7 + 6<<10; got != 7020 {
		t.Errorf("four sheep burnt is %d", got)
	}
}

// The four fields: three copies of each sheep within a tile of the middle one the plan names, and nothing else of the quest's region
// that hunts an account near them or the route to the enclosure.
func TestSheepHerderContentSpawns(t *testing.T) {
	for n := 1; n <= 4; n++ {
		mid := HerdSheepSpawns[n-1]
		copies := csNearRows(t, "NPC", NpcPlagueSheep(n), mid, 2)
		if copies != 3 {
			t.Errorf("sheep %d: %d copies within two tiles of %v, want 3", n, copies, mid)
		}
	}
	// The chaos druids (hunt mode ranged, huntrange 3, level 13) are the only hunters in the square: they stand more than 25 tiles from
	// the enclosure and from every sheep's field.
	src := csContent(t, "maps/m40_52.jm2")
	npc := csPackIDs(t, "npc")
	blocks := npcBlocks(t, contentRoot(t))
	in := false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		head, tail, ok := strings.Cut(line, ":")
		if !in || !ok {
			continue
		}
		var lvl, lx, lz, id int
		fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz)
		fmt.Sscanf(strings.TrimSpace(tail), "%d", &id)
		name := ""
		for k, v := range npc {
			if v == id {
				name = k
			}
		}
		if blocks[name]["huntmode"] == "" {
			continue
		}
		at := tile.Tile{X: 40<<6 + lx, Z: 52<<6 + lz}
		for _, away := range append([]tile.Tile{{X: HerdPen.X0, Z: HerdPen.Z0}, {X: HerdPen.X1, Z: HerdPen.Z1}}, HerdSheepSpawns[:]...) {
			if tile.Chebyshev(at, away) <= 25 {
				t.Errorf("%s (hunt mode %s) stands at %v, %d tiles from %v", name, blocks[name]["huntmode"], at, tile.Chebyshev(at, away), away)
			}
		}
	}
}
