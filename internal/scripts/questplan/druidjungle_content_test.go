package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward, drop and hunting rule Druidic Ritual and Jungle Potion rest on, read from a Content 289
// checkout (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived
// from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run DruidJungleContent ./internal/scripts/questplan/

// dsFragmentsIn checks that every |-separated line of a fixture page is part of the script that renders it.
func dsFragmentsIn(t *testing.T, script, what string, pages ...string) {
	t.Helper()
	for _, page := range pages {
		for _, frag := range strings.Split(page, "|") {
			if frag == "" {
				continue
			}
			if !strings.Contains(script, frag) {
				t.Errorf("%s: %q is not in the journal script", what, frag)
			}
		}
	}
}

// dsSpawns reads the NPC rows of the content maps over a box, by npc id.
func dsSpawns(t *testing.T, x0, x1, z0, z1 int) map[int][]tile.Tile {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to read the npc spawns")
	}
	out := map[int][]tile.Tile{}
	for mx := x0 >> 6; mx <= x1>>6; mx++ {
		for mz := z0 >> 6; mz <= z1>>6; mz++ {
			b, err := os.ReadFile(filepath.Join(root, "maps", fmt.Sprintf("m%d_%d.jm2", mx, mz)))
			if err != nil {
				continue
			}
			in := false
			for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
				if strings.HasPrefix(line, "==== ") {
					in = line == "==== NPC ===="
					continue
				}
				if !in {
					continue
				}
				var lvl, lx, lz, id int
				if n, _ := fmt.Sscanf(line, "%d %d %d: %d", &lvl, &lx, &lz, &id); n == 4 {
					at := tile.Tile{X: mx*64 + lx, Z: mz*64 + lz, Level: lvl}
					if at.X >= x0 && at.X <= x1 && at.Z >= z0 && at.Z <= z1 {
						out[id] = append(out[id], at)
					}
				}
			}
		}
	}
	return out
}

