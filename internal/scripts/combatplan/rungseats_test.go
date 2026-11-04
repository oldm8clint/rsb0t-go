package combatplan

import (
	"fmt"
	"testing"
	"time"
)

// The 7 Oct 2026 03:42 finding: the spread filled the rungs against each other but not against their spawns, so
// accounts sat on "giants 14/12 spawns". These tests pin the cap, the seat order and the shed that the Magic pens
// already have (scarabplan PenBoard / PenCap).

func TestRungCapIsSeatsPerSpawn(t *testing.T) {
	giants := mustRung(t, SpreadLadder(), "giants")
	if giants.SpawnCount() != 12 {
		t.Fatalf("the Giants list %d spawns, the finding was 12", giants.SpawnCount())
	}
	for seats, want := range map[int]int{0: 12, 1: 12, 2: 24, 6: 72} {
		if got := giants.Cap(seats); got != want {
			t.Fatalf("Cap(%d) = %d, want %d", seats, got, want)
		}
	}
	if (Rung{}).Cap(1) != 1 {
		t.Fatal("a rung with no spawns listed still seats one account")
	}
}

func TestPickRungSpreadNeverFillsARungPastItsCapWhileAnotherHasASeat(t *testing.T) {
	ladder := SpreadLadder()
	seat := map[string]int{}
	var fullAt int
	for i := 0; i < 120; i++ {
		c := PickRungSpread(ladder, 45, 60, true, Crowd{Account: fmt.Sprintf("a%d", i), Hitpoints: 60, Loads: copyLoads(seat), SeatsPerSpawn: 1})
		if !c.OK {
			t.Fatalf("pick %d: %+v", i, c)
		}
		if c.Full && fullAt == 0 {
			fullAt = i
		}
		if !c.Full && seat[c.Rung.Key] >= c.Rung.Cap(1) {
			t.Fatalf("pick %d sent an account to %q already at its cap %d (%v) with Full unset", i, c.Rung.Key, c.Rung.Cap(1), seat)
		}
		seat[c.Rung.Key]++
	}
	if fullAt == 0 {
		t.Fatalf("120 accounts never filled the band's rungs: %v", seat)
	}
	// Until the band was full, no rung went over its cap: the accounts seated before fullAt are exactly the seats.
	total := 0
	for _, r := range ladder {
		total += min(seat[r.Key], r.Cap(1))
	}
	if total < fullAt {
		t.Fatalf("only %d seats were filled when the pick said full at %d: %v", total, fullAt, seat)
	}
	giants := mustRung(t, ladder, "giants")
	if seat["giants"] < giants.Cap(1) {
		t.Fatalf("the Giants hold %d of their %d seats with every rung full: %v", seat["giants"], giants.Cap(1), seat)
	}
}

func TestPickRungSpreadFullTakesTheLeastFullRung(t *testing.T) {
	ladder := SpreadLadder()
	giants, wk := mustRung(t, ladder, "giants"), mustRung(t, ladder, "whiteknights")
	// Every rung of the band 40-44 at melee 42 is at its cap or over it; the White Knights are the least over.
	full := map[string]int{"giants": giants.Cap(1) + 6, "whiteknights": wk.Cap(1) + 1, "icewarriors": 40, "barbarians": 40, "alkharid": 40,
		"mossgiants": 40, "icegiants": 40, "ogres": 40, "firegiants": 40}
	c := PickRungSpread(ladder, 42, 60, true, Crowd{Account: "x", Hitpoints: 60, Loads: full, SeatsPerSpawn: 1})
	if !c.OK || !c.Full {
		t.Fatalf("every rung full answered %+v, want the least full with Full set", c)
	}
	if c.Rung.Key == "giants" {
		t.Fatalf("picked the Giants at %d over their %d, the most over-full rung", full["giants"], giants.Cap(1))
	}
}

func TestPickRungSpreadWithoutACapIsTheOldPick(t *testing.T) {
	ladder := SpreadLadder()
	seat := map[string]int{"giants": 40, "whiteknights": 30}
	old := PickRungSpread(ladder, 45, 60, true, Crowd{Account: "x", Hitpoints: 60, Loads: seat})
	zero := PickRungSpread(ladder, 45, 60, true, Crowd{Account: "x", Hitpoints: 60, Loads: seat, SeatsPerSpawn: 0})
	if old.Rung.Key != zero.Rung.Key || old.Full || zero.Full {
		t.Fatalf("a zero cap changed the pick: %+v vs %+v", old, zero)
	}
}

