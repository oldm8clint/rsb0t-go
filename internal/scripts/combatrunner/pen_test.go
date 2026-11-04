package combatrunner

// The trainer's cows rung against the real map (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG; each test skips without
// them): the pasture is a closed pen whose gate is the way in, the rung's stand, entry, beat and every spawn are inside it, and a walk
// in by the gate ends inside. The rung used to stand at the pen's north end with a leash of 8 and count an account as at the rung
// when it was within that leash, which 55 floor tiles outside the pen's north fence were.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The rung this file is about, and the stand and leash it replaced (the figures of the investigation: git history of ladder.go).
func cowsRung(t *testing.T) combatplan.Rung {
	t.Helper()
	r := rungByKey(t, "cows")
	if r.Pen == nil {
		t.Fatal("the cows rung has no pen")
	}
	return r
}

var (
	oldCowsStand = tile.Tile{X: 3256, Z: 3294}
	oldCowsLeash = 8
)

// openPastureGate plays the gate open as the server has it: content's open_gate (general_use/scripts/gates.rs2) deletes both closed leaves and
// adds the open ones a tile and two tiles west of the main one, turned a quarter (angle + 3), parallel to the way through.
func openPastureGate(w *simWorld) {
	w.locChange(3253, 3266, -1, 0, 0)
	w.locChange(3253, 3267, -1, 0, 0)
	w.locChange(3252, 3266, 1552, 0, 3)
	w.locChange(3251, 3266, 1556, 0, 3)
}

func pastureGate() []*simDoor {
	return []*simDoor{
		{id: 1551, x: 3253, z: 3266, open: openPastureGate, group: "gate"},
		{id: 1553, x: 3253, z: 3267, open: openPastureGate, group: "gate"},
	}
}

func penWorld(rd realData, start tile.Tile) *doorWorld {
	return newDoorWorld(rd, start, true)
}

// The rung's stand, the pen's entry and every tile within the entry radius, the beat and the spawns are floor inside the pen; a cow
// at any spawn is a walk over open ground from the stand, the entry and every stop of the beat within view of it.
func TestTheCowsRungsFiguresAreInsideThePen(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	pen := rung.Pen
	g := NewPenGround(rd.pack, *pen)

	for name, tl := range map[string]tile.Tile{"stand": rung.Stand, "entry": pen.Enter} {
		if !rd.pack.Walkable(tl.X, tl.Z, tl.Level) {
			t.Errorf("the %s %v is not floor", name, tl)
		}
		if !g.Inside(tl) {
			t.Errorf("the %s %v is not inside the pen", name, tl)
		}
	}
	floor := 0
	for x := pen.Enter.X - pen.EnterRadius; x <= pen.Enter.X+pen.EnterRadius; x++ {
		for z := pen.Enter.Z - pen.EnterRadius; z <= pen.Enter.Z+pen.EnterRadius; z++ {
			if !rd.pack.Walkable(x, z, 0) {
				continue
			}
			floor++
			if !g.Inside(tile.Tile{X: x, Z: z}) {
				t.Errorf("%d,%d is within %d of the entry and not inside the pen: a walk in could end on the road", x, z, pen.EnterRadius)
			}
		}
	}
	if floor < 20 {
		t.Fatalf("only %d floor tiles within %d of the entry", floor, pen.EnterRadius)
	}
	for i, stop := range pen.Beat {
		if !rd.pack.Walkable(stop.X, stop.Z, 0) || !g.Inside(stop) {
			t.Errorf("stop %d of the beat, %v, is not floor inside the pen", i, stop)
		}
	}
	for _, sp := range rung.Spawns {
		for name, from := range map[string]tile.Tile{"stand": rung.Stand, "entry": pen.Enter} {
			if n := g.Steps(from, sp); n < 0 {
				t.Errorf("the cow spawned at %v is not a walk over open ground from the %s %v", sp, name, from)
			}
		}
	}
	// Every spawn is seen from some stop of the beat and is a walk from it.
	for _, sp := range rung.Spawns {
		seen := false
		for _, stop := range pen.Beat {
			if tile.Chebyshev(stop, sp) <= 15 {
				seen = true
				if n := g.Steps(stop, sp); n < 0 {
					t.Errorf("the cow spawned at %v is not a walk from the stop %v of the beat", sp, stop)
				}
			}
		}
		if !seen {
			t.Errorf("no stop of the beat is within view of the cow spawned at %v", sp)
		}
	}
	// The leash covers the pen: no spawn is picked from outside it.
	for _, sp := range rung.Spawns {
		if d := tile.Chebyshev(rung.Stand, sp); d > rung.Leash {
			t.Errorf("spawn %v is %d from the stand, outside the %d leash", sp, d, rung.Leash)
		}
	}
}

