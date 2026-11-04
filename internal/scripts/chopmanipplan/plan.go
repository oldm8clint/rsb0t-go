// Package chopmanipplan decides when a chopping account writes a knife-on-log batch, so a tree rolls every
// 3 ticks instead of every 4. It is the pure half of woodcutrunner's chop manipulation: no client, no
// actions, no fleet, no bot imports, so every rule below is unit-tested against a small model of the engine.
//
// # The mechanism (lab engine, content 289, measured by claude/tickmanip-1005, docs/tick_manipulation_lab_2026-10-06.md)
//
// A chop is a loop on the per-player varp %action_delay (content skill_woodcutting/scripts/woodcut.rs2). The
// script runs every tick while the interaction lasts: when the varp is below map_clock it re-arms it to
// map_clock+3 (lines 55-59 and 118-120), when it equals map_clock the swing rolls for a log. A roll at tick R
// leaves the varp at R, so tick R+1 is the STALE tick: the script re-arms to R+4, and the next roll is 4 ticks
// after the last.
//
// A knife used on a log runs cut_logs.rs2's process_fletch_logs, which sets %action_delay to map_clock+2 when it
// is below the clock (lines 83-86) and queues the fletch on the WEAK queue. The engine reads a player's packets
// before it runs the player's interaction (World.processClientsIn, then processPlayers), so a knife used in the
// stale tick R+1 re-arms the varp to R+3 BEFORE the chop script would have re-armed it to R+4: the next roll is
// at R+3, a 3-tick period. Anything that closes the modal in the same message (a click on the tree, or a
// CLOSE_MODAL) clears the weak queue, so the queued fletch never runs: the log is kept and only the varp moved.
//
// So the rule is: get [knife on a log, Make 1, CLOSE_MODAL, tree click] read in the stale tick of every swing. The
// same batch read at any other tick moves nothing and costs nothing.
//
// # What the client can read, with no ack
//
// A roll plays the axe's chop animation at tick R whether or not the swing yields a log (woodcut.rs2 plays
// anim(axe) in the roll branch); the swing that fells a tree shows an animation reset instead; a batch that ran its
// Make 1 plays human_fletching (seq 1248) in the tick it was read in, and the fletching animation wins the tick's
// PLAYER_INFO over the chop animation. So the controller is fed (tick, animation) pairs: a swing marker says where
// the swings are, a fletch marker says when a batch was read.
//
// # Lateness, and writing earlier
//
// A write made right after the PLAYER_INFO of tick k is read in tick k+1 only if it reaches the engine before the
// next cycle starts, which a link with a round trip above about 500 ms cannot do. The batch is then read in k+2 (one
// tick late), or k+3, and it moves nothing. Live on the fleet (6 October) most proxied accounts showed exactly +1 on
// every batch. The delay is steady, so the controller measures it (a batch written after tick s and confirmed in
// tick F was late by F-s-1 ticks), takes the commonest recent value as its LEAD, and writes that many ticks BEFORE
// the swing it is aimed at: it knows when the next swing is (3 ticks after the last when the last batch landed, 4
// when it did not), so a batch written after the PLAYER_INFO of tick R+P-lead is read in R+P+1, the stale tick.
// A lost or mistimed batch changes the estimate or nothing; the controller never pauses and never switches itself
// off. With a lead of 0 it is the reactive rule: write right after a swing is seen.
//
// # One batch in flight
//
// Two batches read in the same tick are 8 user events against the engine's 5, and a Make 1 read a tick before its
// cancelling packets would let the queued fletch run and consume the carrier. So at most one batch is ever in
// flight, across Chop calls too, and a batch is lost only after ticks AND real time have passed (a client that
// stalls for seconds is told the ticks it missed in a burst).
//
// # Other skills on the same loop
//
// Essence mining (content skill_mining/scripts/mining.rs2) is the same %action_delay loop with a rate of 2 to 7 instead of 3
// and a roll that always yields. internal/scripts/minemanipplan sets Settings.Swing and Settings.Plain for it and feeds
// Obs.Roll from the Mining experience; woodcutting leaves both at their zero values and is unchanged.
package chopmanipplan

import "fmt"

