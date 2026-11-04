package bot

// The gate cycle's per-leg timing line (CR-3 step 1 of docs/xp_rate_plan_2026-10-19.md; runecraftplan/legs.go has the format and the
// reasoning). One line a cycle, logged when the portal has taken the account out:
//
//	gate: legs: close 5012, click 5013 (+1), inside 5016 (+3), altar 5021 (+5; 6 tile(s); run on; energy 2%), craft 5023 (+2), hold 5027
//	(+4), portal 5031 (+4; 5 tile(s); run off; energy 0%), exit 5032 (+1); 20 ticks close to exit; energy 2% -> 0%; clicks talisman 1
//	craft 1 portal 1 (held back 3 ticks)
//
// It reads and logs; the cycle is the one it always was. It exists so CR-1 (direct-op legs), CR-2 (one-write accept), CX-1 (casts on the
// legs) and the run-energy budget are each judged against a measured leg and not against the plan's "plus or minus one tick", and so a
// lane that makes a leg longer can be reverted on evidence. A failure anywhere in it (no tick source, a loc the config does not know)
// costs the line and nothing else.

import (
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// legTracer is the tick sampler (runecraftrunner.LegTrace; a test supplies its own).
type legTracer interface {
	Arm()
	Take() []runecraftplan.LegSample
	Close()
}

// gateLegs collects one cycle's observations between the trade's close and the exit.
type gateLegs struct {
	d     *fleetrunner.Deps
	trace legTracer
	in    runecraftplan.LegInput
	// open: a cycle is being collected. tries counts how often each stage was entered this cycle (a second entry is a retry).
	open  bool
	tries map[runecraftplan.Stage]int
	// spots are the three tests the legs end at, built once per round from the altar and portal locs.
	spots     runecraftplan.LegSpots
	haveSpots bool
	// said is the last line logged (the tests read it).
	said string
}

func newGateLegs(d *fleetrunner.Deps) *gateLegs {
	return &gateLegs{d: d, trace: runecraftrunner.NewLegTrace(d), tries: map[runecraftplan.Stage]int{}}
}

// close drops the tick subscription (the end of the round).
func (g *gateLegs) close() {
	if g != nil && g.trace != nil {
		g.trace.Close()
	}
}

// arm starts a cycle at the tick its trade closed in.
func (g *gateLegs) arm(closeTick int) {
	if g == nil {
		return
	}
	g.in = runecraftplan.LegInput{Close: closeTick}
	g.open = true
	g.tries = map[runecraftplan.Stage]int{}
	if g.trace != nil {
		g.trace.Arm()
	}
}

// enter counts a stage being entered in this cycle.
func (g *gateLegs) enter(s runecraftplan.Stage) {
	if g != nil && g.open {
		g.tries[s]++
	}
}

func (g *gateLegs) noteTalisman(clickTick, clicks int) {
	if g != nil && g.open {
		g.in.Click, g.in.TalismanClicks = clickTick, clicks
	}
}

// noteOpen records the tick the trade screen opened in.
func (g *gateLegs) noteOpen(tick int) {
	if g != nil && g.open {
		g.in.Open = tick
	}
}

// noteTogetherPending says the talisman went out in the same message as the trade's confirm accept, in tick tick: the click mark is that
// tick, and the legs after it are measured from the close (runecraftplan.LegInput.Together).
func (g *gateLegs) noteTogetherPending(tick int) {
	if g != nil && g.open {
		g.in.Click, g.in.TalismanClicks, g.in.Together = tick, 1, true
	}
}

func (g *gateLegs) noteCraft(craftTick, clicks int) {
	if g != nil && g.open {
		g.in.Craft, g.in.CraftClicks = craftTick, clicks
	}
}

func (g *gateLegs) notePortal(clicks, held int) {
	if g != nil && g.open {
		g.in.PortalClicks, g.in.PortalHeld = clicks, held
	}
}

// noteMode says how a leg was done when a direct op was tried for it (runecraftplan.ModeDirect or ModeFallback).
func (g *gateLegs) noteMode(l runecraftplan.Leg, mode string) {
	if g == nil || !g.open {
		return
	}
	if l == runecraftplan.LegPortal {
		g.in.PortalMode = mode
	} else {
		g.in.AltarMode = mode
	}
}

// noteCast says how the cast on a leg came out (runecraftplan.LegInput.AltarCast).
func (g *gateLegs) noteCast(l runecraftplan.Leg, word string) {
	if g == nil || !g.open {
		return
	}
	if l == runecraftplan.LegPortal {
		g.in.PortalCast = word
	} else {
		g.in.AltarCast = word
	}
}

// spotsFor builds the three place tests the first time they are needed.
func (g *gateLegs) spotsFor(a runecraftplan.Altar) runecraftplan.LegSpots {
	if g.haveSpots {
		return g.spots
	}
	sp := runecraftplan.LegSpots{Inside: a.Inside}
	if altar, ok := runecraftrunner.FindAltar(g.d, a); ok {
		sp.AtAltar = runecraftrunner.ReachesFrom(g.d, altar)
	}
	if portal, ok := runecraftrunner.FindPortal(g.d, a); ok {
		sp.AtPortal = runecraftrunner.ReachesFrom(g.d, portal)
	}
	if sp.AtAltar != nil && sp.AtPortal != nil {
		g.spots, g.haveSpots = sp, true
	}
	return sp
}

// finish ends the cycle at the exit and returns its line ("" when the cycle was not one worth a line: cut short, or never armed).
func (g *gateLegs) finish(a runecraftplan.Altar) string {
	if g == nil || !g.open {
		return ""
	}
	g.open = false
	in := g.in
	if g.trace != nil {
		in.Samples = g.trace.Take()
	}
	for _, n := range g.tries {
		if n > 1 {
			in.ExtraTries += n - 1
		}
	}
	legs := runecraftplan.MeasureLegs(in, g.spotsFor(a))
	if !legs.Complete() {
		return ""
	}
	g.said = legs.Line()
	return g.said
}
