package mastercraftplan

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var jewelNow = time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)

// jewelStock is the mule's bank on 6 Oct 2026 as far as the round cares: 371 gold bars (the limit),
// 59,102 cut Sapphires, 4,374 Cosmic runes and 211,521 Water runes, and no Games necklace.
func jewelStock() map[string]int {
	return map[string]int{GoldBar: 371, CutSapphire: 59102, RuneCosmic: 4374, RuneWater: 211521}
}

// jewelSnap is the main account on that day: Crafting 92, Magic 40, with the mule here and its bank read.
func jewelSnap(stock map[string]int) Snapshot {
	return Snapshot{
		Now: jewelNow, MuleHeard: true, MuleSameWorld: true, Stock: stockMap(stock),
		CraftingLevel: 92, MagicLevel: 40, FletchingLevel: 99, FletchingKnown: true,
		Ledger: &Ledger{}, OwnJewelleryKnown: true,
	}
}

// THE OWNER'S RULE (6 Oct 2026 13:40): the round runs only when the mule holds no Games necklace and no
// Ring of dueling of any charge.
func TestJewelleryRunsOnlyWhenTheMuleHoldsNone(t *testing.T) {
	set := DefaultSettings()
	d := Decide(set, jewelSnap(jewelStock()))
	if d.Mode != ModeJewellery || d.Resume || d.Jewellery.N != DefaultJewelleryBatch || !strings.Contains(d.Why, "no Games necklace or Ring of dueling") {
		t.Fatalf("the mule holds none: %+v", d)
	}
	// Any one of the sixteen names, at any count, holds it off, and says why.
	names := JewelleryDemandNames()
	if len(names) != 16 || names[0] != "Games necklace(8)" || names[7] != "Games necklace(1)" || names[8] != "Ring of dueling(8)" || names[15] != "Ring of dueling(1)" {
		t.Fatalf("the demand names: %v", names)
	}
	for _, name := range names {
		stock := jewelStock()
		stock[name] = 1
		d := Decide(set, jewelSnap(stock))
		if d.Mode == ModeJewellery {
			t.Errorf("%s in the mule's bank: %+v", name, d)
		}
		if !strings.Contains(d.Why, "the mule holds 1 Games necklace(s) and Ring(s) of dueling") {
			t.Errorf("%s: the swarm decision does not say why: %q", name, d.Why)
		}
	}
	// A different item that merely looks alike does not.
	stock := jewelStock()
	stock["Sapphire necklace"], stock["Ring of recoil"], stock["Games necklace"], stock["Ring of dueling"] = 90, 90, 90, 90
	if d := Decide(set, jewelSnap(stock)); d.Mode != ModeJewellery {
		t.Errorf("only the charged names count: %+v", d)
	}
	// Both kinds at once add up.
	stock = jewelStock()
	stock["Games necklace(3)"], stock["Ring of dueling(8)"] = 4, 6
	if got := JewelleryDemandHeld(stockMap(stock)); got != 10 {
		t.Errorf("demand held %d, want 10", got)
	}
}

