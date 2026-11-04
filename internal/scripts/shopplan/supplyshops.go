package shopplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Supply shops: the keepers MasterCrafter's shop trip buys from (a user request: "to get more bolts
// to make pearl bolts, the main account could grab coins from the mule to buy out the various shops stocking
// bolts"). They are NOT ShopBuyout presets (Shops above), so ShopBuyout's own `shop` option list is
// unchanged; they reuse this package's Shop, Item, UnitPrice and the shoprunner that drives a shop.
//
// EVERY SHOP IN CONTENT 289 THAT STOCKS "Bolts" (obj 877 `bolt`, the only bolt a tip goes on:
// skill_fletching/scripts/bolts.rs2 [opheldu,bolt] and [label,make_bolts] inv_del(inv, bolt, ...);
// there is no bronze bolt in 289 at all), read out of content/scripts/**/*.inv, the keepers' npc
// params out of the matching .npc, and their spawns out of content/maps/*.jm2's NPC sections:
//
//	Hickton    archeryshop2 (area_catherby/configs/catherby.inv), bolt,200,10. npc 575 hickton,
//	           spawn 2822,3442,0 (m44_53), sell 1000 buy 500 delta 10. Thirteen tiles east of
//	           the mule's post: a plain walk, no door in the pack's plan.
//	Lowe       archeryshop (area_varrock/configs/varrock.inv), bolt,1500,10. npc 550 lowe, spawn
//	           3232,3423,0 (m50_53), sell 1000 buy 550 delta 10. Webwalk from Catherby: ~600 tiles
//	           through the Taverley gate (loc 1596 at 2935,3451).
//	Shop keeper lathastrainingstore (area_combat_training/configs/combat_training_camp.inv),
//	           bolt,200,10. npc 561 lathas_shopkeeper, spawn 2514,3385,0 (m39_52), sell 1500 buy 500
//	           delta 10. Behind the camp gate (loc 2041 lathastraining_gater), which content opens
//	           only once Biohazard is complete (combat_training_camp.rs2
//	           [label,open_combat_training_gate]). Off unless chosen.
//	Gulluck    gnomeshop_gulluck (area_gnome/configs/gnome.inv), bolt,150,10 (and pearl_bolt,1,1500).
//	           npc 602 gnome_gulluck, spawn 2468,3487,2 (m38_54): level 2 of the Grand Tree. Not
//	           listed below: the webwalk graph has no route to level 2 there (planned from the post,
//	           every tile beside him is "unreachable"), so a trip could not reach him.
//
// THE RESTOCK RULE (engine World.ts, quoted in this package's doc): a slot moves ONE unit toward its
// baseline on every world tick divisible by its rate, so a bought-out `bolt,200,10` shelf is back to
// 200 after 2,000 ticks, twenty minutes. Every bolt shop is rate 10: ten an minute a shop.
//
// THE PRICE (UnitPrice): cost 3 (skill_combat/configs/ranged/bolts.obj), so a sell-1000 keeper
// charges 3 gp at baseline and 1% more of the base per unit below it, clamped at haggle -5000:
// at most 18 gp (Lathas, sell 1500: 4 gp to 19 gp).
//
// Hickton also stocks steel_arrowheads,600,40 ("Steel arrowtips": arrows.obj [steel_arrowheads] cost=6, which the live purchase of 237 for 2,985 off a full shelf confirms (the old 1 says 411); this table said 1 until 13 Oct 2026): the steel
// arrow round's tips, bought on the same trip when that round is short of them.
var SupplyShops = []Shop{
	{
		Key: "archeryshop2", Label: "Hickton's Archery Emporium — Catherby",
		Title: "Hicktons Archery Emporium.", Inv: "archeryshop2", Keeper: "Hickton",
		Sell: 1000, Buy: 500, Delta: 10,
		ShopStand: tile.Tile{X: 2822, Z: 3441, Level: 0},
		BankStand: tile.Tile{X: 2809, Z: 3440, Level: 0},
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "bolt", Name: "Bolts", ID: 877, Baseline: 200, RestockTicks: 10, Cost: 3, Stackable: true},
			{Key: "steel_arrowheads", Name: "Steel arrowtips", ID: 41, Baseline: 600, RestockTicks: 40, Cost: 6, Stackable: true},
		},
	},
	{
		Key: "archeryshop", Label: "Lowe's Archery Emporium — Varrock",
		Title: "Lowe's Archery Emporium", Inv: "archeryshop", Keeper: "Lowe",
		Sell: 1000, Buy: 550, Delta: 10,
		ShopStand: tile.Tile{X: 3231, Z: 3421, Level: 0},
		BankStand: varrockEastBank,
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "bolt", Name: "Bolts", ID: 877, Baseline: 1500, RestockTicks: 10, Cost: 3, Stackable: true},
		},
	},
	{
		Key: "lathastrainingstore", Label: "Combat training camp store — north Ardougne (Biohazard)",
		Title: "Armoury", Inv: "lathastrainingstore", Keeper: "Shop keeper",
		Sell: 1500, Buy: 500, Delta: 10,
		ShopStand: tile.Tile{X: 2513, Z: 3385, Level: 0},
		BankStand: tile.Tile{X: 2617, Z: 3332, Level: 0},
		BoothName: BoothName, BoothOp: BoothOp,
		Items: []Item{
			{Key: "bolt", Name: "Bolts", ID: 877, Baseline: 200, RestockTicks: 10, Cost: 3, Stackable: true},
			{Key: "steel_arrowheads", Name: "Steel arrowtips", ID: 41, Baseline: 800, RestockTicks: 40, Cost: 6, Stackable: true},
		},
	},
}

