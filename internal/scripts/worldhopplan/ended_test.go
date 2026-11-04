package worldhopplan

import (
	"testing"
	"time"
)

// A give-up must end the REQUEST, not every later one. Before Ended, runHop returned from a
// give-up with Leg/Attempts/Last untouched, and Decide answered every later request -- an hour
// later, a day later -- with the same give-up without attempting a login (review revW's probe).
func TestAGiveUpEndsOnlyThatRequest(t *testing.T) {
	now := t0
	later := now.Add(time.Hour)
	cases := []struct {
		name string
		s    State
		want int
	}{
		{"world full on the way home", Attempted(Begin(State{Home: 2, Current: 1}, 2, now), OutcomeWorldFull, now), 2},
		{"connection ceiling on the way home", Attempted(Begin(State{Home: 2, Current: 1}, 2, now), OutcomeCeiling, now), 2},
		{"world full on the way out", Attempted(Begin(State{Home: 2, Current: 2}, 1, now), OutcomeWorldFull, now), 1},
	}
	out := Begin(State{Home: 2, Current: 2}, 1, now)
	for i := 0; i < DefaultLimits.MaxAttempts; i++ {
		out = Attempted(out, OutcomeFailed, now)
	}
	cases = append(cases, struct {
		name string
		s    State
		want int
	}{"an exhausted outward leg", out, 1})

	for _, c := range cases {
		v := View{State: c.s, WantWorld: c.want, WantKnown: true, Now: now, Distinct: true}
		if d := Decide(v); d.Action != ActionGiveUpHere {
			t.Fatalf("%s: the first request gives up (%v: %s)", c.name, d.Action, d.Why)
		}
		ended := Ended(c.s)
		if ended.Leg != 0 || ended.Attempts != 0 || ended.Last != OutcomeNone {
			t.Fatalf("%s: Ended clears the leg: %+v", c.name, ended)
		}
		if len(ended.Recent) != len(c.s.Recent) || !ended.LastAt.Equal(c.s.LastAt) {
			t.Fatalf("%s: Ended keeps the bounds that span requests: %+v", c.name, ended)
		}
		v.State, v.Now = ended, later
		d := Decide(v)
		if d.Action != ActionHop && d.Action != ActionHome {
			t.Errorf("%s: an hour later a new request must attempt a login, got %v (%s)", c.name, d.Action, d.Why)
		}
	}
}

// Ended keeps the flapping bound: a request made straight after a give-up still waits out the
// minimum gap, and a spent window still lands the account instead of hopping.
func TestEndedKeepsTheHopBounds(t *testing.T) {
	s := Attempted(Begin(State{Home: 1, Current: 1}, 2, t0), OutcomeWorldFull, t0)
	v := View{State: Ended(s), WantWorld: 2, WantKnown: true, Now: t0.Add(time.Second), Distinct: true}
	if d := Decide(v); d.Action != ActionWait {
		t.Fatalf("straight after a give-up the minimum gap still applies: %v (%s)", d.Action, d.Why)
	}
	for i := 0; i < DefaultLimits.MaxHops; i++ {
		s = Ended(Attempted(Begin(s, 2, t0), OutcomeWorldFull, t0))
	}
	v = View{State: s, WantWorld: 2, WantKnown: true, Now: t0.Add(DefaultLimits.MinInterval + time.Second), Distinct: true}
	if d := Decide(v); d.Action == ActionHop {
		t.Fatalf("a spent hop budget is not reset by Ended: %v (%s)", d.Action, d.Why)
	}
}
