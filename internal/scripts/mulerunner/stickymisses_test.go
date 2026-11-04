package mulerunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
)

// After an accepted trade RunCycle marks the partner as fully missed, so a donor that has just
// finished is not asked again before it cancels. That mark must end with the hand-off: once the
// donor cancels (Forget) or announces a new hand-off (Announced), the same donor at the head of the
// queue, beside the mule, is asked first again. CONTROL: with the mark left in place, it is not
// (the behaviour main had, adapted from the review's scratch test zz_revh_mulerunner_test.go).
func TestADonorsNextHandOffIsAskedFirstAgain(t *testing.T) {
	s := &tradeSession{}
	muleAmong(s, map[string]int{"donorb": 1})
	chat, logs := chatlog.New(), &lineLog{}
	d := queueDeps(s, chat, logs)
	d.Queue = func() []string { return []string{"donorb"} }
	allowed := AllowList([]string{"donorb"})
	marked := func() *RequestWatcher {
		w := NewRequestWatcher()
		w.misses = map[string]int{normalisePlayerName("Donorb"): maxProactiveMisses}
		return w
	}

	control := marked()
	RunCycle(d, control, allowed, 5*time.Millisecond)
	if control.awaitingPartner == "Donorb" {
		t.Fatal("control: with the post-trade mark still set, the donor should not be asked first")
	}

	for name, clear := range map[string]func(*RequestWatcher){
		"after its cancel":           func(w *RequestWatcher) { w.Forget("donorb") },
		"after a new announcement":   func(w *RequestWatcher) { w.Announced("donorb") },
		"after its cancel, by login": func(w *RequestWatcher) { w.Forget("DonorB") },
	} {
		w := marked()
		clear(w)
		RunCycle(d, w, allowed, 5*time.Millisecond)
		if w.awaitingPartner != "Donorb" || !w.proactive {
			t.Errorf("%s: the donor's next hand-off was not asked first (awaiting %q, misses %v)", name, w.awaitingPartner, w.misses)
		}
	}
}

// The accepted-trade step drops the partner's requests and THEN marks it served, so the mark
// survives: Forget clears misses, and the reverse order would ask a donor that just finished to
// trade again. Its own next cancel or announcement then clears it.
func TestAnAcceptedTradeMarksThePartnerAfterForgettingIt(t *testing.T) {
	w := NewRequestWatcher()
	w.queue("Donorb", time.Now())
	w.traded("Donorb")
	if got := w.MissedFirstRequests("donorb"); got != maxProactiveMisses {
		t.Fatalf("after an accepted trade the partner's misses are %d, want %d", got, maxProactiveMisses)
	}
	if w.QueuedRequests() != 0 {
		t.Fatal("the partner's queued requests were not dropped")
	}
	w.Forget("donorb")
	if got := w.MissedFirstRequests("donorb"); got != 0 {
		t.Fatalf("after the partner's cancel its misses are %d, want 0", got)
	}
}