// The object, npc and loc ids the plans carry are the pack's.
func TestDruidJungleContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	type c struct {
		kind string
		ids  map[string]int
		name string
		want int
	}
	cs := []c{
		{"obj", obj, "raw_beef", ObjRawBeef}, {"obj", obj, "raw_chicken", ObjRawChicken}, {"obj", obj, "raw_rat_meat", ObjRawRatMeat},
		{"obj", obj, "raw_bear_meat", ObjRawBearMeat}, {"obj", obj, "enchanted_beef", ObjEnchantedBeef}, {"obj", obj, "enchanted_chicken", ObjEnchantedChicken},
		{"obj", obj, "enchanted_rat_meat", ObjEnchantedRatMeat}, {"obj", obj, "enchanted_bear_meat", ObjEnchantedBearMeat}, {"obj", obj, "coins", CoinsID},
		{"obj", obj, "shark", ObjShark},
		{"npc", npc, "kaqemeex", NpcKaqemeex}, {"npc", npc, "sanfew", NpcSanfew}, {"npc", npc, "trufitus", NpcTrufitus},
		{"npc", npc, "suit_of_armour", NpcSuitOfArmour}, {"npc", npc, "brownbear", NpcBrownBear}, {"npc", npc, "giantrat1", NpcGiantRat},
		{"npc", npc, "giantrat", NpcGiantRatCub}, {"npc", npc, "chicken", NpcChicken}, {"npc", npc, "cow", NpcDairyCow},
		{"npc", npc, "jogre", NpcJogre}, {"npc", npc, "jungle_spider", NpcJungleSpider}, {"npc", npc, "tribesman", NpcTribesman},
		{"loc", loc, "cauldron_of_thunder", LocCauldronOfThunder}, {"loc", loc, "cauldrondoor", LocCauldronDoor}, {"loc", loc, "cauldrondoor_l", LocCauldronDoorLeft},
		{"loc", loc, "suitofarmour_darkknight", LocSuitOfArmourLoc}, {"loc", loc, "pothole_cave_entrance", LocJunglePothole}, {"loc", loc, "jp_caverocksout", LocJungleHandHolds},
	}
	for i, h := range JungleHerbs {
		slug := []string{"snake_weed", "ardrigal", "sito_foil", "volencia_moss", "rogues_purse"}[i]
		locName := []string{"snake_vine_full", "ardrigal_palm_full", "sito_soil_full", "volencia_moss_rock_full", "rogues_purse_cave_full"}[i]
		cs = append(cs, c{"obj", obj, "unidentified_" + slug, h.Unid}, c{"obj", obj, slug, h.Clean}, c{"loc", loc, locName, h.Loc})
	}
	for _, m := range DruidMeats {
		_ = m
	}
	for _, x := range cs {
		if got, ok := x.ids[x.name]; !ok || got != x.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", x.kind, x.name, got, ok, x.want)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Druidic Ritual
// ---------------------------------------------------------------------------------------------------------------------

func TestDruidJungleContentDruidicRitualScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^druid_complete = %d", DruidComplete), "^druid_questpoints = 4"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_druid/configs/quest_druid.constant")
	for name, v := range map[string]int{"not_started": DruidNotStarted, "started": DruidStarted, "spoken_sanfew": DruidSpokenSanfew, "given_ingredients": DruidGivenIngredients} {
		if want := fmt.Sprintf("^druid_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_druid.constant lacks %q", want)
		}
	}
	// The reward: 2500 at 10x is the 250 Herblore experience of the ladder row, which is level 3: Jungle Potion's Herblore.
	script := csContent(t, "scripts/quests/quest_druid/scripts/quest_druid.rs2")
	for _, want := range []string{"stat_advance(herblore, 2500);", "%druidquest = ^druid_complete;", "~send_quest_complete(questlist:druid, marentill, 220, ^druid_questpoints,"} {
		if !strings.Contains(script, want) {
			t.Errorf("quest_druid.rs2 lacks %q", want)
		}
	}
	if LadderFacts[DruidicRitualID].XP["herblore"] != 250 || LadderFacts[DruidicRitualID].QP != 4 {
		t.Errorf("the ladder row pays %v and %d quest points", LadderFacts[DruidicRitualID].XP, LadderFacts[DruidicRitualID].QP)
	}
	// 250 experience is Herblore 3 (174 for level 3, 276 for level 4): the level Jungle Potion's herbs are identified at.
	if xp := LadderFacts[DruidicRitualID].XP["herblore"]; xp < 174 || xp >= 276 {
		t.Errorf("%d Herblore XP is not level 3", xp)
	}
	// The cauldron works only at Sanfew's stage, and turns each raw meat into its enchanted one with one use.
	if !strings.Contains(script, "[oplocu,cauldron_of_thunder]") || !strings.Contains(script, "if(%druidquest ! ^druid_spoken_sanfew) {\n    ~displaymessage(^dm_default);\n    return;\n}") {
		t.Error("the cauldron is not stage-gated as the plan says")
	}
	obj := csPackIDs(t, "obj")
	byID := map[int]string{}
	for name, id := range obj {
		byID[id] = name
	}
	for _, m := range DruidMeats {
		raw, ench := byID[m.Raw], byID[m.Enchanted]
		if !strings.Contains(script, "$useitem = "+raw+")") || !strings.Contains(script, "inv_del(inv, "+raw+", 1);\n    inv_add(inv, "+ench+", 1);") {
			t.Errorf("the cauldron does not turn %s into %s", raw, ench)
		}
	}
	// Sanfew takes the four enchanted meats at once, without a menu, and Kaqemeex finishes without one.
	sanfew := csContent(t, "scripts/areas/area_taverly/scripts/sanfew.rs2")
	for _, want := range []string{
		"[opnpc1,sanfew]\nif(%druidquest = ^druid_spoken_sanfew) {\n    @druid_sanfew_give_ingredients;\n}",
		"if(inv_total(inv, enchanted_beef) > 0 & inv_total(inv, enchanted_rat_meat) > 0 & inv_total(inv, enchanted_bear_meat) > 0& inv_total(inv, enchanted_chicken) > 0) {",
		"%druidquest = ^druid_given_ingredients;",
	} {
		if !strings.Contains(sanfew, want) {
			t.Errorf("sanfew.rs2 lacks %q", want)
		}
	}
	kaq := csContent(t, "scripts/areas/area_taverly/scripts/kaqemeex.rs2")
	if !strings.Contains(kaq, "} else if(%druidquest = ^druid_given_ingredients) {\n    @druid_completion;") {
		t.Error("Kaqemeex does not finish the quest from stage 3 without a menu")
	}
	// Every preferred option is a menu line of the script, in the order the conversation reaches them.
	for _, c := range []struct {
		script string
		stop   NpcStop
	}{{kaq, KaqemeexStartStop}, {sanfew, SanfewBriefStop}} {
		last := -1
		for _, line := range c.stop.Prefer {
			i := strings.Index(c.script, `"`+line+`"`)
			if i < 0 {
				t.Errorf("%s has no option %q", c.stop.Npc, line)
			}
			if i < last {
				t.Errorf("%s: option %q comes before the previous preference in the script", c.stop.Npc, line)
			}
			last = i
		}
	}
	if !strings.Contains(sanfew, `"I'll get on with it."`) {
		t.Error(`sanfew.rs2 has no option "I'll get on with it."`)
	}
	// The journal pages: every fragment of every fixture is in the script that renders it.
	journal := csContent(t, "scripts/quests/quest_druid/scripts/druid_journal.rs2")
	for stage, page := range druidJournalFixtures {
		dsFragmentsIn(t, journal, fmt.Sprintf("druid stage %d", stage), page)
	}
	for _, needle := range []string{"claim my", "told me for the ritual", "should speak to", "can start this quest by speaking to"} {
		if !strings.Contains(strings.ToLower(journal), needle) {
			t.Errorf("druid_journal.rs2 lacks %q", needle)
		}
	}
}

