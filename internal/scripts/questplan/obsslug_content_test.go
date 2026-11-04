package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward and hunting rule the Observatory Quest and the Sea Slug Quest rest on, read from a Content 289
// checkout (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run ObsSlugContent ./internal/scripts/questplan/
//
// (The loc index the fleet runs on is baked from the production maps, which hold locs this checkout's maps do not, such as the cavern's
// ladders and the sack; those are pinned in the questmods real-data tests.)

const (
	obsDir  = "scripts/quests/quest_itgronigen/"
	slugDir = "scripts/quests/quest_seaslug/"
	platDir = "scripts/areas/area_fishing_platform/"
	ardDir  = "scripts/areas/area_ardougne_east/"
)

// The pack ids the plans carry are the pack's.
func TestObsSlugContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "observatory_assistant", NpcObsAssistant}, {"npc", npc, "observatory_professor", NpcObsProfessor},
		{"npc", npc, "goblin_guard", NpcGoblinGuard}, {"npc", npc, "observatory_professor2", NpcObsProfessorDome},
		{"obj", obj, "keep_key", ObjObsKeepKey}, {"obj", obj, "lens_mould", ObjLensMould}, {"obj", obj, "lens", ObjLens},
		{"obj", obj, "woodplank", ObjPlank}, {"obj", obj, "bronze_bar", ObjBronzeBar}, {"obj", obj, "molten_glass", ObjMoltenGlass},
		{"obj", obj, "bucket_sand", ObjBucketOfSand}, {"obj", obj, "soda_ash", ObjSodaAsh}, {"obj", obj, "seaweed", ObjSeaweed},
		{"obj", obj, "copper_ore", ObjCopperOre}, {"obj", obj, "tin_ore", ObjTinOre}, {"obj", obj, "shark", ObjShark},
		{"loc", loc, "obs_dungeonladderdown", LocObsReceptionLadder}, {"loc", loc, "loc_2188", LocObsCavernExit}, {"loc", loc, "loc_2189", LocObsDomeLadderUp},
		{"loc", loc, "domeladderdown", LocObsDomeLadderDown}, {"loc", loc, "loc_2197", LocObsKeyChestShut}, {"loc", loc, "loc_2198", LocObsKeyChestOpen},
		{"loc", loc, "keepgate_closed", LocObsGateRight}, {"loc", loc, "keepgate_closed_left", LocObsGateLeft}, {"loc", loc, "telescope", LocObsTelescope},
		{"loc", loc, "loc_2212", LocObsSack},
		{"npc", npc, "bailey", NpcBailey}, {"npc", npc, "caroline", NpcCaroline}, {"npc", npc, "kennith", NpcKennith},
		{"npc", npc, "holgartsunkboat", NpcHolgartIsland}, {"npc", npc, "holgartplatform", NpcHolgartPlat}, {"npc", npc, "holgartland", NpcHolgartLand},
		{"npc", npc, "kent", NpcKent}, {"npc", npc, "seaslug", NpcPlatformSlug},
		{"obj", obj, "torch_lit", ObjTorchLit}, {"obj", obj, "torch_unlit", ObjTorchUnlit}, {"obj", obj, "damp_sticks", ObjDampSticks},
		{"obj", obj, "dry_sticks", ObjDrySticks}, {"obj", obj, "broken_glass", ObjBrokenGlass}, {"obj", obj, "swamppaste", ObjSwampPaste},
		{"obj", obj, "seasluginv", ObjSeaSlug},
		{"loc", loc, "slugladder", LocSlugLadderUp}, {"loc", loc, "loosepanel", LocSlugPanel}, {"loc", loc, "kennithwall", LocSlugCrates},
		{"loc", loc, "fishingcrane", LocSlugCrane},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	for i, name := range []string{"fishplatform1", "fishplatform2", "fishplatform3", "fishplatform4"} {
		if npc[name] != 702+i {
			t.Errorf("npc %s is %d, want %d", name, npc[name], 702+i)
		}
	}
}

