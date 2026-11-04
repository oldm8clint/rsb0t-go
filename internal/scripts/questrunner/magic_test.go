package questrunner

import (
	"testing"
	"time"
)

// A cast is waited out in server ticks: on a fast (lab) engine five ticks come quickly and the
// wait ends with them, well inside the wall-clock ceiling; with no ticks at all it ends at the
// ceiling; and a stopping bot ends it at once.
func TestWaitTicksCountsServerTicksNotWallClock(t *testing.T) {
	ticks := 0
	tick := func() int { return ticks }
	delays := 0
	fast := func(time.Duration) bool { delays++; ticks++; return true } // one tick per 100 ms step
	start := time.Now()
	if !waitTicks(fast, tick, castTicks, time.Hour) {
		t.Fatal("waitTicks reported a stop")
	}
	if delays != castTicks {
		t.Fatalf("waited %d steps for %d ticks", delays, castTicks)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("a tick-driven wait took %s", time.Since(start))
	}

	frozen := func(d time.Duration) bool { time.Sleep(d / 10); return true }
	start = time.Now()
	if !waitTicks(frozen, func() int { return 0 }, castTicks, 150*time.Millisecond) {
		t.Fatal("waitTicks reported a stop")
	}
	if el := time.Since(start); el < 150*time.Millisecond || el > 2*time.Second {
		t.Fatalf("with no ticks the wait took %s, want the 150ms ceiling", el)
	}

	if waitTicks(func(time.Duration) bool { return false }, func() int { return 0 }, castTicks, time.Hour) {
		t.Fatal("a stopping bot's wait did not report the stop")
	}
}
