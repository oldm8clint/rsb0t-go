package swarmrally

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// clock is an injectable clock, so a twenty-minute deadline is tested in microseconds.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testPolicy() Policy {
	return Policy{Quorum: 100, MinEngage: 20, MaxWait: 20 * time.Minute, Stale: 90 * time.Second}
}

func TestDecideTable(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		name          string
		ready, presnt int
		waited        time.Duration
		want          Verdict
	}{
		{"nobody yet", 0, 0, 0, Gather},
		{"below quorum, inside the window", 99, 150, 5 * time.Minute, Gather},
		{"exactly the quorum", 100, 150, 5 * time.Minute, EngageQuorum},
		{"over the quorum", 180, 200, time.Minute, EngageQuorum},
		{"quorum reached AT the deadline still counts as a quorum", 100, 100, 20 * time.Minute, EngageQuorum},
		{"deadline with the floor met", 20, 40, 20 * time.Minute, EngageShort},
		{"deadline past, comfortably above the floor", 75, 90, 25 * time.Minute, EngageShort},
		{"deadline with one below the floor", 19, 40, 20 * time.Minute, Abort},
		{"deadline with nobody", 0, 0, 30 * time.Minute, Abort},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(p, c.ready, c.presnt, c.waited)
			if got.Verdict != c.want {
				t.Fatalf("Decide(ready=%d, waited=%s) = %v (%s), want %v",
					c.ready, c.waited, got.Verdict, got.Reason, c.want)
			}
			if got.Reason == "" {
				t.Error("every decision must carry a reason; a bare verdict is what a report cannot caveat")
			}
		})
	}
}

// The owner's rule as a whole: gather, wait, engage at 100. The point of the test is that the
// 99th account does NOT send the swarm in and the 100th does.
func TestQuorumIsTheTrigger(t *testing.T) {
	c := newClock()
	r, err := New(testPolicy(), c.now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	if d := r.Decide(); d.Verdict != Gather {
		t.Fatalf("99 ready = %v (%s), want gather", d.Verdict, d.Reason)
	}
	if r.Engaged() {
		t.Fatal("engaged at 99")
	}
	r.Report("kq099", true)
	d := r.Decide()
	if d.Verdict != EngageQuorum {
		t.Fatalf("100 ready = %v (%s), want engage-quorum", d.Verdict, d.Reason)
	}
	if d.Ready != 100 {
		t.Fatalf("Ready = %d, want 100", d.Ready)
	}
	if !r.Engaged() {
		t.Fatal("not engaged at 100")
	}
}

// Accounts that are present but NOT ready must not count toward the quorum — otherwise the
// gather is just a head count and the rule means nothing.
func TestPresentButNotReadyDoesNotCount(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 60; i++ {
		r.Report(fmt.Sprintf("ready%03d", i), true)
	}
	for i := 0; i < 90; i++ {
		r.Report(fmt.Sprintf("busy%03d", i), false)
	}
	d := r.Decide()
	if d.Verdict != Gather {
		t.Fatalf("verdict = %v, want gather (150 present, only 60 ready)", d.Verdict)
	}
	if d.Ready != 60 || d.Present != 150 {
		t.Fatalf("ready/present = %d/%d, want 60/150", d.Ready, d.Present)
	}
}

// An account that stopped heartbeating stops propping up the quorum. This is the difference
// between "100 are standing by" and "100 once said so".
func TestStaleMembersLeaveTheQuorum(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 99; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	// Two minutes on, every one of those is stale (Stale is 90s) and nothing is ready.
	c.advance(2 * time.Minute)
	d := r.Decide()
	if d.Ready != 0 || d.Present != 0 {
		t.Fatalf("after %s of silence ready/present = %d/%d, want 0/0", 2*time.Minute, d.Ready, d.Present)
	}
	// One account comes back; it is the only live member, and 1 is not a quorum.
	r.Report("kq000", true)
	if d := r.Decide(); d.Verdict != Gather || d.Ready != 1 {
		t.Fatalf("verdict/ready = %v/%d, want gather/1", d.Verdict, d.Ready)
	}
}

// The latch. Accounts start dying the instant they engage, so the ready count falls; the rally
// must not flip back to gather and turn the swarm around in her chamber.
func TestVerdictLatches(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 100; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	if !r.Engaged() {
		t.Fatal("not engaged at 100")
	}
	// Ninety of them die.
	for i := 0; i < 90; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), false)
	}
	d := r.Decide()
	if d.Verdict != EngageQuorum {
		t.Fatalf("after 90 deaths verdict = %v, want the latched engage-quorum", d.Verdict)
	}
	if !r.Engaged() {
		t.Fatal("un-engaged after deaths")
	}
	// And time passing past MaxWait must not turn a latched engage into an abort.
	c.advance(time.Hour)
	if d := r.Decide(); d.Verdict != EngageQuorum {
		t.Fatalf("an hour after engaging verdict = %v, want engage-quorum", d.Verdict)
	}
}

