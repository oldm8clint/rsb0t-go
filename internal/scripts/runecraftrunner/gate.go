package runecraftrunner

// The Nature gate's trade (runecraft P3): the crafter's half, GateStep, and the carrier's, TradeWithCrafter.
//
// WHAT THE ENGINE DOES (content 289 interface_trade/scripts/trade.rs2, read for this step):
//
//   - [opplayer4] "Trade with" is refused in the duel arena and when the TARGET is busy (a craft's four-tick hold, an open modal):
//     "<name> is busy at the moment", and nothing else happens. Otherwise it sets this player's %tradepartner, and when the target
//     had already asked this player the screens open on both sides; if not, the target is sent the raw line "<name>:tradereq:"
//     and is marked as asked. So the screen opens when BOTH have asked, in either order: the carrier asks, the crafter answers.
//   - a refused request leaves no trace on the target, so a carrier that asks while the crafter is inside the temple costs nothing:
//     it asks again every GateRequestEvery (2 ticks) and the first request after the crafter is free and back at the gate is the
//     one he sees.
//   - accepting is [if_button,trademain:accept]: the engine checks both packs have room for what is coming (enough_trade_space;
//     25 essence into a pack of 25 free slots is exactly enough) and, with both accepted, opens the confirm screens.
//   - Offer-All is an inventory button with no delay: it moves every copy of the object in one handler, so 25 essence are on the
//     screen in the tick after the click.
//
//   - ANY op on a player (OPPLAYER1..5) and any walk that is not the walk half of an op (MOVE_GAMECLICK, MOVE_MINIMAPCLICK) calls
//     player.clearPendingAction() the moment the packet is DECODED (OpPlayerHandler, MoveClickHandler), which is clearInteraction()
//     plus closeModal(). closeModal on the trade screen runs [if_close,trademain]: the offer returns to its owner, the partner's
//     screen is closed too and told "Other player declined trade." So a request or a walk sent by EITHER player once the screen is
//     open closes it for both, and "once it is open" starts on the server a tick and a latency before the sender can see it.
//     Live 16 Oct 2026: 17 of 85 handshakes were closed about 0.4 s after they opened, every one by a timed request that was
//     already on the wire. THE RULE OF THIS FILE: after a request has been EXECUTED ("Sending trade offer..."), neither side sends
//     anything that clears a pending action until the screen is seen or a quiet period (runecraftplan.GateRequestQuiet) is over.
//
// THE ONE EXCEPTION is the crafter's talisman (together.go): an op on a loc is such a packet too, and it is written WITH his confirm accept, in
// the same message, only when his screen says "Other player has accepted.": his accept is then the second, it completes the trade inside its
// own packet (both_moveinv, if_close), and the use that follows it in the same tick finds nothing to close.
//
// THE CRAFTER (GateStep) waits for a ":tradereq:" line from one of his carriers (the allow-list is the craftRuneRunners setting
// plus whoever is at work on the bus), answers it ONCE, and takes the offer when it has settled for GateSettle (DecideGateTrade).
// A line is answered once: a newer line from the same carrier (it asked again) is answered again, nothing else is, and while an
// answer is awaited (gateAskWindow) he sends no other request and no walk (Options.Idle). One request is answered at a time: a
// burst of replies corrupts whichever screen opens (the mule's lesson, mulerunner RunCycle).
//
// THE CARRIER (TradeWithCrafter) asks the moment the crafter is in view, then again only by what the game said of the request
// (runecraftplan.GateRequestEvery after a refusal, GateRequestPending when nothing was said, GateRequestQuiet once it was
// executed; at once when the crafter's reverse ":tradereq:" line says his answer found no request to open), offers everything
// with Offer-All, accepts, checks the confirm screen against what it offered, accepts, and counts the essence that left its pack.
// It offers everything unless the crafter says he takes less (OfferOptions.Cap, his gate message's capacity: a stack of bows or a
// random event's fruit takes one of the slots a trade of 25 needs): then it offers exactly that with Offer-X and keeps the rest for the
// next load (the shelf tops a pack up to the load, runecraftplan.PlanShelf). The engine refuses an offer the receiver has no room for
// at the accept (enough_trade_space) and the crafter declines it first, so an Offer-All of 25 into 24 free slots is declined every
// time (live 16 Oct 2026: a Strange fruit from a random event, 110 declines, six carriers' shifts ended within 70 s).
// A screen that closes before anyone accepted is a CLOSE (OfferResult.Closed), not a failure: nothing crossed. A courier takes
// Natures instead: it offers nothing and takes what the crafter puts up.
//
// Nothing is done until it is OBSERVED (docs/trustworthy_harnesses.md): a trade counts when the pack changed by what the
// confirm screen showed.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// gateClockScale multiplies the durations the trade code measures before it hands them to the plan's thresholds; a test sets it
// above 1 so a settle of 1.2 seconds takes a few milliseconds. It is 1 in the fleet.
var gateClockScale = 1.0

func scaled(d time.Duration) time.Duration { return time.Duration(float64(d) * gateClockScale) }

// NatureID is the Nature rune object.
var NatureID = int32(runecraftplan.Nature.RuneID)

// Natures is the Nature runes in the pack.
func Natures(d *fleetrunner.Deps) int { return PackCount(d, NatureID) }

// the trade code's polls and bounds. Variables so a test can shorten them.
var (
	gatePoll = 100 * time.Millisecond
	// gateHeaderWait is how long a new screen is given to show the partner's name; gateConfirmWait how long the confirm screen
	// is waited on for the other side's accept; gateCloseWait how long a closing trade is waited on; gateArriveWait how long
	// the changed pack is waited on after the screen closed.
	gateHeaderWait  = 3 * time.Second
	gateConfirmWait = 30 * time.Second
	gateCloseWait   = 20 * time.Second
	gateArriveWait  = 5 * time.Second
	// gateAskWindow is how long after answering a request the crafter waits for the screen before answering the next or sending
	// anything else: six ticks. His answer is executed one to four ticks after he sends it (the asker is beside him or still
	// walking up), so a window of three ticks ended while the screen could still be about to open, and the next answer closed it.
	gateAskWindow = 3600 * time.Millisecond
	// gateRequestLife is how long a request line is answered: a carrier that asked a quarter minute ago has gone.
	gateRequestLife = 15 * time.Second
	// gateOfferWait is how long the carrier's Offer-All is waited on to show on its side of the screen, gateOfferRetry how soon it
	// is sent again while that side is still empty; gateFlicker how long a screen that vanished after an accept is given to come
	// back as the confirm screen.
	gateOfferWait  = 3 * time.Second
	gateOfferRetry = 1200 * time.Millisecond
	gateFlicker    = 600 * time.Millisecond
)

