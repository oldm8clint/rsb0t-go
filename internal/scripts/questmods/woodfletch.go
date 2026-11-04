package questmods

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Woodcutting and Fletching training rungs for the Fremennik Trials' lyre (Olaf's swaying tree
// branch and the lyre itself), both worked from the Seers bank.

const (
	objKnife      = 946
	objOakLogs    = 1521
	objWillowLogs = 1519
)

// wcTier is the best woodcutplan tier a level can cut below Maple (the rung stops at 40).
func wcTier(level int) woodcutplan.Tier {
	best := woodcutplan.TIERS[0]
	for _, t := range woodcutplan.TIERS {
		if t.Level <= level && t.Level <= 30 {
			best = t
		}
	}
	return best
}

// WoodcuttingRung chops at the Seers camps (normal trees, oaks from 15, willows from 30) until
// level, banking the logs at Seers (they feed the Fletching rung and the fleet).
func WoodcuttingRung(id string, level int) questrunner.Quest {
	const custom = "wc-chop"
	decide := func(s questplan.Snapshot) questplan.Step {
		if s.Level("woodcutting") >= level {
			return questplan.Step{Kind: questplan.StepDone}
		}
		if !questplan.AxeHeld(s) {
			if s.Failed["provision:wc-axe"] {
				return questplan.Step{Kind: questplan.StepWait, Reason: "no axe from the bank, the mule or Aemad"}
			}
			return questplan.Step{Kind: questplan.StepProvision, KitName: "wc-axe", Bank: questplan.SeersBank, BankSet: true,
				KeepIDs: append(append([]int{}, questplan.Axes...), questplan.CoinsID),
				Kit:     []questplan.KitLine{questplan.AxeLine(s)}, Reason: "an axe for Woodcutting"}
		}
		t := wcTier(s.Level("woodcutting"))
		return questplan.Step{Kind: questplan.StepCustom, Custom: custom, Reason: fmt.Sprintf("%s logs to Woodcutting %d", t.Label, level)}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("Woodcutting %d", level)},
			Anchor: questplan.SeersBank, Decide: decide},
		Customs: map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
			return chopTrip(e.Deps(), level)
		}},
		Done: func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())["woodcutting"] >= level },
	}
}

func packLogs(d *questrunner.Deps) int {
	return questrunner.HeldID(d, questplan.ObjLogs) + questrunner.HeldID(d, objOakLogs) + questrunner.HeldID(d, objWillowLogs)
}

// bankAllBut empties the pack at the nearest bank except the ids keep names.
func bankAllBut(d *questrunner.Deps, keep ...int) bool {
	bank := questplan.NearestBank(questrunner.CurrentTile(d))
	b, ok := questrunner.OpenBankAt(d, bank.Tile)
	if !ok {
		return false
	}
	questrunner.DepositExcept(d, b, func(id int) bool {
		for _, k := range keep {
			if id == k {
				return true
			}
		}
		return false
	})
	_ = b.Close()
	return true
}

