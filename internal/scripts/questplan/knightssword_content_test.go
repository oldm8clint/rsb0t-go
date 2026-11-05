package questplan

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, rule and number the Knight's Sword rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run SquireContent ./internal/scripts/questplan/
//
// (What the loc index the fleet runs on holds, the stands of each loc and the routes are pinned in the questmods real-data tests.)

const sqDir = "scripts/quests/quest_squire/"

// sqNpcConfig reads a [section] of the old config dump the npc stats come from into key -> value (a repeated key keeps its last value; the
// params are `param=name,value`, folded into "param:name").
func sqNpcConfig(t *testing.T, name string) map[string]string {
	t.Helper()
	return sqNpcConfigIn(t, "scripts/_unpack/225/all.npc", name)
}

// sqNpcConfigIn is sqNpcConfig over any content npc config file.
func sqNpcConfigIn(t *testing.T, file, name string) map[string]string {
	t.Helper()
	block := csConfigBlock(csContent(t, file), name)
	if block == "" {
		t.Fatalf("no [%s] in %s", name, file)
	}
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if k == "param" {
			if pk, pv, ok := strings.Cut(v, ","); ok {
				out["param:"+pk] = pv
			}
			continue
		}
		out[k] = v
	}
	return out
}

func sqInt(t *testing.T, m map[string]string, key string) int {
	t.Helper()
	n, err := strconv.Atoi(m[key])
	if err != nil {
		t.Fatalf("%s is %q: %v", key, m[key], err)
	}
	return n
}

// The pack ids the plan carries are the pack's.
func TestSquireContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"npc", npc, "squire", NpcSquire}, {"npc", npc, "sir_vyvin", NpcVyvin}, {"npc", npc, "reldo", NpcReldo}, {"npc", npc, "thurgo", NpcThurgo},
		{"npc", npc, "wydin", WydinShop.NpcID}, {"npc", npc, "icewarrior", 125}, {"npc", npc, "icegiant", 111},
		{"obj", obj, "knights_portrait", ObjSqPortrait}, {"obj", obj, "faladian_sword", ObjSqSword}, {"obj", obj, "blurite_ore", ObjSqBluriteOre},
		{"obj", obj, "piedish", ObjSqPieDish}, {"obj", obj, "pie_shell", ObjSqPieShell}, {"obj", obj, "uncooked_redberry_pie", ObjSqUncookedPie},
		{"obj", obj, "redberry_pie", ObjSqRedberryPie}, {"obj", obj, "burnt_pie", ObjSqBurntPie}, {"obj", obj, "pastry_dough", ObjSqPastryDough},
		{"obj", obj, "redberries", ObjSqRedberries}, {"obj", obj, "pot_flour", ObjSqPotOfFlour}, {"obj", obj, "pot_empty", ObjSqEmptyPot},
		{"obj", obj, "bucket_empty", ObjSqBucket}, {"obj", obj, "bucket_water", ObjSqBucketWater}, {"obj", obj, "iron_bar", ObjSqIronBar},
		{"obj", obj, "shark", ObjShark}, {"obj", obj, "bass", ObjBass},
		{"loc", loc, "vyvincupboardshut", LocSqCupboardShut}, {"loc", loc, "vyvincupboardopen", LocSqCupboardOpen}, {"loc", loc, "bluriterock", LocSqBluriteRock},
		{"loc", loc, "range", LocSqRange}, {"loc", loc, "sink", LocSqSink}, {"loc", loc, "sink2", LocSqSinkAlt}, {"loc", loc, "fountain", LocSqFountain},
		{"loc", loc, "well", LocSqWell},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
	for id := range sqPickRates {
		found := false
		for _, v := range obj {
			found = found || v == id
		}
		if !found {
			t.Errorf("pickaxe %d is in no pack entry", id)
		}
	}
}

