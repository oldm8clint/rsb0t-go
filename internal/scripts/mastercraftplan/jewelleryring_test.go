package mastercraftplan

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// THE RING OF DUELING BATCH (a user request): the jewellery round makes Rings of dueling too, from
// gold bars and cut Emeralds, Lvl-2 Enchant (3 Air and 1 Cosmic a cast), and hands them to the mule.

// ringStock is the mule on 7 Oct 2026 as far as the ring cares: 165 gold bars, 30,051 Emeralds, 4,917
// Cosmic and 52,221 Air runes, ten Games necklaces and no Ring of dueling.
func ringStock() map[string]int {
	return map[string]int{GoldBar: 165, CutEmerald: 30051, RuneCosmic: 4917, RuneAir: 52221, "Games necklace(8)": 10}
}

func ringSnap(stock map[string]int) Snapshot {
	s := jewelSnap(stock)
	s.MagicLevel = 40
	return s
}

func TestKindSpecs(t *testing.T) {
	n, r := KindNecklace.Spec(), KindRing.Spec()
	if n.Base != SapphireNecklace || n.Final != GamesNecklaceName || n.Gem != CutSapphire || n.Second != RuneWater || n.SecondPer != 1 || n.SpellLevel != 7 {
		t.Errorf("necklace: %+v", n)
	}
	if r.Base != EmeraldRing || r.Final != DuelingRingName || r.Gem != CutEmerald || r.Mould != RingMould || r.Second != RuneAir || r.SecondPer != 3 ||
		r.SpellLevel != 27 || r.CraftLevel != 27 || r.Spell != "Lvl-2 Enchant" || r.SpellXP != 370 || r.CraftXP != 550 {
		t.Errorf("ring: %+v", r)
	}
	if r.MenuInv != 4233 || r.MenuSlot != 2 || n.MenuInv != 4239 || n.MenuSlot != 1 {
		t.Errorf("menus: ring %d/%d, necklace %d/%d", r.MenuInv, r.MenuSlot, n.MenuInv, n.MenuSlot)
	}
	if KindNecklace != "" || KindRing.String() != "ring" || KindNecklace.String() != "necklace" {
		t.Errorf("kinds: %q %q", KindNecklace, KindRing)
	}
	if got := Kinds(); !reflect.DeepEqual(got, []JewelleryKind{KindRing, KindNecklace}) {
		t.Errorf("kinds in order: %v", got)
	}
	if DefaultSettings().RingBatch != 50 || DefaultSettings().BatchOf(KindRing) != 50 || DefaultSettings().BatchOf(KindNecklace) != 50 {
		t.Errorf("batches: %+v", DefaultSettings())
	}
	if KeyOf(KindRing) != "craftRingBatch" || KeyOf(KindNecklace) != "craftJewelleryBatch" {
		t.Errorf("keys")
	}
}

func TestJewelleryHeldAsAKind(t *testing.T) {
	h := JewelleryHeld{Bars: 5, Sapphires: 1, Necklaces: 2, Games: 3, Mould: 1, Cosmic: 9, Water: 8, Coins: 7,
		Emeralds: 11, EmeraldRings: 12, Duelings: 13, RingMould: 1, Air: 21}
	if got := h.As(KindNecklace); got != (JewelleryHeld{Bars: 5, Sapphires: 1, Necklaces: 2, Games: 3, Mould: 1, Cosmic: 9, Water: 8, Coins: 7}) {
		t.Errorf("as necklace: %+v", got)
	}
	got := h.As(KindRing)
	if got != (JewelleryHeld{Bars: 5, Sapphires: 11, Necklaces: 12, Games: 13, Mould: 1, Cosmic: 9, Water: 21, Coins: 7}) {
		t.Errorf("as ring: %+v", got)
	}
	if got.Pairs() != 5 {
		t.Errorf("five bars, eleven Emeralds make five rings: %d", got.Pairs())
	}
	// Casts: three Air a ring, one Cosmic.
	for _, tc := range []struct {
		h    JewelleryHeld
		paid bool
		want int
	}{
		{JewelleryHeld{Cosmic: 10, Water: 29}, false, 9},
		{JewelleryHeld{Cosmic: 4, Water: 29}, false, 4},
		{JewelleryHeld{Cosmic: 4, Water: 2}, false, 0},
		{JewelleryHeld{Cosmic: 4}, true, 4},
		{JewelleryHeld{Water: 9}, true, 0},
	} {
		if got := KindRing.Casts(tc.h, tc.paid); got != tc.want {
			t.Errorf("ring casts of %+v paid=%v: %d, want %d", tc.h, tc.paid, got, tc.want)
		}
	}
	if got := KindNecklace.Casts(JewelleryHeld{Cosmic: 10, Water: 3}, false); got != 3 {
		t.Errorf("necklace casts: %d", got)
	}
	// JewelleryHeldIn reads every kind's items by object id.
	ids := map[int32]int{EmeraldRingID: 4, DuelingRingID: 2, CutEmeraldID: 7, RingMouldID: 1, RuneAirID: 30, GoldBarID: 3}
	read := JewelleryHeldIn(func(id int32) int { return ids[id] })
	if read.EmeraldRings != 4 || read.Duelings != 2 || read.Emeralds != 7 || read.RingMould != 1 || read.Air != 30 || read.Bars != 3 || read.Games != 0 {
		t.Errorf("held in: %+v", read)
	}
	if sum := h.Add(h); sum.Duelings != 26 || sum.Air != 42 || sum.Bars != 10 {
		t.Errorf("add: %+v", sum)
	}
}

