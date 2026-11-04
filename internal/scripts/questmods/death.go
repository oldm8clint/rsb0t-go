package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// DeathPlateau is Death Plateau ready for the engine.
func DeathPlateau() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.DeathPlateau,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomDeathHaroldTalk: deathHaroldTalk,
			questplan.CustomDeathAle:        deathAle,
			questplan.CustomDeathDice:       deathDice,
			questplan.CustomDeathReclaim:    deathReclaim,
			questplan.CustomDeathReadIOU:    deathReadIOU,
			questplan.CustomDeathBalls:      deathBalls,
			questplan.CustomDeathSaba:       deathSaba,
			questplan.CustomDeathTenzing:    deathTenzing,
			questplan.CustomDeathScout:      deathScout,
		},
	}
}

var haroldStop = questplan.NpcStop{Npc: "Harold", ID: questplan.NpcHarold, Anchor: questplan.HaroldAnchor, Leash: 5}

// enterHaroldRoom knocks on Harold's door from the door's own tile (clicked from z 3544 the
// door's check_axis takes the leaving branch) and waits for "Come in!" and the teleport inside.
func enterHaroldRoom(d *questrunner.Deps) bool {
	if questplan.InHaroldRoom(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.HaroldDoorStand, 0) && !questplan.InHaroldRoom(questrunner.CurrentTile(d)) {
		return false
	}
	if questplan.InHaroldRoom(questrunner.CurrentTile(d)) {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHaroldDoor, "", "Open", questplan.HaroldDoorStand, 2)
	if !ok {
		logf(d, "Harold's door is not in the loc index")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InHaroldRoom(questrunner.CurrentTile(d)) }, nil, 25*time.Second)
	questrunner.ClearLeftoverModal(d)
	if !ok {
		logf(d, "still outside Harold's room at %v (is he in it?)", questrunner.CurrentTile(d))
	}
	return ok
}

func deathHaroldTalk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterHaroldRoom(d) {
		return false
	}
	ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.HaroldDuty})
	e.InvalidateStage()
	return ok
}

