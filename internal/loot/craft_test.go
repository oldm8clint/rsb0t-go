package loot

import "testing"

func TestCraftingAndFletchingAreSeparateCards(t *testing.T) {
	c := Crafting(nil, 1623, "Uncut sapphire")
	f := Fletching(nil, 413, "Oyster pearls")
	if c.GroupKey() == f.GroupKey() {
		t.Fatal("Crafting and Fletching share a card")
	}
	if c.GroupKey() != Crafting(nil, 1779, "Flax").GroupKey() {
		t.Error("two crafting materials are not one Crafting card")
	}
	if Smithing(2351, "Iron ore").GroupKey() == c.GroupKey() || SuperheatCast(2351, "Iron ore", true).GroupKey() == c.GroupKey() || Cleaning(199, "Guam leaf").GroupKey() == c.GroupKey() {
		t.Error("Smithing or Herblore shares the Crafting card")
	}
}

func TestInputsNetBothInputsOfATwoInputRecipe(t *testing.T) {
	names := func(cs []Cost) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	cases := []struct {
		src  Source
		want []string
	}{
		{Fletching(nil, 46, "Pearl bolttips"), []string{"Pearl bolttips", "Bolts"}},
		// A chisel on a pearl uses the pearl alone, although bankxp lists Bolts on that row.
		{Fletching(nil, 413, "Oyster pearls"), []string{"Oyster pearls"}},
		{Fletching(nil, 411, "Oyster pearl"), []string{"Oyster pearl"}},
		{Fletching(nil, 1511, "Logs"), []string{"Logs"}},
		{Stringing(66, "Yew longbow"), []string{"Yew longbow (u)", "Bow string"}},
		{Crafting(nil, 1623, "Uncut sapphire"), []string{"Uncut sapphire"}},
		{Fletching(nil, 52, "Arrow shaft"), []string{"Arrow shaft", "Feather"}},
		{Fletching(nil, 41, "Steel arrowtips"), []string{"Steel arrowtips", "Headless arrow"}},
		{Fletching(nil, 44, "Rune arrowtips"), []string{"Rune arrowtips", "Headless arrow"}},
		{Smithing(2359, "Mithril ore"), []string{"Mithril ore", "Coal"}},
		{Smithing(2351, "Iron ore"), []string{"Iron ore"}},
		{SuperheatCast(2353, "Iron ore", true), []string{"Iron ore", "Coal", "Nature rune"}},
		{SuperheatCast(2353, "Iron ore", false), []string{"Iron ore", "Coal", "Nature rune", "Fire rune"}},
		{Alchemy(1127, "Rune platebody", true, true), []string{"Rune platebody", "Nature rune"}},
		{Alchemy(1127, "Rune platebody", true, false), []string{"Rune platebody", "Nature rune", "Fire rune"}},
	}
	for _, c := range cases {
		got := names(c.src.Inputs())
		if len(got) != len(c.want) {
			t.Errorf("%s: inputs %v, want %v", c.src.Name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: inputs %v, want %v", c.src.Name, got, c.want)
				break
			}
		}
	}
	if ins := Smithing(2359, "Mithril ore").Inputs(); ins[1].Per != 4 {
		t.Errorf("mithril coal per bar = %d, want 4", ins[1].Per)
	}
	if ins := Alchemy(1127, "Rune platebody", true, false).Inputs(); ins[2].Per != 5 {
		t.Errorf("high alchemy fire runes = %d, want 5", ins[2].Per)
	}
	if ins := SuperheatCast(2363, "Runite ore", false).Inputs(); ins[3].Per != 4 || ins[1].Per != 8 {
		t.Errorf("runite superheat inputs = %+v", ins)
	}
	if SuperheatCast(2353, "Iron ore", true).PartKey() == SuperheatCast(2353, "Iron ore", false).PartKey() {
		t.Error("a cast with a staff and one with runes share a row")
	}
}

