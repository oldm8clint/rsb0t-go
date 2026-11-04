package freshstartrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// WHY BOARDING WAITS FOR THE SERVER NOW, measured before anything was changed.
//
// Live 2026-09-23 (build 3dfb543a, 02:31-03:11): 201 "timed out waiting for the sail to
// complete" lines from 18 ProgressiveWCFletch accounts, Acct175 retrying the Brimhaven ->
// Ardougne ship for the whole window at 0 xp. On every one of those attempts the coins never
// left the pack, so the fare was never paid, and the worst accounts were all on the
// high-latency proxy routes (44, 40, 32, 31, 17 and 10 timeouts on psiphon/psiphon2, against 1
// to 3 on direct and WARP).
//
// cmd/sailtest reproduces it on the lab engine with 400ms of added one-way latency (2 of 6
// accounts across in five minutes, 31 sail timeouts) and its trace shows the whole mechanism,
// every attempt the same:
//
//  1. BoardShip clicked Talk-to, waited 700ms, saw no dialogue yet (the answer was still on
//     the wire) and clicked Talk-to AGAIN. An OPNPC is not idempotent: the engine's
//     OpNpcHandler calls clearPendingAction, which is closeModal, so the second click kills
//     the dialogue the first one has just opened and starts it over.
//  2. The client then receives open, close, open. It answered the first "Can I help you?"
//     and, since a fresh IF_OPENCHAT resets the one-press latch, answered the second one too.
//  3. The first answer already moved the server on to the option screen, so the second one
//     arrived there. RESUME_PAUSEBUTTON carries no answer (ResumePauseButtonHandler resumes
//     whatever script is paused and ignores the component), p_choice2 then reads a stale
//     last_com, and its `case default` is the SECOND option: "Does Karamja have unusual
//     customs then?". The officer says "I'm not that sort of customs officer." and the
//     dialogue ends with nothing paid. At the pay screen the same stray answer is "Oh, I'll not
//     bother then."; for the Port Sarim sailors it is "No, thank you."
//  4. BoardShip took "a dialogue we drove has closed" to mean "the sail has started", and
//     SailLeg waited sailCompleteTimeout (35s) for a teleport nobody had paid for. Then the
//     whole thing again, every 43 seconds, which is exactly the live cadence.
//
// Each piece of the fix answers one of those: one Talk-to per attempt and a wait for its
// dialogue (boardTalkWait), one answer per screen and a wait for the next one
// (boardScreenWait), and the end of a dialogue judged on whether the fare was actually taken
// (farePaid) rather than on the dialogue having closed.

