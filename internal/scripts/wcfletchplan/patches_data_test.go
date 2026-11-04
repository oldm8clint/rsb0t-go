package wcfletchplan_test

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// hunter is one NPC spawn with a hunt mode near Seers, read out of content: EVERY level-0 spawn
// in the NPC sections of content/maps/m38..45_51..56.jm2 within 150 tiles of the Seers bank
// (z 3300-3579) whose npc config carries a huntmode, with that config's wanderrange and huntrange
// (content/scripts/_unpack/225/all.npc where no area config overrides it). Every one of these
// hunts an account of this fleet's combat level: `cowardly` attacks anyone under twice its level,
// and the Highwaymen's `ranged` mode is the one webwalk/highwayman.go already avoids at Draynor.
//
// Two of them decided the patch list the hard way, in the lab:
//   - the two level-5 HIGHWAYMEN on the road north of Seers (2735,3521 and 2749,3521), in no
//     webwalk danger zone, which attacked an account walking to a northern patch;
//   - the WHITE WOLVES on White Wolf Mountain's southern foot (2842,3450 and 2846-2848,3472-3477),
//     below the danger zone's z 3468, which an account at the Camelot evergreens walked into:
//     one evergreen's only route from the patch goes round the castle to the east.
type hunter struct {
	name          string
	at            tile.Tile
	wander, reach int
}

var huntersNearSeers = []hunter{
	{"Giant bat", tile.Tile{X: 2578, Z: 3476}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2586, Z: 3473}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2591, Z: 3479}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2592, Z: 3484}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2594, Z: 3474}, 8, 2},       // bat
	{"Guard dog", tile.Tile{X: 2625, Z: 3502}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2628, Z: 3484}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2631, Z: 3472}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2636, Z: 3503}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2645, Z: 3488}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2659, Z: 3478}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2665, Z: 3471}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2666, Z: 3506}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2669, Z: 3488}, 3, 1},       // guarddog
	{"Guard dog", tile.Tile{X: 2674, Z: 3504}, 3, 1},       // guarddog
	{"Bear", tile.Tile{X: 2694, Z: 3344}, 4, 1},            // brownbear
	{"Bear", tile.Tile{X: 2702, Z: 3344}, 4, 1},            // brownbear
	{"Wolf", tile.Tile{X: 2717, Z: 3569}, 6, 2},            // wolf
	{"Highwayman", tile.Tile{X: 2735, Z: 3521}, 8, 5},      // highwayman
	{"Highwayman", tile.Tile{X: 2749, Z: 3521}, 8, 5},      // highwayman
	{"Giant bat", tile.Tile{X: 2752, Z: 3409}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2753, Z: 3396}, 8, 2},       // bat
	{"Giant bat", tile.Tile{X: 2759, Z: 3401}, 8, 2},       // bat
	{"Renegade Knight", tile.Tile{X: 2766, Z: 3397}, 5, 1}, // renegade_knight
	{"Renegade Knight", tile.Tile{X: 2767, Z: 3399}, 5, 1}, // renegade_knight
	{"Renegade Knight", tile.Tile{X: 2773, Z: 3399}, 5, 1}, // renegade_knight
	{"Ice giant", tile.Tile{X: 2804, Z: 3507}, 3, 2},       // icegiant
	{"Bear", tile.Tile{X: 2809, Z: 3376}, 12, 1},           // darkbear
	{"Ice giant", tile.Tile{X: 2811, Z: 3506}, 3, 2},       // icegiant
	{"Ice giant", tile.Tile{X: 2817, Z: 3514}, 3, 2},       // icegiant
	{"Ice giant", tile.Tile{X: 2824, Z: 3510}, 3, 2},       // icegiant
	{"White wolf", tile.Tile{X: 2831, Z: 3515}, 3, 2},      // whitewolf
	{"White wolf", tile.Tile{X: 2833, Z: 3513}, 3, 2},      // whitewolf
	{"Wolf", tile.Tile{X: 2836, Z: 3495}, 20, 2},           // pack_wolf
	{"Wolf", tile.Tile{X: 2837, Z: 3499}, 20, 2},           // pack_wolf
	{"Wolf", tile.Tile{X: 2839, Z: 3502}, 20, 2},           // pack_wolf
	{"Wolf", tile.Tile{X: 2839, Z: 3506}, 20, 2},           // pack_wolf
	{"Big Wolf", tile.Tile{X: 2840, Z: 3497}, 20, 2},       // wolfpack_leader
	{"White wolf", tile.Tile{X: 2842, Z: 3450}, 3, 2},      // whitewolf_sentry
	{"Wolf", tile.Tile{X: 2842, Z: 3504}, 20, 2},           // pack_wolf
	{"Wolf", tile.Tile{X: 2844, Z: 3507}, 20, 2},           // pack_wolf
	{"White wolf", tile.Tile{X: 2846, Z: 3477}, 3, 2},      // whitewolf_sentry
	{"White wolf", tile.Tile{X: 2847, Z: 3474}, 3, 2},      // whitewolf_sentry
	{"Ice warrior", tile.Tile{X: 2847, Z: 3514}, 3, 5},     // icewarrior
	{"White wolf", tile.Tile{X: 2848, Z: 3487}, 3, 2},      // whitewolf
	{"White wolf", tile.Tile{X: 2850, Z: 3484}, 3, 2},      // whitewolf
	{"Ice warrior", tile.Tile{X: 2850, Z: 3512}, 3, 5},     // icewarrior
	{"White wolf", tile.Tile{X: 2854, Z: 3509}, 3, 2},      // whitewolf
	{"White wolf", tile.Tile{X: 2856, Z: 3508}, 3, 2},      // whitewolf
	{"White wolf", tile.Tile{X: 2860, Z: 3492}, 3, 2},      // whitewolf_sentry
	{"White wolf", tile.Tile{X: 2864, Z: 3453}, 3, 2},      // whitewolf_sentry
	{"White wolf", tile.Tile{X: 2865, Z: 3509}, 3, 2},      // whitewolf_sentry
	{"White wolf", tile.Tile{X: 2870, Z: 3438}, 3, 2},      // whitewolf_sentry
	{"White wolf", tile.Tile{X: 2872, Z: 3445}, 3, 2},      // whitewolf_sentry
}

