package essenceplan

// The essence SUPPLY (runecraft P2, docs/runecraft_p2_2026-10-13.md): what keeps the mule's Rune essence row
// stocked. The runecraft phases after it (the carriers' kits, the main account's levelling, a Nature campaign of 100,000)
// and the Priest in Peril ladders parked on "no unnoted rune essence from the bank, the mule, the Seers market or
// mining" (24 each, about 105 of them on 13 Oct 2026) all draw on that one row.
//
// SOURCE LADDER for any consumer: its own bank, then the mule's stock, then mining, then a bounded market lot.
// Mining is the production source: a Magic-66 account at the Wizards' Guild mines 27 essence a load, banks it at
// Yanille (20 steps from the mine's exit), and when the bank holds muleAt essence hands the lot over as ONE note
// stack. This file is the pure half: the mining-rate model (what a pickaxe tier is worth), the decision between
// mining, handing over and holding (DecideSupply), the stack-only hand-over rules, the bounded market lot
// (PlanMarketLot) and the Guild door's refusal.

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
)

// Supply numbers.
const (
	// LoadSize is the essence one load is: a full pack less the slot the pickaxe takes.
	LoadSize = PackSlots - 1
	// MuleEssenceCap is the mule's Rune essence at which the miners stop (muleStopAt): the design's cap. The mule's
	// bank has 220 rows of 288 and this is one of them.
	MuleEssenceCap = 150_000
	// GuildMuleAt is the banked essence that starts a hand-over at a Guild miner (muleAt): 5,000 is about eight hours of
	// bronze mining, and one note stack of it is one trade of one offer.
	GuildMuleAt = 5_000
	// DefaultBatch is the essence one mining stretch brings before the miner reads the mule and the bank again:
	// ten loads, about forty minutes at bronze, short enough for a stop or a full mule to be seen.
	DefaultBatch = 10 * LoadSize
	// MinHandoff is the smallest stack worth a trip to the mule: one load.
	MinHandoff = LoadSize
	// NotedEssenceID is the bank note of the Rune essence (cert_blankrune).
	NotedEssenceID = 1437
)

// The mining model. Mining a rock is one essence every (the pickaxe's mining_rate + 1) ticks, with no success roll
// and no depletion (skill_mining/scripts/mining.rs2:77-113, :197-207; pickaxes.obj mining_rate 7,6,5,4,3,2 from
// bronze to rune), and a load costs the teleport in (5 ticks), the portal out, the walk to the bank and back and
// the bank itself on top.
const (
	// TicksPerHour is an hour of the engine's 600 ms ticks.
	TicksPerHour = 6000
	// LoadOverheadTicks is the time a load takes besides mining it, measured as the design's: the teleport in,
	// the portal, about 20 steps each way to the Yanille bank, the deposit. It is 49 steps at Aubury, which is why
	// the Guild is the better gateway.
	LoadOverheadTicks = 45
)

// pickPeriods is the ticks between two essence for each pickaxe item.
var pickPeriods = map[string]int{
	"bronze pickaxe":  8,
	"iron pickaxe":    7,
	"steel pickaxe":   6,
	"mithril pickaxe": 5,
	"adamant pickaxe": 4,
	"rune pickaxe":    3,
}

// PickPeriod is the ticks between two essence with the pickaxe item (a PickTier's Item); ok is false for anything
// that is not a pickaxe.
func PickPeriod(item string) (ticks int, ok bool) {
	ticks, ok = pickPeriods[strings.ToLower(strings.TrimSpace(item))]
	return ticks, ok
}

// LoadTicks is the ticks one full load takes with the pickaxe: mining it and everything around it.
func LoadTicks(item string) (int, bool) {
	p, ok := PickPeriod(item)
	if !ok {
		return 0, false
	}
	return LoadSize*p + LoadOverheadTicks, true
}

