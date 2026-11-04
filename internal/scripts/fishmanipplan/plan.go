// Package fishmanipplan decides when a fishing account writes the knife-on-log batch and the lone spot click that make a
// net, big net, lobster pot or harpoon spot roll every 3 ticks instead of every 6. It is the pure half of
// fisherrunner's fishing manipulation: no client, no actions, no fleet, no bot imports, so every rule below is
// unit-tested against the engine model in internal/tickmanip.
//
// # The mechanism (lab engine, content 289, claude/tickmanip-1005 and docs/tick_manipulation_lab_2026-10-06.md)
//
// Every fishing script rolls on one per-player varp, %action_delay, compared with map_clock (content
// skill_fishing/scripts/fishing_spots/saltfish.rs2, memberfish.rs2, rarefish.rs2). A spot's first click (op 1: net, big
// net, lobster pot; op 3: harpoon) runs the "attempt" script, whose hidden loop (op 4 or 5) then runs every tick:
//
//	attempt, stale varp:     %action_delay = map_clock+4 (harpoon +5), the click is run again next tick
//	attempt, varp == clock+3 (harpoon +4): the cast animation, the loop starts
//	attempt, varp == clock:  the roll, and NO next op: the interaction ends
//	attempt, any other:      nothing, and the interaction ends
//	loop,    stale varp:     %action_delay = map_clock+5
//	loop,    varp == clock:  the roll, the loop goes on
//
// so an account left to itself rolls at the tick the varp reaches the clock, finds the varp stale on the next tick,
// re-arms it five ticks on and rolls again 6 ticks after the last roll.
//
// A knife used on a log runs cut_logs.rs2's process_fletch_logs, which sets %action_delay to map_clock+2 when it is below
// the clock and queues the fletch on the weak queue. So a batch [knife on a log, Make 1, CLOSE_MODAL, spot click] read
// in the STALE tick right after a roll (the varp below the clock) puts the next roll two ticks on. The spot click in the
// same batch is read before the interaction runs and finds the varp at clock+2, which matches nothing: the interaction
// ends (CLOSE_MODAL has already cancelled the queued fletch, so the log is kept). The roll two ticks later therefore
// needs the interaction to be started again, by ONE LONE spot click read in exactly that tick: the attempt script finds the
// varp equal to the clock and rolls. The tick after it the varp is stale again and the next batch is read.
//
//	tick B     batch   varp = B+2, the interaction ends
//	tick B+2   lone    the roll (and the end of the interaction again)
//	tick B+3   batch   the varp is stale again: varp = B+5
//	tick B+5   lone    the roll ...
//
// A roll every 3 ticks for one batch and one click: x2.0 for the net, x2.2 for the big net, x1.7 for the harpoon in the lab.
//
// # The loop is closed on the batch's own read tick
//
// A write made right after the PLAYER_INFO of tick k is read by the engine in tick k+1+L, where L is the link's lateness
// (0 on a good link, 1 once the round trip is longer than a tick, which is most proxied accounts). Each batch plays the
// fletch animation (seq 1248) in the tick it is read in, which the client sees: that is the batch's READ tick B, whatever
// L was. From it the controller knows the roll tick (B+2) and the next stale tick (B+3), and writes the lone click after the
// PLAYER_INFO of tick B+1-L and the next batch after that of B+2-L, L being the commonest lateness of the last batches
// (the aim, as in chopmanipplan). Before a batch is confirmed the same targets come from its expected read tick.
//
// Why not just write every 3 ticks and every third tick plus 2? That pattern is exact for a link whose lateness never
// changes and unforgiving otherwise: a batch read one tick late leaves the lone click a tick early, the click after it
// then lands on a stale varp and starts a natural cycle with no knife in front of it, and the pattern can stay out of phase
// for dozens of rolls (the model in internal/tickmanip: a flip in 2 writes in 100 cost a net a third of its rolls and a
// harpoon three quarters). Anchored on each batch's confirmed read tick, the same flips cost 3%.
//
// # What can go wrong
//
//   - Two batches read in the same tick are 8 user events against the engine's 5, and a Make 1 read a tick before its
//     cancelling packets would let the queued fletch consume the carrier log. At most one batch is in flight, across holds
//     too, and a batch is lost only after ticks AND real time have passed (a client that stalls is told the ticks it
//     missed in a burst). A batch and the lone click that follows it are 5 events and fit one tick.
//   - A lone click read a tick early or late rolls nothing; it costs that roll, and the next batch is anchored afresh.
package fishmanipplan

import "fmt"

