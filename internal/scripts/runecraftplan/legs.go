package runecraftplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// THE GATE CYCLE'S LEGS, IN GAME TICKS (CR-3 step 1 of docs/xp_rate_plan_2026-10-19.md, 19 Oct 2026).
//
// The main account's Nature gate runs one cycle a trade: the trade closes, the talisman goes on the ruins, the account lands inside the temple, walks
// to the altar, crafts, waits out the craft's hold, walks to the portal and takes it. The plan measured the whole cycle at 29.9-31.4
// ticks from the trade lines alone and BUILT its tick lanes (CR-1 direct ops, CR-2 one-write accept, CX-1 casts on the legs, the run
// energy budget) on numbers that were each "plus or minus one tick": inside to craft 5.5, craft to exit 9.6. None of those lanes can be
// judged, or reverted, without the cycle's own legs measured in the unit the engine counts in. This file is that measurement, and it
// changes nothing the crafter does.
//
// One server tick is one PLAYER_INFO the client read (gamestate.EventTick), so the marks are exact ticks of this client's own view, on
// the same counter as the `ticks ask .. answer .. open .. close ..` of the trade line (the next trade's `ask` minus this line's
// `exit` is the exit-to-request leg CR-5 wants). A mark that is a position (inside, at the altar, at the portal, outside) is the first
// sampled tick the account's tile satisfied it; a mark that is an action (the trade closing, the talisman click, the craft) is the tick
// the code that did it reported.
//
// The run flag is the option_run varp the server last sent (it is cleared by the engine when the energy runs out, so a flag that is on
// is a run the server believes in) and the energy is the percentage of UPDATE_RUNENERGY. A walking leg reports the tiles it covered
// in its ticks: more tiles than ticks is a run, whatever the flag says.

// LegSample is the crafter at one server tick.
type LegSample struct {
	Tick int
	At   tile.Tile
	// Run is the option_run varp; Energy the run energy percentage, -1 when the server has not said.
	Run    bool
	Energy int
}

// LegSpots are the three places the cycle's legs end at, as tests on a tile: inside the Nature temple, standing where the altar's
// Craft-rune is accepted and standing where the portal's Use is accepted. A nil test is a place that is never reached.
type LegSpots struct {
	Inside, AtAltar, AtPortal func(tile.Tile) bool
}

func (s LegSpots) inside(t tile.Tile) bool   { return s.Inside != nil && s.Inside(t) }
func (s LegSpots) atAltar(t tile.Tile) bool  { return s.AtAltar != nil && s.AtAltar(t) }
func (s LegSpots) atPortal(t tile.Tile) bool { return s.AtPortal != nil && s.AtPortal(t) }

// LegInput is what one cycle's code observed: the ticks of the actions it did, how many clicks they took, and the tick-by-tick samples
// from the trade's close to the account's exit.
type LegInput struct {
	// Close is the tick the trade closed and the essence was seen in the pack; Click the tick the talisman click was first sent in
	// (0: not known); Craft the tick the craft was seen in.
	Close, Click, Craft int
	// TalismanClicks, CraftClicks and PortalClicks are how many times each was sent (more than one is a retry), PortalHeld how many
	// ticks the first portal click was held back for the craft's hold, ExtraTries how many times a stage was entered a second time
	// (it failed and the loop ran it again).
	TalismanClicks, CraftClicks, PortalClicks, PortalHeld, ExtraTries int
	// AltarMode and PortalMode say how a leg was done when a direct op was in play (direct.go): ModeDirect, or ModeFallback when the
	// direct attempt failed and the plain walk-then-click finished the leg. Empty: the leg was walked the plain way without trying.
	AltarMode, PortalMode string
	// AltarCast and PortalCast say how the cast on a leg came out ("low landed", "low failed", "low not seen"; empty: none was planned).
	AltarCast, PortalCast string
	// Together says the talisman was written in the same message as the trade's confirm accept (together.go): Click is then the tick of that
	// write, which is at or before the tick the close was SEEN in, and the legs after it are measured from the close.
	Together bool
	// Open is the tick the trade screen opened in (0: not known). The close moves later when the accept and the talisman are written together
	// (the use is read in the tick the trade completes, so close to inside shrinks and the close itself waits for the carrier), so the saving
	// of that lane is read from open to inside, which both flows measure from the same mark.
	Open    int
	Samples []LegSample
}

