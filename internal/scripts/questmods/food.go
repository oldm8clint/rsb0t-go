package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/waterfallplan"
)

// thieveCake is the provisioner's own-skill food source: cakes from the East Ardougne market's
// Baker's stall (Thieving 5), on ProgressiveThiever's stall leg (thieverrunner.StealStall), so a
// quest's food top-up tries the account's own skill before the baker's shop, as the item rule
// orders. The stall also gives bread and chocolate slices; all of it is food and all of it is
// kept. It stops at the task's cakes, a full pack or five minutes, and fails (so the kit falls
// back) on a guard attacking or half hitpoints.
func thieveCake(d *questrunner.Deps, task questplan.SourceTask) bool {
	m, ok := thieverplan.MethodByLabel("Baker's stall")
	if !ok {
		logf(d, "no Baker's stall method")
		return false
	}
	td := &thieverrunner.Deps{Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: thieverrunner.Delayer(d.Delay), Log: d.Log, Loot: d.Loot}
	if !questrunner.Travel(d, m.Anchor, 4) {
		return false
	}
	cakes := func() int { return questrunner.HeldID(d, waterfallplan.FoodCake) }
	start := cakes()
	want := task.Qty
	deadline := time.Now().Add(5 * time.Minute)
	for cakes() < want && time.Now().Before(deadline) && questrunner.FreeSlots(d) > 0 {
		// The market guards answer a seen theft, and lane R measured them killing low-hitpoint
		// accounts at this stall: a guard on the account, or half hitpoints, ends the source as
		// FAILED (so the kit falls back to the mule or the baker) and walks off to the bank.
		if npc, hit := questrunner.Attacker(d); hit {
			// Walking off does not shake a guard (it followed the twelfth EMPTY lab run to the
			// baker next door): fight it off, then give the source up.
			logf(d, "%s is on the account at the cake stall: fighting it off, then giving the stall up", npc.Name)
			questrunner.FightBack(d, npc, questplan.Sustain{FoodIDs: waterfallplan.Foods, EatAt: 10})
			return false
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 && hp*2 < max {
			logf(d, "half hitpoints at the cake stall: leaving for the bank")
			questrunner.Travel(d, waterfallplan.ArdougneBank, 3)
			return false
		}
		// One stand for the whole run: StealStall walks to the stand it is given and returns
		// "walked", so a stand that changes every call (the first cut passed the loop index)
		// only ever walks between the two, as the eleventh EMPTY lab run did for five minutes.
		r := thieverrunner.StealStall(td, m, 0)
		switch r.Reason {
		case "unreachable":
			return cakes() > start
		case "no-stall":
			d.Delay(time.Second)
		}
	}
	return cakes() > start
}
