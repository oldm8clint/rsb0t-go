package mastercraftplan

import (
	"fmt"
	"strconv"
	"time"
)

// THE PLATEBODY ROUND (a user request: "after the adamant is done i want master to teleport to varrock, travel to varrock
// west and use the anvils to make platebodies starting from the highest level bar available saving our steel bars for
// cannonball making ... this round should last for an hour regardless if all the bars get used or not. the platebodies are to
// be used for alching amongst the fleet").
//
// THE CONTENT (content/scripts/skill_smithing/configs/smithing/smithing.dbrow, branch 289): every platebody takes FIVE bars and
// gives one: Mithril Smithing 68, Adamant 88, Rune 99 (Iron 33, which the anvil round already makes; Steel 48 and Bronze are
// the lower ones). The objs are 1115 iron_platebody, 1121 mithril_platebody, 1123 adamant_platebody, 1127 rune_platebody
// (pack/obj.pack). Their cost (skill_combat/configs/melee/platebodies.obj) is 560, 5200, 12800 and 65000, so High Alchemy pays
// 336, 3,120, 7,680 and 39,000 (60%): far over a Nature rune. Smithing XP a bar is the metal's (Iron 25, Mithril 50, Adamant
// 62.5, Rune 75) and a make is five ticks, so one platebody is 25 ticks for five bars.
//
// STEEL IS NEVER USED: the Steel bars are kept for the cannonball round (the owner's words), so Steel is not a product here
// at all, not even "if that is what remains". Iron is the one low bar kept, as the last resort ("Iron if that is what remains").
//
// THE TRIGGER (changed 5 Nov 2026 evening: the Adamantite wait is gone, adamant is superheated last): the Smithing level allows a
// platebody and the two banks hold at least
// craftPlatebodyMinBars bars of those kinds. THE STOP: the round's own hour (craftPlatebodyRoundMinutes, roundlimit.go),
// "regardless if all the bars get used or not"; it also ends when no bar is left. Either way the platebodies go to the mule, the
// leftover bars stay in this account's bank, and the mode is paused craftRoundLimitPauseMinutes.
//
// WHERE: after the superheat round in Decide, before the cannonball and anvil rounds, so the bars the superheat round just made
// are a platebody round's before the anvil round (Steel > 8000 starts it) turns them into arrowtips.

// PlateKinds are the platebodies the round makes, the highest bar first. There is no Steel row and there never is.
var PlateKinds = []SmithProduct{
	{Kind: SmithPlatebodyKind, Name: "Rune platebody", ID: 1127, Bar: "Runite bar", BarID: 2363, BarsPer: 5, Per: 1, Level: 99, Com: SmithColumn3, Slot: 3, XPTenthsPerBar: 750},
	{Kind: SmithPlatebodyKind, Name: "Adamant platebody", ID: 1123, Bar: "Adamantite bar", BarID: 2361, BarsPer: 5, Per: 1, Level: 88, Com: SmithColumn3, Slot: 3, XPTenthsPerBar: 625},
	{Kind: SmithPlatebodyKind, Name: "Mithril platebody", ID: 1121, Bar: "Mithril bar", BarID: 2359, BarsPer: 5, Per: 1, Level: 68, Com: SmithColumn3, Slot: 3, XPTenthsPerBar: 500},
	SmithIronPlatebody,
}

// AdamantiteOre is the ore whose exhaustion starts the round.
const AdamantiteOre = "Adamantite ore"

// The settings of the round.
const (
	KeyPlatebody        = "craftPlatebody"
	KeyPlatebodyMinBars = "craftPlatebodyMinBars"
	KeyPlatebodyKit     = "craftPlatebodyKit"

	DefaultPlatebodyMinBars = 100
	// DefaultPlatebodyKit is the bars one round asks the mule for in all: a trip of 25 bars takes about half a minute, so an
	// hour is about 3,000.
	DefaultPlatebodyKit = 3000
)

func init() {
	knownKeys[KeyPlatebody] = true
	knownKeys[KeyPlatebodyMinBars] = true
	knownKeys[KeyPlatebodyKit] = true
}

func (s Settings) validatePlatebody() []string {
	var p []string
	if s.PlatebodyMinBars < 5 {
		p = append(p, fmt.Sprintf("%s %d is below 5 (one platebody)", KeyPlatebodyMinBars, s.PlatebodyMinBars))
	}
	if s.PlatebodyKit < 5 || s.PlatebodyKit > 100000 {
		p = append(p, fmt.Sprintf("%s %d is outside 5..100000", KeyPlatebodyKit, s.PlatebodyKit))
	}
	return p
}