// The pen is closed: from the road no cow is a walk away over the shut pack, and with the gate open every one is. The figures of the
// reproduction: the old rung called 55 floor tiles at the pen's north fence "at the rung".
func TestThePenIsClosedAndTheRoadIsOutsideIt(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	g := NewPenGround(rd.pack, *rung.Pen)

	// The old leash: floor tiles within it of the old stand, and which of them are outside the pen.
	var outsideOld []tile.Tile
	for x := oldCowsStand.X - oldCowsLeash; x <= oldCowsStand.X+oldCowsLeash; x++ {
		for z := oldCowsStand.Z - oldCowsLeash; z <= oldCowsStand.Z+oldCowsLeash; z++ {
			tl := tile.Tile{X: x, Z: z}
			if rd.pack.Walkable(x, z, 0) && !g.Inside(tl) {
				outsideOld = append(outsideOld, tl)
			}
		}
	}
	if len(outsideOld) == 0 {
		t.Fatal("every floor tile within the old leash was inside the pen: the reproduction does not hold")
	}
	t.Logf("%d floor tiles within the old leash of %d at %v are outside the pen, so the old rung counted an account on them as at the rung", len(outsideOld), oldCowsLeash, oldCowsStand)
	for _, tl := range outsideOld {
		if tl.Z < 3299 {
			t.Errorf("%v is outside the pen and south of its north fence: the old rung's blind strip is z 3299 and up", tl)
		}
		for _, sp := range rung.Spawns {
			if tile.Chebyshev(tl, sp) <= 24 {
				if n := g.Steps(tl, sp); n >= 0 {
					t.Errorf("the cow spawned at %v is %d steps from %v over the shut pack: the fence is not closed", sp, n, tl)
				}
			}
		}
	}

	// The new leash is wider still, and so is the road within it: the floor tiles within the rung's leash that are outside the pen.
	var road []tile.Tile
	for x := rung.Stand.X - rung.Leash; x <= rung.Stand.X+rung.Leash; x++ {
		for z := rung.Stand.Z - rung.Leash; z <= rung.Stand.Z+rung.Leash; z++ {
			tl := tile.Tile{X: x, Z: z}
			if rd.pack.Walkable(x, z, 0) && !g.Inside(tl) {
				road = append(road, tl)
			}
		}
	}
	if len(road) < 100 {
		t.Fatalf("only %d floor tiles within the new leash are outside the pen", len(road))
	}
	t.Logf("%d floor tiles within the new leash %d of %v are outside the pen: the rung is `at the rung` only inside it", len(road), rung.Leash, rung.Stand)
	for _, tl := range road {
		for _, sp := range rung.Spawns {
			if tile.Chebyshev(tl, sp) <= PenWindow-2 {
				if n := g.Steps(tl, sp); n >= 0 {
					t.Errorf("the cow spawned at %v is %d steps from the road tile %v over the shut pack", sp, n, tl)
				}
			}
		}
	}

	// With the gate's walls gone, the stand is a walk from the road beside it, and every spawn within the window is.
	var edits []pathfind.WallEdit
	for _, gate := range rung.Pen.Gates {
		found := false
		for _, l := range rd.locs.At(gate.X, gate.Z, 0) {
			info, ok := rd.cfg.Loc(l.ID)
			if !ok || info.Name != "Gate" || !info.BlockWalk || l.Shape != 0 {
				continue
			}
			edits = append(edits, pathfind.WallEdit{X: l.X, Z: l.Z, Level: l.Level, Shape: l.Shape, Angle: l.Angle})
			found = true
		}
		if !found {
			t.Errorf("no closed Gate at %v in the loc index", gate)
		}
	}
	open := NewPenGround(rd.pack.WithWalls(edits), *rung.Pen)
	from := tile.Tile{X: 3252, Z: 3267}
	if g.Inside(from) || !open.Inside(from) {
		t.Fatalf("the road tile beside the gate: inside %v on the shut pack, %v on the open one", g.Inside(from), open.Inside(from))
	}
	for _, sp := range rung.Spawns {
		if tile.Chebyshev(from, sp) <= PenWindow-2 {
			if n := open.Steps(from, sp); n < 0 {
				t.Errorf("with the gate open the cow spawned at %v is not a walk from the road beside it", sp)
			}
		}
	}
}

