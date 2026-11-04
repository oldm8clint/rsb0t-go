package bot

import (
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// The Loot panel's Crafting, Fletching, Smithing and Herblore rows for what the production scripts
// make (a user request: "we dont have a loot tracker panel for crafting and pearl bolt tips dont
// show in the fletching section either"). internal/loot/craft.go says what each source is and why
// these report measured counts instead of opening a pack window. Every helper is nil-safe: a bare
// test Context has no Loot sink and records nothing.

// lootMade reports one measured batch: events materials consumed, the products they became.
func lootMade(ctx *Context, src loot.Source, events int, products ...loot.Item) {
	if ctx == nil {
		return
	}
	loot.Made(ctx.Loot, ctx.Config, src, events, products...)
}

// lootGems reports gems cut and smashed from one uncut gem: one event an uncut gem used.
func lootGems(ctx *Context, g gemplan.Gem, cut, crushed int) {
	lootMade(ctx, loot.Crafting(nil, int(g.UncutID), g.UncutName), cut+crushed,
		loot.Product(int(g.CutID), g.Name, cut),
		loot.Product(int(gemplan.CrushedGemstoneID), "Crushed gemstone", crushed))
}

// lootTips reports oyster pearls of kind cut into Pearl bolttips (Fletching).
func lootTips(ctx *Context, kind pearlplan.Kind, pearls, tips int) {
	lootMade(ctx, loot.Fletching(nil, int(kind.ID), kind.Name), pearls,
		loot.Product(int(pearlplan.TipsID), pearlplan.Tips, tips))
}

// lootPearlBolts reports Pearl bolts made: one event a tip (and a Bolts) used.
func lootPearlBolts(ctx *Context, made int) {
	lootMade(ctx, loot.Fletching(nil, int(pearlplan.TipsID), pearlplan.Tips), made,
		loot.Product(int(pearlplan.PearlBoltID), pearlplan.PearlBolt, made))
}

// lootStrung reports bows strung: one event an unstrung bow (and a Bow string) used.
func lootStrung(ctx *Context, unstrungID, strungID int32, name string, n int) {
	lootMade(ctx, loot.Stringing(int(unstrungID), name), n, loot.Product(int(strungID), name, n))
}

// lootSpun reports flax spun into Bow strings, or wool into Balls of wool.
func lootSpun(ctx *Context, flax bool, n int) {
	if ctx == nil {
		return
	}
	loot.Spun(ctx.Loot, ctx.Config, flax, n)
}
