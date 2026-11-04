package actions

import "time"

// Waiter is what an action that confirms its own result waits with. *bot.Context satisfies
// it, so a running bot's confirmations stop with the bot; a nil Waiter falls back to plain
// sleeps.
type Waiter interface {
	Delay(d time.Duration) bool
	DelayUntil(cond func() bool, timeout time.Duration) bool
	DelayTicks(n int) bool
}

// sleepTick is a server tick at normal engine speed, for the sleep fallback only.
const sleepTick = 600 * time.Millisecond

type sleepWaiter struct{}

func (sleepWaiter) Delay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

func (sleepWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (sleepWaiter) DelayTicks(n int) bool {
	time.Sleep(time.Duration(n) * sleepTick)
	return true
}

func waiterOr(w Waiter) Waiter {
	if w == nil {
		return sleepWaiter{}
	}
	return w
}

// delayWaiter is a Waiter over a runner's bare Delay (sleep for d; false means stop).
type delayWaiter struct{ delay func(time.Duration) bool }

func (w delayWaiter) Delay(d time.Duration) bool { return w.delay(d) }

func (w delayWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.delay(50 * time.Millisecond) {
			return false
		}
	}
}

func (w delayWaiter) DelayTicks(n int) bool { return w.delay(time.Duration(n) * sleepTick) }

// DelayWaiter adapts a runner's Deps.Delay (sleep for d, false when the caller should stop) to a Waiter, for the
// runner packages that have no Waiter adapter of their own. A nil delay is plain sleeps.
func DelayWaiter(delay func(time.Duration) bool) Waiter {
	if delay == nil {
		return sleepWaiter{}
	}
	return delayWaiter{delay}
}
