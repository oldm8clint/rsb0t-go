package combatplan

import (
	"testing"
	"time"
)

func TestDoorRouteOK(t *testing.T) {
	for _, c := range []struct {
		name string
		r    DoorRoute
		want bool
	}{
		{"one door, a short way", DoorRoute{OK: true, Cost: 10, Doors: 1}, true},
		{"two doors across a hall", DoorRoute{OK: true, Cost: 15, Doors: 2}, true},
		{"the dearest the three rungs ask (White Knights, 26)", DoorRoute{OK: true, Cost: 26, Doors: 1}, true},
		{"exactly the cap", DoorRoute{OK: true, Cost: DoorRouteMaxCost, Doors: 1}, true},
		{"one past the cap", DoorRoute{OK: true, Cost: DoorRouteMaxCost + 1, Doors: 1}, false},
		{"no route at all", DoorRoute{}, false},
		{"a stair on the way", DoorRoute{OK: true, Cost: 12, Doors: 1, Other: 1}, false},
		{"no door on it: the plain walk the box search refused", DoorRoute{OK: true, Cost: 12}, false},
	} {
		if got, why := DoorRouteOK(c.r); got != c.want || (!got && why == "") {
			t.Errorf("%s: DoorRouteOK(%+v) = %v, %q; want %v with a reason when refused", c.name, c.r, got, why, c.want)
		}
	}
}

func TestPickDoorTargetTakesTheCheapestRouteThenTheBetterRank(t *testing.T) {
	if _, ok := PickDoorTarget(nil); ok {
		t.Error("picked from nothing")
	}
	cands := []DoorCand{{Rank: 0, Cost: 20}, {Rank: 1, Cost: 9}, {Rank: 2, Cost: 9}, {Rank: 3, Cost: 30}}
	if i, ok := PickDoorTarget(cands); !ok || i != 1 {
		t.Errorf("picked %d, %v; want index 1 (cost 9, the better rank of two)", i, ok)
	}
	// The nearer by straight line (rank 0) loses to a cheaper way: a monster a wall away is not close.
	if i, _ := PickDoorTarget([]DoorCand{{Rank: 0, Cost: 22}, {Rank: 5, Cost: 6}}); i != 1 {
		t.Errorf("picked %d, want the cheap route over the better rank", i)
	}
}

// The budget is what makes a door that leads nowhere cost a few tries and not a shuttle.
func TestDoorBudgetHoldsTheDoorsShutAfterTheLimitWithoutAKill(t *testing.T) {
	var b DoorBudget
	now := time.Unix(1_000_000, 0)
	if !b.Allowed(now) {
		t.Fatal("a fresh budget refused a door")
	}
	for i := 1; i < DoorStrikeLimit; i++ {
		if b.Strike(now) {
			t.Fatalf("strike %d held the doors, the limit is %d", i, DoorStrikeLimit)
		}
	}
	if b.Strikes() != DoorStrikeLimit-1 {
		t.Errorf("Strikes = %d, want %d", b.Strikes(), DoorStrikeLimit-1)
	}
	if !b.Strike(now) {
		t.Fatalf("strike %d did not hold the doors", DoorStrikeLimit)
	}
	if b.Allowed(now) || b.Allowed(now.Add(DoorHold-time.Second)) {
		t.Error("the doors were open inside the hold")
	}
	if !b.Allowed(now.Add(DoorHold)) {
		t.Error("the doors stayed shut past the hold")
	}
	if b.Strikes() != 0 {
		t.Errorf("Strikes = %d after a hold, want a fresh count", b.Strikes())
	}
}

func TestADoorThatLeadsToAKillClearsTheCount(t *testing.T) {
	var b DoorBudget
	now := time.Unix(1_000_000, 0)
	// Three door targets, each ending in a kill, are never near the limit however many there are.
	for i := 0; i < 50; i++ {
		b.Strike(now)
		b.Strike(now)
		b.Strike(now)
		b.Kill()
		if !b.Allowed(now) {
			t.Fatalf("round %d: the doors were held although every crossing ended in a kill", i)
		}
	}
}