// EssencePerHour is the essence a miner banks in an hour with the pickaxe item, walks and teleports included:
// bronze 620, iron 690, steel 780, mithril 900, adamant 1,060, rune 1,290.
func EssencePerHour(item string) float64 {
	t, ok := LoadTicks(item)
	if !ok {
		return 0
	}
	return float64(LoadSize) / float64(t) * TicksPerHour
}

// HoursToMine is how long n essence take with the pickaxe item.
func HoursToMine(n int, item string) float64 {
	rate := EssencePerHour(item)
	if rate <= 0 || n <= 0 {
		return 0
	}
	return float64(n) / rate
}

// RoundUpToLoads is n rounded up to a whole number of loads, and at least one.
func RoundUpToLoads(n int) int {
	if n < LoadSize {
		return LoadSize
	}
	return (n + LoadSize - 1) / LoadSize * LoadSize
}

// ---- the stack-only hand-over ----

// StackRules are the hand-over rules of MuleItemsEssence: only Rune essence, noted or not, goes to the mule.
// Everything else the account carries stays, whatever the keep list says.
func StackRules() donorplan.Rules {
	return donorplan.Rules{
		Skip:  func(row donorplan.ItemRow) bool { return !isEssenceName(row.Name) },
		First: func(row donorplan.ItemRow) bool { return isEssenceName(row.Name) },
	}
}

// RulesFor are the hand-over rules of a MuleItems value and keep list.
func RulesFor(items string, keep []string) donorplan.Rules {
	if strings.EqualFold(strings.TrimSpace(items), MuleItemsEssence) {
		return StackRules()
	}
	return muleRules(keep)
}

// ---- the decision ----

// SupplyAction is what the supply loop does next.
type SupplyAction string

// The actions.
const (
	// SupplyRead visits the gateway's bank to read how much essence it holds (nothing is known after a start).
	SupplyRead SupplyAction = "read"
	// SupplyMine runs a mining stretch that banks Goal more essence.
	SupplyMine SupplyAction = "mine"
	// SupplyHandoff draws Send essence from the bank as one note stack and trades it to the mule.
	SupplyHandoff SupplyAction = "handoff"
	// SupplyHold does nothing for now: the mule holds all it is to hold.
	SupplyHold SupplyAction = "hold"
)

// SupplyView is what DecideSupply reads.
type SupplyView struct {
	Now time.Time
	// BankKnown is set once the bank's essence has been read this run; BankEssence is what it held.
	BankKnown   bool
	BankEssence int
	// MuleAt is the banked essence that starts a hand-over (0: never), MuleStopAt the mule's stock at which
	// mining stops (0: never) and Batch the essence one stretch brings.
	MuleAt, MuleStopAt, Batch int
	// MuleKnown says the mule's bank is published; MuleEssence is its Rune essence. Delivered is what this run
	// has handed over: the measure of the mule's stock when it is not published.
	MuleKnown   bool
	MuleEssence int
	Delivered   int
	// NextHandoff is the earliest time another hand-over is tried after a failed one; Manual is a hand-over
	// asked for from the control panel.
	NextHandoff time.Time
	Manual      bool
	// HandoverOff says nothing can be handed over: no mule is set, or muleAt is 0 (donorplan.Settings.Enabled is false).
	// DecideSupply then never answers SupplyHandoff, whatever the bank holds, the cap says or the control panel asks:
	// a trip to nobody carries a whole note stack across the map and back, and the mule it waits for is a player named "".
	HandoverOff bool
}

// SupplyDecision is DecideSupply's answer.
type SupplyDecision struct {
	Action SupplyAction
	// Goal is a mining stretch's essence to bank, Send a hand-over's essence to draw.
	Goal, Send int
	// Stock is the mule's essence the decision measured against the cap, and Source says where it came from
	// ("the mule's published bank" or "this run's deliveries"); Headroom is how much more the cap takes (-1: no
	// cap).
	Stock    int
	Source   string
	Headroom int
	Why      string
}

