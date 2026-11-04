package questmods

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The Grail castle on the fleet's real collision pack (skipped without RSB0T_COLLISION_PACK).
//
// Live 12-16 Oct 2026, nine parks of "5 walks in this run to 2760,4681,0 ended unreachable": the account had won the Titan
// fight, taken the Fisherman's bell and stood at the ring stand 2763,4693, and the staircase it walked for lies in the castle
// two tiles away behind a wall. The walks were right that there is no path. The way in is the bell beside a maiden
// (questplan/grailcastle.go); these tests pin that, and that every leg after it is a walk.

func gp(a tile.Tile) webwalk.Point { return webwalk.Point{X: a.X, Z: a.Z, Level: a.Level} }

// No walk joins the ring stand and the castle, in either direction; the ring stand is a walk from the Titan's bridge and not
// from the arrival pocket (the Titan first).
func TestNoWalkJoinsTheRingStandAndTheCastle(t *testing.T) {
	_, pl := loadScorpionPack(t)
	opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
	for _, to := range []tile.Tile{questplan.GrailCastleLanding, questplan.GrailStairStand, {X: 2761, Z: 4682}, {X: 2760, Z: 4681}} {
		if r := pl.Plan(gp(questplan.GrailBellRing), gp(to), opt); r.OK {
			t.Errorf("a walk from the ring stand to %v exists (cost %d): the bell would not be the way in", to, r.Cost)
		}
		if r := pl.Plan(gp(to), gp(questplan.GrailBellRing), opt); r.OK {
			t.Errorf("a walk from %v to the ring stand exists (cost %d)", to, r.Cost)
		}
	}
	if r := pl.Plan(gp(questplan.TitanPassTile), gp(questplan.GrailBellRing), opt); !r.OK {
		t.Errorf("no walk from the Titan's bridge to the ring stand: %s", r.Reason)
	}
	if r := pl.Plan(gp(questplan.TitanArrival), gp(questplan.GrailBellRing), opt); r.OK {
		t.Error("a walk from the arrival pocket to the ring stand exists: the Titan would not be the first step")
	}
}

// The castle table is the real pack's: every interior tile is walkable ground no walk from the ring stand reaches, every
// tile a walk from the landing reaches is an interior tile, and no tile a walk from the ring stand reaches is.
func TestTheCastleTableMatchesTheRealPack(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
	inside := 0
	for z := 4668; z <= 4698; z++ {
		for x := 2750; x <= 2790; x++ {
			at := tile.Tile{X: x, Z: z}
			in := questplan.InGrailCastle(at)
			if !pk.Walkable(x, z, 0) {
				if in {
					t.Errorf("%v is in the table and not walkable", at)
				}
				continue
			}
			fromRing := pl.Plan(gp(questplan.GrailBellRing), gp(at), opt).OK
			fromLanding := pl.Plan(gp(questplan.GrailCastleLanding), gp(at), opt).OK
			switch {
			case in:
				inside++
				if fromRing {
					t.Errorf("%v is in the table and a walk from the ring stand reaches it", at)
				}
			case fromLanding:
				t.Errorf("%v is not in the table and a walk from the landing reaches it", at)
			}
			if fromRing && fromLanding {
				t.Errorf("%v is reached from both sides of the wall", at)
			}
		}
	}
	if inside < 200 {
		t.Errorf("only %d interior tiles", inside)
	}
	if !questplan.GrailBellLanded(questplan.GrailCastleLanding) || questplan.GrailBellLanded(questplan.GrailBellRing) {
		t.Error("the landing tile is not landed, or the ring stand is")
	}
}

