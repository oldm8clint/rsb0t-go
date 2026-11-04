package fisherrunner

// FISHING MANIPULATION: the I/O half of internal/scripts/fishmanipplan.
//
// A fishing account that holds a knife and a log can make a net, big net, lobster pot or harpoon spot roll every 3 ticks
// instead of every 6 by writing [knife on the log, Make 1, CLOSE_MODAL, spot click] in the stale tick after a roll and one
// lone spot click in the roll's own tick (fishmanipplan's package comment has the engine facts, the files they were read
// in and how the controller keeps the pattern on the batch's own read tick; docs/tick_manipulation_lab_2026-10-06.md sections 4.2 and
// 4.3 hold the lab numbers: x2.0 for the net, x2.2 for the big net, x1.7 for the harpoon, and what a slow link costs).
//
// Nothing here runs unless Deps.Manip says so, and then only inside FishUntil's hold. FishUntil keeps every rule it had (the
// whirlpool guard, the vanished spot, the full pack, the random events); this file only changes WHAT it writes and HOW it
// waits, in these places:
//
//   - the first click of a hold (manipClick) is the opening batch when the controller wants one and the kit is held;
//   - the wait between checks (manipWait) becomes, while the pattern runs, a wait for the next PLAYER_INFO (the fleet's own
//     EventTick), after which the controller says whether to write a batch or the lone click, so a write is made within
//     microseconds of the tick it follows;
//   - the hold's liveness test (manipAlive): a net's roll plays no animation, so a hold whose batches are being confirmed is
//     alive even though fishingActive sees none;
//   - the end of a hold (manipSettle, manipEnd): the batch still in flight is waited for before the next thing (the drop
//     cycle, a walk) writes, so the two never share a tick's five user events.
//
// # SAFE TO RUN LIVE: what it measures and when it stops
//
// The a user request, "lets not bother with lab proof we can just fix it running on live": so the manipulation measures
// itself against the plain click on the account's own spots and says so in the log, and goes back to the plain click by itself
// whenever it cannot be shown to help.
//
//   - A plain window comes first. The first 200 ticks of the account's first hold at a rung are fished the ordinary way and their
//     catches counted (calibration); then the interaction is ended (one click on the own tile), the varp is left to go stale (8
//     ticks) and the opening batch starts the pattern. Another plain window of 200 ticks follows every 6,000 manipulated ticks
//     (about an hour), so the baseline stays the account's own.
//   - One line per FishUntil call, and one every 3,000 manipulated ticks: "fish manip: 512 catches in 3100 ticks with batches
//     (16.5 per 100), plain 41 in 240 (8.5 per 100) = x1.9 vs plain; <controller>": catches counted the way FishUntil counts
//     them (raw fish in the pack), per rung.
//   - It stops for the hold, with one plain click to restart the natural cycle, when the controller stops (four batches in a
//     row never read; 32 spacings between batches of no regular value: a link whose lateness is all over the place), when the
//     knife or the log is gone, and when the catch rate with batches, after 300 manipulated ticks against a plain baseline of 10
//     catches or more, is under 85% of the plain rate: then it stays off for 20 minutes.
//   - The writes are checked first: the spot must still be there and not a whirlpool (a click read while it is one starts rolls
//     that delete the tool), and nothing else may hold the account's clicks.
//
// A hold that finds the account still fishing from the last one goes on with that hold's schedule when it is fresh; otherwise
// the interaction is ended and the pattern restarted the same way as after a plain window.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/fishmanipplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

const (
	// manipFletchingStat is UPDATE_STAT's index for Fletching.
	manipFletchingStat = 9
	// manipTickWait is the longest a wait for the next tick lasts before the hold's own checks run anyway.
	manipTickWait = 1500 * time.Millisecond
	// manipSettleTicks is how many ticks a hold's end waits for the batch in flight to be read.
	manipSettleTicks = 4
	// manipPlainTicks is how long a plain window lasts: long enough for a net to roll about 16 times.
	manipPlainTicks = 200
	// manipResampleEvery is how many manipulated ticks go between two plain windows.
	manipResampleEvery = 6000
	// manipQuietTicks is how long the varp is left to go stale after the interaction is ended: the natural loop re-arms it five
	// ticks ahead of the clock, so it is below the clock again 6 ticks later, and the batch is written after the seventh.
	manipQuietTicks = 7
	// manipGuardTicks is how many manipulated ticks the yield guard waits for before it judges, manipGuardFish how many plain
	// catches the baseline needs, manipGuardRatio the share of the plain rate the manipulated rate must reach.
	manipGuardTicks = 300
	manipGuardFish  = 10
	manipGuardRatio = 0.85
	// manipDisableFor is how long the yield guard keeps the manipulation off.
	manipDisableFor = 20 * time.Minute
	// manipReportEvery is how many manipulated ticks go between two log lines inside a long call, manipEndReport how many a call
	// must have added since the last line for the call's end to log one (a call is often a minute: a line each would be a flood).
	manipReportEvery = 3000
	manipEndReport   = 300
)

