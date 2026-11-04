// Package shopplan is ShopBuyout's own decisions, ported from the public catalogue's
// rsb0t/src/bot/scripts/ShopBuyout/ShopBuyout.ts together with the two pure siblings it
// leans on, api/shop/StockModel.ts (the price model) and api/shop/BuyoutLogic.ts (the plan).
// It is pure: no client, actions, fleet or bot imports and no I/O, so its tests run with no
// live server.
//
// The script is a two-phase loop between a bank and one shopkeeper: withdraw coins, walk to
// the keeper, buy the chosen stock out (most valuable first, as far as the coins reach), walk
// back, deposit the haul, repeat until the session budget is spent or the bank runs dry.
//
// Grounding — every game fact below was confirmed against the content checkout on branch 289
// and against the RUNNING local engine's own config archive, not trusted from the TypeScript:
//
//   - Opening a shop is `[opnpc3,_shop_keeper] ~openshop_activenpc` in
//     content/scripts/shop/scripts/shop.rs2, and both keepers below carry `op3=Trade` plus
//     `category=shop_keeper`. The multipliers the price model needs are npc params on the
//     KEEPER, not on the inv: `shop_sell_multiplier`, `shop_buy_multiplier` and `shop_delta`.
//   - The buy price is content's own `~adjusted_item_cost_buying` -> `~calc_shop_value`, whose
//     arithmetic UnitPrice below reproduces exactly. See UnitPrice's own comment for the
//     line-by-line correspondence; the TypeScript's unitPrice matched content exactly, which
//     is worth saying since the precedent is that a TS table often does not.
//   - Stock baselines and restock rates are the `.inv` block's own `stockN=<obj>,<count>,<rate>`
//     triples (content/scripts/areas/area_varrock/configs/varrock.inv), and the TS shopdb's
//     rows for these two shops matched them exactly.
//   - Every object id below was read back from the running engine's config archive
//     (cmd/cachecfgtest -find), never derived by arithmetic. See "ID TRAPS" below for why that
//     matters more here than anywhere else this codebase has ported.
//
// ID TRAPS, and this package sits on top of both of the ones earlier ports paid for:
//
//  1. DISPLAY NAMES ARE NOT UNIQUE. "Shortbow" resolves to FOUR object ids on this content
//     build (50 members, 51 its note, 841 free-to-play, 842 its note), and so do "Longbow",
//     "Oak shortbow" and "Oak longbow". "Bolts" resolves to two (877 and 3810), and even
//     "Coins" resolves to two (617 and the real 995). A name match could pick a members
//     variant, a note, or another item altogether. Everything here matches by OBJECT ID.
//  2. "THE NOTED VARIANT SITS AT id+1" IS TRUE FOR SOME OF THESE ROWS AND FALSE FOR OTHERS,
//     which is why nothing here derives an id by arithmetic. It holds for the bows
//     (shortbow 841 -> note 842) and fails completely for the runes, where fire rune 554 plus
//     one is WATER rune 555, a different item the shop also stocks. The HerbCleaner port found
//     the same split across its own table; this one has both halves inside a single preset.
//
// DELIBERATELY NOT PORTED — StockModel.ts's `expectedStock`, and this is a correction rather
// than an omission. That function models restock as a per-item timer since the stock was last
// seen (`steps = floor((now - seen.atMs) / 600 / restockTicks)`). The engine does something
// different: World.ts's tick loop moves each stocked slot ONE unit toward its baseline only
// when `tick % stockrate[index] === 0`, on the GLOBAL world tick, and converges from both
// sides (an over-stocked slot decays downward at the same rate). So the TS model's phase is
// wrong, and nothing in ShopBuyout needs it anyway — the shop screen reports real stock, which
// is what BuyoutPlan is given. A future ShopRunner-style route port that must estimate stock
// BEFORE arriving should model the engine's rule, not the TypeScript's.
//
// Also deliberately not ported: the paint/status overlay (ShopBuyout.ts's own onPaint), the
// same boundary every other ported script draws; and there is no mule hand-off, confirmed by
// reading the whole TS source — ShopBuyout.ts imports no donorPlan or muleProtocol, and the
// bank is the only destination for every trip.
package shopplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// CoinsID is the real coin object. Matched by id and never by name on purpose: the live config
// archive resolves the display name "Coins" to TWO objects on this build, 617 (members) and
// this one.
const CoinsID int32 = 995

