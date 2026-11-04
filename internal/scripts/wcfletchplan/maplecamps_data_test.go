package wcfletchplan_test

import (
	"fmt"
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
	"rsb0t-go/internal/webwalk"
)

// accountCombat is the combat level the zone rule holds every camp to: the live maple cohort's
// (hitpoints 10, no combat skills), the level at which every automatic zone near Seers applies.
const accountCombat = 3

// zoneAt is the danger zone covering t for a combat-3 account, or "".
func zoneAt(t tile.Tile) string {
	for _, z := range webwalk.KnownDangerZones {
		if z.Automatic && z.AvoidAtOrBelowCombat > 0 && z.AvoidAtOrBelowCombat < accountCombat {
			continue
		}
		for _, r := range z.Rects {
			if (r.Level == webwalk.AllLevels || r.Level == 0) && t.X >= r.MinX && t.X <= r.MaxX && t.Z >= r.MinZ && t.Z <= r.MaxZ {
				return z.ID
			}
		}
	}
	return ""
}

// TestMapleCampsAgreeWithTheLocIndex re-derives every maple-board camp's tree count and walk
// from the real loc index and pack, and holds each to patches.go's rules plus the zone rule
// (maplecamps.go's header): a content or pack change fails here instead of sending the cohort to
// a camp that is not there or past something that kills it. It asks the runner's own questions,
// through woodcutrunner's exported helpers, exactly as TestPatchesAgreeWithTheLocIndex does.
//
//	RSB0T_LOC_INDEX=<locs.lcli.gz> RSB0T_CONFIG_JAG=<config archive> \
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run TestMapleCampsAgreeWithTheLocIndex ./internal/scripts/wcfletchplan/
func TestMapleCampsAgreeWithTheLocIndex(t *testing.T) {
	checkCampsAgainstTheData(t, wcfletchplan.MapleCamps, false)
}

