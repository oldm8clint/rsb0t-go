package controlpanel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/loot"
)

// lootReading builds one LootTotals reading for one account.
func lootReading(account string, epoch int64, sources ...loot.Totals) []fleet.AccountLoot {
	return []fleet.AccountLoot{{Account: account, Epoch: epoch, Sources: sources}}
}

var (
	testCasket = loot.Openable("Casket", 405)
	testMan    = loot.Source{Kind: loot.KindNPC, Action: loot.ActionPickpocket, Name: "Man", ID: 1, Level: 2}
)

func casketTotals(events, coins, sapphires int64) loot.Totals {
	t := loot.Totals{Source: testCasket, Events: events}
	if coins > 0 {
		t.Items = append(t.Items, loot.Item{ID: 995, Name: "Coins", Count: coins, Value: 1})
	}
	if sapphires > 0 {
		t.Items = append(t.Items, loot.Item{ID: 1623, Name: "Uncut sapphire", Count: sapphires, Value: 25})
	}
	return t
}

func sourceView(p LootPeriod, src loot.Source) (LootSourceView, bool) {
	for _, s := range p.Sources {
		if s.Key == src.Key() {
			return s, true
		}
	}
	return LootSourceView{}, false
}

func itemCount(v LootSourceView, id int) int64 {
	for _, it := range v.Items {
		if it.ID == id {
			return it.Count
		}
	}
	return 0
}

// TestLootAggregatorAddsOnlyTheDifference proves each poll adds what arrived since the last
// reading, never the running total again, and that an unchanged epoch is skipped.
func TestLootAggregatorAddsOnlyTheDifference(t *testing.T) {
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))

	a.loot.record(lootReading("fisher1", 1, casketTotals(2, 40, 1)), base)
	a.loot.record(lootReading("fisher1", 1, casketTotals(2, 40, 1)), base.Add(10*time.Second)) // same epoch: skipped
	a.loot.record(lootReading("fisher1", 2, casketTotals(5, 360, 1)), base.Add(20*time.Second))

	body := a.lootSnapshot()
	v, ok := sourceView(body.Lifetime, testCasket)
	if !ok {
		t.Fatalf("no casket source in %+v", body.Lifetime)
	}
	if v.Events != 5 || itemCount(v, 995) != 360 || itemCount(v, 1623) != 1 {
		t.Fatalf("casket lifetime = %d opened, %d coins, %d sapphires; want 5, 360, 1", v.Events, itemCount(v, 995), itemCount(v, 1623))
	}
	// Items counts the sapphire only. Coins are counted as coins and not also as items:
	// the panel has a separate tile for them, and counting them twice made "items looted"
	// read 3,683,761 live when 3,580,168 of that was the coin figure already shown beside
	// it. Value still includes the coins, because that total is explicitly what it is worth.
	// Value is what the loot is WORTH, not what the cache says it costs. With no market price
	// for a sapphire, it falls back to high alchemy -- 60% of the cache's 25, so 15, not 25.
	// Coins stay at 1 each: a coin is worth a coin, stated explicitly in PriceBook.Value
	// because 60% of 1 rounds to nothing.
	if body.Lifetime.Coins != 360 || body.Lifetime.Items != 1 || body.Lifetime.Value != 360+15 {
		t.Fatalf("period totals %+v", body.Lifetime)
	}
	if body.Lifetime.Events["open:Casket"] != 5 || body.Lifetime.Events[loot.ActionOpen] != 5 {
		t.Fatalf("opened counts %v", body.Lifetime.Events)
	}
	// Two polls carried items, so two ticker entries; the newest is the 320-coin one.
	if len(body.Recent) != 2 || body.Recent[0].Items[0].Count != 320 {
		t.Fatalf("recent = %+v", body.Recent)
	}
	if len(body.History) != 1 || body.History[0].Items["995"] != 360 {
		t.Fatalf("history = %+v", body.History)
	}
}

