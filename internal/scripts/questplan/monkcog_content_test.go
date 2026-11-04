package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward and hunting rule Monk's Friend and the Clock Tower rest on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run MonkCogContent ./internal/scripts/questplan/

func mcMapHas(t *testing.T, section string, at tile.Tile, id int) bool {
	t.Helper()
	return csHasRow(csMapRows(t, section, at), id)
}

// The object, npc and loc ids the plans carry are the pack's.
func TestMonkCogContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "childs_blanket", ObjChildsBlanket}, {"obj", obj, "jug_empty", ObjJug}, {"obj", obj, "jug_water", ObjJugOfWater},
		{"obj", obj, "logs", ObjLogs}, {"obj", obj, "bucket_empty", ObjBucket}, {"obj", obj, "bucket_water", ObjBucketOfWater},
		{"obj", obj, "whitecog", ObjCogWhite}, {"obj", obj, "blackcog", ObjCogBlack}, {"obj", obj, "bluecog", ObjCogBlue}, {"obj", obj, "redcog", ObjCogRed},
		{"obj", obj, "rat_poison", ObjRatPoison}, {"obj", obj, "shark", ObjShark}, {"obj", obj, "iron_axe", ObjIronAxe},
		{"npc", npc, "brother_omad", NpcBrotherOmad}, {"npc", npc, "brother_cedric", NpcBrotherCedric}, {"npc", npc, "brother_kojo", NpcBrotherKojo},
		{"npc", npc, "ogre", NpcOgre}, {"npc", npc, "khazard_shopkeeper", NpcKhazardShopkeeper},
		{"loc", loc, "wildymirrorladdertop1", LocMonkRingLadder}, {"loc", loc, "ladder_from_cellar", LocCaveLadderUp},
		{"loc", loc, "sink", LocSink}, {"loc", loc, "sink2", LocSink2}, {"loc", loc, "well", LocWell},
		{"loc", loc, "brokeclockpole_red", LocSpindleRed}, {"loc", loc, "brokeclockpole_black", LocSpindleBlack},
		{"loc", loc, "brokeclockpole_white", LocSpindleWhite}, {"loc", loc, "brokeclockpole_blue", LocSpindleBlue},
		{"loc", loc, "ctlevera", LocCogLever}, {"loc", loc, "ctleverb", LocCogLeverDummy}, {"loc", loc, "ctlevera2", LocCogLeverPulled},
		{"loc", loc, "ctratgatea", LocPenGate}, {"loc", loc, "ctratgatec", LocRatGate}, {"loc", loc, "ctfoodtrough", LocFoodTrough},
		{"loc", loc, "secretdoor2", LocCogSecretWall},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	// The look-alike spindles are the four before the working ones.
	for i, name := range []string{"clockpole_red", "clockpole_black", "clockpole_white", "clockpole_blue"} {
		if got := loc[name]; got != 25+i {
			t.Errorf("loc %s is %d, want %d", name, got, 25+i)
		}
	}
}