func TestJewelleryBatchSize(t *testing.T) {
	set := DefaultSettings()
	if set.JewelleryBatch != 50 {
		t.Fatalf("the default batch is the owner's 50: %d", set.JewelleryBatch)
	}
	for _, tc := range []struct {
		name  string
		mod   func(m map[string]int)
		batch int
		paid  bool
		want  int
	}{
		{"the batch", func(m map[string]int) {}, 50, false, 50},
		{"a bigger batch is the gold bars", func(m map[string]int) {}, 1000, false, 371},
		{"gold is the limit", func(m map[string]int) { m[GoldBar] = 12 }, 50, false, 12},
		{"sapphires", func(m map[string]int) { m[CutSapphire] = 7 }, 50, false, 7},
		{"cosmic", func(m map[string]int) { m[RuneCosmic] = 30 }, 50, false, 30},
		{"water", func(m map[string]int) { m[RuneWater] = 3 }, 50, false, 3},
		{"no water with a Staff of water", func(m map[string]int) { delete(m, RuneWater) }, 50, true, 50},
		{"no gold", func(m map[string]int) { delete(m, GoldBar) }, 50, false, 0},
		{"no sapphires", func(m map[string]int) { delete(m, CutSapphire) }, 50, false, 0},
		{"no cosmic", func(m map[string]int) { delete(m, RuneCosmic) }, 50, false, 0},
		{"no water", func(m map[string]int) { delete(m, RuneWater) }, 50, false, 0},
	} {
		m := jewelStock()
		tc.mod(m)
		set.JewelleryBatch = tc.batch
		p := PlanJewelleryBatch(set, stockMap(m), tc.paid)
		if p.N != tc.want {
			t.Errorf("%s: N %d, want %d (%s)", tc.name, p.N, tc.want, p.Why)
		}
		if tc.want == 0 && !strings.HasPrefix(p.Why, "the mule holds no ") {
			t.Errorf("%s: no reason: %q", tc.name, p.Why)
		}
	}
	// Nothing published is not a stock of zero.
	if p := PlanJewelleryBatch(DefaultSettings(), nil, false); p.N != 0 || !strings.Contains(p.Why, "not published") {
		t.Errorf("no bank: %+v", p)
	}
}

