package questplan

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Shades of Mort'ton's plan against the content checkout (RSB0T_CONTENT): the ids it carries, the stages and the varps, the journal's markers, the lines it chooses from the
// scripts' menus, the levels it asks for, the ops of the villagers and the spawns its tiles and its hazards are made from. Each test skips without the checkout.

// moScripts reads the quest's scripts, line endings folded: the villagers', the journal's and the serum book's, and the temple's, the pyre's and the shades'.
func moScripts(t *testing.T, root string) (razmire, ulsquire, journal, temple, pyre string) {
	t.Helper()
	q := filepath.Join("scripts", "quests", "quest_mortton", "scripts")
	g := filepath.Join("scripts", "minigames", "game_mortton", "scripts")
	return chScript(t, root, q, "razmire_keelgan.rs2"), chScript(t, root, q, "ulsquire_shauncy.rs2"), chScript(t, root, q, "mortton_journal.rs2"),
		chScript(t, root, g, "flamtaer_temple.rs2"), chScript(t, root, g, "mortton_pyre.rs2")
}

// The ids the plan carries are the content packs' names for them.
func TestMorttonIDsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	objs := packNames(t, filepath.Join(root, "pack", "obj.pack"))
	npcs := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	locs := packNames(t, filepath.Join(root, "pack", "loc.pack"))
	for id, want := range map[int]string{
		ObjMtDiary: "serum_book", ObjMtUnidTarromin: "unidentified_tarromin", ObjMtTarromin: "tarromin", ObjMtUnfTarromin: "tarrominvial", ObjMtVialWater: "vial_water",
		ObjMtVial: "vial_empty", ObjMtSerum4: "mort_serum4", ObjMtSerum3: "mort_serum3", ObjMtSerum2: "mort_serum2", ObjMtSerum1: "mort_serum1", ObjMtPerm4: "mort_serum_perm4",
		ObjMtPerm3: "mort_serum_perm3", ObjMtPerm2: "mort_serum_perm2", ObjMtPerm1: "mort_serum_perm1", ObjMtRemains: "shade_bones1", ObjMtBrick: "limestonebrick",
		ObjMtPlank: "woodplank", ObjMtPaste: "swamppaste", ObjMtOil4: "oliveoil4", ObjMtOil3: "oliveoil3", ObjMtOil2: "oliveoil2", ObjMtOil1: "oliveoil1",
		ObjMtSacred4: "sacred_oil4", ObjMtSacred3: "sacred_oil3", ObjMtSacred2: "sacred_oil2", ObjMtSacred1: "sacred_oil1", ObjMtPyreLogs: "logs_pyre",
		ObjMtRoguesPurse: "unidentified_rogues_purse", ObjMtRottenFood: "rotten_food", ObjMtLogs: "logs", ObjTinderbox: "tinderbox", ObjHammer: "hammer", ObjAshes: "ashes",
		ObjShark: "shark", CoinsID: "coins",
	} {
		if objs[id] != want {
			t.Errorf("obj %d is %q in the content, the plan means %q", id, objs[id], want)
		}
	}
	for id, want := range map[int]string{
		NpcMtRazmire: "razmire_keelgan", NpcMtRazmireAfflicted: "razmire_keelgan_afflicted", NpcMtUlsquire: "ulsquire_shauncy", NpcMtUlsquireAfflict: "ulsquire_shauncy_afflicted",
		NpcMtShadow: "shadeshadow_level1", NpcMtShade: "shade_level1",
	} {
		if npcs[id] != want {
			t.Errorf("npc %d is %q in the content, the plan means %q", id, npcs[id], want)
		}
	}
	for id, want := range map[int]string{
		LocMtShelf: "shades_experimentshelf", LocMtSink: "mortton_sink", LocMtTable: "shades_experimenttable", LocMtWallBase: "templewall_base", LocMtWallDone: "templewall_10",
		LocMtCornerBase: "templewallcorner_base", LocMtCornerDone: "templewallcorner_10", LocMtAltarLit: "templefire_altar", LocMtAltarUnlit: "templefire_altar_nofire",
		LocMtAltarBroken: "templefire_altar_nofire_broken", LocMtPyreEmpty: "temple_pyre", LocMtPyreLogs: "temple_pyre_logs", LocMtPyreBones: "temple_pyre_bones_logs",
	} {
		if locs[id] != want {
			t.Errorf("loc %d is %q in the content, the plan means %q", id, locs[id], want)
		}
	}
	// The wall pieces run from the base to the tenth level without a gap, and each is a wall the temple script's category takes.
	for i := 0; i <= 10; i++ {
		want := "templewall_" + strconv.Itoa(i)
		if i == 0 {
			want = "templewall_base"
		}
		if locs[LocMtWallBase+i] != want {
			t.Errorf("loc %d is %q, want %q", LocMtWallBase+i, locs[LocMtWallBase+i], want)
		}
		want = "templewallcorner_" + strconv.Itoa(i)
		if i == 0 {
			want = "templewallcorner_base"
		}
		if locs[LocMtCornerBase+i] != want {
			t.Errorf("loc %d is %q, want %q", LocMtCornerBase+i, locs[LocMtCornerBase+i], want)
		}
	}
	// The doses of each vial: the four ids of a family are the pack's own, strongest first.
	for _, fam := range [][]int{MtSerumIDs(), MtPermSerumIDs(), MtOilIDs(), MtSacredIDs()} {
		for i, id := range fam {
			if !strings.HasSuffix(objs[id], strconv.Itoa(4-i)) {
				t.Errorf("obj %d (%q) holds %d doses in the plan and is not the %d-dose vial of its family", id, objs[id], 4-i, 4-i)
			}
		}
	}
}

