package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The Fletching card as the /api/loot body actually delivers it (owner, 2026-09-21: "arrow
// shafts are showing up in the woodcutting section probably should have a fletching section for
// all fletching materials the fleet makes").
//
// internal/loot's own tests prove a Source's GroupKey; this proves the aggregator that the panel
// reads carries that grouping through, counts the fletch events under their own action so the
// header tile has a number, and keeps chopped logs where they were.

func fletched(materialID int, material string, shafts int64) loot.Totals {
	return loot.Totals{
		Source: loot.Fletching(nil, materialID, material),
		Events: shafts,
		Items:  []loot.Item{{ID: loot.ObjArrowShaft, Name: "Arrow shaft", Count: shafts, Value: 1}},
	}
}

func chopped(locType int, tree string, logID int, logName string, logs int64) loot.Totals {
	return loot.Totals{
		Source: loot.Woodcutting(nil, locType, tree),
		Events: logs,
		Items:  []loot.Item{{ID: logID, Name: logName, Count: logs, Value: 4}},
	}
}

// TestFletchingAndWoodcuttingAreTwoCardsInTheLootBody.
//
// One account chops and fletches, which is exactly what ProgressiveWoodcutter's Draynor start
// camp does and exactly why the two got confused: the same account, the same minute, the same
// pack. They must come back as two separate card groups with the right items in each.
func TestFletchingAndWoodcuttingAreTwoCardsInTheLootBody(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))

	a.loot.record(lootReading("camper1", 1,
		chopped(1276, "Tree", 1511, "Logs", 20),
		fletched(1511, "Logs", 300),
	), base)

	lifetime, _, _, _ := a.loot.body()

	cards := map[string]*LootSourceView{}
	for i := range lifetime.Sources {
		s := &lifetime.Sources[i]
		cards[s.Source.GroupKey()] = s
	}
	wood := loot.Woodcutting(nil, 1276, "Tree").GroupKey()
	fletch := loot.Fletching(nil, 1511, "Logs").GroupKey()
	if wood == fletch {
		t.Fatal("the two sources share a card group; the split under test does not exist")
	}
	if len(cards) != 2 {
		t.Fatalf("got %d card groups, want 2 (Woodcutting and Fletching)", len(cards))
	}

	w, ok := cards[wood]
	if !ok {
		t.Fatalf("no Woodcutting card in %v", cards)
	}
	if w.Events != 20 {
		t.Errorf("Woodcutting events = %d, want the 20 logs chopped", w.Events)
	}
	for _, it := range w.Items {
		if it.ID == loot.ObjArrowShaft {
			t.Errorf("%d arrow shafts are still filed under Woodcutting", it.Count)
		}
	}

	f, ok := cards[fletch]
	if !ok {
		t.Fatalf("no Fletching card in %v", cards)
	}
	if f.Source.Skill != loot.SkillFletching {
		t.Errorf("Fletching card skill = %q", f.Source.Skill)
	}
	if f.Events != 300 {
		t.Errorf("Fletching events = %d, want the 300 shafts made", f.Events)
	}
	if len(f.Items) != 1 || f.Items[0].ID != loot.ObjArrowShaft || f.Items[0].Count != 300 {
		t.Errorf("Fletching items = %+v, want 300 arrow shafts", f.Items)
	}

	// The header tiles read straight off Events[action], so "Items fletched" needs its own key
	// and must not have been added to the chop count.
	if lifetime.Events[loot.ActionFletch] != 300 {
		t.Errorf("Events[%q] = %d, want 300", loot.ActionFletch, lifetime.Events[loot.ActionFletch])
	}
	if lifetime.Events[loot.ActionChop] != 20 {
		t.Errorf("Events[%q] = %d, want 20", loot.ActionChop, lifetime.Events[loot.ActionChop])
	}
}

// TestEveryFletchedMaterialSharesOneCardsAccountCount.
//
// GroupAccounts is computed server-side because the panel cannot recover it from per-source
// counts (see LootSourceView.GroupAccounts, and the Fishing spot card that read 154 of 269).
// Two accounts cutting two different materials are two accounts on one Fletching card, not one.
func TestEveryFletchedMaterialSharesOneCardsAccountCount(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))

	a.loot.record(lootReading("camper1", 1, fletched(1511, "Logs", 150)), base)
	a.loot.record(lootReading("camper2", 1, fletched(1521, "Oak logs", 90)), base)

	lifetime, _, _, _ := a.loot.body()
	if len(lifetime.Sources) != 2 {
		t.Fatalf("got %d sources, want 2 (one row per material)", len(lifetime.Sources))
	}
	for _, s := range lifetime.Sources {
		if s.Accounts != 1 {
			t.Errorf("%s: per-source accounts = %d, want 1", s.Source.Name, s.Accounts)
		}
		if s.GroupAccounts != 2 {
			t.Errorf("%s: card accounts = %d, want 2 — both materials are one Fletching card", s.Source.Name, s.GroupAccounts)
		}
	}
	if lifetime.Events[loot.ActionFletch] != 240 {
		t.Errorf("Events[fletch] = %d, want 240", lifetime.Events[loot.ActionFletch])
	}
}
