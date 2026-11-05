package mastercraftplan

import (
	"fmt"
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/cannonfacts"
)

// THE CANNON RUN (a user request: "we want master to have a ranged training run built into master crafter that sets a cannon up and
// cannons khazard troopers and the gnomes in multi combat wearing range boosting gear, best shortbow attacking npcs and alching
// alchables with nature runes and fire runes in the inventory, that should trigger when there are 10k available cannon balls and should
// run until 10k cannonballs are used up, triggering again when the 10k balls are done").
//
// This file is every decision of the round and none of its I/O (mastercrafter_cannonrun.go does the walking and the clicking). The cannon's
// own cycle (set-up, load, fire, reload, pick-up, decay) is scarabplan/cannon.go's and the step planner scarabplan/rangedstint.go's: the
// same ones ScarabSwarm's graduates run at the Khazard battlefield, so there is one cannon cycle in the code, not two.
//
// THE CONTENT (quests/quest_mcannon/scripts/cannon_fire.rs2, cannon_setup.rs2, skill_magic/scripts/spells/alchemy.rs2; read 6 Nov 2026):
//
//   - A cannonball in the cannon is spent when it fires; a hit (the Ranged hit roll) does random(0..30) damage capped at the NPC's
//     hitpoints and pays Ranged experience of twice that damage. A Khazard trooper has 22 hitpoints (gnome_battlefield.npc), so at the
//     very most 27.7 experience a ball on average over the damage roll; a Gnome has 3 hitpoints and pays at most 6. The cannon fires at the
//     first attackable NPC in its ring, so a gnome in reach spends a ball for a fifth of a trooper's experience.
//   - The cannon's timer is a NORMAL timer ([timer,cannon_rotate], Player.processTimers: "normal cannot [run while busy]"): it does not
//     run while the player is DELAYED or has a modal interface open. High Level Alchemy ends in p_delay(3), four ticks in every five, so an
//     account that alchemises back to back holds its own cannon still for four ticks in five. craftCannonRunAlchDuty is the share of
//     time the alchemy may hold the cannon (default 50: a cast, then as long again to fire; 100 is the owner's continuous alchemy, 0 none).
//     Fletching (a two-tick action, no p_delay) and Prayer's bone burying (p_delay(0), one tick) leave the cannon running.
//   - Alchemy needs Magic 55, one Nature rune and five Fire runes (magic_spell_high_alch). A two-handed bow leaves no staff hand, so
//     the Fire runes are carried: 5 a cast.
//   - The cannon decays 25 minutes after its last stage (CannonDecay) and is destroyed by a King Black Dragon or a Kalphite Queen.
//
// THE RULES
//
//   - TRIGGER (CannonRunReady): the Cannonballs the mule publishes and this account's bank holds together reach craftCannonRunStart
//     (10,000), with the cannon, the Dwarf Cannon quest and a members world. The run's target is craftCannonRunBalls (10,000) balls
//     USED (taken out of the pack and not returned to it by the pick-up); it is kept in Ledger.CannonRun, so a stint cut short by the
//     trawler, a restart or a stint's end carries on, and the next trigger is a new run once the target is reached.
//   - THE CANNON: held whole (four parts) or bought from Nulodion for 750,000 coins (cannonfacts.CannonSetPrice) with the coins above a
//     reserve; with some parts held the round says so and waits (a part is 200,000 and the shop is not wired). A cannon this account set
//     up and did not take down is recorded (Ledger.CannonRun.Placed) and the next round recovers it first, claiming a decayed one from
//     Nulodion for nothing: a stint never leaves a cannon without its record.
//   - A STINT ends (CannonRunEnd) when the stint's minutes are up, the run's target is used, the account is out of food, arrows or balls,
//     it died, or the hold ends it; the cannon is ALWAYS taken down first (the round's wind phase), and a timeout never abandons it.
//   - ALCHEMY (CannonAlchDue): the High Level Alchemy cast clock's gate, with the duty limit above.