// ---- the crafter ----

// GateWatcher remembers how far into the chat log the crafter has read and whom he has answered.
type GateWatcher struct {
	cursor int
	// asks are the requests not yet gone, in the order they first arrived (a carrier that asks again keeps its place).
	asks []gateAsk
	// awaiting is the carrier answered last and awaitingUntil when its window closes.
	awaiting      string
	awaitingUntil time.Time
	// lastLine is when a request line from one of his carriers was last read (zero: none yet).
	lastLine time.Time
}

// LastRequest is when a ":tradereq:" line from one of his carriers was last read. A request is only executed by the engine when the
// asker reaches him, so a gate that waits with carriers at it and no line coming is a gate they cannot ask (gatestall.go).
func (w *GateWatcher) LastRequest() time.Time { return w.lastLine }

// gateAsk is one carrier's request. A request is answered ONCE: answered is set when it has been, and a newer line from the same
// carrier (it asked again, so its earlier request is not the one that stands) clears it.
type gateAsk struct {
	name     string
	at       time.Time
	answered bool
	// askTick and answerTick are the server ticks the line was read in and the answer sent in (for the trade's timing line).
	askTick, answerTick int
}

// NewGateWatcher starts at the end of the chat log: requests from before the gate opened are not answered.
func NewGateWatcher(d *fleetrunner.Deps) *GateWatcher {
	w := &GateWatcher{}
	if d.Chat != nil {
		w.cursor = d.Chat.Total()
	}
	return w
}

// Reset forgets every request and starts again at the end of the chat log. A request line is only good for the gate it was sent at:
// the engine's own state for it (his trade status) is reset when a screen opens, so a line read after he has been away (the temple,
// a trade with somebody else) is answered for nothing, and the answer makes HIM the asker: no screen opens until the carrier asks
// again. The carriers ask again the moment he is back in view (TradeWithCrafter), so nothing valid is lost.
func (w *GateWatcher) Reset(d *fleetrunner.Deps) {
	w.asks = nil
	w.awaiting, w.awaitingUntil = "", time.Time{}
	if d.Chat != nil {
		w.cursor = d.Chat.Total()
	}
}

// Pending is how many requests wait to be answered.
func (w *GateWatcher) Pending() int {
	n := 0
	for _, a := range w.asks {
		if !a.answered {
			n++
		}
	}
	return n
}

// Awaiting says an answer is out and its screen has not been seen: nothing that clears a pending action may be sent now.
func (w *GateWatcher) Awaiting() bool { return w.awaiting != "" }

// poll reads the new request lines and queues the allowed ones (a name already queued keeps its place and takes the newer time,
// and is answered again).
func (w *GateWatcher) poll(d *fleetrunner.Deps, allowed func(string) bool, now time.Time, tick int) (strangers []string) {
	if d.Chat == nil {
		return nil
	}
	lines, next := d.Chat.ReadAfter(w.cursor, time.Time{})
	w.cursor = next
	for _, line := range lines {
		name, ok := mulerunner.TradeRequestFrom(line)
		if !ok {
			continue
		}
		if allowed != nil && !allowed(name) {
			strangers = append(strangers, name)
			continue
		}
		w.lastLine = now
		found := false
		for i := range w.asks {
			if actions.SameUsername(w.asks[i].name, name) {
				w.asks[i].at, w.asks[i].answered, w.asks[i].askTick = now, false, tick
				found = true
			}
		}
		if !found {
			w.asks = append(w.asks, gateAsk{name: name, at: now, askTick: tick})
		}
	}
	// A request older than its life is dropped.
	kept := w.asks[:0]
	for _, a := range w.asks {
		if now.Sub(a.at) < gateRequestLife {
			kept = append(kept, a)
		}
	}
	w.asks = kept
	return strangers
}

// next is the first request still to be answered.
func (w *GateWatcher) next() *gateAsk {
	for i := range w.asks {
		if !w.asks[i].answered {
			return &w.asks[i]
		}
	}
	return nil
}

// expected is the carrier a screen that has just opened most likely belongs to: the one answered last, else the first answered.
func (w *GateWatcher) expected() string {
	if w.awaiting != "" {
		return w.awaiting
	}
	for _, a := range w.asks {
		if a.answered {
			return a.name
		}
	}
	return ""
}

// ticks returns the ticks recorded for name's request.
func (w *GateWatcher) ticks(name string) (ask, answer int) {
	for _, a := range w.asks {
		if actions.SameUsername(a.name, name) {
			return a.askTick, a.answerTick
		}
	}
	return 0, 0
}

func (w *GateWatcher) done(name string) {
	kept := w.asks[:0]
	for _, a := range w.asks {
		if !actions.SameUsername(a.name, name) {
			kept = append(kept, a)
		}
	}
	w.asks = kept
	w.awaiting, w.awaitingUntil = "", time.Time{}
}

