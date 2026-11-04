package fleetplan

import (
	"testing"
)

func penNames(pens []TrainPen) []string {
	out := make([]string, 0, len(pens))
	for _, p := range pens {
		out = append(out, p.Name)
	}
	return out
}

func holds(pens []TrainPen, name string) bool {
	for _, p := range pens {
		if p.Name == name {
			return true
		}
	}
	return false
}

func holdsLow(pens []TrainPen) bool {
	for _, p := range pens {
		if !p.Gated() {
			return true
		}
	}
	return false
}

// The seats for the live population (4 Oct 12:40: 265 accounts at Magic 40+): how many accounts
// the giant tier holds on one world before its share falls to what an empty trooper herd's share
// is at the same crowd, logged for the report; and it must be more than the 3 + 1 spawns before.
func TestGiantTierHoldsMoreThanItsOldSeats(t *testing.T) {
	var giant float64
	spawns := 0
	for _, p := range TrainPens {
		if p.Tier >= PenHillGiant {
			giant += p.SpawnMagicXPh() * float64(p.Spawns)
			spawns += p.Spawns
		}
	}
	troop := KhazardTroopers.SpawnMagicXPh() * float64(KhazardTroopers.Spawns)
	t.Logf("giant tier: %d spawns, %.0f xp/h a world; Khazard troopers: %d spawns, %.0f xp/h a world",
		spawns, giant, KhazardTroopers.Spawns, troop)
	for _, per := range []int{2, 3} {
		seats := 0
		for _, p := range TrainPens {
			seats += p.GiantSeats(per)
		}
		t.Logf("giant seats a world at %d a spawn: %d (%.0f xp/h each when full)", per, seats, giant/float64(seats))
	}
	if KhazardTroopers.GiantSeats(3) != 0 || YanilleWestOgres.GiantSeats(0) != YanilleWestOgres.Spawns*DefaultGiantSeatsPerSpawn {
		t.Error("GiantSeats")
	}
	if spawns <= 2*(3+1) {
		t.Fatalf("the giant tier seats %d spawns a world, no more than before", spawns)
	}
}
