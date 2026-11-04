package questplan

// Every id, stage, menu, reward, spawn and rule Black Knights' Fortress rests on, read from a Content 289 checkout (RSB0T_CONTENT or
// RSB0T_CONTENT_DIR, skipped without one) and, for the ids, the production config archive (RSB0T_CONFIG_JAG), so that none of them can
// drift away from the scripts and maps they were taken from:
//
//	RSB0T_CONTENT=<Content checkout> RSB0T_CONFIG_JAG=<prod-config.jag> go test -run BlackKnights ./internal/scripts/questplan/

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
)

func bkContent(t *testing.T, rel string) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		root = os.Getenv("RSB0T_CONTENT_DIR")
	}
	if root == "" {
		t.Skip("set RSB0T_CONTENT (or RSB0T_CONTENT_DIR) to a Content checkout to check Black Knights' Fortress against it")
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Skipf("no %s under %s: %v", rel, root, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func bkPackIDs(t *testing.T, name string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, line := range strings.Split(bkContent(t, "pack/"+name+".pack"), "\n") {
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

// bkRenderJournal renders every stage of blackknight_journal.rs2 the way the script does: the strings of each `append` in order,
// a stage's own block ending at its `~quest_journal` and its `return`, so the lines of the stage-N block belong to stage N alone.
func bkRenderJournal(src string) map[int]string {
	out := map[int]string{}
	var acc []string
	blockStart, block := 0, -1
	appendRe := regexp.MustCompile(`append\(\$x, "((?:[^"\\]|\\.)*)"\)`)
	ifRe := regexp.MustCompile(`^if \(%spy = (\d+)\)`)
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue // the script keeps an older rendering of stage 1 as comments
		}
		if m := ifRe.FindStringSubmatch(line); m != nil {
			block, _ = strconv.Atoi(m[1])
			blockStart = len(acc)
			continue
		}
		if m := appendRe.FindStringSubmatch(line); m != nil {
			acc = append(acc, m[1])
			continue
		}
		if strings.HasPrefix(line, "~quest_journal(") {
			if block >= 0 {
				out[block] = strings.Join(acc, "")
				acc, block = acc[:blockStart], -1
			} else {
				out[4] = strings.Join(acc, "")
			}
		}
	}
	return out
}

func TestBlackKnightsJournalFixturesAreWhatTheScriptRenders(t *testing.T) {
	rendered := bkRenderJournal(bkContent(t, "scripts/quests/quest_blackknight/scripts/blackknight_journal.rs2"))
	if len(rendered) != 5 {
		t.Fatalf("rendered stages %v", rendered)
	}
	for stage, want := range rendered {
		// The script's `|` ends each of its lines, so a stage ends with one except the last; the fixtures are what the client shows.
		if got := bkJournalFixtures[stage]; got != want {
			t.Errorf("stage %d:\n fixture %q\n script  %q", stage, got, want)
		}
		if got, ok := ParseBlackKnightsFortressJournal([]string{want}); !ok || got != stage {
			t.Errorf("stage %d: parsed %d (ok %v) from the script's own text", stage, got, ok)
		}
	}
}

func TestBlackKnightsStagesRewardsAndGatesAreWhatTheScriptsSay(t *testing.T) {
	constants := bkContent(t, "scripts/general/configs/quest.constant")
	if !strings.Contains(constants, "^blackknight_complete = 4") || !strings.Contains(constants, "^blackknight_questpoints = 3") {
		t.Errorf("quest.constant: complete is not 4 or the reward is not 3 quest points")
	}
	if BKComplete != 4 || BlackKnightsFortressRecord.QuestPoints != 3 {
		t.Errorf("stage %d, %d quest points", BKComplete, BlackKnightsFortressRecord.QuestPoints)
	}
	varp := bkContent(t, "scripts/quests/quest_blackknight/configs/quest_blackknight.varp")
	if !strings.Contains(varp, "[spy]") || !strings.Contains(varp, "scope=perm") || strings.Contains(varp, "transmit=yes") {
		t.Errorf("the %%spy varp is not a permanent untransmitted one (the journal is the only reader): %q", varp)
	}
	main := bkContent(t, "scripts/quests/quest_blackknight/scripts/quest_blackknight.rs2")
	for _, want := range []string{
		"inv_add(inv, coins, 2500)",                              // the reward
		"[oplocu,blackknighthole]",                               // the cabbage on the hole
		"case cabbage : @black_knights_fortress_correct_cabbage", // the right cabbage
		"case magic_cabbage : @black_knights_fortress_incorrect_cabbage",
		"[oploc1,witchgrill]",
		"if (%spy = 1) {", "if (%spy = 2) {", // the grill needs stage 1, the hole stage 2
		"%spy = calc(%spy + 1);",
		"inv_getobj(worn, ^wearpos_hat) ! bronze_med_helm", "inv_getobj(worn, ^wearpos_torso) ! iron_chainbody", // the disguise
		"\"I don't care. I'm going in anyway.\"",
		"def_int $option = ~p_choice2(\"Ok I won't.\", 1,",
		"npc_find(0_47_54_8_56, fortressguard, 5, 1)",
		"[oploc1,bkfortressdoor1]", "[oploc1,bkfortressdoor2]", "[oploc1,bkfortressdoor3]", "[oploc1,bksecretdoor]",
		"~black_knights_aggro(coord, coord);", // door 3 from inside
	} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_blackknight.rs2 no longer contains %q", want)
		}
	}
	// The grill speaks nine times and the stage rises after the last: the module drives the dialogue to its close.
	grill := main[strings.Index(main, "[oploc1,witchgrill]"):]
	grill = grill[:strings.Index(grill, "[proc,update_blackknight_progress]")]
	if n := strings.Count(grill, "~chatnpc_specific("); n != 9 {
		t.Errorf("the grill has %d spoken pages, the notes say nine", n)
	}
	if strings.Index(grill, "~update_blackknight_progress;") < strings.LastIndex(grill, "~chatnpc_specific(") {
		t.Error("the stage no longer rises after the last page")
	}
	// The magic cabbage line quotes the witch's complaint the notes quote.
	if !strings.Contains(main, "Not helping her.") {
		t.Error("the wrong-cabbage message changed")
	}
	// The guard asks the question the hall door's module answers, and nothing in the entrance dialogue admits an account.
	if !strings.Contains(main, "Well this the guards entrance.") {
		t.Error("the entrance dialogue changed")
	}
	// The aggro helper: aggressive_black_knight within five tiles, set on the account.
	if !strings.Contains(main, "npc_findall($all_search_coord, aggressive_black_knight, 5, 0);") || !strings.Contains(main, "npc_setmode(opplayer2)") {
		t.Error("black_knights_aggro changed")
	}
	// Which doors move the account and which only open: the fortress's own doors and its secret walls call open_and_close_door (a teleport
	// through), the plain doors on level 1 (loc_1530, category door_closed) are the generic [oploc1,_door_closed] ~open_door(500).
	for _, want := range []string{"~open_and_close_door(loc_param(next_loc_stage), $is_outside, false);", "~open_and_close_door(loc_param(next_loc_stage), $is_inside, false);",
		"~open_and_close_door2(loc_param(next_loc_stage), ~check_axis_locactive(coord), coffin_open);"} {
		if !strings.Contains(main, want) {
			t.Errorf("quest_blackknight.rs2 no longer contains %q", want)
		}
	}
	doors := bkContent(t, "scripts/doors/configs/doors.loc")
	if b := csConfigBlock(doors, "loc_1530"); !strings.Contains(b, "category=door_closed") || !strings.Contains(b, "param=next_loc_stage,loc_1531") || !strings.Contains(b, "op1=Open") {
		t.Errorf("loc_1530: %q", b)
	}
	if !strings.Contains(bkContent(t, "scripts/doors/scripts/doors.rs2"), "[oploc1,_door_closed] ~open_door(500);") {
		t.Error("the plain door no longer opens for 500 ticks")
	}
	// The ladders that start it: the hall ladder's down side and the quest's cell.
	ladders := bkContent(t, "scripts/ladders+stairs/scripts/ladders.rs2")
	if !strings.Contains(ladders, "case 1_47_54_14_62 :") || !strings.Contains(ladders, "~black_knights_aggro(coord, 0_47_54_17_59);") {
		t.Error("the hall ladder's aggro changed")
	}
	amik := bkContent(t, "scripts/areas/area_falador/scripts/sir_amik_varze.rs2")
	for _, want := range []string{
		"if (%qp < 12) {", "~p_choice2(\"I seek a quest!\", 1, \"I don't, I'm just looking around.\", 2)",
		"~p_choice2(\"I laugh in the face of danger!\", 1, \"I go and cower in a corner at the first sign of danger!\", 2)",
		"~p_choice2(\"Oh, I suppose I'll give it a go then.\", 1, \"No, I'm not convinced.\", 2)",
		"case 1,2,3 : @black_knights_fortress_sir_amik_in_progress;",
		"queue(black_knights_fortress_quest_complete, 0, 0);",
		"} else if (%spy = 3) {",
	} {
		if !strings.Contains(amik, want) {
			t.Errorf("sir_amik_varze.rs2 no longer contains %q", want)
		}
	}
	if BKMinQuestPoints != 12 {
		t.Errorf("minimum %d", BKMinQuestPoints)
	}
	// Both menus' options are the stop's preferences, and the cowering branch is not.
	for _, p := range SirAmikStop.Prefer {
		if !strings.Contains(amik, "\""+p+"\"") {
			t.Errorf("Sir Amik has no option %q", p)
		}
	}
	// The journal's strings name the quest the way the tab does.
	if !strings.Contains(bkContent(t, "scripts/quests/quest_blackknight/scripts/blackknight_journal.rs2"), "~quest_journal(\"Black Knight's Fortress\"") {
		t.Error("the journal's title is not the tab's")
	}
}

