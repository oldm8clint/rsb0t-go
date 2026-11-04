package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// TribalTotem is Tribal Totem ready for the engine. Its talks (Kangai Mau, the R.P.D.T. employee) and its provisioning are the
// engine's generic steps; the five customs below are what has no generic step: the label peeled off one crate and used on the
// other (each raised by a message box that must be continued, never closed), the Wizard's teleport (waited for), the security
// door's four-dial lock (a main-modal interface clicked one letter at a time) and the trapped stairs and the chest upstairs.
func TribalTotem() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.TribalTotem,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomTotemLabel:    totemLabel,
			questplan.CustomTotemMark:     totemMark,
			questplan.CustomTotemTeleport: totemTeleport,
			questplan.CustomTotemLock:     totemLock,
			questplan.CustomTotemSteal:    totemSteal,
		},
	}
}

const (
	// totemBoxWait is how long an op gets to raise its message box.
	totemBoxWait = 10 * time.Second
	// totemTeleportWait is how long the Wizard's teleport gets to land after his dialogue closes: a four-tick delay and the jump.
	totemTeleportWait = 14 * time.Second
	// totemDialWait is how long a click on a dial's arrow gets to change the letter the server writes into its text.
	totemDialWait = 4 * time.Second
	// totemLockWait is how long the lock's interface gets to open after the door's Open.
	totemLockWait = 8 * time.Second
	// totemConfirmWait is how long the confirm button gets to be answered in the chat.
	totemConfirmWait = 8 * time.Second
	// totemStairsWait bounds the stairs' scripts: they walk the account three tiles first.
	totemStairsWait = 20 * time.Second
	// totemChestBudget bounds the chest: the shut chest opened, the open chest searched, either one changed under the account by
	// another account's use (the loc change is the world's, for 300 ticks).
	totemChestBudget = 3 * time.Minute
)

// totemPlayDialogue waits for the box an op raised and continues it to the end (never closes it: closing a message box aborts the
// script that raised it). It reports whether a box came.
func totemPlayDialogue(d *questrunner.Deps, what string) bool {
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, totemBoxWait, dl.Ready) {
		logf(d, "totem: %s raised no message box in %s", what, totemBoxWait)
		return false
	}
	dl.Drive(nil)
	return true
}

// totemLabel peels the address label off the mansion's crate at the R.P.D.T. depot. The crate's op 2 hands it over after a message
// box, and again for as long as the pack holds none; the box has to be continued for the label to arrive.
func totemLabel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTotemLabel) > 0 {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHornCrate, "", "Investigate", questplan.DepotCrates, 8)
	if !ok {
		logf(d, "totem: no crate (loc %d) with Investigate near %v", questplan.LocHornCrate, questplan.DepotCrates)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Investigate"); err != nil {
		logf(d, "totem: investigating the mansion's crate: %v", err)
		return false
	}
	if !totemPlayDialogue(d, "the mansion's crate") {
		return false
	}
	if !waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTotemLabel) > 0 }) {
		logf(d, "totem: the crate's box was continued and no address label came")
		return false
	}
	logf(d, "totem: peeled the address label off the mansion's crate")
	return true
}

// totemMark uses the label on the Wizards' Tower crate. The script's message box comes first, then the label is taken and the
// stage moves; the player's closing line follows as a page of its own.
func totemMark(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTotemLabel) == 0 {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocTeleportCrate, "", "Investigate", questplan.DepotCrates, 8)
	if !ok {
		logf(d, "totem: no crate (loc %d) near %v", questplan.LocTeleportCrate, questplan.DepotCrates)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjTotemLabel, m); err != nil {
		logf(d, "totem: the label on the Wizards' Tower crate: %v", err)
		return false
	}
	if !totemPlayDialogue(d, "the label on the crate") {
		return false
	}
	if !waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTotemLabel) == 0 }) {
		logf(d, "totem: the label is still in the pack after the crate's box")
		return false
	}
	logf(d, "totem: covered the Wizards' Tower label with the mansion's")
	return true
}

// totemTeleport talks to the Wizard Cromperty and waits for the teleport: it lands a few ticks after his dialogue closes, and the
// engine's next look at the world must not find the account still outside and talk to him again.
func totemTeleport(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	inside := func() bool { return questplan.InTotemMansion(questrunner.CurrentTile(d)) }
	if inside() {
		return true
	}
	stop := questplan.CrompertyStop
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		logf(d, "totem: could not open a dialogue with the Wizard Cromperty from %v", questrunner.CurrentTile(d))
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	if !waitUntil(d, totemTeleportWait, inside) {
		logf(d, "totem: the Wizard's teleport did not land inside the mansion in %s (at %v)", totemTeleportWait, questrunner.CurrentTile(d))
		return false
	}
	logf(d, "totem: teleported into Lord Handelmort's mansion at %v", questrunner.CurrentTile(d))
	return true
}

