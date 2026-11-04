package questplan

// Demon Slayer's places on the real collision pack, the baked web graph, the loc index and the production config (skipped without
// RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG): every stand the module sends an account to is on a walkable tile,
// every NPC it talks to can be reached from a tile beside it, every walk it makes plans from where the account is when it starts the
// leg (the palace's stairs, the Wizards' Tower's, the manhole and the sewer's ladder included), the drain key is reached well inside
// the three minutes it lies on the sewer floor, and nothing near the stone circle is the Wilderness. A tile the walker cannot reach
// parks a quest in production after the walk times out; here it fails in a second.

import (
	"os"
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

var dsReal struct {
	once sync.Once
	pk   *pathfind.Pack
	ix   *locindex.Index
	cfg  *cachecfg.Config
	g    *webwalk.Graph
	err  error
}

func dsData(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config, *webwalk.Graph) {
	t.Helper()
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	dsReal.once.Do(func() {
		if dsReal.pk, dsReal.err = pathfind.Load(pp); dsReal.err != nil {
			return
		}
		if dsReal.ix, dsReal.err = locindex.Load(ip); dsReal.err != nil {
			return
		}
		if dsReal.cfg, dsReal.err = cachecfg.LoadFile(cp); dsReal.err != nil {
			return
		}
		dsReal.g, dsReal.err = webwalk.Default()
	})
	if dsReal.err != nil {
		t.Fatal(dsReal.err)
	}
	return dsReal.pk, dsReal.ix, dsReal.cfg, dsReal.g
}

func dsPoint(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }

func TestDemonSlayerPlacesAreOnTheMap(t *testing.T) {
	pk, ix, cfg, _ := dsData(t)
	for name, at := range map[string]tile.Tile{
		"Gypsy Aris": GypsyAnchor, "Sir Prysin": PrysinAnchor, "Captain Rovin": RovinAnchor, "Traiborn": TraibornAnchor, "Delrith": DelrithSpawn,
		"the drain": DemonDrain, "the sink stand": DemonSinkStand, "the manhole's stand": DemonManholeStand, "the sewer landing": DemonSewerLanding,
		"the drain key": DemonSewerKey, "the cow pasture": DemonCowField, "the bucket spawn": DemonBucketSpawn,
		"the Varrock west bank": VarrockWestBank, "the Draynor bank": DraynorBank, "Varrock's general store": VarrockGenStore.Anchor,
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	// The ladder's own tile is not floor (the climb lands one row south of it), the sink's is not either.
	for name, at := range map[string]tile.Tile{"the sewer ladder": DemonSewerLadder, "the sink": t3(3224, 3494, 0)} {
		if pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is a floor tile; the plan stands beside it", name, at)
		}
	}
	// Every NPC the quest talks to or fights can be reached from some tile beside its spawn.
	for name, at := range map[string]tile.Tile{"Gypsy Aris": GypsyAnchor, "Sir Prysin": PrysinAnchor, "Captain Rovin": RovinAnchor, "Traiborn": TraibornAnchor,
		"Delrith": DelrithSpawn, "the general store's keeper": VarrockGenStore.Anchor} {
		if len(pk.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level})) == 0 {
			t.Errorf("no tile to reach %s at %v from", name, at)
		}
	}
	// The locs are where the plan says, of the shape and with the ops it relies on.
	for _, c := range []struct {
		what       string
		at         tile.Tile
		id         int
		name, op   string
		shape      int
		opPosition int
	}{
		{"the drain", DemonDrain, LocQuestDrain, "Drain", "", 5, 0},
		{"the kitchen sink", t3(3224, 3494, 0), LocSink2, "Sink", "", 10, 0},
		{"the manhole", DemonManhole, LocManholeClosed, "Manhole", "Open", 10, 1},
		{"the sewer ladder", DemonSewerLadder, LocCaveLadderUp, "Ladder", "Climb-up", 10, 1},
	} {
		var found *locindex.Loc
		for _, l := range ix.At(c.at.X, c.at.Z, c.at.Level) {
			if l.ID == c.id {
				l := l
				found = &l
			}
		}
		if found == nil {
			t.Errorf("%s: loc %d is not placed at %v", c.what, c.id, c.at)
			continue
		}
		typ, ok := cfg.Loc(c.id)
		if !ok || typ.Name != c.name || found.Shape != c.shape {
			t.Errorf("%s: loc %d is %q shape %d (found %v), want %q shape %d", c.what, c.id, typ.Name, found.Shape, ok, c.name, c.shape)
		}
		if c.op != "" && cachecfg.OpIndex(typ.Ops, c.op) != c.opPosition {
			t.Errorf("%s: %q is not op %d of %v", c.what, c.op, c.opPosition, typ.Ops)
		}
	}
	// The open manhole is the same tile's other loc, and the one with the "Climb down" op.
	if typ, _ := cfg.Loc(LocManholeOpen); cachecfg.OpIndex(typ.Ops, "Climb down") != 1 {
		t.Errorf("the open manhole offers %v", typ.Ops)
	}
	// The sink is category watersource: a bucket used on it is filled (the content test pins the script); the sewer ladder's stands are
	// the tiles around it.
	if typ, _ := cfg.Loc(LocSink2); typ.Name != "Sink" {
		t.Errorf("loc %d is %q", LocSink2, typ.Name)
	}
}

