package questplan

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Lumbridge cows (questmods.CowsRung): Hitpoints and the melee styles through the kills, Prayer through the
// burying. One rung family, a row for each skill it is asked to raise. Prayer is in trainers_prayer.go.

// cowField is the Lumbridge cow field east of the river (questmods.CowField).
var cowField = tile.Tile{X: 3259, Z: 3270}

func init() {
	cows := TrainerSite{Key: "lumbridge-cows", Name: "Lumbridge cow field (east of the river)", Tile: cowField, Capacity: 20}
	// Cow: 8 hit points, about twelve seconds a kill with the bones: 300 kills an hour at 4 xp a point of damage to the
	// style and 1.33 to Hitpoints. Unmeasured: the stint ledger (lane N6) replaces these.
	RegisterTrainer(TrainerSpec{
		ID: "cows-hitpoints", Skill: "hitpoints", From: 1, To: 30, Risk: R1, Via: ViaRung, Family: "cows",
		Rungs: map[int]string{20: "cows20"}, Rank: 5,
		Sites:        []TrainerSite{cows},
		EstXPPerHour: 3200, MinStint: 20 * time.Minute, MaxStint: 90 * time.Minute,
		Source: "estimate: 300 kills an hour x 8 damage x 1.33 xp; unmeasured",
		Note:   "ranked behind the Stage 5 combat loop, which already trains Hitpoints for the Queen's floor",
	})
	RegisterTrainer(TrainerSpec{
		ID: "cows-attack", Skill: "attack", From: 1, To: 30, Risk: R1, Via: ViaRung, Family: "cows",
		Rungs: map[int]string{20: "attack20"}, Rank: 5,
		Sites:        []TrainerSite{cows},
		EstXPPerHour: 9600, MinStint: 20 * time.Minute, MaxStint: 90 * time.Minute,
		Source: "estimate: 300 kills an hour x 8 damage x 4 xp; unmeasured",
		Note:   "normally never inserted: Waterfall's 13,750 Attack XP clears Attack 20 first (LadderFacts attack20)",
	})
}
