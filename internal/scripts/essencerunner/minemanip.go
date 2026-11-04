package essencerunner

// ESSENCE MINING MANIPULATION: the I/O half of internal/scripts/minemanipplan (the content facts, the numbers and the model of
// the mining script are there; the controller is internal/scripts/chopmanipplan's).
//
// A miner that holds a knife and a log can make the rock roll every 3 ticks instead of every pickaxe-rate-plus-one by writing
// [knife on the log, Make 1, CLOSE_MODAL, rock click] in ONE socket write that the engine reads in the stale tick after each
// roll (mining.rs2 re-arms %action_delay to +rate there; cut_logs.rs2's process_fletch_logs gets to it first and arms +2).
// Bronze goes from one essence in 8 ticks to one in 3, iron 7, steel 6, mithril 5, adamant 4; rune is 3 already and never runs
// this. Nothing here runs unless Deps.Manip says so, and then only inside Mine, and only for a pickaxe that gains and a pack that
// holds the kit. Mine keeps every rule it had (the click gate, the chat policy, the stall timeout, the full pack); this file
// changes WHAT the rock click is and HOW the loop waits:
//
//   - the click that opens a mine is a batch when the controller wants one;
//   - the 600 ms sleep becomes a wait for the next PLAYER_INFO, so a batch is written within microseconds of the tick it follows;
//   - the controller is told each tick, the animation the server started in it (the fletching animation confirms a batch) and
//     whether the Mining experience moved in it. THE EXPERIENCE, NOT THE PICKAXE ANIMATION, IS THE ROLL: every essence roll earns
//     exactly one XP update, whereas the animation is also played by the first swing of an unprimed click (no roll) and is
//     hidden by a batch read in the roll tick. The animation-driven controller lost 5 ticks per level-up in the model.
//
// A level-up chat opens in the tick AFTER the roll that earned it and blocks the interaction until it is closed. The batch read
// in that tick has already moved the varp, so the hold's Continue (read a tick later) costs the run nothing. The click the hold
// would send once the chat is gone is NOT sent in a batch run: the interrupted interaction carries on by itself, and a batch now
// would find the varp armed and move nothing. The loop waits resumeTicks for a roll and only then clicks (a batch when the
// controller allows it), in case the interaction did not survive the chat.
//
// A batch is harmless when it is not useful: read in any tick but the stale one it moves no varp, and the click only restarts
// the interaction. One batch is ever in flight (the controller's rule), a batch is four user events and the engine reads five a
// tick, and a batch is held back while the click gate says the rock was clicked less than actions.ClickGapTicks ago.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/minemanipplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// fletchingStat is UPDATE_STAT's index for Fletching (gamestate/skills.go).
const fletchingStat = 9

// manipReportEvery is how many mining ticks pass between two report lines.
const manipReportEvery = 600

// manipWait bounds one wait for a tick: a PLAYER_INFO arrives every 600 ms, so this is only reached when the connection stopped
// delivering them (Mine's own stall timeout then ends the call).
const manipWait = 1500 * time.Millisecond

// resumeTicks is how long a batch run waits, after an answered chat has gone, for the interaction the chat interrupted to roll
// again before it clicks the rock itself. A roll is due within 3 ticks of the batch read before the chat; this leaves room for
// a slow Continue.
const resumeTicks = 6

// ManipConfig turns the knife-and-log batch on for the Mine calls made with these Deps.
type ManipConfig struct {
	// Enabled is the setting. A nil *ManipConfig, or Enabled false, leaves Mine exactly as it was.
	Enabled bool
	// State is the account's controller and tallies, kept across Mine calls. Nil gives each call its own, which forgets the
	// measured delay of the link at every load.
	State *ManipState
}

// ManipState is one account's mining manipulation: the controller and the figures its log line reports.
type ManipState struct {
	ctl    *chopmanipplan.Controller
	period int // the pickaxe period the controller was built for

	// since the last report line
	manipEssence, manipTicks, plainEssence, plainTicks int

	loggedLead   int
	loggedAt     time.Time
	warnedItems  bool
	warnedFloor  bool
	warnedNoPick bool
}