// The stages, the quest point and the reward.
func TestSquireContentStagesAndRewards(t *testing.T) {
	consts := csContent(t, sqDir+"configs/quest_squire.constant")
	for _, want := range []string{"^squire_not_started = 0", "^squire_started = 1", "^squire_spoken_reldo = 2", "^squire_given_pie = 3", "^squire_spoken_thurgo = 4",
		"^squire_looking_portrait = 5", "^squire_looking_blurite = 6"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_squire.constant lacks %q", want)
		}
	}
	quest := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{"^squire_complete = 7", "^squire_questpoints = 1"} {
		if !strings.Contains(quest, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	if SqNotStarted != 0 || SqStarted != 1 || SqSpokenReldo != 2 || SqGivenPie != 3 || SqSpokenThurgo != 4 || SqLookingPortrait != 5 || SqLookingBlurite != 6 || SqComplete != 7 {
		t.Errorf("the plan's stages are not the content's")
	}
	src := csContent(t, sqDir+"scripts/quest_squire.rs2")
	for _, want := range []string{"stat_advance(smithing, 127250);", "~send_quest_complete(questlist:squire, faladian_sword, 260, ^squire_questpoints,"} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_squire.rs2 lacks %q", want)
		}
	}
	if f := LadderFacts[KnightsSwordID]; f.QP != 1 || f.XP["smithing"] != 12725 || len(f.XP) != 1 {
		t.Errorf("the ladder row pays %d quest points and %v", f.QP, f.XP)
	}
	// The sword is taken from the PACK: a worn one is refused, a banked one is sent for, and the hand-in script says so.
	for _, want := range []string{"inv_total(bank, faladian_sword) >= 1", "So can you un-equip it and hand it over to me now please?", "inv_del(inv, faladian_sword, 1);",
		"queue(squire_complete, 0, 0);"} {
		if !strings.Contains(src, want) {
			t.Errorf("quest_squire.rs2 lacks %q", want)
		}
	}
}

// The journal's markers are in the journal script, newest stage last in the file, and no needle spans a colour tag next to punctuation.
func TestSquireContentJournalMarkers(t *testing.T) {
	src := strings.ToLower(csContent(t, sqDir+"scripts/squire_journal.rs2"))
	// The script's text with its colour tags turned to spaces and its string-joining quotes removed, as the client shows it.
	text := NormaliseLines([]string{strings.NewReplacer("\"", " ", ");", " ", "append($x, ", " ").Replace(src)})
	last := -1
	order := []string{}
	for _, m := range sqMarkers {
		order = append(order, m.text)
	}
	slices.Reverse(order) // not started first
	for _, needle := range order {
		i := strings.Index(text, needle)
		if i < 0 {
			t.Errorf("the journal script has no %q", needle)
			continue
		}
		// The two markers of stage 6 (the sword smithed and the materials wanted) are the last two branches, in either order.
		if i < last && needle != "thurgo has now smithed me a replica" {
			t.Errorf("%q comes before the marker of the stage before it in the script", needle)
		}
		last = max(last, i)
	}
	for _, m := range sqMarkers {
		if strings.Contains(m.text, "@") {
			t.Errorf("marker %q has a colour tag in it", m.text)
		}
	}
}

