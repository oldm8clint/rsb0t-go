package loot

import (
	"sort"
	"strconv"
	"strings"
)

// SHOP PURCHASES, on one card of their own (a user request: "we want a loot panel for all things
// bought in shops, 1 combined panel in the loot tracker").
//
// Every script that buys from a shop (RuneRunner's rune shops, ScarabSwarm's rune supply and gear
// buys, the fishers' bait and nets, MasterCrafter's chisels and moulds, the quest ladder's provisions,
// ShopBuyout) goes through the same shop window, so the purchase is measured there and no script
// reports one: internal/bot's shop watch (lootshop.go) reads the pack the shop window shows and pairs
// COINS LEAVING it with ITEMS ARRIVING in it. A sale is the other way round (items leave, coins
// arrive) and is not a purchase.
//
// The card is KindShop with Skill "Shop purchases", one row per item bought. Each row carries what
// arrived as Drop.Items (priced at the market like any loot) and the coins paid as Drop.Spent (priced
// at one gp each, lootcost.go), so a row's net is what the item is worth over what the shop charged:
// runes bought below their market price show as a profit.
//
// NOT DOUBLE-COUNTED. Nothing else records an item that arrives through a shop window: the gathering,
// production and thieving watches count what an action yields, the pickup card what is taken off the
// ground, and a bought item is none of those.

// KindShop groups every shop purchase on one card. Titled by Skill.
const KindShop = "shop"

// SkillShopPurchases titles the Shop purchases card; it is not a game skill, only the card's name.
const SkillShopPurchases = "Shop purchases"

// ActionBuy is one item bought, for the panel's verb.
const ActionBuy = "buy"

// CoinsID is the obj id of Coins, which is what a shop is paid in.
const CoinsID = 995

// ShopBuy is the source of every purchase of one item: one row of the Shop purchases card, drawn
// with the item itself.
func ShopBuy(name string, id int) Source {
	name = strings.TrimSpace(name)
	return Source{Kind: KindShop, Skill: SkillShopPurchases, Action: ActionBuy, Name: name, ID: id, Model: ModelObj}
}

// ShopPurchases turns what one buying spell measurably did (the items that arrived, priced by the
// cache through Items, and the coins that left) into one Drop per item bought. The coins are shared
// between the items in proportion to their cache cost, which is what a shop prices by; one item, the
// usual case, takes them all. Nothing comes back without both halves.
func ShopPurchases(bought []Item, coins int64) []Drop {
	if coins <= 0 || len(bought) == 0 {
		return nil
	}
	items := make([]Item, 0, len(bought))
	var weight int64
	for _, it := range bought {
		if it.Count <= 0 || it.ID == CoinsID {
			continue
		}
		items = append(items, it)
		weight += it.Count * max(it.Value, 1)
	}
	if len(items) == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	out := make([]Drop, 0, len(items))
	left := coins
	for i, it := range items {
		share := coins * it.Count * max(it.Value, 1) / weight
		if i == len(items)-1 {
			share = left
		}
		left -= share
		name := it.Name
		if name == "" {
			name = "obj " + strconv.Itoa(it.ID)
		}
		out = append(out, Drop{
			Source: ShopBuy(name, it.ID),
			Events: int(it.Count),
			Items:  []Item{it},
			Spent:  []Item{{ID: CoinsID, Name: "Coins", Count: share, Value: 1}},
		})
	}
	return out
}
