package mastercraftplan

import (
	"reflect"
	"testing"
)

func drawMap(draws []JewelleryDraw) map[string]int {
	out := map[string]int{}
	for _, d := range draws {
		out[d.Name] += d.Count
	}
	return out
}

func bankOfMap(m map[string]int) func(string) int { return func(n string) int { return m[n] } }

func TestPlanJewelleryDrawsACraftingLoad(t *testing.T) {
	in := JewelleryVisit{
		Bank: JewelleryHeld{Bars: 371, Sapphires: 59102, Mould: 1, Cosmic: 4374, Water: 211521},
		Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true,
	}
	got := PlanJewelleryDraws(in)
	want := []JewelleryDraw{
		{NecklaceMouldID, NecklaceMould, 1}, {GoldBarID, GoldBar, 10}, {CutSapphireID, CutSapphire, 10},
		{RuneCosmicID, RuneCosmic, 10}, {RuneWaterID, RuneWater, 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a load:\n got %v\nwant %v", got, want)
	}
	// Fewer left than a load: exactly what is left, and the runes of exactly those enchants.
	in.Left = 4
	if got := drawMap(PlanJewelleryDraws(in)); got[GoldBar] != 4 || got[CutSapphire] != 4 || got[RuneCosmic] != 4 || got[RuneWater] != 4 || got[NecklaceMould] != 1 {
		t.Errorf("Left 4: %v", got)
	}
	// The bank holds fewer pairs than a load.
	in.Left = 50
	in.Bank.Sapphires = 6
	if got := drawMap(PlanJewelleryDraws(in)); got[GoldBar] != 6 || got[CutSapphire] != 6 || got[RuneWater] != 6 {
		t.Errorf("six pairs: %v", got)
	}
	// The runes cover fewer enchants than a load: the load shrinks to them rather than crafting what
	// cannot be enchanted.
	in.Bank = JewelleryHeld{Bars: 371, Sapphires: 59102, Mould: 1, Cosmic: 3, Water: 100}
	if got := drawMap(PlanJewelleryDraws(in)); got[GoldBar] != 3 || got[RuneCosmic] != 3 {
		t.Errorf("three Cosmic runes: %v", got)
	}
	in.Bank = JewelleryHeld{Bars: 371, Sapphires: 59102, Mould: 1, Cosmic: 100, Water: 2}
	if got := drawMap(PlanJewelleryDraws(in)); got[GoldBar] != 2 || got[RuneWater] != 2 {
		t.Errorf("two Water runes: %v", got)
	}
	// A Staff of water: no Water drawn, and none needed.
	in.WaterPaid = true
	in.Bank.Water = 0
	if got := drawMap(PlanJewelleryDraws(in)); got[GoldBar] != 10 || got[RuneWater] != 0 || got[RuneCosmic] != 10 {
		t.Errorf("a Staff of water: %v", got)
	}
	// A batch with nothing left, or enchanting not allowed, draws nothing.
	in.Left = 0
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("Left 0: %v", got)
	}
	in.Left, in.EnchantOK = 50, false
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("no enchanting: %v", got)
	}
	in.EnchantOK, in.CraftOK = true, false
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("no crafting and no necklaces: %v", got)
	}
}

