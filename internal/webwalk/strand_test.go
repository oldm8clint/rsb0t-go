package webwalk

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/randomevents"
)

// strandWorld is a fake world over grid g with graph edges, for the strand-site tests below.
func strandWorld(t *testing.T, g *grid, edges []Edge, me Point) (*fakeWorld, *Walker) {
	t.Helper()
	f := newFakeWorld(t, g, me)
	w := NewWalker(NewPlanner(g, &Graph{Edges: edges}), f)
	w.Log = func(s string) { t.Log(s) }
	return f, w
}

// hopKinds lists the kinds of a route's hops, in order.
func hopKinds(r Route) string {
	var k []string
	for _, h := range r.Hops() {
		k = append(k, string(h.Kind))
	}
	return strings.Join(k, ",")
}

func inBox(p Point, minX, maxX, minZ, maxZ int) bool {
	return p.X >= minX && p.X <= maxX && p.Z >= minZ && p.Z <= maxZ
}

// inCastleBox is Rogues' Castle's compound written out here, independently of the catalogue that holds it
// (randomevents.roguesCastleAreas, doorescape.CastleCompound): x 3275-3298 and z 3925-3939 on the ground floor,
// z 3925-3941 on the three floors above.
func inCastleBox(p Point) bool {
	top := 3941
	if p.Level == 0 {
		top = 3939
	}
	return p.X >= 3275 && p.X <= 3298 && p.Z >= 3925 && p.Z <= top
}

// edgeInCastle asks withoutCastleEdges' question again: does the edge start, end or work a loc inside the walls.
func edgeInCastle(e Edge) bool {
	return inCastleBox(e.From) || inCastleBox(e.To) || inCastleBox(Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level})
}

// Rogues' Castle's doors and staircases are not in the walker's graph. The baked graph has thirty-two edges inside
// the walls (ten straight doors, both ways, and twelve staircase hops: the map's two diagonal doors, the main one
// among them, were never in it); Default() has none of them and still has everything else. The way out of the
// castle is the Guardian's plan, and the Walker's crossings of those doors failed live (6 October 2026: 317
// "did not step through Door" lines in one log, 20 of them at the main door), each repath a plan of 240,000 nodes.
func TestTheCastleIsNotInTheWalkersGraph(t *testing.T) {
	raw, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	doors, stairs, others := 0, 0, 0
	for _, e := range raw.Edges {
		if !edgeInCastle(e) {
			continue
		}
		switch e.Kind {
		case KindDoor:
			doors++
		case KindStair:
			stairs++
		default:
			others++
		}
	}
	if doors != 20 || stairs != 12 || others != 0 {
		t.Fatalf("the baked graph has %d castle door edges, %d staircase edges and %d others, want 20, 12 and 0: this test no longer proves the pruning", doors, stairs, others)
	}

	kept := withoutCastleEdges(raw.Edges)
	if len(kept) != len(raw.Edges)-32 {
		t.Errorf("withoutCastleEdges kept %d of %d edges, want %d: it dropped something outside the castle or missed something in it", len(kept), len(raw.Edges), len(raw.Edges)-32)
	}
	for _, e := range kept {
		if edgeInCastle(e) {
			t.Errorf("withoutCastleEdges kept the castle edge %s", e.String())
		}
	}

	def, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range def.Edges {
		if edgeInCastle(e) {
			t.Errorf("Default() still has the castle edge %s", e.String())
		}
	}
	for _, x := range ExtraEdges {
		if edgeInCastle(x.Edge) {
			t.Errorf("ExtraEdges has the castle edge %s (%s): the castle is the Guardian's", x.Edge.String(), x.Why)
		}
	}
}

