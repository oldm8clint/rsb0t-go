package mastercraftplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

// The names the kit uses are the names the spells use: a rename in scarabplan must fail here, not
// quietly stop the runes being found.
func TestKitNamesAreTheSpellsNames(t *testing.T) {
	names := map[string]bool{}
	for _, tp := range fleetplan.Teleports {
		for _, r := range tp.Spell.Runes {
			names[r.Name] = true
		}
	}
	for _, n := range []string{RuneAir, RuneWater, RuneLaw} {
		if !names[n] {
			t.Errorf("%q is not a rune of any teleport in scarabplan.Teleports", n)
		}
	}
	if ChestFruit != "Strange fruit" || fleetplan.StrangeFruitID != 464 {
		t.Errorf("the fruit is %q (%d)", ChestFruit, fleetplan.StrangeFruitID)
	}
	if ChestFares != 120 {
		t.Errorf("the ships' fare is %d", ChestFares)
	}
}

// Content's magic_staff_table: a wielded staff pays for its element, the battlestaves only on a
// members world.
func TestRunesPaidByStaff(t *testing.T) {
	for _, c := range []struct {
		name    string
		worn    []string
		members bool
		want    map[string]bool
	}{
		{"nothing worn", nil, true, map[string]bool{}},
		{"a sword", []string{"Rune scimitar", "Amulet of power"}, true, map[string]bool{}},
		{"staff of air", []string{"Staff of air"}, false, map[string]bool{RuneAir: true}},
		{"staff of air among armour, case and spaces ignored", []string{"Rune platebody", " staff OF air "}, true, map[string]bool{RuneAir: true}},
		{"air battlestaff on a members world", []string{"Air battlestaff"}, true, map[string]bool{RuneAir: true}},
		{"air battlestaff on a free world pays nothing", []string{"Air battlestaff"}, false, map[string]bool{}},
		{"mystic air staff", []string{"Mystic air staff"}, true, map[string]bool{RuneAir: true}},
		{"staff of water", []string{"Staff of water"}, false, map[string]bool{RuneWater: true}},
		{"mystic water staff", []string{"Mystic water staff"}, true, map[string]bool{RuneWater: true}},
		{"a fire staff pays the fire runes (Varrock Teleport, High Level Alchemy)", []string{"Staff of fire"}, true, map[string]bool{RuneFire: true}},
		{"a lava battlestaff pays the fire runes on a members world", []string{"Lava battlestaff"}, true, map[string]bool{RuneFire: true}},
		{"a lava battlestaff on a free world pays nothing", []string{"Lava battlestaff"}, false, map[string]bool{}},
		{"an earth staff pays neither", []string{"Staff of earth"}, true, map[string]bool{}},
	} {
		if got := RunesPaidByStaff(c.worn, c.members); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// a user request: "masters taking air runes when it has an air staff equipped".
func TestChestRunesSkipWhatAStaffPays(t *testing.T) {
	air := map[string]bool{RuneAir: true}
	water := map[string]bool{RuneWater: true}
	for _, c := range []struct {
		name  string
		magic int
		paid  map[string]bool
		want  map[string]int
	}{
		// Six Falador Teleports (water, three air, law each) and one Varrock Teleport home (fire, three
		// air, law).
		{"magic 39, no staff", 39, nil, map[string]int{RuneAir: 21, RuneWater: 6, RuneLaw: 7, RuneFire: 1}},
		{"magic 39, air staff: no Air rune", 39, air, map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}},
		{"magic 39, water staff", 39, water, map[string]int{RuneAir: 21, RuneLaw: 7, RuneFire: 1}},
		{"magic 39, both", 39, map[string]bool{RuneAir: true, RuneWater: true}, map[string]int{RuneLaw: 7, RuneFire: 1}},
		{"magic 45, no staff: Camelot's five Air too", 45, nil, map[string]int{RuneAir: 23, RuneWater: 6, RuneLaw: 7}},
		{"magic 45, air staff", 45, air, map[string]int{RuneWater: 6, RuneLaw: 7}},
		{"magic 17, nothing castable", 17, air, map[string]int{}},
	} {
		if got := ChestRunes(c.magic, c.paid); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// The kit has a slot less for it.
	if a, b := ChestKitSlots(39, nil), ChestKitSlots(39, air); a != 6+4+1+ChestFruitCarry || b != a-1 {
		t.Errorf("kit slots %d without the staff, %d with it", a, b)
	}
	// And the cast needs no Air rune in the pack.
	have := map[string]int{RuneWater: 1, RuneLaw: 1}
	held := func(r string) int { return have[r] }
	if CanCastWith(FaladorTeleport, 39, nil, held) {
		t.Error("without the staff the cast needs three Air runes")
	}
	if !CanCastWith(FaladorTeleport, 39, air, held) {
		t.Error("a worn staff of air pays the Air runes: water and law are enough")
	}
	if CanCastWith(FaladorTeleport, 39, air, func(string) int { return 0 }) {
		t.Error("the staff does not pay the Water or the Law rune")
	}
}

// The ask leaves the Air rune out when a staff pays for it, and asks for the fruit.
func TestChestAskWithAStaffAndFruit(t *testing.T) {
	stock := stockMap(map[string]int{ChestKeyItem: 313, RuneAir: 900, RuneWater: 900, RuneLaw: 900, RuneFire: 900, ChestFruit: 4000})
	air := map[string]bool{RuneAir: true}
	got := ChestAsk(stock, 39, air, nil)
	want := []Line{{ChestKeyItem, 313}, {RuneFire, 1}, {RuneLaw, 7}, {RuneWater, 6}, {ChestFruit, ChestFruitAsk}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with the staff: %+v, want %+v", got, want)
	}
	got = ChestAsk(stock, 39, nil, nil)
	want = []Line{{ChestKeyItem, 313}, {RuneAir, 21}, {RuneFire, 1}, {RuneLaw, 7}, {RuneWater, 6}, {ChestFruit, ChestFruitAsk}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("without it: %+v, want %+v", got, want)
	}
}

// "should request about ~1000 not definitive": a thousand in the bank, and whatever the mule has.
func TestChestFruitWanted(t *testing.T) {
	for _, c := range []struct{ published, own, want int }{
		{4000, 0, 1000},
		{4000, 400, 600},
		{4000, 1000, 0},
		{4000, 5000, 0},
		{250, 0, 250},
		{250, 100, 250},
		{0, 0, 0},
		{-5, 0, 0},
		{4000, -3, 1000},
	} {
		if got := ChestFruitWanted(c.published, c.own); got != c.want {
			t.Errorf("published %d own %d: %d, want %d", c.published, c.own, got, c.want)
		}
	}
	// A mule with none, or one that has not published the row, never stops the round: the ask is
	// the keys alone.
	ask := ChestAsk(stockMap(map[string]int{ChestKeyItem: 200}), 17, nil, func(string) int { return 0 })
	if len(ask) != 1 || ask[0].Item != ChestKeyItem {
		t.Errorf("no fruit published: %+v", ask)
	}
	// An account whose bank already holds the thousand asks for none.
	own := map[string]int{ChestFruit: 1200}
	ask = ChestAsk(stockMap(map[string]int{ChestKeyItem: 200, ChestFruit: 5000}), 17, nil, func(i string) int { return own[i] })
	if len(ask) != 1 {
		t.Errorf("a full bank still asked for fruit: %+v", ask)
	}
	// Strange fruit has a bank note, which the mule hands over for a big line.
	if !strings.EqualFold(ChestFruit, fleetplan.StrangeFruit) {
		t.Error("the ask names the fruit as the mule's catalogue does")
	}
}

// The walk eats a fruit when run energy is low, never when it is not known or no fruit is held.
func TestChestEatFruit(t *testing.T) {
	for _, c := range []struct {
		energy int
		known  bool
		held   int
		want   bool
	}{
		{15, true, 3, true},
		{ChestFruitRunAt, true, 1, true},
		{ChestFruitRunAt + 1, true, 3, false},
		{0, true, 3, true},
		{100, true, 3, false},
		{10, true, 0, false},
		{10, false, 3, false},
	} {
		if got := ChestEatFruit(c.energy, c.known, c.held); got != c.want {
			t.Errorf("%+v: %v, want %v", c, got, c.want)
		}
	}
	// A bite never wastes any of its 30% against the 100% ceiling.
	if ChestFruitRunAt+30 > 100 {
		t.Errorf("a fruit at %d%% would overflow the bar", ChestFruitRunAt)
	}
}

func TestChestKitWant(t *testing.T) {
	air := map[string]bool{RuneAir: true}
	for _, c := range []struct {
		name    string
		atPost  bool
		magic   int
		paid    map[string]bool
		expired bool
		banked  int
		want    KitWant
	}{
		{"Falador, magic 39", false, 39, nil, false, 300,
			KitWant{Keys: 6, Runes: map[string]int{RuneAir: 21, RuneWater: 6, RuneLaw: 7, RuneFire: 1}, Coins: 120, Fruit: 3}},
		{"Falador, air staff", false, 39, air, false, 300,
			KitWant{Keys: 6, Runes: map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}, Coins: 120, Fruit: 3}},
		{"the last keys", false, 39, air, false, 4,
			KitWant{Keys: 4, Runes: map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}, Coins: 120, Fruit: 3}},
		{"at the post, castable: the keys leave from here", true, 39, air, false, 300,
			KitWant{Keys: 6, Runes: map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}, Coins: 120, Fruit: 3}},
		{"at the post, walking: the long walk is made with no keys", true, 17, nil, false, 300,
			KitWant{Keys: 0, Runes: map[string]int{}, Coins: 120, Fruit: 3}},
		{"the way home draws no keys", false, 39, air, true, 300,
			KitWant{Keys: 0, Runes: map[string]int{RuneWater: 6, RuneLaw: 7, RuneFire: 1}, Coins: 120, Fruit: 3}},
		{"Camelot's way home at 45", false, 45, air, true, 300,
			KitWant{Keys: 0, Runes: map[string]int{RuneWater: 6, RuneLaw: 7}, Coins: 120, Fruit: 3}},
	} {
		if got := ChestKitWant(c.atPost, c.magic, c.paid, c.expired, c.banked); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

var testKitIDs = KitIDs{
	Keys:  []int{989},
	Runes: map[string][]int{RuneAir: {556}, RuneWater: {555}, RuneLaw: {563}},
	Coins: []int{617, 995},
	Fruit: []int{464},
}

// The draws come out of the bank as it stands with the pack banked: the first id of each item the
// bank holds, no more than the row holds, never a zero (which would be Withdraw-All).
func TestPlanChestDraws(t *testing.T) {
	want := KitWant{Keys: 6, Runes: map[string]int{RuneAir: 18, RuneWater: 6, RuneLaw: 6}, Coins: 120, Fruit: 3}
	bank := []BankRow{{989, 300}, {556, 500}, {555, 3}, {563, 6}, {464, 2}, {995, 4000000}, {1631, 90}}
	got := PlanChestDraws(bank, testKitIDs, want)
	exp := []KitDraw{
		{ChestKeyItem, 989, 6}, {RuneAir, 556, 18}, {RuneLaw, 563, 6}, {RuneWater, 555, 3},
		{ChestFruit, 464, 2}, {"Coins", 995, 120},
	}
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("got  %+v\nwant %+v", got, exp)
	}
	// Nothing banked, nothing drawn; and a want of 0 is not Withdraw-All.
	if got := PlanChestDraws(nil, testKitIDs, want); len(got) != 0 {
		t.Errorf("an empty bank: %+v", got)
	}
	if got := PlanChestDraws(bank, testKitIDs, KitWant{Runes: map[string]int{RuneAir: 0}}); len(got) != 0 {
		t.Errorf("a want of nothing: %+v", got)
	}
	// Coins come from whichever of the two ids the bank holds.
	if got := PlanChestDraws([]BankRow{{617, 50}}, testKitIDs, KitWant{Coins: 120}); !reflect.DeepEqual(got, []KitDraw{{"Coins", 617, 50}}) {
		t.Errorf("the other coin id: %+v", got)
	}
	// A staff pays the Air: no Air rune is drawn though the bank holds thousands.
	paid := ChestKitWant(false, 39, map[string]bool{RuneAir: true}, false, 300)
	for _, d := range PlanChestDraws(bank, testKitIDs, paid) {
		if d.Item == RuneAir {
			t.Errorf("drew Air runes with a staff worn: %+v", d)
		}
	}
}

