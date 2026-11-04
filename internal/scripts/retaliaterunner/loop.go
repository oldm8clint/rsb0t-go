package retaliaterunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/retaliateplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// tickWait bounds one wait for a tick: a PLAYER_INFO arrives every 600 ms, so this is only reached when the connection stopped
// delivering them.
const tickWait = 1500 * time.Millisecond

// noTicksFor is how long without a tick ends the loop.
const noTicksFor = 20 * time.Second

// replug is how many ticks pass between two plain clicks while the loop is not locked (the account chops the ordinary way
// while the rats settle, so its first rolls and the log line have something to show).
const replug = 6

// loopState is what one run of the loop counts.
type loopState struct {
	win        retaliateplan.Window
	base       retaliateplan.Stats
	baseLogs   int64
	baseXP     int
	winStart   int
	startTick  int
	lastRep    int
	lastHealth int // tick of the last gear and controls check
	held       bool
	lastObsAt  time.Time
	lastWarn   map[string]int
}

// run is the tick loop. It returns to Step with the phase set for what comes next.
func (r *Runner) run(d *Deps) {
	why, text := r.loop(d)
	switch why {
	case retaliateplan.WhyStopped:
	case retaliateplan.WhyDead:
		r.stop(d, "%s", text)
	case retaliateplan.WhyHP:
		_ = actions.SetAutoRetaliate(d.Session, false)
		r.stop(d, "%s; auto-retaliate is off", text)
	case retaliateplan.WhyRatsHurt:
		_ = actions.SetAutoRetaliate(d.Session, false)
		r.logf(d, "%s", text)
		r.toFallback(d, "a rat was hurt")
	case retaliateplan.WhyEvent:
		r.logf(d, "%s; the loop pauses until it is gone", text)
		woodcutrunner.WaitForGuardian(d)
		r.phase = PhaseStand
	case retaliateplan.WhyGear:
		r.logf(d, "%s", text)
		_ = actions.SetAutoRetaliate(d.Session, false)
		r.phase = PhasePrepare
	case retaliateplan.WhyFull:
		if r.Set.Full == retaliateplan.FullBank && r.bank(d) {
			r.haveStand = true
			r.phase = PhaseStand
			// the rats are left behind at the bank trip; the stand step goes on to the aggro
			return
		}
		r.dropAll(d)
	case retaliateplan.WhyRephase:
		r.startRephase(d, text)
	case retaliateplan.WhyNoTicks:
		r.logf(d, "%s", text)
		d.Delay(2 * time.Second)
	}
}

// loop runs until something needs the runner's attention.
func (r *Runner) loop(d *Deps) (retaliateplan.Why, string) {
	st := d.Session.State()
	f := newFeed(st)
	defer f.stop()
	r.ctl.Reset()
	r.ctl.SetHeld(false)
	ls := &loopState{lastObsAt: time.Now(), lastWarn: map[string]int{}}
	ls.startTick = st.Tick()
	ls.winStart, ls.lastRep, ls.lastHealth = ls.startTick, ls.startTick, ls.startTick
	ls.base = r.ctl.Stats()
	ls.baseXP = woodcutrunnerXP(d)
	r.lastClick = -100
	r.cur = -1
	r.pickTree(d, ls)
	r.logf(d, "looping at %v: lead %d, clear by %s, %d tree(s) in reach", here(d), r.ctl.Lead(), r.Set.Clear, len(r.trees))
	defer func() {
		r.totLogs += f.logs.Load()
		r.totTicks += st.Tick() - ls.startTick
	}()
	for {
		obs, ok, stopped := f.next(d.Delay, tickWait)
		if stopped {
			return retaliateplan.WhyStopped, ""
		}
		if !ok {
			if time.Since(ls.lastObsAt) > noTicksFor {
				return retaliateplan.WhyNoTicks, "no tick arrived for 20 seconds"
			}
			continue
		}
		ls.lastObsAt = time.Now()
		// feed every queued tick to the controller, but act only on the newest: an older one would be read after the tick it
		// was aimed at
		act := r.ctl.Observe(obs)
		for {
			next, have := f.pop()
			if !have {
				break
			}
			obs = next
			act = r.ctl.Observe(obs)
		}
		if st.Tick() == obs.N {
			r.perform(d, act, obs.N, ls)
		}
		if why, text := r.supervise(d, obs.N, ls); why != retaliateplan.WhyNone {
			r.report(d, f, ls, obs.N, true)
			return why, text
		}
		r.report(d, f, ls, obs.N, false)
		if v, text := r.ctl.Verdict(); v == retaliateplan.VerdictRephase {
			r.report(d, f, ls, obs.N, true)
			return retaliateplan.WhyRephase, text
		}
	}
}

func woodcutrunnerXP(d *Deps) int { return woodcutrunner.WoodcuttingXP(d) }

// pickTree sets r.cur to the first tree still standing (-1 when none) and holds or resumes the controller accordingly.
func (r *Runner) pickTree(d *Deps, ls *loopState) {
	if r.cur >= 0 && r.cur < len(r.trees) && r.up(d, r.trees[r.cur]) {
		return
	}
	prev := r.cur
	r.cur = -1
	for i, t := range r.trees {
		if r.up(d, t) {
			r.cur = i
			break
		}
	}
	switch {
	case r.cur < 0 && !ls.held:
		ls.held = true
		r.ctl.SetHeld(true)
		r.logf(d, "no oak stands in reach: the loop holds until one grows back (the rats keep hitting; the phase is kept)")
	case r.cur >= 0 && ls.held:
		ls.held = false
		r.ctl.SetHeld(false)
		r.ctl.NoteSwap()
		t := r.trees[r.cur]
		r.logf(d, "an oak is back at %d,%d: the loop resumes", t.m.X, t.m.Z)
		r.click(d)
	case r.cur >= 0 && prev >= 0 && r.cur != prev:
		r.ctl.NoteSwap()
		t := r.trees[r.cur]
		r.vlog(d, "swapped to the oak at %d,%d", t.m.X, t.m.Z)
	}
}

