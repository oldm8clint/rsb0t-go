package mastercraftplan

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

func TestGamesRouteFrom(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		where ChestPlace
		have  bool
		want  bool
	}{
		{GamesRouteAlways, ChestAtPost, true, true},
		{GamesRouteAlways, ChestAtFalador, true, true},
		{GamesRouteAlways, ChestAtOther, true, false}, // mid-walk, in Burthorpe: walk on
		{GamesRouteAlways, ChestAtChest, true, false},
		{GamesRouteAlways, ChestAtPost, false, false}, // no necklace
		{GamesRoutePost, ChestAtPost, true, true},
		{GamesRoutePost, ChestAtFalador, true, false},
		{GamesRouteOff, ChestAtPost, true, false},
		{"", ChestAtPost, true, false},
	} {
		if got := GamesRouteFrom(tc.mode, tc.where, tc.have); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
	if got := GamesRoutes(); !reflect.DeepEqual(got, []string{"always", "post", "off"}) {
		t.Errorf("%v", got)
	}
}

func TestChestSettingsDefaultAndDecode(t *testing.T) {
	set := DefaultSettings()
	if set.ChestGamesRoute != GamesRouteAlways || !set.ChestAlch {
		t.Errorf("defaults: %+v", set)
	}
	s, err := DecodeSettings(map[string]interface{}{KeyChestGamesRoute: " POST ", KeyChestAlch: false})
	if err != nil || s.ChestGamesRoute != GamesRoutePost || s.ChestAlch {
		t.Errorf("decode: %+v %v", s, err)
	}
	if _, err := DecodeSettings(map[string]interface{}{KeyChestGamesRoute: "sometimes"}); err == nil || !strings.Contains(err.Error(), "craftChestGamesRoute") {
		t.Errorf("a bad mode: %v", err)
	}
}

func TestChestAlchable(t *testing.T) {
	for _, name := range []string{"Ruby", "Diamond", "Adamant sq shield", "Rune platelegs", "rune plateskirt", " Ruby "} {
		if !ChestAlchable(name) {
			t.Errorf("%q is alchable loot", name)
		}
	}
	for _, name := range []string{"Uncut dragonstone", "Runite bar", "Coins", "Crystal key", "Half of a key", "Emerald", "Nature rune"} {
		if ChestAlchable(name) {
			t.Errorf("%q is not alched", name)
		}
	}
	if ChestAlchOK(DefaultSettings(), 54) || !ChestAlchOK(DefaultSettings(), 55) {
		t.Error("Magic 55 is the level")
	}
	off := DefaultSettings()
	off.ChestAlch = false
	if ChestAlchOK(off, 99) {
		t.Error("the setting is off")
	}
}

func TestChestKitWantForAddsTheExtras(t *testing.T) {
	paid := map[string]bool{RuneFire: true}
	opt := ChestKitOptions{Alch: true, Route: true, PostRoute: true}
	w := ChestKitWantFor(opt, false, 66, paid, false, 300)
	if w.Keys != 6 || w.Extras[GamesNecklaceKit] != 1 || w.Extras[NatureKit] != ChestNaturesCarry || w.Extras["Ruby"] != ChestFodderCarry || w.Extras["Diamond"] != 0 {
		t.Errorf("at the Falador bank: %+v", w)
	}
	// Magic 66 with the staff of fire: Air for 6 Falador Teleports and a Camelot, no Fire.
	if w.Runes[RuneAir] != 23 || w.Runes[RuneFire] != 0 || w.Runes[RuneWater] != 6 || w.Runes[RuneLaw] != 7 {
		t.Errorf("runes: %v", w.Runes)
	}
	// At the post below Magic 37 no teleport can be cast, so no keys would ride from there; the Games Room
	// route's flight takes them.
	if w := ChestKitWantFor(ChestKitOptions{Route: true}, true, 30, nil, false, 300); w.Keys != 0 {
		t.Errorf("no route flight: %+v", w)
	}
	if w := ChestKitWantFor(opt, true, 30, nil, false, 300); w.Keys != 6 {
		t.Errorf("the flight to the chest carries the keys: %+v", w)
	}
	// Going home takes no keys and no extras the options do not name.
	if w := ChestKitWantFor(ChestKitOptions{}, false, 66, nil, true, 300); w.Keys != 0 || len(w.Extras) != 0 {
		t.Errorf("home with no options: %+v", w)
	}
}

