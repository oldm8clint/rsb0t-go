package runerunnerplan

import (
	"strings"
	"testing"
	"time"
)

func TestSwarmTargetFollowsTheFullShelfWorth(t *testing.T) {
	c := SwarmCheck{On: true, Rotation: true, Resume: 1500, FullWorth: 30000, ReadyPercent: 60}
	if c.Target() != 18000 {
		t.Fatalf("target %d", c.Target())
	}
	// 5 Nov: a circuit worth 14,000 right after the last one is not walked, but swarm until it is.
	c.CircuitWorth = 14000
	if ok, why := SwarmAfterRound(c); !ok || !strings.Contains(why, "60% of the 30000") {
		t.Errorf("14,000 of 30,000 should hand over to ScarabSwarm: %v %s", ok, why)
	}
	if back, _ := SwarmResume(c); back {
		t.Errorf("14,000 is not back yet")
	}
	c.CircuitWorth = 18000
	if ok, _ := SwarmAfterRound(c); ok {
		t.Errorf("18,000 is the target: stay on the circuit")
	}
	if back, _ := SwarmResume(c); !back {
		t.Errorf("18,000 is back")
	}
	// A short mule halves the share; percent 0 is the old rule.
	c.CircuitWorth, c.MuleShort = 9000, true
	if c.Target() != 9000 {
		t.Errorf("short target %d", c.Target())
	}
	c.MuleShort, c.ReadyPercent = false, 0
	if c.Target() != 1500 {
		t.Errorf("off target %d", c.Target())
	}
}

func TestRestockWaitFindsTheFirstStep(t *testing.T) {
	// a circuit regrowing 400 runes a minute from 6,000
	worth := func(d time.Duration) int { return 6000 + int(d.Minutes())*400 }
	if w, ok := RestockWait(worth, 18000, 5*time.Minute, 12*time.Hour); !ok || w != 30*time.Minute {
		t.Errorf("wait %v %v", w, ok)
	}
	if w, ok := RestockWait(worth, 1000, 5*time.Minute, time.Hour); !ok || w != 0 {
		t.Errorf("already ready: %v %v", w, ok)
	}
	if _, ok := RestockWait(worth, 1000000, 5*time.Minute, time.Hour); ok {
		t.Errorf("never")
	}
}
