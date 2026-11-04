package controlpanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/loot"
)

// Clearing a card or one item off it (lootclear.go; a user request: "loot panel should have ability
// to reset/clear panels and certain stacks of items from npcs (to clear up previous data not tracking
// right)"). Every case builds the aggregator the way the panel's data really arrives, through
// record, so the cards, the since-restart view, the ticker and the history all exist to be cleared.

var (
	testCow    = loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Cow", ID: 81, Level: 2}
	testCow2   = loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Cow", ID: 397, Level: 2}
	testChick  = loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Chicken", ID: 41, Level: 1}
	testNature = loot.Pickup(nil, 561, "Nature rune")
	testGenie  = loot.Source{Kind: loot.KindRandomEvent, Action: loot.ActionEvent, Name: "Genie", ID: 409, Model: loot.ModelNPC}
	testMaze   = loot.Source{Kind: loot.KindRandomEvent, Action: loot.ActionEvent, Name: "Maze", ID: -1}
	testGuard  = loot.NPC(nil, loot.ActionPickpocket, 32, "Guard")
	testMan1   = loot.NPC(nil, loot.ActionPickpocket, 1, "Man")
	testCoinsX = loot.Item{ID: 995, Name: "Coins", Value: 1}
)

func clearItem(id int, name string, count int64) loot.Item {
	return loot.Item{ID: id, Name: name, Count: count, Value: 100}
}

func clearTotals(src loot.Source, events int64, items ...loot.Item) loot.Totals {
	return loot.Totals{Source: src, Events: events, Items: items}
}

// seedClearable is a fleet's loot over two history buckets: two cow ids on two accounts (one Cow card),
// a chicken, a pickup, a Genie and a Maze on one account, all of them with Feathers somewhere.
func seedClearable(a *statsAggregator, base time.Time) {
	feather := func(n int64) loot.Item { return clearItem(314, "Feather", n) }
	bones := func(n int64) loot.Item { return clearItem(526, "Bones", n) }
	coins := func(n int64) loot.Item { c := testCoinsX; c.Count = n; return c }
	a.loot.record(lootReading("cow1", 1, clearTotals(testCow, 10, feather(5), bones(3))), base)
	a.loot.record(lootReading("cow2", 1, clearTotals(testCow2, 4, feather(2), coins(40))), base)
	a.loot.record(lootReading("chick1", 1, clearTotals(testChick, 6, feather(12))), base)
	a.loot.record(lootReading("nat1", 1, clearTotals(testNature, 7, clearItem(561, "Nature rune", 7))), base)
	a.loot.record(lootReading("evt1", 1,
		clearTotals(testGenie, 3, clearItem(2528, "Lamp", 3)),
		clearTotals(testMaze, 2, coins(100), feather(4))), base)
	// A bucket later the cow tallies grew: cow1 by two kills, a feather and a bone, and cow2 by a
	// Feather alone, which is a ticker row holding nothing else.
	later := base.Add(25 * time.Minute)
	a.loot.record(lootReading("cow1", 2, clearTotals(testCow, 12, feather(6), bones(4))), later)
	a.loot.record(lootReading("cow2", 2, clearTotals(testCow2, 4, feather(3), coins(40))), later.Add(time.Second))
}

func groupsOf(p LootPeriod) map[string]bool {
	out := map[string]bool{}
	for _, s := range p.Sources {
		out[s.Source.GroupKey()] = true
	}
	return out
}

func accountNames(p LootPeriod) map[string]bool {
	out := map[string]bool{}
	for _, a := range p.Accounts {
		out[a.Account] = true
	}
	return out
}

func recentFor(rows []LootRecent, group string) (n int) {
	for _, r := range rows {
		if r.Source.GroupKey() == group {
			n++
		}
	}
	return n
}