// fragmentsIn checks that every |-separated line of a fixture page is part of the script that renders it.
func mcFragmentsIn(t *testing.T, script, what string, pages ...string) {
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

// Monk's Friend: the stages, points and rewards, the dialogue the stops steer, the ladder and the party.
func TestMonkCogContentMonksFriendScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^drunkmonk_complete = %d", MonkComplete), "^drunkmonk_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_drunkmonk/configs/quest_drunkmonk.constant")
	for name, v := range map[string]int{"not_started": MonkNotStarted, "spoken_to_omad": MonkSpokeOmad, "retrieved_blanket": MonkRetrievedBlanket,
		"looking_cedric": MonkLookingCedric, "finding_water": MonkFindingWater, "given_water": 50, "fixing_cart": 60, "fixed_cart": MonkFixedCart} {
		if want := fmt.Sprintf("^drunkmonk_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_drunkmonk.constant lacks %q", want)
		}
	}
	if !strings.Contains(consts, "^blanket_ladder_coord = 0_40_50_1_22") {
		t.Error("the ladder is not at 0_40_50_1_22 (2561,3222)")
	}
	if MonkRingLadder != (tile.Tile{X: 40*64 + 1, Z: 50*64 + 22}) || MonkCaveFoot != (tile.Tile{X: 40*64 + 1, Z: 150*64 + 21}) {
		t.Errorf("ring %v foot %v", MonkRingLadder, MonkCaveFoot)
	}
	script := csContent(t, "scripts/quests/quest_drunkmonk/scripts/quest_drunkmonk.rs2")
	for _, want := range []string{
		"[timer,blanket_ladder]", "distance(^blanket_ladder_coord, coord) <= 2", "loc_add(^blanket_ladder_coord, wildymirrorladdertop1, 1, centrepiece_straight, 200);",
		// The party ends in the queue that moves the quest: the reason for CustomMonkParty.
		"queue(drunkmonk_complete, 0, 0);", "[queue,drunkmonk_complete]\n%drunkmonkquest = ^drunkmonk_complete;", "stat_advance(woodcutting, 20000);", "inv_add(inv, lawrune, 8);",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("quest_drunkmonk.rs2 lacks %q", want)
		}
	}
	if LadderFacts[MonksFriendID].XP["woodcutting"] != 2000 {
		t.Errorf("Monk's Friend pays %d Woodcutting XP in its row, stat_advance(woodcutting, 20000) is 2000 at 1x", LadderFacts[MonksFriendID].XP["woodcutting"])
	}
	if !strings.Contains(csContent(t, "scripts/general/scripts/quests.rs2"), "if (%drunkmonkquest >= ^drunkmonk_spoken_to_omad) {\n    settimer(blanket_ladder, 1);") {
		t.Error("the login script does not restart the ladder timer for an account at stage 10 or later")
	}
	if l := csContent(t, "scripts/ladders+stairs/scripts/ladders.rs2"); !strings.Contains(l, "case 0_40_50_1_22 : ~climb_ladder(0_40_150_1_21, false); // Monk's Friend hidden ladder") ||
		!strings.Contains(l, "[oploc1,wildymirrorladdertop1]") {
		t.Error("the hidden ladder does not climb down to 0_40_150_1_21 (2561,9621)")
	}
	if o := csConfigBlock(csContent(t, "scripts/quests/quest_drunkmonk/configs/quest_drunkmonk.obj"), "childs_blanket"); !strings.Contains(o, "respawnrate=50") ||
		!strings.Contains(o, "tradeable=no") {
		t.Errorf("childs_blanket: %q", o)
	}

	omad := csContent(t, "scripts/areas/area_ardougne_east/scripts/brother_omad.rs2")
	for _, want := range []string{
		"%drunkmonkquest = ^drunkmonk_spoken_to_omad;\nsettimer(blanket_ladder, 1);", "inv_total(inv, childs_blanket) >= 1", "inv_del(inv, childs_blanket, 1);",
		"%drunkmonkquest = ^drunkmonk_retrieved_blanket;", "%drunkmonkquest = ^drunkmonk_looking_cedric;", "@drunkmonk_party;",
		"case ^drunkmonk_fixed_cart : @omad_party;",
	} {
		if !strings.Contains(omad, want) {
			t.Errorf("brother_omad.rs2 lacks %q", want)
		}
	}
	// Every preferred option is a menu line of the script, in the order the conversation reaches them.
	for _, stop := range []NpcStop{OmadStartStop, OmadCedricStop} {
		last := -1
		for _, line := range stop.Prefer {
			i := strings.Index(omad, `"`+line+`"`)
			if i < 0 {
				t.Errorf("brother_omad.rs2 has no option %q", line)
			}
			if i < last {
				t.Errorf("option %q comes before the previous preference in the script", line)
			}
			last = i
		}
	}
	cedric := csContent(t, "scripts/areas/area_ardougne_east/scripts/brother_cedric.rs2")
	for _, want := range []string{
		"inv_total(inv, jug_water) >= 1", "inv_del(inv, jug_water, 1);", "%drunkmonkquest = ^drunkmonk_given_water;", "%drunkmonkquest = ^drunkmonk_finding_water;",
		"%drunkmonkquest = ^drunkmonk_fixing_cart;", "inv_total(inv, logs) >= 1", "inv_del(inv, logs, 1);", "%drunkmonkquest = ^drunkmonk_fixed_cart;",
		"case ^drunkmonk_given_water : @cedric_fix_cart;", `"Yes, I'd be happy to!"`,
	} {
		if !strings.Contains(cedric, want) {
			t.Errorf("brother_cedric.rs2 lacks %q", want)
		}
	}
	// The journal pages are the script's: every line of every fixture is in it.
	journal := csContent(t, "scripts/quests/quest_drunkmonk/scripts/drunkmonk_journal.rs2")
	for stage, page := range mfJournalFixtures {
		mcFragmentsIn(t, journal, fmt.Sprintf("stage %d", stage), page)
	}
	// The jug is filled at a sink (category watersource) and the well takes only a bucket.
	water := csContent(t, "scripts/general_use/scripts/water_sources.rs2")
	for _, want := range []string{"[oplocu,_watersource]", "case bucket_empty, jug_empty, vial_empty, bowl_empty,", "[oplocu,_well]\nswitch_obj (last_useitem) {\n    case bucket_empty : @fill_bucket_well;"} {
		if !strings.Contains(water, want) {
			t.Errorf("water_sources.rs2 lacks %q", want)
		}
	}
	locs := csContent(t, "scripts/general_use/configs/water_sources.loc")
	for _, name := range []string{"sink", "sink2"} {
		if b := csConfigBlock(locs, name); !strings.Contains(b, "category=watersource") {
			t.Errorf("[%s]: %q", name, b)
		}
	}
	if b := csConfigBlock(locs, "well"); !strings.Contains(b, "category=well") {
		t.Errorf("[well]: %q", b)
	}
	// Port Khazard's store sells both containers for 2 gp at base.
	inv := csConfigBlock(csContent(t, "scripts/areas/area_ardougne_east/configs/port_khazard/port_khazard.inv"), "khazardshop")
	for _, want := range []string{"stock3=jug_empty,2,100", "stock5=bucket_empty,2,100"} {
		if !strings.Contains(inv, want) {
			t.Errorf("khazardshop lacks %q", want)
		}
	}
}

