package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, roll, reward and rule the Tourist Trap rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped without
// one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run TouristTrapContent ./internal/scripts/questplan/
//
// (What the production loc index and config hold at these ids and tiles, and each loc's stands, are pinned in the questmods real-data tests.)

const ttDir = "scripts/quests/quest_desertrescue/"

// ttScripts is the text of the quest's rs2 files, joined.
func ttScripts(t *testing.T, names ...string) string {
	t.Helper()
	var b strings.Builder
	for _, n := range names {
		b.WriteString(csContent(t, ttDir+"scripts/"+n+".rs2"))
		b.WriteString("\n")
	}
	return b.String()
}

// ttCoord reads a content coordinate "level_mx_mz_x_z" as a tile.
func ttCoord(t *testing.T, s string) tile.Tile {
	t.Helper()
	p := strings.Split(strings.TrimSpace(s), "_")
	if len(p) != 5 {
		t.Fatalf("coordinate %q", s)
	}
	n := make([]int, 5)
	for i, f := range p {
		v, err := strconv.Atoi(f)
		if err != nil {
			t.Fatalf("coordinate %q: %v", s, err)
		}
		n[i] = v
	}
	return tile.Tile{Level: n[0], X: n[1]*64 + n[3], Z: n[2]*64 + n[4]}
}

var ttPairLine = regexp.MustCompile(`^data=coord_pair,(\S+),(\S+)\s*$`)

// ttPairs reads one table of the desert dbrow as rectangles.
func ttPairs(t *testing.T, table string) []Rect {
	t.Helper()
	src := csContent(t, "scripts/areas/area_desert/configs/desert_heat_zones.dbrow")
	block := csConfigBlock(src, table)
	if block == "" {
		t.Fatalf("no [%s] in desert_heat_zones.dbrow", table)
	}
	var out []Rect
	for _, line := range strings.Split(block, "\n") {
		m := ttPairLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		a, b := ttCoord(t, m[1]), ttCoord(t, m[2])
		out = append(out, Rect{MinX: a.X, MaxX: b.X, MinZ: a.Z, MaxZ: b.Z})
	}
	if len(out) == 0 {
		t.Fatalf("[%s] holds no rectangle", table)
	}
	return out
}

