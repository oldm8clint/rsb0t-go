package swarmrally

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var gateT0 = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func testGatePolicy(quorum, ahead int) GatePolicy {
	p := DefaultGatePolicy()
	p.Quorum, p.GearAhead = quorum, ahead
	return p
}

func newTestGate(t *testing.T, quorum, ahead int) *Gate {
	t.Helper()
	g, err := NewGate(testGatePolicy(quorum, ahead))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func stageN(g *Gate, prefix string, n, world int, now time.Time) []string {
	var names []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s%d", prefix, i)
		g.Report(name, Member{World: world, State: MemberStaged, Graduate: true}, now)
		names = append(names, name)
	}
	return names
}

func graduates(g *Gate, prefix string, n int, now time.Time) {
	for i := 0; i < n; i++ {
		g.Report(fmt.Sprintf("%s%d", prefix, i), Member{World: 2, State: MemberReady, Graduate: true}, now)
	}
}

func TestPolicyValidity(t *testing.T) {
	if err := DefaultGatePolicy().Valid(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*GatePolicy){
		"quorum":  func(p *GatePolicy) { p.Quorum = 0 },
		"ahead":   func(p *GatePolicy) { p.GearAhead = -1 },
		"callFor": func(p *GatePolicy) { p.CallFor = 0 },
		"hold":    func(p *GatePolicy) { p.HomeHold = -time.Second },
		"world":   func(p *GatePolicy) { p.World = -1 },
		"stale":   func(p *GatePolicy) { p.StagedStale = 0 },
	} {
		p := DefaultGatePolicy()
		mut(&p)
		if p.Valid() == nil {
			t.Errorf("%s: an invalid policy was accepted", name)
		}
		if _, err := NewGate(p); err == nil {
			t.Errorf("%s: NewGate accepted it", name)
		}
	}
}

// The release of held graduates: Quorum + GearAhead graduates, not one fewer, and it never closes.
func TestGearOpenLatchesAtQuorumPlusAheadNotBefore(t *testing.T) {
	g := newTestGate(t, 5, 2)
	graduates(g, "g", 6, gateT0)
	if g.GearOpen(gateT0) {
		t.Fatal("6 graduates released a gear-up that needs 7")
	}
	// A stat-short account is not a graduate however it is marked.
	g.Report("short", Member{World: 2, State: MemberTraining, Graduate: true, Short: []Shortfall{{"defence", 47, 50}}}, gateT0)
	if g.GearOpen(gateT0) {
		t.Fatal("a graduate under a floor was counted")
	}
	g.Report("g6", Member{World: 2, State: MemberReady, Graduate: true}, gateT0)
	if !g.GearOpen(gateT0) {
		t.Fatal("7 graduates did not release the gear-up")
	}
	// Monotone: the graduates leaving (or going stale) does not close it.
	if !g.GearOpen(gateT0.Add(24 * time.Hour)) {
		t.Fatal("the latch closed")
	}
	if ev := g.Events(); len(ev) != 1 || !strings.Contains(ev[0], "GEAR-UP RELEASED") || !strings.Contains(ev[0], "7 graduate(s)") {
		t.Fatalf("events: %q", ev)
	}
	if ev := g.Events(); len(ev) != 0 {
		t.Fatalf("events drain once: %q", ev)
	}
}

func TestGearAheadZeroReleasesAtOnce(t *testing.T) {
	g := newTestGate(t, 100, 0)
	if !g.GearOpen(gateT0) {
		t.Fatal("kqGateGearAhead 0 gears at once")
	}
}

func TestCallRosterIsExactlyTheStagedSetAtThatInstant(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "s", 2, 1, gateT0)
	if v := g.Call("s0", gateT0, 1); v.Live {
		t.Fatalf("a call below the quorum: %+v", v)
	}
	// Four are staged when the third arrives: the roster is all four, not the first three.
	stageN(g, "t", 2, 2, gateT0.Add(time.Second))
	v := g.Call("s0", gateT0.Add(time.Second), 1)
	if !v.Live || !v.Member || v.ID != 1 || v.Size != 4 || v.World != 1 {
		t.Fatalf("call: %+v", v)
	}
	for _, n := range []string{"s0", "s1", "t0", "t1", "T1"} { // names compare case-blind
		if !g.Call(n, gateT0.Add(2*time.Second), 1).Member {
			t.Errorf("%s is not on the roster", n)
		}
	}
	ev := g.Events()
	var call string
	for _, e := range ev {
		if strings.HasPrefix(e, "KQ CALL 1") {
			call = e
		}
	}
	if !strings.Contains(call, "4 staged (w1 2, w2 2)") || !strings.Contains(call, "world 1") || !strings.Contains(call, "2 account(s) hop") || !strings.Contains(call, "45m") {
		t.Fatalf("the call line: %q (events %q)", call, ev)
	}
}