const (
	// boardTalkWait is how long one Talk-to gets to open the boarding dialogue before BoardShip
	// clicks again. The account may have to step up to a wandering officer first (a few 600ms
	// ticks), and the answer then has a whole round trip to come back over, which on the proxy
	// routes is most of a second on its own. Clicking again inside that window is the bug, so
	// this is deliberately generous: an attempt that genuinely failed costs these seconds and
	// nothing else, where the old 700ms re-click cost a 35-second sail wait. Measured on a
	// private lab engine at the live 600ms tick with 300ms added each way: the officer's
	// "Can I help you?" arrived 6.1s after the Talk-to (the account had to step after him
	// first), so the margin over that is kept wide.
	boardTalkWait = 12 * time.Second
	// boardScreenWait is how long an answer gets for the server's next screen to arrive. Only
	// a ceiling: the wait ends the moment the screen changes.
	boardScreenWait = 6 * time.Second
	// boardFareSettle is how long a closed boarding dialogue waits for the evidence of a paid
	// fare before calling it unpaid. The engine's openMainModal writes IF_CLOSE the moment
	// ~set_sail opens the ship interface, but the IF_OPENMAIN itself goes out with the tick's
	// modal refresh and the coins with its inventory update, so for a moment the client sees a
	// closed chat and nothing else. cmd/sailtest caught one account in that moment at the
	// live 600ms tick: judged unpaid, it sailed anyway a second later. The refusal branch sends
	// nothing after its close, so waiting costs an unpaid attempt these seconds and no more.
	boardFareSettle = 2400 * time.Millisecond
	// shipFare is what either boarding dialogue takes: content's customs_officer.rs2 and
	// sailors.rs2 both `inv_del(inv, coins, 30)` before ~set_sail.
	shipFare = freshstart.DefaultFare / 2
	// shipJourneyInterface is content's ship_journey, the main modal ~set_sail opens in the
	// same tick it takes the fare (id read off a lab trace: main=3281 right after "You pay 30
	// coins and board the ship.").
	shipJourneyInterface = 3281
	// sailFallbackAfter is how many boarding attempts in a row may fail at a dock before
	// SailLeg stops repeating BoardShip and hands the crossing to the webwalk route instead.
	// An attempt that fails now costs seconds rather than 35, so four is about a minute.
	sailFallbackAfter = 4
	// sailFallbackTimeout bounds the webwalk route's attempt at the CROSSING. The route stops as
	// arrived the moment the account is across the water (SailLeg carries on from there), so
	// this only runs out on a crossing that keeps failing, and must not become a new way of
	// retrying it for ten minutes: measured with cmd/sailtest -mode stranded, the walker re-tried
	// a refused ship hop for as long as it was allowed (150 hop lines in six minutes), because
	// routing around the only ship leaves the destination unreachable.
	sailFallbackTimeout = 3 * time.Minute
	// sailFallbackBackoff is the wait after the webwalk route has also failed, multiplied by
	// how many times in a row it has, up to sailFallbackBackoffMax. Boarding is then tried again:
	// from Karamja there is no other White-Wolf-free way to the mainland north of the river, so
	// giving up for good would strand the account for the rest of its run, and retrying at
	// full speed is the loop this replaces.
	sailFallbackBackoff    = time.Minute
	sailFallbackBackoffMax = 5 * time.Minute
)

// boardAttempt is BoardShip's memory across calls: the answers given in the dialogue now being
// driven, what the account held when the Talk-to that opened it was sent, and how many
// attempts in a row have failed at this dock. The zero value is ready to use.
type boardAttempt struct {
	// step counts answers given in the current dialogue. It is a loop bound, not a position:
	// see BoardShip's matching comment.
	step int
	// talkedAt and coins are when the current attempt's Talk-to was sent and the coins held
	// at that moment, the "before" farePaid compares against. Zero talkedAt means no attempt
	// is in flight.
	talkedAt time.Time
	coins    int
	// from is where the account stood when that Talk-to was sent: the dock a sail has to
	// carry it away from before SailLeg counts the sail as landed.
	from tile.Tile
	// sawDialogue says a boarding dialogue has been seen open since that Talk-to, so a closed
	// screen means the dialogue ENDED rather than never began.
	sawDialogue bool
	// fails counts boarding attempts in a row that ended without the fare being taken.
	fails int
}

// begin starts an attempt for a Talk-to about to be sent from the tile from.
func (a *boardAttempt) begin(coins int, from tile.Tile, now time.Time) {
	a.step, a.talkedAt, a.coins, a.from, a.sawDialogue = 0, now, coins, from, false
}

// end closes the current attempt, keeping only the failure count.
func (a *boardAttempt) end() {
	a.step, a.talkedAt, a.coins, a.from, a.sawDialogue = 0, time.Time{}, 0, tile.Tile{}, false
}

// noteDialogue records that a dialogue is on screen during this attempt. Only a dialogue that
// follows our own Talk-to counts: the arrival box ("The ship arrives at Ardougne.") or any
// other screen seen with no attempt in flight must not later be judged as a boarding dialogue
// that closed without taking the fare.
func (a *boardAttempt) noteDialogue() {
	if !a.talkedAt.IsZero() {
		a.sawDialogue = true
	}
}

// inDialogue reports whether a boarding dialogue this attempt drove or saw has since closed
// being a question worth asking: an answer was given, or the dialogue was seen open.
func (a *boardAttempt) inDialogue() bool { return a.step > 0 || a.sawDialogue }

