package fleetrunner

// The run staller on every walk the account makes, and the jewellery every bank visit shares (rubwalk-1006).
//
// a user request: "everywhere we walk in scarabswarm should be either jewellery stalling or high alch stalling to
// ensure we always are running". scarabrunner.WalkTo builds the staller into its own walk (Aside, ClickSteps, the walk's
// line, the rub's menu closed after it); every other walk the account's Walker makes -- a quest leg (questrunner.Travel),
// the trainer's trip to a rung, a shop or food run, a KQ leg -- is made by code that knows nothing of it, and made none
// until now: live on 6 Oct "acct" walked Falador to Burthorpe for Death Plateau with a Games necklace in its pack and
// rubbed it never. WalkPrepare is the one seam for all of them.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/webwalk"
)

// WalkPrepare is webwalk.Walker.Prepare for an account whose every walk carries the run staller: a walk whose caller
// brought no Aside of its own gets this Travel's (walkAside: walk alch at Magic 55 with natures and an alchable, the
// jewellery rub otherwise), the far clicks a walk that will cast or rub aims with (clickSteps), the walk's run-energy
// line, and the rub's teleport menu closed as the walk returns -- exactly what WalkTo gives its own walk, from the same
// functions, so the one behaves as the other. A walk that brings its own Aside is left to it.
//
// deps builds the Deps the staller reads the session, the item config and the log through, once per walk (the bot's own
// are built the same way); nil Travel, nil deps or a Travel that stalls nothing (walkAlch and walkRub both off) return
// nil, which leaves the Walker as it was.
func (t *Travel) WalkPrepare(deps func() *Deps) func(o *webwalk.WalkOptions) func() {
	if t == nil || deps == nil || (!t.Alch.Enabled && !t.Rub) {
		return nil
	}
	return func(o *webwalk.WalkOptions) func() {
		if o.Aside != nil {
			return nil // the caller's own walk work (WalkTo's, which the staller is part of)
		}
		d := deps()
		if d == nil || d.Session == nil {
			return nil
		}
		aside := t.walkAside(d)
		if aside == nil {
			return nil
		}
		t.sayWalkStall(d, o.Dest)
		t.beginWalk(d)
		o.Aside = aside
		if o.ClickSteps == 0 {
			o.ClickSteps = t.clickSteps()
		}
		return func() {
			t.CloseRubMenu(d)
			t.endWalk(d)
		}
	}
}

// WalkStall is WalkPrepare for a caller that builds its own WalkOptions and has walk work of its own (MasterCrafter's chest
// round eats a Strange fruit when run energy is low): it returns the Aside to use (the caller's own work first, then the
// staller's), the click steps to aim with and the func to call when the walk returns (the rub's menu closed, the walk's
// line logged). A Travel that stalls nothing returns the caller's own and does nothing at the end.
func (t *Travel) WalkStall(d *Deps, dest webwalk.Point, own func(webwalk.AsidePoll)) (aside func(webwalk.AsidePoll), clickSteps int, done func()) {
	if t == nil || d == nil || d.Session == nil || (!t.Alch.Enabled && !t.Rub) {
		return own, 0, func() {}
	}
	stall := t.walkAside(d)
	if stall == nil {
		return own, 0, func() {}
	}
	t.sayWalkStall(d, dest)
	t.beginWalk(d)
	aside = func(p webwalk.AsidePoll) {
		if own != nil {
			own(p)
		}
		stall(p)
	}
	return aside, t.clickSteps(), func() {
		t.CloseRubMenu(d)
		t.endWalk(d)
	}
}

// StallMode is how the run staller will stall a walk starting now and why, in words for the log: "walk alch", "rub (Games
// necklace(8))" or "none (no rub item, no alch kit)" with what is missing. The same tests the walk's Aside makes
// (canAlch, rubSlot), read once at the walk's start; an item picked up mid-walk is still used (the Aside reads the pack
// at every poll).
func (t *Travel) StallMode(d *Deps) (stalls bool, why string) {
	if t == nil || d == nil {
		return false, "none (no Travel)"
	}
	if t.canAlch(d) {
		return true, "walk alch"
	}
	var missing []string
	if t.Alch.Enabled {
		switch {
		case MagicLevel(d) < t.Alch.Normalised().MinMagic:
			missing = append(missing, "Magic below "+strconv.Itoa(t.Alch.Normalised().MinMagic))
		case CountPackNamed(d, alchplan.NatureRune) <= 0:
			missing = append(missing, "no nature runes")
		}
	}
	if t.Rub {
		if slot, ok := rubSlot(d); ok {
			return true, "rub (" + ObjName(d, slot.ID) + ")"
		}
		missing = append(missing, "no rub item")
	} else {
		missing = append(missing, "walkRub off")
	}
	if t.Alch.Enabled {
		missing = append(missing, "no alch kit")
	}
	return false, "none (" + strings.Join(missing, ", ") + ")"
}

// sayWalkStall logs how a walk to dest is stalled, or why it is not: one line per distinct answer every ten minutes, so
// a fleet's walks show which accounts stall and which walk plain (and why) without a line a walk.
func (t *Travel) sayWalkStall(d *Deps, dest webwalk.Point) {
	_, why := t.StallMode(d)
	t.sayOnce(d, "walk-stall:"+why, "run staller: walk to ("+strconv.Itoa(dest.X)+","+strconv.Itoa(dest.Z)+"): "+why)
}

// rubSlotsToLeave is the pack slots a jewellery draw leaves free: the next item the account's work picks up (a quest's
// ground item, the first loot of a fight) must have room, and a necklace is worth less than the step it would block.
const rubSlotsToLeave = 2

// DrawRubItem takes ONE Games necklace or Ring of dueling out of the open bank for the run staller (walkRub): when the
// pack holds none, the bank holds one of any charge (a rub uses no charge) and the pack keeps rubSlotsToLeave slots free
// after it. It is the account's way of carrying its own jewellery: before it only the magic training kit's bank visit
// drew one (DrawAlchKit), so an account whose necklace sat in its bank (live, 6 Oct: "acct") never carried it, and any
// bank visit that deposited the lot (a quest's, a melee trip's) sent it back. Items, never notes: a noted necklace has no
// Rub option. w nil is the thieving runner's waiter. Nothing happens on a nil Travel, with walkRub off or a nil bank.
func (t *Travel) DrawRubItem(d *Deps, bank *actions.Bank, w actions.Waiter) {
	if t == nil || !t.Rub || bank == nil || d == nil || RubItemHeld(d) || FreeSlots(d)-1 < rubSlotsToLeave {
		return
	}
	for _, it := range bank.Items() {
		if it.Count <= 0 || !fleetplan.IsRubItem(ObjName(d, it.ID)) {
			continue
		}
		if w == nil {
			w = thieverrunner.Waiter(Thiev(d))
		}
		if bank.NoteMode() {
			if err := bank.SetNoteMode(false, w); err != nil {
				d.Logf("run staller: switching the bank to items, not notes: %v", err)
				return
			}
		}
		name := ObjName(d, it.ID)
		if err := bank.QuickWithdrawX(it.ID, 1, w); err != nil {
			d.Logf("run staller: withdrawing the %s: %v", name, err)
			return
		}
		WaitFor(d, "the run staller's "+name+" in the pack", 3*time.Second, func() bool { return RubItemHeld(d) })
		return
	}
}
