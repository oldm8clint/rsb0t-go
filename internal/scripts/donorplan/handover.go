package donorplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The hand-over without the browser wall's bus.
//
// The TS donor asks MuleReceiver to collect over the BroadcastChannel rsb0t:mule:v1, which a Go
// process cannot join. MuleReceiver also accepts a plain in-game trade request from an account on
// its partners list, from its home phase at its post (the Seers' Village bank for mulecon), when
// the requester is within 2 tiles and the request is under 30 s old (MuleReceiver.ts stepHome and
// callerInRange). So the Go donor needs no message at all: draw the load, walk to the bank, step
// beside the mule, request a trade every few seconds, offer the load, accept, confirm.
//
// Never send the mule a private message: MuleReceiver reads PM text naming items as an order to
// trade those items back to the sender.
//
// MuleReceiver compares names case-insensitively but does not treat _ and space as one, and the
// game shows names with spaces, so mulecon's partners list must hold each donor's name with spaces.

// Mule settings, matching ProgressiveCrafter's manifest keys mule, muleAt and muleKeep.
const (
	DefaultMule   = "mulecon"
	DefaultMuleAt = 5000
	MinMuleAt     = 100
	MaxMuleAt     = 1_000_000
	// DefaultKeep is what stays in the bank: the emergency coins and the shears.
	DefaultKeep = "Coins, Shears"
	// DefaultTravelReserve is coins kept back from a hand-over as travel/fare money —
	// the account owner's own exact number (2026-09-14), not a value from the TS
	// original (rsb0t-scripts' own DefaultKeep keeps every coin, all-or-nothing, the
	// same as this package's Rules; users asked for a live account to give
	// away everything above a fixed float instead). See donorrunner.Options.Reserve for
	// where this actually gets applied — a whole-row Keep/hand-over choice can't express
	// "some of this stack," so the split happens at the trade screen, not here.
	DefaultTravelReserve = 500
)