// deathAle hands Harold the ale: at stage 40 his talk goes straight to harold_drink.
func deathAle(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterHaroldRoom(d) || !questrunner.OpenDialogue(d, haroldStop) {
		return false
	}
	gone := func() bool { return !held(d, questplan.ObjAsgarnianAle) }
	questrunner.NewDialogue(d).DriveUntil(gone, nil, 45*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return gone()
}

// ---------------------------------------------------------------------------
// The dice
// ---------------------------------------------------------------------------

func diceOpen(d *questrunner.Deps) bool {
	main, _, _ := d.Session.State().Modals()
	return main == questplan.DeathDiceMain
}

func shown(d *questrunner.Deps, com int) bool {
	hidden, ok := d.Session.State().InterfaceHidden(com)
	return ok && !hidden
}

func comboFound(d *questrunner.Deps) bool {
	return held(d, questplan.ObjDeathIOU) || held(d, questplan.ObjCombination)
}

func coins(d *questrunner.Deps) int { return questrunner.HeldID(d, questplan.CoinsID) }

// playRound is one bet: the gamble option, the count dialog, Harold's roll, the Roll Dice!
// button once its layer is shown, the verdict, and Continue. The dice interface must never be
// closed before Roll Dice! is pressed: a closed or walked-off dice window forfeits the stake.
// It reports won, and ok false when the round could not be played.
func playRound(d *questrunner.Deps, bet int) (won, ok bool) {
	if !questrunner.OpenDialogue(d, haroldStop) {
		return false, false
	}
	dl := questrunner.NewDialogue(d)
	counting := func() bool { return d.Session.State().CountDialogOpen() }
	deadline := time.Now().Add(30 * time.Second)
	for !counting() && !diceOpen(d) && time.Now().Before(deadline) {
		if dl.Ready() {
			dl.Drive([]string{"Would you like to gamble?"})
		}
		d.Delay(400 * time.Millisecond)
	}
	if counting() {
		if err := actions.AnswerCountDialog(d.Session, int32(bet)); err != nil {
			logf(d, "the bet of %d: %v", bet, err)
			return false, false
		}
	}
	// Two chatnpc pages stand between the bet and the dice. Drive only the chat: nothing here
	// may close a main modal.
	deadline = time.Now().Add(25 * time.Second)
	for !diceOpen(d) && time.Now().Before(deadline) {
		if dl.Ready() {
			dl.Drive(nil)
		}
		d.Delay(400 * time.Millisecond)
	}
	if !diceOpen(d) {
		logf(d, "a bet of %d never opened the dice", bet)
		return false, false
	}
	if !waitUntil(d, 20*time.Second, func() bool { return shown(d, questplan.DeathDiceRollLyr) }) {
		logf(d, "the Roll Dice! layer never showed")
		return false, false
	}
	for attempt := 0; attempt < 3 && shown(d, questplan.DeathDiceRollLyr); attempt++ {
		_ = actions.IfButton(d.Session, questplan.DeathDiceRoll)
		waitUntil(d, 3*time.Second, func() bool { return !shown(d, questplan.DeathDiceRollLyr) })
	}
	if !waitUntil(d, 20*time.Second, func() bool { return shown(d, questplan.DeathDiceContLyr) }) {
		logf(d, "the roll landed but no verdict came")
		return false, false
	}
	verdict := strings.ToLower(d.Session.State().Text(questplan.DeathDiceVerdict))
	won = strings.Contains(verdict, "win")
	_ = actions.ResumePauseButton(d.Session, questplan.DeathDiceContinue)
	waitUntil(d, 5*time.Second, func() bool { return !diceOpen(d) })
	// haroldgamble_end speaks after the window closes: the payout, or the IOU.
	for i := 0; i < 3; i++ {
		if !waitUntil(d, 2500*time.Millisecond, dl.Ready) {
			break
		}
		if len(dl.OptionTexts()) > 0 {
			break
		}
		dl.Drive(nil)
	}
	questrunner.ClearLeftoverModal(d)
	return won, true
}

// deathDice stakes purse+1 each round (Harold's purse starts at 100 and grows by every loss)
// until a win he cannot pay makes him write the IOU.
func deathDice(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if comboFound(d) {
		return true
	}
	if !enterHaroldRoom(d) {
		return false
	}
	purse := questplan.HaroldPurseStart
	for round := 1; round <= 25 && !comboFound(d); round++ {
		bet := questplan.NextDeathStake(purse, coins(d))
		if bet <= 0 {
			logf(d, "out of coins for the dice after %d rounds", round-1)
			break
		}
		before := coins(d)
		logf(d, "dice round %d: staking %d against a purse read as %d", round, bet, purse)
		won, ok := playRound(d, bet)
		if diceOpen(d) {
			_ = actions.CloseModal(d.Session)
		}
		logf(d, "dice round %d: %s, coins %d -> %d", round, map[bool]string{true: "won", false: "lost"}[won], before, coins(d))
		if !ok || comboFound(d) {
			break
		}
		purse = questplan.DeathPurseAfter(purse, bet, won)
		if !questplan.InHaroldRoom(questrunner.CurrentTile(d)) && !enterHaroldRoom(d) {
			break
		}
	}
	e.InvalidateStage()
	return comboFound(d)
}

// deathReclaim asks Harold for the paper again: past stage 55 with neither paper owned, his talk
// re-issues it by itself. "Owned" is content's obj_gettotal, the pack, the bank and what is worn,
// so a paper in the bank is never re-issued (the questplan decision fetches it from there first).
func deathReclaim(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterHaroldRoom(d) {
		return false
	}
	if !questrunner.OpenDialogue(d, haroldStop) {
		logf(d, "Harold's talk did not open (is he in his room?)")
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return comboFound(d) }, []string{"I've lost the IOU you gave me."}, 30*time.Second)
	questrunner.ClearLeftoverModal(d)
	if comboFound(d) {
		return true
	}
	logf(d, "Harold's talk ended with neither the IOU nor the combination in the pack (%d free slots): he re-issues one only while the pack, the bank and what is worn hold none, and into a pack with a free slot",
		questrunner.FreeSlots(d))
	return false
}