// chopTrip fills the pack at the level's camp (or reaches the level) and banks the logs.
func chopTrip(d *questrunner.Deps, target int) bool {
	wc := func() int { return questrunner.BaseLevels(d)["woodcutting"] }
	keep := append(append([]int{}, questplan.Axes...), questplan.CoinsID)
	if questrunner.FreeSlots(d) < 10 && !bankAllBut(d, keep...) {
		return false
	}
	t := wcTier(wc())
	if !questrunner.Travel(d, t.Anchor, 4) {
		return false
	}
	start := packLogs(d)
	deadline := time.Now().Add(8 * time.Minute)
	for questrunner.FreeSlots(d) > 0 && wc() < target && time.Now().Before(deadline) {
		if wcTier(wc()).Loc != t.Loc {
			break // a new tier: bank and move camps
		}
		m, ok := questrunner.FindLoc(d, 0, t.Loc, "Chop down", t.Anchor, t.Leash+4)
		if !ok {
			d.Delay(2 * time.Second)
			continue
		}
		before := packLogs(d)
		chopStart := before
		packBefore := questrunner.LootCounts(d)
		if err := questrunner.OperateLoc(d, m, "Chop down"); err != nil {
			logf(d, "chop %s: %v", t.Loc, err)
			d.Delay(time.Second)
			continue
		}
		// An oak or willow keeps giving logs until it falls; a normal tree gives one. Wait while
		// logs keep arriving.
		for waitUntil(d, 12*time.Second, func() bool { return packLogs(d) > before }) {
			before = packLogs(d)
			if questrunner.FreeSlots(d) == 0 {
				break
			}
		}
		// The logs this tree gave, onto the Loot tab's Woodcutting card (owner, 7 Oct).
		if n := packLogs(d) - chopStart; n > 0 {
			questrunner.ReportGathered(d, loot.Woodcutting(d.Config, m.ID, t.Loc), n, packBefore,
				questplan.ObjLogs, objOakLogs, objWillowLogs)
		}
	}
	got := packLogs(d) - start
	bankAllBut(d, keep...)
	return got > 0 || wc() >= target
}

// fletchTier is the best knife-on-logs product a Fletching level can make, from content's
// fletch_bow_table: arrow shafts (7.5 xp a log) below 10, then the longbow (10), the oak
// shortbow (16.5) from 20 and the oak longbow (25) from 25.
func fletchTier(level int) (logID int, product string) {
	switch {
	case level >= 25:
		return objOakLogs, "Oak longbow"
	case level >= 20:
		return objOakLogs, "Oak shortbow"
	case level >= 10:
		return questplan.ObjLogs, "Longbow"
	}
	return questplan.ObjLogs, "Arrow shaft"
}

// fletchLogsPerTrip is the most logs a Fletching trip asks for. It is a ceiling (questplan.KitLine.Min): an
// account whose run staller's kit rides in the pack takes what fits. Twenty leaves a pack its knife, an axe, the
// staller's five and slack for whatever Deps.AtBank draws back after the deposit: a pack filled with logs has no
// slot for a knife, and the rung is stuck on it.
const fletchLogsPerTrip = 20

// fletchKit is the Fletching rung's kit for a tier: a knife and the tier's logs, and when logs would have to
// be chopped, an axe too. The order of the rule is the owner's: the bank (the Woodcutting rung banked its
// logs at Seers), then the mule (it holds thousands of every log and about 190 knives), then the ground
// spawns, then the account's own Woodcutting, which fells its own logs at the Seers camp. The knife has four
// ground spawns around Seers and Catherby and no shop worth a walk.
func fletchKit(s questplan.Snapshot, logID int) []questplan.KitLine {
	knife := questplan.KitLine{ID: objKnife, Name: "Knife", Qty: 1}
	for _, at := range woodcutplan.KnifeSpawns {
		if at == woodcutplan.LumbridgeKnife { // across the map from the Seers bank
			continue
		}
		knife.Sources = append(knife.Sources, questplan.Source{Kind: questplan.SourceGround, Tile: at, RespawnTicks: 100})
	}
	logs := questplan.KitLine{ID: logID, Name: "Logs", Qty: fletchLogsPerTrip, Min: 1}
	kit := []questplan.KitLine{knife, logs}
	switch logID {
	case questplan.ObjLogs:
		kit[1].Sources = []questplan.Source{{Kind: questplan.SourceSkill, Skill: "woodcutting", Level: woodcutplan.TIERS[0].Level,
			Method: questplan.MethodChopLogs, Tile: woodcutplan.TIERS[0].Anchor, Note: "the normal trees at the Seers camp, with the kit's axe"}}
	case objOakLogs:
		kit[1].Name = "Oak logs"
		kit[1].Sources = []questplan.Source{{Kind: questplan.SourceSkill, Skill: "woodcutting", Level: woodcutplan.TIERS[1].Level,
			Method: questplan.MethodChopOak, Tile: woodcutplan.TIERS[1].Anchor, Note: "the oaks at the Seers camp, with the kit's axe"}}
	}
	if len(kit[1].Sources) > 0 && !questplan.AxeHeld(s) {
		axe := questplan.AxeLine(s)
		axe.OnlyIfShort = logID // wanted only while the logs are still to be felled
		kit = append(kit, axe)
	}
	return kit
}

