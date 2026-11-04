package freshstartrunner

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// The before/after harness for lowCombatZoneIDs, after internal/webwalk/seershunters_test.go
// (branch claude/seersdanger): plan every walk this package's own router makes for the fleet,
// both ways, at combat 3, with the zone list as it was and as it is, and require the same
// outcome and the same number of tiles. It plans with pathfind.FindPathAvoiding, the search
// walkNearCore itself runs (findPathAvoidingDanger), not webwalk's planner, because that is the
// router the zones are being added to.
//
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run LowCombatRoutes ./internal/scripts/freshstartrunner/
//
// RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG add the in-camp tree walks (anchor to every tree, tree
// to tree), which is what a woodcutter asks this router for most.

// preFszonesZoneIDs is lowCombatZoneIDs before "white-wolf-dens" and "seers-north-highwaymen".
var preFszonesZoneIDs = []string{"draynor-jail-guards", "draynor-highwayman"}

var routePack struct {
	once sync.Once
	pk   *pathfind.Pack
	err  error
}

func realRoutePack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	routePack.once.Do(func() { routePack.pk, routePack.err = pathfind.Load(path) })
	if routePack.err != nil {
		t.Fatal(routePack.err)
	}
	return routePack.pk
}

// routeLeg is one walk the fleet asks this router for.
type routeLeg struct {
	name     string
	from, to pathfind.NavPoint
	// oneWay plans only from -> to; otherwise the leg is planned both ways.
	oneWay bool
}

func np(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

// planAt is findPathAvoidingDanger's search (planAvoidingZones, pocket escape included) for an
// account of this combat level with this zone list, on the plain pack (no live walls).
func planAt(pk *pathfind.Pack, ids []string, combat int, from, to pathfind.NavPoint) pathfind.PathResult {
	res, _ := planEscaping(pk, ids, combat, from, to)
	return res
}

// planEscaping is planAt that also says whether the route is a pocket escape.
func planEscaping(pk *pathfind.Pack, ids []string, combat int, from, to pathfind.NavPoint) (pathfind.PathResult, bool) {
	return planAvoidingZones(pk, from, to, zoneAvoidWith(ids, knownCombat(combat, from, to)))
}

// routeTiles is every tile a route steps on, from its first waypoint.
func routeTiles(r pathfind.PathResult) []pathfind.NavPoint {
	if len(r.Waypoints) == 0 {
		return nil
	}
	out := []pathfind.NavPoint{r.Waypoints[0]}
	cur := r.Waypoints[0]
	for _, w := range r.Waypoints[1:] {
		for cur != w {
			cur.X += sign(w.X - cur.X)
			cur.Z += sign(w.Z - cur.Z)
			out = append(out, cur)
		}
	}
	return out
}

// routeSteps is how many tiles the route walks.
func routeSteps(r pathfind.PathResult) int { return len(routeTiles(r)) - 1 }

// hunter is a hunting spawn near the fleet and how far from it a player can be noticed: the
// numbers seershunters_test.go derives from content and NpcType (maxrange + huntrange).
type hunter struct {
	name  string
	at    pathfind.NavPoint
	reach int
	// maxCombat is the highest combat level it hunts, 0 for every level.
	maxCombat int
}

var fleetHunters = []hunter{
	{"highwayman", pathfind.NavPoint{X: 2735, Z: 3521}, 15, 10},
	{"highwayman", pathfind.NavPoint{X: 2749, Z: 3521}, 15, 10},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2842, Z: 3450}, 7, 0},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2846, Z: 3477}, 7, 0},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2847, Z: 3474}, 7, 0},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2864, Z: 3453}, 7, 0},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2870, Z: 3438}, 7, 0},
	{"whitewolf_sentry", pathfind.NavPoint{X: 2872, Z: 3445}, 7, 0},
	{"whitewolf", pathfind.NavPoint{X: 2848, Z: 3487}, 7, 0},
	{"whitewolf", pathfind.NavPoint{X: 2850, Z: 3484}, 7, 0},
}