// The pruning follows the castle's walls floor by floor: the ground floor ends at z 3939 (the main door's doorway),
// the floors above go two tiles further north; an edge counts when its stand, its landing or its loc is inside.
func TestWithoutCastleEdgesFollowsTheWallsOfEachFloor(t *testing.T) {
	door := func(from, to, loc Point) Edge {
		return Edge{From: from, To: to, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: loc.X, LocZ: loc.Z}
	}
	p := func(x, z, level int) Point { return Point{X: x, Z: z, Level: level} }
	for _, c := range []struct {
		name string
		e    Edge
		drop bool
	}{
		{"a door inside", door(p(3276, 3933, 0), p(3276, 3934, 0), p(3276, 3933, 0)), true},
		{"a hop from open ground into the main doorway", door(p(3279, 3940, 0), p(3279, 3939, 0), p(3279, 3939, 0)), true},
		{"a hop from the doorway out", door(p(3279, 3939, 0), p(3279, 3940, 0), p(3279, 3939, 0)), true},
		{"a loc inside whose two ends are outside", door(p(3274, 3930, 0), p(3299, 3930, 0), p(3280, 3930, 0)), true},
		{"the stairs, up from the ground floor", Edge{From: p(3282, 3936, 0), To: p(3279, 3936, 1), Kind: KindStair, Cost: 6, LocID: 1738, LocX: 3280, LocZ: 3936}, true},
		{"the roof's stairs", Edge{From: p(3281, 3936, 3), To: p(3279, 3936, 2), Kind: KindStair, Cost: 6, LocID: 1740, LocX: 3280, LocZ: 3936}, true},
		{"a door beside the west wall", door(p(3270, 3930, 0), p(3270, 3931, 0), p(3270, 3930, 0)), false},
		{"a door beside the east wall", door(p(3299, 3930, 0), p(3300, 3930, 0), p(3299, 3930, 0)), false},
		{"a door south of the walls", door(p(3280, 3923, 0), p(3280, 3924, 0), p(3280, 3924, 0)), false},
		{"the ground floor north of the doorway", door(p(3277, 3940, 0), p(3277, 3941, 0), p(3277, 3940, 0)), false},
		{"the same tiles on the first floor", door(p(3277, 3940, 1), p(3277, 3941, 1), p(3277, 3940, 1)), true},
		{"a door in another town", door(p(3207, 3217, 0), p(3208, 3217, 0), p(3207, 3217, 0)), false},
	} {
		got := withoutCastleEdges([]Edge{c.e})
		if dropped := len(got) == 0; dropped != c.drop {
			t.Errorf("%s: dropped = %v, want %v", c.name, dropped, c.drop)
		}
	}
}

// A walk that starts on a tile of the castle's rooms, doorways or floors is refused at once and never planned: the
// Guardian plans the way out of those tiles (internal/doorescape), the graph has no castle edge to plan with, and a
// walk of a script's own used to try anyway, fail on the diagonal door and try again. The refusal waits
// strandRefuseWait, once, so the caller's loop does not spin on it, sends no click and logs only that it began
// (beginWalk's "standing in ..." line) and why it stopped; a plan would have logged its route or its failure.
func TestAWalkFromInsideTheCastleIsRefusedAtOnceAndNeverPlanned(t *testing.T) {
	dest := Point{X: 3159, Z: 3950, Level: 0}
	for _, me := range []Point{
		{X: 3282, Z: 3936, Level: 3}, {X: 3281, Z: 3937, Level: 3}, // the roof
		{X: 3282, Z: 3936, Level: 2}, {X: 3279, Z: 3936, Level: 1}, // the floors below
		{X: 3282, Z: 3936, Level: 0}, {X: 3280, Z: 3938, Level: 0}, // the stairs room
		{X: 3276, Z: 3933, Level: 0}, // the west closet's north door
	} {
		if _, planned := randomevents.StrandPlanned(me.X, me.Z, me.Level); !planned {
			t.Fatalf("%s is not a tile the Guardian plans: this test picked a tile outside the castle's rooms", me)
		}
		g := newGrid().fill(me.X-2, me.Z-2, me.X+2, me.Z+2, me.Level)
		g.fill(3158, 3949, 3160, 3951, 0)
		f, w := strandWorld(t, g, nil, me)
		var logs []string
		w.Log = func(s string) { logs = append(logs, s) }
		start := f.now
		res := w.WalkTo(dest, WalkOptions{})
		if res.Outcome != OutcomeUnreachable || !strings.Contains(res.Reason, "stranded in Rogues' Castle") || !strings.Contains(res.Reason, "escape plan owns the way out") {
			t.Errorf("from %s: %+v, want unreachable, naming the castle and the Guardian's plan", me, res)
		}
		if took := f.now.Sub(start); took != strandRefuseWait || f.delays != 1 {
			t.Errorf("from %s the refusal took %v in %d delays, want one wait of %v", me, took, f.delays, strandRefuseWait)
		}
		if len(logs) != 2 || !strings.HasPrefix(logs[0], "walk from ") || !strings.Contains(logs[1], "escape plan owns the way out") || f.walks != 0 || len(f.ops) != 0 {
			t.Errorf("from %s the walk planned or moved (%d log lines, %d walks, %d ops): %v", me, len(logs), f.walks, len(f.ops), logs)
		}
	}
}