// Timing of the hand-over.
const (
	// HandoffRetry: after a refused or failed hand-over, work resumes and it is not tried again
	// before this.
	HandoffRetry = 5 * time.Minute
	// MeetTimeout is how long the donor waits beside the bank for the mule to trade when the
	// mule is NOT visible — i.e. genuinely absent, which is the case this timeout was
	// written for.
	MeetTimeout = time.Minute
	// MeetTimeoutMuleVisible is the longer ceiling used while the mule is still within
	// SightRange: busy with another donor, or stepped off its stand (an idle cooking round
	// puts it ~8 tiles away at the Catherby range for ~99s). See Meet's own comment for the
	// measured reason this exists. Still bounded, so a genuinely wedged mule cannot trap
	// donors beside it indefinitely — it just stops them abandoning a hand-off seconds
	// before the mule walks back.
	//
	// Lowered from 4 minutes to 100s on 2026-09-19, measured on an 85-donor live cohort.
	// The original value was chosen to cover the mule's ~99s cooking absence, and it still
	// does — but it was set when a donor waiting meant "the mule is briefly away", not "84
	// other donors are ahead of me". In that live window: 72 hand-off attempts, 20 trades
	// completed, and 13 donors each burned the full timeout and gave up — roughly 52
	// donor-minutes of fishing lost in twelve minutes, while the mule ran flat out at 1.9
	// trades/min.
	//
	// Waiting longer cannot fix that, because the constraint is the mule's throughput, not
	// the donor's patience: a donor that waits 4 minutes in a queue it cannot see simply
	// loses 4 minutes of fishing and then leaves anyway. Giving up sooner and returning
	// after a jittered number of fishing rounds (see internal/bot/fisher.go's GaveUp branch,
	// already 1-10 rounds) converts dead standing time into caught fish and spreads arrivals
	// out instead of concentrating them.
	//
	// 100s is deliberately still longer than the ~99s cooking round this was written for, so
	// the case it was added for is preserved intact.
	MeetTimeoutMuleVisible = 100 * time.Second
	// MeetTimeoutComing is the hard ceiling on waiting for a mule that has SAID it is on its way
	// (MeetView.WaitUntil, from a mulebus KindComing message). Neither timeout above can cover a
	// mule that travels: both are measured from the moment the load was drawn, so a mule walking
	// the 84 tiles between the Catherby and Seers' Village banks is always "genuinely gone" by
	// the time it could arrive, and every donor waiting for it gives up at 60s having done
	// nothing but stand there. The mule's own promise is what extends the wait, and only as far
	// as the ETA it actually published — this is just the bound on how long a promise can be
	// believed, so a mule that crashes, logs out or lies mid-trip cannot pin a donor forever.
	//
	// RAISED FROM 5 TO 10 MINUTES on 2026-09-23, because five minutes was cutting the queue it was
	// meant to protect. Measured on 263cceda: 61 of 74 give-ups after a quote left exactly at this
	// cap (arrival-to-give-up clustered at 257-325s, median 300s) with a promise still standing --
	// a median of 390s of it, up to 1110s, thrown away along with a walk back and a redraw. Five
	// minutes was only safe while the promise could not be trusted (random service order, a quote
	// made once and never refreshed). The mule now serves earliest-announced first and re-sends
	// every waiting donor its quote after each trade, so the promise is a turn time and is what
	// ends the wait; this cap is the backstop for a mule that promises and never serves, and it
	// still ends every wait.
	MeetTimeoutComing = 10 * time.Minute
	// MaxFailedTrades ends a hand-off after this many trade screens in a row closed with nothing
	// crossing. Before the cap above was raised the five-minute cap was the only thing that ever
	// ended such a loop (live 3dfb543a: Acct193 12, Acct181 10, acct153 7 in a row); at ten
	// minutes it would hold the donor, and the mule, twice as long. Three is one more than the
	// ordinary retry after a screen that flickered shut.
	MaxFailedTrades = 3
	// ComingGrace is added to the mule's own estimate before that promise lapses. The estimate
	// is a queue-depth guess made when the donor announced, and the queue behind it keeps
	// growing, so it runs LATE far more often than early. Measured live 2026-09-23 on build
	// 263ccedad: all three give-ups in the run were donors standing ONE TILE from the post,
	// each abandoned about thirty seconds after an estimate the mule missed — acct165 told
	// 30s gave up at 85s, acct139 told 1m30s gave up at 121s, Acct171 told 2m0s gave up at
	// 150s.
	//
	// Waiting longer is close to free; giving up is not. A donor that leaves carries its
	// undelivered load away, and acct150 shows where that ends: 26 of 28 slots held by a load
	// it never handed over, deadlocked across four restarts, unable to fletch because it has no
	// room for a log. The cost of an over-long wait is idle minutes; the cost of a short one is
	// an account lost until somebody notices. MeetTimeoutComing still caps the whole wait, so a
	// mule that has genuinely gone cannot hold a donor past ten minutes.
	ComingGrace = 90 * time.Second
	// RequestEvery spaces repeated trade requests, as the TS donor does; the mule ignores requests
	// older than 30 s.
	RequestEvery = 3 * time.Second
	// PostTradeCooldown: the mule ignores a partner for 6 s after a trade, then banks.
	PostTradeCooldown = 6 * time.Second
	// TradeTimeout is the trade screens' budget before declining, the same as both TS ends use.
	TradeTimeout = 60 * time.Second
	// StandRadius is how close to the bank stand counts as there.
	StandRadius = 3
	// MeetRange is how close the mule must be for it to answer a trade request.
	MeetRange = 2
	// SightRange is how far away a visible mule is still worth stepping over to.
	SightRange = 15
	// DefaultMuleFreeSlots is the mule's free pack slots with its default rune kit (5 stacks). The
	// mule declines an offer with more stacks than it has free slots.
	DefaultMuleFreeSlots = 23
)

// ParseList splits a comma-separated setting into trimmed, non-empty names, as the TS
// parsePartnerList does.
func ParseList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NamesMatch compares two player names the way the server identifies accounts: case-insensitively,
// with _ and space the same. Blank names never match.
func NamesMatch(a, b string) bool {
	a, b = displayName(a), displayName(b)
	return a != "" && strings.EqualFold(a, b)
}

func displayName(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
}

// Settings are a donor's mule settings.
type Settings struct {
	// Mule is the one account hand-overs go to; empty never hands over.
	Mule string
	// MuleAt is the banked count that starts a hand-over; 0 never does.
	MuleAt int
	// Keep names stay in the donor's bank.
	Keep []string
}

