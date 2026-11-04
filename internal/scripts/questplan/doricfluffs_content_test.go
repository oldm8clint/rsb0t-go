package questplan

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward and rock Doric's Quest and Gertrude's Cat rest on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run DoricFluffs ./internal/scripts/questplan/
//
// It uses the cook/sheep content test's readers (cooksheep_content_test.go).

// The object, npc and loc ids the plans carry are the pack's.
func TestDoricFluffsContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "clay", DoricClay}, {"obj", obj, "copper_ore", DoricCopperOre}, {"obj", obj, "iron_ore", DoricIronOre},
		{"obj", obj, "bronze_pickaxe", 1265}, {"obj", obj, "iron_pickaxe", 1267}, {"obj", obj, "steel_pickaxe", 1269},
		{"obj", obj, "mithril_pickaxe", 1273}, {"obj", obj, "adamant_pickaxe", 1271}, {"obj", obj, "rune_pickaxe", 1275},
		{"obj", obj, "doogleleaves", FluffsDoogleLeaves}, {"obj", obj, "raw_sardine", FluffsRawSardine},
		{"obj", obj, "seasoned_sardine", FluffsSeasonedSardine}, {"obj", obj, "gertrudekittens", FluffsKitten},
		{"obj", obj, "bucket_milk", ObjBucketOfMilk}, {"obj", obj, "bucket_empty", ObjBucket},
		{"npc", npc, "doric", NpcDoric}, {"npc", npc, "gertrude", NpcGertrude}, {"npc", npc, "shilop", NpcShilop},
		{"npc", npc, "wilough", NpcWilough}, {"npc", npc, "gertrudescat", NpcFluffs}, {"npc", npc, "kittens_mew", NpcKittenCrate},
		{"npc", npc, "gerrant", FluffsSardineShop.NpcID}, {"npc", npc, "bob", BobsAxes.NpcID},
		{"loc", loc, "clayrock1", DoricClayRocks[0]}, {"loc", loc, "clayrock2", DoricClayRocks[1]},
		{"loc", loc, "copperrock1", DoricCopperRocks[0]}, {"loc", loc, "copperrock2", DoricCopperRocks[1]},
		{"loc", loc, "ironrock1", DoricIronRocks[0]}, {"loc", loc, "ironrock2", DoricIronRocks[1]},
		{"loc", loc, "gertrudefence", LocYardFence}, {"loc", loc, "ladder", LocYardLadder}, {"loc", loc, "laddertop", LocYardTop},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// Doric's script: the stages, the three ores and their quantities, the reward, and the menus the stop steers.