func chestExtras() KitIDs {
	return KitIDs{
		Keys:  []int{989},
		Coins: []int{995},
		Fruit: []int{464},
		Runes: map[string][]int{RuneAir: {556}},
		Extras: ChestExtrasFor(
			[]int{561}, []int{3853, 3855},
			map[string][]int{"Ruby": {1603}, "Diamond": {1601}},
			map[string][]int{"Ruby": {1604}, "Diamond": {1602}}, 1387),
	}
}

func TestChestExtrasDrawn(t *testing.T) {
	ids := chestExtras()
	bank := []BankRow{{989, 300}, {995, 5000}, {464, 9}, {556, 500}, {561, 700}, {3855, 2}, {1603, 40}, {1601, 80}}
	want := ChestKitWantFor(ChestKitOptions{Alch: true, Route: true}, false, 66, nil, false, 300)
	got := PlanChestDraws(bank, ids, want)
	have := map[string]int{}
	for _, d := range got {
		have[d.Item] = d.Count
		if d.Item == GamesNecklaceKit && d.ID != 3855 {
			t.Errorf("the necklace is the first id the bank holds: %+v", d)
		}
	}
	if have[NatureKit] != ChestNaturesCarry || have[GamesNecklaceKit] != 1 || have["Ruby"] != 0 {
		t.Errorf("item draws: %v", have)
	}
	// The gems come as notes, and only the rubies: the owner's rubies for the walks, never the diamonds.
	noted := PlanChestNotedDraws(bank, ids, want)
	if len(noted) != 1 || noted[0].Item != "Ruby" || noted[0].ID != 1603 || noted[0].Count != 40 {
		t.Errorf("noted draws: %+v", noted)
	}
	noRuby := []BankRow{{989, 300}, {1601, 80}}
	if noted = PlanChestNotedDraws(noRuby, ids, want); len(noted) != 0 {
		t.Errorf("no rubies, so no gems carried (the diamonds are the chest's loot, not the walks' fodder): %+v", noted)
	}
	// With no options nothing extra is drawn.
	plain := ChestKitWantFor(ChestKitOptions{}, false, 66, nil, false, 300)
	if d := PlanChestDraws(bank, ids, plain); len(d) > 5 {
		t.Errorf("plain: %+v", d)
	}
	if n := PlanChestNotedDraws(bank, ids, plain); len(n) != 0 {
		t.Errorf("plain notes: %+v", n)
	}
}

func TestChestPackHasLootKnowsTheExtras(t *testing.T) {
	ids := chestExtras()
	runes := map[string]int{RuneAir: 23}
	kit := []BankRow{{989, 1}, {989, 1}, {995, ChestFares}, {464, 3}, {556, 23}, {561, 40}, {3855, 1}, {1604, 60}, {1387, 1}}
	if ChestPackHasLoot(kit, ids, runes) {
		t.Error("the kit itself is not loot")
	}
	// More than a visit carries is loot; so is an unnoted ruby (the chest's) and a noted diamond.
	for name, extra := range map[string]BankRow{
		"nature runes over the carry": {561, 10}, "a second necklace": {3853, 1}, "an unnoted ruby": {1603, 1}, "a cut diamond": {1601, 1},
		"the shield": {1199, 1},
	} {
		if !ChestPackHasLoot(append(append([]BankRow(nil), kit...), extra), ids, runes) {
			t.Errorf("%s should be loot", name)
		}
	}
	if !ChestPackHasLoot(append(append([]BankRow(nil), kit...), BankRow{1602, 1}), ids, runes) {
		t.Error("a noted diamond is not in the kit: only rubies are carried for the walks")
	}
	// Without the extras in the kit ids, the same pack is all loot.
	plain := ids
	plain.Extras = nil
	if !ChestPackHasLoot(kit, plain, runes) {
		t.Error("the extras are loot to a kit that has none")
	}
}

