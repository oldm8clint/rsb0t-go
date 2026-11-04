package runecraftplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// ALCHEMY ON THE GATE'S WALKING LEGS (CX-1 of docs/xp_rate_plan_2026-10-19.md, masterspeed step 4, 20 Oct 2026).
//
// The owner (19 Oct 2026): "in between trades at the altar the main account could be alching the highest tier of strung bows available". At a saturated
// gate no idle second exists between the trades -- a carrier is always within the 12 tiles DecideGateAlch keeps clear -- so the gate's cast
// rate fell to a few an hour. But two of the cycle's legs are walks inside the temple where nobody can ask for a trade: the four steps to
// the altar and the four to the portal. A cast does not stop a walk (Player.updateMovement moves a delayed player; only a pending move
// click with a script waiting stops it, and the clear-move in front of the cast, actions.ClearMoveRequest, takes that away), so a cast
// read in the middle of a leg costs nothing while the walk is still going on. This file is the pure half: which legs, which spell, what
// it may cost, and the memory that stands the casts down when they cost more than they should.
//
// WHAT THE ENGINE ALLOWS (read for this step: World.processClientsIn, MoveClickHandler, OpHeldTHandler, Player.processInteraction,
// alchemy.rs2; the plan's text for CX-1 was written before this was read and is wrong in one place, said below).
//
//   - A cast (OPHELDT) calls clearPendingAction: it drops the interaction (the altar or portal click the direct op queued) and keeps the
//     walk queue. So the direct op of CR-1 must be sent AGAIN after the cast.
//   - THE PLAN'S "WALK, CLEAR-MOVE AND CAST IN ONE WRITE" LOSES THE WALK. World.processClientsIn, after a tick's packets are decoded:
//     `if (player.userPath.length > 0 || player.opcalled) { if (player.delayed) { player.unsetMapFlag(); continue } }`. A movement click
//     read in the same tick as a cast that delays the player is thrown away, route and all (scarabplan/walkalch.go has known it since the
//     walk alchemy, WalkAlchAfterClick). So the cast is read at least one tick AFTER the walk: tick R the walk, tick R+1 the cast at the
//     earliest. (The direct op's walk and click are still ONE write: the click is the walk's pair and clears nothing the walk needs.)
//   - The cast is p_delay(n): Low Level Alchemy n=1, High n=3. A packet read in the n+1 ticks after the cast's own tick is dropped
//     (OpLocHandler, MoveClickHandler: `if (player.delayed)`), so the first read that is accepted is the cast's tick + n + 2:
//     LegSpell.AcceptedAfter. The interaction is not processed while delayed either (Player.tryInteract needs canAccess), but the
//     walk is.
//   - A loc's op runs in the first tick after the walk's last step (processInteraction: movement, then tryInteract(stepsTaken === 0)).
//     A leg of s steps walked from tick R therefore has its op at R+s, whether the click was read with the walk or any time before R+s.
//
// So a cast read at R+1 and the click sent again for the first accepted read, R+1+n+2, costs max(0, n+3-s) ticks over the plain direct op:
// nothing for Low Level Alchemy on a leg of 4 steps (cast R+1, locked R+2 and R+3, click read R+4, op R+4), two ticks for High Level
// Alchemy (the click is read at R+6). That is LegCastCost. The plan's "High +1 per leg" assumed the cast could share the walk's tick.
//
// THE SETTING. craftRuneLegCasts: "low2" (default: Low Level Alchemy, 31 Magic xp, on both walking legs: about nothing in ticks), "high1"
// (High Level Alchemy, 65 xp, on the portal leg only: 2 ticks), "high2" (both legs: 4 ticks) and "off". The casts need the bows in the
// pack through the cycle, which is craftRuneAlchHold; without it the stack is gone before the first trade and there is nothing to cast.
// Both spells pay for the bow whatever the tier (Low 0.4 of its cost, High 0.6) and 1 Nature rune a cast; the Staff of fire pays the Fire
// runes of both. The highest tier in the pack is cast first (AlchBows' order: Magic longbow before Magic shortbow).
//
// THE TIMING THE CAST RESTS ON IS THE NETWORK'S, so it is measured and the casts stand down when it does not hold. The cast is written
// one tick and LegCastMargin after the click (a timer from the click's own write, not the frame clock: a frame arrives at any moment of a
// tick); a jitter that lets it be read in the click's own tick would drop the walk, one that slips it a tick costs a tick. The re-sent
// click goes out once per frame for LegCastResends frames around the lock's end, so whichever copy is the first read after it is the one
// that counts and the rest are dropped or repeat it. Every leg's cost over the plain direct op is measured in ticks (runecraftrunner's
// ride) and LegCasts stands a leg's casts down when their mean over LegCastWindow of them is more than LegCastMaxMean ticks above what
// they should cost, or when they fail (the cast refused, the walk lost, the click not read).
const (
	// LegCastMargin is how long after one tick from the direct op's write the cast is written.
	LegCastMargin = 40 * time.Millisecond
	// LegCastResends is how many frames, one after the other, the click is sent again around the lock's end (see above).
	LegCastResends = 3
	// LegCastLandTicks is how many ticks after the cast was written it is waited on to show before it is taken for refused or lost.
	LegCastLandTicks = 4
	// LegCastWindow is how many casts of a leg are judged together, and LegCastMaxMean how many ticks over its expected cost they may
	// average. Eight casts at 0.6 are about two cycles' worth of one-tick slips.
	LegCastWindow  = 8
	LegCastMaxMean = 0.6
	// LegCastBackoff is how many cycles a leg that cost too much walks without a cast, LegCastFailBackoff how many after failures, and
	// LegCastGiveUp how many stand-downs in a row (no healthy window between) end the leg's casts for the round.
	LegCastBackoff     = 30
	LegCastFailBackoff = 15
	LegCastGiveUp      = 3
	// LegCastFailsInARow is how many failed casts in a row stand a leg down.
	LegCastFailsInARow = 2
	// LegCastMinSteps is the shortest leg a cast is planned on: under it the lock cannot end before the leg does, whatever the spell.
	LegCastMinSteps = 3
	// LegCastMinPaceTicks is the fewest ticks the view may have seen in its last three seconds (gamestate.ViewWindow) for a cast to be
	// planned: the cast is written a tick and a margin after the click by a timer set for a 600 ms tick, so a server that ticks slowly
	// (four in three seconds is 750 ms) would read it in the click's own tick and throw the walk away. A healthy server shows five or six.
	LegCastMinPaceTicks = 4
)

