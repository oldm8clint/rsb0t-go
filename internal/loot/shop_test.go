package loot

import "testing"

func TestShopPurchasesSplitsCoinsByCost(t *testing.T) {
	drops := ShopPurchases([]Item{
		{ID: 562, Name: "Chaos rune", Count: 100, Value: 90},
		{ID: 561, Name: "Nature rune", Count: 50, Value: 180},
	}, 18000)
	if len(drops) != 2 {
		t.Fatalf("drops = %d, want 2", len(drops))
	}
	var total int64
	for _, d := range drops {
		if d.Source.Kind != KindShop || d.Source.Skill != SkillShopPurchases || d.Source.Action != ActionBuy {
			t.Fatalf("source = %+v", d.Source)
		}
		if len(d.Spent) != 1 || d.Spent[0].ID != CoinsID {
			t.Fatalf("spent = %+v", d.Spent)
		}
		if d.Spent[0].Count != 9000 {
			t.Fatalf("%s paid %d, want 9000 (equal cache weight)", d.Source.Name, d.Spent[0].Count)
		}
		total += d.Spent[0].Count
	}
	if total != 18000 {
		t.Fatalf("coins shared = %d, want 18000", total)
	}
	if drops[0].Source.Name != "Nature rune" || drops[0].Events != 50 {
		t.Fatalf("first drop = %+v", drops[0])
	}
}

func TestShopPurchasesNeedsBothHalves(t *testing.T) {
	if d := ShopPurchases(nil, 100); d != nil {
		t.Fatalf("no items: %+v", d)
	}
	if d := ShopPurchases([]Item{{ID: 562, Count: 5}}, 0); d != nil {
		t.Fatalf("no coins: %+v", d)
	}
	if d := ShopPurchases([]Item{{ID: CoinsID, Count: 5}}, 10); d != nil {
		t.Fatalf("coins are never a purchase: %+v", d)
	}
}

func TestShopBuyIsOneCard(t *testing.T) {
	a, b := ShopBuy("Chaos rune", 562), ShopBuy("Mind rune", 558)
	if a.GroupKey() != b.GroupKey() {
		t.Fatalf("group keys differ: %q %q", a.GroupKey(), b.GroupKey())
	}
	if a.Key() == b.Key() {
		t.Fatalf("rows share a key: %q", a.Key())
	}
}