func TestDoricFluffsContentDoricScript(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^doric_complete = 100", "^doric_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	if DoricComplete != 100 || DoricStarted != 10 || DoricNotStarted != 0 {
		t.Errorf("stages %d %d %d", DoricNotStarted, DoricStarted, DoricComplete)
	}
	src := csContent(t, "scripts/quests/quest_doric/scripts/quest_doric.rs2")
	for _, want := range []string{
		"%doricquest = 10;", "inv_total(inv, clay) > 5 & inv_total(inv, copper_ore) > 3 & inv_total(inv, iron_ore) > 1",
		"inv_del(inv, clay, 6);", "inv_del(inv, copper_ore, 4);", "inv_del(inv, iron_ore, 2);",
		"inv_add(inv, coins, 180);", "stat_advance(mining, 13000);", "queue(doric_quest_complete, 0, 0);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_doric.rs2 lacks %q", want)
		}
	}
	if DoricClayQty != 6 || DoricCopperQty != 4 || DoricIronQty != 2 {
		t.Errorf("quantities %d %d %d", DoricClayQty, DoricCopperQty, DoricIronQty)
	}
	if DoricsQuestRecord.QuestPoints != 1 || LadderFacts["doric"].XP["mining"] != 1300 {
		t.Errorf("record %+v xp %v: 13000 Mining XP at 1x is 1300", DoricsQuestRecord, LadderFacts["doric"].XP)
	}
	// The start is two menus deep, in this order.
	last := -1
	for _, line := range DoricStop.Prefer {
		i := strings.Index(src, `"`+line+`"`)
		if i < 0 {
			t.Errorf("quest_doric.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	// The journal: the three tab states and the stage-10 line the parser reads, the title spelling the tab does not share.
	journal := csContent(t, "scripts/quests/quest_doric/scripts/doric_journal.rs2")
	for _, want := range []string{"I can start this quest by talking to @dre@Doric@dbl@", "I need to collect some items and bring them to @dre@Doric|",
		"@red@QUEST COMPLETE!", "%doricquest = 0", "%doricquest = 10", `"Dorics' Quest"`} {
		if !strings.Contains(journal, want) {
			t.Errorf("doric_journal.rs2 lacks %q", want)
		}
	}
	// A varp that is perm and not transmitted: the tab's colour and the journal are the only readers.
	if v := csContent(t, "scripts/quests/quest_doric/configs/quest_doric.varp"); !strings.Contains(v, "[doricquest]\nscope=perm") || strings.Contains(v, "transmit") {
		t.Errorf("quest_doric.varp: %q", v)
	}
}

// The rocks: every ore rock of the Rimmington mine is named "Rocks" with the one op Mine, the rock ids pair with their ore, and
// the levels are the plan's (iron 15, clay and copper 1).
func TestDoricFluffsContentRocks(t *testing.T) {
	dbrow := csContent(t, "scripts/skill_mining/configs/mine.dbrow")
	for _, c := range []struct {
		table string
		rocks []int
		ore   string
		level int
	}{
		{"clay_rock_table", DoricClayRocks, "clay", 1},
		{"copper_rock_table", DoricCopperRocks, "copper_ore", 1},
		{"iron_rock_table", DoricIronRocks, "iron_ore", DoricIronLevel},
	} {
		block := csConfigBlock(dbrow, c.table)
		if !strings.Contains(block, "data=rock_output,"+c.ore) || !strings.Contains(block, fmt.Sprintf("data=rock_level,%d", c.level)) {
			t.Errorf("%s: %q", c.table, block)
		}
		loc := csPackIDs(t, "loc")
		for i, id := range c.rocks {
			name := strings.TrimSuffix(c.table, "_table")
			name = strings.TrimSuffix(name, "_rock") + "rock" + strconv.Itoa(i+1)
			if loc[name] != id || !strings.Contains(block, "data=rock,"+name) {
				t.Errorf("%s: rock %s is %d in the pack, the plan says %d", c.table, name, loc[name], id)
			}
			rock := csConfigBlock(csContent(t, "scripts/skill_mining/configs/rocks.loc"), name)
			if !strings.Contains(rock, "name=Rocks") || !strings.Contains(rock, "op1=Mine") {
				t.Errorf("%s: %q", name, rock)
			}
		}
	}
	// The pickaxes' levels (pickaxe_checker.rs2 asks for the best one the account can use).
	pick := csContent(t, "scripts/skill_mining/configs/pickaxes.obj")
	for _, p := range doricPickaxes {
		name := strings.ToLower(strings.ReplaceAll(p.Name, " ", "_"))
		block := csConfigBlock(pick, name)
		if !strings.Contains(block, fmt.Sprintf("param=levelrequire,%d", p.Level)) {
			t.Errorf("%s: level %d is not in %q", p.Name, p.Level, block)
		}
	}
}

// The mine's rocks and the pickaxe spawn are where the plan walks: two clay rocks beside each other, copper and iron within a
// short walk, and the free Bronze pickaxe on its table.
func TestDoricFluffsContentMine(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"clay rock", "LOC", tile.Tile{X: 2986, Z: 3239}, DoricClayRocks[0]},
		{"clay rock", "LOC", tile.Tile{X: 2987, Z: 3240}, DoricClayRocks[1]},
		{"the Bronze pickaxe on the table", "OBJ", RimmingtonPickaxe, 1265},
		{"Doric", "NPC", DoricAnchor, NpcDoric},
		{"Bob", "NPC", tile.Tile{X: 3232, Z: 3203}, BobsAxes.NpcID},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z)
		}
	}
	count := func(ids []int, around tile.Tile, r int) int { return dfCountRows(t, "LOC", ids, around, r) }
	if n := count(DoricClayRocks, RimmingtonMine, 12); n != 2 {
		t.Errorf("%d clay rocks within 12 of the mine's middle %v, want the 2", n, RimmingtonMine)
	}
	if n := count(DoricCopperRocks, RimmingtonMine, 12); n < 4 {
		t.Errorf("%d copper rocks within 12 of the mine's middle %v", n, RimmingtonMine)
	}
	if n := count(DoricIronRocks, RimmingtonMine, 12); n < 5 {
		t.Errorf("%d iron rocks within 12 of the mine's middle %v", n, RimmingtonMine)
	}
	// Each ore's stand has its rocks within the miner's reach (doricRockSearch is 14) and the pickaxe spawn is in the mine's
	// neighbourhood, not across the town.
	for name, c := range map[string]struct {
		stand tile.Tile
		ids   []int
	}{"clay": {RimmingtonClay, DoricClayRocks}, "copper": {RimmingtonCopper, DoricCopperRocks}, "iron": {RimmingtonIron, DoricIronRocks}} {
		if n := count(c.ids, c.stand, 14); n == 0 {
			t.Errorf("no %s rock within 14 of its stand %v", name, c.stand)
		}
	}
	if tile.Chebyshev(RimmingtonPickaxe, RimmingtonMine) > 30 {
		t.Errorf("the pickaxe spawn %v is far from the mine %v", RimmingtonPickaxe, RimmingtonMine)
	}
}