func TestChestAsksForTheAlchemyKitAndTheNecklace(t *testing.T) {
	stock := stockMap(map[string]int{
		ChestKeyItem: 500, "Nature rune": 90000, "Ruby": 5000, "Diamond": 300, "Games necklace(5)": 3, "Games necklace(8)": 7, "Ring of dueling(8)": 4,
	})
	own := func(m map[string]int) func(string) int { return func(item string) int { return m[item] } }
	opt := ChestKitOptions{Alch: true, Route: true}
	lines := ChestAskFor(opt, stock, 66, nil, own(nil))
	asked := map[string]int{}
	for _, l := range lines {
		asked[l.Item] = l.Qty
	}
	if asked["Nature rune"] != ChestNaturesAsk || asked["Ruby"] != ChestFodderAsk || asked["Games necklace(8)"] != 1 || asked["Diamond"] != 0 || asked["Ring of dueling(8)"] != 0 {
		t.Errorf("asked %v", asked)
	}
	// What the own bank already holds is not asked again.
	lines = ChestAskFor(opt, stock, 66, nil, own(map[string]int{"Games necklace(2)": 1, "Nature rune": 2000, "Ruby": 400}))
	for _, l := range lines {
		if l.Item == "Nature rune" || l.Item == "Ruby" || strings.HasPrefix(l.Item, "Games necklace") {
			t.Errorf("asked for what the bank holds: %v", l)
		}
	}
	// No rubies at the mule: the natures alone (diamonds are never asked for).
	stock = stockMap(map[string]int{"Diamond": 300, "Nature rune": 100})
	lines = ChestAskFor(opt, stock, 66, nil, own(nil))
	asked = map[string]int{}
	for _, l := range lines {
		asked[l.Item] = l.Qty
	}
	if asked["Diamond"] != 0 || asked["Nature rune"] != 100 || asked["Ruby"] != 0 {
		t.Errorf("no rubies: %v", asked)
	}
	// With the options off the ask is the old one.
	if got, old := ChestAskFor(ChestKitOptions{}, stock, 66, nil, own(nil)), ChestAsk(stock, 66, nil, own(nil)); !reflect.DeepEqual(got, old) {
		t.Errorf("plain ask %v, old %v", got, old)
	}
}