func TestAccountStagedAfterTheCallIsNotCalled(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "s", 3, 1, gateT0)
	if !g.Call("s0", gateT0, 1).Live {
		t.Fatal("no call")
	}
	g.Report("late", Member{World: 1, State: MemberStaged, Graduate: true}, gateT0.Add(time.Minute))
	v := g.Call("late", gateT0.Add(time.Minute), 1)
	if !v.Live || v.Member {
		t.Fatalf("a latecomer must wait for the next hundred: %+v", v)
	}
}

func TestNoSecondCallWhileOneIsLive(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "a", 3, 1, gateT0)
	first := g.Call("a0", gateT0, 1)
	stageN(g, "b", 5, 1, gateT0.Add(time.Minute)) // a fresh quorum, but a call is live
	again := g.Call("b0", gateT0.Add(time.Minute), 1)
	if again.ID != first.ID || again.Member {
		t.Fatalf("a second call opened inside the first: first %+v again %+v", first, again)
	}
}

func TestNextCallNeedsAFreshQuorumOfUncalled(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "a", 3, 1, gateT0)
	g.Call("a0", gateT0, 1)
	// a0 and a1 leave the gate; a2 never does and keeps reporting staged.
	now := gateT0.Add(30 * time.Minute)
	g.Report("a0", Member{World: 1, State: MemberCalled, Graduate: true, Place: PlacePast}, now)
	g.Report("a1", Member{World: 1, State: MemberCalled, Graduate: true, Place: PlacePast}, now)
	g.Report("a2", Member{World: 1, State: MemberStaged, Graduate: true}, now)
	g.Report("d0", Member{World: 1, State: MemberStaged, Graduate: true}, now)
	after := gateT0.Add(46 * time.Minute) // the call has lapsed
	g.Report("a2", Member{World: 1, State: MemberStaged, Graduate: true}, after)
	g.Report("d0", Member{World: 1, State: MemberStaged, Graduate: true}, after)
	if v := g.Call("a2", after, 1); v.Live {
		t.Fatalf("2 staged is not a quorum of 3: %+v", v)
	}
	g.Report("e0", Member{World: 1, State: MemberStaged, Graduate: true}, after)
	v := g.Call("a2", after, 1)
	if !v.Live || v.ID != 2 || !v.Member || v.Size != 3 {
		t.Fatalf("the next call: %+v", v)
	}
	if g.Call("a0", after, 1).Member {
		t.Fatal("a0 left the gate on call 1; it is not on call 2's roster")
	}
	var over string
	for _, e := range g.Events() {
		if strings.HasPrefix(e, "kq gate - call 1 over") {
			over = e
		}
	}
	if !strings.Contains(over, "roster 3, 2 left the gate, 1 stayed behind") || !strings.Contains(over, "after 46m") && !strings.Contains(over, "after 45m") {
		t.Fatalf("the closing line: %q", over)
	}
}

func TestStaleStagedMembersDoNotCount(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "s", 3, 1, gateT0)
	// 91 seconds on, none has reported again: the quorum is gone.
	if v := g.Call("s0", gateT0.Add(91*time.Second), 1); v.Live {
		t.Fatalf("stale staged reports made a call: %+v", v)
	}
	// 89 seconds is fine.
	if v := g.Call("s0", gateT0.Add(89*time.Second), 1); !v.Live {
		t.Fatalf("fresh reports did not: %+v", v)
	}
}

