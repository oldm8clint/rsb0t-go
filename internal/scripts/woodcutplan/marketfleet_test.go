package woodcutplan

import (
	"sync"
	"testing"
	"time"
)

// TestMarketSoldOutIsHoursAndSharedAcrossBuyers pins the fleet-wide half of the fix: one account
// hearing "I have no rune axe right now" keeps every other account away for hours, not the ten
// minutes the per-account memory used to give it.
func TestMarketSoldOutIsHoursAndSharedAcrossBuyers(t *testing.T) {
	var m MarketSoldOut
	now := time.Date(2026, 9, 25, 20, 19, 10, 0, time.UTC)

	if _, ok := m.Until("Seers Market", "Rune axe", now); ok {
		t.Fatal("an empty memory reported the rune axe as sold out")
	}
	until := m.Mark("Seers Market", "Rune axe", now)
	if got := until.Sub(now); got != SoldOutFirst || got < time.Hour {
		t.Fatalf("first sold-out note lasts %s, want %s (hours, not minutes)", got, SoldOutFirst)
	}
	// Name matching ignores case and spacing, so another account's spelling finds the same note.
	if u, ok := m.Until("  seers market ", "rune AXE", now.Add(90*time.Minute)); !ok || !u.Equal(until) {
		t.Errorf("Until under another spelling 90m later = %v,%v, want %v,true", u, ok, until)
	}
	// Other items and other stalls are untouched.
	if _, ok := m.Until("Seers Market", "Adamant axe", now); ok {
		t.Error("a rune-axe note hid the adamant axe")
	}
	if _, ok := m.Until("Tradevault", "Rune axe", now); ok {
		t.Error("the Seers Market's note hid Tradevault's stock")
	}
	if _, ok := m.Until("Seers Market", "Rune axe", until); ok {
		t.Error("the note did not lapse at its own deadline")
	}
}

// TestMarketSoldOutEscalatesOnlyOnFreshConfirmations: accounts already mid-trip when the first
// "none" was heard hear it too, and that is still one answer. Only a re-check after the note has
// lapsed that gets the same answer doubles the wait, capped at SoldOutMax; a sale resets it.
func TestMarketSoldOutEscalatesOnlyOnFreshConfirmations(t *testing.T) {
	var m MarketSoldOut
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	first := m.Mark("Seers Market", "Rune axe", now)
	for i := 0; i < 5; i++ {
		if again := m.Mark("Seers Market", "Rune axe", now.Add(time.Duration(i)*time.Minute)); !again.Equal(first) {
			t.Fatalf("a concurrent confirmation moved the note from %v to %v", first, again)
		}
	}
	at := first
	for i, want := range []time.Duration{2 * SoldOutFirst, 4 * SoldOutFirst, SoldOutMax, SoldOutMax} {
		next := m.Mark("Seers Market", "Rune axe", at)
		if got := next.Sub(at); got != want {
			t.Fatalf("fresh confirmation %d waits %s, want %s", i+2, got, want)
		}
		at = next
	}
	m.Clear("SEERS MARKET", "rune axe")
	if _, ok := m.Until("Seers Market", "Rune axe", at); ok {
		t.Fatal("Clear did not forget the note")
	}
	if got := m.Mark("Seers Market", "Rune axe", at).Sub(at); got != SoldOutFirst {
		t.Errorf("after a sale the next note waits %s, want the first-strike %s", got, SoldOutFirst)
	}
}

// TestMarketSoldOutConcurrentUse is run under -race by anyone who asks for it: every account in
// the fleet process shares one instance.
func TestMarketSoldOutConcurrentUse(t *testing.T) {
	var m MarketSoldOut
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Mark("Seers Market", "Rune axe", now)
			m.Until("Seers Market", "Rune axe", now)
			if i%4 == 0 {
				m.Clear("Seers Market", "Adamant axe")
			}
		}(i)
	}
	wg.Wait()
	if _, ok := m.Until("Seers Market", "Rune axe", now); !ok {
		t.Fatal("concurrent marks lost the note")
	}
	var nilMemory *MarketSoldOut
	nilMemory.Mark("Seers Market", "Rune axe", now)
	nilMemory.Clear("Seers Market", "Rune axe")
	if _, ok := nilMemory.Until("Seers Market", "Rune axe", now); ok {
		t.Fatal("a nil memory reported something sold out")
	}
}

// TestMarketBusyBackoffGrowsAndIsJittered: the old fixed 90 s kept a crowd in step. The first wait
// is unchanged, later ones double to a cap, and the jitter spreads a cohort that lost together.
func TestMarketBusyBackoffGrowsAndIsJittered(t *testing.T) {
	mid := func(n int) time.Duration { return MarketBusyBackoff(n, 0.5) }
	if got := mid(1); got != MarketBusyFirst {
		t.Fatalf("first lost turn waits %s at mid jitter, want the old %s", got, MarketBusyFirst)
	}
	if got := mid(0); got != MarketBusyFirst {
		t.Errorf("zero losses waits %s, want it treated as the first", got)
	}
	if mid(2) != 2*MarketBusyFirst || mid(3) != 4*MarketBusyFirst {
		t.Errorf("waits %s, %s, want doubling from %s", mid(2), mid(3), MarketBusyFirst)
	}
	if got := mid(50); got != MarketBusyMax {
		t.Errorf("many losses wait %s, want the %s cap", got, MarketBusyMax)
	}
	lo, hi := MarketBusyBackoff(1, 0), MarketBusyBackoff(1, 0.999)
	if lo >= hi || lo < MarketBusyFirst*3/4 || hi > MarketBusyFirst*5/4 {
		t.Errorf("jitter spread %s..%s, want within 0.75x..1.25x of %s and not collapsed", lo, hi, MarketBusyFirst)
	}
	if got := MarketBusyBackoff(1, 7); got > MarketBusyFirst*5/4 {
		t.Errorf("an out-of-range jitter produced %s", got)
	}
}