// TestClearCardRemovesItEverywhere: a whole card goes from the lifetime view, the since-restart view,
// the ticker and the history's per-source counts, and nothing else moves.
func TestClearCardRemovesItEverywhere(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	seedClearable(a, base)
	before := a.lootSnapshot()
	cow := testCow.GroupKey()
	if !groupsOf(before.Lifetime)[cow] || !groupsOf(before.SinceRestart)[cow] || recentFor(before.Recent, cow) != 4 {
		t.Fatalf("seed has no Cow card to clear: %v, %d ticker rows", groupsOf(before.Lifetime), recentFor(before.Recent, cow))
	}
	if len(before.History) != 2 {
		t.Fatalf("seed history = %d buckets, want 2", len(before.History))
	}

	res := a.clearLoot(lootClearRequest{Group: cow})
	if !res.touched() || res.Lifetime.Records != 2 || res.Lifetime.Accounts != 2 || res.Lifetime.Events != 16 || res.Lifetime.Title != "Cow" {
		t.Fatalf("clear reported %+v, want the two Cow records on two accounts with their 16 kills", res)
	}
	after := a.lootSnapshot()

	for name, p := range map[string]LootPeriod{"lifetime": after.Lifetime, "since restart": after.SinceRestart} {
		if groupsOf(p)[cow] {
			t.Errorf("%s still has the Cow card", name)
		}
		// An account left with nothing must not stay behind as a zero row in the account table.
		if names := accountNames(p); names["cow1"] || names["cow2"] {
			t.Errorf("%s still lists the cow accounts: %v", name, names)
		}
		for _, other := range []loot.Source{testChick, testNature, testGenie, testMaze} {
			if _, ok := sourceView(p, other); !ok {
				t.Errorf("%s lost %s, which was never cleared", name, other.Key())
			}
		}
	}
	// The totals follow: the cow's 13 non-coin units (9 Feathers, 4 Bones) and 40 coins are out, and
	// everything else is as it was.
	if before.Lifetime.Items-after.Lifetime.Items != 9+4 || before.Lifetime.Coins-after.Lifetime.Coins != 40 {
		t.Errorf("lifetime totals went items %d -> %d, coins %d -> %d; want exactly the cow's 13 units and 40 coins out",
			before.Lifetime.Items, after.Lifetime.Items, before.Lifetime.Coins, after.Lifetime.Coins)
	}
	chickBefore, _ := sourceView(before.Lifetime, testChick)
	chickAfter, _ := sourceView(after.Lifetime, testChick)
	if chickBefore.Events != chickAfter.Events || itemCount(chickBefore, 314) != itemCount(chickAfter, 314) || itemCount(chickAfter, 314) != 12 {
		t.Errorf("the chicken's card changed: %+v -> %+v", chickBefore, chickAfter)
	}

	if n := recentFor(after.Recent, cow); n != 0 || len(after.Recent) != len(before.Recent)-4 {
		t.Errorf("ticker has %d Cow rows and %d rows in all, want none and %d", n, len(after.Recent), len(before.Recent)-4)
	}
	if len(after.History) != 2 {
		t.Fatalf("the clear changed the history's time axis: %d buckets", len(after.History))
	}
	for i, b := range after.History {
		for k := range b.Events {
			if strings.HasPrefix(k, "kill:cow:") {
				t.Errorf("history bucket %d still counts %s", i, k)
			}
		}
		if got, want := b.Events["kill:chicken:41"], before.History[i].Events["kill:chicken:41"]; got != want {
			t.Errorf("history bucket %d chicken kills %d, want %d", i, got, want)
		}
		// The per-item gains are the whole fleet's, by item id: a clear cannot split the cow's share
		// out of them (lootclear.go says so), so they are exactly as they were.
		for id, n := range before.History[i].Items {
			if b.Items[id] != n {
				t.Errorf("history bucket %d item %s went %d -> %d", i, id, n, b.Items[id])
			}
		}
	}
	// A second clear of the same card finds nothing, and says so.
	if again := a.clearLoot(lootClearRequest{Group: cow}); again.touched() {
		t.Errorf("clearing a cleared card removed %+v", again)
	}
}

// TestClearItemRemovesOnlyThatItem: one Feather stack comes off the Cow card for every account and both
// periods; the kills, the card's other items and every other card's Feathers stay.
func TestClearItemRemovesOnlyThatItem(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	seedClearable(a, base)
	before := a.lootSnapshot()
	cow := testCow.GroupKey()
	feather := 314

	res := a.clearLoot(lootClearRequest{Group: cow, Item: &feather})
	if !res.touched() || res.Lifetime.Records != 2 || res.Lifetime.Units != 6+3 || res.Lifetime.ItemName != "Feather" {
		t.Fatalf("clear reported %+v, want the 9 Feathers on the two Cow records", res)
	}
	after := a.lootSnapshot()

	for name, p := range map[string]LootPeriod{"lifetime": after.Lifetime, "since restart": after.SinceRestart} {
		for _, src := range []loot.Source{testCow, testCow2} {
			v, ok := sourceView(p, src)
			if !ok {
				t.Fatalf("%s lost the %s record: an item clear must keep the card", name, src.Key())
			}
			if itemCount(v, 314) != 0 {
				t.Errorf("%s %s still has %d Feathers", name, src.Key(), itemCount(v, 314))
			}
		}
		cow1, _ := sourceView(p, testCow)
		cow2, _ := sourceView(p, testCow2)
		if cow1.Events != 12 || cow2.Events != 4 || itemCount(cow1, 526) != 4 || itemCount(cow2, 995) != 40 {
			t.Errorf("%s: the cow's kills or its other items changed: %+v / %+v", name, cow1, cow2)
		}
		// Feathers on a different card are another card's drops, not this one's.
		if v, _ := sourceView(p, testChick); itemCount(v, 314) != 12 {
			t.Errorf("%s: the chicken's Feathers went with them: %d", name, itemCount(v, 314))
		}
		if v, _ := sourceView(p, testMaze); itemCount(v, 314) != 4 {
			t.Errorf("%s: the Maze's Feathers went with them: %d", name, itemCount(v, 314))
		}
	}
	if before.Lifetime.Items-after.Lifetime.Items != 9 {
		t.Errorf("lifetime items %d -> %d, want exactly 9 Feathers out", before.Lifetime.Items, after.Lifetime.Items)
	}
	// And what they were worth: the period's value falls by exactly those Feathers, not by a card's.
	unit, _ := a.loot.prices.Value("Feather", 100)
	if got := before.Lifetime.Value - after.Lifetime.Value; unit == 0 || got != 9*unit {
		t.Errorf("lifetime value fell by %d, want 9 Feathers at %d each", got, unit)
	}
	// Ticker: the rows keep their other items, and the one that held a Feather alone is gone.
	if n := recentFor(after.Recent, cow); n != 3 {
		t.Errorf("ticker has %d Cow rows, want 3 (the Feather-only one dropped)", n)
	}
	for _, r := range after.Recent {
		for _, it := range r.Items {
			if it.ID == 314 && r.Source.GroupKey() == cow {
				t.Errorf("a Cow ticker row still shows a Feather: %+v", r)
			}
		}
		if len(r.Items) == 0 {
			t.Errorf("a ticker row with no items survived: %+v", r)
		}
	}
	// History is per source for events (unchanged: no event was removed) and per item for the fleet.
	for i, b := range after.History {
		if fmt.Sprint(b.Events) != fmt.Sprint(before.History[i].Events) || fmt.Sprint(b.Items) != fmt.Sprint(before.History[i].Items) {
			t.Errorf("history bucket %d changed on an item clear: %v / %v -> %v / %v", i, before.History[i].Events, before.History[i].Items, b.Events, b.Items)
		}
	}
	// An item the card does not hold is a no-op, not an error and not a card clear.
	nope := 9999
	if again := a.clearLoot(lootClearRequest{Group: cow, Item: &nope}); again.touched() {
		t.Errorf("clearing an item the card never held removed %+v", again)
	}
	if v, _ := sourceView(a.lootSnapshot().Lifetime, testCow); v.Events != 12 {
		t.Errorf("a no-op item clear touched the card: %+v", v)
	}
}