// totemDialLetter reads dial i's letter off the text the server writes into it.
func totemDialLetter(d *questrunner.Deps, i int) (byte, bool) {
	text, known := d.Session.State().TextKnown(questplan.TotemDialText(i))
	if !known {
		return 0, false
	}
	return questplan.TotemDialLetter(text)
}

// totemLockShowing reports whether the lock's interface is the open main modal.
func totemLockShowing(d *questrunner.Deps) bool {
	main, _, _ := d.Session.State().Modals()
	return main == questplan.TotemLockInterface
}

// totemSetDial clicks dial i to the wanted letter, one click at a time and each click answered: the server writes the new letter
// into the dial's text (tribal_door_combo), and the next click is chosen from what the text says, not from a count, so that a click
// the server dropped cannot shift every letter after it.
func totemSetDial(d *questrunner.Deps, i int, want byte) bool {
	for clicks := 0; clicks <= questplan.TotemLockLetters+2; clicks++ {
		cur, ok := totemDialLetter(d, i)
		if !ok {
			logf(d, "totem: dial %d is not readable", i+1)
			return false
		}
		if cur == want {
			return true
		}
		if !totemLockShowing(d) {
			logf(d, "totem: the lock's interface closed under dial %d", i+1)
			return false
		}
		up, _ := questplan.TotemDialClicks(cur, want)
		com := questplan.TotemDialDown(i)
		if up {
			com = questplan.TotemDialUp(i)
		}
		if err := actions.IfButton(d.Session, com); err != nil {
			logf(d, "totem: clicking dial %d: %v", i+1, err)
			return false
		}
		if !waitUntil(d, totemDialWait, func() bool { l, ok := totemDialLetter(d, i); return ok && l != cur }) {
			logf(d, "totem: dial %d did not move off %c", i+1, cur)
			return false
		}
	}
	cur, ok := totemDialLetter(d, i)
	return ok && cur == want
}

// totemLock opens the security door, sets the four dials to KURT and confirms. The confirm button closes the interface before the
// script tests the word, so the answer is the game message that follows, not the interface. A door that opens instead of raising the
// interface is one the lock has been solved for already (the journal had not caught up), and the step is done.
func totemLock(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	here := questrunner.CurrentTile(d)
	if !questplan.InTotemMansion(here) {
		logf(d, "totem: the security door is inside the mansion, and the account is at %v", here)
		return false
	}
	for attempt := 1; attempt <= 3; attempt++ {
		m, ok := questrunner.FindLoc(d, questplan.LocComboDoor, "", "Open", questplan.ComboStand, 6)
		if !ok {
			logf(d, "totem: no security door (loc %d) near %v", questplan.LocComboDoor, questplan.ComboStand)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "totem: opening the security door: %v", err)
			return false
		}
		if !waitUntil(d, totemLockWait, func() bool { return totemLockShowing(d) }) {
			e.InvalidateStage()
			if e.StageIs(questplan.TotemUnlocked) {
				logf(d, "totem: the security door opened without a lock: the combination was solved already")
				return true
			}
			logf(d, "totem: the security door raised no lock in %s (attempt %d of 3)", totemLockWait, attempt)
			continue
		}
		for i := 0; i < len(questplan.TotemCombination); i++ {
			if !totemSetDial(d, i, questplan.TotemCombination[i]) {
				questrunner.ClearLeftoverModal(d)
				return false
			}
		}
		mark := time.Now()
		logf(d, "totem: entering %s on the security door", questplan.TotemCombination)
		if err := actions.IfButton(d.Session, questplan.TotemLockConfirm); err != nil {
			logf(d, "totem: the lock's confirm button: %v", err)
			questrunner.ClearLeftoverModal(d)
			return false
		}
		answered := waitUntil(d, totemConfirmWait, func() bool {
			return cookSaw(d, mark, "combination seems correct", "combination is incorrect")
		})
		if !answered || !cookSaw(d, mark, "combination seems correct") {
			logf(d, "totem: the security door refused the combination (answered %v)", answered)
			questrunner.ClearLeftoverModal(d)
			return false
		}
		logf(d, "totem: the security door accepted the combination")
		return true
	}
	e.RequestPark("the security door never raised its lock", totemRetryPark)
	return false
}

// totemRetryPark is how long a step that learned it cannot work now is left alone.
const totemRetryPark = 30 * time.Minute

