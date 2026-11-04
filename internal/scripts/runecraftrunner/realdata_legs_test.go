package runecraftrunner

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// expandPath is the tiles a corner-compressed route walks, the start excluded: every segment is a straight run in one of the eight
// directions (pathfind.PathResult.Steps).
func expandPath(wp []pathfind.NavPoint) []tile.Tile {
	var out []tile.Tile
	for i := 1; i < len(wp); i++ {
		a, b := wp[i-1], wp[i]
		dx, dz := sgn(b.X-a.X), sgn(b.Z-a.Z)
		x, z := a.X, a.Z
		for x != b.X || z != b.Z {
			x, z = x+dx, z+dz
			out = append(out, tile.Tile{X: x, Z: z, Level: b.Level})
		}
	}
	return out
}

func sgn(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// The leg line on the Nature temple's real geometry: a cycle walked a tile a tick along the planned walks reads as the plan's steps on
// each leg, the altar and the portal are reached from the plan's stands and from nowhere else on the way, and the hold sits between.
func TestLegsMeasureTheRealNatureTemple(t *testing.T) {
	r := loadReal(t)
	a := runecraftplan.Nature
	altar := r.one(t, runecraftplan.AltarName, runecraftplan.AltarOp, a.AltarAt)
	portal := r.one(t, runecraftplan.PortalName, runecraftplan.PortalOp, a.PortalAt)
	plan, ok := PlanTemple(r.pk, r.cfg, a, altar, portal)
	if !ok {
		t.Fatal("no temple plan")
	}
	d := &fleetrunner.Deps{Config: r.cfg}
	sp := runecraftplan.LegSpots{Inside: a.Inside, AtAltar: ReachesFrom(d, altar), AtPortal: ReachesFrom(d, portal)}
	if sp.AtAltar == nil || sp.AtPortal == nil {
		t.Fatal("no reach test for the altar or the portal")
	}
	if !sp.AtAltar(plan.AltarStand) || !sp.AtPortal(plan.PortalStand) {
		t.Fatalf("the plan's stands %v and %v are not where the engine accepts the altar and the portal", plan.AltarStand, plan.PortalStand)
	}
	if sp.AtAltar(a.Exit) || sp.AtPortal(a.Exit) || sp.AtAltar(tile.Tile{X: plan.AltarStand.X, Z: plan.AltarStand.Z, Level: 1}) {
		t.Error("the altar or the portal was reached from the wrong place or the wrong level")
	}

	toAltar := r.pk.FindPath(pathfind.NavPoint{X: a.Enter.X, Z: a.Enter.Z}, pathfind.NavPoint{X: plan.AltarStand.X, Z: plan.AltarStand.Z})
	toPortal := r.pk.FindPath(pathfind.NavPoint{X: plan.AltarStand.X, Z: plan.AltarStand.Z}, pathfind.NavPoint{X: plan.PortalStand.X, Z: plan.PortalStand.Z})
	if !toAltar.OK || !toPortal.OK {
		t.Fatalf("paths: %v %v", toAltar.OK, toPortal.OK)
	}

	// Close 200, click 201, inside 204 (a teleport takes three), then a tile a tick, the craft the tick he arrives, the hold, the walk
	// back a tile a tick, the portal the tick after.
	var s []runecraftplan.LegSample
	tick := 200
	push := func(at tile.Tile) { s = append(s, runecraftplan.LegSample{Tick: tick, At: at, Energy: 12}); tick++ }
	stand := tile.Tile{X: a.Exit.X + 2, Z: a.Exit.Z - 2}
	push(stand)
	push(stand)
	push(stand)
	push(stand)
	push(a.Enter)
	for _, p := range expandPath(toAltar.Waypoints) {
		push(p)
	}
	altarTick := tick - 1
	push(plan.AltarStand) // the craft is seen the tick after he arrives
	craftTick := tick - 1
	for i := 0; i < runecraftplan.HoldTicks; i++ {
		push(plan.AltarStand)
	}
	for _, p := range expandPath(toPortal.Waypoints) {
		push(p)
	}
	portalTick := tick - 1
	push(a.Exit)

	l := runecraftplan.MeasureLegs(runecraftplan.LegInput{Close: 200, Click: 201, Craft: craftTick, TalismanClicks: 1, CraftClicks: 1, PortalClicks: 1, PortalHeld: 4, Samples: s}, sp)
	if !l.Complete() {
		t.Fatalf("incomplete: %+v", l)
	}
	if l.Inside != 204 {
		t.Errorf("inside at %d, want 204", l.Inside)
	}
	if l.Altar != altarTick || l.Altar-l.Inside != plan.ToAltar || l.AltarTiles != plan.ToAltar {
		t.Errorf("altar at %d (%d ticks, %d tiles after inside), the plan says %d steps", l.Altar, l.Altar-l.Inside, l.AltarTiles, plan.ToAltar)
	}
	if l.Hold != craftTick+runecraftplan.HoldTicks || l.Portal != portalTick || l.Portal-l.Hold != plan.ToPortal || l.PortalTiles != plan.ToPortal {
		t.Errorf("portal at %d (hold ends %d), %d tiles, the plan says %d steps", l.Portal, l.Hold, l.PortalTiles, plan.ToPortal)
	}
	if l.Exit != portalTick+1 {
		t.Errorf("exit at %d, want %d", l.Exit, portalTick+1)
	}
	t.Log(l.Line())
}