// TestClearThenNewLootCountsAgain: the fleet's own tallies keep their running totals through a clear, so
// the poll adds only what came after it. Nothing already cleared is added a second time, and the card
// is made afresh.
func TestClearThenNewLootCountsAgain(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	bones := func(n int64) loot.Item { return clearItem(526, "Bones", n) }
	a.loot.record(lootReading("cow1", 1, clearTotals(testCow, 5, bones(3))), base)
	a.clearLoot(lootClearRequest{Group: testCow.GroupKey()})

	// The same tally read again, and under the same epoch: nothing comes back.
	a.loot.record(lootReading("cow1", 1, clearTotals(testCow, 5, bones(3))), base.Add(10*time.Second))
	if groupsOf(a.lootSnapshot().Lifetime)[testCow.GroupKey()] {
		t.Fatal("the cleared card came back from an unchanged tally")
	}
	// The tally grew to 8 kills and 4 bones: the 3 kills and 1 bone since the clear count, not 8 and 4.
	later := base.Add(20 * time.Second)
	a.loot.record(lootReading("cow1", 2, clearTotals(testCow, 8, bones(4))), later)
	body := a.lootSnapshot()
	for name, p := range map[string]LootPeriod{"lifetime": body.Lifetime, "since restart": body.SinceRestart} {
		v, ok := sourceView(p, testCow)
		if !ok || v.Events != 3 || itemCount(v, 526) != 1 {
			t.Fatalf("%s Cow after the clear = %+v (found %v), want 3 kills and 1 bone: only what came after", name, v, ok)
		}
		if v.First != later.UnixMilli() {
			t.Errorf("%s Cow tracking starts at %d, want the first loot after the clear (%d)", name, v.First, later.UnixMilli())
		}
	}
	if len(body.Recent) != 1 || body.Recent[0].Items[0].Count != 1 {
		t.Errorf("ticker after the clear = %+v, want only the post-clear row", body.Recent)
	}
}

// lootStubSource is a Source whose loot tallies a test sets directly.
type lootStubSource struct {
	*staticSource
	readings []fleet.AccountLoot
}

func (s *lootStubSource) LootTotals() []fleet.AccountLoot { return s.readings }

// TestClearTakesInWhatTheFleetHadTalliedFirst: loot the fleet tallied in the seconds before the click,
// which the aggregator had not polled yet, goes with the clear instead of appearing after it.
func TestClearTakesInWhatTheFleetHadTalliedFirst(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	src := &lootStubSource{staticSource: &staticSource{}}
	a := newStatsAggregator("", src, fixedNow(base))
	src.readings = lootReading("cow1", 1, clearTotals(testCow, 5))
	a.pollLoot(base)
	// Three more kills are tallied. The next poll is up to 10 seconds away: the owner clicks now.
	src.readings = lootReading("cow1", 2, clearTotals(testCow, 8))
	res := a.clearLoot(lootClearRequest{Group: testCow.GroupKey()})
	if !res.touched() || res.Lifetime.Events != 8 {
		t.Fatalf("clear removed %+v, want all 8 kills the fleet had tallied", res)
	}
	a.pollLoot(base.Add(10 * time.Second))
	if groupsOf(a.lootSnapshot().Lifetime)[testCow.GroupKey()] {
		t.Fatal("the 3 kills tallied before the click came back on the next poll")
	}
	// And the tally keeps going from there.
	src.readings = lootReading("cow1", 3, clearTotals(testCow, 9))
	a.pollLoot(base.Add(20 * time.Second))
	if v, _ := sourceView(a.lootSnapshot().Lifetime, testCow); v.Events != 1 {
		t.Fatalf("Cow after the clear = %d kills, want the 1 since", v.Events)
	}
}

func readLootFile(t *testing.T, path string) *lootFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f statsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Loot == nil {
		t.Fatalf("the stats file has no loot: %s", data)
	}
	return f.Loot
}