// The menus and the lines the stops prefer are the scripts' own, in the order the scripts ask them.
func TestSquireContentDialogue(t *testing.T) {
	squire := csContent(t, "scripts/areas/area_falador/scripts/squire.rs2") + csContent(t, sqDir+"scripts/quest_squire.rs2")
	reldo := csContent(t, "scripts/areas/area_varrock/scripts/reldo.rs2") + csContent(t, sqDir+"scripts/quest_squire.rs2")
	thurgo := csContent(t, "scripts/areas/area_port_sarim/scripts/thurgo.rs2") + csContent(t, sqDir+"scripts/quest_squire.rs2")
	for _, p := range SqSquireStop.Prefer {
		if !strings.Contains(squire, p) {
			t.Errorf("the Squire's scripts have no option %q", p)
		}
	}
	// The four menus in the order they are asked: how is life, the offer, the dwarves, the go.
	last := -1
	for _, line := range []string{
		`~p_choice2("And how is life as a squire?", 1, "Wouldn't you prefer to be a squire for me?", 2);`,
		`~p_choice3("Do you know where you lost it?", 1, "I can make a new sword if you like...", 2, "Is he angry?", 3);`,
		`~p_choice2("So would these dwarves make another one?", 1, "Well I hope you find it soon.", 2);`,
		`~p_choice2("Ok, I'll give it a go.", 1, "No, I've got lots of mining work to do.", 2);`,
	} {
		i := strings.Index(squire, line)
		if i < 0 {
			t.Errorf("the Squire's script lacks %q", line)
			continue
		}
		if i < last {
			t.Errorf("%q is asked before the menu the plan puts it after", line)
		}
		last = i
	}
	// None of the Squire's menus has an option the stop's list would take by mistake: a preference matches an option's text, so the
	// four preferences must each match one option of their own menu and no option of another.
	menus := [][]string{
		{"And how is life as a squire?", "Wouldn't you prefer to be a squire for me?"},
		{"Do you know where you lost it?", "I can make a new sword if you like...", "Is he angry?"},
		{"So would these dwarves make another one?", "Well I hope you find it soon."},
		{"Ok, I'll give it a go.", "No, I've got lots of mining work to do."},
	}
	for i, menu := range menus {
		pick, ok := PickPreferred(menu, SqSquireStop.Prefer)
		if !ok || pick != SqSquireStop.Prefer[i] && !strings.HasPrefix(pick, SqSquireStop.Prefer[i]) {
			t.Errorf("menu %d %v: the stop picks %q (%v), want the option for %q", i, menu, pick, ok, SqSquireStop.Prefer[i])
		}
	}
	if !strings.Contains(reldo, `"What do you know about the Imcando dwarves?"`) || SqReldoStop.Prefer[0] != "What do you know about the Imcando dwarves?" {
		t.Errorf("Reldo's option is not the plan's")
	}
	// Thurgo: the pie's menu at stage 2 holds only the pie and the greeting; every other stage's talk is a straight run of dialogue.
	if !strings.Contains(thurgo, `@multi2("Hello. Are you an Imcando dwarf?", thurgo_who_wants, "Would you like a redberry pie?", thurgo_redberry_pie);`) {
		t.Errorf("Thurgo's first menu is not the plan's")
	}
	if pick, ok := PickPreferred([]string{"Hello. Are you an Imcando dwarf?", "Would you like a redberry pie?"}, SqThurgoStop.Prefer); !ok || pick != "Would you like a redberry pie?" {
		t.Errorf("Thurgo's menu: the stop picks %q", pick)
	}
	for _, want := range []string{"case ^squire_spoken_reldo : @thurgo_inquire;", "case ^squire_given_pie : @thurgo_special_sword_post_pie;",
		"inv_del(inv, redberry_pie, 1);", "%squire = ^squire_given_pie;", "%squire = ^squire_spoken_thurgo;", "%squire = ^squire_looking_blurite;",
		"inv_del(inv, blurite_ore, 1);", "inv_del(inv, iron_bar, 2);", "inv_add(inv, faladian_sword, 1);"} {
		if !strings.Contains(thurgo, want) {
			t.Errorf("Thurgo's scripts lack %q", want)
		}
	}
	// The first stop of the quest, and Reldo's menu, are reached at the stages the plan talks at.
	for _, want := range []string{"%squire = ^squire_started;", "%squire = ^squire_spoken_reldo;", "%squire = ^squire_looking_portrait;"} {
		if !strings.Contains(squire+reldo, want) {
			t.Errorf("the scripts lack %q", want)
		}
	}
}

