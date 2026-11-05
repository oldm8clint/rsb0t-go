package mastercraftplan

import (
	"fmt"
	"strconv"
	"time"
)

// THE FIRE BATTLESTAFF ROUND (a user request: "fire battlestaff making should trigger when the mule hold 5k battlestaff and
// 5k fire orbs to attach, attaching and banking as fast as possible").
//
// THE CONTENT (content/scripts/skill_crafting/scripts/battlestaves/battlestaves.rs2 and configs/battlestaves/battlestaves.struct,
// branch 289): a Fire orb used on a Battlestaff (or the staff on the orb: both triggers reach [label,craft_staff]) makes a Fire
// battlestaff at Crafting 62 for 125 Crafting XP (productexp 1250 tenths), on a members world only (map_members). The script has
// NO p_delay and no progress bar: the use is over in the tick it is read, so the pace is the engine's five handled user events a
// tick, which is exactly what mastercraftrunner's fast stream (the bow-stringing load: 14 staves and 14 orbs, 14 uses, 20 events,
// four ticks) is built for. The objs (pack/obj.pack): 1391 battlestaff, 569 fire_orb, 1393 fire_battlestaff.
//
// WHERE: the mule's post, its bank at Catherby (the bow and pearl rounds' place): the bank is a booth the fast stream's
// Use-quickly opens in the last event of a tick, and the mule's kit lands there with no walk. Nothing else is nearer.
//
// THE TRIGGER: the mule's published bank holds at least set.StavesMinStaves Battlestaff AND set.StavesMinOrbs Fire orb
// (5,000 each by default) at Crafting 62 or above on a members world. A round cut short resumes from this account's own bank with no
// threshold while it holds a load. The kit asks go in set.KitSize lines (the mule's per-ask cap), as many as the stock needs.
// WHERE IN DECIDE: after the gem, bow and pearl rounds (the other fast Crafting rounds), before the storage round.

// The staff's content facts.
const (
	BattlestaffName         = "Battlestaff"
	BattlestaffID     int32 = 1391
	FireOrbName             = "Fire orb"
	FireOrbID         int32 = 569
	FireBattlestaff         = "Fire battlestaff"
	FireBattlestaffID int32 = 1393
	// StavesCraftingLevel is the Crafting level the Fire battlestaff needs, and StavesXPTenths its experience a staff.
	StavesCraftingLevel = 62
	StavesXPTenths      = 1250
)

// The settings of the round.
const (
	KeyStaves         = "craftFireBattlestaves"
	KeyStavesMinStaff = "craftFireBattlestaveMinStaves"
	KeyStavesMinOrbs  = "craftFireBattlestaveMinOrbs"

	DefaultStavesMin = 5000
)

func init() {
	knownKeys[KeyStaves] = true
	knownKeys[KeyStavesMinStaff] = true
	knownKeys[KeyStavesMinOrbs] = true
}

func (s Settings) validateStaves() []string {
	var p []string
	if s.StavesMinStaves < 14 {
		p = append(p, fmt.Sprintf("%s %d is below 14 (one load)", KeyStavesMinStaff, s.StavesMinStaves))
	}
	if s.StavesMinOrbs < 14 {
		p = append(p, fmt.Sprintf("%s %d is below 14 (one load)", KeyStavesMinOrbs, s.StavesMinOrbs))
	}
	return p
}

// StavesLedger is the round's memory.
type StavesLedger struct {
	// PauseUntil holds the round off after one that made nothing.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Rounds and Made count the rounds and the Fire battlestaves they made.
	Rounds int `json:"rounds,omitempty"`
	Made   int `json:"made,omitempty"`
}

// StavesLoad is the staves (and orbs) one fast load uses: a pack of 14 pairs.
const StavesLoad = 14

// StavesPlan is a decided round.
type StavesPlan struct {
	// MuleStaves and MuleOrbs are what the mule's published bank held, OwnStaves and OwnOrbs this account's bank.
	MuleStaves, MuleOrbs, OwnStaves, OwnOrbs int
	Resume                                   bool
	Why                                      string
}

// Pairs is the staffs the two banks can make together.
func (p StavesPlan) Pairs() int { return min(p.MuleStaves+p.OwnStaves, p.MuleOrbs+p.OwnOrbs) }

// StavesReady decides the round. mule is nil when the mule's bank has not been published; ownKnown whether this account's bank
// has been read since the script started.
func StavesReady(set Settings, crafting int, mule Stock, ownStaves, ownOrbs int) (StavesPlan, bool) {
	p := StavesPlan{MuleStaves: stockOf(mule, BattlestaffName), MuleOrbs: stockOf(mule, FireOrbName), OwnStaves: ownStaves, OwnOrbs: ownOrbs}
	switch {
	case !set.MembersWorld:
		p.Why = "Fire battlestaves are members' items (" + KeyMembersWorld + " is off)"
	case crafting < StavesCraftingLevel:
		p.Why = "Crafting " + strconv.Itoa(crafting) + " is under the " + strconv.Itoa(StavesCraftingLevel) + " a Fire battlestaff needs"
	case min(ownStaves, ownOrbs) >= StavesLoad:
		p.Resume = true
		p.Why = "this account's bank holds " + strconv.Itoa(ownStaves) + " Battlestaff and " + strconv.Itoa(ownOrbs) + " Fire orb to attach"
		return p, true
	case mule == nil:
		p.Why = "the mule has not published its bank"
	case p.MuleStaves >= set.StavesMinStaves && p.MuleOrbs >= set.StavesMinOrbs:
		p.Why = "the mule holds " + strconv.Itoa(p.MuleStaves) + " Battlestaff and " + strconv.Itoa(p.MuleOrbs) + " Fire orb (thresholds " + strconv.Itoa(set.StavesMinStaves) + " and " + strconv.Itoa(set.StavesMinOrbs) + ")"
		return p, true
	default:
		p.Why = "the mule holds " + strconv.Itoa(p.MuleStaves) + " Battlestaff (need " + strconv.Itoa(set.StavesMinStaves) + ") and " + strconv.Itoa(p.MuleOrbs) + " Fire orb (need " + strconv.Itoa(set.StavesMinOrbs) + ")"
	}
	return p, false
}

// PlanStavesAsk is one kit ask: equal lines of Battlestaff and Fire orb, at most set.KitSize each (the mule's per-ask cap), as
// many as the mule holds of the scarcer; nil when there is no pair to ask for.
func PlanStavesAsk(set Settings, mule Stock) []Line {
	n := min(stockOf(mule, BattlestaffName), stockOf(mule, FireOrbName), set.KitSize)
	if n < 1 {
		return nil
	}
	return []Line{{Item: BattlestaffName, Qty: n}, {Item: FireOrbName, Qty: n}}
}