func TestPlanJewelleryDrawsEnchantsFirst(t *testing.T) {
	in := JewelleryVisit{
		Bank: JewelleryHeld{Bars: 100, Sapphires: 100, Necklaces: 8, Mould: 1, Cosmic: 50, Water: 50},
		Left: 30, CraftOK: true, EnchantOK: true, ShopOK: true,
	}
	got := PlanJewelleryDraws(in)
	want := []JewelleryDraw{{SapphireNecklaceID, SapphireNecklace, 8}, {RuneCosmicID, RuneCosmic, 8}, {RuneWaterID, RuneWater, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("necklaces in the bank:\n got %v\nwant %v", got, want)
	}
	// More than the batch has left: only what is left.
	in.Left = 5
	if got := drawMap(PlanJewelleryDraws(in)); got[SapphireNecklace] != 5 || got[RuneCosmic] != 5 || got[GoldBar] != 0 {
		t.Errorf("Left 5: %v", got)
	}
	// An enchant load is as many as the pack has slots for beside its runes.
	in = JewelleryVisit{Bank: JewelleryHeld{Necklaces: 100, Cosmic: 100, Water: 100}, Left: 100, EnchantOK: true,
		Travel: map[string]int{RuneLaw: 1, RuneFire: 1, RuneAir: 3}, BankOf: bankOfMap(map[string]int{RuneLaw: 9, RuneFire: 9, RuneAir: 9})}
	draws := PlanJewelleryDraws(in)
	rows := 0
	for _, d := range draws {
		if d.Name == SapphireNecklace {
			if d.Count != JewelleryEnchantMaxLoad {
				t.Errorf("an enchant load is %d, got %d", JewelleryEnchantMaxLoad, d.Count)
			}
			rows += d.Count
		} else {
			rows++
		}
	}
	if rows > JewelleryPackSlots {
		t.Errorf("the load takes %d slots of %d: %v", rows, JewelleryPackSlots, draws)
	}
	// No runes, no enchant.
	in = JewelleryVisit{Bank: JewelleryHeld{Necklaces: 8, Water: 9}, Left: 8, EnchantOK: true}
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("no Cosmic: %v", got)
	}
}

func TestPlanJewelleryDrawsTheMould(t *testing.T) {
	in := JewelleryVisit{
		Bank: JewelleryHeld{Bars: 50, Sapphires: 50, Cosmic: 50, Water: 50, Coins: 4000},
		Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true,
	}
	// No mould, coins in the bank: the coins for one, with the load, so the purchase can be made.
	got := drawMap(PlanJewelleryDraws(in))
	if got[CoinsName] != MouldCoins || got[GoldBar] != 10 || got[NecklaceMould] != 0 {
		t.Errorf("no mould, coins: %v", got)
	}
	// The coins are not enough for it, or a purchase already failed: nothing is drawn, so the round
	// ends instead of carrying bars about it cannot use.
	in.Bank.Coins = MouldCoins - 1
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("not enough coins: %v", got)
	}
	in.Bank.Coins, in.ShopOK = 4000, false
	if got := PlanJewelleryDraws(in); len(got) != 0 {
		t.Errorf("the shop failed: %v", got)
	}
	// A mould in the bank is drawn instead of coins.
	in.Bank.Mould = 1
	if got := drawMap(PlanJewelleryDraws(in)); got[NecklaceMould] != 1 || got[CoinsName] != 0 {
		t.Errorf("a mould held: %v", got)
	}
}

func TestPlanJewelleryDrawsTheTeleportRunes(t *testing.T) {
	bank := bankOfMap(map[string]int{RuneLaw: 5, RuneFire: 5, RuneAir: 50})
	in := JewelleryVisit{
		Bank: JewelleryHeld{Bars: 50, Sapphires: 50, Mould: 1, Cosmic: 50, Water: 12},
		Left: 50, CraftOK: true, EnchantOK: true, BankOf: bank,
		// Falador Teleport with a Staff of air: water and law.
		Travel: map[string]int{RuneWater: 1, RuneLaw: 1},
	}
	got := drawMap(PlanJewelleryDraws(in))
	if got[RuneWater] != 11 || got[RuneLaw] != 1 || got[GoldBar] != 10 {
		t.Errorf("a load and a teleport: %v", got)
	}
	// The ID of a rune known only by name is 0, for the caller to resolve.
	for _, d := range PlanJewelleryDraws(in) {
		if d.Name == RuneLaw && d.ID != 0 {
			t.Errorf("law rune ID %d", d.ID)
		}
		if d.Name == RuneWater && d.ID != RuneWaterID {
			t.Errorf("water rune ID %d", d.ID)
		}
	}
	// The Water the enchants leave is not enough for the teleport: none of its runes are drawn.
	in.Bank.Water = 10
	got = drawMap(PlanJewelleryDraws(in))
	if got[RuneWater] != 10 || got[RuneLaw] != 0 {
		t.Errorf("no Water to spare: %v", got)
	}
	// A rune the bank lacks: the others are not drawn either.
	in.Bank.Water = 12
	in.BankOf = bankOfMap(map[string]int{RuneFire: 5})
	got = drawMap(PlanJewelleryDraws(in))
	if got[RuneWater] != 10 || got[RuneLaw] != 0 {
		t.Errorf("no Law rune: %v", got)
	}
	// Wrapping up (Left 0): the teleport home's runes alone.
	in = JewelleryVisit{Bank: JewelleryHeld{Cosmic: 50, Water: 50}, Left: 0, EnchantOK: true, BankOf: bank,
		Travel: map[string]int{RuneFire: 1, RuneLaw: 1}}
	if got := drawMap(PlanJewelleryDraws(in)); !reflect.DeepEqual(got, map[string]int{RuneFire: 1, RuneLaw: 1}) {
		t.Errorf("wrapping up: %v", got)
	}
}

