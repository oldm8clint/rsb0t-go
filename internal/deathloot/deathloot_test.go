package deathloot

import (
	"strconv"
	"testing"
)

const t0 = int64(1_800_000_000_000)

func death(account string, at int64, x, z int, items ...Stack) Death {
	return Death{ID: account + "@" + strconv.FormatInt(at, 10), Account: account, At: at, HaveTile: true, X: x, Z: z, Level: 0, Place: "Yanille", Cause: "Ogre", Items: items}
}

func gain(by string, at int64, x, z, id int, n int64) Gain {
	return Gain{Account: by, At: at, X: x, Z: z, ID: id, Count: n}
}

func stackOf(t *testing.T, l *Ledger, i, j int) Stack {
	t.Helper()
	ds := l.Deaths()
	if i >= len(ds) || j >= len(ds[i].Items) {
		t.Fatalf("no stack %d,%d in %+v", i, j, ds)
	}
	return ds[i].Items[j]
}

func TestTheDeadAccountRecoversItsOwnPile(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 1, Name: "Yew longbow", Count: 4}))
	// 30 s later, standing beside the tile: the owner sees the pile at once.
	if got := l.NoteGain(gain("alice", t0+30_000, 2501, 3101, 1, 4)); got != 4 {
		t.Fatalf("attributed %d, want 4", got)
	}
	s := stackOf(t, l, 0, 0)
	if s.Remaining() != 0 || len(s.Recovered) != 1 || s.Recovered[0].By != "alice" {
		t.Fatalf("stack = %+v", s)
	}
	if l.Deaths()[0].Open(t0 + 31_000) {
		t.Fatal("a pile with nothing left is not open")
	}
}

func TestAnotherAccountRecoversAfterTheReveal(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 1, Name: "Yew longbow", Count: 4}))
	// 20 s: still the owner's alone; a gain by bob is not the pile.
	if got := l.NoteGain(gain("bob", t0+20_000, 2500, 3100, 1, 4)); got != 0 {
		t.Fatalf("bob took %d before the reveal", got)
	}
	if got := l.NoteGain(gain("bob", t0+RevealMs+1000, 2500, 3100, 1, 4)); got != 4 {
		t.Fatalf("bob took %d after the reveal, want 4", got)
	}
	if by := stackOf(t, l, 0, 0).Recovered[0].By; by != "bob" {
		t.Fatalf("recovered by %q", by)
	}
}

func TestAPartialPickupLeavesTheRestOpenThenLost(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 561, Name: "Nature rune", Count: 100}))
	l.NoteGain(gain("alice", t0+40_000, 2500, 3100, 561, 30))
	l.NoteGain(gain("carol", t0+RevealMs+5000, 2499, 3099, 561, 25))
	s := stackOf(t, l, 0, 0)
	if s.Taken() != 55 || s.Remaining() != 45 || len(s.Recovered) != 2 {
		t.Fatalf("stack = %+v", s)
	}
	d := l.Deaths()[0]
	if !d.Open(t0 + DespawnMs) {
		t.Fatal("45 left inside the window must still read open")
	}
	if d.Open(d.WindowEnd() + 1) {
		t.Fatal("after the window the remainder is lost for good, not open")
	}
	// Too late to count: the pile is gone.
	if got := l.NoteGain(gain("alice", d.WindowEnd()+1000, 2500, 3100, 561, 45)); got != 0 {
		t.Fatalf("a gain after the despawn was attributed (%d)", got)
	}
	if stackOf(t, l, 0, 0).Remaining() != 45 {
		t.Fatal("the remainder moved after the window")
	}
}

func TestGainsThatCannotBeThePileAreIgnored(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 1, Name: "Yew longbow", Count: 2}))
	cases := map[string]Gain{
		"too far":              gain("alice", t0+30_000, 2500+Radius+1, 3100, 1, 1),
		"other item":           gain("alice", t0+30_000, 2500, 3100, 2, 1),
		"owner at the respawn": gain("alice", t0+2000, 2500, 3100, 1, 1),
		"other level":          {Account: "alice", At: t0 + 30_000, X: 2500, Z: 3100, Level: 1, ID: 1, Count: 1},
	}
	for name, g := range cases {
		if got := l.NoteGain(g); got != 0 {
			t.Errorf("%s: attributed %d", name, got)
		}
	}
	if stackOf(t, l, 0, 0).Taken() != 0 {
		t.Fatal("something was recovered")
	}
}

func TestAGainBiggerThanThePileOnlyCreditsThePile(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 1, Name: "Coins", Count: 10}))
	if got := l.NoteGain(gain("alice", t0+30_000, 2500, 3100, 1, 25)); got != 10 {
		t.Fatalf("attributed %d, want 10", got)
	}
}

