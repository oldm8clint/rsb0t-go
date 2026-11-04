package peerbank

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestPublishAndRead(t *testing.T) {
	d := New()
	bank := []Holding{{Item: "Bronze axe", Count: 1}}
	d.Publish("Acct218", tile.Tile{X: 2800, Z: 3430}, true, bank, []Holding{{Item: "Rune axe", Count: 1}}, true)

	// The caller may reuse its slice; the directory must have copied it.
	bank[0].Item = "MUTATED"

	got := d.Entries(time.Now())
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Bank[0].Item != "Bronze axe" {
		t.Fatalf("directory kept the caller's slice: %q", got[0].Bank[0].Item)
	}
	if !got[0].Located || got[0].At.X != 2800 || !got[0].Working {
		t.Fatalf("row did not round-trip: %+v", got[0])
	}
}

func TestPublishIsKeyedCaseInsensitivelyAndReplaces(t *testing.T) {
	d := New()
	d.Publish("Bob", tile.Tile{}, true, []Holding{{Item: "Bronze axe", Count: 1}}, nil, false)
	d.Publish("BOB", tile.Tile{}, true, []Holding{{Item: "Iron axe", Count: 1}}, nil, false)
	got := d.Entries(time.Now())
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 — an account has one row whatever the case of its name", len(got))
	}
	if got[0].Bank[0].Item != "Iron axe" {
		t.Fatalf("republish should replace, got %q", got[0].Bank[0].Item)
	}
}

func TestEntriesAreSortedForDeterministicTieBreaking(t *testing.T) {
	d := New()
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		d.Publish(n, tile.Tile{}, true, nil, nil, false)
	}
	// Run it a few times: Go's map order is randomised per iteration, so a single pass
	// could agree by luck.
	for i := 0; i < 20; i++ {
		got := d.Entries(time.Now())
		if got[0].Account != "alpha" || got[1].Account != "bravo" || got[2].Account != "charlie" {
			t.Fatalf("unsorted: %q %q %q", got[0].Account, got[1].Account, got[2].Account)
		}
	}
}

func TestForgetDropsTheRowAndItsTurn(t *testing.T) {
	d := New()
	d.Publish("holder", tile.Tile{}, true, nil, nil, false)
	if !d.Reserve("holder", "asker", time.Now(), time.Minute) {
		t.Fatal("first reserve should succeed")
	}
	d.Forget("holder")
	if d.Count() != 0 {
		t.Fatalf("count = %d, want 0", d.Count())
	}
	if _, busy := d.Busy("holder", time.Now()); busy {
		t.Fatal("a forgotten account must not keep a reserved turn")
	}
}

// TestOneTurnAtATime is the contention rule: an account serves one trade at a time.
func TestOneTurnAtATime(t *testing.T) {
	d := New()
	now := time.Now()
	if !d.Reserve("holder", "first", now, time.Minute) {
		t.Fatal("first asker should get the turn")
	}
	if d.Reserve("holder", "second", now, time.Minute) {
		t.Fatal("second asker must NOT get the turn — that is the bottleneck rebuilt")
	}
	who, busy := d.Busy("holder", now)
	if !busy || who != "first" {
		t.Fatalf("Busy = %q/%v, want first/true", who, busy)
	}
}

func TestReReservingExtendsTheSameAskersTurn(t *testing.T) {
	d := New()
	now := time.Now()
	d.Reserve("holder", "walker", now, 10*time.Second)
	// Still walking, refreshes its own turn.
	if !d.Reserve("holder", "walker", now.Add(5*time.Second), 10*time.Second) {
		t.Fatal("an asker must be able to refresh its own turn while it walks")
	}
	// Past the ORIGINAL expiry, the refreshed turn is still held.
	if _, busy := d.Busy("holder", now.Add(12*time.Second)); !busy {
		t.Fatal("the refresh should have extended the turn")
	}
}

func TestATurnLapses(t *testing.T) {
	d := New()
	now := time.Now()
	d.Reserve("holder", "vanished", now, time.Second)
	if !d.Reserve("holder", "next", now.Add(2*time.Second), time.Minute) {
		t.Fatal("a lapsed turn must be takeable — an asker that logged out cannot block a holder forever")
	}
}

func TestOnlyTheHolderOfATurnCanReleaseIt(t *testing.T) {
	d := New()
	now := time.Now()
	d.Reserve("holder", "mine", now, time.Minute)
	d.Release("holder", "someone-else")
	if _, busy := d.Busy("holder", now); !busy {
		t.Fatal("a late arrival must not be able to free somebody else's turn")
	}
	d.Release("holder", "MINE")
	if _, busy := d.Busy("holder", now); busy {
		t.Fatal("the real holder's release should free the turn")
	}
}

func TestAnAccountCannotReserveItself(t *testing.T) {
	d := New()
	if d.Reserve("bob", "BOB", time.Now(), time.Minute) {
		t.Fatal("an account must not reserve its own turn")
	}
}

// TestSixtyAskersAtOneHolderYieldExactlyOneWinner is the reason Reserve is here rather than in
// the pure layer: "is it free?" and "then it is mine" have to be ONE atomic step, or every
// asker reads free in the same instant and they all walk at the same peer.
func TestSixtyAskersAtOneHolderYieldExactlyOneWinner(t *testing.T) {
	d := New()
	d.Publish("holder", tile.Tile{}, true, []Holding{{Item: "Bronze axe", Count: 2}}, nil, false)

	const askers = 60
	now := time.Now()
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	var mu sync.Mutex
	winners := 0
	for i := 0; i < askers; i++ {
		done.Add(1)
		go func(n int) {
			defer done.Done()
			start.Wait()
			if d.Reserve("holder", "asker"+string(rune('a'+n%26))+string(rune('a'+n/26)), now, time.Minute) {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	start.Done()
	done.Wait()

	if winners != 1 {
		t.Fatalf("%d of %d askers took the turn, want exactly 1", winners, askers)
	}
}

// TestEntriesReportAReservedHolderAsWorking so a chooser sees contention without a second call.
func TestEntriesReportAReservedHolderAsWorking(t *testing.T) {
	d := New()
	now := time.Now()
	d.Publish("holder", tile.Tile{}, true, nil, nil, false)
	if d.Entries(now)[0].Working {
		t.Fatal("an unreserved, idle holder should not read as working")
	}
	d.Reserve("holder", "asker", now, time.Minute)
	if !d.Entries(now)[0].Working {
		t.Fatal("a reserved holder should read as working")
	}
	if d.Entries(now.Add(2 * time.Minute))[0].Working {
		t.Fatal("once the turn lapses the holder is free again")
	}
}

func TestPublishIgnoresAnEmptyName(t *testing.T) {
	d := New()
	d.Publish("   ", tile.Tile{}, true, nil, nil, false)
	if d.Count() != 0 {
		t.Fatalf("count = %d, want 0 — an unaddressable row helps nobody", d.Count())
	}
}