// fareTaken is the pure half of farePaid: any one of the three things ~set_sail does in the
// tick it takes the fare is proof it was taken. coinsBefore < 0 means no snapshot was taken.
func fareTaken(coinsBefore, coinsNow int, mainModal int, messagesSince string) bool {
	if coinsBefore >= 0 && coinsBefore-coinsNow >= shipFare {
		return true
	}
	if mainModal == shipJourneyInterface {
		return true
	}
	return strings.Contains(strings.ToLower(messagesSince), "you pay")
}

// farePaid reports whether the boarding dialogue that just closed took the fare. It is the
// only honest answer to "is a sail on the way?": a dialogue that closed may equally have
// ended at "I'm not that sort of customs officer." with every coin still in the pack, and
// telling the two apart by the dialogue alone is how 201 sail waits were spent on nothing.
func farePaid(d *Deps, a *boardAttempt) bool {
	before := -1
	msgs := ""
	if !a.talkedAt.IsZero() {
		before = a.coins
		if d.Chat != nil {
			msgs = d.Chat.Since(a.talkedAt)
		}
	}
	main, _, _ := d.Session.State().Modals()
	return fareTaken(before, coinsHeld(d), main, msgs)
}

// awaitFarePaid is farePaid given boardFareSettle for the evidence to arrive. See
// boardFareSettle for the window it covers.
func awaitFarePaid(d *Deps, a *boardAttempt) bool {
	deadline := time.Now().Add(boardFareSettle)
	for {
		if farePaid(d, a) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(100 * time.Millisecond) {
			return farePaid(d, a)
		}
	}
}

// sailUnderWay reports whether the ship interface is up: the fare is paid and the engine is in
// ~set_sail's p_delay. Nothing may be clicked then. A Talk-to is refused (the player is
// delayed), and the dialogue it seems to open a few seconds later is the arrival box on the
// far side, which is not a boarding dialogue at all.
func sailUnderWay(d *Deps) bool {
	main, _, _ := d.Session.State().Modals()
	return main == shipJourneyInterface
}

// dialogueSignature identifies the screen on show, so an answer can wait for the next one.
// Modal ids alone are not enough: the customs tree opens multi2 twice and npcchat1 twice.
func dialogueSignature(d *Deps, dlg *actions.ChatDialog) string {
	main, _, chat := d.Session.State().Modals()
	var opts []string
	for _, o := range dlg.Options() {
		opts = append(opts, o.Text)
	}
	return fmt.Sprintf("%d/%d:%q:%q", main, chat, dlg.Texts(), opts)
}

