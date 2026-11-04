package crafterplan

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestSpinGuardPlainWindowComesFirst(t *testing.T) {
	g := NewSpinGuard(true)
	if g.WantBatch(t0) {
		t.Fatal("batched before any plain baseline")
	}
	g.RecordPlain(SpinPlainWindow-1, 24)
	if g.WantBatch(t0) {
		t.Fatal("batched before the plain window was full")
	}
	g.RecordPlain(1, 4)
	if !g.WantBatch(t0) {
		t.Fatal("did not batch after the plain window")
	}
}

func TestSpinGuardOffNeverBatches(t *testing.T) {
	g := NewSpinGuard(false)
	g.RecordPlain(20, 80)
	if g.WantBatch(t0) {
		t.Fatal("an off guard batched")
	}
	var nilg *SpinGuard
	if nilg.WantBatch(t0) {
		t.Fatal("a nil guard batched")
	}
	nilg.RecordPlain(1, 1)
	if nilg.RecordBatch(1, 1, t0) != "" || nilg.RecordMenuFail(t0) != "" {
		t.Fatal("a nil guard stopped something")
	}
}

func TestSpinGuardKeepsBatchingWhenFaster(t *testing.T) {
	g := NewSpinGuard(true)
	g.RecordPlain(SpinPlainWindow, 25)
	for i := 0; i < 5; i++ {
		if !g.WantBatch(t0) {
			t.Fatalf("stopped batching on pass %d", i)
		}
		if why := g.RecordBatch(27, 58, t0); why != "" {
			t.Fatalf("a batch twice as fast as plain was stopped: %s", why)
		}
	}
	line, ok := g.Line(t0, true)
	if !ok || !strings.Contains(line, "crafter manip: 135 flax in 290 ticks") || !strings.Contains(line, "plain 6 in 25") || !strings.Contains(line, "= x1.9") {
		t.Fatalf("line = %q, %v", line, ok)
	}
}

func TestSpinGuardStopsWhenNotBetter(t *testing.T) {
	g := NewSpinGuard(true)
	g.RecordPlain(SpinPlainWindow, 24) // 25 per 100
	if why := g.RecordBatch(10, 60, t0); why != "" {
		t.Fatalf("judged on 10 flax: %s", why)
	}
	why := g.RecordBatch(20, 120, t0) // 30 flax in 180 ticks = 16.7 per 100, under 85% of 25
	if why == "" {
		t.Fatal("a batch slower than plain was not stopped")
	}
	if g.WantBatch(t0.Add(time.Minute)) {
		t.Fatal("batched while stopped")
	}
	if got := g.DisabledFor(t0.Add(time.Minute)); got != SpinDisableFor-time.Minute {
		t.Fatalf("DisabledFor = %s", got)
	}
	g.RecordPlain(SpinPlainWindow, 24)
	if !g.WantBatch(t0.Add(SpinDisableFor + time.Second)) {
		t.Fatal("did not try the batch again once the stop ran out")
	}
}

func TestSpinGuardMenuFailuresStopIt(t *testing.T) {
	g := NewSpinGuard(true)
	g.RecordPlain(SpinPlainWindow, 24)
	for i := 0; i < SpinMenuFails-1; i++ {
		if why := g.RecordMenuFail(t0); why != "" {
			t.Fatalf("stopped after %d failures", i+1)
		}
	}
	if why := g.RecordMenuFail(t0); why == "" {
		t.Fatal("not stopped after the last failure")
	}
	if g.WantBatch(t0) {
		t.Fatal("batched while stopped")
	}
	// A batch that works clears the count.
	g2 := NewSpinGuard(true)
	g2.RecordPlain(SpinPlainWindow, 24)
	g2.RecordMenuFail(t0)
	g2.RecordMenuFail(t0)
	g2.RecordBatch(27, 58, t0)
	if why := g2.RecordMenuFail(t0); why != "" {
		t.Fatalf("a success did not reset the failure count: %s", why)
	}
}

func TestSpinGuardResamplesThePlainRate(t *testing.T) {
	g := NewSpinGuard(true)
	g.RecordPlain(SpinPlainWindow, 24)
	spun := 0
	for spun < SpinResampleEvery {
		if !g.WantBatch(t0) {
			t.Fatalf("stopped batching at %d flax, before the resample", spun)
		}
		g.RecordBatch(27, 58, t0)
		spun += 27
	}
	if g.WantBatch(t0) {
		t.Fatal("no plain window after the resample interval")
	}
	g.RecordPlain(SpinPlainWindow, 24)
	if !g.WantBatch(t0) {
		t.Fatal("did not go back to batches after the resample window")
	}
}

func TestSpinGuardLineIsRateLimited(t *testing.T) {
	g := NewSpinGuard(true)
	g.RecordPlain(SpinPlainWindow, 24)
	g.RecordBatch(27, 58, t0)
	if _, ok := g.Line(t0, false); !ok {
		t.Fatal("no first line")
	}
	g.RecordBatch(27, 58, t0)
	if _, ok := g.Line(t0.Add(time.Minute), false); ok {
		t.Fatal("a second line inside the report interval")
	}
	if _, ok := g.Line(t0.Add(SpinReportEvery), false); !ok {
		t.Fatal("no line after the report interval")
	}
	if _, ok := g.Line(t0.Add(2*SpinReportEvery), false); ok {
		t.Fatal("a line with nothing new in it")
	}
}
