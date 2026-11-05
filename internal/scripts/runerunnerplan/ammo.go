package runerunnerplan

// Ammo: the arrowtips, bolts, feathers and arrow shafts a rotating runner buys on its way round the rune
// shops.
//
// a user request: "runerunner should be buying all arrowtips steel and up + feathers + arrowshafts +
// bolts from all available shops on every world while they do their shopping runs for runes." One runner
// already walks Catherby, Port Sarim, Varrock, Ardougne and Yanille on three worlds; three archery and fishing
// shops lie on that walk, and each is a shared shop with the same price rule as the rune shops, so they are
// modelled as scarabplan.RuneShop literals (Zaff, the staff shop, is the precedent for a non-rune line) and every
// price, cap, plan and board function below is the rune one, unchanged.
//
// READ FROM CONTENT 289 (scripts/**/configs/*.inv, the keepers' .npc params, maps/*.jm2, arrows.obj), not
// remembered, and re-derived with an independent script from shop.rs2 adjusted_item_cost_buying and
// calc_shop_value (a unit costs obj_cost * max(100, 1000 - clamp((stock-base)*delta, -5000, 1000)) / 1000, so
// cost * (1000 + 10*(base-stock)) / 1000 at delta 10, and never more than six times the cost):
//
//   - Hickton's Archery Emporium, Catherby (inv archeryshop2, npc hickton 575, spawn 2822,3442, sell 1000,
//     buy 500, delta 10): Bolts 200 (one back every 10 ticks), Steel arrowtips 600, Mithril 400, Adamant 200 and
//     Rune 100 (one back every 40 ticks). Thirteen tiles east of the mule's post and bank. It also stocks the
//     bronze and iron tips, arrows and bows: not wanted. Full shelves at the shop's own ceiling cost 13,840
//     (steel), 18,976 (mithril), 15,880 (adamant), 29,900 (rune) and 1,098 (bolts).
//   - Lowe's Archery Emporium, Varrock (inv archeryshop, npc lowe 550, spawn 3232,3423, same prices):
//     Bolts 1,500 (one back every 10 ticks), nothing else wanted. A full shelf is 22,995.
//   - Gerrant's Fishy Business, Port Sarim (inv fishingshop, npc gerrant 558, spawn 3013,3225, same prices):
//     Feather 1,000, one back EVERY tick, so a bought-out shelf is full again in ten minutes. A full shelf is
//     9,250.
//
// OBJ COSTS (arrows.obj, bolts.obj, fishing.obj): Steel arrowtips 6, Mithril 16, Adamant 40, Rune 200, Bolts 3,
// Feather 2, Arrow shaft 1. shopplan priced the Steel arrowtips at 1 until 13 Oct 2026; the live purchase of 237
// for 2,985 (MasterCrafter, 5 Oct, off a full shelf) is the cost-6 model's price to the coin (the cost-1 model
// says 411).
//
// THE CEILINGS. A shelf cannot go below nought, so the dearest unit a shop ever charges is PriceAt(item, 0):
// six times the cost where the shelf holds 500 or more (Bolts at Lowe 18, Steel at Hickton 36, Feather 12),
// and less where it holds fewer (Mithril 80, Adamant 120, Rune 400, Bolts at Hickton 9). Buying "all" is
// therefore bounded by the shop itself, as for runes (MaxPriceCap 1000 is above every one of them).
//
// WHAT IS WORTH BUYING (the consumers, from the live banks on 13 Oct): Steel arrowtips (516,812 Headless
// arrows wait on none; mastercraftplan makes Steel arrows only), Bolts (3,320,682 Pearl bolttips wait on 40,306
// Bolts) and, behind them, Feathers. Mithril, Adamant and Rune arrowtips have NO fletching consumer in the code
// yet (the mule already holds 39,330 idle Mithril arrowtips), and Arrow shaft is sold by no shop this runner can
// open (Dargaud needs Ranged 40; the Seers market stall holds 3,375). They are bought because users asked
// for all of them, but only behind ammoStopAbove (the mule's own stock) and the round's ammoCoins budget, and
// last in the priority order.
//
// This file is pure: no client, actions, fleet or bot imports.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// The ammo item names, as the shops stock them and the mule's bank lists them.
const (
	AmmoBolts   = "Bolts"
	AmmoSteel   = "Steel arrowtips"
	AmmoFeather = "Feather"
	AmmoMithril = "Mithril arrowtips"
	AmmoAdamant = "Adamant arrowtips"
	AmmoRune    = "Rune arrowtips"
	AmmoShaft   = "Arrow shaft"
	// The Fishing Trawler's kit (a user request: "include swamp paste buying and bailing buckets ... buy any ropes from
	// all stores on all worlds to stock up for KQ and Fishing trawler"): paste stacks, rope and buckets have notes.
	AmmoPaste  = "Swamp paste"
	AmmoRope   = "Rope"
	AmmoBucket = "Bailing bucket"
)

// The shelf parts the 2 November 2026 shop round added (owner: Aemad and Kortan's Vials of water, Rope and Bronze arrows, Lowe's
// Steel arrows): known to ammoItems and ammoStopAbove, not in their defaults (a runner lists them by hand).
const (
	AmmoVial        = "Vial of water"
	AmmoBronzeArrow = "Bronze arrow"
	AmmoIronArrow   = "Iron arrow"
	AmmoSteelArrow  = "Steel arrow"
)

// Ammo site keys, the spelling the ammoShops setting uses.
const (
	SiteHickton      = "Hickton"
	SiteLowe         = "Lowe"
	SiteGerrant      = "Gerrant"
	SiteDargaud      = "Dargaud"
	SiteLathas       = "Lathas"
	SiteGulluck      = "Gulluck"
	SiteFishingGuild = "FishingGuild"
	SitePortKhazard  = "PortKhazard"
	SiteAemad        = "Aemad"
	SiteShantay      = "Shantay"
	SiteNurmof       = "Nurmof"
	// SiteNed is Ned in Draynor, who sells Rope for 15 coins a conversation (NedShop).
	SiteNed = "Ned"
)

// AmmoItem is one ammo part and the facts that decide how it is bought.
type AmmoItem struct {
	Name string
	// Cost is the obj's cost in the cache: the price at exactly base stock.
	Cost int
	// StopAbove is the default of the ammoStopAbove setting: the item is not bought while the mule's bank
	// (and what this runner already holds for it) reaches this many. 0 is no gate.
	StopAbove int
	// Always marks an item bought even when the mule's bank was not read lately (nothing to compare with).
	Always bool
	// Consumer says what the fleet makes of the item; "" means nothing does yet.
	Consumer string
	// Unstackable marks an item that takes one pack slot per unit (Rope, Vial of water, Bailing bucket): a visit buys no
	// more of it than the pack has free slots (arrows, feathers and paste stack).
	Unstackable bool
}