// TestClearIsSavedAtOnceAndSurvivesARestart: the file is written by the clear itself, not by the next
// timer tick or a clean shutdown, so a process killed a second later still has it. A restart then
// restores the cleared state, with the audit of what was cleared, and later loot adds to it.
func TestClearIsSavedAtOnceAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator(path, nil, fixedNow(base))
	seedClearable(a, base)
	a.save()
	if f := readLootFile(t, path); f.Accounts["cow1"] == nil || len(f.Cleared) != 0 {
		t.Fatalf("setup: the file should hold the cow and no clears: %+v", f)
	}

	feather := 314
	a.clearLoot(lootClearRequest{Group: testNature.GroupKey()})
	a.clearLoot(lootClearRequest{Group: testCow.GroupKey(), Item: &feather})
	// No close() and no save(): the file already says it.
	f := readLootFile(t, path)
	if f.Accounts["nat1"] != nil {
		t.Errorf("the saved file still holds the cleared pickup card: %+v", f.Accounts["nat1"])
	}
	for _, rec := range f.Accounts["cow1"] {
		for _, it := range rec.Items {
			if it.ID == 314 {
				t.Errorf("the saved file still holds a Cow Feather: %+v", rec)
			}
		}
	}
	if len(f.Cleared) != 2 || f.Cleared[0].Group != "pickup|take|nature rune" || f.Cleared[0].Name != "Nature rune" || f.Cleared[0].At != base.UnixMilli() ||
		f.Cleared[1].Item == nil || *f.Cleared[1].Item != 314 || f.Cleared[1].ItemName != "Feather" || f.Cleared[1].Name != "Cow" {
		t.Errorf("audit list = %+v, want the pickup card and then the Cow's Feather, each with its time", f.Cleared)
	}

	// The restart: a new process reads the file.
	b := newStatsAggregator(path, nil, fixedNow(base.Add(time.Hour)))
	body := b.lootSnapshot()
	if groupsOf(body.Lifetime)[testNature.GroupKey()] {
		t.Error("the cleared pickup card is back after a restart")
	}
	cow, ok := sourceView(body.Lifetime, testCow)
	if !ok || cow.Events != 12 || itemCount(cow, 314) != 0 || itemCount(cow, 526) != 4 {
		t.Errorf("Cow after a restart = %+v (found %v), want 12 kills, no Feathers, 4 bones", cow, ok)
	}
	for _, bucket := range body.History {
		for k := range bucket.Events {
			if k == testNature.Key() {
				t.Errorf("history still counts the cleared pickup: %v", bucket.Events)
			}
		}
	}
	if saved := b.loot.persisted(); saved == nil || len(saved.Cleared) != 2 {
		t.Errorf("a restored aggregator dropped the audit list: %+v", saved)
	}
	// This process's tallies start from zero: what they add lands on the restored state.
	b.loot.record(lootReading("cow1", 1, clearTotals(testCow, 3)), base.Add(time.Hour))
	if cow, _ := sourceView(b.lootSnapshot().Lifetime, testCow); cow.Events != 15 {
		t.Errorf("Cow after the restart's own loot = %d kills, want 12 + 3", cow.Events)
	}
}

// TestClearedLegacyCardsAreNotRebuiltByTheFolds: restore's legacy moves (a Ground pickup folded onto the
// Cow, a Genie under a wrong id moved to the right one) only act on records a file still carries in an
// old shape. A cleared card is saved without any, so a restart cannot make it again.
func TestClearedLegacyCardsAreNotRebuiltByTheFolds(t *testing.T) {
	// The legacy file has the Cow's bones on an openable named Ground; restoring folds them onto the Cow.
	a := restoreLootFile(t, legacyLootFile)
	cow := testCow.GroupKey()
	if v, ok := sourceView(a.lootSnapshot().Lifetime, testCow); !ok || itemCount(v, 526) != 4 {
		t.Fatalf("setup: the legacy Ground bones did not fold onto the Cow: %+v", v)
	}
	if res := a.clearLoot(lootClearRequest{Group: cow}); !res.touched() || res.Lifetime.Units != 4 || res.History == 0 || res.Recent != 1 {
		t.Fatalf("clearing the folded Cow card removed %+v, want its 4 bones, its ticker row and its history counts", res)
	}
	raw, err := os.ReadFile(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ground") || strings.Contains(string(raw), `"kill:cow:81"`) {
		t.Errorf("the saved file still names the legacy record or the cleared Cow: %s", raw)
	}

	b := newStatsAggregator(a.path, nil, fixedNow(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)))
	body := b.lootSnapshot()
	for _, s := range body.Lifetime.Sources {
		if s.Source.GroupKey() == cow || loot.IsLegacyGroundPickup(s.Source) {
			t.Errorf("a restart rebuilt %s from the cleared file", s.Key)
		}
	}
	if len(body.Recent) != 0 {
		t.Errorf("the cleared Cow's ticker row came back: %+v", body.Recent)
	}
	for _, h := range body.History {
		if _, ok := h.Events["kill:cow:81"]; ok || h.Events["open:ground:-1"] != 0 {
			t.Errorf("history counts the cleared Cow again: %v", h.Events)
		}
	}
	// What was not cleared is untouched: the thieving card with its rows.
	if man, _ := sourceView(body.Lifetime, loot.NPC(nil, loot.ActionPickpocket, 1, "Man")); man.Events != 47 {
		t.Errorf("an unrelated card changed: Man = %+v", man)
	}

	// The same for the random events: a Genie recorded under the wrong id is moved onto 409 on the way
	// in, and clearing the one Random events card keeps both it and the Maze gone for good.
	const stale = `{"trackingSince": 1726700000000, "loot": {"accounts": {"evt1": {
	  "event:genie:411": {"source": {"kind": "randomevent", "action": "event", "name": "Genie", "id": 411, "model": "npc"},
	    "events": 5, "items": [{"id": 2528, "name": "Lamp", "count": 5, "value": 1}], "first": 1726700000000, "last": 1726700100000},
	  "event:maze:-1": {"source": {"kind": "randomevent", "action": "event", "name": "Maze", "id": -1},
	    "events": 2, "items": [{"id": 995, "name": "Coins", "count": 90, "value": 1}], "first": 1726700000000, "last": 1726700100000}}}}}`
	c := restoreLootFile(t, stale)
	if v, ok := sourceView(c.lootSnapshot().Lifetime, testGenie); !ok || v.Events != 5 {
		t.Fatalf("setup: the Genie was not moved onto its own id: %+v", v)
	}
	if res := c.clearLoot(lootClearRequest{Group: "randomevent"}); !res.touched() || res.Lifetime.Records != 2 {
		t.Fatalf("clearing the Random events card removed %+v, want both events", res)
	}
	d := newStatsAggregator(c.path, nil, fixedNow(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)))
	if got := groupsOf(d.lootSnapshot().Lifetime); len(got) != 0 {
		t.Errorf("a restart rebuilt cards from the cleared file: %v", got)
	}
}