// ManipConfig turns the fishing manipulation on for the FishUntil calls that carry it; State is the account's, kept across
// calls so the measured link delay and the plain baseline survive from hold to hold. A nil Deps.Manip, or Enabled false, is the
// plain click.
type ManipConfig struct {
	Enabled bool
	State   *ManipState
}

// KeepsName reports whether a bank deposit leaves an un-noted item called name in the pack because it is the manipulation's kit:
// the knife, one item. The plain logs are NOT kept: a deposit banks every item of an id, and whatever logs the pack holds (the
// Fletching training draws a whole pack of them) would then never leave it, the pack stays full and the bank phase repeats
// (live, 7 Oct 2026, 05:24: two BassOysterCaskets accounts banked every second). The bank visit deposits the logs and DrawManipKit
// draws the kit's few back. A nil or disabled config keeps nothing.
func (c *ManipConfig) KeepsName(name string) bool {
	if c == nil || !c.Enabled {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(name), woodcutplan.Knife)
}

// ManipState is one account's controller and tallies. Create it once with NewManipState.
type ManipState struct {
	ctl *fishmanipplan.Controller
	run *manipRun
	// rung is the rung the tallies are for; they start over when it changes (a new rung is a new baseline).
	rung string
	// manipFish and manipTicks are the catches and ticks with the pattern running, plainFish and plainTicks those of the plain
	// windows, sampleMark the manipulated ticks when the last plain window began, reportMark when the last line was logged.
	manipFish, manipTicks, plainFish, plainTicks int
	sampleMark, reportMark                       int
	// callFish and callTicks are the manipulated catches and ticks at the start of the FishUntil call in progress, so the line it
	// ends with can say what that call alone yielded.
	callFish, callTicks int
	// spotIdx is the NPC index of the spot the last click of the pattern went to (0: none), so a later FishUntil call at the
	// same spot goes on with the schedule instead of starting it afresh (manipResumable).
	spotIdx       int
	disabledUntil time.Time
	warnedKit     bool
	now           func() time.Time
	// nameFn and spotOK are handed to every run: a test's stand-ins for the config's object names and the NPC checks.
	nameFn func(id int32) (string, bool, bool)
	spotOK func() bool
}

var monoBase = time.Now()

func nowMS() int64 { return time.Since(monoBase).Milliseconds() }

// NewManipState returns a state with the fleet's controller settings.
func NewManipState() *ManipState {
	ctl := fishmanipplan.New(fishmanipplan.DefaultSettings())
	ctl.SetClock(nowMS)
	return &ManipState{ctl: ctl, now: time.Now}
}

// Controller is the state's controller, for a tool that wants its statistics.
func (s *ManipState) Controller() *fishmanipplan.Controller { return s.ctl }

func (s *ManipState) resetTallies() {
	s.manipFish, s.manipTicks, s.plainFish, s.plainTicks, s.sampleMark, s.reportMark = 0, 0, 0, 0, 0, 0
}

// Rates are catches per 100 ticks.
func (s *ManipState) manipRate() float64 { return rate(s.manipFish, s.manipTicks) }
func (s *ManipState) plainRate() float64 { return rate(s.plainFish, s.plainTicks) }

func rate(fish, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return 100 * float64(fish) / float64(ticks)
}

// ---- the tick feed ----

// tickWatch queues one observation per PLAYER_INFO, sampled at the moment the state announces the tick (the animation the
// packet carried is applied by then), so a loop that is a few ticks behind still sees every tick.
type tickWatch struct {
	mu     sync.Mutex // the queue is filled on the read loop and drained on the hold loop
	q      []fishmanipplan.Obs
	wake   chan struct{}
	cancel func()
}