// Sir Vyvin's cupboard: where it is, when it gives the portrait and what stops it.
func TestSquireContentTheCupboard(t *testing.T) {
	cup := csContent(t, "scripts/general_use/scripts/cupboards.rs2")
	for _, want := range []string{"case 2_46_52_40_8 : // falador castle sir vyvin room", "if(%squire >= 5) {", "if(~vyvin_distracted = false) {",
		"inv_total(inv, knights_portrait) = 0 & inv_total(bank, knights_portrait) = 0", "inv_add(inv, knights_portrait, 1);"} {
		if !strings.Contains(cup, want) {
			t.Errorf("cupboards.rs2 lacks %q", want)
		}
	}
	// 2_46_52_40_8 is level 2, mapsquare 46_52, zone tile 40,8: 2984,3336.
	if SqCupboardAt != (tile.Tile{X: 46*64 + 40, Z: 52*64 + 8, Level: 2}) {
		t.Errorf("the cupboard is %v, the script's coordinate says %v", SqCupboardAt, tile.Tile{X: 46*64 + 40, Z: 52*64 + 8, Level: 2})
	}
	// The refusal is proximity alone: npc_find(coord, sir_vyvin, 1, 0) on the player's own coordinate (no line of sight).
	if !strings.Contains(csContent(t, sqDir+"scripts/quest_squire.rs2"), "if (npc_find(coord, sir_vyvin, 1, 0) = true) {") {
		t.Errorf("vyvin_distracted is not npc_find(coord, sir_vyvin, 1, 0)")
	}
	locs := csContent(t, "scripts/general_use/configs/cupboards.loc")
	shut := csConfigBlock(locs, "vyvincupboardshut")
	open := csConfigBlock(locs, "vyvincupboardopen")
	for _, want := range []string{"op1=Open", "length=2", "forceapproach=east", "param=cupboard_other,vyvincupboardopen"} {
		if !strings.Contains(shut, want) {
			t.Errorf("[vyvincupboardshut] lacks %q", want)
		}
	}
	for _, want := range []string{"op1=Search", "op2=Shut", "forceapproach=east", "param=cupboard_other,vyvincupboardshut"} {
		if !strings.Contains(open, want) {
			t.Errorf("[vyvincupboardopen] lacks %q", want)
		}
	}
	if !strings.Contains(cup, "loc_change(loc_param(cupboard_other), 300);") {
		t.Errorf("the cupboard no longer stays open 300 ticks")
	}
	// The map: the shut cupboard at 2984,3336 on level 2, angle 1, and the keeper.
	rows := csMapRows(t, "LOC", SqCupboardAt)
	if !csHasRow(rows, LocSqCupboardShut) {
		t.Errorf("the map has no cupboard %d at %v: %v", LocSqCupboardShut, SqCupboardAt, rows)
	}
	for _, r := range rows {
		if len(r) >= 3 && r[0] == strconv.Itoa(LocSqCupboardShut) && (r[1] != "10" || r[2] != "1") {
			t.Errorf("the cupboard's shape and angle are %v, the plan says 10 and 1", r[1:])
		}
	}
	// Both approaches are the tiles south of the cupboard's two tiles.
	for _, a := range SqVyvinApproaches {
		if a.Level != 2 || a.Z != SqCupboardAt.Z-1 || a.X < SqCupboardAt.X || a.X > SqCupboardAt.X+1 {
			t.Errorf("approach %v is not south of the cupboard's two tiles", a)
		}
	}
}