// NewSettings normalises the raw setting values: the first name of the mule list, the threshold
// clamped to 0 through MaxMuleAt, and the keep list split on commas.
func NewSettings(mule string, muleAt int, keep string) Settings {
	s := Settings{MuleAt: muleAt, Keep: ParseList(keep)}
	if names := ParseList(mule); len(names) > 0 {
		s.Mule = names[0]
	}
	if s.MuleAt < 0 {
		s.MuleAt = 0
	}
	if s.MuleAt > MaxMuleAt {
		s.MuleAt = MaxMuleAt
	}
	return s
}

// Enabled reports whether hand-overs happen at all.
func (s Settings) Enabled() bool {
	return s.Mule != "" && s.MuleAt > 0
}

// Step is where a hand-over is.
type Step string

// The hand-over steps.
const (
	// StepLoad draws the next load out of the bank as notes.
	StepLoad Step = "load"
	// StepMeet stands at the bank beside the mule and requests a trade.
	StepMeet Step = "meet"
	// StepTrade is on the trade screens.
	StepTrade Step = "trade"
	// StepReturn puts an unfinished load back in the bank.
	StepReturn Step = "return"
)

// Handover is one hand-over in progress.
type Handover struct {
	Step Step
	// Since is when the current step began.
	Since time.Time
	// LastRequest is when the last trade request was sent in this meet.
	LastRequest time.Time
	// NotBefore holds the next request back until the mule's post-trade cooldown has passed.
	NotBefore time.Time
	// ArrivedAt is when this donor first reached the meet stand, zero while it is still
	// walking there. The meet timeouts are measured from it, NOT from Since.
	//
	// THE BUG THIS EXISTS TO END, and it is the one the fleet owner kept reporting while I
	// kept misreading the log. Every timeout here used to run from Since -- the moment the
	// LOAD WAS DRAWN -- so the walk to the stand spent the donor's whole patience before it
	// could arrive. From the Seers bank to the Catherby mule post is about 84 tiles; the
	// budget was MeetTimeout, sixty seconds. Live 2026-09-23:
	//
	//	23:59:26 [acct139] hand-off to "mule" — 19 pack slot(s) drawn
	//	00:00:43 [acct139] donor: giving up waiting for "mule" at 2809,3441,0
	//
	// Seventy-seven seconds, and it never got there. Twenty-six of thirty-one wcfletch
	// accounts did exactly that inside eighty-two seconds. The give-up line prints
	// o.Stand, not the donor's own tile, which is why it reads as "waited at Catherby"
	// and is nothing of the kind -- I read it that way for hours.
	//
	// It also silently disabled the queueing fix that went out the same evening: a donor
	// announces within announceWithin of the stand, so a donor that never arrives never
	// announces, the mule is never told, and it never answers with a KindComing. Measured
	// on that build: the mule answered ONE donor out of forty-six.
	//
	// The walk is not unbounded without this clock. WalkNear carries its own timeout, and
	// standUnreachable counts consecutive failures to reach the stand at all -- those are
	// the right instruments for "I cannot get there", and this one is for "I am there and
	// nobody is coming".
	ArrivedAt time.Time
	// Loads and Slots count what has crossed so far.
	Loads int
	Slots int
	// FailedTrades counts trade screens in a row that closed with nothing crossing; a trade
	// that moves anything resets it. See MaxFailedTrades.
	FailedTrades int
}

// Begin starts a hand-over at the load step.
func Begin(now time.Time) Handover {
	return Handover{Step: StepLoad, Since: now}
}

func (h *Handover) enter(step Step, now time.Time) {
	h.Step = step
	h.Since = now
	h.LastRequest = time.Time{}
	h.NotBefore = time.Time{}
}

// Outcome is what a hand-over step decided for the hand-over as a whole.
type Outcome string

// The outcomes.
const (
	// Continue: the hand-over goes on.
	Continue Outcome = "continue"
	// Finished: nothing is left to hand over; no retry is scheduled.
	Finished Outcome = "finished"
	// Paused: stopped for now; try again after HandoffRetry.
	Paused Outcome = "paused"
)