// ammoItems are the parts in the default priority order: the consumed ones first, the ones nothing uses yet
// last, so a short purse or the round's budget leaves the dead stock behind.
var ammoItems = []AmmoItem{
	{Name: AmmoBolts, Cost: 3, StopAbove: 1000000, Always: true, Consumer: "MasterCrafter's Pearl bolts (bolts.rs2, 3.2 xp a bolt)"},
	{Name: AmmoSteel, Cost: 6, StopAbove: 150000, Always: true, Consumer: "MasterCrafter's Steel arrows (5 xp each)"},
	{Name: AmmoFeather, Cost: 2, StopAbove: 250000, Always: true, Consumer: "Headless arrows (1 xp each)"},
	{Name: AmmoMithril, Cost: 16, StopAbove: 20000},
	{Name: AmmoAdamant, Cost: 40, StopAbove: 20000},
	{Name: AmmoRune, Cost: 200, StopAbove: 20000},
	{Name: AmmoShaft, Cost: 1, StopAbove: 300000, Consumer: "Headless arrows"},
}

// trawlerAmmo are the Fishing Trawler's kit items: known to ammoItems and ammoStopAbove, not in their defaults (a runner
// lists them with the PortKhazard site).
var trawlerAmmo = []AmmoItem{
	{Name: AmmoPaste, Cost: 30, StopAbove: 100000, Always: true, Consumer: "the Fishing Trawler's leaks"},
	{Name: AmmoRope, Cost: 18, StopAbove: 50000, Always: true, Unstackable: true, Consumer: "the Fishing Trawler's net and the Kalphite Queen's ropes"},
	{Name: AmmoBucket, Cost: 10, StopAbove: 10000, Always: true, Unstackable: true, Consumer: "the Fishing Trawler's bailing"},
}

// shopAmmo are the parts of the shop round of 2 November 2026 (arrows.obj costs: Bronze 1, Iron 3, Steel 12; vials.obj: Vial of
// water 2). Like trawlerAmmo they are not in the defaults.
var shopAmmo = []AmmoItem{
	{Name: AmmoBronzeArrow, Cost: 1, StopAbove: 200000, Always: true, Consumer: "ScarabSwarm's endgame Ranged (1,000 worn Bronze arrows a Guild account, scarabplan.DefaultEndgameRangedGuildArrows)"},
	{Name: AmmoIronArrow, Cost: 3, StopAbove: 50000, Consumer: "the Ranged arrow ladder (scarabplan.ArrowLadder)"},
	{Name: AmmoSteelArrow, Cost: 12, StopAbove: 100000, Always: true, Consumer: "the Ranged arrow ladder (scarabplan.ArrowLadder, Ranged 5)"},
	{Name: AmmoVial, Cost: 2, StopAbove: 20000, Always: true, Unstackable: true, Consumer: "the Antipoison maker and the potion makers (vialplan, antipoisonmakeplan)"},
	// Nurmof's shelf (a site that is registered, not built): the miners' own pickaxe purchase (essencerunner.BuyPickaxeAtNurmof).
	{Name: "Bronze pickaxe", Cost: 1, StopAbove: 50, Unstackable: true},
	{Name: "Iron pickaxe", Cost: 140, StopAbove: 50, Unstackable: true},
	{Name: "Steel pickaxe", Cost: 500, StopAbove: 50, Unstackable: true},
	{Name: "Mithril pickaxe", Cost: 1300, StopAbove: 50, Unstackable: true, Consumer: "the miners and the rune essence runners (essenceplan, minemanipplan)"},
	{Name: "Adamant pickaxe", Cost: 3200, StopAbove: 50, Unstackable: true},
	{Name: "Rune pickaxe", Cost: 32000, StopAbove: 50, Unstackable: true},
}

// allAmmo is every part the registry knows, the defaults first.
func allAmmo() []AmmoItem {
	out := append([]AmmoItem(nil), ammoItems...)
	out = append(out, trawlerAmmo...)
	return append(out, shopAmmo...)
}

// AmmoItems are the ammo parts in the default priority order.
func AmmoItems() []AmmoItem { return append([]AmmoItem(nil), ammoItems...) }

// AmmoItemFor finds an ammo part by name, ignoring case.
func AmmoItemFor(name string) (AmmoItem, bool) {
	name = strings.TrimSpace(name)
	for _, it := range allAmmo() {
		if strings.EqualFold(it.Name, name) {
			return it, true
		}
	}
	return AmmoItem{}, false
}

// DefaultAmmoItems are the ammoItems setting's default, in priority order.
func DefaultAmmoItems() []string {
	out := make([]string, len(ammoItems))
	for i, it := range ammoItems {
		out[i] = it.Name
	}
	return out
}

// DefaultAmmoStopAbove is the ammoStopAbove setting's default: "Item=count" for every item that has a gate.
func DefaultAmmoStopAbove() []string {
	var out []string
	for _, it := range ammoItems {
		if it.StopAbove > 0 {
			out = append(out, it.Name+"="+strconv.Itoa(it.StopAbove))
		}
	}
	return out
}

// DefaultAmmoCapMultiple is the derived price cap as a multiple of an item's cost (the ammoBuyAll=false caps),
// the same three as DefaultCapMultiple for runes.
const DefaultAmmoCapMultiple = 3

// DerivedAmmoCap is DefaultAmmoCapMultiple times the item's cost, never above MaxPriceCap; 0 for a name that is
// not an ammo part.
func DerivedAmmoCap(item string) int {
	it, ok := AmmoItemFor(item)
	if !ok {
		return 0
	}
	c := DefaultAmmoCapMultiple * it.Cost
	if c > MaxPriceCap {
		c = MaxPriceCap
	}
	return c
}

// ---- the shops ----

// Hickton is Catherby's archery shop (see the file comment for its content facts).
var Hickton = fleetplan.RuneShop{
	Label: "Hickton's Archery Emporium (Catherby)", Keeper: "Hickton", NPCID: 575,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoBolts, Base: 200, RateTicks: 10, Cost: 3},
		{Rune: AmmoSteel, Base: 600, RateTicks: 40, Cost: 6},
		{Rune: AmmoMithril, Base: 400, RateTicks: 40, Cost: 16},
		{Rune: AmmoAdamant, Base: 200, RateTicks: 40, Cost: 40},
		{Rune: AmmoRune, Base: 100, RateTicks: 40, Cost: 200},
		// catherby.inv archeryshop2 stock2/stock3 (2 Nov 2026). Its Steel, Mithril, Adamant and Rune ARROW lines (stock4-7) have base
		// 0 and are never stocked, so they are not modelled.
		{Rune: AmmoBronzeArrow, Base: 1000, RateTicks: 10, Cost: 1},
		{Rune: AmmoIronArrow, Base: 800, RateTicks: 20, Cost: 3},
	},
}

// Lowe is Varrock's archery shop: the one bolt shop on the circuit, with the biggest shelf (1,500). Since 2 November 2026 it also
// carries its arrow lines (varrock.inv archeryshop stock1-3, owner: "Lowe: Steel arrows"): Bronze 2,000 (one back every 10 ticks),
// Iron 1,500 (15) and Steel 1,000 (every 20 ticks, cost 12, 12,000 coins a full shelf).
var Lowe = fleetplan.RuneShop{
	Label: "Lowe's Archery Emporium (Varrock)", Keeper: "Lowe", NPCID: 550,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoBolts, Base: 1500, RateTicks: 10, Cost: 3},
		{Rune: AmmoBronzeArrow, Base: 2000, RateTicks: 10, Cost: 1},
		{Rune: AmmoIronArrow, Base: 1500, RateTicks: 15, Cost: 3},
		{Rune: AmmoSteelArrow, Base: 1000, RateTicks: 20, Cost: 12},
	},
}