func newTickWatch(st *gamestate.State) *tickWatch {
	w := &tickWatch{wake: make(chan struct{}, 1)}
	w.cancel = st.Subscribe(func(e gamestate.Event) {
		if e.Kind != gamestate.EventTick {
			return
		}
		anim := fishmanipplan.NoAnim
		if seq, at := st.SelfAnim(); at == e.Tick {
			anim = seq
		}
		w.mu.Lock()
		w.q = append(w.q, fishmanipplan.Obs{N: e.Tick, Anim: anim})
		if len(w.q) > 64 {
			w.q = w.q[len(w.q)-64:]
		}
		w.mu.Unlock()
		select {
		case w.wake <- struct{}{}:
		default:
		}
	})
	return w
}

func (w *tickWatch) stop() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}

func (w *tickWatch) pop() (fishmanipplan.Obs, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.q) == 0 {
		return fishmanipplan.Obs{}, false
	}
	o := w.q[0]
	w.q = w.q[1:]
	return o, true
}

// next waits up to timeout for an observation. stopped is true when d.Delay says the caller should stop.
func (w *tickWatch) next(d *Deps, timeout time.Duration) (obs fishmanipplan.Obs, ok, stopped bool) {
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if o, have := w.pop(); have {
			return o, true, false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fishmanipplan.Obs{}, false, false
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(remaining)
		select {
		case <-w.wake:
		case <-timer.C:
		}
		if !d.Delay(0) {
			return fishmanipplan.Obs{}, false, true
		}
	}
}

// ---- the kit ----

// manipItems is what a batch uses: the carrier log and the knife, with their pack slots.
type manipItems struct {
	carrier   chopmanipplan.Carrier
	logID     int32
	logSlot   int
	knifeID   int32
	knifeSlot int
}

// pickManipItems chooses the carrier and finds the knife among the pack's items. name reads an object type: its display
// name, whether it is a bank note, and whether it exists.
func pickManipItems(items []gamestate.SlotItem, name func(id int32) (string, bool, bool), fletching int) (manipItems, bool) {
	held := map[string]int{}
	firstSlot := map[string]gamestate.SlotItem{}
	var knife gamestate.SlotItem
	haveKnife := false
	for _, it := range items {
		n, noted, ok := name(it.ID)
		if !ok || noted {
			continue
		}
		switch {
		case strings.EqualFold(n, woodcutplan.Knife):
			if !haveKnife {
				knife, haveKnife = it, true
			}
		case woodcutplan.IsLogs(n):
			held[n]++
			if _, seen := firstSlot[n]; !seen {
				firstSlot[n] = it
			}
		}
	}
	if !haveKnife {
		return manipItems{}, false
	}
	car, ok := chopmanipplan.PickCarrier(held, fletching)
	if !ok {
		return manipItems{}, false
	}
	var log gamestate.SlotItem
	for n, it := range firstSlot {
		if strings.EqualFold(n, car.Logs) {
			log = it
		}
	}
	return manipItems{carrier: car, logID: log.ID, logSlot: log.Slot, knifeID: knife.ID, knifeSlot: knife.Slot}, true
}

// ManipLogsWanted is how many plain logs the kit holds (the bot's kit ask and the bank draw both aim at it).
const ManipLogsWanted = 1

// ManipLogsHeld is the plain, un-noted logs in the pack.
func ManipLogsHeld(d *Deps) int {
	n := 0
	for _, it := range cycleItems(d) {
		if !it.Noted && strings.EqualFold(strings.TrimSpace(it.Name), chopmanipplan.PlainLogs) {
			n++
		}
	}
	return n
}