// monoBase is the clock's origin: the controller's millisecond clock is time since this process read it first, monotonic.
var monoBase = time.Now()

func nowMS() int64 { return time.Since(monoBase).Milliseconds() }

// NewManipState returns an empty state; its controller is built for the pickaxe in use at the first Mine call.
func NewManipState() *ManipState { return &ManipState{} }

// Controller is the state's controller (for tests and the panel); nil until a Mine call has built one.
func (s *ManipState) Controller() *chopmanipplan.Controller { return s.ctl }

// controller returns the state's controller for the pickaxe, built with the process clock so a batch is only called lost after
// real time has passed as well as ticks. A different pickaxe (an upgrade) is a different period and gets a new one.
func (s *ManipState) controller(p minemanipplan.Pickaxe) *chopmanipplan.Controller {
	if s.ctl == nil || s.period != p.Period() {
		set := minemanipplan.ControllerSettings(p)
		s.ctl = chopmanipplan.New(set)
		s.ctl.SetClock(nowMS)
		s.period, s.loggedLead = p.Period(), 0
	}
	return s.ctl
}

// note adds one finished Mine call to the tallies.
func (s *ManipState) note(manip bool, essence, ticks int) {
	if ticks < 0 {
		ticks = 0
	}
	if manip {
		s.manipEssence += essence
		s.manipTicks += ticks
		return
	}
	s.plainEssence += essence
	s.plainTicks += ticks
}

// per100 is essence per 100 ticks, 0 when no ticks were spent.
func per100(n, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return 100 * float64(n) / float64(ticks)
}

// report logs what the account has done since the last report once enough mining ticks have passed, and says when the controller
// re-aimed (at most once a minute: it only ever changes its aim, it never pauses or switches itself off).
func (s *ManipState) report(d *Deps) {
	if s.ctl == nil {
		return
	}
	if lead := s.ctl.Lead(); lead != s.loggedLead && time.Since(s.loggedAt) > time.Minute {
		d.logf("mine manip: batches reach the engine %d tick(s) after the tick they follow, so they are now written %d tick(s) before the roll they are aimed at (%s)", lead, lead, s.ctl.Stats().Line())
		s.loggedLead, s.loggedAt = lead, time.Now()
	}
	if s.manipTicks+s.plainTicks < manipReportEvery {
		return
	}
	d.logf("mine manip: %d essence in %d ticks with batches = %.1f per 100 ticks; %d essence in %d ticks without = %.1f per 100 ticks; %s",
		s.manipEssence, s.manipTicks, per100(s.manipEssence, s.manipTicks),
		s.plainEssence, s.plainTicks, per100(s.plainEssence, s.plainTicks), s.ctl.Stats().Line())
	s.manipEssence, s.manipTicks, s.plainEssence, s.plainTicks = 0, 0, 0, 0
}

// ---- the tick feed ----

// tickSource is the part of *gamestate.State the tick feed reads.
type tickSource interface {
	Subscribe(gamestate.Subscriber) (cancel func())
	Tick() int
	SelfAnim() (seq, tick int)
}

// tickWatch queues one observation per PLAYER_INFO, sampled at the moment the state announces the tick (the animation the packet
// carried is applied by then), plus a roll mark when the Mining experience moves in that tick, so a loop that is a few ticks
// behind still sees every tick.
type tickWatch struct {
	mu     sync.Mutex // the queue is filled on the read loop and drained on the mine loop
	q      []chopmanipplan.Obs
	wake   chan struct{}
	cancel func()
}

