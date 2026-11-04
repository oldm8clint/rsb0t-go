package woodcutrunner

// CHOP MANIPULATION: the I/O half of internal/scripts/chopmanipplan.
//
// A chopping account that holds a knife and a log can make a tree roll every 3 ticks instead of every 4 by writing
// [knife on the log, Make 1, CLOSE_MODAL, tree click] in ONE socket write that the engine reads in the stale tick after
// each swing (chopmanipplan's package comment has the engine facts and the file and line of each, and how the
// controller aims the write earlier when the link delivers it late; docs/tick_manipulation_lab_2026-10-06.md and
// docs/wcfletch_chop_manip_2026-10-06.md the lab numbers: +25% to +32% logs).
//
// Nothing here runs unless Deps.Manip says so, and then only inside Chop. Chop keeps every rule it had (the stall
// timeout, the Ent, the dialog, the fallen tree, the full pack); this file only changes WHAT it writes and HOW it waits:
//
//   - the tree click that opens a chop is a batch when the controller wants one and a carrier log and a knife are held;
//   - the 600 ms sleep between checks becomes a wait for the next PLAYER_INFO (the fleet's own EventTick), so a batch is
//     written within microseconds of the tick it follows, which is what gives it the whole of the tick to arrive in;
//   - after each tick the controller is told the tick and the animation the server started in it (the only things the
//     client can read: no ack cheat exists in production) and says whether to write a batch now.
//
// A batch is harmless when it is not useful: read at any other tick it moves no varp, and the click only restarts an
// interaction that was running. The packets that cancel the queued fletch are the CLOSE_MODAL and the tree click; the
// CLOSE_MODAL works whether or not the tree is still there (the engine's OpLocHandler refuses a click on a felled tree
// BEFORE it clears the weak queue, so a batch aimed at the swing that fells the tree would otherwise fletch the carrier).
// The one hazard left, a batch split across the engine's five-events-a-tick limit, needs two batches in flight, and the
// controller never has more than one.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// fletchingStat is UPDATE_STAT's index for Fletching (gamestate/skills.go: ..., Woodcutting 8, Cooking 7, Fletching 9).
const fletchingStat = 9

// manipReportEvery is how many chopping ticks pass between two report lines.
const manipReportEvery = 600

// manipWait bounds one wait for a tick: a PLAYER_INFO arrives every 600 ms, so this is only reached when the
// connection stopped delivering them (Chop's own stall timeout then ends the call).
const manipWait = 1500 * time.Millisecond

// ManipConfig turns the knife-and-log batch on for the Chop calls made with these Deps.
type ManipConfig struct {
	// Enabled is the setting. A nil *ManipConfig, or Enabled false, leaves Chop exactly as it was.
	Enabled bool
	// State is the account's controller and tallies, kept across Chop calls (one per tree). Nil gives each call its own,
	// which forgets the measured delay of the link at every tree.
	State *ManipState
}

// ManipState is one account's chop manipulation: the controller and the figures its log line reports.
type ManipState struct {
	ctl *chopmanipplan.Controller

	// since the last report line
	manipLogs, manipTicks, plainLogs, plainTicks int

	loggedLead  int
	loggedAt    time.Time
	warnedItems bool
}

// monoBase is the clock's origin: the controller's millisecond clock is time since this process read it first, monotonic.
var monoBase = time.Now()

func nowMS() int64 { return time.Since(monoBase).Milliseconds() }

// NewManipState returns a state with the fleet's controller settings and the process clock, so that a batch is only
// called lost after real time has passed as well as ticks (a stalled client receives its missed ticks in a burst).
func NewManipState() *ManipState {
	ctl := chopmanipplan.New(chopmanipplan.DefaultSettings())
	ctl.SetClock(nowMS)
	return &ManipState{ctl: ctl}
}

// Controller is the state's controller (for tests and the panel).
func (s *ManipState) Controller() *chopmanipplan.Controller { return s.ctl }

// note adds one finished Chop call to the tallies.
func (s *ManipState) note(manip bool, logs, ticks int) {
	if ticks < 0 {
		ticks = 0
	}
	if manip {
		s.manipLogs += logs
		s.manipTicks += ticks
		return
	}
	s.plainLogs += logs
	s.plainTicks += ticks
}

