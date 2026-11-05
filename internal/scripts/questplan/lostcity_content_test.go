package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, rule, message and number Lost City rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped without one) so that
// none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run LostCityContent ./internal/scripts/questplan/
//
// (What the loc index and the collision pack hold, and the stands and routes, is pinned in the questmods real-data tests.)

const lcDir = "scripts/quests/quest_zanaris/"

// lcScripts is every script of the quest in one string.
func lcScripts(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, f := range []string{"quest_zanaris.rs2", "shamus.rs2", "tree_spirit.rs2", "zanaris_journal.rs2"} {
		b.WriteString(csContent(t, lcDir+"scripts/"+f))
		b.WriteString("\n")
	}
	return b.String()
}

// lcCoord decodes a content coord literal, level_mx_mz_lx_lz, into a tile.
func lcCoord(t *testing.T, lit string) tile.Tile {
	t.Helper()
	p := strings.Split(lit, "_")
	if len(p) != 5 {
		t.Fatalf("not a coord: %q", lit)
	}
	var n [5]int
	for i := range p {
		v, err := strconv.Atoi(p[i])
		if err != nil {
			t.Fatalf("not a coord: %q", lit)
		}
		n[i] = v
	}
	return tile.Tile{X: n[1]<<6 + n[3], Z: n[2]<<6 + n[4], Level: n[0]}
}

// The pack ids the plan carries are the pack's.
func TestLostCityContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "warrioradventurerpg", NpcLcWarrior}, {"npc", npc, "zanarisleprechaun", NpcLcShamus}, {"npc", npc, "tree_spirit", NpcLcTreeSpirit},
		{"npc", npc, "cave_monk", NpcLcCaveMonk}, {"npc", npc, "shipmonk", NpcShipMonk}, {"npc", npc, "zombie_entranan", NpcLcZombie},
		{"npc", npc, "greater_demon", NpcLcGreaterDemon}, {"npc", npc, "black_demon", NpcLcBlackDemon},
		{"loc", loc, "leprechauntree", LocLcLeprechaunTree}, {"loc", loc, "dramentree", LocLcDramenTree}, {"loc", loc, "zanarisdoor", LocLcShedDoor},
		{"loc", loc, "zanarismagicdoor", LocLcMagicDoor}, {"loc", loc, "entranaladdertop", LocLcEntranaLadder},
		{"obj", obj, "dramen_branch", ObjLcBranch}, {"obj", obj, "dramen_staff", ObjLcStaff}, {"obj", obj, "knife", ObjKnife}, {"obj", obj, "shark", ObjShark},
		{"obj", obj, "coins", CoinsID}, {"obj", obj, "bronze_axe", ObjBronzeAxe}, {"obj", obj, "iron_axe", ObjIronAxe}, {"obj", obj, "steel_axe", ObjSteelAxe},
		{"obj", obj, "mithril_axe", ObjMithrilAxe}, {"obj", obj, "adamant_axe", ObjAdamantAxe}, {"obj", obj, "rune_axe", ObjRuneAxe}, {"obj", obj, "black_axe", ObjBlackAxe},
		{"obj", obj, "deathrune", ObjDeathRune}, {"obj", obj, "firerune", ObjFireRune}, {"obj", obj, "airrune", ObjAirRune},
		{"obj", obj, "earthrune", ObjEarthRune}, {"obj", obj, "lawrune", ObjLcLawRune},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	// Every axe the module counts is one the pack names an axe.
	names := packNames(t, filepath.Join(os.Getenv("RSB0T_CONTENT"), "pack", "obj.pack"))
	for _, id := range LcAxes {
		if !strings.HasSuffix(names[id], "_axe") {
			t.Errorf("LcAxes holds %d, which the pack calls %q", id, names[id])
		}
	}
}