// The four animals drop their meat every time, whatever the roll, and the npcs the plan names are those animals.
func TestDruidJungleContentTheMeatsDropEveryTime(t *testing.T) {
	obj := csPackIDs(t, "obj")
	byID := map[int]string{}
	for name, id := range obj {
		byID[id] = name
	}
	npcIDs := csPackIDs(t, "npc")
	npcName := map[int]string{}
	for name, id := range npcIDs {
		npcName[id] = name
	}
	blocks := npcBlocks(t, contentRoot(t))
	for _, c := range []struct {
		meat     string
		script   string
		category string
	}{
		{"bear", "scripts/drop tables/scripts/bear.rs2", "bear"}, {"rat", "scripts/drop tables/scripts/giant_rat.rs2", "giantrat"},
		{"chicken", "scripts/drop tables/scripts/chicken.rs2", "chicken"}, {"beef", "scripts/drop tables/scripts/cow.rs2", "cow"},
	} {
		var m DruidMeat
		for _, x := range DruidMeats {
			if x.Name == c.meat {
				m = x
			}
		}
		src := csContent(t, c.script)
		// At the start of a line: not inside a roll's branch.
		if want := "\nobj_add(npc_coord, " + byID[m.Raw] + ", 1, ^lootdrop_duration);"; !strings.Contains(src, want) {
			t.Errorf("%s lacks the unconditional %q", c.script, want)
		}
		if !strings.Contains(src, "[ai_queue3,_"+c.category+"]") {
			t.Errorf("%s does not handle the %s category", c.script, c.category)
		}
		for _, id := range m.Npcs {
			if got := blocks[npcName[id]]["category"]; got != c.category {
				t.Errorf("npc %d (%s) is category %q, want %q: its death does not run %s", id, npcName[id], got, c.category, c.script)
			}
		}
	}
	// The bear is level 21 and hunts only adjacent accounts of combat 42 or less; the rat and the others are harmless.
	bear := blocks["brownbear"]
	if bear["vislevel"] != "21" || bear["huntmode"] != "cowardly" || bear["huntrange"] != "1" {
		t.Errorf("the bear is %v", bear)
	}
	// The raw beef and chicken are on Wydin's shelf, one of each.
	port := csContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.inv")
	wydin := csConfigBlock(port, "wydinstore")
	for _, want := range []string{"stock2=raw_beef,1,100", "stock3=raw_chicken,1,100"} {
		if !strings.Contains(wydin, want) {
			t.Errorf("[wydinstore] lacks %q", want)
		}
	}
	// The enchanted forms are untradeable: never asked of the mule, never bought.
	enchanted := csContent(t, "scripts/quests/quest_druid/configs/quest_druid.obj")
	for _, m := range DruidMeats {
		if blk := csConfigBlock(enchanted, byID[m.Enchanted]); !strings.Contains(blk, "tradeable=no") {
			t.Errorf("%s is tradeable", byID[m.Enchanted])
		}
	}
}