// THE NATURE RULE (a user request: "if accounts dont have nature runes they shouldnt take things to alch"): the
// noted rubies the chest round carries to alch on its walks are drawn and asked for only against Nature runes.
func TestChestRubiesAreAskedAndDrawnOnlyAgainstNatures(t *testing.T) {
	own := func(m map[string]int) func(string) int { return func(item string) int { return m[item] } }
	asked := func(lines []Line) map[string]int {
		out := map[string]int{}
		for _, l := range lines {
			out[l.Item] = l.Qty
		}
		return out
	}
	rubiesOnly := stockMap(map[string]int{"Ruby": 5000, "Diamond": 300})
	withNatures := stockMap(map[string]int{"Ruby": 5000, "Nature rune": 90000})

	// The mule holds rubies and no natures and the own bank none: the ask is for nothing.
	if lines := ChestAlchAsk(rubiesOnly, own(nil)); len(lines) != 0 {
		t.Errorf("no natures at the mule or in the bank, yet asked %v", lines)
	}
	// The own bank holds natures: the rubies are asked against them, the natures are not (the bank has them).
	got := asked(ChestAlchAsk(rubiesOnly, own(map[string]int{"Nature rune": ChestNaturesCarry * 4})))
	if got["Ruby"] != ChestFodderAsk || got["Nature rune"] != 0 {
		t.Errorf("natures in the own bank: asked %v, want the rubies alone", got)
	}
	// The mule holds natures: the same ask takes both.
	got = asked(ChestAlchAsk(withNatures, own(nil)))
	if got["Ruby"] != ChestFodderAsk || got["Nature rune"] != ChestNaturesAsk {
		t.Errorf("natures at the mule: asked %v, want both", got)
	}
	// The draw: the same bank with rubies and no natures draws no noted gems, nor natures; with natures, both.
	ids := chestExtras()
	want := ChestKitWantFor(ChestKitOptions{Alch: true}, false, 66, nil, false, 300)
	noNatures := []BankRow{{989, 300}, {995, 5000}, {1603, 40}}
	if noted := PlanChestNotedDraws(noNatures, ids, want); len(noted) != 0 {
		t.Errorf("rubies drawn with no Nature rune in the bank: %+v", noted)
	}
	withNaturesBank := append(append([]BankRow(nil), noNatures...), BankRow{561, 3})
	if noted := PlanChestNotedDraws(withNaturesBank, ids, want); len(noted) != 1 || noted[0].Item != "Ruby" || noted[0].Count != 40 {
		t.Errorf("rubies not drawn against 3 natures: %+v", noted)
	}
	if draws := PlanChestDraws(withNaturesBank, ids, want); len(draws) == 0 {
		t.Error("the natures themselves are drawn")
	}
	// A kit that wants no natures (the options' Alch off) draws no gems either, bank as it may be.
	if noted := PlanChestNotedDraws(withNaturesBank, ids, ChestKitWantFor(ChestKitOptions{}, false, 66, nil, false, 300)); len(noted) != 0 {
		t.Errorf("alchemy off: %+v", noted)
	}
	// A kit whose ids name no Nature rune draws no gems.
	noNatureIDs := ids
	noNatureIDs.Extras = ChestExtrasFor(nil, nil, map[string][]int{"Ruby": {1603}}, map[string][]int{"Ruby": {1604}}, 0)
	if noted := PlanChestNotedDraws(withNaturesBank, noNatureIDs, want); len(noted) != 0 {
		t.Errorf("a kit with no Nature rune ids drew gems: %+v", noted)
	}
}

func TestNextChestStepAlchsBeforeItEats(t *testing.T) {
	v := ChestView{Asked: true, PostBanked: true, Where: ChestAtChest, KeysPack: 3, Room: false, LootInPack: true,
		Alchables: 4, AlchReady: true, AlchForRoom: true, Fruit: 2, EatForRoom: true}
	if got := NextChestStep(v); got != ChestStepAlch {
		t.Errorf("no room, loot to alch: %s", got)
	}
	v.AlchReady = false // no nature rune, no fire: the fruit is eaten as before
	if got := NextChestStep(v); got != ChestStepEat {
		t.Errorf("alchemy not ready: %s", got)
	}
	v.AlchReady, v.AlchForRoom, v.EatForRoom = true, false, false // alching would not make room
	if got := NextChestStep(v); got != ChestStepToFalador {
		t.Errorf("neither helps: %s", got)
	}
	v.Room = true
	if got := NextChestStep(v); got != ChestStepOpen {
		t.Errorf("room: %s", got)
	}
	// Alchemy is for the chest: elsewhere the trip goes on to the bank.
	v = ChestView{Asked: true, PostBanked: true, Where: ChestAtOther, KeysPack: 3, LootInPack: true, Alchables: 4, AlchReady: true, AlchForRoom: true}
	if got := NextChestStep(v); got == ChestStepAlch {
		t.Errorf("not at the chest: %s", got)
	}
}