// closestHunter is the route's nearest approach to a hunter that hunts this combat level,
// ignoring the route's own endpoints (a walk to or from inside a zone is exempt on purpose).
func closestHunter(r pathfind.PathResult, combat int) (int, hunter, pathfind.NavPoint) {
	best, who, at := 1<<30, hunter{}, pathfind.NavPoint{}
	tiles := routeTiles(r)
	for i, p := range tiles {
		if i == 0 || i == len(tiles)-1 || p.Level != 0 {
			continue
		}
		for _, h := range fleetHunters {
			if h.maxCombat > 0 && combat > h.maxCombat {
				continue
			}
			if d := tileDist(p, h.at); d < best {
				best, who, at = d, h, p
			}
		}
	}
	return best, who, at
}

// catherbySpots is content's fishing_movement_catherby_enum (skill_fishing/configs/
// fishing_movement.enum): every tile a Catherby fishing spot moves between.
var catherbySpots = []pathfind.NavPoint{
	{X: 2836, Z: 3431}, {X: 2837, Z: 3431}, {X: 2838, Z: 3431},
	{X: 2844, Z: 3429}, {X: 2845, Z: 3429}, {X: 2846, Z: 3429},
	{X: 2853, Z: 3423}, {X: 2854, Z: 3423}, {X: 2855, Z: 3423},
	{X: 2859, Z: 3426}, {X: 2860, Z: 3426},
}