// A visit has something to bank when the pack holds anything beyond the kit: the chest's coins on top
// of the fare, its runes on top of the spells', fruit beyond the trip's, any other item, a note.
func TestChestPackHasLoot(t *testing.T) {
	runes := map[string]int{RuneAir: 18, RuneWater: 6, RuneLaw: 6}
	kit := []BankRow{{989, 1}, {989, 1}, {989, 1}, {556, 18}, {555, 6}, {563, 6}, {995, 120}, {464, 1}, {464, 1}, {464, 1}}
	if ChestPackHasLoot(kit, testKitIDs, runes) {
		t.Error("the kit itself is not loot")
	}
	if ChestPackHasLoot(nil, testKitIDs, runes) {
		t.Error("an empty pack has no loot")
	}
	if ChestPackHasLoot([]BankRow{{989, 1}}, testKitIDs, runes) {
		t.Error("keys alone are the kit")
	}
	if ChestPackHasLoot([]BankRow{{995, 80}}, testKitIDs, runes) {
		t.Error("less than the fare is no loot")
	}
	for _, c := range []struct {
		name string
		pack []BankRow
	}{
		{"coins from the chest on top of the fare", append(append([]BankRow(nil), kit...), BankRow{995, 2000})},
		{"coins alone beyond the fare", []BankRow{{995, 2120}}},
		{"runes from the bundle on top of the spells'", []BankRow{{556, 68}}},
		{"an item that is not the kit's", append(append([]BankRow(nil), kit...), BankRow{1631, 1})},
		{"a note", []BankRow{{990, 300}}},
		{"a fourth fruit", []BankRow{{464, 1}, {464, 1}, {464, 1}, {464, 1}}},
		{"a rune no spell needs", []BankRow{{557, 50}}},
	} {
		if !ChestPackHasLoot(c.pack, testKitIDs, runes) {
			t.Errorf("%s: not seen as loot", c.name)
		}
	}
	// With an air staff the kit has no Air rune: any in the pack is the chest's.
	staffRunes := ChestRunes(39, map[string]bool{RuneAir: true})
	if !ChestPackHasLoot([]BankRow{{556, 50}}, testKitIDs, staffRunes) {
		t.Error("Air runes with a staff worn are loot, to be banked")
	}
}

