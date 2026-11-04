package gamestate

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/protocol/serverprot"
)

// viewClockFor makes a State whose view clock reads a clock the test moves.
func viewClockFor(t *testing.T) (*State, *time.Time) {
	t.Helper()
	clock := time.Unix(10_000, 0)
	prev := viewNow
	viewNow = func() time.Time { return clock }
	t.Cleanup(func() { viewNow = prev })
	return New(), &clock
}

// tickAt delivers one PLAYER_INFO to the scene tracker, as Handle does, at the test clock.
func tickAt(s *State) {
	s.mu.Lock()
	s.observeScene(serverprot.PLAYER_INFO, nil)
	s.mu.Unlock()
}

// warm gives the state the rhythm of a session that has been in the game a few seconds: five ticks at the server's pace.
func warm(s *State, clock *time.Time) {
	for i := 0; i < 5; i++ {
		tickAt(s)
		*clock = clock.Add(600 * time.Millisecond)
	}
	*clock = clock.Add(-600 * time.Millisecond)
}

func TestAHealthyViewIsNotLagging(t *testing.T) {
	s, clock := viewClockFor(t)
	if s.ViewLagging() {
		t.Fatal("a state that has seen no tick reads as lagging")
	}
	for i := 0; i < 50; i++ {
		tickAt(s)
		if l := s.ViewLag(); l.Lagging || l.InWindow > 6 {
			t.Fatalf("tick %d at the server's own pace reads as lagging: %+v", i, l)
		}
		*clock = clock.Add(600 * time.Millisecond)
	}
}

// 17 Oct 12:21:27: the tunnels hung and no byte came down for 105 seconds.
func TestASilentViewIsLaggingFromThreeSecondsOn(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(2 * time.Second)
	if s.ViewLagging() {
		t.Error("two seconds without a tick reads as lagging; the server's own hiccups are longer than that")
	}
	*clock = clock.Add(1500 * time.Millisecond)
	l := s.ViewLag()
	if !l.Lagging || !l.Silent || l.CatchingUp {
		t.Fatalf("3.5 s without a tick = %+v, want silent", l)
	}
	if !strings.Contains(l.Why(), "no game tick for 3.5s") {
		t.Errorf("Why() = %q", l.Why())
	}
	*clock = clock.Add(100 * time.Second)
	if !s.ViewLagging() {
		t.Error("a hundred seconds without a tick is not lagging")
	}
}

// The backlog that followed: about 175 ticks in eight seconds. The view is behind real time until it has been read, and a few
// seconds after, while the window still holds it.
func TestACatchUpBurstIsLaggingUntilItIsRead(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(105 * time.Second)
	tickAt(s) // the first tick back
	l := s.ViewLag()
	if l.Silent || !l.Lagging || !l.CatchingUp {
		t.Fatalf("the first tick after the silence = %+v: the backlog follows it, the view is not current yet", l)
	}
	if !strings.Contains(l.Why(), "just come back after 1m45s of silence") {
		t.Errorf("Why() = %q", l.Why())
	}
	for i := 0; i < 175; i++ {
		*clock = clock.Add(45 * time.Millisecond)
		tickAt(s)
		if l := s.ViewLag(); !l.Lagging || !l.CatchingUp {
			t.Fatalf("replayed tick %d = %+v, want catching up", i, l)
		}
	}
	if l := s.ViewLag(); !strings.Contains(l.Why(), "ticks in the last 3s: a backlog is being read") {
		t.Errorf("Why() = %q", l.Why())
	}
	// The queue is drained and the server's own pace is back, but the window still holds the burst: the view is not called current
	// for three seconds after it.
	ticks := 176
	step := func() {
		*clock = clock.Add(600 * time.Millisecond)
		tickAt(s)
		ticks++
	}
	for i := 0; i < 3; i++ {
		step()
	}
	if !s.ViewLagging() {
		t.Error("1.8 s after the last replayed tick, the window still holding the burst, the view reads as current")
	}
	for i := 0; i < 3; i++ {
		step()
	}
	l = s.ViewLag()
	if l.Lagging {
		t.Fatalf("3.6 s after the replay, at the server's pace, the view still reads as lagging: %+v", l)
	}
	if l.Silence != 105*time.Second || l.SinceSilence != ticks {
		t.Errorf("after the burst = %+v, want the 105 s silence and the %d ticks since it on record", l, ticks)
	}
}