// catherbyStands is every walkable tile beside a Catherby spot, the tiles a fisher fishes from.
func catherbyStands(pk *pathfind.Pack) []pathfind.NavPoint {
	seen := map[pathfind.NavPoint]bool{}
	var out []pathfind.NavPoint
	for _, s := range catherbySpots {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				st := pathfind.NavPoint{X: s.X + dx, Z: s.Z + dz}
				if (dx == 0 && dz == 0) || seen[st] || !pk.Walkable(st.X, st.Z, 0) {
					continue
				}
				seen[st] = true
				out = append(out, st)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}

// seersPlaces are the Seers-side tiles the woodcutters, wcfletch, the crafter and the mule walk
// between. The wcfletch patch anchors are claude/wcrung's NormalPatches (743427b7), not on main;
// its two evergreen stands (dropped there after wcrg13) are kept here because they are the shape
// this change is for.
func seersPlaces() map[string]tile.Tile {
	p := map[string]tile.Tile{
		"Seers bank":                            woodcutplan.Seers,
		"Seers mule stand":                      mulereceiverplan.SeersStand,
		"Seers knife":                           woodcutplan.SeersKnife,
		"Catherby knife":                        woodcutplan.CatherbyKnife,
		"South Seers knife":                     woodcutplan.SouthSeersKnife,
		"Camelot teleport":                      {X: 2757, Z: 3478},
		"crafter flax field":                    {X: 2741, Z: 3444},
		"flax field":                            {X: 2742, Z: 3452},
		"fail-teleport Camelot topiary":         {X: 2787, Z: 3515},
		"wcfletch south field":                  {X: 2718, Z: 3442},
		"wcfletch Catherby road":                {X: 2765, Z: 3463},
		"wcfletch Ranging Guild east":           {X: 2692, Z: 3442},
		"wcfletch Catherby west":                {X: 2776, Z: 3442},
		"wcfletch south-west":                   {X: 2699, Z: 3426},
		"wcfletch Catherby north":               {X: 2792, Z: 3459},
		"wcfletch Seers south dead trees":       {X: 2709, Z: 3462},
		"wcfletch Ranging Guild gate":           {X: 2675, Z: 3445},
		"wcfletch (dropped) Camelot evergreens": {X: 2782, Z: 3503},
		"wcfletch (dropped) east evergreens":    {X: 2787, Z: 3482},
	}
	for _, tier := range woodcutplan.TIERS {
		p["woodcut "+tier.Label] = tier.Anchor
	}
	return p
}

// fleetRouteLegs is every leg the comparison plans, each once (planned both ways).
func fleetRouteLegs(pk *pathfind.Pack) []routeLeg {
	c, g := fisherplan.Catherby(), fisherplan.FishingGuild()
	cath := map[string]tile.Tile{
		"Catherby bank":          c.Bank,
		"Catherby camp":          c.Camp,
		"Harry":                  c.HarryStand,
		"Arhein":                 c.ArheinStand,
		"Catherby range":         mulereceiverplan.CatherbyRange,
		"Catherby camp east end": {X: 2865, Z: 3428},
	}
	var legs []routeLeg
	names := func(m map[string]tile.Tile) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	// The Catherby fisher: every pair of its places, and bank and camp to every stand tile.
	cn := names(cath)
	for i, a := range cn {
		for _, b := range cn[i+1:] {
			legs = append(legs, routeLeg{"fisher " + a + " -> " + b, np(cath[a]), np(cath[b]), false})
		}
	}
	stands := catherbyStands(pk)
	for _, st := range stands {
		legs = append(legs,
			routeLeg{fmt.Sprintf("fisher Catherby bank -> stand %d,%d", st.X, st.Z), np(c.Bank), st, false},
			routeLeg{fmt.Sprintf("fisher Catherby camp -> stand %d,%d", st.X, st.Z), np(c.Camp), st, false})
	}
	// Spot changes: every stand to every other stand.
	for i, a := range stands {
		for _, b := range stands[i+1:] {
			legs = append(legs, routeLeg{fmt.Sprintf("fisher stand %d,%d -> stand %d,%d", a.X, a.Z, b.X, b.Z), a, b, false})
		}
	}
	legs = append(legs,
		routeLeg{"fisher Guild bank -> Guild camp", np(g.Bank), np(g.Camp), false},
		routeLeg{"fisher Catherby bank -> Guild bank", np(c.Bank), np(g.Bank), false},
	)
	// The Seers side: the bank to every place, and Catherby bank to every place.
	sp := seersPlaces()
	for _, n := range names(sp) {
		if n != "Seers bank" {
			legs = append(legs, routeLeg{"seers Seers bank -> " + n, np(woodcutplan.Seers), np(sp[n]), false})
		}
		legs = append(legs, routeLeg{"seers Catherby bank -> " + n, np(c.Bank), np(sp[n]), false})
	}
	legs = append(legs, routeLeg{"barbarian Seers bank -> outpost pipe", np(mulereceiverplan.SeersStand), pathfind.NavPoint{X: 2552, Z: 3561}, false})
	// freshstart: cmd/webwalkplan's scriptRoutes freshstart legs, with each ship-deck end
	// replaced by the ground tile its gangplank lands on (webwalk graph.json), since this router
	// is same-level only and SailLeg crosses the plank itself.
	legs = append(legs,
		routeLeg{"freshstart lumbridge-store", np(freshstart.LumbridgeCentre), np(freshstart.StoreStand), false},
		routeLeg{"freshstart lumbridge-pen", np(freshstart.LumbridgeCentre), np(freshstart.LumbridgePen), false},
		routeLeg{"freshstart lumbridge-shears", np(freshstart.LumbridgeCentre), np(freshstart.ShearsSpawn), false},
		routeLeg{"freshstart lumbridge-draynor", np(freshstart.LumbridgeCentre), np(freshstart.Draynor), false},
		routeLeg{"freshstart draynor-portsarim", np(freshstart.Draynor), np(freshstart.PortSarimDock), false},
		routeLeg{"freshstart lumbridge-portsarim", np(freshstart.LumbridgeCentre), np(freshstart.PortSarimDock), false},
		routeLeg{"freshstart karamja plank foot-mainlandgate", pathfind.NavPoint{X: 2956, Z: 3147}, np(freshstart.KaramjaMainlandGate), false},
		routeLeg{"freshstart karamja-brimhaven", np(freshstart.KaramjaMainlandGate), np(freshstart.BrimhavenPier), false},
		routeLeg{"freshstart ardougne plank foot-catherby", pathfind.NavPoint{X: 2683, Z: 3271}, np(c.Bank), false},
		routeLeg{"freshstart ardougne plank foot-seers", pathfind.NavPoint{X: 2683, Z: 3271}, np(woodcutplan.Seers), false},
		routeLeg{"freshstart brimhaven plank foot-pier", pathfind.NavPoint{X: 2772, Z: 3234}, np(freshstart.BrimhavenPier), false},
		routeLeg{"freshstart varrock-thessalia", np(freshstart.LumbridgeCentre), np(freshstart.ThessaliaStand), false},
	)
	return legs
}

// campTrees is every tree of the tier's name within its leash of its anchor, from the loc index.
func campTrees(t *testing.T, anchor tile.Tile, name string, radius int) []pathfind.NavPoint {
	ix, cfg := realLocs(t)
	var out []pathfind.NavPoint
	seen := map[pathfind.NavPoint]bool{}
	for _, m := range ix.Find(cfg, locindex.Query{X: anchor.X, Z: anchor.Z, Level: anchor.Level, Radius: radius, Name: name}, nil) {
		p := pathfind.NavPoint{X: m.X, Z: m.Z, Level: m.Level}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}

var locData struct {
	once sync.Once
	ix   *locindex.Index
	cfg  *cachecfg.Config
	err  error
}

func realLocs(t *testing.T) (*locindex.Index, *cachecfg.Config) {
	t.Helper()
	ix, cfg, ok := optionalLocs(t)
	if !ok {
		t.Skip("RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG not set")
	}
	return ix, cfg
}

// optionalLocs is realLocs for a test that runs without them, doing less.
func optionalLocs(t *testing.T) (*locindex.Index, *cachecfg.Config, bool) {
	t.Helper()
	lp, cp := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if lp == "" || cp == "" {
		return nil, nil, false
	}
	locData.once.Do(func() {
		if locData.ix, locData.err = locindex.Load(lp); locData.err != nil {
			return
		}
		locData.cfg, locData.err = cachecfg.LoadFile(cp)
	})
	if locData.err != nil {
		t.Fatal(locData.err)
	}
	return locData.ix, locData.cfg, true
}

// doorsOpen is pk with every closed door within 48 tiles of either end opened, found the way
// pickPlannedDoor finds them (doorNames with an "Open" op, straight or corner walls that block):
// the pack a walk plans on once walkNearCore's door recovery has opened the way. Nil without the
// loc index.
func doorsOpen(t *testing.T, pk *pathfind.Pack, ends ...pathfind.NavPoint) *pathfind.Pack {
	ix, cfg, ok := optionalLocs(t)
	if !ok {
		return nil
	}
	var edits []pathfind.WallEdit
	seen := map[[4]int]bool{}
	for _, e := range ends {
		for _, name := range doorNames {
			for _, op := range doorOps {
				for _, m := range ix.Find(cfg, locindex.Query{X: e.X, Z: e.Z, Level: e.Level, Radius: 48, Name: name, Op: op}, nil) {
					info, ok := cfg.Loc(m.ID)
					if (m.Shape != 0 && m.Shape != 2) || !ok || !info.BlockWalk || cachecfg.OpIndex(info.Ops, op) == 0 {
						continue
					}
					if k := [4]int{m.X, m.Z, m.Level, m.Shape}; !seen[k] {
						seen[k] = true
						edits = append(edits, pathfind.WallEdit{X: m.X, Z: m.Z, Level: m.Level, Shape: m.Shape, Angle: m.Angle})
					}
				}
			}
		}
	}
	return pk.WithWalls(edits)
}

// legChange is one leg whose route the two zones changed.
type legChange struct {
	name          string
	before, after pathfind.PathResult
}

// legComparison is what compareLegs found.
type legComparison struct {
	// planned counts legs times directions; plainUnplannable, those the pack cannot plan even
	// with no zone at all (indoors behind a door the pack bakes shut, across water), which no
	// zone list can make plannable because a zone only removes tiles.
	planned, plainUnplannable int
	plainNames                []string
	// viaDoors counts the legs planned only once the doors near their ends were opened.
	viaDoors int
	changed  []legChange
	// escaped names the legs whose after route is a pocket escape (planAvoidingZones): allowed
	// through a zone on purpose, so not held to the hunter check.
	escaped []string
}

// compareLegs plans each leg (both ways unless oneWay) at combat with the zone list before and
// after, and keeps every leg whose outcome or length differs. An after route that still passes a
// hunter it hunts is an error whatever the before was. With openDoors, a leg the pack cannot plan
// is planned again with the doors near its ends open (doorsOpen), since that is the pack the walk
// really plans on once walkNearCore has opened them.
func compareLegs(t *testing.T, pk *pathfind.Pack, legs []routeLeg, combat int, openDoors bool) legComparison {
	t.Helper()
	var c legComparison
	for _, l := range legs {
		for dir := 0; dir < 2; dir++ {
			if dir == 1 && l.oneWay {
				break
			}
			from, to, name := l.from, l.to, l.name
			if dir == 1 {
				from, to, name = l.to, l.from, l.name+" (back)"
			}
			c.planned++
			on := pk
			if plain := pk.FindPath(from, to); !plain.OK {
				var opened *pathfind.Pack
				if openDoors {
					opened = doorsOpen(t, pk, from, to)
				}
				if opened == nil || !opened.FindPath(from, to).OK {
					c.plainUnplannable++
					c.plainNames = append(c.plainNames, name+" ("+plain.Reason+")")
					continue
				}
				on, name = opened, name+" [doors open]"
				c.viaDoors++
			}
			before := planAt(on, preFszonesZoneIDs, combat, from, to)
			after, escaped := planEscaping(on, lowCombatZoneIDs, combat, from, to)
			if before.OK != after.OK || routeSteps(before) != routeSteps(after) {
				c.changed = append(c.changed, legChange{name, before, after})
			}
			if !after.OK {
				continue
			}
			if escaped {
				c.escaped = append(c.escaped, name)
				continue
			}
			if d, h, at := closestHunter(after, combat); d <= h.reach {
				t.Errorf("%s at combat %d: passes the %s at %d,%d within %d tiles (at %d,%d; reach %d)", name, combat, h.name, h.at.X, h.at.Z, d, at.X, at.Z, h.reach)
			}
		}
	}
	return c
}

// describe is one line for a changed leg.
func (lc legChange) describe(combat int) string {
	out := lc.name + ": before no route"
	if lc.before.OK {
		out = fmt.Sprintf("%s: before %d tiles", lc.name, routeSteps(lc.before))
		d, h, at := closestHunter(lc.before, combat)
		out += fmt.Sprintf(" (passing %d tiles from the %s at %d,%d, at %d,%d)", d, h.name, h.at.X, h.at.Z, at.X, at.Z)
	}
	if lc.after.OK {
		out += fmt.Sprintf(", after %d tiles", routeSteps(lc.after))
	} else {
		out += ", after no route (" + lc.after.Reason + ")"
	}
	return out
}

// requireUnchanged fails on every changed leg, and on every pocket escape: none of these walks
// starts anywhere the zones wall in.
func requireUnchanged(t *testing.T, what string, c legComparison, combat int) {
	t.Helper()
	for _, lc := range c.changed {
		t.Errorf("%s changed at combat %d: %s", what, combat, lc.describe(combat))
	}
	for _, n := range c.escaped {
		t.Errorf("%s at combat %d: %s starts in a pocket the zones wall in", what, combat, n)
	}
	t.Logf("%s: %d walks planned at combat %d (%d of them only with the doors near their ends open), %d changed by the two zones, %d with no route even with no zone",
		what, c.planned, combat, c.viaDoors, len(c.changed), c.plainUnplannable)
}

// TestLowCombatRoutesFleetLegsUnchanged is the proof the brief asked for: with the two zones
// added, no Catherby fisher leg (bank, camp, every stand beside every spot, stand to stand,
// Harry, Arhein, the range, the Guild), no Seers woodcutter, wcfletch, crafter or mule leg, and
// no freshstart leg plans differently at combat 3 (the live fishers and woodcutters), in
// outcome or in length.
func TestLowCombatRoutesFleetLegsUnchanged(t *testing.T) {
	pk := realRoutePack(t)
	c := compareLegs(t, pk, fleetRouteLegs(pk), 3, true)
	for _, n := range c.plainNames {
		t.Logf("no route even with no zone, before and after alike: %s", n)
	}
	requireUnchanged(t, "fleet legs", c, 3)
}

// wcfletchPatches are claude/wcrung's rung-1 NormalPatches after 743427b7 (not on main): the
// camps a wcfletch account is handed trees in, each walked from its anchor without a Walker.
var wcfletchPatches = []struct {
	name, loc string
	anchor    tile.Tile
	leash     int
}{
	{"south field", "Tree", tile.Tile{X: 2718, Z: 3442}, 8},
	{"Catherby road", "Tree", tile.Tile{X: 2765, Z: 3463}, 12},
	{"Ranging Guild east", "Tree", tile.Tile{X: 2692, Z: 3442}, 9},
	{"Catherby west", "Tree", tile.Tile{X: 2776, Z: 3442}, 8},
	{"south-west", "Tree", tile.Tile{X: 2699, Z: 3426}, 9},
	{"Catherby north", "Tree", tile.Tile{X: 2792, Z: 3459}, 8},
	{"Seers south dead trees", "Dead tree", tile.Tile{X: 2709, Z: 3462}, 5},
	{"Ranging Guild gate", "Tree", tile.Tile{X: 2675, Z: 3445}, 6},
}

// campWalks is a camp's in-camp legs, walked the way woodcutrunner walks them: from the anchor to
// every tree of that name within 2*leash+2 (the hand-out radius wcfletch's own patch test uses),
// then from the tile each tree is chopped from (the end of the anchor's route to it) back to the
// anchor and, with pairs, to every other tree. A tree the plain pack cannot route to from the anchor is one
// woodcutrunner's own Routable check already refuses, so it is counted and left out.
func campWalks(t *testing.T, pk *pathfind.Pack, label, loc string, anchor tile.Tile, leash int, pairs bool) (legs []routeLeg, trees, unroutable int) {
	type chop struct{ tree, stand pathfind.NavPoint }
	var ok []chop
	for _, tr := range campTrees(t, anchor, loc, 2*leash+2) {
		r := pk.FindPath(np(anchor), tr)
		if !r.OK || len(r.Waypoints) == 0 {
			unroutable++
			continue
		}
		ok = append(ok, chop{tr, r.Waypoints[len(r.Waypoints)-1]})
	}
	for _, a := range ok {
		legs = append(legs,
			routeLeg{fmt.Sprintf("%s anchor -> %s %d,%d", label, loc, a.tree.X, a.tree.Z), np(anchor), a.tree, true},
			routeLeg{fmt.Sprintf("%s stand %d,%d -> anchor", label, a.stand.X, a.stand.Z), a.stand, np(anchor), true})
		for _, b := range ok {
			if pairs && a != b {
				legs = append(legs, routeLeg{fmt.Sprintf("%s stand %d,%d -> %s %d,%d", label, a.stand.X, a.stand.Z, loc, b.tree.X, b.tree.Z), a.stand, b.tree, true})
			}
		}
	}
	return legs, len(ok), unroutable
}

// TestLowCombatRoutesCampTreeWalksUnchanged is the same for the walks a woodcutter makes inside a
// camp, which never have a webwalk Walker: every Seers woodcutting tier and every wcfletch patch.
func TestLowCombatRoutesCampTreeWalksUnchanged(t *testing.T) {
	pk := realRoutePack(t)
	var legs []routeLeg
	trees, unroutable := 0, 0
	for _, tier := range woodcutplan.TIERS {
		l, n, u := campWalks(t, pk, "woodcut "+tier.Label, tier.Loc, tier.Anchor, tier.Leash, true)
		legs, trees, unroutable = append(legs, l...), trees+n, unroutable+u
	}
	for _, p := range wcfletchPatches {
		l, n, u := campWalks(t, pk, "wcfletch "+p.name, p.loc, p.anchor, p.leash, true)
		legs, trees, unroutable = append(legs, l...), trees+n, unroutable+u
	}
	t.Logf("%d trees in reach of their camp's anchor (%d more the plain pack cannot route to, left out)", trees, unroutable)
	requireUnchanged(t, "in-camp tree walks", compareLegs(t, pk, legs, 3, false), 3)
}

// TestLowCombatRoutesZonePocketsWalkOut: an account already standing where the zones wall it in
// is walked out, by the route it had before the zones, instead of being left there. 2835,3468 is
// where wcrg13 ended its lab run (its webwalk walks, which have had the wolf zone since
// 2026-09-19, found no way out of it for nine minutes); 2793,3500 is the far Camelot evergreen
// stand, which the pack reaches only past the 2842,3450 sentry. Neither is inside a rectangle,
// so the endpoint exemption alone would leave both stuck.
func TestLowCombatRoutesZonePocketsWalkOut(t *testing.T) {
	pk := realRoutePack(t)
	for _, from := range []pathfind.NavPoint{{X: 2835, Z: 3468}, {X: 2793, Z: 3500}} {
		for _, to := range []pathfind.NavPoint{catherbyBank, seersBank} {
			zoned := zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, from, to))
			if pk.FindPathAvoiding(from, to, zoned).OK {
				t.Fatalf("%v -> %v plans without leaving through a zone; this is no longer a pocket, pick another", from, to)
			}
			before := planAt(pk, preFszonesZoneIDs, 3, from, to)
			res, escaped := planAvoidingZones(pk, from, to, zoned)
			if !res.OK || !escaped {
				t.Errorf("%v -> %v: ok=%v escaped=%v (%s); the account is left walled in", from, to, res.OK, escaped, res.Reason)
				continue
			}
			if routeSteps(res) != routeSteps(before) {
				t.Errorf("%v -> %v: the way out is %d tiles, the route before the zones %d", from, to, routeSteps(res), routeSteps(before))
			}
			t.Logf("%v -> %v: walked out, %d tiles, the same as before the zones", from, to, routeSteps(res))
		}
	}
}