func TestStaleLimitsByState(t *testing.T) {
	g := newTestGate(t, 100, 3)
	g.Report("train", Member{World: 1, State: MemberTraining, Short: []Shortfall{{"defence", 40, 50}}}, gateT0)
	g.Report("gear", Member{World: 1, State: MemberGearing, Graduate: true}, gateT0)
	s := g.Snapshot(gateT0.Add(9 * time.Minute))
	if s.Training != 1 || s.Gearing != 1 {
		t.Fatalf("at 9m: %+v", s)
	}
	s = g.Snapshot(gateT0.Add(11 * time.Minute))
	if s.Training != 0 || s.Gearing != 1 { // training goes stale at 10m, gearing at 15m
		t.Fatalf("at 11m: %+v", s)
	}
	s = g.Snapshot(gateT0.Add(16 * time.Minute))
	if s.Gearing != 0 {
		t.Fatalf("at 16m: %+v", s)
	}
}

func TestCallLapsesAfterCallFor(t *testing.T) {
	g := newTestGate(t, 2, 0)
	stageN(g, "s", 2, 1, gateT0)
	v := g.Call("s0", gateT0, 1)
	if !v.Live || !v.Until.Equal(gateT0.Add(45*time.Minute)) {
		t.Fatalf("call: %+v", v)
	}
	g.Report("s0", Member{World: 1, State: MemberCalled, Graduate: true, Place: PlacePast}, gateT0.Add(44*time.Minute))
	if !g.Call("s0", gateT0.Add(44*time.Minute+59*time.Second), 1).Live {
		t.Fatal("the call lapsed early")
	}
	// At 45m the call is over; with nobody staged there is no new one.
	g.Report("s1", Member{World: 1, State: MemberGearing, Graduate: true}, gateT0.Add(45*time.Minute))
	if g.Call("s0", gateT0.Add(45*time.Minute), 1).Live {
		t.Fatal("the call outlived CallFor")
	}
}

func TestHomeWorldForOutlivesTheCallByHomeHold(t *testing.T) {
	g := newTestGate(t, 2, 0)
	stageN(g, "s", 2, 2, gateT0)
	g.Report("outsider", Member{World: 2, State: MemberStaged, Graduate: true}, gateT0.Add(-time.Hour)) // stale, not on the roster
	g.Call("s0", gateT0, 1)
	if got := g.HomeWorldFor("s0", gateT0.Add(time.Minute)); got != 1 {
		t.Fatalf("a called account's home is the call world: %d", got)
	}
	if got := g.HomeWorldFor("outsider", gateT0.Add(time.Minute)); got != 0 {
		t.Fatalf("an account off the roster keeps its own home: %d", got)
	}
	// 45m call + 120m hold.
	if got := g.HomeWorldFor("S0", gateT0.Add(45*time.Minute+119*time.Minute)); got != 1 {
		t.Fatalf("inside the hold: %d", got)
	}
	if got := g.HomeWorldFor("s0", gateT0.Add(45*time.Minute+121*time.Minute)); got != 0 {
		t.Fatalf("after the hold the account goes home: %d", got)
	}
}

func TestWorldIsForcedThenMuleThenMostStagedTiesLowest(t *testing.T) {
	mk := func(force int) *Gate {
		p := testGatePolicy(5, 0)
		p.World = force
		g, err := NewGate(p)
		if err != nil {
			t.Fatal(err)
		}
		stageN(g, "x", 3, 3, gateT0)
		stageN(g, "y", 2, 2, gateT0)
		return g
	}
	if v := mk(7).Call("x0", gateT0, 1); v.World != 7 {
		t.Fatalf("forced world: %+v", v)
	}
	if v := mk(0).Call("x0", gateT0, 1); v.World != 1 {
		t.Fatalf("the mule's world: %+v", v)
	}
	if v := mk(0).Call("x0", gateT0, 0); v.World != 3 {
		t.Fatalf("the world with the most staged: %+v", v)
	}
	// A tie goes to the lowest world.
	g := newTestGate(t, 4, 0)
	stageN(g, "p", 2, 3, gateT0)
	stageN(g, "q", 2, 2, gateT0)
	if v := g.Call("p0", gateT0, 0); v.World != 2 {
		t.Fatalf("a tie goes to the lowest world: %+v", v)
	}
}

