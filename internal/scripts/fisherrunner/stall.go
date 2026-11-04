package fisherrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// THE RUN STALLER on the fishers' walks (a user request: "the same interaction can be used for the
// fishers bank trips when travelling back to fishing spot, to be used for both progressivefisher and
// bassoyster scripts"). ScarabSwarm already rubs a carried Games necklace or Ring of dueling every
// third tick while it runs (scarabrunner/travel.go): a rub is op 4 on the jewellery, it runs p_delay(1)
// and the engine drains no run energy on a tick the player is delayed (Player.updateEnergy), while
// OpHeldHandler clears moveClickRequest so a running walk keeps going. The tick lab measured a 65% cut
// in run drain at no charge used (scarabplan/runstall.go has the numbers and the engine facts).
//
// The DECISION is scarabplan.ShouldRub, called as it is: nothing about when to rub is restated here,
// so the cadence the tick lab refines follows. This file is the fisher's copy of the I/O half: it
// cannot reuse scarabrunner.Travel, which imports this package.
//
// The jewellery is the account's own: a pack item (rubSlot), kept through every bank deposit
// (DepositExcept keeps it while the stall is on), drawn from the account's own bank when the pack
// holds none (DrawRubItem), and asked of the mule on the hand-off the account is making anyway
// (internal/bot/fisherstall.go). An account with none simply never rubs.
//
// A rub's p_delay refuses an OPHELD read inside it (OpHeldHandler: "cannot interact while delayed"),
// so the drop cycle never starts within RubEveryTicks of a rub (RubedRecently): its clicks would be
// read by the engine and thrown away.

// Stall is one account's run staller: its setting and what it has done. A nil *Stall, or one that is
// not On, never rubs.
type Stall struct {
	// On is the walkRub setting.
	On bool
	// Tick is the engine tick; 600 ms when zero.
	Tick time.Duration
	// Now is the clock; time.Now when nil. A test sets it.
	Now func() time.Time

	lastRub  time.Time
	menuOpen bool
	// walkRubs counts the rubs of the walk under way, walkEnergy is the run energy it began with.
	walkRubs, walkEnergy int
	walkEnergyOK         bool
	// Rubs is every rub since the account started, for the stop line.
	Rubs int
}

func (s *Stall) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Stall) tick() time.Duration {
	if s.Tick <= 0 {
		return 600 * time.Millisecond
	}
	return s.Tick
}

func (s *Stall) active() bool { return s != nil && s.On }

// RubedRecently reports whether a rub was sent so recently that its delay may still refuse a click.
func (s *Stall) RubedRecently() bool {
	if !s.active() || s.lastRub.IsZero() {
		return false
	}
	return s.now().Sub(s.lastRub) < time.Duration(fleetplan.RubEveryTicks)*s.tick()
}

// RubNames is every name a rub item has, any charge: "Games necklace(8)" to "(1)" and "Ring of dueling(8)"
// to "(1)". They are the names a deposit keeps and a hand-off never gives the mule.
func RubNames() []string {
	line, _ := fleetplan.RubAskLine(false)
	return append([]string(nil), line.Choices...)
}

// KeepsName reports whether a deposit leaves an item of this name in the pack for the stall.
func (s *Stall) KeepsName(name string) bool {
	return s.active() && fleetplan.IsRubItem(name)
}

// rubSlot is the first unnoted rub item in the pack.
func rubSlot(d *Deps) (gamestate.SlotItem, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return gamestate.SlotItem{}, false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && fleetplan.IsRubItem(obj.Name) {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// RubItemHeld is whether the pack holds a Games necklace or Ring of dueling.
func RubItemHeld(d *Deps) bool {
	_, ok := rubSlot(d)
	return ok
}

// begin notes the walk's starting run energy, for the line end logs.
func (s *Stall) begin(d *Deps) {
	if !s.active() {
		return
	}
	s.walkRubs = 0
	s.walkEnergy, s.walkEnergyOK = d.Session.State().Energy()
}

// poll is one poll of a walk (webwalk.WalkOptions.Aside): rub once when scarabplan.ShouldRub says so.
func (s *Stall) poll(d *Deps, p webwalk.AsidePoll) {
	if !s.active() {
		return
	}
	st := d.Session.State()
	main, side, _ := st.Modals()
	poll := fleetplan.RubPoll{
		SinceClick: p.SinceClick, SinceRub: s.now().Sub(s.lastRub), Tick: s.tick(),
		Ahead:   tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z}),
		Running: st.RunEnabled(), InCombat: st.InCombat(), Modal: main != gamestate.NoModal || side != gamestate.NoModal,
	}
	if !fleetplan.ShouldRub(poll) {
		return
	}
	slot, ok := rubSlot(d)
	if !ok {
		return
	}
	if err := actions.OpHeld(d.Session, 4, slot.ID, slot.Slot, PlayerPack); err != nil {
		d.logf("run staller: rubbing the item in slot %d: %v", slot.Slot, err)
		return
	}
	s.lastRub, s.menuOpen = s.now(), true
	s.walkRubs++
	s.Rubs++
}

// end closes the teleport menu the walk's last rub left up, so the next action does not find a dialogue
// open (answering nothing teleports nowhere and uses no charge), and says what the walk's rubs did.
func (s *Stall) end(d *Deps) {
	if !s.active() {
		return
	}
	if s.menuOpen {
		s.menuOpen = false
		_ = actions.CloseModal(d.Session)
	}
	if s.walkRubs == 0 {
		return
	}
	energy := ""
	if now, ok := d.Session.State().Energy(); ok && s.walkEnergyOK {
		energy = fmt.Sprintf(", run energy %d%% to %d%%", s.walkEnergy, now)
	}
	d.logf("run staller: %d rub(s) on that walk%s (%d in all)", s.walkRubs, energy, s.Rubs)
	s.walkRubs = 0
}

// DrawRubItem draws one Games necklace or Ring of dueling from an open bank when the pack holds none
// and the stall is on, the best charge first (a rub uses none, so the charge only orders them). It reports
// whether one was drawn.
func DrawRubItem(d *Deps, bank *actions.Bank) bool {
	if !d.Stall.active() || RubItemHeld(d) {
		return false
	}
	have := map[string]bool{}
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && it.Count > 0 && fleetplan.IsRubItem(obj.Name) {
			have[strings.ToLower(strings.TrimSpace(obj.Name))] = true
		}
	}
	for _, name := range RubNames() {
		if !have[strings.ToLower(name)] {
			continue
		}
		if WithdrawOneOf(d, bank, name) {
			d.logf("run staller: drew a %s from the bank", name)
			return true
		}
		return false
	}
	return false
}