// The biggest load the planner can draw, with every stack it can carry, fits the 28-slot pack.
func TestPlanJewelleryDrawsAlwaysFitThePack(t *testing.T) {
	bank := bankOfMap(map[string]int{RuneLaw: 9, RuneFire: 9, RuneAir: 99})
	for _, paid := range []bool{false, true} {
		for _, shop := range []bool{false, true} {
			for _, necklaces := range []int{0, 3, 40, 500} {
				for _, left := range []int{0, 1, 7, 10, 11, 60, 400} {
					in := JewelleryVisit{
						Bank: JewelleryHeld{Bars: 400, Sapphires: 400, Necklaces: necklaces, Mould: boolInt(!shop), Cosmic: 500, Water: 500, Coins: 900},
						Left: left, CraftOK: true, EnchantOK: true, WaterPaid: paid, ShopOK: shop, BankOf: bank,
						Travel: map[string]int{RuneAir: 3, RuneFire: 1, RuneLaw: 1, RuneWater: 1},
					}
					slots, stacks := 0, map[string]bool{}
					for _, d := range PlanJewelleryDraws(in) {
						switch d.Name {
						case GoldBar, CutSapphire, SapphireNecklace, NecklaceMould:
							slots += d.Count
						default:
							if !stacks[d.Name] {
								stacks[d.Name] = true
								slots++
							}
						}
					}
					if slots > JewelleryPackSlots {
						t.Errorf("paid %v shop %v necklaces %d left %d: %d slots", paid, shop, necklaces, left, slots)
					}
				}
			}
		}
	}
}

func TestJewelleryVisitFor(t *testing.T) {
	magic, paid := 40, map[string]bool{RuneAir: true}
	v := JewelleryView{Where: JewelleryAtPost, Asked: true, CraftOK: true, EnchantOK: true, Left: 50,
		Bank: JewelleryHeld{Bars: 50, Sapphires: 50, Mould: 1, Cosmic: 50, Water: 50}}
	after := v.Bank
	// At the post with crafting to do: the Falador Teleport's runes.
	in := JewelleryVisitFor(v, after, nil, magic, paid)
	if in.Left != 50 || !reflect.DeepEqual(in.Travel, map[string]int{RuneWater: 1, RuneLaw: 1}) || !in.ShopOK {
		t.Errorf("post, crafting: %+v", in)
	}
	// At the post with only enchants to do: no teleport.
	v2 := v
	v2.Bank = JewelleryHeld{Necklaces: 8, Cosmic: 50, Water: 50}
	if in := JewelleryVisitFor(v2, v2.Bank, nil, magic, paid); in.Left != 50 || len(in.Travel) != 0 {
		t.Errorf("post, enchanting: %+v", in)
	}
	// At the work bank, working: no teleport; wrapping up: the way home's, and nothing left to make.
	v3 := v
	v3.Where = JewelleryAtWorkBank
	if in := JewelleryVisitFor(v3, after, nil, magic, paid); len(in.Travel) != 0 || in.Left != 50 {
		t.Errorf("work bank, working: %+v", in)
	}
	v3.Left = 0
	if in := JewelleryVisitFor(v3, after, nil, magic, paid); in.Left != 0 || !reflect.DeepEqual(in.Travel, map[string]int{RuneFire: 1, RuneLaw: 1}) {
		t.Errorf("work bank, wrapping up: %+v", in)
	}
	// Futile and expired wrap up too; a failed shop says so.
	v3.Left, v3.Futile = 50, true
	if in := JewelleryVisitFor(v3, after, nil, magic, paid); in.Left != 0 {
		t.Errorf("futile: %+v", in)
	}
	v3.Futile, v3.Expired, v3.ShopFailed = false, true, true
	if in := JewelleryVisitFor(v3, after, nil, magic, paid); in.Left != 0 || in.ShopOK {
		t.Errorf("expired: %+v", in)
	}
}