// GateOptions are the crafter's policy for one GateStep.
type GateOptions struct {
	// Allowed says a name is one of his carriers; nil allows everyone (a test).
	Allowed func(name string) bool
	// CourierQty is the Natures the named partner may take now: 0 when it is not a courier or there is nothing to give.
	CourierQty func(name string) int
	// Ending says the round is ending: a short offer is taken at once.
	Ending func() bool
	// Wait is how long GateStep waits for a request before it returns with nothing done.
	Wait time.Duration
	// OnStand says he stands on his stand tile and Idle walks him there: Idle is called while no answer is awaited (never when one
	// is out: a walk then would close the screen it opens), and a request is answered once he is on the stand (or has waited
	// runecraftplan.GateStandHold for it), so the engine never walks him after the carrier. Both nil: no stand.
	OnStand func() bool
	Idle    func()
	// Hold says no request may be answered now: a stack of bows is being cast away and a trade of 25 essence would be refused for want
	// of a slot (runecraftplan.BurnDown). Delayed says the account is locked by a cast: the engine drops an answer sent in the lock
	// without a word, so it is held until the lock is over. A held request stays queued.
	Hold, Delayed func() bool
	// Together, when set, lets the confirm screen's Accept go out in the same message as the talisman on the ruins once the carrier's
	// accept is on the screen (together.go). Nil: the accept on its own, the moment the screen matches the offer, as before.
	Together *GateTogether
}

// GateKind is what a GateStep did.
type GateKind string

// The results.
const (
	GateNothing  GateKind = "nothing"
	GateReceived GateKind = "received"
	GateGave     GateKind = "gave"
	GateRefused  GateKind = "refused"
	GateFailed   GateKind = "failed"
	// GateClosed: the screen closed before anybody accepted, so nothing crossed (the partner's or his own stray packet; see the file
	// comment). It is not a failure of the trade and not counted as one.
	GateClosed GateKind = "closed"
)

// GateTicks are the server ticks of one handshake: the request line read, the answer sent, the screen first seen, the trade closed.
type GateTicks struct{ Ask, Answer, Open, Close int }

// GateResult is one GateStep.
type GateResult struct {
	Kind    GateKind
	Partner string
	// Essence is what arrived, Natures what he handed over; Slots the stacks on the screen.
	Essence, Natures int
	// Took is the time from the screen opening to the trade closing; Reason says what went wrong.
	Took    time.Duration
	Reason  string
	Strange []string
	// Ticks are the handshake's ticks (zero when the screen was not one he answered).
	Ticks GateTicks
	// Together is what the one-write accept did (zero when GateOptions.Together was nil). Together.Sent with a Kind that is not GateReceived
	// is a trade that did not bring the essence while the talisman was already on its way: the account teleports into the temple with an
	// empty pack.
	Together TogetherInfo
	// Lag is set when the view of the game was lagging, or had been silent in the last shakyFor, at any point of the handshake
	// (lagwait.go, Shaky). A trade that failed with Lag says nothing about the gate: a screen read late shows a half-made offer, a
	// confirm screen that "does not match" is the previous frame's, and the carrier's accept is read after our decline.
	// 18 Oct 2026 19:46-19:52: ten trades in a row failed that way in a view that went silent every few seconds, the round ended on them,
	// and the crafter walked home and backed off for an hour.
	Lag bool
}

// GateStep waits up to o.Wait for a carrier's request and carries a trade through, or returns with GateNothing. It never leaves
// a screen open.
func GateStep(d *fleetrunner.Deps, w *GateWatcher, o GateOptions) (res GateResult) {
	res.Kind = GateNothing
	sess := d.Session
	st := sess.State()
	trade := actions.NewTrade(sess)
	deadline := time.Now().Add(o.Wait)
	for {
		if randomevents.Busy(sess) {
			return res
		}
		now := time.Now()
		if trade.Active() {
			expected := w.expected()
			if expected == "" && len(w.asks) > 0 {
				expected = w.asks[0].name
			}
			started := now
			openTick := st.Tick()
			r := gateScreen(d, trade, w, o, expected)
			r.Took = time.Since(started)
			if r.Partner != "" {
				ask, answer := w.ticks(r.Partner)
				r.Ticks = GateTicks{Ask: ask, Answer: answer, Open: openTick, Close: st.Tick()}
				w.done(r.Partner)
			}
			w.awaiting, w.awaitingUntil = "", time.Time{}
			return r
		}
		res.Strange = append(res.Strange, w.poll(d, o.Allowed, now, st.Tick())...)
		if w.awaiting != "" && !now.Before(w.awaitingUntil) {
			w.awaiting = ""
		}
		if w.awaiting == "" {
			if o.Idle != nil {
				o.Idle()
			}
			if ask := w.next(); ask != nil {
				onStand := o.OnStand == nil || o.OnStand() || now.Sub(ask.at) >= scaledDown(runecraftplan.GateStandHold)
				held := (o.Hold != nil && o.Hold()) || (o.Delayed != nil && o.Delayed())
				if onStand && !held {
					err := trade.RequestPlayer(ask.name)
					switch {
					case err == nil:
						ask.answered, ask.answerTick = true, st.Tick()
						w.awaiting, w.awaitingUntil = ask.name, now.Add(gateAskWindow)
					case errors.Is(err, actions.ErrPlayerNotVisible) && now.Sub(ask.at) > 3*time.Second:
						// A carrier who asked and is no longer in view has gone; one just arrived may not be in the player list yet.
						w.done(ask.name)
					}
				}
			}
		}
		if !now.Before(deadline) {
			return res
		}
		if !d.Delay(gatePoll) {
			return res
		}
	}
}

// offerSummary reads a side of the screen: the unnoted essence, the Natures, anything else.
func offerSummary(items []gamestate.SlotItem) (essence, natures, other int) {
	for _, it := range items {
		n := max(1, int(it.Count))
		switch int(it.ID) {
		case runecraftplan.EssenceID:
			essence += n
		case runecraftplan.Nature.RuneID:
			natures += n
		default:
			other += n
		}
	}
	return essence, natures, other
}

// sayWord is what the game said since t, as a parenthesis for a reason ("" when it said nothing). "Other player declined trade."
// names the victim of a stray close: the OTHER side sent the packet that closed the screen.
func sayWord(d *fleetrunner.Deps, t time.Time) string {
	if s := said(d, t); s != "" {
		return " (the game said: " + s + ")"
	}
	return ""
}

// declined reports the game saying the other side closed the screen since t.
func declined(d *fleetrunner.Deps, t time.Time) bool {
	return strings.Contains(strings.ToLower(said(d, t)), "declined")
}

