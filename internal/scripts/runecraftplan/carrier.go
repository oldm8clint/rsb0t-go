package runecraftplan

// The RuneCarrier (runecraft P3, docs/runecraft_p3_2026-10-14.md): a ScarabSwarm account that, while the Nature gate is open,
// shuttles unnoted Rune essence from Jiminua's Jungle Store on Karamja to the crafter standing at the Nature ruins.
//
// THE JUNGLE STORE'S SHELF. The carrier needs UNNOTED essence to trade (the altar binds only the unnoted id) and the mule hands
// out NOTED essence in bulk (one slot, one trade). A general store un-notes it for a few coins: a sold note is stored as the
// plain item (shop.rs2 sell_item moves with inv_moveitem_uncert) and a bought unit always comes out plain; Jiminua's shop
// (junglestore) is an "allstock" shop of 40 rows, 25 of them her base stock, and it takes anything tradeable. The prices are the
// content's (shop.rs2 calc_shop_value):
//
//	buy  = max(100, 1500 - min(1000, 20*stock)) * cost / 1000, at least 1,   cost 4 (ores.obj Rune essence)
//	sell = max(100,  500 - min(1000, 20*stock)) * cost / 1000
//
// so a unit costs 6 gold at stock 0, 5 at 1-12, 4 at 13-25, 3 at 26-37 and 2 from 38, and a sold unit pays 2, then 1 (stock 1-12),
// then nothing from stock 13. Rows of unlisted stock lose one unit every 100 ticks (World.ts, INV_STOCKRATE). The old rule, sell
// what you need to buy back, drained the shelf to nothing and cost up to 112 gold a trip; the FLOOR policy sells the 25 it needs
// plus whatever lifts the shelf to carrierShelfFloor (40), so every purchase is at the 2 gold floor: 50 gold a trip.
//
// The decisions are all here and pure; the I/O is runecraftrunner/carrier.go and the bot internal/bot/runecarrier.go.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/tickmanip"
)

// ---- the shelf ----

// The Jungle Store's numbers (content 289: areas/area_karamja karamja.inv [junglestore], karamja.npc [jiminua], ores.obj).
const (
	// ShopCost is the Rune essence's base cost (oc_cost).
	ShopCost = 4
	// ShopSellMult is what the shop charges (shop_sell_multiplier), ShopBuyMult what it pays (shop_buy_multiplier) and ShopDelta
	// the haggle (shop_delta), all in thousandths.
	ShopSellMult = 1500
	ShopBuyMult  = 500
	ShopDelta    = 20
	// ShopRows is the shop's size and ShopBaseRows the rows of its own stock.
	ShopRows     = 40
	ShopBaseRows = 25
	// ShelfFloor is the shelf at which a unit costs the floor price, 2 (38 and over): a carrierShelfFloor of this keeps every
	// purchase at the floor, if the shop keeps what was sold to it.
	ShelfFloor = 40
	// DefaultShelfFloor is the carrierShelfFloor default: none. On the live server Jiminua's surplus is gone by the next visit
	// (16 Oct 2026, a 40-unit floor read "stock 0" two minutes later on six trips in a row, against the reference engine's one
	// unit a minute), so a floor sold 40 notes a trip into nothing; at 0 a trip sells its 25 notes and buys them back, about 98 gp.
	DefaultShelfFloor = 0
	// ShelfMinCoins is the coins under which the carrier refuses to shop: the trip's purchase is 50 to 112 and the fare home 30.
	ShelfMinCoins = 200
)

// shopValue is calc_shop_value: haggle by the stock, never under a tenth of the multiplier.
func shopValue(baseCost, haggle, mult, stock int) int {
	h := min(1000, max(-5000, stock*haggle))
	m := max(100, mult-h)
	return m * baseCost / 1000
}

// ShelfBuyPrice is what one unit costs when the shelf holds `stock` (at least 1).
func ShelfBuyPrice(stock int) int { return max(1, shopValue(ShopCost, ShopDelta, ShopSellMult, stock)) }