func jview(mod func(v *JewelleryView)) JewelleryView {
	v := JewelleryView{
		Where: JewelleryAtPost, Asked: true, CraftOK: true, EnchantOK: true, Left: 10,
		Bank: JewelleryHeld{Bars: 10, Sapphires: 10, Mould: 1, Cosmic: 10, Water: 10},
	}
	if mod != nil {
		mod(&v)
	}
	return v
}

func TestNextJewelleryStep(t *testing.T) {
	loaded := JewelleryHeld{Mould: 1, Bars: 10, Sapphires: 10, Cosmic: 10, Water: 10}
	for _, tc := range []struct {
		name string
		v    JewelleryView
		want JewelleryStep
	}{
		{"not asked: ask", jview(func(v *JewelleryView) { v.Asked = false }), JewelleryStepAsk},
		{"not asked, far away: ask (it walks there itself)", jview(func(v *JewelleryView) { v.Asked = false; v.Where = JewelleryAtFurnace }), JewelleryStepAsk},
		{"handed over: done", jview(func(v *JewelleryView) { v.HandedOver = true }), JewelleryStepDone},

		{"materials in the bank, at the post: bank", jview(nil), JewelleryStepBank},
		{"materials in the bank, at the work bank: bank", jview(func(v *JewelleryView) { v.Where = JewelleryAtWorkBank; v.NearWork = true }), JewelleryStepBank},
		{"materials in the bank, notes in the pack: bank", jview(func(v *JewelleryView) { v.PackOther = 4 }), JewelleryStepBank},
		{"materials in the bank, at the furnace: to the bank", jview(func(v *JewelleryView) { v.Where = JewelleryAtFurnace; v.NearWork = true }), JewelleryStepToWork},
		{"materials in the bank, far from both: to the work bank (crafting)", jview(func(v *JewelleryView) { v.Where = JewelleryAtOther }), JewelleryStepToWork},

		{"a loaded pack at the post: to the furnace", jview(func(v *JewelleryView) { v.Pack, v.Bank = loaded, JewelleryHeld{} }), JewelleryStepToFurnace},
		{"a loaded pack at the work bank: to the furnace", jview(func(v *JewelleryView) { v.Pack, v.Bank = loaded, JewelleryHeld{}; v.Where = JewelleryAtWorkBank }), JewelleryStepToFurnace},
		{"a loaded pack at the furnace: craft", jview(func(v *JewelleryView) { v.Pack, v.Bank = loaded, JewelleryHeld{}; v.Where = JewelleryAtFurnace }), JewelleryStepCraft},
		{"a loaded pack, mid-road: to the furnace", jview(func(v *JewelleryView) { v.Pack, v.Bank = loaded, JewelleryHeld{}; v.Where = JewelleryAtOther }), JewelleryStepToFurnace},

		{"necklaces and runes in the pack: enchant", jview(func(v *JewelleryView) {
			v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Mould: 1, Cosmic: 10, Water: 10}, JewelleryHeld{}
			v.Where = JewelleryAtFurnace
		}), JewelleryStepEnchant},
		{"necklaces, a Staff of water: enchant", jview(func(v *JewelleryView) {
			v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10}, JewelleryHeld{}
			v.WaterPaid = true
		}), JewelleryStepEnchant},
		{"necklaces in the pack, no Cosmic: nothing can be enchanted, wrap up", jview(func(v *JewelleryView) {
			v.Pack, v.Bank = JewelleryHeld{Necklaces: 10, Water: 10}, JewelleryHeld{}
			v.Where = JewelleryAtFurnace
		}), JewelleryStepToWork},
		{"necklaces in the bank, runes, at the post: bank (no furnace needed)", jview(func(v *JewelleryView) {
			v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 10}
		}), JewelleryStepBank},
		{"necklaces in the bank, at the furnace: to the bank", jview(func(v *JewelleryView) {
			v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 10}
			v.Where, v.NearWork = JewelleryAtFurnace, true
		}), JewelleryStepToWork},
		{"necklaces in the bank, far from the work, not at the post: home to bank them", jview(func(v *JewelleryView) {
			v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 10}
			v.Where = JewelleryAtOther
		}), JewelleryStepHome},
		{"necklaces in the bank, on the Falador side: to its bank", jview(func(v *JewelleryView) {
			v.Bank = JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 10}
			v.Where, v.NearWork = JewelleryAtOther, true
		}), JewelleryStepToWork},
		{"necklaces and bars both: the bars are crafted first", jview(func(v *JewelleryView) {
			v.Pack, v.Bank = JewelleryHeld{Mould: 1, Bars: 5, Sapphires: 5, Necklaces: 5, Cosmic: 10, Water: 10}, JewelleryHeld{}
			v.Where = JewelleryAtFurnace
		}), JewelleryStepCraft},

		// The mould.
		{"no mould, coins in the pack: shop", jview(func(v *JewelleryView) {
			v.Pack = JewelleryHeld{Bars: 10, Sapphires: 10, Cosmic: 10, Water: 10, Coins: MouldCoins}
			v.Bank = JewelleryHeld{}
		}), JewelleryStepShop},
		{"no mould, no coins in the pack, at a bank: bank (draws the coins)", jview(func(v *JewelleryView) {
			v.Bank.Mould, v.Bank.Coins = 0, 5000
		}), JewelleryStepBank},
		{"no mould, no coins, at the furnace: to the bank", jview(func(v *JewelleryView) {
			v.Bank.Mould, v.Bank.Coins = 0, 5000
			v.Where, v.NearWork = JewelleryAtFurnace, true
		}), JewelleryStepToWork},
		{"the mould bought, loaded: to the furnace", jview(func(v *JewelleryView) {
			v.Pack = JewelleryHeld{Mould: 1, Bars: 10, Sapphires: 10, Cosmic: 10, Water: 10, Coins: 95}
			v.Bank = JewelleryHeld{}
			v.Where = JewelleryAtShop
		}), JewelleryStepToFurnace},
		{"the shop failed, nothing else to make: wrap up", jview(func(v *JewelleryView) {
			v.Bank.Mould = 0
			v.ShopFailed = true
		}), JewelleryStepDone},
		{"the shop failed, necklaces to enchant: carry on with those", jview(func(v *JewelleryView) {
			v.Bank = JewelleryHeld{Bars: 10, Sapphires: 10, Necklaces: 4, Cosmic: 10, Water: 10}
			v.ShopFailed = true
			v.Left = 10
		}), JewelleryStepBank},

		// Wrapping up.
		{"nothing left to make, at the post, clean, nothing owed: done", jview(func(v *JewelleryView) { v.Left = 0 }), JewelleryStepDone},
		{"nothing left, at the post, necklaces owed: hand over", jview(func(v *JewelleryView) { v.Left, v.Owed = 0, 10 }), JewelleryStepHandover},
		{"nothing left, at the post, a dirty pack: bank first", jview(func(v *JewelleryView) {
			v.Left, v.Owed = 0, 10
			v.Pack = JewelleryHeld{Games: 10}
		}), JewelleryStepBank},
		{"nothing left, at the work bank, a dirty pack: bank", jview(func(v *JewelleryView) {
			v.Left, v.Owed, v.Where = 0, 10, JewelleryAtWorkBank
			v.Pack = JewelleryHeld{Games: 10, Mould: 1, Cosmic: 2}
		}), JewelleryStepBank},
		{"nothing left, at the work bank, clean: home", jview(func(v *JewelleryView) {
			v.Left, v.Owed, v.Where = 0, 10, JewelleryAtWorkBank
		}), JewelleryStepHome},
		{"nothing left, at the furnace, a dirty pack: to the bank", jview(func(v *JewelleryView) {
			v.Left, v.Where = 0, JewelleryAtFurnace
			v.Pack = JewelleryHeld{Games: 10, Mould: 1}
		}), JewelleryStepToWork},
		{"nothing left, at the shop, a clean pack: home", jview(func(v *JewelleryView) { v.Left, v.Where = 0, JewelleryAtShop }), JewelleryStepHome},
		{"nothing left, mid-road on the Falador side with the pack: to the bank", jview(func(v *JewelleryView) {
			v.Left, v.Where, v.NearWork = 0, JewelleryAtOther, true
			v.Pack = JewelleryHeld{Games: 10}
		}), JewelleryStepToWork},
		{"nothing left, mid-road elsewhere: home", jview(func(v *JewelleryView) {
			v.Left, v.Where = 0, JewelleryAtOther
			v.Pack = JewelleryHeld{Games: 10}
		}), JewelleryStepHome},
		{"expired: wrap up", jview(func(v *JewelleryView) { v.Expired = true; v.Owed = 3 }), JewelleryStepHandover},
		{"futile: wrap up", jview(func(v *JewelleryView) { v.Futile = true; v.Where = JewelleryAtWorkBank }), JewelleryStepHome},
		{"no Cosmic anywhere: nothing can be made", jview(func(v *JewelleryView) { v.Bank.Cosmic = 0 }), JewelleryStepDone},
		{"enchanting refused: nothing can be made", jview(func(v *JewelleryView) { v.EnchantOK = false }), JewelleryStepDone},
		{"crafting refused, no necklaces: nothing", jview(func(v *JewelleryView) { v.CraftOK = false }), JewelleryStepDone},
		{"crafting refused, necklaces in the bank: enchant them", jview(func(v *JewelleryView) {
			v.CraftOK = false
			v.Bank = JewelleryHeld{Necklaces: 6, Cosmic: 10, Water: 10}
		}), JewelleryStepBank},
	} {
		if got := NextJewelleryStep(tc.v); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// A resumed round (Asked from the start) on the Falador side with a loaded pack goes straight to
// work; one with only necklaces in the bank never leaves the post.
func TestNextJewelleryStepResumes(t *testing.T) {
	v := jview(func(v *JewelleryView) {
		v.Where, v.NearWork = JewelleryAtFurnace, true
		v.Pack = JewelleryHeld{Mould: 1, Bars: 6, Sapphires: 6, Cosmic: 6, Water: 6}
		v.Bank = JewelleryHeld{}
		v.Left = 6
	})
	if got := NextJewelleryStep(v); got != JewelleryStepCraft {
		t.Errorf("a loaded pack at the furnace: %s", got)
	}
	v = jview(func(v *JewelleryView) { v.Bank = JewelleryHeld{Necklaces: 20, Cosmic: 20, Water: 20}; v.Left = 20 })
	seq := []JewelleryStep{NextJewelleryStep(v)}
	v.Pack, v.Bank = JewelleryHeld{Necklaces: 20, Cosmic: 20, Water: 20}, JewelleryHeld{}
	seq = append(seq, NextJewelleryStep(v))
	v.Pack, v.Left, v.Owed = JewelleryHeld{Games: 20}, 0, 20
	seq = append(seq, NextJewelleryStep(v))
	v.Pack, v.Bank = JewelleryHeld{}, JewelleryHeld{Games: 20}
	seq = append(seq, NextJewelleryStep(v))
	v.HandedOver = true
	seq = append(seq, NextJewelleryStep(v))
	want := []JewelleryStep{JewelleryStepBank, JewelleryStepEnchant, JewelleryStepBank, JewelleryStepHandover, JewelleryStepDone}
	if !reflect.DeepEqual(seq, want) {
		t.Errorf("enchant-only at the post: %v, want %v", seq, want)
	}
}