// Constants read out of content 289.
const (
	// FletchAnim is human_fletching (content/pack/seq.pack:1249, 1248=human_fletching), the animation a Make 1 plays.
	FletchAnim = 1248
	// Period is the ticks between rolls when every batch lands in the stale tick; PlainPeriod without any.
	Period      = 3
	PlainPeriod = 4
	// Make1Shafts is skill_multi3:make1_a (content/pack/interface.pack:8890, 8889=skill_multi3:make1_a): "Make 1" of
	// the first product of plain logs, 15 arrow shafts, which needs Fletching level 1.
	Make1Shafts = 8889
	// Make1Bow is skill_multi2:make1_a (interface.pack:8875, 8874=skill_multi2:make1_a): "Make 1" of the first
	// product of every other log, its shortbow, which needs the Fletching level in ShortbowLevel.
	Make1Bow = 8874
)

// NoAnim marks a tick in which the server started no animation on the local player.
const NoAnim = -1 << 20

// ChopAnims are the axes' chop animations (content/pack/seq.pack: human_woodcutting_<axe>_axe, rune 867 down to
// bronze 879 in steps of two). One of them at tick R is a roll.
var ChopAnims = map[int]bool{867: true, 869: true, 871: true, 873: true, 875: true, 877: true, 879: true}

// IsChopAnim reports whether seq is an axe's chop animation.
func IsChopAnim(seq int) bool { return ChopAnims[seq] }

// SwingFresh is how many ticks old the newest swing animation may be for the account to count as still chopping: two
// plain periods. A swing that rolls every 4 ticks (3 with the manipulation) shows its animation at least that often,
// and the window also spans the tick in which a batch's fletch animation takes the place of a chop animation.
const SwingFresh = 2 * PlainPeriod

// Swinging reports whether the local player is still chopping at tick now, given the newest animation the server
// started on it (seq, at the tick it arrived on: gamestate.State.SelfAnim). It is how a chop tells a tree that is slow
// from a tree that gives nothing: a yew gives a log in about one swing in six at Woodcutting 60, so a quarter of a
// minute without a log is ordinary there, while a quarter of a minute without a swing is a click the server ignored.
func Swinging(seq, at, now int) bool {
	if !IsChopAnim(seq) && seq != FletchAnim {
		return false
	}
	age := now - at
	return age >= 0 && age <= SwingFresh
}

// Settings tune the controller. DefaultSettings are what the fleet runs.
type Settings struct {
	// Enabled is the master switch; a disabled controller never asks for a batch.
	Enabled bool
	// History is how many confirmed batches the lateness estimate votes over.
	History int
	// MaxLead is the most ticks early a batch is aimed. Beyond it a link cannot be pipelined with one batch in flight
	// (a batch aimed 3 early is still unconfirmed when the next is due), so a slower link is aimed at MaxLead and
	// lands late sometimes.
	MaxLead int
	// ConfirmTicks is how many ticks past the stale tick a batch may take to show its fletch animation before it is
	// counted lost.
	ConfirmTicks int
	// LostAfterMS is how long, by the controller's clock (SetClock), a batch must have been out before it can be
	// called lost or let a second batch go. Ticks alone are not time: a client that stalls for seconds receives the
	// PLAYER_INFOs it missed in a burst, and counting those as the ticks a batch had to arrive in would declare it lost
	// before it was even written (a leak found in the lab under load: two batches read in one tick). Without a clock
	// only the ticks count.
	LostAfterMS int

	// Swing reports whether an animation is a roll of the skill being manipulated. Nil is the axes' chop animations
	// (IsChopAnim), which is woodcutting. Essence mining (internal/scripts/minemanipplan: the same %action_delay loop, a
	// different rate) trusts no animation and feeds Obs.Roll from the experience instead.
	Swing func(seq int) bool
	// Plain is the ticks between two rolls when no batch lands, the skill's own period. Zero is PlainPeriod (woodcutting's
	// 4); mining is the pickaxe's mining_rate plus one (8 for bronze down to 3 for rune).
	Plain int
}

