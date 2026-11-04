// Package walkstall is the run staller any script's webwalk Walker can carry: a Games necklace or Ring of dueling in the
// pack is rubbed (op 4) every third tick of a walk, which holds the player in a one-tick p_delay on which the engine drains no
// run energy (Player.updateEnergy returns early while delayed), and which leaves a running walk's movement alone
// (OpHeldHandler clears moveClickRequest). The tick lab measured a 65% cut in run drain at no charge used; the decision, the
// numbers and the engine facts are scarabplan/runstall.go's, called as they are.
//
// ScarabSwarm has its own copy (scarabrunner.Travel, which also casts High Alchemy on the walk) and the fishers theirs
// (fisherrunner/stall.go, which cannot import scarabrunner); this one has no script's Deps, only the session, the item
// config and a log, so ProgressiveWCFletch, ProgressiveCrafter, RuneRunner and the rest install it on their Walkers the same
// way: webwalk.Walker.Prepare, which every WalkTo calls with its options.
//
// The jewellery is the account's own, from three places: the pack, kept through every bank deposit (KeepsName); its own bank,
// drawn when the pack holds none (DrawRubItem); and the mule, asked on a hand-off the account is making anyway (RubAskLine in
// internal/bot). An account with none never rubs, and a walk never waits for one.
//
// A rub opens the jewellery's teleport menu (a chat dialogue) and its p_delay refuses a click read inside it, so the walk's
// next movement click is kept clear (scarabplan.ShouldRub's SinceClick and Ahead) and the menu is closed when the walk ends,
// so the next action does not find a dialogue open. Answering nothing teleports nowhere and uses no charge.
package walkstall

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent backpack component, the id every runner uses for the account's own pack.
const PlayerPack = 3214

// Env is what the staller reads the account through.
type Env struct {
	Session actions.Session
	Config  *cachecfg.Config
	// Log receives one already-formatted line; nil drops it.
	Log func(string)
	// Label prefixes the walk's log line ("wcfletch", "crafter").
	Label string
}

func (e *Env) logf(format string, args ...any) {
	if e != nil && e.Log != nil {
		e.Log(fmt.Sprintf(format, args...))
	}
}

// Stall is one account's run staller: its setting and what it has done. A nil *Stall, or one that is not On, never rubs.
type Stall struct {
	// On is the walkRub setting.
	On bool
	// Tick is the engine tick; 600 ms when zero. Now is the clock; time.Now when nil. A test sets them.
	Tick time.Duration
	Now  func() time.Time
	// Busy, when set, says another walk aside has the account's attention (the fletch aside's batch): the stall waits, so a
	// rub's menu and delay never cut a batch's weak queue.
	Busy func() bool

	lastRub  time.Time
	menuOpen bool
	// walkRubs counts the rubs of the walk under way, walkEnergy is the run energy it began with.
	walkRubs, walkEnergy int
	walkEnergyOK         bool
	// Rubs is every rub since the account started, for the stop line.
	Rubs int
}

// New returns a staller; on false never rubs.
func New(on bool) *Stall { return &Stall{On: on} }

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

// Active reports whether the staller is on.
func (s *Stall) Active() bool { return s != nil && s.On }

// RubedRecently reports whether a rub was sent so recently that its delay may still refuse a click.
func (s *Stall) RubedRecently() bool {
	if !s.Active() || s.lastRub.IsZero() {
		return false
	}
	return s.now().Sub(s.lastRub) < time.Duration(fleetplan.RubEveryTicks)*s.tick()
}

// RubNames is every name a rub item has, any charge: the names a deposit keeps and a hand-off never gives the mule.
func RubNames() []string {
	line, _ := fleetplan.RubAskLine(false)
	return append([]string(nil), line.Choices...)
}

// KeepsName reports whether a deposit leaves an item of this name in the pack for the stall.
func (s *Stall) KeepsName(name string) bool {
	return s.Active() && fleetplan.IsRubItem(name)
}