func TestJewelleryOffAndGated(t *testing.T) {
	set := DefaultSettings()
	// 0 turns it off.
	set.JewelleryBatch, set.RingBatch = 0, 0
	if d := Decide(set, jewelSnap(jewelStock())); d.Mode == ModeJewellery || !strings.Contains(d.Why, "jewellery is off") {
		t.Errorf("batch 0: %+v", d)
	}
	// A free world: the necklace is a members item.
	set = DefaultSettings()
	set.MembersWorld = false
	if d := Decide(set, jewelSnap(jewelStock())); d.Mode == ModeJewellery || !strings.Contains(d.Why, "members") {
		t.Errorf("free world: %+v", d)
	}
	// Levels.
	set = DefaultSettings()
	s := jewelSnap(jewelStock())
	s.MagicLevel = 6
	if d := Decide(set, s); d.Mode == ModeJewellery || !strings.Contains(d.Why, "Lvl-1 Enchant") {
		t.Errorf("Magic 6: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.CraftingLevel = 19
	if d := Decide(set, s); d.Mode == ModeJewellery || !strings.Contains(d.Why, "Sapphire necklace") {
		t.Errorf("Crafting 19: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.MagicLevel, s.CraftingLevel = EnchantLevel, NecklaceLevel
	if d := Decide(set, s); d.Mode != ModeJewellery {
		t.Errorf("the exact levels: %+v", d)
	}
	// The mule not heard, on another world, or its bank not published: nothing, and a bank not
	// published is inconclusive.
	s = jewelSnap(jewelStock())
	s.Stock = nil
	if d := Decide(set, s); d.Mode == ModeJewellery || !d.Inconclusive {
		t.Errorf("no bank: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.MuleHeard = false
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("mule not heard: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.MuleSameWorld = false
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("mule on another world: %+v", d)
	}
	// A mule that cannot fund a batch is a "no", with its reason.
	m := jewelStock()
	delete(m, GoldBar)
	if d := Decide(set, jewelSnap(m)); d.Mode == ModeJewellery || !strings.Contains(d.Why, "cannot fund a batch: the mule holds no Gold bar") {
		t.Errorf("no gold: %+v", d)
	}
}

// The jewellery round comes before every other round and after a hand-over; the other rounds follow
// once the mule holds a necklace.
func TestJewelleryComesFirst(t *testing.T) {
	set := DefaultSettings()
	set.GemThreshold, set.BowThreshold = 500, 100 // the order is the point, not the thresholds (20,000 since 6 Oct)
	m := jewelStock()
	m["Uncut sapphire"], m["Chisel"] = 900, 1
	m["Maple longbow"], m["Bow string"] = 500, 500
	s := jewelSnap(m)
	if d := Decide(set, s); d.Mode != ModeJewellery {
		t.Fatalf("with gems and bows due: %+v", d)
	}
	m["Games necklace(8)"] = 50 // the batch has been handed over
	if d := Decide(set, jewelSnap(m)); d.Mode != ModeGems {
		t.Fatalf("the mule holds necklaces: %+v, want the gem round", d)
	}
	// A pending hand-over comes before it, a gave-up suspension holds everything.
	s = jewelSnap(jewelStock())
	s.Ledger.PendingDeliver = true
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Errorf("hand-over first: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.Ledger.Backoffs = map[string]Backoff{string(ModeDeliver): {Fails: set.MaxHandoverFailures, Until: jewelNow.Add(time.Hour)}}
	if d := Decide(set, s); d.Mode == ModeJewellery || !d.GaveUp {
		t.Errorf("given up: %+v", d)
	}
	// Paused, or backing off, it waits like any round.
	s = jewelSnap(jewelStock())
	s.Ledger.Jewellery.PauseUntil = jewelNow.Add(time.Hour)
	if d := Decide(set, s); d.Mode == ModeJewellery || !strings.Contains(d.Why, "jewellery rounds are paused until") {
		t.Errorf("paused: %+v", d)
	}
	s = jewelSnap(jewelStock())
	s.Ledger.Fail(ModeJewellery, jewelNow, set)
	if d := Decide(set, s); d.Mode == ModeJewellery || !strings.Contains(d.Why, "jewellery rounds are backing off until") {
		t.Errorf("backing off: %+v", d)
	}
	// The swarm decision names jewellery among the rounds that are off.
	m = jewelStock()
	m["Ring of dueling(2)"] = 1
	if d := Decide(set, jewelSnap(m)); d.Mode != ModeSwarm || !strings.Contains(d.Why, "Ring(s) of dueling") {
		t.Errorf("%+v", d)
	}
}

func TestJewelleryResume(t *testing.T) {
	set := DefaultSettings()
	owed := func(s *Snapshot, n int) { s.Ledger.Jewellery.AddOwed(int(GamesNecklaceID), n) }

	// Games necklaces owed to the mule: a hand-over round, whatever the mule holds, whatever the
	// setting says, until the hand-over backs off.
	m := jewelStock()
	m["Games necklace(8)"] = 40
	s := jewelSnap(m)
	owed(&s, 12)
	if d := Decide(set, s); d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "12 Games necklace(s)") {
		t.Errorf("owed: %+v", d)
	}
	off := DefaultSettings()
	off.JewelleryBatch = 0
	if d := Decide(off, s); d.Mode != ModeJewellery || !d.Resume {
		t.Errorf("owed with the setting at 0: %+v", d)
	}
	s.Ledger.Backoffs = map[string]Backoff{string(ModeDeliver): {Fails: 1, Until: jewelNow.Add(time.Hour)}}
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("owed, hand-over backing off: %+v", d)
	}

	// Sapphire necklaces in the account's own bank are enchanted, with the runes it holds.
	s = jewelSnap(m)
	s.OwnJewellery = JewelleryHeld{Necklaces: 8, Cosmic: 30, Water: 30}
	if d := Decide(set, s); d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "8 Sapphire necklace(s) to enchant") {
		t.Errorf("own necklaces: %+v", d)
	}
	s.OwnJewellery.Water = 0
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("own necklaces, no Water: %+v", d)
	}
	s.WaterPaid = true
	if d := Decide(set, s); d.Mode != ModeJewellery {
		t.Errorf("own necklaces, a Staff of water: %+v", d)
	}
	if d := Decide(off, s); d.Mode == ModeJewellery {
		t.Errorf("own necklaces with the setting at 0: %+v", d)
	}

	// A batch under way: the bank holds its materials, or has not been read since the script started.
	s = jewelSnap(m)
	s.Ledger.Jewellery.StartBatch(50)
	s.Ledger.Jewellery.Enchanted = 20
	s.OwnJewellery = JewelleryHeld{Bars: 30, Sapphires: 30, Mould: 1, Cosmic: 30, Water: 30}
	if d := Decide(set, s); d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "under way (20 made)") {
		t.Errorf("a batch under way: %+v", d)
	}
	s.OwnJewellery = JewelleryHeld{}
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("a batch under way, the bank read and empty: %+v", d)
	}
	s.OwnJewelleryKnown = false
	if d := Decide(set, s); d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "has not been read") {
		t.Errorf("a batch under way, the bank not read yet: %+v", d)
	}
	// Bars and Sapphires in the bank with no batch under way are not touched: they may be reserved.
	s = jewelSnap(m)
	s.OwnJewellery = JewelleryHeld{Bars: 30, Sapphires: 30, Mould: 1, Cosmic: 30, Water: 30}
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("own bars with no batch under way: %+v", d)
	}
	// A batch that is complete is not under way.
	s = jewelSnap(m)
	s.Ledger.Jewellery.StartBatch(50)
	s.Ledger.Jewellery.Enchanted = 50
	s.OwnJewelleryKnown = false
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("a finished batch: %+v", d)
	}
	// Levels still gate a resume of materials (the hand-over of owed necklaces does not need them).
	s = jewelSnap(m)
	s.MagicLevel = 0
	s.Ledger.Jewellery.StartBatch(50)
	s.OwnJewelleryKnown = false
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("Magic not known yet: %+v", d)
	}
	owed(&s, 3)
	if d := Decide(set, s); d.Mode != ModeJewellery || !d.Resume {
		t.Errorf("owed with Magic not known yet: %+v", d)
	}
}

