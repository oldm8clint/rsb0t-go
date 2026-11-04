package questmods

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// PrinceAli is Prince Ali Rescue ready for the engine. Most of its steps are generic ones (the talks to Hassan, Osman, Ned and
// Aggie, the dye on the wig, the water on the clay, picking onions) or the provisioner's (the shops, the spawns); the
// executors here are the ones a plain talk cannot be: the conversations whose scripts raise a message box in the middle (Osman and
// Leela's key, Joe's beers, the prince), the three visits to the jail compound, which top the account up first (Keli's print, Joe's
// beers, the break-out), the clay, and the fire for the ashes. The wool is the Sheep Shearer
// module's own shear-and-spin, on the legs the Crafting rung proved live.
func PrinceAli() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.PrinceAli,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomSheepWool:      sheepWool,
			questplan.CustomPrinceKey:      princeKey,
			questplan.CustomPrincePrint:    princePrint,
			questplan.CustomPrinceAshes:    princeAshes,
			questplan.CustomPrinceClay:     princeClay,
			questplan.CustomPrinceRope:     princeBuy,
			questplan.CustomPrinceBeerBuy:  princeBuy,
			questplan.CustomPrinceBeer:     princeBeer,
			questplan.CustomPrinceBreakout: princeBreakout,
			questplan.CustomPrinceLeave:    princeLeave,
		},
	}
}

const (
	// princeTalkBudget bounds one conversation, the page turns and the message boxes in it.
	princeTalkBudget = 90 * time.Second
	// princeQuiet is how long the chat and the main modal must both stay shut for a conversation to count as over: the longest
	// pause in the scripts is a p_delay(3), and the driver's own lull is 1.8 s.
	princeQuiet = 2 * time.Second
	// princeKeliWait bounds the wait for Lady Keli to be there to tie (stage 40) or to be gone (the door): she returns a hundred ticks
	// after another account ties her.
	princeKeliWait = 150 * time.Second
	// princePrinceWait bounds the wait for Prince Ali to be in his cell: another account's rescue removes him for a spell.
	princePrinceWait = 150 * time.Second
	// princeKeliRadius is how far from the account Keli is looked for: the yard's width and the door's ten tiles.
	princeKeliRadius = 14
	// princeBuyTalks bounds the talks of a purchase by dialogue (a rope or a beer is one talk each).
	princeBuyTalks = 6
	// princeFireBudget bounds the fire: the logs burn 100 to 200 ticks, the ashes lie 100 more.
	princeFireBudget = 6 * time.Minute
	// princeClayBudget bounds the clay mining.
	princeClayBudget = 4 * time.Minute
)

// princeConverse walks to the stop, opens its conversation and drives it to its end: pages are continued and menus answered
// from the stop's preferences by questrunner's Dialogue, and every message box a script raises in the middle (a ~mesbox sits on
// the main modal, which Dialogue.Drive does not see, so a plain talk step returns at the first one and leaves the rest of the
// conversation half done) is closed so the script goes on. It returns true when done (nil for none) holds once the chat and the
// main modal have stayed shut for princeQuiet. Joe's three message boxes and Osman's, Ned's and Leela's are why this exists.
func princeConverse(d *questrunner.Deps, stop questplan.NpcStop, done func() bool, budget time.Duration) bool {
	if !questrunner.GotoNpc(d, stop, nil) {
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	var quiet time.Time
	for time.Now().Before(deadline) {
		if dl.Ready() {
			quiet = time.Time{}
			if !dl.Drive(stop.Prefer) {
				break // no option to choose, or the page budget ran out: what is open is closed below
			}
			continue
		}
		if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
			quiet = time.Time{}
			questrunner.ClearLeftoverModal(d)
			continue
		}
		if quiet.IsZero() {
			quiet = time.Now()
		}
		if time.Since(quiet) >= princeQuiet {
			break
		}
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
	}
	questrunner.ClearLeftoverModal(d)
	return done == nil || done()
}

