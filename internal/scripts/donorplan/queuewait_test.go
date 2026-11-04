package donorplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// TestAQueuedDonorIsNotCutBeforeItsTurn replays acct144 on 3dfb543a: quoted 6m0s at 02:14:09,
// it gave up at 02:19:09, exactly 300s after arriving, with 180s of its own promise still
// standing -- and 10 donors that announced after it were served first. With the queue served in
// order and the quote refreshed after every trade, it is still waiting at arrival + 5m1s.
// Control: the old MeetTimeoutComing of 5 minutes returns MeetGiveUp here.
func TestAQueuedDonorIsNotCutBeforeItsTurn(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 26, start)
	h.ArrivedAt = start
	// The mule's quotes as the queue drains: 6m at arrival, then one cycle less every 20s.
	var until time.Time
	for heard := time.Duration(0); heard <= 5*time.Minute; heard += 20 * time.Second {
		eta := 6*time.Minute - heard
		until = start.Add(heard + eta + 30*time.Second) // donorrunner's own comingGrace
	}
	v := meetingView(start.Add(5*time.Minute+time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetWait {
		t.Fatalf("a donor whose turn is a minute away was cut at arrival+5m1s: got %s", got)
	}
}

// TestAPromiseThatStopsBeingRefreshedLapses: a mule that goes quiet (crashed, logged out, stuck)
// stops refreshing, and the promise it last made ends the wait at its eta plus the grace.
func TestAPromiseThatStopsBeingRefreshedLapses(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	h.ArrivedAt = start
	until := start.Add(90 * time.Second) // last heard at 0 with a 60s eta, plus the runner's 30s
	v := meetingView(until.Add(ComingGrace-time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetWait {
		t.Errorf("inside the grace: %s", got)
	}
	v = meetingView(until.Add(ComingGrace+time.Second), stand)
	v.WaitUntil = &until
	if got := h.Meet(v); got != MeetGiveUp {
		t.Errorf("a promise nobody refreshed must lapse: %s", got)
	}
}

// TestTheHardCapBoundsAMuleThatPromisesButNeverServes: refreshed forever, still bounded.
func TestTheHardCapBoundsAMuleThatPromisesButNeverServes(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	h.ArrivedAt = start
	for _, at := range []time.Duration{MeetTimeoutComing - time.Second, MeetTimeoutComing + time.Second} {
		until := start.Add(at + time.Minute) // always a minute away
		v := meetingView(start.Add(at), stand)
		v.WaitUntil = &until
		got := h.Meet(v)
		if at < MeetTimeoutComing && got != MeetWait {
			t.Errorf("at %s, inside the cap: %s", at, got)
		}
		if at > MeetTimeoutComing && got != MeetGiveUp {
			t.Errorf("at %s, past the cap: %s", at, got)
		}
	}
	if MeetTimeoutComing != 10*time.Minute {
		t.Errorf("MeetTimeoutComing = %s, want 10m", MeetTimeoutComing)
	}
}

// TestThreeFailedTradesEndTheHandoff: consecutive screens that closed with nothing crossing
// (Acct181 on 3dfb543a closed ten in a row) end the hand-off at once; one that crosses resets.
func TestThreeFailedTradesEndTheHandoff(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	mule := tile.Tile{X: 2809, Z: 3441}
	beside := tile.Tile{X: 2809, Z: 3442}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 3, start)
	h.ArrivedAt = start
	until := start.Add(5 * time.Minute)
	view := func(at time.Duration) MeetView {
		return MeetView{Now: start.Add(at), Me: &beside, Mule: &mule, Stand: stand, WaitUntil: &until}
	}
	h.Traded(0, 3, start.Add(10*time.Second))
	h.Traded(0, 3, start.Add(20*time.Second))
	if got := h.Meet(view(30 * time.Second)); got == MeetGiveUp {
		t.Fatalf("two failures must not end it yet")
	}
	h.Traded(2, 1, start.Add(40*time.Second)) // something crossed: the count starts again
	h.Traded(0, 1, start.Add(50*time.Second))
	h.Traded(0, 1, start.Add(60*time.Second))
	if got := h.Meet(view(70 * time.Second)); got == MeetGiveUp {
		t.Fatalf("a crossing trade must reset the failure count")
	}
	h.Traded(0, 1, start.Add(80*time.Second))
	if got := h.Meet(view(90 * time.Second)); got != MeetGiveUp {
		t.Errorf("three failed screens in a row: got %s, want %s", got, MeetGiveUp)
	}
}

func TestDeferDraw(t *testing.T) {
	for _, c := range []struct {
		queue  int
		cycle  time.Duration
		want   time.Duration
		defer_ bool
	}{
		{0, 30 * time.Second, 0, false},           // nobody queued
		{5, 0, 0, false},                          // an old mule that carries no cycle
		{12, 15 * time.Second, 0, false},          // exactly three minutes: go
		{13, 15 * time.Second, time.Minute, true}, // a little over: the minimum deferral
		{20, 15 * time.Second, 2 * time.Minute, true},
		{46, 30 * time.Second, DrawGateMaxDefer, true}, // the 00:43 crush: capped
	} {
		got, ok := DeferDraw(c.queue, c.cycle)
		if ok != c.defer_ || got != c.want {
			t.Errorf("DeferDraw(%d, %s) = %s, %v; want %s, %v", c.queue, c.cycle, got, ok, c.want, c.defer_)
		}
	}
}