// gateScreen carries the open offer screen to its end: the crafter's side of a receive or of a courier hand-over.
func gateScreen(d *fleetrunner.Deps, trade *actions.Trade, w *GateWatcher, o GateOptions, expected string) (res GateResult) {
	res.Kind = GateFailed
	opened := time.Now()
	// Whether the view lagged at any look of the handshake (every poll below looks once): a failure is then the view's, not the gate's.
	lagged := Shaky(d)
	defer func() { res.Lag = res.Lag || lagged || Shaky(d) }()
	// The header is set before the screen opens (trade.rs2 sets the text, then opens the interface), so a name that is not the
	// one answered is somebody else's screen, not a stale one: it is read, not waited for.
	waitUntil(d, func() bool { return trade.Partner() != "" }, gateHeaderWait)
	partner := trade.Partner()
	res.Partner = partner
	if expected != "" && !actions.SameUsername(partner, expected) {
		logf(d, "gate: the trade screen shows %q, not %q whom I answered", partner, expected)
	}
	if o.Allowed != nil && !o.Allowed(partner) {
		_ = trade.Decline(3 * time.Second)
		res.Kind, res.Reason = GateRefused, partner+" is not one of the carriers"
		return res
	}
	essence0, natures0 := Essence(d), Natures(d)
	free := PackFree(d)
	var lastSig string
	lastChange := time.Now()
	acceptedSig := ""
	var theirEss, gaveQty int
	// The one-write accept: planAsked is set when the plan has been asked for this screen, planned its answer, confirmSince when the
	// matching confirm screen was first seen (the wait for the carrier's accept is timed from it).
	var planAsked, planned bool
	var confirmSince time.Time
	// finishConfirm waits for the trade to close after a confirm accept (plain or with the talisman) and judges the pack.
	finishConfirm := func(what string) GateResult {
		if !waitUntil(d, func() bool { return !trade.Active() }, gateCloseWait) {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "the trade never closed after the " + what
			return res
		}
		r := gateAfter(d, res, essence0, natures0, theirEss, gaveQty, partner, opened)
		if r.Together.Sent && r.Kind != GateReceived {
			r.Reason += " (the talisman was written with the accept, so the account teleports into the temple without the essence)"
		}
		return r
	}
	for {
		if !lagged {
			lagged = Shaky(d)
		}
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "a random event interrupted the trade"
			return res
		}
		if !trade.Active() {
			if acceptedSig == "" {
				// Nobody has accepted, so nothing can have crossed: the screen was closed on us.
				res.Kind = GateClosed
				res.Reason = "the screen closed before an accept" + sayWord(d, opened)
				return res
			}
			// A flicker is not a close (the mule's lesson, mulerunner stillClosed): the offer screen goes and the confirm screen comes
			// after both have accepted.
			if !waitUntil(d, trade.Active, gateFlicker) {
				return gateAfter(d, res, essence0, natures0, theirEss, gaveQty, partner, opened)
			}
		}
		if trade.OnConfirmScreen() {
			mine, theirs := trade.ConfirmOffers()
			me, mn, mo := offerSummary(mine)
			te, tn, to := offerSummary(theirs)
			ok := false
			switch {
			case theirEss > 0:
				ok = me == 0 && mn == 0 && mo == 0 && to == 0 && tn == 0 && te == theirEss
			case gaveQty > 0:
				ok = me == 0 && mo == 0 && mn == gaveQty && te == 0 && tn == 0 && to == 0
			}
			if !ok {
				_ = trade.Decline(3 * time.Second)
				res.Reason = fmt.Sprintf("the confirm screen does not match the offer (mine %d/%d/%d, theirs %d/%d/%d; expected %d essence in, %d Natures out)", me, mn, mo, te, tn, to, theirEss, gaveQty)
				return res
			}
			if o.Together != nil && theirEss > 0 {
				if !planAsked {
					planAsked, confirmSince = true, time.Now()
					res.Together.ConfirmTick = d.Session.State().Tick()
					planned = o.Together.Plan != nil && o.Together.Plan()
					res.Together.Planned = planned
				}
				if planned {
					// The offers are the trade's and the carrier's accept is on the screen (read AFTER the offers: the engine resets the line
					// before it transmits them, so a line read now is this screen's own): the accept is the second one.
					if trade.OtherAccepted() {
						res.Together.OtherTick = d.Session.State().Tick()
						sent, held, err := o.Together.send(d)
						switch {
						case sent != nil:
							res.Together.Sent, res.Together.Talisman = true, sent
							acceptedSig = "confirm"
							return finishConfirm("one-write accept")
						case held:
							res.Together.Why = "another caller holds the account's input"
						case err != nil:
							res.Together.Why = "the accept and the talisman could not be written together: " + err.Error()
						}
						planned = false
					} else if wait := scaledDown(runecraftplan.GateTogetherWait) - time.Since(confirmSince); wait > 0 {
						// The carrier has not accepted yet: wait for the line, not for the poll. Whatever ends the wait is looked at afresh.
						waitUntil(d, func() bool {
							return !trade.Active() || !trade.OnConfirmScreen() || trade.OtherAccepted() || randomevents.Busy(d.Session)
						}, wait)
						if stopped(d) {
							_ = trade.Decline(3 * time.Second)
							res.Reason = "stopped"
							return res
						}
						continue
					} else {
						res.Together.WaitedOut = true
						res.Together.Why = "the carrier's accept was not on the screen after " + runecraftplan.GateTogetherWait.String()
						planned = false
					}
				}
			}
			if err := trade.Accept(); err != nil {
				res.Reason = "accept (confirm screen): " + err.Error()
				return res
			}
			return finishConfirm("confirm accept")
		}
		// The offer screen.
		theirs, mine := trade.TheirOffer(), trade.MyOffer()
		te, tn, to := offerSummary(theirs)
		_, mn, mo := offerSummary(mine)
		sig := fmt.Sprintf("%d/%d/%d|%d/%d", te, tn, to, mn, mo)
		if sig != lastSig {
			lastSig, lastChange = sig, time.Now()
		}
		courier := 0
		if o.CourierQty != nil {
			courier = o.CourierQty(partner)
		}
		view := runecraftplan.GateTradeView{
			Allowed: true, Partner: partner, TheirEssence: te, TheirOther: to + tn, MyOffer: mn + mo, FreeSlots: free,
			Courier: courier > 0, CourierQty: min(courier, max(Natures(d), 0)),
			Opened: scaled(time.Since(opened)), Settled: scaled(time.Since(lastChange)),
			GateEnding: o.Ending != nil && o.Ending(),
		}
		act := runecraftplan.DecideGateTrade(view)
		switch act.Kind {
		case runecraftplan.GateTradeDecline:
			_ = trade.Decline(3 * time.Second)
			res.Kind, res.Reason = GateRefused, act.Reason
			return res
		case runecraftplan.GateTradeGive:
			if mn == 0 && gaveQty == 0 {
				if err := trade.Offer(NatureID, int32(act.Qty), 3*time.Second); err != nil {
					_ = trade.Decline(3 * time.Second)
					res.Reason = "offering the Natures: " + err.Error()
					return res
				}
				gaveQty = act.Qty
				lastChange = time.Now()
			}
		case runecraftplan.GateTradeAccept:
			if acceptedSig != sig {
				if mn > 0 {
					gaveQty = mn
				}
				theirEss = te
				if err := trade.Accept(); err != nil {
					res.Reason = "accept (offer screen): " + err.Error()
					return res
				}
				acceptedSig = sig
			}
		}
		if time.Since(opened) > gateConfirmWait+gateCloseWait {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "the trade took longer than " + (gateConfirmWait + gateCloseWait).String()
			return res
		}
		// The poll comes AFTER the look: an offer already on the screen is accepted in the tick it is seen in.
		if !d.Delay(gatePoll) {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "stopped"
			return res
		}
	}
}