// The rule is for the castle's rooms and floors only: an account on the open ground inside its walls (the
// courtyards) or beside it walks the way it always did, and arrives.
func TestAWalkFromOpenGroundByTheCastleIsNotRefused(t *testing.T) {
	dest := Point{X: 3279, Z: 3950, Level: 0}
	for _, me := range []Point{
		{X: 3276, Z: 3937, Level: 0}, // the west courtyard, inside the compound's rectangle
		{X: 3279, Z: 3945, Level: 0}, // north of the main door, outside it
	} {
		if site, planned := randomevents.StrandPlanned(me.X, me.Z, me.Level); planned {
			t.Fatalf("%s is a tile the Guardian plans (%s): this test picked a tile inside the castle's rooms", me, site)
		}
		g := newGrid().fill(3268, 3935, 3290, 3952, 0)
		_, w := strandWorld(t, g, nil, me)
		if res := w.WalkTo(dest, WalkOptions{}); res.Outcome != OutcomeArrived {
			t.Errorf("from open ground at %s: %+v, want arrived", me, res)
		}
	}
}

// The King Black Dragon's lair, fake-gridded the same way: the lair, its lever's pocket in the
// Wilderness dungeon, and the surface at the top of that pocket's ladder (an edge that IS in graph.json).
func lairWorld(t *testing.T, me Point) (*fakeWorld, *Walker) {
	g := newGrid()
	g.fill(2708, 9801, 2729, 9830, 0)   // the lair
	g.fill(3065, 10253, 3070, 10260, 0) // the pocket the lever lands in
	g.fill(3005, 3840, 3030, 3860, 0)   // the surface above the ladder
	def, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	var edges []Edge
	for _, e := range def.Edges {
		lair := inBox(e.From, 2700, 2735, 9795, 9835) && e.From.Level == 0
		pocket := inBox(e.From, 3064, 3071, 10252, 10261) && e.From.Level == 0
		if lair || pocket {
			edges = append(edges, e)
		}
	}
	return strandWorld(t, g, edges, me)
}

// The lair's only exit is its lever, which lands in the Wilderness dungeon: a hop INTO the danger zone
// from outside it, which the zone rule refuses outright. The pocket rule is what lets the walker plan it.
func TestKingBlackDragonLairPlansOutByTheLeverAndTheLadder(t *testing.T) {
	arrival := Point{X: 2718, Z: 9816, Level: 0}
	surface := Point{X: 3016, Z: 3852, Level: 0}

	_, w := lairWorld(t, arrival)
	r := w.Plan(surface, WalkOptions{})
	if !r.OK {
		t.Fatalf("no route out of the lair: %s", r.Reason)
	}
	if got := hopKinds(r); got != "portal,dungeon" {
		t.Errorf("hops = %s, want the lever then the ladder", got)
	}
	if hops := r.Hops(); hops[0].LocID != 1817 || hops[0].Action != "Pull" || hops[1].LocID != 1766 {
		t.Errorf("hops = %v, want Pull on lever 1817 then the 1766 ladder", hops)
	}
	// The lever's landing is in the Wilderness dungeon's zone rectangle, so only the pocket rule can have
	// made this plan: TestResolveZonesDropsTheAutomaticZonesInAPocketOnly and the Mage Bank control below
	// show the same layout is unreachable from a start that is not a catalogued pocket.
	if wild, _ := KnownDangerZoneByID("wilderness"); !InZones(Point{X: 3067, Z: 10254, Level: 0}, wild.Rects) {
		t.Fatal("the lever's landing is not in the Wilderness zone; this test no longer proves the pocket rule")
	}
}

// resolveZones itself: from a pocket start the automatic zones are off; from anywhere else they are on.
func TestResolveZonesDropsTheAutomaticZonesInAPocketOnly(t *testing.T) {
	dest := Point{X: 2562, Z: 3311, Level: 0}
	for _, c := range []struct {
		name string
		me   Point
		want bool // automatic zones resolved
	}{
		{"the Mage Bank", Point{X: 2543, Z: 4713, Level: 0}, false},
		{"the dragon's lair", Point{X: 2718, Z: 9816, Level: 0}, false},
		{"the castle roof", Point{X: 3282, Z: 3936, Level: 3}, false},
		{"Lumbridge", Point{X: 3222, Z: 3218, Level: 0}, true},
		{"the Ardougne market", Point{X: 2668, Z: 3312, Level: 0}, true},
	} {
		g := newGrid().fill(c.me.X-2, c.me.Z-2, c.me.X+2, c.me.Z+2, c.me.Level)
		_, w := strandWorld(t, g, nil, c.me)
		zones, _ := w.resolveZones(dest, WalkOptions{})
		hasWild := false
		for _, r := range zones {
			if r.MinX == 2944 && r.MaxX == 3391 {
				hasWild = true
			}
		}
		if hasWild != c.want {
			t.Errorf("%s: Wilderness zone resolved = %v, want %v", c.name, hasWild, c.want)
		}
	}
}