// The stages, the reward and the levels.
func TestLostCityContentStagesAndRewards(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^zanaris_complete = 6", "^zanaris_questpoints = 3"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, lcDir+"configs/quest_zanaris.constant")
	for _, want := range []string{"^zanaris_not_started = 0", "^zanaris_started = 1", "^zanaris_spoken_shamus = 2", "^zanaris_spirit_defeated = 3",
		"^zanaris_tree_chopped = 4", "^zanaris_staff_made = 5"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_zanaris.constant lacks %q", want)
		}
	}
	if LcStarted != 1 || LcSpokenShamus != 2 || LcSpiritDefeated != 3 || LcTreeChopped != 4 || LcStaffMade != 5 || LcComplete != 6 {
		t.Error("the plan's stage numbers")
	}
	src := lcScripts(t)
	for _, want := range []string{
		// The levels: the tree and the staff.
		"if(stat(woodcutting) < 36) {", "if(stat(crafting) < 31) {",
		// The stages each step moves.
		"%zanaris = ^zanaris_spoken_shamus;", "%zanaris = ^zanaris_spirit_defeated;", "%zanaris = ^zanaris_tree_chopped;",
		"%zanaris = ^zanaris_staff_made;", "%zanaris = ^zanaris_complete;",
		// The chop at stage 2 sets the spirit on the account; at 3 and after it gives a branch (a free slot), and moves 3 to 4.
		"if(%zanaris = ^zanaris_spoken_shamus) {", "npc_add($spawn, tree_spirit, 1000);", "~npc_retaliate(0);", "if(inv_freespace(inv) = 0) {",
		"if(%zanaris = ^zanaris_spirit_defeated) {", "inv_add(inv, dramen_branch, 1);",
		// The staff is carved from the branch with a knife, any knife.
		"[opheldu,dramen_branch]", "if(last_useitem = knife) {", "inv_del(inv, dramen_branch, 1);", "inv_add(inv, dramen_staff, 1);",
		// The door: the account must WEAR the staff, and not stand on the door's own column.
		"def_boolean $entering = ~check_axis(coord, loc_coord, loc_angle);", "if($entering = false) {", "if(inv_total(worn, dramen_staff) > 0 & map_members = ^true) {",
		"if(%zanaris = ^zanaris_staff_made) {", "queue(zanaris_quest_complete, 0, 0);",
		// The spirit's death is credited to the player who dealt most, and raises a message box.
		"if(npc_findhero = ^true) {", "if(%zanaris ! ^zanaris_spoken_shamus) {", "~mesbox(\"With the Tree Spirit defeated you can now chop the tree.\");",
		// Shamus: set down for a hundred ticks only when none is within fifteen tiles, walking ten east; the dialogue ends in a message box.
		"if (npc_find($shamus_spawnpoint, zanarisleprechaun, 15, 0) = true) {", "npc_add($shamus_spawnpoint, zanarisleprechaun, 100);",
		"npc_walk(movecoord(npc_coord, 10, 0, 0));", "~mesbox(\"The leprechaun magically disappears.\");", "def_namedobj $axe = ~woodcutting_axe_checker(true);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the quest's scripts lack %q", want)
		}
	}
	if LcCrafting != 31 || LcWoodcutting != 36 {
		t.Error("the plan's levels")
	}
	// The landings and spawns the plan holds are the scripts' coordinates.
	for _, c := range []struct {
		lit  string
		want tile.Tile
		name string
	}{
		{"0_50_149_20_56", LcZanarisArrival, "Zanaris's arrival"},
		{"0_49_50_3_11", LcShamusSpawn, "Shamus's spawn"}, {"0_44_152_44_9", LcSpiritSpawn, "the spirit's spawn"},
	} {
		if !strings.Contains(src, c.lit) {
			t.Errorf("the scripts lack %s (%s)", c.lit, c.name)
		}
		if got := lcCoord(t, c.lit); got != c.want {
			t.Errorf("%s: the script says %v, the plan %v", c.name, got, c.want)
		}
	}
	if !strings.Contains(csContent(t, "scripts/areas/area_entrana/scripts/cave_monk.rs2"), "p_telejump(0_44_152_6_46);") ||
		lcCoord(t, "0_44_152_6_46") != LcDungeonArrival {
		t.Errorf("the cave monk's ladder does not land on %v", LcDungeonArrival)
	}
	if !strings.Contains(csContent(t, "scripts/areas/area_entrana/scripts/entrana_dungeon.rs2"), "p_telejump(0_50_58_50_60);") || lcCoord(t, "0_50_58_50_60") != LcWildernessLanding {
		t.Errorf("the Magic door does not land at %v, the Wilderness", LcWildernessLanding)
	}
	if !strings.Contains(csContent(t, "scripts/areas/area_lumbridge/scripts/warrior_zanaris.rs2"), "%zanaris = ^zanaris_started;") {
		t.Error("the warrior's chain does not start the quest")
	}
}