// Loaded records drawing a load. rowsInBank is how many rows the bank had to hand over, and
// drawn the pack slots the withdrawal filled.
func (h *Handover) Loaded(rowsInBank, drawn int, now time.Time) Outcome {
	switch {
	case rowsInBank == 0:
		return Finished
	case drawn <= 0:
		return Paused
	}
	h.enter(StepMeet, now)
	return Continue
}

// MeetView is what the meet step reads.
type MeetView struct {
	Now time.Time
	Me  *tile.Tile
	// Mule is the mule's tile while it is visible, nil otherwise.
	Mule *tile.Tile
	// Stand is the bank stand the mule posts at.
	Stand tile.Tile
	// TradeOpen is true once the offer or confirm screen is open.
	TradeOpen bool
	// WaitUntil, when set, is when the mule's own KindComing message said it would arrive. It
	// extends the give-up timeout to that moment (bounded by MeetTimeoutComing) so a donor waits
	// out a mule that is genuinely travelling to it. nil is every caller that hears no such
	// promise, which is the unchanged behaviour: the timeouts above decide on their own.
	WaitUntil *time.Time
	// Declined is set when the mule answered this hand-off with a KindDeclined — it is not
	// coming. Giving up at once beats standing for the full timeout for an answer already
	// received, and the donor's own retry cooldown then applies as usual.
	Declined bool
}

// MeetAction is what the meet step does.
type MeetAction string

// The meet actions.
const (
	MeetWalk MeetAction = "walk to the bank stand"
	MeetWait MeetAction = "wait for the mule"
	// MeetApproach steps beside the mule: it only answers requests from within MeetRange.
	MeetApproach  MeetAction = "step beside the mule"
	MeetRequest   MeetAction = "send a trade request"
	MeetWaitReply MeetAction = "wait for the mule to answer"
	MeetGiveUp    MeetAction = "give up for now"
	MeetTrading   MeetAction = "trading"
)

// Meet decides the meet step. A MeetRequest also records the request time, so the next call
// waits RequestEvery before asking again. After MeetGiveUp the caller calls Failed.
func (h *Handover) Meet(v MeetView) MeetAction {
	if v.TradeOpen {
		h.enter(StepTrade, v.Now)
		return MeetTrading
	}
	// A mule that is still VISIBLE has not abandoned this hand-off — it is busy, or it has
	// stepped off its stand and will be back. Measured 2026-09-18 (see COLLAB.md's mule
	// throughput analysis): the mule's idle-cooking round walks it from the Catherby bank
	// stand to the range ~8 tiles away for ~99 seconds, well inside SightRange, and 28% of
	// donor arrivals landed during one of those rounds. With a flat MeetTimeout every one
	// of those donors gave up at exactly 60s — often seconds before the mule got back —
	// then walked away and did a redundant solo bank trip, which is pure lost fishing time
	// on both sides. Waiting it out is strictly better whenever the mule is demonstrably
	// still there. MeetTimeout still applies unchanged when the mule is genuinely gone
	// (out of sight, logged out, or somewhere else entirely), which is the case that
	// timeout was actually written for.
	if v.Declined || h.TooManyFailedTrades() {
		return MeetGiveUp
	}
	timeout := MeetTimeout
	if v.Me != nil && MuleNear(v.Mule, v.Me, SightRange) {
		timeout = MeetTimeoutMuleVisible
	}
	// A mule that published a KindComing is on its way to this very spot, so the "it is
	// genuinely gone" reading the timeouts above are built on is simply wrong for as long as
	// that promise stands. Bounded by MeetTimeoutComing so a promise cannot outlive the mule
	// that made it.
	// Time spent WALKING to the stand is not time spent waiting for the mule. See
	// Handover.ArrivedAt for the measured reason: from Since, the 84-tile walk from Seers to
	// the Catherby post spent the entire sixty-second budget before the donor could arrive,
	// and twenty-six of thirty-one accounts gave up without ever getting there.
	//
	// A donor that has not arrived yet is still travelling and cannot have been stood up.
	// ARRIVAL FIRST, because it is what the clock below is measured from. Either standing on
	// the meet stand or already beside the mule counts; set once, so a donor nudged off the
	// stand and back does not have its patience refreshed and a mule that never comes cannot
	// hold it for ever.
	if h.ArrivedAt.IsZero() && v.Me != nil && v.Me.Level == v.Stand.Level &&
		(MuleNear(v.Mule, v.Me, MeetRange) || tile.Chebyshev(*v.Me, v.Stand) <= StandRadius) {
		h.ArrivedAt = v.Now
	}
	waiting := time.Duration(0)
	if !h.ArrivedAt.IsZero() {
		waiting = v.Now.Sub(h.ArrivedAt)
	}
	promised := v.WaitUntil != nil && v.Now.Before(v.WaitUntil.Add(ComingGrace)) && waiting <= MeetTimeoutComing
	if !h.ArrivedAt.IsZero() && waiting > timeout && !promised {
		return MeetGiveUp
	}
	if v.Me == nil || v.Me.Level != v.Stand.Level {
		return MeetWalk
	}
	near := MuleNear(v.Mule, v.Me, MeetRange)
	if !near && tile.Chebyshev(*v.Me, v.Stand) > StandRadius {
		return MeetWalk
	}
	if !near {
		if MuleNear(v.Mule, v.Me, SightRange) {
			return MeetApproach
		}
		return MeetWait
	}
	if v.Now.Before(h.NotBefore) {
		return MeetWaitReply
	}
	if h.LastRequest.IsZero() || v.Now.Sub(h.LastRequest) >= RequestEvery {
		h.LastRequest = v.Now
		return MeetRequest
	}
	return MeetWaitReply
}