func TestMadeReportsMeasuredCountsAndSkipsEmpty(t *testing.T) {
	var got []Drop
	sink := func(d Drop) { got = append(got, d) }
	Made(sink, nil, Crafting(nil, 1623, "Uncut sapphire"), 5, Product(1607, "Sapphire", 4), Product(1633, "Crushed gemstone", 1))
	Made(sink, nil, Crafting(nil, 1623, "Uncut sapphire"), 0, Product(1607, "Sapphire", 4))
	Made(sink, nil, Crafting(nil, 1623, "Uncut sapphire"), 3, Product(1607, "Sapphire", 0))
	Made(nil, nil, Crafting(nil, 1623, "Uncut sapphire"), 3, Product(1607, "Sapphire", 3))
	if len(got) != 1 {
		t.Fatalf("got %d drops, want 1", len(got))
	}
	if got[0].Events != 5 || len(got[0].Items) != 2 || got[0].Items[0].Count != 4 {
		t.Errorf("drop = %+v", got[0])
	}
}

// TestAnvilWorkIsPricedOnTheBarAlone: smelting adds a bar's coal to its cost, an anvil job does not
// (a steel bar worked into nails used no coal), and both stay on the one Smithing card.
func TestAnvilWorkIsPricedOnTheBarAlone(t *testing.T) {
	forge := Forging(2353, "Steel bar")
	if ins := forge.Inputs(); len(ins) != 1 || ins[0].Name != "Steel bar" {
		t.Fatalf("anvil inputs = %+v, want the bar alone", ins)
	}
	if ins := Smithing(2353, "Iron ore").Inputs(); len(ins) != 2 || ins[1].Name != "Coal" {
		t.Fatalf("smelting inputs = %+v, want the ore and its coal", ins)
	}
	if forge.GroupKey() != Smithing(2353, "Iron ore").GroupKey() {
		t.Error("smelting and anvil work are not one Smithing card")
	}
	if forge.PartKey() == Smithing(2353, "Iron ore").PartKey() {
		t.Error("an anvil row and a smelting row share a row")
	}
}

// TestMadeFromCountsOnlyWhatArrived: the pack's gain is the product, the materials and tools are not.
func TestMadeFromCountsOnlyWhatArrived(t *testing.T) {
	var got []Drop
	sink := func(d Drop) { got = append(got, d) }
	before := Counts{1511: 20, 946: 1, 995: 100}
	after := Counts{1511: 5, 946: 1, 995: 100, 52: 225, 1205: 2}
	MadeFrom(sink, nil, Fletching(nil, 1511, "Logs"), 15, before, after, func(id int) bool { return id == 946 })
	if len(got) != 1 || got[0].Events != 15 {
		t.Fatalf("drops = %+v", got)
	}
	by := map[int]int64{}
	for _, it := range got[0].Items {
		by[it.ID] = it.Count
	}
	if by[52] != 225 || by[1205] != 2 || len(by) != 2 {
		t.Fatalf("products = %v, want only what arrived", by)
	}
	// Nothing arrived, or no events, or no sink: nothing is reported.
	got = nil
	MadeFrom(sink, nil, Fletching(nil, 1511, "Logs"), 15, before, before, nil)
	MadeFrom(sink, nil, Fletching(nil, 1511, "Logs"), 0, before, after, nil)
	MadeFrom(nil, nil, Fletching(nil, 1511, "Logs"), 15, before, after, nil)
	if len(got) != 0 {
		t.Fatalf("reported %+v for nothing", got)
	}
}

// TestSpunAndCookedLandOnTheirOwnCards: bow strings and balls of wool on Crafting, cooked food on a
// Cooking card, each costing the material it used.
func TestSpunAndCookedLandOnTheirOwnCards(t *testing.T) {
	var got []Drop
	sink := func(d Drop) { got = append(got, d) }
	Spun(sink, nil, true, 28)
	Spun(sink, nil, false, 14)
	if len(got) != 2 || got[0].Source.Name != "Flax" || got[0].Items[0].ID != ObjBowString || got[1].Source.Name != "Wool" || got[1].Items[0].ID != ObjBallOfWool {
		t.Fatalf("spun = %+v", got)
	}
	cook := Cooking(nil, 377, "Raw lobster")
	if cook.Skill != SkillCooking || cook.GroupKey() == Crafting(nil, 1779, "Flax").GroupKey() {
		t.Fatalf("cooking source %+v", cook)
	}
	if c, ok := cook.Consumes(); !ok || c.ID != 377 || c.Per != 1 {
		t.Fatalf("a cook consumes %+v (%v), want the raw food", c, ok)
	}
}
