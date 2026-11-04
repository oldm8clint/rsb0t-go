package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// The trawler gate: the crew takes the account only between two rounds (the new round's delivery made, no shop visited),
// for one stint, and a round that has held it past the cap lets it go anyway.
func TestTrawlerReady(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	ring := []Station{{Kind: KindDeliver, World: 1}, {Kind: KindBuy, Site: "Hickton", World: 1}, {Kind: KindDraw, World: 1},
		{Kind: KindBuy, Site: "Betty", World: 1}, {Kind: KindHop, World: 1}}
	round := func(cursor int, done ...string) State {
		st := State{Round: 765, RoundStarted: now.Add(-10 * time.Minute), Ring: ring, Cursor: cursor, Done: map[string]StationResult{}}
		for _, k := range done {
			st.Done[k] = StationResult{Outcome: OutcomeDelivered}
		}
		return st
	}
	stint, holdMax := 20*time.Minute, 3*time.Hour
	cases := []struct {
		name   string
		st     State
		ready  bool
		forced bool
		why    string
	}{
		{"fresh state: no round under way", State{}, true, false, "no round under way"},
		{"delivery made, first shop not started", round(1, "deliver@1"), true, false, "round=764 done and round=765's delivery made"},
		{"delivery still to make", round(0), false, false, "delivery to the mule first"},
		{"mid-round", round(3, "deliver@1", "Hickton@1", "draw@1"), false, false, "under way at Betty@1 (3 of 5"},
		{"jumped past the delivery", round(2), false, false, "under way at draw@1"},
		{"closed round", State{Round: 765, RoundStarted: now.Add(-time.Hour)}, false, false, "closed"},
		{"route walked", round(5, "deliver@1"), false, false, "closed"},
		{"hop pending", func() State { s := round(1, "deliver@1"); s.HopPending = 3; return s }(), false, false, "hop to world 3"},
		{"Lundail trip", func() State { s := round(1, "deliver@1"); s.Trip = fleetplan.Trip{Active: true}; return s }(), false, false, "Mage Bank"},
		{"stint running", func() State {
			s := round(1, "deliver@1")
			s.TrawlerRound, s.TrawlerSince = 765, now.Add(-5*time.Minute)
			return s
		}(), true, false, "delivery made"},
		{"stint over", func() State {
			s := round(1, "deliver@1")
			s.TrawlerRound, s.TrawlerSince = 765, now.Add(-25*time.Minute)
			return s
		}(), false, false, "stint of round=765"},
		{"last round's stint does not count", func() State {
			s := round(1, "deliver@1")
			s.TrawlerRound, s.TrawlerSince = 764, now.Add(-25*time.Minute)
			return s
		}(), true, false, "delivery made"},
		{"stalled round past the cap", func() State {
			s := round(3, "deliver@1", "Hickton@1", "draw@1")
			s.RoundStarted = now.Add(-4 * time.Hour)
			return s
		}(), true, true, "joining anyway"},
		{"stalled round after a stint: the hold restarts at the stint's end", func() State {
			s := round(3, "deliver@1", "Hickton@1", "draw@1")
			s.RoundStarted, s.TrawlerRound, s.TrawlerSince = now.Add(-4*time.Hour), 765, now.Add(-time.Hour)
			return s
		}(), false, false, "stint of round=765"},
		{"stalled round, cap after the stint", func() State {
			s := round(3, "deliver@1", "Hickton@1", "draw@1")
			s.RoundStarted, s.TrawlerRound, s.TrawlerSince = now.Add(-5*time.Hour), 765, now.Add(-4*time.Hour)
			return s
		}(), true, true, "joining anyway"},
	}
	for _, c := range cases {
		v := TrawlerReady(c.st, now, stint, holdMax, time.Time{})
		if v.Ready != c.ready || v.Forced != c.forced || !strings.Contains(v.Why, c.why) {
			t.Errorf("%s: %+v, want ready %v forced %v why ~%q", c.name, v, c.ready, c.forced, c.why)
		}
	}
	// No cap: a stalled round holds for good. No stint: the account stays until the block ends.
	stalled := round(3, "deliver@1", "Hickton@1", "draw@1")
	stalled.RoundStarted = now.Add(-10 * time.Hour)
	if v := TrawlerReady(stalled, now, stint, 0, time.Time{}); v.Ready {
		t.Errorf("holdMax 0 must not force: %+v", v)
	}
	long := round(1, "deliver@1")
	long.TrawlerRound, long.TrawlerSince = 765, now.Add(-5*time.Hour)
	if v := TrawlerReady(long, now, 0, holdMax, time.Time{}); !v.Ready {
		t.Errorf("stint 0 is until the block ends: %+v", v)
	}
	// The cap counts from when the gate first held, not from an old round's start.
	old := round(3, "deliver@1", "Hickton@1", "draw@1")
	old.RoundStarted = now.Add(-5 * time.Hour)
	if v := TrawlerReady(old, now, stint, holdMax, now.Add(-time.Hour)); v.Ready {
		t.Errorf("held one hour of a 180-minute cap must hold: %+v", v)
	}
	if v := TrawlerReady(old, now, stint, holdMax, now.Add(-4*time.Hour)); !v.Ready || !v.Forced {
		t.Errorf("held four hours must force: %+v", v)
	}
	// Round 0 with no ring still ends its stint.
	zero := State{TrawlerSince: now.Add(-time.Hour)}
	if v := TrawlerReady(zero, now, stint, holdMax, time.Time{}); v.Ready {
		t.Errorf("round 0 past its stint must hold: %+v", v)
	}
}