// per100 is logs per 100 ticks, 0 when no ticks were spent.
func per100(logs, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return 100 * float64(logs) / float64(ticks)
}

// report logs what the account has done since the last report once enough chopping ticks have passed, and says when the
// controller re-aimed (at most once a minute: it only ever changes its aim, it never pauses or switches itself off).
func (s *ManipState) report(d *Deps) {
	if lead := s.ctl.Lead(); lead != s.loggedLead && time.Since(s.loggedAt) > time.Minute {
		d.logf("chop manip: batches reach the engine %d tick(s) after the tick they follow, so they are now written %d tick(s) before the swing they are aimed at (%s)", lead, lead, s.ctl.Stats().Line())
		s.loggedLead, s.loggedAt = lead, time.Now()
	}
	if s.manipTicks+s.plainTicks < manipReportEvery {
		return
	}
	d.logf("chop manip: %d logs in %d ticks with batches = %.1f per 100 ticks; %d logs in %d ticks without = %.1f per 100 ticks; %s",
		s.manipLogs, s.manipTicks, per100(s.manipLogs, s.manipTicks),
		s.plainLogs, s.plainTicks, per100(s.plainLogs, s.plainTicks), s.ctl.Stats().Line())
	s.manipLogs, s.manipTicks, s.plainLogs, s.plainTicks = 0, 0, 0, 0
}

// ---- the tick feed ----

// tickSource is the part of *gamestate.State the tick feed reads.
type tickSource interface {
	Subscribe(gamestate.Subscriber) (cancel func())
	Tick() int
	SelfAnim() (seq, tick int)
}

// tickWatch queues one observation per PLAYER_INFO, sampled at the moment the state announces the tick (the
// animation the packet carried is applied by then), so a loop that is a few ticks behind still sees every tick.
type tickWatch struct {
	mu     sync.Mutex // the queue is filled on the read loop and drained on the chop loop
	q      []chopmanipplan.Obs
	wake   chan struct{}
	cancel func()
}

func newTickWatch(src tickSource) *tickWatch {
	w := &tickWatch{wake: make(chan struct{}, 1)}
	w.cancel = src.Subscribe(func(e gamestate.Event) {
		if e.Kind != gamestate.EventTick {
			return
		}
		anim := chopmanipplan.NoAnim
		if seq, at := src.SelfAnim(); at == e.Tick {
			anim = seq
		}
		w.mu.Lock()
		w.q = append(w.q, chopmanipplan.Obs{N: e.Tick, Anim: anim})
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

// stop detaches the watch from the state.
func (w *tickWatch) stop() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}

// pop takes the oldest unread observation.
func (w *tickWatch) pop() (chopmanipplan.Obs, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.q) == 0 {
		return chopmanipplan.Obs{}, false
	}
	o := w.q[0]
	w.q = w.q[1:]
	return o, true
}

// next waits up to timeout for an observation. stopped is true when d.Delay says the caller should stop.
func (w *tickWatch) next(d *Deps, timeout time.Duration) (obs chopmanipplan.Obs, ok, stopped bool) {
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if o, have := w.pop(); have {
			return o, true, false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return chopmanipplan.Obs{}, false, false
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
			return chopmanipplan.Obs{}, false, true
		}
	}
}

// ---- the batch ----

// batchItems is what one batch uses: the carrier log and the knife, with their pack slots.
type batchItems struct {
	carrier   chopmanipplan.Carrier
	logID     int32
	logSlot   int
	knifeID   int32
	knifeSlot int
}

// itemName reads an object type: its display name, whether it is a bank note, and whether it exists.
type itemName func(id int32) (name string, noted, ok bool)

// pickBatchItems chooses the carrier and finds the knife among the pack's items.
func pickBatchItems(items []gamestate.SlotItem, name itemName, fletching int) (batchItems, bool) {
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
		return batchItems{}, false
	}
	car, ok := chopmanipplan.PickCarrier(held, fletching)
	if !ok {
		return batchItems{}, false
	}
	var log gamestate.SlotItem
	for n, it := range firstSlot {
		if strings.EqualFold(n, car.Logs) {
			log = it
		}
	}
	return batchItems{carrier: car, logID: log.ID, logSlot: log.Slot, knifeID: knife.ID, knifeSlot: knife.Slot}, true
}

