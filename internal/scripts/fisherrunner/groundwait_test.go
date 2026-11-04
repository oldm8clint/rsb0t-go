package fisherrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

func sleepDelay(d time.Duration) bool { time.Sleep(d); return true }

// The spat-out tool can reach the client's ground items a moment after the pack update that
// removed it; the search must wait for it rather than give up at once.
func TestGroundSearchWaitsForALateGroundItem(t *testing.T) {
	appearAt := time.Now().Add(500 * time.Millisecond)
	item, ok := waitForGround(func() (gamestate.GroundItem, bool) {
		if time.Now().After(appearAt) {
			return gamestate.GroundItem{ID: 305, X: 2850, Z: 3425}, true
		}
		return gamestate.GroundItem{}, false
	}, sleepDelay, 3*time.Second)
	if !ok || item.ID != 305 {
		t.Fatalf("late ground item not found: %+v %v", item, ok)
	}
}

func TestGroundSearchGivesUpAfterItsWait(t *testing.T) {
	start := time.Now()
	if _, ok := waitForGround(func() (gamestate.GroundItem, bool) { return gamestate.GroundItem{}, false }, sleepDelay, 400*time.Millisecond); ok {
		t.Fatal("found an item that never appeared")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("gave up after %s, want about the wait", took)
	}
}

func TestGroundSearchStopsOnAStopRequest(t *testing.T) {
	if _, ok := waitForGround(func() (gamestate.GroundItem, bool) { return gamestate.GroundItem{}, false }, func(time.Duration) bool { return false }, time.Minute); ok {
		t.Fatal("kept searching after a stop request")
	}
}
