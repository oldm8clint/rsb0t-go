package bot

import (
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/live"
)

var (
	mulePackOnce sync.Once
	mulePack     *pathfind.Pack
	mulePlanner  *webwalk.Planner
	mulePackErr  error
)

// muleRealPack loads RSB0T_COLLISION_PACK, as the webwalk and pathfind real-pack tests do;
// without it these tests skip.
func muleRealPack(t *testing.T) (*pathfind.Pack, *webwalk.Planner) {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	mulePackOnce.Do(func() {
		mulePack, mulePackErr = pathfind.Load(path)
		if mulePackErr == nil {
			mulePlanner, mulePackErr = live.PlannerFor(mulePack)
		}
	})
	if mulePackErr != nil {
		t.Fatal(mulePackErr)
	}
	return mulePack, mulePlanner
}

// mulePlan plans as returnHome's Walker.Plan does for a combat-3 mule holding coins.
func mulePlan(p *webwalk.Planner, from webwalk.Point, coins int) webwalk.Route {
	home := webwalk.Point{X: 2809, Z: 3441}
	zones := webwalk.ResolveDangerZones(nil, muleWolfRects(), webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &from, Dest: &home})
	state := &webwalk.WorldState{FreeSlots: 20, ItemCount: func(name string) int {
		if name == "Coins" {
			return coins
		}
		return 0
	}}
	return p.Plan(from, home, webwalk.Options{AvoidZones: zones, AllowNPC: true, AllowSpecial: true, State: state})
}

// The start points the live mule was displaced to or fled through on 2026-09-19.
var muleEastStarts = []webwalk.Point{
	{X: 2898, Z: 3423, Level: 1}, // the Mysterious Old Man's Taverley fail-teleport tile
	{X: 2934, Z: 3320},           // the Taverley gate from Falador
	{X: 2875, Z: 3410},           // the eastern foothills, below the sentries
	{X: 2868, Z: 3435},           // inside the 2870,3438 sentry's reach
	{X: 3222, Z: 3218},           // the Lumbridge respawn
}

// From the east, with no fare, there is no wolf-free way home: the only land route is the
// pass. This is what makes returnHome earn a fare instead of walking.
func TestMuleHomeNoWolfFreeLandRouteFromTheEast(t *testing.T) {
	_, p := muleRealPack(t)
	for _, s := range muleEastStarts {
		if r := mulePlan(p, s, 0); r.OK {
			t.Errorf("from %s with no coins the planner found a wolf-free route (cost %d): %s", s, r.Cost, r.String())
		}
	}
}

// With the two fares it plans the boat chain, and no step of it (the account's own start
// square aside) is in wolf territory.
func TestMuleHomeTakesTheBoatsWithTheFare(t *testing.T) {
	_, p := muleRealPack(t)
	rects := muleWolfRects()
	starts := append(append([]webwalk.Point(nil), muleEastStarts...), webwalk.Point{X: 2772, Z: 3235})
	for _, s := range starts {
		r := mulePlan(p, s, 60)
		if !r.OK {
			t.Errorf("from %s with 60 coins: no route (%s)", s, r.Reason)
			continue
		}
		ships := 0
		for _, h := range r.Hops() {
			if h.Kind == webwalk.KindShip {
				ships++
			}
		}
		if ships == 0 {
			t.Errorf("from %s the wolf-free route takes no ship: %s", s, r.String())
		}
		left := !webwalk.InZones(s, rects)
		prev := s
		for _, w := range r.Waypoints {
			seg := []webwalk.Point{prev, w.Point}
			if w.Hop != nil || prev.Level != w.Level {
				seg = []webwalk.Point{w.Point}
			}
			for _, pt := range expandForTest(seg) {
				in := webwalk.InZones(pt, rects)
				if !in {
					left = true
				} else if left {
					t.Fatalf("from %s the route re-enters wolf territory at %s", s, pt)
				}
			}
			prev = w.Point
		}
	}
}

func expandForTest(corners []webwalk.Point) []webwalk.Point {
	if len(corners) == 0 {
		return nil
	}
	out := []webwalk.Point{corners[0]}
	for _, c := range corners[1:] {
		cur := out[len(out)-1]
		for cur != c {
			if cur.X < c.X {
				cur.X++
			} else if cur.X > c.X {
				cur.X--
			}
			if cur.Z < c.Z {
				cur.Z++
			} else if cur.Z > c.Z {
				cur.Z--
			}
			out = append(out, cur)
		}
	}
	return out
}

// The short local walk is kept for Catherby's own side (so the range room and the beach
// behave exactly as before) and refused on the foothills east of the pass, which are inside
// fisherplan.InCatherby's radius but whose plain route home is over the mountain.
func TestMuleLocalWalkOnlyOnCatherbysSide(t *testing.T) {
	pk, _ := muleRealPack(t)
	home := tile.Tile{X: 2809, Z: 3441}
	rects := muleWolfRects()
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{tile.Tile{X: 2817, Z: 3443}, true}, // the range room
		{tile.Tile{X: 2828, Z: 3441}, true}, // labfleettest's short displacement
		{tile.Tile{X: 2846, Z: 3430}, true}, // the beach
		{tile.Tile{X: 2860, Z: 3430}, true},
		{tile.Tile{X: 2868, Z: 3435}, false},
		{tile.Tile{X: 2875, Z: 3410}, false},
		{tile.Tile{X: 2898, Z: 3423, Level: 1}, false},
		{tile.Tile{X: 2934, Z: 3320}, false},
	} {
		if got := muleLocalWalkSafe(pk, c.at, home, rects); got != c.want {
			t.Errorf("muleLocalWalkSafe(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}