// Every walk the quest makes plans from where the account is when it begins it, with the planner's automatic danger zones (the
// Wilderness, the Draynor jail guards) on, and takes the transports the plan expects.
func TestDemonSlayerRoutesPlanFromEveryStart(t *testing.T) {
	pk, _, _, g := dsData(t)
	pl := webwalk.NewPlanner(pk, g)
	type leg struct {
		name     string
		from, to tile.Tile
		hops     []string // transports the route must take, in any order; nil for none required
		maxCost  int
	}
	vw, draynor := VarrockWestBank, DraynorBank
	legs := []leg{
		{"the Varrock west bank to the Gypsy", vw, GypsyAnchor, nil, 80},
		{"the Gypsy to Sir Prysin", GypsyAnchor, PrysinAnchor, nil, 120},
		{"the bank to Sir Prysin", vw, PrysinAnchor, nil, 120},
		{"Sir Prysin to Captain Rovin (the palace's two stairs)", PrysinAnchor, RovinAnchor, []string{"Staircase Climb-up"}, 100},
		{"Rovin to the sink", RovinAnchor, DemonSinkStand, []string{"Staircase Climb-down"}, 100},
		{"the bank to the sink", vw, DemonSinkStand, nil, 160},
		{"the sink to the drain", DemonSinkStand, DemonDrain, nil, 20},
		{"the drain to the sewer's key (the manhole)", DemonDrain, DemonSewerKey, []string{"Manhole Climb down"}, 200},
		{"the drain to the manhole's stand", DemonDrain, DemonManholeStand, nil, 120},
		{"the sewer's key to Sir Prysin (the ladder up)", DemonSewerKey, PrysinAnchor, []string{"Ladder Climb-up"}, 200},
		{"the sewer's landing to the manhole's stand (the ladder up)", DemonSewerLanding, DemonManholeStand, []string{"Ladder Climb-up"}, 40},
		{"the bank to Varrock's general store", vw, VarrockGenStore.Anchor, nil, 80},
		{"the store to the bucket on the palace's first floor", VarrockGenStore.Anchor, DemonBucketSpawn, []string{"Staircase Climb-up"}, 140},
		{"the bucket to the sink", DemonBucketSpawn, DemonSinkStand, []string{"Staircase Climb-down"}, 60},
		{"Sir Prysin to the cow pasture", PrysinAnchor, DemonCowField, nil, 300},
		{"the cow pasture to Traiborn (the tower's stairs)", DemonCowField, TraibornAnchor, []string{"Staircase Climb-up"}, 320},
		{"the Draynor bank to the cow pasture", draynor, DemonCowField, nil, 260},
		{"the Draynor bank to Traiborn", draynor, TraibornAnchor, []string{"Staircase Climb-up"}, 220},
		{"Traiborn to the Draynor bank", TraibornAnchor, draynor, []string{"Staircase Climb-down"}, 220},
		{"Traiborn to Captain Rovin", TraibornAnchor, RovinAnchor, []string{"Staircase Climb-down", "Staircase Climb-up"}, 560},
		{"Sir Prysin to Delrith", PrysinAnchor, DelrithSpawn, nil, 200},
		{"the bank to Delrith", vw, DelrithSpawn, nil, 160},
		{"Delrith to Sir Prysin", DelrithSpawn, PrysinAnchor, nil, 200},
		{"Delrith to the bank", DelrithSpawn, vw, nil, 160},
		{"the Wizards' Tower's ground floor to Sir Prysin", t3(3105, 3160, 0), PrysinAnchor, nil, 520},
	}
	for _, l := range legs {
		from, to := dsPoint(l.from), dsPoint(l.to)
		opts := webwalk.Options{AllowSpecial: true,
			AvoidZones: webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, Start: &from, Dest: &to})}
		r := pl.Plan(from, to, opts)
		if !r.OK {
			t.Errorf("%s: no route from %v to %v: %s", l.name, l.from, l.to, r.Reason)
			continue
		}
		var got []string
		for _, h := range r.Hops() {
			got = append(got, h.LocName+" "+h.Action)
		}
		for _, want := range l.hops {
			if !strings.Contains(strings.Join(got, ","), want) {
				t.Errorf("%s: the route takes %v, want a %q", l.name, got, want)
			}
		}
		if r.Cost > l.maxCost {
			t.Errorf("%s: cost %d, over %d", l.name, r.Cost, l.maxCost)
		}
		// No route of the quest goes near the Wilderness.
		for _, w := range r.Waypoints {
			if w.Point.Level == 0 && w.Point.Z >= 3523 && w.Point.Z < 4000 && w.Point.X >= 2944 && w.Point.X <= 3391 {
				t.Errorf("%s: the route passes %v, in the Wilderness", l.name, w.Point)
			}
		}
		t.Logf("%s: cost %d, transports %v", l.name, r.Cost, got)
	}
}