// Observatory: the stages, the points, the rewards, the dialogue the stops steer, the refusals the plan reads.
func TestObsSlugContentObservatoryScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^itgronigen_complete = %d", ObsComplete), "^itgronigen_questpoints = 2"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, obsDir+"configs/quest_itgronigen.constant")
	for name, v := range map[string]int{"not_started": ObsNotStarted, "started": ObsStarted, "given_planks": ObsGivenPlanks, "given_bronze": ObsGivenBronze,
		"given_glass": ObsGivenGlass, "given_mould": ObsGivenMould, "sent_telescope": ObsSentTelescope, "claimed_wine": 8} {
		if want := fmt.Sprintf("^itgronigen_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_itgronigen.constant lacks %q", want)
		}
	}
	prof := csContent(t, obsDir+"scripts/observatory_professor.rs2")
	for _, want := range []string{
		// Every hand-over takes what its stage names, and only at that stage.
		"inv_total(inv, woodplank) < 3", "inv_del(inv, woodplank, 3);", "inv_total(inv, bronze_bar) = 0", "inv_total(inv, molten_glass) = 0",
		"inv_total(inv, lens_mould) = 0", "inv_total(inv, lens) = 0",
		// The glass is taken and handed back, into a pack with room only.
		"if(inv_freespace(inv) = 0)", "inv_add(inv, molten_glass, 1);", "The professor gives you back the molten glass.",
		"%itgronigen = ^itgronigen_sent_telescope;", "inv_del(inv, lens_mould, 1);",
		// The start talk's two menus.
		"~p_choice4(\"I am lost!!!\", 1, \"I'd like to have a look through that telescope.\", 2,", "\"Sounds interesting, what can I do for you?\", 1",
		"\"Oh, sorry, I don't have time for that.\", 2",
	} {
		if !strings.Contains(prof, want) {
			t.Errorf("observatory_professor.rs2 lacks %q", want)
		}
	}
	for _, stop := range []NpcStop{ObsProfessorStop} {
		last := -1
		for _, line := range stop.Prefer {
			i := strings.Index(prof, `"`+line+`"`)
			if i < 0 {
				t.Errorf("observatory_professor.rs2 has no option %q", line)
			}
			if i < last {
				t.Errorf("option %q comes before the previous preference in the script", line)
			}
			last = i
		}
	}
	quests := csContent(t, obsDir+"scripts/quest_itgronigen.rs2")
	for _, want := range []string{
		// The ladder: the assistant's warning, then the teleport to the landing.
		"[oploc1,obs_dungeonladderdown]", "if(%itgronigen < ^itgronigen_complete) {", "p_telejump(0_37_147_52_51);",
		"~climb_ladder(0_38_49_12_56, true);", "~climb_ladder(0_38_49_8_29, true);", "~climb_ladder(0_37_147_55_30, false);",
		// The chests: only loc 2197/2198 holds the key; the shut ones spawn a poisonous spider and poison the player.
		"[oploc1,loc_2197]", "[oploc2,loc_2198]", "~obj_gettotal(keep_key) > 0", "You already have a keep key.", "npc_add(map_findsquare(coord, 0, 1, ^map_findsquare_lineofwalk), dungeonspider, 500);",
		"queue(poison_player, 0, 15);", "loc_change(loc_2197, 300);",
		// The gate.
		"if(last_useitem = keep_key) {", "%itkeepgatelock = 0 & coordz(coord) > coordz(loc_coord)", "mes(\"The gate is locked.\");",
		"if(npc_find(coord, goblin_guard, 8, 0) = true) {", "p_teleport(movecoord(coord, 0, 0, -1));", "p_teleport(movecoord(coord, 0, 0, 1));",
		"The key is useless now. You discard it.",
		// The sack and the lens.
		"[oploc1,loc_2212]", "~obj_gettotal(lens_mould) > 0", "You already have this lens mould.", "Underneath the sack you find a peculiar mould.",
		"[opheldu,lens_mould]", "if (last_useitem = molten_glass) @craft_telescope_disc;", "inv_del(inv, molten_glass, 1);\ninv_add(inv, lens, 1);",
		// The telescope: the dome's professor within 7, the chart, the queued conversation, the reward.
		"[oploc1,telescope]", "case ^itgronigen_sent_telescope :", "npc_find(coord, observatory_professor2, 7, 0) = true", "if_openmain(telescope);",
		"queue(constellation_dialogue, 0, $constellation);", "if (npc_find(coord, observatory_professor2, 7, 0) = false) {",
		"~p_choice2(\"I can see a constellation.\", 1, \"What am I looking at?\", 2)", "stat_advance(crafting, 22500);", "inv_add(inv, uncut_sapphire, 1);",
		"%itgronigen = ^itgronigen_complete;", "%qp = calc(%qp + ^itgronigen_questpoints);",
	} {
		if !strings.Contains(quests, want) {
			t.Errorf("quest_itgronigen.rs2 lacks %q", want)
		}
	}
	// The three crossings land where the plan says: the cavern's landing, the reception and the dome.
	for _, c := range []struct {
		what string
		got  tile.Tile
		x, z int
	}{{"the landing 0_37_147_52_51", ObsCavernLanding, 37*64 + 52, 147*64 + 51}, {"the reception 0_38_49_12_56", ObsReceptionLadderStand, 38*64 + 12, 49*64 + 56},
		{"the dome 0_38_49_8_29", ObsDomeArrive, 38*64 + 8, 49*64 + 29}} {
		if c.got.X != c.x || c.got.Z != c.z {
			t.Errorf("%s is %d,%d, the plan has %v", c.what, c.x, c.z, c.got)
		}
	}
	// The assistant's warning is two chat lines and no menu.
	if a := csContent(t, obsDir+"scripts/observatory_assistant.rs2"); !strings.Contains(a, "case ^itgronigen_given_glass :") {
		t.Error("the assistant has no stage 4 line")
	}
	// The lens is made with no Crafting level: craft_telescope_disc checks none, which is why the quest asks for no level.
	if i := strings.Index(quests, "[label,craft_telescope_disc]"); i < 0 || strings.Contains(quests[i:i+400], "stat(crafting)") {
		t.Error("craft_telescope_disc checks a Crafting level")
	}
	fx := csContent(t, obsDir+"scripts/itgronigen_journal.rs2")
	for stage, page := range obsJournalFixtures {
		mcFragmentsIn(t, fx, fmt.Sprintf("stage %d", stage), page)
	}
	if LadderFacts[ObservatoryID].XP["crafting"] != 2250 {
		t.Errorf("the row pays %d Crafting XP, stat_advance(crafting, 22500) is 2250 at 1x", LadderFacts[ObservatoryID].XP["crafting"])
	}
}

