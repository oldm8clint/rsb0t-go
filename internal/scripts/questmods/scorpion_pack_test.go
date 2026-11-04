package questmods

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/live"
)

// Scorpion Catcher's legs on the fleet's real collision pack (skipped without RSB0T_COLLISION_PACK).
//
// The plan of 15 Oct 2026 read 50 parks on "talk to Thormac" as "the Thormac walk (726-cost leg) times out". The
// walk is sound: the live log shows every account reaching the roof of the Sorcerer's Tower (the door, then three
// ladders) and standing within two tiles of Thormac, and the 6-minute walk timeouts it also holds (2 legs in 17,
// both from Al Kharid on a 726-cost route) are followed at once by a second walk that finishes the climb. What
// parked them was the cage in the bank (questplan's scorpioncatcher.go). These tests pin that the roof and every
// other leg of the quest are reachable, so the next failure on this quest is not looked for in the walker.
func loadScorpionPack(t *testing.T) (*pathfind.Pack, *webwalk.Planner) {
	t.Helper()
	pp := os.Getenv("RSB0T_COLLISION_PACK")
	if pp == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := live.PlannerFor(pk)
	if err != nil {
		t.Fatal(err)
	}
	return pk, pl
}

func scorpPoint(a tile.Tile) webwalk.Point { return webwalk.Point{X: a.X, Z: a.Z, Level: a.Level} }

func scorpPlan(pl *webwalk.Planner, from, to tile.Tile) webwalk.Route {
	return pl.Plan(scorpPoint(from), scorpPoint(to), webwalk.Options{AllowNPC: true, AllowSpecial: true})
}

// From the Seers' bank, where a cage in the bank is drawn, to Thormac on the tower's roof: out of the bank, through
// the tower door and up the ladders of the first three floors, a short walk.
func TestThormacsRoofIsReachableFromTheSeersBank(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	anchor := questplan.ThormacAnchor
	if anchor.Level != 3 || !pk.Walkable(anchor.X, anchor.Z, anchor.Level) {
		t.Fatalf("Thormac's anchor %v is not a walkable tile of level 3", anchor)
	}
	r := scorpPlan(pl, questplan.SeersBank, anchor)
	if !r.OK {
		t.Fatalf("no route from the Seers' bank to %v: %s", anchor, r.Reason)
	}
	if end := r.End(); end.X != anchor.X || end.Z != anchor.Z || end.Level != anchor.Level {
		t.Errorf("the route ends at %v, not %v", end, anchor)
	}
	// The live legs cost 163 (from the Camelot landing) and the plan here 165 with a door and three ladders.
	if r.Cost < 100 || r.Cost > 250 {
		t.Errorf("route cost %d, want about 165 (door and three ladders)", r.Cost)
	}
	levels := map[int]bool{}
	for _, w := range r.Waypoints {
		levels[w.Level] = true
	}
	for lv := 0; lv <= 3; lv++ {
		if !levels[lv] {
			t.Errorf("the route never stands on level %d: it does not climb the tower floor by floor", lv)
		}
	}
	if len(r.Hops()) < 4 {
		t.Errorf("%d transports on the route, want the door and three ladders", len(r.Hops()))
	}
}

// On the roof itself: from the top of the last ladder (where the climb lands, three tiles west of the anchor) to the
// anchor and to a tile Thormac can be talked to from, walking only. The live accounts that parked stood at
// 2703-2704,3403-3407 beside him: there is no wall between.
func TestThormacsRoofIsOneOpenFloor(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	landing := tile.Tile{X: 2699, Z: 3406, Level: 3} // "Climb-up Ladder at 2699,3405 ok", 2699,3406,2 -> 2699,3406,3
	anchor := questplan.ThormacAnchor
	if !pk.Walkable(landing.X, landing.Z, landing.Level) {
		t.Fatalf("the ladder's landing %v is not walkable", landing)
	}
	r := pl.Plan(scorpPoint(landing), scorpPoint(anchor), webwalk.Options{WalkOnly: true})
	if !r.OK || r.Cost > 6 {
		t.Errorf("landing -> anchor: ok %v cost %d (%s), want a walk of 3", r.OK, r.Cost, r.Reason)
	}
	stands := pk.InteractTiles(pathfind.NavPoint{X: anchor.X, Z: anchor.Z, Level: anchor.Level})
	if len(stands) < 4 {
		t.Fatalf("%d tiles to talk to Thormac from at his anchor, want its four neighbours: %v", len(stands), stands)
	}
	for _, s := range stands {
		rr := pl.Plan(scorpPoint(landing), webwalk.Point{X: s.X, Z: s.Z, Level: s.Level}, webwalk.Options{WalkOnly: true})
		if !rr.OK {
			t.Errorf("no walk from the landing to the stand %v: %s", s, rr.Reason)
		}
	}
}

// Every other leg a run makes after the cage is back in the pack: the Seer, the outpost hut, the monastery (and the
// Abbot below it), the Falador bank for the Taverley kit, Velrak's cell and the deep gate. The secret room behind the
// wall is entered by searching the wall, not on foot (questmods.crossWall), so it is not asked of the planner.
func TestScorpionCatcherLegsFromTheSeersBank(t *testing.T) {
	pk, pl := loadScorpionPack(t)
	legs := []struct {
		name string
		to   tile.Tile
	}{
		{"the Seer", questplan.SeerAnchor},
		{"Ivor's hut at the outpost", questplan.OutpostHut},
		{"the Abbot", questplan.AbbotAnchor},
		{"the monastery's upper room", questplan.MonasteryRoom},
		{"the Falador west bank", questplan.FaladorWestBank},
		{"the jail door", questplan.JailDoorStand},
		{"the deep gate", questplan.DeepGateEast},
	}
	for _, l := range legs {
		if !pk.Walkable(l.to.X, l.to.Z, l.to.Level) {
			t.Errorf("%s: %v is not a walkable tile", l.name, l.to)
			continue
		}
		if r := scorpPlan(pl, questplan.SeersBank, l.to); !r.OK {
			t.Errorf("%s: no route from the Seers' bank to %v: %s", l.name, l.to, r.Reason)
		}
	}
	for _, a := range []tile.Tile{questplan.SecretWallStand, questplan.SecretRoomTile, questplan.ScorpionATile} {
		if !pk.Walkable(a.X, a.Z, a.Level) {
			t.Errorf("%v (the secret room) is not walkable", a)
		}
	}
}
