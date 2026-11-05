package questplan

import "time"

func init() {
	// questmods.CowsRung with skill prayer: the bones loop (the pack's bones, the bank's, an ask of the mule), the
	// cows when no bone is found. prayer31 and prayer43 are GATE rungs (Scorpion Catcher, Horror from the Deep);
	// prayerkq trains to whatever the Kalphite Queen's protection prayer needs (kqPrayerRungLevel). Past 45 is lane T7.
	RegisterTrainer(TrainerSpec{
		ID: "prayer-bones", Skill: "prayer", From: 1, To: 45, Risk: R1, Via: ViaRung, Family: "cows",
		Rungs: map[int]string{31: "prayer31", 43: "prayer43"}, RungsAtSetting: []string{KQPrayerRungID},
		Sites:  []TrainerSite{{Key: "lumbridge-cows", Name: "Lumbridge cow field (east of the river)", Tile: cowField, Capacity: 20}},
		Inputs: []TrainerInput{{Name: "Bones or big bones", IDs: []int{ObjBones, ObjBigBones}, Qty: 28, Gathers: true}},
		// The measured 500 to 830 XP an hour of a prayer rung; 540 is the figure the plan prices Prayer 37 to 43 at.
		EstXPPerHour: 540, MeasuredXPPerHour: 540,
		MinStint: 30 * time.Minute, MaxStint: 120 * time.Minute,
		Source: "measured: 500-830 xp/h on the live fleet, plan 3.5 prices 22.9k xp (37 to 43) at 540 xp/h; cow bones alone were 175 xp/h (16 Oct 2026)",
		Note:   "R1 and not in the default allow list (R0 trainers only): Prayer is the slowest need, about 42 account-hours from 37 to 43",
	})
}