func TestChestRoomWithAlch(t *testing.T) {
	none := func(int) int { return 0 }
	if ChestRoomWithAlch(0, 0, none) {
		t.Error("a full pack with nothing to alch has no room")
	}
	worst := lootchestfisherplan.WorstCaseNewSlots(none)
	if !ChestRoomWithAlch(0, worst-1, none) || ChestRoomWithAlch(0, worst-2, none) {
		t.Errorf("the worst reward needs %d slots, one of them the key's own", worst)
	}
	if ChestRoomWithAlch(0, -3, none) {
		t.Error("negative alchables are none")
	}
}

func TestChestLedgerRemoveOwed(t *testing.T) {
	var c ChestLedger
	c.AddOwed(1603, 2)
	c.AddOwed(1199, 1)
	c.RemoveOwed(1603, 1)
	if c.Owed["1603"] != 1 || c.OwedTotal() != 2 {
		t.Errorf("%v", c.Owed)
	}
	c.RemoveOwed(1603, 5)
	if _, ok := c.Owed["1603"]; ok || c.OwedTotal() != 1 {
		t.Errorf("never below zero: %v", c.Owed)
	}
	c.RemoveOwed(1199, 1)
	if c.Owed != nil {
		t.Errorf("an empty ledger owes nothing: %v", c.Owed)
	}
	c.RemoveOwed(5, 1) // nothing owed: no panic on a nil map
}

func TestRingOfDuelingFireStaffPaysFire(t *testing.T) {
	// The Staff of fire pays Varrock Teleport's Fire rune: the way home needs none when it is worn.
	got := ChestRunes(39, RunesPaidByStaff([]string{"Staff of fire"}, true))
	if got[RuneFire] != 0 || got[RuneAir] != 21 {
		t.Errorf("runes with a Staff of fire: %v", got)
	}
}

// ---- the content the alchemy and the route rest on (RSB0T_CONTENT, skipped without it) ----

func TestChestAlchemyIsContents(t *testing.T) {
	alch := contentFile(t, "skill_magic/scripts/spells/alchemy.rs2")
	for _, want := range []string{
		"[opheldt,magic:highlvl_alchemy]@magic_spell_high_alch(^highlvl_alchemy, last_item);",
		"def_int $profit = max(scale(6, 10, oc_cost($item)), 1);",
		"inv_del(inv, $item, 1);",
		"inv_add(inv, coins, $profit);",
		"p_delay(3);",
	} {
		if !strings.Contains(alch, want) {
			t.Errorf("alchemy.rs2 lacks %q", want)
		}
	}
	spells := contentFile(t, "skill_magic/configs/magic_spells.dbrow")
	b := block(spells, "magic_spell_high_alch")
	if !strings.Contains(b, "data=levelrequired,"+strconv.Itoa(ChestAlchMagic)+"\n") || !strings.Contains(b, "data=runesrequired,naturerune,1,firerune,5") {
		t.Errorf("[magic_spell_high_alch]: %q", b)
	}
	chest := contentFile(t, "areas/area_taverly/scripts/crystal_chest.rs2")
	for _, want := range []string{
		"inv_add(inv, uncut_dragonstone, 1);",
		"inv_add(inv, ruby, 2);", "inv_add(inv, diamond, 2);",
		"inv_add(inv, adamant_sq_shield, 1);", "inv_add(inv, rune_platelegs, 1);", "inv_add(inv, rune_plateskirt, 1);",
	} {
		if !strings.Contains(chest, want) {
			t.Errorf("crystal_chest.rs2 lacks %q", want)
		}
	}
	// The Games necklace's menu and its first option.
	nm := contentFile(t, "general/scripts/enchanted_jewellry/necklace_of_minigames.rs2")
	for _, want := range []string{
		"[opheld4,_necklace_of_minigames]",
		`~p_choice2_header("Burthorpe Games Rooms.", 1, "Nowhere.", 2,`,
		"~player_teleport_normal(map_findsquare(0_34_77_31_12, 0, 2, ^map_findsquare_lineofwalk));",
	} {
		if !strings.Contains(nm, want) {
			t.Errorf("necklace_of_minigames.rs2 lacks %q", want)
		}
	}
}