// The pie: the levels, the rates, the menu, the burnt pie's dish and Wydin's shelf.
func TestSquireContentThePie(t *testing.T) {
	structs := csContent(t, "scripts/skill_cooking/configs/cooking_inv/configs/pies/pies.struct")
	if b := csConfigBlock(structs, "uncooked_redberry_pie"); !strings.Contains(b, "param=levelrequired,10") || !strings.Contains(b, "param=product,uncooked_redberry_pie") {
		t.Errorf("the uncooked redberry pie's struct is %q", b)
	}
	cooking := csContent(t, "scripts/skill_cooking/configs/cooking_source/cooking_generic.dbrow")
	b := csConfigBlock(cooking, "cooking_generic_redberry_pie")
	for _, want := range []string{"data=uncooked,uncooked_redberry_pie", "data=cooked,redberry_pie", "data=burnt,burnt_pie", "data=levelrequired,10", "data=successchance,98,452"} {
		if !strings.Contains(b, want) {
			t.Errorf("the redberry pie's cooking row lacks %q", want)
		}
	}
	pies := csContent(t, "scripts/skill_cooking/scripts/cooking_inv/scripts/pies/pies.rs2")
	for _, want := range []string{"[opheldu,pastry_dough]", "case piedish : @make_pie_shell;", "[opheldu,pie_shell]", "case cooking_apple, cooked_meat, cooked_chicken, redberries : @make_uncooked_pie(last_useslot);",
		"[opheld1,burnt_pie]", "inv_add(inv, piedish, 1);"} {
		if !strings.Contains(pies, want) {
			t.Errorf("pies.rs2 lacks %q", want)
		}
	}
	if SqCooking != 10 {
		t.Errorf("SqCooking is %d", SqCooking)
	}
	// The expected burn rate: 131 in 256 at Cooking 10 (the comment's number): floor(98*89/98) + floor(452*9/98) + 1.
	if v := (98*(99-10))/98 + (452*(10-1))/98 + 1; v != 131 {
		t.Errorf("the success value at Cooking 10 is %d, the plan's comment says 131", v)
	}
	// Burnt pie: Empty Dish.
	if bp := csConfigBlock(csContent(t, "scripts/skill_cooking/configs/cooking_inv/configs/pies/pies.obj"), "burnt_pie"); !strings.Contains(bp, "iop1=Empty Dish") {
		t.Errorf("the burnt pie's first op is not Empty Dish: %q", bp)
	}
	// The dough menu.
	dough := csContent(t, "scripts/skill_cooking/scripts/cooking_inv/scripts/dough/dough.rs2")
	if !strings.Contains(dough, `~p_choice4_header("Bread dough.", 1, "Pastry dough.", 2, "Pizza dough.", 3, "Pitta dough.", 4,`) {
		t.Errorf("the dough menu is not Bread, Pastry, Pizza, Pitta")
	}
	if pick, ok := PickPreferred([]string{"Bread dough.", "Pastry dough.", "Pizza dough.", "Pitta dough."}, []string{"Pastry dough"}); !ok || pick != "Pastry dough." {
		t.Errorf("the dough menu: %q", pick)
	}
	water := csContent(t, "scripts/general_use/scripts/water_sources.rs2")
	for _, want := range []string{"[opheldu,bucket_water]", "case pot_flour : @dough_interface(last_item);", "[oplocu,_watersource]", "case bucket_empty, jug_empty,", "[oplocu,_well]", "case bucket_empty : @fill_bucket_well;"} {
		if !strings.Contains(water, want) {
			t.Errorf("water_sources.rs2 lacks %q", want)
		}
	}
	// Wydin's shelf: three pots of flour and one redberry, each restocking in a hundred ticks.
	shelf := csConfigBlock(csContent(t, "scripts/areas/area_port_sarim/configs/port_sarim.inv"), "wydinstore")
	for _, want := range []string{"stock1=pot_flour,3,100", "stock6=redberries,1,100"} {
		if !strings.Contains(shelf, want) {
			t.Errorf("Wydin's shelf lacks %q", want)
		}
	}
	// The flour and the berries are asked for in threes with one the least, and the burnt pie keeps the dish.
	kit := sqPieKitLines(Snapshot{InvIDs: map[int]int{}})
	for _, l := range kit {
		if (l.ID == ObjSqPotOfFlour || l.ID == ObjSqRedberries) && (l.Qty != 3 || l.Min != 1) {
			t.Errorf("%s is asked for as %d with a minimum of %d", l.Name, l.Qty, l.Min)
		}
	}
	// The ground spawn of the dish is the palace kitchen's.
	spawns := csMapRows(t, "OBJ", tile.Tile{X: 3222, Z: 3494})
	if !csHasRow(spawns, ObjSqPieDish) {
		t.Errorf("the palace kitchen's floor at 3222,3494 holds no pie dish: %v", spawns)
	}
}

