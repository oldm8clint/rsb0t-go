package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// loopGate is what ProgressiveWoodcutter and ProgressiveWCFletch remember about the two waits at
// the top of their Loop: a trade screen that is open when the loop comes round, and a snapshot
// refused because the backpack and worn panels cannot be read.
//
// BOTH USED TO BE SILENT AND UNBOUNDED, and the second one cost ~22.6 account-hours live. Up to
// 3dfb543a each was an early return with a short Delay, no log line and no memory: Acct188 came
// back from a Mime on 2026-09-23 with the emote-unlock mesbox open, the server holding its
// backpack and worn tabs null until that mesbox closed, and the loop returning at the refusal
// every 1.2s for twenty minutes -- before the dialogue-continue branch that would have closed
// it. Nothing was written, so nothing noticed; a restart changed nothing, because the new
// instance reached the same refusal. The Guardian now presses a trapped event's reward
// dialogue itself (randomevents/trapreward.go); this is the second layer, for any dialogue that
// holds the tabs back, and the bound that says so out loud.
//
// The refusal itself stays exactly as it was -- deciding a phase while blind is what made a
// wielding account look axeless -- and nothing here changes WHAT the loop decides. It changes
// only what the loop does while it waits:
//
//   - once the blindness has outlasted blindLogAfter it logs the tabs and modals, once -- a ship
//     crossing's own short blind window stays as quiet as it always was;
//   - after blindPressAfter of blindness outside any random event it presses an open dialogue's
//     continue (the mesbox case; one press restored the tabs in 0.2s in the lab), at most
//     blindPressMax times, so a dialogue that keeps reopening cannot keep the account "busy";
//   - after blindCloseAfter, with a modal open and nothing to continue, it closes the modal, at
//     most blindCloseMax times;
//   - after blindLongAfter it writes ONE line saying the account is stuck blind and then stays
//     quiet, so the fleet watchdog (internal/fleet/watchdog.go), which acts on an account that
//     is silent AND making no progress, can relog it -- a fresh login runs ~initalltabs;
//   - the moment the panels are readable again it says how long they were not.
//
// The clock only runs while the account is outside the mime/maze squares and the Guardian is
// idle: during an event the tabs are the event's business, and the Guardian and WaitForGuardian
// both log while it lasts.
type loopGate struct {
	blindSince  time.Time // zero while readable, or while an event owns the account
	blindLogged bool      // this blind spell has been reported (first-seen line, or a press)
	blindLong   bool      // the one "stuck blind" line has been written for this spell
	lastPress   time.Time
	presses     int
	lastClose   time.Time
	closes      int

	tradeSince  time.Time
	tradeLogged bool
	tradeLong   bool
}

const (
	blindLogAfter   = 10 * time.Second
	blindPressAfter = 3 * time.Second
	blindPressEvery = 3 * time.Second
	blindPressMax   = 5
	blindCloseAfter = 60 * time.Second
	blindCloseEvery = 60 * time.Second
	blindCloseMax   = 3
	blindLongAfter  = 5 * time.Minute

	// tradeGateWait is the pass delay while a trade screen is open, unchanged from before.
	tradeGateWait = 2 * time.Second
	// tradeGateLong is when an open trade screen the loop did not start is reported once, and
	// tradeGateDecline when it is declined. This script's own hand-off drives its trades inside
	// one Loop call, so a screen still open at the top of the loop is somebody else's or a
	// leftover; the mule gives up on a trade long before three minutes.
	tradeGateLong    = 60 * time.Second
	tradeGateDecline = 3 * time.Minute
)

// blindObs is what one refused pass saw. Split out so the decision is testable without a session.
type blindObs struct {
	EventOwned  bool // the Guardian is busy, or the account stands in a mime/maze square
	CanContinue bool // an open chat dialogue offers "Click here to continue"
	ModalOpen   bool // a main or chat modal is open
}

// blindStep is what one refused pass should do, besides waiting.
type blindStep struct {
	LogFirst bool
	Press    bool
	Close    bool
	LogLong  bool
}

// planBlind advances the gate's memory for one refused pass and returns what to do. Pure.
func (g *loopGate) planBlind(now time.Time, o blindObs) blindStep {
	var st blindStep
	if o.EventOwned {
		// The event owns the tabs; its own lines cover the wait. Restart the clock afterwards.
		g.blindSince = time.Time{}
		return st
	}
	if g.blindSince.IsZero() {
		g.blindSince = now
		g.presses, g.closes = 0, 0
		g.blindLong = false
	}
	held := now.Sub(g.blindSince)
	if !g.blindLogged && held >= blindLogAfter {
		g.blindLogged = true
		st.LogFirst = true
	}
	switch {
	case o.CanContinue && held >= blindPressAfter && g.presses < blindPressMax && now.Sub(g.lastPress) >= blindPressEvery:
		g.lastPress = now
		g.presses++
		g.blindLogged = true // the press writes its own line, and the recovery line should follow it
		st.Press = true
	case !o.CanContinue && o.ModalOpen && held >= blindCloseAfter && g.closes < blindCloseMax && now.Sub(g.lastClose) >= blindCloseEvery:
		g.lastClose = now
		g.closes++
		st.Close = true
	}
	if held >= blindLongAfter && !g.blindLong {
		g.blindLong = true
		st.LogLong = true
	}
	return st
}