func (r *Runner) click(d *Deps) {
	if r.cur < 0 || actions.MovementHeld(d.Session) {
		return
	}
	t := r.trees[r.cur]
	if err := actions.OpLoc(d.Session, t.m.Op, t.m.X, t.m.Z, t.m.ID); err != nil {
		r.logf(d, "writing the tree click failed: %v", err)
	}
}

// perform writes what the controller asked for, or the plain click that keeps the account chopping while it is not locked.
func (r *Runner) perform(d *Deps, act retaliateplan.Action, n int, ls *loopState) {
	switch act {
	case retaliateplan.ActClick:
		r.click(d)
	case retaliateplan.ActClear:
		r.clear(d, n, ls)
	default:
		if !r.ctl.Locked() && r.cur >= 0 && n-r.lastClick >= replug && !swinging(d) {
			r.lastClick = n
			r.click(d)
		}
	}
}

// clear writes the interaction clear: a Drop of a log not already aimed at, or the controls-tab button, which clears the
// interaction and needs no item.
func (r *Runner) clear(d *Deps, n int, ls *loopState) {
	if actions.MovementHeld(d.Session) {
		return
	}
	ls.win.Clears++
	if r.Set.Clear == retaliateplan.ClearDrop {
		if c, ok := r.picker.Pick(r.packLogs(d), n, r.ctl.Lead()); ok {
			if err := actions.OpHeld(d.Session, 5, c.ID, c.Slot, PlayerPack); err == nil {
				ls.win.Drops++
				return
			}
		}
	}
	if err := actions.IfButton(d.Session, actions.ControlsRetaliateOn); err != nil {
		r.logf(d, "writing the clear failed: %v", err)
	}
}

// supervise reads the safety state and says whether the loop must stop. It also keeps the account's tree pointer and its
// dialogs.
func (r *Runner) supervise(d *Deps, n int, ls *loopState) (retaliateplan.Why, string) {
	st := d.Session.State()
	hp, hpOK := st.HP()
	maxHP, _ := st.MaxHP()
	if !hpOK {
		hp = -1
	}
	check := retaliateplan.Check{
		Dead: st.IsDead(), HP: hp, MaxHP: maxHP, StopPct: r.Set.StopHPPercent,
		EventBusy: randomevents.Busy(d.Session), WeaponOK: true, QuiverEmpty: true,
	}
	var views []retaliateplan.NpcView
	for _, rat := range r.rats(d) {
		views = append(views, retaliateplan.NpcView{Name: npcName(d, rat.Type), Health: rat.Health, TotalHealth: rat.TotalHealth})
	}
	check.RatsHurt, _ = retaliateplan.RatsHurt(views, r.Site.RatName)
	if n-ls.lastHealth >= 5 {
		ls.lastHealth = n
		weapon, ammo, _, _ := worn(d)
		check.WeaponOK = retaliateplan.IsShortbow(weapon)
		check.QuiverEmpty = ammo == ""
		if !st.AutoRetaliateOn() {
			r.logf(d, "auto-retaliate is off: pressing it on again")
			_ = actions.SetAutoRetaliate(d.Session, true)
		}
	}
	check.PackFull = woodcutrunner.PackFull(d)
	if why, text := retaliateplan.Supervise(check); why != retaliateplan.WhyNone {
		return why, text
	}
	r.pickTree(d, ls)
	if dlg := actions.NewChatDialog(d.Session, d.Components); dlg.CanContinue() {
		_ = dlg.Continue()
	}
	return retaliateplan.WhyNone, ""
}

// report writes the rolling line every ReportEvery ticks (and once more when the loop ends, if enough ticks passed).
func (r *Runner) report(d *Deps, f *feed, ls *loopState, n int, final bool) {
	if n-ls.winStart < r.Set.ReportEvery && !(final && n-ls.winStart >= 30) {
		return
	}
	cs := r.ctl.Stats()
	w := ls.win
	w.Ticks = n - ls.winStart
	w.Rolls = cs.Rolls - ls.base.Rolls
	w.Logs = int(f.logs.Load() - ls.baseLogs)
	w.XP = float64(woodcutrunnerXP(d) - ls.baseXP)
	w.Gaps2 = cs.Gaps2 - ls.base.Gaps2
	w.Gaps = w.Gaps2 + cs.Gaps3 - ls.base.Gaps3 + cs.Gaps4 - ls.base.Gaps4 + cs.GapsMid - ls.base.GapsMid
	w.HitGaps = retaliateplan.HitGapsOf(r.ctl.Hits(), 6)
	w.Lead, w.Pattern, w.Locked = r.ctl.Lead(), r.ctl.Pattern(), r.ctl.Locked()
	r.logf(d, "%s", w.Line())
	r.logf(d, "stats: %s; refused attacks (late clear or click) %d", cs.Line(), f.refused.Load())
	ls.win = retaliateplan.Window{}
	ls.base, ls.baseLogs, ls.baseXP, ls.winStart = cs, f.logs.Load(), woodcutrunnerXP(d), n
	if v, _ := r.ctl.Verdict(); v == retaliateplan.VerdictRun && r.ctl.Locked() {
		if share, k := r.ctl.GainShare(); k >= retaliateplan.GainMinGaps && share >= r.Set.MinGain {
			r.rephase.Held()
		}
	}
}
