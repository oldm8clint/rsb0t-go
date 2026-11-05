package questplan

import "time"

func init() {
	// The Gnome Stronghold course: questmods.GnomeAgility, one rung family for agility25 and agility35 (the same
	// laps, a different target). It trains to 35 and no further here: the Barbarian course itself needs 35 and the
	// Outpost course past it is lane T8 (a gap, SkillGaps).
	RegisterTrainer(TrainerSpec{
		ID: "gnome-course", Skill: "agility", From: 1, To: 35, Risk: R0, Via: ViaRung, Family: "gnome",
		Rungs: map[int]string{25: "agility25", 35: "agility35"},
		Sites: []TrainerSite{{Key: "gnome-stronghold-course", Name: "Gnome Stronghold agility course", Tile: GnomeStart, Capacity: 30}},
		// 86.5 XP a lap at 47 seconds a lap is 6,625 an hour, measured; LadderFacts' 60 minutes for 7,842 XP (Agility 25) is the estimate.
		EstXPPerHour: 7800, MeasuredXPPerHour: 6600,
		MinStint: 20 * time.Minute, MaxStint: 120 * time.Minute,
		Source: "measured: 86.5 xp a lap, 47 s a lap (laneq/studies/agility25.md, plan 3.5); estimate: LadderFacts agility25 EstMinutes 60",
		Note:   "ends on the ground, never mid-lap (gnomeBudget 15 minutes a run); capacity is a provisional cap",
	})
}
