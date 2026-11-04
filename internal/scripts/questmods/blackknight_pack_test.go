package questmods

// Black Knights' Fortress's executors against the real data (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG; each test skips
// without them), the way cooksheep_realdata_test.go does for the fillers: the lookups bkCross and the spy and the sabotage make find
// their locs where the table says, the tile each is sent from is one questrunner's own LocStands accepts, every walk between two
// crossings has a path on the pack, and bkLocate names the room of every tile the table uses.

import (
	"slices"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

func bkNav(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

func TestBKLocateNamesTheRoomOfEveryTileTheTableUses(t *testing.T) {
	d := csDeps(t)
	for region, ts := range questplan.BKRegionTiles() {
		for _, p := range ts {
			got, where := bkLocate(d, p)
			if where != bkAt || got != region {
				t.Errorf("%v: located in %q (%v), the table says %q", p, got, where, region)
			}
		}
	}
	// Open ground near the door, the sentries' ground, the west waypoint: outside. Far away: far. Floor the route does not name: lost.
	for _, p := range []tile.Tile{questplan.BKDoorApproach, questplan.BKWestWaypoint, {X: 3030, Z: 3502}, {X: 3024, Z: 3506}, {X: 3016, Z: 3512}} {
		if got, where := bkLocate(d, p); where != bkAt || got != questplan.BKOutside {
			t.Errorf("%v: %q %v, want the open ground", p, got, where)
		}
	}
	if _, where := bkLocate(d, tile.Tile{X: 2962, Z: 3338, Level: 2}); where != bkFar {
		t.Errorf("Sir Amik's floor is not far from the fortress")
	}
	for _, p := range []tile.Tile{{X: 3028, Z: 3508}, {X: 3010, Z: 3516, Level: 1}, {X: 3028, Z: 3507, Level: 3}, {X: 3012, Z: 3516, Level: 1}} {
		if got, where := bkLocate(d, p); where != bkLost {
			t.Errorf("%v: located in %q (%v), but no room of the route is there", p, got, where)
		}
	}
}

func TestBKCrossingsFindTheirLocsAndStandWhereTheEngineAccepts(t *testing.T) {
	d := csDeps(t)
	for _, l := range questplan.BKLegs {
		m, ok := questrunner.FindLoc(d, l.Loc.ID, "", l.Op, l.Loc.Tile(), 0)
		if !ok || m.X != l.Loc.X || m.Z != l.Loc.Z || m.Level != l.Loc.Level {
			t.Errorf("%s: FindLoc(%d at %v, radius 0) = %+v (%v)", l.Name, l.Loc.ID, l.Loc.Tile(), m, ok)
			continue
		}
		stands := questrunner.LocStands(d, m, l.Stand)
		if !slices.Contains(stands, l.Stand) {
			t.Errorf("%s: questrunner accepts %v for %s, not the table's %v", l.Name, stands, m.Name, l.Stand)
		}
		// The stand is the nearest accepted tile to itself, so ApproachLoc (which OperateLoc runs) sends from where the account is.
		if len(stands) == 0 || stands[0] != l.Stand {
			t.Errorf("%s: standing on %v, the first accepted tile is %v", l.Name, l.Stand, stands[0])
		}
	}
}

func TestBKPlansAreWalkableBetweenCrossings(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct{ from, to string }{
		{questplan.BKOutside, questplan.BKGrillShaft}, {questplan.BKGrillShaft, questplan.BKWarehouse}, {questplan.BKWarehouse, questplan.BKOutside},
		{questplan.BKRoomCSouth, questplan.BKOutside}, {questplan.BKRoomA, questplan.BKGrillShaft}, {questplan.BKBanquetHall, questplan.BKWarehouse},
		{questplan.BKEntryHall, questplan.BKOutside}, {questplan.BKAlcove0, questplan.BKOutside},
	} {
		plan := questplan.BKPlan(c.from, c.to)
		if len(plan) == 0 {
			t.Errorf("no plan from %q to %q", c.from, c.to)
			continue
		}
		total := 0
		for i := 1; i < len(plan); i++ {
			prev, next := plan[i-1], plan[i]
			if prev.Land == next.Stand {
				continue
			}
			r := d.Pack.FindPath(bkNav(prev.Land), bkNav(next.Stand))
			if !r.OK {
				t.Errorf("%s -> %s: no path from the landing %v to the next stand %v (%s)", prev.Name, next.Name, prev.Land, next.Stand, r.Reason)
				continue
			}
			if r.Steps() > 30 {
				t.Errorf("%s -> %s: %d steps between two crossings", prev.Name, next.Name, r.Steps())
			}
			total += r.Steps()
		}
		t.Logf("%q to %q: %d crossings, %d steps of walking between them", c.from, c.to, len(plan), total)
	}
	// The approach and the first crossing: from the door's approach to its stand; from the last landing to the grill and the hole.
	for _, c := range []struct {
		what     string
		from, to tile.Tile
	}{
		{"the approach to the front door's stand", questplan.BKDoorApproach, questplan.BKLegs[0].Stand},
		{"the grill ladder's landing to the grill", tile.Tile{X: 3022, Z: 3510}, questplan.BKGrillStand},
		{"the warehouse wall's landing to the hole's first stand", tile.Tile{X: 3030, Z: 3509, Level: 1}, questplan.BKHoleStands[0]},
		{"the west waypoint to the approach", questplan.BKWestWaypoint, questplan.BKDoorApproach},
	} {
		if r := d.Pack.FindPath(bkNav(c.from), bkNav(c.to)); !r.OK || r.Steps() > 30 {
			t.Errorf("%s: %+v", c.what, r)
		}
	}
}

func TestBKGrillAndHoleLookupsWork(t *testing.T) {
	d := csDeps(t)
	g, ok := questrunner.FindLoc(d, questplan.LocBKGrill, "", "Listen-at", questplan.BKGrill.Tile(), 0)
	if !ok {
		t.Fatal("the grill is not found")
	}
	// OperateLoc's ApproachLoc stands the account on the first tile LocStands lists: the grill's own.
	if stands := questrunner.LocStands(d, g, questplan.BKGrillStand); len(stands) != 1 || stands[0] != questplan.BKGrillStand {
		t.Errorf("grill stands %v", stands)
	}
	h, ok := questrunner.FindLoc(d, questplan.LocBKHole, "", "", questplan.BKHole.Tile(), 0)
	if !ok {
		t.Fatal("the hole is not found")
	}
	for _, s := range questplan.BKHoleStands {
		if stands := questrunner.LocStands(d, h, s); !slices.Contains(stands, s) {
			t.Errorf("the hole's accepted stands %v lack %v", stands, s)
		}
	}
	// The cabbage field: the pick finds a cabbage near the anchor the step walks to.
	if _, ok := questrunner.FindLoc(d, questplan.LocCabbage, "", "Pick", questplan.CabbageField, 12); !ok {
		t.Error("no cabbage within 12 of the field's middle")
	}
	// The walker's targets are floor tiles.
	for _, p := range []tile.Tile{questplan.BKDoorApproach, questplan.BKWestWaypoint, questplan.SirAmikAnchor, questplan.CabbageField,
		questplan.WayneShop.Anchor, questplan.HorvikShop.Anchor, questplan.PeksaShop.Anchor} {
		if !d.Pack.Walkable(p.X, p.Z, p.Level) {
			t.Errorf("%v is not a floor tile", p)
		}
	}
}

func TestBKSentryAvoidIsTheWalkersRectangle(t *testing.T) {
	r := bkSentryAvoid()
	g := questplan.BKSentryGround
	if len(r) != 1 || r[0].MinX != g.MinX || r[0].MaxX != g.MaxX || r[0].MinZ != g.MinZ || r[0].MaxZ != g.MaxZ || r[0].Level != g.Level {
		t.Errorf("%+v for %+v", r, g)
	}
	if g.Contains(questplan.BKDoorApproach) || g.Contains(questplan.BKLegs[0].Stand) || g.Contains(questplan.BKWestWaypoint) {
		t.Error("the sentries' rectangle holds the door's approach, its stand or the west waypoint: a walk to them would start inside what it must avoid")
	}
}