// Constants read out of content 289.
const (
	// FletchAnim is human_fletching (content/pack/seq.pack, 1248), the animation a Make 1 plays and so a batch's mark.
	FletchAnim = 1248
	// Period is the ticks between batches (and between rolls): the shortest the mechanism allows.
	Period = 3
	// RollAfter is how many ticks after a batch is read the roll comes: the lone click is read then.
	RollAfter = 2
)

// NoAnim marks a tick in which the server started no animation on the local player.
const NoAnim = -1 << 20

// Settings tune the controller.
type Settings struct {
	// Enabled is the master switch; a disabled controller never asks for a write.
	Enabled bool
	// History is how many confirmed batches the lateness estimate votes over.
	History int
	// MaxLead is the most ticks early a write is aimed (the link lateness the controller compensates for). A slower link
	// is aimed at MaxLead and lands late sometimes.
	MaxLead int
	// ConfirmTicks is how many ticks past the earliest tick a batch could be read it may take to show its fletch
	// animation before it is counted lost.
	ConfirmTicks int
	// LostAfterMS is how long, by the controller's clock (SetClock), a batch must have been out before it can be called
	// lost or let the next one go: ticks alone are not time (a stalled client is told its missed ticks in a burst).
	LostAfterMS int
	// MaxLost is how many batches in a row may be lost before the controller stops writing batches for the rest of the
	// hold (the kit is not doing what it should: no log, no knife, an interface in the way). The next hold starts afresh.
	MaxLost int
	// Window is how many batch spacings the regularity check looks at: a hold whose last Window spacings (the ticks between
	// the read ticks of consecutive confirmed batches) have no one value in at least two fifths of them is stopped, for a link
	// whose lateness is all over the place is no better off with the pattern than with the plain click (the model: lateness
	// drawn from 0, 1 and 2 at random leaves a net 30% and a harpoon 40% below it; one flip in two writes is 8% and 16%
	// below, a flip in one write in four is 20% above).
	Window int
}

// DefaultSettings are the fleet's: a vote over the last 5 confirmed batches, aimed at most 2 ticks early, a batch lost 3
// ticks and 2.4 s after it could first have been read, four lost in a row or 32 spacings of no regular value stop the
// hold's manipulation.
func DefaultSettings() Settings {
	return Settings{Enabled: true, History: 5, MaxLead: 2, ConfirmTicks: 3, LostAfterMS: 2400, MaxLost: 4, Window: 32}
}

func (s Settings) normalised() Settings {
	d := DefaultSettings()
	if s.History < 2 {
		s.History = d.History
	}
	if s.MaxLead < 1 {
		s.MaxLead = d.MaxLead
	}
	if s.MaxLead > 3 {
		s.MaxLead = 3
	}
	if s.ConfirmTicks < 1 {
		s.ConfirmTicks = d.ConfirmTicks
	}
	if s.LostAfterMS < 1 {
		s.LostAfterMS = d.LostAfterMS
	}
	if s.MaxLost < 1 {
		s.MaxLost = d.MaxLost
	}
	if s.Window < 4 {
		s.Window = d.Window
	}
	return s
}

// Obs is one PLAYER_INFO as the client saw it: the tick ordinal and the animation the server started on the local
// player in it (NoAnim for none).
type Obs struct {
	N    int
	Anim int
}

// Action is what the controller asks the runner to write now, right after the PLAYER_INFO it was told about.
type Action int

const (
	// None writes nothing.
	None Action = iota
	// Batch is knife on the carrier log, Make 1, CLOSE_MODAL and the spot click, in one write.
	Batch
	// Lone is the spot click alone.
	Lone
)

func (a Action) String() string {
	switch a {
	case Batch:
		return "batch"
	case Lone:
		return "lone click"
	}
	return "none"
}

// AliveTicks is how recently a batch must have been confirmed for the hold to count as fishing: the pattern shows no
// fishing animation of its own (a net's roll plays none), so the batches are the sign of life.
const AliveTicks = 6

// Stats is what a controller counted, for the log line.
type Stats struct {
	// Batches and Lones written; Confirmed batches showed their fletch animation; Lost never did.
	Batches, Lones, Confirmed, Lost int
	// Lateness counts confirmed batches by how many ticks after the next tick they were read (0, 1, 2, 3 or more): the
	// link's own delay, whatever the aim was.
	Lateness [4]int
	// Spacings are the ticks between the read ticks of consecutive confirmed batches of a hold, Spacing3 those that were
	// exactly 3: the batch was read in the stale tick of the roll it was aimed at and the pattern held.
	Spacings, Spacing3 int
	// Retimes is how many times the aim changed; Lead is the aim now, in ticks early. Stops is how many holds the controller
	// stopped writing batches in.
	Retimes, Lead, Stops int
}