// Aemad is Aemad's Adventuring Supplies in East Ardougne, one shop run by two keepers (npc aemad 590 and kortan 591, both
// param=owned_shop,adventurershop, op3 Trade; aemad.rs2 opens it from the talk option). READ FROM CONTENT 289:
//
//   - area_ardougne_east/configs/ardougne_east.inv [adventurershop]: scope=shared, restock=yes; vial_water 500 (one back every 30
//     ticks), bronze_arrow 500 (10), rope 20 (100), ball_of_wool 30 (100), papyrus 50 (1000) and a few tools. Only the first three
//     are modelled: nothing else on the shelf has a consumer (the crafter spins its own wool).
//   - ardougne_east.npc [aemad], [kortan]: shop_sell_multiplier 1300, shop_buy_multiplier 400, shop_delta 20, so a unit costs
//     1.3 times its obj cost at base stock (Vial of water 2, Bronze arrow 1, Rope 23) and 20/10 percent more for every unit below it.
//   - maps/m40_51.jm2: "0 53 30: 590" and "0 55 28: 591", i.e. (2613,3294) and (2615,3292) (the origin is 2560,3264).
//   - vials.obj [vial_water] cost 2, arrows.obj [bronze_arrow] cost 1, rope (_unpack/225/all.obj) cost 18.
//
// Vials and Rope are not stackable, so a visit buys what the pack has room for (the stop follows the round's draw, so the pack
// holds coins and runes only). 500 Bronze arrows fit one slot.
var Aemad = fleetplan.RuneShop{
	Label: "Aemad's Adventuring Supplies (East Ardougne)", Keeper: "Aemad", NPCID: 590,
	SellMultiplier: 1300, Delta: 20,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoVial, Base: 500, RateTicks: 30, Cost: 2},
		{Rune: AmmoBronzeArrow, Base: 500, RateTicks: 10, Cost: 1},
		{Rune: AmmoRope, Base: 20, RateTicks: 100, Cost: 18},
	},
}

// Kortan is the second keeper of Aemad's shop; a station tries Aemad first and Kortan when Aemad is out of reach.
const Kortan = "Kortan"

// Shantay is the Shantay Pass shop (inv shantayshop, npc shantay 836 at 3304,3123 per maps/m51_48.jm2 "0 40 51: 836"; op3 Trade,
// members only, shop_sell_multiplier 1000, delta 10): 500 Feathers one back every 50 ticks, and the pass, skins and bars nobody
// here wants. Registered but not built (AmmoSiteFor says why).
var Shantay = fleetplan.RuneShop{
	Label: "Shantay Pass Shop", Keeper: "Shantay", NPCID: 836,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoFeather, Base: 500, RateTicks: 50, Cost: 2},
	},
}

// Nurmof is Nurmof's Pickaxe Shop under the Dwarven Mine (dwarven_mine.inv [pickaxeshop], npc nurmof 594 at 2998,9844 from
// maps/m46_153.jm2 "0 54 52: 594"; shop_sell_multiplier 1000, delta 20): bronze 6 (back every 50 ticks), iron 5 (100), steel 4 (200),
// mithril 3 (300), adamant 2 (500) and rune 1 (700). Costs are pickaxes.obj's (1, 140, 500, 1300, 3200, 32000). Registered but not
// built (AmmoSiteFor says why).
var Nurmof = fleetplan.RuneShop{
	Label: "Nurmof's Pickaxe Shop (Dwarven Mine)", Keeper: "Nurmof", NPCID: 594,
	SellMultiplier: 1000, Delta: 20,
	Lines: []fleetplan.StockLine{
		{Rune: "Bronze pickaxe", Base: 6, RateTicks: 50, Cost: 1},
		{Rune: "Iron pickaxe", Base: 5, RateTicks: 100, Cost: 140},
		{Rune: "Steel pickaxe", Base: 4, RateTicks: 200, Cost: 500},
		{Rune: "Mithril pickaxe", Base: 3, RateTicks: 300, Cost: 1300},
		{Rune: "Adamant pickaxe", Base: 2, RateTicks: 500, Cost: 3200},
		{Rune: "Rune pickaxe", Base: 1, RateTicks: 700, Cost: 32000},
	},
}

// Gerrant is Port Sarim's fishing shop: Feathers, one back every tick.
var Gerrant = fleetplan.RuneShop{
	Label: "Gerrant's Fishy Business (Port Sarim)", Keeper: "Gerrant", NPCID: 558,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoFeather, Base: 1000, RateTicks: 1, Cost: 2},
	},
}

// Dargaud is the Ranging Guild's bow shop, the ONLY shop in content 289 that sells Arrow shafts (inv
// ranging_guild_bowshop, npc ranging_guild_bow_salesman 683 at 2673,3434, sell 1000, buy 500, delta 10). Its door
// reads stat(ranged) >= 40 (ranging_guild_door.rs2) and no account on the roster has Ranged 20. Not built.
var Dargaud = fleetplan.RuneShop{
	Label: "Dargaud's Bow and Arrows (Ranging Guild)", Keeper: "Ranging Guild bow salesman", NPCID: 683,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoShaft, Base: 1000, RateTicks: 50, Cost: 1},
		{Rune: AmmoSteel, Base: 300, RateTicks: 30, Cost: 6},
		{Rune: AmmoMithril, Base: 200, RateTicks: 40, Cost: 16},
		{Rune: AmmoAdamant, Base: 200, RateTicks: 50, Cost: 40},
		{Rune: AmmoRune, Base: 150, RateTicks: 60, Cost: 200},
	},
}

// Lathas is the Combat Training Camp's armoury (inv lathastrainingstore, npc 561 at 2514,3385; sell 1500).
// The camp gate opens only after Biohazard (combat_training_camp.rs2). Not built.
var Lathas = fleetplan.RuneShop{
	Label: "Armoury (Combat Training Camp)", Keeper: "Shop keeper", NPCID: 561,
	SellMultiplier: 1500, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoBolts, Base: 200, RateTicks: 10, Cost: 3},
		{Rune: AmmoSteel, Base: 800, RateTicks: 40, Cost: 6},
		{Rune: AmmoMithril, Base: 800, RateTicks: 80, Cost: 16},
	},
}

// Gulluck is Gulluck and Sons in the Gnome Stronghold, on level 2 of the Grand Tree (inv gnomeshop_gulluck,
// npc 602 at 2468,3487): the webwalk graph has no route to that level. Not built.
var Gulluck = fleetplan.RuneShop{
	Label: "Gulluck and Sons (Gnome Stronghold)", Keeper: "Gulluck", NPCID: 602,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoBolts, Base: 150, RateTicks: 10, Cost: 3},
		{Rune: AmmoSteel, Base: 160, RateTicks: 40, Cost: 6},
		{Rune: AmmoMithril, Base: 140, RateTicks: 80, Cost: 16},
	},
}