// gateAfter judges a closed trade by the pack: essence arrived, Natures left, or nothing came of it. A close the game says the other
// side made ("Other player declined trade.") is not waited on.
func gateAfter(d *fleetrunner.Deps, res GateResult, essence0, natures0, wantEss, wantNat int, partner string, opened time.Time) GateResult {
	waitUntil(d, func() bool { return Essence(d) > essence0 || Natures(d) < natures0 || declined(d, opened) }, gateArriveWait)
	gotEss, gaveNat := Essence(d)-essence0, natures0-Natures(d)
	res.Partner = partner
	switch {
	case wantEss > 0 && gotEss >= wantEss:
		res.Kind, res.Essence = GateReceived, gotEss
	case wantNat > 0 && gaveNat >= wantNat:
		res.Kind, res.Natures = GateGave, gaveNat
	default:
		res.Kind = GateFailed
		res.Reason = fmt.Sprintf("the trade closed and the pack gained %d essence (wanted %d) and gave %d Natures (wanted %d)%s", gotEss, wantEss, gaveNat, wantNat, sayWord(d, opened))
	}
	return res
}

// ---- the carrier ----

// OfferMode is what a carrier's trade is for.
type OfferMode int

// The modes.
const (
	// OfferEssence trades the whole unnoted load to the crafter.
	OfferEssence OfferMode = iota
	// TakeNatures takes what the crafter puts up (a courier).
	TakeNatures
)

// OfferOptions are one TradeWithCrafter's policy.
type OfferOptions struct {
	Crafter string
	Mode    OfferMode
	// Timeout bounds the whole attempt, ask included; Absent is how long the crafter may be out of view before the call gives up
	// with NotHere (0: it asks as long as Timeout).
	Timeout, Absent time.Duration
	// Stand is the tile the crafter waits on (zero: none). A carrier standing beside it holds its first request until he is on it
	// (runecraftplan.GateStandHold at most), so the engine does not walk it up to him.
	Stand tile.Tile
	// Cap, when set, says how much essence the crafter takes in one trade right now (his gate message; 0: not said), read when the offer
	// is made. A carrier holding more offers Cap and keeps the rest. A value under runecraftplan.MinLoad is not a trade worth making
	// and is ignored.
	Cap func() int
}

// takes is the essence the crafter says he takes in one trade (0: not said).
func (o OfferOptions) takes() int {
	if o.Cap == nil {
		return 0
	}
	return o.Cap()
}

// OfferResult is one TradeWithCrafter.
type OfferResult struct {
	Done bool
	// Moved is the essence that left the pack (OfferEssence) or the Natures that arrived (TakeNatures).
	Moved int
	// NotHere is set when the crafter was not in view; NoScreen when he was but no trade screen ever opened (he was busy, or
	// another carrier had him); Closed when a screen opened and closed again before anybody accepted (nothing crossed: ask again);
	// Requests how many times the carrier asked; Took the whole attempt.
	NotHere, NoScreen, Closed bool
	Requests                  int
	Took                      time.Duration
	Reason                    string
	// Declined is set when the trade closed with nothing moved and the game said the other side declined it ("Other player declined
	// trade."): the crafter's own decision (he had no room for the offer, say), not a fault of the handshake.
	Declined bool
	// Kept is the essence the carrier held back because the crafter said he takes less than it held (OfferOptions.Cap).
	Kept int
	// Heard is set when the game said anything at all of the requests during the attempt (executed, busy, the crafter's reverse line,
	// "I can't reach that!"). Requests sent and Heard false is a crafter nobody can ask: Silent. CrafterAt and StoodAt are where he and
	// the carrier stood when the attempt ended.
	Heard, Silent      bool
	CrafterAt, StoodAt tile.Tile
	// Moves is how many times the carrier walked to another tile beside him because the one it stood on was a dead end (the engine's
	// walk to him parks against a wall and says nothing), MovedTo the last tile and MoveWhy the reason.
	Moves   int
	MovedTo tile.Tile
	MoveWhy string
}

// askHeard is what the game has said of the last request since it was sent.
type askHeard struct {
	// executed: "Sending trade offer..." (the request ran and the crafter was told); busy: "<name> is busy at the moment." (it did not
	// run); reverse: the crafter's own ":tradereq:" line (his answer found no request of ours standing and made him the asker, so ONE
	// request from us opens the screen); cantReach: "I can't reach that!" (the engine gave the walk up).
	executed, busy, reverse, cantReach bool
}