// Inside is the question "is the entry a walk away within the flood window", and the window is wide enough for it: it agrees, tile by
// tile over the whole pasture and the ground round it, with the pen as the entry's own unbounded flood draws it (the gate is shut
// in the pack, so that flood is exactly the pen's floor).
func TestInsideAgreesWithThePensWholeFloor(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	pen := rung.Pen
	g := NewPenGround(rd.pack, *pen)
	floor := rd.pack.ReachFrom(pathfind.NavPoint{X: pen.Enter.X, Z: pen.Enter.Z}, 80, nil)
	inPen, outside, wrong := 0, 0, 0
	for x := 3200; x <= 3300; x++ {
		for z := 3215; z <= 3330; z++ {
			if !rd.pack.Walkable(x, z, 0) {
				continue
			}
			_, want := floor.Steps(x, z, 0)
			if got := g.Inside(tile.Tile{X: x, Z: z}); got != want {
				wrong++
				if wrong <= 10 {
					t.Errorf("%d,%d: Inside = %v, the pen's floor says %v", x, z, got, want)
				}
			}
			if want {
				inPen++
			} else {
				outside++
			}
		}
	}
	if inPen < 500 || outside < 2000 {
		t.Errorf("fixture: %d floor tiles in the pen and %d outside it", inPen, outside)
	}
	t.Logf("%d floor tiles in the pen, %d round it, %d disagreements", inPen, outside, wrong)
	// The pen's floor is the pasture and nothing else: a closed area of 780 tiles bounded by the fences.
	minX, maxX, minZ, maxZ := 1<<30, 0, 1<<30, 0
	floor.Each(func(x, z, level, steps int) bool {
		minX, maxX, minZ, maxZ = min(minX, x), max(maxX, x), min(minZ, z), max(maxZ, z)
		return true
	})
	t.Logf("the pen's floor spans x %d to %d, z %d to %d (%d tiles)", minX, maxX, minZ, maxZ, floor.Size())
	if minX < 3240 || maxX > 3266 || minZ < 3254 || maxZ > 3298 || floor.Size() != inPen {
		t.Errorf("the pen's floor spans x %d to %d, z %d to %d with %d tiles: it is not the closed pasture (%d floor tiles read inside)", minX, maxX, minZ, maxZ, floor.Size(), inPen)
	}
}

