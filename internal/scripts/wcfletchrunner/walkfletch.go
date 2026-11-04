package wcfletchrunner

// Cutting the pack's logs WHILE the account walks to the bank (wcfletchplan/walkfletch.go has the engine facts and the
// decision). The walk is webwalk's; this is the aside its poll loop makes room for: each poll the pure decision looks at the
// pack and the menu and says nothing, press Make 10 on the menu that is open, or send a knife on the logs. Nothing here sends
// movement, and a poll never waits.
//
// Armed by the bot right before it walks to the bank with a pack of cuttable logs (Arm) and disarmed after (Disarm); a walk
// made while it is not armed is untouched. The walk it makes is one click of up to 100 tiles (ClickSteps), because every
// movement click ends the batch the weak queue is running.

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/webwalk"
)

// WalkFletch is one account's cut-while-walking state: its setting, the guard that switches it off after empty walks, and the
// totals for the log line. A nil *WalkFletch is off.
type WalkFletch struct {
	// On is the walkFletch setting.
	On    bool
	Guard wcfletchplan.WalkFletchGuard
	// TotalLogs and TotalTicks are every logged walk's logs cut and walking ticks since the account started.
	TotalLogs, TotalTicks int

	armed    bool
	logsName string
	product  wcfletchplan.Product
	reserve  int

	// The walk under way.
	startLogs, startTick, lastLogs int
	lastFall, lastUse, lastAnswer  time.Time
	window                         *loot.Window
	answerFailed                   bool
}

// NewWalkFletch returns the account's state; on false never arms.
func NewWalkFletch(on bool) *WalkFletch { return &WalkFletch{On: on} }

// Allowed reports whether a walk to the bank should be armed now: the setting is on and the guard has not switched it off.
func (w *WalkFletch) Allowed(now time.Time) bool { return w != nil && w.On && w.Guard.Allowed(now) }

// Arm readies the next walk to cut logsName into p, leaving reserve logs uncut.
func (w *WalkFletch) Arm(logsName string, p wcfletchplan.Product, reserve int) {
	if w == nil {
		return
	}
	w.armed, w.logsName, w.product, w.reserve = true, logsName, p, max(reserve, 0)
}

// Disarm ends the arming: the next walk is plain.
func (w *WalkFletch) Disarm() {
	if w != nil {
		w.armed = false
	}
}

// Busy reports whether the walk has cutting to do, so the run staller leaves it the account's attention (a rub's menu and
// delay would cut the batch's weak queue).
func (w *WalkFletch) Busy(d *Deps) bool {
	if w == nil || !w.armed {
		return false
	}
	return HasKnife(d) && CountByName(d, w.logsName)-w.reserve > 0
}

// Prepare is the Walker.Prepare hook: an armed walk gets the cutting aside and the longest clicks; a walk that brings its own
// aside is left to it.
func (w *WalkFletch) Prepare(deps func() *Deps) func(o *webwalk.WalkOptions) func() {
	if w == nil || !w.On || deps == nil {
		return nil
	}
	return func(o *webwalk.WalkOptions) func() {
		if !w.armed || o.Aside != nil {
			return nil
		}
		d := deps()
		if d == nil || d.Session == nil {
			return nil
		}
		w.begin(d)
		o.Aside = func(p webwalk.AsidePoll) { w.poll(d, p) }
		if o.ClickSteps == 0 {
			o.ClickSteps = fleetplan.WalkAlchClickSteps
		}
		return func() { w.end(d) }
	}
}

func (w *WalkFletch) begin(d *Deps) {
	w.startLogs = CountByName(d, w.logsName)
	w.lastLogs = w.startLogs
	w.startTick = d.Session.State().Tick()
	w.lastFall, w.lastUse, w.lastAnswer = time.Time{}, time.Time{}, time.Time{}
	w.answerFailed = false
	if id, ok := itemID(d, w.logsName); ok {
		w.window = beginFletch(d, int(id), w.logsName)
	}
}

func since(t time.Time) time.Duration {
	if t.IsZero() {
		return time.Hour
	}
	return time.Since(t)
}

func (w *WalkFletch) poll(d *Deps, p webwalk.AsidePoll) {
	if !w.armed {
		return
	}
	st := d.Session.State()
	logs := CountByName(d, w.logsName)
	if logs < w.lastLogs {
		w.lastFall = time.Now()
	}
	w.lastLogs = logs
	dlg := actions.NewChatDialog(d.Session, d.Components)
	main, side, _ := st.Modals()
	offers := dlg.Offers(d.Config, w.product.MakeMatch)
	poll := wcfletchplan.WalkFletchPoll{
		HaveKnife:    HasKnife(d),
		LogsToCut:    logs - w.reserve,
		MenuOffers:   offers,
		OtherModal:   main != gamestate.NoModal || side != gamestate.NoModal || (dlg.IsOpen() && !offers),
		CloseSettled: st.ModalCloseSettled(),
		SinceFall:    since(w.lastFall), SinceUse: since(w.lastUse), SinceAnswer: since(w.lastAnswer),
		Ahead:    tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z}),
		InCombat: st.InCombat(),
	}
	switch wcfletchplan.DecideWalkFletch(poll) {
	case wcfletchplan.WalkUse:
		knifeID, ok := itemID(d, wcfletchplan.Knife)
		logsID, ok2 := itemID(d, w.logsName)
		if !ok || !ok2 {
			return
		}
		if err := actions.NewEquipment(d.Session, d.Components).UseOnHeld(knifeID, logsID); err != nil {
			logf(d, "wcfletch: walk-fletch: knife on %s was refused: %v", w.logsName, err)
			w.lastUse = time.Now() // not again this tick
			return
		}
		w.lastUse = time.Now()
	case wcfletchplan.WalkAnswer:
		// Make 10: no count dialogue, so one packet. A batch of ten is twenty ticks, about one walk click.
		if err := dlg.Make(d.Config, w.product.MakeMatch, nil); err != nil {
			if !w.answerFailed {
				w.answerFailed = true
				logf(d, "wcfletch: walk-fletch: could not answer the make menu: %v", err)
			}
		}
		w.lastAnswer = time.Now()
	}
}

// end closes a make menu the walk left unanswered, and says what the walk cut.
func (w *WalkFletch) end(d *Deps) {
	dlg := actions.NewChatDialog(d.Session, d.Components)
	if dlg.Offers(d.Config, w.product.MakeMatch) {
		_ = actions.CloseModal(d.Session)
	}
	now := CountByName(d, w.logsName)
	cut := max(w.startLogs-now, 0)
	ticks := max(d.Session.State().Tick()-w.startTick, 0)
	if w.window != nil {
		if cut > 0 {
			w.window.Got(cut)
		}
		w.window.Flush()
		w.window = nil
	}
	if w.startLogs-w.reserve < 1 {
		return // nothing was to be cut: not a walk the cutting could have worked on
	}
	if cut > 0 {
		w.TotalLogs += cut
		w.TotalTicks += ticks
	}
	if reason := w.Guard.Note(cut, time.Now()); reason != "" {
		logf(d, "wcfletch: walk-fletch OFF for %s: %s — the pack is cut where the account stands, as before", wcfletchplan.WalkFletchOffFor, reason)
	}
	if cut > 0 || ticks >= 10 {
		logf(d, "%s", wcfletchplan.WalkFletchLine(cut, ticks, w.TotalLogs, w.TotalTicks))
	}
}
