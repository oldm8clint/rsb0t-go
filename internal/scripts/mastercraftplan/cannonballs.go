package mastercraftplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// THE CANNONBALL ROUND (a user request: "master should make cannonballs ... 2k cannon balls are needed per account"; the plan
// of 31 Oct, docs/master_ranged_cannon_2026-10-31.md: the fleet's Ranged accounts fire a Dwarf Cannon, which eats balls).
//
// THE CONTENT (content/scripts, branch 289; read, not run):
//
//   - The smelt (skill_smithing/scripts/smelting/cannonballs.rs2 [label,smelt_cannonballs]): a Steel bar used on a
//     furnace (smelting.rs2 [label,use_furnace] "case steel_bar : @smelt_cannonballs"). It refuses on a free world
//     ("You need to be on a members' world to make cannonballs."), under Smithing 35 ("You need a Smithing level of 35 to
//     make cannonballs."), with no Ammo mould in the pack ("You need a cannonball mould to make cannonballs.") and with no
//     steel bar. Otherwise ONE bar becomes FOUR cannonballs (obj 2 "Cannonball", stackable, members) and 37.5 Smithing
//     XP (stat_advance(smithing, 375)). There is NO count and no menu: each bar is its own use of the furnace.
//   - The Dwarf Cannon quest is NOT checked by the smelt. It is the SOURCE OF THE MOULD: Nulodion hands one over during the
//     quest (quest_mcannon/scripts/npcs/nulodion.rs2) that the Dwarf Commander takes (lawgof2.rs2 inv_del), and once the
//     quest is complete (%mcannon = ^mcannon_complete) Nulodion's shop sells it (opnpc3 [nulodion]; dwarven_workshop.inv
//     stock6=ammo_mould). The mould (quest_mcannon.obj [ammo_mould]) is tradeable=no, so the mule can never hand one over and
//     a published bank says nothing of it: it must already be in this account's pack or bank, and the round never starts
//     without it (CannonballsReady names why).
//   - THE PACE: p_delay(4), p_delay(0), p_delay(2) are 5 + 1 + 3 ticks (a p_delay(n) holds the player n+1 ticks, see the
//     superheat round's THE PACE), and the balls arrive when the last one ends, so a bar takes 9 ticks and the next use,
//     sent when the balls show, is read on the tenth: CannonBarTicks = 10, 6 seconds a bar, 2,400 balls and 22,500
//     Smithing XP an hour. The bar leaves the pack and the balls come in the same instant, at the end (inv_del, inv_add
//     after the last delay), so a use read while the player is delayed is dropped without a word.
//
// WHAT STARTS A ROUND. The fleet's stock of balls (the mule's published bank and this account's bank together) is under
// craftCannonballTarget, the two banks hold at least craftCannonballMinBars Steel bars (or fewer, when fewer are what the
// target is short), Smithing is 35+ and under the 200 million cap, a members world, the Dwarf Cannon quest is not known to be
// unfinished and an Ammo mould is in this account's pack or bank. A round smelts at most craftCannonballBatch bars. Balls
// the round has made are handed to the mule like every other round's product; the target is "balls on hand", so balls the
// fleet fires fall out of the stock and the next check smelts them again.
//
// THE SKILL GOAL (goal.go) does NOT hold this round: the balls are the product (a supply round, like the hand-over), not
// Smithing training. The 200 million Smithing experience cap does (a user request: a round is held once it can earn
// nothing more).

// Cannonball facts.
const (
	// Cannonball is the product (obj 2 mcannonball) and CannonballID its id; AmmoMould and AmmoMouldID the mould (obj 4).
	Cannonball         = "Cannonball"
	CannonballID int32 = 2
	AmmoMould          = "Ammo mould"
	AmmoMouldID  int32 = 4
	// SteelBarID is the Steel bar's obj (obj.pack 2353 steel_bar).
	SteelBarID int32 = 2353
	// CannonballLevel is the Smithing level the smelt needs; CannonballsPerBar the balls one bar makes;
	// CannonballXPTenths the Smithing XP of a bar in tenths.
	CannonballLevel    = 35
	CannonballsPerBar  = 4
	CannonballXPTenths = 375
	// CannonBarTicks is the ticks one bar takes (see THE PACE).
	CannonBarTicks = 10
	// DwarfCannonQuest is the quest's name in the quest tab (internal/quests).
	DwarfCannonQuest = "Dwarf Cannon"
	// CannonPackBars is the Steel bars one load holds: the pack's 28 slots less the mould.
	CannonPackBars = 27
	// MaxCannonballTarget bounds craftCannonballTarget.
	MaxCannonballTarget = 10_000_000
	// MaxCannonballBatch bounds craftCannonballBatch (bars).
	MaxCannonballBatch = 100_000
)

