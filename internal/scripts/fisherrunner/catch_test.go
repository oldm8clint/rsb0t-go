package fisherrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fisherplan"
)

// TestEveryRungToolIsSkippedAsLoot couples the skip set to the ladder it is meant to cover.
//
// A net arriving in the pack is a re-buy, a bank withdrawal, or the tool a Big fish stole being
// picked back up — never a catch (owner, 2026-09-20: "net shouldnt be included"). The skip set
// matches by NAME, so a rung renaming its tool would silently start recording that tool as
// fishing loot again. This fails if that ever happens.
func TestEveryRungToolIsSkippedAsLoot(t *testing.T) {
	if len(fisherplan.Rungs) == 0 {
		t.Fatal("no rungs to check against")
	}
	for _, r := range fisherplan.Rungs {
		if !fishingKit[strings.ToLower(r.Tool)] {
			t.Errorf("rung %q uses tool %q, which the catch recorder would count as fishing loot", r.Label, r.Tool)
		}
	}
}

// TestFishingKitDoesNotSwallowRealCatches is the other side of it: the skip set must not name
// anything a fishing spot genuinely yields, or the card would quietly lose real loot.
func TestFishingKitDoesNotSwallowRealCatches(t *testing.T) {
	for _, name := range []string{
		"raw shrimps", "raw anchovies", "raw trout", "raw salmon", "raw lobster", "raw swordfish",
		"raw tuna", "raw mackerel", "raw cod", "raw bass", "seaweed", "casket", "oyster",
		"leather gloves", "leather boots", "oyster pearl",
	} {
		if fishingKit[name] {
			t.Errorf("%q is real fishing loot but the skip set drops it", name)
		}
	}
}

// TestEventOwnItemIsNeverACatch: the Strange box the Mysterious Old Man hands out was landing on
// the Fishing spot card because the account happened to be fishing when it arrived.
func TestEventOwnItemIsNeverACatch(t *testing.T) {
	if !loot.IsEventOwnItem(3062) {
		t.Fatal("the Strange box should be recognised as an event's own item")
	}
	// A Casket is a Big net yield as well as a Mysterious Old Man reward, so it must survive.
	if loot.IsEventOwnItem(405) {
		t.Fatal("a Casket must not be skipped: the Big net spot really does give them")
	}
}

// TestTaintedWindowReportsNothing proves the general defence, now living in loot.Window: a
// window a random event ran inside is thrown away rather than attributed to the spot. Without
// it, every hand-out, reward and recovered item lands on whatever the account happened to be
// doing. Driven here through the FISHING skip set, which is the part this package owns.
func TestTaintedWindowReportsNothing(t *testing.T) {
	var got []loot.Drop
	d := &Deps{}
	pack := loot.Counts{}
	w := loot.NewWindow(func(dr loot.Drop) { got = append(got, dr) }, nil,
		func() loot.Counts { return pack }, loot.Fishing(nil, 316), notACatch(d))
	if w == nil {
		t.Fatal("NewWindow returned nil with a real sink")
	}

	// A hold that caught three fish, interrupted by an event that handed over a Strange box.
	w.Got(3)
	w.Taint()
	pack = loot.Counts{317: 4, 3062: 1}
	w.Flush()
	if len(got) != 0 {
		t.Fatalf("a tainted window reported %+v; it must report nothing", got)
	}

	// The next window is clean, and the event's own item is still refused on its own.
	pack = loot.Counts{317: 8, 3062: 2}
	w.Flush()
	if len(got) != 1 {
		t.Fatalf("the window after a taint reported %d drops; want 1", len(got))
	}
	for _, it := range got[0].Items {
		if it.ID == 3062 {
			t.Fatalf("the Strange box was reported as fishing loot: %+v", got[0].Items)
		}
	}
	if len(got[0].Items) != 1 || got[0].Items[0].ID != 317 || got[0].Items[0].Count != 4 {
		t.Fatalf("reported items = %+v; want 4 more shrimps only", got[0].Items)
	}
}

// TestNilSinkMeasuresNothing: a script run by a bare test context must cost nothing and must not
// panic on any of the window's methods.
func TestNilSinkMeasuresNothing(t *testing.T) {
	if w := beginCatch(nil, 316); w != nil {
		t.Fatal("beginCatch with no Deps should return nil")
	}
	var w *loot.Window
	w.Got(5)
	w.Taint()
	w.Flush()
	if got := w.Done(7); got != 7 {
		t.Fatalf("Done on a nil window returned %d; want 7 unchanged", got)
	}
	if w.Due(time.Second) {
		t.Fatal("a nil window is never due")
	}
}

// bigNetNames is a name lookup for the Big net's whole catch, by made-up ids, so the rule is
// tested without a cache.
var bigNetNames = map[int]string{
	1: "Raw bass", 2: "Raw mackerel", 3: "Raw cod", 4: "Leather boots", 5: "Leather gloves",
	6: "Seaweed", 7: "Casket", 8: "Oyster", 9: "Oyster pearls", 10: "Big fishing net",
}

