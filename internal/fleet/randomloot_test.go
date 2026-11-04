package fleet

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// fakePack is a pack a test changes between Guardian lines.
type fakePack struct {
	mu sync.Mutex
	c  loot.Counts
}

func (p *fakePack) set(c loot.Counts) { p.mu.Lock(); p.c = c; p.mu.Unlock() }
func (p *fakePack) read() loot.Counts {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := loot.Counts{}
	for id, n := range p.c {
		out[id] = n
	}
	return out
}

func newTestEventLoot(p *fakePack) (*RandomEventLoot, *loot.Tally, *[]string) {
	tally := loot.NewTally(nil)
	var mu sync.Mutex
	var lines []string
	r := NewRandomEventLoot(p.read, nil, tally.Add, func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() })
	r.Settle, r.Poll = 300*time.Millisecond, 5*time.Millisecond
	return r, tally, &lines
}

// waitTally waits for the tally to hold want sources (settle runs on its own goroutine).
func waitTally(t *testing.T, tally *loot.Tally, want int) []loot.Totals {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := tally.Snapshot()
		if len(snap) >= want || time.Now().After(deadline) {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func itemCount(tt loot.Totals, id int) int64 {
	for _, it := range tt.Items {
		if it.ID == id {
			return it.Count
		}
	}
	return 0
}

const (
	testBox     = 3062
	testCoins   = 995
	testShrimps = 317
	testKebab   = 1971
)

func TestRandomEventLootStrangeBoxAcrossRetries(t *testing.T) {
	p := &fakePack{c: loot.Counts{testBox: 2, testCoins: 10}}
	r, tally, _ := newTestEventLoot(p)
	// First box: solved, one left, a shrimp lands meanwhile.
	r.Note("random event: Strange box — handling")
	r.Note("random event: Strange box — solving: What colour is the Star?")
	p.set(loot.Counts{testBox: 1, testCoins: 10, testShrimps: 1})
	r.Note("random event: Strange box — cleared")
	// The second box's handling finishes the first window at once.
	r.Note("random event: Strange box — handling")
	p.set(loot.Counts{testCoins: 90, testShrimps: 1})
	r.Note("random event: Strange box — still present after handling")
	snap := waitTally(t, tally, 1)
	if len(snap) != 1 {
		t.Fatalf("tally = %+v", snap)
	}
	got := snap[0]
	if got.Source.Kind != loot.KindRandomEvent || got.Source.Name != "Strange box" || got.Source.Model != loot.ModelObj || got.Source.ID != testBox {
		t.Fatalf("source = %+v", got.Source)
	}
	if got.Events != 1 || itemCount(got, testCoins) != 80 || itemCount(got, testShrimps) != 0 || len(got.Items) != 1 {
		t.Fatalf("totals = %+v", got)
	}
}

func TestRandomEventLootRewardAfterTerminalLine(t *testing.T) {
	p := &fakePack{c: loot.Counts{testShrimps: 3}}
	r, tally, lines := newTestEventLoot(p)
	r.Note("random event: Mysterious Old Man — handling")
	r.Note("random event: Mysterious Old Man — cleared")
	time.Sleep(40 * time.Millisecond)
	p.set(loot.Counts{testShrimps: 3, testKebab: 1}) // the inventory update lands late
	snap := waitTally(t, tally, 1)
	if len(snap) != 1 || snap[0].Events != 1 || itemCount(snap[0], testKebab) != 1 || snap[0].Source.Model != loot.ModelNPC || snap[0].Source.ID != 410 {
		t.Fatalf("tally = %+v", snap)
	}
	time.Sleep(20 * time.Millisecond)
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "Mysterious Old Man: completed, reward 1 x obj 1971") {
		t.Fatalf("log = %q", *lines)
	}
}

func TestRandomEventLootRecordsNothingForOthers(t *testing.T) {
	p := &fakePack{c: loot.Counts{}}
	r, tally, _ := newTestEventLoot(p)
	// The standalone Lamp event (a lamp a Genie left unclaimed) is the Genie's follow-up, not an
	// event of its own: spending it records nothing.
	p.set(loot.Counts{2528: 1})
	r.Note("random event: Lamp — handling")
	p.set(loot.Counts{})
	r.Note("random event: Lamp — cleared")
	// Another account's Genie: rejected, even though a lamp happened to be in the pack.
	r.Note("random event: Genie — handling")
	p.set(loot.Counts{2528: 1})
	r.Note("random event: Genie — rejected for this account; checking other events next")
	p.set(loot.Counts{})
	// Another account's dwarf: rejected, even though a kebab happened to arrive.
	r.Note("random event: Drunken Dwarf — handling")
	p.set(loot.Counts{testKebab: 1})
	r.Note("random event: Drunken Dwarf — rejected for this account; checking other events next")
	// A progress line and an unrelated line change nothing.
	r.Note("random event: whirlpool — handling")
	p.set(loot.Counts{testKebab: 1, testShrimps: 5})
	r.Note("random event: whirlpool — cleared")
	r.Note("in combat with no tracked event — fleeing immediately (streak 1, dist 6)")
	time.Sleep(400 * time.Millisecond)
	if snap := tally.Snapshot(); len(snap) != 0 {
		t.Fatalf("recorded %+v", snap)
	}
}

func TestRandomEventLootContestedWindow(t *testing.T) {
	p := &fakePack{c: loot.Counts{testBox: 1, 405: 2}}
	r, tally, lines := newTestEventLoot(p)
	r.Note("random event: Strange box — handling")
	p.set(loot.Counts{405: 1, testCoins: 160})
	r.Note("random event: Strange box — cleared")
	time.Sleep(400 * time.Millisecond)
	if snap := tally.Snapshot(); len(snap) != 0 {
		t.Fatalf("recorded %+v", snap)
	}
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "nothing recorded") {
		t.Fatalf("log = %q", *lines)
	}
}