// Screen is which trade screen is open.
type Screen int

// The trade screens.
const (
	ScreenNone Screen = iota
	ScreenOffer
	ScreenConfirm
)

// TradeView is what the trade step reads.
type TradeView struct {
	Screen  Screen
	Partner string
	Mule    string
	// Load is the planned load (PlanLoad over the pack).
	Load []LoadGroup
	// Offered is our side of the trade screen; TheirOffer is the partner's.
	Offered    []ItemRow
	TheirOffer []ItemRow
	// Accepted is true once we have accepted the current screen.
	Accepted bool
}

// TradeAction is what the trade step does.
type TradeAction string

// The trade actions.
const (
	TradeClosed  TradeAction = "no trade open"
	TradeDecline TradeAction = "decline"
	TradeOffer   TradeAction = "offer"
	TradeAccept  TradeAction = "accept"
	TradeConfirm TradeAction = "confirm"
	TradeWait    TradeAction = "wait for the mule"
)

// DecideTrade decides the trade step. A trade with anyone but the mule, or one where the partner
// offers anything, is declined: the donor only gives, and only to its mule. On the offer screen
// each planned group is offered in order (the group is returned with TradeOffer), then accepted;
// the confirm screen is confirmed.
func DecideTrade(v TradeView) (TradeAction, LoadGroup) {
	if v.Screen == ScreenNone {
		return TradeClosed, LoadGroup{}
	}
	if !NamesMatch(v.Partner, v.Mule) {
		return TradeDecline, LoadGroup{}
	}
	for _, row := range v.TheirOffer {
		if row.Count > 0 {
			return TradeDecline, LoadGroup{}
		}
	}
	if v.Screen == ScreenOffer {
		for _, g := range v.Load {
			if !offered(v.Offered, g) {
				return TradeOffer, g
			}
		}
	}
	if !anyOffered(v.Offered) {
		return TradeDecline, LoadGroup{}
	}
	if v.Accepted {
		return TradeWait, LoadGroup{}
	}
	if v.Screen == ScreenConfirm {
		return TradeConfirm, LoadGroup{}
	}
	return TradeAccept, LoadGroup{}
}

func offered(rows []ItemRow, g LoadGroup) bool {
	for _, row := range rows {
		if row.Count > 0 && strings.EqualFold(row.Name, g.Name) && (row.ID == 0 || g.ID == 0 || row.ID == g.ID) {
			return true
		}
	}
	return false
}

func anyOffered(rows []ItemRow) bool {
	for _, row := range rows {
		if row.Count > 0 {
			return true
		}
	}
	return false
}

