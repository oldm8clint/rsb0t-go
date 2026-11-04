package runecraftrunner

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The carrier's whole road, planned the way the Walker plans it (ships and dialogue crossings allowed, the corridor's spider
// boxes as no-go zones) on the real collision pack: from the mule's post at Catherby by Captain Barnaby's ship to Jiminua's store,
// from the store to the Nature ruins' exit tile, and home. A planned route is not a walked one, but a plan that does not exist (a
// box across the pier, a store the walker cannot reach) would end every shift at its first step.
func TestTheCarriersRoadPlansOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	var zones []webwalk.Rect
	zones = append(zones, KaramjaRects()...)
	plan := func(from, to tile.Tile, avoid bool) webwalk.Route {
		opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
		if avoid {
			opt.AvoidZones = zones
		}
		return planner.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}, opt)
	}
	post := tile.Tile{X: 2809, Z: 3441}
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		max      int
		ship     bool
	}{
		{"the post to Jiminua's store", post, runecraftplan.StoreStand, 480, true},
		{"the store to the ruins' exit tile", runecraftplan.StoreStand, runecraftplan.Nature.Exit, 135, false},
		{"the exit tile to the store", runecraftplan.Nature.Exit, runecraftplan.StoreStand, 135, false},
		{"the ruins' exit tile home to the post", runecraftplan.Nature.Exit, post, 700, true},
	} {
		route := plan(c.from, c.to, true)
		if !route.OK || route.Cost > c.max {
			t.Errorf("%s: ok %v cost %d (%s), want at most %d", c.name, route.OK, route.Cost, route.Reason, c.max)
			continue
		}
		var hops []string
		ship := false
		for _, e := range route.Hops() {
			hops = append(hops, fmt.Sprintf("%s:%s:%s %d,%d -> %d,%d", e.Kind, e.LocName, e.Action, e.From.X, e.From.Z, e.To.X, e.To.Z))
			if e.Kind == webwalk.KindShip {
				ship = true
			}
		}
		if ship != c.ship {
			t.Errorf("%s: ship %v, want %v (%v)", c.name, ship, c.ship, hops)
		}
		plain := plan(c.from, c.to, false)
		t.Logf("%s: cost %d around the spiders, %d without; hops %s", c.name, route.Cost, plain.Cost, strings.Join(hops, ", "))
	}
}

// The crafter's wait tile: beside the Nature ruins, a tile the engine accepts the talisman from, a few steps from the portal's exit
// tile, so the walk after every portal is as short as the map allows. On the real loc index, item config and collision pack.
func TestTheGateStandOnTheRealData(t *testing.T) {
	r := loadReal(t)
	a := runecraftplan.Nature
	ruins := r.one(t, runecraftplan.RuinsName, "", a.Ruins)
	d := &fleetrunner.Deps{Config: r.cfg, Pack: r.pk}
	stand, ok := GateStand(d, ruins)
	if !ok {
		t.Fatal("no stand tile beside the Nature ruins")
	}
	if tile.Chebyshev(stand, a.Exit) > 3 {
		t.Errorf("the stand %v is %d tiles from the exit %v", stand, tile.Chebyshev(stand, a.Exit), a.Exit)
	}
	typ, found := r.cfg.Loc(ruins.ID)
	if !found {
		t.Fatalf("loc %d not in the config", ruins.ID)
	}
	loc := locreach.Loc{X: ruins.X, Z: ruins.Z, Level: ruins.Level, Width: typ.Width, Length: typ.Length, Angle: ruins.Angle, Shape: ruins.Shape, ForceApproach: typ.ForceApproach}
	if reached, modelled := locreach.Reached(stand.X, stand.Z, loc); !modelled || !reached {
		t.Errorf("the engine does not accept a use on the ruins from %v (modelled %v reached %v)", stand, modelled, reached)
	}
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	back := planner.Plan(webwalk.Point{X: a.Exit.X, Z: a.Exit.Z}, webwalk.Point{X: stand.X, Z: stand.Z}, webwalk.Options{WalkOnly: true})
	if !back.OK || back.Cost > 5 {
		t.Errorf("the exit tile to the stand: ok %v cost %d (%s)", back.OK, back.Cost, back.Reason)
	}
	t.Logf("the Nature gate stand is %v, %d tile(s) from the exit %v (%d steps)", stand, tile.Chebyshev(stand, a.Exit), a.Exit, back.Cost)
}