// NoteTrawlerJoin starts the stint's clock once per round, and again only after a stint has lapsed (a forced join).
func TestNoteTrawlerJoin(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	st := State{Round: 765}
	if !st.NoteTrawlerJoin(now, 20*time.Minute) || st.TrawlerRound != 765 || !st.TrawlerSince.Equal(now) {
		t.Fatalf("first join not noted: %+v", st)
	}
	if st.NoteTrawlerJoin(now.Add(10*time.Minute), 20*time.Minute) || !st.TrawlerSince.Equal(now) {
		t.Errorf("a restart inside the stint must keep its clock: %v", st.TrawlerSince)
	}
	if !st.NoteTrawlerJoin(now.Add(30*time.Minute), 20*time.Minute) {
		t.Error("a join after the stint lapsed starts a new one")
	}
	st.Round++
	if !st.NoteTrawlerJoin(now.Add(40*time.Minute), 20*time.Minute) || st.TrawlerRound != 766 {
		t.Errorf("a new round's join not noted: %+v", st)
	}
}

func TestTrawlerGateSettings(t *testing.T) {
	r, err := DefaultSettings().Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.TrawlerStint() != 20*time.Minute || r.TrawlerRoundHoldMax() != 3*time.Hour {
		t.Errorf("defaults: stint %s hold %s", r.TrawlerStint(), r.TrawlerRoundHoldMax())
	}
	for _, bad := range []func(*Settings){
		func(s *Settings) { s.TrawlerStintMinutes = -1 },
		func(s *Settings) { s.TrawlerStintMinutes = MaxTrawlerStintMinutes + 1 },
		func(s *Settings) { s.TrawlerRoundHoldMaxMinutes = 5 },
		func(s *Settings) { s.TrawlerRoundHoldMaxMinutes = MaxTrawlerRoundHoldMaxMinutes + 1 },
	} {
		s := DefaultSettings()
		bad(&s)
		if _, err := s.Resolve(); err == nil || !strings.Contains(err.Error(), "trawler") {
			t.Errorf("not refused: %+v (%v)", s, err)
		}
	}
	s := DefaultSettings()
	s.TrawlerStintMinutes, s.TrawlerRoundHoldMaxMinutes = 0, 0
	if _, err := s.Resolve(); err != nil {
		t.Errorf("0 and 0 are allowed: %v", err)
	}
}