// Line is the stats as one log fragment.
func (s Stats) Line() string {
	return fmt.Sprintf("%d batches (%d confirmed, %d lost) and %d lone clicks; link delay 0/1/2/3+ ticks late %d/%d/%d/%d, aiming %d tick(s) early (%d re-aims); %d of %d batch spacings were 3 ticks; stopped in %d hold(s)",
		s.Batches, s.Confirmed, s.Lost, s.Lones, s.Lateness[0], s.Lateness[1], s.Lateness[2], s.Lateness[3], s.Lead, s.Retimes, s.Spacing3, s.Spacings, s.Stops)
}

// Controller is the batch policy of one fishing account. Its zero value is not usable; use New. It is not safe for
// concurrent use: the runner calls it from the hold loop only.
type Controller struct {
	cfg   Settings
	clock func() int64

	// sentAt is the tick the last batch was written after (0: no schedule). loneDone says whether the lone click for it has
	// been written (or passed). inFlight: the batch is unconfirmed; carried: an earlier hold wrote it, whose ticks this
	// hold's feed did not see.
	sentAt   int
	loneDone bool
	inFlight bool
	carried  bool
	sentMS   int64
	// read is the read tick of the last batch when it is confirmed (0: not yet); lastConfirm the latest of any hold.
	read, lastConfirm int
	prevRead          int // the read tick of the batch confirmed before, for the spacing statistic
	spacings          []int
	lost              int
	stopped           bool
	why               string

	hist []int // delivery lateness of the latest confirmed batches (clamped to MaxLead), oldest first
	lead int

	stats Stats
}

// New returns a controller with the given settings (zero fields take their defaults; Enabled is as given).
func New(s Settings) *Controller {
	n := s.normalised()
	n.Enabled = s.Enabled
	return &Controller{cfg: n}
}

// SetClock gives the controller a monotonic millisecond clock (see Settings.LostAfterMS). The runner sets it; the pure
// tests that count ticks alone leave it nil.
func (c *Controller) SetClock(now func() int64) { c.clock = now }

func (c *Controller) nowMS() int64 {
	if c.clock == nil {
		return 0
	}
	return c.clock()
}

// aged reports whether the batch in flight has been out long enough, in ticks and by the clock, to have been read.
func (c *Controller) aged(n int) bool {
	if n < c.sentAt+1+c.cfg.ConfirmTicks {
		return false
	}
	return c.clock == nil || c.nowMS()-c.sentMS >= int64(c.cfg.LostAfterMS)
}

// Lead is the aim now: how many ticks before a tick a write is made so it is read in it.
func (c *Controller) Lead() int { return c.lead }

// InFlight reports whether a batch has been written and not yet settled.
func (c *Controller) InFlight() bool { return c.inFlight }

// Stopped reports whether this hold's manipulation was stopped (lost batches, or spacings with no regular value).
func (c *Controller) Stopped() bool { return c.stopped }

// StopReason is why the hold was stopped, for the log; "" when it was not.
func (c *Controller) StopReason() string { return c.why }

// Stats returns the counters so far.
func (c *Controller) Stats() Stats {
	s := c.stats
	s.Lead = c.lead
	return s
}

// Alive reports whether a batch was confirmed within AliveTicks of tick n.
func (c *Controller) Alive(n int) bool {
	return c.lastConfirm != 0 && n >= c.lastConfirm && n-c.lastConfirm <= AliveTicks
}

// Begin starts a hold whose first spot click is about to be written after tick n. It reports whether that click should
// be the opening batch. The first click of a hold finds the varp stale (the account was not rolling: it had walked to the
// spot, or a drop cycle or a bank trip came before), so the opening batch pulls the first roll to 2 ticks after it is
// read and the pattern starts at once. A batch a previous hold left in flight is not forgotten: the opening click is a
// plain one until it is settled, either confirmed by the new hold's tick feed or old enough that it was read, so two
// batches are never read in one tick.
func (c *Controller) Begin(n int) bool {
	if c.inFlight {
		if c.aged(n) {
			c.inFlight = false
		} else {
			c.carried = true
		}
	}
	c.sentAt, c.loneDone, c.read, c.prevRead, c.lost, c.stopped, c.why = 0, true, 0, 0, 0, false, ""
	c.spacings = c.spacings[:0]
	return c.cfg.Enabled && !c.inFlight
}

// Resume reports whether a hold that begins while the account is still fishing from the last one should go on with its
// schedule instead of clicking: there is one, and its last batch is only a few ticks old.
func (c *Controller) Resume(n int) bool {
	return c.cfg.Enabled && !c.stopped && c.sentAt != 0 && n >= c.sentAt && n-c.sentAt <= 2*Period
}