// Eating the fruit empties their slots.
func TestChestRoomWithFruit(t *testing.T) {
	empty := func(int) int { return 0 }
	worst := lootchestfisherplan.WorstCaseNewSlots(empty)
	if ChestRoom(worst-3, empty) {
		t.Fatal("the rule is the worst case less the key's slot")
	}
	if ChestRoomWithFruit(worst-3, 1, empty) || !ChestRoomWithFruit(worst-3, 2, empty) {
		t.Error("two fruit eaten make two slots")
	}
	if !ChestRoomWithFruit(worst-1, 0, empty) || ChestRoomWithFruit(worst-2, -4, empty) {
		t.Error("no fruit, no change; a negative count is no fruit")
	}
}

// Keys in the bank are the ledger's after a restart (the session has not opened the bank), the
// session's otherwise; the pack's are always added.
func TestChestOwnKeys(t *testing.T) {
	for _, c := range []struct {
		known                  bool
		bank, ledger, pack, wt int
	}{
		{true, 40, 999, 0, 40},
		{true, 0, 999, 0, 0},
		{false, 0, 141, 0, 141},
		{false, 0, 141, 5, 146},
		{true, 12, 0, 6, 18},
		{false, 0, -3, -1, 0},
	} {
		if got := ChestOwnKeys(c.known, c.bank, c.ledger, c.pack); got != c.wt {
			t.Errorf("%+v: %d", c, got)
		}
	}
}