func TestJewelleryLedger(t *testing.T) {
	var l JewelleryLedger
	if l.Under() || l.Left() != 0 || l.OwedTotal() != 0 {
		t.Fatalf("zero: %+v", l)
	}
	l.StartBatch(50)
	l.Enchanted = 20
	if !l.Under() || l.Left() != 30 {
		t.Fatalf("under way: under %v left %d", l.Under(), l.Left())
	}
	l.Enchanted = 50
	if l.Under() || l.Left() != 0 {
		t.Fatalf("done: under %v left %d", l.Under(), l.Left())
	}
	l.StartBatch(10)
	if l.Crafted != 0 || l.Enchanted != 0 || l.Target != 10 {
		t.Fatalf("a new batch starts the counts again: %+v", l)
	}
	l.ClearBatch()
	if l.Target != 0 {
		t.Fatalf("cleared: %+v", l)
	}
	l.AddOwed(3853, 30)
	l.AddOwed(3853, 5)
	l.AddOwed(0, 5)
	l.AddOwed(3855, 0)
	if l.OwedTotal() != 35 {
		t.Fatalf("owed %d: %v", l.OwedTotal(), l.Owed)
	}
	// 20 crossed and the bank holds 12: the rest is capped at what is there.
	l.Settle(map[int]int{3853: 20}, func(id int) int { return 12 })
	if l.OwedTotal() != 12 {
		t.Fatalf("settled %v", l.Owed)
	}
	l.Settle(map[int]int{3853: 12}, nil)
	if l.OwedTotal() != 0 || l.Owed != nil {
		t.Fatalf("all handed over: %v", l.Owed)
	}
	// Not read (nil): nothing is capped.
	l.AddOwed(3853, 7)
	l.Settle(nil, nil)
	if l.OwedTotal() != 7 {
		t.Fatalf("nothing handed, nothing read: %v", l.Owed)
	}
}