func bigNetName(id int) (string, bool) { n, ok := bigNetNames[id]; return n, ok }

// TestDroppedJunkIsNotRecorded (owner, 2026-10-04: "bassoystercaskets shouldnt track the items
// it drops"): a hold that netted the whole Big net catch reports only what BassOysterCaskets
// keeps, while ProgressiveFisher (no Junk) still reports everything it banks.
func TestDroppedJunkIsNotRecorded(t *testing.T) {
	report := func(junk func(string) bool) map[string]int64 {
		var got []loot.Drop
		pack := loot.Counts{10: 1}
		w := loot.NewWindow(func(dr loot.Drop) { got = append(got, dr) }, nil,
			func() loot.Counts { return pack }, loot.Fishing(nil, 316), notACatchNamed(bigNetName, junk))
		w.Got(2)
		pack = loot.Counts{10: 2, 1: 2, 2: 3, 3: 1, 4: 1, 5: 1, 6: 2, 7: 1, 8: 1, 9: 1}
		w.Flush()
		if len(got) != 1 {
			t.Fatalf("reported %d drops; want 1", len(got))
		}
		out := map[string]int64{}
		for _, it := range got[0].Items {
			out[bigNetNames[it.ID]] = it.Count
		}
		return out
	}

	kept := report(fisherplan.IsBassOysterJunk)
	want := map[string]int64{"Raw bass": 2, "Casket": 1, "Oyster": 1, "Oyster pearls": 1}
	if len(kept) != len(want) {
		t.Fatalf("BassOysterCaskets reported %v; want only %v", kept, want)
	}
	for name, n := range want {
		if kept[name] != n {
			t.Errorf("BassOysterCaskets reported %d %s; want %d (all reported: %v)", kept[name], name, n, kept)
		}
	}
	for _, name := range fisherplan.BassOysterJunk {
		if _, ok := kept[name]; ok {
			t.Errorf("BassOysterCaskets drops %s but reported it as loot: %v", name, kept)
		}
	}

	all := report(nil)
	for _, name := range []string{"Raw bass", "Raw mackerel", "Raw cod", "Leather boots", "Leather gloves", "Seaweed", "Casket", "Oyster", "Oyster pearls"} {
		if all[name] == 0 {
			t.Errorf("ProgressiveFisher banks %s but it was not reported: %v", name, all)
		}
	}
	if _, ok := all["Big fishing net"]; ok {
		t.Errorf("the net was reported as a catch: %v", all)
	}
}

// TestDroppedRawFishIsNotACatch: the card's catch count is the raw fish the script keeps, so a
// Raw mackerel or Raw cod BassOysterCaskets drops is no catch; without Junk every raw fish is one.
func TestDroppedRawFishIsNotACatch(t *testing.T) {
	for name, keep := range map[string]bool{
		"Raw bass": true, "Raw mackerel": false, "Raw cod": false, "Casket": false, "Seaweed": false,
	} {
		if got := isKeptCatch(name, fisherplan.IsBassOysterJunk); got != keep {
			t.Errorf("isKeptCatch(%q, BassOysterJunk) = %v; want %v", name, got, keep)
		}
	}
	for _, name := range []string{"Raw bass", "Raw mackerel", "Raw cod"} {
		if !isKeptCatch(name, nil) {
			t.Errorf("without Junk %q must count as a catch", name)
		}
	}
}

// TestKeptRawFishCountsOnlyTheKeptCatchRealConfig: against the real content, keptRawFish counts
// the bass and not the mackerel and cod, CurrentRawFish (the session target) still counts them
// all, and notACatch refuses the real junk ids while keeping the casket and the bass.
func TestKeptRawFishCountsOnlyTheKeptCatchRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	s := newFakeSession()
	setPackForEatTest(t, s, [2]int32{id("Big fishing net"), 1}, [2]int32{id("Raw bass"), 1}, [2]int32{id("Raw mackerel"), 1},
		[2]int32{id("Raw cod"), 1}, [2]int32{id("Raw bass"), 1}, [2]int32{id("Casket"), 1}, [2]int32{id("Seaweed"), 1})
	d := &Deps{Session: s, Config: cfg, Junk: fisherplan.IsBassOysterJunk}
	if got := keptRawFish(d); got != 2 {
		t.Errorf("keptRawFish = %d; want the 2 bass", got)
	}
	if got := CurrentRawFish(d); got != 4 {
		t.Errorf("CurrentRawFish = %d; want all 4 raw fish", got)
	}
	skip := notACatch(d)
	for _, name := range fisherplan.BassOysterJunk {
		if !skip(int(id(name))) {
			t.Errorf("%s is dropped but notACatch keeps it", name)
		}
	}
	for _, name := range []string{"Raw bass", "Casket", "Oyster"} {
		if skip(int(id(name))) {
			t.Errorf("%s is kept but notACatch refuses it", name)
		}
	}
	if keep := notACatch(&Deps{Config: cfg}); keep(int(id("Raw mackerel"))) {
		t.Error("ProgressiveFisher banks Raw mackerel; notACatch without Junk must keep it")
	}
}