// Setting keys of the cannonball round.
const (
	KeyCannonballTarget  = "craftCannonballTarget"
	KeyCannonballBatch   = "craftCannonballBatch"
	KeyCannonballMinBars = "craftCannonballMinBars"
)

// Defaults of the cannonball round. DefaultCannonballTarget is 2,000 balls for each of about thirty accounts (a user request: "2k cannon
// balls are needed per account"); DefaultCannonballBatch 500 bars (2,000 balls, about fifty minutes of smelting) is one round's
// most; DefaultCannonballMinBars 100 keeps a round from starting for a handful of bars.
const (
	DefaultCannonballTarget  = 60000
	DefaultCannonballBatch   = 500
	DefaultCannonballMinBars = 100
)

func init() {
	knownKeys[KeyCannonballTarget] = true
	knownKeys[KeyCannonballBatch] = true
	knownKeys[KeyCannonballMinBars] = true
}

// validateCannonballs reports every problem with the cannonball round's settings.
func (s Settings) validateCannonballs() []string {
	var p []string
	if s.CannonballTarget < 0 || s.CannonballTarget > MaxCannonballTarget {
		p = append(p, fmt.Sprintf("%s %d is outside 0..%d (0 turns the round off)", KeyCannonballTarget, s.CannonballTarget, MaxCannonballTarget))
	}
	if s.CannonballBatch < 1 || s.CannonballBatch > MaxCannonballBatch {
		p = append(p, fmt.Sprintf("%s %d is outside 1..%d", KeyCannonballBatch, s.CannonballBatch, MaxCannonballBatch))
	}
	if s.CannonballMinBars < 1 || s.CannonballMinBars > s.CannonballBatch {
		p = append(p, fmt.Sprintf("%s %d is outside 1..%s (%d)", KeyCannonballMinBars, s.CannonballMinBars, KeyCannonballBatch, s.CannonballBatch))
	}
	return p
}

// CannonballInput is the cannonball round's reading of the account.
type CannonballInput struct {
	// SmithingXP is the account's whole Smithing experience (0 when unknown).
	SmithingXP int
	// QuestKnown says the quest tab has been read; QuestDone that Dwarf Cannon is complete.
	QuestKnown, QuestDone bool
	// OwnKnown says this account's bank has been read since the script started; OwnSteel and OwnBalls are its Steel bars and
	// Cannonballs; Mould says an Ammo mould is in the bank or the pack.
	OwnKnown           bool
	OwnSteel, OwnBalls int
	Mould              bool
}

// CannonLedger is the cannonball round's memory.
type CannonLedger struct {
	// PauseUntil holds the round off after one that smelted nothing for a reason that will not pass by itself (no mould).
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Active is set when a round drew Steel bars into this account's bank to smelt and cleared when none is left: a round the
	// time budget or a restart cut short is resumed from the bank, with no trigger.
	Active bool `json:"active,omitempty"`
	// Lifetime counters: rounds, bars smelted, balls made, balls handed to the mule.
	Rounds int `json:"rounds,omitempty"`
	Bars   int `json:"bars,omitempty"`
	Balls  int `json:"balls,omitempty"`
	Handed int `json:"handed,omitempty"`
}

// CannonballPlan is a decided cannonball round.
type CannonballPlan struct {
	// Stock is the balls on hand (the mule's bank and this account's); Short how many the target lacks.
	Stock, Short int
	// Steel is the Steel bars in the two banks; Bars the bars this round smelts (at most the batch, the bars the target is
	// short, and the bars held).
	Steel, Bars int
	// Resume is a round carried on from this account's own bank (Ledger.Cannon.Active), with no trigger.
	Resume bool
	Why    string
}

// BarsFor is the Steel bars that make balls cannonballs, rounded up.
func BarsFor(balls int) int {
	if balls <= 0 {
		return 0
	}
	return (balls + CannonballsPerBar - 1) / CannonballsPerBar
}