// DrawManipKit draws a knife and plain logs from an open bank until the pack holds the kit (one knife, ManipLogsWanted logs),
// when the manipulation is on and the bank has them. It reports whether it drew anything. The bank is where an account's own
// kit comes from first; the mule's hand-off ask covers an account whose bank has none.
func DrawManipKit(d *Deps, bank *actions.Bank) bool {
	if d.Manip == nil || !d.Manip.Enabled {
		return false
	}
	// The knife and the plain log are drawn in ONE burst (twopertick-1012): two Withdraws, confirmed once, where each
	// was a Withdraw-X that waited for its count dialog and then for its items.
	var ops []actions.BankOp
	if !KnifeHeld(d) {
		if ids := bank.RowIDsByName(d.Config, []string{woodcutplan.Knife}); len(ids) > 0 {
			ops = append(ops, actions.WithdrawOp(ids[0], 1))
		}
	}
	if have := ManipLogsHeld(d); have < ManipLogsWanted {
		if ids := bank.RowIDsByName(d.Config, []string{chopmanipplan.PlainLogs}); len(ids) > 0 {
			ops = append(ops, actions.WithdrawOp(ids[0], int32(ManipLogsWanted-have)))
		}
	}
	if len(ops) == 0 {
		return false
	}
	r, retried := bank.Visit(ops, Waiter(d), actions.BatchOptions{})
	d.logf("%s", actions.VisitLog("fish manip kit", len(ops), r, retried))
	drew := r.Err == nil && len(r.Short) < len(ops)
	if drew {
		d.logf("fish manip: drew the kit from the bank (knife held %v, %d plain logs held)", KnifeHeld(d), ManipLogsHeld(d))
	}
	return drew
}

// HasManipKit reports whether the pack holds a knife and a log the Make 1 button can use (a plain log, or one whose
// shortbow the account's Fletching level allows).
func HasManipKit(d *Deps) bool {
	_, ok := (&manipRun{d: d, src: d.Session.State()}).items()
	return ok
}

// ---- one hold ----

type manipMode int

const (
	// modeManip: the controller writes.
	modeManip manipMode = iota
	// modePlain: a plain window; the natural cycle runs and its catches are counted.
	modePlain
	// modeQuiet: the interaction was ended on purpose and the varp is going stale; the opening batch follows.
	modeQuiet
	// modeOff: this hold is plain from here (stopped, kit gone) and is not counted.
	modeOff
)

// manipRun is the manipulation of one hold.
type manipRun struct {
	d     *Deps
	st    *ManipState
	src   *gamestate.State
	watch *tickWatch
	op    int
	spot  gamestate.NPC
	mode  manipMode
	// mark is the last tick counted into the tallies; until is the tick a plain window or a quiet spell ends after.
	mark, until int
	wrote       int
	// nameFn and spotOK let a test stand in for the config's object names and for the NPC checks.
	nameFn func(id int32) (string, bool, bool)
	spotOK func() bool
}

func (r *manipRun) name(id int32) (string, bool, bool) {
	if r.nameFn != nil {
		return r.nameFn(id)
	}
	obj, ok := r.d.Config.Obj(int(id))
	if !ok {
		return "", false, false
	}
	return obj.Name, obj.Noted(), true
}

// items reads the pack and the Fletching level.
func (r *manipRun) items() (manipItems, bool) {
	inv := r.src.Inventory(PlayerPack)
	if inv == nil || (r.d.Config == nil && r.nameFn == nil) {
		return manipItems{}, false
	}
	level := 0
	if sk, ok := r.src.Skill(manipFletchingStat); ok {
		level = sk.Level
	}
	return pickManipItems(inv.NonEmpty(), r.name, level)
}

func manipOn(d *Deps) bool {
	return d != nil && d.Manip != nil && d.Manip.Enabled && d.Session != nil && d.Session.State() != nil
}

// activeRun is the hold's run while it is still counting or manipulating.
func activeRun(d *Deps) *manipRun {
	if !manipOn(d) || d.Manip.State == nil {
		return nil
	}
	if r := d.Manip.State.run; r != nil && r.mode != modeOff {
		return r
	}
	return nil
}

func (r *manipRun) detach() {
	if r.watch != nil {
		r.watch.stop()
		r.watch = nil
	}
	if r.st != nil && r.st.run == r {
		r.st.run = nil
	}
}

// startManip attaches a new run to the state, replacing a finished one. It is nil when the manipulation is off for the
// account.
func startManip(d *Deps, op int, spot gamestate.NPC) *manipRun {
	if !manipOn(d) {
		return nil
	}
	st := d.Manip.State
	if st == nil {
		st = NewManipState()
		d.Manip.State = st
	}
	if st.run != nil {
		st.run.detach()
	}
	src := d.Session.State()
	r := &manipRun{d: d, st: st, src: src, op: op, spot: spot, mark: src.Tick(), nameFn: st.nameFn, spotOK: st.spotOK}
	st.run = r
	return r
}