func TestPlanRingBatch(t *testing.T) {
	set := DefaultSettings()
	p := PlanKindBatch(KindRing, set, stockMap(ringStock()), false)
	if p.Kind != KindRing || p.N != 50 || p.Gold != 165 || p.Sapphires != 30051 || p.Water != 52221 {
		t.Errorf("a batch of 50: %+v", p)
	}
	// Air is three a ring: 52,221 Air is 17,407 rings; 120 Air is 40.
	m := ringStock()
	m[RuneAir] = 120
	if p := PlanKindBatch(KindRing, set, stockMap(m), false); p.N != 40 {
		t.Errorf("120 Air makes 40 rings: %+v", p)
	}
	m[RuneAir] = 2
	if p := PlanKindBatch(KindRing, set, stockMap(m), false); p.N != 0 || !strings.Contains(p.Why, "no Air rune") {
		t.Errorf("2 Air: %+v", p)
	}
	// A Staff of air pays the Air.
	if p := PlanKindBatch(KindRing, set, stockMap(m), true); p.N != 50 {
		t.Errorf("a Staff of air: %+v", p)
	}
	for item, want := range map[string]string{GoldBar: "no Gold bar", CutEmerald: "no Emerald", RuneCosmic: "no Cosmic rune"} {
		m := ringStock()
		m[item] = 0
		if p := PlanKindBatch(KindRing, set, stockMap(m), false); p.N != 0 || !strings.Contains(p.Why, want) {
			t.Errorf("no %s: %+v", item, p)
		}
	}
	set.RingBatch = 12
	if p := PlanKindBatch(KindRing, set, stockMap(ringStock()), false); p.N != 12 {
		t.Errorf("batch 12: %+v", p)
	}
	// The necklace plan is unchanged and ignores the ring's items.
	if p := PlanJewelleryBatch(DefaultSettings(), stockMap(ringStock()), false); p.N != 0 || !strings.Contains(p.Why, "no Sapphire") {
		t.Errorf("the necklace plan on the ring's stock: %+v", p)
	}
}

func TestJewelleryOKForRings(t *testing.T) {
	set := DefaultSettings()
	if ok, _ := JewelleryOKFor(KindRing, set, 27, 27); !ok {
		t.Error("Magic 27 and Crafting 27 make a ring")
	}
	if ok, why := JewelleryOKFor(KindRing, set, 26, 92); ok || !strings.Contains(why, "Lvl-2 Enchant") {
		t.Errorf("Magic 26: %v %q", ok, why)
	}
	if ok, why := JewelleryOKFor(KindRing, set, 40, 26); ok || !strings.Contains(why, "an Emerald ring") {
		t.Errorf("Crafting 26: %v %q", ok, why)
	}
	if ok, why := JewelleryOKFor(KindNecklace, set, 40, 19); ok || !strings.Contains(why, "a Sapphire necklace") {
		t.Errorf("Crafting 19: %v %q", ok, why)
	}
	set.MembersWorld = false
	if ok, why := JewelleryOKFor(KindRing, set, 40, 92); ok || !strings.Contains(why, "members") {
		t.Errorf("a free world: %v %q", ok, why)
	}
}