// cannonballBlocked reports what stops a round at all, whatever the stock: Smithing, the cap, the world, the quest and the mould.
func cannonballBlocked(set Settings, s Snapshot) string {
	c := s.Cannon
	switch {
	case !set.MembersWorld:
		return "cannonballs need a members world (" + KeyMembersWorld + " is off)"
	case s.SmithingLevel < CannonballLevel:
		return "Smithing " + strconv.Itoa(s.SmithingLevel) + " is under the " + strconv.Itoa(CannonballLevel) + " cannonballs need"
	case c.SmithingXP >= MaxSkillXP:
		return "Smithing is at " + strconv.Itoa(MaxSkillXP) + " experience, the most it can earn"
	case c.QuestKnown && !c.QuestDone:
		return DwarfCannonQuest + " is not complete (Nulodion's shop sells the Ammo mould only after it)"
	case c.OwnKnown && !c.Mould:
		return "no " + AmmoMould + " in the pack or this account's bank, and it cannot be traded (Nulodion's shop sells one after " + DwarfCannonQuest + ")"
	}
	return ""
}

// CannonballsReady decides a cannonball round (see the file comment). active is the ledger's Cannon.Active.
//
// A round under way, with Steel bars in this account's own bank, carries on with no trigger. A new one starts when the balls on hand
// are under the target and the two banks hold the bars. An own bank not read yet (a restart) lets the round start so that it reads
// the bank at the mule's post and ends there, with the reason logged, when the mould is not in it: nothing is smelted blind.
func CannonballsReady(set Settings, s Snapshot, active bool) (CannonballPlan, bool) {
	var p CannonballPlan
	c := s.Cannon
	if set.CannonballTarget <= 0 {
		p.Why = KeyCannonballTarget + " is 0"
		return p, false
	}
	if why := cannonballBlocked(set, s); why != "" {
		p.Why = why
		return p, false
	}
	if active && (!c.OwnKnown || c.OwnSteel > 0) {
		p.Resume = true
		p.Steel = c.OwnSteel
		p.Bars = min(max(c.OwnSteel, 0), set.CannonballBatch)
		if !c.OwnKnown {
			p.Bars = set.CannonballBatch // the round reads the bank itself and smelts what it finds
		}
		p.Why = "a round under way: this account's bank holds " + strconv.Itoa(c.OwnSteel) + " Steel bars to smelt into cannonballs"
		return p, true
	}
	if s.Stock == nil {
		p.Why = "the mule has not published its bank"
		return p, false
	}
	muleBalls, muleSteel := stockOf(s.Stock, Cannonball), stockOf(s.Stock, SteelBar)
	p.Stock = muleBalls + c.OwnBalls
	p.Steel = muleSteel + c.OwnSteel
	p.Short = max(0, set.CannonballTarget-p.Stock)
	if p.Short == 0 {
		p.Why = strconv.Itoa(p.Stock) + " cannonballs on hand, at the target " + strconv.Itoa(set.CannonballTarget)
		return p, false
	}
	needBars := BarsFor(p.Short)
	p.Bars = min(needBars, p.Steel, set.CannonballBatch)
	if p.Bars < min(set.CannonballMinBars, needBars) {
		p.Why = strconv.Itoa(p.Steel) + " Steel bars in the two banks, under the " + strconv.Itoa(min(set.CannonballMinBars, needBars)) + " that start a round (" + strconv.Itoa(p.Stock) + " of " + strconv.Itoa(set.CannonballTarget) + " cannonballs on hand)"
		return p, false
	}
	p.Why = fmt.Sprintf("%d of %d cannonballs on hand and %d Steel bars in the two banks: smelting %d bars into %d balls", p.Stock, set.CannonballTarget, p.Steel, p.Bars, p.Bars*CannonballsPerBar)
	return p, true
}

// PlanCannonAsk is the kit a new round asks the mule for: the Steel bars the round smelts that this account's own bank does not
// hold, no more than the mule holds (nothing else is needed: the mould is this account's own).
func PlanCannonAsk(p CannonballPlan, mule Stock, ownSteel int) []Line {
	n := min(p.Bars-ownSteel, stockOf(mule, SteelBar))
	if n <= 0 {
		return nil
	}
	return []Line{{Item: SteelBar, Qty: n}}
}

// CannonDraw is the Steel bars one bank trip draws: as many as the free pack slots hold, the bars the round still has to smelt and the
// bank holds allow.
func CannonDraw(free, remaining, inBank int) int {
	return max(0, min(free, remaining, inBank))
}

// CannonballRefusal reads the furnace's chat for the smelt's refusals: "members", "level", "mould" or "bar" ("" none).
func CannonballRefusal(said string) string {
	switch {
	case strings.Contains(said, "members' world"):
		return "members"
	case strings.Contains(said, "You need a Smithing level of 35"):
		return "level"
	case strings.Contains(said, "You need a cannonball mould"):
		return "mould"
	case strings.Contains(said, "You need a steel bar"):
		return "bar"
	}
	return ""
}