// KeepNames is RubNames while the stall is on, nil otherwise: the names a hand-off must never give the mule.
func (s *Stall) KeepNames() []string {
	if !s.Active() {
		return nil
	}
	return RubNames()
}

// rubSlot is the first unnoted rub item in the pack.
func rubSlot(e *Env) (gamestate.SlotItem, bool) {
	inv := e.Session.State().Inventory(PlayerPack)
	if inv == nil || e.Config == nil {
		return gamestate.SlotItem{}, false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := e.Config.Obj(int(it.ID)); ok && !obj.Noted() && fleetplan.IsRubItem(obj.Name) {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// Held is whether the pack holds a Games necklace or Ring of dueling.
func Held(e *Env) bool {
	_, ok := rubSlot(e)
	return ok
}

// poll is one poll of a walk (webwalk.WalkOptions.Aside): rub once when scarabplan.ShouldRub says so.
func (s *Stall) poll(e *Env, p webwalk.AsidePoll) {
	if !s.Active() {
		return
	}
	if s.Busy != nil && s.Busy() {
		return
	}
	st := e.Session.State()
	main, side, _ := st.Modals()
	hp, _ := st.HP()
	maxHP, _ := st.MaxHP()
	poll := fleetplan.RubPoll{
		SinceClick: p.SinceClick, SinceRub: s.now().Sub(s.lastRub), Tick: s.tick(),
		Ahead:   tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z}),
		Running: st.RunEnabled(), InCombat: st.InCombat(), Modal: main != gamestate.NoModal || side != gamestate.NoModal,
		HP: hp, MaxHP: maxHP,
	}
	if !fleetplan.ShouldRub(poll) {
		return
	}
	slot, ok := rubSlot(e)
	if !ok {
		return
	}
	if err := actions.OpHeld(e.Session, 4, slot.ID, slot.Slot, PlayerPack); err != nil {
		e.logf("run staller: rubbing the item in slot %d: %v", slot.Slot, err)
		return
	}
	s.lastRub, s.menuOpen = s.now(), true
	s.walkRubs++
	s.Rubs++
}

// begin notes the walk's starting run energy, for the line end logs.
func (s *Stall) begin(e *Env) {
	s.walkRubs = 0
	s.walkEnergy, s.walkEnergyOK = e.Session.State().Energy()
}

// end closes the teleport menu the walk's last rub left up and says what the walk's rubs did.
func (s *Stall) end(e *Env) {
	if s.menuOpen {
		s.menuOpen = false
		_ = actions.CloseModal(e.Session)
	}
	if s.walkRubs == 0 {
		return
	}
	energy := ""
	if now, ok := e.Session.State().Energy(); ok && s.walkEnergyOK {
		energy = fmt.Sprintf(", run energy %d%% to %d%%", s.walkEnergy, now)
	}
	label := "run staller"
	if e.Label != "" {
		label = e.Label + " run staller"
	}
	e.logf("%s: %d rub(s) on that walk%s (%d in all)", label, s.walkRubs, energy, s.Rubs)
	s.walkRubs = 0
}

// Prepare is the webwalk.Walker.Prepare hook of this staller: a walk that brings its own Aside is left to it; otherwise,
// when the pack holds a rub item, the walk gets the rub aside and clicks that aim as far as scarabplan.WalkAlchClickSteps (a
// click is a tick the stalling cannot use).
func (s *Stall) Prepare(e *Env) func(o *webwalk.WalkOptions) func() {
	if !s.Active() || e == nil || e.Session == nil {
		return nil
	}
	return func(o *webwalk.WalkOptions) func() {
		if o.Aside != nil || !Held(e) {
			return nil
		}
		s.begin(e)
		o.Aside = func(p webwalk.AsidePoll) { s.poll(e, p) }
		if o.ClickSteps == 0 {
			o.ClickSteps = fleetplan.WalkAlchClickSteps
		}
		return func() { s.end(e) }
	}
}

// DrawRubItem draws one Games necklace or Ring of dueling from an open bank when the pack holds none and the stall is on,
// the best charge first (a rub uses none, so the charge only orders them). withdrawOne takes one item of a name out of the
// bank (every script has its own, with its own waits). It reports whether one was drawn.
func (s *Stall) DrawRubItem(e *Env, bank *actions.Bank, withdrawOne func(name string) bool) bool {
	if !s.Active() || e == nil || Held(e) {
		return false
	}
	have := map[string]bool{}
	for _, it := range bank.Items() {
		if obj, ok := e.Config.Obj(int(it.ID)); ok && !obj.Noted() && it.Count > 0 && fleetplan.IsRubItem(obj.Name) {
			have[strings.ToLower(strings.TrimSpace(obj.Name))] = true
		}
	}
	for _, name := range RubNames() {
		if !have[strings.ToLower(name)] {
			continue
		}
		if withdrawOne(name) {
			e.logf("run staller: drew a %s from the bank", name)
			return true
		}
		return false
	}
	return false
}

// BankHolds counts the rub items an open bank holds (un-noted, any charge), for the kit ask's "the bank has none" test.
func BankHolds(e *Env, bank *actions.Bank) int {
	n := 0
	for _, it := range bank.Items() {
		if obj, ok := e.Config.Obj(int(it.ID)); ok && !obj.Noted() && fleetplan.IsRubItem(obj.Name) {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// Compose joins several Walker.Prepare hooks into one: each is given the walk's options with no Aside, the Asides they set
// run in order at every poll, the largest ClickSteps wins, and every returned done function runs when the walk ends (in
// reverse). A hook that leaves Aside nil adds none. An Aside the caller set is left alone and no hook runs.
func Compose(parts ...func(o *webwalk.WalkOptions) func()) func(o *webwalk.WalkOptions) func() {
	var live []func(o *webwalk.WalkOptions) func()
	for _, p := range parts {
		if p != nil {
			live = append(live, p)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return func(o *webwalk.WalkOptions) func() {
		if o.Aside != nil {
			return nil
		}
		var asides []func(webwalk.AsidePoll)
		var dones []func()
		clicks := o.ClickSteps
		for _, p := range live {
			sub := *o
			sub.Aside = nil
			if done := p(&sub); done != nil {
				dones = append(dones, done)
			}
			if sub.Aside != nil {
				asides = append(asides, sub.Aside)
			}
			if sub.ClickSteps > clicks {
				clicks = sub.ClickSteps
			}
		}
		if len(asides) > 0 {
			o.Aside = func(p webwalk.AsidePoll) {
				for _, a := range asides {
					a(p)
				}
			}
		}
		o.ClickSteps = clicks
		if len(dones) == 0 {
			return nil
		}
		return func() {
			for i := len(dones) - 1; i >= 0; i-- {
				dones[i]()
			}
		}
	}
}

// RunEnergyThreshold is the run-energy percentage at or above which EnsureRun turns running on: the same 20 the shared walk
// helpers use (freshstartrunner.RunEnergyThreshold).
const RunEnergyThreshold = 20

// WantRun is EnsureRun's decision, pure: running is off, the energy has been observed and is at or above the threshold. An
// energy not yet observed is unknown, not plenty.
func WantRun(runOn bool, energy int, energyKnown bool) bool {
	return !runOn && energyKnown && energy >= RunEnergyThreshold
}

// EnsureRun turns the run toggle on when there is energy for it. A walk made through webwalk.Walker.WalkTo directly (ProgressiveCrafter's
// every walk, for one) never did: the server switches running off at 0% and nothing switched it on again, so the account walked
// one tile a tick for good. Called as every walk starts, it costs nothing once the toggle is on.
func EnsureRun(e *Env) {
	if e == nil || e.Session == nil {
		return
	}
	st := e.Session.State()
	energy, ok := st.Energy()
	if !WantRun(st.RunEnabled(), energy, ok) {
		return
	}
	if err := actions.SetRun(e.Session, true); err != nil {
		e.logf("enable run: %v", err)
	}
}