// Rings of dueling first when the mule holds none, even with Games necklaces in its bank; Games
// necklaces only when it holds neither.
func TestJewelleryReadyMakesRingsFirst(t *testing.T) {
	set := DefaultSettings()
	d := Decide(set, ringSnap(ringStock()))
	if d.Mode != ModeJewellery || d.Kind != KindRing || d.Resume || d.Jewellery.N != 50 || d.Jewellery.Kind != KindRing || !strings.Contains(d.Why, "no Ring of dueling") {
		t.Fatalf("the mule holds Games necklaces and no ring: %+v", d)
	}
	// Any charge of the ring holds the ring batch off.
	for _, name := range KindRing.DemandNames() {
		m := ringStock()
		m[name] = 1
		if d := Decide(set, ringSnap(m)); d.Mode == ModeJewellery {
			t.Errorf("%s in the mule's bank: %+v", name, d)
		}
	}
	if names := KindRing.DemandNames(); len(names) != 8 || names[0] != "Ring of dueling(8)" || names[7] != "Ring of dueling(1)" {
		t.Errorf("ring names: %v", names)
	}
	if n := KindNecklace.DemandHeld(stockMap(ringStock())); n != 10 {
		t.Errorf("necklaces held: %d", n)
	}
	// Neither held and everything stocked: still the ring.
	m := ringStock()
	delete(m, "Games necklace(8)")
	m[CutSapphire], m[RuneWater] = 5000, 5000
	if d := Decide(set, ringSnap(m)); d.Kind != KindRing || d.Mode != ModeJewellery {
		t.Errorf("neither held: %+v", d)
	}
	// No Emeralds: the necklace when the mule holds none of either.
	m[CutEmerald] = 0
	d = Decide(set, ringSnap(m))
	if d.Mode != ModeJewellery || d.Kind != KindNecklace || d.Jewellery.N != 50 {
		t.Errorf("no Emeralds, no necklaces held: %+v", d)
	}
	// No Emeralds and Games necklaces held: nothing, and the reasons say both.
	m["Games necklace(8)"] = 3
	d = Decide(set, ringSnap(m))
	if d.Mode == ModeJewellery || !strings.Contains(d.Why, "the mule holds 3 Games necklace(s) and Ring(s) of dueling") || !strings.Contains(d.Why, "the mule holds no Ring of dueling and cannot fund a batch: the mule holds no Emerald") {
		t.Errorf("no Emeralds, necklaces held: %+v", d)
	}
	// The ring needs Magic 27: at 26 a necklace batch is made when the mule holds none.
	m26 := ringStock()
	delete(m26, "Games necklace(8)")
	m26[CutSapphire], m26[RuneWater] = 5000, 5000
	s := ringSnap(m26)
	s.MagicLevel = 26
	if d := Decide(set, s); d.Mode != ModeJewellery || d.Kind != KindNecklace {
		t.Errorf("Magic 26: %+v", d)
	}
	// craftRingBatch 0: no rings, the necklaces as before.
	off := DefaultSettings()
	off.RingBatch = 0
	if d := Decide(off, ringSnap(ringStock())); d.Mode == ModeJewellery {
		t.Errorf("rings off, necklaces held: %+v", d)
	}
	// Both off: the round is off.
	off.JewelleryBatch = 0
	if d := Decide(off, ringSnap(ringStock())); d.Mode == ModeJewellery || !strings.Contains(d.Why, "jewellery is off") {
		t.Errorf("both off: %+v", d)
	}
	// Necklaces off, rings on.
	only := DefaultSettings()
	only.JewelleryBatch = 0
	if d := Decide(only, ringSnap(ringStock())); d.Kind != KindRing || d.Mode != ModeJewellery {
		t.Errorf("necklaces off: %+v", d)
	}
}