// LegCastMode is craftRuneLegCasts.
type LegCastMode string

// The modes.
const (
	LegCastsOff   LegCastMode = "off"
	LegCastsLow2  LegCastMode = "low2"
	LegCastsHigh1 LegCastMode = "high1"
	LegCastsHigh2 LegCastMode = "high2"
	// DefaultLegCasts is the default mode.
	DefaultLegCasts = LegCastsLow2
)

// LegCastModes are the modes a setting may name.
func LegCastModes() []LegCastMode {
	return []LegCastMode{LegCastsOff, LegCastsLow2, LegCastsHigh1, LegCastsHigh2}
}

// ParseLegCastMode reads a setting value (case and spaces ignored).
func ParseLegCastMode(s string) (LegCastMode, bool) {
	m := LegCastMode(strings.ToLower(strings.TrimSpace(s)))
	for _, ok := range LegCastModes() {
		if m == ok {
			return m, true
		}
	}
	return "", false
}

// LegSpell is an alchemy spell cast on a leg.
type LegSpell struct {
	// Name is the spell as actions.CastOnItem names it; Short the word the log uses.
	Name, Short string
	// Level is the Magic level it needs, XP what a cast gives, Delay the p_delay of alchemy.rs2 and PayTenths the tenths of the item's cost
	// a cast pays (4 for Low, 6 for High).
	Level, XP, Delay, PayTenths int
}

// The spells (alchemy.rs2; scripts/alchplan has the same numbers for the idle casts).
var (
	LowLegSpell  = LegSpell{Name: "Low Level Alchemy", Short: "low", Level: 21, XP: 31, Delay: 1, PayTenths: 4}
	HighLegSpell = LegSpell{Name: "High Level Alchemy", Short: "high", Level: AlchMagic, XP: 65, Delay: 3, PayTenths: 6}
)