// PackSlots is a full pack.
const PackSlots = 28

const (
	// TradeOp is the keeper option that opens a shop, and BoothName/BoothOp the bank loc and
	// the option that opens it. All three are live-confirmed: Aubury (npc 553) and Lowe
	// (npc 550) both report ops ["Talk-to" "" "Trade" "Teleport"/"" ""], and loc 2213
	// "Bank booth" reports ops ["Use" "Use-quickly" "" "" ""].
	TradeOp   = "Trade"
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// Item is one row of a shop's `.inv` stock list, joined to the object's own config.
type Item struct {
	// Key is the content debugname, the stable key logs and tests use.
	Key string
	// Name is the display name. It is NOT unique across objects — see the package doc
	// comment's ID TRAPS — so it is for display only and never for matching.
	Name string
	// ID is the live-confirmed object id, the only thing this package ever matches on.
	ID int32
	// Baseline is the `.inv` row's stock count: the level the engine restocks toward, and the
	// stock at which the item sells for exactly the shop's sell multiplier.
	Baseline int32
	// RestockTicks is the `.inv` row's rate: the engine moves this slot one unit toward
	// Baseline on every world tick divisible by it. See the package doc comment.
	RestockTicks int32
	// Cost is the object's own `cost=`, content's `oc_cost`, which the price model scales.
	Cost int32
	// Stackable decides whether a buy needs a free pack slot at all — see NeedsSpace.
	Stackable bool
	// Members marks a members-only object, which a free world refuses to trade.
	Members bool
}

// Shop is one preset: a keeper, the two tiles the loop walks between, and the stock list.
type Shop struct {
	Key string
	// Label is what the `shop` setting offers and matches on.
	Label string
	Title string
	// Inv is the content `.inv` debugname backing the stock.
	Inv string
	// Keeper is the NPC display name the run opens the shop on.
	Keeper string
	// Sell, Buy and Delta are the keeper's own npc params `shop_sell_multiplier`,
	// `shop_buy_multiplier` and `shop_delta`. Only Sell and Delta take part in a purchase;
	// Buy is recorded because it is the same param block and a future selling port needs it.
	Sell  int32
	Buy   int32
	Delta int32
	// ShopStand is where the run stands to open the shop, BankStand where it banks.
	ShopStand tile.Tile
	BankStand tile.Tile
	BoothName string
	BoothOp   string
	Items     []Item
}

// varrockEastBank is the bank both presets use. Confirmed against the baked static loc index
// rather than taken from the TypeScript, which named (3251,3420): the four open "Bank booth"
// locs (id 2213, op "Use-quickly") stand at x=3252,3253,3254,3256 on z=3419, and 3251,3419
// holds a "Closed bank booth" (id 2215, no ops) instead. Standing a tile north of the middle
// of the open run is the honest translation of "the Varrock East bank".
var varrockEastBank = tile.Tile{X: 3253, Z: 3420, Level: 0}

// Shops are the presets, in the order the `shop` setting offers them.
//
// Two shops rather than the TypeScript's eleven, deliberately: every row below is confirmed
// against content AND the live config archive, and the remaining nine presets would each need
// their own keeper params, `.inv` block and per-item id/cost confirmation before they could be
// offered honestly. The table is shaped for more; adding one is a content-confirmation
// exercise, not a code change.
var Shops = []Shop{
	{
		Key: "runeshop", Label: "Aubury's runes — Varrock (Varrock East bank)",
		Title: "Aubury's Rune Shop.", Inv: "runeshop", Keeper: "Aubury",
		Sell: 1000, Buy: 550, Delta: 10,
		ShopStand: tile.Tile{X: 3253, Z: 3401, Level: 0},
		BankStand: varrockEastBank,
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "firerune", Name: "Fire rune", ID: 554, Baseline: 2000, RestockTicks: 10, Cost: 4, Stackable: true},
			{Key: "waterrune", Name: "Water rune", ID: 555, Baseline: 2000, RestockTicks: 10, Cost: 4, Stackable: true},
			{Key: "airrune", Name: "Air rune", ID: 556, Baseline: 2000, RestockTicks: 10, Cost: 4, Stackable: true},
			{Key: "earthrune", Name: "Earth rune", ID: 557, Baseline: 2000, RestockTicks: 10, Cost: 4, Stackable: true},
			{Key: "mindrune", Name: "Mind rune", ID: 558, Baseline: 1000, RestockTicks: 10, Cost: 3, Stackable: true},
			{Key: "bodyrune", Name: "Body rune", ID: 559, Baseline: 1000, RestockTicks: 10, Cost: 3, Stackable: true},
			{Key: "chaosrune", Name: "Chaos rune", ID: 562, Baseline: 1000, RestockTicks: 100, Cost: 15, Stackable: true},
			{Key: "deathrune", Name: "Death rune", ID: 560, Baseline: 1000, RestockTicks: 150, Cost: 30, Stackable: true},
		},
	},
	{
		Key: "archeryshop", Label: "Lowe's arrows and bows — Varrock (Varrock East bank)",
		Title: "Lowe's Archery Emporium", Inv: "archeryshop", Keeper: "Lowe",
		Sell: 1000, Buy: 550, Delta: 10,
		ShopStand: tile.Tile{X: 3231, Z: 3421, Level: 0},
		BankStand: varrockEastBank,
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "bronze_arrow", Name: "Bronze arrow", ID: 882, Baseline: 2000, RestockTicks: 10, Cost: 1, Stackable: true},
			{Key: "iron_arrow", Name: "Iron arrow", ID: 884, Baseline: 1500, RestockTicks: 15, Cost: 3, Stackable: true},
			{Key: "steel_arrow", Name: "Steel arrow", ID: 886, Baseline: 1000, RestockTicks: 20, Cost: 12, Stackable: true},
			// Display name "Bolts", not "Bolt" — and it resolves to two object ids (877 and
			// 3810) in the live archive, which is why this row carries the id.
			{Key: "bolt", Name: "Bolts", ID: 877, Baseline: 1500, RestockTicks: 10, Cost: 3, Stackable: true},
			// The five bows below are NOT stackable, so each one bought needs its own free
			// pack slot. They are the reason NeedsSpace exists.
			{Key: "shortbow", Name: "Shortbow", ID: 841, Baseline: 4, RestockTicks: 100, Cost: 50},
			{Key: "longbow", Name: "Longbow", ID: 839, Baseline: 4, RestockTicks: 100, Cost: 80},
			{Key: "oak_shortbow", Name: "Oak shortbow", ID: 843, Baseline: 2, RestockTicks: 200, Cost: 100},
			{Key: "oak_longbow", Name: "Oak longbow", ID: 845, Baseline: 2, RestockTicks: 200, Cost: 160},
			{Key: "crossbow", Name: "Crossbow", ID: 837, Baseline: 2, RestockTicks: 400, Cost: 70},
		},
	},
	{
		// content area_catherby: arheinstore (stock6 chisel,2,100) and npc arhein (563), nomove at
		// 2803,3430 (m43_53), the shop just south of the Catherby bank. Owner, 3 Oct: a chisel for
		// the main account from here. Only the chisel is listed.
		Key: "arheinstore", Label: "Arhein's store — Catherby (Catherby bank)",
		Title: "Arhein Store", Inv: "arheinstore", Keeper: "Arhein",
		Sell: 1300, Buy: 400, Delta: 30,
		ShopStand: tile.Tile{X: 2803, Z: 3431, Level: 0},
		BankStand: tile.Tile{X: 2809, Z: 3440, Level: 0},
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "chisel", Name: "Chisel", ID: 1755, Baseline: 2, RestockTicks: 100, Cost: 1},
		},
	},
}