// FishingGuildShop is the Fishing Guild's shop (inv fishingguild, npc fishguildshop "Roachey" at 2596,3400):
// 1,500 Feathers, one back every tick, 15,250 a full shelf, behind a Fishing 68 door 223 tiles from the
// Catherby bank. Dropped as a circuit stop (3.3 feathers a tile walked against Gerrant's 15, and the Seers
// stall holds 507,747): kept as a registry entry so the setting can name it and the round can say why not.
var FishingGuildShop = fleetplan.RuneShop{
	Label: "Fishing Guild shop", Keeper: "Roachey", NPCID: 592,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoFeather, Base: 1500, RateTicks: 1, Cost: 2},
	},
}

// PortKhazard is the Port Khazard general store (inv khazardshop, "Shop keeper" npc 555 at 2641,3171; shared, so each world's
// shelf is its own): the only seller of Swamp paste, and Rope and Bailing buckets beside it. READ FROM CONTENT 289:
//
//   - area_ardougne_east/configs/port_khazard/port_khazard.inv [khazardshop]: scope=shared, restock=yes, stock9=rope,30,20,
//     stock11=bucket_bailing,30,100, stock12=swamppaste,500,20 -- so the Rope shelf is 30 and one comes back every 20 ticks (12 s):
//     five a minute (12 s each), a bought-out shelf full again in six minutes, 300 an hour a world.
//   - port_khazard.npc [khazard_shopkeeper]: shop_sell_multiplier 1400, shop_buy_multiplier 400, shop_delta 10 (this literal said
//     1300 and 30 -- Aemad's pair, a guess -- until 4 Nov 2026). A unit costs obj cost * (1400 + 10 per unit below base) / 1000, so
//     a Rope (cost 18, _unpack/225/all.obj) is 25 at the full shelf and 30 at the last of 30; the whole shelf is about 800 coins.
//   - Both keepers (npc 555 at 2641,3171 and 2655,3152) are the one npc type, so they open the one shelf (scarabplan.PortKhazardResolved).
//
// The Fishing Trawler crew buys its paste, ropes and buckets from the same shelf on the game world, so the runner takes only the
// Rope there (khazardRopeOnly) and never on the world and in the minutes the crew is shopping (RopeCrewBlockWait).
var PortKhazard = fleetplan.RuneShop{
	Label: "Port Khazard general store", Keeper: "Shop keeper", NPCID: 555,
	SellMultiplier: 1400, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoPaste, Base: 500, RateTicks: 20, Cost: 30},
		{Rune: AmmoRope, Base: 30, RateTicks: 20, Cost: 18},
		{Rune: AmmoBucket, Base: 30, RateTicks: 100, Cost: 10},
	},
}

// NedRopeCost is what Ned charges a rope (ned.rs2 [label,ned_rope] takes 15 coins and adds one rope, every conversation; he has no
// shelf to run out, and four Balls of wool make a rope instead: [label,ned_have_wool]). scarabplan.NedRopeCost is the same figure.
const NedRopeCost = 15

// NedShelf is the most Rope a Ned stop is planned for: a shelf that never runs out has no base stock, so this is the pack's room
// (AmmoPackRoom), which is what a visit can hold before it banks.
const NedShelf = AmmoPackRoom

// NedShop is Ned (Draynor, npc 918 at 3100,3258 from maps/m48_50.jm2 "0 28 58: 918", scarabplan/nedrope.go) modelled as a shop
// with a constant price (delta 0) and a shelf the pack can always take, so PlanAmmoVisit, the purse estimate and the price check
// treat him as they do any shelf. The Rope line keeps the obj's own cost (18); the multiplier 834 makes a unit 834*18/1000 = 15
// coins at any stock (a test pins it). The purchase itself is a conversation (scarabrunner.BuyRopesFromNed), not a trade screen.
var NedShop = fleetplan.RuneShop{
	Label: "Ned (Draynor)", Keeper: "Ned", NPCID: 918,
	SellMultiplier: 834, Delta: 0,
	Lines: []fleetplan.StockLine{
		{Rune: AmmoRope, Base: NedShelf, RateTicks: 1, Cost: 18},
	},
}

// NedStand is Ned's own tile (fleetplan.NedStand: radius 1, he is inside his house).
var NedStand = fleetplan.NedStand

// KhazardMinVisitCoins is the smallest purchase Port Khazard's stop is made for: a bought rope is 25-30 coins, so a shelf of the
// 10 ropes this is worth is already a trawler game's mends; the stop is on the walk the Lundail kit makes.
const KhazardMinVisitCoins = 250

// Stands (the tile a station walks to; the keeper wanders inside his shop and is found by name there).
var (
	// HicktonStand is MasterCrafter's proven stand at Hickton (keeper spawn 2822,3442).
	HicktonStand = tile.Tile{X: 2822, Z: 3441}
	// LoweStand is MasterCrafter's stand at Lowe (keeper spawn 3232,3423). Never reached live: all five 4-5 October
	// attempts ended on a ~600-tile walk from Catherby that RuneRunner never makes.
	LoweStand = tile.Tile{X: 3231, Z: 3421}
	// GerrantStand is Gerrant's spawn tile (m47_50 "0 5 25: 558").
	GerrantStand = tile.Tile{X: 3013, Z: 3225}
	// DargaudStand, FishingGuildStand are for the registry only.
	DargaudStand = tile.Tile{X: 2673, Z: 3434}
	// PortKhazardStand is the store keeper's spawn (trawlerplan.Stores[0]).
	PortKhazardStand  = tile.Tile{X: 2641, Z: 3171}
	FishingGuildStand = tile.Tile{X: 2596, Z: 3400}
	// AemadStand is the tile the Antipoison maker walks to for the same shop (antipoisonmakeplan.AemadStand, bought from live on
	// 1 November): between Aemad (2613,3294) and Kortan (2615,3292).
	AemadStand = tile.Tile{X: 2614, Z: 3293}
	// ShantayStand is the keeper's spawn (scarabplan.ShantayStand, where Stage 6 stages); NurmofStand is his, underground
	// (essenceplan.NurmofStand).
	ShantayStand = tile.Tile{X: 3304, Z: 3123}
	NurmofStand  = tile.Tile{X: 2997, Z: 9844}
)