// AcceptedAfter is the ticks from the cast's own tick to the first read the engine accepts: p_delay(n) drops the reads of the next n+1
// ticks (delayedUntil = tick+1+n, cleared after the reads of that tick).
func (s LegSpell) AcceptedAfter() int { return s.Delay + 2 }

// Pays is the coins one cast pays for an item of that cost (at least 1: alchemy.rs2).
func (s LegSpell) Pays(cost int) int { return max(cost*s.PayTenths/10, 1) }

// SpellFor is the spell the mode casts on a leg; ok is false for a leg it does not cast on.
func (m LegCastMode) SpellFor(l Leg) (LegSpell, bool) {
	switch m {
	case LegCastsLow2:
		return LowLegSpell, true
	case LegCastsHigh2:
		return HighLegSpell, true
	case LegCastsHigh1:
		if l == LegPortal {
			return HighLegSpell, true
		}
	}
	return LegSpell{}, false
}

// Casts reports whether the mode casts on any leg.
func (m LegCastMode) Casts() bool {
	_, a := m.SpellFor(LegAltar)
	_, p := m.SpellFor(LegPortal)
	return a || p
}

// LegWalkTicks is the ticks a walk of tiles tiles takes: a tile a tick, two when the account runs (the run toggle on with energy to run on;
// the direct op's click does not set ctrl, so the toggle decides). A leg that runs is over before a cast's lock is, which is why a leg that
// runs is not cast on.
func LegWalkTicks(tiles int, running bool) int {
	if running {
		return (tiles + 1) / 2
	}
	return tiles
}

// LegCastCost is the ticks a cast costs a leg that walks for ticks ticks over the plain direct op, when everything goes as planned: the
// cast is read one tick after the walk, locks the account for AcceptedAfter ticks and the click is read the moment the lock is over, while
// the walk's last step is at ticks-1 ticks after the walk was read.
func LegCastCost(s LegSpell, ticks int) int { return max(0, 1+s.AcceptedAfter()-ticks) }

// LegCastCostLimit is the most ticks a leg's cast may be planned to cost: none for Low Level Alchemy (the free cast), and for High Level
// Alchemy what the owner chose by naming it (two on a four-step leg, none more).
func LegCastCostLimit(s LegSpell) int {
	if s.Delay >= HighLegSpell.Delay {
		return 2
	}
	return 0
}

// LegCastView is what DecideLegCast reads.
type LegCastView struct {
	Leg  Leg
	Mode LegCastMode
	// Hold is craftRuneAlchHold: the bows stay in the pack through the trades. Stack is the noted bows held.
	Hold  bool
	Stack int
	// Steps is the tiles the leg's direct op walks, Running that the account will run them (run on, with energy): the leg is then half the
	// ticks (LegWalkTicks).
	Steps   int
	Running bool
	// Magic and MagicXP are the account's Magic level and experience; FireStaff a fire staff worn; Natures the Nature runes held.
	Magic, MagicXP int
	FireStaff      bool
	Natures        int
	// Locked is a cast's lock still on, Busy a random event on screen, Shaky a view that lags or was silent a minute ago, Chat a chat
	// box open (a level-up's), which stops every op.
	Locked, Busy, Shaky, Chat bool
	// PaceTicks is the ticks the view saw in its last three seconds (0: not known).
	PaceTicks int
}

// LegCastDecision is DecideLegCast's answer.
type LegCastDecision struct {
	Cast  bool
	Spell LegSpell
	// Cost is the ticks the cast is planned to cost over the plain direct op.
	Cost int
	Why  string
}