// princeKey has Osman forge the key from the print and a bronze bar (step.Qty above 0; his menu ends "Thank you. I will try to
// find the other items.") and Leela hand it over. Osman is asked even when his answer is no (keystatus is not transmitted, so a
// print that stays in the pack proves the key was forged already), and Leela is asked either way: she gives the key when
// keystatus is 1, and a lost one for 15 coins when it is 2 and neither the pack nor the bank holds one (the pantry drew a banked
// one first). Leela's conversation holds a message box, and the key arrives after it.
func princeKey(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.ObjPrinceKey) {
		return true
	}
	if step.Qty > 0 {
		if !princeConverse(d, questplan.OsmanForgeStop, func() bool { return !held(d, questplan.ObjKeyPrint) }, princeTalkBudget) {
			logf(d, "prince: Osman took no print (the key may be forged already); asking Leela all the same")
		}
	}
	princeConverse(d, questplan.LeelaStop, func() bool { return held(d, questplan.ObjPrinceKey) }, princeTalkBudget)
	got := held(d, questplan.ObjPrinceKey)
	logf(d, "prince: the key %s", map[bool]string{true: "is in the pack", false: "did not arrive"}[got])
	return got
}

// princePrint takes the print of Lady Keli's key: the account tops itself up to the fight's starting hitpoints first (the same
// top-up as Joe's and the break-out's, because the visit is eighteen pages and five menus inside the reach of a level-26 guard and
// nothing can be eaten while the chat is open), then one conversation walks her menus to "Could I touch the key for a moment?",
// which swaps a soft clay in the pack for the print. The conversation holds two message boxes, which princeConverse closes.
func princePrint(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	sus := questplan.PrinceAli.Sustain
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeJailGuard.PeakHit())
	ok := princeConverse(d, questplan.KeliPrintStop, func() bool { return held(d, questplan.ObjKeyPrint) }, princeTalkBudget)
	logf(d, "prince: Keli's print: %v (the pack holds %d)", ok, questrunner.HeldID(d, questplan.ObjKeyPrint))
	return ok
}

// princeBuy buys by dialogue until the pack holds step.Qty of step.ItemID: Ned's rope (15 coins a talk) and the Rusty Anchor's beer
// (2). Each talk is one purchase; two talks in a row that buy nothing end it (no coins, no stock, a menu that does not offer it).
func princeBuy(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	start := questrunner.HeldID(d, step.ItemID)
	fails := 0
	for i := 0; i < princeBuyTalks && questrunner.HeldID(d, step.ItemID) < step.Qty && fails < 2; i++ {
		before := questrunner.HeldID(d, step.ItemID)
		princeConverse(d, step.Stop, func() bool { return questrunner.HeldID(d, step.ItemID) > before }, 45*time.Second)
		if questrunner.HeldID(d, step.ItemID) > before {
			fails = 0
		} else {
			fails++
		}
	}
	got := questrunner.HeldID(d, step.ItemID) - start
	logf(d, "prince: bought %d of %d %s from %s", got, step.Qty, questrunner.ObjName(d, step.ItemID), step.Stop.Npc)
	return got > 0
}

// princeBeer gives Joe the three beers in one conversation: he drinks the first, and with two more in the pack takes both and
// is drunk (stage 40). The account tops itself up to the fight's starting hitpoints first, because eating is not possible with
// the chat open and the two guards that reach him hit through it.
func princeBeer(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	sus := questplan.PrinceAli.Sustain
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeJailGuard.PeakHit())
	ok := princeConverse(d, questplan.JoeBeerStop, func() bool { return !held(d, questplan.ObjBeer) }, princeTalkBudget)
	logf(d, "prince: Joe's beers: %v (the pack holds %d)", ok, questrunner.HeldID(d, questplan.ObjBeer))
	return ok
}