func newTickWatch(src tickSource) *tickWatch {
	w := &tickWatch{wake: make(chan struct{}, 1)}
	w.cancel = src.Subscribe(func(e gamestate.Event) {
		switch e.Kind {
		case gamestate.EventTick:
			anim := chopmanipplan.NoAnim
			if seq, at := src.SelfAnim(); at == e.Tick {
				anim = seq
			}
			w.push(chopmanipplan.Obs{N: e.Tick, Anim: anim}, false)
		case gamestate.EventSkillXP:
			if e.Skill == MiningStat && e.XPDelta > 0 {
				// The UPDATE_STAT of an essence arrives right after the PLAYER_INFO of the tick it was earned in.
				w.push(chopmanipplan.Obs{N: src.Tick(), Anim: chopmanipplan.NoAnim, Roll: true}, true)
			}
		}
	})
	return w
}

// push queues o. A roll mark joins the observation of its own tick when that has not been read yet; otherwise it follows as an
// observation of its own (the controller treats a second observation of one tick as the same tick).
func (w *tickWatch) push(o chopmanipplan.Obs, roll bool) {
	w.mu.Lock()
	if roll {
		for i := len(w.q) - 1; i >= 0; i-- {
			if w.q[i].N == o.N {
				w.q[i].Roll = true
				w.mu.Unlock()
				w.signal()
				return
			}
		}
	}
	w.q = append(w.q, o)
	if len(w.q) > 64 {
		w.q = w.q[len(w.q)-64:]
	}
	w.mu.Unlock()
	w.signal()
}

func (w *tickWatch) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
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

// ---- the kit ----

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

// manipNames, manipPickaxe and manipSource are how the manipulation reads object names, finds the pickaxe in use and reads the
// ticks. They are variables so a test can stand in for the cache config, the equipment panel and the animation of a PLAYER_INFO.
var (
	manipNames   = packName
	manipPickaxe = func(d *Deps) (minemanipplan.Pickaxe, bool) {
		return minemanipplan.Best(MiningLevel(d), HeldNames(d))
	}
	// manipSource is the tick feed's source: the client's state. A test gives the animation the model says the server started.
	manipSource = func(st *gamestate.State) tickSource { return st }
)

// packName reads an object's name from the config.
func packName(d *Deps) itemName {
	return func(id int32) (string, bool, bool) {
		if d.Config == nil {
			return "", false, false
		}
		obj, ok := d.Config.Obj(int(id))
		if !ok {
			return "", false, false
		}
		return obj.Name, obj.Noted(), true
	}
}

// fletchingLevel is the account's Fletching level as last reported (0 when not read).
func fletchingLevel(st *gamestate.State) int {
	if sk, ok := st.Skill(fletchingStat); ok {
		return sk.Level
	}
	return 0
}

// HasKit reports whether the pack holds a knife and a log the Make 1 button can use.
func HasKit(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	_, ok := pickBatchItems(inv.NonEmpty(), manipNames(d), fletchingLevel(d.Session.State()))
	return ok
}

// ManipLogsWanted is how many plain logs the kit holds.
const ManipLogsWanted = 1

// kitCounts is the un-noted knives and plain logs the pack holds (the kit is a plain log: the one carrier every account can cut).
func kitCounts(d *Deps) (knives, plainLogs int) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, 0
	}
	name := manipNames(d)
	for _, it := range inv.NonEmpty() {
		n, noted, ok := name(it.ID)
		if !ok || noted {
			continue
		}
		switch {
		case strings.EqualFold(n, woodcutplan.Knife):
			knives++
		case strings.EqualFold(strings.TrimSpace(n), chopmanipplan.PlainLogs):
			plainLogs++
		}
	}
	return knives, plainLogs
}

// KitWanted says whether the account should carry the kit: the manipulation is on and the pickaxe in use gains from it. A rune
// pickaxe's own period is the floor, so it keeps the two slots for essence (27 a load against 25); with no pickaxe known the kit
// held is kept.
func KitWanted(d *Deps) bool {
	if d == nil || d.Manip == nil || !d.Manip.Enabled || d.Session == nil {
		return false
	}
	if p, ok := manipPickaxe(d); ok && !p.Worth() {
		return false
	}
	return true
}

