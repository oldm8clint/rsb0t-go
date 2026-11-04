package fleetrunner

// The run staller's account of a walk: how many casts and rubs went out, how much of the walk they held, and what the
// walk cost in run energy, in one line per long walk, so the live log can show whether the stalling works (owner, 6
// October 2026: "we still run out of run energy while alching meaning its not correctly working").

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetplan"
)

// minLoggedWalk is the fewest tiles a walk covers for its line to be worth logging: a run energy reading is whole
// percents, so the cost of a shorter walk is noise.
const minLoggedWalk = 30

// walkLogEvery is the least time between two of these lines from one account.
const walkLogEvery = time.Minute

// walkLog is the walk in progress.
type walkLog struct {
	on bool
	// capable is whether the account could stall this walk at all: Magic, runes and an alchable for walk alch, or a
	// rub item. Only such a walk is logged, and only such a walk aims its clicks far (scarabplan.WalkAlchClickSteps).
	capable bool
	running bool
	// tick, energy and kg are the readings at the start; casts0 and rubs0 the session's counts then.
	TickLen, energy, kg int
	casts0, rubs0       int
	// tiles is the length of the path the walk's polls saw the account cover.
	tiles int
	// why is the last lasting reason a poll sent no cast (noteWhy), said on the walk's line when it stalled nothing.
	why string
}

// noteWhy keeps why a poll of this walk sent no cast. The passing gates (a click this tick, the last cast's delay, the
// walk clicking again soon) are not kept: every walk meets them, and they hide the reason that held a whole walk back.
func (t *Travel) noteWhy(why string) {
	if t == nil || !t.walk.on {
		return
	}
	switch why {
	case "a movement click went out this tick", "the last cast's delay is not over", "the walk will click again inside the cast's delay",
		"the walk fires a zone trigger inside the cast's delay":
		return
	}
	t.walk.why = why
}

// canAlch is whether this account walk-alchs now: the walk's own test for choosing a cast over a rub.
func (t *Travel) canAlch(d *Deps) bool {
	return t.Alch.Enabled && MagicLevel(d) >= t.Alch.Normalised().MinMagic && CountPackNamed(d, alchplan.NatureRune) > 0
}

// beginWalk takes the readings a walk's line is made of. Nothing for a Travel that stalls nothing.
func (t *Travel) beginWalk(d *Deps) {
	if t == nil {
		return
	}
	t.walkPrevOK = false // the last walk's last tile is not this walk's previous one
	t.walk = walkLog{}
	if t.walkAside(d) == nil {
		return
	}
	st := d.Session.State()
	w := walkLog{on: true, running: st.RunEnabled(), TickLen: st.Tick(), casts0: t.castsSent, rubs0: t.rubs}
	w.capable = t.canAlch(d) || (t.Rub && RubItemHeld(d)) || t.enchantActive()
	if e, ok := st.Energy(); ok {
		w.energy = e
	} else {
		w.on = false
	}
	if kg, ok := st.Weight(); ok {
		w.kg = kg
	}
	t.walk = w
}

// clickSteps is how far this walk's clicks aim: far when the walk will cast or rub (fewer clicks, so fewer ticks the
// stalling cannot use), the walker's default otherwise.
func (t *Travel) clickSteps() int {
	if t == nil || !t.walk.on || !t.walk.capable {
		return 0
	}
	if t.Tune.ClickSteps != 0 {
		return t.Tune.ClickSteps
	}
	return fleetplan.WalkAlchClickSteps
}

// endWalk logs the walk's line.
func (t *Travel) endWalk(d *Deps) {
	if t == nil {
		return
	}
	t.logEstimate(d) // what the last teleport predicted for this walk, against what it took
	if !t.walk.on {
		return
	}
	w := t.walk
	t.walk = walkLog{}
	st := d.Session.State()
	ticks := st.Tick() - w.TickLen
	e1, ok := st.Energy()
	if !ok || !w.capable || !w.running || w.tiles < minLoggedWalk || ticks <= 0 {
		return
	}
	casts, rubs := t.castsSent-w.casts0, t.rubs-w.rubs0
	why := ""
	if casts == 0 && rubs == 0 && w.why != "" {
		why = "; stalled nothing: " + w.why
	}
	t.sayEvery(d, "run-staller-walk", walkLogEvery, fmt.Sprintf(
		"run staller: %d tiles in %d ticks (%.2f a tick), %d casts and %d rubs, %d%% of the ticks held; run energy %d%% -> %d%%, %.3f%% a tile against %.3f%% with no staller%s",
		w.tiles, ticks, float64(w.tiles)/float64(ticks), casts, rubs, fleetplan.HeldPercent(casts, rubs, ticks),
		w.energy, e1, float64(w.energy-e1)/float64(w.tiles), fleetplan.PlainDrainPerTile(w.kg), why))
}
