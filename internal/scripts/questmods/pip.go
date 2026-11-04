package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PriestInPeril is Priest in Peril ready for the engine.
func PriestInPeril() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.PriestInPeril,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomPipKnock:     pipKnock,
			questplan.CustomPipDog:       pipDog,
			questplan.CustomPipMonk:      pipMonk,
			questplan.CustomPipCellTalk:  pipCellTalk,
			questplan.CustomPipMausoleum: pipMausoleum,
			questplan.CustomPipUnlock:    pipUnlock,
			questplan.CustomPipBless:     pipBless,
			questplan.CustomPipCoffin:    pipCoffin,
			questplan.CustomPipEssence:   pipEssence,
		},
	}
}

// pipKnock knocks at the temple doors from outside and agrees to help (stage 1 to 2).
func pipKnock(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.TempleDoorOutside, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, 0, "Large door", "Knock-at", questplan.TempleDoorOutside, 3)
	if !ok {
		logf(d, "no temple door to knock at")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Knock-at"); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 6*time.Second, dl.Ready)
	dl.Drive([]string{"Roald sent me to check on Drezel.", "Sure."})
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// pipDown goes down the mausoleum trapdoor north of the temple (opening it first).
func pipDown(d *questrunner.Deps) bool {
	if questplan.IsUnderground(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.PipTrapdoorStand, 1) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocPipTrapdoor, "", "Open", questplan.PipTrapdoorStand, 3); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		d.Delay(1200 * time.Millisecond)
	}
	m, ok := questrunner.FindLoc(d, 0, "Trapdoor", "Climb-down", questplan.PipTrapdoorStand, 3)
	if !ok {
		logf(d, "no open trapdoor to climb down")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return questplan.IsUnderground(questrunner.CurrentTile(d)) })
}

// pipDog kills the Temple guardian (stage 2 only; melee, it is immune to magic).
func pipDog(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !pipDown(d) {
		return false
	}
	if _, ok := questrunner.FindNpcByID(d, questplan.NpcTempleGuardian, "", 12); !ok {
		logf(d, "no temple guardian (respawning after another account's kill?)")
		d.Delay(10 * time.Second)
		return false
	}
	questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcTempleGuardian,
		Target: "Temple guardian", Anchor: questplan.TempleGuardianTile}, questplan.PriestInPeril.Sustain)
	e.InvalidateStage()
	return true
}

// pipTopUpMissing is how many missing hitpoints the monk fight tops up before it starts: a lobster
// heals 12, so ten missing is the least that does not waste most of a bite.
const pipTopUpMissing = 10

// pipTopUp eats while pipTopUpMissing or more hitpoints are missing and food remains, so the fight
// starts near full. The usual fight costs 12 to 21 hitpoints live (about 23 by questplan's ceiling
// for an account with no weapon), so an account that arrives hurt is the one that has to eat in the
// middle of it, and every failed fight of 5 Oct 2026 was such an account.
func pipTopUp(d *questrunner.Deps, foods []int) {
	for i := 0; i < 6; i++ {
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || max-hp < pipTopUpMissing {
			return
		}
		if !questrunner.EatIfNeeded(d, foods, hp) {
			return
		}
	}
}

// pipMonk kills a Monk of Zamorak 1046 in the temple and takes the golden key it drops. The fight is
// questrunner's melee loop (attack again after every bite, walk away from a lost fight; see
// questplan/fightwatch.go for why), started near full hitpoints, against a 1046 that is not already
// fighting another account (the temple is a single-combat area and other accounts come for the same
// key).
func pipMonk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	sus := questplan.PriestInPeril.Sustain
	if !questrunner.Travel(d, questplan.TempleMonks, 4) {
		return false
	}
	if _, ok := questrunner.FindNpcByID(d, questplan.NpcZamorakKeyMonk, "", 16); !ok {
		logf(d, "no key monk in view")
		d.Delay(5 * time.Second)
		return false
	}
	pipTopUp(d, sus.FoodIDs)
	won := questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcZamorakKeyMonk,
		Target: "Monk of Zamorak", Anchor: questplan.TempleMonks}, sus)
	tries := 10
	if !won {
		tries = 1 // a fight given up, or timed out, has no key to wait for
	}
	for i := 0; i < tries && questrunner.HeldID(d, questplan.ObjGoldenKey) == 0; i++ {
		if questrunner.TakeGround(d, questplan.ObjGoldenKey, questrunner.CurrentTile(d), 4) {
			break
		}
		d.Delay(500 * time.Millisecond)
	}
	return questrunner.HeldID(d, questplan.ObjGoldenKey) > 0
}

// pipCellTalk talks to Drezel through the cell door (stage 4 to 5).
func pipCellTalk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.CellDoorTile, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCellDoor, "", "Talk-through", questplan.CellDoorTile, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Talk-through"); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 6*time.Second, dl.Ready)
	dl.Drive([]string{"Tell me anyway.", "Yes."})
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// pipMausoleum tries the golden key on each monument until one swaps it for the iron key (never
// Take-from, which damages), and fills the bucket at the well.
func pipMausoleum(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !pipDown(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.PipMonumentsCentre, 4) {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjIronKey) == 0 && questrunner.HeldID(d, questplan.ObjGoldenKey) > 0 {
		for _, id := range questplan.PipMonuments {
			m, ok := questrunner.FindLoc(d, id, "", "", questplan.PipMonumentsCentre, 12)
			if !ok {
				continue
			}
			if err := questrunner.UseItemOnLoc(d, questplan.ObjGoldenKey, m); err != nil {
				continue
			}
			if waitUntil(d, 3*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjIronKey) > 0 }) {
				break
			}
			questrunner.ClearLeftoverModal(d)
		}
	}
	if questrunner.HeldID(d, questplan.ObjBucket) > 0 && questrunner.HeldID(d, questplan.ObjMurkyWater) == 0 &&
		questrunner.HeldID(d, questplan.ObjBlessedWater) == 0 {
		if m, ok := questrunner.FindLoc(d, questplan.LocPipWell, "", "", questplan.PipWellTile, 6); ok {
			_ = questrunner.UseItemOnLoc(d, questplan.ObjBucket, m)
			waitUntil(d, 4*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjMurkyWater) > 0 })
		}
	}
	return questrunner.HeldID(d, questplan.ObjIronKey) > 0 || questrunner.HeldID(d, questplan.ObjMurkyWater) > 0
}

