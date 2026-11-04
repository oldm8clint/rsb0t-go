package thieverrunner

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A bite is confirmed for longer than the slowest route takes to answer. The fleet's Psiphon routes pass a latency
// gate of 2.5 s a round trip, and a bite also waits for the next tick: the 2.4 s confirmation of 11 Oct 2026 called
// 3,044 landed bites failures. The wait is only ever spent in full on a bite that did not land.
func TestEatConfirmOutlastsASlowRoute(t *testing.T) {
	const slowestRoundTrip, tick = 2500 * time.Millisecond, 600 * time.Millisecond
	if EatConfirmWait < slowestRoundTrip+2*tick {
		t.Errorf("EatConfirmWait = %s, want over the slowest route's round trip %s plus two ticks", EatConfirmWait, slowestRoundTrip)
	}
	if EatSlowBite != 2400*time.Millisecond {
		t.Errorf("EatSlowBite = %s: it is the old confirmation, the bite it would have called a failure", EatSlowBite)
	}
}

func TestLogSlowBiteOnlySpeaksForABiteTheOldWaitWouldHaveFailed(t *testing.T) {
	var lines []string
	logf := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
	LogSlowBite(logf, "Shrimps", 900*time.Millisecond)
	if len(lines) != 0 {
		t.Fatalf("a quick bite was logged: %v", lines)
	}
	LogSlowBite(logf, "Shrimps", 3140*time.Millisecond)
	if len(lines) != 1 || !strings.Contains(lines[0], "Shrimps") || !strings.Contains(lines[0], "3.1s") {
		t.Fatalf("a slow bite logged %v", lines)
	}
	LogSlowBite(nil, "Shrimps", time.Minute) // no logger: no panic
}