// checkCampsAgainstTheData is the check every board's camps are held to, the maple board's
// (above) and the yew board's (yewcamps_data_test.go): it re-derives each camp's tree count and
// walk from the real loc index and pack and holds it to the safety rules in maplecamps.go's header.
// It skips unless the three data files are named.
//
// standWithinLeash adds a rule the maple camps were drawn without and the yew camps are held to:
// every counted tree has a stand tile within the camp's Leash of its Anchor. A tree whose nearest
// stand tile is further out leaves an account chopping it "not at the camp" (woodcutplan.AtCamp),
// so the next pass walks it back to the anchor and out to the tree again each time a tree falls.
// Where the rule is off it is only logged, so the maple camps' own excess stays on the record.
func checkCampsAgainstTheData(t *testing.T, camps []wcfletchplan.Camp, standWithinLeash bool) {
	t.Helper()
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK to check the camps against the real loc index and pack")
	}
	ix, err := locindex.Load(locsPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	countedBy := map[tile.Tile]string{}

	for _, c := range camps {
		bank := c.BankOf().Floor // the Seers bank floor (2725,3491), in front of the booths, unless the camp names another
		if !pk.Walkable(c.Anchor.X, c.Anchor.Z, 0) {
			t.Errorf("%s: anchor %v is not walkable", c.Name, c.Anchor)
			continue
		}
		// The trees the camp counts: within its leash, with a stand tile the bank routes to.
		reachable, standGap, standWho := 0, 1<<30, ""
		farthestStand, farthestTree := 0, tile.Tile{}
		for _, m := range ix.Find(cfg, locindex.Query{X: c.Anchor.X, Z: c.Anchor.Z, Level: 0, Radius: c.Leash, Name: c.Loc, Op: woodcutplan.ChopOp}, nil) {
			l, ok := woodcutrunner.LocOf(cfg, m)
			if !ok {
				continue
			}
			counted, nearest := false, 1<<30
			for _, s := range woodcutrunner.StandTiles(pk, l, c.Anchor) {
				if !woodcutrunner.Routable(pk, bank, s) {
					continue
				}
				counted = true
				if d := tile.Chebyshev(s, c.Anchor); d < nearest {
					nearest = d
				}
				if g, who := huntGap(s); g < standGap {
					standGap, standWho = g, who
				}
			}
			if counted {
				reachable++
				if nearest > farthestStand {
					farthestStand, farthestTree = nearest, tile.Tile{X: m.X, Z: m.Z}
				}
				at := tile.Tile{X: m.X, Z: m.Z}
				if other, dup := countedBy[at]; dup {
					t.Errorf("%s: the %s at %v is already counted by %s; a tree's supply belongs to one camp", c.Name, c.Loc, at, other)
				}
				countedBy[at] = c.Name
			}
		}
		if reachable != c.Trees {
			t.Errorf("%s: %d %q locs within leash %d of %v are reachable from the bank, the table says %d",
				c.Name, reachable, c.Loc, c.Leash, c.Anchor, c.Trees)
		}
		if standGap < minStandGap {
			t.Errorf("%s: a stand tile is %d tile(s) outside a %s's reach, want at least %d", c.Name, standGap, standWho, minStandGap)
		}
		if farthestStand > c.Leash {
			if standWithinLeash {
				t.Errorf("%s: the nearest stand tile of the %s at %v is %d tiles from the anchor %v, past the leash %d: an account chopping it is not 'at the camp'",
					c.Name, c.Loc, farthestTree, farthestStand, c.Anchor, c.Leash)
			} else {
				t.Logf("%s: the nearest stand tile of the %s at %v is %d from the anchor, past the leash %d", c.Name, c.Loc, farthestTree, farthestStand, c.Leash)
			}
		}

		// The walk from the bank: its length is what a load costs, and it must keep clear.
		route := pk.FindPath(pathfind.NavPoint{X: bank.X, Z: bank.Z}, pathfind.NavPoint{X: c.Anchor.X, Z: c.Anchor.Z})
		if !route.OK {
			t.Errorf("%s: no door-free route from the %s bank to %v (%s)", c.Name, c.BankOf().Name, c.Anchor, route.Reason)
			continue
		}
		if route.Steps() != c.Steps {
			t.Errorf("%s: the door-free walk from the bank is %d steps, the table says %d", c.Name, route.Steps(), c.Steps)
		}
		routeGap, routeWho, routeAt, routeZone := 1<<30, "", tile.Tile{}, ""
		prev := bank
		for _, w := range route.Waypoints {
			next := tile.Tile{X: w.X, Z: w.Z}
			for _, step := range line(prev, next) {
				if g, who := huntGap(step); g < routeGap {
					routeGap, routeWho, routeAt = g, who, step
				}
				if z := zoneAt(step); z != "" && routeZone == "" {
					routeZone = fmt.Sprintf("%s at %v", z, step)
				}
			}
			prev = next
		}
		if routeGap < minRouteGap {
			t.Errorf("%s: the route from the bank passes %d tile(s) outside a %s's reach at %v, want at least %d", c.Name, routeGap, routeWho, routeAt, minRouteGap)
		}
		if routeZone != "" {
			t.Errorf("%s: the route from the bank enters danger zone %s", c.Name, routeZone)
		}

		// Every tree the account could be handed there (Chop searches Leash+2 around a stand up to
		// Leash out): a short walk from the anchor, clear of every hunter and every zone, and no
		// stand tile inside a zone.
		inCamp, inWho, inAt, worstDetour, detourAt, inZone := 1<<30, "", tile.Tile{}, 0, tile.Tile{}, ""
		for _, m := range ix.Find(cfg, locindex.Query{X: c.Anchor.X, Z: c.Anchor.Z, Level: 0, Radius: 2*c.Leash + 2, Name: c.Loc, Op: woodcutplan.ChopOp}, nil) {
			l, ok := woodcutrunner.LocOf(cfg, m)
			if !ok {
				continue
			}
			for _, st := range woodcutrunner.StandTiles(pk, l, c.Anchor) {
				r := pk.FindPath(pathfind.NavPoint{X: c.Anchor.X, Z: c.Anchor.Z}, pathfind.NavPoint{X: st.X, Z: st.Z})
				if !r.OK {
					continue // unroutable: Chop excludes it and picks another
				}
				if z := zoneAt(st); z != "" && inZone == "" {
					inZone = fmt.Sprintf("stand tile %v of the %s at %d,%d is in %s", st, c.Loc, m.X, m.Z, z)
				}
				steps, prev := 0, c.Anchor
				for _, w := range r.Waypoints {
					next := tile.Tile{X: w.X, Z: w.Z}
					steps += tile.Chebyshev(prev, next)
					for _, step := range line(prev, next) {
						if g, who := huntGap(step); g < inCamp {
							inCamp, inWho, inAt = g, who, step
						}
						if z := zoneAt(step); z != "" && inZone == "" {
							inZone = fmt.Sprintf("the walk to %v passes %v in %s", st, step, z)
						}
					}
					prev = next
				}
				if over := steps - (2*tile.Chebyshev(c.Anchor, st) + maxDetour); over > worstDetour {
					worstDetour, detourAt = over, st
				}
			}
		}
		if worstDetour > 0 {
			t.Errorf("%s: the walk from the anchor to a stand tile at %v is %d tiles longer than a detour-free walk should be", c.Name, detourAt, worstDetour)
		}
		if inCamp < minRouteGap {
			t.Errorf("%s: a walk inside the camp passes %d tile(s) outside a %s's reach at %v, want at least %d", c.Name, inCamp, inWho, inAt, minRouteGap)
		}
		if inZone != "" {
			t.Errorf("%s: %s", c.Name, inZone)
		}
		t.Logf("%-22s %-10s %2d trees, %3d steps; nearest hunter: stand %2d (%s), route %2d (%s), in-camp %2d (%s)",
			c.Name, c.Loc, reachable, route.Steps(), standGap, standWho, routeGap, routeWho, inCamp, inWho)
	}
}