// The cave's thieves: who hunts whom, how far, how hard they hit.
func TestMonkCogContentTheThievesAndTheMonsters(t *testing.T) {
	npcs := csContent(t, "scripts/_unpack/225/all.npc")
	for name, want := range map[string][]string{
		"thief_blanket":     {"vislevel=14", "huntrange=1", "huntmode=cowardly", "strength=10", "param=strengthbonus,7"},
		"headthief_blanket": {"vislevel=26", "huntrange=1", "huntmode=cowardly", "strength=18", "param=strengthbonus,10"},
		"giantspider1":      {"vislevel=2", "huntrange=1", "huntmode=cowardly"},
		"dungeon_rat":       {"vislevel=12", "huntmode=cowardly"},
		"hobgoblin_unarmed": {"vislevel=28", "huntmode=cowardly"},
	} {
		b := csConfigBlock(npcs, name)
		for _, w := range want {
			if !strings.Contains(b, w) {
				t.Errorf("[%s] lacks %q:\n%s", name, w, b)
			}
		}
	}
	// The goblins of the poison room and the pen's rats have no hunt mode: they never come for the account.
	for _, name := range []string{"goblin", "goblin_armed", "goblin_helmet", "rat"} {
		if b := csConfigBlock(npcs, name); b == "" || strings.Contains(b, "huntmode") {
			t.Errorf("[%s] hunts or is missing: %q", name, b)
		}
	}
	if b := csConfigBlock(csContent(t, "scripts/quests/quest_cog/configs/quest_cog.npc"), "clocktower_rat"); b == "" || strings.Contains(b, "huntmode") || !strings.Contains(b, "vislevel=12") {
		t.Errorf("[clocktower_rat]: %q", b)
	}
	// The ogre: size 2, level 53, hunts within 2 tiles, wanders 3 from its spawn and chases 5, one attack in six ticks.
	ogre := csConfigBlock(csContent(t, "scripts/areas/area_yanille/configs/ogre.npc"), "ogre")
	for _, want := range []string{"size=2", "vislevel=53", "huntrange=2", "huntmode=cowardly", "wanderrange=3", "maxrange=5", "param=attackrate,6", "hitpoints=60", "strength=43", "param=strengthbonus,20"} {
		if !strings.Contains(ogre, want) {
			t.Errorf("[ogre] lacks %q", want)
		}
	}
	// What cowardly means: skip a player of more than twice the npc's level, and a player (or this npc) in combat within 8 ticks.
	hunt := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "cowardly")
	for _, want := range []string{"check_nottoostrong=outside_wilderness", "check_notcombat=%lastcombat", "check_notcombat_self=%npc_lastcombat", "find_keephunting=off", "find_newmode=opplayer2"} {
		if !strings.Contains(hunt, want) {
			t.Errorf("[cowardly] lacks %q", want)
		}
	}
	engine := csContent(t, "../engine/src/engine/entity/Npc.ts")
	for _, want := range []string{"player.combatLevel > type.vislevel * 2", "(player.getVar(hunt.checkNotCombat) as number) + 8 > World.currentTick"} {
		if !strings.Contains(engine, want) {
			t.Errorf("Npc.ts lacks %q", want)
		}
	}
	// The ogres' maximum hit (combat_maxhit, npc_melee_maxhit: ((strength + 9) * (bonus + 64) + 320) / 640) is 7; the guard's food
	// floor is three of them and more.
	if hit := ((43+9)*(20+64) + 320) / 640; hit != 7 {
		t.Errorf("the ogre's maximum hit is %d", hit)
	}
	if CogRedFoodHeal < 3*7 {
		t.Errorf("CogRedFoodHeal %d is under three of the ogre's hardest hits", CogRedFoodHeal)
	}
}