// dfCountRows counts the section's rows of any of ids within r tiles (Chebyshev, ground floor) of a tile, reading each map square
// that overlaps the area once (csNearRows reads the square for every tile it asks about).
func dfCountRows(t *testing.T, section string, ids []int, around tile.Tile, r int) int {
	t.Helper()
	want := map[string]bool{}
	for _, id := range ids {
		want[strconv.Itoa(id)] = true
	}
	n := 0
	for mx := (around.X - r) >> 6; mx <= (around.X+r)>>6; mx++ {
		for mz := (around.Z - r) >> 6; mz <= (around.Z+r)>>6; mz++ {
			in := false
			for _, line := range strings.Split(csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", mx, mz)), "\n") {
				if strings.HasPrefix(line, "==== ") {
					in = line == "==== "+section+" ===="
					continue
				}
				head, tail, ok := strings.Cut(line, ":")
				if !in || !ok {
					continue
				}
				var lvl, lx, lz int
				if k, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); k != 3 || lvl != around.Level {
					continue
				}
				f := strings.Fields(tail)
				if len(f) > 0 && want[f[0]] && tile.Chebyshev(tile.Tile{X: mx<<6 + lx, Z: mz<<6 + lz, Level: lvl}, around) <= r {
					n++
				}
			}
		}
	}
	return n
}

var fluffsCrateRoll = regexp.MustCompile(`%fluffs_crate = 0_51_54_(\d+)_(\d+);`)