// ModeCannonRun is the cannon run round.
const ModeCannonRun Mode = "cannonrun"

// Setting keys of the cannon run.
const (
	KeyCannonRunStart        = "craftCannonRunStart"
	KeyCannonRunBalls        = "craftCannonRunBalls"
	KeyCannonRunCarry        = "craftCannonRunCarry"
	KeyCannonRunStintMinutes = "craftCannonRunStintMinutes"
	KeyCannonRunBuy          = "craftCannonRunBuy"
	KeyCannonRunAlchDuty     = "craftCannonRunAlchDuty"
	KeyCannonRunReserve      = "craftCannonRunCoinReserve"
)

// Defaults and bounds of the cannon run.
const (
	DefaultCannonRunStart   = 10000
	DefaultCannonRunBalls   = 10000
	DefaultCannonRunCarry   = cannonfacts.RangedBallsCarry
	DefaultCannonRunStint   = 60 * time.Minute
	DefaultCannonRunDuty    = 50
	DefaultCannonRunReserve = cannonfacts.MinRangedCoinReserve
	// CannonRunMinBalls is the fewest Cannonballs a run under way needs available to start another stint (a stint under that is a
	// walk for a minute of fire).
	CannonRunMinBalls = 300
	// CannonAlchMagic, CannonAlchNatures and CannonAlchFires are High Level Alchemy's level and runes.
	CannonAlchMagic   = 55
	CannonAlchNatures = 1
	CannonAlchFires   = 5
	// CannonAlchHoldTicks is how many ticks one cast holds the player (p_delay(3) is four ticks), and CannonAlchCastTicks the ticks from
	// one accepted cast to the next (fleetplan.WalkAlchCastTicks).
	CannonAlchHoldTicks = 4
	CannonAlchCastTicks = 5
)

func init() {
	for _, k := range []string{KeyCannonRunStart, KeyCannonRunBalls, KeyCannonRunCarry, KeyCannonRunStintMinutes, KeyCannonRunBuy, KeyCannonRunAlchDuty, KeyCannonRunReserve} {
		knownKeys[k] = true
	}
}

// CannonRunSettings are the round's settings.
type CannonRunSettings struct {
	// Start is the available Cannonballs that start a run (0 turns the round off); Balls the balls a run uses; Carry the most one stint
	// draws from the bank (a stack is one slot).
	Start, Balls, Carry int
	// Stint is how long one round lasts; AlchDuty the percent of the time alchemy may hold the cannon (0 turns alchemy off).
	Stint    time.Duration
	AlchDuty int
	// Buy lets the round buy the set from Nulodion; Reserve is the coins never spent on it.
	Buy     bool
	Reserve int
}

// DefaultCannonRunSettings are the owner's 10,000 and 10,000.
func DefaultCannonRunSettings() CannonRunSettings {
	return CannonRunSettings{Start: DefaultCannonRunStart, Balls: DefaultCannonRunBalls, Carry: DefaultCannonRunCarry, Stint: DefaultCannonRunStint,
		AlchDuty: DefaultCannonRunDuty, Buy: true, Reserve: DefaultCannonRunReserve}
}

// decode reads one key; it reports whether the key was one of the cannon run's.
func (c *CannonRunSettings) decode(k string, v interface{}, num func(string, interface{}, *int), mins func(string, interface{}, *time.Duration), flag func(string, interface{}, *bool)) bool {
	switch k {
	case KeyCannonRunStart:
		num(k, v, &c.Start)
	case KeyCannonRunBalls:
		num(k, v, &c.Balls)
	case KeyCannonRunCarry:
		num(k, v, &c.Carry)
	case KeyCannonRunStintMinutes:
		mins(k, v, &c.Stint)
	case KeyCannonRunBuy:
		flag(k, v, &c.Buy)
	case KeyCannonRunAlchDuty:
		num(k, v, &c.AlchDuty)
	case KeyCannonRunReserve:
		num(k, v, &c.Reserve)
	default:
		return false
	}
	return true
}

