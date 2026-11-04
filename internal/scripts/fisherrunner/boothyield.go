package fisherrunner

import (
	"sort"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
)

// THE BANK BOOTH CLICK YIELDS TO THE GUARDIAN, AND REMEMBERS WHICH BOOTHS DO NOT OPEN (plantfisher-1018, 18 Oct 2026).
//
// openNearestBankBooth walked to a booth, clicked it and waited up to bankBoothOpenTimeout (30 s) for the bank, then tried the
// next booth, without once asking whether the Guardian had something to do. Its comment said "BankFish already owns Guardian's
// action lock here", which is true of BankFish and false of OpenCatherbyBank, the path every bank trip of the fisher, the
// bass-oyster fletcher and the mule takes. At the Fishing Guild bank on 18 Oct 20:01-20:03 a Strange plant stood on the booth's
// stand tile; the Guardian picked at it and fled it while this code walked to the next booth, clicked the booth again, and
// finally, with the plant angry, clicked it a third time two seconds before the evade began.
//
// So the wait for the bank ends as soon as the Guardian has an event (Busy), the click is repeated only after the Guardian has
// finished (WaitForGuardian), and the account's input is held while the Guardian works (actions.HoldInput), whichever script is
// running. A caller that holds the action lock itself (BankFish) is not asked to yield: it cannot be waiting on a Guardian
// that cannot run (randomevents.BusyForCaller).
//
// And the booths are not equal. At the Fishing Guild bank 2585,3418's first click fails to open the bank 55 of 107 times (51%),
// against 2 of 40 (5%) for 2585,3419, always from the right stand tile; nearest-first put the flaky booth first on every visit
// and spent 30 s on it half the time, which is the exposure the plant spawned in. boothHistory orders the candidates nearest-first
// except that a booth that has failed a third of its recent clicks goes after the others.

// boothYieldMax is how many times one booth open yields to the Guardian before it gives up: a Guardian that is Busy for ever
// cannot hold a bank trip for ever.
const boothYieldMax = 6

// busyWaiter is an actions.Waiter that stops, and says so, as soon as the Guardian is busy.
type busyWaiter struct {
	actions.Waiter
	d       *Deps
	yielded *bool
}

func (w busyWaiter) yield() bool {
	if randomevents.BusyForCaller(w.d.Session) {
		*w.yielded = true
		return true
	}
	return false
}

func (w busyWaiter) Delay(d time.Duration) bool {
	if w.yield() {
		return false
	}
	return w.Waiter.Delay(d)
}

func (w busyWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	if w.yield() {
		return false
	}
	return w.Waiter.DelayUntil(cond, timeout)
}

func (w busyWaiter) DelayTicks(n int) bool {
	if w.yield() {
		return false
	}
	return w.Waiter.DelayTicks(n)
}

// boothKey identifies a booth by its tile.
type boothKey struct{ x, z, level int }

type boothStat struct{ tries, fails int }

// boothHistory is the fleet's memory of how often each bank booth opened on a click. One process runs the fleet, and the booth is
// the same booth for every account.
var boothHistory = struct {
	mu sync.Mutex
	m  map[boothKey]*boothStat
}{m: map[boothKey]*boothStat{}}

const (
	// boothMinTries is how many clicks a booth needs before its record counts.
	boothMinTries = 8
	// boothFlakyPercent is the failure share from which a booth goes after the others.
	boothFlakyPercent = 30
	// boothWindow is how many clicks a booth's record covers; at twice this it is halved, so it follows the booth, not its history.
	boothWindow = 40
)

// noteBoothOutcome records one click on a booth: whether the bank opened.
func noteBoothOutcome(b locindex.Match, opened bool) {
	boothHistory.mu.Lock()
	defer boothHistory.mu.Unlock()
	k := boothKey{b.X, b.Z, b.Level}
	st := boothHistory.m[k]
	if st == nil {
		st = &boothStat{}
		boothHistory.m[k] = st
	}
	st.tries++
	if !opened {
		st.fails++
	}
	if st.tries >= 2*boothWindow {
		st.tries, st.fails = st.tries/2, st.fails/2
	}
}

// boothFlaky reports whether a booth has failed a third of its recent clicks.
func boothFlaky(k boothKey) bool {
	boothHistory.mu.Lock()
	defer boothHistory.mu.Unlock()
	st := boothHistory.m[k]
	return st != nil && st.tries >= boothMinTries && st.fails*100 >= st.tries*boothFlakyPercent
}

// orderBooths keeps the candidates nearest-first and moves the flaky booths after the rest, otherwise in the same order.
func orderBooths(cands []locindex.Match) []locindex.Match {
	out := append([]locindex.Match(nil), cands...)
	flaky := make(map[boothKey]bool, len(out))
	for _, c := range out {
		flaky[boothKey{c.X, c.Z, c.Level}] = boothFlaky(boothKey{c.X, c.Z, c.Level})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return !flaky[boothKey{out[i].X, out[i].Z, out[i].Level}] && flaky[boothKey{out[j].X, out[j].Z, out[j].Level}]
	})
	return out
}

// resetBoothHistory empties the record, for tests.
func resetBoothHistory() {
	boothHistory.mu.Lock()
	boothHistory.m = map[boothKey]*boothStat{}
	boothHistory.mu.Unlock()
}
