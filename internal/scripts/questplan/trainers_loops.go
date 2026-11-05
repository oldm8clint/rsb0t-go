package questplan

// The loops the fleet already runs for an account, as catalogue rows, so that a need they train is neither a gap
// nor a stint: the planner reports it as covered by the loop and waits (plan 3.5, "Thieving, Magic, melee,
// Hitpoints"). Their Rank is 0, ahead of any rung that would duplicate them (the cows rows are Rank 5), and
// their rates are shown for an ETA only: the loop owns the pace, and the needs engine never schedules it.

func init() {
	// The Stage 5 CombatTrainer: Attack, Strength and Defence to the melee goal and Hitpoints with them, up to the
	// trainer's own ladder ceiling of 75 (scarabplan.Stage5LadderCeiling).
	for _, skill := range []string{"attack", "strength", "defence", "hitpoints"} {
		rate := 9000
		if skill == "hitpoints" {
			rate = 3000
		}
		RegisterTrainer(TrainerSpec{
			ID: "stage5-" + skill, Skill: skill, From: 1, To: 75, Risk: R1, Via: ViaLoop, Loop: "stage5-combat",
			EstXPPerHour: rate,
			Source:       "estimate for an ETA only: the Stage 5 combat trainer owns the pace (scarabplan/stage5trainer.go)",
			Note:         "melee goal 40, Defence and Hitpoints 50 for the Queen's floor (kqGate); no Strength floor exists",
		})
	}
	// The magic slices: Magic to the Queen's floor of 66.
	RegisterTrainer(TrainerSpec{
		ID: "stage5-magic", Skill: "magic", From: 1, To: 66, Risk: R0, Via: ViaLoop, Loop: "stage5-magic",
		EstXPPerHour: 3800,
		Source:       "estimate: 22.0M xp fleet-wide at about 0.56M xp/h over the 149 accounts short (plan 0 and 3.3)",
		Note:         "Stage 5's magic slices; every account below 66 is already in them",
	})
	// Thieving: the Heroes and Paladin stalls and pockets (Stages 3 to 5).
	RegisterTrainer(TrainerSpec{
		ID: "stage5-thieving", Skill: "thieving", From: 1, To: 99, Risk: R0, Via: ViaLoop, Loop: "stage5-thieving",
		EstXPPerHour: 30000,
		Source:       "estimate for an ETA only: every account is at 55 or more already (plan 1.1)",
		Note:         "no built quest needs more than 25",
	})
	// Self-cook: the mule's raw fish cooked by the account itself, for the 22 accounts below Cooking 30 (plan 3.5).
	RegisterTrainer(TrainerSpec{
		ID: "scarab-selfcook", Skill: "cooking", From: 1, To: 60, Risk: R0, Via: ViaLoop, Loop: "scarab-selfcook",
		EstXPPerHour: 125000, MeasuredXPPerHour: 125000,
		Source: "measured about 125k xp/h on the self-cook session (plan 3.5 Cooking)",
		Note:   "needs the mule's raw fish; Hero's Quest wants 53",
	})
}