func TestBlackKnightsFortressIDsMatchContentAndTheProductionConfig(t *testing.T) {
	npcs, objs, locs := bkPackIDs(t, "npc"), bkPackIDs(t, "obj"), bkPackIDs(t, "loc")
	for name, id := range map[string]int{"sir_amik_varze": NpcSirAmik, "fortressguard": NpcFortressGuard, "black_knight": NpcBlackKnight,
		"aggressive_black_knight": NpcAggressiveBlackKnight, "wayne": NpcWayne, "horvik_the_armourer": NpcHorvik, "peksa": NpcPeksa} {
		if npcs[name] != id {
			t.Errorf("npc %s is %d in content, the module says %d", name, npcs[name], id)
		}
	}
	for name, id := range map[string]int{"iron_chainbody": ObjIronChainbody, "bronze_med_helm": ObjBronzeMedHelm, "cabbage": ObjCabbage,
		"magic_cabbage": ObjMagicCabbage, "shark": ObjShark, "bass": ObjBass, "coins": CoinsID} {
		if objs[name] != id {
			t.Errorf("obj %s is %d in content, the module says %d", name, objs[name], id)
		}
	}
	for name, id := range map[string]int{"bkfortressdoor1": LocBKFrontDoor, "bkfortressdoor2": LocBKHallDoor, "bkfortressdoor3": LocBKSturdyDoor,
		"bksecretdoor": LocBKSecretWall, "witchgrill": LocBKGrill, "blackknighthole": LocBKHole, "cabbage": LocCabbage,
		"laddertop": LocBKLadderTop, "ladder": LocBKLadder, "laddertop_directional": LocBKLadderTopD, "ladder_directional": LocBKLadderD} {
		if locs[name] != id {
			t.Errorf("loc %s is %d in content, the module says %d", name, locs[name], id)
		}
	}
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Log("RSB0T_CONFIG_JAG not set: the production config half is skipped")
		return
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hasOp := func(ops [5]string, op string) bool { return cachecfg.OpIndex(ops, op) > 0 }
	for id, want := range map[int]string{ObjIronChainbody: "Iron chainbody", ObjBronzeMedHelm: "Bronze med helm", ObjCabbage: "Cabbage",
		ObjMagicCabbage: "Cabbage", ObjShark: "Shark", ObjBass: "Bass", CoinsID: "Coins"} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the module says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	if o, _ := cfg.Obj(ObjIronChainbody); !hasOp(o.IOps, "Wear") || o.Stackable {
		t.Errorf("the iron chainbody offers %v", o.IOps)
	}
	if o, _ := cfg.Obj(ObjBronzeMedHelm); !hasOp(o.IOps, "Wear") || o.Stackable {
		t.Errorf("the bronze med helm offers %v", o.IOps)
	}
	if o, _ := cfg.Obj(ObjCabbage); o.Stackable {
		t.Error("a cabbage stacks")
	}
	for id, c := range map[int]struct {
		name string
		op   string
		lvl  int
	}{NpcSirAmik: {"Sir Amik Varze", "Talk-to", -1}, NpcFortressGuard: {"Fortress Guard", "Talk-to", 20}, NpcBlackKnight: {"Black Knight", "Attack", 33},
		NpcAggressiveBlackKnight: {"Black Knight", "Attack", 33}, NpcWayne: {"Wayne", "Trade", -1}, NpcHorvik: {"Horvik", "Trade", -1},
		NpcPeksa: {"Peksa", "Trade", -1}} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != c.name || !hasOp(n.Ops, c.op) {
			t.Errorf("npc %d is %q %v (found %v), the module says %q with %q", id, n.Name, n.Ops, ok, c.name, c.op)
		}
		if c.lvl > 0 && n.Level != c.lvl {
			t.Errorf("npc %d is level %d, want %d", id, n.Level, c.lvl)
		}
	}
	// Sir Amik is the NPC the stop names, and the stop's menus are the module's: no attack option anywhere on him.
	if n, _ := cfg.Npc(NpcSirAmik); hasOp(n.Ops, "Attack") {
		t.Errorf("Sir Amik offers %v", n.Ops)
	}
}