// safeButCampless are the three maples within SeersRadius that are safe to stand at and walk to
// on their own, yet in no camp. Each is within a few tiles of a maple in the north-west field's
// FENCED-OFF northern half (2684,3527, 2676,3533), whose only door-free route from here is a walk
// of about 160 steps round past the Seers Highwaymen, and Chop searches Leash+2 around wherever
// the account stands, so most camps drawn round them hand that tree out. Searched over every
// walkable anchor and leash 2-7 round them (2026-09-25): no camp round 2679,3525 passes the
// rules; 2683,3521 only as a one-tree camp; 2667,3526 only alone or by taking trees from the two
// north-west camps (12 trees between them as drawn). A one-tree camp 52-80 steps out is not
// worth a seat.
var safeButCampless = map[tile.Tile]bool{
	{X: 2667, Z: 3526}: true,
	{X: 2679, Z: 3525}: true,
	{X: 2683, Z: 3521}: true,
}

// TestNoOtherMapleIsSafeAndInReach is the other half of "chosen, not guessed": every maple
// within SeersRadius of the bank that NO camp counts is one of the refused kinds, so a content
// change that adds a safe maple (or makes a refused one safe) fails here. A maple is refused when
// every bank-routable stand tile is inside a hunter's reach margin or a zone, or every door-free
// route to it from the bank passes one, or it has no routable stand tile at all.
func TestNoOtherMapleIsSafeAndInReach(t *testing.T) {
	var maples []wcfletchplan.Camp
	for _, c := range wcfletchplan.MapleCamps {
		if !c.Overflow() {
			maples = append(maples, c)
		}
	}
	checkNoOtherTreeIsSafe(t, "Maple tree", maples, safeButCampless)
}

// checkNoOtherTreeIsSafe is that check for any board: every tree called name within SeersRadius of
// the bank that none of camps counts is a refused kind, or is listed in exempt with its reason.
func checkNoOtherTreeIsSafe(t *testing.T, name string, camps []wcfletchplan.Camp, exempt map[tile.Tile]bool) {
	t.Helper()
	checkNoOtherTreeIsSafeWithin(t, name, camps, exempt, woodcutplan.SeersRadius)
}

// checkNoOtherTreeIsSafeWithin is checkNoOtherTreeIsSafe over any radius of the Seers bank.
func checkNoOtherTreeIsSafeWithin(t *testing.T, name string, camps []wcfletchplan.Camp, exempt map[tile.Tile]bool, radius int) {
	t.Helper()
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK")
	}
	ix, err := locindex.Load(locsPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	bank := tile.Tile{X: 2725, Z: 3491}
	counted := map[tile.Tile]bool{}
	for _, c := range camps {
		for _, m := range ix.Find(cfg, locindex.Query{X: c.Anchor.X, Z: c.Anchor.Z, Level: 0, Radius: c.Leash, Name: c.Loc, Op: woodcutplan.ChopOp}, nil) {
			counted[tile.Tile{X: m.X, Z: m.Z}] = true
		}
	}
	all := ix.Find(cfg, locindex.Query{X: woodcutplan.Seers.X, Z: woodcutplan.Seers.Z, Level: 0, Radius: radius, Name: name, Op: woodcutplan.ChopOp}, nil)
	kinds := map[string]int{}
	for _, m := range all {
		at := tile.Tile{X: m.X, Z: m.Z}
		if counted[at] {
			continue
		}
		l, ok := woodcutrunner.LocOf(cfg, m)
		if !ok {
			continue
		}
		why := "no stand tile the bank routes to"
		for _, s := range woodcutrunner.StandTiles(pk, l, bank) {
			r := pk.FindPath(pathfind.NavPoint{X: bank.X, Z: bank.Z}, pathfind.NavPoint{X: s.X, Z: s.Z})
			if !r.OK {
				continue
			}
			if g, _ := huntGap(s); g < minStandGap {
				why = "stands inside a hunter's margin"
				continue
			}
			if zoneAt(s) != "" {
				why = "stands in a danger zone"
				continue
			}
			bad, prev := "", bank
			for _, w := range r.Waypoints {
				next := tile.Tile{X: w.X, Z: w.Z}
				for _, step := range line(prev, next) {
					if z := zoneAt(step); z != "" {
						bad = "its only door-free route crosses " + z
					} else if g, _ := huntGap(step); g < minRouteGap && bad == "" {
						bad = "its door-free route passes inside a hunter's margin"
					}
				}
				prev = next
			}
			if bad != "" {
				why = bad
				continue
			}
			why = ""
			break
		}
		if why == "" {
			if !exempt[at] {
				t.Errorf("the %s at %v is safe and in reach but in no camp: add it to a camp, or to the exempt list with the reason", name, at)
			}
			why = "safe on its own, but no camp can be drawn round it (exempt)"
		}
		kinds[why]++
	}
	for why, n := range kinds {
		t.Logf("%3d %s trees in no camp: %s", n, name, why)
	}
	t.Logf("%d %s trees within %d tiles of the bank, %d of them in camps", len(all), name, radius, len(counted))
}