// Stock is the mule's Rune essence as far as the miner knows it.
func (v SupplyView) Stock() (n int, source string) {
	if v.MuleKnown {
		return max(0, v.MuleEssence), "the mule's published bank"
	}
	return max(0, v.Delivered), "this run's deliveries (the mule's bank is not published)"
}

// DecideSupply chooses between reading the bank, mining, handing over and holding.
//
//   - the bank is read before anything else after a start;
//   - the cap: when the mule's stock plus what the bank holds reaches MuleStopAt, no more is mined; what the bank
//     holds goes over (up to the room the cap leaves) and the miner holds until the mule uses some;
//   - the threshold: with MuleAt essence banked (and no failed hand-over to wait out) it is handed over, all of it
//     or the room the cap leaves, whichever is less;
//   - otherwise a stretch of Batch essence, no more than the threshold or the cap still wants.
//
// With HandoverOff (no mule set, or muleAt 0) none of the hand-over branches is reachable: the cap holds the miner with
// its essence in the bank, the threshold and a manual request are ignored.
func DecideSupply(v SupplyView) SupplyDecision {
	stock, source := v.Stock()
	d := SupplyDecision{Stock: stock, Source: source, Headroom: -1}
	if v.MuleStopAt > 0 {
		d.Headroom = max(0, v.MuleStopAt-stock)
	}
	if !v.BankKnown {
		d.Action, d.Why = SupplyRead, "the bank has not been read yet"
		return d
	}
	bank := max(0, v.BankEssence)
	retryOK := !v.Now.Before(v.NextHandoff)
	muleAt, manual := v.MuleAt, v.Manual
	if v.HandoverOff {
		muleAt, manual = 0, false
	}
	send := func() int {
		n := bank
		if d.Headroom >= 0 {
			n = min(n, d.Headroom)
		}
		return n
	}

	if v.MuleStopAt > 0 && stock+bank >= v.MuleStopAt {
		// Enough is mined: what the bank holds is the last of it.
		if n := send(); !v.HandoverOff && n >= MinHandoff && retryOK {
			d.Action, d.Send = SupplyHandoff, n
			d.Why = fmt.Sprintf("the mule holds %d (%s) with %d banked: over the %d cap, so the %d the cap takes go over and mining stops", stock, source, bank, v.MuleStopAt, n)
			return d
		}
		d.Action = SupplyHold
		d.Why = fmt.Sprintf("the mule holds %d (%s) and %d are banked: at the %d cap (muleStopAt), so nothing more is mined", stock, source, bank, v.MuleStopAt)
		if v.HandoverOff {
			d.Why += "; hand-overs are off, so the essence stays in the bank"
		}
		return d
	}

	due := muleAt > 0 && bank >= muleAt
	switch {
	case due && retryOK:
		d.Action, d.Send = SupplyHandoff, send()
		d.Why = fmt.Sprintf("%d essence banked, muleAt %d", bank, muleAt)
		return d
	case manual && bank >= MinHandoff:
		d.Action, d.Send = SupplyHandoff, send()
		d.Why = fmt.Sprintf("a hand-over was asked for with %d essence banked", bank)
		return d
	}

	// Mine: toward the threshold when there is one, a batch otherwise, never past the cap by more than a load.
	batch := v.Batch
	if batch < LoadSize {
		batch = DefaultBatch
	}
	goal := batch
	if muleAt > 0 && !due {
		goal = min(goal, muleAt-bank)
	}
	if v.MuleStopAt > 0 {
		goal = min(goal, v.MuleStopAt-stock-bank)
	}
	d.Action, d.Goal = SupplyMine, RoundUpToLoads(goal)
	switch {
	case due:
		d.Why = fmt.Sprintf("%d essence banked and the next hand-over is not before %s", bank, v.NextHandoff.Format("15:04"))
	case muleAt > 0:
		d.Why = fmt.Sprintf("%d of the %d banked toward muleAt", bank, muleAt)
	default:
		d.Why = fmt.Sprintf("%d essence banked and nothing to hand it to (no mule, or muleAt 0)", bank)
	}
	return d
}