func TestBlackKnightsFortressShopsStockTheDisguise(t *testing.T) {
	for _, c := range []struct {
		file, section string
		item          string
	}{
		{"scripts/areas/area_falador/configs/falador.inv", "[chainshop]", "iron_chainbody"},
		{"scripts/areas/area_varrock/configs/varrock.inv", "[armourshop]", "iron_chainbody"},
		{"scripts/areas/area_barbarian_village/configs/barbarian_village.inv", "[helmetshop]", "bronze_med_helm"},
	} {
		src := bkContent(t, c.file)
		i := strings.Index(src, c.section)
		if i < 0 {
			// Wayne's shop section is named after the file's comment (Wayne's Chains): find the section holding the item.
			i = strings.Index(src, "stock2="+c.item)
			if i < 0 {
				t.Errorf("%s: no section stocks %s", c.file, c.item)
				continue
			}
			i = strings.LastIndex(src[:i], "\n[")
		}
		rest := src[i+1:]
		if j := strings.Index(rest, "\n["); j > 0 {
			rest = rest[:j]
		}
		if !strings.Contains(rest, c.item+",") {
			t.Errorf("%s %s: %s is not in stock", c.file, c.section, c.item)
		}
	}
	// The cabbage field: ordinary cabbages, and Wydin's shelf as the alternative the module does not use.
	if !strings.Contains(bkContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.inv"), "stock4=cabbage,3,100") {
		t.Error("Wydin no longer sells cabbages")
	}
	pick := bkContent(t, "scripts/general_use/scripts/pickables.rs2")
	if !strings.Contains(pick, "[oploc2,cabbage]") || !strings.Contains(pick, "~pickup_loc_floor(loc_coord, cabbage, 70, false);") {
		t.Error("the cabbage pick changed")
	}
}