func TestDecideGateIsPure(t *testing.T) {
	p := testGatePolicy(3, 2)
	in := GateInput{Policy: p, Staged: []string{"c", "a", "b"}, StagedWorlds: map[int]int{1: 3}}
	d := DecideGate(in)
	if !d.NewCall || d.World != 1 || strings.Join(d.Roster, "") != "abc" || d.GearOpen {
		t.Fatalf("decision: %+v", d)
	}
	if in.Staged[0] != "c" {
		t.Fatal("DecideGate sorted the caller's slice")
	}
	in.Live = true
	if DecideGate(in).NewCall {
		t.Fatal("a call while one is live")
	}
	in.Live, in.Staged = false, []string{"a", "b"}
	if DecideGate(in).NewCall {
		t.Fatal("a call under the quorum")
	}
	in.Graduates = 5
	if !DecideGate(in).GearOpen {
		t.Fatal("5 graduates release quorum 3 + ahead 2")
	}
}

func TestLogDueFiresOncePerInterval(t *testing.T) {
	g := newTestGate(t, 100, 10)
	if !g.LogDue(gateT0, 5*time.Minute) {
		t.Fatal("the first caller logs")
	}
	if g.LogDue(gateT0.Add(time.Minute), 5*time.Minute) || g.LogDue(gateT0.Add(5*time.Minute-time.Second), 5*time.Minute) {
		t.Fatal("a second caller inside the interval logs")
	}
	if !g.LogDue(gateT0.Add(5*time.Minute), 5*time.Minute) {
		t.Fatal("the next interval's first caller logs")
	}
}

func TestSnapshotLineNamesTheShortfalls(t *testing.T) {
	g := newTestGate(t, 100, 10)
	for i := 0; i < 9; i++ {
		g.Report(fmt.Sprintf("d47-%d", i), Member{World: 1, State: MemberTraining, Graduate: false, Short: []Shortfall{{"defence", 47, 50}}}, gateT0)
	}
	for i := 0; i < 7; i++ {
		g.Report(fmt.Sprintf("d46-%d", i), Member{World: 2, State: MemberTraining, Short: []Shortfall{{"defence", 46, 50}}}, gateT0)
	}
	for i := 0; i < 3; i++ {
		g.Report(fmt.Sprintf("m64-%d", i), Member{World: 2, State: MemberTraining, Short: []Shortfall{{"magic", 64, 66}}}, gateT0)
	}
	g.Report("both", Member{World: 3, State: MemberTraining, Short: []Shortfall{{"defence", 40, 50}, {"magic", 50, 66}}}, gateT0)
	s := g.Snapshot(gateT0)
	if s.Training != 20 || s.AlmostN != 19 || s.AlmostStat != "defence" {
		t.Fatalf("snapshot: %+v", s)
	}
	line := s.Line()
	// Closest first (smallest gap): magic 64/66 (gap 2), defence 47/50 (3), defence 46/50 (4).
	for _, want := range []string{"0 of 100 staged", "0 graduates of 110 needed", "19 almost (all but defence)",
		"closest shortfalls: magic 64/66 (x3), defence 47/50 (x9), defence 46/50 (x7)"} {
		if !strings.Contains(line, want) {
			t.Errorf("the census line lacks %q:\n%s", want, line)
		}
	}
}

func TestLeftTheGateIsCountedOnlyForTheRosterWhileLive(t *testing.T) {
	g := newTestGate(t, 2, 0)
	stageN(g, "s", 2, 1, gateT0)
	g.Call("s0", gateT0, 1)
	g.Report("s0", Member{World: 1, State: MemberCalled, Graduate: true, Place: PlacePast}, gateT0.Add(time.Minute))
	g.Report("stranger", Member{World: 1, State: MemberCalled, Graduate: true, Place: PlacePast}, gateT0.Add(time.Minute))
	g.Call("s1", gateT0.Add(46*time.Minute), 1)
	var over string
	for _, e := range g.Events() {
		if strings.Contains(e, "call 1 over") {
			over = e
		}
	}
	if !strings.Contains(over, "1 left the gate, 1 stayed behind") {
		t.Fatalf("closing line: %q", over)
	}
}