// ShopLabels lists every preset label, the option set the `shop` setting accepts.
func ShopLabels() []string {
	out := make([]string, 0, len(Shops))
	for _, s := range Shops {
		out = append(out, s.Label)
	}
	return out
}

// ShopByLabel finds a preset by its label, matching ignoring case.
func ShopByLabel(label string) (Shop, bool) {
	want := strings.TrimSpace(label)
	for _, s := range Shops {
		if strings.EqualFold(s.Label, want) {
			return s, true
		}
	}
	return Shop{}, false
}

// ShopByKey finds a preset by its content inv debugname.
func ShopByKey(key string) (Shop, bool) {
	want := strings.TrimSpace(key)
	for _, s := range Shops {
		if strings.EqualFold(s.Key, want) {
			return s, true
		}
	}
	return Shop{}, false
}

// ItemByID finds the stock row an object id belongs to. This is the only lookup the runner
// uses to turn a live shop slot into a known item.
func (s Shop) ItemByID(id int32) (Item, bool) {
	for _, it := range s.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// ItemNames lists this shop's stock by display name, the option set its `buyItems` setting
// offers. Names are for the operator's benefit only; selection is resolved back to ids.
func (s Shop) ItemNames() []string {
	out := make([]string, 0, len(s.Items))
	for _, it := range s.Items {
		out = append(out, it.Name)
	}
	return out
}

// AllItemNames lists every stock name across every preset, de-duplicated, for a settings
// schema that must offer one option list before the shop is chosen.
func AllItemNames() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range Shops {
		for _, it := range s.Items {
			lower := strings.ToLower(it.Name)
			if seen[lower] {
				continue
			}
			seen[lower] = true
			out = append(out, it.Name)
		}
	}
	return out
}