// DefaultSettings are the fleet's: a vote over the last 6 confirmed batches, aimed at most 2 ticks early, a batch
// lost after 3 ticks past the stale tick and 2.4 s.
func DefaultSettings() Settings {
	return Settings{Enabled: true, History: 6, MaxLead: 2, ConfirmTicks: 3, LostAfterMS: 2400}
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
	if s.Plain < 1 {
		s.Plain = PlainPeriod
	}
	return s
}

// isSwing reports whether seq is a roll animation of the skill this controller manipulates.
func (s Settings) isSwing(seq int) bool {
	if s.Swing != nil {
		return s.Swing(seq)
	}
	return IsChopAnim(seq)
}

// Obs is one PLAYER_INFO as the client saw it: the tick ordinal and the animation the server started on the local
// player in it (NoAnim for none).
type Obs struct {
	N    int
	Anim int
	// Roll says the client saw the skill pay out in this tick by some other sign than an animation (mining: the Mining
	// experience of an essence, which every roll earns). It counts as a swing. An animation can lie in both directions: the
	// first swing of an unprimed click plays the pickaxe animation without a roll, and a batch read in the roll tick takes
	// the tick's animation over (the fletching animation outranks the pickaxe's), hiding the roll.
	Roll bool
}

// Stats is what a controller counted, for the log line.
type Stats struct {
	// Batches written. Each confirmed one is read relative to the swing it was aimed at: OnTime in the stale tick
	// after it, Late1 one tick later, LateMore later still, Early before it. Lost never showed its fletch animation.
	Batches, OnTime, Late1, LateMore, Early, Lost int
	// Lateness counts confirmed batches by how many ticks after the next tick they were read (0, 1, 2, 3 or more): the
	// link's own delay, whatever the aim was.
	Lateness [4]int
	// Gaps are the ticks between consecutive swings of a chop, ShortGaps those of 3 ticks or less.
	Gaps, ShortGaps int
	// Retimes is how many times the aim changed; Lead is the aim now, in ticks early.
	Retimes, Lead int
}

// Line is the stats as one log fragment.
func (s Stats) Line() string {
	return fmt.Sprintf("%d batches (%d on target, %d one tick late, %d later, %d early, %d lost); link delay 0/1/2/3+ ticks late %d/%d/%d/%d, aiming %d tick(s) early (%d re-aims); %d of %d swing gaps were 3 ticks",
		s.Batches, s.OnTime, s.Late1, s.LateMore, s.Early, s.Lost,
		s.Lateness[0], s.Lateness[1], s.Lateness[2], s.Lateness[3], s.Lead, s.Retimes, s.ShortGaps, s.Gaps)
}

