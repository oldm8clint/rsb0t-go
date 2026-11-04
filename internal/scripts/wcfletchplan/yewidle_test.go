package wcfletchplan

import (
	"strings"
	"testing"
	"time"
)

// A pass that chopped nothing is counted in its kind, the spells are separated by work, and a report
// resets the window.
func TestIdleClockCountsPassesAndSpells(t *testing.T) {
	var c IdleClock
	now := t0
	if c.Due(now) {
		t.Error("an unused clock is due")
	}
	c.Worked(now) // starts the window
	if c.Due(now.Add(IdleReportEvery - time.Second)) {
		t.Error("due early")
	}
	for i := 0; i < 5; i++ {
		c.Pass(IdleStumps, 2*time.Second, now)
		now = now.Add(2 * time.Second)
	}
	if c.Spell() != 10*time.Second {
		t.Errorf("spell = %v, want 10s", c.Spell())
	}
	c.Worked(now)
	if c.Spell() != 0 {
		t.Errorf("work did not end the spell: %v", c.Spell())
	}
	c.Pass(IdleExcluded, 3*time.Second, now)
	c.Pass(IdleStumps, 2*time.Second, now)
	now = t0.Add(IdleReportEvery)
	if !c.Due(now) {
		t.Error("not due after the window")
	}
	r := c.Take(now)
	if r.Stump != 12*time.Second || r.Excluded != 3*time.Second || r.Spells != 2 || r.Window != IdleReportEvery {
		t.Errorf("report %+v", r)
	}
	if r.Idle() != 15*time.Second {
		t.Errorf("idle %v", r.Idle())
	}
	if f := r.Fraction(); f < 0.0166 || f > 0.0167 {
		t.Errorf("fraction %v, want 15s of 900s", f)
	}
	// The spell in progress carries into the next window as one spell; the idle seconds do not.
	r2 := c.Take(now.Add(time.Minute))
	if r2.Idle() != 0 || r2.Spells != 1 || r2.Window != time.Minute {
		t.Errorf("second report %+v", r2)
	}
	if got := r.Describe(); !strings.Contains(got, "15s waiting = 1.7% of 900s") || !strings.Contains(got, "12s no yew standing in 2 spell(s)") || !strings.Contains(got, "3s every yew excluded") {
		t.Errorf("describe = %q", got)
	}
	if (IdleReport{}).Fraction() != 0 {
		t.Error("an empty window has a fraction")
	}
	if over := (IdleReport{Window: time.Second, Stump: time.Minute}).Fraction(); over != 1 {
		t.Errorf("a fraction past 1: %v", over)
	}
}

// The ledger adds the accounts by world and speaks once a window.
func TestIdleLedgerFlushesOncePerWindow(t *testing.T) {
	l := NewIdleLedger()
	rep := func(idle time.Duration) IdleReport {
		return IdleReport{Window: IdleReportEvery, Stump: idle}
	}
	if line, ok := l.Add(1, rep(30*time.Second), t0); ok {
		t.Fatalf("flushed at once: %q", line)
	}
	if _, ok := l.Add(2, rep(0), t0.Add(5*time.Minute)); ok {
		t.Fatal("flushed early")
	}
	if _, ok := l.Add(2, rep(60*time.Second), t0.Add(10*time.Minute)); ok {
		t.Fatal("flushed early")
	}
	line, ok := l.Add(3, rep(90*time.Second), t0.Add(IdleReportEvery))
	if !ok {
		t.Fatal("did not flush after the window")
	}
	for _, want := range []string{
		"wcfletch: cohort idle over the last 15 min: 5.0% (180s waiting of 3600s account time, 4 report(s))",
		"world 1 3.3% (1 report(s), 30s idle of 900s)",
		"world 2 3.3% (2 report(s), 60s idle of 1800s)",
		"world 3 10.0% (1 report(s), 90s idle of 900s)",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("%q is missing from %q", want, line)
		}
	}
	// And the next window starts empty.
	if line, ok := l.Add(1, rep(0), t0.Add(IdleReportEvery+time.Minute)); ok {
		t.Errorf("flushed again at once: %q", line)
	}
}