// any says the game answered the request in some way.
func (h askHeard) any() bool { return h.executed || h.busy || h.reverse || h.cantReach }

// hear reads the chat lines after the cursor.
func (h *askHeard) hear(d *fleetrunner.Deps, crafter string, cursor *int) {
	if d.Chat == nil {
		return
	}
	lines, next := d.Chat.ReadAfter(*cursor, time.Time{})
	*cursor = next
	for _, line := range lines {
		low := strings.ToLower(strings.TrimSpace(line))
		switch {
		case strings.Contains(low, "sending trade offer"):
			h.executed, h.busy = true, false
		case strings.Contains(low, "is busy at the moment"):
			h.busy, h.executed = true, false
		case strings.Contains(low, "reach that"):
			// "I can't reach that!": Player.processInteraction gives the walk to the target up when it has no waypoint left and took no
			// step. (A waypoint that is a solid tile stays queued and the engine says nothing: the usual dead end is silent.)
			h.cantReach = true
		default:
			if name, ok := mulerunner.TradeRequestFrom(line); ok && actions.SameUsername(name, crafter) {
				h.reverse = true
			}
		}
	}
}

// gap is how long after a request the next may be sent, by what the game said of it (see runecraftplan.GateRequestQuiet).
func (h askHeard) gap() time.Duration {
	switch {
	case h.reverse:
		return 0
	case h.executed:
		return scaledDown(runecraftplan.GateRequestQuiet)
	case h.busy, h.cantReach:
		return scaledDown(runecraftplan.GateRequestEvery)
	}
	return scaledDown(runecraftplan.GateRequestPending)
}

// crafterAt is where the named player stands.
func crafterAt(d *fleetrunner.Deps, name string) (tile.Tile, bool) {
	for _, p := range d.Session.State().Players() {
		if actions.SameUsername(p.Name, name) {
			return tile.Tile{X: p.X, Z: p.Z, Level: p.Level}, true
		}
	}
	return tile.Tile{}, false
}

// approachState is what a carrier tracks to tell a crafter that is slow from one it cannot reach: how many requests went unanswered
// since it last moved, and how long he has stood on his tile.
type approachState struct {
	silentAsks int
	// countedFor is the request (by when it was sent) whose silence silentAsks already includes: consider runs again at once when a
	// request could not be sent, and must not count the same silence twice.
	countedFor time.Time
	stillAt    tile.Tile
	stillSince time.Time
	// noTile is the crafter tile no approach was found for (do not look again until he moves), said the line once.
	noTile tile.Tile
}

// see notes where the crafter stands now.
func (a *approachState) see(crafter tile.Tile, now time.Time) {
	if crafter != a.stillAt || a.stillSince.IsZero() {
		a.stillAt, a.stillSince = crafter, now
	}
}

func (a *approachState) lost() { a.stillSince = time.Time{} }

// consider is the moment before a new request: when the game has said nothing of the last ones and the tile the carrier stands on is
// one the engine cannot bring it to the crafter from (a pillar between them on the asker's side), it walks to another tile beside him
// and reports true. Live 18 Oct 2026 eight carriers asked 17 times a minute for 55 minutes from the one tile no request could run from.
func (a *approachState) consider(d *fleetrunner.Deps, trade *actions.Trade, o OfferOptions, res *OfferResult, crafter tile.Tile, heard askHeard, lastAsk, now time.Time) bool {
	pk := d.Pack
	if pk == nil {
		return false
	}
	if heard.any() {
		a.silentAsks = 0
	} else if res.Requests > 0 && !lastAsk.Equal(a.countedFor) {
		a.silentAsks++
		a.countedFor = lastAsk
	}
	here := Here(d)
	view := runecraftplan.ApproachView{
		Operable: pk.OperableFrom(navPoint(here), navPoint(crafter)), EngineReaches: true,
		Heard: heard.executed || heard.busy || heard.reverse, CantReach: heard.cantReach, Asks: a.silentAsks, Still: scaled(now.Sub(a.stillSince)),
	}
	if view.Operable || view.Heard || (a.silentAsks == 0 && !view.CantReach) {
		return false
	}
	_, view.EngineReaches = pk.NaiveApproach(navPoint(here), navPoint(crafter), 48)
	move, why := runecraftplan.DecideApproach(view)
	if !move {
		return false
	}
	if a.noTile == crafter {
		return false
	}
	ap, ok := ApproachTile(pk, here, crafter, o.Stand)
	if !ok {
		a.noTile = crafter
		logf(d, "runecraft: carrier: WARNING: %s has not answered %d request(s) (%s) but no tile beside him (%v) can be walked to from %d,%d", o.Crafter, res.Requests, why,
			ApproachTiles(pk, crafter), here.X, here.Z)
		return false
	}
	logf(d, "runecraft: carrier: %s does not answer %d request(s) from %d,%d (he stands at %d,%d): %s; walking %d step(s) to %d,%d beside him", o.Crafter, res.Requests, here.X, here.Z, crafter.X, crafter.Z, why, ap.Steps, ap.Tile.X, ap.Tile.Z)
	res.Moves++
	res.MovedTo, res.MoveWhy = ap.Tile, why
	arrived := walkToApproach(d, ap, trade)
	if !arrived {
		logf(d, "runecraft: carrier: the walk to %d,%d beside %s did not arrive (at %d,%d)", ap.Tile.X, ap.Tile.Z, o.Crafter, Here(d).X, Here(d).Z)
	}
	a.silentAsks = 0
	return true
}