// a user request: continue what it was doing, before just heading to the mule upon startup. A
// resume starts the trips where the account stands only on the Falador side, and only with keys.
func TestChestResumesOnlyOnTheFaladorSide(t *testing.T) {
	resume := Decision{Mode: ModeChest, Resume: true}
	fresh := Decision{Mode: ModeChest}
	for _, c := range []struct {
		name string
		dec  Decision
		keys int
		side bool
		want bool
	}{
		{"keys of its own on the Falador side", resume, 141, true, true},
		{"the one key", resume, 1, true, true},
		{"no keys: loot owed only, the mule's post first", resume, 0, true, false},
		{"at the post or anywhere else: ask there, draw the kit", resume, 141, false, false},
		{"a round decided on the mule's keys asks first", fresh, 0, true, false},
		{"not a chest round", Decision{Mode: ModeGems, Resume: true}, 141, true, false},
	} {
		if got := ChestResumes(c.dec, c.keys, c.side); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// A round under way carries on before a new round starts, so a restart does not walk the account to
// the mule for the arrows and back: it is the case the live fleet was in at 23:15 on 5 Oct.
func TestDecideWithChestResumesTheRoundUnderWay(t *testing.T) {
	now := time.Date(2026, 10, 5, 23, 15, 0, 0, time.UTC)
	set := orderSettings()
	// A gem round is due on the mule's bank; a chest round in the middle of its trips comes first.
	s := chestSnap(now, 400)
	s.Stock = stockMap(map[string]int{ChestKeyItem: 400, "Uncut sapphire": 900, "Chisel": 1})
	if d := DecideWithChest(set, s, ChestInput{}); d.Mode != ModeGems {
		t.Fatalf("with no keys of its own the gems come first: %+v", d)
	}
	if d := DecideWithChest(set, s, ChestInput{OwnKeys: 141}); d.Mode != ModeGems {
		t.Errorf("keys in the bank but the account away from the chest: %+v", d)
	}
	d := DecideWithChest(set, s, ChestInput{OwnKeys: 141, OnFaladorSide: true})
	if d.Mode != ModeChest || !d.Resume || !strings.Contains(d.Why, "under way") {
		t.Errorf("under way on the Falador side: %+v", d)
	}
	// A hand-over of products pending is the mule's and still first.
	s.Ledger.PendingDeliver = true
	if d := DecideWithChest(set, s, ChestInput{OwnKeys: 141, OnFaladorSide: true}); d.Mode != ModeDeliver {
		t.Errorf("a pending hand-over first: %+v", d)
	}
	// The waits still hold it off: a round backed off after failures, a pause, the mule away.
	s = chestSnap(now, 400)
	s.Ledger.Fail(ModeChest, now, set)
	if d := DecideWithChest(set, s, ChestInput{OwnKeys: 141, OnFaladorSide: true}); d.Mode == ModeChest {
		t.Errorf("backing off: %+v", d)
	}
	s = chestSnap(now, 400)
	s.MuleHeard = false
	if d := DecideWithChest(set, s, ChestInput{OwnKeys: 141, OnFaladorSide: true}); d.Mode == ModeChest {
		t.Errorf("mule not heard: %+v", d)
	}
	set.CrystalChest = false
	if d := DecideWithChest(set, chestSnap(now, 400), ChestInput{OwnKeys: 141, OnFaladorSide: true}); d.Mode == ModeChest {
		t.Errorf("switched off: %+v", d)
	}
}

// A restart knows the keys in the bank only from the ledger, so it has to be written and read: a
// ledger from before the field was added reads as none, which the resume answers with the pack's keys
// alone (the next bank visit writes the count).
func TestTheLedgerRemembersTheKeysInTheBank(t *testing.T) {
	led := Ledger{Chest: ChestLedger{KeysBank: 141, Owed: map[string]int{"995": 2000}}}
	raw, err := json.Marshal(led)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"keysBank":141`) {
		t.Fatalf("not written: %s", raw)
	}
	var back Ledger
	if err := json.Unmarshal(raw, &back); err != nil || back.Chest.KeysBank != 141 || back.Chest.OwedTotal() != 2000 {
		t.Fatalf("read back %+v (%v)", back.Chest, err)
	}
	var old Ledger
	if err := json.Unmarshal([]byte(`{"chest":{"owed":{"995":5},"rounds":3}}`), &old); err != nil || old.Chest.KeysBank != 0 || old.Chest.Rounds != 3 {
		t.Fatalf("a ledger from before the field: %+v (%v)", old.Chest, err)
	}
	if ChestOwnKeys(false, 0, old.Chest.KeysBank, 6) != 6 {
		t.Error("the pack's keys still resume a round when the ledger knows nothing of the bank")
	}
}

// A second bite inside the third tick after the first is refused by content, so the next one is held
// until ChestBiteGap after it, and not held at all when that long has passed (or none was ever made).
func TestChestBiteWait(t *testing.T) {
	for _, c := range []struct{ since, want time.Duration }{
		{0, ChestBiteGap},
		{500 * time.Millisecond, ChestBiteGap - 500*time.Millisecond},
		{ChestBiteGap - time.Millisecond, time.Millisecond},
		{ChestBiteGap, 0},
		{time.Hour, 0},
		{-time.Second, ChestBiteGap},
	} {
		if got := ChestBiteWait(c.since); got != c.want {
			t.Errorf("since %v: wait %v, want %v", c.since, got, c.want)
		}
	}
}