func TestJewelleryReadyResumesRings(t *testing.T) {
	set := DefaultSettings()
	// Emerald rings in this account's own bank with the runes (3 Air each): enchant them.
	s := ringSnap(ringStock())
	s.OwnJewellery = JewelleryHeld{EmeraldRings: 8, Cosmic: 8, Air: 24}
	d := Decide(set, s)
	if d.Mode != ModeJewellery || d.Kind != KindRing || !d.Resume || !strings.Contains(d.Why, "8 Emerald ring(s)") {
		t.Errorf("rings in the bank: %+v", d)
	}
	// Two Air: not enough for one cast, nothing to resume (and the mule's stock starts a new ring batch).
	s.OwnJewellery = JewelleryHeld{EmeraldRings: 8, Cosmic: 8, Air: 2}
	if d := Decide(set, s); d.Resume {
		t.Errorf("two Air: %+v", d)
	}
	// A Staff of air pays them.
	s.AirPaid = true
	if d := Decide(set, s); !d.Resume || d.Kind != KindRing {
		t.Errorf("a Staff of air: %+v", d)
	}
	// A necklace batch resumes as a necklace even when rings are the usual first kind.
	s = ringSnap(ringStock())
	s.OwnJewellery = JewelleryHeld{Necklaces: 5, Cosmic: 5, Water: 5}
	d = Decide(set, s)
	if d.Mode != ModeJewellery || d.Kind != KindNecklace || !d.Resume {
		t.Errorf("necklaces in the bank: %+v", d)
	}
	// A ring batch under way (the ledger), the bank not read yet.
	s = ringSnap(ringStock())
	s.OwnJewelleryKnown = false
	s.Ledger.Jewellery.StartKindBatch(KindRing, 40)
	s.Ledger.Jewellery.Enchanted = 10
	d = Decide(set, s)
	if d.Mode != ModeJewellery || d.Kind != KindRing || !d.Resume || !strings.Contains(d.Why, "40 Rings of dueling is under way") {
		t.Errorf("a batch under way: %+v", d)
	}
	// ...and a necklace batch under way resumes as a necklace.
	s.Ledger.Jewellery.StartKindBatch(KindNecklace, 40)
	d = Decide(set, s)
	if d.Kind != KindNecklace || !d.Resume {
		t.Errorf("a necklace batch under way: %+v", d)
	}
	// Rings owed to the mule: a hand-over, by object id.
	s = ringSnap(ringStock())
	s.Ledger.Jewellery.AddOwed(int(DuelingRingID), 30)
	d = Decide(set, s)
	if d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "30 Games necklace(s) and Ring(s) of dueling") {
		t.Errorf("owed rings: %+v", d)
	}
	ids, owed := s.Ledger.Jewellery.OwedIDs()
	if len(ids) != 1 || ids[0] != 2552 || owed[2552] != 30 {
		t.Errorf("owed ids %v %v", ids, owed)
	}
}

func TestJewelleryLedgerKind(t *testing.T) {
	var l JewelleryLedger
	l.StartBatch(10)
	if l.Kind != KindNecklace || l.Target != 10 {
		t.Errorf("start: %+v", l)
	}
	l.StartKindBatch(KindRing, 20)
	l.Enchanted = 5
	if l.Kind != KindRing || !l.Under() || l.Left() != 15 {
		t.Errorf("ring batch: %+v", l)
	}
	l.ClearBatch()
	if l.Kind != KindNecklace || l.Under() {
		t.Errorf("cleared: %+v", l)
	}
	// A ledger saved before rings existed carries no kind and reads as the necklace.
	led := Ledger{}
	led.Jewellery.StartKindBatch(KindRing, 5)
	b := mustJSON(t, led)
	if !strings.Contains(b, `"kind":"ring"`) {
		t.Errorf("the ring batch is saved with its kind: %s", b)
	}
	led.Jewellery.ClearBatch()
	if b := mustJSON(t, led); strings.Contains(b, `"kind"`) {
		t.Errorf("a necklace batch saves no kind: %s", b)
	}
}