// A walk in from where each trip begins ends inside: the first tile within the entry radius of every planned route to the entry, from
// the rung's own bank and from five places an account is thrown to or sent from, is inside the pen, and the route crosses the fence.
func TestEveryWalkToTheEntryEndsInsideThePen(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	pen := rung.Pen
	g := NewPenGround(rd.pack, *pen)
	// A bank is a booth: the tiles it is used from include the keeper's side of the counter, which no walk leaves, so the first of
	// them with a route is where the account banks.
	origins := map[string][]tile.Tile{
		"Lumbridge castle":            {{X: 3222, Z: 3218}},
		"Draynor bank":                {{X: 3092, Z: 3243}},
		"Al Kharid":                   {{X: 3270, Z: 3167}},
		"Varrock west":                {{X: 3185, Z: 3436}},
		"Falador":                     {{X: 2965, Z: 3380}},
		"the road at the pen's north": {{X: 3256, Z: 3300}},
	}
	for _, s := range rd.pack.InteractTiles(navPoint(rung.Bank)) {
		origins["the rung's bank (Varrock east)"] = append(origins["the rung's bank (Varrock east)"], tile.Tile{X: s.X, Z: s.Z, Level: s.Level})
	}
	for name, starts := range origins {
		var route webwalk.Route
		for _, o := range starts {
			if route = rd.planner.Plan(webwalk.Point{X: o.X, Z: o.Z}, webwalk.Point{X: pen.Enter.X, Z: pen.Enter.Z}, webwalk.Options{MaxExpansions: 800000}); route.OK {
				break
			}
		}
		if !route.OK {
			t.Errorf("%s: no route to the entry from any of %v", name, starts)
			continue
		}
		var first tile.Tile
		found, crossed := false, false
		for _, p := range routeTiles(route) {
			tl := tileOf(p)
			if tl.X == 3253 && (tl.Z == 3266 || tl.Z == 3267) {
				crossed = true
			}
			if !found && tile.Chebyshev(tl, pen.Enter) <= pen.EnterRadius {
				first, found = tl, true
			}
		}
		switch {
		case !found:
			t.Errorf("%s: the route never comes within %d of the entry", name, pen.EnterRadius)
		case !g.Inside(first):
			t.Errorf("%s: the walk would stop at %v, within %d of the entry and outside the pen", name, first, pen.EnterRadius)
		case !crossed:
			t.Errorf("%s: the route into the pen does not cross at the gate's two rows", name)
		}
	}
}

// EnterPen, played on the real walker over a simulated server: from the road beside the gate, from the road at the pen's north fence
// and from the far side of the river, the account is walked in, the gate is operated, and it ends inside.
func TestEnterPenWalksInByTheGate(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	for name, start := range map[string]tile.Tile{
		"the road beside the gate":   {X: 3252, Z: 3267},
		"the road at the north":      {X: 3256, Z: 3300},
		"the road south of the gate": {X: 3250, Z: 3258},
	} {
		dw := penWorld(rd, start)
		dw.c.doors = pastureGate()
		g := NewPenGround(rd.pack, *rung.Pen)
		if g.Inside(start) {
			t.Fatalf("%s: fixture: %v is inside the pen", name, start)
		}
		if !EnterPen(dw.d, g, 2*time.Minute) {
			t.Fatalf("%s: EnterPen did not get in; log:\n%s", name, dw.logs())
		}
		x, z, level, _ := dw.w.st.Tile()
		if !g.Inside(tile.Tile{X: x, Z: z, Level: level}) {
			t.Errorf("%s: the account ended at %d,%d outside the pen", name, x, z)
		}
		if tile.Chebyshev(tile.Tile{X: x, Z: z}, rung.Pen.Enter) > rung.Pen.EnterRadius {
			t.Errorf("%s: the account ended at %d,%d, not within %d of the entry %v", name, x, z, rung.Pen.EnterRadius, rung.Pen.Enter)
		}
		if !dw.c.doors[0].opened || !dw.c.doors[1].opened || dw.c.ops == 0 {
			t.Errorf("%s: the gate was not operated (opened=%v,%v ops=%d)", name, dw.c.doors[0].opened, dw.c.doors[1].opened, dw.c.ops)
		}
		if !strings.Contains(dw.logs(), "outside the pen: walking in by the gate") {
			t.Errorf("%s: the walk in is not in the log:\n%s", name, dw.logs())
		}
	}
}