// The black knight's stats in the model are the content's: attack, defence, strength 25, 42 hitpoints, attack rate 5, attack bonus 18,
// strength bonus 16, huntmode cowardly with huntrange 5.
func TestFoeBlackKnightIsWhatContentSays(t *testing.T) {
	src := bkContent(t, "scripts/_unpack/225/all.npc")
	block := csConfigBlock(src, "aggressive_black_knight")
	if block == "" {
		t.Fatal("no [aggressive_black_knight] in all.npc")
	}
	num := func(key string) int {
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == key {
				n, _ := strconv.Atoi(v)
				return n
			}
		}
		return -1
	}
	param := func(name string) int {
		for _, line := range strings.Split(block, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "param="+name+","); ok {
				n, _ := strconv.Atoi(rest)
				return n
			}
		}
		return -1
	}
	f := FoeBlackKnight
	if num("attack") != f.Attack || num("strength") != f.Strength || num("defence") != f.Defence || num("hitpoints") != f.Hitpoints {
		t.Errorf("stats %d/%d/%d/%d, model %d/%d/%d/%d", num("attack"), num("strength"), num("defence"), num("hitpoints"), f.Attack, f.Strength, f.Defence, f.Hitpoints)
	}
	if param("attackrate") != f.AttackTicks || param("attackbonus") != f.AttackBonus || param("strengthbonus") != f.StrengthBonus {
		t.Errorf("params attackrate %d attackbonus %d strengthbonus %d, model %d %d %d", param("attackrate"), param("attackbonus"), param("strengthbonus"),
			f.AttackTicks, f.AttackBonus, f.StrengthBonus)
	}
	if def := min(param("stabdefence"), param("slashdefence"), param("crushdefence")); def != f.DefenceBonus {
		t.Errorf("lowest defence bonus %d, model %d", def, f.DefenceBonus)
	}
	if !strings.Contains(block, "huntmode=cowardly") || num("huntrange") != 5 || num("vislevel") != 33 || BKHuntCeiling != 2*num("vislevel") {
		t.Errorf("hunt: %q huntrange %d vislevel %d ceiling %d", block, num("huntrange"), num("vislevel"), BKHuntCeiling)
	}
	if f.MaxHit() != 4 || f.PeakHit() != 4 {
		t.Errorf("max hit %d", f.MaxHit())
	}
	// Cowardly hunters leave a stronger account alone outside the Wilderness (the hunt config), 2 * the level: the engine's rule.
	hunt := bkContent(t, "scripts/_unpack/225/all.hunt")
	if b := csConfigBlock(hunt, "cowardly"); !strings.Contains(b, "check_nottoostrong=outside_wilderness") {
		t.Errorf("cowardly hunt: %q", b)
	}
}
