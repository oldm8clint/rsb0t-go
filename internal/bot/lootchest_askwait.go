package bot

import "time"

// A LootChestFisher account standing at the mule with nothing to trade, whose next key order is
// not due yet, used to SPIN: PhaseHandoff's empty-pack branch called askForKeys, which returns at
// once while nextAsk is in the future, and Loop returned nil with no delay -- and the Runner calls
// Loop again straight away. Live on c0da98ee spare104 did exactly that for the whole 30-minute
// askRetryAfterDecline twice in one morning (04:21:25-04:51:25 and 06:30:37-07:00:37 Perth, after
// "declined the key order ... asking again in 30m0s"): not one line, not one tile, one goroutine
// re-running the whole Loop head in a tight loop on the machine the other 298 accounts share. It
// was found by stallwatch's silent-gap scan of the live log, not by any report.
//
// The wait itself is unchanged -- the account still stands at the mule until the order is due;
// whether it should fish meanwhile is the script owner's call -- but each pass now pauses, and the
// wait says what it is waiting for every askWaitLogEvery, so it is visible in the panel and the
// fleet stall watchdog (internal/fleet/watchdog.go) reads it as an account that is waiting and
// saying so, which it never acts on.

// askWait is the memory of that wait; zero while not waiting.
type askWait struct {
	since   time.Time
	lastLog time.Time
}

const (
	// askWaitPass is the longest one waiting pass sleeps, so a stop, an event or the order
	// coming due is seen promptly.
	askWaitPass = 5 * time.Second
	// askWaitLogEvery is how often the wait says so -- under the watchdog's ten-minute silence.
	askWaitLogEvery = 5 * time.Minute
)

// askWaitStep is what one pass of the wait should do. Pure, for the test.
type askWaitStep struct {
	Wait  bool          // the order is not due: pause instead of asking
	Log   bool          // say so this pass
	Pause time.Duration // how long to pause
	Left  time.Duration // until the order is due
	For   time.Duration // how long the account has waited so far
}

// plan advances the wait for one pass. nextAsk zero or past means the order is due.
func (w *askWait) plan(now, nextAsk time.Time) askWaitStep {
	left := nextAsk.Sub(now)
	if nextAsk.IsZero() || left <= 0 {
		*w = askWait{}
		return askWaitStep{}
	}
	if w.since.IsZero() {
		w.since = now
	}
	st := askWaitStep{Wait: true, Left: left, For: now.Sub(w.since), Pause: askWaitPass}
	if left < st.Pause {
		st.Pause = left
	}
	if w.lastLog.IsZero() || now.Sub(w.lastLog) >= askWaitLogEvery {
		w.lastLog = now
		st.Log = true
	}
	return st
}

// holdForKeyOrder is PhaseHandoff's empty-pack branch: true (after pausing) while the next key
// order is not due, false when askForKeys should run.
func (b *lootChestBot) holdForKeyOrder(ctx *Context) bool {
	if ctx.Bus == nil {
		return false // askForKeys says why it cannot ask
	}
	st := b.askWait.plan(time.Now(), b.nextAsk)
	if !st.Wait {
		return false
	}
	if st.Log {
		ctx.logf("lootchest: at the mule with nothing to trade — the next key order to %q is due in %s; waiting here (%s so far)",
			b.mule, st.Left.Round(time.Second), st.For.Round(time.Second))
	}
	ctx.Delay(st.Pause)
	return true
}