func TestPlanRingDraws(t *testing.T) {
	// A crafting load: the Ring mould, ten gold bars and ten Emeralds, the runes of ten Lvl-2 enchants
	// (Cosmic and three Air each).
	in := JewelleryVisit{
		Kind: KindRing,
		Bank: JewelleryHeld{Bars: 165, Sapphires: 30051, Mould: 1, Cosmic: 4917, Water: 52221},
		Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true,
	}
	got := PlanJewelleryDraws(in)
	want := []JewelleryDraw{
		{RingMouldID, RingMould, 1}, {GoldBarID, GoldBar, 10}, {CutEmeraldID, CutEmerald, 10},
		{RuneCosmicID, RuneCosmic, 10}, {RuneAirID, RuneAir, 30},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a load:\n got %v\nwant %v", got, want)
	}
	if !JewelleryLoaded(got) {
		t.Error("a ring load is a load")
	}
	// Air for 4 casts only: the load shrinks to four rings.
	in.Bank.Water = 13
	if g := drawMap(PlanJewelleryDraws(in)); g[GoldBar] != 4 || g[RuneAir] != 12 || g[RuneCosmic] != 4 {
		t.Errorf("13 Air: %v", g)
	}
	// A Staff of air: no Air drawn.
	in.WaterPaid, in.Bank.Water = true, 0
	if g := drawMap(PlanJewelleryDraws(in)); g[GoldBar] != 10 || g[RuneAir] != 0 || g[RuneCosmic] != 10 {
		t.Errorf("a Staff of air: %v", g)
	}
	// Emerald rings in the bank are enchanted first.
	in = JewelleryVisit{Kind: KindRing, Bank: JewelleryHeld{Bars: 100, Sapphires: 100, Necklaces: 8, Mould: 1, Cosmic: 50, Water: 50}, Left: 30, CraftOK: true, EnchantOK: true, ShopOK: true}
	if got := PlanJewelleryDraws(in); !reflect.DeepEqual(got, []JewelleryDraw{{EmeraldRingID, EmeraldRing, 8}, {RuneCosmicID, RuneCosmic, 8}, {RuneAirID, RuneAir, 24}}) {
		t.Errorf("rings in the bank: %v", got)
	}
	// The mould's coins when none is held.
	in = JewelleryVisit{Kind: KindRing, Bank: JewelleryHeld{Bars: 50, Sapphires: 50, Cosmic: 50, Water: 150, Coins: 4000}, Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true}
	if g := drawMap(PlanJewelleryDraws(in)); g[CoinsName] != MouldCoins || g[RingMould] != 0 || g[GoldBar] != 10 {
		t.Errorf("no mould: %v", g)
	}
	// The Falador Teleport's three Air ride on the same stack as the enchants' (Staff of water, no air).
	in = JewelleryVisit{Kind: KindRing, Bank: JewelleryHeld{Bars: 50, Sapphires: 50, Mould: 1, Cosmic: 50, Water: 40}, Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true,
		BankOf: bankOfMap(map[string]int{RuneLaw: 5, RuneWater: 5}), Travel: map[string]int{RuneAir: 3, RuneWater: 1, RuneLaw: 1}}
	g := drawMap(PlanJewelleryDraws(in))
	if g[RuneAir] != 33 || g[RuneLaw] != 1 || g[RuneWater] != 1 || g[GoldBar] != 10 {
		t.Errorf("a load and a teleport: %v", g)
	}
	// Air for the enchants and none to spare for the teleport: no teleport runes at all.
	in.Bank.Water = 31
	g = drawMap(PlanJewelleryDraws(in))
	if g[RuneAir] != 30 || g[RuneLaw] != 0 || g[RuneWater] != 0 {
		t.Errorf("no Air to spare: %v", g)
	}
}

func TestPlanRingAsk(t *testing.T) {
	mule := stockMap(map[string]int{GoldBar: 165, CutEmerald: 30051, RuneCosmic: 4917, RuneAir: 52221, RuneWater: 900, RuneLaw: 900, RuneFire: 50, CoinsName: 9000})
	own := func(m map[string]int) func(string) int { return func(item string) int { return m[item] } }
	// Magic 40, no staff, a mould held: bars and Emeralds of 50, Cosmic, 150 Air for the enchants and
	// 3 more for the Falador Teleport's, Water and Law for the teleports, Fire for Varrock.
	got := PlanKindAsk(KindRing, 50, 40, nil, mule, JewelleryHeld{}, own(nil), true)
	asked := map[string]int{}
	for _, l := range got {
		asked[l.Item] = l.Qty
	}
	if asked[GoldBar] != 50 || asked[CutEmerald] != 50 || asked[RuneCosmic] != 50 || asked[RuneAir] < 150+3 || asked[CutSapphire] != 0 || asked[RuneWater] != 1+0 {
		t.Errorf("ask: %v", asked)
	}
	// A Staff of air: no Air for the enchants or the teleports.
	got = PlanKindAsk(KindRing, 50, 40, map[string]bool{RuneAir: true}, mule, JewelleryHeld{}, own(nil), true)
	for _, l := range got {
		if l.Item == RuneAir {
			t.Errorf("Air asked with a Staff of air: %v", got)
		}
	}
	// Own Air comes off the ask, own Emeralds do not, rings already made come off the crafting.
	got = PlanKindAsk(KindRing, 50, 40, map[string]bool{RuneAir: true}, mule, JewelleryHeld{Bars: 400, Sapphires: 400, Necklaces: 8},
		own(map[string]int{RuneAir: 5}), true)
	asked = map[string]int{}
	for _, l := range got {
		asked[l.Item] = l.Qty
	}
	if asked[GoldBar] != 42 || asked[CutEmerald] != 42 {
		t.Errorf("8 rings made: %v", asked)
	}
}