// pipUnlock uses the iron key on the cell door (stage 5 to 6).
func pipUnlock(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.CellDoorTile, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCellDoor, "", "", questplan.CellDoorTile, 3)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjIronKey, m); err != nil {
		return false
	}
	d.Delay(2 * time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// intoCell opens the cell door (allowed at stage 6 and on) and steps inside.
func intoCell(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if here.Level == 2 && here.X >= 3416 {
		return true
	}
	if !questrunner.Travel(d, questplan.CellDoorTile, 1) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocCellDoor, "", "Open", questplan.CellDoorTile, 3); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		d.Delay(1200 * time.Millisecond)
	}
	return questrunner.WalkNear(d, questplan.CellInside, 1)
}

// pipBless has Drezel bless the murky water (a use-on, no stage check), or at stage 7 just talks
// to him.
func pipBless(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !intoCell(d) {
		return false
	}
	drezel, ok := questrunner.FindNpcByID(d, questplan.NpcTrappedDrezel, "", 8)
	if !ok {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjMurkyWater) > 0 {
		if err := questrunner.UseItemOnNpc(d, questplan.ObjMurkyWater, drezel); err != nil {
			return false
		}
	} else {
		stop := questplan.NpcStop{Npc: "Drezel", ID: questplan.NpcTrappedDrezel, Anchor: questplan.CellInside, Leash: 8, Prefer: []string{"Yes."}}
		if !questrunner.OpenDialogue(d, stop) {
			return false
		}
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 4*time.Second, dl.Ready)
	dl.Drive([]string{"Yes."})
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// pipCoffin pours the blessed water on the coffin outside the cell (stage 6 to 7).
func pipCoffin(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if here := questrunner.CurrentTile(d); here.Level == 2 && here.X >= 3416 {
		questrunner.WalkNear(d, tile.Tile{X: 3414, Z: 3489, Level: 2}, 1)
	}
	if !questrunner.Travel(d, questplan.PipCoffinTile, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocPipCoffin, "", "", questplan.PipCoffinTile, 4)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjBlessedWater, m); err != nil {
		return false
	}
	d.Delay(2 * time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// pipEssence meets Drezel in the mausoleum (stage 8 to 10) and hands over the essence held.
func pipEssence(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !pipDown(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.MausoleumDrezel, 2) {
		return false
	}
	stop := questplan.NpcStop{Npc: "Drezel", ID: questplan.NpcMausoleumDrez, Anchor: questplan.MausoleumDrezel, Leash: 8, Prefer: []string{"Yes."}}
	before := questrunner.HeldID(d, questplan.ObjRuneEssence)
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	questrunner.ClearLeftoverModal(d)
	if before > 0 && questrunner.HeldID(d, questplan.ObjRuneEssence) == before {
		if drezel, ok := questrunner.FindNpcByID(d, questplan.NpcMausoleumDrez, "", 8); ok {
			_ = questrunner.UseItemOnNpc(d, questplan.ObjRuneEssence, drezel)
			dl := questrunner.NewDialogue(d)
			waitUntil(d, 4*time.Second, dl.Ready)
			dl.Drive(stop.Prefer)
			questrunner.ClearLeftoverModal(d)
		}
	}
	e.InvalidateStage()
	return true
}

// mineEssence is the provisioner's skill source for unnoted rune essence: Aubury's teleport
// (open once Rune Mysteries is done), EssenceMiner's own mining and portal legs, until the pack
// is full or the task's quantity is held.
func mineEssence(d *questrunner.Deps, task questplan.SourceTask) bool {
	ed := &essencerunner.Deps{Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: essencerunner.Delayer(d.Delay), Log: d.Log}
	start := questrunner.HeldID(d, questplan.ObjRuneEssence)
	if !essencerunner.InMine(ed) {
		// Aubury's teleport is refused until Rune Mysteries is done, and the walk to him is minutes:
		// live 14 Oct 2026, 13 refused teleports after 13 such walks. The kit no longer lists this
		// source for an account that has not done the quest; this is the same rule at the door.
		if why := questplan.MineEssenceRefusal(questrunner.ReadStatus(d, questplan.RuneMysteriesName)); why != "" {
			logf(d, "mine-essence: not walking to Aubury: %s", why)
			return false
		}
		if !questrunner.Travel(d, questplan.AuburyShop, 3) {
			return false
		}
		if res := essencerunner.TeleportIn(ed); !res.Landed {
			if res.QuestRefused {
				logf(d, "Aubury refuses the teleport: Rune Mysteries is not complete")
			}
			return false
		}
	}
	essencerunner.Mine(ed)
	essencerunner.PortalOut(ed)
	return questrunner.HeldID(d, questplan.ObjRuneEssence) > start
}
