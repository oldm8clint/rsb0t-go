package retaliateplan

import "fmt"

// The controller is the decision half of RetaliateChopper's tick loop. It follows chopmanipplan.Controller's shape (one
// observation per PLAYER_INFO, a decision written right after it, the lead measured from what the client can see) and is
// pure: no client, no actions.
//
// # What it is fed
//
// Obs{N, Hit, Roll} per tick: Hit is a hitmark on the local player (a block counts; the rats hit for 0) and Roll an axe
// chop animation, the sign of a roll (the same reading chopmanipplan uses). Nothing else the client can see tells a roll
// from a stale swing, and no ack cheat exists in production.
//
// # What it decides
//
// Two rats whose hits are exactly 2 ticks apart give a hit on every second tick. The loop is, per predicted hit tick H
// (the engine reads a write made after the PLAYER_INFO of tick k in tick k+1+lead):
//
//	read at H    a Clear: anything that drops the player's interaction, so busy2 is false when the monster hits
//	read at H+1  a Click on the tree: rolls at once, because the flinch left the varp at H+1 (docs/two_tick_retaliate_research
//	             section 2.3 and 2.6; retaliateplan.Sim reproduces it)
//
// With lead L the write for the Click is made after tick H-L and the write for the Clear after tick H-1-L: so every tick
// carries exactly one write, alternating, whatever the lead. A tick N writes the Click when (N+L) has the parity of the
// hits, the Clear otherwise. The loop needs no reaction to a hit once locked: the phase is predicted from the last hits.
//
// # Phase and lead
//
// It is locked when the last LockHits hits were each 2 ticks apart; two predicted hits in a row that did not come, or a
// hit on the wrong parity, unlock it. While unlocked it writes nothing (the runner chops plain). The lead starts at the
// setting and is rotated when the aimed clicks keep missing: a click aimed to be read at H+1 should show a roll animation
// in tick H+1, and a window of WindowLen aims with half or more misses moves the lead to the next value (the link delay is
// steady, so one of 0, 1 and 2 fits). It never invents a delay: the runner reads the real roll.
//
// # When it gives up
//
// Verdict says Rephase when the hits have not been 2 ticks apart for Warmup ticks, or when the lead has been rotated through
// every value without the aims landing. The runner then re-phases the rats (up to the settings' PhaseTries) and, after that,
// falls back to ordinary chopping.
const (
	// MaxLead is the largest lead the controller aims with.
	MaxLead = 2
	// LockHits is how many consecutive hits, each 2 ticks apart, make the phase locked.
	LockHits = 4
	// WindowLen is how many aims one lead is judged over.
	WindowLen = 8
	// Warmup is how many ticks after a reset the controller gives the rats to settle into a 2-tick spacing.
	Warmup = 24
	// GainMinGaps is how many roll gaps the share of 2-tick gaps is judged over.
	GainMinGaps = 16
	// MaxGap is the longest roll gap that counts for the share: a longer one is a tree swap or a wait for a tree.
	MaxGap = 12
)

// Action is what the loop writes right after a PLAYER_INFO.
type Action int

// The actions.
const (
	ActNone Action = iota
	ActClear
	ActClick
)

func (a Action) String() string {
	switch a {
	case ActClear:
		return "clear"
	case ActClick:
		return "click"
	}
	return "none"
}

// Obs is one PLAYER_INFO as the client saw it.
type Obs struct {
	N    int
	Hit  bool
	Roll bool
}

// Pattern is the shape of the recent hit spacing.
type Pattern int

// The patterns ClassifyHits reports.
const (
	PatternTooFew Pattern = iota
	// PatternTwoApart is the wanted one: hits every 2 ticks.
	PatternTwoApart
	// PatternOneRat is a hit every 4 ticks: one rat hitting, or both on the same tick.
	PatternOneRat
	// PatternOneApart is hits 1 and 3 ticks apart in turn: the rats are 1 tick out.
	PatternOneApart
	PatternOther
)

func (p Pattern) String() string {
	switch p {
	case PatternTwoApart:
		return "2 ticks apart"
	case PatternOneRat:
		return "every 4 ticks (one rat, or both on one tick)"
	case PatternOneApart:
		return "1 and 3 ticks apart in turn"
	case PatternOther:
		return "irregular"
	}
	return "too few hits"
}

// ClassifyHits reads the shape of the spacing of the last hits (ticks, oldest first). It needs four hits.
func ClassifyHits(hits []int) Pattern {
	if len(hits) < LockHits {
		return PatternTooFew
	}
	h := hits[len(hits)-LockHits:]
	g := []int{h[1] - h[0], h[2] - h[1], h[3] - h[2]}
	switch {
	case g[0] == 2 && g[1] == 2 && g[2] == 2:
		return PatternTwoApart
	case g[0] == 4 && g[1] == 4 && g[2] == 4:
		return PatternOneRat
	case g[0] == g[2] && g[0]+g[1] == 4 && g[0] != 2:
		return PatternOneApart
	}
	return PatternOther
}

