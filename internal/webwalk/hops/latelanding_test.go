package hops

import (
	"testing"
	"time"

	"rsb0t-go/internal/webwalk"
)

// sailWorld is a walkWorld whose player is set down at landTo after the landAfter-th wait, as a ship's p_telejump does
// some ticks after the last dialogue line, and which can be stopped.
type sailWorld struct {
	*walkWorld
	landTo    webwalk.Point
	landAfter int
	waits     int
	stopAt    int
}

func (s *sailWorld) Delay(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	s.waits++
	if s.stopAt > 0 && s.waits >= s.stopAt {
		return false
	}
	if s.waits == s.landAfter {
		s.me = s.landTo
	}
	return s.walkWorld.Delay(d)
}

// ardougneFerry is the Brimhaven to Ardougne crossing as the graph has it.
func ardougneFerry() *webwalk.Edge {
	return &webwalk.Edge{
		Kind: webwalk.KindShip, LocName: "Customs officer", Action: "Pay-fare", LocID: -1,
		From: webwalk.Point{X: 2772, Z: 3234}, To: webwalk.Point{X: 2683, Z: 3268, Level: 1},
		Special: &webwalk.Special{Label: "Brimhaven->Ardougne ship", NPC: "Customs officer", Choose: []string{"Can I journey on this ship?", "Search away", "Ok."}},
	}
}

// The Brimhaven to Ardougne customs officer's dialogue takes 18 s at the median and 24 s at the 90th percentile, and
// the sailing lands a few seconds after its last line. drive's 40 passes (24 s) ended 18 times in 409 crossings a
// second or two before the player landed on the deck (16 Oct 2026), and the walk that read that as a failed crossing
// replanned from the dock and was thrown onto the deck mid-route.
func TestAnAnsweredSailingIsWaitedForAfterTheDialogueBudget(t *testing.T) {
	deck := ardougneFerry().To
	for _, after := range []int{1, 2, 5, lateLandingTicks} {
		w := &sailWorld{walkWorld: newWalkWorld(webwalk.Point{X: 2772, Z: 3232}), landTo: deck, landAfter: after}
		h := &Handler{d: Deps{Client: w, Log: func(string) {}}}
		waited, landed, stopped := h.awaitLanding(ardougneFerry(), lateLandingTicks)
		if stopped || !landed || waited != after {
			t.Errorf("landing after %d ticks: waited %d, landed %v, stopped %v", after, waited, landed, stopped)
		}
	}
}

// A sailing that never lands is still a failure, after a bounded wait, and a stop ends the wait at once.
func TestAnAnsweredSailingThatNeverLandsStillFailsAndAStopEndsTheWait(t *testing.T) {
	never := &sailWorld{walkWorld: newWalkWorld(webwalk.Point{X: 2772, Z: 3232}), landAfter: -1}
	h := &Handler{d: Deps{Client: never, Log: func(string) {}}}
	started := never.now
	waited, landed, stopped := h.awaitLanding(ardougneFerry(), lateLandingTicks)
	if landed || stopped || waited != lateLandingTicks {
		t.Errorf("a ship that never sailed: waited %d, landed %v, stopped %v", waited, landed, stopped)
	}
	if took := never.now.Sub(started); took != lateLandingTicks*tick {
		t.Errorf("the wait took %v, want %d ticks (%v)", took, lateLandingTicks, lateLandingTicks*tick)
	}

	stopping := &sailWorld{walkWorld: newWalkWorld(webwalk.Point{X: 2772, Z: 3232}), landAfter: -1, stopAt: 3}
	h = &Handler{d: Deps{Client: stopping, Log: func(string) {}}}
	if _, landed, stopped := h.awaitLanding(ardougneFerry(), lateLandingTicks); landed || !stopped {
		t.Errorf("a stop request: landed %v, stopped %v, want stopped", landed, stopped)
	}
}

// The wait is for crossings whose dialogue was answered and that are ships: the late landing is not extended to a
// crossing the budget ran out on without a word (nothing happened is its own exit), and the tile the hop starts from is
// never a landing (TestLandedNeverAcceptsTheTileTheHopStartsFrom), so waiting cannot turn a failure into a success.
func TestWaitingForAShipNeverCountsTheDockAsALanding(t *testing.T) {
	w := &sailWorld{walkWorld: newWalkWorld(ardougneFerry().From), landAfter: -1}
	h := &Handler{d: Deps{Client: w, Log: func(string) {}}}
	if _, landed, _ := h.awaitLanding(ardougneFerry(), 3); landed {
		t.Error("standing on the pier counted as a landing on the Ardougne deck")
	}
}