// From the Mage Bank the Keep lever is the way home, and it is only plannable because the pocket rule
// lets the ladder up into the Wilderness through; from a pocket that is not catalogued it is not.
func TestMageBankPlansHomeByTheKeepLever(t *testing.T) {
	build := func(me Point) (*Walker, *fakeWorld) {
		g := newGrid()
		g.fill(me.X-2, me.Z-2, me.X+2, me.Z+2, 0) // the cellar
		g.fill(3089, 3956, 3093, 3959, 0)         // the ruined house
		g.fill(3093, 3957, 3154, 3957, 0)         // the walk east
		g.fill(3153, 3923, 3154, 3957, 0)         // down to the lever
		g.fill(2560, 3310, 2564, 3313, 0)         // East Ardougne
		ladder := Edge{
			From: Point{X: me.X, Z: me.Z, Level: 0}, To: Point{X: 3091, Z: 3958, Level: 0},
			Kind: KindDungeon, Cost: 8, LocName: "Ladder", Action: "Climb-up", LocID: 2872, OpenLocID: -1,
			LocX: me.X, LocZ: me.Z - 1, LandsOnTile: true,
		}
		var lever Edge
		for _, x := range ExtraEdges {
			if x.Edge.LocID == 1815 {
				lever = x.Edge
			}
		}
		f, w := strandWorld(t, g, []Edge{ladder, lever}, me)
		return w, f
	}
	dest := Point{X: 2562, Z: 3311, Level: 0}

	w, _ := build(Point{X: 2543, Z: 4713, Level: 0}) // the Mage Bank
	r := w.Plan(dest, WalkOptions{})
	if !r.OK {
		t.Fatalf("no route from the Mage Bank home: %s", r.Reason)
	}
	if got := hopKinds(r); got != "dungeon,portal" {
		t.Errorf("hops = %s, want the ladder then the Keep lever", got)
	}
	if hops := r.Hops(); hops[1].LocID != 1815 || hops[1].Action != "Pull" {
		t.Errorf("last hop = %v, want Pull on the Keep lever 1815", hops[1])
	}

	other, _ := build(Point{X: 2300, Z: 4713, Level: 0}) // the same cellar nowhere catalogued
	if r := other.Plan(dest, WalkOptions{}); r.OK {
		t.Errorf("an uncatalogued pocket planned a hop into the Wilderness zone from outside it: %s", r.String())
	}
}

// A walk that cannot plan from a strand site with exit steps says so at once; the same fault anywhere else still
// takes the walker's whole ladder (scene walk, unstick, backoff, verify), the way it always did. (The castle does
// not come this way any more: the Walker refuses its rooms before planning, which the tests above cover; the
// dragon's lair stands for every other site whose way out is a list of clicks.)
func TestWalkFromAStrandSiteFailsFastWhileOthersRunTheLadder(t *testing.T) {
	fail := func(me Point) (Result, time.Duration, int) {
		g := newGrid().fill(me.X-2, me.Z-2, me.X+2, me.Z+2, me.Level)
		g.fill(3158, 3949, 3160, 3951, 0)
		f, w := strandWorld(t, g, nil, me)
		start := f.now
		res := w.WalkTo(Point{X: 3159, Z: 3950, Level: 0}, WalkOptions{})
		return res, f.now.Sub(start), f.delays
	}
	res, took, delays := fail(Point{X: 2718, Z: 9816, Level: 0}) // the dragon's lair
	if res.Outcome != OutcomeUnreachable || !strings.Contains(res.Reason, "stranded in King Black Dragon's lair") {
		t.Fatalf("from the lair: %+v, want unreachable naming the lair", res)
	}
	if took > 5*time.Second || delays > 6 {
		t.Errorf("from the lair the failure took %v and %d delays; it should be immediate", took, delays)
	}

	res, ordinary, _ := fail(Point{X: 3000, Z: 3000, Level: 3}) // a sealed pocket nowhere catalogued
	if res.Outcome != OutcomeUnreachable || strings.Contains(res.Reason, "stranded in") {
		t.Fatalf("from an ordinary pocket: %+v, want the plain ladder's unreachable", res)
	}
	if ordinary < 10*time.Second {
		t.Errorf("an ordinary pocket gave up after %v: the ladder was skipped for places it was never meant to be skipped for", ordinary)
	}
}