// Legs is one cycle measured. A tick of 0 is a mark that was not observed.
type Legs struct {
	Close, Click, Inside, Altar, Craft, Hold, Portal, Exit int
	// AltarTiles and PortalTiles are the tiles covered walking each leg; AltarRun and PortalRun whether the run flag was on when it
	// began (-1: unknown) and AltarEnergy and PortalEnergy the energy then (-1: unknown).
	AltarTiles, PortalTiles   int
	AltarRun, PortalRun       int
	AltarEnergy, PortalEnergy int
	// EnergyClose and EnergyExit are the energy at the trade's close and at the exit (-1: unknown).
	EnergyClose, EnergyExit int
	In                      LegInput
}

// MeasureLegs reads the marks of one cycle off its samples. It does not guess: a mark the samples do not show stays 0.
func MeasureLegs(in LegInput, sp LegSpots) Legs {
	l := Legs{Close: in.Close, Click: in.Click, Craft: in.Craft, In: in,
		AltarRun: -1, PortalRun: -1, AltarEnergy: -1, PortalEnergy: -1, EnergyClose: -1, EnergyExit: -1}
	if in.Craft > 0 {
		l.Hold = in.Craft + HoldTicks
	}
	first := func(from int, ok func(LegSample) bool) (LegSample, bool) {
		for _, s := range in.Samples {
			if s.Tick >= from && ok(s) {
				return s, true
			}
		}
		return LegSample{}, false
	}
	if s, ok := first(in.Close, func(LegSample) bool { return true }); ok {
		l.EnergyClose = s.Energy
	}
	if s, ok := first(in.Close, func(s LegSample) bool { return sp.inside(s.At) }); ok {
		l.Inside = s.Tick
	}
	if l.Inside > 0 {
		if s, ok := first(l.Inside, func(s LegSample) bool { return sp.atAltar(s.At) }); ok {
			l.Altar = s.Tick
			l.AltarTiles = tilesBetween(in.Samples, l.Inside, l.Altar)
		}
		l.AltarRun, l.AltarEnergy = runAt(in.Samples, l.Inside)
	}
	if l.Craft > 0 {
		// The account cannot walk in the hold (a walk read there is dropped), so a portal it already stood at is reached when the
		// hold ends, not before: the leg is measured from the hold's end.
		if s, ok := first(l.Craft, func(s LegSample) bool { return sp.atPortal(s.At) }); ok {
			l.Portal = max(s.Tick, l.Hold)
			l.PortalTiles = tilesBetween(in.Samples, l.Craft, l.Portal)
		}
		l.PortalRun, l.PortalEnergy = runAt(in.Samples, l.Hold)
		if s, ok := first(l.Craft+1, func(s LegSample) bool { return !sp.inside(s.At) }); ok {
			l.Exit = s.Tick
			l.EnergyExit = s.Energy
		}
	}
	return l
}

// tilesBetween is the tiles covered by the samples in the ticks after from up to and including to.
func tilesBetween(samples []LegSample, from, to int) int {
	n := 0
	for i := 1; i < len(samples); i++ {
		s := samples[i]
		if s.Tick > from && s.Tick <= to {
			n += tile.Chebyshev(samples[i-1].At, s.At)
		}
	}
	return n
}

// runAt is the run flag (1 on, 0 off, -1 unknown) and the energy of the last sample at or before tick.
func runAt(samples []LegSample, tick int) (run, energy int) {
	run, energy = -1, -1
	for _, s := range samples {
		if s.Tick > tick {
			break
		}
		run = 0
		if s.Run {
			run = 1
		}
		energy = s.Energy
	}
	return run, energy
}

// Complete reports whether the marks a cycle line is worth printing for were all observed: the trade's close, the arrival inside, the
// craft and the exit. A cycle that was cut short (a restart in the temple, the first cycle of a resumed round) has no line.
func (l Legs) Complete() bool {
	return l.Close > 0 && l.Inside > 0 && l.Craft > 0 && l.Exit > 0
}

// Total is the ticks from the trade's close to the exit.
func (l Legs) Total() int { return l.Exit - l.Close }

// OpenToInside is the ticks from the trade screen opening to the account being seen inside the temple (0 when either was not observed).
func (l Legs) OpenToInside() int {
	if l.In.Open <= 0 || l.Inside <= 0 {
		return 0
	}
	return l.Inside - l.In.Open
}

