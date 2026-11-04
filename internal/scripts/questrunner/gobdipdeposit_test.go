package questrunner

// Goblin Diplomacy's full-pack deposit (stage5idle-1017). Live 17 Oct 2026, 22 parks on "bank all but 23 kept item type(s)":
// an account held at Stage 5 arrives with the hold's eight food, the trainer's leftovers, the run staller's four kit items
// and the quest's own items, and the module's keep list named every food id, so the deposit took nothing out of a pack with no
// room, came round every tick, and parked after eight (each park ten slot-minutes of the fleet's twelve slots and a six hour
// retry). The step is now bare and keeps the quest's items alone.

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
)

// gobdipFullPack is an account at an open bank whose pack is the live shape: sixteen food (the sustain's twelve kinds, some of
// them twice), the run staller's four kit items and eight of the quest's items, 28 slots in all. KeepInPack claims the kit.
func gobdipFullPack(t *testing.T) (e *Engine, sim *actionsim.Session, atBank *int, kit, quest []int) {
	t.Helper()
	foods := questplan.GoblinDiplomacy.Sustain.FoodIDs
	var stacks []actionsim.Stack
	for i := 0; i < 16; i++ {
		stacks = append(stacks, actionsim.Stack{ID: int32(foods[i%len(foods)]), Count: 1})
	}
	kit = []int{bareFireRune, bareAirRune, bareLawRune, bareGames}
	for _, id := range kit {
		stacks = append(stacks, actionsim.Stack{ID: int32(id), Count: 1})
	}
	quest = questplan.GoblinDiplomacy.ToolIDs[:8]
	for _, id := range quest {
		stacks = append(stacks, actionsim.Stack{ID: int32(id), Count: 1})
	}
	sim = actionsim.New(actionsim.Objs{}, stacks)
	sim.OpenBank(nil)
	atBank = new(int)
	isKit := map[string]bool{}
	for _, id := range kit {
		isKit[fmt.Sprintf("obj %d", id)] = true
	}
	d := &Deps{
		Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: func(name string) bool { return isKit[name] },
		AtBank: func(b *actions.Bank, w actions.Waiter) {
			*atBank++
			_ = b.QuickWithdrawX(bareGames, 1, w) // the rub item comes straight back out of the bank
		},
	}
	e = &Engine{D: d, quest: Quest{Module: questplan.GoblinDiplomacy}}
	return e, sim, atBank, kit, quest
}

// gobdipFullSnapshot is the module's view of that pack: past the pantry, no free slot.
func gobdipFullSnapshot(e *Engine) questplan.Snapshot {
	return questplan.Snapshot{
		Journal: questplan.StatusInProgress, Stage: questplan.GobdipStarted, StageKnown: true,
		InvIDs: PackCounts(e.D), FreeSlots: FreeSlots(e.D),
		Ran: map[string]bool{"provision:" + questplan.KitGobdipPantry: true}, Failed: map[string]bool{},
	}
}

func TestAFullPackIsBankedAllButTheQuestsItems(t *testing.T) {
	e, sim, atBank, kit, quest := gobdipFullPack(t)
	snap := gobdipFullSnapshot(e)
	if snap.FreeSlots != 0 {
		t.Fatalf("the rig has %d free slots, want a full pack", snap.FreeSlots)
	}
	step := questplan.DecideGoblinDiplomacy(snap)
	if step.Kind != questplan.StepDeposit || !step.Bare {
		t.Fatalf("a full pack decided %s (bare %v), want a bare deposit", step.Describe(), step.Bare)
	}
	if !e.Execute(step) {
		t.Fatal("the deposit step failed")
	}
	if got := FreeSlots(e.D); got < 10 {
		t.Errorf("%d free slots after the deposit (pack %v), want at least 10", got, PackCounts(e.D))
	}
	pack := PackCounts(e.D)
	for _, id := range quest {
		if pack[id] == 0 {
			t.Errorf("the quest's item %d was banked", id)
		}
	}
	for _, id := range append(append([]int(nil), kit...), questplan.GoblinDiplomacy.Sustain.FoodIDs...) {
		if pack[id] != 0 {
			t.Errorf("object %d is still in the pack after a bare deposit: %v", id, pack)
		}
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s) behind a bare deposit, want none (it would draw the rub item straight back)", *atBank)
	}
	banked := 0
	for _, r := range sim.Bank() {
		banked += r.Count
	}
	if banked != 20 {
		t.Errorf("the bank holds %d items, want the 16 food and 4 kit items", banked)
	}
	// The next decision moves on: room, not another deposit.
	if again := questplan.DecideGoblinDiplomacy(gobdipFullSnapshot(e)); again.Kind == questplan.StepDeposit {
		t.Errorf("after the deposit the module still decides %s", again.Describe())
	}
}

// The control, the live bug: the old step (an ordinary deposit keeping every food id and the tools) takes nothing out of that
// pack. If this stops failing the test above proves nothing.
func TestControlTheOldKeepListBankedNothingFromAFullPack(t *testing.T) {
	e, _, _, _, _ := gobdipFullPack(t)
	old := questplan.Step{Kind: questplan.StepDeposit, Bank: questplan.DraynorBank, BankSet: true}
	old.KeepIDs = append(append([]int(nil), questplan.GoblinDiplomacy.ToolIDs...), questplan.GoblinDiplomacy.Sustain.FoodIDs...)
	if !e.Execute(old) {
		t.Fatal("the old deposit step failed")
	}
	if got := FreeSlots(e.D); got > 1 {
		t.Errorf("the old keep list freed %d slots; the live bug was that it freed none", got)
	}
}