// DecideLegCast says whether the leg gets a cast. Every refusal is the plain direct op, with the reason in words.
func DecideLegCast(v LegCastView) LegCastDecision {
	no := func(why string) LegCastDecision { return LegCastDecision{Why: why} }
	ticks := LegWalkTicks(v.Steps, v.Running)
	spell, ok := v.Mode.SpellFor(v.Leg)
	switch {
	case !ok:
		return no("craftRuneLegCasts " + strconv.Quote(string(v.Mode)) + " casts nothing on the " + v.Leg.String() + " leg")
	case !v.Hold:
		return no("the bows are not held through the trades (craftRuneAlchHold is off), so none is in the pack to cast")
	case v.Stack <= 0:
		return no("no bows held")
	case v.Magic < spell.Level:
		return no("Magic " + strconv.Itoa(v.Magic) + " is under the " + strconv.Itoa(spell.Level) + " " + spell.Name + " needs")
	case !v.FireStaff:
		return no("no fire staff is worn for the Fire runes")
	case v.Natures <= AlchNaturesFloor/2:
		return no("no Nature rune to cast with above the floor")
	case v.Shaky:
		return no("the view is lagging or was silent in the last minute")
	case v.PaceTicks > 0 && v.PaceTicks < LegCastMinPaceTicks:
		return no(strconv.Itoa(v.PaceTicks) + " tick(s) in the last 3 s: the server is ticking slowly, and the cast's timer is set for a 600 ms tick")
	case v.Locked:
		return no("the last cast's lock is not over")
	case v.Busy:
		return no("a random event is on screen")
	case v.Chat:
		return no("a chat box is open")
	case ticks < LegCastMinSteps:
		if v.Running {
			return no("the account runs this leg (run is on with energy), so " + strconv.Itoa(v.Steps) + " tiles are " + strconv.Itoa(ticks) + " tick(s): too few for the lock to end before the leg does")
		}
		return no(strconv.Itoa(ticks) + " step(s) are too few for the lock to end before the leg does")
	case LegCastLevelsUp(v.Magic, v.MagicXP, spell.XP):
		return no("the cast would level Magic up, and a level-up's chat box stops the click")
	}
	cost := LegCastCost(spell, ticks)
	if cost > LegCastCostLimit(spell) {
		return no(fmt.Sprintf("%s costs %d tick(s) on a leg of %d tick(s) (the limit is %d)", spell.Name, cost, ticks, LegCastCostLimit(spell)))
	}
	return LegCastDecision{Cast: true, Spell: spell, Cost: cost, Why: "the leg is a walk with nobody to trade with"}
}

// LegCastLevelsUp says a cast of xp experience would take Magic to the next level: the level-up opens a chat box (a modal that busies the
// player), and the click that follows the lock would be dropped by it. The idle casts meet that box at the next stage; a cast whose
// timing is the point does not.
func LegCastLevelsUp(level, xp, gain int) bool {
	if level <= 0 || xp <= 0 {
		return false
	}
	return xp+gain >= levels.XPAtLevel(level+1)
}

// ---- the ride's record and the judging ----

// LegCastRecord is what the cast on one leg did, in the client's own frame numbers (the ticks of the PLAYER_INFO it saw).
type LegCastRecord struct {
	Leg   Leg
	Spell LegSpell
	Bow   string
	// Steps is the tiles the leg walked. FirstStep is the frame the first step was seen in (R: the walk was read in that tick), Sent the
	// frame the cast was written in, Landed the frame it was first seen to have landed in (0: it never was), Op the frame the leg's op
	// (the craft, the exit) was seen in (0: it was not).
	Steps, FirstStep, Sent, Landed, Op int
	// Resends is how many times the click was sent again, WalkLost that the walk was found thrown away (the account never moved) and sent
	// again with the click.
	Resends  int
	WalkLost bool
	// Tried is the cast having been written; Missed that it was written and never seen to land.
	Tried, Missed bool
	// Why is what went wrong, when the leg failed because of the cast.
	Why string
}

// Ideal is the frame the leg's op would be seen in without the cast: the walk's first step plus its steps.
func (r LegCastRecord) Ideal() int {
	if r.FirstStep <= 0 {
		return 0
	}
	return r.FirstStep + r.Steps
}

// Cost is the ticks the cast added to the leg: the op's frame over Ideal (0 when either is not known).
func (r LegCastRecord) Cost() int {
	if r.Op <= 0 || r.Ideal() <= 0 {
		return 0
	}
	return r.Op - r.Ideal()
}

// Slip is how many ticks later than the earliest legal read the cast was read: it cannot be read in the walk's own tick, so the earliest
// is the first step's frame plus one (0 when it was on time, negative when the walk was thrown away and the cast was read with it).
func (r LegCastRecord) Slip() int {
	if r.Landed <= 0 || r.FirstStep <= 0 {
		return 0
	}
	return r.Landed - (r.FirstStep + 1)
}

