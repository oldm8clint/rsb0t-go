package freshstartrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// DON'T WAIT FOR A BOAT NOBODY BOARDED.
//
// SailLeg waits sailCompleteTimeout for the server's set_sail teleport after BoardShip reports it
// has nothing left to do at this dock. BoardShip reports that for two completely different
// reasons — a boarding dialogue was driven to its end, or no boarding NPC was found at all — and
// only the first can produce a teleport. Conflating them cost the live fleet 35 seconds a turn
// for nothing.
//
// Measured 2026-09-22 on the 110-account ScarabSwarm cohort
// (run/logs/fleet-20260922-101543-232-16204.log): 8,367 "boat leg: Brimhaven ship -> Ardougne ->
// destination" against 6,409 "timed out waiting for the sail to complete" — 77% of every boat leg
// the fleet attempted — with acct42 two tiles off freshstart.BrimhavenPier producing that exact
// pair every 41 seconds (35s wait plus BoardShip's own delays) for 25 minutes and logging nothing
// else at all. 6,409 x 35s is about 62 hours of fleet time.
//
// The waits are the only part of this that can be asserted without a live session, and they are
// the part that mattered: one is a wait for something already in flight, the other a look-again
// interval for a wandering NPC.
func TestTheNoBoardingWaitIsAnIntervalNotASailTimeout(t *testing.T) {
	if sailNoBoardingWait >= sailCompleteTimeout {
		t.Errorf("sailNoBoardingWait (%v) is not shorter than sailCompleteTimeout (%v); an account "+
			"holding station for a wandering NPC must look again sooner than one waiting out a "+
			"teleport that is already on its way", sailNoBoardingWait, sailCompleteTimeout)
	}
	if sailNoBoardingWait <= 0 {
		t.Fatalf("sailNoBoardingWait = %v; it is the only brake on the hold-station path now that "+
			"the sail wait no longer runs there, and SailLeg's own WalkNear answers a zero-tile "+
			"walk instantly with no packet sent", sailNoBoardingWait)
	}
}

// The hold-station line names a real dock and a real NPC, so it has to stay rate-limited: one
// wandering Customs officer can have a hundred accounts queued for it, and this project has
// already produced a 6.7 GB fleet log once from a line nobody bounded.
func TestHoldingLineIsRateLimitedPerAccount(t *testing.T) {
	p := NewSailProgress()
	if !p.noteNoBoarding() {
		t.Fatal("the first hold at a dock wrote nothing; the operator needs to see the reason once")
	}
	if p.noteNoBoarding() {
		t.Error("two hold lines in a row from one account; want at most one a minute, because a " +
			"dock full of accounts each writing every 8s is how the fleet log becomes unreadable")
	}
	p.noBoardingAt = time.Now().Add(-2 * time.Minute)
	if !p.noteNoBoarding() {
		t.Error("an account still holding a minute later wrote nothing; the line has to come back, " +
			"or a permanently stuck dock looks identical to a transient one")
	}
	// A caller that passes no SailProgress (stuck-detection off) still gets its log, matching
	// every other line in SailLeg.
	var none *SailProgress
	if !none.noteNoBoarding() {
		t.Error("a nil SailProgress swallowed the line; nil turns stuck-detection off, not logging")
	}
}

// The hold branch only fires where SailLeg has deliberately parked the account on its own tile —
// the Brimhaven pier arrival case. Anywhere else the walk target is a real destination and the
// walk itself takes the time, so there is nothing to brake.
func TestOnlyThePierHoldCaseParksTheAccountOnItsOwnTile(t *testing.T) {
	atPier := tile.Tile{X: freshstart.BrimhavenPier.X, Z: freshstart.BrimhavenPier.Z - 2}
	if tile.Chebyshev(atPier, freshstart.BrimhavenPier) > 5 {
		t.Fatalf("%v is not inside SailLeg's own 5-tile pier-arrival radius, so this test is not "+
			"exercising the hold case it claims to", atPier)
	}
	if !freshstart.OnKaramjaSide(atPier) || freshstart.TravelLeg(atPier) != freshstart.LegDestination {
		t.Errorf("an account at %v is not on the Karamja side of the destination leg; the hold "+
			"branch is reached through that leg's own walk-target switch", atPier)
	}
	// And it is inside the 20-tile radius that makes SailLeg look for a boarding NPC at all
	// (skipBoard), which is what makes "found nobody" possible here in the first place.
	if tile.Chebyshev(atPier, freshstart.BrimhavenPier) > 20 {
		t.Errorf("%v is outside SailLeg's skipBoard radius, so BoardShip would never run and the "+
			"hold branch could not be reached", atPier)
	}
}