func TestEmptyNamesAndTheEventBound(t *testing.T) {
	g := newTestGate(t, 1, 0)
	g.Report("  ", Member{State: MemberStaged}, gateT0)
	if s := g.Snapshot(gateT0); s.Staged != 0 {
		t.Fatal("an empty name was recorded")
	}
	for i := 0; i < maxGateEvents*2; i++ {
		g.mu.Lock()
		g.event("e%d", i)
		g.mu.Unlock()
	}
	ev := g.Events()
	if len(ev) != maxGateEvents || ev[len(ev)-1] != fmt.Sprintf("e%d", maxGateEvents*2-1) {
		t.Fatalf("%d events kept, last %q", len(ev), ev[len(ev)-1])
	}
}

// The registry: the NEWEST policy wins (unlike Shared and SharedWaves), keeping the census.
func TestNewestPolicyReplacesAndKeepsCounters(t *testing.T) {
	const name = "gate-test-newest"
	ForgetGate(name)
	defer ForgetGate(name)
	if LookupGate(name) != nil {
		t.Fatal("a stale gate")
	}
	p := testGatePolicy(3, 0)
	g, st, err := SharedGate(name, p)
	if err != nil || st != GateCreated {
		t.Fatalf("first: %v %v", st, err)
	}
	stageN(g, "s", 3, 1, gateT0)
	if !g.Call("s0", gateT0, 1).Live {
		t.Fatal("no call under quorum 3")
	}
	g2, st, err := SharedGate(name, p)
	if err != nil || st != GateExisting || g2 != g {
		t.Fatalf("same policy: %v %v", st, err)
	}
	p.Quorum = 7
	g3, st, err := SharedGate(name, p)
	if err != nil || st != GatePolicyChanged || g3 != g || g.Policy().Quorum != 7 {
		t.Fatalf("a new policy replaces the rule: %v %v quorum %d", st, err, g.Policy().Quorum)
	}
	// The census and the call in flight survived.
	if v := g.Call("s1", gateT0, 1); !v.Live || !v.Member || v.ID != 1 {
		t.Fatalf("the call was lost on a policy change: %+v", v)
	}
	if _, _, err := SharedGate(name, GatePolicy{}); err == nil {
		t.Fatal("an invalid policy was accepted")
	}
	if g.Policy().Quorum != 7 {
		t.Fatal("an invalid policy changed the rule")
	}
	if _, _, err := SharedGate("", p); err == nil {
		t.Fatal("an unnamed gate was opened")
	}
	if LookupGate(name) != g {
		t.Fatal("Lookup")
	}
}

// Concurrent reporters and askers: the race detector's job; the assertion is that exactly one call
// was issued and every roster member is on it.
func TestConcurrentReportAndCall(t *testing.T) {
	g := newTestGate(t, 50, 0)
	var wg sync.WaitGroup
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("acc%d", i)
			for step := 0; step < 20; step++ {
				now := gateT0.Add(time.Duration(step) * time.Second)
				g.Report(name, Member{World: 1 + i%3, State: MemberStaged, Graduate: true}, now)
				g.Call(name, now, 1)
				g.HomeWorldFor(name, now)
				g.GearOpen(now)
				g.Snapshot(now)
				g.Events()
				g.LogDue(now, time.Second)
			}
		}(i)
	}
	wg.Wait()
	s := g.Snapshot(gateT0.Add(20 * time.Second))
	if s.Calls != 1 {
		t.Fatalf("%d calls issued, want exactly 1", s.Calls)
	}
}

// ---- a roster member that turns back is dropped while the call is still live ----

func calledMember(w int, p GatePlace) Member {
	return Member{World: w, State: MemberCalled, Graduate: true, Place: p}
}

func memberAt(w int, st GateMemberState, p GatePlace) Member {
	return Member{World: w, State: st, Graduate: true, Place: p}
}

