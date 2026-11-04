package bot

import (
	"testing"
	"time"
)

// planBlind/planTrade are the whole decision behind the woodcutter family's two top-of-Loop
// waits, so they are tested pass by pass on a fake clock.

var gateT0 = time.Date(2026, 9, 23, 2, 58, 1, 0, time.UTC)

// TestBlindGateOnTheLiveFixture is the live fixture: back from the mime, tabs null, the emote mesbox
// open with a continue. The continue is pressed once the blindness has outlasted
// blindPressAfter, not on the very first pass (a crossing's short blind window must not have its
// dialogues answered for it), and the press is the report.
func TestBlindGateOnTheLiveFixture(t *testing.T) {
	var g loopGate
	mesbox := blindObs{CanContinue: true, ModalOpen: true}
	if st := g.planBlind(gateT0, mesbox); st != (blindStep{}) {
		t.Fatalf("first pass = %+v, want nothing yet", st)
	}
	if st := g.planBlind(gateT0.Add(1200*time.Millisecond), mesbox); st != (blindStep{}) {
		t.Fatalf("second pass = %+v, want nothing yet", st)
	}
	if st := g.planBlind(gateT0.Add(3600*time.Millisecond), mesbox); !st.Press {
		t.Fatalf("after %s blind the continue must be pressed: %+v", blindPressAfter, st)
	}
	if st := g.planBlind(gateT0.Add(4800*time.Millisecond), mesbox); st.Press {
		t.Fatal("presses are throttled")
	}
}

// TestBlindGateStepsAsideForAnEvent: inside the cage (or with the Guardian busy) the tabs are the
// event's, nothing is pressed and the clock does not run -- so a long mime cannot age the gate.
func TestBlindGateStepsAsideForAnEvent(t *testing.T) {
	var g loopGate
	owned := blindObs{EventOwned: true, CanContinue: true, ModalOpen: true}
	for i := 0; i < 600; i++ { // ten minutes of passes inside the event
		if st := g.planBlind(gateT0.Add(time.Duration(i)*time.Second), owned); st != (blindStep{}) {
			t.Fatalf("pass %d during the event = %+v, want nothing", i, st)
		}
	}
	after := gateT0.Add(10 * time.Minute)
	if st := g.planBlind(after, blindObs{CanContinue: true, ModalOpen: true}); st != (blindStep{}) {
		t.Fatalf("first pass after the event = %+v: the clock must start now, not at the event", st)
	}
	if st := g.planBlind(after.Add(blindPressAfter), blindObs{CanContinue: true, ModalOpen: true}); !st.Press {
		t.Fatalf("and run from there: %+v", st)
	}
}

// TestBlindGateClosesAModalItCannotContinue and is bounded: at most blindCloseMax closes, spaced.
func TestBlindGateClosesAModalItCannotContinue(t *testing.T) {
	var g loopGate
	stuck := blindObs{ModalOpen: true}
	g.planBlind(gateT0, stuck)
	if st := g.planBlind(gateT0.Add(30*time.Second), stuck); st.Close {
		t.Fatal("no close before blindCloseAfter")
	}
	closes := 0
	for s := 60; s <= 20*60; s++ {
		if st := g.planBlind(gateT0.Add(time.Duration(s)*time.Second), stuck); st.Close {
			closes++
		}
	}
	if closes != blindCloseMax {
		t.Fatalf("closes over twenty minutes = %d, want exactly %d", closes, blindCloseMax)
	}
	var h loopGate
	h.planBlind(gateT0, blindObs{})
	if st := h.planBlind(gateT0.Add(2*time.Minute), blindObs{}); st.Close || st.Press {
		t.Fatalf("with nothing open there is nothing to press or close: %+v", st)
	}
}

// TestBlindGateGoesQuietAfterOneLongLine: the only periodic output is ONE line at
// blindLongAfter, so the fleet watchdog sees a silent, stuck account rather than a talkative
// one it must leave alone.
func TestBlindGateGoesQuietAfterOneLongLine(t *testing.T) {
	var g loopGate
	lines := 0
	for s := 0; s <= 30*60; s++ {
		st := g.planBlind(gateT0.Add(time.Duration(s)*time.Second), blindObs{})
		if st.LogFirst {
			lines++
		}
		if st.LogLong {
			lines++
			if got := time.Duration(s) * time.Second; got < blindLongAfter {
				t.Fatalf("long line at %s, before %s", got, blindLongAfter)
			}
		}
	}
	if lines != 2 {
		t.Fatalf("lines over thirty blind minutes = %d, want 2 (first seen, and one stuck line)", lines)
	}
}

// TestBlindGateShortWindowIsSilent: a crossing's few seconds of null tabs, nothing open, write
// nothing, exactly as before.
func TestBlindGateShortWindowIsSilent(t *testing.T) {
	var g loopGate
	for ms := 0; ms < int(blindLogAfter/time.Millisecond); ms += 1200 {
		if st := g.planBlind(gateT0.Add(time.Duration(ms)*time.Millisecond), blindObs{}); st != (blindStep{}) {
			t.Fatalf("pass at %dms = %+v, want silence", ms, st)
		}
	}
	if g.blindLogged {
		t.Fatal("nothing was reported, so no recovery line is owed")
	}
}

func TestBlindGatePressesAreBounded(t *testing.T) {
	var g loopGate
	reopening := blindObs{CanContinue: true, ModalOpen: true} // a dialogue that keeps coming back
	presses := 0
	for s := 0; s <= 10*60; s++ {
		if st := g.planBlind(gateT0.Add(time.Duration(s)*time.Second), reopening); st.Press {
			presses++
		}
	}
	if presses != blindPressMax {
		t.Fatalf("presses = %d, want %d", presses, blindPressMax)
	}
}

func TestTradeGate(t *testing.T) {
	var g loopGate
	if st := g.planTrade(gateT0, false); st != (tradeStep{}) {
		t.Fatalf("no trade, nothing to do: %+v", st)
	}
	if st := g.planTrade(gateT0, true); !st.LogFirst || st.Decline {
		t.Fatalf("first pass = %+v", st)
	}
	if st := g.planTrade(gateT0.Add(30*time.Second), true); st != (tradeStep{}) {
		t.Fatalf("30s in = %+v, want nothing", st)
	}
	if st := g.planTrade(gateT0.Add(tradeGateLong), true); !st.LogLong || st.Decline {
		t.Fatalf("at %s = %+v, want the one long line", tradeGateLong, st)
	}
	if st := g.planTrade(gateT0.Add(tradeGateDecline), true); !st.Decline {
		t.Fatalf("at %s = %+v, want a decline", tradeGateDecline, st)
	}
	if st := g.planTrade(gateT0.Add(tradeGateDecline+2*time.Second), true); st.Decline {
		t.Fatal("one decline per window, not one per pass")
	}
	g.planTrade(gateT0.Add(4*time.Minute), false)
	if st := g.planTrade(gateT0.Add(5*time.Minute), true); !st.LogFirst {
		t.Fatal("a new trade after the old one closed is a new spell")
	}
}