// ---- the bounded market lot ----

// The market lot's numbers. The lot is for the account that cannot mine (or whose mining failed): the main account, from
// the Seers stalls, at most the budget the owner sets, and only from coins the mule can spare.
const (
	// MarketMuleCoinsFloor is the coins the mule keeps whatever the lot: it pays the fleet's kits, its hand-outs and
	// the carriers' float. The lot may only use what is above it.
	MarketMuleCoinsFloor = 1_000_000
	// MarketUnitCeiling is the most one essence may cost, whatever a stall says or lists: questplan's
	// PipEssenceUnitCeiling, which a test keeps equal. The stalls' last-sold price on 13 Oct was 100.
	MarketUnitCeiling = 150
)

// LotTerms are PlanMarketLot's inputs.
type LotTerms struct {
	// Budget is craftRuneBuyBudget in coins (0: the lot is off) and Spent what earlier lots have already paid.
	Budget, Spent int
	// Want is the essence the account is short.
	Want int
	// MuleKnown says the mule's bank is published; MuleCoins is what it holds.
	MuleKnown bool
	MuleCoins int
	// OnMarketWorld says the stalls stand on this world (they do not on worlds 2 and 3).
	OnMarketWorld bool
}

// LotPlan is what PlanMarketLot allows.
type LotPlan struct {
	// Qty is the most essence to buy; MaxCoins the most the lot may pay (Qty at the ceiling); Ask the coins to ask
	// the mule for.
	Qty, MaxCoins, Ask int
}

// PlanMarketLot is the lot the budget, the mule's coins and the ceiling allow, or the reason there is none.
// The coins come from the mule's side: only what it holds above MarketMuleCoinsFloor can fund a lot, so a mule
// under a million is refused outright, and a lot never exceeds the budget that is left.
func PlanMarketLot(t LotTerms) (LotPlan, string) {
	switch {
	case t.Budget <= 0:
		return LotPlan{}, "the market lot is off (craftRuneBuyBudget 0)"
	case t.Spent >= t.Budget:
		return LotPlan{}, fmt.Sprintf("the market budget is spent (%d of %d coins)", t.Spent, t.Budget)
	case t.Want <= 0:
		return LotPlan{}, "no essence is short"
	case !t.OnMarketWorld:
		return LotPlan{}, "the Seers stalls do not stand on this world"
	case !t.MuleKnown:
		return LotPlan{}, "the mule's coins are not published, so a lot cannot be funded"
	case t.MuleCoins <= MarketMuleCoinsFloor:
		return LotPlan{}, fmt.Sprintf("the mule holds %d coins, not above its %d floor", t.MuleCoins, MarketMuleCoinsFloor)
	}
	funds := min(t.Budget-t.Spent, t.MuleCoins-MarketMuleCoinsFloor)
	qty := min(t.Want, funds/MarketUnitCeiling)
	if qty < 1 {
		return LotPlan{}, fmt.Sprintf("%d coins do not buy one essence at up to %d", funds, MarketUnitCeiling)
	}
	return LotPlan{Qty: qty, MaxCoins: qty * MarketUnitCeiling, Ask: qty * MarketUnitCeiling}, ""
}

// ---- the Guild door ----

// doorRefusalPattern matches the Magic Guild's door-keeper turning an account under Magic 66 away
// (magic_guild.rs2:11, a guild wizard's chat: "You need a magic level of 66").
var doorRefusalPattern = regexp.MustCompile(`(?i)you need a magic level of 66`)

// IsGuildDoorRefusal reports whether a chat line is the Wizards' Guild turning the account away at its door.
func IsGuildDoorRefusal(line string) bool { return doorRefusalPattern.MatchString(line) }