// TradeWithCrafter asks the crafter for a trade and carries it through. It asks the moment he is in view and then only as the game's
// answers to the request say (askHeard.gap): a request that was executed is never repeated before the screen has had time to open,
// because the repeat would close it. A crafter nobody can ask from where the carrier stands (a request the engine never runs and says
// nothing of) is approached from another side of him (approachState.consider).
func TradeWithCrafter(d *fleetrunner.Deps, o OfferOptions) (res OfferResult) {
	started := time.Now()
	defer func() { res.Took = time.Since(started) }()
	trade := actions.NewTrade(d.Session)
	var lastAsk, firstAbsent, seenAt time.Time
	var heard askHeard
	var app approachState
	cursor := 0
	if d.Chat != nil {
		cursor = d.Chat.Total()
	}
	for time.Since(started) < o.Timeout {
		if !d.Delay(0) {
			res.Reason = "stopped"
			return res
		}
		if randomevents.Busy(d.Session) {
			if !d.Delay(gatePoll) {
				res.Reason = "stopped"
				return res
			}
			continue
		}
		if trade.Active() {
			return carrierScreen(d, trade, o, res)
		}
		heard.hear(d, o.Crafter, &cursor)
		if heard.any() {
			res.Heard = true
		}
		now := time.Now()
		crafter, visible := crafterAt(d, o.Crafter)
		if visible {
			app.see(crafter, now)
			res.CrafterAt = crafter
		} else {
			app.lost()
		}
		switch {
		case !visible:
			// Whatever was asked of him before is gone with him (his trade status is reset by the next screen and he will be asked
			// afresh); the first request is the moment he shows.
			if firstAbsent.IsZero() {
				firstAbsent = now
			}
			lastAsk, seenAt, heard = time.Time{}, time.Time{}, askHeard{}
			app.silentAsks = 0
			if o.Absent > 0 && now.Sub(firstAbsent) >= o.Absent {
				res.NotHere, res.Reason = true, o.Crafter+" is not in view"
				return res
			}
		case lastAsk.IsZero() || now.Sub(lastAsk) >= heard.gap():
			firstAbsent = time.Time{}
			if seenAt.IsZero() {
				seenAt = now
			}
			if o.holdForStand(d, crafter) && now.Sub(seenAt) < scaledDown(runecraftplan.GateStandHold) {
				break
			}
			if !lastAsk.IsZero() && app.consider(d, trade, o, &res, crafter, heard, lastAsk, now) {
				lastAsk, heard = time.Time{}, askHeard{}
				break
			}
			err := trade.RequestPlayer(o.Crafter)
			switch {
			case err == nil:
				res.Requests++
				lastAsk, heard = now, askHeard{}
			case errors.Is(err, actions.ErrPlayerNotVisible):
				// Gone between the two looks: the next look says so.
			default:
				res.Reason = "the trade request could not be sent: " + err.Error()
				return res
			}
		default:
			firstAbsent = time.Time{}
		}
		if !d.Delay(gatePoll) {
			res.Reason = "stopped"
			return res
		}
	}
	res.NoScreen = true
	res.StoodAt = Here(d)
	res.Reason = fmt.Sprintf("no trade screen after %s and %d request(s)", o.Timeout.Round(time.Second), res.Requests)
	if res.Requests > 0 && !res.Heard {
		res.Silent = true
		res.Reason += fmt.Sprintf(" (the game said nothing of any of them; he stands at %d,%d and I stand at %d,%d)", res.CrafterAt.X, res.CrafterAt.Z, res.StoodAt.X, res.StoodAt.Z)
	}
	return res
}

// holdForStand says the request waits for the crafter to reach his stand: the carrier stands beside the stand (so it would not have
// to move once he is on it) and he is not on it yet.
func (o OfferOptions) holdForStand(d *fleetrunner.Deps, crafter tile.Tile) bool {
	if o.Stand == (tile.Tile{}) {
		return false
	}
	here := Here(d)
	return here.Level == o.Stand.Level && tile.Chebyshev(here, o.Stand) <= 1 && crafter != o.Stand
}

// scaledDown is the inverse of scaled: a wait the code spends is shortened by the test's clock scale.
func scaledDown(d time.Duration) time.Duration { return time.Duration(float64(d) / gateClockScale) }

// closedResult is the carrier's account of a screen that closed before anybody accepted.
func closedResult(d *fleetrunner.Deps, res OfferResult, opened time.Time) OfferResult {
	res.Closed = true
	res.Reason = "the trade screen closed before it was accepted" + sayWord(d, opened)
	return res
}