// The hunting grounds hold the animals the plan sends the account to, and the druids stand where it walks.
func TestDruidJungleContentTheMapsHoldWhatTheHuntsLookFor(t *testing.T) {
	for _, m := range DruidMeats {
		near := 0
		for _, id := range m.Npcs {
			near += csNearRows(t, "NPC", id, m.Field, m.Radius)
		}
		if near < 2 {
			t.Errorf("the %s field %v (radius %d) holds %d of npcs %v", m.Name, m.Field, m.Radius, near, m.Npcs)
		}
		for _, sp := range m.Spawns {
			if tile.Chebyshev(sp, m.Field) > m.Radius {
				t.Errorf("the %s spawn %v lies outside the field's radius %d of %v", m.Name, sp, m.Radius, m.Field)
			}
			found := false
			for _, id := range m.Npcs {
				found = found || csHasRow(csMapRows(t, "NPC", sp), id)
			}
			if !found {
				t.Errorf("the %s spawn %v holds none of npcs %v", m.Name, sp, m.Npcs)
			}
		}
	}
	// The dairy field holds six cows (the farm's chickens five and more, the rats ten, the bear's two spawns are the only ones in the
	// region): enough that an animal another account has just killed is not the only one.
	if n := csNearRows(t, "NPC", NpcDairyCow, DruidMeats[3].Field, DruidMeats[3].Radius); n < 5 {
		t.Errorf("%d cows in the dairy field", n)
	}
	for _, c := range []struct {
		npc   int
		at    tile.Tile
		level int
		what  string
	}{{NpcKaqemeex, KaqemeexAnchor, 0, "Kaqemeex"}, {NpcSanfew, SanfewAnchor, 1, "Sanfew"}} {
		if !csHasRow(csMapRows(t, "NPC", c.at), c.npc) {
			t.Errorf("%s does not stand at %v", c.what, c.at)
		}
	}
	// The door's suits: the two locs the script looks for, where the script says, and the script's own words.
	door := csContent(t, "scripts/areas/area_taverly/dungeon/scripts/prison_doors.rs2")
	for _, want := range []string{
		"def_coord $southern_position = 0_45_153_7_37;", "def_coord $northern_position = 0_45_153_7_40;",
		"if(.loc_find($northern_position, suitofarmour_darkknight) = true) {\n        .loc_del(500);\n        mes(\"Suddenly the suit of armour comes to life!\");\n        npc_add($northern_position, suit_of_armour, 500);\n        ~npc_retaliate(0);\n        return;",
		"if(.loc_find($southern_position, suitofarmour_darkknight) = true) {",
		"[oploc1,cauldrondoor] @taverley_dungeon_open_doors(^right);", "[oploc1,cauldrondoor_l] @taverley_dungeon_open_doors(^left);",
	} {
		if !strings.Contains(door, want) {
			t.Errorf("prison_doors.rs2 lacks %q", want)
		}
	}
	if SuitNorth != (tile.Tile{X: 45*64 + 7, Z: 153*64 + 40}) || SuitSouth != (tile.Tile{X: 45*64 + 7, Z: 153*64 + 37}) {
		t.Errorf("the suits stand at %v and %v", SuitNorth, SuitSouth)
	}
	for _, at := range []tile.Tile{SuitNorth, SuitSouth} {
		if !csHasRow(csMapRows(t, "LOC", at), LocSuitOfArmourLoc) {
			t.Errorf("no suit of armour loc at %v in the map", at)
		}
	}
	// The cauldron and the door's leaves stand where the plan walks.
	for _, c := range []struct {
		id int
		at tile.Tile
	}{{LocCauldronOfThunder, tile.Tile{X: 2893, Z: 9831}}, {LocCauldronDoor, tile.Tile{X: 2889, Z: 9831}}, {LocCauldronDoorLeft, tile.Tile{X: 2889, Z: 9830}}} {
		if !csHasRow(csMapRows(t, "LOC", c.at), c.id) {
			t.Errorf("no loc %d at %v in the map", c.id, c.at)
		}
	}
}