// Controller is the batch policy of one chopping account. Its zero value is not usable; use New. It is not safe for
// concurrent use: the runner calls it from the chop loop only.
type Controller struct {
	cfg   Settings
	clock func() int64

	inFlight bool
	carried  bool  // the batch in flight was written by the previous chop, whose ticks this chop's feed did not see
	sentAt   int   // the tick the client had seen when the batch in flight was written
	sentMS   int64 // the clock when it was actually written
	aimed    int   // the swing tick the batch in flight was aimed at

	openAt   int   // the tick this chop's opening batch was written after (0: none)
	swings   []int // ticks of the latest swing markers of this chop, oldest first
	confirms []int // ticks of the latest fletch animations that settled a batch, oldest first

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

// SetClock gives the controller a monotonic millisecond clock. With one, a batch is only called lost, and only stops
// blocking the next opening click, once Settings.LostAfterMS have passed since it was written as well as the ticks. The
// runner sets it; the pure tests that count ticks alone leave it nil.
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

// Lead is the aim now: how many ticks before a swing a batch is written.
func (c *Controller) Lead() int { return c.lead }

// InFlight reports whether a batch has been written and not yet settled (confirmed, or old enough to have been read).
func (c *Controller) InFlight() bool { return c.inFlight }

// Stats returns the counters so far.
func (c *Controller) Stats() Stats {
	s := c.stats
	s.Lead = c.lead
	return s
}

// BeginChop starts a chop whose tree click is about to be written after tick n. It reports whether that click
// should be a batch. The first click of a chop finds the varp stale whatever the link, so an opening batch always
// pulls the first swing from tick +3 to +2.
//
// A batch the previous chop left in flight is NOT forgotten. A tree that falls with the batch written after its last
// swing still on its way, and an adjacent tree whose stand tile the account is already on (the yews behind the church,
// found in the lab), would otherwise get its opening batch in the same tick: more user events than the engine reads in
// one tick, the second batch's cancelling packets read a tick late and its queued fletch run, the carrier log consumed.
// So the opening click is a plain one until that batch is settled, either confirmed by the tick feed of the new chop or
// old enough that it was read. Its confirmation may have come before the new chop's feed began, so silence about it is
// not evidence that it was lost and is never counted as a loss.
func (c *Controller) BeginChop(n int) bool {
	if c.inFlight {
		if c.aged(n) {
			c.inFlight = false
		} else {
			c.carried = true
		}
	}
	c.swings, c.openAt = nil, 0
	return c.cfg.Enabled && !c.inFlight
}

// Sent records that the caller wrote a batch after tick n (the opening one or one Observe asked for).
func (c *Controller) Sent(n int) {
	opening := len(c.swings) == 0 && c.openAt == 0
	c.inFlight, c.carried, c.sentAt, c.sentMS, c.aimed = true, false, n, c.nowMS(), n+c.lead
	if opening {
		c.openAt = n
	}
	c.stats.Batches++
}

// Observe feeds the PLAYER_INFO of tick o.N and reports whether the caller should write a batch now (right after
// that PLAYER_INFO). It settles the batch in flight, notes a swing, and decides when the next batch is due.
func (c *Controller) Observe(o Obs) (send bool) {
	c.settle(o)
	swing := o.Roll || c.cfg.isSwing(o.Anim)
	if swing {
		c.noteSwing(o.N)
	}
	return c.decide(o.N, swing)
}

// settle confirms the batch in flight from its fletch animation, or calls it lost once it is old enough.
func (c *Controller) settle(o Obs) {
	if !c.inFlight {
		return
	}
	switch {
	case o.Anim == FletchAnim:
		c.inFlight, c.carried = false, false
		late := o.N - c.sentAt - 1
		switch rel := o.N - (c.aimed + 1); {
		case rel == 0:
			c.stats.OnTime++
		case rel < 0:
			c.stats.Early++
		case rel == 1:
			c.stats.Late1++
		default:
			c.stats.LateMore++
		}
		c.stats.Lateness[min(max(late, 0), 3)]++
		c.confirms = append(c.confirms, o.N)
		if len(c.confirms) > 4 {
			c.confirms = c.confirms[len(c.confirms)-4:]
		}
		c.vote(late)
	case c.aged(o.N):
		c.inFlight = false
		if c.carried {
			c.carried = false
			return
		}
		c.stats.Lost++
	}
}

// vote adds one confirmed batch's delivery lateness to the history and moves the aim to the commonest value, but only
// to one with strictly more votes than the aim now has, so a single stray batch does not flap it.
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

func (c *Controller) noteSwing(n int) {
	if k := len(c.swings); k > 0 {
		c.stats.Gaps++
		if n-c.swings[k-1] <= Period {
			c.stats.ShortGaps++
		}
	}
	c.swings = append(c.swings, n)
	if len(c.swings) > 4 {
		c.swings = c.swings[len(c.swings)-4:]
	}
}

func (c *Controller) confirmedAt(n int) bool {
	for _, t := range c.confirms {
		if t == n {
			return true
		}
	}
	return false
}

// decide says whether a batch is due after tick n.
func (c *Controller) decide(n int, swing bool) bool {
	if !c.cfg.Enabled || c.inFlight {
		return false
	}
	if c.lead == 0 {
		// Reactive: a batch right after a swing is read in the stale tick after it.
		return swing
	}
	if len(c.swings) == 0 {
		// No swing seen yet: the opening batch was read at its tick +1+delay and the first swing comes 2 ticks after the
		// read, so the batch for that swing is written 3 ticks after the opening one whatever the delay was.
		return c.openAt != 0 && n == c.openAt+Period
	}
	r := c.swings[len(c.swings)-1]
	p := c.cfg.Plain
	if c.confirmedAt(r + 1) {
		p = Period // the batch for the swing at r was read in its stale tick: the next swing is 3 on
	}
	return n == r+p-c.lead
}