// TestLootAggregatorPersistsAcrossRestart proves lifetime loot survives a restart while the
// since-restart view starts again, and that the new process's fleet tallies (which restart at
// zero) are added on top rather than being mistaken for a drop in the totals.
func TestLootAggregatorPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	a1 := newStatsAggregator(path, nil, fixedNow(base))
	a1.loot.record(lootReading("fisher1", 3, casketTotals(4, 80, 2)), base)
	a1.loot.record(lootReading("thief1", 1, loot.Totals{Source: testMan, Events: 10, Items: []loot.Item{{ID: 995, Name: "Coins", Count: 30}}}), base)
	a1.close()

	a2 := newStatsAggregator(path, nil, fixedNow(base.Add(time.Hour)))
	a2.loot.record(lootReading("fisher1", 1, casketTotals(1, 20, 0)), base.Add(time.Hour))
	body := a2.lootSnapshot()

	life, _ := sourceView(body.Lifetime, testCasket)
	if life.Events != 5 || itemCount(life, 995) != 100 || itemCount(life, 1623) != 2 {
		t.Fatalf("lifetime casket after restart = %+v", life)
	}
	if man, ok := sourceView(body.Lifetime, testMan); !ok || man.Events != 10 || man.Source.Level != 2 {
		t.Fatalf("lifetime Man after restart = %+v (found %v)", man, ok)
	}
	since, _ := sourceView(body.SinceRestart, testCasket)
	if since.Events != 1 || itemCount(since, 995) != 20 {
		t.Fatalf("since-restart casket = %+v, want only this process's 1 open and 20 coins", since)
	}
	if _, ok := sourceView(body.SinceRestart, testMan); ok {
		t.Fatal("since-restart shows a source only the previous process saw")
	}
	if len(body.Recent) != 3 {
		t.Fatalf("recent ticker lost across the restart: %d entries", len(body.Recent))
	}
}

// TestStatsFileWithoutLootStillLoads proves a stats file written before the loot tracker
// existed loads with its accounts intact and loot at zero, and that the next save keeps both.
func TestStatsFileWithoutLootStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	old := `{
  "trackingSince": 1726700000000,
  "accounts": {
    "fisher1": {"firstSeen": 1726700000000, "lastSeen": 1726700100000, "lifetimeConnectedSecs": 50, "deaths": 3, "randomsCleared": 7, "randomsFailed": 1, "reconnects": 2}
  }
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator(path, nil, fixedNow(base))
	if rec := a.accounts["fisher1"]; rec == nil || rec.Deaths != 3 || rec.RandomsCleared != 7 {
		t.Fatalf("old account record not loaded: %+v", rec)
	}
	if body := a.lootSnapshot(); len(body.Lifetime.Sources) != 0 || body.TrackingSince != 1726700000000 {
		t.Fatalf("old file produced loot or lost trackingSince: %+v", body)
	}

	a.loot.record(lootReading("fisher1", 1, casketTotals(1, 20, 0)), base)
	a.close()

	var saved statsFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Accounts["fisher1"] == nil || saved.Accounts["fisher1"].Deaths != 3 {
		t.Fatalf("save dropped the old account stats: %+v", saved.Accounts)
	}
	if saved.Loot == nil || saved.Loot.Accounts["fisher1"][testCasket.Key()] == nil {
		t.Fatalf("save did not persist the loot: %s", data)
	}
}

// TestLootAggregatorFromFakeFleet runs the whole path the panel uses: the fake fleet's
// simulated drops through LootSource into /api/loot's body.
func TestLootAggregatorFromFakeFleet(t *testing.T) {
	f := NewFakeSource(12, 7)
	for i := 0; i < 400; i++ {
		f.Step()
	}
	a := newStatsAggregator("", f, time.Now)
	a.poll()
	body := a.lootSnapshot()
	if !body.Available || len(body.Lifetime.Sources) == 0 {
		t.Fatalf("fake fleet produced no loot: available=%v sources=%d", body.Available, len(body.Lifetime.Sources))
	}
	// Coins are excluded from Items and counted in Coins instead, so the two together are
	// what the tallies hold. Summing every item including coins would assert the old
	// double-count that made the panel's item total meaningless.
	var fromTally, coinsFromTally int64
	for _, acct := range f.LootTotals() {
		for _, s := range acct.Sources {
			for _, it := range s.Items {
				if it.ID == coinsID {
					coinsFromTally += it.Count
					continue
				}
				fromTally += it.Count
			}
		}
	}
	if body.Lifetime.Items != fromTally {
		t.Fatalf("aggregated %d non-coin items, the fake tallies hold %d", body.Lifetime.Items, fromTally)
	}
	if body.Lifetime.Coins != coinsFromTally {
		t.Fatalf("aggregated %d coins, the fake tallies hold %d", body.Lifetime.Coins, coinsFromTally)
	}
	if body.Lifetime.Items+body.Lifetime.Coins != fromTally+coinsFromTally {
		t.Fatalf("items %d + coins %d do not account for everything looted (%d)", body.Lifetime.Items, body.Lifetime.Coins, fromTally+coinsFromTally)
	}
}
