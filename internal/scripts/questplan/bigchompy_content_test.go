package questplan

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Big Chompy Bird Hunting's plan against the content checkout (RSB0T_CONTENT): the ids it carries, the stages and levels, the lines it chooses from the
// scripts' menus, the spawns its tiles and its hazards are made from, and the foe's stats. Each test skips without the checkout.

var chMaps struct {
	once sync.Once
	rows map[string]map[int][]tile.Tile
	err  error
}

// chMapRows is every row of one section of the content maps ("NPC" or "OBJ": the "level x z: id ..." lines), by id.
func chMapRows(t *testing.T, root, section string) map[int][]tile.Tile {
	t.Helper()
	chMaps.once.Do(func() {
		chMaps.rows = map[string]map[int][]tile.Tile{"NPC": {}, "OBJ": {}}
		files, err := filepath.Glob(filepath.Join(root, "maps", "m*_*.jm2"))
		if err != nil {
			chMaps.err = err
			return
		}
		name := regexp.MustCompile(`m(\d+)_(\d+)\.jm2$`)
		row := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)`)
		for _, f := range files {
			m := name.FindStringSubmatch(filepath.ToSlash(f))
			if m == nil {
				continue
			}
			rx, _ := strconv.Atoi(m[1])
			rz, _ := strconv.Atoi(m[2])
			raw, err := os.ReadFile(f)
			if err != nil {
				chMaps.err = err
				return
			}
			in := ""
			for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
				switch {
				case strings.HasPrefix(line, "==== "):
					in = strings.TrimSuffix(strings.TrimPrefix(line, "==== "), " ====")
				case in == "NPC" || in == "OBJ":
					if r := row.FindStringSubmatch(strings.TrimSpace(line)); r != nil {
						lv, _ := strconv.Atoi(r[1])
						x, _ := strconv.Atoi(r[2])
						z, _ := strconv.Atoi(r[3])
						id, _ := strconv.Atoi(r[4])
						chMaps.rows[in][id] = append(chMaps.rows[in][id], tile.Tile{X: rx*64 + x, Z: rz*64 + z, Level: lv})
					}
				}
			}
		}
	})
	if chMaps.err != nil {
		t.Fatal(chMaps.err)
	}
	return chMaps.rows[section]
}

// chHead is up to n bytes of s from i.
func chHead(s string, i, n int) string {
	if i < 0 {
		return ""
	}
	return s[i:min(len(s), i+n)]
}

// chScript reads a content file, line endings folded.
func chScript(t *testing.T, root string, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// The ids the plan carries are the content packs' names for them.
func TestChompyIDsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	objs := packNames(t, filepath.Join(root, "pack", "obj.pack"))
	npcs := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	locs := packNames(t, filepath.Join(root, "pack", "loc.pack"))
	for id, want := range map[int]string{
		ObjChLogs: "achey_tree_logs", ObjChShaft: "ogre_arrow_shaft", ObjChFlighted: "ogre_headless_arrow", ObjChWolfBones: "wolf_bones", ObjChTips: "wolfbone_arrowheads",
		ObjChArrow: "ogre_arrow", ObjChBow: "ogre_bow", ObjChBellowsEmpty: "empty_ogre_bellows", ObjChBellows3: "filled_ogre_bellow3", ObjChBellows2: "filled_ogre_bellow2",
		ObjChBellows1: "filled_ogre_bellow1", ObjChToad: "bloated_toad", ObjChRawChompy: "raw_chompy", ObjChRuinedChompy: "ruined_chompy", ObjChSeasonedChomp: "cooked_s_chompy",
		ObjChFeather: "feather", ObjChChisel: "chisel", ObjChPotato: "potato", ObjChOnion: "onion", ObjChCabbage: "cabbage", ObjChTomato: "tomato", ObjChDoogle: "doogleleaves",
		ObjChEqua: "equa_leaves", ObjKnife: "knife", CoinsID: "coins", ObjShark: "shark", ObjBronzeAxe: "bronze_axe",
	} {
		if objs[id] != want {
			t.Errorf("obj %d is %q in the content, the plan means %q", id, objs[id], want)
		}
	}
	for id, want := range map[int]string{
		NpcChRantz: "rantz", NpcChFycie: "fycie", NpcChBugs: "bugs", NpcChToad: "toad", NpcChBloatedToad: "bloated_toad", NpcChChompy: "chompy_bird",
		NpcChChompyDead: "chompybird_dead", NpcFeldipWolf: "wolf",
	} {
		if npcs[id] != want {
			t.Errorf("npc %d is %q in the content, the plan means %q", id, npcs[id], want)
		}
	}
	for id, want := range map[int]string{
		LocChChestShut: "chompybird_chest", LocChChestOpen: "chompybird_chest_open", LocChSpitEmpty: "chompybird_spitroast_empty", LocChBubbles: "swampbubbles",
		LocChAchey: "achey_tree", LocChPotato: "potato", LocChCabbage: "cabbage", LocChOnion: "onion", LocChCaveMouth: "rantzogrecaveentrance",
	} {
		if locs[id] != want {
			t.Errorf("loc %d is %q in the content, the plan means %q", id, locs[id], want)
		}
	}
	// The seasonings' table agrees with the packs, and every patch has an op to Pick.
	if len(ChSeasoningList()) != ChSeasoningCount {
		t.Errorf("%d seasonings, want %d", len(ChSeasoningList()), ChSeasoningCount)
	}
	for _, c := range ChSeasoningList() {
		if c.Loc != 0 && locs[c.Loc] != strings.ToLower(strings.Fields(c.Name)[0]) {
			t.Errorf("the %s patch is loc %d (%q)", c.Name, c.Loc, locs[c.Loc])
		}
	}
}

// The stages are the content's own, and the quest is worth what the plan says.
func TestChompyStagesAndRewardsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	consts := chScript(t, root, "scripts", "quests", "quest_chompybird", "configs", "quest_chompybird.constant")
	val := func(name string) int {
		m := regexp.MustCompile(`\^` + name + ` = (\d+)`).FindStringSubmatch(consts)
		if m == nil {
			t.Fatalf("no %s in quest_chompybird.constant", name)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	for name, want := range map[string]int{
		"chompybird_not_started": ChNotStarted, "chompybird_started": ChStarted, "chompybird_given_arrows": ChGivenArrows, "chompybird_kids_play_with_toad": ChToadies,
		"chompybird_shown_toad": ChShownToad, "chompybird_dropped_toad": ChDroppedToad, "chompybird_rantz_tried_to_shoot_chompy": ChRantzMissed,
		"chompybird_rantz_gave_player_bow": ChGotBow, "chompybird_player_killed_chompy": ChKilled, "chompybird_told_to_cook_chompy": ChToldToCook,
		"chompybird_chompy_cooked": ChCooked,
	} {
		if got := val(name); got != want {
			t.Errorf("%s is %d in the content, the plan says %d", name, got, want)
		}
	}
	general := chScript(t, root, "scripts", "general", "configs", "quest.constant")
	if !regexp.MustCompile(`\^chompybird_complete = 65`).MatchString(general) || !regexp.MustCompile(`\^chompybird_questpoints = 2`).MatchString(general) {
		t.Error("quest.constant does not say ^chompybird_complete = 65 and ^chompybird_questpoints = 2")
	}
	quest := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "quest_chompybird.rs2")
	for _, want := range []string{"stat_advance(fletching, 2620)", "stat_advance(cooking, 14700)", "stat_advance(ranged, 7350)"} {
		if !strings.Contains(quest, want) {
			t.Errorf("the completion queue does not say %q", want)
		}
	}
	// The journal's markers are lines of the journal script.
	journal := strings.ToLower(regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "chompybird_journal.rs2"), " "))
	journal = regexp.MustCompile(`\s+`).ReplaceAllString(strings.ReplaceAll(journal, "|", " "), " ")
	for _, m := range chMarkers {
		if m.stage == ChComplete {
			if !strings.Contains(journal, "quest complete!") {
				t.Error("the journal script has no QUEST COMPLETE! line")
			}
			continue
		}
		if !strings.Contains(journal, m.text) {
			t.Errorf("stage %d: the marker %q is not in chompybird_journal.rs2", m.stage, m.text)
		}
	}
}

// The levels, the item rules and the numbers the plan's flow rests on are the scripts'.
func TestChompyScriptsSayWhatThePlanReads(t *testing.T) {
	root := contentRoot(t)
	rantz := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "rantz.rs2")
	bugs := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "bugs.rs2")
	fycie := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "fycie.rs2")
	raw := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "raw_chompy.rs2")
	bird := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "chompy_bird.rs2")
	toad := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "bloated_toad.rs2")
	chest := chScript(t, root, "scripts", "quests", "quest_chompybird", "scripts", "ogre_chest.rs2")
	arrows := chScript(t, root, "scripts", "skill_fletching", "scripts", "ogre_arrows.rs2")
	// The menu lines the talks choose by (substring, case-insensitive: the plan's lists are prefixes).
	lower := func(s string) string { return strings.ToLower(s) }
	for _, p := range append(append([]string{}, chToadiesMenuLines...), "Ok, I'll make you some 'stabbers'") {
		if !strings.Contains(lower(rantz), lower(p)) {
			t.Errorf("Rantz's script has no line %q", p)
		}
	}
	for _, p := range []string{"Come on, let me have a go", "I'm actually quite strong", "Yes, I'll buy the bow."} {
		if !strings.Contains(lower(rantz), lower(p)) {
			t.Errorf("Rantz's script has no line %q", p)
		}
	}
	if !strings.Contains(bugs, "Ok, I'll give you 10 bright pretties.") || !strings.Contains(bugs, "inv_total(inv, coins) < 10") ||
		!strings.Contains(bugs, "inv_add(inv, chisel, 1)") || !strings.Contains(bugs, "inv_add(inv, knife, 1)") {
		t.Error("Bugs's script does not sell a chisel and a knife for ten coins")
	}
	if !strings.Contains(fycie, "inv_add(inv, feather, 25)") || !strings.Contains(fycie, "inv_total(inv, coins) < 50") {
		t.Error("Fycie's script does not sell 25 feathers for fifty coins")
	}
	// The levels.
	if !strings.Contains(raw, "stat(cooking) < 30") {
		t.Error("the spit does not ask for Cooking 30")
	}
	for _, want := range []string{"stat(fletching) < 5", "stat(crafting) < 5", "def_int $shaft_count = ~random_range(2, 6)", "def_int $tip_count = ~random_range(2, 6)",
		"min(inv_total(inv, feather), 24), 4)", "min(inv_total(inv, ogre_arrow_shaft), 6)", "min(inv_total(inv, ogre_headless_arrow), 6)",
		"%chompybird_kills = setbit(%chompybird_kills, ^chompybird_varbit_made_arrows)"} {
		if !strings.Contains(arrows, strings.TrimSpace(want)) && !strings.Contains(strings.ReplaceAll(arrows, "divide(", ""), strings.TrimSpace(want)) {
			t.Errorf("ogre_arrows.rs2 lacks %q", want)
		}
	}
	bows := chScript(t, root, "scripts", "skill_combat", "configs", "ranged", "bows.obj")
	if i := strings.Index(bows, "[ogre_bow]"); i < 0 || !strings.Contains(chHead(bows, i, 700), "param=levelrequire,30") {
		t.Error("the ogre bow does not ask for Ranged 30")
	}
	arrowObj := chScript(t, root, "scripts", "skill_fletching", "configs", "ogre", "ogre_arrows.obj")
	if i := strings.Index(arrowObj, "[ogre_arrow]"); i < 0 || !strings.Contains(chHead(arrowObj, i, 900), "param=levelrequire,30") || !strings.Contains(chHead(arrowObj, i, 900), "iop2=Wield") {
		t.Error("the ogre arrow does not ask for Ranged 30 and offer Wield")
	}
	// The chompy: ten hitpoints, Attack on op 5, never fights back; the dead one is plucked (op 4) for a raw chompy, bones and feathers.
	npcs := chScript(t, root, "scripts", "quests", "quest_chompybird", "configs", "quest_chompybird.npc")
	if i := strings.Index(npcs, "[chompy_bird]"); i < 0 || !strings.Contains(chHead(npcs, i, 700), "op5=Attack") || !strings.Contains(chHead(npcs, i, 700), "hitpoints=10") {
		t.Error("[chompy_bird] is not op5 Attack with 10 hitpoints")
	}
	if i := strings.Index(npcs, "[chompybird_dead]"); i < 0 || !strings.Contains(chHead(npcs, i, 500), "op4=Pluck") {
		t.Error("[chompybird_dead] is not op4 Pluck")
	}
	for _, want := range []string{"obj_add(npc_coord, raw_chompy, 1, ^lootdrop_duration)", "inv_add(inv, feather, calc(random(20) + 10))", "Chompies don't fight back",
		"The Chompy Bird is too quick for your melee weapon.", "$rhand ! ogre_bow", "$ammo ! ogre_arrow", "This is not your Chompy Bird to shoot."} {
		if !strings.Contains(bird, want) {
			t.Errorf("chompy_bird.rs2 lacks %q", want)
		}
	}
	// The bait: the clearing's corners, the one-in-five roll every 25 ticks, the placement refused anywhere else.
	if !strings.Contains(toad, "inzone(0_41_46_7_18, 0_41_46_15_27, coord) = false") || !strings.Contains(toad, "npc_queue(4, 0, 25)") || !strings.Contains(toad, "random(5) = 1") ||
		!strings.Contains(toad, "iop1") && !strings.Contains(chScript(t, root, "scripts", "quests", "quest_chompybird", "configs", "quest_chompybird.obj"), "iop1=Drop") {
		t.Error("the bloated toad's script is not the one the plan describes")
	}
	// 0_41_46_7_18 is the mapsquare x 41, z 46, local 7,18: 2624+7 = 2631, 2944+18 = 2962; 0_41_46_15_27 is 2639, 2971.
	if ChBaitZone.MinX != 41*64+7 || ChBaitZone.MinZ != 46*64+18 || ChBaitZone.MaxX != 41*64+15 || ChBaitZone.MaxZ != 46*64+27 {
		t.Errorf("the clearing is %+v, the content's corners are (%d,%d) to (%d,%d)", ChBaitZone, 41*64+7, 46*64+18, 41*64+15, 46*64+27)
	}
	// The chest: a Strength roll that costs a level on a miss, the stage 15 gate, a pair for an account with none.
	for _, want := range []string{"if (%chompybird < ^chompybird_kids_play_with_toad)", "stat_random(strength, 30, 254)", "stat_sub(strength, 1, 0)",
		"inv_totalcat(inv, ogre_bellows) > 0 | inv_totalcat(bank, ogre_bellows) > 0", "You search but find nothing in the ogre chest.", "inv_add(inv, empty_ogre_bellows, 1)"} {
		if !strings.Contains(chest, want) {
			t.Errorf("ogre_chest.rs2 lacks %q", want)
		}
	}
	// The bow: lent at 40, sold at 45 when lost, for 500 to 550 coins.
	for _, want := range []string{"~random_range(500, 550)", "inv_add(inv, ogre_bow, 1)", "%chompybird = ^chompybird_rantz_gave_player_bow"} {
		if !strings.Contains(rantz, want) {
			t.Errorf("rantz.rs2 lacks %q", want)
		}
	}
	// The roast: the six seasonings are the ones the spit deletes, and a burn loses them as well as a roast.
	for _, want := range []string{"inv_del(inv, potato, 1)", "inv_del(inv, onion, 1)", "inv_del(inv, equa_leaves, 1)", "inv_del(inv, cabbage, 1)", "inv_del(inv, tomato, 1)", "inv_del(inv, doogleleaves, 1)",
		"stat_random(cooking, 200, 255)", "You need to find what Bugs wants on the chompy bird before you can cook it."} {
		if !strings.Contains(raw, want) {
			t.Errorf("raw_chompy.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(raw, "[oplocu,chompybird_spitroast_empty]") {
		t.Error("the spit's use-on is not on the empty spit loc")
	}
	// The kills Rantz counts: a shot at 45 moves the quest to 50, and the stage is untouched after.
	if !strings.Contains(bird, "if (%chompybird = ^chompybird_rantz_gave_player_bow) {") {
		t.Error("the kill queue is not at stage 45")
	}
	// The Strength of the chest and the quiver level are the plan's stated hard parts: the arrows' own script gives the limits of one use (24 feathers, 6 shafts, 6 arrows).
	if !strings.Contains(arrows, "min(inv_total(inv, feather), 24), 4") && !strings.Contains(arrows, "min(inv_total(inv, feather), 24)") {
		t.Error("the flighting is not capped at 24 feathers a use")
	}
}

// chToadiesMenuLines are the plan's menu lines for the toadies talk, as the questmods package spells them (the test spells them again so a rename fails here).
var chToadiesMenuLines = []string{"How do we make the chompys come?", "Ok, thanks."}

// The spawns the plan's tiles and hazards are made from.
func TestChompySpawnsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	npcs := chMapRows(t, root, "NPC")
	objs := chMapRows(t, root, "OBJ")
	near := func(list []tile.Tile, want tile.Tile, within int) bool {
		for _, at := range list {
			if tile.Chebyshev(at, want) <= within {
				return true
			}
		}
		return false
	}
	// Rantz, Bugs and Fycie stand where the plan's stops are.
	for id, want := range map[int]tile.Tile{NpcChRantz: ChRantzAnchor, NpcChBugs: ChBugsAnchor, NpcChFycie: ChFycieAnchor} {
		if !near(npcs[id], want, 1) {
			t.Errorf("npc %d spawns at %v, the plan's stop is %v", id, npcs[id], want)
		}
	}
	// The wolves: six in the Feldip pack, every one within the hunt's radius of its field; no other wolf of the pack is in the country round Rantz.
	in := 0
	for _, at := range npcs[NpcFeldipWolf] {
		if tile.Chebyshev(at, ChWolfField) <= ChWolfRadius {
			in++
		}
		if at.X >= 2560 && at.X <= 2700 && at.Z >= 2900 && at.Z <= 3010 && tile.Chebyshev(at, ChWolfField) > ChWolfRadius+2 {
			t.Errorf("a wolf at %v is in Rantz's country and outside the hunt's field", at)
		}
	}
	if in != 6 {
		t.Errorf("%d wolves within %d of %v, the plan was made from six", in, ChWolfRadius, ChWolfField)
	}
	// The toads the bellows are used on stand round the pool, the bubble's lip among them.
	count := 0
	for _, at := range npcs[NpcChToad] {
		if tile.Chebyshev(at, ChBubbleStand) <= 8 {
			count++
		}
	}
	if count < 6 {
		t.Errorf("%d swamp toads within 8 of the pool's lip, the plan was made from eight", count)
	}
	// The ground seasonings.
	for id, want := range map[int]tile.Tile{ObjChEqua: ChEquaSpawn, ObjChTomato: ChTomatoSpawn, ObjChDoogle: ChDoogleSpawn} {
		if !near(objs[id], want, 3) {
			t.Errorf("obj %d spawns at %v, the plan's place is %v", id, objs[id], want)
		}
	}
	// Every candidate seasoning is somewhere the walk reaches in one trip: the patches and the spawns are within 90 tiles of the clearing.
	for _, c := range ChSeasoningList() {
		if tile.Chebyshev(c.At, ChBaitSpot) > 90 {
			t.Errorf("the %s at %v is %d tiles from the clearing", c.Name, c.At, tile.Chebyshev(c.At, ChBaitSpot))
		}
	}
}

// The wolf's stats are the content's.
func TestChompyWolfIsTheContents(t *testing.T) {
	root := contentRoot(t)
	all := chScript(t, root, "scripts", "_unpack", "225", "all.npc")
	i := strings.Index(all, "\n[wolf]\n")
	if i < 0 {
		t.Fatal("no [wolf] in the 225 unpack")
	}
	block := all[i+1:]
	if j := strings.Index(block[1:], "\n["); j >= 0 {
		block = block[:j+1]
	}
	for _, want := range []string{"attack=50", "strength=55", "defence=52", "hitpoints=69", "vislevel=64", "huntmode=cowardly", "huntrange=2", "param=death_drop,wolf_bones", "op2=Attack"} {
		if !strings.Contains(block, want) {
			t.Errorf("[wolf] lacks %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "attackrate") || strings.Contains(block, "param=attackbonus") || strings.Contains(block, "param=strengthbonus") {
		t.Errorf("[wolf] names an attack rate or a bonus the model leaves out:\n%s", block)
	}
	f := FoeFeldipWolf
	if f.Attack != 50 || f.Strength != 55 || f.Defence != 52 || f.Hitpoints != 69 || f.AttackBonus != 0 || f.StrengthBonus != 0 || f.AttackTicks != 4 {
		t.Errorf("FoeFeldipWolf %+v is not the content's wolf", f)
	}
	// The hunt: cowardly, an account within two tiles with a line of sight whose combat is under twice the wolf's.
	hunt := chScript(t, root, "scripts", "_unpack", "225", "all.hunt")
	k := strings.Index(hunt, "[cowardly]")
	if k < 0 || !strings.Contains(chHead(hunt, k, 400), "check_nottoostrong=outside_wilderness") || !strings.Contains(chHead(hunt, k, 400), "check_vis=lineofsight") {
		t.Error("[cowardly] is not a line-of-sight hunt that spares an account too strong for the wolf")
	}
}

// The hostile spawn lists the enclave is made from are the content maps': every wolf and every ogre of the Feldip hills, no more and no fewer.
func TestChompyHostileSpawnsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	npcs := chMapRows(t, root, "NPC")
	inHills := func(at tile.Tile) bool {
		return at.Level == 0 && at.X >= 2540 && at.X <= 2700 && at.Z >= 2900 && at.Z <= 3010
	}
	sameSet := func(name string, got []tile.Tile, want []tile.Tile) {
		t.Helper()
		have := map[tile.Tile]bool{}
		for _, at := range got {
			if inHills(at) {
				have[at] = true
			}
		}
		if len(have) != len(want) {
			t.Errorf("%s: the maps hold %d in the hills (%v), the plan lists %d", name, len(have), have, len(want))
		}
		for _, at := range want {
			if !have[at] {
				t.Errorf("%s: the plan lists %v and the maps do not", name, at)
			}
		}
	}
	sameSet("wolves", npcs[NpcFeldipWolf], ChWolfSpawns)
	sameSet("ogres", npcs[NpcOgre], ChOgreSpawns)
}

// The ogre's stats and its reach are the content's: [ogre] in the Yanille configs, and the two reaches are the wander plus the hunt's range.
func TestChompyOgreIsTheContents(t *testing.T) {
	root := contentRoot(t)
	all := chScript(t, root, "scripts", "areas", "area_yanille", "configs", "ogre.npc")
	i := strings.Index(all, "[ogre]\n")
	if i < 0 {
		t.Fatal("no [ogre] in ogre.npc")
	}
	block := all[i:]
	if j := strings.Index(block[1:], "\n["); j >= 0 {
		block = block[:j+1]
	}
	for _, want := range []string{"vislevel=53", "attack=43", "defence=43", "strength=43", "hitpoints=60", "huntrange=2", "huntmode=cowardly", "wanderrange=3", "maxrange=5", "param=attackrate,6",
		"param=attackbonus,22", "param=strengthbonus,20", "op2=Attack"} {
		if !strings.Contains(block, want) {
			t.Errorf("[ogre] lacks %q:\n%s", want, block)
		}
	}
	// The reaches: the ogre wanders to maxrange 5 and hunts 2 tiles further; the wolf's wanderrange is 6 and its huntrange 2.
	if ChOgreReach != 5+2 || ChWolfReach != 6+2 {
		t.Errorf("reaches %d and %d, the content says 7 and 8", ChOgreReach, ChWolfReach)
	}
	f := FoeOgre
	if f.Attack != 43 || f.Strength != 43 || f.Defence != 43 || f.Hitpoints != 60 || f.AttackBonus != 22 || f.StrengthBonus != 20 || f.AttackTicks != 6 {
		t.Errorf("FoeOgre %+v is not the content's ogre", f)
	}
	// Two things the plan's park reasons say: the onion patch and the tomato spawn are within two tiles of an ogre's spawn.
	for name, at := range map[string]tile.Tile{"the onion patch": ChOnionPatch, "the tomato spawn": ChTomatoSpawn} {
		near := 1 << 30
		for _, o := range ChOgreSpawns {
			near = min(near, tile.Chebyshev(at, o))
		}
		if near > 2 {
			t.Errorf("%s is %d tiles from the nearest ogre spawn", name, near)
		}
	}
}