// AmmoSite is one ammo shop and how the buyer reaches it.
type AmmoSite struct {
	Key  string
	Shop fleetplan.RuneShop
	// Stand is the tile a station walks to first.
	Stand tile.Tile
	// Bank and BankName: the nearest bank (the account's bank is the same everywhere).
	Bank     tile.Tile
	BankName string
	// Anchor is the rune site whose pairs the station joins in the ring ("" for Hickton, which joins the
	// Catherby ends). A site whose anchor is not in the round is not placed.
	Anchor string
	// AltKeeper is the shop's second keeper, tried when Keeper is out of reach ("" for none).
	AltKeeper string
	// MinVisitCoins, when set, replaces the ammoMinVisitCoins setting where that is higher: a stop the round passes anyway (Aemad's,
	// beside the Lundail kit's bank walk) is worth a smaller purchase than one that needs a walk of its own. 0 takes the setting.
	MinVisitCoins int
	// BankAfter says the pack is banked at Bank after a visit that bought unstackable parts (Rope): a full pack of single items
	// would ride on into the next stop and crowd out its purchase (Khazard's 24 ropes into Aemad's shelf, Ned's into Aubury's rune
	// stacks). Coins stay in the pack (RuneRunner.bankPack).
	BankAfter bool
	// CrewShelf marks a shop whose shelf is the Fishing Trawler crew's own (Port Khazard's paste, ropes and buckets): with the
	// khazardRopeOnly setting the runner buys only the Rope there and leaves the crew's paste and buckets.
	CrewShelf bool
	// Dialogue marks a seller that is not a shop (Ned): his stop is a conversation, not a trade screen.
	Dialogue bool
	// Built is false for a site this package does not put on the circuit; Gate says why.
	Built bool
	Gate  string
	// MinRanged is the Ranged level the site's door needs (0 for none): the Ranging Guild's. A site above the account's level is
	// left out of the round (RangingGuildSites), exactly as the Wizards' Guild is below Magic 66.
	MinRanged int
}

// AmmoSiteFor is the registry of the ammo shops RuneRunner knows.
func AmmoSiteFor(key string) (AmmoSite, bool) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case strings.ToLower(SiteHickton):
		return AmmoSite{Key: SiteHickton, Shop: Hickton, Stand: HicktonStand, Bank: CatherbyBank, BankName: "Catherby", Built: true}, true
	case strings.ToLower(SiteLowe):
		return AmmoSite{Key: SiteLowe, Shop: Lowe, Stand: LoweStand, Bank: VarrockEastBank, BankName: "Varrock East", Anchor: SiteAubury, Built: true}, true
	case strings.ToLower(SiteGerrant):
		return AmmoSite{Key: SiteGerrant, Shop: Gerrant, Stand: GerrantStand, Bank: DraynorBank, BankName: "Draynor", Anchor: SiteBetty, Built: true}, true
	case strings.ToLower(SiteDargaud):
		// Built since 5 Nov 2026 (owner: "get acct 40 ranged for the ranged guild shop asap ... rangeguild shop unlocked asap for more
		// arrowtip purchases per hour"): placed after each Hickton stop once the account's Ranged reaches the door's 40
		// (RangingGuildSites). Its route through the guild door is not live-proven.
		return AmmoSite{Key: SiteDargaud, Shop: Dargaud, Stand: DargaudStand, Bank: CatherbyBank, BankName: "Catherby", MinRanged: RangingGuildRanged,
			MinVisitCoins: DargaudMinVisitCoins, Built: true}, true
	case strings.ToLower(SiteLathas):
		return AmmoSite{Key: SiteLathas, Shop: Lathas, Stand: tile.Tile{X: 2514, Z: 3385}, Bank: ArdougneBank, BankName: "East Ardougne",
			Gate: "the Combat Training Camp gate opens only after Biohazard"}, true
	case strings.ToLower(SiteGulluck):
		return AmmoSite{Key: SiteGulluck, Shop: Gulluck, Stand: tile.Tile{X: 2468, Z: 3487, Level: 2}, Bank: CatherbyBank, BankName: "Catherby",
			Gate: "Gulluck is on level 2 of the Grand Tree and the webwalk graph has no route there"}, true
	case strings.ToLower(SitePortKhazard):
		// Placed with Lundail's pairs, BEFORE Aemad's on each world (rotation.go): the next stop is East Ardougne, so the ropes are
		// banked there (BankAfter) and Aemad's shelf finds an empty pack.
		return AmmoSite{Key: SitePortKhazard, Shop: PortKhazard, Stand: PortKhazardStand, Bank: ArdougneBank, BankName: "East Ardougne", Anchor: SiteLundail,
			MinVisitCoins: KhazardMinVisitCoins, BankAfter: true, CrewShelf: true, Built: true}, true
	case strings.ToLower(SiteNed):
		// Placed after each Betty pair (rotation.go): Draynor lies on the walk east from Port Sarim, and its bank is Betty's.
		return AmmoSite{Key: SiteNed, Shop: NedShop, Stand: NedStand, Bank: DraynorBank, BankName: "Draynor", Anchor: SiteBetty,
			MinVisitCoins: 1, BankAfter: true, Dialogue: true, Built: true}, true
	case strings.ToLower(SiteAemad):
		// Placed with Lundail's pairs, BEFORE each one: the Lundail kit banks everything in East Ardougne before the lever, so the
		// pack's coins are spent here first and never ride into the Wilderness (owner: "banking coins before any Wilderness leg").
		return AmmoSite{Key: SiteAemad, Shop: Aemad, Stand: AemadStand, Bank: ArdougneBank, BankName: "East Ardougne", Anchor: SiteLundail,
			AltKeeper: Kortan, MinVisitCoins: AemadMinVisitCoins, Built: true}, true
	case strings.ToLower(SiteShantay):
		return AmmoSite{Key: SiteShantay, Shop: Shantay, Stand: ShantayStand, Bank: tile.Tile{X: 3270, Z: 3167}, BankName: "Al Kharid",
			Gate: "dropped: 280 tiles each way from Varrock or Port Sarim past the Al Kharid toll gate for 500 feathers a world, and a hop block of its own on every world; Gerrant sells 1,000 a tick-regrown feather beside Betty"}, true
	case strings.ToLower(SiteNurmof):
		return AmmoSite{Key: SiteNurmof, Shop: Nurmof, Stand: NurmofStand, Bank: CatherbyBank, BankName: "Catherby",
			Gate: "Nurmof is under the Dwarven Mine (z 9844): the trapdoor at 3019,3449 down and the ladder at 3019,9850 back are essencerunner.BuyPickaxeAtNurmof's, and an ammo visit only walks"}, true
	case strings.ToLower(SiteFishingGuild):
		return AmmoSite{Key: SiteFishingGuild, Shop: FishingGuildShop, Stand: FishingGuildStand, Bank: CatherbyBank, BankName: "Catherby",
			Gate: "dropped: a 450-tile round trip behind a Fishing 68 door for 3.3 feathers a tile walked; the Seers market stall holds the bulk"}, true
	}
	return AmmoSite{}, false
}

// AemadMinVisitCoins is the smallest purchase Aemad's stop is made for: a whole shelf is only about 2,900 coins (24 vials, 500 Bronze
// arrows, 20 Ropes), under the ammoMinVisitCoins default, and the stop is 42 tiles from the bank the Lundail kit walks to anyway.
const AemadMinVisitCoins = 500

// MinVisit is the coins a stop here must be worth: the ammoMinVisitCoins setting, or the site's own smaller figure.
func (s AmmoSite) MinVisit(setting int) int {
	if s.MinVisitCoins > 0 && s.MinVisitCoins < setting {
		return s.MinVisitCoins
	}
	return setting
}

// AmmoSiteKeys are every key the ammoShops setting accepts, built ones first.
func AmmoSiteKeys() []string {
	return []string{SiteGerrant, SiteLowe, SiteHickton, SitePortKhazard, SiteAemad, SiteNed, SiteDargaud, SiteLathas, SiteGulluck, SiteFishingGuild, SiteShantay, SiteNurmof}
}

