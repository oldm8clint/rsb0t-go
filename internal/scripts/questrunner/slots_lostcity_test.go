package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Lost City runs under a fleet-wide slot of one (19 Oct 2026): Shamus is one npc a hundred ticks long at one tree, the zombies that drop the axe
// are five spawns of one cave, and the Tree spirit is one npc with one health bar whose defeat is credited to the player who dealt most. The
// slot is held until the account is past the spirit and never again (the stage never goes back), so a later run for a branch is not queued.
func TestLostCityRunsAloneUntilTheSpiritIsBeaten(t *testing.T) {
	id := questplan.LostCityID
	if !exclusiveQuests[id] || capacityOf(id) != 1 {
		t.Fatalf("%s: exclusive %v capacity %d, want true and 1", id, exclusiveQuests[id], capacityOf(id))
	}
	if slotRepeats[id] {
		t.Errorf("%s is a repeating slot", id)
	}
	now := time.Now()
	a, b := &Deps{}, &Deps{}
	if !tryHoldQuest(id, a, now) {
		t.Fatal("the first account was refused")
	}
	if tryHoldQuest(id, b, now) {
		t.Error("a second account was admitted while the first is on the spirit")
	}
	if !SlotsFree(a, []string{id}, now) || SlotsFree(b, []string{id}, now) {
		t.Error("SlotsFree: the holder sees its own slot and the other does not")
	}
	releaseQuest(id, a)
	if !tryHoldQuest(id, b, now) {
		t.Error("a released slot was not given to the next account")
	}
	releaseQuest(id, b)
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
	// A holder that never released it (a relog mid-quest) is dropped after the stale limit, which is over the quest's own slot.
	if got := questplan.QuestSlot(id, 20*time.Minute); got >= slotStale {
		t.Errorf("the quest's slot %s is not under the stale limit %s", got, slotStale)
	}
	// The engine gives the slot up at the module's word: once the journal says the spirit is defeated, and not before.
	m := questplan.LostCity
	if m.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.LcSpokenShamus, StageKnown: true}) ||
		!m.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.LcSpiritDefeated, StageKnown: true}) {
		t.Error("Lost City's slot is not released at the spirit")
	}
}