// The Nature gate's tiles on the real collision pack (live 16 Oct 2026: four episodes of two 27 second walk-to-ruins failures, every one
// from (2866,3020) towards (2867,3019), a tile the collision pack says nobody can stand on). The list the stand is chosen from holds
// only tiles an account can stand on, the two tiles the plan names are in it or beside it, and a use on the ruins from any tile around
// them lands on a stand the engine accepts.
func TestTheGateTilesOnTheRealData(t *testing.T) {
	r := loadReal(t)
	a := runecraftplan.Nature
	ruins := r.one(t, runecraftplan.RuinsName, "", a.Ruins)
	d := &fleetrunner.Deps{Config: r.cfg, Pack: r.pk}
	typ, _ := r.cfg.Loc(ruins.ID)
	loc := locreach.Loc{X: ruins.X, Z: ruins.Z, Level: ruins.Level, Width: typ.Width, Length: typ.Length, Angle: ruins.Angle, Shape: ruins.Shape, ForceApproach: typ.ForceApproach}

	// The blocked tiles the live log named are blocked, and are not offered as stands.
	stands := Stands(d, ruins, a.Exit)
	have := map[tile.Tile]bool{}
	for _, s := range stands {
		have[s] = true
		if !r.pk.Walkable(s.X, s.Z, s.Level) {
			t.Errorf("stand %v is not walkable", s)
		}
		if reached, modelled := locreach.Reached(s.X, s.Z, loc); !modelled || !reached {
			t.Errorf("stand %v is not a tile the engine accepts a use from", s)
		}
	}
	for _, bad := range []tile.Tile{{X: 2867, Z: 3019}, {X: 2870, Z: 3021}} {
		if r.pk.Walkable(bad.X, bad.Z, bad.Level) {
			t.Errorf("%v is walkable: the collision pack changed under the plan", bad)
		}
		if have[bad] {
			t.Errorf("the blocked tile %v is offered as a stand", bad)
		}
	}
	if len(stands) < 4 {
		t.Errorf("only %d stands: %v", len(stands), stands)
	}

	// The crafter's stand is the plan's constant, and the carriers' wait tile is orthogonally beside it, walkable, one step away.
	stand, ok := GateStand(d, ruins)
	if !ok || stand != runecraftplan.GateStandTile {
		t.Fatalf("GateStand %v (%v), the plan says %v", stand, ok, runecraftplan.GateStandTile)
	}
	wait := runecraftplan.GateWaitTile
	if !r.pk.Walkable(wait.X, wait.Z, wait.Level) {
		t.Fatalf("the wait tile %v is not walkable", wait)
	}
	if dx, dz := abs(wait.X-stand.X), abs(wait.Z-stand.Z); dx+dz != 1 {
		t.Errorf("the wait tile %v is not orthogonally beside the stand %v", wait, stand)
	}
	step := r.pk.FindPath(pathfind.NavPoint{X: wait.X, Z: wait.Z}, pathfind.NavPoint{X: stand.X, Z: stand.Z})
	if !step.OK || step.Steps() != 1 {
		t.Errorf("the wait tile to the stand: ok %v, %d steps (%s)", step.OK, step.Steps(), step.Reason)
	}
	if tile.Chebyshev(wait, a.Exit) > 3 {
		t.Errorf("the wait tile %v is %d tiles from the exit %v", wait, tile.Chebyshev(wait, a.Exit), a.Exit)
	}

	// From every tile around the ruins the nearest stand is a tile an account can stand on and the engine accepts: the account that is
	// left two tiles west by a trade ends on one, not one tile short.
	for x := a.Ruins.X - 4; x <= a.Ruins.X+6; x++ {
		for z := a.Ruins.Z - 4; z <= a.Ruins.Z+6; z++ {
			from := tile.Tile{X: x, Z: z}
			if !r.pk.Walkable(x, z, 0) {
				continue
			}
			near := Stands(d, ruins, from)
			if len(near) == 0 {
				t.Fatalf("no stand from %v", from)
			}
			if !r.pk.Walkable(near[0].X, near[0].Z, 0) {
				t.Errorf("from %v the nearest stand %v is not walkable", from, near[0])
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// The walk a carrier makes to the mule's post when ScarabSwarm's quest hold has parked it at a bank on the far side of White Wolf
// Mountain (carrierhold-1017; live 17 Oct 2026: one sat at Varrock's west bank for 25 minutes with a shift due). It is the ship chain
// the mule itself takes home: Port Sarim, Karamja on foot to Brimhaven, Ardougne, then north. Planned the way scarabrunner.WalkTo
// plans it (the dens and the Ardougne bears kept out of, the walker's automatic zones, the combat level of a carrier) with the fares in
// the pack: a route has to exist from every bank the hold can park an account at, it has to take the ships and no den, and with no
// coins it must NOT exist (a walk that is not paid for is "unreachable", never a trip over the mountain), which is why a shift that
// starts there draws its fares from the bank first (carrierWorld.drawFares). It is also why the walk is not WalkKaramja's: with the
// corridor's boxes and the wrong-boat zone added, which lie across Musa Point and the Brimhaven road, no route from the mainland exists.
func TestTheCarriersWalkFromAMainlandBankToThePostPlansOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	post := tile.Tile{X: 2809, Z: 3441}
	var dens []webwalk.Rect
	for _, s := range randomevents.WolfTerritorySquares(2) {
		dens = append(dens, webwalk.Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	acrossAvoid := append(append([]webwalk.Rect(nil), dens...), fleetrunner.ArdougneBears)
	corridorAvoid := append(append([]webwalk.Rect(nil), acrossAvoid...), KaramjaRects()...)
	plan := func(from tile.Tile, avoid []webwalk.Rect, coins int) webwalk.Route {
		f, to := webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: post.X, Z: post.Z, Level: post.Level}
		zones := webwalk.ResolveDangerZones(nil, avoid, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: 60, CombatKnown: true, Start: &f, Dest: &to})
		return planner.Plan(f, to, webwalk.Options{AllowNPC: true, AllowSpecial: true, AvoidZones: zones,
			State: &webwalk.WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
				ItemCount: func(string) int { return coins }, WornCount: func(string) int { return 0 }}})
	}
	checked := 0
	for _, b := range questplan.Banks {
		if fleetplan.WestOfTheMountain(b.Tile) {
			continue
		}
		checked++
		rich := plan(b.Tile, acrossAvoid, 2000)
		if !rich.OK {
			t.Errorf("%s (%v): no route to the post with the fares in the pack: %s", b.Name, b.Tile, rich.Reason)
			continue
		}
		ships := 0
		for _, e := range rich.Hops() {
			if e.Kind == webwalk.KindShip {
				ships++
			}
		}
		if ships != 2 {
			t.Errorf("%s: the route to the post takes %d ship(s), want the Port Sarim and the Brimhaven ships", b.Name, ships)
		}
		for _, w := range rich.Waypoints {
			if webwalk.InZones(w.Point, dens) {
				t.Errorf("%s: the route steps on %s inside White Wolf Mountain's dens", b.Name, w.Point)
				break
			}
		}
		if poor := plan(b.Tile, acrossAvoid, 0); poor.OK {
			t.Errorf("%s: a route to the post exists with no coins in the pack (%d steps), so the fares are not being checked", b.Name, poor.Cost)
		}
		if rich.Cost > 1100 {
			t.Errorf("%s: %d steps to the post, more than the 45 minute walk home allows", b.Name, rich.Cost)
		}
		corridor := plan(b.Tile, corridorAvoid, 2000)
		t.Logf("%s: %d steps, %d ship(s); with the corridor's boxes added: ok %v", b.Name, rich.Cost, ships, corridor.OK)
	}
	if checked < 6 {
		t.Fatalf("only %d banks on the far side of the mountain were checked", checked)
	}
}

// A walk across the water that fails after the Port Sarim ship leaves the account on the island's east side, inside the corridor's
// wrong-boat zone, and the shift's walk home is WalkKaramja's (corridor boxes, wrong-boat zone and all). It plans from there: a start
// inside a rectangle may leave it. So a shift that started across the water and failed halfway is not stranded by its own go-home stage.
func TestTheCarriersWalkHomeFromEasternKaramjaPlansOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	post := tile.Tile{X: 2809, Z: 3441}
	var avoid []webwalk.Rect
	for _, s := range randomevents.WolfTerritorySquares(2) {
		avoid = append(avoid, webwalk.Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	avoid = append(avoid, fleetrunner.ArdougneBears)
	avoid = append(avoid, KaramjaRects()...)
	for _, from := range []tile.Tile{{X: 2954, Z: 3147}, {X: 2880, Z: 3170}, {X: 2820, Z: 3182}, {X: 2772, Z: 3234}} {
		f, to := webwalk.Point{X: from.X, Z: from.Z}, webwalk.Point{X: post.X, Z: post.Z}
		zones := webwalk.ResolveDangerZones(nil, avoid, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: 60, CombatKnown: true, Start: &f, Dest: &to})
		route := planner.Plan(f, to, webwalk.Options{AllowNPC: true, AllowSpecial: true, AvoidZones: zones,
			State: &webwalk.WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
				ItemCount: func(string) int { return 2000 }, WornCount: func(string) int { return 0 }}})
		if !route.OK || route.Cost > 700 {
			t.Errorf("from %v on Karamja: ok %v cost %d (%s), want a walk home of at most 700 steps", from, route.OK, route.Cost, route.Reason)
		}
	}
}