// deathReadIOU reads the IOU, which turns it into the combination (stage 60).
func deathReadIOU(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if err := questrunner.OpHeldByName(d, questplan.ObjDeathIOU, "Read"); err != nil {
		logf(d, "reading the IOU: %v", err)
		return false
	}
	ok := questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjCombination) }, nil, 20*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// ---------------------------------------------------------------------------
// The stone balls
// ---------------------------------------------------------------------------

func ballAt(d *questrunner.Deps, id int, at tile.Tile) bool {
	for _, g := range questrunner.GroundItemsNear(d, id, at, 0) {
		if g.X == at.X && g.Z == at.Z {
			return true
		}
	}
	return false
}

// deathBalls places the five balls on their mechanism tiles. The unlock is checked only when a
// ball is placed at stage 60, so a layout already complete from before is re-placed once.
func deathBalls(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, tile.Tile{X: 2894, Z: 3563}, 3) {
		return false
	}
	mark := time.Now()
	unlocked := func() bool {
		return d.Chat != nil && strings.Contains(strings.ToLower(d.Chat.Since(mark)), "equipment room door has unlocked")
	}
	allRight := func() bool {
		for _, b := range questplan.StoneBalls {
			if !ballAt(d, b.ID, b.Target) {
				return false
			}
		}
		return true
	}
	if allRight() {
		last := questplan.StoneBalls[len(questplan.StoneBalls)-1]
		logf(d, "the layout is already right: lifting the %s ball to place it again at stage 60", last.Colour)
		questrunner.TakeGround(d, last.ID, last.Target, 0)
	}
	for _, b := range questplan.StoneBalls {
		if ballAt(d, b.ID, b.Target) {
			continue
		}
		// A wrong ball on the tile must be lifted first ("There is already a stone ball here.").
		for _, other := range questplan.StoneBalls {
			if other.ID != b.ID && ballAt(d, other.ID, b.Target) {
				questrunner.TakeGround(d, other.ID, b.Target, 0)
			}
		}
		if !held(d, b.ID) {
			src := b.Spawn
			if gs := questrunner.GroundItemsNear(d, b.ID, b.Target, 16); len(gs) > 0 && len(questrunner.GroundItemsNear(d, b.ID, b.Spawn, 0)) == 0 {
				src = tile.Tile{X: gs[0].X, Z: gs[0].Z}
			}
			if !waitUntil(d, 12*time.Second, func() bool { return len(questrunner.GroundItemsNear(d, b.ID, src, 0)) > 0 }) ||
				!questrunner.TakeGround(d, b.ID, src, 0) {
				logf(d, "the %s stone ball is not to be had", b.Colour)
				return false
			}
		}
		m, ok := questrunner.FindLoc(d, 0, "Stone Mechanism", "", b.Target, 0)
		if !ok {
			logf(d, "no Stone Mechanism at %d,%d", b.Target.X, b.Target.Z)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, b.ID, m); err != nil {
			logf(d, "the %s ball on the mechanism: %v", b.Colour, err)
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return ballAt(d, b.ID, b.Target) }) {
			logf(d, "the %s ball did not land on %d,%d", b.Colour, b.Target.X, b.Target.Z)
			return false
		}
	}
	waitUntil(d, 4*time.Second, unlocked)
	e.InvalidateStage()
	return allRight()
}

// ---------------------------------------------------------------------------
// The map track
// ---------------------------------------------------------------------------

