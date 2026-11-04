package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Prince Ali Rescue runs under a three-account slot: Lady Keli and Prince Ali are single npcs that one account's rope or rescue
// removes for every player for a hundred ticks, the logs and the pickaxe are shared spawns, and the module releases the slot the
// moment the prince is out (Module.ExclusiveDone), so the long gathering after it does not hold a place.
func TestPrinceAliRescueRunsUnderASmallSlotReleasedAtTheRescue(t *testing.T) {
	id := questplan.PrinceAliID
	if !exclusiveQuests[id] || capacityOf(id) != 3 {
		t.Fatalf("exclusive %v capacity %d, want true and 3", exclusiveQuests[id], capacityOf(id))
	}
	if questplan.PrinceAli.ExclusiveDone == nil {
		t.Fatal("no ExclusiveDone: the slot would be held until the quest is complete")
	}
	for stage, want := range map[int]bool{questplan.PrinceSpokeOsman: false, questplan.PrinceHasEquipment: false,
		questplan.PrinceGuardDrunk: false, questplan.PrinceTiedKeli: false, questplan.PrinceSaved: true, questplan.PrinceComplete: true} {
		if got := questplan.PrinceAli.ExclusiveDone(questplan.Snapshot{Stage: stage, StageKnown: true}); got != want {
			t.Errorf("stage %d: past the compound %v, want %v", stage, got, want)
		}
	}
	if questplan.PrinceAli.ExclusiveDone(questplan.Snapshot{Stage: questplan.PrinceSaved}) {
		t.Error("an unread stage is not past the compound")
	}
	// Three accounts hold it, a fourth waits, and a release lets the fourth in.
	now := time.Now()
	ds := []*Deps{{}, {}, {}, {}}
	defer func() {
		for _, d := range ds {
			releaseQuest(id, d)
		}
	}()
	for i := 0; i < 3; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("holder %d refused", i)
		}
	}
	if tryHoldQuest(id, ds[3], now) {
		t.Fatal("a fourth account must wait")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[3], now) {
		t.Fatal("a release must let the fourth account in")
	}
}