// PlateLedger is the platebody round's memory across rounds and restarts.
type PlateLedger struct {
	// Active and StartedAt: a round STARTED at StartedAt and not yet over. A restart inside its hour resumes it from this
	// account's own bank with the clock already running; after the hour it is a new round.
	Active    bool      `json:"active,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	// PauseUntil holds the round off after it reached its time limit (roundlimit.go) or found nothing.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Rounds, Bars and Bodies count what the rounds did.
	Rounds int `json:"rounds,omitempty"`
	Bars   int `json:"bars,omitempty"`
	Bodies int `json:"bodies,omitempty"`
}

// PlateProductsFor is the platebodies this account may make, the highest bar first.
func PlateProductsFor(smithing int) []SmithProduct {
	var out []SmithProduct
	for _, p := range PlateKinds {
		if p.Level <= smithing {
			out = append(out, p)
		}
	}
	return out
}

// PlatePlan is a decided platebody round.
type PlatePlan struct {
	Products []SmithProduct
	// Bars is the bars of the products the two banks held when it was decided, Makes the platebodies they pay for.
	Bars, Makes int
	// Resume: a round under way, carried on inside its hour with the clock already running (Started).
	Resume  bool
	Started time.Time
	Why     string
}

// PlatebodyReady decides the round (a user request: Adamantite superheating now runs LAST, so the round no longer waits for the
// Adamantite ore to run out; schedule.go has the order). mule is nil when its bank has not been published, own nil when this account's has not
// been read.
func PlatebodyReady(set Settings, smithing int, led PlateLedger, now time.Time, mule Stock, own SmithStock) (PlatePlan, bool) {
	var p PlatePlan
	p.Products = PlateProductsFor(smithing)
	if len(p.Products) == 0 {
		p.Why = "Smithing " + strconv.Itoa(smithing) + " is under the 33 an Iron platebody needs"
		return p, false
	}
	if own == nil {
		own = func(string) int { return 0 }
	}
	total := func(item string) int { return own(item) + stockOf(mule, item) }
	for _, pr := range p.Products {
		p.Bars += total(pr.Bar)
		p.Makes += pr.Makes(total(pr.Bar))
	}
	// A round inside its hour carries on from this account's own bank (a restart, a crash, a Stop).
	if led.Active && !led.StartedAt.IsZero() && (set.PlatebodyRound <= 0 || now.Sub(led.StartedAt) < set.PlatebodyRound) {
		if n := SmithMakes(p.Products, own); n > 0 {
			p.Resume, p.Started = true, led.StartedAt
			p.Why = "a platebody round under way (" + now.Sub(led.StartedAt).Round(time.Minute).String() + " of " + set.PlatebodyRound.String() + "): this account's bank holds bars for " + strconv.Itoa(n) + " more platebodies"
			return p, true
		}
	}
	switch {
	case p.Bars < set.PlatebodyMinBars || p.Makes < 1:
		p.Why = strconv.Itoa(p.Bars) + " bars the platebodies take in the two banks (" + strconv.Itoa(p.Makes) + " platebodies), under " + strconv.Itoa(set.PlatebodyMinBars)
	default:
		p.Why = strconv.Itoa(p.Bars) + " bars in the two banks make " + strconv.Itoa(p.Makes) + " platebodies: smithing the highest bar first for " + set.PlatebodyRound.String()
		return p, true
	}
	return p, false
}

// PlanPlateAsk is the kit a new round asks the mule for: the bars of the products, the highest first, until the round's
// budget (set.PlatebodyKit, counting what this account's bank already holds) is covered; a hammer when none is held; the rub
// jewellery as the anvil round does. Steel is never a line.
func PlanPlateAsk(set Settings, products []SmithProduct, mule Stock, own SmithStock, haveHammer bool) []Line {
	var lines []Line
	budget := set.PlatebodyKit
	for _, p := range products {
		budget -= own(p.Bar)
		if budget <= 0 {
			break
		}
		if n := min(stockOf(mule, p.Bar), budget); n > 0 {
			lines = append(lines, Line{Item: p.Bar, Qty: n})
			budget -= n
		}
	}
	if !haveHammer && own(Hammer) <= 0 && stockOf(mule, Hammer) > 0 {
		lines = append(lines, Line{Item: Hammer, Qty: 1})
	}
	if rubOwned(own) == 0 {
		if name, ok := MuleRubItem(mule); ok {
			lines = append(lines, Line{Item: name, Qty: 1})
		}
	}
	return lines
}

// PlateTeleportSpell is the spell that starts the round's way to the anvils.
const PlateTeleportSpell = VarrockTeleport

// PlateTeleportKit is the runes one Varrock Teleport still needs asked for, given what the pack holds and what a worn staff
// pays: nothing when the Magic level is short or the mule cannot pay the whole cast (the round then walks).
func PlateTeleportKit(magic int, paid map[string]bool, have func(rune string) int, mule Stock) []Line {
	lvl := ChestSpellLevel(PlateTeleportSpell)
	if lvl == 0 || magic < lvl {
		return nil
	}
	var lines []Line
	for name, n := range ChestSpellRunes(PlateTeleportSpell) {
		if paid[name] {
			continue
		}
		if short := n - have(name); short > 0 {
			if stockOf(mule, name) < short {
				return nil
			}
			lines = append(lines, Line{Item: name, Qty: short})
		}
	}
	return lines
}
