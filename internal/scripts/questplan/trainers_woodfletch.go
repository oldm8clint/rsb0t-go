package questplan

import "time"

func init() {
	// questmods.WoodcuttingRung: normal trees, oaks from 15 and willows from 30 at the Seers camps, banked at Seers. It stops
	// at 40 for the lyre; the 50s for Legends are lane T9.
	RegisterTrainer(TrainerSpec{
		ID: "seers-chop", Skill: "woodcutting", From: 1, To: 40, Risk: R0, Via: ViaRung, Family: "woodcutting",
		Rungs:      map[int]string{40: "woodcutting40"},
		Sites:      []TrainerSite{{Key: "seers-wood-camps", Name: "Seers' Village tree camps", Tile: SeersBank, Capacity: 20}},
		Inputs:     []TrainerInput{{Name: "Axe", IDs: Axes, Qty: 1, Tool: true}},
		Byproducts: []TrainerByproduct{{Name: "Logs", ID: ObjLogs, PerHour: 600}},
		// 37,224 XP in LadderFacts woodcutting40's 120 minutes.
		EstXPPerHour: 18600, MinStint: 20 * time.Minute, MaxStint: 120 * time.Minute,
		Source: "estimate: LadderFacts woodcutting40 EstMinutes 120 for 37,224 xp; unmeasured",
		Note:   "the logs feed the Fletching rung and the fleet",
	})
	// questmods.FletchingRung: logs cut with a knife at the Seers bank, to 25 for the lyre (Fremennik).
	RegisterTrainer(TrainerSpec{
		ID: "seers-fletch", Skill: "fletching", From: 1, To: 25, Risk: R0, Via: ViaRung, Family: "fletching",
		Rungs: map[int]string{25: "fletching25"},
		Sites: []TrainerSite{{Key: "seers-bank-fletch", Name: "Seers' Village bank", Tile: SeersBank, Capacity: 30}},
		Inputs: []TrainerInput{
			{Name: "Knife", IDs: []int{ObjKnife}, Qty: 1, Tool: true},
			{Name: "Logs", IDs: []int{ObjLogs, 1521, 1519}, Qty: 26},
		},
		// 7,842 XP (Fletching 25) in LadderFacts fletching25's 45 minutes.
		EstXPPerHour: 10400, MinStint: 20 * time.Minute, MaxStint: 60 * time.Minute,
		Source: "estimate: LadderFacts fletching25 EstMinutes 45 for 7,842 xp; unmeasured",
		Note:   "the logs-or-knife park was fixed on 16 Oct 2026",
	})
}