// ---- the price model ----

// UnitPrice is what the next single unit of it costs when the shop holds stock of it, ported
// from content rather than from the TypeScript and matching it line for line:
//
//	[proc,calc_shop_value](int $base_cost, int $shophaggle, int $shopbuy, int $int3)(int)
//	def_int $int5 = min(1000, max(-5000, calc($int3 * $shophaggle)));
//	$int5 = max(100, calc($shopbuy - $int5));
//	$int4 = scale($int5, 1000, $base_cost);
//
// with `$int3` supplied by `~price_mod` as `stock - baseline`, `$shophaggle` the keeper's
// `shop_delta`, and `$shopbuy` the keeper's `shop_sell_multiplier` for a purchase (content
// passes the SELL multiplier into the parameter named `$shopbuy`; the name is the engine's,
// not a mistake here). `scale(a, b, c)` is `(a * c) / b` in the engine's NumberOps, pushed
// through `toInt32`, which is `num | 0` — truncation toward zero, and every value here is
// positive, so Go's integer division agrees exactly.
//
// The final floor at 1 is content's own, and only on the buying side:
// `adjusted_item_cost_buying` clamps a sub-1 value up to 1, while the selling side does not.
func UnitPrice(it Item, s Shop, stock int32) int32 {
	haggle := (stock - it.Baseline) * s.Delta
	if haggle > 1000 {
		haggle = 1000
	}
	if haggle < -5000 {
		haggle = -5000
	}
	pct := s.Sell - haggle
	if pct < 100 {
		pct = 100
	}
	value := (pct * it.Cost) / 1000
	if value < 1 {
		value = 1
	}
	return value
}

// BuyoutCost is what buying units of it costs in one go starting from stock, walking the price
// up as each unit drains the shelf — content charges each unit at its own price, in a loop
// (`[label,buy_item]` recomputes `~adjusted_item_cost_buying` on every iteration), so a flat
// "units times unit price" would under-estimate a large purchase.
func BuyoutCost(it Item, s Shop, stock, units int32) int32 {
	total := int32(0)
	for i := int32(0); i < units; i++ {
		total += UnitPrice(it, s, stock-i)
	}
	return total
}

// ---- the plan ----