// ShelfSellPrice is what the shop pays for the unit sold when the shelf holds `stock` (it can be 0).
func ShelfSellPrice(stock int) int { return shopValue(ShopCost, ShopDelta, ShopBuyMult, stock) }

// ShelfBuyCost is the coins for buying n units from a shelf of `stock`: buy_item prices unit i at stock-i, and stops when the
// shelf is empty. It returns the coins and the units that could be bought.
func ShelfBuyCost(stock, n int) (cost, bought int) {
	for i := 0; i < n && stock-i > 0; i++ {
		cost += ShelfBuyPrice(stock - i)
		bought++
	}
	return cost, bought
}

// ShelfSellValue is the coins for selling n units to a shelf of `stock`: sell_item prices unit i at stock+i.
func ShelfSellValue(stock, n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v += ShelfSellPrice(stock + i)
	}
	return v
}

// ShelfState is what PlanShelf reads: the shelf as the shop's stock panel shows it and the carrier's pack.
type ShelfState struct {
	// Stock is the unnoted Rune essence on the shelf now; FreeRow whether the shop has an empty row (a row for a new item).
	Stock   int
	FreeRow bool
	// Notes and Unnoted are the essence held noted and unnoted; Coins the coins; PackFree the empty pack slots.
	Notes, Unnoted, Coins, PackFree int
	// Want is the unnoted essence the trip needs (CarrierOffer), Floor the shelf kept after the visit.
	Want, Floor int
}

// ShelfPlan is the visit: sell Sell notes, then buy Buy units.
type ShelfPlan struct {
	Sell, Buy int
	// Income and Cost are the coins the sale pays and the purchase costs; ShelfAfter the shelf once both are done.
	Income, Cost, ShelfAfter int
	// BuyOnly is set when the shop could take no sale (no free row and no essence row).
	BuyOnly bool
	// OK is false when nothing sensible can be done (Why says why); a plan that buys less than Want is still OK.
	OK  bool
	Why string
}

// Net is the coins the visit costs.
func (p ShelfPlan) Net() int { return p.Cost - p.Income }

// PlanShelf is the floor policy: sell the units the trip needs plus those that lift the shelf to Floor (as many as the notes
// allow), then buy what the trip needs, never more than the shelf holds. A shop with no free row and no essence row takes no
// sale: the carrier buys only what is on the shelf. Under ShelfMinCoins coins it does not shop at all.
func PlanShelf(s ShelfState) ShelfPlan {
	if s.Floor < 0 {
		s.Floor = 0
	}
	need := s.Want - s.Unnoted
	if need > s.PackFree {
		need = s.PackFree
	}
	if need <= 0 {
		return ShelfPlan{OK: true, ShelfAfter: s.Stock, Why: "the pack already holds the load"}
	}
	if s.Coins < ShelfMinCoins {
		return ShelfPlan{ShelfAfter: s.Stock, Why: strconv.Itoa(s.Coins) + " coins is under the " + strconv.Itoa(ShelfMinCoins) + " a visit needs"}
	}
	p := ShelfPlan{OK: true}
	canSell := s.Stock > 0 || s.FreeRow
	if canSell {
		p.Sell = need + max(0, s.Floor-s.Stock)
		if s.Floor == 0 && s.Stock >= need {
			// The shelf already holds the load: it is bought as it stands and nothing is sold into it. A sale is the visit's one
			// window for another shopper to buy the units just sold (about a second and a half from the sale to the first buy), and
			// buying from a shelf that is not overstocked costs about 30 gp more than buying from one that is. Live 16 Oct 2026
			// ten of 75 visits found 25 on the shelf and six lost their stack to another shopper.
			p.Sell = 0
		}
		if p.Sell > s.Notes {
			p.Sell = s.Notes
		}
		p.Income = ShelfSellValue(s.Stock, p.Sell)
	} else {
		p.BuyOnly = true
	}
	shelf := s.Stock + p.Sell
	p.Buy = min(need, shelf)
	// Coins: the purchase must leave the fare. Fewer units until it is affordable.
	for p.Buy > 0 {
		cost, _ := ShelfBuyCost(shelf, p.Buy)
		if s.Coins+p.Income-cost >= 30 {
			p.Cost = cost
			break
		}
		p.Buy--
	}
	p.ShelfAfter = shelf - p.Buy
	switch {
	case p.Buy == 0 && shelf == 0:
		p.OK, p.Why = false, "the shelf is empty and there are no notes to sell into it"
	case p.Buy == 0:
		p.OK, p.Why = false, "the coins do not pay for a unit"
	case p.Buy < need:
		p.Why = "the shelf holds only " + strconv.Itoa(shelf) + " after the sale"
	}
	return p
}

