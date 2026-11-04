// Package restockrunner is the live half of a mule replenishing itself: the mule's appeal
// campaign on the wall bus (campaign.go) and the donor's actual coin hand-over (donate.go).
// restockplan holds every decision; nothing here decides anything it could have decided there.
//
// THE RECEIPT RULE, first because it is the one thing that must not be softened. Neither side
// ever treats a trade closing, a message arriving or a Result field as proof. The donor counts
// the coins that actually LEFT its own pack; the mule counts the coins that actually ARRIVED in
// its own bank, read through a real bank open (mulerunner.ReadBankTotals' own callers document
// why State().Inventory hands back the last reading after a bank closes, so a second "snapshot"
// without an open is the first one again). docs/trustworthy_harnesses.md records roughly 25
// tools in this repo that reported verdicts they could not support, and "the trade closed" is
// exactly the shape of claim that produced them.
//
// The mule's half is deliberately split so most of it is testable without a game at all:
// Campaign below drives the whole appeal/pledge/grant conversation over a real mulebus.Bus and
// touches no session, so its tests exercise the real message flow rather than a mock of it.
package restockrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/tile"
)

// Campaign is the mule's own restock conversation: it broadcasts when restockplan says it is
// low, collects the pledges that come back, and grants a bounded few of them at a time.
//
// It owns no lock. One Campaign belongs to one mule bot and is stepped from that bot's own Loop
// goroutine, the same single-goroutine convention every other per-bot struct in internal/bot
// follows (muleAsk, the pickup queue, the hand-out ledger). A Campaign shared between goroutines
// would need one; none is.
type Campaign struct {
	board *restockplan.Board
	// last is when this mule last broadcast, per item — a map because the appeal machinery is
	// item-generic even though only coins has a donor-side rule today (see restockplan's own
	// package doc on exactly how far this generalises).
	last map[string]time.Time
	// watcher reads only what was published after the campaign started, matching
	// mulerunner.NewRequestWatcher's own behaviour: a mule that restarts must not act on an
	// hour-old pledge from an account that has long since moved on.
	watcher *mulebus.Watcher

	appeals, grants, pledges int
	// received is what actually arrived, proven by the mule's own count rising and recorded by
	// the caller through Received. Never inferred from a trade closing.
	received int
}

// NewCampaign starts one. maxInFlight below 1 falls back to restockplan.DefaultMaxInFlight —
// see NewBoard for why a board that can never grant is a kill switch rather than a setting.
func NewCampaign(maxInFlight int) *Campaign {
	return &Campaign{
		board:   restockplan.NewBoard(maxInFlight),
		last:    map[string]time.Time{},
		watcher: mulebus.NewWatcher(),
	}
}

// Board exposes the queue for a status line or a panel. Read-only in spirit; the campaign owns
// its lifetime.
func (c *Campaign) Board() *restockplan.Board { return c.board }

// Stats is what an operator wants on one line: how many appeals went out, how many pledges came
// back, how many donors were sent for, and how much actually arrived.
func (c *Campaign) Stats() (appeals, pledges, grants, received int) {
	return c.appeals, c.pledges, c.grants, c.received
}

// Outstanding is what has been granted and has not arrived yet — what Step subtracts before
// deciding whether to appeal again.
func (c *Campaign) Outstanding(now time.Time) int { return c.board.Outstanding(now) }

// Received records coins (or units) the mule's own count is CONFIRMED to have gained from a
// restock donor, and releases that donor's grant slot.
//
// Call it with the real delta, never with what the donor pledged: a pledge is a reading of a
// bank the donor was not standing at (see mulebus.PledgeMessage), and the whole point of the
// receipt rule is that only the receiving side's own count settles it. gained may legitimately
// be 0 — a donor that arrived and failed still stops occupying a slot, which is why Arrived is
// called either way.
func (c *Campaign) Received(now time.Time, from string, gained int) {
	if gained > 0 {
		c.received += gained
	}
	c.board.Arrived(now, from)
}

// StepResult is what one Step did, for the caller's log.
type StepResult struct {
	// Appealed is the broadcast that went out, if one did.
	Appealed *restockplan.Appeal
	// Granted is every donor told to come on this step.
	Granted []restockplan.Grant
	// Pledged is how many pledges were taken off the bus on this step.
	Pledged int
	// Why is the reason nothing was broadcast, when nothing was. Always set when Appealed is
	// nil, so a quiet campaign is never silent in the log.
	Why string
}

// Step is one poll of the whole conversation, called from the mule bot's Loop:
//
//  1. drain the bus for pledges addressed to me and record them,
//  2. ask restockplan whether I am low enough to broadcast, and broadcast if so,
//  3. grant a bounded few of the waiting pledges, publishing one grant each.
//
// need carries the mule's own CURRENT stock (read from a real bank open by the caller) and its
// floor/target; Step fills in Promised and LastAppeal itself, because those are the campaign's
// own bookkeeping and a caller that had to supply them could get them wrong.
//
// bus nil, or me blank, makes this a no-op with a reason rather than a panic — a fleet
// configured without a wall bus simply has no restocking, exactly as it has no hand-outs.
func (c *Campaign) Step(now time.Time, bus *mulebus.Bus, me string, at tile.Tile, need restockplan.Need) StepResult {
	if bus == nil {
		return StepResult{Why: "no wall bus"}
	}
	if me == "" {
		return StepResult{Why: "this account has no name to sign an appeal with"}
	}

	// ---- 1. pledges ----
	var res StepResult
	for _, msg := range c.watcher.Poll(bus) {
		if msg.Kind != mulebus.KindPledge {
			continue
		}
		if !mulebus.NamesMatch(msg.To, me) {
			// A pledge answers ONE appeal. A fleet may run several mules, and taking another
			// mule's pledge would send a donor to the wrong post.
			continue
		}
		if msg.Qty < 1 || msg.From == "" {
			continue
		}
		c.board.Pledge(now, msg.From, msg.Qty)
		c.pledges++
		res.Pledged++
	}

	// ---- 2. the appeal ----
	need.Promised = c.board.Outstanding(now)
	need.LastAppeal = c.last[need.Item]
	appeal, ok, why := restockplan.DecideAppeal(now, need)
	if ok {
		bus.Publish(mulebus.AppealMessage(me, appeal.Item, appeal.Qty, at))
		c.last[need.Item] = now
		c.appeals++
		a := appeal
		res.Appealed = &a
	} else {
		res.Why = why
	}

	// ---- 3. the grants ----
	//
	// Granting is NOT conditional on having appealed on this same step. The appeal is rate
	// limited to once a minute and a pledge can arrive at any point inside that minute; tying
	// the two together would leave a willing donor queued for up to a minute for no reason.
	want := 0
	if res.Appealed != nil {
		want = res.Appealed.Qty
	} else if need.Target > 0 && need.Held < need.Target {
		want = need.Target - need.Held - c.board.Outstanding(now)
	}
	if want < 0 {
		want = 0
	}
	for _, g := range c.board.NextGrants(now, want) {
		bus.Publish(mulebus.GrantMessage(me, g.To, need.Item, g.Qty, at))
		c.grants++
		res.Granted = append(res.Granted, g)
	}
	return res
}

// Describe is one operator-readable line about where the campaign is.
func (c *Campaign) Describe(now time.Time) string {
	return fmt.Sprintf("%d appeal(s), %d pledge(s), %d grant(s), %d confirmed received; %s",
		c.appeals, c.pledges, c.grants, c.received, c.board.Describe(now))
}