// SupplyShopNames are the names a setting may use for a supply shop: the keeper's name, with
// "Lathas" for the camp's anonymous Shop keeper.
var supplyShopAliases = map[string]string{
	"hickton": "archeryshop2", "archeryshop2": "archeryshop2", "catherby": "archeryshop2",
	"lowe": "archeryshop", "archeryshop": "archeryshop", "varrock": "archeryshop",
	"lathas": "lathastrainingstore", "lathastrainingstore": "lathastrainingstore", "shop keeper": "lathastrainingstore",
}

// SupplyShopNames lists the names SupplyShopByName accepts, one per shop.
func SupplyShopNames() []string { return []string{"Hickton", "Lowe", "Lathas"} }

// SupplyShopByName finds a supply shop by keeper name, alias or inv key, ignoring case.
func SupplyShopByName(name string) (Shop, bool) {
	key, ok := supplyShopAliases[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Shop{}, false
	}
	for _, s := range SupplyShops {
		if s.Key == key {
			return s, true
		}
	}
	return Shop{}, false
}

// TickDuration is the engine's game tick, for restock estimates.
const TickDuration = 600 * time.Millisecond

// EstimateShelf is what a slot of it probably holds now, seen holding left elapsed ago: one unit
// back every RestockTicks ticks, never above the baseline. A slot never seen (seen false) is taken
// to be full. An estimate, not a reading: the shelf is shared with every other player, and the
// engine's clock is the world tick, not the moment this account looked, so a real visit can find a
// unit more or less. The shop screen's own count is what is bought against.
func EstimateShelf(it Item, left int32, elapsed time.Duration, seen bool) int32 {
	if !seen {
		return it.Baseline
	}
	left = max(left, 0)
	if it.RestockTicks <= 0 || elapsed <= 0 {
		return min(left, it.Baseline)
	}
	back := int32(elapsed / (time.Duration(it.RestockTicks) * TickDuration))
	return min(it.Baseline, left+back)
}

// UnitsUnderCap is how many units of it can be bought from a shelf holding stock, one at a time,
// while each next unit costs at most maxUnit (UnitPrice walks up as the shelf drains) and the total
// stays within coins; and what they cost. maxUnit <= 0 means no cap; coins < 0 no purse limit.
func UnitsUnderCap(it Item, s Shop, stock, maxUnit, coins int32) (units, cost int32) {
	for units < stock {
		next := UnitPrice(it, s, stock-units)
		if maxUnit > 0 && next > maxUnit {
			break
		}
		if coins >= 0 && cost+next > coins {
			break
		}
		cost += next
		units++
	}
	return units, cost
}