// manipBegin tells the state which rung the call fishes: its tallies are for one rung.
func manipBegin(d *Deps, rungLabel string) {
	if !manipOn(d) {
		return
	}
	if d.Manip.State == nil {
		d.Manip.State = NewManipState()
	}
	if st := d.Manip.State; st.rung != rungLabel {
		st.rung = rungLabel
		st.resetTallies()
	}
	d.Manip.State.callFish, d.Manip.State.callTicks = d.Manip.State.manipFish, d.Manip.State.manipTicks
}

// manipClick is a hold's first click on the spot: the opening batch when the plain window is done and the kit is held, the plain
// click otherwise (and the start of a plain window when one is due). It replaces the plain OpNpc and returns the same error.
func manipClick(d *Deps, op int, spot gamestate.NPC) error {
	if r := startManip(d, op, spot); r != nil {
		switch r.begin() {
		case modeManip:
			return nil
		case modeOff:
			r.detach()
		}
	}
	// The plain click goes through the click gate (internal/actions/clickgate.go): the same spot is not clicked more than once
	// per actions.ClickGapTicks game ticks, however FishUntil's re-find loop comes round (a hold that ends in a tick or two and
	// re-clicks the spot it just clicked, with a chat or a whirlpool guard in the way).
	return actions.GatedClick(d.Session, Waiter(d), actions.NpcKey(spot.Index), nil, func() error {
		return actions.OpNpc(d.Session, op, spot.Index)
	})
}

// begin decides how the hold starts. It returns modeManip when the opening batch was written (the click is done), modePlain when
// the caller's plain click starts a plain window, and modeOff when the hold is plain and uncounted.
func (r *manipRun) begin() manipMode {
	st := r.st
	if st.now().Before(st.disabledUntil) {
		r.mode = modeOff
		return modeOff
	}
	if _, ok := r.items(); !ok {
		if !st.warnedKit {
			st.warnedKit = true
			r.d.logf("fish manip: no knife, or no log the Make 1 button can use (a plain log, or one whose shortbow the Fletching level allows), so this fishing is the plain click")
		}
		r.mode = modeOff
		return modeOff
	}
	if st.plainTicks < manipPlainTicks || st.manipTicks-st.sampleMark >= manipResampleEvery {
		r.startPlain()
		return modePlain
	}
	n := r.src.Tick()
	if st.ctl.Begin(n) {
		r.watch = newTickWatch(r.src) // before the write, so the tick the batch is read in is not missed
		if r.write(fishmanipplan.Batch, n) {
			r.mode, r.mark = modeManip, n
			return modeManip
		}
		r.watch.stop()
		r.watch = nil
	}
	r.mode = modeOff
	return modeOff
}

// startPlain begins a plain window: the ticks and catches that follow are the baseline.
func (r *manipRun) startPlain() {
	r.mode = modePlain
	r.mark = r.src.Tick()
	r.until = r.mark + manipPlainTicks
	r.st.sampleMark = r.st.manipTicks
}

// manipResumable reports whether the pattern is still running on spot from an earlier FishUntil call: its last batch is fresh, it
// went to this very spot, the kit is held and no plain window is due. A FishUntil call is often half a minute (five fish), and a
// hold that restarts the pattern at every call with a fresh opening batch loses about a roll in ten (live, 7 Oct 2026: Big net
// 17.5 catches per 100 ticks against the model's 20). Such a call takes the schedule over (manipResume) and writes no click.
func manipResumable(d *Deps, spot gamestate.NPC) bool {
	if !manipOn(d) || d.Manip.State == nil {
		return false
	}
	st := d.Manip.State
	if st.spotIdx == 0 || st.spotIdx != spot.Index || st.now().Before(st.disabledUntil) || st.ctl.Stopped() {
		return false
	}
	if st.plainTicks < manipPlainTicks || st.manipTicks-st.sampleMark >= manipResampleEvery {
		return false
	}
	r := &manipRun{d: d, st: st, src: d.Session.State(), nameFn: st.nameFn, spotOK: st.spotOK}
	if _, ok := r.items(); !ok {
		return false
	}
	return st.ctl.Resume(r.src.Tick())
}