// An account already inside is not moved, and a gate that never opens ends the walk, false, in bounded time and with the reason in the
// log: nothing here spins.
func TestEnterPenLeavesAnInsideAccountAloneAndEndsAtAGateThatWillNotOpen(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)

	in := penWorld(rd, tile.Tile{X: 3257, Z: 3280})
	in.c.doors = pastureGate()
	g := NewPenGround(rd.pack, *rung.Pen)
	if !EnterPen(in.d, g, time.Minute) || in.c.ops != 0 || strings.Contains(in.logs(), "walking in") {
		t.Errorf("an account inside the pen was walked (ops=%d):\n%s", in.c.ops, in.logs())
	}

	stuck := penWorld(rd, tile.Tile{X: 3252, Z: 3267})
	stuck.c.doors = []*simDoor{{id: 1551, x: 3253, z: 3266}, {id: 1553, x: 3253, z: 3267}} // no open func: the server ignores the click
	start := stuck.c.now
	if EnterPen(stuck.d, NewPenGround(rd.pack, *rung.Pen), 30*time.Second) {
		t.Fatal("EnterPen reported being inside the pen through a gate that never opened")
	}
	if took := stuck.c.now.Sub(start); took > 4*time.Minute {
		t.Errorf("the failed walk took %s of (simulated) time", took)
	}
	if !strings.Contains(stuck.logs(), "outside the pen: walking in by the gate") {
		t.Errorf("the attempt is not in the log:\n%s", stuck.logs())
	}
}

// Steps ranks by the walk, not the crow: a cow across the fence from the account is nearer by distance than one in the pen and is no
// walk at all, and a cow on the far side of a long inner stretch is dearer than its distance.
func TestStepsIsTheWalkOverOpenGroundAndNotTheDistance(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	g := NewPenGround(rd.pack, *rung.Pen)

	// From inside, just east of the west fence, the road's tile one step west is nearer than any cow and no walk.
	inside := tile.Tile{X: 3254, Z: 3262}
	across := tile.Tile{X: 3250, Z: 3262}
	if !g.Inside(inside) || g.Inside(across) {
		t.Fatalf("fixture: %v inside=%v, %v inside=%v", inside, g.Inside(inside), across, g.Inside(across))
	}
	if n := g.Steps(inside, across); n >= 0 {
		t.Errorf("a monster across the fence, %d tiles away, is %d steps by the walk", tile.Chebyshev(inside, across), n)
	}
	if n := g.Steps(inside, tile.Tile{X: 3258, Z: 3260}); n < 0 || n > 6 {
		t.Errorf("the cow spawned at 3258,3260 is %d steps from %v", n, inside)
	}
	// Another level is no walk, and a pack-less ground answers by distance and always inside.
	if n := g.Steps(inside, tile.Tile{X: 3256, Z: 3262, Level: 1}); n != -1 {
		t.Errorf("a monster on another level is %d steps away", n)
	}
	bare := NewPenGround(nil, *rung.Pen)
	if !bare.Inside(across) || bare.Steps(inside, across) != 4 {
		t.Errorf("a ground with no pack: inside %v, steps %d; want inside and the distance 4", bare.Inside(across), bare.Steps(inside, across))
	}
}

// The flood is reused from one tile for PenFloodReuse and redone after it or from another tile.
func TestPenGroundReusesItsFloodOnlyBriefly(t *testing.T) {
	rd := loadReal(t)
	rung := cowsRung(t)
	g := NewPenGround(rd.pack, *rung.Pen)
	clock := time.Unix(1_000_000, 0)
	g.now = func() time.Time { return clock }
	here := tile.Tile{X: 3257, Z: 3270}
	first := g.flood(here)
	if first == nil || g.flood(here) != first {
		t.Fatal("the flood was not reused from the same tile")
	}
	if g.flood(tile.Tile{X: 3258, Z: 3270}) == first {
		t.Error("the flood was reused from another tile")
	}
	again := g.flood(here)
	clock = clock.Add(PenFloodReuse + time.Millisecond)
	if g.flood(here) == again {
		t.Error("the flood was reused past PenFloodReuse")
	}
}
