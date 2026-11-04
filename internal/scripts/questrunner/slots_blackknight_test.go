package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Black Knights' Fortress holds an exclusive slot, two accounts at a time: fifteen aggressive knights about one loop of ladders and
// doors and the shops' two or three iron chainbodies are what a crowd of accounts would collide on. The quest's own ExclusiveDone
// gives the slot up once the potion is ruined and the account is out of the fortress.
func TestBlackKnightsFortressHoldsAnExclusiveSlotOfTwo(t *testing.T) {
	id := questplan.BlackKnightsFortressID
	if !exclusiveQuests[id] || capacityOf(id) != 2 || slotRepeats[id] {
		t.Fatalf("exclusive %v capacity %d repeats %v", exclusiveQuests[id], capacityOf(id), slotRepeats[id])
	}
	a, b, c := &Deps{}, &Deps{}, &Deps{}
	now := time.Now()
	if !tryHoldQuest(id, a, now) || !tryHoldQuest(id, b, now) {
		t.Fatal("the first two accounts did not get a slot")
	}
	if tryHoldQuest(id, c, now) {
		t.Error("a third account got a slot: the capacity is two")
	}
	releaseQuest(id, a)
	if !tryHoldQuest(id, c, now) {
		t.Error("the slot a holder gave up was not free")
	}
	releaseQuest(id, b)
	releaseQuest(id, c)
}