// The extra edges are exits only, and well-formed.
func TestExtraEdgesAreExitsAndWellFormed(t *testing.T) {
	if len(ExtraEdges) == 0 {
		t.Fatal("no extra edges")
	}
	for _, x := range ExtraEdges {
		e := x.Edge
		if x.Why == "" || e.LocName == "" || e.Action == "" || e.LocID <= 0 || e.Cost <= 0 || e.From == e.To {
			t.Errorf("malformed extra edge %s (%q)", e.String(), x.Why)
		}
		if !e.HasLoc() {
			t.Errorf("%s: an extra edge must be a loc hop the Walker can run", e.String())
		}
		switch e.Kind {
		case KindDoor:
			if e.ChangesLevel() || e.LandsOnTile {
				t.Errorf("%s: a door edge cannot change level or land on a tile", e.String())
			}
		case KindPortal:
			if !e.LandsOnTile || !e.LongRange() {
				t.Errorf("%s: a lever hop lands on a tile and spans more tiles than it costs", e.String())
			}
		default:
			t.Errorf("%s: unexpected kind %s", e.String(), e.Kind)
		}
		// Nothing leads INTO a lair or a Wilderness pocket.
		for _, banned := range []struct{ minX, maxX, minZ, maxZ int }{{2700, 2735, 9795, 9835}, {3064, 3071, 10252, 10261}} {
			if inBox(e.To, banned.minX, banned.maxX, banned.minZ, banned.maxZ) && e.To.Level == 0 && !inBox(e.From, banned.minX, banned.maxX, banned.minZ, banned.maxZ) && e.Kind != KindPortal {
				t.Errorf("%s leads into a lair or pocket", e.String())
			}
		}
	}
	// The castle's doors were extra edges once; they are the Guardian's now (TestTheCastleIsNotInTheWalkersGraph).
}

// A second walk from the strand site where one just failed is refused without a plan, and waits so
// its caller's loop does not spin: live on 9d28c8f7 (6 Oct 2026, 01:32) a mass summon left 356 accounts
// in Rogues' Castle and their scripts planned 4,245 routes of about 280,000 nodes each in seven
// minutes. Once strandRetryEvery has passed the walk is planned again. The castle's rooms are now refused
// before any plan; the dragon's lair is the site this rule still serves in the test.
func TestAWalkFromAStrandSiteIsNotPlannedAgainForAWhile(t *testing.T) {
	roof := Point{X: 2718, Z: 9816, Level: 0} // the lair's arrival tile
	g := newGrid().fill(roof.X-2, roof.Z-2, roof.X+2, roof.Z+2, roof.Level)
	g.fill(3158, 3949, 3160, 3951, 0)
	f, w := strandWorld(t, g, nil, roof)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	plans := func() int {
		n := 0
		for _, l := range logs {
			if strings.Contains(l, "baked walk") {
				n++
			}
		}
		return n
	}
	dest := Point{X: 3159, Z: 3950, Level: 0}
	if res := w.WalkTo(dest, WalkOptions{}); res.Outcome != OutcomeUnreachable || !strings.Contains(res.Reason, "stranded in King Black Dragon's lair") {
		t.Fatalf("first walk from the lair: %+v", res)
	}
	first := plans()
	if first == 0 {
		t.Fatalf("the first walk did not plan: %v", logs)
	}

	start := f.now
	res := w.WalkTo(dest, WalkOptions{})
	if res.Outcome != OutcomeUnreachable || !strings.Contains(res.Reason, "a walk from here failed") {
		t.Fatalf("second walk: %+v, want refused as stranded without a plan", res)
	}
	if plans() != first {
		t.Fatalf("the second walk planned again: %v", logs)
	}
	if waited := f.now.Sub(start); waited < strandRefuseWait {
		t.Errorf("the refusal waited %v, want at least %v", waited, strandRefuseWait)
	}

	f.now = f.now.Add(strandRetryEvery)
	w.WalkTo(dest, WalkOptions{})
	if plans() == first {
		t.Fatalf("%v later the walk was still not planned: %v", strandRetryEvery, logs)
	}
}