// manipResume re-attaches the controller to a hold that begins while the account is still fishing from the last one: no click is
// written, and the pattern goes on from the batch last written when that is fresh. When it is not, the interaction is ended and
// the pattern restarted the way it is after a plain window.
func manipResume(d *Deps, op int, spot gamestate.NPC) {
	if !manipOn(d) || d.Manip.State == nil {
		return
	}
	st := d.Manip.State
	if st.now().Before(st.disabledUntil) {
		return
	}
	src := d.Session.State()
	if st.run != nil {
		st.run.detach()
	}
	r := &manipRun{d: d, st: st, src: src, op: op, spot: spot, mark: src.Tick(), nameFn: st.nameFn, spotOK: st.spotOK}
	if _, ok := r.items(); !ok {
		return
	}
	if st.ctl.Continue(src.Tick()) {
		r.mode = modeManip
		r.watch = newTickWatch(src)
	} else if st.plainTicks < manipPlainTicks {
		r.startPlain()
	} else {
		r.quiesce()
	}
	st.run = r
}

// quiesce ends the interaction with a click on the own tile and leaves the varp to go stale: the natural loop cannot be joined
// at an arbitrary tick (the batch needs a stale varp), but a dead interaction can.
func (r *manipRun) quiesce() {
	x, z, level, ok := r.src.Tile()
	if ok {
		_ = actions.Walk(r.d.Session, []pathfind.NavPoint{{X: x, Z: z, Level: level}}, false)
	}
	r.mode = modeQuiet
	r.until = r.src.Tick() + manipQuietTicks
}

// reopen writes the opening batch after a quiet spell.
func (r *manipRun) reopen() {
	n := r.src.Tick()
	if !r.st.ctl.Begin(n) {
		// A batch from before is still in flight: another tick, and the plain click if it never settles.
		if n > r.until+8 {
			r.fallback("a batch is still in flight")
		}
		return
	}
	if r.watch == nil {
		r.watch = newTickWatch(r.src) // before the write, so the tick the batch is read in is not missed
	}
	if r.write(fishmanipplan.Batch, n) {
		r.mode, r.mark = modeManip, n
		return
	}
	r.fallback("the opening batch could not be written")
}

// write sends one batch or lone click after tick n, reporting whether it went out.
func (r *manipRun) write(a fishmanipplan.Action, n int) bool {
	if actions.MovementHeld(r.d.Session) {
		// Another caller (a random event's solver) holds this account's clicks; the plain click is dropped for the same reason.
		return false
	}
	if !r.spotUsable() {
		// The hold's own checks end it a poll later; a click read while the spot is a whirlpool would start rolls on it.
		return false
	}
	switch a {
	case fishmanipplan.Lone:
		if err := actions.OpNpc(r.d.Session, r.op, r.spot.Index); err != nil {
			return false
		}
		r.st.ctl.Sent(fishmanipplan.Lone, n)
		r.st.spotIdx = r.spot.Index
		return true
	case fishmanipplan.Batch:
		bi, ok := r.items()
		if !ok {
			r.fallback("the knife or the log is gone")
			return false
		}
		b := actions.NewCoalescer(r.d.Session)
		sess := b.Session()
		if err := actions.OpHeldU(sess, bi.logID, bi.logSlot, PlayerPack, bi.knifeID, bi.knifeSlot, PlayerPack); err != nil {
			return false
		}
		if err := actions.IfButton(sess, bi.carrier.MakeComp); err != nil {
			return false
		}
		// The CLOSE_MODAL is what cancels the fletch the Make 1 queued, whatever becomes of the click: the engine runs closeModal
		// (which clears the weak queue) at the top of the player's queue step, before the weak queue runs.
		if err := actions.CloseModal(sess); err != nil {
			return false
		}
		if err := actions.OpNpc(sess, r.op, r.spot.Index); err != nil {
			return false
		}
		if b.Pending() != 4 {
			b.Discard()
			return false
		}
		if err := b.Flush(); err != nil {
			r.d.logf("fish manip: writing a batch failed: %v", err)
			return false
		}
		r.st.ctl.Sent(fishmanipplan.Batch, n)
		r.st.spotIdx = r.spot.Index
		r.wrote++
		return true
	}
	return false
}