// ShelfLine is the log line of a visit.
func ShelfLine(before ShelfState, p ShelfPlan, soldFor, boughtFor int) string {
	b := strings.Builder{}
	b.WriteString("shelf: Jiminua stock " + strconv.Itoa(before.Stock) + " Rune essence; sold " + strconv.Itoa(p.Sell) + " notes for " + strconv.Itoa(soldFor) + " gp, bought " + strconv.Itoa(p.Buy) + " for " + strconv.Itoa(boughtFor) + " gp; shelf " + strconv.Itoa(p.ShelfAfter))
	return b.String()
}

// ---- where the carrier is ----

// Place is where a carrier stands as far as its shift goes.
type Place int

// The places.
const (
	// PlaceMainland is anywhere off the island: the mule's post, the road, Ardougne's pier.
	PlaceMainland Place = iota
	// PlaceIsland is on Karamja's surface away from the store and the gate: the pier, the jungle road.
	PlaceIsland
	// PlaceStore is within StoreNear of Jiminua.
	PlaceStore
	// PlaceGate is within GateNear of the ruins' exit tile.
	PlaceGate
)

func (p Place) String() string {
	switch p {
	case PlaceIsland:
		return "on the island"
	case PlaceStore:
		return "at the store"
	case PlaceGate:
		return "at the gate"
	}
	return "on the mainland"
}

// StoreNear is how close to Jiminua counts as at the store.
const StoreNear = 6

// PlaceOf is the place of a tile.
func PlaceOf(t tile.Tile) Place {
	switch {
	case NearGate(t):
		return PlaceGate
	case t.Level == 0 && tile.Chebyshev(t, JiminuaAt) <= StoreNear:
		return PlaceStore
	case OnKaramja(t):
		return PlaceIsland
	}
	return PlaceMainland
}

// OnIsland reports whether the place is anywhere on Karamja's surface.
func (p Place) OnIsland() bool { return p != PlaceMainland }

// ---- the carrier's stages ----

// CarrierStage is the next thing a carrier does.
type CarrierStage string

// The stages.
const (
	// CarrierRecover: the account died. The kit is gone (or on the Karamja floor) and the shift is over.
	CarrierRecover CarrierStage = "recover"
	// CarrierRest stands still where it is until the hitpoints are back.
	CarrierRest CarrierStage = "rest"
	// CarrierAskKit asks the mule for the kit at its post: a note stack of essence, coins and a rub item.
	CarrierAskKit CarrierStage = "ask-kit"
	// CarrierCross walks from the mainland to the store: the ship from Ardougne, the jungle road.
	CarrierCross CarrierStage = "cross"
	// CarrierWalkToStore walks from the gate (or the pier) to Jiminua.
	CarrierWalkToStore CarrierStage = "walk-to-store"
	// CarrierShelf sells notes to Jiminua and buys the load back unnoted.
	CarrierShelf CarrierStage = "shelf"
	// CarrierWalkToGate walks to the crafter with the load.
	CarrierWalkToGate CarrierStage = "walk-to-gate"
	// CarrierTrade trades the load to the crafter.
	CarrierTrade CarrierStage = "trade"
	// CarrierWait stands at the gate while the crafter is away or busy.
	CarrierWait CarrierStage = "wait"
	// CarrierCourier takes Natures from the crafter, to carry them home.
	CarrierCourier CarrierStage = "courier"
	// CarrierGoHome walks home to the mule's post, ship included.
	CarrierGoHome CarrierStage = "go-home"
	// CarrierDeliver hands the leftovers (and any Natures) to the mule at its post and banks the rest.
	CarrierDeliver CarrierStage = "deliver"
	// CarrierDone ends the shift.
	CarrierDone CarrierStage = "done"
)