// Gertrude's Cat's scripts: the stages and the reward, the menus each stop steers, the 100 coins, the three gifts and their
// stages, the six crate coordinates, the fence and the strings the journal parser reads.
func TestDoricFluffsContentFluffsScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^fluffs_complete = 6", "^fluffs_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_fluffs/configs/quest_fluffs.constant")
	for name, v := range map[string]int{"not_started": FluffsNotStarted, "started": FluffsStarted, "paid_boy": FluffsPaidBoy,
		"gave_milk": FluffsGaveMilk, "gave_sardine": FluffsGaveSardine, "rescued": FluffsRescued} {
		if !strings.Contains(consts, fmt.Sprintf("^fluffs_%s = %d", name, v)) {
			t.Errorf("quest_fluffs.constant does not say %s = %d:\n%s", name, v, consts)
		}
	}
	main := csContent(t, "scripts/quests/quest_fluffs/scripts/quest_fluffs.rs2")
	for _, want := range []string{
		"inv_del(inv, coins, 100);", "%fluffs = ^fluffs_paid_boy;", "stat_advance(cooking, 15250);",
		// The payment branch that needs the brother close: the check and the arm without a menu.
		".npc_find(npc_coord, $other_boy, 3, ^vis_lineofsight) = true",
		// The three gifts, each at its stage, and the kitten's crate roll.
		"[opnpcu,gertrudescat]", "last_useitem = bucket_milk", "%fluffs ! ^fluffs_paid_boy", "%fluffs = ^fluffs_gave_milk;",
		"last_useitem = seasoned_sardine", "%fluffs ! ^fluffs_gave_milk", "%fluffs = ^fluffs_gave_sardine;",
		"%fluffs = ^fluffs_gave_sardine & last_useitem = gertrudekittens", "%fluffs = ^fluffs_rescued;",
		// The crates: four ticks, the kitten only while neither the pack nor the bank holds one, the message box before the item.
		"[opnpc1,kittens_mew]", "p_delay(4);", "npc_coord = %fluffs_crate & %fluffs = ^fluffs_gave_sardine & inv_total(inv, gertrudekittens) = 0 & inv_total(bank, gertrudekittens) = 0",
		`~mesbox("You find a kitten!");`, "You find nothing.",
		// The fence and the seasoning.
		"[oploc1,gertrudefence]", `~mesbox("You rub the doogle leaves over the sardine.");`, "inv_add(inv, seasoned_sardine, 1);",
		// Fluffs claws for 3 when clicked, which is why every gift is a use-on.
		"[opnpc1,gertrudescat]", "~damage_self(3);",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_fluffs.rs2 lacks %q", want)
		}
	}
	// The six crates the roll names are the six the plan searches.
	rolls := map[tile.Tile]bool{}
	for _, m := range fluffsCrateRoll.FindAllStringSubmatch(main, -1) {
		x, _ := strconv.Atoi(m[1])
		z, _ := strconv.Atoi(m[2])
		rolls[tile.Tile{X: 51*64 + x, Z: 54*64 + z}] = true
	}
	if len(rolls) != 6 {
		t.Fatalf("%d crate coordinates in the roll: %v", len(rolls), rolls)
	}
	for _, c := range FluffsCrateTiles {
		if !rolls[c] {
			t.Errorf("crate %v is not one of the roll's %v", c, rolls)
		}
		delete(rolls, c)
	}
	if len(rolls) != 0 {
		t.Errorf("the roll names crates the plan does not search: %v", rolls)
	}
	// The menus: Gertrude's one, then the brothers' two, in the order their conversation reaches them.
	gertrude := csContent(t, "scripts/areas/area_varrock/scripts/gertrude.rs2")
	for _, line := range GertrudeStop.Prefer {
		if !strings.Contains(gertrude, `"`+line+`"`) {
			t.Errorf("gertrude.rs2 has no option %q", line)
		}
	}
	for _, want := range []string{"%fluffs = ^fluffs_started;", "%fluffs = ^fluffs_complete;", "~gertrude_give_cat;", "queue(fluffs_complete, 0, 0);",
		"inv_add(inv, chocolate_cake, 1);", "if_close;"} {
		if !strings.Contains(gertrude, want) {
			t.Errorf("gertrude.rs2 lacks %q", want)
		}
	}
	last := -1
	for _, line := range ShilopStop.Prefer {
		i := strings.Index(main, `"`+line+`"`)
		if i < 0 {
			// The second menu's option is "Okay then, I'll pay.": the preference is a substring.
			i = strings.Index(main, line)
		}
		if i < 0 {
			t.Errorf("quest_fluffs.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	if !slicesEqual(ShilopStop.Prefer, WilloughStop.Prefer) {
		t.Error("the two brothers' conversations are the same script")
	}
	for _, f := range []string{"shilop.rs2", "wilough.rs2"} {
		if b := csContent(t, "scripts/areas/area_varrock/scripts/"+f); !strings.Contains(b, "@fluffs_boy_dialogue;") {
			t.Errorf("%s does not run fluffs_boy_dialogue", f)
		}
	}
	if LadderFacts["fluffs"].XP["cooking"] != 1525 || GertrudesCatRecord.QuestPoints != 1 {
		t.Errorf("record %+v xp %v: 15250 Cooking XP at 1x is 1525", GertrudesCatRecord, LadderFacts["fluffs"].XP)
	}
	// The ladder: the climb lands on the player's own x,z one level up (so the plan climbs from a walkable twin).
	ladders := csContent(t, "scripts/ladders+stairs/scripts/ladders.rs2")
	for _, want := range []string{"[oploc1,ladder]", "~climb_ladder(movecoord(coord(), 0, 1, 0), true);", "[oploc1,laddertop]",
		"~climb_ladder(movecoord(coord(), 0, -1, 0), false);", "p_telejump($coord);"} {
		if !strings.Contains(ladders, want) {
			t.Errorf("ladders.rs2 lacks %q", want)
		}
	}
	// The journal parser's needles are strings of the journal script (colour tags and line breaks stripped).
	jr := strings.ToLower(regexp.MustCompile(`@[a-z]{3}@`).ReplaceAllString(csContent(t, "scripts/quests/quest_fluffs/scripts/fluffs_journal.rs2"), ""))
	for _, n := range fluffsJournalNeedles {
		if !strings.Contains(jr, n.needle) {
			t.Errorf("fluffs_journal.rs2 has no %q (stage %d)", n.needle, n.stage)
		}
	}
	jd := strings.ToLower(regexp.MustCompile(`@[a-z]{3}@`).ReplaceAllString(csContent(t, "scripts/quests/quest_doric/scripts/doric_journal.rs2"), ""))
	for _, needle := range []string{"i need to collect some items", "i can start this quest by talking to", "quest complete"} {
		if !strings.Contains(jd, needle) {
			t.Errorf("doric_journal.rs2 has no %q", needle)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Every shop the plans name sells what they say, and the keeper is the npc the plan aims at.
func TestDoricFluffsContentShops(t *testing.T) {
	type shop struct {
		inv, npcFile, npc, inventory, stock string
	}
	for _, s := range []shop{
		{"scripts/areas/area_port_sarim/configs/port_sarim.inv", "scripts/areas/area_port_sarim/configs/port_sarim.npc", "gerrant", "", "stock9=raw_sardine,200,1"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "scripts/areas/area_lumbridge/configs/lumbridge.npc", "bob", "axeshop", "stock1=bronze_pickaxe,5,100"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "scripts/areas/area_lumbridge/configs/lumbridge.npc", "generalshopkeeper1", "generalshop1", "stock4=bucket_empty"},
	} {
		npc := csConfigBlock(csContent(t, s.npcFile), s.npc)
		owned := ""
		if _, after, ok := strings.Cut(npc, "param=owned_shop,"); ok {
			owned, _, _ = strings.Cut(after, "\n")
			owned = strings.TrimSpace(owned)
		}
		if owned == "" || (s.inventory != "" && owned != s.inventory) {
			t.Errorf("npc %s owns shop %q, want %q", s.npc, owned, s.inventory)
			continue
		}
		inv := csConfigBlock(csContent(t, s.inv), owned)
		if !strings.Contains(inv, s.stock) {
			t.Errorf("shop %s does not stock %q:\n%s", owned, s.stock, inv)
		}
	}
	if c := csContent(t, "scripts/skill_fishing/configs/fishing.obj"); !strings.Contains(csConfigBlock(c, "raw_sardine"), "cost=10") {
		t.Error("a raw sardine no longer costs 10 gp at base (the sardine line's price)")
	}
}

// The tiles: the keepers, the brothers, the leaves, the cat, the crates, the fence and the ladder.
func TestDoricFluffsContentTiles(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"Gertrude", "NPC", GertrudeAnchor, NpcGertrude},
		{"Shilop", "NPC", FluffsMarket, NpcShilop},
		{"Wilough", "NPC", tile.Tile{X: FluffsMarket.X, Z: FluffsMarket.Z + 1}, NpcWilough},
		{"Gerrant", "NPC", FluffsSardineShop.Anchor, FluffsSardineShop.NpcID},
		{"Fluffs", "NPC", FluffsSpot, NpcFluffs},
		{"the broken fence", "LOC", YardFence, LocYardFence},
		{"the ladder", "LOC", YardLadder, LocYardLadder},
		{"the ladder's top", "LOC", YardLadderTop, LocYardTop},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	for _, sp := range DoogleSpawns {
		if !csHasRow(csMapRows(t, "OBJ", sp), FluffsDoogleLeaves) {
			t.Errorf("no doogle leaves spawn at %v", sp)
		}
	}
	for _, cr := range FluffsCrateTiles {
		if !csHasRow(csMapRows(t, "NPC", cr), NpcKittenCrate) {
			t.Errorf("no crate NPC at %v", cr)
		}
	}
	// The fence is the south wall's one gap: the tile south of it is where the climb in starts, and its x is the loc's.
	if YardFence.X != YardOutside.X || YardFence.Z != YardOutside.Z+1 || YardInside.Z != YardFence.Z+1 || YardInside.X != YardFence.X {
		t.Errorf("fence %v outside %v inside %v", YardFence, YardOutside, YardInside)
	}
}

// The ids against the production config archive (RSB0T_CONFIG_JAG; skipped without it): a wrong id here sends an account to the
// wrong NPC or asks the provisioner for the wrong item, and the stackable flag is what the kits' slot arithmetic follows.
func TestDoricFluffsIDsMatchTheProductionConfig(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int]string{
		DoricClay: "Clay", DoricCopperOre: "Copper ore", DoricIronOre: "Iron ore",
		1265: "Bronze pickaxe", 1267: "Iron pickaxe", 1269: "Steel pickaxe", 1273: "Mithril pickaxe", 1271: "Adamant pickaxe", 1275: "Rune pickaxe",
		FluffsDoogleLeaves: "Doogle leaves", FluffsRawSardine: "Raw sardine", FluffsSeasonedSardine: "Seasoned sardine",
		FluffsKitten: "Fluffs' kitten", ObjBucketOfMilk: "Bucket of milk", ObjBucket: "Bucket", CoinsID: "Coins",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want {
			t.Errorf("obj %d is %q (ok %v), want %q", id, o.Name, ok, want)
		}
		if o.Noted() {
			t.Errorf("obj %d is a note", id)
		}
		if wantStack := id == CoinsID; o.Stackable != wantStack {
			t.Errorf("obj %d stackable %v, want %v (the kits' pack-slot arithmetic follows it)", id, o.Stackable, wantStack)
		}
	}
	hasOp := func(ops []string, op string) bool {
		for _, o := range ops {
			if strings.EqualFold(o, op) {
				return true
			}
		}
		return false
	}
	for id, c := range map[int]struct{ name, op string }{
		NpcDoric: {"Doric", "Talk-to"}, NpcGertrude: {"Gertrude", "Talk-to"}, NpcShilop: {"Shilop", "Talk-to"}, NpcWilough: {"Wilough", "Talk-to"},
		NpcKittenCrate: {"Crate", "Search"}, FluffsSardineShop.NpcID: {"Gerrant", "Trade"}, BobsAxes.NpcID: {"Bob", "Trade"},
		NpcFluffs: {"Gertrudes cat", "Pick-up"},
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != c.name {
			t.Errorf("npc %d is %q (ok %v), want %q", id, n.Name, ok, c.name)
			continue
		}
		if !hasOp(n.Ops[:], c.op) {
			t.Errorf("npc %d (%s) offers %v, want %q", id, n.Name, n.Ops, c.op)
		}
	}
	// Fluffs has no Talk-to and a claw behind Pick-up and Stroke: the module uses neither.
	if n, _ := cfg.Npc(NpcFluffs); hasOp(n.Ops[:], "Talk-to") {
		t.Errorf("Fluffs offers %v", n.Ops)
	}
	for id, c := range map[int]struct{ name, op string }{
		DoricClayRocks[0]: {"Rocks", "Mine"}, DoricClayRocks[1]: {"Rocks", "Mine"}, DoricCopperRocks[0]: {"Rocks", "Mine"}, DoricCopperRocks[1]: {"Rocks", "Mine"},
		DoricIronRocks[0]: {"Rocks", "Mine"}, DoricIronRocks[1]: {"Rocks", "Mine"},
		LocYardFence: {"Broken fence", "Climb-over"}, LocYardLadder: {"Ladder", "Climb-up"}, LocYardTop: {"Ladder", "Climb-down"},
	} {
		l, ok := cfg.Loc(id)
		if !ok || l.Name != c.name || !hasOp(l.Ops[:], c.op) {
			t.Errorf("loc %d is %q %v (ok %v), want %q offering %q", id, l.Name, l.Ops, ok, c.name, c.op)
		}
	}
}