// princeClay mines one clay at the Rimmington rocks (either rock type: 2108 or 2109 is placed there) with the pickaxe the pack or
// the worn items hold.
func princeClay(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	want := max(step.Qty, 1)
	start := questrunner.HeldID(d, questplan.ObjClay)
	deadline := time.Now().Add(princeClayBudget)
	for _, rock := range []int{questplan.LocClayRockA, questplan.LocClayRockB} {
		if questrunner.HeldID(d, questplan.ObjClay)-start >= want || !time.Now().Before(deadline) {
			break
		}
		questrunner.ExecuteStep(d, questplan.Step{Kind: questplan.StepMineRock, LocID: rock, Loc: "Rocks",
			ItemID: questplan.ObjClay, Item: "Clay", Qty: want, Anchor: questplan.ClayRocks}, nil)
	}
	got := questrunner.HeldID(d, questplan.ObjClay) - start
	logf(d, "prince: mined %d of %d clay", got, want)
	return got > 0
}

// princeAshes lights the logs and takes the ashes the fire leaves. The tinderbox goes on the logs (opheldu on the logs is
// cut_logs.rs2, which calls light_logs_inv); the script drops the logs where the account stands, retries the strike by itself,
// pushes the account aside when the fire catches and, 100 to 200 ticks later, replaces the fire with the ashes, which lie a
// hundred ticks for anyone (firemaking.rs2 firemaking_success). The account stays by the fire and takes them.
func princeAshes(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.ObjAshes) {
		return true
	}
	if !held(d, questplan.ObjLogs) || !held(d, questplan.ObjTinderbox) {
		logf(d, "prince: no logs or no tinderbox to burn")
		return false
	}
	if !questrunner.Travel(d, questplan.PrinceFireSite, 1) {
		return false
	}
	if err := questrunner.UseItemOnItem(d, questplan.ObjTinderbox, questplan.ObjLogs); err != nil {
		logf(d, "prince: the tinderbox on the logs: %v", err)
		return false
	}
	if !waitUntil(d, 45*time.Second, func() bool { return !held(d, questplan.ObjLogs) }) {
		logf(d, "prince: the logs never caught")
		return false
	}
	logf(d, "prince: the fire is lit; waiting for the ashes (up to %s)", princeFireBudget)
	deadline := time.Now().Add(princeFireBudget)
	for time.Now().Before(deadline) && !held(d, questplan.ObjAshes) {
		if questrunner.TakeGround(d, questplan.ObjAshes, questrunner.CurrentTile(d), 6) {
			break
		}
		if !questrunner.WaitForGuardian(d) || !d.Delay(2*time.Second) {
			break
		}
	}
	got := held(d, questplan.ObjAshes)
	logf(d, "prince: ashes in the pack: %v", got)
	return got
}

// princeInCell reports whether the account stands on the cell's floor.
func princeInCell(d *questrunner.Deps) bool {
	t, ok := questrunner.CurrentTileOK(d)
	return ok && questplan.InPrinceCell(t)
}

// princeTieKeli uses a rope on Lady Keli: the script answers with a message box, takes the rope and removes her for a hundred
// ticks (stage 50). tied is true when the journal already says she was tied; she is then only waited for if she is back, and
// not being there is the point. Before the tie, at stage 40, her absence means another account tied her a moment ago and she
// is waited for.
func princeTieKeli(d *questrunner.Deps, tied bool) bool {
	deadline := time.Now().Add(princeKeliWait)
	// The yard first: she is out of view from the road, and the walker opens the yard door.
	if !questrunner.Travel(d, questplan.KeliAnchor, 3) {
		return false
	}
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		keli, ok := questrunner.FindNpcByID(d, questplan.NpcLadyKeli, "Lady Keli", princeKeliRadius)
		if !ok {
			if tied {
				return true
			}
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		if !held(d, questplan.ObjRope) {
			logf(d, "prince: Lady Keli is here and there is no rope left to tie her with")
			return false
		}
		if err := questrunner.UseItemOnNpc(d, questplan.ObjRope, keli); err != nil {
			logf(d, "prince: the rope on Lady Keli: %v", err)
			if !d.Delay(2 * time.Second) {
				return false
			}
			continue
		}
		gone := waitUntil(d, 12*time.Second, func() bool {
			questrunner.ClearLeftoverModal(d) // "You overpower Keli, tie her up, and put her in a cupboard." is a message box
			_, there := questrunner.FindNpcByID(d, questplan.NpcLadyKeli, "Lady Keli", princeKeliRadius)
			return !there
		})
		if gone {
			logf(d, "prince: Lady Keli is tied up")
			return true
		}
		tied = false
	}
	logf(d, "prince: Lady Keli could not be tied inside %s", princeKeliWait)
	return false
}