// The pack ids the plan carries are the pack's.
func TestTouristTrapContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "water_skin4", ObjTtSkin4}, {"obj", obj, "water_skin3", ObjTtSkin3}, {"obj", obj, "water_skin2", ObjTtSkin2}, {"obj", obj, "water_skin1", ObjTtSkin1},
		{"obj", obj, "water_skin0", ObjTtSkin0}, {"obj", obj, "desert_shirt", ObjTtShirt}, {"obj", obj, "desert_robe", ObjTtRobe}, {"obj", obj, "desert_boots", ObjTtBoots},
		{"obj", obj, "metal_key", ObjTtMetalKey}, {"obj", obj, "thcelldoorkey", ObjTtCellKey}, {"obj", obj, "thminebarrel_empty", ObjTtBarrel},
		{"obj", obj, "thanainabarrel", ObjTtAnaBarrel}, {"obj", obj, "thgoodminekey", ObjTtGoodMineKey}, {"obj", obj, "slave_shirt", ObjTtSlaveShirt},
		{"obj", obj, "slave_robe", ObjTtSlaveRobe}, {"obj", obj, "slave_boots", ObjTtSlaveBoots}, {"obj", obj, "thshantaydisc", ObjTtDisclaimer},
		{"obj", obj, "thprotodart", ObjTtDart}, {"obj", obj, "thcaptplans", ObjTtPlans}, {"obj", obj, "tentipineapple", ObjTtPineapple},
		{"obj", obj, "thbedobinkey", ObjTtBedobinKey}, {"obj", obj, "thprotodarttip", ObjTtDartTip}, {"obj", obj, "shantay_pass", ObjTtPass},
		{"obj", obj, "thpunishrock", ObjTtPunishRock}, {"obj", obj, "feather", ObjTtFeather}, {"obj", obj, "bronze_dart", ObjTtBronzeDart},
		{"obj", obj, "kebab", ObjTtKebab}, {"obj", obj, "bronze_pickaxe", ObjTtBronzePickax},
		{"npc", npc, "ana", NpcTtAna}, {"npc", npc, "mining_slave_male", NpcTtSlave}, {"npc", npc, "escaped_slave", NpcTtEscaped}, {"npc", npc, "slave_rowdy", NpcTtRowdySlave},
		{"npc", npc, "desertminingmercenary1", NpcTtMerc}, {"npc", npc, "desertminingmercenary2", NpcTtCaveGuard}, {"npc", npc, "desertminingcaptain", NpcTtCaptain},
		{"npc", npc, "capt_siad", NpcTtSiad}, {"npc", npc, "al_shabim", NpcTtAlShabim}, {"npc", npc, "bedabin", NpcTtBedabin}, {"npc", npc, "bedabin_guard", NpcTtBedabinGrd},
		{"npc", npc, "irena", NpcTtIrena}, {"npc", npc, "mining_cart_driver", NpcTtDriver}, {"npc", npc, "rowdymineguard", NpcTtRowdyGuard},
		{"npc", npc, "desert_wolf", NpcTtDesertWolf}, {"npc", npc, "ugthanki", NpcTtUgthanki},
		{"loc", loc, "shantay_pass_henge_doorway", LocTtPass}, {"loc", loc, "miningcampgateclosedl", LocTtGateSouth}, {"loc", loc, "miningcampgateclosedr", LocTtGateNorth},
		{"loc", loc, "thttmineentrancel", LocTtMineDoorL}, {"loc", loc, "thttmineentrancer", LocTtMineDoorR}, {"loc", loc, "thttmineexitl", LocTtExitDoorL},
		{"loc", loc, "thttmineexitr", LocTtExitDoorR}, {"loc", loc, "thminecavel", LocTtCaveIn}, {"loc", loc, "thminecaver", LocTtCaveOut},
		{"loc", loc, "ladder", LocTtLadderUp}, {"loc", loc, "laddertop", LocTtLadderDown}, {"loc", loc, "capt_siad_bookcase", LocTtBookcase},
		{"loc", loc, "loc_2679", LocTtDesk}, {"loc", loc, "captain_siads_chest_closed", LocTtChest}, {"loc", loc, "cell_escape_window", LocTtWindow},
		{"loc", loc, "ttcellclimbrock1", LocTtRock1}, {"loc", loc, "ttcellclimbrock2", LocTtRock2}, {"loc", loc, "ttcellclimbrock3", LocTtRock3},
		{"loc", loc, "ttcellclimbrock4", LocTtRock4}, {"loc", loc, "ropepullthingy", LocTtWinch}, {"loc", loc, "ropepullthingy2", LocTtLiftBucket},
		{"loc", loc, "thminebarrel_full", LocTtFullBarrel}, {"loc", loc, "thminebarrel_empty", LocTtEmptyBarrel}, {"loc", loc, "loc_2682", LocTtSurfaceCart},
		{"loc", loc, "touristtrap_minecart", LocTtMineCart}, {"loc", loc, "undergroundcellgatel", LocTtCellGateL}, {"loc", loc, "undergroundcellgater", LocTtCellGateR},
		{"loc", loc, "punishrocks", LocTtPunishRock}, {"loc", loc, "experimental_anvil", LocTtAnvil}, {"loc", loc, "bedabin_tentdoor", LocTtTentDoor},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the quest points and the rewards.