// The carrier's numbers.
const (
	// MinLoad is the fewest unnoted essence worth walking to the gate with (a carrier whose pack was a slot short).
	MinLoad = GateOfferMin
	// RestBelowPercent and RestToPercent are the hitpoint hysteresis of a rest on the island.
	RestBelowPercent = 50
	RestToPercent    = 85
	// GateWaitMax is how long a carrier stands at the gate with a load and no trade before it gives the shift up.
	GateWaitMax = 10 * time.Minute
	// ShiftMax bounds one shift: a kit of 3,000 essence is 120 trips of about 4 minutes; longer than this something is wrong.
	ShiftMax = 6 * time.Hour
	// MaxCarriers is how many carriers saturate the gate; a carrier does not start a shift when this many are at work. Ten was
	// the queue model's six thousand an hour; on 20 Oct 2026, with the main account on a direct route, ten carriers at work kept the gate
	// at a 15.6 s trade (5.5k essence an hour) with the main account taking every load as it came, and the shorter temple legs of
	// claude/masterspeed-1019 shorten the cycle further. Thirteen covers a cycle down to about 21 ticks
	// (docs/xp_rate_plan_2026-10-19.md, CR-8).
	MaxCarriers = 13
)

// GateInfo is the gate as the carrier hears it (mulebus.GateView, without the import).
type GateInfo struct {
	// Heard is set when the crafter's message is on the bus within its TTL.
	Heard bool
	// State is "opening", "open", "away" or "closed".
	State string
	// Surplus is the Natures a courier may take now.
	Surplus int
	// Cap is the essence the crafter takes in one trade (0: not said, CarrierOffer). It is 24 while he holds a stack of bows to alch
	// (a 4th pack slot besides the talisman, the Natures and the fare).
	Cap int
}

// ShelfWant is the essence a carrier loads for the gate: a full load, or what the crafter says he can take when that is less (a trade of
// more is refused by the engine for want of pack slots). A capacity under MinLoad is not worth a trip and is ignored.
func (g GateInfo) ShelfWant() int {
	if g.Cap >= MinLoad && g.Cap < CarrierOffer {
		return g.Cap
	}
	return CarrierOffer
}

// Usable reports whether the gate takes carriers: opening or open.
func (g GateInfo) Usable() bool { return g.Heard && (g.State == "opening" || g.State == "open") }

// Away reports the crafter away for a while.
func (g GateInfo) Away() bool { return g.Heard && g.State == "away" }

// Closed reports the round over, or the crafter unheard.
func (g GateInfo) Closed() bool { return !g.Heard || g.State == "closed" }

// CarrierSnapshot is what DecideCarrierStage reads, all of it the live state.
type CarrierSnapshot struct {
	Dead bool
	// Resting is the hitpoint hysteresis already applied by the caller (HPLow).
	Resting bool
	Place   Place
	// AtPost is set when the carrier stands by the mule's post.
	AtPost bool
	// Notes and Unnoted are the Rune essence held noted and unnoted; Natures the Nature runes held (a courier's haul).
	Notes, Unnoted, Natures int
	Coins                   int
	Gate                    GateInfo
	// KitAsked is set once the mule has been asked this shift, Over when the shift must end (the budget, failures, a stop).
	KitAsked, Over bool
	// GateWaited is how long the carrier has stood at the gate with a load and no trade.
	GateWaited time.Duration
}

// CarrierStep is DecideCarrierStage's answer.
type CarrierStep struct {
	Stage CarrierStage
	Why   string
}

// HPLow is the rest hysteresis: resting begins under RestBelowPercent of the maximum and lasts until RestToPercent. resting is
// the state it was in.
func HPLow(hp, maxHP int, resting bool) bool {
	if maxHP <= 0 || hp <= 0 {
		return false
	}
	pct := hp * 100 / maxHP
	if resting {
		return pct < RestToPercent
	}
	return pct < RestBelowPercent
}