// ---- Rope for the Fishing Trawler's net (trawlerRope, 4 November 2026) ----
//
// The trawler crew needs about 150-200 Rope a two-hour block (a mend spends one; a good game mends 30-45 times) and the mule held 37.
// The measured sources, per world, tick 0.6 s (the .inv files under content/scripts/areas, `stockN=rope,<shelf>,<ticks per unit>`):
//
//	Port Khazard general store  shelf 30, one per 20 ticks   300 an hour, 600 a two-hour rest   25-30 coins each
//	Aemad's, East Ardougne      shelf 20, one per 100 ticks   60 an hour, 120 a rest             23-30 each
//	Arhein's, Catherby          shelf  2, one per 300 ticks   20 an hour,  40 a rest             (not on the circuit: ~6 a round)
//	Rellekka (two), Karamja, Shilo   shelf 10, one per 200 ticks, 30 an hour each             (islands and the far north: not reachable)
//	West Ardougne (Underground Pass) shelf 3, one per 100 ticks                                  (behind a quest)
//	Ned, Draynor                no shelf: 15 coins a conversation, or four Balls of wool       unlimited (about 7 s a rope)
//
// A shelf only regrows to its base, so a visit takes at most the shelf (Khazard 30, Aemad 20), and the pack takes at most 24 single
// Ropes: a world's visit to both is about 44 and a three-world round about 130 -- which is why the mule held 37, the more so as the
// delivery drew the Ropes as single items (one pack's worth a round, WithdrawNoted). Ned has no shelf to wait on, so the floor
// (ropeFloor) is his: he buys only the part of the mule's stock the shelves leave short.

// NedMinBuy is the fewest Ropes a Ned stop is made for: below it the walk costs more than it brings.
const NedMinBuy = 6

// NedRopesWanted is how many Ropes a Ned stop buys: the floor less what the mule's published bank (when it was read) and this
// runner already hold, at most the pack's room (free) and NedShelf, and none under NedMinBuy. why says what decided it.
func NedRopesWanted(floor, mule int, muleKnown bool, held, free int) (n int, why string) {
	switch {
	case floor <= 0:
		return 0, "ropeFloor is 0: Ned is off"
	case free <= 0:
		return 0, "no free pack slot beyond the reserve for single Ropes"
	}
	if !muleKnown {
		mule = 0
	}
	n = floor - mule - held
	if n <= 0 {
		return 0, fmt.Sprintf("the mule holds %d and this runner %d: the floor %d is met", mule, held, floor)
	}
	n = min(n, free, NedShelf)
	if n < NedMinBuy {
		return 0, fmt.Sprintf("%d Ropes short of the floor %d is under %d: not worth the walk", n, floor, NedMinBuy)
	}
	return n, fmt.Sprintf("the mule holds %d and this runner %d, the floor is %d", mule, held, floor)
}

// AmmoBankAfterMin is the fewest unstackable parts a BankAfter site's visit must have bought for the pack to be banked.
const AmmoBankAfterMin = 6

// BankAfterWanted reports whether a visit that bought `bought` leaves a pack worth banking at the site's bank.
func BankAfterWanted(site AmmoSite, bought map[string]int) bool {
	if !site.BankAfter {
		return false
	}
	n := 0
	for item, c := range bought {
		if it, ok := AmmoItemFor(item); ok && it.Unstackable {
			n += c
		}
	}
	return n >= AmmoBankAfterMin
}

// RopeOnly is items less everything but Rope: the Fishing Trawler crew's shelf (CrewShelf) keeps its paste and buckets for the crew.
func RopeOnly(items []string) []string {
	var out []string
	for _, it := range items {
		if strings.EqualFold(it, AmmoRope) {
			out = append(out, it)
		}
	}
	return out
}

// TrawlerRopeStopAbove is the Rope count from which the mule's stock gates every Rope shelf when the setting is on and the operator
// wrote no ammoStopAbove entry for Rope: far more than the crew spends, so a shelf is never left behind for want of a gate.
const TrawlerRopeStopAbove = 1000

// TrawlerRopeSites are the ammo shops the trawlerRope setting adds to the listed ones: Port Khazard and Aemad (placed with the
// Lundail pairs) and Ned (with the Betty pairs, when the floor is above 0).
func TrawlerRopeSites(floor int) []string {
	out := []string{SitePortKhazard, SiteAemad}
	if floor > 0 {
		out = append(out, SiteNed)
	}
	return out
}

// BuiltAmmoSites are the ammo shops on the circuit, in the order the ammoShops default lists them.
func BuiltAmmoSites() []string { return []string{SiteGerrant, SiteLowe, SiteHickton} }

// DefaultAmmoShops are the ammoShops setting's default: the built ones (PortKhazard is built but listed by hand, with the
// trawler items).
func DefaultAmmoShops() []string { return BuiltAmmoSites() }

// SplitAmmoSites separates listed ammo shop keys into those the ring places and "Key: why" for those it
// leaves out (an unbuilt site, or a built one whose anchor shop is not in the round).
func SplitAmmoSites(listed, roundShops []string) (placed, skipped []string) {
	for _, k := range listed {
		site, ok := AmmoSiteFor(k)
		switch {
		case !ok:
			skipped = append(skipped, k+": unknown")
		case !site.Built:
			skipped = append(skipped, site.Key+": "+site.Gate)
		case site.Anchor != "" && !enabled(roundShops, site.Anchor):
			skipped = append(skipped, fmt.Sprintf("%s: it is placed with %s, which is not in this round", site.Key, site.Anchor))
		default:
			placed = append(placed, site.Key)
		}
	}
	return placed, skipped
}

// AmmoCeiling is the dearest one unit of an item ever costs at the site: the price with the shelf empty.
// 0 for an item the shop does not sell.
func (s AmmoSite) AmmoCeiling(item string) int { return s.Shop.PriceAt(item, 0) }

// ---- the settings' lists ----

// AmmoShopKeys is the ammoShops setting in canonical spelling, checked: known, no duplicates. Unbuilt sites are
// accepted (SplitAmmoSites says why each is not placed).
func (s Settings) AmmoShopKeys() ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, name := range s.AmmoShops {
		site, ok := AmmoSiteFor(name)
		if !ok {
			return nil, fmt.Errorf("%q is not an ammo shop RuneRunner knows (%s)", name, strings.Join(AmmoSiteKeys(), ", "))
		}
		if seen[site.Key] {
			return nil, fmt.Errorf("%s is listed twice", site.Key)
		}
		seen[site.Key] = true
		out = append(out, site.Key)
	}
	return out, nil
}

// AmmoItemNames is the ammoItems setting in canonical spelling and priority order, checked: known, no
// duplicates.
func (s Settings) AmmoItemNames() ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, name := range s.AmmoItems {
		it, ok := AmmoItemFor(name)
		if !ok {
			return nil, fmt.Errorf("%q is not an ammo part RuneRunner knows (%s)", name, strings.Join(DefaultAmmoItems(), ", "))
		}
		if seen[it.Name] {
			return nil, fmt.Errorf("%s is listed twice", it.Name)
		}
		seen[it.Name] = true
		out = append(out, it.Name)
	}
	return out, nil
}