func TestTouristTrapContentStagesAndRewards(t *testing.T) {
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^desertrescue_complete = %d", TtComplete), "^desertrescue_questpoints = 2"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	if TouristTrapRecord.QuestPoints != 2 {
		t.Errorf("the record says %d quest points, the content two", TouristTrapRecord.QuestPoints)
	}
	consts := csContent(t, ttDir+"configs/desertrescue.constant")
	for name, v := range map[string]int{"not_started": TtNotStarted, "started": TtStarted, "approached_captain": TtApproached, "killed_capt": TtKilledCapt,
		"entered_camp": TtEnteredCmp, "spoken_slave": TtSpokenSlv, "freed_slave": TtFreedSlave, "traded_clothes": TtTraded, "entered_mine": TtEnteredMin,
		"finding_pineapple": TtFindingPin, "given_bedobin_key": TtBedobinKey, "retrieved_plans": TtGotPlans, "shown_plans_shabim": TtShownPlans,
		"made_dart_tip": TtMadeTip, "finished_dart": TtMadeDart, "learned_darts": TtLearned, "given_pineapple": TtGivenPine, "used_mine_cart": TtUsedCart,
		"caught_ana": TtRescue, "escaped": TtRescue + 7, "saved_ana": TtReward, "picked_second_skill": TtReward + 2} {
		if want := fmt.Sprintf("^desertrescue_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("desertrescue.constant lacks %q", want)
		}
	}
	// The hidden stages: the rescue's seven steps all read the same page, and the reward's three too.
	for i, name := range []string{"caught_ana", "ana_minecart", "retrieved_ana_minecart", "ana_lift", "retrieved_ana_lift", "retrieved_ana_liftbarrel", "ana_on_mining_cart", "escaped"} {
		if want := fmt.Sprintf("^desertrescue_%s = %d", name, TtRescue+i); !strings.Contains(consts, want) {
			t.Errorf("desertrescue.constant lacks %q", want)
		}
	}
	// The reward: two picks of 4,650 experience (46500 in tenths), the four skills offered, the second pick last and the completion queue.
	irena := ttScripts(t, "irena")
	for _, want := range []string{
		`~p_choice4("Fletching.", 1, "Agility.", 2, "Smithing.", 3, "Thieving", 4)`, "stat_advance(thieving, 46500);", "stat_advance(fletching, 46500);",
		"stat_advance(agility, 46500);", "stat_advance(smithing, 46500);", "%desertrescue = calc(%desertrescue + 1);", "queue(desertrescue_complete, 0, 0);",
		"%desertrescue = ^desertrescue_started;",
	} {
		if !strings.Contains(irena, want) {
			t.Errorf("irena.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(TtRewardPick, "Thieving") || TtRewardPick != "Thieving" {
		t.Errorf("the reward pick is %q", TtRewardPick)
	}
	quests := ttScripts(t, "quest_desertrescue")
	for _, want := range []string{"[queue,desertrescue_complete]", "%desertrescue = ^desertrescue_complete;", "^desertrescue_questpoints"} {
		if !strings.Contains(quests, want) {
			t.Errorf("quest_desertrescue.rs2 lacks %q", want)
		}
	}
	// The requirements: Fletching 10 and Smithing 20 are the journal's own lines; the quest's start is Irena's alone.
	journal := ttScripts(t, "desertrescue_journal")
	for _, want := range []string{"if (stat_base(fletching) < 10) {", "if (stat_base(smithing) < 20) {"} {
		if !strings.Contains(journal, want) {
			t.Errorf("desertrescue_journal.rs2 lacks %q", want)
		}
	}
	if TtSmithing != 20 || TtFletching != 10 {
		t.Errorf("the plan's levels are Smithing %d and Fletching %d", TtSmithing, TtFletching)
	}
	have := map[string]int{}
	for _, r := range TouristTrapRecord.Requirements.Skills {
		have[r.Skill] = r.Level
	}
	if have["smithing"] != 20 || have["fletching"] != 10 || len(have) != 2 {
		t.Errorf("the record's requirements are %v, the content's Smithing 20 and Fletching 10", have)
	}
}

// The journal's needles are the script's: every one is in the text a page can hold, and the order the parser tries them in is the order the
// script prints the paragraphs (the page keeps every earlier paragraph, so the newest stage's needle is the one that comes last).
func TestTouristTrapContentJournalNeedlesAreTheScripts(t *testing.T) {
	src := ttScripts(t, "desertrescue_journal")
	// The tags and the line breaks of a rendered page.
	page := regexp.MustCompile(`@[a-z]{3}@`).ReplaceAllString(strings.ToLower(src), "")
	page = strings.ReplaceAll(strings.ReplaceAll(page, "|", " "), "  ", " ")
	last := len(page) + 1
	lastStage := TtComplete + 1
	for _, m := range ttMarkers {
		first := -1
		for _, n := range m.all {
			i := strings.Index(page, n)
			if i < 0 {
				t.Errorf("stage %d: the journal script never prints %q", m.stage, n)
				continue
			}
			if first < 0 || i < first {
				first = i
			}
		}
		if m.stage == lastStage {
			// The two spellings of stage 16 sit in one paragraph pair: the second is printed after the first.
			continue
		}
		if first >= 0 && first > last {
			t.Errorf("stage %d: its needles are printed after the next stage's paragraph", m.stage)
		}
		if first >= 0 {
			last = first
		}
		lastStage = m.stage
	}
	// Every stage the journal tells apart parses back to itself from a page that holds the paragraphs up to it.
	for _, m := range ttMarkers {
		var page []string
		for _, n := range m.all {
			page = append(page, n)
		}
		if got, ok := ParseTouristTrapJournal(page); !ok || got != m.stage {
			// A later marker's needles may be a subset of an earlier one's text; the order of ttMarkers is what resolves them.
			if got < m.stage {
				t.Errorf("a page holding only stage %d's needles parses as %d (%v)", m.stage, got, ok)
			}
		}
	}
}

// Every menu choice the plan prefers is in the script of the npc it talks to, and the ones whose order matters come in that order.
func TestTouristTrapContentTheTalks(t *testing.T) {
	for _, c := range []struct {
		stop    NpcStop
		scripts []string
	}{
		{TtIrenaStartStop, []string{"irena"}}, {TtCaptainStop, []string{"mercenary_captain"}}, {TtSlaveStop, []string{"mining_slave"}},
		{TtGuardStop, []string{"camp_guard"}}, {TtAlDealStop, []string{"al_shabim"}}, {TtAlKeyStop, []string{"al_shabim"}}, {TtAlPineappleStop, []string{"al_shabim"}},
		{TtSiadStop, []string{"captain_siad"}}, {TtDriverStop, []string{"mine_cart_driver"}},
	} {
		src := ttScripts(t, c.scripts...)
		for _, line := range c.stop.Prefer {
			if !strings.Contains(src, `"`+line+`"`) {
				t.Errorf("%s: no option %q in %v", c.stop.Npc, line, c.scripts)
			}
		}
	}
	all := ttScripts(t, "mine_cart_driver", "ana", "mercenary", "camp_guard")
	for _, line := range append(append([]string{}, TtLiftGuardAnswers...), TtDriverRiotAnswers...) {
		if !strings.Contains(all, `"`+line+`"`) {
			t.Errorf("no option %q in the driver's, Ana's or the guards' scripts", line)
		}
	}
	// Irena's and the captain's menus come in the order the plan lists them.
	for _, c := range []struct {
		name  string
		stop  NpcStop
		files string
	}{{"irena.rs2", TtIrenaStartStop, "irena"}, {"mercenary_captain.rs2", TtCaptainStop, "mercenary_captain"}} {
		src := ttScripts(t, c.files)
		last := -1
		for _, line := range c.stop.Prefer {
			i := strings.Index(src, `"`+line+`"`)
			if i < 0 {
				continue
			}
			if i < last {
				t.Errorf("%s: option %q comes before the previous preference in the script", c.name, line)
			}
			last = i
		}
	}
	// The captain: the taunt ends in his retaliation, and the cold attack is the one that has the guards throw the account out.
	capt := ttScripts(t, "mercenary_captain")
	for _, want := range []string{"npc_retaliate", "Very well, if you're challenging me, let's get on with it!", "defeat_merc_capt"} {
		if !strings.Contains(capt, want) {
			t.Errorf("mercenary_captain.rs2 lacks %q", want)
		}
	}
	// The bribe: a hundred coins, taken when the chat ends, and the ready bit set after the dialogue.
	driver := ttScripts(t, "mine_cart_driver")
	for _, want := range []string{"if(inv_total(inv, coins) < 100) {", "inv_del(inv, coins, 100);", "%desertrescue_map_mechanisms = setbit(%desertrescue_map_mechanisms, ^desertrescue_ready_rescue);"} {
		if !strings.Contains(driver, want) {
			t.Errorf("mine_cart_driver.rs2 lacks %q", want)
		}
	}
}

// The rolls: the lock, the window, the rocks, the anvil, the dart and the barrel.
func TestTouristTrapContentTheRolls(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{"mining_slave", "stat_random(thieving, 20, 250)"}, {"quest_desertrescue", "stat_random(strength, 100, 250)"}, {"quest_desertrescue", "stat_random(agility, 150, 250)"},
		{"quest_desertrescue", "stat_random(smithing, 61, 245)"}, {"quest_desertrescue", "stat_random(fletching, 61, 254)"}, {"quest_desertrescue", "stat_random(agility, 100, 250)"},
	} {
		if !strings.Contains(ttScripts(t, c.file), c.want) {
			t.Errorf("%s.rs2 lacks %q", c.file, c.want)
		}
	}
	// The anvil and the dart: the bar and the hammer, the ten feathers and the levels.
	q := ttScripts(t, "quest_desertrescue")
	for _, want := range []string{"[oploc1,experimental_anvil]", "[opheldu,thprotodarttip]", "[oploc1,thminebarrel_empty]", "[oploc1,touristtrap_minecart]", "[oploc1,ropepullthingy]"} {
		if !strings.Contains(q, want) {
			t.Errorf("quest_desertrescue.rs2 lacks %q", want)
		}
	}
}

