package questmods

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/crafterrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// CraftingSpin is a training rung: spinning to a Crafting target on ProgressiveCrafter's proven
// legs (crafterrunner). Below Crafting 10 it shears the Lumbridge sheep and spins the wool at the
// castle wheel (flax needs 10); from 10 it spins flax at the Seers wheel, the owner's
// "spinning the mule's flax at Seers": banked flax first, then the field. Balls of wool are dropped
// (worthless); bow strings are banked. id is the ladder's rung id (crafting18, crafting40).
func CraftingSpin(id string, target int) questrunner.Quest {
	custom := "craft-spin"
	decide := func(s questplan.Snapshot) questplan.Step {
		if s.Level("crafting") >= target {
			return questplan.Step{Kind: questplan.StepDone}
		}
		return questplan.Step{Kind: questplan.StepCustom, Custom: custom, Reason: fmt.Sprintf("spinning to Crafting %d", target)}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("Crafting %d", target)},
			Anchor: questplan.SeersBank, Decide: decide},
		Customs: map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
			return spinTo(e.Deps(), target, 15*time.Minute)
		}},
		Done: func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())["crafting"] >= target },
	}
}

func crafterDeps(d *questrunner.Deps) *crafterrunner.Deps {
	return &crafterrunner.Deps{Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: crafterrunner.Delayer(d.Delay), Log: d.Log, Walker: d.Walker}
}

// spinTo runs spin trips until the target or the budget.
func spinTo(d *questrunner.Deps, target int, budget time.Duration) bool {
	cd := crafterDeps(d)
	crafting := func() int { return questrunner.BaseLevels(d)["crafting"] }
	// A ladder account arrives carrying a quest's leftovers: a spin trip wants the pack.
	if questrunner.FreeSlots(d) < 20 {
		bank := questplan.NearestBank(questrunner.CurrentTile(d))
		if b, ok := questrunner.OpenBankAt(d, bank.Tile); ok {
			questrunner.DepositExcept(d, b, func(id int) bool { return id == questplan.CoinsID || id == 1735 })
			_ = b.Close()
		}
	}
	deadline := time.Now().Add(budget)
	for crafting() < target && time.Now().Before(deadline) {
		var ok bool
		if crafting() < 10 {
			ok = woolTrip(d, cd)
		} else {
			ok = flaxTrip(d, cd)
		}
		if !ok {
			return false
		}
	}
	return crafting() >= target
}

// woolTrip shears a pack of wool in the Lumbridge pen and spins it at the castle wheel.
func woolTrip(d *questrunner.Deps, cd *crafterrunner.Deps) bool {
	crafterrunner.Drop(cd, []string{"Ball of wool"})
	if crafterrunner.Count(cd, "Shears") == 0 && !crafterrunner.GrabGroundShears(cd) {
		if cd.Log != nil {
			cd.Log("no shears, and none at Fred's farm")
		}
		return false
	}
	if !crafterrunner.GoToPen(cd) {
		return false
	}
	skip := map[int]time.Time{}
	for i := 0; i < 60 && !crafterrunner.PackFull(cd); i++ {
		switch crafterrunner.Shear(cd, skip) {
		case crafterrunner.ShearNoShears:
			return false
		case crafterrunner.ShearNoSheep:
			cd.Delay(2 * time.Second)
		}
	}
	crafterrunner.LeavePen(cd)
	spun := crafterrunner.SpinWool(cd, freshstart.LumbridgeWheel)
	// Onto the Loot tab's Crafting card, as ProgressiveCrafter's own spinning is (owner, 7 Oct).
	questrunner.ReportSpun(d, false, spun)
	crafterrunner.Drop(cd, []string{"Ball of wool"})
	return spun > 0
}

// flaxTrip draws flax from the Seers bank (the field when the bank has none), spins it at the
// wheel and banks the bow strings.
func flaxTrip(d *questrunner.Deps, cd *crafterrunner.Deps) bool {
	if crafterrunner.FlaxCount(cd) == 0 {
		got, _, ok := crafterrunner.DrawFlax(cd)
		if !ok {
			return false
		}
		if got == 0 {
			if !crafterrunner.GoToField(cd) {
				return false
			}
			crafterrunner.PickFlaxUntil(cd, 27, 5*time.Minute)
		}
	}
	if crafterrunner.FlaxCount(cd) == 0 {
		return false
	}
	if !crafterrunner.GoToWheel(cd) {
		return false
	}
	spun := crafterrunner.SpinFlaxUntil(cd, 28, 5*time.Minute)
	questrunner.ReportSpun(d, true, spun)
	if !crafterrunner.GoToBank(cd) {
		return spun > 0
	}
	crafterrunner.BankAll(cd, crafterplan.SeersKeep())
	return spun > 0
}
