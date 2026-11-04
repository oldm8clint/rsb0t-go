package runecraftrunner

import (
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// realLoc is the locreach model of a found loc, as the live world's reach test builds it.
func (r realData) realLoc(t *testing.T, m locindex.Match) locreach.Loc {
	t.Helper()
	typ, ok := r.cfg.Loc(m.ID)
	if !ok {
		t.Fatalf("loc %d is not in the config", m.ID)
	}
	return locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
}

// The direct ops on the Nature temple's REAL geometry, the real loc index and the real collision pack: the plan's stands are the tiles the
// engine accepts the altar and the portal from, the planned routes are the plan's steps long and the server's own replay walks them, and a
// whole cycle through the movement engine puts the craft Steps ticks after the read and the exit nine ticks after the craft.
func TestDirectOpsOnTheRealNatureTemple(t *testing.T) {
	r := loadReal(t)
	a := runecraftplan.Nature
	altar := r.one(t, runecraftplan.AltarName, runecraftplan.AltarOp, a.AltarAt)
	portal := r.one(t, runecraftplan.PortalName, runecraftplan.PortalOp, a.PortalAt)
	plan, ok := PlanTemple(r.pk, r.cfg, a, altar, portal)
	if !ok {
		t.Fatal("no temple plan")
	}

	toAltar, why := PlanDirect(r.pk, a.Enter, plan.AltarStand)
	if why != "" || toAltar.Steps != plan.ToAltar || toAltar.To != plan.AltarStand {
		t.Fatalf("arrival to the altar stand %v: %+v, %q (the plan says %d steps)", plan.AltarStand, toAltar, why, plan.ToAltar)
	}
	toPortal, why := PlanDirect(r.pk, plan.AltarStand, plan.PortalStand)
	if why != "" || toPortal.Steps != plan.ToPortal || toPortal.To != plan.PortalStand {
		t.Fatalf("altar stand to the portal stand %v: %+v, %q (the plan says %d steps)", plan.PortalStand, toPortal, why, plan.ToPortal)
	}
	t.Logf("arrival %v -> altar stand %v: %d steps, route %v; -> portal stand %v: %d steps, route %v",
		a.Enter, plan.AltarStand, toAltar.Steps, toAltar.Route, plan.PortalStand, toPortal.Steps, toPortal.Route)

	d0 := &fleetrunner.Deps{Config: r.cfg}
	if !ReachesFrom(d0, altar)(plan.AltarStand) || !ReachesFrom(d0, portal)(plan.PortalStand) {
		t.Fatal("a planned stand is not a tile the engine accepts its loc from")
	}
	// The click alone from the arrival tile is the engine's "I can't reach that!": the walk is what the direct op is made of.
	if ReachesFrom(d0, altar)(a.Enter) {
		t.Fatal("the arrival tile reaches the altar: there is nothing to walk")
	}

	// The whole cycle on the movement engine, with the real pack and the real locs.
	e, d := newDirEngine(t, r.pk, natureKit(25), a.Enter, 74)
	e.mu.Lock()
	e.altar, e.portal = r.realLoc(t, altar), r.realLoc(t, portal)
	e.mu.Unlock()
	cr := CraftDirect(d, a, altar, toAltar)
	if !cr.OK() || !cr.Direct || cr.DirectFailed || cr.Clicks != 1 {
		t.Fatalf("craft: %+v", cr)
	}
	res := PortalOutDirect(d, a, portal, cr.Tick, func() (DirectOp, bool) { return planTo(d, plan.PortalStand) }, func() bool { t.Error("the plain approach ran"); return true })
	if !res.Out || !res.Direct || res.DirectFailed || res.Clicks != 1 {
		t.Fatalf("portal: %+v", res)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	locs := e.readsOfLocked(clientprot.OPLOC1)
	if len(locs) != 2 || len(e.craftTicks) != 1 || len(e.exitTicks) != 1 {
		t.Fatalf("loc reads %v, crafts %v, exits %v", locs, e.craftTicks, e.exitTicks)
	}
	if got := e.craftTicks[0] - locs[0]; got != plan.ToAltar {
		t.Errorf("crafted %d ticks after the read, want the plan's %d steps", got, plan.ToAltar)
	}
	if first := e.craftTicks[0] + runecraftplan.HoldTicks + 1; locs[1] < first || locs[1] > first+1 || e.exitTicks[0]-locs[1] != plan.ToPortal {
		t.Errorf("portal read in tick %d (craft %d), out in tick %d: want the first accepted read, %d, and %d steps", locs[1], e.craftTicks[0], e.exitTicks[0], first, plan.ToPortal)
	}
	if e.dropped != 0 || e.cantReach != 0 {
		t.Errorf("dropped %d, can't reach %d", e.dropped, e.cantReach)
	}
	t.Logf("%d ticks from the read to the craft, %d from the craft to the exit (a walk-then-click cannot beat %d and %d at zero latency)",
		e.craftTicks[0]-locs[0], e.exitTicks[0]-e.craftTicks[0], plan.ToAltar, runecraftplan.HoldTicks+1+plan.ToPortal)
}