func (r *manipRun) spotUsable() bool {
	if r.spotOK != nil {
		return r.spotOK()
	}
	return npcStillThere(r.d, r.spot) && !randomevents.WhirlpoolNearby(r.src, r.spot.X, r.spot.Z, whirlpoolAvoidRadius)
}

// fallback ends the hold's manipulation: one plain click restarts the natural fishing the pattern left dead.
func (r *manipRun) fallback(why string) {
	if r.mode == modeOff {
		return
	}
	r.account()
	r.mode = modeOff
	if r.watch != nil {
		r.watch.stop()
		r.watch = nil
	}
	if r.spotUsable() {
		r.d.logf("fish manip: stopping for this hold (%s); one plain click and the spot is fished the ordinary way", why)
		_ = actions.OpNpc(r.d.Session, r.op, r.spot.Index)
	} else {
		r.d.logf("fish manip: stopping for this hold (%s); the spot is gone or a whirlpool, so no click", why)
	}
	r.st.report(r.d, true)
}

// account adds the ticks since the last call to the tallies of the mode the run was in.
func (r *manipRun) account() {
	n := r.src.Tick()
	if n <= r.mark {
		return
	}
	switch r.mode {
	case modeManip:
		r.st.manipTicks += n - r.mark
	case modePlain:
		r.st.plainTicks += n - r.mark
	}
	r.mark = n
}

// wait is one pass of the hold's wait, by mode. It reports false when the caller asked to stop.
func (r *manipRun) wait(poll time.Duration) bool {
	if r.mode == modeManip {
		stopped := r.pace()
		r.account()
		r.guard()
		r.st.report(r.d, false)
		return !stopped
	}
	ok := r.d.Delay(poll)
	r.account()
	switch r.mode {
	case modePlain:
		if r.src.Tick() >= r.until {
			r.quiesce()
		}
	case modeQuiet:
		if r.src.Tick() >= r.until {
			r.reopen()
		}
	}
	return ok
}

// guard is the yield check: with the pattern running long enough against a baseline long enough, a rate under manipGuardRatio of
// the plain one turns the manipulation off for manipDisableFor.
func (r *manipRun) guard() {
	st := r.st
	if r.mode != modeManip || st.manipTicks < manipGuardTicks || st.plainTicks < manipPlainTicks || st.plainFish < manipGuardFish {
		return
	}
	if st.manipRate() >= manipGuardRatio*st.plainRate() {
		return
	}
	st.disabledUntil = st.now().Add(manipDisableFor)
	r.fallback(fmt.Sprintf("the catch rate with batches, %.1f per 100 ticks over %d ticks, is under %.0f%% of the plain rate, %.1f per 100 over %d; off for %s",
		st.manipRate(), st.manipTicks, 100*manipGuardRatio, st.plainRate(), st.plainTicks, manipDisableFor))
}

// pace waits for the next PLAYER_INFO, tells the controller what it carried and writes what it asks for: the replacement for
// the hold's fixed poll. It reports true when the caller asked to stop.
func (r *manipRun) pace() (stopped bool) {
	if r.watch == nil {
		r.watch = newTickWatch(r.src)
	}
	obs, ok, stopped := r.watch.next(r.d, manipTickWait)
	if stopped {
		return true
	}
	if !ok {
		return false
	}
	for {
		act := r.st.ctl.Observe(obs)
		if r.st.ctl.Stopped() {
			r.fallback(r.st.ctl.StopReason())
			return false
		}
		if act != fishmanipplan.None && r.src.Tick() == obs.N {
			// Only an observation that is still the newest is worth a write: one a tick or more old would be read after the
			// tick it was meant for.
			r.write(act, obs.N)
			if r.mode == modeOff {
				return false
			}
		}
		var have bool
		if obs, have = r.watch.pop(); !have {
			break
		}
	}
	return false
}

// settle lets the batch in flight be read: the hold has ended and something else is about to write. It waits up to
// manipSettleTicks ticks for the fletch animation (or for the controller to call the batch lost) and writes nothing.
func (r *manipRun) settle() {
	if r.watch == nil || !r.st.ctl.InFlight() {
		return
	}
	deadline := time.Now().Add(time.Duration(manipSettleTicks) * 700 * time.Millisecond)
	for r.st.ctl.InFlight() && time.Now().Before(deadline) {
		obs, ok, stopped := r.watch.next(r.d, 700*time.Millisecond)
		if stopped {
			return
		}
		if ok {
			r.st.ctl.Observe(obs)
		}
	}
}

