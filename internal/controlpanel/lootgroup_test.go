package controlpanel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// TestGroupAccountsCountsTheUnionNotTheMaximum is the Fishing spot card's account figure.
//
// Three accounts fish, and no single spot is fished by more than two of them. They merge into
// one card, so the honest number is 3 — the union. The panel used to take the MAXIMUM per-source
// count, which is 2 here and read 154 against 269 logged in on the live fleet (owner,
// 2026-09-20). Summing would give 4, more accounts than exist, which is why the maximum was
// chosen in the first place.
func TestGroupAccountsCountsTheUnionNotTheMaximum(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))

	spot := func(id int) loot.Source {
		return loot.Fishing(nil, id)
	}
	caught := func(id int, n int64) loot.Totals {
		return loot.Totals{Source: spot(id), Events: n, Items: []loot.Item{{ID: 317, Name: "Raw shrimps", Count: n, Value: 5}}}
	}
	// Spot 316 is fished by fisher1 and fisher2; spot 309 by fisher2 and fisher3.
	a.loot.record(lootReading("fisher1", 1, caught(316, 4)), base)
	a.loot.record(lootReading("fisher2", 1, caught(316, 3), caught(309, 2)), base)
	a.loot.record(lootReading("fisher3", 1, caught(309, 5)), base)

	p := a.lootSnapshot().Lifetime
	first, ok := sourceView(p, spot(316))
	if !ok {
		t.Fatalf("no 316 source in %+v", p.Sources)
	}
	second, ok := sourceView(p, spot(309))
	if !ok {
		t.Fatalf("no 309 source in %+v", p.Sources)
	}
	if first.Accounts != 2 || second.Accounts != 2 {
		t.Fatalf("per-source accounts = %d and %d; want 2 and 2", first.Accounts, second.Accounts)
	}
	if first.GroupAccounts != 3 || second.GroupAccounts != 3 {
		t.Fatalf("group accounts = %d and %d; want 3 and 3 (the union of {fisher1,fisher2} and {fisher2,fisher3})",
			first.GroupAccounts, second.GroupAccounts)
	}
}

// TestLoneSourceGroupAccountsMatchesItsOwnCount: a source that is a card on its own must report
// the same number both ways, so nothing changes for the overwhelming majority of cards.
func TestLoneSourceGroupAccountsMatchesItsOwnCount(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.record(lootReading("thief1", 1, loot.Totals{Source: testMan, Events: 1}), base)
	a.loot.record(lootReading("thief2", 1, loot.Totals{Source: testMan, Events: 1}), base)

	v, ok := sourceView(a.lootSnapshot().Lifetime, testMan)
	if !ok {
		t.Fatal("no Man source")
	}
	if v.Accounts != 2 || v.GroupAccounts != 2 {
		t.Fatalf("lone source = %d accounts, %d group; want 2 and 2", v.Accounts, v.GroupAccounts)
	}
}

// TestEventOwnItemsAreNotRewards is the other half of the owner's report: "it also shows a
// strange box and net loot from the fishing spot. net shouldnt be included and strangebox should
// be part of mysterious old man loot as he hands them out".
//
// The line that matters is between an event's OWN item and its rewards. Refusing the former
// costs nothing; refusing the latter would throw away real loot, because the reward tables
// overlap other sources.
func TestEventOwnItemsAreNotRewards(t *testing.T) {
	if !loot.IsEventOwnItem(3062) {
		t.Fatal("the Strange box (3062) should be an event's own item, so another source can refuse it")
	}
	// A Casket must NOT be, even though the Mysterious Old Man hands them out: the Big net
	// fishing spot gives Caskets too, and skipping the id would lose real fishing loot.
	if loot.IsEventOwnItem(405) {
		t.Fatal("a Casket is a reward, not an event's own item; skipping it would lose real loot")
	}
	// Likewise a feather, which the maze pays and fly fishing needs.
	if loot.IsEventOwnItem(314) {
		t.Fatal("a feather is a reward, not an event's own item")
	}
	if loot.IsEventOwnItem(317) {
		t.Fatal("Raw shrimps should never be an event's own item")
	}
}

// TestLegacyFishingRecordsRestoreOntoTheOneGatheringCard is the owner's two-fishing-cards bug,
// reproduced from the file that causes it and then fixed.
//
// The file is written by hand in the shape a pre-1491f91d build really persisted (read off the
// live panel's own /api/loot: kind "fishing", no skill, no model) rather than produced by
// calling today's constructors, because today's constructors cannot produce it — which is the
// whole point. A build that quietly stopped migrating would pass a test that built its input
// with loot.Fishing.
func TestLegacyFishingRecordsRestoreOntoTheOneGatheringCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	// 313 and 322 are the two spots the live fleet actually recorded under the retired kind.
	old := `{
  "trackingSince": 1726700000000,
  "loot": {
    "accounts": {
      "fisher1": {
        "fish:fishing spot:313": {"source": {"kind": "fishing", "action": "fish", "name": "Fishing spot", "id": 313, "model": "npc"},
          "events": 120, "items": [{"id": 353, "name": "Raw mackerel", "count": 90, "value": 30}], "first": 1726700000000, "last": 1726700100000},
        "fish:fishing spot:322": {"source": {"kind": "fishing", "action": "fish", "name": "Fishing spot", "id": 322, "model": "npc"},
          "events": 80, "items": [{"id": 341, "name": "Raw cod", "count": 60, "value": 25}], "first": 1726700000000, "last": 1726700100000}
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator(path, nil, fixedNow(base))
	// A catch recorded by THIS build, under the current shape, by a different account.
	a.loot.record(lootReading("fisher2", 1, loot.Totals{Source: loot.Fishing(nil, 320), Events: 5,
		Items: []loot.Item{{ID: 317, Name: "Raw shrimps", Count: 5, Value: 5}}}), base)

	p := a.lootSnapshot().Lifetime
	groups := map[string]int{}
	for _, s := range p.Sources {
		if s.Source.Action != loot.ActionFish {
			continue
		}
		if s.Source.Kind != loot.KindGathering || s.Source.Skill != loot.SkillFishing {
			t.Fatalf("a fishing source came back as %+v; the retired kind survived the restore", s.Source)
		}
		groups[s.Source.GroupKey()]++
	}
	if len(groups) != 1 || groups["gathering|fishing"] != 3 {
		t.Fatalf("fishing sources landed on %d cards (%v); want all three on gathering|fishing", len(groups), groups)
	}
	// The per-spot rows survive under their own persisted keys: nothing was merged away, and
	// the counts are the ones the file carried.
	for key, want := range map[string]int64{"fish:fishing spot:313": 120, "fish:fishing spot:322": 80, "fish:fishing spot:320": 5} {
		var found *LootSourceView
		for i := range p.Sources {
			if p.Sources[i].Key == key {
				found = &p.Sources[i]
			}
		}
		if found == nil {
			t.Fatalf("persisted key %q disappeared; the migration moved it", key)
		}
		if found.Events != want {
			t.Fatalf("%s events = %d, want %d", key, found.Events, want)
		}
		// Every spot is on one card, so each reports the union of the card's accounts.
		if found.GroupAccounts != 2 {
			t.Fatalf("%s group accounts = %d, want 2 (fisher1 and fisher2 on one card)", key, found.GroupAccounts)
		}
	}
}