// KitKeep is what a bank visit must leave in the pack, and what a stage decision must not count as a haul, for an account whose
// Deps run the batch: the knife, and the plain log when exactly ManipLogsWanted are held (a pack with more has them all banked
// and one drawn back, so the kit never takes more than its two slots). Nil when the manipulation is off or the pickaxe gains
// nothing from it (KitWanted).
func KitKeep(d *Deps) []string {
	if !KitWanted(d) {
		return nil
	}
	knives, logs := kitCounts(d)
	var keep []string
	if knives > 0 {
		keep = append(keep, woodcutplan.Knife)
	}
	if logs == ManipLogsWanted {
		keep = append(keep, chopmanipplan.PlainLogs)
	}
	return keep
}

// DrawKit draws a knife and a plain log from an open bank until the pack holds the kit, when the manipulation is on and the bank
// has them, and reports whether it drew anything. The bank is where an account's own kit comes from; the mule's ask covers an
// account whose bank has none (internal/bot/essence_supply.go).
func DrawKit(d *Deps, bank *actions.Bank) bool {
	if !KitWanted(d) || bank == nil {
		return false
	}
	knives, logs := kitCounts(d)
	// The knife and the log are drawn in ONE burst (twopertick-1012): two Withdraws, confirmed once.
	var ops []actions.BankOp
	if knives < 1 {
		if ids := bank.RowIDsByName(d.Config, []string{woodcutplan.Knife}); len(ids) > 0 {
			ops = append(ops, actions.WithdrawOp(ids[0], 1))
		}
	}
	if logs < ManipLogsWanted {
		if ids := bank.RowIDsByName(d.Config, []string{chopmanipplan.PlainLogs}); len(ids) > 0 {
			ops = append(ops, actions.WithdrawOp(ids[0], int32(ManipLogsWanted-logs)))
		}
	}
	if len(ops) == 0 {
		return false
	}
	r, retried := bank.Visit(ops, Waiter(d), actions.BatchOptions{})
	d.logf("%s", actions.VisitLog("mine manip kit", len(ops), r, retried))
	drew := r.Err == nil && len(r.Short) < len(ops)
	if drew {
		k, l := kitCounts(d)
		d.logf("mine manip: drew the kit from the bank (%d knife, %d plain log held)", k, l)
	}
	return drew
}

// ---- one Mine call ----

// manipRun is the manipulation of one Mine call.
type manipRun struct {
	d         *Deps
	st        *ManipState
	ctl       *chopmanipplan.Controller
	pick      minemanipplan.Pickaxe
	m         locindex.Match
	key       string
	src       *gamestate.State
	watch     *tickWatch
	name      itemName
	startTick int
	wrote     int
	// resumeBy is the tick after which a rock click is made if no roll has come back since the chat the hold answered.
	resumeBy int
}

// startManip returns the call's manipulation, or nil when Deps.Manip leaves it off, the pickaxe in use gains nothing, or the pack
// holds no kit (said once each).
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
	pick, ok := manipPickaxe(d)
	if !ok {
		if !st.warnedNoPick {
			st.warnedNoPick = true
			d.logf("mine manip: no pickaxe the Mining level allows is held, so this mine is a plain click")
		}
		return nil
	}
	if !pick.Worth() {
		if !st.warnedFloor {
			st.warnedFloor = true
			d.logf("mine manip: a %s rolls every %d ticks, the batch's own floor, so it gains nothing and the kit is not used", strings.ToLower(pick.Item), pick.Period())
		}
		return nil
	}
	r := &manipRun{d: d, st: st, ctl: st.controller(pick), pick: pick, m: m, src: src, name: manipNames(d), startTick: src.Tick(),
		key: actions.LocKey(m.X, m.Z, int(m.ID))}
	if _, ok := r.items(); !ok {
		if !st.warnedItems {
			st.warnedItems = true
			d.logf("mine manip: no knife, or no log the Make 1 button can use (a plain log, or one whose shortbow the Fletching level allows), so this mine is a plain click: a %s rolls every %d ticks, a batch every %d", strings.ToLower(pick.Item), pick.Period(), minemanipplan.ManipPeriod)
		}
		return nil
	}
	r.watch = newTickWatch(manipSource(src))
	return r
}