// parseAmmoPairs reads "Item=n" entries for listed ammo items: each name known and listed, no duplicates,
// each value a whole number in [min, max].
func parseAmmoPairs(setting string, entries, items []string, min, max int) (map[string]int, error) {
	listed := map[string]bool{}
	for _, it := range items {
		listed[it] = true
	}
	out := map[string]int{}
	for _, e := range entries {
		eq := strings.LastIndex(e, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("%q is not Item=number", e)
		}
		item, ok := AmmoItemFor(e[:eq])
		if !ok {
			return nil, fmt.Errorf("%q: %q is not an ammo part (%s)", e, strings.TrimSpace(e[:eq]), strings.Join(DefaultAmmoItems(), ", "))
		}
		n, err := strconv.Atoi(strings.TrimSpace(e[eq+1:]))
		if err != nil {
			return nil, fmt.Errorf("%q: %q is not a whole number", e, strings.TrimSpace(e[eq+1:]))
		}
		if n < min || n > max {
			return nil, fmt.Errorf("%q: %s must be %d-%d", e, setting, min, max)
		}
		if !listed[item.Name] {
			return nil, fmt.Errorf("%q names %s, which is not in ammoItems", e, item.Name)
		}
		if _, dup := out[item.Name]; dup {
			return nil, fmt.Errorf("%s is listed twice", item.Name)
		}
		out[item.Name] = n
	}
	return out, nil
}

// ParseAmmoCaps reads the ammoPriceCaps entries ("Item=gp", 1..MaxPriceCap) against the listed items. Unlike
// the rune caps there need not be one per item: an item without an entry takes AmmoCaps' default.
func ParseAmmoCaps(entries, items []string) (map[string]int, error) {
	return parseAmmoPairs("the cap", entries, items, 1, MaxPriceCap)
}

// MaxAmmoStopAbove is the largest ammoStopAbove value.
const MaxAmmoStopAbove = 100000000

// ParseAmmoStop reads the ammoStopAbove entries ("Item=count", 0 = no gate) against the listed items.
func ParseAmmoStop(entries, items []string) (map[string]int, error) {
	return parseAmmoPairs("the count", entries, items, 0, MaxAmmoStopAbove)
}

// AmmoCaps is the cap table for the listed items: a written cap, else MaxPriceCap with buyAll (the shop's own
// ceiling is the only limit, as BuyAllCaps does for runes), else the derived three-times cap.
func AmmoCaps(items []string, written map[string]int, buyAll bool) Caps {
	out := make(Caps, len(items))
	for _, it := range items {
		switch {
		case written[it] > 0:
			out[it] = written[it]
		case buyAll:
			out[it] = MaxPriceCap
		default:
			out[it] = DerivedAmmoCap(it)
		}
	}
	return out
}

// ---- the mule-stock gate ----

// AmmoSkip is an item left out and why.
type AmmoSkip struct {
	Item   string
	Reason string
}

// MuleStock reads the mule's published bank: the count of an item, and false when none was published lately.
type MuleStock func(item string) (count int, known bool)

// AmmoWanted filters the listed items (priority order) by ammoStopAbove: an item is not bought while the
// mule's bank, plus what this runner already carries or has banked for it (pending), reaches its threshold.
// With the mule's bank unread, only the items marked Always are bought (the consumed ones): nothing is known
// about the rest.
func AmmoWanted(items []string, stop map[string]int, mule MuleStock, pending func(item string) int) (want []string, skipped []AmmoSkip) {
	for _, name := range items {
		it, _ := AmmoItemFor(name)
		limit := stop[name]
		held, known := 0, false
		if mule != nil {
			held, known = mule(name)
		}
		if pending != nil {
			held += pending(name)
		}
		switch {
		case !known && !it.Always:
			skipped = append(skipped, AmmoSkip{name, "the mule's bank was not read lately and nothing consumes it yet"})
		case limit > 0 && known && held >= limit:
			skipped = append(skipped, AmmoSkip{name, fmt.Sprintf("the mule holds %d (ammoStopAbove %d)", held, limit)})
		case limit > 0 && !known && held >= limit:
			skipped = append(skipped, AmmoSkip{name, fmt.Sprintf("%d held for delivery (ammoStopAbove %d)", held, limit)})
		default:
			want = append(want, name)
		}
	}
	return want, skipped
}

// ---- one visit's purchase ----

// AmmoVisitInput is one visit's facts for PlanAmmoVisit.
type AmmoVisitInput struct {
	Shop fleetplan.RuneShop
	// Items are the wanted items in priority order (AmmoWanted's answer).
	Items []string
	// Seen is the shelf: the real stock read off the open shop, or an estimate when planning ahead.
	Seen map[string]int
	Caps Caps
	// Spendable is the pack's coins less the fare and what the runes still ahead are held back for.
	Spendable int
	// Limit is the most this visit may spend (ammoCoins less the round's spend so far); 0 is no limit.
	Limit int
	// FreeSlots is the pack's empty slots less ammoSlotReserve; Stacked is the items already in the pack,
	// which need no new slot. A negative FreeSlots is no limit.
	FreeSlots int
	Stacked   map[string]bool
}

// AmmoVisitPlan is PlanAmmoVisit's answer: the purchase and what each skipped line was skipped for.
type AmmoVisitPlan struct {
	fleetplan.PurchasePlan
	Skips []AmmoSkip
}

// PlanAmmoVisit walks the wanted items in priority order and buys, from each line the shop stocks, what the
// shelf holds under the item's cap within what is left of the purse and of the visit's limit, starting a new
// pack stack only while a slot is free. The plan's Coins is exactly the sum of its lines and never more than
// the spendable coins or the limit; an item the shop does not sell is dropped without a word.
func PlanAmmoVisit(in AmmoVisitInput) AmmoVisitPlan {
	var p AmmoVisitPlan
	left := in.Spendable
	if in.Limit > 0 && in.Limit < left {
		left = in.Limit
	}
	slots := in.FreeSlots
	for _, name := range in.Items {
		l, ok := in.Shop.Line(name)
		if !ok {
			continue
		}
		stock, _ := countOf(in.Seen, name)
		capGP := in.Caps.For(l.Rune)
		part, _ := AmmoItemFor(l.Rune)
		// A stackable part needs a slot only for a new stack; an unstackable one a slot for every unit.
		newStack := part.Unstackable || !in.Stacked[l.Rune]
		want := stock
		if part.Unstackable {
			want = min(stock, AmmoPackRoom)
			if slots >= 0 {
				want = min(stock, slots)
			}
		}
		switch {
		case stock <= 0:
			p.Skips = append(p.Skips, AmmoSkip{l.Rune, "the shelf is empty"})
			continue
		case capGP <= 0:
			p.Skips = append(p.Skips, AmmoSkip{l.Rune, "no price cap for it"})
			continue
		case left <= 0:
			p.Skips = append(p.Skips, AmmoSkip{l.Rune, "no coins left to spend on ammo"})
			continue
		case newStack && slots == 0:
			p.Skips = append(p.Skips, AmmoSkip{l.Rune, "no free pack slot for a new stack"})
			continue
		}
		n, cost := in.Shop.AffordableCapped(l.Rune, stock, left, want, capGP)
		if n <= 0 {
			why := fmt.Sprintf("price %d over the cap %d", in.Shop.PriceAt(l.Rune, stock), capGP)
			if price := in.Shop.PriceAt(l.Rune, stock); price <= capGP {
				why = fmt.Sprintf("coins: %d left, the next unit costs %d", left, price)
			}
			p.Skips = append(p.Skips, AmmoSkip{l.Rune, why})
			continue
		}
		p.Lines = append(p.Lines, fleetplan.PlannedBuy{Rune: l.Rune, Count: n, Cost: cost, AssumedStock: stock})
		p.Coins += cost
		left -= cost
		switch {
		case part.Unstackable && slots > 0:
			slots -= n
		case newStack && slots > 0:
			slots--
		}
	}
	return p
}