// ---- what FishUntil calls ----

// manipWait replaces the hold's wait of poll: while a run is counting or manipulating it is a pass of the run's wait, otherwise the
// plain delay. It reports false when the caller asked to stop.
func manipWait(d *Deps, poll time.Duration) bool {
	if r := activeRun(d); r != nil {
		return r.wait(poll)
	}
	return d.Delay(poll)
}

// manipAlive reports whether the manipulation says the hold is fishing: a batch was confirmed a few ticks ago, or the run is in the
// quiet spell between a plain window and the pattern (nothing is rolling for a few ticks on purpose). A plain window is the ordinary
// fishing, which fishingActive judges as it always did, so the hold still ends when the interaction dies in it. A net's roll plays no
// animation of its own under the pattern, so fishingActive alone would read a manipulated hold as dead.
func manipAlive(d *Deps) bool {
	r := activeRun(d)
	if r == nil {
		return false
	}
	switch r.mode {
	case modeManip:
		return r.st.ctl.InFlight() || r.st.ctl.Alive(r.src.Tick()) // a batch not yet read is not a dead hold
	case modeQuiet:
		return true
	}
	return false
}

// manipCatch tallies fish caught while a run is counting.
func manipCatch(d *Deps, n int) {
	r := activeRun(d)
	if r == nil || n <= 0 {
		return
	}
	switch r.mode {
	case modeManip:
		r.st.manipFish += n
	case modePlain:
		r.st.plainFish += n
	}
}

// manipSettle is called at the top of FishUntil's outer loop, between holds: the batch of the hold that just ended is read
// before anything else is written (the drop cycle's clicks share the five user events a tick).
func manipSettle(d *Deps) {
	// A full pack has stopped the fishing (content's "You can't carry any more fish." mesbox): no batch in
	// flight will land, so waiting for it only delays the drop cycle that frees the pack.
	if d.Session != nil && FreeSlots(d) == 0 {
		return
	}
	if manipOn(d) && d.Manip.State != nil && d.Manip.State.run != nil {
		d.Manip.State.run.settle()
	}
}

// manipEnd ends the manipulation when FishUntil returns: the tick feed is detached and what was tallied is reported.
func manipEnd(d *Deps) {
	if !manipOn(d) || d.Manip.State == nil {
		return
	}
	st := d.Manip.State
	if st.run != nil {
		st.run.account()
		st.run.detach()
	}
	if st.manipTicks-st.reportMark >= manipEndReport {
		st.report(d, true)
	}
}

// report logs the tallies: forced (a hold stopped, or a call ended with manipEndReport ticks unreported) or every manipReportEvery
// manipulated ticks inside a call.
func (s *ManipState) report(d *Deps, force bool) {
	if s.manipTicks == 0 || (!force && s.manipTicks-s.reportMark < manipReportEvery) {
		return
	}
	s.reportMark = s.manipTicks
	d.logf("fish manip: %s", s.Line())
}

func (s *ManipState) rungName() string {
	if s.rung == "" {
		return "rung ?"
	}
	return s.rung
}

// Line is the tallies as one log fragment.
func (s *ManipState) Line() string {
	base := "no plain window yet"
	switch {
	case s.plainTicks >= manipPlainTicks/2 && s.plainFish > 0 && s.manipTicks > 0:
		base = fmt.Sprintf("plain %d in %d ticks (%.1f per 100) = x%.1f vs plain", s.plainFish, s.plainTicks, s.plainRate(), s.manipRate()/s.plainRate())
	case s.plainTicks > 0:
		base = fmt.Sprintf("plain %d in %d ticks (%.1f per 100) = no ratio yet", s.plainFish, s.plainTicks, s.plainRate())
	}
	call := ""
	if dt := s.manipTicks - s.callTicks; dt > 0 {
		call = fmt.Sprintf("; this call %d in %d ticks (%.1f per 100)", s.manipFish-s.callFish, dt, rate(s.manipFish-s.callFish, dt))
	}
	return fmt.Sprintf("%s: %d catches in %d ticks with batches (%.1f per 100), %s%s; %s", s.rungName(), s.manipFish, s.manipTicks, s.manipRate(), base, call, s.ctl.Stats().Line())
}
