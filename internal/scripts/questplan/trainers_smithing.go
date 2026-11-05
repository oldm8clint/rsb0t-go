package questplan

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func init() {
	// questmods.SmithingRung: daggers from bronze, iron (15) and steel (30) bars at an anvil beside a bank. It trains to 34
	// for the nails (2 per steel bar, Smithing 34); Crest's 40 and Legends' 50 are beyond it (lane T2).
	RegisterTrainer(TrainerSpec{
		ID: "anvil-daggers", Skill: "smithing", From: 1, To: 34, Risk: R0, Via: ViaRung, Family: "smithing",
		Rungs: map[int]string{34: "smithing34"},
		Sites: []TrainerSite{
			// Seers' three anvils (questmods.Anvils) and Varrock west's.
			{Key: "seers-anvils", Name: "Seers' Village anvils", Tile: tile.Tile{X: 2713, Z: 3492}, Capacity: 3},
			{Key: "varrock-west-anvils", Name: "Varrock west anvils", Tile: VarrockAnvil, Capacity: 3},
		},
		Inputs: []TrainerInput{
			{Name: "Hammer", IDs: []int{ObjHammer}, Qty: 1, Tool: true},
			{Name: "Bronze, iron or steel bars", IDs: []int{ObjBronzeBar, ObjIronBar, ObjSteelBar}, Qty: 28},
		},
		// An anvil strike every four ticks at 25 XP an iron dagger is above 20,000 an hour with the banking left out; 12,000 is a guess
		// with it in. The plan's own sizing: 1 to 20 is 4,470 XP, about 180 iron daggers, under an hour.
		EstXPPerHour: 12000, MinStint: 20 * time.Minute, MaxStint: 90 * time.Minute,
		Source: "estimate: 180 iron daggers for Smithing 20 in under an hour (plan 3.5 Smithing); unmeasured",
		Note:   "the mule holds the bars (about 14k iron); an anvil takes one account at a time, so Capacity is the anvil count",
	})
}