// TestClearOneRowOfASkillCard: a part narrows a clear to one row of a merged card, and an item clear with
// a part takes the item off that row alone.
func TestClearOneRowOfASkillCard(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	coins := func(n int64) loot.Item { c := testCoinsX; c.Count = n; return c }
	a.loot.record(lootReading("thief1", 1,
		clearTotals(testMan1, 40, coins(120)),
		clearTotals(testGuard, 5, coins(150), clearItem(1895, "Slice of cake", 3))), base)
	a.loot.record(lootReading("thief2", 1, clearTotals(testGuard, 2, coins(60))), base)
	card, man, guard := testGuard.GroupKey(), testMan1.PartKey(), testGuard.PartKey()
	if card != "thieving|thieving" || testMan1.GroupKey() != card || man == guard {
		t.Fatalf("setup: Man and Guard should be two rows of one card: %q %q %q", card, man, guard)
	}

	// Coins off the Guard row only.
	c := 995
	res := a.clearLoot(lootClearRequest{Group: card, Part: guard, Item: &c})
	if !res.touched() || res.Lifetime.Records != 2 || res.Lifetime.Units != 150+60 {
		t.Fatalf("clear removed %+v, want the Guard's 210 coins on two accounts", res)
	}
	p := a.lootSnapshot().Lifetime
	g, _ := sourceView(p, testGuard)
	m, _ := sourceView(p, testMan1)
	if itemCount(g, 995) != 0 || g.Events != 7 || itemCount(g, 1895) != 3 || itemCount(m, 995) != 120 {
		t.Fatalf("after: Guard %+v, Man %+v; want the Guard's coins gone and the Man's kept", g, m)
	}
	// Then the Guard row as a whole: the Man row, and the card, stay.
	if res := a.clearLoot(lootClearRequest{Group: card, Part: guard}); !res.touched() || res.Lifetime.Records != 2 {
		t.Fatalf("row clear removed %+v", res)
	}
	p = a.lootSnapshot().Lifetime
	if _, ok := sourceView(p, testGuard); ok {
		t.Error("the Guard row survived its own clear")
	}
	if m, ok := sourceView(p, testMan1); !ok || m.Events != 40 || !groupsOf(p)[card] {
		t.Errorf("the Man row went with the Guard's: %+v (found %v)", m, ok)
	}
	if names := accountNames(p); names["thief2"] {
		t.Errorf("thief2 has nothing left but is still a row: %v", names)
	}
}

// TestClearRandomEventsIsOneCard: every random event is one card (loot.Source.GroupKey), so clearing
// it clears them all, while a part names a single event.
func TestClearRandomEventsIsOneCard(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	seedClearable(a, base)
	if testGenie.GroupKey() != "randomevent" || testMaze.GroupKey() != "randomevent" {
		t.Fatalf("setup: random events should share one group: %q %q", testGenie.GroupKey(), testMaze.GroupKey())
	}
	res := a.clearLoot(lootClearRequest{Group: "randomevent", Part: testMaze.PartKey()})
	if !res.touched() || res.Lifetime.Records != 1 || res.Lifetime.Title != "Random events" {
		t.Fatalf("clearing the Maze row removed %+v", res)
	}
	p := a.lootSnapshot().Lifetime
	if _, ok := sourceView(p, testMaze); ok {
		t.Error("the Maze survived")
	}
	if _, ok := sourceView(p, testGenie); !ok {
		t.Error("the Genie went with the Maze")
	}
	if res := a.clearLoot(lootClearRequest{Group: "randomevent"}); !res.touched() || res.Lifetime.Records != 1 {
		t.Fatalf("clearing the card removed %+v, want the Genie", res)
	}
	if groupsOf(a.lootSnapshot().Lifetime)["randomevent"] {
		t.Error("a Random events card is left")
	}
}