// Want is one line of a buyout plan: how many of an item to ask for, and what it is expected
// to cost. The cost is an estimate by construction — the shop's stock can move between
// planning and clicking — so the runner always measures the real coin delta instead.
type Want struct {
	Item    Item
	Units   int32
	EstCost int32
}

// BuyoutPlan is BuyoutLogic.ts's own buyoutPlan: take the chosen items that are actually in
// stock, most valuable first, and for each buy as many as the coins still reach, charging each
// unit at its own falling-stock price. An empty or nil chosen set means every item, which is
// the TypeScript's own "leave the selection empty to buy the shop out" default.
//
// stock is keyed by object id, read from the live shop screen.
func BuyoutPlan(s Shop, stock map[int32]int32, coins int32, chosen map[string]bool) []Want {
	wants := make([]Item, 0, len(s.Items))
	for _, it := range s.Items {
		if len(chosen) > 0 && !chosen[strings.ToLower(it.Name)] {
			continue
		}
		if stock[it.ID] <= 0 {
			continue
		}
		wants = append(wants, it)
	}
	// Most valuable first, by the object's own base cost, as the TS sorts. A stable
	// insertion sort keeps the table's order among equal-cost rows, so a plan is reproducible.
	for i := 1; i < len(wants); i++ {
		for j := i; j > 0 && wants[j].Cost > wants[j-1].Cost; j-- {
			wants[j], wants[j-1] = wants[j-1], wants[j]
		}
	}

	left := coins
	plan := make([]Want, 0, len(wants))
	for _, it := range wants {
		have := stock[it.ID]
		units, est := int32(0), int32(0)
		for units < have {
			next := UnitPrice(it, s, have-units)
			if est+next > left {
				break
			}
			est += next
			units++
		}
		left -= est
		if units > 0 {
			plan = append(plan, Want{Item: it, Units: units, EstCost: est})
		}
	}
	return plan
}

// NeedsSpace reports whether buying it requires a free pack slot the run does not have,
// ported from ShopBuyout.ts's own needSpaceFor. A stackable item only needs a slot when the
// pack holds none of it yet; an unstackable one needs a slot for every unit, so a full pack
// blocks it outright.
func NeedsSpace(it Item, packHoldsSome bool, packFull bool) bool {
	if !packFull {
		return false
	}
	if it.Stackable {
		return !packHoldsSome
	}
	return true
}

// AnyChosenInStock reports whether the keeper currently holds any of the chosen items at all.
// Together with CanAffordAnything it separates the two reasons a buying pass can plan nothing,
// which need opposite responses: an empty shelf is worth waiting at, an empty purse is not.
func AnyChosenInStock(s Shop, stock map[int32]int32, chosen map[string]bool) bool {
	for _, it := range s.Items {
		if len(chosen) > 0 && !chosen[strings.ToLower(it.Name)] {
			continue
		}
		if stock[it.ID] > 0 {
			return true
		}
	}
	return false
}

// CanAffordAnything reports whether coins stretch to at least one unit of any chosen item at
// the price its CURRENT stock implies.
//
// The current stock is the whole point. A drained shelf charges far more than its baseline —
// content raises the price by shop_delta for every unit below baseline — so a purse that
// looks sufficient against the baseline price can be unable to buy a single unit in practice.
func CanAffordAnything(s Shop, stock map[int32]int32, coins int32, chosen map[string]bool) bool {
	for _, it := range s.Items {
		if len(chosen) > 0 && !chosen[strings.ToLower(it.Name)] {
			continue
		}
		have := stock[it.ID]
		if have <= 0 {
			continue
		}
		if UnitPrice(it, s, have) <= coins {
			return true
		}
	}
	return false
}

// ---- stages ----

// Stage is the two-phase machine ShopBuyout.ts keeps in its own `phase` field. There is no
// third "done" stage on purpose: the TypeScript stops from inside its bank trip, AFTER
// depositing the haul, so a run that has spent its budget still banks what it bought. See
// DecideBank, which owns every stop condition.
type Stage string