// awaitScreenChange waits until the dialogue shows something other than before, or closes,
// for at most timeout. It reports whether it did. Ends early, reporting false, on combat or a
// stop request, so a hit taken mid-dialogue still reaches the Guardian promptly.
func awaitScreenChange(d *Deps, dlg *actions.ChatDialog, before string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if dialogueSignature(d, dlg) != before {
			return true
		}
		if d.Session.State().InCombat() {
			return false
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return dialogueSignature(d, dlg) != before
}

// awaitBoardingDialogue waits, after a Talk-to sent at since, for the dialogue it asked for.
// why says what ended a wait that did not see one.
func awaitBoardingDialogue(d *Deps, dlg *actions.ChatDialog, since time.Time, timeout time.Duration) (opened bool, why string) {
	deadline := since.Add(timeout)
	for time.Now().Before(deadline) {
		if dlg.IsOpen() {
			return true, ""
		}
		if d.Chat != nil && strings.Contains(d.Chat.Since(since), "I can't reach that!") {
			return false, `the server answered "I can't reach that!"`
		}
		if d.Session.State().InCombat() {
			return false, "the account is in combat"
		}
		if !d.Delay(100 * time.Millisecond) {
			return false, "stopped"
		}
	}
	if dlg.IsOpen() {
		return true, ""
	}
	return false, fmt.Sprintf("nothing opened in %s", timeout)
}

// sailLandedMinTiles is how far from the dock a position must be, on the same level, to count
// as a sail having set the account down. Every ship this package boards lands on a deck one
// level up and 70 to 90 tiles away, while walking up to a wandering officer moves it a few.
const sailLandedMinTiles = 20

// sailLanded reports whether an account that paid its fare at from is now where a sail put it.
//
// This replaced "the tile is no longer the one SailLeg started this call on", which was right
// while BoardShip only ever finished a dialogue in the same call it was waited for, and wrong
// once a slow caller loop can let the teleport land before the call that notices the fare: the
// account is then already on the deck, never moves again, and the old test waited out the
// full 35 seconds for a sail that had arrived.
func sailLanded(from, now tile.Tile) bool {
	return now.Level != from.Level || tile.Chebyshev(from, now) > sailLandedMinTiles
}

// crossedSince reports whether an account that was at from, needing leg, is now past that
// leg's water: off Karamja for the destination leg, on it for the pier leg.
func crossedSince(from, now tile.Tile, leg freshstart.Leg) bool {
	if leg == freshstart.LegPier {
		return !freshstart.OnKaramjaSide(from) && freshstart.OnKaramjaSide(now)
	}
	return freshstart.OnKaramjaSide(from) && !freshstart.OnKaramjaSide(now)
}

// fallbackBackoff is how long to wait after the webwalk route has failed n times in a row.
func fallbackBackoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	b := time.Duration(n) * sailFallbackBackoff
	if b > sailFallbackBackoffMax {
		b = sailFallbackBackoffMax
	}
	return b
}

// sailFallback is what SailLeg does once boarding has failed sailFallbackAfter times in a row
// at the same dock: stop repeating BoardShip and let the webwalk walker take the account to
// dest its own way. The walker plans the ship fares as graph edges and runs them through
// internal/webwalk/hops, whose approach follows a wandering npc and which sends one Talk-to
// per crossing, so it is a genuinely different attempt rather than the same one again. Its
// automatic danger zones (the White Wolf Mountain dens above all) apply exactly as they do to
// every other walk: nothing here asks for a route over the mountain, and none is offered.
//
// Reports whether the account arrived. A walk that got across the water but not all the way to
// dest is still progress, and is not punished with the back-off.
func sailFallback(d *Deps, dest tile.Tile, leg freshstart.Leg, here tile.Tile, p *SailProgress) bool {
	fails := p.board.fails
	p.board = boardAttempt{}
	if d.Walker == nil {
		p.fallbackFails++
		wait := fallbackBackoff(p.fallbackFails)
		d.logf("STRANDED: boarding the %s leg has failed %d times in a row at %d,%d,%d and this account has no webwalk walker to try another route — backing off %s before boarding again",
			leg, fails, here.X, here.Z, here.Level, wait)
		delayWatchingCombat(d, wait)
		return false
	}
	d.logf("boarding the %s leg has failed %d times in a row at %d,%d,%d — handing the crossing to the webwalk route (it runs the ship fares itself and keeps out of the White Wolf Mountain danger zones)",
		leg, fails, here.X, here.Z, here.Level)
	across := func() bool {
		cur := currentTile(d)
		return cur != nil && crossedSince(here, *cur, leg)
	}
	if walkWithWalkerUntil(d, dest, sailDirectArrivalRadius, sailFallbackTimeout, across) {
		p.fallbackFails = 0
		if cur := currentTile(d); cur != nil && across() {
			d.logf("the webwalk route got the account across the water (now at %d,%d,%d) — carrying on from here", cur.X, cur.Z, cur.Level)
		}
		return true
	}
	if across() {
		p.fallbackFails = 0
		return false
	}
	p.fallbackFails++
	wait := fallbackBackoff(p.fallbackFails)
	d.logf("STRANDED: the webwalk route did not get across the %s leg either (%d time(s) in a row) — backing off %s before boarding again; if this repeats the account needs an operator",
		leg, p.fallbackFails, wait)
	delayWatchingCombat(d, wait)
	return false
}