// The goblin guard: his config is the table's, he has no hunt mode (passive until the gate moves), and the cavern holds only goblins and
// rats beside him.
func TestObsSlugContentTheGuardAndTheCavern(t *testing.T) {
	npcs := csContent(t, obsDir+"configs/quest_itgronigen.npc")
	g := csConfigBlock(npcs, "goblin_guard")
	for _, want := range []string{"attack=32", "defence=37", "strength=37", "hitpoints=43", "param=attackbonus,8", "param=strengthbonus,5", "param=stabdefence,4",
		"op2=Attack", "wanderrange=0", "respawnrate=500"} {
		if !strings.Contains(g, want) {
			t.Errorf("[goblin_guard] lacks %q", want)
		}
	}
	for _, not := range []string{"huntmode", "huntrange", "attackrate", "magic="} {
		if strings.Contains(g, not) {
			t.Errorf("[goblin_guard] has %q: the plan prices him as passive with the default attack rate", not)
		}
	}
	if !strings.Contains(csContent(t, obsDir+"scripts/goblin_guard.rs2"), "~npc_retaliate(0);") {
		t.Error("talking to the guard no longer sets him on the player")
	}
	// The cavern map (m37_147): the guard's spawn at the gate, and goblins (100, 101) and rats (47) as its only other residents.
	src := csContent(t, "maps/m37_147.jm2")
	in, found := false, map[string]int{}
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		if _, tail, ok := strings.Cut(line, ":"); in && ok {
			found[strings.Fields(tail)[0]]++
		}
	}
	for id := range found {
		if id != "100" && id != "101" && id != "47" && id != "489" {
			t.Errorf("npc %s lives in the cavern: the plan assumes goblins, rats and the guard", id)
		}
	}
	if found["489"] != 1 {
		t.Errorf("%d goblin guards in the cavern", found["489"])
	}
	if !csHasRow(csMapRows(t, "NPC", ObsGuardSpawn), NpcGoblinGuard) {
		t.Errorf("no goblin guard at %v", ObsGuardSpawn)
	}
	// Goblins: the level 2 one has no stats and neither has a hunt mode.
	all := csContent(t, "scripts/_unpack/225/all.npc")
	for _, name := range []string{"goblin", "goblin_armed", "rat"} {
		b := csConfigBlock(all, name)
		if b == "" || strings.Contains(b, "huntmode") {
			t.Errorf("[%s] is missing or hunts (block %.80q)", name, b)
		}
	}
	// The spider is only ever spawned by a chest search; no map places one.
	if csPlaces := strings.Contains(src, ": 1009"); csPlaces {
		t.Error("the cavern's map places a poison spider")
	}
}