// The deadlock the owner's rule would otherwise have: 92 accounts and a quorum of 100.
func TestShortGatherEngagesRatherThanHanging(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 92; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	if d := r.Decide(); d.Verdict != Gather {
		t.Fatalf("92 ready inside the window = %v, want gather", d.Verdict)
	}
	// Heartbeats keep coming at a cadence well inside Policy.Stale, which is what the harness
	// guarantees with a heartbeat goroutine per member. That matters: the deadline verdict is
	// taken on the LIVE count, so a cohort that went quiet for longer than Stale and then woke
	// up past the deadline would correctly — and unhelpfully — abort on a count of one. The
	// cadence, not the rally, is what prevents that.
	for waited := time.Duration(0); waited <= 21*time.Minute; waited += 30 * time.Second {
		for i := 0; i < 92; i++ {
			r.Report(fmt.Sprintf("kq%03d", i), true)
		}
		if r.Decide().Verdict.Terminal() {
			break
		}
		c.advance(30 * time.Second)
	}
	d := r.Decide()
	if d.Verdict != EngageShort {
		t.Fatalf("verdict = %v (%s), want engage-short", d.Verdict, d.Reason)
	}
	if !r.Engaged() {
		t.Fatal("a short engage must still send the swarm in")
	}
}

func TestExpiredAndUnderTheFloorAborts(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	// Same realistic cadence as the short-gather test, so the abort below is decided on a live
	// count of 19 rather than on a cohort that had simply gone stale.
	for waited := time.Duration(0); waited <= 21*time.Minute; waited += 30 * time.Second {
		for i := 0; i < 19; i++ {
			r.Report(fmt.Sprintf("kq%03d", i), true)
		}
		if r.Decide().Verdict.Terminal() {
			break
		}
		c.advance(30 * time.Second)
	}
	d := r.Decide()
	if d.Ready != 19 {
		t.Fatalf("Ready = %d, want 19 — the abort must be decided on a live count", d.Ready)
	}
	if d.Verdict != Abort {
		t.Fatalf("19 ready at the deadline = %v (%s), want abort", d.Verdict, d.Reason)
	}
	if r.Engaged() {
		t.Fatal("aborted rally reports engaged")
	}
}

// Wait must return on the deadline without anybody calling Decide, and must never hang.
func TestWaitTerminatesOnTheDeadline(t *testing.T) {
	c := newClock()
	r, _ := New(Policy{Quorum: 100, MinEngage: 20, MaxWait: 20 * time.Minute, Stale: time.Hour}, c.now)
	for i := 0; i < 30; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.advance(21 * time.Minute)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := r.Wait(ctx, 5*time.Millisecond)
	if d.Verdict != EngageShort {
		t.Fatalf("Wait returned %v (%s), want engage-short once the deadline passed", d.Verdict, d.Reason)
	}
	if ctx.Err() != nil {
		t.Fatal("Wait was rescued by the context rather than by the deadline")
	}
}