// The rock: its level, its chance, its respawn, the pickaxes' rolls.
func TestSquireContentTheRock(t *testing.T) {
	mine := csContent(t, "scripts/skill_mining/configs/mine.dbrow")
	b := csConfigBlock(mine, "blurite_rock")
	for _, want := range []string{"data=rock,bluriterock", "data=rock_output,blurite_ore", "data=rock_level,10", "data=rock_successchance,95,350", "data=rock_respawnrate,80"} {
		if !strings.Contains(b, want) {
			t.Errorf("the blurite rock's row lacks %q", want)
		}
	}
	if SqMining != 10 {
		t.Errorf("SqMining is %d", SqMining)
	}
	picks := csContent(t, "scripts/skill_mining/configs/pickaxes.obj")
	obj := csPackIDs(t, "obj")
	for name, rate := range map[string]int{"bronze_pickaxe": 7, "iron_pickaxe": 6, "steel_pickaxe": 5, "mithril_pickaxe": 4, "adamant_pickaxe": 3, "rune_pickaxe": 2} {
		if got := sqPickRates[obj[name]]; got != rate {
			t.Errorf("%s rolls every %d ticks in the plan, %d in the content", name, got, rate)
		}
		if blk := csConfigBlock(picks, name); !strings.Contains(blk, fmt.Sprintf("param=mining_rate,%d", rate)) {
			t.Errorf("%s: the content's mining_rate is not %d", name, rate)
		}
	}
	// stat_random: floor(low*(99-level)/98) + floor(high*(level-1)/98) + 1 against 256: the chance at Mining 10 is 119 in 256.
	if got := SqRockChance(10); got*256 != 119 {
		t.Errorf("the chance at Mining 10 is %v of 256, want 119", got*256)
	}
	// Mined with a bronze pickaxe (7 ticks a roll) at Mining 10, the 99th percentile is 8 rolls.
	if got := SqMineTicks(10, 7); got != 3+7*8 {
		t.Errorf("SqMineTicks(10, 7) = %d", got)
	}
	// The mining script: a bluriterock is mined by op 1, and a pickaxe must be in the pack or worn.
	script := csContent(t, "scripts/skill_mining/scripts/mining.rs2")
	for _, want := range []string{"[oploc1,_mining_rock_normal] @mining_firstswing(get_ore_normal);", "You need a Mining level of <tostring($levelreq)> to mine this rock.", "if (random($chance) = ^true) {"} {
		if !strings.Contains(script, want) {
			t.Errorf("mining.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(csContent(t, "scripts/skill_mining/configs/rocks.loc"), "[bluriterock]") {
		t.Errorf("rocks.loc has no [bluriterock]")
	}
}

// The two iron bars are fetched from the bank or the mule, never smelted: a bar wants Smithing 15 at a furnace, which an account that comes
// to the quest for its first thousands of Smithing experience may not have.
func TestSquireContentIronBarsWantSmithing15(t *testing.T) {
	b := csConfigBlock(csContent(t, "scripts/skill_smithing/configs/smelting/smelting.struct"), "smelting_iron_bar")
	for _, want := range []string{"param=product,iron_bar", "param=levelrequired,15", "param=ingredient,iron_ore"} {
		if !strings.Contains(b, want) {
			t.Errorf("the iron bar's smelting row lacks %q", want)
		}
	}
}

// The hunters of the dungeon: their stats, their spawns, the combat levels they ignore, and that the cave is single-way combat.
func TestSquireContentTheHunters(t *testing.T) {
	// The warrior and the giant are the plan's foes.
	w := sqNpcConfig(t, "icewarrior")
	if sqInt(t, w, "vislevel") != 57 || sqInt(t, w, "attack") != FoeIceWarrior.Attack || sqInt(t, w, "strength") != FoeIceWarrior.Strength ||
		sqInt(t, w, "defence") != FoeIceWarrior.Defence || sqInt(t, w, "hitpoints") != FoeIceWarrior.Hitpoints || sqInt(t, w, "huntrange") != 5 ||
		sqInt(t, w, "wanderrange") != 3 || w["huntmode"] != "cowardly" || w["param:attackrate"] != "" || w["param:attackbonus"] != "" || w["param:strengthbonus"] != "" {
		t.Errorf("the Ice warrior in the content is %v", w)
	}
	if FoeIceWarrior.AttackTicks != 4 || FoeIceWarrior.MaxHit() != 6 {
		t.Errorf("the plan's Ice warrior hits up to %d every %d ticks", FoeIceWarrior.MaxHit(), FoeIceWarrior.AttackTicks)
	}
	g := sqNpcConfig(t, "icegiant")
	if sqInt(t, g, "vislevel") != 53 || sqInt(t, g, "attack") != FoeIceGiant.Attack || sqInt(t, g, "strength") != FoeIceGiant.Strength || sqInt(t, g, "defence") != FoeIceGiant.Defence ||
		sqInt(t, g, "hitpoints") != FoeIceGiant.Hitpoints || sqInt(t, g, "huntrange") != 2 || sqInt(t, g, "wanderrange") != 3 || sqInt(t, g, "size") != 2 ||
		g["huntmode"] != "cowardly" || g["param:attackrate"] != "5" || g["param:attackbonus"] != "29" || g["param:strengthbonus"] != "31" {
		t.Errorf("the Ice giant in the content is %v", g)
	}
	if FoeIceGiant.AttackTicks != 5 || FoeIceGiant.AttackBonus != 29 || FoeIceGiant.StrengthBonus != 31 || FoeIceGiant.MaxHit() != 7 {
		t.Errorf("the plan's Ice giant hits up to %d", FoeIceGiant.MaxHit())
	}
	// The hunt mode: cowardly checks line of sight, skips a player over twice the level outside the Wilderness and one in combat in the last
	// eight ticks.
	hunt := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "cowardly")
	for _, want := range []string{"type=player", "check_vis=lineofsight", "check_nottoostrong=outside_wilderness", "check_notcombat=%lastcombat"} {
		if !strings.Contains(hunt, want) {
			t.Errorf("the cowardly hunt lacks %q", want)
		}
	}
	// The kinds of hunter: ids, levels and ceilings (twice the level).
	npc := csPackIDs(t, "npc")
	names := map[string]string{"Ice warrior": "icewarrior", "Ice giant": "icegiant", "Hobgoblin (armed)": "hobgoblin_armed", "Hobgoblin": "hobgoblin_unarmed",
		"Pirate": "pirate_aggressive", "Mugger": "mugger"}
	byID := map[int]SqHunter{}
	for _, h := range SqHunters {
		cfg := sqNpcConfig(t, names[h.Name])
		if npc[names[h.Name]] != h.NPC || sqInt(t, cfg, "vislevel") != h.Level || h.Ceiling != 2*h.Level || cfg["huntmode"] != "cowardly" {
			t.Errorf("%s: pack id %d (plan %d), level %s (plan %d), ceiling %d, huntmode %q", h.Name, npc[names[h.Name]], h.NPC, cfg["vislevel"], h.Level, h.Ceiling, cfg["huntmode"])
		}
		byID[h.NPC] = h
	}
	// The combat gate takes the Hobgoblins, the Pirates and the Muggers out: each hunts only accounts under the gate.
	for _, h := range SqHunters {
		if (h.Name == "Hobgoblin" || h.Name == "Pirate" || h.Name == "Mugger") && h.Ceiling >= SqMinCombat {
			t.Errorf("%s hunts combat %d and under, and the gate is %d", h.Name, h.Ceiling, SqMinCombat)
		}
	}
	if SqMinCombat != 2*byID[122].Level+2 {
		t.Errorf("the combat gate %d is not the unarmed Hobgoblin's ceiling %d and a level's margin and one more", SqMinCombat, byID[122].Ceiling)
	}
	// Every spawn in the cave's box that hunts is one of those kinds, and the warriors and the giants are exactly the plan's.
	spawns := dsSpawns(t, sqDungeonBox.MinX, sqDungeonBox.MaxX, sqDungeonBox.MinZ, sqDungeonBox.MaxZ)
	idName := map[int]string{}
	for n, id := range npc {
		idName[id] = n
	}
	for id, at := range spawns {
		cfg, ok := sqNpcConfigMaybe(t, idName[id])
		if !ok || cfg["huntmode"] == "" {
			continue
		}
		if _, known := byID[id]; !known {
			t.Errorf("npc %d (%s) hunts in the cave, at %v, and is not among the plan's hunters", id, idName[id], at)
		}
	}
	sortTiles := func(ts []tile.Tile) []tile.Tile {
		out := append([]tile.Tile(nil), ts...)
		slices.SortFunc(out, func(a, b tile.Tile) int {
			if a.X != b.X {
				return a.X - b.X
			}
			return a.Z - b.Z
		})
		return out
	}
	if !slices.Equal(sortTiles(spawns[125]), sortTiles(SqIceWarriorSpawns)) {
		t.Errorf("the Ice warriors in the maps are %v, the plan has %v", sortTiles(spawns[125]), sortTiles(SqIceWarriorSpawns))
	}
	if !slices.Equal(sortTiles(spawns[111]), sortTiles(SqIceGiantSpawns)) {
		t.Errorf("the Ice giants in the maps are %v, the plan has %v", sortTiles(spawns[111]), sortTiles(SqIceGiantSpawns))
	}
	// The same herd the Magic pens' threat table has.
	var threatWarriors, threatGiants []tile.Tile
	for _, th := range fleetplan.IceWarriorThreats {
		switch th.Name {
		case "Ice warrior":
			threatWarriors = append(threatWarriors, th.At)
		case "Ice giant":
			threatGiants = append(threatGiants, th.At)
		}
	}
	if !slices.Equal(sortTiles(threatWarriors), sortTiles(SqIceWarriorSpawns)) || !slices.Equal(sortTiles(threatGiants), sortTiles(SqIceGiantSpawns)) {
		t.Errorf("fleetplan.IceWarriorThreats names %v and %v", threatWarriors, threatGiants)
	}
	// Single-way combat: no zone of the three squares is in multiway.csv.
	multi := csContent(t, "maps/multiway.csv")
	for _, sq := range []string{"0_46_149_", "0_47_149_", "0_48_149_"} {
		for _, line := range strings.Split(multi, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), sq) {
				t.Errorf("multiway.csv has a zone %q of the cave: %s", sq, line)
			}
		}
	}
}