// The backlog can come in clumps with gaps between them longer than any tick-to-tick test would call a burst: ten ticks in a
// millisecond, a pause, ten more. The view is behind the whole time.
func TestAClumpedBacklogIsLaggingBetweenTheClumps(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(105 * time.Second)
	for c := 0; c < 17; c++ {
		for i := 0; i < 10; i++ {
			tickAt(s)
			*clock = clock.Add(time.Millisecond)
		}
		*clock = clock.Add(350 * time.Millisecond)
		if l := s.ViewLag(); !l.Lagging {
			t.Fatalf("clump %d: 350 ms after ten ticks the view reads as current: %+v", c, l)
		}
		*clock = clock.Add(350 * time.Millisecond)
	}
	for i := 0; i < 6; i++ { // and the server's own pace again
		*clock = clock.Add(600 * time.Millisecond)
		tickAt(s)
	}
	if l := s.ViewLag(); l.Lagging {
		t.Errorf("3.6 s after the last clump, at the server's pace, the view still lags: %+v", l)
	}
}

// A server that stalled for four seconds and came back at its own pace sends no backlog: the view is current again two seconds after
// the first tick back, not for good lagging.
func TestAStallWithNoBacklogSettlesAfterTwoSeconds(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(4 * time.Second)
	if !s.ViewLagging() {
		t.Fatal("setup: four seconds of silence should lag")
	}
	tickAt(s)
	for i := 0; i < 3; i++ {
		*clock = clock.Add(600 * time.Millisecond)
		tickAt(s)
		if !s.ViewLagging() {
			t.Fatalf("%.1f s after the first tick back the view reads as current (it settles for %s)", float64(i+1)*0.6, ViewSettle)
		}
	}
	*clock = clock.Add(600 * time.Millisecond)
	tickAt(s)
	*clock = clock.Add(600 * time.Millisecond)
	tickAt(s)
	if l := s.ViewLag(); l.Lagging {
		t.Errorf("3 s after the first tick back, at the server's pace, the view lags: %+v", l)
	}
}

func TestAFewCloseTicksAreJitterNotABacklog(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	for i := 0; i < 3; i++ {
		*clock = clock.Add(100 * time.Millisecond)
		tickAt(s)
		if s.ViewLagging() {
			t.Fatalf("%d close ticks read as a backlog: %+v", i+1, s.ViewLag())
		}
	}
	*clock = clock.Add(600 * time.Millisecond)
	tickAt(s)
	if s.ViewLagging() {
		t.Error("the tick after the jitter reads as lagging")
	}
}

func TestALoggedOutStateIsNotLagging(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(time.Minute)
	if !s.ViewLagging() {
		t.Fatal("setup: a minute of silence should lag")
	}
	s.mu.Lock()
	s.observeScene(serverprot.LOGOUT, nil)
	s.LoggedOut = true
	s.mu.Unlock()
	if s.ViewLagging() {
		t.Error("an account that has logged out is lagging; there is no view to be behind")
	}
}

// A session that has seen fewer than viewMinTicks ticks has no rhythm to be late against: a login, a simulated session placed once.
func TestAFreshSessionIsNotJudged(t *testing.T) {
	s, clock := viewClockFor(t)
	tickAt(s)
	tickAt(s)
	*clock = clock.Add(time.Minute)
	if s.ViewLagging() {
		t.Error("a session with two ticks is lagging after a minute")
	}
	tickAt(s)
	*clock = clock.Add(time.Minute)
	if !s.ViewLagging() {
		t.Error("a session with three ticks is not lagging after a minute")
	}
}

func TestOverrideViewLagReportsWhatItIsGiven(t *testing.T) {
	s, _ := viewClockFor(t)
	s.OverrideViewLag(&ViewLag{Lagging: true, Silent: true, Age: 9 * time.Second})
	if l := s.ViewLag(); !l.Lagging || l.Age != 9*time.Second {
		t.Errorf("ViewLag = %+v under an override", l)
	}
	s.OverrideViewLag(&ViewLag{})
	if s.ViewLagging() {
		t.Error("an override of nothing lags")
	}
	s.OverrideViewLag(nil)
	if s.ViewLag() != (ViewLag{}) {
		t.Errorf("ViewLag = %+v with the override removed and no ticks", s.ViewLag())
	}
}

// HOW LONG AGO THE VIEW LAST LAGGED (lageat-1018). The 18 Oct 04:41 death: a silence of seven seconds, then fifteen ticks at once.
// Recent is the worst episode of the last ViewLagMemory, kept after the view is current again.

func TestAHealthyViewHasNoRecentLag(t *testing.T) {
	s, clock := viewClockFor(t)
	for i := 0; i < 80; i++ {
		tickAt(s)
		if r := s.RecentViewLag(); r != (RecentLag{}) {
			t.Fatalf("tick %d at the server's own pace has recent lag %+v", i, r)
		}
		*clock = clock.Add(600 * time.Millisecond)
	}
}

