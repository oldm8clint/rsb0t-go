package actions

import (
	"bytes"
	"testing"
)

func TestCoalescerWritesOneSendWithEveryPacketInOrder(t *testing.T) {
	f := newFakeSession()
	b := NewCoalescer(f)
	if err := IfButton(b.Session(), 8889); err != nil {
		t.Fatal(err)
	}
	if err := IfButton(b.Session(), 8874); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 0 || b.Pending() != 2 {
		t.Fatalf("a queued action reached the session: %d sends, %d pending", f.sendCount(), b.Pending())
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 1 || b.Pending() != 0 {
		t.Fatalf("flush made %d sends, %d still pending", f.sendCount(), b.Pending())
	}

	// The one message is the two packets the actions send alone, back to back.
	a, c := newFakeSession(), newFakeSession()
	_ = IfButton(a, 8889)
	_ = IfButton(c, 8874)
	want := append(append([]byte(nil), a.lastSent()...), c.lastSent()...)
	if !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("coalesced bytes % x, want % x", f.lastSent(), want)
	}
}

func TestCoalescerFlushOfNothingSendsNothing(t *testing.T) {
	f := newFakeSession()
	b := NewCoalescer(f)
	if err := b.Flush(); err != nil || f.sendCount() != 0 {
		t.Fatalf("empty flush: err %v, %d sends", err, f.sendCount())
	}
}

func TestCoalescerDiscardDropsTheQueue(t *testing.T) {
	f := newFakeSession()
	b := NewCoalescer(f)
	_ = IfButton(b.Session(), 1)
	b.Discard()
	if err := b.Flush(); err != nil || f.sendCount() != 0 {
		t.Fatalf("a discarded queue was sent: err %v, %d sends", err, f.sendCount())
	}
}

func TestCoalescerSessionSharesTheRealStateAndCounters(t *testing.T) {
	f := newFakeSession()
	b := NewCoalescer(f)
	if b.Session().State() != f.State() || b.Session().Cheats() != f.Cheats() {
		t.Fatal("the coalescing session must expose the real session's state and counters")
	}
}

func TestCoalescerPropagatesASendError(t *testing.T) {
	f := newFakeSession()
	f.sendErr = errBoom
	b := NewCoalescer(f)
	_ = IfButton(b.Session(), 1)
	if err := b.Flush(); err != errBoom {
		t.Fatalf("got %v, want the session's error", err)
	}
	if b.Pending() != 0 {
		t.Fatal("a failed flush must still empty the queue so the next batch starts clean")
	}
}
