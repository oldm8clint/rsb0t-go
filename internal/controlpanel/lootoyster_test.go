package controlpanel

import (
	"testing"

	"rsb0t-go/internal/loot"
)

const oysterFile = `{
  "trackingSince": 1726700000000,
  "loot": {
    "accounts": {
      "fisher1": {
        "open:oyster:407": {"source": {"kind": "openable", "action": "open", "name": "Oyster", "id": 407},
          "events": 10, "items": [{"id": 411, "name": "Oyster pearl", "count": 10, "value": 112}], "first": 1726700000000, "last": 1726700100000},
        "fish:fishing spot:316": {"source": {"kind": "gathering", "action": "fish", "name": "Fishing spot", "id": 316, "model": "npc", "skill": "Fishing"},
          "events": 40, "items": [{"id": 407, "name": "Oyster", "count": 10, "value": 200}], "first": 1726700000000, "last": 1726700100000}
      }
    }
  }
}`

// TestOystersOpenedRestoreAsTheOysterPearlsRowWithNoClosedOyster: the owner's "merge it into the
// fishing tab" and "the closed oyster shouldnt show anywhere on the loot panel just oyster pearls"
// (7 Oct 2026), against a file written when oysters were an openable and caught oysters were loot.
func TestOystersOpenedRestoreAsTheOysterPearlsRowWithNoClosedOyster(t *testing.T) {
	a := restoreLootFile(t, oysterFile)
	a.loot.prices = priced(map[string]MarketPrice{
		"oyster":       {Name: "Oyster", Coins: 150, Trades: 40, Units: 900},
		"oyster pearl": {Name: "Oyster pearl", Coins: 300, Trades: 40, Units: 900},
	})
	p := a.lootSnapshot().Lifetime
	cards := map[string]bool{}
	var open LootSourceView
	for _, s := range p.Sources {
		cards[s.Source.GroupKey()] = true
		if s.Source.Action == loot.ActionOpen {
			open = s
		}
		for _, it := range s.Items {
			if loot.HiddenItem(it.ID) {
				t.Fatalf("%s still shows %+v", s.Source.Name, it)
			}
		}
	}
	if len(cards) != 1 || !cards["gathering|fishing"] {
		t.Fatalf("sources on cards %v, want the one Fishing card", cards)
	}
	if open.Source.Name != loot.OysterPearlsName || open.Cost != 0 || open.CostItem != "" {
		t.Fatalf("oyster row = %+v, want Oyster pearls with no oyster cost", open)
	}
	// 10 pearls at 300 and nothing else: the caught oysters are not loot and cost nothing to open.
	if p.Gross != 3000 || p.Cost != 0 || p.Value != 3000 {
		t.Fatalf("gross %d cost %d net %d, want 3000/0/3000", p.Gross, p.Cost, p.Value)
	}
}