// The maps place what the plans stand on: the professors, the planks, the sand, the chests, the gate and the telescope.
func TestObsSlugContentObservatoryMaps(t *testing.T) {
	for _, c := range []struct {
		section string
		at      tile.Tile
		id      int
	}{
		{"NPC", ObsProfessorAnchor, NpcObsProfessor}, {"NPC", ObsDomeProfessor, NpcObsProfessorDome}, {"NPC", tile.Tile{X: 2442, Z: 3189}, NpcObsAssistant},
		{"OBJ", ObsSandSpawn, ObjBucketOfSand}, {"LOC", tile.Tile{X: 2428, Z: 9419}, LocObsKeyChestShut},
		{"LOC", tile.Tile{X: 2390, Z: 9457}, LocObsGateLeft}, {"LOC", tile.Tile{X: 2391, Z: 9457}, LocObsGateRight}, {"LOC", ObsTelescopeAt, LocObsTelescope},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("no %s %d at %v in the maps", c.section, c.id, c.at)
		}
	}
	for _, p := range ObsPlankSpawns {
		if !csHasRow(csMapRows(t, "OBJ", p), ObjPlank) {
			t.Errorf("no plank at %v in the maps", p)
		}
	}
	// The other chests of the cavern are the spider's: ids 2191..2193 shut, none of them the key chest.
	for _, at := range []tile.Tile{{X: 2390, Z: 9424}, {X: 2397, Z: 9469}, {X: 2399, Z: 9438}, {X: 2415, Z: 9425}, {X: 2420, Z: 9444}, {X: 2424, Z: 9430}} {
		if !csHasRow(csMapRows(t, "LOC", at), 2191) {
			t.Errorf("no spider chest (loc 2191) at %v", at)
		}
	}
}