func TestJewelleryTravelRunes(t *testing.T) {
	none := map[string]bool{}
	air := map[string]bool{RuneAir: true}
	for _, tc := range []struct {
		name  string
		leg   JewelleryLeg
		magic int
		paid  map[string]bool
		spell string
		want  map[string]int
	}{
		{"Magic 36 walks to Falador", LegToFalador, 36, none, "", map[string]int{}},
		{"Magic 37", LegToFalador, 37, none, FaladorTeleport, map[string]int{RuneAir: 3, RuneWater: 1, RuneLaw: 1}},
		{"Magic 40 with a Staff of air", LegToFalador, 40, air, FaladorTeleport, map[string]int{RuneWater: 1, RuneLaw: 1}},
		{"Magic 40 with a Staff of water and air", LegToFalador, 40, map[string]bool{RuneAir: true, RuneWater: true}, FaladorTeleport, map[string]int{RuneLaw: 1}},
		{"Magic 24 walks home", LegHome, 24, none, "", map[string]int{}},
		{"Magic 25 casts Varrock home", LegHome, 25, none, VarrockTeleport, map[string]int{RuneAir: 3, RuneFire: 1, RuneLaw: 1}},
		{"Magic 40 casts Varrock home", LegHome, 40, air, VarrockTeleport, map[string]int{RuneFire: 1, RuneLaw: 1}},
		{"Magic 45 casts Camelot home", LegHome, 45, air, CamelotTeleport, map[string]int{RuneLaw: 1}},
	} {
		if got := JewelleryLegSpell(tc.leg, tc.magic); got != tc.spell {
			t.Errorf("%s: spell %q, want %q", tc.name, got, tc.spell)
		}
		if got := JewelleryTravelRunes(tc.leg, tc.magic, tc.paid); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: runes %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPlanJewelleryAsk(t *testing.T) {
	mule := stockMap(map[string]int{
		GoldBar: 371, CutSapphire: 59102, RuneCosmic: 4374, RuneWater: 211521, RuneLaw: 900, RuneFire: 50, RuneAir: 8000, CoinsName: 90000,
	})
	air := map[string]bool{RuneAir: true}
	own := func(m map[string]int) func(string) int { return func(item string) int { return m[item] } }

	// Magic 40 with a Staff of air, an empty bank, a mould held: the bars and gems of 50 necklaces,
	// the runes of the 50 enchants, one Falador Teleport and one Varrock Teleport.
	got := PlanJewelleryAsk(50, 40, air, mule, JewelleryHeld{}, own(nil), true)
	want := []Line{
		{GoldBar, 50}, {CutSapphire, 50},
		{RuneCosmic, 50}, {RuneFire, 1}, {RuneLaw, 2}, {RuneWater, 51},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty bank, a mould held:\n got %v\nwant %v", got, want)
	}
	// No mould and no coins: 200 coins for it.
	got = PlanJewelleryAsk(50, 40, air, mule, JewelleryHeld{}, own(nil), false)
	if len(got) != len(want)+1 || got[len(got)-1] != (Line{CoinsName, 2 * MouldCoins}) {
		t.Errorf("no mould: %v", got)
	}
	// Coins in the bank: none asked.
	got = PlanJewelleryAsk(50, 40, air, mule, JewelleryHeld{Coins: 5000}, own(map[string]int{CoinsName: 5000}), false)
	for _, l := range got {
		if l.Item == CoinsName {
			t.Errorf("coins asked with 5000 banked: %v", got)
		}
	}
	// Own runes come off the ask; own bars and Sapphires do not (they may be reserved), but Sapphire
	// necklaces already made come off the crafting.
	got = PlanJewelleryAsk(50, 40, air, mule, JewelleryHeld{Bars: 400, Sapphires: 400, Necklaces: 8, Cosmic: 20, Water: 10},
		own(map[string]int{RuneCosmic: 20, RuneWater: 10, RuneLaw: 5}), true)
	want = []Line{
		{GoldBar, 42}, {CutSapphire, 42},
		{RuneCosmic, 30}, {RuneFire, 1}, {RuneWater, 41},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("own runes and necklaces:\n got %v\nwant %v", got, want)
	}
	// A Staff of water asks for no Water for the enchants, nor for the Falador Teleport's.
	got = PlanJewelleryAsk(50, 40, map[string]bool{RuneAir: true, RuneWater: true}, mule, JewelleryHeld{}, own(nil), true)
	for _, l := range got {
		if l.Item == RuneWater {
			t.Errorf("water asked with a Staff of water: %v", got)
		}
	}
	// Below Magic 37 nothing is cast to Falador: no teleport runes but Varrock's at Magic 25.
	got = PlanJewelleryAsk(10, 30, air, mule, JewelleryHeld{}, own(nil), true)
	want = []Line{{GoldBar, 10}, {CutSapphire, 10}, {RuneCosmic, 10}, {RuneFire, 1}, {RuneLaw, 1}, {RuneWater, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Magic 30:\n got %v\nwant %v", got, want)
	}
	// Only what the mule publishes, at most its stock.
	thin := stockMap(map[string]int{GoldBar: 20, CutSapphire: 30, RuneCosmic: 15})
	got = PlanJewelleryAsk(50, 40, air, thin, JewelleryHeld{}, own(nil), true)
	want = []Line{{GoldBar, 20}, {CutSapphire, 30}, {RuneCosmic, 15}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a thin mule:\n got %v\nwant %v", got, want)
	}
	// An unpublished bank asks for nothing.
	if got := PlanJewelleryAsk(50, 40, air, nil, JewelleryHeld{}, own(nil), true); len(got) != 0 {
		t.Errorf("no bank: %v", got)
	}
}

func TestJewellerySettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeyJewelleryBatch: "25"})
	if err != nil || s.JewelleryBatch != 25 {
		t.Fatalf("a string from the panel: %+v %v", s, err)
	}
	s, err = DecodeSettings(map[string]interface{}{KeyJewelleryBatch: float64(0)})
	if err != nil || s.JewelleryBatch != 0 {
		t.Fatalf("0 is allowed (off): %+v %v", s, err)
	}
	for name, v := range map[string]interface{}{"negative": -1, "over the mule's cap": MaxJewelleryBatch + 1, "not a number": "lots", "fractional": 2.5} {
		if _, err := DecodeSettings(map[string]interface{}{KeyJewelleryBatch: v}); err == nil {
			t.Errorf("%s: accepted %v", name, v)
		}
	}
	// It does not count among the rounds whose all being off is an error (like the chest's).
	if _, err := DecodeSettings(map[string]interface{}{KeyGemCutting: false, KeyBowStringing: false, KeyPearls: false,
		KeySuperheat: false, KeyArrows: false, KeyHerbs: false, KeySmith: false}); err == nil {
		t.Error("every older round off must still be refused")
	}
	if !reflect.DeepEqual(DefaultSettings().JewelleryBatch, 50) {
		t.Error("the default batch is 50")
	}
}

func TestJewelleryRefusals(t *testing.T) {
	for said, want := range map[string]string{
		"Your Magic level is not high enough for this spell.":                   "level",
		"You do not have enough Water Runes to cast this spell.":                "runes",
		"You do not have enough Cosmic Runes to cast this spell.":               "runes",
		"You can only make that on a members' server.":                          "members",
		"You need to be on a members' server to use this spell.":                "members",
		"This spell can only be cast on Sapphire Amulets, Necklaces and Rings.": "item",
		"You successfully enchant the sapphire necklace.":                       "",
		"": "",
	} {
		if got := EnchantRefusal(said); got != want {
			t.Errorf("EnchantRefusal(%q) = %q, want %q", said, got, want)
		}
	}
	for said, want := range map[string]string{
		"You need at least level 20 Crafting to make that.": "level",
		"I can't reach that!":                               "reach",
		"You make a sapphire necklace.":                     "",
	} {
		if got := NecklaceRefusal(said); got != want {
			t.Errorf("NecklaceRefusal(%q) = %q, want %q", said, got, want)
		}
	}
}