func (c CannonRunSettings) validate() []string {
	var p []string
	if c.Start < 0 || c.Start > 10_000_000 {
		p = append(p, fmt.Sprintf("%s %d is outside 0..10000000 (0 turns the round off)", KeyCannonRunStart, c.Start))
	}
	if c.Balls < 1 || c.Balls > 10_000_000 {
		p = append(p, fmt.Sprintf("%s %d is outside 1..10000000", KeyCannonRunBalls, c.Balls))
	}
	if c.Carry < 100 || c.Carry > 100_000 {
		p = append(p, fmt.Sprintf("%s %d is outside 100..100000", KeyCannonRunCarry, c.Carry))
	}
	if c.Stint < 10*time.Minute || c.Stint > 6*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 10..360 minutes", KeyCannonRunStintMinutes, c.Stint))
	}
	if c.AlchDuty < 0 || c.AlchDuty > 100 {
		p = append(p, fmt.Sprintf("%s %d is outside 0..100", KeyCannonRunAlchDuty, c.AlchDuty))
	}
	if c.Reserve < 0 {
		p = append(p, fmt.Sprintf("%s %d is below 0", KeyCannonRunReserve, c.Reserve))
	}
	return p
}

// CannonRunLedger is the round's memory.
type CannonRunLedger struct {
	// Active is a run under way: Target balls to use, Used so far, begun at StartedAt.
	Active    bool      `json:"active,omitempty"`
	Target    int       `json:"target,omitempty"`
	Used      int       `json:"used,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	// Rounds are the stints made, Runs the runs finished, Fired the balls used over every run; LastEnd when a run last finished.
	Rounds  int       `json:"rounds,omitempty"`
	Runs    int       `json:"runs,omitempty"`
	Fired   int       `json:"fired,omitempty"`
	LastEnd time.Time `json:"lastEnd,omitempty"`
	// PauseUntil holds the round off after one that could not start for a reason that will not pass by itself.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Placed is this account's cannon that may still stand in the world (cannonfacts.RangedRecord); Claimed counts the cannons
	// Nulodion returned and Bought the sets bought.
	Placed  cannonfacts.RangedRecord `json:"placed,omitempty"`
	Claimed int                      `json:"claimed,omitempty"`
	Bought  int                      `json:"bought,omitempty"`
}

// Remaining is the balls the run under way has still to use (0 with none under way).
func (l CannonRunLedger) Remaining() int {
	if !l.Active {
		return 0
	}
	return max(l.Target-l.Used, 0)
}

// Begin starts a run of target balls at now.
func (l *CannonRunLedger) Begin(target int, now time.Time) {
	l.Active, l.Target, l.Used, l.StartedAt = true, max(target, 1), 0, now
}

// NoteUsed adds balls used and reports whether that finished the run (the ledger is then idle and the next trigger is a new run).
func (l *CannonRunLedger) NoteUsed(n int, now time.Time) (done bool) {
	n = max(n, 0)
	l.Used += n
	l.Fired += n
	if l.Active && l.Used >= l.Target {
		l.Active, l.LastEnd = false, now
		l.Runs++
		return true
	}
	return false
}

// CannonRunInput is the round's reading of the account.
type CannonRunInput struct {
	// OwnKnown says the account's bank has been read since the script started (a restart has not read it yet).
	OwnKnown bool
	// OwnBalls are the Cannonballs in this account's bank and pack; Parts the distinct cannon parts held (0 to 4, bank and pack);
	// Coins the coins held (bank and pack).
	OwnBalls, Parts, Coins int
	// QuestKnown says the quest tab has been read; QuestDone that Dwarf Cannon is complete.
	QuestKnown, QuestDone bool
	// Ranged is the Ranged level (0 unknown).
	Ranged int
}

// CannonRunErrand is the one errand a round makes before the stint.
type CannonRunErrand string

const (
	// ErrandNone is no errand.
	ErrandNone CannonRunErrand = ""
	// ErrandRecover finds the cannon this account left standing (a stint cut short) and takes it down, or claims it from Nulodion once it
	// has decayed.
	ErrandRecover CannonRunErrand = "recover"
	// ErrandBuy buys the set from Nulodion.
	ErrandBuy CannonRunErrand = "buy"
)

// CannonRunPlan is a decided round.
type CannonRunPlan struct {
	// Resume is a run under way carried on, with no trigger.
	Resume bool
	// Available is the Cannonballs on hand (the mule's bank and this account's); Target and Remaining the run's.
	Available, Target, Remaining int
	// Errand is what the round does first.
	Errand CannonRunErrand
	Why    string
}

// CannonRunReady decides a cannon run round (see the file comment). It reports false with the reason in the plan's Why.
func CannonRunReady(set Settings, s Snapshot, led CannonRunLedger) (CannonRunPlan, bool) {
	var p CannonRunPlan
	c, in := set.CannonRun, s.CannonRun
	no := func(f string, a ...interface{}) (CannonRunPlan, bool) {
		p.Why = fmt.Sprintf(f, a...)
		return p, false
	}
	switch {
	case c.Start <= 0:
		return no("%s is 0", KeyCannonRunStart)
	case !set.MembersWorld:
		return no("a cannon needs a members world (%s is off)", KeyMembersWorld)
	case in.QuestKnown && !in.QuestDone:
		return no("%s is not complete: a cannon cannot be set up without it", DwarfCannonQuest)
	}
	p.Target, p.Remaining = led.Target, led.Remaining()
	// A cannon of this account's that may still stand is recovered before anything else: the round's first duty is never to lose it.
	if led.Placed.Placed() {
		p.Errand = ErrandRecover
		p.Why = "a cannon this account set up at " + led.Placed.PlacedAt + " may still stand: recovering it first (taken down, or claimed from Nulodion once it has decayed)"
		return p, true
	}
	stock := 0
	if s.Stock != nil {
		stock = stockOf(s.Stock, Cannonball)
	}
	p.Available = stock + in.OwnBalls
	if s.Stock == nil && in.OwnBalls == 0 {
		return no("the mule has not published its bank")
	}
	if led.Active && p.Remaining > 0 {
		if p.Available < min(p.Remaining, CannonRunMinBalls) {
			return no("a run is under way (%d of %d balls used) but only %d Cannonballs are available", led.Used, led.Target, p.Available)
		}
		p.Resume = true
	} else {
		p.Target, p.Remaining = c.Balls, c.Balls
		if p.Available < c.Start {
			return no("%d Cannonballs available (the mule's %d and this account's %d), under the %d that start a run", p.Available, stock, in.OwnBalls, c.Start)
		}
	}
	if in.OwnKnown {
		switch {
		case in.Parts >= cannonfacts.CannonPartsNeeded:
		case in.Parts == 0:
			price := cannonfacts.CannonSetPrice
			switch {
			case !c.Buy:
				return no("no cannon and %s is off (Nulodion sells the set for %d coins)", KeyCannonRunBuy, price)
			case in.Coins < price+c.Reserve:
				return no("no cannon: Nulodion's set costs %d coins and the account holds %d (with the %d reserve it needs %d)", price, in.Coins, c.Reserve, price+c.Reserve)
			}
			p.Errand = ErrandBuy
		default:
			return no("holds %d of the %d cannon parts: not buying a second set over them (a part is 200,000 at Nulodion's shop, which is not wired)", in.Parts, cannonfacts.CannonPartsNeeded)
		}
	}
	verb := "starting a run"
	if p.Resume {
		verb = fmt.Sprintf("carrying on a run (%d of %d used)", led.Used, led.Target)
	}
	p.Why = fmt.Sprintf("%s: %d Cannonballs available, %d to use", verb, p.Available, p.Remaining)
	if p.Errand == ErrandBuy {
		p.Why += "; buying the cannon set from Nulodion first"
	}
	return p, true
}

// CannonRunStintBalls is the Cannonballs one stint carries: the run's remainder, the carry limit and what is available.
func CannonRunStintBalls(set Settings, remaining, available int) int {
	return max(0, min(remaining, set.CannonRun.Carry, available))
}

// CannonRunEndInput is what a stint's end reads.
type CannonRunEndInput struct {
	Now, Start time.Time
	Stint      time.Duration
	// Used and Target are the run's, this stint's balls included.
	Used, Target int
	// Balls is the Cannonballs in the pack, CannonLoaded the balls the cannon is believed to hold and CannonUp that one stands.
	Balls, CannonLoaded int
	CannonUp            bool
	Food, Arrows        int
	Deaths              int
	// Stopped is the hold ending the round (the script stopping, the trawler's crew waiting in the lobby).
	Stopped bool
}

// CannonRunEnd says whether a stint is over, why, and whether that is a failure (the round's backoff): the caller takes the cannon down
// first, always.
func CannonRunEnd(in CannonRunEndInput) (end bool, why string, failed bool) {
	switch {
	case in.Stopped:
		return true, "stopped", false
	case in.Deaths > 0:
		return true, "died", true
	case in.Target > 0 && in.Used >= in.Target:
		return true, "the run's " + strconv.Itoa(in.Target) + " Cannonballs are used", false
	case in.Stint > 0 && in.Now.Sub(in.Start) >= in.Stint:
		return true, "the stint's " + in.Stint.String() + " are up", false
	case in.Food <= 0:
		return true, "out of food", true
	case in.Arrows <= 0:
		return true, "out of arrows", true
	case in.Balls <= 0 && in.CannonLoaded <= 0 && !in.CannonUp:
		return true, "out of Cannonballs", false
	}
	return false, "", false
}

// CannonAlchInput is what the alchemy gate reads.
type CannonAlchInput struct {
	Magic, Natures, Fires, Alchables int
	// Duty is craftCannonRunAlchDuty.
	Duty int
	// Now and LastCast are the clock and the last cast's start (zero: none).
	Now, LastCast time.Time
	// Tick is the engine tick.
	Tick time.Duration
}

// CannonAlchGap is the least time between two casts that keeps the alchemy's hold of the cannon to duty percent: a cast holds it for
// CannonAlchHoldTicks of the CannonAlchCastTicks between two casts, so at duty 100 the casts go back to back and at 50 a cast is followed
// by as long again of fire. 0 duty never casts.
func CannonAlchGap(duty int, tick time.Duration) (time.Duration, bool) {
	if duty <= 0 {
		return 0, false
	}
	if tick <= 0 {
		tick = 600 * time.Millisecond
	}
	hold := time.Duration(CannonAlchHoldTicks) * tick
	castEvery := time.Duration(CannonAlchCastTicks) * tick
	if duty >= 100 {
		return castEvery, true
	}
	return max(castEvery, hold*100/time.Duration(duty)), true
}

// CannonAlchDue says whether a High Level Alchemy cast may go out now, and if not why.
func CannonAlchDue(in CannonAlchInput) (bool, string) {
	gap, on := CannonAlchGap(in.Duty, in.Tick)
	switch {
	case !on:
		return false, "alchemy is off (" + KeyCannonRunAlchDuty + " 0)"
	case in.Magic < CannonAlchMagic:
		return false, "Magic " + strconv.Itoa(in.Magic) + " is under the " + strconv.Itoa(CannonAlchMagic) + " High Level Alchemy needs"
	case in.Natures < CannonAlchNatures:
		return false, "no Nature rune"
	case in.Fires < CannonAlchFires:
		return false, "under " + strconv.Itoa(CannonAlchFires) + " Fire runes (a bow leaves no staff hand)"
	case in.Alchables <= 0:
		return false, "nothing to alchemise"
	case !in.LastCast.IsZero() && in.Now.Sub(in.LastCast) < gap:
		return false, "the cannon's share of the time: next cast in " + (gap - in.Now.Sub(in.LastCast)).Round(100*time.Millisecond).String()
	}
	return true, ""
}
