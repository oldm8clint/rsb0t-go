package controlpanel

import (
	"testing"

	"rsb0t-go/internal/deathloot"
	"rsb0t-go/internal/loot"
)

const cactusID = 3138

func closedDeath(acct string, count, taken int64) deathloot.Death {
	d := deathloot.Death{ID: acct, Account: acct, At: 1000, Items: []deathloot.Stack{{ID: cactusID, Name: "Potato cactus", Count: count}, {ID: coinsID, Name: "Coins", Count: 9000}}}
	if taken > 0 {
		d.Items[0].Recovered = []deathloot.Recovery{{By: "other", Count: taken, At: 2000}}
	}
	return d
}

func TestDeathLossesByCountsOnlyClosedUnrecoveredNonCoin(t *testing.T) {
	d := closedDeath("a", 100, 30)
	late := d.WindowEnd() + 1
	got := deathLossesBy([]deathloot.Death{d}, late, 0)
	if got["a"][cactusID] != 70 || len(got["a"]) != 1 {
		t.Fatalf("losses %v", got)
	}
	if got := deathLossesBy([]deathloot.Death{d}, d.WindowEnd()-1, 0); got != nil {
		t.Errorf("open pile deducted: %v", got)
	}
	if got := deathLossesBy([]deathloot.Death{d}, late, d.At+1); got != nil {
		t.Errorf("death before the period deducted: %v", got)
	}
}

func TestDeductLostIsNetWithRawAndBoundedByTheRows(t *testing.T) {
	items := []loot.Item{{ID: cactusID, Name: "Potato cactus", Count: 40}, {ID: 526, Name: "Bones", Count: 5}}
	lost := map[int]int64{cactusID: 70}
	got := deductLost(items, lost)
	if got[0].Count != 0 || got[0].Raw != 40 || got[1].Count != 5 || got[1].Raw != 0 {
		t.Fatalf("net %+v", got)
	}
	if lost[cactusID] != 30 {
		t.Errorf("left to deduct on the next row: %d", lost[cactusID])
	}
	if items[0].Count != 40 {
		t.Error("input modified")
	}
	if same := deductLost(items, nil); &same[0] != &items[0] {
		t.Error("nothing to deduct should return the input")
	}
}

func TestPeriodViewDeductsDeathsAndKeepsRaw(t *testing.T) {
	m := map[string]map[string]*lootRec{
		"a": {"pickup": {Source: loot.Pickup(nil, cactusID, "Potato cactus"), Events: 10, Items: []loot.Item{{ID: cactusID, Name: "Potato cactus", Count: 522}}}},
	}
	lost := map[string]map[int]int64{"a": {cactusID: 362}}
	p := periodView(m, priced(map[string]MarketPrice{}), creditBook{}, lost)
	if len(p.Sources) != 1 {
		t.Fatalf("sources %d", len(p.Sources))
	}
	it := p.Sources[0].Items[0]
	if it.Count != 160 || it.Raw != 522 {
		t.Errorf("item %+v", it)
	}
	if raw := periodView(m, priced(map[string]MarketPrice{}), creditBook{}, nil).Sources[0].Items[0]; raw.Count != 522 {
		t.Errorf("no deaths: %+v", raw)
	}
}