// Excess is the ticks over what the cast was planned to cost.
func (r LegCastRecord) Excess() int { return r.Cost() - LegCastCost(r.Spell, r.Steps) }

// String is the record in the log: "altar low (Magic longbow): walk seen 5018, cast written 5018, landed 5019 (on time), click sent again 3
// time(s), craft seen 5022: +0 tick(s) over the walk (planned +0)".
func (r LegCastRecord) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", r.Leg, r.Spell.Short)
	if r.Bow != "" {
		fmt.Fprintf(&b, " (%s)", r.Bow)
	}
	if !r.Tried {
		b.WriteString(": not cast")
		if r.Why != "" {
			b.WriteString(" (" + r.Why + ")")
		}
		return b.String()
	}
	fmt.Fprintf(&b, ": walk seen %d, cast written %d", r.FirstStep, r.Sent)
	switch {
	case r.Landed > 0:
		fmt.Fprintf(&b, ", landed %d (%s)", r.Landed, slipWord(r.Slip()))
	case r.Missed:
		b.WriteString(", never seen to land")
	}
	if r.WalkLost {
		b.WriteString(", WALK THROWN AWAY (the account never moved) and sent again")
	}
	fmt.Fprintf(&b, ", click sent again %d time(s)", r.Resends)
	if r.Op > 0 {
		fmt.Fprintf(&b, ", op seen %d: %+d tick(s) over the walk (planned %+d)", r.Op, r.Cost(), LegCastCost(r.Spell, r.Steps))
	}
	if r.Why != "" {
		b.WriteString(" (" + r.Why + ")")
	}
	return b.String()
}

func slipWord(s int) string {
	switch {
	case s == 0:
		return "on time"
	case s < 0:
		return "in the walk's own tick"
	}
	return fmt.Sprintf("%d tick(s) late", s)
}

// ---- the score ----

// LegCastOutcome is what one cast on a leg came to, for the score.
type LegCastOutcome int

// The outcomes.
const (
	// LegCastOK: the cast landed and the leg's op came off. Its cost over what was planned is judged.
	LegCastOK LegCastOutcome = iota
	// LegCastFailed: the cast did not land (refused or lost), or the walk was thrown away, or the leg's op did not come off after it.
	LegCastFailed
	// LegCastUnseen: the view lagged and the result was not seen in time: nothing is known.
	LegCastUnseen
)

type castLeg struct {
	// window holds the excess ticks of the last casts, wait the cycles still to run without a cast, fails the failures in a row,
	// standDowns the stand-downs since a window that was healthy; off: given up.
	window     []int
	wait       int
	fails      int
	standDowns int
	off        bool
	// The round's counts and sums.
	ok, failed, unseen, plain int
	xp                        int
	cost, excess              int
	slips, lost               int
}

// LegCasts is a round's memory of how the casts on the legs are doing. A nil *LegCasts (and one made with a mode that casts nothing)
// never casts.
type LegCasts struct {
	mode LegCastMode
	legs [legCount]castLeg
}

// NewLegCasts is the state for a round; mode is craftRuneLegCasts.
func NewLegCasts(mode LegCastMode) *LegCasts { return &LegCasts{mode: mode} }

// On reports whether any leg may be cast on.
func (c *LegCasts) On() bool { return c != nil && c.mode.Casts() }

// Mode is the setting's mode (off for a nil state).
func (c *LegCasts) Mode() LegCastMode {
	if c == nil {
		return LegCastsOff
	}
	return c.mode
}

// Try reports whether the leg is cast on now. A leg in its back-off (or given up) answers false and counts the plain cycle it is about to
// run, so the back-off is measured in cycles. Call it once per cycle of the leg, after every other reason not to cast has been ruled out.
func (c *LegCasts) Try(l Leg) bool {
	if !c.On() || l < 0 || l >= legCount {
		return false
	}
	if _, ok := c.mode.SpellFor(l); !ok {
		return false
	}
	s := &c.legs[l]
	if s.off {
		s.plain++
		return false
	}
	if s.wait > 0 {
		s.wait--
		s.plain++
		return false
	}
	return true
}