// The reviewer's case: called, died in the lair at about minute 20, re-kitted and re-staged before minute 45.
// The call must stop naming it the moment it is seen back before the counter, or its plan keeps the door open
// and sends it to the lair alone.
func TestARosterMemberThatDiesInsideTheWindowIsNoLongerNamed(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "s", 3, 1, gateT0)
	if v := g.Call("s0", gateT0, 1); !v.Member || v.Size != 3 {
		t.Fatalf("call: %+v", v)
	}
	// s0 goes through the desert and into the burrow, then dies: the next pass starts on the respawn side.
	g.Report("s0", calledMember(1, PlacePast), gateT0.Add(5*time.Minute))
	if !g.Call("s0", gateT0.Add(5*time.Minute), 1).Member {
		t.Fatal("a member past the gate is still named")
	}
	if dropped := g.Report("s0", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(20*time.Minute)); !dropped {
		t.Fatal("Report did not say it dropped a member found back before the counter")
	}
	v := g.Call("s0", gateT0.Add(20*time.Minute), 1)
	if !v.Live || v.Member || v.Size != 3 || v.Back != 1 || v.ID != 1 {
		t.Fatalf("after its death the call is live, the size as issued, and no longer names s0: %+v", v)
	}
	// Re-kitted and staged again, still inside the window: the call does not name it back.
	g.Report("s0", memberAt(1, MemberStaged, PlaceCounter), gateT0.Add(35*time.Minute))
	if g.Call("s0", gateT0.Add(35*time.Minute), 1).Member {
		t.Fatal("a re-staged account walked back onto the call it died on")
	}
	// Its two companions are untouched.
	for _, n := range []string{"s1", "s2"} {
		if !g.Call(n, gateT0.Add(35*time.Minute), 1).Member {
			t.Errorf("%s was dropped with s0", n)
		}
	}
	// The home world is still held, so it is not hopped home and back inside the window.
	if got := g.HomeWorldFor("s0", gateT0.Add(35*time.Minute)); got != 1 {
		t.Fatalf("a dropped member keeps the call world as its home: %d", got)
	}
	if s := g.Snapshot(gateT0.Add(35 * time.Minute)); !strings.Contains(s.Line(), "call 1 live to world 1 (1 of 3 turned back)") {
		t.Fatalf("the census line does not say who turned back:\n%s", s.Line())
	}
}

// Died on the way, before it was ever seen past the pass: still dropped, by where it stands.
func TestAMemberKilledOnTheWalkOutIsDroppedWithoutEverHavingLeft(t *testing.T) {
	g := newTestGate(t, 2, 0)
	stageN(g, "s", 2, 1, gateT0)
	g.Call("s0", gateT0, 1)
	g.Report("s0", calledMember(1, PlaceCounter), gateT0.Add(time.Minute)) // walking out of Al Kharid
	if !g.Call("s0", gateT0.Add(time.Minute), 1).Member {
		t.Fatal("a called account walking out of Al Kharid was dropped")
	}
	if dropped := g.Report("s0", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(2*time.Minute)); !dropped {
		t.Fatal("a member killed on the walk out was not dropped")
	}
	if g.Call("s0", gateT0.Add(2*time.Minute), 1).Member {
		t.Fatal("still named")
	}
}

// What must NOT drop: walking out of Al Kharid, standing at the counter, or held there on another world.
func TestCalledAccountsThatHaveNotTurnedBackAreNeverDropped(t *testing.T) {
	g := newTestGate(t, 4, 0)
	stageN(g, "s", 4, 1, gateT0)
	g.Call("s0", gateT0, 1)
	cases := []struct {
		name string
		m    Member
	}{
		{"s0", calledMember(1, PlaceCounter)},            // walking out of Al Kharid
		{"s1", memberAt(2, MemberStaged, PlaceCounter)},  // held at the counter, hopping to the call world
		{"s2", memberAt(2, MemberGearing, PlaceCounter)}, // at the counter, gate shut by the wrong world
		{"s3", calledMember(1, PlaceUnknown)},            // no tile yet
	}
	for _, c := range cases {
		if dropped := g.Report(c.name, c.m, gateT0.Add(time.Minute)); dropped {
			t.Errorf("%s was dropped on %+v", c.name, c.m)
		}
		if !g.Call(c.name, gateT0.Add(time.Minute), 1).Member {
			t.Errorf("%s is no longer named after %+v", c.name, c.m)
		}
	}
	// Past the gate, a member only ever reports Past: more of the same never drops it.
	for i := 0; i < 5; i++ {
		if g.Report("s0", calledMember(1, PlacePast), gateT0.Add(time.Duration(2+i)*time.Minute)) {
			t.Fatal("a member crossing was dropped")
		}
	}
}