// sqNpcConfigMaybe is sqNpcConfig for a name that may have no section in the dump.
func sqNpcConfigMaybe(t *testing.T, name string) (map[string]string, bool) {
	t.Helper()
	if csConfigBlock(csContent(t, "scripts/_unpack/225/all.npc"), name) == "" {
		return nil, false
	}
	return sqNpcConfig(t, name), true
}

// The keepers stand where the plan says.
func TestSquireContentSpawns(t *testing.T) {
	spawns := dsSpawns(t, 2940, 3260, 3120, 3510)
	has := func(id int, at tile.Tile) bool {
		for _, s := range spawns[id] {
			if s == at {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		name string
		id   int
		at   tile.Tile
	}{{"the Squire", NpcSquire, SqSquireAnchor}, {"Reldo", NpcReldo, SqReldoAnchor}, {"Thurgo", NpcThurgo, SqThurgoAnchor}, {"Wydin", WydinShop.NpcID, WydinShop.Anchor},
		{"Sir Vyvin", NpcVyvin, tile.Tile{X: 2983, Z: 3335, Level: 2}}} {
		if !has(c.id, c.at) {
			t.Errorf("%s is not at %v in the maps: %v", c.name, c.at, spawns[c.id])
		}
	}
	if vy := sqNpcConfigIn(t, "scripts/areas/area_falador/configs/falador.npc", "sir_vyvin"); vy["wanderrange"] != "10" || vy["huntmode"] != "" {
		t.Errorf("Sir Vyvin's wander range is %q and his hunt mode %q, the plan says ten tiles and none", vy["wanderrange"], vy["huntmode"])
	}
	// The ladder to the dungeon and its landing: the walk graph's stand is the tile east of the loc.
	if SqLadderTop != (tile.Tile{X: 3009, Z: 3150}) || SqLanding != (tile.Tile{X: 3008, Z: 9551}) {
		t.Errorf("the ladder's top %v and the landing %v", SqLadderTop, SqLanding)
	}
}

// The quest's loc index facts that are map data: the four rocks are where the plan says and the east stands are those the module uses.
func TestSquireContentTheRocksAreWhereThePlanSays(t *testing.T) {
	for _, at := range []tile.Tile{{X: 3049, Z: 9566}, {X: 3059, Z: 9564}, {X: 3067, Z: 9581}, {X: 3067, Z: 9582}} {
		if rows := csMapRows(t, "LOC", at); !csHasRow(rows, LocSqBluriteRock) {
			t.Errorf("no blurite rock at %v: %v", at, rows)
		}
	}
	for _, st := range SqStands {
		if st.Stand.X != st.Rock.X-1 || st.Stand.Z != st.Rock.Z {
			t.Errorf("stand %v is not the tile west of rock %v", st.Stand, st.Rock)
		}
	}
	// Ten bluriterock rows in all? No: exactly the four, in the squares of the cave.
	n := 0
	for _, sq := range []tile.Tile{{X: 3008, Z: 9536}, {X: 2944, Z: 9536}, {X: 3072, Z: 9536}} {
		src := csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", sq.X>>6, sq.Z>>6))
		in := false
		for _, line := range strings.Split(src, "\n") {
			if strings.HasPrefix(line, "==== ") {
				in = line == "==== LOC ===="
				continue
			}
			if in && strings.Contains(line, ": "+strconv.Itoa(LocSqBluriteRock)+" ") {
				n++
			}
		}
	}
	if n != 4 {
		t.Errorf("%d bluriterock rows in the cave's squares, want 4", n)
	}
}
