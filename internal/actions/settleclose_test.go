package actions

import (
	"testing"
	"time"
)

// recordingWaiter notes whether a wait was asked for, and what its condition said at that moment.
type recordingWaiter struct {
	waited   bool
	condTrue bool
}

func (r *recordingWaiter) Delay(time.Duration) bool { return true }
func (r *recordingWaiter) DelayUntil(cond func() bool, _ time.Duration) bool {
	r.waited, r.condTrue = true, cond()
	return r.condTrue
}
func (r *recordingWaiter) DelayTicks(int) bool { return true }

func TestSettleAfterCloseWaitsOnlyAfterAClose(t *testing.T) {
	s := newFakeSession()
	w := &recordingWaiter{}
	if !SettleAfterClose(s, w) || w.waited {
		t.Fatalf("no modal was closed, yet it waited (waited=%v)", w.waited)
	}
	if err := CloseModal(s); err != nil {
		t.Fatal(err)
	}
	if !SettleAfterClose(s, w) {
		t.Fatal("SettleAfterClose reported stop")
	}
	if !w.waited {
		t.Fatal("a close was just sent and it did not wait for the engine to apply it")
	}
	if w.condTrue {
		t.Fatal("the settle condition was already true in the tick the close was sent")
	}
}