// Stats is what a controller counted, for the log line.
type Stats struct {
	Clicks, Clears int
	// AimHit and AimMiss count the aimed clicks whose read tick did or did not show a roll (only counted when the hit
	// before them was seen, so a wrong phase is not blamed on the link).
	AimHit, AimMiss int
	Retunes         int
	Locks, Unlocks  int
	Rolls           int
	// Gaps2, Gaps3, Gaps4, GapsMid and GapsLong bucket the ticks between consecutive rolls: 2, 3, 4, 5 to 7, 8 or more.
	Gaps2, Gaps3, Gaps4, GapsMid, GapsLong int
	Hits                                   int
}

// Line is the stats as one log fragment.
func (s Stats) Line() string {
	return fmt.Sprintf("%d rolls; roll gaps 2/3/4/5-7/8+ ticks: %d/%d/%d/%d/%d; %d clicks, %d clears; aimed clicks that rolled on time %d of %d; %d lead re-aims; %d hits; locked %d times, lost %d times",
		s.Rolls, s.Gaps2, s.Gaps3, s.Gaps4, s.GapsMid, s.GapsLong, s.Clicks, s.Clears, s.AimHit, s.AimHit+s.AimMiss, s.Retunes, s.Hits, s.Locks, s.Unlocks)
}

// Verdict is the controller's opinion of how the loop is going.
type Verdict int

// The verdicts.
const (
	VerdictRun Verdict = iota
	VerdictRephase
)

// Controller is one account's two-tick loop. Its zero value is not usable; use NewController. It is not safe for concurrent
// use: the runner calls it from the loop only.
type Controller struct {
	cfg  Settings
	lead int

	seen    bool
	started int // tick of the first observation since the last Reset
	lastN   int

	hits       []int // ticks of the latest hits, oldest first
	locked     bool
	parity     int
	missed     int
	lastChange int // tick of the last lock or unlock

	aims  map[int]bool // read ticks a click was aimed at
	win   []bool       // the latest WindowLen aim results
	spins int          // lead rotations since the aims last landed

	rolls []int // roll ticks of the current tree (after the last swap)
	gaps  []int // the latest roll gaps that count for the share
	held  bool

	stats Stats
}

// NewController returns a controller with the (normalised) settings' starting lead.
func NewController(s Settings) *Controller {
	s = s.Normalised()
	return &Controller{cfg: s, lead: s.Lead, aims: map[int]bool{}}
}

// Lead is the aim now: how many ticks before the tick it should be read a write is made.
func (c *Controller) Lead() int { return c.lead }

// Locked reports whether the hit phase is locked (the loop is writing).
func (c *Controller) Locked() bool { return c.locked }

// Stats returns the counters since the controller was made (Reset keeps them).
func (c *Controller) Stats() Stats { return c.stats }

// Pattern is the shape of the recent hit spacing.
func (c *Controller) Pattern() Pattern { return ClassifyHits(c.hits) }

// Hits is the ticks of the latest hits, oldest first (a copy).
func (c *Controller) Hits() []int { return append([]int(nil), c.hits...) }

// Parity is the hit parity while locked (hits fall on ticks with N%2 == Parity).
func (c *Controller) Parity() int { return c.parity }

// SetHeld stops (true) or resumes (false) the writes: the runner holds while no tree stands. Hits keep being tracked, so a
// resume finds the phase still locked.
func (c *Controller) SetHeld(h bool) { c.held = h }

// NoteSwap forgets the roll history of the tree just left: the gap across a swap is not a chop gap.
func (c *Controller) NoteSwap() { c.rolls = nil }

// Reset forgets the phase, the windows and the clock of the warm-up. The runner calls it after re-phasing the rats. The
// counters and the lead stay.
func (c *Controller) Reset() {
	c.seen = false
	c.hits, c.locked, c.missed = nil, false, 0
	c.aims = map[int]bool{}
	c.win, c.spins = nil, 0
	c.rolls, c.gaps = nil, nil
}

// Observe feeds the PLAYER_INFO of tick o.N and returns what to write right after it.
func (c *Controller) Observe(o Obs) Action {
	if c.seen && o.N <= c.lastN {
		return ActNone
	}
	if !c.seen {
		c.seen, c.started, c.lastChange = true, o.N, o.N
	}
	c.lastN = o.N
	if o.Hit {
		c.noteHit(o.N)
	} else {
		c.noteNoHit(o.N)
	}
	c.noteAim(o)
	if o.Roll {
		c.noteRoll(o.N)
	}
	if !c.locked || c.held {
		return ActNone
	}
	return c.decide(o.N)
}

func (c *Controller) noteHit(n int) {
	c.stats.Hits++
	c.missed = 0
	if c.locked && n%2 != c.parity {
		c.unlock(n)
		c.hits = nil
	}
	c.hits = append(c.hits, n)
	if len(c.hits) > 8 {
		c.hits = c.hits[len(c.hits)-8:]
	}
	if !c.locked && ClassifyHits(c.hits) == PatternTwoApart {
		c.locked, c.parity, c.lastChange = true, n%2, n
		c.stats.Locks++
		c.gaps, c.rolls = nil, nil
	}
}