// DecideCarrierStage chooses a carrier's next stage from the live state. It is a function of that state alone, so a restart
// anywhere (on the road with the kit, at the gate with a load) resumes in the right place.
func DecideCarrierStage(s CarrierSnapshot) CarrierStep {
	if s.Dead {
		return CarrierStep{CarrierRecover, "the account died: the shift is over"}
	}
	g := s.Gate
	if !s.Place.OnIsland() {
		return decideMainland(s)
	}
	if s.Resting {
		return CarrierStep{CarrierRest, "hitpoints are low on the island: standing still until they are back"}
	}
	hasLoad := s.Unnoted >= MinLoad
	canRestock := s.Notes >= CarrierOffer
	switch {
	case s.Over:
		return CarrierStep{CarrierGoHome, "the shift is over"}
	case g.Closed():
		return CarrierStep{CarrierGoHome, "the gate is closed or its crafter is not heard"}
	case hasLoad && s.Place == PlaceGate && s.GateWaited >= GateWaitMax:
		return CarrierStep{CarrierGoHome, "waited " + s.GateWaited.Round(time.Second).String() + " at the gate with a load and no trade"}
	case hasLoad && s.Place == PlaceGate && g.Away():
		return CarrierStep{CarrierWait, "the crafter is away; waiting at the gate with the load"}
	case hasLoad && s.Place == PlaceGate:
		return CarrierStep{CarrierTrade, strconv.Itoa(s.Unnoted) + " essence to trade at the gate"}
	case hasLoad:
		return CarrierStep{CarrierWalkToGate, strconv.Itoa(s.Unnoted) + " essence in the pack"}
	case canRestock && s.Place == PlaceStore:
		return CarrierStep{CarrierShelf, strconv.Itoa(s.Notes) + " notes to turn into a load at the store"}
	case canRestock:
		return CarrierStep{CarrierWalkToStore, strconv.Itoa(s.Notes) + " notes, no load"}
	case g.Usable() && g.Surplus >= CourierMin:
		return CarrierStep{CarrierCourier, "the kit is spent and the crafter holds " + strconv.Itoa(g.Surplus) + " Natures for a courier"}
	}
	return CarrierStep{CarrierGoHome, "the kit is spent (" + strconv.Itoa(s.Notes) + " notes)"}
}

// decideMainland is the part of the decision made off the island.
func decideMainland(s CarrierSnapshot) CarrierStep {
	g := s.Gate
	// A kit is NOTES. Loose unnoted essence on the mainland (a load a shift ended with) is a leftover to hand to the mule, not a kit:
	// read as one it sent the carrier across the sea on a sixteen minute trip for a single trade, with no new kit asked.
	haveKit := s.Notes >= CarrierOffer
	leftovers := s.Notes > 0 || s.Unnoted > 0 || s.Natures > 0
	if s.Over || g.Closed() || (!haveKit && s.KitAsked) {
		why := "the shift is over"
		switch {
		case !s.Over && g.Closed():
			why = "the gate is closed or its crafter is not heard"
		case !s.Over && !haveKit && s.KitAsked:
			why = "the kit is spent"
		}
		if leftovers {
			if s.AtPost {
				return CarrierStep{CarrierDeliver, why + "; handing the leftovers to the mule"}
			}
			return CarrierStep{CarrierGoHome, why + "; walking to the mule's post with the leftovers"}
		}
		return CarrierStep{CarrierDone, why}
	}
	if !haveKit {
		return CarrierStep{CarrierAskKit, "no kit: asking the mule"}
	}
	return CarrierStep{CarrierCross, "the kit is in the pack (" + strconv.Itoa(s.Notes) + " notes, " + strconv.Itoa(s.Coins) + " coins)"}
}

// ---- starting a shift ----

