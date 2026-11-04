package questplan

import "testing"

// pip-empty-3: an EMPTY account must buy a pickaxe before it can mine the essence (Rune Mysteries
// done: it is the mining kit).
func TestPipEssenceKitBuysAPickaxe(t *testing.T) {
	p := PlanProvision(pipEssenceKit(true), ProvisionState{Bank: map[int]int{CoinsID: 3000}, BankKnown: true, MuleKnown: true,
		FreeSlots: 20, Levels: map[string]int{"mining": 1}})
	if p.Phase == PhaseBlocked || len(p.Tasks) != 2 || p.Tasks[0].Source.Kind != SourceShop ||
		p.Tasks[0].Source.Shop.NpcID != BobsAxes.NpcID || p.Tasks[1].Source.Method != MethodMineEssence {
		t.Fatalf("plan %s", p.Summary())
	}
}