// TestClearItemDropsARecordItLeavesEmpty: a record that only ever held that item (no kills, no experience,
// nothing spent) is not kept as an empty card, and its account is not kept as a zero row.
func TestClearItemDropsARecordItLeavesEmpty(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.record(lootReading("a1", 1, clearTotals(testCasket, 0, clearItem(526, "Bones", 4))), base)
	a.loot.record(lootReading("a2", 1, clearTotals(testCasket, 3, clearItem(526, "Bones", 1), clearItem(995, "Coins", 5))), base)
	bones := 526
	if res := a.clearLoot(lootClearRequest{Group: testCasket.GroupKey(), Item: &bones}); !res.touched() || res.Lifetime.Records != 2 {
		t.Fatalf("clear removed %+v", res)
	}
	p := a.lootSnapshot().Lifetime
	if names := accountNames(p); names["a1"] || !names["a2"] {
		t.Errorf("accounts after = %v, want a1 (nothing left) gone and a2 kept", names)
	}
	if v, ok := sourceView(p, testCasket); !ok || v.Events != 3 || v.Accounts != 1 || itemCount(v, 995) != 5 {
		t.Errorf("casket after = %+v (found %v), want a2's record alone", v, ok)
	}
}

func TestClearRequestChecks(t *testing.T) {
	neg, huge, ok := -1, maxLootItemID+1, 561
	long := strings.Repeat("a", maxLootKeyBytes+1)
	for name, tc := range map[string]struct {
		req  lootClearRequest
		fine bool
	}{
		"card":                {lootClearRequest{Group: "npc|kill|cow"}, true},
		"item":                {lootClearRequest{Group: "npc|kill|cow", Item: &ok}, true},
		"row and item":        {lootClearRequest{Group: "thieving|thieving", Part: "thieving|pickpocket|man", Item: &ok}, true},
		"no group":            {lootClearRequest{}, false},
		"only a part":         {lootClearRequest{Part: "thieving|pickpocket|man"}, false},
		"negative item":       {lootClearRequest{Group: "g", Item: &neg}, false},
		"item past the cache": {lootClearRequest{Group: "g", Item: &huge}, false},
		"long group":          {lootClearRequest{Group: long}, false},
		"long part":           {lootClearRequest{Group: "g", Part: long}, false},
		"control in group":    {lootClearRequest{Group: "npc|kill|cow\n"}, false},
		"control in part":     {lootClearRequest{Group: "g", Part: "a\x00b"}, false},
	} {
		if msg := tc.req.check(); (msg == "") != tc.fine {
			t.Errorf("%s: check = %q, want fine=%v", name, msg, tc.fine)
		}
	}
}

// TestClearAuditListIsBounded: only the newest maxLootClears are kept, in the order they were made, and
// the log line names a card, never an account.
func TestClearAuditListIsBounded(t *testing.T) {
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	total := maxLootClears + 20
	var last lootCleared
	var lastReq lootClearRequest
	for i := 0; i < total; i++ {
		src := loot.Pickup(nil, 1000+i, fmt.Sprintf("Thing %d", i))
		a.loot.record(lootReading("secretaccount", int64(i+1), clearTotals(src, 1, clearItem(1000+i, "Thing", 1))), base)
		lastReq = lootClearRequest{Group: src.GroupKey()}
		last = a.clearLoot(lastReq)
	}
	got := a.loot.persisted().Cleared
	if len(got) != maxLootClears || got[0].Group != "pickup|take|thing 20" || got[len(got)-1].Group != fmt.Sprintf("pickup|take|thing %d", total-1) {
		t.Fatalf("audit list = %d entries from %q to %q, want the newest %d", len(got), got[0].Group, got[len(got)-1].Group, maxLootClears)
	}
	if line := last.describe(lastReq); strings.Contains(line, "secretaccount") || !strings.Contains(line, "pickup|take|thing") {
		t.Errorf("log line = %q: it should name the card and no account", line)
	}
	var f lootFile
	b := newLootAggregator()
	data, _ := json.Marshal(lootFile{Accounts: map[string]map[string]*lootRec{}, Cleared: make([]lootClear, maxLootClears+7)})
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	b.restore(&f)
	if len(b.cleared) != maxLootClears {
		t.Errorf("a file with %d clears restored %d, want it held to %d", maxLootClears+7, len(b.cleared), maxLootClears)
	}
}

// TestClearSavesAreSerialisedWithThePeriodicSave: a clear's save waits for any save already in flight, so
// an older snapshot cannot be renamed over it after it. Holding the save lock stands in for a slow write.
func TestClearSavesAreSerialisedWithThePeriodicSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator(path, nil, fixedNow(base))
	seedClearable(a, base)
	a.save()

	a.saveMu.Lock()
	done := make(chan lootCleared)
	go func() { done <- a.clearLoot(lootClearRequest{Group: testCow.GroupKey()}) }()
	select {
	case <-done:
		t.Fatal("the clear finished its save while another save held the lock")
	case <-time.After(150 * time.Millisecond):
	}
	// The aggregate is already cleared in memory, so a save that started before it could not have
	// included it; the file must not be one of those.
	if f := readLootFile(t, path); f.Accounts["cow1"] == nil {
		t.Fatal("the file lost the cow before any save ran")
	}
	a.saveMu.Unlock()
	select {
	case res := <-done:
		if !res.touched() {
			t.Errorf("clear removed nothing: %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the clear never finished once the lock was released")
	}
	if f := readLootFile(t, path); f.Accounts["cow1"] != nil || f.Accounts["cow2"] != nil {
		t.Errorf("the file still holds the cow after the clear returned: %+v", f.Accounts)
	}
}