// huntGap is how many tiles t stands outside the reach of the nearest hunter: its spawn, plus
// how far it wanders, plus how far it looks. Zero or less is inside it.
func huntGap(t tile.Tile) (int, string) {
	best, who := 1<<30, ""
	for _, h := range huntersNearSeers {
		if g := tile.Chebyshev(t, h.at) - h.wander - h.reach; g < best {
			best, who = g, h.name
		}
	}
	return best, who
}

// The margins every patch must keep. A stand tile an account chops from must be at least 3
// tiles outside any hunter's reach; the door-free route from the bank at least 2.
const (
	minStandGap = 3
	minRouteGap = 2
	// maxDetour is how much longer than twice the straight-line distance a walk from the anchor
	// to a tree it could be handed may be. Past that it is going round a wall: the evergreens'
	// was 143 and 267 tiles over, while the worst kept patch (Catherby road) is 17 over, a fence.
	maxDetour = 30
)

// TestPatchesAgreeWithTheLocIndex re-derives every patch's tree count, reachability and safety
// from the real data, so a content or pack change fails here instead of sending the cohort to a
// field that is not there or past something that kills it. It asks the SAME questions the
// runner asks, through woodcutrunner's own exported helpers: locindex.Find by name and "Chop
// down" op (pickTree's query), LocOf and StandTiles (walkToInteractLoc's candidate set) and
// Routable (the pack's own A*).
//
//	RSB0T_LOC_INDEX=<locs.lcli.gz> RSB0T_CONFIG_JAG=<config archive> \
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run TestPatchesAgreeWithTheLocIndex ./internal/scripts/wcfletchplan/
func TestPatchesAgreeWithTheLocIndex(t *testing.T) {
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK to check the patches against the real loc index and pack")
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
	bank := tile.Tile{X: 2725, Z: 3491} // the Seers bank floor, in front of the booths

	for _, p := range wcfletchplan.NormalPatches {
		if !pk.Walkable(p.Anchor.X, p.Anchor.Z, 0) {
			t.Errorf("%s: anchor %v is not walkable", p.Name, p.Anchor)
		}
		all := ix.Find(cfg, locindex.Query{X: p.Anchor.X, Z: p.Anchor.Z, Level: 0, Radius: p.Leash, Name: p.Loc, Op: woodcutplan.ChopOp}, nil)
		reachable, standGap, standWho := 0, 1<<30, ""
		for _, m := range all {
			l, ok := woodcutrunner.LocOf(cfg, m)
			if !ok {
				continue
			}
			counted := false
			for _, s := range woodcutrunner.StandTiles(pk, l, p.Anchor) {
				if !woodcutrunner.Routable(pk, bank, s) {
					continue
				}
				counted = true
				if g, who := huntGap(s); g < standGap {
					standGap, standWho = g, who
				}
			}
			if counted {
				reachable++
			}
		}
		if reachable != p.Trees {
			t.Errorf("%s: %d %q locs within leash %d of %v are reachable from the bank, the table says %d",
				p.Name, reachable, p.Loc, p.Leash, p.Anchor, p.Trees)
		}
		if standGap < minStandGap {
			t.Errorf("%s: a stand tile is %d tile(s) outside a %s's reach, want at least %d", p.Name, standGap, standWho, minStandGap)
		}

		// The route an account takes there: the pack's door-free path, walked tile by tile.
		route := pk.FindPath(pathfind.NavPoint{X: bank.X, Z: bank.Z}, pathfind.NavPoint{X: p.Anchor.X, Z: p.Anchor.Z})
		if !route.OK {
			t.Errorf("%s: no door-free route from the Seers bank to %v (%s)", p.Name, p.Anchor, route.Reason)
			continue
		}
		routeGap, routeWho, at := 1<<30, "", tile.Tile{}
		prev := tile.Tile{X: bank.X, Z: bank.Z}
		for _, w := range route.Waypoints {
			next := tile.Tile{X: w.X, Z: w.Z}
			for _, step := range line(prev, next) {
				if g, who := huntGap(step); g < routeGap {
					routeGap, routeWho, at = g, who, step
				}
			}
			prev = next
		}
		if routeGap < minRouteGap {
			t.Errorf("%s: the route from the bank passes %d tile(s) outside a %s's reach at %v, want at least %d", p.Name, routeGap, routeWho, at, minRouteGap)
		}
		// And every tree the account could be handed once it is there. Chop searches Leash+2
		// around wherever the account stands, and the account stands up to Leash from the anchor,
		// so anything of this name within 2*Leash+2 can be chosen -- including trees the table
		// does not count. Each one's walk from the anchor must be a short walk, not a detour
		// round a wall, and must keep clear of every hunter.
		inPatch, worstDetour, detourAt := 1<<30, 0, tile.Tile{}
		inPatchWho, inPatchAt := "", tile.Tile{}
		for _, m := range ix.Find(cfg, locindex.Query{X: p.Anchor.X, Z: p.Anchor.Z, Level: 0, Radius: 2*p.Leash + 2, Name: p.Loc, Op: woodcutplan.ChopOp}, nil) {
			l, ok := woodcutrunner.LocOf(cfg, m)
			if !ok {
				continue
			}
			for _, st := range woodcutrunner.StandTiles(pk, l, p.Anchor) {
				r := pk.FindPath(pathfind.NavPoint{X: p.Anchor.X, Z: p.Anchor.Z}, pathfind.NavPoint{X: st.X, Z: st.Z})
				if !r.OK {
					continue // unroutable: Chop excludes it and picks another
				}
				steps, prev := 0, p.Anchor
				for _, w := range r.Waypoints {
					next := tile.Tile{X: w.X, Z: w.Z}
					steps += tile.Chebyshev(prev, next)
					for _, step := range line(prev, next) {
						if g, who := huntGap(step); g < inPatch {
							inPatch, inPatchWho, inPatchAt = g, who, step
						}
					}
					prev = next
				}
				if over := steps - (2*tile.Chebyshev(p.Anchor, st) + maxDetour); over > worstDetour {
					worstDetour, detourAt = over, st
				}
			}
		}
		if worstDetour > 0 {
			t.Errorf("%s: the walk from the anchor to a stand tile at %v is %d tiles longer than a detour-free walk should be", p.Name, detourAt, worstDetour)
		}
		if inPatch < minRouteGap {
			t.Errorf("%s: a walk inside the patch passes %d tile(s) outside a %s's reach at %v, want at least %d", p.Name, inPatch, inPatchWho, inPatchAt, minRouteGap)
		}
		t.Logf("%-24s %-10s %3d in leash, %3d reachable; nearest hunter: stand %2d (%s), route %2d (%s), in-patch %2d (%s)",
			p.Name, p.Loc, len(all), reachable, standGap, standWho, routeGap, routeWho, inPatch, inPatchWho)
	}
}

// line is every tile from a to b, one Chebyshev step at a time.
func line(a, b tile.Tile) []tile.Tile {
	n := tile.Chebyshev(a, b)
	if n == 0 {
		return []tile.Tile{a}
	}
	out := make([]tile.Tile, 0, n+1)
	for i := 0; i <= n; i++ {
		out = append(out, tile.Tile{X: a.X + (b.X-a.X)*i/n, Z: a.Z + (b.Z-a.Z)*i/n})
	}
	return out
}