// Sea Slug: the stages, the points, the rewards and the dialogue the stops steer.
func TestObsSlugContentSeaSlugScripts(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^seaslug_complete = %d", SlugComplete), "^seaslug_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, slugDir+"configs/quest_seaslug.constant")
	for name, v := range map[string]int{"not_started": SlugNotStarted, "started": SlugStarted, "spoken_holgart": SlugSpokenHolgart, "boat_repaired": SlugBoatRepaired,
		"spoken_kennith": SlugSpokenKennith, "sailed_kent": SlugSailedKent, "spoken_kent": SlugSpokenKent, "lit_torch": SlugLitTorch,
		"kennith_need_escape": SlugKennithNeedEscape, "panel_opened": SlugPanelOpened, "need_kennith_path": SlugNeedKennithPath, "saved_kennith": SlugSavedKennith} {
		if want := fmt.Sprintf("^seaslug_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("quest_seaslug.constant lacks %q", want)
		}
	}
	// The three boat coordinates are the plan's tiles.
	for _, c := range []struct {
		name string
		got  tile.Tile
		mx   int
		mz   int
		lx   int
		lz   int
	}{{"island", SlugIslandArrive, 43, 51, 48, 56}, {"shore", SlugShore, 42, 51, 34, 41}, {"platform", SlugPlatformArrive, 43, 51, 30, 9}} {
		if want := fmt.Sprintf("^seaslug_%s_coord = 0_%d_%d_%d_%d", c.name, c.mx, c.mz, c.lx, c.lz); !strings.Contains(consts, want) {
			t.Errorf("quest_seaslug.constant lacks %q", want)
		}
		if c.got.X != c.mx*64+c.lx || c.got.Z != c.mz*64+c.lz {
			t.Errorf("the %s coordinate is %d,%d, the plan has %v", c.name, c.mx*64+c.lx, c.mz*64+c.lz, c.got)
		}
	}
	main := csContent(t, slugDir+"scripts/quest_seaslug.rs2")
	for _, want := range []string{
		// The torch: the sticks, the glass held to them, Firemaking 30, the three answers of a rub, the stage it moves.
		"[opheldu,damp_sticks]", "if(last_useitem = broken_glass) {", "inv_del(inv, damp_sticks, 1);", "inv_add(inv, dry_sticks, 1);",
		"[opheld1,dry_sticks]", "if(stat(firemaking) < 30) {", "You need a Firemaking level of 30 or above.", "stat_random(firemaking, 64, 512) = false",
		"the sticks smoke momentarily then die out", "You rub together the dry sticks and the sticks catch alight.", "if(inv_total(inv, torch_unlit) > 0) {",
		"if(%seaslugquest = ^seaslug_spoken_kent) {\n    %seaslugquest = ^seaslug_lit_torch;",
		// The ladder: from stage 6 on it wants a lit torch and smacks the account for 4 without one.
		"[oploc1,slugladder]", "if(%seaslugquest >= ^seaslug_spoken_kent & %seaslugquest < ^seaslug_complete) {", "if(inv_total(inv, torch_lit) = 0) {",
		"smack you on the head with a fishing rod!", "~damage_self(4);",
		// The panel, the crane, the crates.
		"[oploc1,loosepanel]", "if(%seaslugquest = ^seaslug_kennith_need_escape) {", "The wood is rotten and crumbles away...", "%seaslugquest = ^seaslug_panel_opened;",
		"[oploc1,fishingcrane]", "if(coordz(coord) < coordz(movecoord(loc_coord, 0, 0, 3))) {", "I need to get closer to use that.", "and lower Kennith to the row boat waiting below.",
		"%seaslugquest = ^seaslug_saved_kennith;", "[oploc1,kennithwall]", "if (npc_find(movecoord(coord, 0, 0, 2), kennith, 5, 0) = true) {",
		// A sea slug is never taken: its Take bites.
		"[opnpc1,seaslug] @pickup_seaslug;", "It sinks its teeth deep into your hand.", "~damage_self(3);",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_seaslug.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(main, "inv_add(inv, pot_empty, 1);\n    inv_add(inv, rawswamppaste, 1);") && !strings.Contains(main, "inv_add(inv, pot_empty, 1);\ninv_add(inv, rawswamppaste, 1);") {
		t.Error("the swamp tar and flour no longer make a raw paste")
	}
	car := csContent(t, ardDir+"scripts/caroline.rs2")
	for _, want := range []string{"\"I suppose so, how do I get there?\", 1", "%seaslugquest = ^seaslug_started;", "case ^seaslug_saved_kennith : @caroline_savedkennith;",
		"%seaslugquest = ^seaslug_complete;", "%qp = add(%qp, ^seaslug_questpoints);", "stat_advance(fishing, 71750);", "inv_add(inv, bigoysterpearls, 1);"} {
		if !strings.Contains(car, want) {
			t.Errorf("caroline.rs2 lacks %q", want)
		}
	}
	hol := csContent(t, ardDir+"scripts/holgart_ardougne.rs2")
	for _, want := range []string{"case ^seaslug_started : @holgart_wantboat;", "%seaslugquest = ^seaslug_spoken_holgart;", "if(inv_total(inv, swamppaste) < 1) {",
		"inv_del(inv, swamppaste, 1);", "%seaslugquest = ^seaslug_boat_repaired;", "switch_int(~p_choice2(\"I'll come back later.\", 1, \"Okay, lets do it.\", 2))",
		"\"Will you take me there?\", 1", "case ^seaslug_sailed_kent : @holgart_back_kent;", "~board_fishing_platform_to_island;",
		"inv_del(inv, torch_lit, $count);", "inv_add(inv, torch_unlit, $count);", "p_telejump(^seaslug_platform_coord);"} {
		if !strings.Contains(hol, want) {
			t.Errorf("holgart_ardougne.rs2 lacks %q", want)
		}
	}
	for _, stop := range []NpcStop{SlugCarolineStop, SlugHolgartLandStop} {
		src := car
		if stop.ID == NpcHolgartLand {
			src = hol
		}
		for _, line := range stop.Prefer {
			if !strings.Contains(src, `"`+line+`"`) && line != "Okay, let's do it." {
				t.Errorf("the %s script has no option %q", stop.Npc, line)
			}
		}
	}
	if !strings.Contains(hol, "\"Okay, let's do it.\"") {
		t.Error("the post-quest menu lost its apostrophe spelling")
	}
	plat := csContent(t, platDir+"scripts/holgart_platform.rs2")
	for _, want := range []string{"case ^seaslug_spoken_kennith : @holgart_something_strange;", "case ^seaslug_saved_kennith : @holgart_kid_shore;",
		"%seaslugquest = ^seaslug_sailed_kent;", "~board_fishing_platform_to_island;", "~board_fishing_platform_to_ardougne;",
		"switch_int(~p_choice2(\"No, I'm going to stay a while.\", 1, \"Okay, let's go back.\", 2))"} {
		if !strings.Contains(plat, want) {
			t.Errorf("holgart_platform.rs2 lacks %q", want)
		}
	}
	for _, line := range SlugHolgartPlatformStop.Prefer {
		if !strings.Contains(plat, `"`+line+`"`) {
			t.Errorf("the platform Holgart has no option %q", line)
		}
	}
	isl := csContent(t, platDir+"scripts/holgart_island.rs2")
	if !strings.Contains(isl, "if(%seaslugquest ! ^seaslug_sailed_kent) {") || !strings.Contains(isl, "Someway off mainland still.") {
		t.Error("the island's Holgart does not refuse to sail at stage 5")
	}
	bail := csContent(t, platDir+"scripts/bailey.rs2")
	for _, want := range []string{"case ^seaslug_spoken_kent :", "if(inv_total(inv, torch_unlit) = 0) {", "inv_add(inv, torch_unlit, 1);",
		"case ^seaslug_lit_torch, ^seaslug_kennith_need_escape, ^seaslug_panel_opened :", "That was silly, fortunately I have another."} {
		if !strings.Contains(bail, want) {
			t.Errorf("bailey.rs2 lacks %q", want)
		}
	}
	// Bailey has no lost-torch line from stage 10 on.
	if i := strings.Index(bail, "case ^seaslug_saved_kennith :"); i < 0 || strings.Contains(bail[i:], "inv_add(inv, torch_unlit, 1)") {
		t.Error("Bailey hands out a torch past the panel")
	}
	ken := csContent(t, platDir+"scripts/kennith.rs2")
	for _, want := range []string{"case ^seaslug_boat_repaired :", "%seaslugquest = ^seaslug_spoken_kennith;", "case ^seaslug_lit_torch :", "%seaslugquest = ^seaslug_kennith_need_escape;",
		"case ^seaslug_panel_opened :", "%seaslugquest = ^seaslug_need_kennith_path;", "He doesn't seem interested in talking."} {
		if !strings.Contains(ken, want) {
			t.Errorf("kennith.rs2 lacks %q", want)
		}
	}
	kent := csContent(t, platDir+"scripts/kent.rs2")
	for _, want := range []string{"if(%seaslugquest ! ^seaslug_sailed_kent) {", "%seaslugquest = ^seaslug_spoken_kent;", "obj_add(coord, seasluginv, 1, ^lootdrop_duration);"} {
		if !strings.Contains(kent, want) {
			t.Errorf("kent.rs2 lacks %q", want)
		}
	}
	fx := csContent(t, slugDir+"scripts/seaslug_journal.rs2")
	for stage, page := range slugJournalFixtures {
		mcFragmentsIn(t, fx, fmt.Sprintf("stage %d", stage), page)
	}
	if LadderFacts[SeaSlugID].XP["fishing"] != 7175 {
		t.Errorf("the row pays %d Fishing XP, stat_advance(fishing, 71750) is 7175 at 1x", LadderFacts[SeaSlugID].XP["fishing"])
	}
}