// The drain key lies for DemonKeyTicks after the pour; the pour, the doors, the manhole and the sewer are the step's whole walk, and it
// is a fraction of that, on the planner's cost (one tile is about one tick on foot, half as much running) and on the pack's own path
// through the sewer.
func TestTheDrainKeyIsReachedInsideItsClock(t *testing.T) {
	pk, _, _, g := dsData(t)
	pl := webwalk.NewPlanner(pk, g)
	from, to := dsPoint(DemonDrain), dsPoint(DemonSewerKey)
	r := pl.Plan(from, to, webwalk.Options{AllowSpecial: true,
		AvoidZones: webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, Start: &from, Dest: &to})})
	if !r.OK {
		t.Fatalf("no route from the drain to the key: %s", r.Reason)
	}
	// Cost is in ticks of walking, doors and the manhole counted: it must leave half of the key's life unspent at a walk.
	if r.Cost*2 > DemonKeyTicks {
		t.Errorf("the walk costs %d ticks of the key's %d", r.Cost, DemonKeyTicks)
	}
	// The sewer on foot, from the ladder's foot to the key, is a single straight corridor system of fewer than 60 steps.
	res := pk.FindPath(pathfind.NavPoint{X: DemonSewerLanding.X, Z: DemonSewerLanding.Z}, pathfind.NavPoint{X: DemonSewerKey.X, Z: DemonSewerKey.Z})
	if !res.OK || res.Steps() > 60 {
		t.Errorf("landing to key on the pack: %+v (%d steps)", res.OK, res.Steps())
	}
	back := pk.FindPath(pathfind.NavPoint{X: DemonSewerKey.X, Z: DemonSewerKey.Z}, pathfind.NavPoint{X: DemonSewerLanding.X, Z: DemonSewerLanding.Z})
	if !back.OK || back.Steps() > 60 {
		t.Errorf("key to landing on the pack: %+v (%d steps)", back.OK, back.Steps())
	}
	// And the sewer's key stays out of the Wilderness dungeon, which begins at z 9923: the route is within the sewer's rectangle.
	for _, w := range r.Waypoints {
		if w.Point.Level == 0 && w.Point.Z >= 9000 && !InVarrockSewer(tile.Tile{X: w.Point.X, Z: w.Point.Z}) {
			t.Errorf("the route passes %v, outside the Varrock sewer rectangle", w.Point)
		}
	}
	t.Logf("drain to key: cost %d of %d ticks; the sewer on foot %d steps there, %d back", r.Cost, DemonKeyTicks, res.Steps(), back.Steps())
}

// The stone circle is south of the city and the walk to it never goes north, so it is nowhere near the Wilderness line, and the
// circle itself stands on floor whose neighbours the wizards walk (their spawns are floor tiles).
func TestTheStoneCircleIsSouthOfVarrock(t *testing.T) {
	pk, _, _, _ := dsData(t)
	if DelrithSpawn.Z >= PrysinAnchor.Z-50 || DelrithSpawn.Z > 3400 {
		t.Errorf("Delrith %v is not south of the city (Sir Prysin is at %v)", DelrithSpawn, PrysinAnchor)
	}
	for _, p := range []tile.Tile{t3(3223, 3367, 0), t3(3225, 3365, 0), t3(3232, 3367, 0), t3(3225, 3374, 0), t3(3230, 3365, 0)} {
		if !pk.Walkable(p.X, p.Z, p.Level) {
			t.Errorf("a dark wizard's spawn %v is not floor", p)
		}
	}
	// A tile the account can fight Delrith from: one of the tiles that touch his square without a wall between.
	if tiles := pk.InteractTiles(pathfind.NavPoint{X: DelrithSpawn.X, Z: DelrithSpawn.Z}); len(tiles) < 2 {
		t.Errorf("only %d tiles to attack Delrith from", len(tiles))
	}
}
