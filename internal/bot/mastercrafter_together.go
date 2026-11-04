package bot

// The Nature gate's ONE-WRITE ACCEPT (CR-2 of docs/xp_rate_plan_2026-10-19.md; runecraftplan/together.go has the why, the engine rules and
// the failure, runecraftrunner/together.go the I/O).
//
// When the carrier's accept is on the crafter's confirm screen, his own accept and the talisman on the ruins are written as ONE message, so
// the use is read in the tick the trade completes and the cycle does not wait for the account to SEE the trade close. This file is the
// wiring: the plan for a cycle (is the account where the engine accepts the use, is the view current, is the feature in its back-off), what
// is done with the result (the written use is handed to ToRuins, which waits for it and never writes it a second time unless it did nothing;
// a trade that did not bring the essence while the talisman was on its way is a failed trade and a strike), and the log.
//
// It is an optimisation of a handshake that has a plain way (accept, see the close, use the talisman): every reason not to is the plain way,
// uncounted, and every failure of the written one falls back to it, for ten cycles, and three in a row for the round.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// talismanFresh is how long a talisman written with the accept is waited for by ToRuins: past it the use is forgotten and the plain use is sent.
const talismanFresh = 20 * time.Second

// togetherPlain says why a cycle was accepted the plain way, once a round for each reason.
func (w *masterRuneWorld) togetherPlain(why string) {
	if w.togetherSaid == nil {
		w.togetherSaid = map[string]bool{}
	}
	if w.togetherSaid[why] {
		return
	}
	w.togetherSaid[why] = true
	w.logf("one-write accept: the confirm accept is sent on its own: %s", why)
}

// togetherNote tells the state what a one-write cycle came to and logs what it answers.
func (w *masterRuneWorld) togetherNote(o runecraftplan.DirectOutcome, why string) {
	if line := w.together.Note(o, why); line != "" {
		w.logf("%s", line)
	}
}

// gateTogether is runecraftrunner.GateOptions.Together: nil (the accept on its own) when the feature is off, given up for the round, or the
// ruins are not in the loc index.
func (w *masterNatureWorld) gateTogether() *runecraftrunner.GateTogether {
	if !w.together.On() || w.together.GivenUp() {
		return nil
	}
	a := runecraftplan.Nature
	if !w.haveRuins {
		ruins, ok := runecraftrunner.FindRuins(w.d, a)
		if !ok {
			return nil
		}
		w.ruins, w.haveRuins = ruins, true
	}
	return &runecraftrunner.GateTogether{Altar: a, Ruins: w.ruins, Plan: w.togetherPlan}
}

// togetherPlan is asked once per confirm screen that matches the offer, before the carrier's accept is waited for: whether this cycle is to
// go out as one write. Every reason it says no is a cycle accepted the plain way and not counted; Try counts the back-off.
func (w *masterNatureWorld) togetherPlan() bool {
	d := w.d
	a := runecraftplan.Nature
	why := ""
	switch {
	case runecraftrunner.Shaky(d):
		why = "the view is lagging or was silent in the last minute"
	case w.alchLocked():
		why = "the account is locked by a cast, which would drop the use"
	case w.standTry[a.Key] > 0 || len(w.standBad[a.Key]) > 0:
		why = "a use on the ruins has failed from a stand this round"
	case !w.haveRuins || !runecraftrunner.Reached(d, w.ruins):
		why = "the account does not stand where the engine accepts a use on the ruins (at " + w.hereWord() + ")"
	case runecraftrunner.PackCount(d, int32(a.TalismanID)) == 0:
		why = "the talisman is not in the pack"
	}
	if why != "" {
		w.togetherPlain(why)
		return false
	}
	return w.together.Try()
}

func (w *masterRuneWorld) hereWord() string { return fleetrunner.HereIs(w.d) }

// togetherSeen is what Gate() does with every GateStep result before it reads it: the talisman written with a trade that brought the essence
// is kept for ToRuins to wait on, a plain cycle that was to go out together is counted, and a trade that did not bring the essence is lost.
func (w *masterNatureWorld) togetherSeen(res runecraftrunner.GateResult) {
	w.talisman = nil
	t := res.Together
	switch {
	case t.Sent && res.Kind == runecraftrunner.GateReceived:
		w.talisman = t.Talisman
	case t.WaitedOut:
		if line := w.together.NoteWaitedOut(); line != "" {
			w.logf("%s", line)
		}
		w.togetherPlain(t.Why)
	case t.Planned && t.Why != "":
		w.togetherPlain(t.Why)
	}
}

// togetherWord is the tail of the trade line: what the one-write accept did in this handshake ("" when it was not in play).
func togetherWord(res runecraftrunner.GateResult) string {
	t := res.Together
	switch {
	case t.Sent:
		written := 0
		if t.Talisman != nil {
			written = t.Talisman.Tick
		}
		return "; one write: accept and talisman in tick " + strconv.Itoa(written) + " (confirm screen seen in " + strconv.Itoa(t.ConfirmTick) + ", the carrier's accept in " + strconv.Itoa(t.OtherTick) + ")"
	case t.WaitedOut:
		return "; plain accept (the carrier's accept did not show in " + runecraftplan.GateTogetherWait.String() + ")"
	case t.Planned:
		return "; plain accept (" + t.Why + ")"
	}
	return ""
}

