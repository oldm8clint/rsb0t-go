package bot

// ALCHEMY ON THE NATURE GATE'S WALKING LEGS (CX-1 of docs/xp_rate_plan_2026-10-19.md, masterspeed step 4; runecraftplan/legcast.go has the
// engine rules, the cost and the score, runecraftrunner/legcast.go the ride).
//
// The two walks inside the temple are the only stretch of the cycle where the crafter is alone and cannot be asked for a trade, so they are
// where the gate's bows are cast: Low Level Alchemy on both legs ("low2", the default) costs the cycle no tick when the walk is four steps,
// High Level Alchemy costs two. The cast rides on the direct op of CR-1 (mastercrafter_directops.go) and never replaces it: when no cast is
// planned the leg is the direct op, to the byte, and when a cast fails the leg is walked and clicked the plain way like any failed direct op.
//
// It needs the bows in the pack through the trades, which is craftRuneAlchHold: without it the stack is cast away on the road and at the
// gate before the first trade, and the legs have nothing to cast. The highest tier held is cast first (Magic longbow before Magic
// shortbow, runecraftplan.AlchBows' order); the idle casts between trades (mastercrafter_gatealch.go) go on as they were.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// legCastState is the gate alchemy's part of the casts on the legs: the figures of the casts that landed (the idle casts are counted by
// the Travel, which does not see these).
type legCastState struct {
	casts, xp, coins int
	// lockUntil is the client frame the last leg cast's lock is over in (its landing frame and the spell's AcceptedAfter): a cast on a leg is
	// asked about in ticks, not by the idle casts' wall clock (alchLocked), which assumes High Level Alchemy's lock and a server that never
	// ticks slow.
	lockUntil int
}

// legCastBow is the dearest bow the pack holds, the one the legs cast on.
func (w *masterNatureWorld) legCastBow() (runecraftrunner.AlchBowIDs, bool) {
	for _, b := range w.alch.bows {
		if runecraftrunner.PackCount(w.d, b.Noted) > 0 {
			return b, true
		}
	}
	return runecraftrunner.AlchBowIDs{}, false
}

// legCastStartup says at the start of a gate round what the casts on the legs will do.
func (w *masterNatureWorld) legCastStartup() {
	mode := w.legcasts.Mode()
	switch {
	case !w.legcasts.On():
		w.logf("leg casts: OFF (%s %q): the walks inside the temple are the direct ops of before, with no cast", mastercraftplan.KeyRuneLegCasts, string(mode))
	case !w.set.AlchHold || !w.set.GateAlch || w.set.AlchBows <= 0:
		w.logf("leg casts: %s (%s) but nothing to cast with: the legs cast the bows held through the trades, which needs %s true, %s true and %s above 0 (the stack is cast away before the first trade otherwise)",
			mode, mastercraftplan.KeyRuneLegCasts, mastercraftplan.KeyRuneAlchHold, mastercraftplan.KeyRuneGateAlch, mastercraftplan.KeyRuneAlchBows)
	default:
		a, _ := mode.SpellFor(runecraftplan.LegAltar)
		p, _ := mode.SpellFor(runecraftplan.LegPortal)
		w.logf("leg casts: ON (%s %q): altar leg %s, portal leg %s on the dearest bow held, the cast written a tick after the direct op and the click sent again after the lock; a leg whose casts cost more than %.1f tick(s) over plan (%d casts judged) or fail walks %d cycles without one, %d stand-downs in a row end it for the round",
			mastercraftplan.KeyRuneLegCasts, string(mode), legSpellWord(a), legSpellWord(p), runecraftplan.LegCastMaxMean, runecraftplan.LegCastWindow, runecraftplan.LegCastBackoff, runecraftplan.LegCastGiveUp)
	}
}

func legSpellWord(s runecraftplan.LegSpell) string {
	if s.Name == "" {
		return "none"
	}
	return s.Name
}

// legCastSay logs a reason a leg was not cast on, once a round for each.
func (w *masterNatureWorld) legCastSay(l runecraftplan.Leg, why string) {
	key := l.String() + ": " + why
	if w.legCastSaid == nil {
		w.legCastSaid = map[string]bool{}
	}
	if w.legCastSaid[key] {
		return
	}
	w.legCastSaid[key] = true
	w.logf("leg cast: the %s leg is walked without a cast: %s", l, why)
}

