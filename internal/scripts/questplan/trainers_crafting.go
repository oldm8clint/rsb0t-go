package questplan

import "time"

func init() {
	// questmods.CraftingSpin: wool at the Lumbridge castle wheel below Crafting 10 (flax needs 10), flax at the Seers
	// wheel from 10. One rung family for crafting18 (Nature Spirit's silver sickle) and crafting40 (Olaf's lyre).
	RegisterTrainer(TrainerSpec{
		ID: "crafting-spin", Skill: "crafting", From: 1, To: 40, Risk: R0, Via: ViaRung, Family: "crafting",
		Rungs: map[int]string{18: "crafting18", 40: "crafting40"},
		Sites: []TrainerSite{
			{Key: "seers-flax-wheel", Name: "Seers' Village spinning wheel", Tile: SeersBank, Capacity: 12},
			{Key: "lumbridge-wool-wheel", Name: "Lumbridge castle spinning wheel", Capacity: 12},
		},
		Inputs:     []TrainerInput{{Name: "Flax or wool", IDs: []int{1779, 1737}, Qty: 28, Gathers: true}},
		Byproducts: []TrainerByproduct{{Name: "Bow string", ID: 1777, PerHour: 1200}},
		// 37,224 XP (Crafting 40) in LadderFacts crafting40's 190 minutes is 11,755 an hour: an estimate that averages the slow wool stretch.
		EstXPPerHour: 11700,
		MinStint:     20 * time.Minute, MaxStint: 120 * time.Minute,
		Source: "estimate: LadderFacts crafting40 EstMinutes 190 for 37,224 xp; unmeasured",
		Note:   "the bow strings are banked and feed Fletching; capacities are provisional caps",
	})
}