func (c *Controller) noteNoHit(n int) {
	if !c.locked || n%2 != c.parity {
		return
	}
	if c.missed++; c.missed >= 2 {
		c.unlock(n)
	}
}

func (c *Controller) unlock(n int) {
	c.locked, c.lastChange, c.missed = false, n, 0
	c.aims = map[int]bool{}
	c.win, c.spins = nil, 0
	c.gaps = nil
	c.stats.Unlocks++
}

// decide is the alternation: the Click when (n+lead) has the hit parity, the Clear otherwise.
func (c *Controller) decide(n int) Action {
	if (n+c.lead)%2 == c.parity {
		c.aims[n+1+c.lead] = true
		c.stats.Clicks++
		return ActClick
	}
	c.stats.Clears++
	return ActClear
}

// noteAim judges a click aimed at this tick. A roll animation at the read tick is the sign it landed; the judgement is made
// only when the hit just before it was seen, so a phase that slipped is not blamed on the lead.
func (c *Controller) noteAim(o Obs) {
	for t := range c.aims {
		if t < o.N {
			delete(c.aims, t)
		}
	}
	if !c.aims[o.N] {
		return
	}
	delete(c.aims, o.N)
	if n := len(c.hits); n == 0 || c.hits[n-1] != o.N-1 {
		return
	}
	if o.Roll {
		c.stats.AimHit++
	} else {
		c.stats.AimMiss++
	}
	c.win = append(c.win, o.Roll)
	if len(c.win) > WindowLen {
		c.win = c.win[len(c.win)-WindowLen:]
	}
	if len(c.win) < WindowLen {
		return
	}
	hit := 0
	for _, ok := range c.win {
		if ok {
			hit++
		}
	}
	switch {
	case hit*2 < WindowLen:
		c.lead = (c.lead + 1) % (MaxLead + 1)
		c.spins++
		c.stats.Retunes++
		c.win = nil
	case hit >= WindowLen*3/4:
		c.spins = 0
	}
}

func (c *Controller) noteRoll(n int) {
	c.stats.Rolls++
	if k := len(c.rolls); k > 0 {
		g := n - c.rolls[k-1]
		switch {
		case g == 2:
			c.stats.Gaps2++
		case g == 3:
			c.stats.Gaps3++
		case g == 4:
			c.stats.Gaps4++
		case g < 8:
			c.stats.GapsMid++
		default:
			c.stats.GapsLong++
		}
		if g <= MaxGap {
			c.gaps = append(c.gaps, g)
			if len(c.gaps) > 2*GainMinGaps {
				c.gaps = c.gaps[len(c.gaps)-2*GainMinGaps:]
			}
		}
	}
	c.rolls = append(c.rolls, n)
	if len(c.rolls) > 4 {
		c.rolls = c.rolls[len(c.rolls)-4:]
	}
}

// GainShare is the share of the latest roll gaps (since the phase locked) that were 2 ticks, and how many there were.
func (c *Controller) GainShare() (share float64, n int) {
	if len(c.gaps) == 0 {
		return 0, 0
	}
	two := 0
	for _, g := range c.gaps {
		if g == 2 {
			two++
		}
	}
	return float64(two) / float64(len(c.gaps)), len(c.gaps)
}

// Verdict says whether the loop is working or the rats need re-phasing. The second return is why, for the log.
func (c *Controller) Verdict() (Verdict, string) {
	if !c.seen || c.held || c.lastN-c.started < Warmup {
		return VerdictRun, ""
	}
	if !c.locked && c.lastN-c.lastChange >= Warmup {
		return VerdictRephase, fmt.Sprintf("the rats' hits are not 2 ticks apart (%s, last hits %v)", c.Pattern(), c.hits)
	}
	if c.locked && c.spins > MaxLead {
		return VerdictRephase, fmt.Sprintf("the aimed clicks keep missing at every lead (0, 1 and 2 ticks were all tried): %d of %d rolled on time", c.stats.AimHit, c.stats.AimHit+c.stats.AimMiss)
	}
	if share, n := c.GainShare(); c.locked && n >= GainMinGaps && share < c.cfg.MinGain && c.lastN-c.lastChange >= 2*Warmup {
		return VerdictRephase, fmt.Sprintf("only %.0f%% of the last %d roll gaps were 2 ticks (want %.0f%%)", share*100, n, c.cfg.MinGain*100)
	}
	return VerdictRun, ""
}

// Rephaser counts the re-phase attempts of one setup.
type Rephaser struct {
	Max  int
	used int
}

// Next takes one attempt: its number (from 1) and whether any were left.
func (r *Rephaser) Next() (int, bool) {
	if r.used >= r.Max {
		return r.used, false
	}
	r.used++
	return r.used, true
}

// Used is how many attempts have been taken.
func (r *Rephaser) Used() int { return r.used }

// Held says the loop has run well for a long stretch, which earns the attempts back: a rare drift in hours of 2-tick chopping
// should not count against the account.
func (r *Rephaser) Held() { r.used = 0 }