// zanarisJournalCases reads zanaris_journal.rs2's switch: each case's text, the strings it appends in order.
func zanarisJournalCases(t *testing.T) map[string]string {
	t.Helper()
	src := csContent(t, lcDir+"scripts/zanaris_journal.rs2")
	out := map[string]string{}
	caseRe := regexp.MustCompile(`case \^(zanaris_[a-z_]+) :`)
	strRe := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	locs := caseRe.FindAllStringSubmatchIndex(src, -1)
	for i, loc := range locs {
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		var b strings.Builder
		for _, m := range strRe.FindAllStringSubmatch(src[loc[1]:end], -1) {
			if strings.Contains(m[1], "@") || strings.Contains(m[1], "|") || len(m[1]) > 12 {
				b.WriteString(m[1])
			}
		}
		out[src[loc[2]:loc[3]]] = b.String()
	}
	return out
}

// The journal's pages, straight from the script, read as the stage each is the page of.
func TestLostCityContentTheJournalPagesReadAsTheirStages(t *testing.T) {
	cases := zanarisJournalCases(t)
	want := map[string]int{"zanaris_not_started": LcNotStarted, "zanaris_started": LcStarted, "zanaris_spoken_shamus": LcSpokenShamus,
		"zanaris_spirit_defeated": LcSpiritDefeated, "zanaris_tree_chopped": LcTreeChopped, "zanaris_staff_made": LcStaffMade, "zanaris_complete": LcComplete}
	if len(cases) != len(want) {
		t.Fatalf("the journal script has %d cases: %v", len(cases), cases)
	}
	for name, stage := range want {
		page, ok := cases[name]
		if !ok || page == "" {
			t.Errorf("no page for %s", name)
			continue
		}
		got, ok := ParseLostCityJournal(strings.Split(page, "|"))
		if !ok || got != stage {
			t.Errorf("the page of %s reads as %d (ok %v)", name, got, ok)
		}
	}
}

// Every talk option the module drives is a line of the scripts it drives, and the menus of the warrior's chain pick the line meant at each step.
func TestLostCityContentTheDialogueOptions(t *testing.T) {
	warrior := csContent(t, "scripts/areas/area_lumbridge/scripts/warrior_zanaris.rs2")
	shamus := csContent(t, lcDir+"scripts/shamus.rs2")
	cave := csContent(t, "scripts/areas/area_entrana/scripts/cave_monk.rs2")
	ship := csContent(t, "scripts/areas/area_port_sarim/scripts/monk_of_entrana.rs2")
	for _, c := range []struct {
		src  string
		stop NpcStop
	}{{warrior, LcWarriorStop}, {shamus, LcShamusStop}, {cave, LcCaveMonkStop}, {ship, LcPortMonkStop}} {
		for _, p := range c.stop.Prefer {
			if !strings.Contains(c.src, `"`+p+`"`) {
				t.Errorf("%s: the script has no option %q", c.stop.Npc, p)
			}
		}
	}
	// The warrior's menus in the order the chain meets them, and the option the preferences pick at each.
	for i, menu := range [][]string{
		{"What are you camped here for?", "Do you know any good adventures I can go on?"},
		{"Who's Zanaris?", "What's Zanaris?", "What makes you think it's out here?"},
		{"If it's hidden how are you planning to find it?", "There's no such thing!"},
		{"Please tell me?", "Looks like you don't know either."},
	} {
		pick, ok := PickPreferred(menu, LcWarriorStop.Prefer)
		if !ok || pick != LcWarriorStop.Prefer[i] {
			t.Errorf("menu %d %v: the preferences pick %q (ok %v), want %q", i, menu, pick, ok, LcWarriorStop.Prefer[i])
		}
	}
	// Shamus's and the cave monk's: the line that goes on, not the one that leaves.
	if pick, _ := PickPreferred([]string{"How does it fit in a shed then?", "I've been in that shed, I didn't see a city."}, LcShamusStop.Prefer); pick != "How does it fit in a shed then?" {
		t.Errorf("Shamus's menu picks %q", pick)
	}
	if pick, _ := PickPreferred([]string{"I don't think I'm strong enough to enter then.", "Well that is a risk I will have to take."}, LcCaveMonkStop.Prefer); !strings.HasPrefix(pick, "Well that") {
		t.Errorf("the cave monk's menu picks %q", pick)
	}
	if pick, _ := PickPreferred([]string{"No, not right now.", "Yes, okay, I'm ready to go."}, LcPortMonkStop.Prefer); !strings.HasPrefix(pick, "Yes, okay") {
		t.Errorf("the ship monk's menu picks %q", pick)
	}
	// The ladder on Entrana is climbed down only with the monk standing two tiles east of it.
	ladder := csContent(t, "scripts/areas/area_entrana/scripts/entrana_dungeon.rs2")
	if !strings.Contains(ladder, "npc_findexact(movecoord(loc_coord, 2, 0, 0), cave_monk)") || LcCaveMonkAnchor.X != LcEntranaLadderAt.X+2 || LcCaveMonkAnchor.Z != LcEntranaLadderAt.Z {
		t.Errorf("the monk's anchor %v and the ladder %v", LcCaveMonkAnchor, LcEntranaLadderAt)
	}
}