func TestNextJewelleryStepForRings(t *testing.T) {
	ring := func(f func(v *JewelleryView)) JewelleryView {
		return jview(func(v *JewelleryView) {
			v.Kind = KindRing
			if f != nil {
				f(v)
			}
		})
	}
	// Rings and the runes (three Air each) in the pack: enchant.
	v := ring(func(v *JewelleryView) {
		v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Mould: 1, Cosmic: 10, Water: 30}, JewelleryHeld{}
		v.Where = JewelleryAtFurnace
	})
	if got := NextJewelleryStep(v); got != JewelleryStepEnchant {
		t.Errorf("rings and Air in the pack: %s", got)
	}
	// Two Air are not a cast: the enchants cannot be made, the round wraps up.
	v = ring(func(v *JewelleryView) {
		v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 2}, JewelleryHeld{}
		v.Where = JewelleryAtFurnace
	})
	if got := NextJewelleryStep(v); got == JewelleryStepEnchant {
		t.Errorf("two Air: %s", got)
	}
	// A Staff of air pays them.
	v = ring(func(v *JewelleryView) {
		v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10}, JewelleryHeld{}
		v.WaterPaid = true
	})
	if got := NextJewelleryStep(v); got != JewelleryStepEnchant {
		t.Errorf("a Staff of air: %s", got)
	}
	// The runes sit in the bank: a bank visit, the next load drawn.
	v = ring(func(v *JewelleryView) {
		v.Bank = JewelleryHeld{Bars: 10, Sapphires: 10, Mould: 1, Cosmic: 10, Water: 30}
	})
	if got := NextJewelleryStep(v); got != JewelleryStepBank {
		t.Errorf("materials in the bank: %s", got)
	}
	// Air in the bank for two casts only caps what the view can still make.
	v = ring(func(v *JewelleryView) {
		v.Bank = JewelleryHeld{Bars: 10, Sapphires: 10, Mould: 1, Cosmic: 10, Water: 2}
	})
	if v.runesAnywhere() {
		t.Error("two Air are not a cast")
	}
}

func TestJewelleryVisitForKeepsTheKind(t *testing.T) {
	v := jview(func(v *JewelleryView) { v.Kind = KindRing })
	in := JewelleryVisitFor(v, JewelleryHeld{Bars: 10}, nil, 40, nil)
	if in.Kind != KindRing {
		t.Errorf("the visit lost the kind: %+v", in)
	}
}