// The corridor the cauldron's door stands at the end of holds what the price counts: the unarmed skeletons the planned line passes
// (DruidCorridorSkeletons, both ways) and the two it leaves out (one three tiles off the line, the armed one five from the door), the
// config's hunt ranges and levels the gauntlet rests on, the hunt rule that makes it a gauntlet for an account of combat level 50 or
// less, and the single-combat ground that makes the fights come one at a time.
func TestDruidJungleContentTheCorridorsSkeletons(t *testing.T) {
	sp := dsSpawns(t, 2870, 2900, 9790, 9840)
	var unarmed []tile.Tile
	for _, at := range sp[NpcManorSkeleton] {
		if at.Level == 0 {
			unarmed = append(unarmed, at)
		}
	}
	want := append(append([]tile.Tile(nil), DruidCorridorSkeletons...), tile.Tile{X: 2882, Z: 9826})
	for _, at := range want {
		if !slices.Contains(unarmed, at) {
			t.Errorf("no unarmed skeleton at %v in the map", at)
		}
	}
	if len(unarmed) != len(want) {
		t.Errorf("the corridor's map holds %d unarmed skeletons %v; the plan counts %d (%d priced and the one beside the line)", len(unarmed), unarmed, len(want), len(DruidCorridorSkeletons))
	}
	if armed := sp[92]; len(armed) != 1 || armed[0] != (tile.Tile{X: 2884, Z: 9836}) {
		t.Errorf("the armed skeleton is %v", armed)
	}
	// The priced spawns run up the corridor in the order the price meets them, each within its reach of the next's wander.
	for i := 1; i < len(DruidCorridorSkeletons); i++ {
		if DruidCorridorSkeletons[i].Z < DruidCorridorSkeletons[i-1].Z {
			t.Errorf("DruidCorridorSkeletons is out of order at %v", DruidCorridorSkeletons[i])
		}
	}
	blocks := npcBlocks(t, contentRoot(t))
	unarmedCfg, armedCfg, spider := blocks["skeleton_unarmed"], blocks["skeleton_armed"], blocks["poisonspider"]
	if unarmedCfg["vislevel"] != "25" || unarmedCfg["huntrange"] != "1" || unarmedCfg["wanderrange"] != "8" || unarmedCfg["huntmode"] != "cowardly" || unarmedCfg["respawnrate"] != "140" {
		t.Errorf("the unarmed skeleton is %v", unarmedCfg)
	}
	if armedCfg["vislevel"] != "22" || armedCfg["huntrange"] != "1" || armedCfg["wanderrange"] != "9" || armedCfg["huntmode"] != "cowardly" {
		t.Errorf("the armed skeleton is %v", armedCfg)
	}
	// The poison spiders are not priced: level 64, a wander range of 10 round spawns the nearest of which lies nine tiles off the line.
	if spider["vislevel"] != "64" || spider["wanderrange"] != "10" || spider["huntrange"] != "1" {
		t.Errorf("the poison spider is %v", spider)
	}
	// An account of combat level 50 or less is hunted by a level 25 skeleton: [cowardly] checks nottoostrong, and the engine's check is
	// twice the npc's level (Npc.ts huntPlayers).
	hunt := csContent(t, "scripts/_unpack/225/all.hunt")
	i := strings.Index(hunt, "[cowardly]")
	if i < 0 {
		t.Fatal("no [cowardly] hunt mode")
	}
	block := hunt[i:]
	if j := strings.Index(block[1:], "\n["); j >= 0 {
		block = block[:j+1]
	}
	for _, w := range []string{"type=player", "check_vis=lineofsight", "check_nottoostrong=outside_wilderness", "find_keephunting=off", "find_newmode=opplayer2"} {
		if !strings.Contains(block, w) {
			t.Errorf("[cowardly] lacks %q:\n%s", w, block)
		}
	}
	// The corridor is single-combat ground: no zone of the Taverley dungeon's squares is in multiway.csv, so the skeletons find the
	// account one at a time (a hunter skips an account in combat within the last eight ticks) and the price is a chain.
	for _, line := range strings.Split(csContent(t, "maps/multiway.csv"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "0_44_153_") || strings.HasPrefix(line, "0_45_153_") {
			t.Errorf("multiway.csv holds %s: the corridor is multi-combat, so the skeletons come together and the chain price is too kind", line)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Jungle Potion
// ---------------------------------------------------------------------------------------------------------------------

// jpJournalFixtures are the pages junglepotion_journal.rs2 renders, by the stage the parser reads from them. A found_X page reads
// the pack, so a stage has up to three: the herb's clean copy held, the unidentified one held, neither.
var jpHeader = "@str@I spoke to Trufitus, he needs to commune with the|@str@gods, he's asked me to help him by collecting herbs.||"

var jpJournalFixtures = []struct {
	name  string
	stage int
	page  string
}{
	{"not started", JungleNotStarted, "@dbl@I can start this quest by speaking to @dre@Trufitus Shakaya|@dbl@who lives in the main hut in @dre@Tai Bwo Wannai|@dbl@village on the island of @dre@Karamja."},
	{"get snake weed", 1, jpHeader + "@dbl@I need to pick some fresh @dre@Snakeweed@dbl@ for @dre@Trufitus@dbl@."},
	{"found snake weed, clean in the pack", 2, jpHeader + "@str@I picked some fresh Snakeweed for Trufitus.||" + "@dbl@I need to give the @dre@Snake Weed@dbl@ to @dre@Trufitus@dbl@."},
	{"found snake weed, unidentified in the pack", 2, jpHeader},
	{"found snake weed, nothing in the pack", 1, jpHeader + "@dbl@I need to pick some fresh @dre@Snakeweed@dbl@ for @dre@Trufitus@dbl@."},
	{"get ardrigal", 3, jpHeader + "@str@I've given Snakeweed to Trufitus.||" + "@dbl@I need to pick some fresh @dre@Ardrigal@dbl@ for @dre@Trufitus@dbl@."},
	{"found ardrigal, clean in the pack", 4, jpHeader + "@str@I've given Snakeweed to Trufitus.|" + "@str@I picked some fresh Ardrigal for Trufitus.||" + "@dbl@I need to give the @dre@Ardrigal@dbl@ to @dre@Trufitus@dbl@."},
	{"get sito foil", 5, jpHeader + "@str@I've given Snakeweed and Ardrigal to Trufitus.||" + "@dbl@I need to pick some fresh @dre@Sito Foil@dbl@ for @dre@Trufitus@dbl@."},
	{"found sito foil, clean in the pack", 6, jpHeader + "@str@I've given Snakeweed and Ardrigal to Trufitus.|" + "@str@I picked some fresh Sito Foil for Trufitus.||" + "@dbl@I need to give the @dre@Sito Foil@dbl@ to @dre@Trufitus@dbl@."},
	{"get volencia moss", 7, jpHeader + "@str@I've given Snakeweed, Ardrigal and Sito Foil|@str@to Trufitus.||" + "@dbl@I need to pick some fresh @dre@Volencia Moss@dbl@ for @dre@Trufitus@dbl@."},
	{"found volencia moss, clean in the pack", 8, jpHeader + "@str@I've given Snakeweed, Ardrigal and Sito Foil|@str@to Trufitus.||" + "@str@I picked some fresh Volencia Moss for Trufitus.||" +
		"@dbl@I need to give the @dre@Volencia Moss@dbl@ to @dre@Trufitus@dbl@."},
	{"get rogues purse", 9, jpHeader + "@str@I've given Snakeweed, Ardrigal, Sito Foil|@str@and Volencia Moss to Trufitus.||" + "@dbl@I need to pick some fresh @dre@Rogues Purse@dbl@ for @dre@Trufitus@dbl@."},
	{"found rogues purse, clean in the pack", 10, jpHeader + "@str@I've given Snakeweed, Ardrigal, Sito Foil|@str@and Volencia Moss to Trufitus.||" + "@str@I picked some fresh Rogues Purse for Trufitus.||" +
		"@dbl@I need to give the @dre@Rogue Purse@dbl@ to @dre@Trufitus@dbl@."},
	{"all the herbs given", JungleFoundAllHerbs, jpHeader + "@str@I have given Trufitus Snakeweed, Ardrigal,|@str@Sito Foil,Volencia moss and Rogues purse.||" + "@str@Trufitus needs to commune with the gods.||" + "@dbl@I should speak to @dre@Trufitus@dbl@."},
	{"complete", JungleComplete, "@str@Trufitus Shakaya of the Tai Bwo Wannai village needed|@str@some jungle herbs in order to make a potion which would|@str@help him commune with the gods. I collected five lots|" +
		"@str@of jungle herbs for him and he was able to|@str@commune with the gods.||@str@As a reward he showed me some herblore techniques.||@red@QUEST COMPLETE!"},
}

func TestParseJunglePotionJournalReadsEveryRenderedPage(t *testing.T) {
	for _, c := range jpJournalFixtures {
		for _, lines := range [][]string{{c.page}, strings.Split(c.page, "|")} {
			got, ok := ParseJunglePotionJournal(lines)
			if !ok || got != c.stage {
				t.Errorf("%s: parsed %d %v, want %d", c.name, got, ok, c.stage)
			}
		}
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}, {jpHeader + "@str@I've given Snakeweed to Trufitus."}} {
		if got, ok := ParseJunglePotionJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

func TestDruidJungleContentJunglePotionScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^junglepotion_complete = %d", JungleComplete), "^junglepotion_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_junglepotion/configs/quest_junglepotion.constant")
	for i, herb := range []string{"snake_weed", "ardrigal", "sito_foil", "volencia_moss", "rogues_purse"} {
		for off, kind := range []string{"get", "found"} {
			if want := fmt.Sprintf("^junglepotion_%s_%s = %d", kind, herb, 2*i+1+off); !strings.Contains(consts, want) {
				t.Errorf("quest_junglepotion.constant lacks %q", want)
			}
		}
	}
	if want := fmt.Sprintf("^junglepotion_found_all_herbs = %d", JungleFoundAllHerbs); !strings.Contains(consts, want) {
		t.Errorf("quest_junglepotion.constant lacks %q", want)
	}
	script := csContent(t, "scripts/quests/quest_junglepotion/scripts/quest_junglepotion.rs2")
	for _, want := range []string{
		"stat_advance(herblore, 7750);", "%junglepotion = ^junglepotion_complete;",
		// A pick advances the stage only at its own get_ stage, and the shared locs empty for a hundred ticks.
		"case unidentified_snake_weed : if (%junglepotion = ^junglepotion_get_snake_weed) %junglepotion = ^junglepotion_found_snake_weed;",
		"case unidentified_rogues_purse : if (%junglepotion = ^junglepotion_get_rogues_purse) %junglepotion = ^junglepotion_found_rogues_purse;",
		"loc_change($loc, 100);", "~junglepotion_pick_herb(\"vine\", unidentified_snake_weed, snake_vine_empty);",
		"~junglepotion_pick_herb(\"palm\", unidentified_ardrigal, ardrigal_palm_empty);", "~junglepotion_pick_herb(\"scorched earth\", unidentified_sito_foil, sito_soil_empty);",
		"~junglepotion_pick_herb(\"rock\", unidentified_volencia_moss, volencia_moss_rock_empty);", "~junglepotion_pick_herb(\"wall\", unidentified_rogues_purse, null);",
		// The cave: the options, the landings.
		`def_int $option = ~p_choice2_header("Yes, I'll enter the cave.", 1, "No thanks, I'll give it a miss.", 2, "Would you like to enter the caves?");`,
		"p_telejump(0_44_148_14_48);", "p_telejump(0_44_48_7_48);", "[oploc2,pothole_cave_entrance]", "[oploc1,jp_caverocksout]",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("quest_junglepotion.rs2 lacks %q", want)
		}
	}
	if JungleCaveArrival != (tile.Tile{X: 44*64 + 14, Z: 148*64 + 48}) || JungleCaveSurface != (tile.Tile{X: 44*64 + 7, Z: 48*64 + 48}) {
		t.Errorf("the cave's landings are %v and %v", JungleCaveArrival, JungleCaveSurface)
	}
	if LadderFacts[JunglePotionID].XP["herblore"] != 775 || LadderFacts[JunglePotionID].QP != 1 {
		t.Errorf("the ladder row pays %v and %d quest points", LadderFacts[JunglePotionID].XP, LadderFacts[JunglePotionID].QP)
	}
	// Every herb is identified at Herblore 3, which Druidic Ritual's 250 experience is.
	herbs := csContent(t, "scripts/skill_herblore/configs/herbs.obj")
	for _, slug := range []string{"snake_weed", "ardrigal", "sito_foil", "volencia_moss", "rogues_purse"} {
		blk := csConfigBlock(herbs, slug)
		if !strings.Contains(blk, "param=identified_herb_level,3\n") {
			t.Errorf("%s is not identified at Herblore 3: %q", slug, blk)
		}
	}
	// Trufitus: the conversations and what he asks.
	truf := csContent(t, "scripts/areas/area_karamja/scripts/trufitus.rs2")
	last := -1
	for _, line := range TrufitusStartStop.Prefer {
		i := strings.Index(truf, `"`+line+`"`)
		if i < 0 {
			t.Errorf("trufitus.rs2 has no option %q", line)
		}
		if i < last {
			t.Errorf("option %q comes before the previous preference in the script", line)
		}
		last = i
	}
	for _, want := range []string{
		`def_int $option6 = ~p_choice2("Of course!", 1, "Not yet, sorry, what's the clue again?", 2);`,
		// The start wants Druidic Ritual complete.
		"if (%druidquest = ^druid_complete) {", "%junglepotion = ^junglepotion_get_snake_weed;",
		// The clean herb is accepted at found_ only; the unidentified one is refused, and so is a clean one at a get_ stage.
		"case ^junglepotion_found_snake_weed :\n    %junglepotion = ^junglepotion_get_ardrigal;", "case ^junglepotion_found_rogues_purse :\n    // https://youtu.be/9LG-opZVpfY?si=fQ-DSP4VpQUVWiSb&t=835\n    %junglepotion = ^junglepotion_found_all_herbs;",
		"Sorry, Bwana, that herb is so dirty that I can't even tell whether it is fresh. Please clean it first.",
		"[label,trufitus_finish_quest]\n~mesbox(\"<npc_name> shows you some techniques in Herblore.|You gain some experience in Herblore.\");\nqueue(junglepotion_quest_complete, 0, 0);",
	} {
		if !strings.Contains(truf, want) {
			t.Errorf("trufitus.rs2 lacks %q", want)
		}
	}
	// The journal pages: every fragment of every fixture is in the script that renders it, so the needles cannot have been written
	// against text the content does not render.
	journal := csContent(t, "scripts/quests/quest_junglepotion/scripts/junglepotion_journal.rs2")
	for _, c := range jpJournalFixtures {
		dsFragmentsIn(t, journal, "jungle "+c.name, c.page)
	}
	// And the needles themselves are lines of it, with the spellings the parser relies on (Snakeweed in the pick line and Snake Weed
	// in the give line; Rogues Purse and Rogue Purse).
	low := strings.ToLower(journal)
	for _, herb := range JungleHerbs {
		for _, needle := range []string{herb.pick, herb.give} {
			// A needle is a line of the page with its colour tags dropped and the herb's name in the middle of it.
			stripped := low
			for _, tag := range []string{"@dbl@", "@dre@", "@str@", "@red@"} {
				stripped = strings.ReplaceAll(stripped, tag, "")
			}
			if !strings.Contains(stripped, strings.TrimSuffix(needle, " to trufitus")) {
				t.Errorf("the journal script never renders %q", needle)
			}
		}
	}
}

// The hunters' tables are the maps' own: every spider and tribesman the maps put on the ground the quest walks is a row of the
// plan's tables and every row of the tables is on the map; their wander and hunt ranges are what the pads assume.
func TestDruidJungleContentTheHostilesAreTheMapsOwn(t *testing.T) {
	sp := dsSpawns(t, 2740, 2900, 2975, 3200)
	for _, c := range []struct {
		id    int
		table []tile.Tile
		name  string
	}{{NpcJungleSpider, jungleSpiders, "spider"}, {NpcTribesman, jungleTribesmen, "tribesman"}} {
		var onMap []tile.Tile
		for _, at := range sp[c.id] {
			if at.Level == 0 {
				onMap = append(onMap, at)
			}
		}
		for _, at := range onMap {
			if !slices.Contains(c.table, at) {
				t.Errorf("the map has a %s at %v that the plan's table lacks", c.name, at)
			}
		}
		for _, at := range c.table {
			if !slices.Contains(onMap, at) {
				t.Errorf("the plan's %s table has %v, which the map has no %s at", c.name, at, c.name)
			}
		}
	}
	// The pads: the config names the spiders' wander range (3) and neither creature's hunt range beyond 1; the tribesman names no
	// wander range at all, which is the engine's default of 5.
	blocks := npcBlocks(t, contentRoot(t))
	spider, tribesman := blocks["jungle_spider"], blocks["tribesman"]
	if spider["wanderrange"] != "3" || spider["huntrange"] != "1" || spider["huntmode"] != "cowardly" {
		t.Errorf("the jungle spider is %v", spider)
	}
	if _, named := tribesman["wanderrange"]; named || tribesman["huntrange"] != "1" || tribesman["huntmode"] != "cowardly" {
		t.Errorf("the tribesman is %v", tribesman)
	}
	if JungleSpiderPad != 3+1+1 || JungleTribesmanPad != 5+1+1 {
		t.Errorf("pads %d and %d", JungleSpiderPad, JungleTribesmanPad)
	}
	// The herbs' locs, Trufitus and the jogre cave's twelve.
	for _, h := range JungleHerbs {
		if !csHasRow(csMapRows(t, "LOC", h.At), h.Loc) {
			t.Errorf("no loc %d (%s) at %v in the map", h.Loc, h.Name, h.At)
		}
	}
	if !csHasRow(csMapRows(t, "NPC", TrufitusAnchor), NpcTrufitus) {
		t.Errorf("Trufitus does not stand at %v", TrufitusAnchor)
	}
	if !csHasRow(csMapRows(t, "LOC", tile.Tile{X: 2824, Z: 3118}), LocJunglePothole) || !csHasRow(csMapRows(t, "LOC", tile.Tile{X: 2830, Z: 9522}), LocJungleHandHolds) {
		t.Error("the Rocks or the Hand holds are not where the plan says")
	}
}