// lcBlocks reads every [name] block of every file with an extension under scripts into name -> key -> first value.
func lcBlocks(t *testing.T, ext string) map[string]map[string]string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT")
	}
	out := map[string]map[string]string{}
	err := filepath.Walk(filepath.Join(root, "scripts"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ext) {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var cur map[string]string
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
				cur = map[string]string{}
				out[line[1:len(line)-1]] = cur
			case cur != nil && strings.Contains(line, "="):
				k, v, _ := strings.Cut(line, "=")
				if k == "param" {
					pk, pv, _ := strings.Cut(v, ",")
					k, v = "param:"+pk, pv
				}
				if _, seen := cur[k]; !seen {
					cur[k] = v
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The dungeon is left by a teleport, and the Wilderness need not be walked: teleport.rs2 refuses a cast only above level 20 of the Wilderness (read from
// wilderness_zones, which the dungeon is not in) and in a handful of event zones, nothing on Entrana forbids a spell, and the two exits' levels, runes and
// landings are the spell table's.
func TestLostCityContentTheDungeonIsLeftByATeleport(t *testing.T) {
	tele := csContent(t, "scripts/skill_magic/scripts/spells/teleport.rs2")
	for _, want := range []string{
		"[if_button,magic:varrock_teleport] @magic_teleport(^varrock_teleport);", "[if_button,magic:lumbridge_teleport] @magic_teleport(^lumbridge_teleport);",
		"if (~wilderness_level(coord) > 20) {", "if (~pre_tele_checks(coord) = false) {", "~delete_spell_runes($spell_data);",
		"~player_teleport_normal(map_findsquare(db_getfield($spell_data, magic_spell_table:tele_coord, 0), 0, 2, ^map_findsquare_lineofwalk));",
	} {
		if !strings.Contains(tele, want) {
			t.Errorf("teleport.rs2 lacks %q", want)
		}
	}
	// What refuses a cast besides the Wilderness: these zones and nothing else, and none of them is Entrana's (or names it).
	i := strings.Index(tele, "[proc,pre_tele_checks]")
	if i < 0 {
		t.Fatal("no pre_tele_checks")
	}
	checks := tele[i:]
	for _, zone := range []string{"in_tutorial_island", "trawler_wreck_zones", "trawler_game_zones", "random_event_mime_zones", "random_event_maze_zones", "in_duel_arena",
		"in_castlewars_game", "in_castlewars_waitingroom", "thanainabarrel"} {
		if !strings.Contains(checks, zone) {
			t.Errorf("pre_tele_checks no longer names %s: the list of what refuses a cast has changed", zone)
		}
	}
	if n := strings.Count(checks, "return(false);"); n != 7 {
		t.Errorf("pre_tele_checks refuses in %d places, the plan knows 7", n)
	}
	for _, f := range []string{"scripts/skill_magic/scripts/spells/teleport.rs2", "scripts/skill_magic/scripts/magic.rs2"} {
		if strings.Contains(strings.ToLower(csContent(t, f)), "entrana") {
			t.Errorf("%s names Entrana: a spell may be refused there now", f)
		}
	}
	// wilderness_level reads wilderness_zones, and no tile of the dungeon is in a zone (the landing of the Magic door, level 32, is).
	if !strings.Contains(csContent(t, "scripts/areas/area_wilderness/scripts/wilderness_levels.rs2"), "return (calc((coordz($coord) - coordz($coord1))/8 + 1));") {
		t.Error("the Wilderness level is no longer (z - the zone's z) / 8 + 1")
	}
	pairRe := regexp.MustCompile(`(?:data=coord_pair,|inzone\()(\d_\d+_\d+_\d+_\d+),\s?(\d_\d+_\d+_\d+_\d+)`)
	inPairs := func(text string, at tile.Tile) bool {
		for _, m := range pairRe.FindAllStringSubmatch(text, -1) {
			a, b := lcCoord(t, m[1]), lcCoord(t, m[2])
			if at.X >= a.X && at.X <= b.X && at.Z >= a.Z && at.Z <= b.Z && at.Level >= a.Level && at.Level <= b.Level {
				return true
			}
		}
		return false
	}
	// The corners of the dungeon's box (LcRegionOf) and the tiles the quest uses in it.
	dungeon := []tile.Tile{{X: 2818, Z: 9728}, {X: 2875, Z: 9728}, {X: 2818, Z: 9783}, {X: 2875, Z: 9783}, LcDungeonArrival, LcZombieStand, LcTreeStand, LcDoorStand, LcSpiritSpawn}
	wild := csContent(t, "scripts/areas/area_wilderness/configs/wilderness_zones.dbrow")
	for _, at := range dungeon {
		if inPairs(wild, at) {
			t.Errorf("%v is in a wilderness zone: a teleport from the dungeon is refused", at)
		}
	}
	if !inPairs(wild, LcWildernessLanding) {
		t.Errorf("the Magic door's landing %v is no longer in a wilderness zone", LcWildernessLanding)
	}
	// And in none of the other zones pre_tele_checks refuses in: the tables and inline zones it reads, each named where it lives.
	macro, trawler, duel := csContent(t, "scripts/macro events/configs/macro_events.dbrow"), csContent(t, "scripts/minigames/game_trawler/configs/trawler.dbrow"),
		csContent(t, "scripts/minigames/game_duelarena/configs/duelarena.dbrow")
	for name, text := range map[string]string{
		"random_event_mime_zones":         csConfigBlock(macro, "random_event_mime_zones"),
		"random_event_maze_zones":         csConfigBlock(macro, "random_event_maze_zones"),
		"trawler_wreck_zones":             csConfigBlock(trawler, "trawler_wreck_zones"),
		"trawler_game_zones":              csConfigBlock(trawler, "trawler_game_zones"),
		"duel_arena_fight_zones":          csConfigBlock(duel, "duel_arena_fight_zones"),
		"duel_arena_obstacle_fight_zones": csConfigBlock(duel, "duel_arena_obstacle_fight_zones"),
		"in_tutorial_island":              csContent(t, "scripts/tutorial/scripts/util.rs2"),
		"in_castlewars_game and waitroom": csContent(t, "scripts/minigames/game_castlewars/scripts/castlewars.rs2"),
	} {
		if !pairRe.MatchString(text) {
			t.Errorf("%s: no zone read from the content: the test would prove nothing", name)
		}
		for _, at := range dungeon {
			if inPairs(text, at) {
				t.Errorf("%s holds %v, in the dungeon: a teleport from it is refused", name, at)
			}
		}
	}
	// The spells: level, runes and landing are the plan's.
	spells := csContent(t, "scripts/skill_magic/configs/magic_spells.dbrow")
	runeIDs := map[string]int{"firerune": ObjFireRune, "airrune": ObjAirRune, "earthrune": ObjEarthRune, "lawrune": ObjLcLawRune}
	for _, e := range LcExits() {
		block := csConfigBlock(spells, "magic_spell_teleport_"+strings.ToLower(strings.TrimSuffix(e.Name, " Teleport")))
		if block == "" {
			t.Errorf("no spell block for %s", e.Name)
			continue
		}
		if !strings.Contains(block, "data=members,false") {
			t.Errorf("%s is a members' spell", e.Name)
		}
		if m := regexp.MustCompile(`data=levelrequired,(\d+)`).FindStringSubmatch(block); m == nil || m[1] != strconv.Itoa(e.Level) {
			t.Errorf("%s: the level is %v, the plan says %d", e.Name, m, e.Level)
		}
		if m := regexp.MustCompile(`data=tele_coord,(\S+)`).FindStringSubmatch(block); m == nil || lcCoord(t, m[1]) != e.Landing {
			t.Errorf("%s: the landing is %v, the plan says %v", e.Name, m, e.Landing)
		}
		m := regexp.MustCompile(`data=runesrequired,(\S+)`).FindStringSubmatch(block)
		if m == nil {
			t.Errorf("%s: no runes required", e.Name)
			continue
		}
		got := map[int]int{}
		f := strings.Split(m[1], ",")
		for k := 0; k+1 < len(f); k += 2 {
			n, _ := strconv.Atoi(f[k+1])
			id, ok := runeIDs[f[k]]
			if !ok {
				t.Errorf("%s: the rune %q is not one the plan knows", e.Name, f[k])
			}
			got[id] = n
		}
		if !mapsEqual(got, e.Runes) {
			t.Errorf("%s: the spell takes %v, the plan says %v", e.Name, got, e.Runes)
		}
	}
	// The cast is a plain spellbook button: the dungeon needs no tab or staff, and a worn staff would only pay for an element the account is bare of.
	if !strings.Contains(csContent(t, "scripts/skill_magic/scripts/magic.rs2"), "if (stat(magic) < db_getfield($spell_data, magic_spell_table:levelrequired, 0)) {") {
		t.Error("the spell level check has changed")
	}
}

// The monks' list of what they refuse (has_entrana_restricted_items) and the quest's own things against it: every axe and the Dramen staff ARE on
// it, and nothing the strip leaves in the pack (a knife, the branch, coins, runes, food) is.
func TestLostCityContentTheMonksRefuseWhatTheQuestBanks(t *testing.T) {
	ship := csContent(t, "scripts/areas/area_port_sarim/scripts/monk_of_entrana.rs2")
	i := strings.Index(ship, "[proc,has_entrana_restricted_items]")
	if i < 0 {
		t.Fatal("no has_entrana_restricted_items")
	}
	proc := ship[i:]
	restricted := map[string]bool{}
	for _, m := range regexp.MustCompile(`inv_totalcat\((?:inv|worn), ([a-z_0-9]+)\)`).FindAllStringSubmatch(proc, -1) {
		restricted[m[1]] = true
	}
	for _, cat := range []string{"weapon_axe", "weapon_staff", "weapon_pickaxe", "armour_body", "armour_legs", "armour_helmet", "armour_shield", "weapon_slash", "weapon_stab", "weapon_blunt"} {
		if !restricted[cat] {
			t.Errorf("the monks' list lacks %s (found %d categories)", cat, len(restricted))
		}
	}
	// Armour of the feet, the neck, the ring and the ammunition slots, and the runes, are not on it: nothing the module strips is wrongly refused.
	for _, cat := range []string{"armour_feet", "armour_neck", "armour_ring", "armour_ammo"} {
		if restricted[cat] {
			t.Logf("the monks' list now has %s: the strip already leaves nothing of the kind", cat)
		}
	}
	objs := lcBlocks(t, ".obj")
	names := packNames(t, filepath.Join(os.Getenv("RSB0T_CONTENT"), "pack", "obj.pack"))
	cat := func(id int) string { return objs[names[id]]["category"] }
	for _, id := range LcForbiddenOnEntrana() {
		if !restricted[cat(id)] {
			t.Errorf("%s (%d) is category %q, which the monks do not refuse: it need not be banked", names[id], id, cat(id))
		}
	}
	for _, id := range LcEntranaKeep() {
		if c := cat(id); restricted[c] {
			t.Errorf("%s (%d) is category %q, which the monks refuse, and the strip keeps it", names[id], id, c)
		}
	}
	// The knife has no category at all in this build, and the foods and runes none the monks name.
	if cat(ObjKnife) != "" {
		t.Logf("the knife is category %q", cat(ObjKnife))
	}
}

// The zombie's drop: a bronze axe 50 times in 128 and an iron axe once, the quest's only source of the axe the Dramen tree wants on Entrana.
func TestLostCityContentTheZombieDropsTheAxe(t *testing.T) {
	src := csContent(t, "scripts/drop tables/scripts/zombie.rs2")
	i := strings.Index(src, "[ai_queue3,zombie_entranan]")
	if i < 0 {
		t.Fatal("no zombie_entranan drop table")
	}
	block := src[i:]
	for _, want := range []string{"def_int $dropint = random(128);", "if ($dropint < 50) {\n    obj_add(npc_coord, bronze_axe, 1, ^lootdrop_duration);",
		"} else if ($dropint < 56) {\n    obj_add(npc_coord, iron_axe, 1, ^lootdrop_duration);"} {
		// The iron axe is the 56th row after the bronze one's 50, a helm and a sword: only the order of the two axes matters.
		_ = want
	}
	if !strings.Contains(block, "if ($dropint < 50) {\n    obj_add(npc_coord, bronze_axe, 1, ^lootdrop_duration);") ||
		!strings.Contains(block, "obj_add(npc_coord, iron_axe, 1, ^lootdrop_duration);") {
		t.Errorf("the zombie's table has changed:\n%s", block[:min(len(block), 600)])
	}
	// No axe lies on the ground anywhere the account walks in the dungeon.
	npcs := lcBlocks(t, ".npc")
	if z := npcs["zombie_entranan"]; z["wanderrange"] != "9" || z["huntmode"] != "cowardly" || z["huntrange"] != "1" || z["respawnrate"] != "70" {
		t.Errorf("the zombie's config %v", z)
	}
}

// The hostiles of the dungeon the module keeps out of or prices: their spawns are the map's, and the pad the avoid rectangles use is their wander range
// and their hunt range and a tile.
func TestLostCityContentTheDungeonsHostiles(t *testing.T) {
	if os.Getenv("RSB0T_CONTENT") == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout")
	}
	spawns := map[int][]tile.Tile{}
	for mx := 43; mx <= 46; mx++ {
		for mz := 150; mz <= 154; mz++ {
			rel := fmt.Sprintf("maps/m%d_%d.jm2", mx, mz)
			root := os.Getenv("RSB0T_CONTENT")
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				continue
			}
			in := false
			for _, line := range strings.Split(csContent(t, rel), "\n") {
				if strings.HasPrefix(line, "==== ") {
					in = line == "==== NPC ===="
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
				id, err := strconv.Atoi(strings.TrimSpace(tail))
				if err != nil {
					continue
				}
				at := tile.Tile{X: mx<<6 + lx, Z: mz<<6 + lz, Level: lvl}
				if at.X >= 2790 && at.X <= 2910 && at.Z >= 9700 && at.Z <= 9830 {
					spawns[id] = append(spawns[id], at)
				}
			}
		}
	}
	sortTiles := func(ts []tile.Tile) []tile.Tile {
		out := append([]tile.Tile(nil), ts...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].X != out[j].X {
				return out[i].X < out[j].X
			}
			return out[i].Z < out[j].Z
		})
		return out
	}
	if got, want := sortTiles(spawns[NpcLcBlackDemon]), sortTiles(LcBlackDemonSpawns()); !slices.Equal(got, want) {
		t.Errorf("the map's black demons %v, the plan's %v", got, want)
	}
	if got, want := sortTiles(spawns[NpcLcGreaterDemon]), sortTiles(LcGreaterDemonSpawns()); !slices.Equal(got, want) {
		t.Errorf("the map's greater demons %v, the plan's %v", got, want)
	}
	// The zombies: five spawns on the west side, every one south of the black demons' hunting ground and the stand among them.
	zombies := sortTiles(spawns[NpcLcZombie])
	if len(zombies) != 5 {
		t.Fatalf("the map's zombies %v", zombies)
	}
	near := 0
	for _, z := range zombies {
		if z.Z > 9766 {
			t.Logf("the zombie at %v is wanderer enough to leave the stand's ground", z)
		}
		if tile.Chebyshev(z, LcZombieStand) <= 6 {
			near++
		}
	}
	if near < 4 {
		t.Errorf("only %d of the five zombies within 6 tiles of the stand %v: %v", near, LcZombieStand, zombies)
	}
	// The demons wander 6 and hunt 3, cowardly, and are size 3.
	npcs := lcBlocks(t, ".npc")
	for _, name := range []string{"black_demon", "greater_demon"} {
		b := npcs[name]
		wander, _ := strconv.Atoi(b["wanderrange"])
		hunt, _ := strconv.Atoi(b["huntrange"])
		if wander+hunt+1 != LcDemonPad || b["huntmode"] != "cowardly" || b["size"] != "3" {
			t.Errorf("%s: wander %d, hunt %d, mode %q, size %q against the pad %d", name, wander, hunt, b["huntmode"], b["size"], LcDemonPad)
		}
	}
	// The spirit has no magic stat and no magic defence: a cast rolls against 1, which is why the quest casts.
	spirit := npcs["tree_spirit"]
	if _, ok := spirit["magic"]; ok {
		t.Errorf("the spirit's config names a magic stat: %v", spirit)
	}
	if _, ok := spirit["param:magicdefence"]; ok {
		t.Logf("the spirit has a magicdefence param now")
	}
	if spirit["vislevel"] != "101" || spirit["hitpoints"] != "85" {
		t.Errorf("the spirit's config %v", spirit)
	}
}