// Kit sizes.
const (
	// KitEssenceDefault is carrierKitEssence: 3,000 notes are 120 trips.
	KitEssenceDefault = 3000
	// KitCoinsDefault is carrierKitCoins: 120 trips at about 50 gold, with a long margin and the fares.
	KitCoinsDefault = 10000
	// MinKitEssence is the least essence worth a shift: four trips.
	MinKitEssence = 4 * CarrierOffer
	// MuleReserve is the essence the mule keeps back from a kit: the Priest in Peril ladders draw 24 each.
	MuleReserve = 0
	// ShiftMinEssence is the fewest essence a shift is worth starting for: the crossing, the kit ask and the way home cost about
	// sixteen minutes whatever the kit, so a shift of 20 trips (500 essence, 83 percent of its time on trips) is the least that
	// pays; a carrierKitEssence under it is the least instead.
	ShiftMinEssence = 500
)

// KitEssence is the essence to ask the mule for: the setting, at most what the mule has published, in whole loads. 0 when
// it is under MinKitEssence.
func KitEssence(want, mule int) int {
	n := min(want, mule-MuleReserve)
	n -= n % CarrierOffer
	if n < MinKitEssence {
		return 0
	}
	return n
}

// ShiftTrips is how many trips a kit of notes pays for.
func ShiftTrips(notes int) int { return notes / CarrierOffer }

// ShiftView is what DecideCarrierShift reads.
type ShiftView struct {
	Now time.Time
	// CrafterSet says the runecraftCrafter setting names the crafter.
	CrafterSet bool
	Gate       GateInfo
	// GateWorld and MyWorld are the worlds (0: not carried / not known).
	GateWorld, MyWorld int
	// Carriers is how many other carriers are at work now.
	Carriers int
	// MaxHP is this account's maximum hitpoints, MinHP the floor (carrierMinHP).
	MaxHP, MinHP int
	// MuleHeard and MuleSameWorld: the mule's heartbeat is on the bus, on this world. MuleEssence is its published Rune essence
	// and MuleKnown whether it has published at all.
	MuleHeard, MuleSameWorld bool
	MuleEssence              int
	MuleKnown                bool
	KitWant                  int
	// BackedOffUntil holds the carrier off after a failed shift.
	BackedOffUntil time.Time
	Members        bool
}

// DecideCarrierShift says whether a shift starts now and why not when it does not. The reasons are the log's.
func DecideCarrierShift(v ShiftView) (start bool, why string) {
	switch {
	case !v.CrafterSet:
		return false, "runecraftCrafter is not set"
	case !v.Members:
		return false, "this is not a members world: the crafter's Nature runes would not be made"
	case v.Now.Before(v.BackedOffUntil):
		return false, "backing off until " + v.BackedOffUntil.Format("15:04") + " after a shift that failed"
	case !v.Gate.Heard:
		return false, "the gate is not heard on the bus"
	case v.Gate.State == "away":
		return false, "the crafter is away"
	case v.Gate.State == "closed":
		return false, "the gate is closed"
	case !v.Gate.Usable():
		return false, "the gate is " + v.Gate.State
	case v.GateWorld > 0 && v.MyWorld > 0 && v.GateWorld != v.MyWorld:
		return false, "the gate is on world " + strconv.Itoa(v.GateWorld) + ", this account on " + strconv.Itoa(v.MyWorld)
	case v.MaxHP < v.MinHP:
		return false, "maximum hitpoints " + strconv.Itoa(v.MaxHP) + " is under carrierMinHP " + strconv.Itoa(v.MinHP)
	case v.Carriers >= MaxCarriers:
		return false, strconv.Itoa(v.Carriers) + " carriers are at work: the gate is saturated"
	case !v.MuleHeard:
		return false, "the mule is not heard on the bus"
	case !v.MuleSameWorld:
		return false, "the mule is on another world"
	case !v.MuleKnown:
		return false, "the mule has not published its bank"
	case KitEssence(v.KitWant, v.MuleEssence) == 0:
		return false, "the mule holds " + strconv.Itoa(v.MuleEssence) + " Rune essence, under the " + strconv.Itoa(MinKitEssence) + " a kit needs"
	case KitEssence(v.KitWant, v.MuleEssence) < min(ShiftMinEssence, KitEssence(v.KitWant, v.KitWant)):
		return false, "the mule holds " + strconv.Itoa(v.MuleEssence) + " Rune essence, under the " + strconv.Itoa(min(ShiftMinEssence, KitEssence(v.KitWant, v.KitWant))) + " a shift is worth the crossing for"
	}
	return true, "the gate is " + v.Gate.State + ", the mule holds " + strconv.Itoa(v.MuleEssence) + " Rune essence"
}