// 04:41:42 to 04:41:50: no tick for 7 seconds is 12 ticks the server ran that the client was not told of, and the 15 that followed in
// three seconds are the same episode, so it is the 15 that are kept.
func TestASilenceAndItsBacklogAreOneEpisodeAndTheWorstIsKept(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(7 * time.Second)
	if r := s.RecentViewLag(); r.Ticks != 12 || r.Ago != 0 {
		t.Fatalf("a silence of 7 s in progress = %+v, want the 12 ticks it has hidden so far", r)
	}
	tickAt(s) // the first tick back
	if r := s.RecentViewLag(); r.Ticks != 12 {
		t.Fatalf("the first tick back = %+v, want the 12 ticks the silence hid", r)
	}
	for i := 0; i < 14; i++ {
		*clock = clock.Add(100 * time.Millisecond)
		tickAt(s)
	}
	if r := s.RecentViewLag(); r.Ticks != 15 {
		t.Fatalf("after the 15-tick backlog = %+v, want 15 (the episode's worst), not a second episode", r)
	}
	// Back at the server's own pace and current again, but it lagged a few seconds ago: the margin is still wanted.
	for i := 0; i < 8; i++ {
		*clock = clock.Add(600 * time.Millisecond)
		tickAt(s)
	}
	l := s.ViewLag()
	if l.Lagging {
		t.Fatalf("setup: the view should be current again: %+v", l)
	}
	// The first paced ticks after the burst still have it in the window, so the episode's worst reading is one or two over 15.
	if l.Recent.Ticks < 15 || l.Recent.Ticks > viewRing || l.Recent.Ago <= 0 || l.Recent.Ago > 10*time.Second {
		t.Errorf("recent lag after the view is current = %+v, want at least 15 ticks seen a few seconds ago", l.Recent)
	}
}

// The memory is three minutes from the episode's last reading, then it is forgotten: healthy play is not slowed for ever.
func TestRecentLagIsForgottenAfterTheMemory(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(7 * time.Second)
	tickAt(s)
	pace := func(d time.Duration) {
		for end := clock.Add(d); clock.Before(end); {
			*clock = clock.Add(600 * time.Millisecond)
			tickAt(s)
		}
	}
	pace(ViewLagMemory - 10*time.Second)
	if r := s.RecentViewLag(); r.Ticks != 12 {
		t.Fatalf("2m50s after the episode = %+v, want it remembered", r)
	}
	pace(20 * time.Second)
	if r := s.RecentViewLag(); r != (RecentLag{}) {
		t.Fatalf("3m10s after the episode = %+v, want it forgotten", r)
	}
}

// Two episodes a minute apart: the worst of the window is reported, not the latest.
func TestRecentLagReportsTheWorstEpisodeNotTheLatest(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	*clock = clock.Add(9 * time.Second) // 15 ticks hidden
	tickAt(s)
	for i := 0; i < 100; i++ { // a minute at the server's pace
		*clock = clock.Add(600 * time.Millisecond)
		tickAt(s)
	}
	*clock = clock.Add(4 * time.Second) // 7 ticks hidden
	tickAt(s)
	r := s.RecentViewLag()
	if r.Ticks != 15 {
		t.Errorf("recent lag = %+v, want the earlier episode's 15 ticks (the later one hid 7)", r)
	}
	if r.Ago < 55*time.Second {
		t.Errorf("recent lag ago = %s, want about a minute: the worst episode, not the latest", r.Ago)
	}
}

// A stall of four seconds that comes back at its own pace hid seven ticks; a few close ticks are jitter and hid none.
func TestAShortStallHidesItsTicksAndJitterHidesNone(t *testing.T) {
	s, clock := viewClockFor(t)
	warm(s, clock)
	for i := 0; i < 3; i++ {
		*clock = clock.Add(100 * time.Millisecond)
		tickAt(s)
	}
	if r := s.RecentViewLag(); r != (RecentLag{}) {
		t.Fatalf("jitter = %+v, want no lag", r)
	}
	*clock = clock.Add(4 * time.Second)
	tickAt(s)
	if r := s.RecentViewLag(); r.Ticks != 7 {
		t.Errorf("a stall of 4 s = %+v, want 7 ticks", r)
	}
}

func TestOverrideViewLagCarriesTheRecentLag(t *testing.T) {
	s, _ := viewClockFor(t)
	s.OverrideViewLag(&ViewLag{Recent: RecentLag{Ticks: 13, Ago: 20 * time.Second}})
	if r := s.RecentViewLag(); r.Ticks != 13 || s.ViewLagging() {
		t.Errorf("under an override: recent %+v, lagging %v; want 13 ticks and a current view", r, s.ViewLagging())
	}
}