// From the landing the staircase is a walk through the room door the baked graph holds, and the staircase's Climb-up edge
// starts from the stand the module walks to and arrives on the first floor; from there the Fisher King's door stand is one
// more door away. (The King's own hall is behind a diagonal door the pack bakes shut: it is opened, not planned.)
func TestTheCastleLegsAfterTheBellAreWalks(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
	for _, a := range []tile.Tile{questplan.GrailCastleLanding, questplan.GrailStairStand} {
		if !pk.Walkable(a.X, a.Z, a.Level) {
			t.Fatalf("%v is not walkable", a)
		}
	}
	r := pl.Plan(gp(questplan.GrailCastleLanding), gp(questplan.GrailStairStand), opt)
	if !r.OK {
		t.Fatalf("no walk from the landing to the stair stand: %s", r.Reason)
	}
	hops := r.Hops()
	if len(hops) != 1 || hops[0].LocName != "Door" || hops[0].From.X != 2761 || hops[0].From.Z != 4684 && hops[0].From.Z != 4683 {
		t.Errorf("the landing's walk to the stair is %v, want the one room door at 2761,4684", hops)
	}
	up := pl.Plan(gp(questplan.GrailCastleLanding), webwalk.Point{X: 2762, Z: 4682, Level: 1}, opt)
	if !up.OK {
		t.Fatalf("no route from the landing to the first floor: %s", up.Reason)
	}
	var stair *webwalk.Edge
	for _, h := range up.Hops() {
		if h.LocName == "Staircase" {
			stair = h
		}
	}
	if stair == nil || stair.Action != "Climb-up" || stair.To.Level != 1 {
		t.Errorf("the climb is %+v, want a Climb-up edge to the first floor", stair)
	}
	// The stand the module walks to is itself the start of a baked Climb-up edge: the stair is a single hop from it.
	from := pl.Plan(gp(questplan.GrailStairStand), webwalk.Point{X: 2762, Z: 4682, Level: 1}, opt)
	if h := from.Hops(); !from.OK || len(h) != 1 || h[0].LocName != "Staircase" || h[0].From.X != questplan.GrailStairStand.X || h[0].From.Z != questplan.GrailStairStand.Z {
		t.Errorf("from the stair stand the climb is %v, want one Climb-up hop starting on it", h)
	}
	top := tile.Tile{X: 2762, Z: 4682, Level: 1}
	d := pl.Plan(gp(top), gp(questplan.FisherKingDoorSt), opt)
	if !d.OK {
		t.Fatalf("no walk from the staircase's top to the Fisher King's door stand: %s", d.Reason)
	}
	if k := pl.Plan(gp(top), gp(questplan.FisherKingTile), opt); k.OK {
		t.Error("the planner reaches the Fisher King's hall: the diagonal door is meant to be opened, not planned")
	}
}

// The stage 9 legs have never run live (no account has reached the Fisher King): the renewed realm's table is a walk through
// its doors and two staircases from where the whistle lands, and Sir Percival's sacks are a walk to the Goblin Village.
func TestTheRenewedRealmAndThePercivalSacksAreWalks(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
	arrival := tile.Tile{X: 41<<6 + 54, Z: 73<<6 + 43} // p_teleport(0_41_73_54_43) in [opheld1,magic_whistle]
	stand := tile.Tile{X: 2649, Z: 4683, Level: 2}     // grailTake's stand, south of the table
	if !pk.Walkable(stand.X, stand.Z, stand.Level) || pk.Walkable(questplan.GrailTableTile.X, questplan.GrailTableTile.Z, questplan.GrailTableTile.Level) {
		t.Errorf("the table's stand %v is not walkable, or the table %v is", stand, questplan.GrailTableTile)
	}
	if r := pl.Plan(gp(arrival), gp(stand), opt); !r.OK {
		t.Errorf("no route from the renewed realm's arrival %v to the table's stand %v: %s", arrival, stand, r.Reason)
	}
	if r := pl.Plan(gp(questplan.SixHeads), gp(questplan.PercySacks), opt); !r.OK {
		t.Errorf("no route from the six heads to Percival's sacks %v: %s", questplan.PercySacks, r.Reason)
	}
}