// GivenUp reports whether the leg's casts are off for the rest of the round.
func (c *LegCasts) GivenUp(l Leg) bool {
	return c != nil && l >= 0 && l < legCount && c.legs[l].off
}

// Note records what a cast came to and returns a line for the log ("" when nothing is worth saying). rec is the ride's record; for
// LegCastOK its Excess is judged against the window.
func (c *LegCasts) Note(l Leg, o LegCastOutcome, rec LegCastRecord) string {
	if !c.On() || l < 0 || l >= legCount {
		return ""
	}
	s := &c.legs[l]
	switch o {
	case LegCastUnseen:
		s.unseen++
		return ""
	case LegCastFailed:
		s.failed++
		s.fails++
		if rec.WalkLost {
			s.lost++
		}
		if s.fails >= LegCastFailsInARow {
			return c.standDown(l, LegCastFailBackoff, fmt.Sprintf("%d casts in a row failed (%s)", s.fails, rec.Why))
		}
		return fmt.Sprintf("leg cast: the %s cast failed (%s); the click goes the plain way this cycle", l, rec.Why)
	}
	s.ok++
	s.fails = 0
	s.xp += rec.Spell.XP
	s.cost += rec.Cost()
	s.excess += rec.Excess()
	if rec.Slip() > 0 {
		s.slips++
	}
	s.window = append(s.window, rec.Excess())
	if len(s.window) < LegCastWindow {
		return ""
	}
	sum := 0
	for _, e := range s.window {
		sum += e
	}
	mean := float64(sum) / float64(len(s.window))
	s.window = s.window[:0]
	if mean > LegCastMaxMean {
		return c.standDown(l, LegCastBackoff, fmt.Sprintf("the last %d casts cost %.2f tick(s) each over what they should (more than %.1f)", LegCastWindow, mean, LegCastMaxMean))
	}
	s.standDowns = 0
	return ""
}

// standDown puts the leg in a back-off of cycles cycles, or gives it up after LegCastGiveUp stand-downs with no healthy window between.
func (c *LegCasts) standDown(l Leg, cycles int, why string) string {
	s := &c.legs[l]
	s.window = s.window[:0]
	s.fails = 0
	s.standDowns++
	if s.standDowns >= LegCastGiveUp {
		s.off = true
		return fmt.Sprintf("leg cast: the %s leg's casts are off for the rest of the round: %s (stood down %d times with no healthy run between)", l, why, s.standDowns)
	}
	s.wait = cycles
	return fmt.Sprintf("leg cast: the %s leg walks without a cast for the next %d cycle(s): %s", l, cycles, why)
}

// Casts is the landed casts of the round on the leg, and Total all legs'.
func (c *LegCasts) Casts(l Leg) int {
	if c == nil || l < 0 || l >= legCount {
		return 0
	}
	return c.legs[l].ok
}

// Total is the landed casts of the round and the Magic experience they gave.
func (c *LegCasts) Total() (casts, xp int) {
	if c == nil {
		return 0, 0
	}
	for l := Leg(0); l < legCount; l++ {
		casts += c.legs[l].ok
		xp += c.legs[l].xp
	}
	return casts, xp
}

// Summary is one line of the round's counts for the log.
func (c *LegCasts) Summary() string {
	if !c.On() {
		return "leg casts: off"
	}
	parts := make([]string, 0, int(legCount))
	for l := Leg(0); l < legCount; l++ {
		s := c.legs[l]
		if _, ok := c.mode.SpellFor(l); !ok {
			parts = append(parts, fmt.Sprintf("%s none", l))
			continue
		}
		p := fmt.Sprintf("%s %d cast, %d failed, %d not seen, %d plain", l, s.ok, s.failed, s.unseen, s.plain)
		if s.ok > 0 {
			p += fmt.Sprintf(", %.2f tick(s) over the walk on average (%.2f over plan), %d slipped a tick, %d walk(s) lost", float64(s.cost)/float64(s.ok), float64(s.excess)/float64(s.ok), s.slips, s.lost)
		}
		if s.off {
			p += " (given up)"
		}
		parts = append(parts, p)
	}
	casts, xp := c.Total()
	return fmt.Sprintf("leg casts (%s): %s; %s Magic xp from %d cast(s)", c.mode, strings.Join(parts, "; "), Thousands(xp), casts)
}