// The journal's stages are quest_mortton.constant's and quest.constant's, and the temple's varps are the packs' own, transmitted.
func TestMorttonStagesAndVarpsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	consts := map[string]int{}
	re := regexp.MustCompile(`(?m)^\^(mortton_[a-z0-9_]+)\s*=\s*(\d+)\s*$`)
	for _, rel := range [][]string{{"scripts", "quests", "quest_mortton", "configs", "quest_mortton.constant"}, {"scripts", "general", "configs", "quest.constant"}} {
		for _, m := range re.FindAllStringSubmatch(chScript(t, root, rel...), -1) {
			consts[m[1]], _ = strconv.Atoi(m[2])
		}
	}
	for name, want := range map[string]int{
		"mortton_not_started": MtNotStarted, "mortton_read_diary": MtReadDiary, "mortton_made_serum": MtMadeSerum, "mortton_kill_shades": MtKillShades,
		"mortton_killed_1_shade": MtKilled1, "mortton_killed_2_shades": MtKilled2, "mortton_killed_3_shades": MtKilled3, "mortton_killed_4_shades": MtKilled4,
		"mortton_killed_5_shades": MtKilled5, "mortton_shades_to_razmire": MtShadesToRazmire, "mortton_shades_to_ulsquire": MtShadesToUlsquire,
		"mortton_ulsquire_temple": MtUlsquireTemple, "mortton_rebuild_temple": MtRebuildTemple, "mortton_can_light_altar": MtCanLightAltar,
		"mortton_created_sacred_oil": MtSacredOil, "mortton_created_pyre_logs": MtPyreLogs, "mortton_logs_on_pyre": MtLogsOnPyre, "mortton_lit_pyre": MtLitPyre,
		"mortton_quest_complete": MtComplete,
	} {
		if got, ok := consts[name]; !ok || got != want {
			t.Errorf("^%s is %d (found %v) in the content, the plan means %d", name, got, ok, want)
		}
	}
	varps := packNames(t, filepath.Join(root, "pack", "varp.pack"))
	for id, want := range map[int]string{VarpMtRepaired: "temple_repaired_p", VarpMtResources: "temple_resources_p", VarpMtSanctity: "temple_sanctity_p"} {
		if varps[id] != want {
			t.Errorf("varp %d is %q, the plan means %q", id, varps[id], want)
		}
		block := regexp.MustCompile(`(?s)\[` + want + `\](.*?)(\n\[|$)`).FindStringSubmatch(chScript(t, root, "scripts", "quests", "quest_mortton", "configs", "quest_mortton.varp"))
		if block == nil || !strings.Contains(block[1], "transmit=yes") {
			t.Errorf("varp %s is not transmitted: the temple's meters could not be read from the client", want)
		}
	}
	for _, name := range []string{"morttonquest", "morttonmulti"} {
		block := regexp.MustCompile(`(?s)\[` + name + `\](.*?)(\n\[|$)`).FindStringSubmatch(chScript(t, root, "scripts", "quests", "quest_mortton", "configs", "quest_mortton.varp"))
		if block == nil || strings.Contains(block[1], "transmit=yes") {
			t.Errorf("varp %s is transmitted: the stage is read from the journal because it is not", name)
		}
	}
}