// ---- one Chop call ----

// manipRun is the manipulation of one Chop call.
type manipRun struct {
	d         *Deps
	st        *ManipState
	m         locindex.Match
	src       *gamestate.State
	watch     *tickWatch
	name      itemName
	startTick int
	wrote     int
}

// startManip returns the call's manipulation, or nil when Deps.Manip leaves it off.
func startManip(d *Deps, m locindex.Match) *manipRun {
	if d.Manip == nil || !d.Manip.Enabled || d.Session == nil {
		return nil
	}
	src := d.Session.State()
	if src == nil {
		return nil
	}
	st := d.Manip.State
	if st == nil {
		st = NewManipState()
	}
	r := &manipRun{d: d, st: st, m: m, src: src, watch: newTickWatch(src), startTick: src.Tick()}
	r.name = func(id int32) (string, bool, bool) {
		obj, ok := d.Config.Obj(int(id))
		if !ok {
			return "", false, false
		}
		return obj.Name, obj.Noted(), true
	}
	return r
}

// batchItems reads the pack and the Fletching level.
func (r *manipRun) items() (batchItems, bool) {
	inv := r.src.Inventory(PlayerPack)
	if inv == nil {
		return batchItems{}, false
	}
	level := 0
	if sk, ok := r.src.Skill(fletchingStat); ok {
		level = sk.Level
	}
	return pickBatchItems(inv.NonEmpty(), r.name, level)
}

// write sends one batch after tick n (the tick the client had seen when it wrote), reporting whether it went out.
func (r *manipRun) write(n int) bool {
	if actions.MovementHeld(r.d.Session) {
		// Another caller (a random event's solver) holds this account's clicks; the plain click is dropped for the same
		// reason, so a batch must not carry one past the hold.
		return false
	}
	bi, ok := r.items()
	if !ok {
		if !r.st.warnedItems {
			r.st.warnedItems = true
			r.d.logf("chop manip: no knife, or no log the Make 1 button can use (a plain log, or one whose shortbow the Fletching level allows), so this chop is a plain click")
		}
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
	// The CLOSE_MODAL is what cancels the fletch the Make 1 queued, whatever becomes of the tree click: the engine runs
	// closeModal (which clears the weak queue) at the top of the player's queue step, before the weak queue runs, and a
	// click on a tree that has just fallen is refused before it clears anything.
	if err := actions.CloseModal(sess); err != nil {
		return false
	}
	if err := actions.OpLoc(sess, r.m.Op, r.m.X, r.m.Z, r.m.ID); err != nil {
		return false
	}
	if b.Pending() != 4 {
		// An action that queued nothing (a click dropped by a hold) would leave a knife and a Make 1 without their
		// cancelling packets.
		b.Discard()
		return false
	}
	if err := b.Flush(); err != nil {
		r.d.logf("chop manip: writing a batch failed: %v", err)
		return false
	}
	r.st.ctl.Sent(n)
	r.wrote++
	return true
}

// open writes the chop's opening click as a batch when the controller wants one, and reports whether it did; false
// means the caller sends the plain click it always sent.
func (r *manipRun) open() bool {
	n := r.src.Tick()
	if !r.st.ctl.BeginChop(n) {
		return false
	}
	return r.write(n)
}

// pace waits for the next PLAYER_INFO, tells the controller what it carried and writes the batch it asks for. It is the
// replacement for Chop's 600 ms sleep, and returns true when the caller asked to stop.
func (r *manipRun) pace() (stopped bool) {
	obs, ok, stopped := r.watch.next(r.d, manipWait)
	if stopped {
		return true
	}
	if !ok {
		return false
	}
	for {
		if r.st.ctl.Observe(obs) && r.src.Tick() == obs.N {
			// Only an observation that is still the newest is worth a batch: one a tick or more old would be read after
			// the stale tick it was meant for.
			r.write(obs.N)
		}
		var have bool
		if obs, have = r.watch.pop(); !have {
			return false
		}
	}
}

// finish ends the call's manipulation: detaches the tick feed and adds the call to the account's tallies.
func (r *manipRun) finish(gained *int) {
	r.watch.stop()
	ticks := r.src.Tick() - r.startTick
	r.st.note(r.wrote > 0, *gained, ticks)
	r.st.report(r.d)
}