// TradeEnded reads a closed trade screen from the haul before and after: traded is true when
// slots left the pack.
func TradeEnded(haulBefore, haulAfter int) (traded bool, slots int) {
	if haulAfter < haulBefore {
		return true, haulBefore - haulAfter
	}
	return false, 0
}

// Traded records a completed trade. With haul still in the pack the donor meets the mule again;
// otherwise it draws the next load.
func (h *Handover) Traded(slots, haulLeft int, now time.Time) {
	if slots > 0 {
		h.FailedTrades = 0
	} else {
		h.FailedTrades++
	}
	h.Slots += slots
	h.Loads++
	if haulLeft > 0 {
		h.enter(StepMeet, now)
		h.NotBefore = now.Add(PostTradeCooldown)
		return
	}
	h.enter(StepLoad, now)
}

// TooManyFailedTrades reports whether MaxFailedTrades screens in a row closed with nothing
// crossing, when Meet gives the hand-off up rather than asking for another.
func (h *Handover) TooManyFailedTrades() bool { return h.FailedTrades >= MaxFailedTrades }

// TradeExpired reports whether the trade screens have been open longer than TradeTimeout, when
// the donor declines and calls Failed.
func (h *Handover) TradeExpired(now time.Time) bool {
	return h.Step == StepTrade && now.Sub(h.Since) > TradeTimeout
}

// Failed records a hand-over that stopped: the load goes back to the bank.
func (h *Handover) Failed(now time.Time) {
	h.enter(StepReturn, now)
}

// ---- the queue gate: look at the mule's queue BEFORE drawing a load ----

// DrawGateWait is the longest queue a donor sets out into. A longer one is worked through
// instead: the donor keeps skilling and asks again later, rather than drawing a load, walking it
// over and standing with it.
//
// Why a gate and not only a longer wait: the mule is the bottleneck, so throughput is the same
// either way and what differs is where the queueing delay is spent. Modelled on the measured
// 263cceda restart ramp (65 donors, service times drawn from the live 3dfb543a trade gaps):
// today's policy loses 311 / 698 / 1081 account-minutes at 20 / 39 / 59 s per trade; FIFO with a
// longer patience alone is WORSE, 392 / 871 / 1374, because the walking turns into standing;
// FIFO, a 10-minute cap and this gate counting donors still walking lose 72 / 97 / 111.
const DrawGateWait = 3 * time.Minute

// Bounds on how long one deferral lasts.
const (
	DrawGateMinDefer = time.Minute
	DrawGateMaxDefer = 5 * time.Minute
	// DrawGateMaxDeferrals is how many deferrals in a row a donor accepts before it goes anyway,
	// so a donor that keeps landing on a long queue cannot be starved of its turn for good.
	DrawGateMaxDeferrals = 3
)

// DeferDraw decides whether a donor should draw its load now, from the mule's own published
// queue: queue donors already expected (announced, or reserved and walking) at cycle each. When
// that wait is longer than DrawGateWait it answers true and how long to keep working first --
// the excess, bounded to DrawGateMinDefer..DrawGateMaxDefer. An unknown queue (no heartbeat, or
// one from a mule too old to carry it) never defers: that is the behaviour before the gate.
func DeferDraw(queue int, cycle time.Duration) (time.Duration, bool) {
	if queue <= 0 || cycle <= 0 {
		return 0, false
	}
	wait := time.Duration(queue) * cycle
	if wait <= DrawGateWait {
		return 0, false
	}
	d := wait - DrawGateWait
	if d < DrawGateMinDefer {
		d = DrawGateMinDefer
	}
	if d > DrawGateMaxDefer {
		d = DrawGateMaxDefer
	}
	return d, true
}

// NextAllowed is when the next hand-over may start after one ends: HandoffRetry from now when it
// is to be retried later, otherwise unchanged.
func NextAllowed(now time.Time, retryLater bool, current time.Time) time.Time {
	if retryLater {
		return now.Add(HandoffRetry)
	}
	return current
}

// Describe is the status line for a step.
func Describe(step Step, mule string) string {
	switch step {
	case StepLoad:
		return "drawing the hand-over for " + mule
	case StepMeet:
		return "waiting at the bank for " + mule
	case StepTrade:
		return "trading with " + mule
	case StepReturn:
		return "putting the hand-over back"
	}
	return string(step)
}