func TestRungBoardSeatsHoldAndHaveAnOrder(t *testing.T) {
	b := NewRungBoard()
	now := time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)
	// A seat taken for a walk holds past the TTL; a plain seat does not.
	b.SitFor("walker", 1, "giants", now, RungWalkHold)
	b.Sit("plain", 1, "giants", now)
	later := now.Add(RungSeatTTL + time.Minute)
	if got := b.Loads("", 1, later)["giants"]; got != 1 {
		t.Fatalf("after the TTL the loads are %d, want the held walker only", got)
	}
	if got := b.Loads("", 1, now.Add(RungWalkHold+time.Minute))["giants"]; got != 0 {
		t.Fatalf("after the hold the loads are %d, want none", got)
	}

	// Senior: the seats ahead of an account's own, by when each took the rung; a re-seat on the same rung keeps its place.
	b = NewRungBoard()
	for i, name := range []string{"first", "second", "third"} {
		b.Sit(name, 1, "giants", now.Add(time.Duration(i)*time.Second))
	}
	b.Sit("other-world", 2, "giants", now)
	b.Sit("elsewhere", 1, "whiteknights", now)
	for name, want := range map[string]int{"first": 0, "second": 1, "third": 2} {
		rung, world, senior, ok := b.Senior(name, now.Add(time.Minute))
		if !ok || rung != "giants" || world != 1 || senior != want {
			t.Fatalf("%s: Senior = %q %d %d %v, want giants 1 %d", name, rung, world, senior, ok, want)
		}
	}
	b.Sit("first", 1, "giants", now.Add(5*time.Minute)) // a refresh on the same rung keeps its Since
	if _, _, senior, _ := b.Senior("first", now.Add(5*time.Minute)); senior != 0 {
		t.Fatalf("a refreshed seat lost its place: %d seats ahead", senior)
	}
	b.Sit("first", 1, "whiteknights", now.Add(6*time.Minute)) // a move starts a new place
	if _, _, senior, _ := b.Senior("first", now.Add(6*time.Minute)); senior != 1 {
		t.Fatalf("a moved seat is the newest at the White Knights after %q: %d ahead, want 1", "elsewhere", senior)
	}
	if _, _, _, ok := b.Senior("nobody", now); ok {
		t.Fatal("an account with no seat has a Senior")
	}
	var nilBoard *RungBoard
	if _, _, _, ok := nilBoard.Senior("x", now); ok || !nilBoard.TrySit("x", 1, "giants", 1, now, 0) {
		t.Fatal("a nil board has no seats and takes every TrySit")
	}
}

func TestRungBoardTrySitIsAtomicUnderTheCap(t *testing.T) {
	b := NewRungBoard()
	now := time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)
	if !b.TrySit("a", 1, "giants", 2, now, RungWalkHold) || !b.TrySit("b", 1, "giants", 2, now, RungWalkHold) {
		t.Fatal("the first two seats of a cap of two were refused")
	}
	if b.TrySit("c", 1, "giants", 2, now, RungWalkHold) {
		t.Fatal("a third account got a seat on a rung capped at two")
	}
	if !b.TrySit("a", 1, "giants", 2, now.Add(time.Minute), 0) {
		t.Fatal("an account already seated was turned away from its own seat")
	}
	if !b.TrySit("d", 2, "giants", 2, now, RungWalkHold) {
		t.Fatal("another world's rung shares the cap of this one")
	}
	if got := b.Loads("", 1, now)["giants"]; got != 2 {
		t.Fatalf("loads %d, want 2", got)
	}
	// Leaving frees the seat.
	b.Leave("b")
	if !b.TrySit("c", 1, "giants", 2, now, RungWalkHold) {
		t.Fatal("a seat freed by Leave was not taken")
	}
}

// Seventeen accounts at a sixteen-seat rung: only the newest one is over, not all of them (the Magic pens' shed).
func TestOnlyTheNewestAccountsOfAnOverFullRungAreShed(t *testing.T) {
	b := NewRungBoard()
	now := time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)
	const capSeats = 12
	for i := 0; i < capSeats+2; i++ {
		b.Sit(fmt.Sprintf("acct%02d", i), 1, "giants", now.Add(time.Duration(i)*time.Second))
	}
	over := 0
	for i := 0; i < capSeats+2; i++ {
		_, _, senior, ok := b.Senior(fmt.Sprintf("acct%02d", i), now.Add(time.Minute))
		if !ok {
			t.Fatalf("acct%02d lost its seat", i)
		}
		if senior >= capSeats {
			over++
			if i < capSeats {
				t.Fatalf("acct%02d, seat %d of %d, is shed ahead of newer accounts", i, i+1, capSeats)
			}
		}
	}
	if over != 2 {
		t.Fatalf("%d accounts are over a cap of %d with %d seated, want 2", over, capSeats, capSeats+2)
	}
}