// Continue is Resume for a hold that does take the schedule over: when it reports true, a batch still in flight is marked as one
// this hold's feed may not have watched, so silence about it is not counted as a loss.
func (c *Controller) Continue(n int) bool {
	if !c.Resume(n) {
		return false
	}
	if c.inFlight {
		c.carried = true
	}
	return true
}

// Sent records that the caller wrote a: the batch (the opening one or one Observe asked for) or the lone click, after
// tick n.
func (c *Controller) Sent(a Action, n int) {
	switch a {
	case Batch:
		c.sentAt, c.read, c.loneDone = n, 0, false
		c.inFlight, c.carried, c.sentMS = true, false, c.nowMS()
		c.stats.Batches++
	case Lone:
		c.loneDone = true
		c.stats.Lones++
	}
}

// anchor is the tick the schedule is measured from: the last batch's read tick when it is confirmed, else the tick it is
// expected to be read in.
func (c *Controller) anchor() int {
	if c.read != 0 {
		return c.read
	}
	return c.sentAt + 1 + c.lead
}

// Observe feeds the PLAYER_INFO of tick o.N and reports what the caller should write now (right after that
// PLAYER_INFO). It settles the batch in flight, then keeps the pattern: the lone click for the roll 2 ticks after the
// batch is read, the next batch for the stale tick after it, each written the aim's number of ticks early.
func (c *Controller) Observe(o Obs) Action {
	c.settle(o)
	if !c.cfg.Enabled || c.stopped || c.sentAt == 0 {
		return None
	}
	b := c.anchor()
	if !c.loneDone {
		switch due := b + RollAfter - 1 - c.lead; {
		case o.N == due:
			return Lone
		case o.N > due:
			// A tick that went by unseen (a stalled client told its ticks in a burst) or a confirmation that came after it: a
			// lone click this late rolls nothing.
			c.loneDone = true
		}
	}
	if !c.inFlight && o.N >= b+Period-1-c.lead {
		return Batch
	}
	return None
}

// settle confirms the batch in flight from its fletch animation, or calls it lost once it is old enough.
func (c *Controller) settle(o Obs) {
	if !c.inFlight {
		return
	}
	switch {
	case o.Anim == FletchAnim:
		c.inFlight, c.carried = false, false
		c.lost = 0
		late := o.N - c.sentAt - 1
		c.stats.Confirmed++
		c.stats.Lateness[min(max(late, 0), 3)]++
		if c.prevRead != 0 {
			c.stats.Spacings++
			if o.N-c.prevRead == Period {
				c.stats.Spacing3++
			}
			c.noteSpacing(o.N - c.prevRead)
		}
		c.read, c.prevRead, c.lastConfirm = o.N, o.N, o.N
		c.vote(late)
	case c.aged(o.N):
		c.inFlight = false
		if c.carried {
			// The confirmation may have come before this hold's feed began: silence about it is not a loss.
			c.carried = false
			return
		}
		c.stats.Lost++
		c.lost++
		if c.lost >= c.cfg.MaxLost {
			c.stop(fmt.Sprintf("%d batches in a row were never read", c.lost))
		}
	}
}

func (c *Controller) stop(why string) {
	if !c.stopped {
		c.stopped, c.why = true, why
		c.stats.Stops++
	}
}

// noteSpacing adds one spacing between consecutive confirmed batches and stops the hold when the last Window of them have
// no one value in at least two fifths of them: the batches are not landing in a pattern, and the plain click does better.
func (c *Controller) noteSpacing(sp int) {
	c.spacings = append(c.spacings, sp)
	if len(c.spacings) > c.cfg.Window {
		c.spacings = c.spacings[len(c.spacings)-c.cfg.Window:]
	}
	if len(c.spacings) < c.cfg.Window {
		return
	}
	counts := map[int]int{}
	best := 0
	for _, v := range c.spacings {
		counts[v]++
		best = max(best, counts[v])
	}
	if 5*best < 2*len(c.spacings) {
		c.stop(fmt.Sprintf("the last %d batch spacings had no regular value (the commonest was %d of them)", len(c.spacings), best))
	}
}

// vote adds one confirmed batch's delivery lateness to the history and moves the aim to the commonest value, but only to
// one with strictly more votes than the aim now has, so a single stray batch does not flap it.
func (c *Controller) vote(late int) {
	c.hist = append(c.hist, min(max(late, 0), c.cfg.MaxLead))
	if len(c.hist) > c.cfg.History {
		c.hist = c.hist[len(c.hist)-c.cfg.History:]
	}
	var counts [4]int
	for _, v := range c.hist {
		counts[v]++
	}
	best := c.lead
	for v := 0; v <= c.cfg.MaxLead; v++ {
		if counts[v] > counts[best] {
			best = v
		}
	}
	if best != c.lead {
		c.lead = best
		c.stats.Retimes++
	}
}