func TestJewelleryPackOtherCountsTheOtherKind(t *testing.T) {
	// The plan side of it: a necklace-shaped holding read as a ring has no Emerald rings.
	h := JewelleryHeld{Necklaces: 4, Games: 2}
	if r := h.As(KindRing); r.Necklaces != 0 || r.Games != 0 {
		t.Errorf("%+v", r)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- the content the ring rests on (RSB0T_CONTENT, skipped without it) ----

func TestRingObjectIDsAreContents(t *testing.T) {
	pack := contentRoot(t, "pack/obj.pack")
	for name, want := range map[string]int{
		"ring_mould": int(RingMouldID), "emerald": int(CutEmeraldID), "emerald_ring": int(EmeraldRingID),
		"invis_ring2": int(MakeMenuRingID), "ring_of_dueling_8": int(DuelingRingID), "airrune": int(RuneAirID),
	} {
		if got := packID(pack, name); got != want {
			t.Errorf("%s: content's id is %d, the plan's %d", name, got, want)
		}
	}
	iface := contentRoot(t, "pack/interface.pack")
	for name, want := range map[string]int{
		"magic:enchant_lvl2": 1165, "crafting_jewelry:rings_inv": RingsInv,
	} {
		if got := packID(iface, name); got != want {
			t.Errorf("%s: content's interface id is %d, want %d", name, got, want)
		}
	}
	obj := contentFile(t, "skill_magic/configs/enchanted_jewelry.obj")
	if b := block(obj, "ring_of_dueling_8"); !strings.Contains(b, "name=Ring of dueling(8)") || !strings.Contains(b, "members=yes") || !strings.Contains(b, "param=charges,8") || !strings.Contains(b, "iop4=Rub") {
		t.Errorf("ring_of_dueling_8: %q", b)
	}
}

func TestLvl2EnchantIsContents(t *testing.T) {
	spells := contentFile(t, "skill_magic/configs/magic_spells.dbrow")
	b := block(spells, "magic_spell_enchant_level2")
	for _, want := range []string{
		"data=spell,^enchant_lvl2\n", "data=members,false\n", "data=levelrequired," + strconv.Itoa(EnchantRingLevel) + "\n",
		"data=runesrequired,airrune," + strconv.Itoa(EnchantRingAir) + ",cosmicrune,1,null,null\n",
		"data=experience," + strconv.Itoa(EnchantRingXPTenths) + "\n", "data=convertobj,emerald_ring,ring_of_dueling_8,",
	} {
		if !strings.Contains(b+"\n", want) {
			t.Errorf("[magic_spell_enchant_level2] lacks %q:\n%s", want, b)
		}
	}
	enchant := contentFile(t, "skill_magic/scripts/spells/enchant.rs2")
	for _, want := range []string{"[opheldt,magic:enchant_lvl2]@magic_spell_enchant(^enchant_lvl2, last_slot);", "inv_setslot(inv, $slot, $final_obj, 1);", "p_delay(1);"} {
		if !strings.Contains(enchant, want) {
			t.Errorf("enchant.rs2 lacks %q", want)
		}
	}
}

func TestRingCraftingIsContents(t *testing.T) {
	st := contentFile(t, "skill_crafting/configs/jewellery/jewellery.struct")
	b := block(st, "emerald_ring")
	for _, want := range []string{"param=gem,emerald\n", "param=levelrequired," + strconv.Itoa(RingLevel) + "\n", "param=productexp," + strconv.Itoa(RingXPTenths) + "\n"} {
		if !strings.Contains(b+"\n", want) {
			t.Errorf("[emerald_ring] lacks %q:\n%s", want, b)
		}
	}
	rs2 := contentFile(t, "skill_crafting/scripts/jewellery/jewellery.rs2")
	for _, want := range []string{
		"[inv_button1,crafting_jewelry:rings_inv] @craft_gold(crafting_make_rings, last_slot, 1);",
		"[inv_button2,crafting_jewelry:rings_inv] @craft_gold(crafting_make_rings, last_slot, 5);",
		"[inv_button3,crafting_jewelry:rings_inv] @craft_gold(crafting_make_rings, last_slot, 10);",
		"if (inv_total(inv, ring_mould) > 0) {",
		"inv_setslot(crafting_make_rings, 2, invis_ring2, 1);", // the Emerald ring is slot 2
	} {
		if !strings.Contains(rs2, want) {
			t.Errorf("jewellery.rs2 lacks %q", want)
		}
	}
	ifs := contentFile(t, "skill_crafting/interfaces/crafting_jewelry.if")
	if b := block(ifs, "rings_inv"); !strings.Contains(b, "option1=Make\n") || !strings.Contains(b, "option2=Make 5\n") || !strings.Contains(b, "option3=Make 10\n") || strings.Contains(b, "option4") {
		t.Errorf("[rings_inv]: %q", b)
	}
	moulds := contentFile(t, "skill_crafting/configs/jewellery/moulds.obj")
	if b := block(moulds, "ring_mould"); !strings.Contains(b, "name=Ring mould\n") || !strings.Contains(b, "cost=5\n") {
		t.Errorf("[ring_mould]: %q", b)
	}
	for file, shops := range map[string][]string{
		"areas/area_rimmington/configs/rimmington.inv": {"craftingshop", "craftingshop_free"},
		"areas/area_alkharid/configs/alkharid.inv":     {"craftingshop2", "craftingshop2_free"},
	} {
		inv := contentFile(t, file)
		for _, shop := range shops {
			if b := block(inv, shop); !strings.Contains(b, "stock2=ring_mould,4,100\n") {
				t.Errorf("%s [%s] does not stock the Ring mould: %q", file, shop, b)
			}
		}
	}
	// The dueling ring's rub is the run staller's (category 136, iop4).
	rub := contentFile(t, "general/scripts/enchanted_jewellry/ring_of_dueling.rs2")
	if !strings.Contains(rub, "[opheld4,_category_136]") {
		t.Errorf("ring_of_dueling.rs2 lacks the rub")
	}
}

// LIVE BUG, 7 Oct 2026 14:42: a ring round with its 50 rings made banked at Falador eleven times and never went
// home. The visit that ends the work draws the teleport home's runes (Camelot Teleport: 5 Air, 1 Law), and Air is the
// ring's second rune, so the pack's Air counted as dirt (dirty()) and the next step was a bank visit again.
func TestRingRoundGoesHomeWithTheTeleportsAir(t *testing.T) {
	wrap := func(f func(v *JewelleryView)) JewelleryView {
		return jview(func(v *JewelleryView) {
			v.Kind, v.Left, v.Owed = KindRing, 0, 50
			v.Where, v.NearWork = JewelleryAtWorkBank, true
			v.Bank = JewelleryHeld{Games: 50, Mould: 1}
			v.Pack = JewelleryHeld{Water: 5} // the 5 Air of Camelot Teleport, drawn by the visit (its Law rune is no dirt)
			if f != nil {
				f(v)
			}
		})
	}
	// Without the allowance the Air is dirt: the old loop.
	if got := NextJewelleryStep(wrap(nil)); got != JewelleryStepBank {
		t.Fatalf("an unexplained 5 Air is dirt: %s", got)
	}
	// With it the account goes home (the Law rune, a rune, is not dirt either: PackOther counts only non-runes).
	v := wrap(func(v *JewelleryView) { v.KeepSecond = 5 })
	if got := NextJewelleryStep(v); got != JewelleryStepHome {
		t.Errorf("with the teleport's Air kept: %s", got)
	}
	// More Air than the teleport takes is the enchants' leftover: banked.
	if got := NextJewelleryStep(wrap(func(v *JewelleryView) { v.KeepSecond, v.Pack.Water = 5, 8 })); got != JewelleryStepBank {
		t.Errorf("leftover Air: %s", got)
	}
	// At the post, with nothing in the pack: the hand-over.
	if got := NextJewelleryStep(wrap(func(v *JewelleryView) { v.Where, v.Pack = JewelleryAtPost, JewelleryHeld{} })); got != JewelleryStepHandover {
		t.Errorf("at the post: %s", got)
	}
}

// The visit that ends a ring batch draws exactly the Air of the way home, and the home leg's allowance is that.
func TestRingWrapUpDrawsTheHomeAir(t *testing.T) {
	v := jview(func(v *JewelleryView) {
		v.Kind, v.Left, v.Where, v.NearWork = KindRing, 0, JewelleryAtWorkBank, true
	})
	in := JewelleryVisitFor(v, JewelleryHeld{Games: 50, Water: 900, Cosmic: 10}, bankOfMap(map[string]int{RuneLaw: 9, RuneAir: 900}), 50, nil)
	draws := drawMap(PlanJewelleryDraws(in))
	home := JewelleryTravelRunes(LegHome, 50, nil)
	if draws[RuneAir] != home[RuneAir] || home[RuneAir] != 5 || draws[RuneLaw] != 1 {
		t.Errorf("draws %v, home runes %v", draws, home)
	}
}

// Jewellery made and not handed over is the mule's: a pause or backoff of the jewellery round does not hold the
// hand-over off, but the hand-over's own backoff does.
func TestOwedJewelleryResumesThroughAJewelleryBackoff(t *testing.T) {
	set := DefaultSettings()
	s := ringSnap(ringStock())
	s.Ledger.Jewellery.AddOwed(int(DuelingRingID), 50)
	s.Ledger.Jewellery.PauseUntil = s.Now.Add(time.Hour)
	s.Ledger.Backoffs = map[string]Backoff{string(ModeJewellery): {Fails: 2, Until: s.Now.Add(time.Hour)}}
	d := Decide(set, s)
	if d.Mode != ModeJewellery || !d.Resume || !strings.Contains(d.Why, "50 Games necklace(s) and Ring(s) of dueling") {
		t.Errorf("a paused, backing-off round with 50 rings owed: %+v", d)
	}
	s.Ledger.Backoffs[string(ModeDeliver)] = Backoff{Fails: 1, Until: s.Now.Add(time.Hour)}
	if d := Decide(set, s); d.Mode == ModeJewellery {
		t.Errorf("the hand-over's own backoff holds it: %+v", d)
	}
	// Nothing owed: the pause holds the round off as before.
	s2 := ringSnap(ringStock())
	s2.Ledger.Jewellery.PauseUntil = s2.Now.Add(time.Hour)
	if d := Decide(set, s2); d.Mode == ModeJewellery {
		t.Errorf("a paused round with nothing owed: %+v", d)
	}
}