// legCastPlan plans the cast on a leg of steps steps, or says nothing is cast (nil). It is asked once the leg is a direct op.
func (w *masterNatureWorld) legCastPlan(l runecraftplan.Leg, steps int) *runecraftrunner.LegCast {
	if !w.legcasts.On() {
		return nil
	}
	d, a := w.d, &w.alch
	if _, ok := w.legcasts.Mode().SpellFor(l); !ok {
		return nil
	}
	if !a.on || !a.arrived || d.Travel == nil {
		w.legCastSay(l, "the gate's alchemy is not running this round")
		return nil
	}
	bow, held := w.legCastBow()
	// The account runs a leg when the toggle is on and it has the energy to: two tiles a tick, a leg over before the lock is.
	energy, known := d.Session.State().Energy()
	running := d.Session.State().RunEnabled() && (!known || energy >= 1)
	view := runecraftplan.LegCastView{Running: running,
		Leg: l, Mode: w.legcasts.Mode(), Hold: w.set.AlchHold, Stack: w.alchStack(), Steps: steps,
		Magic: fleetrunner.MagicLevel(d), MagicXP: fleetrunner.MagicXP(d), FireStaff: w.fireStaff(), Natures: runecraftrunner.Natures(d),
		Locked: d.Session.State().Tick() < a.leg.lockUntil, Busy: randomevents.Busy(d.Session), Shaky: runecraftrunner.Shaky(d),
		PaceTicks: d.Session.State().ViewLag().InWindow,
	}
	dec := runecraftplan.DecideLegCast(view)
	if !dec.Cast {
		w.legCastSay(l, dec.Why)
		return nil
	}
	slot, ok := runecraftrunner.PackSlot(d, bow.Noted)
	if !held || !ok {
		w.legCastSay(l, "no noted bow is in the pack")
		return nil
	}
	// The score's back-off is measured in cycles that would have cast: asked last, once everything else says yes.
	if !w.legcasts.Try(l) {
		return nil
	}
	w.noteHeld() // the bows the idle casts took so far are paid at their rate; the bow this cast takes is booked by legCastBook
	spell, n0 := dec.Spell, runecraftrunner.PackCount(d, bow.Noted)
	return &runecraftrunner.LegCast{
		Spell: spell, Bow: bow.Name,
		Send:   func() error { return d.Travel.CastOn(d, spell.Name, slot) },
		Landed: func() bool { return runecraftrunner.PackCount(d, bow.Noted) < n0 },
	}
}

// legCastBook books a cast that landed: the experience and coins of the round, the stack the idle casts' bookkeeping sees, and the clock
// the idle casts keep their lock by.
func (w *masterNatureWorld) legCastBook(l runecraftplan.Leg, rec runecraftplan.LegCastRecord) {
	a := &w.alch
	pay := rec.Spell.Pays(costOf(rec.Bow))
	a.leg.casts++
	a.leg.xp += rec.Spell.XP
	a.leg.coins += pay
	a.coins += pay
	a.leg.lockUntil = w.d.Session.State().Tick() + rec.Spell.AcceptedAfter()
	if a.held != nil && a.held[rec.Bow] > 0 {
		a.held[rec.Bow]--
	}
	a.castAt = time.Now()
}

// legCastScore scores what a cast on a leg came to and keeps its record for the cycle's line. lc is the cast that was planned (nil: none),
// legOK that the leg's op came off, lag that the view lagged while it was waited for.
func (w *masterRuneWorld) legCastScore(l runecraftplan.Leg, lc *runecraftrunner.LegCast, rec runecraftplan.LegCastRecord, legOK, lag bool) (castFailed bool) {
	if lc == nil || w.legcasts == nil {
		return false
	}
	if rec.Landed > 0 && w.castBook != nil {
		w.castBook(l, rec)
	}
	out := runecraftplan.LegCastOK
	switch {
	case lag && (!rec.Tried || rec.Landed == 0 || !legOK):
		out = runecraftplan.LegCastUnseen
	case !rec.Tried:
		out = runecraftplan.LegCastFailed
	case rec.Landed == 0:
		out = runecraftplan.LegCastFailed
		rec.Why = "the cast was never seen to land"
	case rec.WalkLost:
		out = runecraftplan.LegCastFailed
		rec.Why = "the cast was read in the walk's own tick, which threw the walk away"
	case !legOK || rec.Op == 0:
		out = runecraftplan.LegCastFailed
		rec.Why = "the leg's op did not come off after the cast"
	}
	if out == runecraftplan.LegCastFailed && rec.Why == "" {
		rec.Why = "the cast failed"
	}
	w.legRecs[l], w.legRecSet[l] = rec, true
	word := rec.Spell.Short + " " + map[runecraftplan.LegCastOutcome]string{runecraftplan.LegCastOK: "landed", runecraftplan.LegCastFailed: "failed", runecraftplan.LegCastUnseen: "not seen"}[out]
	w.legs.noteCast(l, word)
	if line := w.legcasts.Note(l, out, rec); line != "" {
		w.logf("%s", line)
	}
	return out == runecraftplan.LegCastFailed
}

// legCastLine is the cycle's line about the casts on its legs, and clears the records: "" when no leg had a cast planned.
func (w *masterRuneWorld) legCastLine() string {
	var parts []string
	for _, l := range []runecraftplan.Leg{runecraftplan.LegAltar, runecraftplan.LegPortal} {
		if w.legRecSet[l] {
			parts = append(parts, w.legRecs[l].String())
		}
	}
	w.legRecSet = [2]bool{}
	if len(parts) == 0 {
		return ""
	}
	casts, xp := w.legcasts.Total()
	return fmt.Sprintf("%s; this round %d cast(s) scored, %s Magic xp", strings.Join(parts, "; "), casts, runecraftplan.Thousands(xp))
}