// The heat: the zones of the dbrow are the plan's rectangles, the camps that switch it off are the plan's, and the timer's numbers are the plan's.
func TestTouristTrapContentTheHeat(t *testing.T) {
	same := func(what string, got, want []Rect) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: the dbrow has %d rectangles, the plan %d", what, len(got), len(want))
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s %d: the dbrow has %+v, the plan %+v", what, i, got[i], want[i])
			}
		}
	}
	same("desert_zones", ttPairs(t, "desert_zones"), desertRects)
	// The disabled zones: the plan lists the Bedabin camp's three and the mining camp's one in the dbrow's order.
	same("desertheat_disabled_zones", ttPairs(t, "desertheat_disabled_zones"), heatFreeRects)
	heat := csContent(t, "scripts/areas/area_desert/scripts/desert_heat.rs2")
	for _, want := range []string{
		"def_int $time = 150;", "$time = calc($time + 10);", "$time = calc($time + 20);", "inv_changeslot(inv, water_skin1, water_skin0, 1);",
		"inv_changeslot(inv, water_skin2, water_skin1, 1);", "inv_changeslot(inv, water_skin3, water_skin2, 1);", "inv_changeslot(inv, water_skin4, water_skin3, 1);",
		"queue(damage_player, 0, ~random_range(1, 10));", "if(~inzone_coord_pair_table(desertheat_disabled_zones, coord) = true) {",
	} {
		if !strings.Contains(heat, want) {
			t.Errorf("desert_heat.rs2 lacks %q", want)
		}
	}
	// The water the plan asks for is whole skins: three full skins are twelve doses, and a skin holds four.
	if TtWaterEntry != 4*TtSkins {
		t.Errorf("the plan enters the desert with %d doses and asks for %d skins", TtWaterEntry, TtSkins)
	}
	// The underground is not desert at all.
	if TtInDesertZone(tile.Tile{X: 3290, Z: 9420}) || TtHeat(TtAnaAt) {
		t.Error("the underground is in a desert zone")
	}
	// The mining camp and the Bedabin camp are desert zones with the heat off; the captain's post, the gate and the cart's landing are on.
	for _, c := range []struct {
		at   tile.Tile
		heat bool
	}{{TtGateIn, false}, {TtAlAt, false}, {TtGateOut, true}, {TtCaptainAt, true}, {TtCartLanding, true}, {TtIrena, true}, {TtTentOutside, false}} {
		if TtHeat(c.at) != c.heat {
			t.Errorf("heat at %v is %v, content says %v", c.at, TtHeat(c.at), c.heat)
		}
	}
}