// princeUnlock walks to the Prison Door's north side and uses the key on it. The script teleports the account onto the door's
// tile, inside the cell, and shuts the door behind it; it refuses with Keli within ten tiles ("You'd better get rid of Lady Keli"),
// which reports keliBack so the break-out ties her again.
func princeUnlock(d *questrunner.Deps) (inside, keliBack bool) {
	if princeInCell(d) {
		return true, false
	}
	// The stand itself; one tile off it will do (Joe wanders into the yard, and the key is sent from a stand ApproachLoc walks to).
	if !questrunner.Travel(d, questplan.PrisonDoorStand, 0) && !questrunner.Travel(d, questplan.PrisonDoorStand, 1) {
		return false, false
	}
	// Looked for from the stand: the door's rule is Keli within ten tiles of the account.
	if _, there := questrunner.FindNpcByID(d, questplan.NpcLadyKeli, "Lady Keli", 10); there {
		return false, true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocPrisonDoor, "Prison Door", "Open", questplan.PrisonDoorTile, 4)
	if !ok {
		logf(d, "prince: no Prison Door near %d,%d", questplan.PrisonDoorTile.X, questplan.PrisonDoorTile.Z)
		return false, false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjPrinceKey, m); err != nil {
		logf(d, "prince: the key on the Prison Door: %v", err)
		return false, false
	}
	if waitUntil(d, 8*time.Second, func() bool { return princeInCell(d) }) {
		logf(d, "prince: the Prison Door is unlocked; inside the cell")
		return true, false
	}
	_, there := questrunner.FindNpcByID(d, questplan.NpcLadyKeli, "Lady Keli", 10)
	logf(d, "prince: the key did not open the door (Lady Keli within ten tiles: %v)", there)
	return false, there
}

// princeBreakout is stages 40 and 50 to 100: top up, rope on Keli, the key on the door, the disguise and the key to the prince, out
// of the cell. step.Qty is 1 once the journal says Keli is tied. The loop ties her again if she is back when the door is tried.
func princeBreakout(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	sus := questplan.PrinceAli.Sustain
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeJailGuard.PeakHit())
	tied := step.Qty > 0
	inside := princeInCell(d)
	for attempt := 0; attempt < 3 && !inside; attempt++ {
		if !princeTieKeli(d, tied) {
			return false
		}
		tied = true
		var keliBack bool
		if inside, keliBack = princeUnlock(d); !inside && !keliBack {
			return false
		}
	}
	if !inside {
		return false
	}
	if !waitUntil(d, princePrinceWait, func() bool {
		_, there := questrunner.FindNpcByID(d, questplan.NpcPrinceAliPrison, "Prince Ali", 8)
		return there
	}) {
		logf(d, "prince: Prince Ali is not in his cell")
		return false
	}
	handed := princeConverse(d, questplan.PrinceAliStop, func() bool { return !held(d, questplan.ObjBlondWig) }, princeTalkBudget)
	logf(d, "prince: the disguise and the key handed over: %v", handed)
	if handed {
		princeLeaveCell(d)
	}
	return handed
}

// princeLeaveCell opens the Prison Door from inside (oploc1 from z <= 3243 opens it and puts the account on the north side) and
// reports whether the account is out. The walker's graph does not know the door.
func princeLeaveCell(d *questrunner.Deps) bool {
	for attempt := 0; attempt < 3 && princeInCell(d); attempt++ {
		m, ok := questrunner.FindLoc(d, questplan.LocPrisonDoor, "Prison Door", "Open", questplan.PrisonDoorTile, 4)
		if !ok {
			logf(d, "prince: no Prison Door to open from inside")
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "prince: opening the Prison Door from inside: %v", err)
			continue
		}
		waitUntil(d, 6*time.Second, func() bool { return !princeInCell(d) })
	}
	return !princeInCell(d)
}

// princeLeave is the stage-100 step of an account still in the cell.
func princeLeave(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return princeLeaveCell(d)
}
