package woodcutrunner

import (
	"testing"
	"time"
)

// TestAcquireMarketTurn pins the one-buyer-at-a-time rule.
//
// A stall serves one trade at a time and the whole fleet shares one process, so without this
// twelve woodcutters interleave rather than queue. Measured live on 2026-09-21: three accounts
// logged the identical "1 X rune axe = 150,000gp (150,000ea). Trade me." within the same second,
// all asked to trade, and all got "the seller never offered it" because the stall had promised
// the axe to whichever asked last.
func TestAcquireMarketTurn(t *testing.T) {
	d := &Deps{Delay: func(time.Duration) bool { return true }}

	release, ok := acquireMarketTurn(d, "Seers Market", time.Second)
	if !ok {
		t.Fatal("the first buyer could not take a free stall's turn")
	}

	// A second buyer must NOT get in while the first holds it.
	if _, ok := acquireMarketTurn(d, "Seers Market", 20*time.Millisecond); ok {
		t.Error("a second buyer took the turn while the first still held it")
	}

	// A different stall is independent — two stalls must never block each other.
	otherRelease, ok := acquireMarketTurn(d, "Rune Axe", 20*time.Millisecond)
	if !ok {
		t.Error("a different stall's turn was blocked by an unrelated stall")
	} else {
		otherRelease()
	}

	// Once released, the next buyer gets in.
	release()
	next, ok := acquireMarketTurn(d, "Seers Market", time.Second)
	if !ok {
		t.Fatal("the turn was not handed on after release")
	}

	// Releasing twice must not free a turn someone else now holds.
	release()
	if _, ok := acquireMarketTurn(d, "Seers Market", 20*time.Millisecond); ok {
		t.Error("a double release let a third buyer in while the second held the turn")
	}
	next()

	// Name matching ignores case and spacing, like every other stall comparison here.
	same, ok := acquireMarketTurn(d, "  seers market  ", 20*time.Millisecond)
	if !ok {
		t.Fatal("a freed turn was not available under a differently-cased name")
	}
	if _, ok := acquireMarketTurn(d, "Seers Market", 20*time.Millisecond); ok {
		t.Error("case or spacing created a second, separate turn for the same stall")
	}
	same()
}