// TestClearWhileTheFleetPollsAndSaves hammers the paths a clear shares with the poll and the periodic
// save, for a deadlock or a map written from two goroutines.
func TestClearWhileTheFleetPollsAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator(path, nil, fixedNow(base))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	feather := 314
	work := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
					f(i)
				}
			}
		}()
	}
	work(func(i int) {
		a.loot.record(lootReading("cow1", int64(i+1), clearTotals(testCow, int64(i+1), clearItem(314, "Feather", int64(i+1)))), base.Add(time.Duration(i)*time.Second))
	})
	work(func(int) { a.save(); time.Sleep(time.Millisecond) })
	work(func(i int) {
		req := lootClearRequest{Group: testCow.GroupKey()}
		if i%2 == 1 {
			req.Item = &feather
		}
		a.clearLoot(req)
		time.Sleep(time.Millisecond)
	})
	work(func(int) { a.lootSnapshot(); a.loot.persisted() })
	time.Sleep(300 * time.Millisecond)
	close(stop)
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("deadlock: the clear, the poll and the save never finished")
	}
	// Whatever order they ran in, one last clear leaves the card gone, in memory and on disk.
	a.clearLoot(lootClearRequest{Group: testCow.GroupKey()})
	if groupsOf(a.lootSnapshot().Lifetime)[testCow.GroupKey()] {
		t.Error("the Cow card survived the last clear")
	}
	if f := readLootFile(t, path); f.Accounts["cow1"] != nil {
		t.Errorf("the file holds the cow after the last clear: %+v", f.Accounts["cow1"])
	}
}

// ---- POST /api/loot/clear ----

func sameOriginJSON(h *harness) http.Header {
	return http.Header{"Origin": {h.ts.URL}, "Sec-Fetch-Site": {"same-origin"}, "Content-Type": {"application/json"}}
}

func clearPost(h *harness, body string, header http.Header) *http.Response {
	return h.request(http.MethodPost, "/api/loot/clear", strings.NewReader(body), header)
}

func newClearHarness(t *testing.T, cfg Config) (*harness, time.Time) {
	t.Helper()
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	h := newHarness(t, cfg, testOptions(), &staticSource{})
	seedClearable(h.srv.stats, base)
	return h, base
}

func TestLootClearEndpointClearsACardAndAnItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	h, _ := newClearHarness(t, Config{StatsStateFile: path})
	same := sameOriginJSON(h)

	// One item: answered with the new /api/loot body.
	res := clearPost(h, `{"group":"npc|kill|cow","item":314}`, same)
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("item clear: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	var body LootBody
	decode(t, res, &body)
	if v, ok := sourceView(body.Lifetime, testCow); !ok || itemCount(v, 314) != 0 || v.Events != 12 {
		t.Fatalf("the answer's Cow = %+v (found %v), want 12 kills and no Feathers", v, ok)
	}
	if v, _ := sourceView(body.Lifetime, testChick); itemCount(v, 314) != 12 {
		t.Errorf("the answer lost the chicken's Feathers: %+v", v)
	}
	// The next GET agrees and carries no trace of the audit list, and the file already says it.
	var fresh LootBody
	getRes := h.get("/api/loot")
	text := bodyText(t, getRes)
	if strings.Contains(text, "cleared") || strings.Contains(text, `"audit"`) {
		t.Errorf("/api/loot carries the audit list: %.200s", text)
	}
	if err := json.Unmarshal([]byte(text), &fresh); err != nil {
		t.Fatal(err)
	}
	if v, _ := sourceView(fresh.Lifetime, testCow); itemCount(v, 314) != 0 {
		t.Errorf("GET /api/loot after the clear still has Feathers: %+v", v)
	}
	if f := readLootFile(t, path); len(f.Cleared) != 1 {
		t.Errorf("the file's audit list = %+v", f.Cleared)
	}

	// One card.
	res = clearPost(h, `{"group":"pickup|take|nature rune"}`, same)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("card clear: %d %s", res.StatusCode, bodyText(t, res))
	}
	decode(t, res, &body)
	if groupsOf(body.Lifetime)["pickup|take|nature rune"] || !groupsOf(body.Lifetime)[testCow.GroupKey()] {
		t.Errorf("the answer's cards = %v", groupsOf(body.Lifetime))
	}
	if f := readLootFile(t, path); f.Accounts["nat1"] != nil || len(f.Cleared) != 2 {
		t.Errorf("the file after the card clear: nat1 %+v, %d clears", f.Accounts["nat1"], len(f.Cleared))
	}

	// What it named is gone, so naming it again is a 404 and says what happened.
	for _, tc := range []struct{ body, want string }{
		{`{"group":"pickup|take|nature rune"}`, "already have been cleared"},
		{`{"group":"npc|kill|cow","item":314}`, "none of that item"},
		{`{"group":"npc|kill|nothing"}`, "already have been cleared"},
	} {
		res := clearPost(h, tc.body, same)
		if res.StatusCode != http.StatusNotFound || !strings.Contains(bodyText(t, res), tc.want) {
			t.Errorf("%s: %d, want a 404 saying %q", tc.body, res.StatusCode, tc.want)
		}
	}
}