// Clock Tower: the stages, rewards, dialogue, and the scripts of every scripted way (cogs, spindles, levers, gates, the trough).
func TestMonkCogContentClockTowerScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^quest_cog_complete = 8", "^cog_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_cog/configs/quest_cog.constant")
	for _, want := range []string{"^quest_cog_tasked_with_placing_cogs = 1", "^quest_cog_no_remaining_cogs = 5", "^quest_cog_rat_door_bit = 4",
		"^quest_cog_cooled = 0", "^quest_cog_blue = 1", "^quest_cog_black = 2", "^quest_cog_white = 3", "^quest_cog_red = 4"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_cog.constant lacks %q", want)
		}
	}
	main := csContent(t, "scripts/quests/quest_cog/scripts/quest_cog.rs2")
	for _, want := range []string{"inv_add(inv, coins, 500);", "%cogquest = setbit_range_toint(%cogquest, $step, 0, 3);", "return(getbit_range(%cogquest, 0, 3));"} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_cog.rs2 lacks %q", want)
		}
	}
	kojo := csContent(t, "scripts/quests/quest_cog/scripts/npcs/brother_kojo.rs2")
	for _, want := range []string{`"OK old monk, what can I do?"`, "~cog_update_main_quest_step(^quest_cog_tasked_with_placing_cogs);", "queue(cog_complete, 0, 0);"} {
		if !strings.Contains(kojo, want) {
			t.Errorf("brother_kojo.rs2 lacks %q", want)
		}
	}
	if m := csContent(t, "scripts/areas/area_ardougne_east/scripts/brother_kojo.rs2"); !strings.Contains(m, "case ^quest_cog_no_remaining_cogs : @brother_kojo_placed_all_cogs;") {
		t.Error("Kojo does not take the news that all four cogs are placed")
	}
	cogs := csContent(t, "scripts/quests/quest_cog/scripts/objs/cogs.rs2")
	for _, want := range []string{
		"[opobj3,blackcog]", `~mesbox("The cog is red hot from flames.|You cannot pick it up.");`, "[opobju,blackcog]\nif(last_useitem = bucket_water) {",
		"%cog_bits = setbit(%cog_bits, ^quest_cog_cooled);", `"You pour water over the cog.|It quickly cools down enough to take."`,
		"inv_totalcat(inv, cog) > 0", "The cogs are too heavy to carry more than one at a time.", "You must speak to Brother Kojo to begin this quest.",
	} {
		if !strings.Contains(cogs, want) {
			t.Errorf("cogs.rs2 lacks %q", want)
		}
	}
	spindles := csContent(t, "scripts/quests/quest_cog/scripts/locs/quest_cog_spindles.rs2")
	for _, colour := range []string{"black", "red", "white", "blue"} {
		if want := fmt.Sprintf("[oplocu,brokeclockpole_%s]\nif(last_useitem = %scog) {", colour, colour); !strings.Contains(spindles, want) {
			t.Errorf("quest_cog_spindles.rs2 lacks %q", want)
		}
	}
	for _, want := range []string{"You have already placed a cog here.", "The cog fits perfectly.", "[oplocu,_incorrect_clock_spindle]", "The cog doesn't seem to fit."} {
		if !strings.Contains(spindles, want) {
			t.Errorf("quest_cog_spindles.rs2 lacks %q", want)
		}
	}
	trough := csContent(t, "scripts/quests/quest_cog/scripts/locs/quest_cog_food_trough.rs2")
	for _, want := range []string{"[oplocu,ctfoodtrough]", "last_useitem = rat_poison", "inv_del(inv, rat_poison, 1);", "%cogquest = setbit(%cogquest, ^quest_cog_rat_door_bit);"} {
		if !strings.Contains(trough, want) {
			t.Errorf("quest_cog_food_trough.rs2 lacks %q", want)
		}
	}
	gates := csContent(t, "scripts/quests/quest_cog/scripts/locs/quest_cog_gates_and_levers.rs2")
	for _, want := range []string{
		"[oploc1,ctratgatec]\nif (testbit(%cogquest, ^quest_cog_rat_door_bit) = ^true) {", "This door does not seem to be openable.",
		"[oploc1,ctlevera]", "loc_find(0_40_150_35_57, ctratgatea)", "[oploc1,ctlevera2]", "[oploc1,ctleverb]\n@quest_cog_pull_dummy_lever(ctleverb2);",
		"loc_add(movecoord(loc_coord, 1, 0, 0), loc_1541, 3, loc_shape, 500);",
	} {
		if !strings.Contains(gates, want) {
			t.Errorf("quest_cog_gates_and_levers.rs2 lacks %q", want)
		}
	}
	if j := csContent(t, "scripts/areas/area_taverly/dungeon/scripts/jail_doors.rs2"); !strings.Contains(j, "case 0_40_150_35_57 : @clock_tower_first_door_for_rat_cage;") {
		t.Error("the pen gate's Open is not the cage door's label")
	}
	if s := csContent(t, "scripts/quests/quest_ikov/scripts/ikov_dungeon.rs2"); !strings.Contains(s, "[oploc1,secretdoor2] ~open_and_close_door2(inactivesecretdoor2, ~check_axis_locactive(coord), coffin_open);") {
		t.Error("the secret wall is not an open_and_close_door2 door")
	}
	// Both gates teleport the account through: the side is by the axis of the wall (check_axis).
	doors := csContent(t, "scripts/doors/scripts/open_and_close_doors.rs2")
	if !strings.Contains(doors, "[proc,open_and_close_door2](loc $replacement, boolean $entering, synth $sound)") || !strings.Contains(doors, "p_teleport($dest);") {
		t.Error("open_and_close_door2 no longer teleports the player through")
	}
	// The stages of the journal's colours.
	journal := csContent(t, "scripts/quests/quest_cog/scripts/cog_journal.rs2")
	var pages []string
	for mask := 0; mask < 16; mask++ {
		pages = append(pages, ctJournal(mask))
	}
	pages = append(pages, ctJournalNotStarted, ctJournalAllPlaced, ctJournalComplete)
	mcFragmentsIn(t, journal, "the Clock Tower journal", pages...)
	// The journal's colour words are the stage's bits in the content's order of lines (blue, black, white, red).
	order := []string{"Blue Cog", "Black Cog", "White Cog", "Red Cog"}
	last := -1
	for _, name := range order {
		i := strings.Index(journal, "I haven't placed the @dre@"+name)
		if i < last || i < 0 {
			t.Errorf("the journal's line for the %s is out of order", name)
		}
		last = i
	}
}

