package bot

import (
	"testing"
	"time"
)

// TestKeyOrderWaitPausesAndSaysSo is spare104's thirty silent minutes at the mule: with the next
// order half an hour away every pass must pause (never zero -- that was the spin), say so at once
// and then every askWaitLogEvery (under the watchdog's ten-minute silence), and stop waiting the
// moment the order is due.
func TestKeyOrderWaitPausesAndSaysSo(t *testing.T) {
	var w askWait
	t0 := time.Date(2026, 9, 23, 6, 30, 37, 0, time.UTC)
	due := t0.Add(30 * time.Minute)
	var logs []time.Duration
	now := t0
	for passes := 0; ; passes++ {
		st := w.plan(now, due)
		if !now.Before(due) {
			if st.Wait {
				t.Fatalf("still waiting at %s, past the due time", now.Sub(t0))
			}
			break
		}
		if !st.Wait || st.Pause <= 0 || st.Pause > askWaitPass {
			t.Fatalf("pass %d at +%s: %+v -- every pass before the order is due must pause, briefly", passes, now.Sub(t0), st)
		}
		if st.Log {
			logs = append(logs, now.Sub(t0))
		}
		now = now.Add(st.Pause)
		if passes > 10000 {
			t.Fatal("the wait never ended")
		}
	}
	if len(logs) == 0 || logs[0] != 0 {
		t.Fatalf("the wait must say so on its first pass: %v", logs)
	}
	for i := 1; i < len(logs); i++ {
		if gap := logs[i] - logs[i-1]; gap < askWaitLogEvery || gap > askWaitLogEvery+askWaitPass {
			t.Fatalf("lines at %v: gap %s, want about %s", logs, gap, askWaitLogEvery)
		}
	}
	if gapToEnd := 30*time.Minute - logs[len(logs)-1]; gapToEnd > askWaitLogEvery+askWaitPass {
		t.Fatalf("last line %s before the end, longer than the log cadence", gapToEnd)
	}
	// Due, and a later wait starts its own spell.
	if st := w.plan(due.Add(time.Second), due); st.Wait {
		t.Fatal("an order that is due must be asked, not waited on")
	}
	if st := w.plan(due.Add(time.Minute), due.Add(31*time.Minute)); !st.Wait || !st.Log || st.For != 0 {
		t.Fatalf("a new wait must start fresh and say so: %+v", st)
	}
}

// TestKeyOrderWaitNotArmed: a zero clock (never declined, or reset by a delivery) never waits.
func TestKeyOrderWaitNotArmed(t *testing.T) {
	var w askWait
	if st := w.plan(time.Now(), time.Time{}); st.Wait {
		t.Fatal("no retry clock set: ask")
	}
}