// FletchingRung cuts logs with a knife until level: logs from the bank, the mule or its own trees (up to a
// pack of them a trip), the knife from the bank, the mule or a ground spawn. The products are banked.
func FletchingRung(id string, level int) questrunner.Quest {
	const custom = "fletch-logs"
	decide := func(s questplan.Snapshot) questplan.Step {
		if s.Level("fletching") >= level {
			return questplan.Step{Kind: questplan.StepDone}
		}
		logID, _ := fletchTier(s.Level("fletching"))
		if s.HeldID(objKnife) == 0 || s.HeldID(logID) == 0 {
			kit := fmt.Sprintf("fletch-%d", logID)
			if s.Failed["provision:"+kit] {
				return questplan.Step{Kind: questplan.StepWait, Reason: "no logs or no knife from the bank, the mule, the ground or its own trees"}
			}
			return questplan.Step{Kind: questplan.StepProvision, KitName: kit, Bank: questplan.SeersBank, BankSet: true,
				KeepIDs: append([]int{objKnife, logID, questplan.CoinsID}, questplan.Axes...),
				Kit:     fletchKit(s, logID), Reason: "logs and a knife for Fletching"}
		}
		return questplan.Step{Kind: questplan.StepCustom, Custom: custom, Reason: fmt.Sprintf("fletching to %d", level)}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("Fletching %d", level)},
			Anchor: questplan.SeersBank, Decide: decide},
		Customs: map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
			return fletchPack(e.Deps(), level)
		}},
		Done: func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())["fletching"] >= level },
	}
}

// fletchPack cuts every held log of the level's tier, then banks the products.
func fletchPack(d *questrunner.Deps, target int) bool {
	lvl := func() int { return questrunner.BaseLevels(d)["fletching"] }
	logID, product := fletchTier(lvl())
	start := questrunner.HeldID(d, logID)
	packBefore := questrunner.LootCounts(d)
	for tries := 0; questrunner.HeldID(d, logID) > 0 && lvl() < target && tries < 30; tries++ {
		if l, p := fletchTier(lvl()); l != logID || p != product {
			break // a better product unlocked: re-plan
		}
		if err := questrunner.UseItemOnItem(d, objKnife, logID); err != nil {
			logf(d, "knife on logs: %v", err)
			return false
		}
		if !waitUntil(d, 5*time.Second, func() bool { return questrunner.IsMakeMenu(d) }) {
			logf(d, "the knife opened no make menu")
			continue
		}
		before := questrunner.HeldID(d, logID)
		if err := actions.NewChatDialog(d.Session, d.Components).Make(d.Config, product, nil); err != nil {
			logf(d, "make %s: %v", product, err)
			return false
		}
		// The largest fixed count is ten; each log takes three ticks. Wait while logs are used.
		for waitUntil(d, 5*time.Second, func() bool { return questrunner.HeldID(d, logID) < before }) {
			before = questrunner.HeldID(d, logID)
			if before == 0 {
				break
			}
		}
	}
	used := start - questrunner.HeldID(d, logID)
	// What the knife made, before the products are banked: onto the Loot tab's Fletching card, one
	// event a log cut, as BankFletcher's own batches are (owner, 7 Oct).
	questrunner.ReportMade(d, loot.Fletching(d.Config, logID, "Logs"), used, packBefore, logID, objKnife)
	bankAllBut(d, objKnife, questplan.CoinsID)
	return used > 0 || lvl() >= target
}