// Nothing on the platform attacks: the fishermen and the sea slugs have no combat stats, no hunt mode and no Attack option, so the
// quest prices no fight and its only hurt is the two scripted ones.
func TestObsSlugContentNothingOnThePlatformAttacks(t *testing.T) {
	npcs := csContent(t, platDir+"configs/fishing_platform.npc")
	for _, name := range []string{"bailey", "kennith", "holgartsunkboat", "holgartplatform", "kent", "fishplatform1", "fishplatform2", "fishplatform3", "fishplatform4", "seaslug"} {
		b := csConfigBlock(npcs, name)
		if b == "" {
			t.Errorf("no [%s] block in fishing_platform.npc", name)
			continue
		}
		for _, not := range []string{"attack=", "strength=", "defence=", "hitpoints=", "huntmode", "huntrange", "op2=Attack", "param=attackbonus"} {
			if strings.Contains(b, not) {
				t.Errorf("[%s] has %q: the quest's no-fight pricing no longer holds", name, not)
			}
		}
	}
	if !strings.Contains(csConfigBlock(npcs, "seaslug"), "op1=Take") {
		t.Error("the sea slug's only option is no longer Take")
	}
	// No hunt rule of the content mentions any of them.
	root := os.Getenv("RSB0T_CONTENT")
	_ = filepath.Walk(filepath.Join(root, "scripts"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".hunt") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, name := range []string{"seaslug", "fishplatform", "fisherman_platform", "goblin_guard", "kennith", "holgart"} {
			if strings.Contains(string(b), name) {
				t.Errorf("%s mentions %q: it hunts", p, name)
			}
		}
		return nil
	})
	// The fishermen are talk-only.
	if f := csContent(t, platDir+"scripts/fisherman.rs2"); strings.Contains(f, "npc_setmode") || strings.Contains(f, "retaliate") {
		t.Error("the fishermen's script sets an attack")
	}
}