// refused is snapshot's "not ready" answer at the top of Loop: act on planBlind, then wait the
// same 1200ms the loop always waited. An unknown tile keeps its old plain wait.
func (g *loopGate) refused(ctx *Context, d *woodcutrunner.Deps, who string) {
	st := ctx.Session.State()
	x, z, _, tileOK := st.Tile()
	if !tileOK || woodcutrunner.ContainersReadable(d) {
		ctx.Delay(1200 * time.Millisecond)
		return
	}
	main, side, chat := st.Modals()
	var dlg *actions.ChatDialog
	canContinue := false
	if ctx.Components != nil {
		dlg = actions.NewChatDialog(ctx.Session, ctx.Components)
		canContinue = dlg.CanContinue()
	}
	now := time.Now()
	step := g.planBlind(now, blindObs{
		EventOwned:  randomevents.Busy(ctx.Session) || randomevents.InTrapEventSquare(x, z),
		CanContinue: canContinue,
		ModalOpen:   main != -1 || chat != -1,
	})
	texts := ""
	if dlg != nil && chat != -1 {
		texts = strings.Join(dlg.Texts(), " ")
	}
	if step.LogFirst {
		ctx.logf("%s: the backpack and worn panels are not readable (tabs 3/4 = %d/%d, modals main/side/chat = %d/%d/%d) — waiting rather than deciding blind (a random event or a ship crossing nulls them, and an open dialogue holds the server's restore back)",
			who, st.SideIcon(3), st.SideIcon(4), main, side, chat)
	}
	switch {
	case step.Press:
		err := dlg.Continue()
		ctx.logf("%s: pressed continue on %q after %s blind, so the server can send the tabs back (err=%v)", who, texts, now.Sub(g.blindSince).Round(time.Second), err)
	case step.Close:
		err := actions.CloseModal(ctx.Session)
		ctx.logf("%s: closed modal main=%d chat=%d (%q) after %s blind, attempt %d of %d (err=%v)",
			who, main, chat, texts, now.Sub(g.blindSince).Round(time.Second), g.closes, blindCloseMax, err)
	}
	if step.LogLong {
		ctx.logf("%s: STILL BLIND after %s (tabs 3/4 = %d/%d, modals main/side/chat = %d/%d/%d) — this is a stuck interface, not an event; waiting quietly now so the fleet watchdog can relog the account (a fresh login restores the tabs)",
			who, now.Sub(g.blindSince).Round(time.Second), st.SideIcon(3), st.SideIcon(4), main, side, chat)
	}
	ctx.Delay(1200 * time.Millisecond)
}

// readable is called on every pass whose snapshot succeeded. It ends a blind spell, saying how
// long it lasted when the spell was reported.
func (g *loopGate) readable(ctx *Context, who string) {
	if g.blindLogged {
		held := time.Duration(0)
		if !g.blindSince.IsZero() {
			held = time.Since(g.blindSince).Round(time.Second)
		}
		ctx.logf("%s: the backpack and worn panels are readable again (blind for %s outside any event) — carrying on", who, held)
	}
	g.blindSince, g.blindLogged, g.blindLong, g.presses, g.closes = time.Time{}, false, false, 0, 0
}

// tradeStep is what one pass with a trade screen open should do, besides waiting.
type tradeStep struct {
	LogFirst bool
	LogLong  bool
	Decline  bool
}

// planTrade advances the trade half of the gate for one pass. open=false ends the spell. Pure.
func (g *loopGate) planTrade(now time.Time, open bool) tradeStep {
	var st tradeStep
	if !open {
		g.tradeSince, g.tradeLogged, g.tradeLong = time.Time{}, false, false
		return st
	}
	if g.tradeSince.IsZero() {
		g.tradeSince = now
	}
	held := now.Sub(g.tradeSince)
	if !g.tradeLogged {
		g.tradeLogged = true
		st.LogFirst = true
	}
	if held >= tradeGateLong && !g.tradeLong {
		g.tradeLong = true
		st.LogLong = true
	}
	if held >= tradeGateDecline {
		st.Decline = true
		g.tradeSince = now // one decline per window, not one per pass
		g.tradeLong = false
	}
	return st
}

// holdForTrade is the loop's trade-screen wait: true (after waiting) while a trade screen is
// open, false when there is none and the loop should carry on.
func (g *loopGate) holdForTrade(ctx *Context, who string) bool {
	tr := actions.NewTrade(ctx.Session)
	open := tr.Active()
	step := g.planTrade(time.Now(), open)
	if !open {
		return false
	}
	partner := tr.Partner()
	if step.LogFirst {
		ctx.logf("%s: a trade screen is open at the top of the loop (partner %q) — waiting for it to close", who, partner)
	}
	if step.LogLong {
		ctx.logf("%s: the trade screen with %q has been open for %s with nothing of this script's driving it", who, partner, tradeGateLong)
	}
	if step.Decline {
		err := tr.Decline(3 * time.Second)
		ctx.logf("%s: declined the trade with %q after %s open at the top of the loop (err=%v)", who, partner, tradeGateDecline, err)
	}
	ctx.Delay(tradeGateWait)
	return true
}
