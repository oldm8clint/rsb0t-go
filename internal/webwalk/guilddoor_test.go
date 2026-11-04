package webwalk

import "testing"

// guildExit is the outward edge of the Fishing Guild's inner door: loc 2025 at 2611,3394,
// crossed from the guild side (the loc's own tile) to 2611,3393 outside.
var guildExit = struct{ from, to Point }{
	from: Point{X: 2611, Z: 3394},
	to:   Point{X: 2611, Z: 3393},
}

// TestFishingGuildDoorIsInTheGraphBothWays exists because a diagnosis of the 2026-09-22
// stranding started from "the webwalk graph has no edge for loc 2025, so an account inside the
// guild cannot leave by any route". That was wrong, and a whole class of fix (add the edge,
// add a hops crossing, overlay the door onto the collision pack) would have been built on it.
// The edge is baked, in both directions, and this pins that so the next person does not have
// to re-derive it from graph.json.
func TestFishingGuildDoorIsInTheGraphBothWays(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	var out, in *Edge
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.LocID != 2025 || e.LocX != 2611 || e.LocZ != 3394 {
			continue
		}
		switch e.From {
		case guildExit.from:
			out = e
		case guildExit.to:
			in = e
		}
	}
	if out == nil {
		t.Fatalf("no graph edge leaves the Fishing Guild through loc 2025 from %s; an account inside the guild would have no way out at all", guildExit.from)
	}
	if in == nil {
		t.Fatalf("no graph edge enters the Fishing Guild through loc 2025 from %s", guildExit.to)
	}
	if out.To != guildExit.to || in.To != guildExit.from {
		t.Fatalf("the guild door's edges do not face each other: out %s -> %s, in %s -> %s", out.From, out.To, in.From, in.To)
	}
}

// TestFishingGuildExitIsNotGatedOnTheEntryLevel holds the graph to what content does.
// skill_fishing/scripts/fishing_guild.rs2 checks Fishing 68 only when check_axis says the
// player is OUTSIDE; from the guild side the door opens with no check. The baked graph gated
// both directions, and internal/webwalk/live.WorldState reads a skill the server has not
// reported yet as 0 and fails gated edges closed — so an account inside a sealed two-door room
// had no exit at all until its stats synced. See guildDoorWhy in graph.go.
func TestFishingGuildExitIsNotGatedOnTheEntryLevel(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		locID, locX, locZ int
		from              Point
		gated             bool
		what              string
	}
	wants := []want{
		{2025, 2611, 3394, Point{X: 2611, Z: 3394}, false, "the way OUT through the inner guild door"},
		{2025, 2611, 3394, Point{X: 2611, Z: 3393}, true, "the way IN through the inner guild door"},
		{1530, 2611, 3398, Point{X: 2611, Z: 3399}, false, "the way OUT through the outer guild door"},
		{1530, 2611, 3398, Point{X: 2611, Z: 3398}, true, "the way IN through the outer guild door"},
	}
	for _, w := range wants {
		found := false
		for i := range g.Edges {
			e := &g.Edges[i]
			if e.LocID != w.locID || e.LocX != w.locX || e.LocZ != w.locZ || e.From != w.from {
				continue
			}
			found = true
			gated := e.Requires != nil && len(e.Requires.Skills) > 0
			if gated != w.gated {
				t.Errorf("%s (loc %d from %s) has a skill gate = %v, want %v: content gates entry only, and gating the exit seals the guild",
					w.what, w.locID, w.from, gated, w.gated)
			}
		}
		if !found {
			t.Errorf("no edge for %s (loc %d at %d,%d from %s)", w.what, w.locID, w.locX, w.locZ, w.from)
		}
	}
}

// TestPlanLeavesTheFishingGuildWithNoFishingLevel is the same rule end to end over the real
// collision pack: a player standing on the guild's inner door tile with an EMPTY skills map —
// which is exactly what live.WorldState returns before the server has sent UPDATE_STAT — must
// still be able to plan its way out to East Ardougne. Before the correction this answered
// "unreachable", which is the log line 68 accounts were repeating on 2026-09-22.
func TestPlanLeavesTheFishingGuildWithNoFishingLevel(t *testing.T) {
	p := realPack(t)
	unsynced := &WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
		ItemCount: func(string) int { return 0 }, WornCount: func(string) int { return 0 }}
	// The Stage 3 destination the stranded accounts were walking to.
	dest := Point{X: 2655, Z: 3283}
	for _, from := range []Point{
		guildExit.from,     // on the door tile itself
		{X: 2616, Z: 3395}, // where the stranded accounts actually stood
		{X: 2611, Z: 3397}, // the courtyard between the two guild doors
		{X: 2586, Z: 3419}, // the guild bank
	} {
		r := p.Plan(from, dest, Options{State: unsynced})
		if !r.OK {
			t.Errorf("no route from %s (inside the Fishing Guild) to %s for an account whose skills have not synced: %s", from, dest, r.Reason)
		}
	}
}

// TestTheGuildPlanWouldFailWithoutTheCorrection is the control for the test above. A check that
// passed on a route that was already fine says nothing, and docs/trustworthy_harnesses.md is a
// list of the times this repo shipped exactly that. Planning the same journeys over the
// UNCORRECTED graph must answer "unreachable".
func TestTheGuildPlanWouldFailWithoutTheCorrection(t *testing.T) {
	p := realPack(t)
	raw, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	raw.ApplyOverrides(ApproachOverrides)
	uncorrected := NewPlanner(p.col, raw)
	unsynced := &WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
		ItemCount: func(string) int { return 0 }}
	dest := Point{X: 2655, Z: 3283}
	for _, from := range []Point{guildExit.from, {X: 2616, Z: 3395}, {X: 2586, Z: 3419}} {
		if r := uncorrected.Plan(from, dest, Options{State: unsynced}); r.OK {
			t.Errorf("the control passed: %s could already reach %s without the correction (cost %d), so the test above proves nothing", from, dest, r.Cost)
		}
	}
}