// The maps place what the plans stand on: the people of Witchaven, the platform and the island, and the platform's ground spawns.
func TestObsSlugContentSeaSlugMaps(t *testing.T) {
	for _, c := range []struct {
		section string
		at      tile.Tile
		id      int
	}{
		{"NPC", SlugCarolineAnchor, NpcCaroline}, {"NPC", SlugHolgartLandAnchor, NpcHolgartLand}, {"NPC", SlugHolgartPlatform, NpcHolgartPlat},
		{"NPC", SlugBaileyAnchor, NpcBailey}, {"NPC", SlugKennithAnchor, NpcKennith}, {"NPC", SlugKentAnchor, NpcKent},
		{"NPC", tile.Tile{X: 2799, Z: 3320}, NpcHolgartIsland},
		{"OBJ", SlugDampSticksSpawn, ObjDampSticks}, {"OBJ", SlugBrokenGlassNorth, ObjBrokenGlass}, {"OBJ", SlugBrokenGlassSouth, ObjBrokenGlass},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("no %s %d at %v in the maps", c.section, c.id, c.at)
		}
	}
	// The glass and the sticks are untradeable ground spawns of one each; the torch is Bailey's alone (no map places one).
	objs := csContent(t, "scripts/_unpack/225/all.obj")
	for _, name := range []string{"damp_sticks", "dry_sticks", "broken_glass"} {
		if b := csConfigBlock(objs, name); !strings.Contains(b, "tradeable=no") {
			t.Errorf("[%s] is tradeable: %.80q", name, b)
		}
	}
	// Port Khazard's general store sells the swamp paste (500 of them, restocked), at the obj's cost of 30.
	inv := csContent(t, ardDir+"configs/port_khazard/port_khazard.inv")
	if !strings.Contains(inv, "stock12=swamppaste,500,20") {
		t.Error("Port Khazard's store no longer stocks 500 swamp paste")
	}
	cook := csContent(t, "scripts/skill_cooking/configs/cooking_generic/cooking.obj")
	if b := csConfigBlock(cook, "swamppaste"); !strings.Contains(b, "cost=30") || !strings.Contains(b, "stackable=yes") {
		t.Errorf("[swamppaste] is %.100q", b)
	}
	if SlugPasteGP != 30 {
		t.Errorf("SlugPasteGP is %d", SlugPasteGP)
	}
	// The ladder up is a one-tile loc whose own tile is no stand, and the platform's rectangle holds every platform tile and no mainland one.
	for _, at := range []tile.Tile{SlugPlatformArrive, SlugHolgartPlatform, SlugBaileyAnchor, SlugDampSticksSpawn, SlugBrokenGlassNorth, SlugBrokenGlassSouth, SlugCratesStand, SlugPanelStand, SlugCraneStand} {
		if !OnSlugPlatform(at) {
			t.Errorf("%v is not in the platform's rectangle", at)
		}
	}
}
