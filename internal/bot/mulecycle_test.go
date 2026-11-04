package bot

import (
	"testing"
	"time"
)

// The mule's published ETA is a promise a donor abandons its load on, so the number behind it has
// to be one the mule can keep. Live 2026-09-23 it could not: 1.08 trades/min actually completed
// against a flat 30s book figure, so every queuing donor was told roughly half the truth and
// walked off when the mule missed it. cycle() is the mule measuring itself instead.
func TestTheMuleQuotesItsOwnMeasuredCycleNotTheBookFigure(t *testing.T) {
	b := &muleBot{}

	if got := b.cycle(); got != muleCycle {
		t.Fatalf("before it has measured anything the mule must quote the book figure, got %s", got)
	}

	// A first slow cycle with somebody queued behind it is taken whole.
	b.lastTradeAt = time.Now().Add(-55 * time.Second)
	b.noteCycle(2)
	if got := b.cycle(); got < 50*time.Second || got > 60*time.Second {
		t.Fatalf("one measured 55s cycle: got %s, want about 55s", got)
	}

	// Further cycles move it, but only a quarter of the way, so one outlier cannot swing it.
	for i := 0; i < 6; i++ {
		b.lastTradeAt = time.Now().Add(-75 * time.Second)
		b.noteCycle(1)
	}
	if got := b.cycle(); got < 65*time.Second || got > 76*time.Second {
		t.Fatalf("settling on 75s cycles: got %s", got)
	}
}

// The measurement only means anything when somebody was waiting THROUGH the cycle. With an empty
// queue the gap between trades is mostly the mule standing idle, which is not what a queuing
// donor is asking about — folding it in would quote an eta of minutes to a donor who is next.
func TestAnIdleGapIsNotAHandoffCycle(t *testing.T) {
	b := &muleBot{lastTradeAt: time.Now().Add(-4 * time.Minute)}
	b.noteCycle(0)
	if got := b.cycle(); got != muleCycle {
		t.Fatalf("a four-minute idle gap was counted as a cycle: got %s", got)
	}
}

// Bounds at both ends, so a restart, a clock jump or one stalled trade cannot poison the average
// or produce an absurd quote.
func TestTheMeasuredCycleIsBounded(t *testing.T) {
	b := &muleBot{lastTradeAt: time.Now().Add(-2 * time.Second)}
	b.noteCycle(3) // implausibly fast, ignored
	if got := b.cycle(); got != muleCycle {
		t.Errorf("a 2s gap should be ignored, got %s", got)
	}

	b = &muleBot{lastTradeAt: time.Now().Add(-9 * time.Minute)}
	b.noteCycle(3) // implausibly slow, ignored
	if got := b.cycle(); got != muleCycle {
		t.Errorf("a nine-minute gap should be ignored, got %s", got)
	}

	// And a real but extreme run of slow cycles is capped rather than quoted.
	b = &muleBot{}
	for i := 0; i < 20; i++ {
		b.lastTradeAt = time.Now().Add(-4*time.Minute - 30*time.Second)
		b.noteCycle(1)
	}
	if got := b.cycle(); got != muleCycleCap {
		t.Errorf("a long run of 4m30s cycles should quote the cap: got %s, want %s", got, muleCycleCap)
	}

	// A mule that has never traded has no lastTradeAt, and must not read the zero time as a gap.
	b = &muleBot{}
	b.noteCycle(5)
	if got := b.cycle(); got != muleCycle {
		t.Errorf("no previous trade should leave the book figure alone, got %s", got)
	}
}

// LIVE 26 Sep (mulequeue): only donor trades were measured, so a mule serving a kit every 20 s
// between two donor trades quoted 1m35s per queue entry. A delivered hand-out now counts too.
func TestAHandoutTradeIsMeasuredAsACycle(t *testing.T) {
	b := &muleBot{kits: map[string]queuedKit{"next": {}}}
	b.lastTradeAt = time.Now().Add(-20 * time.Second)
	b.noteHandoutTrade()
	if got := b.cycle(); got < 15*time.Second || got > 25*time.Second {
		t.Fatalf("a 20s kit hand-out with a kit queued behind it: cycle %s, want about 20s", got)
	}
	if time.Since(b.lastTradeAt) > time.Second {
		t.Fatal("a hand-out must move lastTradeAt, or the next gap spans it")
	}
	// Nobody waiting behind it: an idle gap, not a cycle.
	idle := &muleBot{lastTradeAt: time.Now().Add(-3 * time.Minute)}
	idle.noteHandoutTrade()
	if got := idle.cycle(); got != muleCycle {
		t.Fatalf("an idle gap before a lone hand-out was counted: %s", got)
	}
}