func TestTwoPilesOnOneTileAreServedOldestFirst(t *testing.T) {
	l := NewLedger(0)
	l.NoteDeath(death("alice", t0, 2500, 3100, Stack{ID: 1, Name: "Rune axe", Count: 1}))
	l.NoteDeath(death("bob", t0+60_000, 2500, 3100, Stack{ID: 1, Name: "Rune axe", Count: 1}))
	l.NoteGain(gain("carol", t0+RevealMs+10_000, 2500, 3100, 1, 1))
	ds := l.Deaths()
	if ds[0].Items[0].Remaining() != 0 || ds[1].Items[0].Remaining() != 1 {
		t.Fatalf("the oldest pile should be served first: %+v", ds)
	}
}

func TestActiveIsLockFreeAndExpires(t *testing.T) {
	l := NewLedger(0)
	if l.Active(t0) {
		t.Fatal("empty ledger is active")
	}
	l.NoteDeath(death("alice", t0, 1, 1, Stack{ID: 1, Count: 1}))
	if !l.Active(t0 + 1000) {
		t.Fatal("a fresh death must be active")
	}
	if l.Active(t0 + DespawnMs + SlackMs + 1) {
		t.Fatal("a closed window must not be active")
	}
}

func TestLedgerIsBoundedAndIgnoresADuplicate(t *testing.T) {
	l := NewLedger(3)
	for i := int64(0); i < 5; i++ {
		l.NoteDeath(death("a", t0+i, 1, 1, Stack{ID: 1, Count: 1}))
	}
	l.NoteDeath(death("a", t0+4, 1, 1, Stack{ID: 1, Count: 1}))
	ds := l.Deaths()
	if len(ds) != 3 || ds[0].At != t0+2 {
		t.Fatalf("deaths = %d first at %d", len(ds), ds[0].At)
	}
}

func TestGainOf(t *testing.T) {
	cases := []struct {
		pid, pc, id, c int32
		want           int64
	}{
		{0, 0, 5, 3, 3}, {5, 2, 5, 7, 5}, {5, 7, 5, 2, 0}, {5, 2, 0, 0, 0}, {4, 9, 5, 1, 1},
	}
	for _, c := range cases {
		if got := GainOf(c.pid, c.pc, c.id, c.c); got != c.want {
			t.Errorf("GainOf(%+v) = %d, want %d", c, got, c.want)
		}
	}
}

func TestLostKeepsTheThreePriciestUnits(t *testing.T) {
	held := []Held{
		{ID: 1, Name: "Rune scimitar", Count: 1, Cost: 15000, Worn: true},
		{ID: 2, Name: "Yew longbow", Count: 2, Cost: 800},
		{ID: 3, Name: "Coins", Count: 5000, Cost: 1},
		{ID: 4, Name: "Rune full helm", Count: 1, Cost: 21000, Worn: true},
		{ID: 5, Name: "Lobster", Count: 20, Cost: 120},
	}
	lost, kept := Lost(held, false, false)
	keptNames := map[string]int64{}
	for _, k := range kept {
		keptNames[k.Name] += k.Count
	}
	if keptNames["Rune full helm"] != 1 || keptNames["Rune scimitar"] != 1 || keptNames["Yew longbow"] != 1 || len(keptNames) != 3 {
		t.Fatalf("kept = %+v", kept)
	}
	got := map[string]int64{}
	for _, s := range lost {
		got[s.Name] = s.Count
	}
	if got["Yew longbow"] != 1 || got["Coins"] != 5000 || got["Lobster"] != 20 || got["Rune scimitar"] != 0 {
		t.Fatalf("lost = %+v", lost)
	}
}

func TestLostKeepsNothingWhenSkulledAndOneMoreUnderProtectItem(t *testing.T) {
	held := []Held{{ID: 1, Name: "Dragon dagger", Count: 1, Cost: 20000}, {ID: 2, Name: "Coins", Count: 10, Cost: 1}}
	lost, kept := Lost(held, true, false)
	if len(kept) != 0 || len(lost) != 2 {
		t.Fatalf("skulled: lost %+v kept %+v", lost, kept)
	}
	lost, kept = Lost(held, true, true)
	if len(kept) != 1 || kept[0].Name != "Dragon dagger" || len(lost) != 1 {
		t.Fatalf("skulled + protect: lost %+v kept %+v", lost, kept)
	}
}

func TestMergePrefersTheLiveCopyUnlessTheStoredOneKnowsMore(t *testing.T) {
	stored := death("a", t0, 1, 1, Stack{ID: 1, Count: 4, Recovered: []Recovery{{By: "a", Count: 3}}})
	rebuilt := death("a", t0, 1, 1, Stack{ID: 1, Count: 4}) // a fleet that restarted knows no recoveries
	gone := death("b", t0+5, 1, 1, Stack{ID: 1, Count: 1})
	out := Merge([]Death{stored, gone}, []Death{rebuilt}, 0)
	if len(out) != 2 || out[0].Items[0].Taken() != 3 {
		t.Fatalf("merge = %+v", out)
	}
	grown := death("a", t0, 1, 1, Stack{ID: 1, Count: 4, Recovered: []Recovery{{By: "a", Count: 4}}})
	out = Merge(out, []Death{grown}, 0)
	if out[0].Items[0].Taken() != 4 {
		t.Fatalf("live copy with more pickups must win: %+v", out[0])
	}
}
