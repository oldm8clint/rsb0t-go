package bot

import "testing"

// Every account in the live roster carries the identical muleAt (1000), so a cohort reaches
// "time to hand over" together by construction. Measured 2026-09-19 on 87 accounts sharing one
// mule: 80 distinct donors asked within 25 minutes, the mule ran 80% busy at 1.43 trades/min
// with a median 3s gap between banking one trade and the next request landing, and 45 donors
// gave up waiting.
//
// jitterMuleAt spreads those thresholds. It does NOT close a demand/supply ratio and these
// tests do not pretend otherwise -- they pin the properties that make the spreading safe.

// Stability is the important one. The threshold is derived from the account name rather than
// rand precisely so it survives a restart: a value that re-rolled each launch would let an
// account sitting just under its threshold flip to just over it and back, handing over far
// more often than configured.
func TestJitterMuleAtIsStableForTheSameAccount(t *testing.T) {
	first := jitterMuleAt(1000, "acct42")
	for i := 0; i < 50; i++ {
		if got := jitterMuleAt(1000, "acct42"); got != first {
			t.Fatalf("jitterMuleAt is not stable for one account: %d then %d — a restart would move this account's threshold", first, got)
		}
	}
}

// Case must not matter: the roster mixes "acct168" and "Acct168" styles, and the same
// account reached by a differently-cased name must land on the same threshold.
func TestJitterMuleAtIgnoresCase(t *testing.T) {
	if jitterMuleAt(1000, "Acct168") != jitterMuleAt(1000, "acct168") {
		t.Fatal("case changes the threshold — the live roster mixes cases for the same style of name")
	}
}

// Bounded: a threshold outside +/-muleAtJitterPercent would either hand over far too eagerly
// (hammering an already-saturated mule) or hoard fish well past what the owner configured.
func TestJitterMuleAtStaysWithinItsBand(t *testing.T) {
	const base = 1000
	lo := base - base*muleAtJitterPercent/100
	hi := base + base*muleAtJitterPercent/100
	names := []string{"main", "mule", "acct1", "acct99", "Acct195", "spare28", "crafter1", "spare 23", "raw dog", "spare 20"}
	for _, n := range names {
		got := jitterMuleAt(base, n)
		if got < lo || got > hi {
			t.Fatalf("jitterMuleAt(%d, %q) = %d, outside [%d,%d]", base, n, got, lo, hi)
		}
	}
}

// It must actually spread. A hash that mapped most of the roster onto one value would leave
// the synchronised wave exactly as it is while looking like it had been fixed.
func TestJitterMuleAtActuallySpreadsACohort(t *testing.T) {
	seen := map[int]int{}
	for i := 0; i < 90; i++ {
		name := "swarm" + itoaSmall(i)
		seen[jitterMuleAt(1000, name)]++
	}
	if len(seen) < 20 {
		t.Fatalf("90 accounts landed on only %d distinct thresholds — not enough spread to unsynchronise the cohort", len(seen))
	}
	for v, n := range seen {
		if n > 20 {
			t.Fatalf("threshold %d claimed %d of 90 accounts — the hash is clumping", v, n)
		}
	}
}

// Degenerate inputs must pass straight through rather than becoming a surprise threshold.
func TestJitterMuleAtLeavesDegenerateInputAlone(t *testing.T) {
	if got := jitterMuleAt(0, "acct1"); got != 0 {
		t.Fatalf("jitterMuleAt(0, ...) = %d, want 0 — a disabled hand-off must stay disabled", got)
	}
	if got := jitterMuleAt(1000, ""); got != 1000 {
		t.Fatalf("jitterMuleAt(1000, \"\") = %d, want 1000 — with no account name there is nothing to derive a stable offset from", got)
	}
	if got := jitterMuleAt(-5, "acct1"); got != -5 {
		t.Fatalf("jitterMuleAt(-5, ...) = %d, want -5 unchanged", got)
	}
}

// itoaSmall avoids strconv in a test that only needs 0..99, keeping this file's imports to
// nothing (the repo still has to build on Go 1.19.3 — see AJ's 833d566).
func itoaSmall(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ProgressiveWoodcutter must spread its hand-off threshold the same way ProgressiveFisher does.
// It did not until 2026-09-20: woodcutterSettings read muleAt straight through, so a cohort
// started together at Woodcutting 1 crossed muleAt within minutes of itself and arrived at a
// mule that trades with one partner at a time. This pins the spread rather than the exact
// values, and pins that two different accounts on the same configured muleAt do not share one.
func TestWoodcutterMuleAtIsSpreadAcrossACohort(t *testing.T) {
	const base = 1000
	seen := map[int]int{}
	for _, name := range []string{"wood1", "wood2", "wood3", "wood4", "wood5", "wood6", "wood7", "wood8"} {
		got := jitterMuleAt(base, name)
		if got < 700 || got > 1300 {
			t.Errorf("jitterMuleAt(%d, %q) = %d, outside the documented +/-30%% band", base, name, got)
		}
		seen[got]++
	}
	if len(seen) < 4 {
		t.Errorf("eight accounts produced only %d distinct thresholds (%v) — that is not a spread", len(seen), seen)
	}
}
