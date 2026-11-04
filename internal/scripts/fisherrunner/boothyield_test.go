package fisherrunner

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
)

func booth(x, z int) locindex.Match { return locindex.Match{Loc: locindex.Loc{X: x, Z: z}} }

func noteMany(b locindex.Match, tries, fails int) {
	for i := 0; i < tries; i++ {
		noteBoothOutcome(b, i >= fails)
	}
}

// The Fishing Guild bank's two booths on 18 Oct 2026: 2585,3418's first click failed 55 of 107 times, 2585,3419's 2 of 40. Nearest-first
// tried the flaky one first on every visit and spent up to 30 s on it; the order now follows what the booths have done.
func TestBoothsThatRarelyOpenAreTriedLast(t *testing.T) {
	resetBoothHistory()
	t.Cleanup(resetBoothHistory)
	flaky, good, unknown := booth(2585, 3418), booth(2585, 3419), booth(2585, 3421)
	in := []locindex.Match{flaky, good, unknown}

	if got := orderBooths(in); got[0] != flaky {
		t.Fatalf("with no record the order is the caller's (nearest first): %v", got)
	}
	noteMany(flaky, boothMinTries-1, boothMinTries-1)
	if got := orderBooths(in); got[0] != flaky {
		t.Fatalf("a booth with %d clicks on record is judged: %v", boothMinTries-1, got)
	}
	noteMany(flaky, 107-(boothMinTries-1), 55-(boothMinTries-1))
	noteMany(good, 40, 2)
	got := orderBooths(in)
	if got[0] != good || got[1] != unknown || got[2] != flaky {
		t.Fatalf("order = %v, want the good booth, the unknown one (still nearest-first), then the flaky one last", got)
	}
	if in[0] != flaky {
		t.Fatal("orderBooths reordered its input")
	}
	// A booth that has recovered is let back: the record follows the booth, not its history.
	noteMany(flaky, 3*boothWindow, 0)
	if boothFlaky(boothKey{2585, 3418, 0}) {
		t.Fatal("a booth that has opened every time for 120 clicks is still flaky")
	}
}

type countingWaiter struct{ delays int }

func (c *countingWaiter) Delay(time.Duration) bool                   { c.delays++; return true }
func (c *countingWaiter) DelayUntil(func() bool, time.Duration) bool { c.delays++; return true }
func (c *countingWaiter) DelayTicks(int) bool                        { c.delays++; return true }

// The wait for the bank ends as soon as the Guardian has an event, and says so; with nothing going on it is the plain wait.
func TestBusyWaiterStopsTheWaitForTheBankWhenTheGuardianIsBusy(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	g := randomevents.NewGuardian(sim, nil)
	t.Cleanup(g.Stop)
	d := &Deps{Session: sim}
	inner := &countingWaiter{}
	yielded := false
	w := busyWaiter{Waiter: inner, d: d, yielded: &yielded}

	if !w.Delay(time.Millisecond) || !w.DelayUntil(func() bool { return true }, time.Millisecond) || !w.DelayTicks(1) || yielded || inner.delays != 3 {
		t.Fatalf("an idle Guardian stopped the wait (yielded %v, %d waits)", yielded, inner.delays)
	}

	locked, free, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the Guardian (or anyone) holding the account's action lock: Busy for everyone else
		defer wg.Done()
		unlock := g.Lock()
		close(locked)
		<-free
		unlock()
		close(done)
	}()
	<-locked
	if w.Delay(time.Millisecond) || !yielded || inner.delays != 3 {
		t.Fatalf("a busy Guardian did not stop the wait (yielded %v, %d waits)", yielded, inner.delays)
	}
	close(free)
	<-done
	wg.Wait()
}

// A caller that holds the action lock itself (BankFish across its booth open) is not told to yield to a Guardian that cannot run.
func TestBusyWaiterDoesNotYieldInsideTheCallersOwnLock(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	g := randomevents.NewGuardian(sim, nil)
	t.Cleanup(g.Stop)
	d := &Deps{Session: sim}
	yielded := false
	w := busyWaiter{Waiter: &countingWaiter{}, d: d, yielded: &yielded}
	unlock := g.Lock()
	defer unlock()
	if !w.Delay(time.Millisecond) || yielded {
		t.Fatal("the wait stopped for the caller's own lock")
	}
}

// Source pins: the booth open yields to the Guardian and keeps the booth record, and the bank-open path every trip takes is the one.
func TestOpenNearestBankBoothYieldsAndRemembers(t *testing.T) {
	b, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(string(b), "\r\n", "\n")
	start := strings.Index(src, "func openNearestBankBooth(")
	end := strings.Index(src[start:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("openNearestBankBooth not found")
	}
	body := src[start : start+end]
	for _, want := range []string{"orderBooths(candidates)", "randomevents.BusyForCaller(d.Session)", "busyWaiter{Waiter: actions.DelayWaiter(d.Delay)", "noteBoothOutcome(booth, true)", "noteBoothOutcome(booth, false)", "WaitForGuardian(d)"} {
		if !strings.Contains(body, want) {
			t.Errorf("openNearestBankBooth lost %q", want)
		}
	}
}
