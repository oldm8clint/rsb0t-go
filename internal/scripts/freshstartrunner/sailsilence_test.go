package freshstartrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// sailThrottleSession is the smallest thing satisfying actions.Session: the throttles are
// keyed on the session VALUE and never call through it, so nothing here needs to work.
type sailThrottleSession struct{ state *gamestate.State }

func (s *sailThrottleSession) Send(func(*packet.Packet)) error { return nil }
func (s *sailThrottleSession) State() *gamestate.State {
	if s.state == nil {
		s.state = gamestate.New()
	}
	return s.state
}
func (s *sailThrottleSession) Cheats() *anticheat.Counters { return &anticheat.Counters{} }

// Live 2026-09-22: eight ProgressiveFisher accounts sat at 2780,3214 on Karamja for a whole
// 57-minute run at 0 xp. Each logged "phase: sailing to Catherby" once and then nothing --
// acct80's entire run is six startup lines plus 178 copies of the Guardian's unrelated drift
// message. They were returning through SailLeg's `if !boarded { return false }`, which wrote
// no line at all, so "stuck at the boarding step" looked exactly like "never tried".
//
// These hold the rate limiter at both ends. An unconditional log is not the answer either:
// this same function once wrote 8,367 "boat leg" lines in one afternoon.

func TestUnboardedIsReportedTheFirstTime(t *testing.T) {
	p := NewSailProgress()
	if !p.noteUnboarded() {
		t.Fatal("the first stuck-boarding return said nothing -- that is the half-hour of " +
			"silence this exists to end")
	}
}

func TestUnboardedIsNotRepeatedEveryPoll(t *testing.T) {
	p := NewSailProgress()
	p.noteUnboarded()
	if p.noteUnboarded() {
		t.Fatal("wrote twice in a row; SailLeg is called several times a second, which is how " +
			"8,367 lines of one message reached the fleet log in an afternoon")
	}
}

func TestUnboardedSpeaksAgainAfterItsWindow(t *testing.T) {
	p := NewSailProgress()
	p.noteUnboarded()
	p.unboardedAt = time.Now().Add(-2 * time.Minute)
	if !p.noteUnboarded() {
		t.Fatal("never spoke again -- a permanently silenced line is the original bug, not a fix")
	}
}

// A nil SailProgress means stuck-detection is off, and every other log in SailLeg still
// writes in that case. This one must not be the exception, or a caller that passes nil goes
// back to being silent.
func TestUnboardedAlwaysWritesWithoutProgressTracking(t *testing.T) {
	var p *SailProgress
	if !p.noteUnboarded() || !p.noteUnboarded() {
		t.Fatal("a nil SailProgress suppressed the line")
	}
}

// The two throttles must not share state: an account holding at a dock for a missing NPC and
// one stuck mid-dialogue are different conditions, and one must not silence the other.
func TestTheTwoSailThrottlesAreIndependent(t *testing.T) {
	p := NewSailProgress()
	if !p.noteNoBoarding() {
		t.Fatal("noteNoBoarding did not write the first time")
	}
	if !p.noteUnboarded() {
		t.Fatal("noteNoBoarding silenced noteUnboarded -- two different conditions, one budget")
	}
}

// The approach-failure throttle, on the same once-a-minute budget as the other two sail
// lines. Eight accounts stacked on one tile all failing the same walk would otherwise write
// three lines a minute each.
func TestApproachFailureIsReportedThenThrottled(t *testing.T) {
	d := &Deps{}
	// No session: always writes, matching every other log in this file.
	if !noteApproachFailed(d) || !noteApproachFailed(d) {
		t.Fatal("a Deps with no session suppressed the line")
	}
}

func TestApproachFailureThrottleIsPerSession(t *testing.T) {
	a, b := &sailThrottleSession{}, &sailThrottleSession{}
	da, db := &Deps{Session: a}, &Deps{Session: b}
	if !noteApproachFailed(da) {
		t.Fatal("first call for session a did not write")
	}
	if noteApproachFailed(da) {
		t.Fatal("wrote twice in a row for the same session")
	}
	if !noteApproachFailed(db) {
		t.Fatal("one account's throttle silenced another's -- eight accounts stuck on one " +
			"tile would report as one")
	}
}

func TestApproachFailureSpeaksAgainAfterItsWindow(t *testing.T) {
	s := &sailThrottleSession{}
	d := &Deps{Session: s}
	noteApproachFailed(d)
	approachFailedAt.Store(s, time.Now().Add(-2*time.Minute))
	if !noteApproachFailed(d) {
		t.Fatal("never spoke again -- a permanently silenced line is the original bug")
	}
}

// The staging ladder in SailLeg picks a waypoint by DISTANCE, which is the wrong question on
// an island made of two collision-pack components. Live 2026-09-22, acct92 sat at 2758,3196
// for two entire runs at 0 xp logging "no path from 2758,3196,0 to 2816,3182,0: unreachable"
// 118 times: it is 38 tiles from BrimhavenPier and 58 from KaramjaMainlandGate, so distance
// aimed it at the gate -- which is unreachable from there (21,951 expansions, the whole
// component exhausted) while the pier is reachable in 76.
//
// These assert the geometry the fix depends on, so a later change to either constant fails
// here rather than silently re-stranding an account.
func TestThePierFixtureTileIsNearerThePierThanTheDistanceLadderAssumes(t *testing.T) {
	stuck := tile.Tile{X: 2758, Z: 3196}
	toPier := tile.Chebyshev(stuck, freshstart.BrimhavenPier)
	toGate := tile.Chebyshev(stuck, freshstart.KaramjaMainlandGate)
	if toPier <= 5 || toGate <= 3 {
		t.Fatalf("2758,3196 now falls into an earlier rung (pier %d, gate %d); the default "+
			"branch this fix guards is no longer the one it takes", toPier, toGate)
	}
	if toGate <= toPier {
		t.Fatalf("the gate (%d) is no longer further than the pier (%d), so distance would "+
			"not have chosen it and this test no longer describes the live bug", toGate, toPier)
	}
}