// Retried reports a cycle that took a second click anywhere: its legs include the retry.
func (l Legs) Retried() bool {
	return l.In.TalismanClicks > 1 || l.In.CraftClicks > 1 || l.In.PortalClicks > 1 || l.In.ExtraTries > 0
}

// Line is the log line of the cycle (without the "gate:" prefix the caller's logger adds):
//
//	legs: close 5012, click 5013 (+1), inside 5016 (+3), altar 5021 (+5; 6 tiles; run on; energy 2%), craft 5023 (+2), hold 5027 (+4),
//	portal 5031 (+4; 5 tiles; run off; energy 0%), exit 5032 (+1); 20 ticks close to exit; energy 2% -> 0%; clicks talisman 1 craft 1
//	portal 1 (held back 3 ticks)
//
// The deltas are from the mark before it in the list; a mark that was not observed is "?" and has no delta.
func (l Legs) Line() string {
	var b strings.Builder
	b.WriteString("legs: ")
	prev := l.Close
	mark := func(name string, tick int, extra string) {
		if tick <= 0 {
			fmt.Fprintf(&b, ", %s ?", name)
			return
		}
		fmt.Fprintf(&b, ", %s %d", name, tick)
		var parts []string
		if prev > 0 {
			parts = append(parts, fmt.Sprintf("%+d", tick-prev))
		}
		if extra != "" {
			parts = append(parts, extra)
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(parts, "; "))
		}
		prev = tick
	}
	fmt.Fprintf(&b, "close %d", l.Close)
	if l.Click > 0 {
		if l.In.Together {
			// Sent with the accept, so before the close could be seen: the click is a note on the close, not a step after it.
			mark("click", l.Click, "with the accept")
			prev = l.Close
		} else {
			mark("click", l.Click, "")
		}
	}
	mark("inside", l.Inside, "")
	mark("altar", l.Altar, walkWord(l.AltarTiles, l.AltarRun, l.AltarEnergy))
	mark("craft", l.Craft, "")
	mark("hold", l.Hold, "")
	mark("portal", l.Portal, walkWord(l.PortalTiles, l.PortalRun, l.PortalEnergy))
	mark("exit", l.Exit, "")
	if l.Exit > 0 && l.Close > 0 {
		fmt.Fprintf(&b, "; %d ticks close to exit", l.Total())
	}
	if n := l.OpenToInside(); n > 0 {
		fmt.Fprintf(&b, "; %d ticks open to inside", n)
	}
	if l.EnergyClose >= 0 || l.EnergyExit >= 0 {
		fmt.Fprintf(&b, "; energy %s -> %s", pctWord(l.EnergyClose), pctWord(l.EnergyExit))
	}
	fmt.Fprintf(&b, "; clicks talisman %d craft %d portal %d", l.In.TalismanClicks, l.In.CraftClicks, l.In.PortalClicks)
	if l.In.PortalClicks > 0 {
		fmt.Fprintf(&b, " (held back %d ticks)", l.In.PortalHeld)
	}
	if l.Retried() {
		b.WriteString("; RETRIED, so the legs include the retry")
	}
	if l.In.Together {
		b.WriteString("; one write: the confirm accept and the talisman")
	}
	if l.In.AltarMode != "" || l.In.PortalMode != "" {
		fmt.Fprintf(&b, "; direct ops: altar %s, portal %s", modeWord(l.In.AltarMode), modeWord(l.In.PortalMode))
	}
	if l.In.AltarCast != "" || l.In.PortalCast != "" {
		fmt.Fprintf(&b, "; leg casts: altar %s, portal %s", castWord(l.In.AltarCast), castWord(l.In.PortalCast))
	}
	return b.String()
}

func castWord(m string) string {
	if m == "" {
		return "none"
	}
	return m
}

func modeWord(m string) string {
	if m == "" {
		return "plain"
	}
	return m
}

func walkWord(tiles, run, energy int) string {
	parts := []string{fmt.Sprintf("%d tile(s)", tiles)}
	switch run {
	case 1:
		parts = append(parts, "run on")
	case 0:
		parts = append(parts, "run off")
	}
	if energy >= 0 {
		parts = append(parts, "energy "+pctWord(energy))
	}
	return strings.Join(parts, "; ")
}

func pctWord(p int) string {
	if p < 0 {
		return "?"
	}
	return fmt.Sprintf("%d%%", p)
}