// moNormalise is the journal's own text with its colour tags and its line breaks taken out, lower-cased and whitespace-folded, the way ParseMorttonJournal reads a page.
func moNormalise(s string) string {
	s = regexp.MustCompile(`@[a-z0-9]+@`).ReplaceAllString(s, "")
	s = strings.NewReplacer("|", " ").Replace(s)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Every marker the plan reads the journal by is in the journal script, in a string literal it renders, and the markers of the stages are told apart by the script's own
// conditions (a marker of an earlier stage is on every later page, so the plan reads newest first).
func TestMorttonJournalMarkersAreInTheScript(t *testing.T) {
	root := contentRoot(t)
	_, _, journal, _, _ := moScripts(t, root)
	literals := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(journal, -1)
	var pages []string
	for _, l := range literals {
		pages = append(pages, moNormalise(l[1]))
	}
	all := strings.Join(pages, " ")
	for _, m := range mtMarkers {
		if !strings.Contains(all, m.text) {
			t.Errorf("the marker of stage %d, %q, is in no string of mortton_journal.rs2", m.stage, m.text)
		}
	}
	// The quest's name as the journal titles it (an apostrophe the quest tab's spelling leaves out).
	if !strings.Contains(journal, `~quest_journal("Shades of Mort'ton"`) {
		t.Error("the journal's title is not Shades of Mort'ton")
	}
	// Markers are tried newest first: every earlier paragraph is on every later page, so an older marker tried first would read a later page as its own stage.
	for i := 1; i < len(mtMarkers); i++ {
		if mtMarkers[i].stage > mtMarkers[i-1].stage {
			t.Errorf("marker order: stage %d is tried after the older stage %d", mtMarkers[i-1].stage, mtMarkers[i].stage)
		}
	}
}

// The menu lines the executors choose are lines of the scripts' menus (the start of one, up to a question mark or a full stop).
func TestMorttonPicksAreMenuLines(t *testing.T) {
	root := contentRoot(t)
	razmire, ulsquire, _, _, _ := moScripts(t, root)
	razmireLines := moNormalise(strings.Join(regexp.MustCompile(`"([^"]*)"`).FindAllString(razmire, -1), " "))
	ulsquireLines := moNormalise(strings.Join(regexp.MustCompile(`"([^"]*)"`).FindAllString(ulsquire, -1), " "))
	pick := func(name string, picks []string, script string) {
		for _, p := range picks {
			if !strings.Contains(script, strings.ToLower(p)) {
				t.Errorf("%s: %q is no line of the script", name, p)
			}
		}
	}
	pick("send-off", MtSendPicks, razmireLines)
	pick("hand-over", MtHandOverPicks, razmireLines)
	pick("builders", MtBuildersPicks, razmireLines)
	pick("general", MtGeneralPicks, razmireLines)
	pick("temple", MtTemplePicks, ulsquireLines)
	// The lines are menu options, not what the player says in a chat box: each is written with "~chatplayer" only after the menu, so a menu is where it first appears.
	for _, p := range slices.Concat(MtSendPicks, MtHandOverPicks, MtBuildersPicks, MtGeneralPicks) {
		if !regexp.MustCompile(`@multi[0-9a-z_]*\(.*"` + regexp.QuoteMeta(p)).MatchString(razmire) {
			t.Errorf("%q is not an option of any menu of Razmire's script", p)
		}
	}
	for _, p := range MtTemplePicks {
		if !regexp.MustCompile(`@multi[0-9a-z_]*\(.*"` + regexp.QuoteMeta(p)).MatchString(ulsquire) {
			t.Errorf("%q is not an option of any menu of Ulsquire's script", p)
		}
	}
}

// The levels the quest asks for, the amounts of its doses and its pyre, and the ops of its villagers, as the scripts and the configs say them.
func TestMorttonRulesAreTheContents(t *testing.T) {
	root := contentRoot(t)
	razmire, ulsquire, _, temple, pyre := moScripts(t, root)
	for _, c := range []struct{ name, script, want string }{
		{"the temple's Crafting level", temple, "if (stat(crafting) < " + strconv.Itoa(MtCrafting) + ")"},
		{"the temple's hammer", temple, "inv_total(inv, hammer) < 1"},
		{"the building set", temple, "inv_total(inv,woodplank) > 0 & inv_total(inv,swamppaste) > 4 & inv_total(inv,limestonebrick) > 0"},
		{"the set's size", temple, "inv_del(inv, swamppaste, 5)"},
		{"the altar's sanctity", temple, "if(%temple_sanctity_p < 10)"},
		{"the altar's light roll", temple, "stat_random(firemaking, 100, 250)"},
		{"the pyre's logs level", pyre, "stat(firemaking) < $req"},
		{"the pyre's reward queue", pyre, "queue*(funeral_pyre_reward, 2)"},
		{"the pyre's 49 ticks", pyre, "longqueue(clear_pyre_loc, 49, 0, ^accelerate)"},
		{"the remains Razmire wants", razmire, "inv_total(inv, shade_bones1) < " + strconv.Itoa(MtRemainsWanted)},
		{"the remains Razmire keeps", razmire, "inv_del(inv, shade_bones1, 2)"},
		{"the remains Ulsquire keeps", ulsquire, "inv_del(inv, shade_bones1, 1)"},
		{"the dose window", razmire, "longqueue(razmire_reset, 200, 0, ^accelerate)"},
		{"the dose window of Ulsquire", ulsquire, "longqueue(ulsquire_reset, 200, 0, ^accelerate)"},
	} {
		if !strings.Contains(c.script, c.want) {
			t.Errorf("%s: %q is not in the script", c.name, c.want)
		}
	}
	// The brews' levels: the unfinished potion's and the serum's.
	brew := chScript(t, root, "scripts", "skill_herblore", "configs", "brewing", "brew_potion.struct")
	for _, name := range []string{"mort_serum3_tarrominvial", "mort_serum3_ashesvial"} {
		block := regexp.MustCompile(`(?s)\[` + name + `\](.*?)(\n\[|$)`).FindStringSubmatch(brew)
		if block == nil || !strings.Contains(block[1], "brew_potion_level,"+strconv.Itoa(MtHerblore)) {
			t.Errorf("the %s brew is not at Herblore %d", name, MtHerblore)
		}
	}
	// The villagers' ops: the plan's Talk-to (never "Talk"), the two shops by their ops.
	npcs := chScript(t, root, "scripts", "quests", "quest_mortton", "configs", "quest_mortton.npc")
	razBlock := regexp.MustCompile(`(?s)\[razmire_keelgan\](.*?)(\n\[|$)`).FindStringSubmatch(npcs)
	if razBlock == nil || !strings.Contains(razBlock[1], "op1=Talk-to") || !strings.Contains(razBlock[1], "op3=Trade-General-Store") || !strings.Contains(razBlock[1], "op4=Trade-Builders-Store") {
		t.Errorf("Razmire's ops: %v", razBlock)
	}
	// The shops' stock: the general store sells the hammer and the three-dose olive oil, the builders' store the planks, the bricks and the paste.
	inv := chScript(t, root, "scripts", "_unpack", "274", "all.inv")
	general := regexp.MustCompile(`(?s)\[razmiregeneralstore\](.*?)(\n\[|$)`).FindStringSubmatch(inv)
	builders := regexp.MustCompile(`(?s)\[razmirebuildingstore\](.*?)(\n\[|$)`).FindStringSubmatch(inv)
	if general == nil || builders == nil {
		t.Fatal("the stores are not in all.inv")
	}
	for _, want := range []string{"=hammer,", "=oliveoil3,", "=tinderbox,", "=vial_water,"} {
		if !strings.Contains(general[1], want) {
			t.Errorf("Razmire's general store does not stock %s", want)
		}
	}
	for _, want := range []string{"=woodplank,", "=limestonebrick,", "=swamppaste,"} {
		if !strings.Contains(builders[1], want) {
			t.Errorf("Razmire's builders' store does not stock %s", want)
		}
	}
	if !strings.Contains(razmire, "~openshop(razmiregeneralstore") || !strings.Contains(razmire, "~openshop(razmirebuildingstore") {
		t.Error("the shops are opened by other names")
	}
}

// The tiles the plan walks to are where the content's maps put the spawns: the afflicted villagers, the shades, the vials and the log spawns by Varrock's bank.
func TestMorttonSpawnsAreWhereThePlanSaysTheyAre(t *testing.T) {
	root := contentRoot(t)
	npcs := chMapRows(t, root, "NPC")
	objs := chMapRows(t, root, "OBJ")
	has := func(rows []tile.Tile, at tile.Tile) bool {
		for _, r := range rows {
			if r == at {
				return true
			}
		}
		return false
	}
	if !has(npcs[NpcMtRazmireAfflicted], MtRazmire) {
		t.Errorf("Razmire's spawn is %v, the plan says %v", npcs[NpcMtRazmireAfflicted], MtRazmire)
	}
	if !has(npcs[NpcMtUlsquireAfflict], MtUlsquire) {
		t.Errorf("Ulsquire's spawn is %v, the plan says %v", npcs[NpcMtUlsquireAfflict], MtUlsquire)
	}
	shadows := npcs[NpcMtShadow]
	if len(shadows) < 20 {
		t.Errorf("%d shadows in the maps, the plan counts on some 25", len(shadows))
	}
	for _, at := range shadows {
		if !InMortton(at) {
			t.Errorf("a shadow spawns at %v, outside the town's square", at)
		}
		if tile.Chebyshev(at, MtShadeField) > MtShadeFieldRadius+5 {
			t.Errorf("a shadow spawns at %v, more than %d tiles from the field %v: the hunt would never choose it", at, MtShadeFieldRadius+5, MtShadeField)
		}
	}
	if !has(objs[ObjMtVial], MtVials) {
		t.Errorf("the vials' spawns are %v, the plan says %v", objs[ObjMtVial], MtVials)
	}
	for _, at := range []tile.Tile{{X: 3243, Z: 3395}, {X: 3243, Z: 3397}} {
		if !has(objs[ObjMtLogs], at) {
			t.Errorf("no log spawn at %v (the spawns are %v)", at, objs[ObjMtLogs])
		}
	}
	// Both spawns of the vials and the shelf, the table and the sink are inside one house of the town.
	for _, at := range []tile.Tile{MtShelf, MtTable, MtVials, MtSink} {
		if !InMortton(at) {
			t.Errorf("%v is outside the town", at)
		}
	}
	// The temple's centre (shadow_transform_center_coord 0_54_51_46_54) and its altar (0_54_51_50_52) are the plan's tiles.
	if centre := (tile.Tile{X: 54*64 + 46, Z: 51*64 + 54}); !InMortton(centre) || !MtInTempleZone(centre) {
		t.Errorf("the temple's centre %v is outside the temple's zone", centre)
	}
	if MtAltar != (tile.Tile{X: 3506, Z: 3316}) {
		t.Errorf("the altar's tile %v, the content's coordinate 0_54_51_50_52 is (3506,3316)", MtAltar)
	}
	if MtRazmire != (tile.Tile{X: 3489, Z: 3296}) {
		t.Errorf("Razmire's tile %v, the content's coordinate 0_54_51_33_32 is (3489,3296)", MtRazmire)
	}
}