// togetherLost is the cycle whose talisman went with the accept and whose trade did not bring the essence: the carrier closed the screen in
// the same tick, or the accept was read before his. The account teleports into the temple with an empty pack and leaves by the portal like
// any cycle with nothing to craft. It is a failed trade and a strike, whatever the view did, and it puts the one-write accept in its back-off.
func (w *masterNatureWorld) togetherLost(res runecraftrunner.GateResult) runeStepResult {
	why := strings.TrimSpace(res.Reason)
	w.togetherNote(runecraftplan.DirectFailed, "the trade with "+res.Partner+" did not bring the essence: "+why)
	w.failedTrades++
	w.logf("WARNING: trade with %s failed (%d in a row): %s", res.Partner, w.failedTrades, why)
	// The use is on its way: let the account land before the next stage is decided from where it stands.
	a := runecraftplan.Nature
	w.ctx.DelayUntil(func() bool { return a.Inside(runecraftrunner.Here(w.d)) }, 5*time.Second)
	if w.failedTrades >= natureTradeFails {
		return runeFailf("%d trades in a row failed, the last with %s: %s", w.failedTrades, res.Partner, why)
	}
	return runeOK()
}

// takeTalisman is the use written with the last trade's accept, if it is still the one to wait for. It is in flight when ToRuins runs (it
// was read in the tick the trade completed and lands about a tick after the close), so the account can already be INSIDE the temple, or
// land while the stage is being decided or the view is waited for: landed says so, and then nothing is walked or used (talismanHadLanded).
// Otherwise it is not older than talismanFresh and the account still stands where it was written from; nil: ToRuins uses the talisman
// the plain way.
func (w *masterRuneWorld) takeTalisman(a runecraftplan.Altar, ruins locindex.Match) (s *runecraftrunner.TalismanSent, landed bool) {
	s = w.talisman
	w.talisman = nil
	if s == nil {
		return nil, false
	}
	if a.Inside(runecraftrunner.Here(w.d)) {
		return s, true
	}
	if time.Since(s.At) > talismanFresh {
		return w.forgetTalisman(s), false
	}
	return w.recheckTalisman(a, ruins, s)
}

// recheckTalisman asks the same of a written talisman again once the view has been waited for (runecraftrunner.AwaitView): a lagging view
// delivers the landing, or the account's displacement, only when it catches up. It does not look at the age again: a use written a moment
// ago is not forgotten because the wait was long, since it may still land.
func (w *masterRuneWorld) recheckTalisman(a runecraftplan.Altar, ruins locindex.Match, s *runecraftrunner.TalismanSent) (*runecraftrunner.TalismanSent, bool) {
	switch {
	case s == nil:
		return nil, false
	case a.Inside(runecraftrunner.Here(w.d)):
		return s, true
	case !runecraftrunner.Reached(w.d, ruins):
		return w.forgetTalisman(s), false
	}
	return s, false
}

// forgetTalisman drops a written talisman that cannot be waited for (too old, or the account was moved off the stand it was written from):
// the plain use follows. It is neither a success nor a failure of the one-write accept.
func (w *masterRuneWorld) forgetTalisman(s *runecraftrunner.TalismanSent) *runecraftrunner.TalismanSent {
	w.logf("one-write accept: the talisman written with the accept is not waited for (written %s ago, account at %s): using it the plain way", time.Since(s.At).Round(time.Second), w.hereWord())
	w.togetherNote(runecraftplan.DirectUnseen, "")
	return nil
}

// talismanHadLanded is ToRuins for a written talisman that has already taken the account into the temple: the stage is done, whatever the
// view showed when it was decided. Nothing is walked (from inside the temple the ruins are over a hundred tiles and no walk away) and no
// second use is written; the loop goes on to the craft.
func (w *masterRuneWorld) talismanHadLanded(a runecraftplan.Altar, s *runecraftrunner.TalismanSent) runeStepResult {
	here := runecraftrunner.Here(w.d)
	ago := time.Since(s.At)
	w.legs.enter(runecraftplan.StageUseTalisman)
	w.standTry[a.Key] = 0
	delete(w.standBad, a.Key)
	w.logf("%s ruins: the %s written with the trade's accept had taken the account inside before the stage ran (at (%d,%d), %s after it was written): nothing is walked or used again",
		a.Name, a.Talisman, here.X, here.Z, ago.Round(100*time.Millisecond))
	if ago > talismanFresh {
		// Landed, but so long after the write that the write is not what is known to have done it.
		w.togetherNote(runecraftplan.DirectUnseen, "")
	} else {
		w.togetherNote(runecraftplan.DirectOK, "")
	}
	return runeOK()
}

// togetherLanded scores the written talisman from what UseTalismanSent saw of it.
func (w *masterRuneWorld) togetherLanded(res runecraftrunner.TalismanResult) {
	switch {
	case res.Arrived && res.Clicks == 1:
		w.togetherNote(runecraftplan.DirectOK, "")
	case res.Arrived:
		w.togetherNote(runecraftplan.DirectFailed, "the use written with the accept did nothing, so a second use was sent after the wait")
	case res.Lag:
		w.togetherNote(runecraftplan.DirectUnseen, res.Reason)
	default:
		w.togetherNote(runecraftplan.DirectFailed, res.Reason)
	}
}

// Craft is the gate's stage inside the temple. A talisman written with the accept that ToRuins never waited on (the account was inside the
// temple before the stage was decided) has landed: it is scored here.
func (w *masterNatureWorld) Craft(st runecraftplan.Step) runeStepResult {
	if w.talisman != nil {
		w.talisman = nil
		w.logf("one-write accept: the talisman written with the accept had landed before the stage was decided")
		w.togetherNote(runecraftplan.DirectOK, "")
	}
	return w.masterRuneWorld.Craft(st)
}