// carrierScreen carries the open screen to its end from the carrier's side.
func carrierScreen(d *fleetrunner.Deps, trade *actions.Trade, o OfferOptions, res OfferResult) OfferResult {
	opened := time.Now()
	waitUntil(d, func() bool { return trade.Partner() != "" }, gateHeaderWait)
	if p := trade.Partner(); !actions.SameUsername(p, o.Crafter) {
		if !trade.Active() {
			return closedResult(d, res, opened)
		}
		_ = trade.Decline(3 * time.Second)
		res.Reason = fmt.Sprintf("the trade screen is with %q, not %s", p, o.Crafter)
		return res
	}
	essence0, natures0 := Essence(d), Natures(d)
	offered := 0
	if o.Mode == OfferEssence {
		offered = essence0
		if offered == 0 {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "no unnoted essence in the pack to offer"
			return res
		}
		// The crafter takes what he has room for. Offering more is declined, so a carrier holding more keeps the rest for its next load.
		capped := false
		if c := o.takes(); c >= runecraftplan.MinLoad && c < offered {
			offered, capped = c, true
			res.Kept = essence0 - offered
		}
		// offer puts the essence still to put up on the screen: everything with Offer-All, or, under a cap, exactly what is short of it
		// with Offer-X (an Offer-All again would put the whole pack up).
		offer := func(onScreen int) error {
			if !capped {
				return trade.OfferAll(int32(runecraftplan.EssenceID))
			}
			return trade.Offer(int32(runecraftplan.EssenceID), int32(offered-onScreen), 3*time.Second)
		}
		if err := offer(0); err != nil {
			if !trade.Active() {
				return closedResult(d, res, opened)
			}
			_ = trade.Decline(3 * time.Second)
			if capped {
				res.Reason = fmt.Sprintf("Offer-X of %d of the %d essence: %v", offered, essence0, err)
			} else {
				res.Reason = "Offer-All of the essence: " + err.Error()
			}
			return res
		}
		clicks := 1
		sent := time.Now()
		limit := sent.Add(gateOfferWait)
		on := false
		for time.Now().Before(limit) {
			if !trade.Active() {
				return closedResult(d, res, opened)
			}
			if e, _, _ := offerSummary(trade.MyOffer()); e >= offered {
				on = true
				break
			}
			// An offer that moved nothing (the click was read while the player was delayed, or the side pack was a tick behind) is
			// sent again: it moves whatever is left and is a no-op on an offer that is complete.
			if time.Since(sent) >= gateOfferRetry {
				onScreen, _, _ := offerSummary(trade.MyOffer())
				if err := offer(onScreen); err == nil {
					clicks++
				}
				sent = time.Now()
			}
			if !d.Delay(poll) {
				_ = trade.Decline(3 * time.Second)
				res.Reason = "stopped"
				return res
			}
		}
		if !on {
			e, _, _ := offerSummary(trade.MyOffer())
			_ = trade.Decline(3 * time.Second)
			res.Reason = fmt.Sprintf("the offer screen shows %d of the %d essence offered (%d offer click(s))%s", e, offered, clicks, sayWord(d, opened))
			return res
		}
	}
	var lastSig string
	lastChange := time.Now()
	acceptedSig := ""
	theirNat := 0
	loopOpened := time.Now()
	for {
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "a random event interrupted the trade"
			return res
		}
		if !trade.Active() {
			if acceptedSig == "" {
				return closedResult(d, res, opened)
			}
			if !waitUntil(d, trade.Active, gateFlicker) {
				return carrierAfter(d, res, o, essence0, natures0, offered, theirNat, opened)
			}
		}
		if trade.OnConfirmScreen() {
			mine, theirs := trade.ConfirmOffers()
			me, mn, mo := offerSummary(mine)
			te, tn, to := offerSummary(theirs)
			ok := false
			switch o.Mode {
			case OfferEssence:
				ok = me == offered && mn == 0 && mo == 0 && te == 0 && tn == 0 && to == 0
			case TakeNatures:
				ok = me == 0 && mn == 0 && mo == 0 && te == 0 && to == 0 && tn > 0 && tn == theirNat
			}
			if !ok {
				_ = trade.Decline(3 * time.Second)
				res.Reason = fmt.Sprintf("the confirm screen does not match (mine %d/%d/%d, theirs %d/%d/%d; offered %d essence, expecting %d Natures)", me, mn, mo, te, tn, to, offered, theirNat)
				return res
			}
			if err := trade.Accept(); err != nil {
				res.Reason = "accept (confirm screen): " + err.Error()
				return res
			}
			if !waitUntil(d, func() bool { return !trade.Active() }, gateCloseWait) {
				_ = trade.Decline(3 * time.Second)
				res.Reason = "the trade never closed after the confirm accept"
				return res
			}
			return carrierAfter(d, res, o, essence0, natures0, offered, theirNat, opened)
		}
		te, tn, to := offerSummary(trade.TheirOffer())
		me, _, mo := offerSummary(trade.MyOffer())
		sig := fmt.Sprintf("%d/%d/%d|%d/%d", te, tn, to, me, mo)
		if sig != lastSig {
			lastSig, lastChange = sig, time.Now()
		}
		ready := false
		switch o.Mode {
		case OfferEssence:
			ready = me >= offered
		case TakeNatures:
			ready = tn > 0 && te == 0 && to == 0 && scaled(time.Since(lastChange)) >= runecraftplan.GateSettle
			if ready {
				theirNat = tn
			}
		}
		if ready && acceptedSig != sig {
			if err := trade.Accept(); err != nil {
				res.Reason = "accept (offer screen): " + err.Error()
				return res
			}
			acceptedSig = sig
		}
		if time.Since(loopOpened) > gateConfirmWait+gateCloseWait {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "the trade took longer than " + (gateConfirmWait + gateCloseWait).String()
			return res
		}
		if !d.Delay(gatePoll) {
			_ = trade.Decline(3 * time.Second)
			res.Reason = "stopped"
			return res
		}
	}
}

// carrierAfter judges the closed trade by the pack. A close the game says the other side made is not waited on.
func carrierAfter(d *fleetrunner.Deps, res OfferResult, o OfferOptions, essence0, natures0, offered, wantNat int, opened time.Time) OfferResult {
	switch o.Mode {
	case OfferEssence:
		waitUntil(d, func() bool { return Essence(d) <= essence0-offered || declined(d, opened) }, gateArriveWait)
		if left := essence0 - Essence(d); left >= offered && offered > 0 {
			res.Done, res.Moved = true, left
			return res
		}
		res.Reason = fmt.Sprintf("the trade closed and the pack still holds %d of the %d essence offered%s", Essence(d), essence0, sayWord(d, opened))
		res.Declined = declined(d, opened)
	case TakeNatures:
		waitUntil(d, func() bool { return Natures(d) >= natures0+wantNat || declined(d, opened) }, gateArriveWait)
		if got := Natures(d) - natures0; got > 0 && got >= wantNat {
			res.Done, res.Moved = true, got
			return res
		}
		res.Reason = fmt.Sprintf("the trade closed and the pack gained %d Natures (expected %d)%s", Natures(d)-natures0, wantNat, sayWord(d, opened))
		res.Declined = declined(d, opened)
	}
	return res
}

// Describe is the one-line account of a finished carrier trade for the log.
func (r OfferResult) Describe(mode OfferMode, crafter string) string {
	what := "essence"
	if mode == TakeNatures {
		what = "Nature runes"
	}
	switch {
	case r.Done:
		kept := ""
		if r.Kept > 0 {
			kept = fmt.Sprintf(", kept %d: he takes %d a trade", r.Kept, r.Moved)
		}
		return fmt.Sprintf("traded %d %s with %s in %s (%d request(s)%s)", r.Moved, what, crafter, r.Took.Round(100*time.Millisecond), r.Requests, kept)
	case r.Closed:
		return fmt.Sprintf("trade with %s closed before it was accepted after %s and %d request(s): %s", crafter, r.Took.Round(100*time.Millisecond), r.Requests, strings.TrimSpace(r.Reason))
	}
	return fmt.Sprintf("trade with %s failed after %s and %d request(s): %s", crafter, r.Took.Round(100*time.Millisecond), r.Requests, strings.TrimSpace(r.Reason))
}