// ---- the walk, in ticks ----

// WalkMode is how the carrier covers its tiles.
type WalkMode int

// The walk modes.
const (
	// WalkPlain walks every tile (run off).
	WalkPlain WalkMode = iota
	// WalkRun runs whenever the client allows (energy of 1% or more), nothing holding the player.
	WalkRun
	// WalkRub runs and rubs a Games necklace or Ring of dueling about every third tick (scarabplan.RubEveryTicks): a rub's
	// p_delay(1) freezes the tick it is read in and the next, and the engine neither drains nor recovers energy in a frozen
	// tick. The tick lab measured the running walk at 0.125 to 0.139 percent of energy a tile with the rub, against 0.351 to
	// 0.393 without: 13 units a tile, which is a drain in RubDrainShare of the running ticks (a perfect every-third-tick rub would
	// be 11).
	WalkRub
)

// RubDrainShare is the share of running ticks that still drain energy while the carrier rubs: 26 units a running tick of two
// tiles against the 67 an unheld tick costs (the tick lab's 13 units a tile).
const RubDrainShare = 0.39

func (m WalkMode) String() string {
	switch m {
	case WalkRun:
		return "run"
	case WalkRub:
		return "run with rubs"
	}
	return "walk"
}

// TripTicks is the ticks of one carrier trip at Agility `agility` carrying `weightGrams`, run through the engine's energy rule
// (tickmanip.ApplyTick = Player.updateEnergy) trip after trip until it repeats, and the last trip's ticks returned: the
// equilibrium, not a fresh start's full energy. The client turns run on at 1% (100 units) and the engine turns it off at 0.
//
// Agility 1, 244 tiles and 30 ticks standing: about 274 walking, 248 running and 224 with the rub. That is how a carrier is
// sized: 25 essence a trip, 6,000 / 224 trips an hour, about 670 an hour.
func TripTicks(t CarrierTrip, agility, weightGrams int, mode WalkMode) int {
	energy := tickmanip.MaxEnergy
	running := false
	last := 0
	for trip := 0; trip < 12; trip++ {
		tiles, ticks, share := t.Tiles, 0, 0.0
		for tiles > 0 {
			ticks++
			if mode == WalkPlain {
				tiles--
				energy = tickmanip.ApplyTick(energy, tickmanip.EnergyTick{Steps: 1}, agility, weightGrams)
				continue
			}
			if !running && energy >= 100 {
				running = true
			}
			if running && energy <= 0 {
				running = false
			}
			if !running {
				tiles--
				energy = tickmanip.ApplyTick(energy, tickmanip.EnergyTick{Steps: 1}, agility, weightGrams)
				continue
			}
			tiles -= 2
			frozen := false
			if mode == WalkRub {
				// A drain tick when the share's accumulator fills, a held tick otherwise.
				share += RubDrainShare
				if share >= 1 {
					share--
				} else {
					frozen = true
				}
			}
			energy = tickmanip.ApplyTick(energy, tickmanip.EnergyTick{Steps: 2, Frozen: frozen}, agility, weightGrams)
			if energy <= 0 {
				running = false
			}
		}
		// The ticks standing still (the shelf, the trade, the queue) recover energy.
		for i := 0; i < t.Idle; i++ {
			ticks++
			energy = tickmanip.ApplyTick(energy, tickmanip.EnergyTick{Steps: 0}, agility, weightGrams)
		}
		last = ticks
	}
	return last
}

// CarrierRate is the essence an hour one carrier brings at a trip of `ticks`.
func CarrierRate(ticks, perTrip int) int {
	if ticks <= 0 {
		return 0
	}
	return perTrip * TicksPerHour / ticks
}
