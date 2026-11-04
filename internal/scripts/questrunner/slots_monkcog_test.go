package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Monk's Friend and the Clock Tower (17 Oct 2026) run under fleet-wide slots of four: the blanket is one ground spawn in a thieves'
// cave, and the Clock Tower's cogs, rat poison and bucket are single ground spawns too. The fifth account waits and a release
// lets it in. (Monk's Friend names ExclusiveDone, the end of its shared step, and the engine's narrow-slot release is
// narrowslot_test.go's; the Clock Tower names none and holds its slot to the end of the run: monkcog_test.go in questplan.)
func TestMonksFriendAndTheClockTowerRunUnderSlotsOfFour(t *testing.T) {
	for _, id := range []string{questplan.MonksFriendID, questplan.ClockTowerID} {
		if !exclusiveQuests[id] || capacityOf(id) != 4 {
			t.Errorf("%s: exclusive %v capacity %d, want true and 4", id, exclusiveQuests[id], capacityOf(id))
		}
		now := time.Now()
		ds := make([]*Deps, 6)
		for i := range ds {
			ds[i] = &Deps{}
		}
		for i := 0; i < 4; i++ {
			if !tryHoldQuest(id, ds[i], now) {
				t.Fatalf("%s: account %d of 4 was refused", id, i+1)
			}
		}
		if tryHoldQuest(id, ds[4], now) {
			t.Errorf("%s: a fifth account was admitted", id)
		}
		releaseQuest(id, ds[0])
		if !tryHoldQuest(id, ds[4], now) {
			t.Errorf("%s: a released slot was not given to the fifth account", id)
		}
		for _, d := range ds {
			releaseQuest(id, d)
		}
		if _, held := slots[id]; held {
			t.Errorf("%s: slots left behind: %v", id, slots[id])
		}
	}
}