// TestLowCombatRoutesWcrg13EvergreenWalk is the walk that started this, lab 2026-09-23
// (private evidence notes 06:50:13): wcrg13, combat 3, between two
// Camelot evergreens, "FindPath 2784,3503,0 -> 2791,3502,0: 92 waypoints", out round Camelot and
// up among the White wolf sentries. Before, the pack router plans exactly that; after, it plans
// nothing that goes near a wolf. And every in-camp walk the two zones change at either evergreen
// stand (the stands claude/wcrung dropped for this) is one whose old route went within a wolf's
// reach; a walk back from a tree behind the mountain is not among them, because the pocket
// escape (planAvoidingZones) walks it out the way it came.
func TestLowCombatRoutesWcrg13EvergreenWalk(t *testing.T) {
	pk := realRoutePack(t)
	from, to := pathfind.NavPoint{X: 2784, Z: 3503}, pathfind.NavPoint{X: 2791, Z: 3502}
	before := planAt(pk, preFszonesZoneIDs, 3, from, to)
	if !before.OK {
		t.Fatalf("before: no route at all (%s); the lab walk had one", before.Reason)
	}
	d, h, at := closestHunter(before, 3)
	if d > h.reach {
		t.Fatalf("before: %d tiles, never within a hunter's reach (closest %d from the %s at %d,%d); the lab walk went among the wolves", routeSteps(before), d, h.name, h.at.X, h.at.Z)
	}
	t.Logf("before: %d waypoints, %d tiles, within %d of the %s at %d,%d (at %d,%d)", len(before.Waypoints), routeSteps(before), d, h.name, h.at.X, h.at.Z, at.X, at.Z)
	after := planAt(pk, lowCombatZoneIDs, 3, from, to)
	if after.OK {
		if d, h, at := closestHunter(after, 3); d <= h.reach {
			t.Fatalf("after: still passes the %s at %d,%d within %d (at %d,%d)", h.name, h.at.X, h.at.Z, d, at.X, at.Z)
		}
		t.Logf("after: a clear route of %d tiles", routeSteps(after))
	} else {
		t.Logf("after: no route (%s)", after.Reason)
	}
	if !after.OK && !zoneBlocked(pk, from, to, after) {
		t.Error("zoneBlocked does not recognise a walk only the zones refuse, so walkNearCore would start door and STRANDED recovery on it")
	}

	var legs []routeLeg
	for _, stand := range []struct {
		name   string
		anchor tile.Tile
		leash  int
	}{{"Camelot evergreens", tile.Tile{X: 2782, Z: 3503}, 9}, {"east evergreens", tile.Tile{X: 2787, Z: 3482}, 8}} {
		// The anchor and back only: 2,052 tree-to-tree walks there were also measured, 546 of them
		// changed and every one a route past the 2842,3450 sentry, but each refused walk floods
		// the map and together they took nine minutes.
		l, _, _ := campWalks(t, pk, stand.name, "Evergreen", stand.anchor, stand.leash, false)
		legs = append(legs, l...)
	}
	c := compareLegs(t, pk, legs, 3, false)
	for _, lc := range c.changed {
		if !lc.before.OK {
			t.Errorf("%s: the zones made an unplannable walk plannable, which removing tiles cannot do", lc.describe(3))
			continue
		}
		if d, h, _ := closestHunter(lc.before, 3); d > h.reach {
			t.Errorf("changed without a hunter to answer for it: %s", lc.describe(3))
		}
		t.Logf("changed: %s", lc.describe(3))
	}
	for _, n := range c.escaped {
		t.Logf("walked out of the pocket behind the mountain: %s", n)
	}
	t.Logf("evergreen stands: %d walks planned, %d changed (every one of them a route among the wolves before), %d walked out of the pocket by the route they had before", c.planned, len(c.changed), len(c.escaped))
}
