package bot

// The carrier's side of the crafter's room (runecraftrunner.OfferOptions.Cap, runecarrier_shift.go trade): live 16 Oct 2026 a Strange fruit
// left in the crafter's pack by a random event made his trade 24 essence, and every carrier's Offer-All of 25 was declined: 121 declines,
// 26 of the 50 shift endings in the log.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

func (c *carrierRig) sayRoom(room int) {
	c.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(crafterName, tile.Tile{X: exitX, Z: exitZ}, mulebus.GateOpen, 0, 0).WithCapacity(room), 1))
}

// The trade stage hands runecraftrunner the crafter's room, read from his gate message when the offer is made: 24 while a fruit takes a slot,
// 25 once it is gone, nothing when he has not said.
func TestCarrierTradeFollowsTheRoomTheGateMessageSays(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	var opts []runecraftrunner.OfferOptions
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		opts = append(opts, o)
		return runecraftrunner.OfferResult{Done: true, Moved: 24, Kept: 1, Requests: 1, Took: 9 * time.Second}
	}
	w := c.world()
	c.sayRoom(24)
	if res := w.trade(); !res.OK {
		t.Fatalf("%+v", res)
	}
	if opts[0].Cap == nil || opts[0].Cap() != 24 {
		t.Fatalf("the offer is not told his room")
	}
	c.sayRoom(25)
	if opts[0].Cap() != 25 {
		t.Fatalf("the cap is read when the offer is made, not when the stage began: %d", opts[0].Cap())
	}
	if !c.logged("traded 24 essence with MasterCrafter") || !c.logged("kept 1: he takes 24 a trade") {
		t.Fatalf("log: %v", c.logs)
	}
}

// A decline that his gate message explains (he takes less than the carrier held) is a capacity change the next offer follows: no strike, no
// shift ending. One the message does not explain is a failure as before, and so is a run of explained ones that never gets better.
func TestCarrierADeclineTheRoomExplainsIsNotAStrike(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	declined := runecraftrunner.OfferResult{Declined: true, Requests: 1, Took: 8 * time.Second,
		Reason: "the trade closed and the pack still holds 25 of the 25 essence offered (the game said: Other player declined trade.)"}
	var answer runecraftrunner.OfferResult
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult { return answer }
	w := c.world()

	// The message says 24: the decline is explained.
	c.sayRoom(24)
	answer = declined
	for i := 1; i <= carrierCapDeclines; i++ {
		if res := w.trade(); !res.OK || w.tradeFails != 0 || w.capDeclines != i {
			t.Fatalf("decline %d: %+v, %d fails, %d explained", i, res, w.tradeFails, w.capDeclines)
		}
	}
	if !c.logged("declined 25 essence: he takes 24 a trade now") {
		t.Fatalf("log: %v", c.logs)
	}
	// ... but not for ever: the next one counts.
	if res := w.trade(); res.OK || w.tradeFails != 1 {
		t.Fatalf("the %dth explained decline in a row must count: %+v, %d fails", carrierCapDeclines+1, res, w.tradeFails)
	}
	// A trade that goes through starts the count again.
	answer = runecraftrunner.OfferResult{Done: true, Moved: 24, Kept: 1, Requests: 1, Took: 9 * time.Second}
	if res := w.trade(); !res.OK || w.capDeclines != 0 || w.tradeFails != 0 {
		t.Fatalf("%+v, %d explained, %d fails", res, w.capDeclines, w.tradeFails)
	}

	// The message says 25 (a full load): a decline is not explained by it. And a decline of a load that is within his room is not either.
	c.sayRoom(25)
	answer = declined
	if res := w.trade(); res.OK || w.tradeFails != 1 || w.capDeclines != 0 {
		t.Fatalf("a decline his message does not explain must be a failure: %+v, %d fails, %d explained", res, w.tradeFails, w.capDeclines)
	}
	// Nothing heard from him at all: not explained either.
	c2 := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		return declined
	}
	w2 := c2.world()
	if res := w2.trade(); res.OK || w2.tradeFails != 1 {
		t.Fatalf("%+v, %d fails", res, w2.tradeFails)
	}
	// A room under the least load is not a trade worth making: the message does not explain a decline either.
	c2.sayRoom(runecraftplan.MinLoad - 1)
	if res := w2.trade(); res.OK {
		t.Fatalf("a room of %d explained a decline: %+v", runecraftplan.MinLoad-1, res)
	}
}

// Screens that close before anybody accepts are a passing race and no strike, but a run of them is not passing: carrierClosesInARow without a
// trade between is a failure like any other (and a trade between them starts the count again).
func TestCarrierAClosedScreenRunIsEventuallyAFailure(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	closed := runecraftrunner.OfferResult{Closed: true, Requests: 1, Took: 4 * time.Second, Reason: "the trade screen closed before it was accepted (the game said: Other player declined trade.)"}
	var answer runecraftrunner.OfferResult
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult { return answer }
	w := c.world()
	answer = closed
	for i := 1; i < carrierClosesInARow; i++ {
		if res := w.trade(); !res.OK {
			t.Fatalf("close %d of the run was a failure: %+v", i, res)
		}
	}
	answer = runecraftrunner.OfferResult{Done: true, Moved: 25, Requests: 1, Took: 9 * time.Second}
	if res := w.trade(); !res.OK || w.closesInARow != 0 {
		t.Fatalf("a trade must start the run again: %+v, %d", res, w.closesInARow)
	}
	answer = closed
	for i := 1; i < carrierClosesInARow; i++ {
		if res := w.trade(); !res.OK {
			t.Fatalf("close %d of the second run was a failure: %+v", i, res)
		}
	}
	res := w.trade()
	if res.OK || !strings.Contains(res.Why, "closed before it was accepted 8 times in a row") || w.tradeFails != 1 || w.closesInARow != 0 {
		t.Fatalf("the %dth close in a row must fail: %+v, %d fails", carrierClosesInARow, res, w.tradeFails)
	}
}