const (
	StageBank Stage = "bank"
	StageBuy  Stage = "buy"
)

// Snapshot is the live state DecideStage reads.
type Snapshot struct {
	// BudgetLeft is the session budget minus what has been spent.
	BudgetLeft int32
	// CoinsHeld is real coins in the pack, counted by CoinsID.
	CoinsHeld int32
	// PackFull is true when no pack slot is free.
	PackFull bool
	// MinUnitPrice is the cheapest chosen item's price at its baseline stock — a cheap
	// pre-walk floor on "enough coins to be worth the trip". Zero disables the check. It is
	// deliberately NOT the only funding check; see NeedCoins for why it cannot be.
	MinUnitPrice int32
	// NeedCoins is set by the caller when the last visit to the keeper could not afford a
	// single unit of the chosen stock AT ITS CURRENT PRICE.
	//
	// This exists because of a real live failure, not for symmetry. A run bought fire runes
	// until it had 7gp left and then stood at the shelf logging "nothing to buy, waiting 10s
	// for restock" indefinitely: 7gp still cleared MinUnitPrice, which is the 4gp a rune costs
	// at the shop's 2000 baseline, while the shelf it had just drained was charging 16gp. The
	// stock was fine and the restock wait was pointless — the purse was the problem, and the
	// bank was the answer. Waiting is only ever right when the shelf is empty.
	NeedCoins bool
}

// DecideStage picks the next stage from live state. Every call re-derives it rather than
// trusting a remembered phase, this codebase's own convention for a ported loop.
func DecideStage(s Snapshot) Stage {
	// Budget spent: still go to the bank, so the haul is deposited before the run stops.
	if s.BudgetLeft <= 0 {
		return StageBank
	}
	if s.PackFull {
		return StageBank
	}
	if s.NeedCoins {
		return StageBank
	}
	if s.CoinsHeld < s.MinUnitPrice {
		return StageBank
	}
	return StageBuy
}

// DescribeStage is a log-friendly name.
func DescribeStage(st Stage) string {
	switch st {
	case StageBank:
		return "banking the haul and drawing coins"
	case StageBuy:
		return "buying out the keeper"
	}
	return string(st)
}

// ---- the bank trip ----

// BankDecision is what one bank visit should do once the haul has been deposited.
type BankDecision struct {
	// Stop ends the run; Reason says why, and is the text the bot stops with.
	Stop   bool
	Reason string
	// Withdraw is how many coins to take out, 0 for none.
	Withdraw int32
}

// DecideBank ports ShopBuyout.ts's BankTrip, in its own order: the budget check first, then
// the bank-coins floor, then the top-up. Both stops are checked AFTER the caller has
// deposited, matching the TypeScript, so a finished run leaves nothing in the pack.
func DecideBank(bankCoins, coinsHeld, perTrip, budgetLeft, stopFloor int32) BankDecision {
	if budgetLeft <= 0 {
		return BankDecision{Stop: true, Reason: "the session budget is spent"}
	}
	if bankCoins < stopFloor {
		return BankDecision{Stop: true, Reason: "the bank's coins are below the stop floor"}
	}
	want := perTrip
	if budgetLeft < want {
		want = budgetLeft
	}
	need := want - coinsHeld
	if need <= 0 {
		return BankDecision{}
	}
	if need > bankCoins {
		need = bankCoins
	}
	return BankDecision{Withdraw: need}
}

// MinUnitPrice is the cheapest price among the chosen items at their baseline stock, for
// Snapshot.MinUnitPrice. Baseline is used rather than live stock because this is asked before
// walking to the shop, where no stock is visible. It is a floor on "enough coins to buy
// anything", not a promise about any particular purchase.
func MinUnitPrice(s Shop, chosen map[string]bool) int32 {
	best := int32(0)
	for _, it := range s.Items {
		if len(chosen) > 0 && !chosen[strings.ToLower(it.Name)] {
			continue
		}
		price := UnitPrice(it, s, it.Baseline)
		if best == 0 || price < best {
			best = price
		}
	}
	return best
}