// totemClimb notes the trap on the stairs and climbs them. The climb is made only after the box that says the trap is noted has been
// seen: the stairs drop an account that has not noted it into the Ardougne sewers, and a Thieving level under 21 notes nothing.
func totemClimb(e *questrunner.Engine, d *questrunner.Deps) bool {
	stairs, ok := questrunner.FindLoc(d, questplan.LocTrapStairs, "", "Investigate", questplan.StairsStand, 6)
	if !ok {
		logf(d, "totem: no trapped stairs (loc %d) near %v", questplan.LocTrapStairs, questplan.StairsStand)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, stairs, "Investigate"); err != nil {
		logf(d, "totem: searching the stairs for the trap: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	noted := func() bool { return strings.Contains(questplan.NormaliseLines(dl.Texts()), "trap in these stairs") }
	nothing := func() bool { return cookSaw(d, mark, "you don't find anything interesting") }
	if !waitUntil(d, totemStairsWait, func() bool { return noted() || nothing() }) {
		logf(d, "totem: the stairs said nothing about a trap in %s", totemStairsWait)
		return false
	}
	if nothing() {
		e.RequestPark("Thieving 21 is needed to find the trap on the mansion's stairs", questplan.TotemLevelPark)
		return false
	}
	dl.Drive(nil)
	logf(d, "totem: the trap on the stairs is noted")
	// Notes recorded: only now is the climb safe.
	if err := questrunner.OperateLoc(d, stairs, "Climb-up"); err != nil {
		logf(d, "totem: climbing the stairs: %v", err)
		return false
	}
	if !waitUntil(d, totemStairsWait, func() bool { return questrunner.CurrentTile(d).Level == 1 }) {
		if at := questrunner.CurrentTile(d); questplan.IsUnderground(at) {
			e.RequestPark("the stairs' trap dropped the account into the Ardougne sewers", time.Hour)
		}
		logf(d, "totem: the climb did not reach the first floor (at %v)", questrunner.CurrentTile(d))
		return false
	}
	logf(d, "totem: up the stairs to %v", questrunner.CurrentTile(d))
	return true
}

// totemChestLoc finds the chest in the state the world has it: the open chest to search, else the shut one to open. The chest is the
// world's, for 300 ticks after another account searched it.
func totemChestLoc(d *questrunner.Deps) (open, shut bool, m locindex.Match) {
	if o, ok := questrunner.FindLoc(d, questplan.LocTotemChestOpen, "", "Search", questplan.TotemChest, 6); ok {
		return true, false, o
	}
	if s, ok := questrunner.FindLoc(d, questplan.LocTotemChestShut, "", "Open", questplan.TotemChest, 6); ok {
		return false, true, s
	}
	return false, false, locindex.Match{}
}

// totemChest opens the chest and searches it for the totem. The totem comes after a message box (continued, never closed); the
// chest answers "The chest is empty." to an account whose pack or bank holds a totem already, which the step reports as failed so
// that the decision reads the bank.
func totemChest(d *questrunner.Deps) bool {
	deadline := time.Now().Add(totemChestBudget)
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if questrunner.HeldID(d, questplan.ObjTribalTotem) > 0 {
			return true
		}
		open, shut, m := totemChestLoc(d)
		switch {
		case open:
			mark := time.Now()
			if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
				logf(d, "totem: searching the chest: %v", err)
				d.Delay(2 * time.Second)
				continue
			}
			dl := questrunner.NewDialogue(d)
			empty := func() bool { return cookSaw(d, mark, "the chest is empty") }
			waitUntil(d, totemBoxWait, func() bool { return dl.Ready() || empty() || questrunner.HeldID(d, questplan.ObjTribalTotem) > 0 })
			if empty() {
				logf(d, "totem: the chest is empty: a totem is in the pack or the bank already")
				return false
			}
			if dl.Ready() {
				dl.Drive(nil)
			}
			if waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTribalTotem) > 0 }) {
				logf(d, "totem: found the tribal totem in the chest")
				return true
			}
		case shut:
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "totem: opening the chest: %v", err)
				d.Delay(2 * time.Second)
				continue
			}
			// The shut chest lingers for a tick after the Open lands.
			if !waitUntil(d, 8*time.Second, func() bool { o, _, _ := totemChestLoc(d); return o }) {
				logf(d, "totem: the chest did not open")
			}
		default:
			logf(d, "totem: no chest (locs %d and %d) near %v", questplan.LocTotemChestShut, questplan.LocTotemChestOpen, questplan.TotemChest)
			d.Delay(2 * time.Second)
		}
	}
	logf(d, "totem: no totem from the chest in %s", totemChestBudget)
	return false
}

// totemSteal disarms the stairs, climbs them and takes the totem from the chest. It starts from the ground floor (stairs first) or from
// the first floor (the chest only), and reports whether the pack holds the totem.
func totemSteal(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTribalTotem) > 0 {
		return true
	}
	if lvl := questrunner.BaseLevels(d)["thieving"]; lvl < questplan.TotemThieving {
		e.RequestPark("Thieving 21 is needed to find the trap on the mansion's stairs", questplan.TotemLevelPark)
		logf(d, "totem: Thieving %d, the trap on the stairs wants %d", lvl, questplan.TotemThieving)
		return false
	}
	here := questrunner.CurrentTile(d)
	if !questplan.InTotemMansion(here) {
		logf(d, "totem: the stairs are inside the mansion, and the account is at %v", here)
		return false
	}
	if here.Level == 0 && !totemClimb(e, d) {
		return false
	}
	return totemChest(d)
}