// items reads the pack and the Fletching level.
func (r *manipRun) items() (batchItems, bool) {
	inv := r.src.Inventory(PlayerPack)
	if inv == nil {
		return batchItems{}, false
	}
	return pickBatchItems(inv.NonEmpty(), r.name, fletchingLevel(r.src))
}

// write sends one batch after tick n (the tick the client had seen when it wrote), reporting whether it went out.
func (r *manipRun) write(n int) bool {
	if actions.MovementHeld(r.d.Session) {
		// Another caller (a random event's solver) holds this account's clicks; the plain click is dropped for the same reason,
		// so a batch must not carry one past the hold.
		return false
	}
	if ticks, _ := actions.ClickWait(r.d.Session, r.key); ticks > 0 {
		// The rock was clicked less than actions.ClickGapTicks ago (a hold's own click after a chat): a batch is a click too.
		return false
	}
	if PackFull(r.d) {
		// The roll that fills the pack: the experience that marks it arrives after the inventory, so the pack reads full here,
		// and the next swing would only open the "too full" box.
		return false
	}
	bi, ok := r.items()
	if !ok {
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
	// The CLOSE_MODAL is what cancels the fletch the Make 1 queued, whatever becomes of the rock click.
	if err := actions.CloseModal(sess); err != nil {
		return false
	}
	if err := actions.OpLoc(sess, r.m.Op, r.m.X, r.m.Z, r.m.ID); err != nil {
		return false
	}
	if b.Pending() != minemanipplan.BatchEvents {
		// An action that queued nothing (a click dropped by a hold) would leave a knife and a Make 1 without their cancelling
		// packets.
		b.Discard()
		return false
	}
	if err := b.Flush(); err != nil {
		r.d.logf("mine manip: writing a batch failed: %v", err)
		return false
	}
	actions.MarkClick(r.d.Session, r.key)
	r.ctl.Sent(n)
	r.wrote++
	return true
}

// open writes the mine's opening click as a batch when the controller wants one, and reports whether it did; false means the
// caller sends the plain click it always sent.
func (r *manipRun) open() bool {
	n := r.src.Tick()
	if !r.ctl.BeginChop(n) {
		return false
	}
	return r.write(n)
}

// answered is told that the hold answered a chat and found it gone: the interrupted interaction carries on by itself, so no
// click is made unless no roll comes back within resumeTicks.
func (r *manipRun) answered() { r.resumeBy = r.src.Tick() + resumeTicks }

// resumeDue reports whether the wait after an answered chat has run out without a roll, and ends it.
func (r *manipRun) resumeDue() bool {
	if r.resumeBy == 0 || r.src.Tick() < r.resumeBy {
		return false
	}
	r.resumeBy = 0
	return true
}

// pace waits for the next PLAYER_INFO, tells the controller what it carried and writes the batch it asks for. It is the
// replacement for Mine's 600 ms sleep, and returns true when the caller asked to stop.
func (r *manipRun) pace() (stopped bool) {
	obs, ok, stopped := r.watch.next(r.d, manipWait)
	if stopped {
		return true
	}
	if !ok {
		return false
	}
	for {
		if obs.Roll || minemanipplan.IsPickAnim(obs.Anim) {
			r.resumeBy = 0 // the interaction is alive
		}
		if r.ctl.Observe(obs) && r.src.Tick() == obs.N {
			// Only an observation that is still the newest is worth a batch: one a tick or more old would be read after the
			// stale tick it was meant for.
			r.write(obs.N)
		}
		var have bool
		if obs, have = r.watch.pop(); !have {
			return false
		}
	}
}

// finish ends the call's manipulation: detaches the tick feed and adds the call to the account's tallies.
func (r *manipRun) finish(gained int) {
	r.watch.stop()
	ticks := r.src.Tick() - r.startTick
	r.st.note(r.wrote > 0, gained, ticks)
	r.st.report(r.d)
}