// The spawns: every npc and loc tile of the plan is a spawn of the maps.
func TestTouristTrapContentTheSpawns(t *testing.T) {
	npc := func(sx, sz int) []jm2Entry { return jm2Entries(t, sx, sz, "NPC") }
	loc := func(sx, sz int) []jm2Entry { return jm2Entries(t, sx, sz, "LOC") }
	n4747, n4748, n4947, n51147 := npc(51, 47), npc(51, 48), npc(49, 47), npc(51, 147)
	for _, c := range []struct {
		what string
		es   []jm2Entry
		id   int
		at   tile.Tile
	}{
		{"Irena", n4748, NpcTtIrena, TtIrena}, {"the captain", n4747, NpcTtCaptain, TtCaptainAt}, {"the male slave", n4747, NpcTtSlave, TtSlaveAt},
		{"the driver", n4747, NpcTtDriver, TtDriverAt}, {"Captain Siad", n4747, NpcTtSiad, TtSiadAt}, {"Al Shabim", n4947, NpcTtAlShabim, TtAlAt},
		{"Ana", n51147, NpcTtAna, TtAnaAt}, {"a cave guard", n51147, NpcTtCaveGuard, tile.Tile{X: 3279, Z: 9414}}, {"a cave guard", n51147, NpcTtCaveGuard, tile.Tile{X: 3279, Z: 9416}},
		{"the Bedabin guard", n4947, NpcTtBedabinGrd, tile.Tile{X: 3170, Z: 3045}},
	} {
		if !jm2Has(c.es, c.id, c.at) {
			t.Errorf("%s: no npc %d at %v on the map", c.what, c.id, c.at)
		}
	}
	// The rowdy guards stand exactly where the plan keeps out of.
	var posts []tile.Tile
	for _, es := range [][]jm2Entry{n4747, n51147} {
		for _, e := range es {
			if e.ID == NpcTtRowdyGuard {
				posts = append(posts, tile.Tile{X: e.X, Z: e.Z, Level: e.Level})
			}
		}
	}
	if len(posts) != len(TtRowdyPosts) {
		t.Errorf("%d rowdy guards on the maps, %d posts in the plan", len(posts), len(TtRowdyPosts))
	}
	for _, p := range TtRowdyPosts {
		found := false
		for _, q := range posts {
			found = found || p == q
		}
		if !found {
			t.Errorf("the plan's rowdy post %v is no spawn of npc %d", p, NpcTtRowdyGuard)
		}
	}
	l4747, l51147, l4947, l4848 := loc(51, 47), loc(51, 147), loc(49, 47), loc(51, 48)
	for _, c := range []struct {
		what string
		es   []jm2Entry
		id   int
		at   tile.Tile
	}{
		{"the pass", l4848, LocTtPass, tile.Tile{X: 3302, Z: 3116}},
		{"the gate (south)", l4747, LocTtGateSouth, TtGateOut}, {"the gate (north)", l4747, LocTtGateNorth, tile.Tile{X: 3273, Z: 3028}},
		{"the mine door", l4747, LocTtMineDoorL, tile.Tile{X: 3301, Z: 3036}}, {"the mine door (right)", l4747, LocTtMineDoorR, tile.Tile{X: 3302, Z: 3036}},
		{"the exit door", l51147, LocTtExitDoorL, tile.Tile{X: 3278, Z: 9426}}, {"the exit door (right)", l51147, LocTtExitDoorR, tile.Tile{X: 3279, Z: 9426}},
		{"the cave (hall side)", l51147, LocTtCaveIn, tile.Tile{X: 3281, Z: 9415}}, {"the cave (lower side)", l51147, LocTtCaveOut, tile.Tile{X: 3283, Z: 9415}},
		{"the ladder up", l4747, LocTtLadderUp, TtLadder}, {"the ladder down", l4747, LocTtLadderDown, tile.Tile{X: 3290, Z: 3036, Level: 1}},
		{"the bookcase", l4747, LocTtBookcase, TtBookcase}, {"the desk", l4747, LocTtDesk, TtDesk}, {"the chest", l4747, LocTtChest, TtChest},
		{"the window", l4747, LocTtWindow, TtWindowAt}, {"the winch", l4747, LocTtWinch, TtWinchAt}, {"the full barrel", l4747, LocTtFullBarrel, TtTopBarrel},
		{"the surface cart", l4747, LocTtSurfaceCart, TtSurfaceCart}, {"the winch bucket", l51147, LocTtLiftBucket, TtLiftAt},
		{"the lower barrel", l51147, LocTtEmptyBarrel, TtLowerBarrel}, {"the lower cart", l51147, LocTtMineCart, TtLowerCart}, {"the deep cart", l51147, LocTtMineCart, TtDeepCart},
		{"the cell gate", l51147, LocTtCellGateR, TtCellGate}, {"the anvil", l4947, LocTtAnvil, TtAnvil}, {"the tent door", l4947, LocTtTentDoor, TtTentInside},
	} {
		if !jm2Has(c.es, c.id, c.at) {
			t.Errorf("%s: no loc %d at %v on the map", c.what, c.id, c.at)
		}
	}
	// The punishment mine holds the rocks the plan mines (at least the fifteen the sentence is, with a margin for the ones other accounts took).
	rocks := 0
	for _, e := range l51147 {
		if e.ID == LocTtPunishRock {
			rocks++
		}
	}
	if rocks < 40 {
		t.Errorf("%d punishment rocks on the map, want at least 40", rocks)
	}
}
