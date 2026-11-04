package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// CooksAssistant is Cook's Assistant ready for the engine.
func CooksAssistant() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.CooksAssistant,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomCookMilk: cookMilk,
			questplan.CustomCookMill: cookMill,
		},
	}
}

// saw reports whether any needle (lower case) is in the game messages since mark. A deps without a chat log sees nothing.
func cookSaw(d *questrunner.Deps, mark time.Time, needles ...string) bool {
	if d.Chat == nil {
		return false
	}
	text := strings.ToLower(d.Chat.Since(mark))
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// cookMilkBudget bounds one milking run.
const cookMilkBudget = 4 * time.Minute

// cookMilk milks a dairy cow with the empty bucket in the pack ([opnpcu,cow] with a bucket_empty: "You milk the cow.", no
// delay, no dialogue). A cow the use-on cannot reach (a fence between, "I can't reach that!") is set aside and the next
// is tried.
func cookMilk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjBucket) == 0 {
		logf(d, "cook: no empty bucket to milk with")
		return false
	}
	if !questrunner.Travel(d, questplan.DairyField, 6) {
		return false
	}
	refused := map[int]bool{}
	deadline := time.Now().Add(cookMilkBudget)
	for time.Now().Before(deadline) && questrunner.HeldID(d, questplan.ObjBucketOfMilk) == 0 {
		cow, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(20).
			Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcDairyCow && !refused[n.Index] }).Nearest()
		if !ok {
			// None in the scene, or every one refused: walk back to the middle of the field and give them time to wander.
			questrunner.Travel(d, questplan.DairyField, 4)
			refused = map[int]bool{}
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		if err := questrunner.UseItemOnNpc(d, questplan.ObjBucket, cow); err != nil {
			logf(d, "cook: the bucket on the cow at %d,%d: %v", cow.X, cow.Z, err)
			refused[cow.Index] = true
			continue
		}
		if waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjBucketOfMilk) > 0 }) {
			logf(d, "cook: milked the cow at %d,%d", cow.X, cow.Z)
			return true
		}
		refused[cow.Index] = true
	}
	return questrunner.HeldID(d, questplan.ObjBucketOfMilk) > 0
}

// cookMill makes a pot of flour at the Lumbridge windmill from the empty pot and the grain in the pack: the grain into the
// hopper on the top floor, the lever beside it, then the flour bin on the ground floor into the pot (windmills.rs2). The
// hopper and the bin are per player (%hopper_full and %flour are perm varps), so a bin another account loaded is not this
// account's flour and an interrupted run is picked up where it stopped: grain already in the hopper is ground, flour
// already in the bin is emptied.
func cookMill(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjEmptyPot) == 0 {
		logf(d, "cook: no empty pot for the flour")
		return false
	}
	if questrunner.HeldID(d, questplan.ObjGrain) > 0 && !millGrind(d) {
		logf(d, "cook: the mill did not take the grain (the bin may still hold flour from an earlier run)")
	}
	return millEmptyBin(d)
}

// millGrind puts the grain in the hopper and pulls the lever. It reports whether the grain went down the chute.
func millGrind(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.MillTop, 1) {
		return false
	}
	if at := questrunner.CurrentTile(d); at.Level != questplan.MillTop.Level {
		logf(d, "cook: the mill's top floor was not reached (standing on level %d)", at.Level)
		return false
	}
	hopper, ok := questrunner.FindLoc(d, questplan.LocMillHopper, "", "", questplan.MillTop, 6)
	if !ok {
		logf(d, "cook: no hopper on the mill's top floor")
		return false
	}
	mark := time.Now()
	if err := questrunner.UseItemOnLoc(d, questplan.ObjGrain, hopper); err != nil {
		logf(d, "cook: the grain on the hopper: %v", err)
		return false
	}
	// The grain leaves the pack before the walk to the hopper ends, so the closing message is what says it is in.
	if !waitUntil(d, 20*time.Second, func() bool {
		return cookSaw(d, mark, "put the grain in the hopper", "already grain in the hopper") ||
			(d.Chat == nil && questrunner.HeldID(d, questplan.ObjGrain) == 0)
	}) {
		logf(d, "cook: the hopper never answered the grain")
		return false
	}
	lever, ok := questrunner.FindLoc(d, questplan.LocHopperContr, "", "Operate", questplan.MillTop, 6)
	if !ok {
		logf(d, "cook: no hopper controls on the mill's top floor")
		return false
	}
	mark = time.Now()
	if err := questrunner.OperateLoc(d, lever, "Operate"); err != nil {
		logf(d, "cook: the hopper controls: %v", err)
		return false
	}
	const slid, full, empty = "slides down the chute", "bin downstairs is full", "nothing interesting happens"
	if !waitUntil(d, 20*time.Second, func() bool {
		return cookSaw(d, mark, slid, full, empty) || (d.Chat == nil && questrunner.HeldID(d, questplan.ObjGrain) == 0)
	}) {
		logf(d, "cook: the hopper controls never answered")
		return false
	}
	if cookSaw(d, mark, empty) && !cookSaw(d, mark, slid) {
		logf(d, "cook: the hopper was empty when the lever was pulled")
		return false
	}
	logf(d, "cook: the grain went down the chute")
	return true
}

// millEmptyBin goes down to the flour bin and empties it into the pot.
func millEmptyBin(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.MillBin, 2) {
		return false
	}
	bin, ok := questrunner.FindLoc(d, 0, "Flour bin", "Empty", questplan.MillBin, 6)
	if !ok {
		logf(d, "cook: no flour bin at the mill's foot")
		return false
	}
	if err := questrunner.OperateLoc(d, bin, "Empty"); err != nil {
		logf(d, "cook: the flour bin: %v", err)
		return false
	}
	if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjPotOfFlour) > 0 }) {
		logf(d, "cook: the flour bin gave no flour")
		return false
	}
	logf(d, "cook: a pot of flour from the mill")
	return true
}