// The maps: spawns, locs and npcs at the tiles the plans name.
func TestMonkCogContentTiles(t *testing.T) {
	loc := csPackIDs(t, "loc")
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"Omad", "NPC", OmadAnchor, NpcBrotherOmad}, {"Cedric", "NPC", CedricAnchor, NpcBrotherCedric}, {"Kojo", "NPC", KojoAnchor, NpcBrotherKojo},
		{"the blanket", "OBJ", MonkBlanketSpawn, ObjChildsBlanket}, {"the bucket", "OBJ", CogBucketSpawn, ObjBucket}, {"the rat poison", "OBJ", PoisonSpawn, ObjRatPoison},
		{"the cave's way up", "LOC", MonkCaveLadder, LocCaveLadderUp},
		{"lever A", "LOC", CogLeverTile, LocCogLever}, {"lever B", "LOC", tile.Tile{X: 2593, Z: 9661}, LocCogLeverDummy},
		{"the pen gate", "LOC", PenGateTile, LocPenGate}, {"the rat gate", "LOC", RatGateTile, LocRatGate},
		{"the food trough", "LOC", tile.Tile{X: 2586, Z: 9654}, LocFoodTrough}, {"the secret wall", "LOC", tile.Tile{X: 2575, Z: 9631}, LocCogSecretWall},
		{"the well", "LOC", tile.Tile{X: 2612, Z: 3254}, LocWell},
		{"a thief", "NPC", tile.Tile{X: 2564, Z: 9609}, 282}, {"a thief", "NPC", tile.Tile{X: 2566, Z: 9605}, 282}, {"the head thief", "NPC", tile.Tile{X: 2569, Z: 9606}, 283},
		{"an ogre", "NPC", tile.Tile{X: 2585, Z: 9608}, NpcOgre}, {"an ogre", "NPC", tile.Tile{X: 2586, Z: 9614}, NpcOgre}, {"an ogre", "NPC", tile.Tile{X: 2588, Z: 9606}, NpcOgre},
		{"the blue room's dungeon rat", "NPC", tile.Tile{X: 2573, Z: 9630}, 88},
	} {
		if !mcMapHas(t, c.section, c.at, c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	// The blue room's way out and the white room's, and the cellar's ladder down, are in the map (they are graph edges).
	for _, at := range []tile.Tile{{X: 2572, Z: 9631}, {X: 2575, Z: 9655}} {
		if !mcMapHas(t, "LOC", at, 1755) {
			t.Errorf("no ladder up at %v", at)
		}
	}
	// The cogs lie where the colour table says, and nowhere else (one spawn each).
	for _, c := range CogColours {
		if !mcMapHas(t, "OBJ", c.Spawn, c.Obj) {
			t.Errorf("the %s cog does not lie at %v", c.Name, c.Spawn)
		}
	}
	// Every floor has the four spindles of a colour each, one of them the working one: the spindle table names it by id.
	floorSpindles := map[string]tile.Tile{"red": {X: 2568, Z: 3243}, "blue": {X: 2569, Z: 3240, Level: 1}, "white": {X: 2567, Z: 3241, Level: 2}, "black": {X: 2570, Z: 9642}}
	for _, c := range CogColours {
		at := floorSpindles[c.Name]
		if !mcMapHas(t, "LOC", at, c.Spindle) {
			t.Errorf("the %s spindle (loc %d) is not at %v", c.Name, c.Spindle, at)
		}
		if tile.Chebyshev(at, c.Stand) != 1 || at.Level != c.Stand.Level {
			t.Errorf("the %s cog is fitted from %v, which is not beside the spindle at %v", c.Name, c.Stand, at)
		}
	}
	// The look-alikes: on the ground floor the working red one stands among a white, a blue and a black look-alike.
	for _, c := range []struct {
		at tile.Tile
		id int
	}{
		{tile.Tile{X: 2567, Z: 3241}, loc["clockpole_white"]}, {tile.Tile{X: 2569, Z: 3240}, loc["clockpole_blue"]}, {tile.Tile{X: 2570, Z: 3242}, loc["clockpole_black"]},
	} {
		if !mcMapHas(t, "LOC", c.at, c.id) {
			t.Errorf("no look-alike spindle %d at %v", c.id, c.at)
		}
	}
	// The pen holds the rats the poison kills: 11 of them, all inside the pen's tiles.
	rats := 0
	for x := 2579; x <= 2595; x++ {
		for z := 9652; z <= 9660; z++ {
			p := tile.Tile{X: x, Z: z}
			if mcMapHas(t, "NPC", p, 224) {
				if !InCogPen(p) {
					t.Errorf("a clock tower rat spawns at %v, outside InCogPen", p)
				}
				rats++
			}
		}
	}
	if rats != 11 {
		t.Errorf("%d clock tower rats spawn in the pen, 11 expected", rats)
	}
	// The giant spiders by the black cog are the hunters of combat 4 and under only.
	spiders := 0
	for x := 2600; x <= 2612; x++ {
		for z := 9634; z <= 9650; z++ {
			if mcMapHas(t, "NPC", tile.Tile{X: x, Z: z}, 59) {
				spiders++
			}
		}
	}
	if spiders < 10 {
		t.Errorf("%d giant spiders near the black cog", spiders)
	}
	// The sinks of the jug's fill are where the plan puts them.
	for _, s := range MonkSinks {
		if !mcMapHas(t, "LOC", s, LocSink) && !mcMapHas(t, "LOC", s, LocSink2) {
			t.Errorf("no sink at %v", s)
		}
	}
	// The owner of Port Khazard's shop stands where the shop anchor says.
	if !mcMapHas(t, "NPC", KhazardGeneralStore.Anchor, NpcKhazardShopkeeper) {
		t.Errorf("Port Khazard's keeper does not stand at %v", KhazardGeneralStore.Anchor)
	}
	// And the plans' own tables: the ogres' staging tile is 6 or more from each spawn (the pure test restates it), and the cave and
	// the pen rectangles agree with the map's rats and thieves.
	for _, thief := range []tile.Tile{{X: 2564, Z: 9609}, {X: 2566, Z: 9605}, {X: 2569, Z: 9606}, MonkBlanketSpawn} {
		if !InMonkCave(thief) {
			t.Errorf("%v is outside InMonkCave", thief)
		}
	}
}
