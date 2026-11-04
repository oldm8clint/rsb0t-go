package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage and reward the two fillers rest on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run CookSheepContent ./internal/scripts/questplan/

func csContent(t *testing.T, rel string) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to check the fillers' facts against it")
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Skipf("no %s under %s: %v", rel, root, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// csPackIDs reads a content pack file (`id=name` lines) into name -> id.
func csPackIDs(t *testing.T, name string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, line := range strings.Split(csContent(t, "pack/"+name+".pack"), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if id, err := strconv.Atoi(k); err == nil {
			out[v] = id
		}
	}
	return out
}

// csConfigBlock is the text of one [section] of a content config file.
func csConfigBlock(src, name string) string {
	i := strings.Index(src, "["+name+"]")
	if i < 0 {
		return ""
	}
	rest := src[i+len(name)+2:]
	if j := strings.Index(rest, "\n["); j >= 0 {
		return rest[:j]
	}
	return rest
}

// csJM2Cache holds the map files already read (a test walks hundreds of tiles of the same square).
var csJM2Cache = map[string]string{}

// csMapRows reads one section ("LOC", "NPC", "OBJ") of the map square holding (x, z) and returns the rows at that exact tile
// and level, each as the fields after the colon.
func csMapRows(t *testing.T, section string, at tile.Tile) [][]string {
	t.Helper()
	rel := fmt.Sprintf("maps/m%d_%d.jm2", at.X>>6, at.Z>>6)
	src, ok := csJM2Cache[rel]
	if !ok {
		src = csContent(t, rel)
		csJM2Cache[rel] = src
	}
	var rows [][]string
	in := false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== "+section+" ===="
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
		if lvl == at.Level && lx == at.X&63 && lz == at.Z&63 {
			rows = append(rows, strings.Fields(tail))
		}
	}
	return rows
}

func csHasRow(rows [][]string, id int) bool {
	for _, r := range rows {
		if len(r) > 0 && r[0] == strconv.Itoa(id) {
			return true
		}
	}
	return false
}

// csNearRows counts the rows of an id within r tiles (Chebyshev, same level) of a tile.
func csNearRows(t *testing.T, section string, id int, around tile.Tile, r int) int {
	t.Helper()
	n := 0
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			if csHasRow(csMapRows(t, section, tile.Tile{X: around.X + dx, Z: around.Z + dz, Level: around.Level}), id) {
				n++
			}
		}
	}
	return n
}

// The object, npc and loc ids the plans carry are the pack's.
func TestCookSheepContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "egg", ObjEgg}, {"obj", obj, "bucket_milk", ObjBucketOfMilk}, {"obj", obj, "pot_flour", ObjPotOfFlour},
		{"obj", obj, "bucket_empty", ObjBucket}, {"obj", obj, "pot_empty", ObjEmptyPot}, {"obj", obj, "grain", ObjGrain},
		{"obj", obj, "shears", ObjShears}, {"obj", obj, "wool", ObjWool}, {"obj", obj, "ball_of_wool", ObjBallOfWool},
		{"npc", npc, "cook", NpcCook}, {"npc", npc, "cow", NpcDairyCow}, {"npc", npc, "fred_the_farmer", NpcFred},
		{"npc", npc, "sheepunsheered", NpcWoollySheep}, {"npc", npc, "generalshopkeeper1", LumbridgeStore.NpcID},
		{"npc", npc, "wydin", WydinShop.NpcID}, {"npc", npc, "aemad", AemadStore.NpcID},
		{"loc", loc, "wheat", LocWheat}, {"loc", loc, "hopperlevers1", LocHopperContr}, {"loc", loc, "hopper_lumbridge", LocMillHopper},
		{"loc", loc, "millbase", LocFlourBin},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The quest stages, points and rewards, and the dialogue the stops steer.
func TestCookSheepContentScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^cook_complete = 2", "^sheep_complete = 22", "^cook_questpoints = 1", "^sheep_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	if c := csContent(t, "scripts/quests/quest_sheep/configs/quest_sheep.constant"); !strings.Contains(c, fmt.Sprintf("^sheep_last_wool = %d", SheepLastWool)) ||
		!strings.Contains(c, fmt.Sprintf("^sheep_started = %d", SheepStarted)) {
		t.Errorf("quest_sheep.constant does not say started = %d and last_wool = %d:\n%s", SheepStarted, SheepLastWool, c)
	}
	if SheepComplete != 22 || SheepBalls != 20 {
		t.Errorf("SheepComplete %d, SheepBalls %d", SheepComplete, SheepBalls)
	}

	cook := csContent(t, "scripts/quests/quest_cook/scripts/quest_cook.rs2")
	for _, want := range []string{
		"stat_advance(cooking, 3000)",
		// The hand-in wants all three in the pack and takes one of each.
		"inv_total(inv, pot_flour) > 0 & inv_total(inv, egg) > 0 & inv_total(inv, bucket_milk) > 0",
		"inv_del(inv, egg, 1)", "inv_del(inv, bucket_milk, 1)", "inv_del(inv, pot_flour, 1)",
	} {
		if !strings.Contains(cook, want) {
			t.Errorf("quest_cook.rs2 lacks %q", want)
		}
	}
	if CooksAssistantRecord.QuestPoints != 1 || LadderFacts["cook"].XP["cooking"] != 300 {
		t.Errorf("record %+v xp %v: 3000 Cooking XP at 1x is 300", CooksAssistantRecord, LadderFacts["cook"].XP)
	}
	// Every preferred option is a menu line of the script, in the order the conversation reaches them.
	last := -1
	for _, line := range CookStop.Prefer {
		i := strings.Index(cook, `"`+line+`"`)
		if i < 0 {
			t.Errorf("quest_cook.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}

	if !strings.Contains(cook, fmt.Sprintf("%%cookquest = %d;", CookStarted)) {
		t.Errorf("quest_cook.rs2 does not set %%cookquest = %d on the start", CookStarted)
	}
	cookJournal := csContent(t, "scripts/quests/quest_cook/scripts/cook_journal.rs2")
	for _, want := range []string{"I can start this quest by speaking to the @dre@Cook@dbl@", "bring him the following ingredients:", "@red@QUEST COMPLETE!", "%cookquest = 0", "%cookquest = 1"} {
		if !strings.Contains(cookJournal, want) {
			t.Errorf("cook_journal.rs2 lacks %q", want)
		}
	}

	sheep := csContent(t, "scripts/quests/quest_sheep/scripts/quest_sheep.rs2")
	if !strings.Contains(sheep, "stat_advance(crafting, 1500)") || !strings.Contains(sheep, "inv_add(inv, coins, 60)") {
		t.Errorf("quest_sheep.rs2 does not pay 1500 Crafting XP and 60 coins:\n%s", sheep)
	}
	if LadderFacts["sheep"].XP["crafting"] != 150 {
		t.Errorf("sheep XP %v: 1500 Crafting XP at 1x is 150", LadderFacts["sheep"].XP)
	}
	fred := csContent(t, "scripts/areas/area_lumbridge/scripts/fred_the_farmer.rs2")
	for _, want := range []string{
		"%sheep = ^sheep_started;", // yes okay
		"if(inv_total(inv, ball_of_wool) > 0 & %sheep ! ^sheep_last_wool)", "%sheep = calc(%sheep + 1);",
		"if(%sheep = ^sheep_last_wool & inv_total(inv, ball_of_wool) > 0)", "queue(sheep_complete, 0, 0);",
		"if_close;", // the dialogue is shut between the balls: the reason for CustomSheepHandIn
	} {
		if !strings.Contains(fred, want) {
			t.Errorf("fred_the_farmer.rs2 lacks %q", want)
		}
	}
	last = -1
	for _, line := range FredStop.Prefer {
		i := strings.Index(fred, `"`+line+`"`)
		if i < 0 {
			t.Errorf("fred_the_farmer.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	// The journal's own strings.
	journal := csContent(t, "scripts/quests/quest_sheep/scripts/sheep_journal.rs2")
	for _, want := range []string{"I need to collect <tostring($ballsToGo)> more @dre@balls of wool.", "calc(20 - calc(%sheep - 1))", "Farmer Fred"} {
		if !strings.Contains(journal, want) {
			t.Errorf("sheep_journal.rs2 lacks %q", want)
		}
	}
	// The sheep: a shearing succeeds three times in four and turns the sheep into [sheepsheered] for 100 ticks.
	shear := csContent(t, "scripts/general_use/scripts/shear_sheep.rs2")
	for _, want := range []string{"[opnpcu,sheepunsheered]", "random(4) = 0", "inv_add(inv, wool, 1)", "npc_changetype_keepall(sheepsheered, 100)"} {
		if !strings.Contains(shear, want) {
			t.Errorf("shear_sheep.rs2 lacks %q", want)
		}
	}
	// The cow takes the empty bucket with no dialogue.
	milk := csContent(t, "scripts/npc/scripts/cow_milking.rs2")
	if !strings.Contains(milk, "[opnpcu,cow]") || !strings.Contains(milk, "inv_add(inv, bucket_milk, 1)") {
		t.Errorf("cow_milking.rs2 does not milk [cow]:\n%s", milk)
	}
	// The mill: the messages cookMill waits for, the per-player varps, and the pot the bin needs.
	mill := csContent(t, "scripts/general_use/scripts/windmills.rs2")
	for _, want := range []string{"You put the grain in the hopper.", "There is already grain in the hopper.", "The grain slides down the chute.",
		"The flour bin downstairs is full, I should empty it first.", "You operate the empty hopper. Nothing interesting happens.",
		"[oploc1,_hoppercontrol]", "if (inv_total(inv, pot_empty) < 1)", "inv_add(inv, pot_flour, 1)"} {
		if !strings.Contains(mill, want) {
			t.Errorf("windmills.rs2 lacks %q", want)
		}
	}
	if hv := csContent(t, "scripts/general_use/configs/hopper.varp"); !strings.Contains(hv, "[hopper_full]\nscope=perm") || !strings.Contains(hv, "[flour]\nscope=perm") {
		t.Errorf("hopper.varp: %q", hv)
	}
	hop := csContent(t, "scripts/general_use/configs/hopper.loc")
	if b := csConfigBlock(hop, "hopperlevers1"); !strings.Contains(b, "name=Hopper controls") || !strings.Contains(b, "op1=Operate") ||
		!strings.Contains(b, "millbase_coord,0_49_51_30_43") {
		t.Errorf("hopperlevers1: %q", b)
	}
}

// Every shop the plans name sells what they say, and the keeper is the npc the plan aims at.
func TestCookSheepContentShops(t *testing.T) {
	type shop struct {
		inv, npcFile, npc, inventory, stock string
	}
	for _, s := range []shop{
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "scripts/areas/area_lumbridge/configs/lumbridge.npc", "generalshopkeeper1", "generalshop1", "stock1=pot_empty"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "scripts/areas/area_lumbridge/configs/lumbridge.npc", "generalshopkeeper1", "generalshop1", "stock3=shears"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "scripts/areas/area_lumbridge/configs/lumbridge.npc", "generalshopkeeper1", "generalshop1", "stock4=bucket_empty"},
		{"scripts/areas/area_port_sarim/configs/port_sarim.inv", "scripts/areas/area_port_sarim/configs/port_sarim.npc", "wydin", "wydinstore", "stock1=pot_flour,3,100"},
		{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "scripts/areas/area_ardougne_east/configs/ardougne_east.npc", "aemad", "adventurershop", "stock6=ball_of_wool,30,100"},
	} {
		inv := csConfigBlock(csContent(t, s.inv), s.inventory)
		if !strings.Contains(inv, s.stock) {
			t.Errorf("shop %s does not stock %q:\n%s", s.inventory, s.stock, inv)
		}
		npc := csConfigBlock(csContent(t, s.npcFile), s.npc)
		if !strings.Contains(npc, "param=owned_shop,"+s.inventory) {
			t.Errorf("npc %s does not own shop %s", s.npc, s.inventory)
		}
	}
	// Aemad's sell multiplier is the base cost times 1.3 or so: 2 gp balls stay a few coins each.
	if c := csContent(t, "scripts/skill_crafting/configs/crafting.obj"); !strings.Contains(csConfigBlock(c, "ball_of_wool"), "cost=2") {
		t.Error("a ball of wool no longer costs 2 gp at base")
	}
}

// The tiles: spawns, keepers, the Cook, Fred, the mill's floors and the castle's wheel.
func TestCookSheepContentTiles(t *testing.T) {
	loc := csPackIDs(t, "loc")
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"egg spawn A", "OBJ", CookEggSpawns[0], ObjEgg},
		{"egg spawn B", "OBJ", CookEggSpawns[1], ObjEgg},
		{"the farmhouse bucket", "OBJ", FarmBucketSpawn, ObjBucket},
		{"the kitchen pot", "OBJ", KitchenPotSpawn, ObjEmptyPot},
		{"Fred's farm shears", "OBJ", ShearsFarm, ObjShears},
		{"the Cook", "NPC", CookAnchor, NpcCook},
		{"Fred", "NPC", FredAnchor, NpcFred},
		{"the Lumbridge shop keeper", "NPC", LumbridgeStore.Anchor, LumbridgeStore.NpcID},
		{"Wydin", "NPC", WydinShop.Anchor, WydinShop.NpcID},
		{"Aemad", "NPC", AemadStore.Anchor, AemadStore.NpcID},
		{"the hopper", "LOC", tile.Tile{X: 3166, Z: 3307, Level: 2}, LocMillHopper},
		{"the hopper controls", "LOC", tile.Tile{X: 3166, Z: 3305, Level: 2}, LocHopperContr},
		{"the flour bin", "LOC", MillBin, LocFlourBin},
		{"the castle's spinning wheel", "LOC", tile.Tile{X: 3209, Z: 3212, Level: 1}, loc["spinningwheel"]},
		{"the mill ladder, ground floor", "LOC", tile.Tile{X: 3164, Z: 3307}, 1747},
		{"the mill ladder, top floor", "LOC", tile.Tile{X: 3164, Z: 3307, Level: 2}, 1746},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	// The mill stand is on the top floor, one step from the ladder, beside the hopper's west stand and the controls' (the real
	// pack's floor tiles are checked in questmods); the wheat field is where the plan says.
	if MillTop.Level != 2 || tile.Chebyshev(MillTop, tile.Tile{X: 3164, Z: 3307}) != 1 {
		t.Errorf("MillTop %v", MillTop)
	}
	if n := csNearRows(t, "LOC", LocWheat, MillWheat, 4); n < 4 {
		t.Errorf("%d wheat stalks within 4 of %d,%d, want a field", n, MillWheat.X, MillWheat.Z)
	}
	// The dairy field and the sheep pen are populated.
	if n := csNearRows(t, "NPC", NpcDairyCow, DairyField, 14); n < 4 {
		t.Errorf("%d dairy cows within 14 of %d,%d", n, DairyField.X, DairyField.Z)
	}
	if n := csNearRows(t, "NPC", NpcWoollySheep, tile.Tile{X: 3202, Z: 3266}, 10); n < 15 {
		t.Errorf("%d woolly sheep in the Lumbridge pen, want the 17 that graze there", n)
	}
}
