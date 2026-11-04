package client

import (
	"testing"
	"time"
)

// The keepalive goroutine wakes just after the idle gate can pass, and retries at the old poll
// interval when it had passed but nothing could be written.
func TestKeepaliveWait(t *testing.T) {
	for _, c := range []struct {
		idle, want time.Duration
	}{
		{0, noTimeoutAfter + keepaliveSlack},
		{400 * time.Millisecond, 600*time.Millisecond + keepaliveSlack},
		{noTimeoutAfter, keepaliveSlack},
		{noTimeoutAfter + time.Millisecond, keepaliveRetry},
		{5 * time.Second, keepaliveRetry},
	} {
		if got := keepaliveWait(c.idle); got != c.want {
			t.Errorf("idle %v: wait %v, want %v", c.idle, got, c.want)
		}
		// Whatever the idle time, waking after the wait finds the gate passed or retries.
		if c.idle <= noTimeoutAfter && c.idle+keepaliveWait(c.idle) <= noTimeoutAfter {
			t.Errorf("idle %v: wakes before the gate can pass", c.idle)
		}
	}
}