// TestLootClearEndpointRefusesWhatItShould: nothing but a same-origin, JSON, POST request with a
// well-formed body clears anything, and a body that does not parse strictly is not read as "clear the
// card".
func TestLootClearEndpointRefusesWhatItShould(t *testing.T) {
	h, _ := newClearHarness(t, Config{})
	same := sameOriginJSON(h)
	cows := func() int64 {
		v, _ := sourceView(h.srv.stats.lootSnapshot().Lifetime, testCow)
		return v.Events
	}
	if cows() != 12 {
		t.Fatalf("setup: Cow = %d kills", cows())
	}
	const card = `{"group":"npc|kill|cow"}`
	for name, tc := range map[string]struct {
		res  *http.Response
		want int
	}{
		"GET":                 {h.get("/api/loot/clear"), http.StatusMethodNotAllowed},
		"PUT":                 {h.request(http.MethodPut, "/api/loot/clear", strings.NewReader(card), same), http.StatusMethodNotAllowed},
		"cross-origin":        {clearPost(h, card, http.Header{"Origin": {"http://evil.example"}, "Content-Type": {"application/json"}}), http.StatusForbidden},
		"cross-site fetch":    {clearPost(h, card, http.Header{"Sec-Fetch-Site": {"cross-site"}, "Content-Type": {"application/json"}}), http.StatusForbidden},
		"a form post":         {clearPost(h, "group=npc%7Ckill%7Ccow", http.Header{"Origin": {h.ts.URL}, "Content-Type": {"application/x-www-form-urlencoded"}}), http.StatusUnsupportedMediaType},
		"text/plain":          {clearPost(h, card, http.Header{"Origin": {h.ts.URL}, "Content-Type": {"text/plain"}}), http.StatusUnsupportedMediaType},
		"not JSON":            {clearPost(h, `{"group":`, same), http.StatusBadRequest},
		"two values":          {clearPost(h, card+card, same), http.StatusBadRequest},
		"an array":            {clearPost(h, `["npc|kill|cow"]`, same), http.StatusBadRequest},
		"null":                {clearPost(h, `null`, same), http.StatusBadRequest},
		"no group":            {clearPost(h, `{}`, same), http.StatusBadRequest},
		"empty group":         {clearPost(h, `{"group":""}`, same), http.StatusBadRequest},
		"a misspelt item":     {clearPost(h, `{"group":"npc|kill|cow","itme":314}`, same), http.StatusBadRequest},
		"an unknown field":    {clearPost(h, `{"group":"npc|kill|cow","all":true}`, same), http.StatusBadRequest},
		"item as a string":    {clearPost(h, `{"group":"npc|kill|cow","item":"314"}`, same), http.StatusBadRequest},
		"a fractional item":   {clearPost(h, `{"group":"npc|kill|cow","item":3.5}`, same), http.StatusBadRequest},
		"a negative item":     {clearPost(h, `{"group":"npc|kill|cow","item":-1}`, same), http.StatusBadRequest},
		"group as a number":   {clearPost(h, `{"group":5}`, same), http.StatusBadRequest},
		"a control character": {clearPost(h, `{"group":"npc|kill|cow\n"}`, same), http.StatusBadRequest},
		"an oversized key":    {clearPost(h, `{"group":"`+strings.Repeat("a", maxLootKeyBytes+1)+`"}`, same), http.StatusBadRequest},
		"an oversized body":   {clearPost(h, `{"group":"`+strings.Repeat("a", 2*maxLootClearBytes)+`"}`, same), http.StatusRequestEntityTooLarge},
	} {
		if tc.res.StatusCode != tc.want {
			t.Errorf("%s: %d, want %d", name, tc.res.StatusCode, tc.want)
		}
	}
	if cows() != 12 {
		t.Fatalf("a refused request cleared something: Cow = %d kills", cows())
	}
	// A JSON null item is "no item", the same as leaving it out, which is a card clear: it was asked for.
	if res := clearPost(h, `{"group":"npc|kill|cow","item":null}`, same); res.StatusCode != http.StatusOK {
		t.Errorf("null item: %d", res.StatusCode)
	}
}

// TestLootClearEndpointNeedsTheToken: with a token configured, the clear is as protected as every other
// write: no token, a wrong one or a cookie for another value clears nothing.
func TestLootClearEndpointNeedsTheToken(t *testing.T) {
	const token = "s3cret token"
	h, _ := newClearHarness(t, Config{Token: token})
	same := sameOriginJSON(h)
	const card = `{"group":"npc|kill|cow"}`
	wrong := sameOriginJSON(h)
	wrong.Set("Authorization", "Bearer wrong")
	for name, header := range map[string]http.Header{"no token": same, "a wrong token": wrong} {
		if res := clearPost(h, card, header); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, res.StatusCode)
		}
	}
	if v, _ := sourceView(h.srv.stats.lootSnapshot().Lifetime, testCow); v.Events != 12 {
		t.Fatalf("an unauthorised request cleared the card: %+v", v)
	}
	ok := sameOriginJSON(h)
	ok.Set("Authorization", "Bearer "+token)
	if res := clearPost(h, card, ok); res.StatusCode != http.StatusOK {
		t.Errorf("with the token: %d", res.StatusCode)
	}
	if groupsOf(h.srv.stats.lootSnapshot().Lifetime)[testCow.GroupKey()] {
		t.Error("the authorised clear did nothing")
	}
}