func leaveSabaCave(d *questrunner.Deps) bool {
	if !questplan.InSabaCave(questrunner.CurrentTile(d)) {
		return true
	}
	questrunner.WalkNear(d, questplan.SabaExit, 2)
	m, ok := questrunner.FindLoc(d, questplan.LocSabaExit, "", "Exit", questplan.SabaExit, 8)
	if !ok {
		logf(d, "no Cave Exit in Saba's cave")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Exit"); err != nil {
		return false
	}
	return waitUntil(d, 10*time.Second, func() bool { return !questplan.InSabaCave(questrunner.CurrentTile(d)) })
}

// deathSaba goes into Saba's cave, asks him about another way up (map 0), and comes out.
func deathSaba(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InSabaCave(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.SabaEntrance, 2) {
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocSabaEntrance, "", "Enter", tile.Tile{X: 2857, Z: 3578}, 4)
		if !ok {
			logf(d, "no Cave Entrance near Saba")
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Enter"); err != nil {
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questplan.InSabaCave(questrunner.CurrentTile(d)) }) {
			return false
		}
		if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.SabaPath}) {
			return false
		}
	}
	e.InvalidateStage()
	return leaveSabaCave(d)
}

// tenzingDoor gets the account into Tenzing's house by his front door: a knock, "No milk
// today!", then the door opens.
func tenzingDoor(d *questrunner.Deps) bool {
	inside := func() bool {
		t := questrunner.CurrentTile(d)
		return t.Level == 0 && t.X >= 2815 && t.X <= 2822 && t.Z >= 3553 && t.Z <= 3557
	}
	if inside() {
		return true
	}
	if !leaveSabaCave(d) || !questrunner.Travel(d, questplan.TenzingDoor, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocTenzingDoor, "", "Open", tile.Tile{X: 2822, Z: 3555}, 2)
	if !ok {
		logf(d, "Tenzing's front door is not in the loc index")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(inside, []string{"I'm not the milkman", "I need your help"}, 20*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// deathTenzing talks to Tenzing inside his house: the first visit (the boots), the supplies
// hand-in (the map), and any re-issue. A talk that ends is not a talk that gave what the step came
// for: Tenzing re-issues his boots or the map only while none is in the pack, the bank or worn, and
// otherwise says what he says to anyone. The step names the item it came for (ExpectItemID), and
// the talk counts only when the pack holds it, so the decision can tell a re-issue that did not
// come from one that did.
func deathTenzing(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	if !tenzingDoor(d) {
		return false
	}
	ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.TenzingHelp})
	e.InvalidateStage()
	if want := step.ExpectItemID; want != 0 {
		// The inventory update can trail the dialogue's last page by a tick.
		if waitUntil(d, 3*time.Second, func() bool { return held(d, want) }) {
			return true
		}
		if !ok {
			logf(d, "the talk with Tenzing did not complete")
		}
		logf(d, "Tenzing's talk ended without the %s in the pack (%d free slots): he gives it only while the pack, the bank and what is worn hold none",
			questrunner.ObjName(d, want), questrunner.FreeSlots(d))
		return false
	}
	return ok
}

// deathScout leaves Tenzing's house by the back door (it opens from inside at map 7), climbs the
// stile and walks the secret way to the zone that sets map 8.
func deathScout(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !tenzingDoor(d) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocTenzingBack, "", "Open", tile.Tile{X: 2820, Z: 3557}, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err == nil {
			waitUntil(d, 8*time.Second, func() bool { return questrunner.CurrentTile(d).Z >= 3558 })
		}
	}
	if questrunner.CurrentTile(d).Z <= 3562 {
		if m, ok := questrunner.FindLoc(d, questplan.LocStile, "", "Climb-over", questplan.StileTile, 6); ok {
			if err := questrunner.OperateLoc(d, m, "Climb-over"); err == nil {
				waitUntil(d, 10*time.Second, func() bool { return questrunner.CurrentTile(d).Z > questplan.StileTile.Z })
			}
		}
	}
	if !questrunner.Travel(d, questplan.ScoutTile, 3) {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return false }, nil, 5*time.Second)
	e.InvalidateStage()
	return true
}