// A caller shutting down is never held, and can TELL it was not a real verdict.
func TestWaitHonoursContext(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	r.Report("kq000", true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	d := r.Wait(ctx, 5*time.Millisecond)
	if time.Since(start) > 2*time.Second {
		t.Fatal("Wait did not honour the context")
	}
	if d.Verdict.Terminal() {
		t.Fatalf("verdict = %v; a context-cancelled wait must return a NON-terminal decision so the caller knows nothing was decided", d.Verdict)
	}
}

// Wait returns immediately when the rally has already latched.
func TestWaitReturnsImmediatelyWhenLatched(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 100; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	start := time.Now()
	d := r.Wait(context.Background(), time.Minute)
	if time.Since(start) > time.Second {
		t.Fatal("Wait blocked on an already-latched rally")
	}
	if d.Verdict != EngageQuorum {
		t.Fatalf("verdict = %v, want engage-quorum", d.Verdict)
	}
}

func TestLeaveDropsAMember(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	for i := 0; i < 99; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	r.Leave("kq000")
	r.Report("kq099", true)
	if d := r.Decide(); d.Verdict != Gather || d.Ready != 99 {
		t.Fatalf("verdict/ready = %v/%d, want gather/99 — a member that left must not still count", d.Verdict, d.Ready)
	}
}

func TestSnapshotReportsTheCostOfWaiting(t *testing.T) {
	c := newClock()
	r, _ := New(testPolicy(), c.now)
	r.Report("early", true)
	c.advance(6 * time.Minute)
	// early must keep heartbeating or it goes stale; its firstReady is preserved.
	r.Report("early", true)
	r.Report("late", true)
	s := r.Snapshot()
	if s.LongestWait < 6*time.Minute {
		t.Fatalf("LongestWait = %s, want at least 6m — the first arrival's standing-around is the price of the rule", s.LongestWait)
	}
	if s.PeakReady != 2 {
		t.Fatalf("PeakReady = %d, want 2", s.PeakReady)
	}
	if len(s.Members) != 2 || s.Members[0].Name != "early" {
		t.Fatalf("members = %+v, want early and late sorted", s.Members)
	}
	if s.Members[0].Stale {
		t.Error("early is stale despite a fresh heartbeat")
	}
}

// PeakReady must survive the fall that follows engaging, because it is the number a report
// should quote for how big the crowd actually was.
func TestPeakReadySurvivesTheFall(t *testing.T) {
	c := newClock()
	r, _ := New(Policy{Quorum: 100, MinEngage: 20, MaxWait: time.Hour, Stale: time.Hour}, c.now)
	for i := 0; i < 40; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), true)
	}
	for i := 0; i < 35; i++ {
		r.Report(fmt.Sprintf("kq%03d", i), false)
	}
	if s := r.Snapshot(); s.PeakReady != 40 {
		t.Fatalf("PeakReady = %d, want 40", s.PeakReady)
	}
}

func TestInvalidPoliciesAreRefused(t *testing.T) {
	bad := []Policy{
		{Quorum: 0, MinEngage: 1, MaxWait: time.Minute, Stale: time.Minute},
		{Quorum: 10, MinEngage: 0, MaxWait: time.Minute, Stale: time.Minute},
		{Quorum: 10, MinEngage: 11, MaxWait: time.Minute, Stale: time.Minute},
		{Quorum: 10, MinEngage: 5, MaxWait: 0, Stale: time.Minute},
		{Quorum: 10, MinEngage: 5, MaxWait: time.Minute, Stale: 0},
	}
	for i, p := range bad {
		if _, err := New(p, nil); err == nil {
			t.Errorf("case %d: %+v was accepted; an unterminating rally is a hang waiting to happen", i, p)
		}
	}
	if _, err := New(DefaultPolicy(), nil); err != nil {
		t.Fatalf("DefaultPolicy rejected: %v", err)
	}
}

// 200 accounts heartbeating at once is the real shape of this. Run with -race.
func TestConcurrentReportsAreSafe(t *testing.T) {
	r, _ := New(Policy{Quorum: 100, MinEngage: 20, MaxWait: time.Hour, Stale: time.Hour}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		name := fmt.Sprintf("kq%03d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				r.Report(name, j%2 == 0)
				r.Decide()
				r.Snapshot()
			}
		}()
	}
	wg.Wait()
	if s := r.Snapshot(); len(s.Members) != 200 {
		t.Fatalf("members = %d, want 200", len(s.Members))
	}
}