// AmmoPackRoom is how many units of an unstackable part a visit is planned for when the pack's free slots are not
// known (the estimate a round's purse is sized from): a pack's worth, less the coin and rune stacks.
const AmmoPackRoom = 24

// AmmoStationCost is what one ammo station is expected to cost: PlanAmmoVisit over the estimated shelf with
// the purse and the slots unbounded, so the coins an east leg or a round should carry for it.
func AmmoStationCost(sh fleetplan.RuneShop, est map[string]int, items []string, caps Caps, limit int) int {
	return PlanAmmoVisit(AmmoVisitInput{Shop: sh, Items: items, Seen: est, Caps: caps, Spendable: math.MaxInt32, Limit: limit, FreeSlots: -1}).Coins
}

// AmmoWorthVisiting is the skip rule for one ammo station, checked just before it starts: a stop is walked to
// only for at least minVisitCoins of planned purchases (a value in coins, not units: feathers are cheap).
func AmmoWorthVisiting(estCoins, minVisitCoins int) (bool, string) {
	if estCoins < minVisitCoins {
		return false, fmt.Sprintf("est %d coins of ammo under ammoMinVisitCoins %d", estCoins, minVisitCoins)
	}
	return true, ""
}

// ---- the round's budget ----

// AmmoSpentThisRound sums what the round's finished ammo stations spent.
func AmmoSpentThisRound(ring []Station, done map[string]StationResult) int {
	total := 0
	for _, s := range ring {
		if s.Kind != KindAmmo {
			continue
		}
		total += done[s.Key()].Spent
	}
	return total
}

// AmmoLeft is the ammo budget left this round: limit (the ammoCoins setting, 0 = none) less the spend so
// far. ok is false when the budget is spent. With no limit it returns (0, true): PlanAmmoVisit reads a Limit
// of 0 as unbounded.
func AmmoLeft(limit, spent int) (left int, ok bool) {
	if limit <= 0 {
		return 0, true
	}
	if spent >= limit {
		return 0, false
	}
	return limit - spent, true
}

// ---- the coins: ammo is paid after the runes, but not instead of them ----
//
// Ammo is the ring's lowest priority, and until 14 October 2026 the purse was SHORT (one round spends about
// 950,000, the mule takes in about 774,000 an hour, and the runner asked for 300,000). So the ammo estimate
// (RoundNeed.Ammo) is kept out of PackNeed, Est and Total, which size the shop-bank top-ups, the market
// station's float and Lundail's keep, and reaches the pack in exactly two ways, both from coins the runes do not
// need:
//
//   - the coin draw adds AmmoDrawShare, the part of the estimate that the coins at hand hold beyond the whole
//     pack-paid rune need; and
//   - an ammo station spends the pack's coins beyond the fare and HoldAhead: the pack-paid rune and staff need of
//     the stations still ahead (RoundNeed.PackNeed, margin included) LESS what the bank can add for them. That
//     hold is NOT capped to half the purse as a rune visit's reserve is: a cap there is an order of preference,
//     and for ammo it would let a short purse buy ammo with half of what the runes are short of.
//
// The bank's share of the hold is the 14 October change (owner: "spend it on everything instead of just running
// out"). LIVE that day the pack held 824,379, the bank 764,906 and the runes ahead needed 949,886, and all three
// ammo stops skipped "nothing to spend on ammo" although the next rune station tops its pack up from the nearest
// bank. The pack is not the purse: the bank is drawn from at every shop. So a bank that holds the runes ahead
// (above its reserve and Lundail's keep) holds nothing back, and one that covers only part of them holds back the
// rest. The mule ask is sized to the full bill (RoundNeed.Bill) and takes the mule's whole stack, so that bank
// is normally full.
//
// A round that has already left runes on a shelf, or passed a station, for coins (AmmoWaitsForRunes) buys no more
// ammo that round, which also covers the trailing Hickton stop that has no rune station ahead of it: that is the
// "truly short" case, after the whole mule stack was taken.

// AmmoDrawShare is the part of the ammo estimate a coin draw adds to the rune need: what the coins available to
// the pack (the pack's own and the bank's spare above its reserve and Lundail's keep) leave once the whole rune
// need and the fare are covered, never more than the estimate. A purse short of the rune need gets none, so the
// draw is then exactly what it is without ammo. tripCoins and the bank's spare still bound the draw itself
// (DrawAmount).
func AmmoDrawShare(ammo, runeNeed, inPack, fare, bankSpare int) int {
	if ammo <= 0 {
		return 0
	}
	surplus := inPack + max(bankSpare, 0) - fare - max(runeNeed, 0)
	if surplus <= 0 {
		return 0
	}
	return min(ammo, surplus)
}

// HoldAhead is the coins an ammo stop (or Zaff's) leaves in the pack for the rune and staff stations still ahead:
// aheadNeed (RoundNeed.PackNeed of those stations) less what the bank can add above its reserve and Lundail's
// keep (bankSpare), never below 0. The stations ahead draw from the bank at their own shops, so a bank that holds
// their need leaves the pack free for ammo. A bank never read (bankRead false) adds nothing: the whole need is held,
// as before.
func HoldAhead(aheadNeed int, bankRead bool, bankSpare int) int {
	hold := max(aheadNeed, 0)
	if !bankRead {
		return hold
	}
	return max(hold-max(bankSpare, 0), 0)
}

// AmmoWaitsForRunes reports that the round has already left runes on a shelf for lack of coins, or passed a
// station for lack of them (State.RoundShort): the round is short, and the lowest priority buys nothing more
// until a round fits its purse.
func AmmoWaitsForRunes(st State) (bool, string) {
	stations, runes, coins := st.RoundShort()
	if runes <= 0 && len(stations) == 0 {
		return false, ""
	}
	return true, fmt.Sprintf("this round is short of coins (%d runes worth %d left on shelves, %d stations passed for coins): the runes come first", runes, coins, len(stations))
}

// IsAmmoStation reports whether a station buys ammo.
func IsAmmoStation(s Station) bool { return s.Kind == KindAmmo }

// AmmoBoughtLine is the ammo part of a bought map, sorted, for a log line: "Bolts=2380,Feather=3000".
func AmmoBoughtLine(bought map[string]int) string {
	var keys []string
	for k, n := range bought {
		if _, ok := AmmoItemFor(k); ok && n > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + strconv.Itoa(bought[k])
	}
	return strings.Join(parts, ",")
}