// The reviewer's own rule, whatever the glue says about the place: seen past the gate and then Staged or
// Gearing anywhere but past it, the member has come back.
func TestAMemberThatLeftAndIsThenStagedOrGearingIsDropped(t *testing.T) {
	for _, st := range []GateMemberState{MemberStaged, MemberGearing} {
		for _, p := range []GatePlace{PlaceCounter, PlaceUnknown} {
			g := newTestGate(t, 2, 0)
			stageN(g, "s", 2, 1, gateT0)
			g.Call("s0", gateT0, 1)
			g.Report("s0", calledMember(1, PlacePast), gateT0.Add(time.Minute))
			if !g.Report("s0", memberAt(1, st, p), gateT0.Add(2*time.Minute)) {
				t.Errorf("left then %s at %q was not dropped", st, p)
			}
			if g.Call("s0", gateT0.Add(2*time.Minute), 1).Member {
				t.Errorf("left then %s at %q is still named", st, p)
			}
		}
	}
}

// A report that is not about a live call's roster drops nothing, and Report says so.
func TestReportDropsOnlyALiveCallsRoster(t *testing.T) {
	g := newTestGate(t, 2, 0)
	// No call yet: nothing to drop from.
	if g.Report("early", memberAt(1, MemberGearing, PlaceBefore), gateT0) {
		t.Fatal("dropped from a call that does not exist")
	}
	stageN(g, "s", 2, 1, gateT0)
	g.Call("s0", gateT0, 1)
	if g.Report("stranger", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(time.Minute)) {
		t.Fatal("dropped an account the call never named")
	}
	// Once dropped, a repeat report is not a second drop, and being seen past again does not resurrect it.
	if !g.Report("s0", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(time.Minute)) {
		t.Fatal("first drop")
	}
	if g.Report("s0", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(2*time.Minute)) {
		t.Fatal("a second drop of the same member")
	}
	g.Report("s0", calledMember(1, PlacePast), gateT0.Add(3*time.Minute))
	if g.Call("s0", gateT0.Add(3*time.Minute), 1).Member {
		t.Fatal("a dropped member came back onto the roster")
	}
	// After the call lapses nothing is dropped any more.
	g.Call("s1", gateT0.Add(46*time.Minute), 1)
	if g.Report("s1", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(47*time.Minute)) {
		t.Fatal("dropped from a call that is over")
	}
}

// A member dropped inside the window stages again and is named by the NEXT call, with the closing line saying so.
func TestADroppedMemberJoinsTheNextCallAndTheClosingLineCountsIt(t *testing.T) {
	g := newTestGate(t, 3, 0)
	stageN(g, "s", 3, 1, gateT0)
	g.Call("s0", gateT0, 1)
	g.Report("s0", calledMember(1, PlacePast), gateT0.Add(5*time.Minute))
	g.Report("s1", calledMember(1, PlacePast), gateT0.Add(5*time.Minute))
	g.Report("s0", memberAt(1, MemberGearing, PlaceBefore), gateT0.Add(15*time.Minute)) // died
	// s0 re-stages; s1 is still in the lair; s2 never left the counter; two newcomers stage.
	after := gateT0.Add(46 * time.Minute)
	g.Report("s0", memberAt(1, MemberStaged, PlaceCounter), after)
	g.Report("s2", memberAt(1, MemberStaged, PlaceCounter), after)
	g.Report("n0", memberAt(1, MemberStaged, PlaceCounter), after)
	v := g.Call("s0", after, 1)
	if !v.Live || v.ID != 2 || !v.Member || v.Size != 3 {
		t.Fatalf("the next call must name the re-staged account: %+v", v)
	}
	var over string
	for _, e := range g.Events() {
		if strings.HasPrefix(e, "kq gate - call 1 over") {
			over = e
		}
	}
	// 3 named: s0 left and turned back, s1 left, s2 stayed behind.
	if !strings.Contains(over, "roster 3, 2 left the gate, 1 turned back (died or teleported out) and re-stage for the next 3, 1 stayed behind") {
		t.Fatalf("the closing line: %q", over)
	}
}
