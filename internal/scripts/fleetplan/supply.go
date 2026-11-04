package fleetplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// StockLine is one line of a shared shop's stock, exactly as its .inv defines it.
type StockLine struct {
	// Rune is the display name, matching the Spell rune names ("Chaos rune").
	Rune string
	// Base is the stock count the shop regrows toward (the .inv's second field).
	Base int
	// RateTicks is the .inv's third field: one item regrows every RateTicks of the global tick.
	RateTicks int
	// Cost is the obj's own `cost`, the price at exactly Base stock.
	Cost int
}

// RuneShop is one shared rune shop.
type RuneShop struct {
	Label string
	// Keeper is the shopkeeper's display name, the one entityquery.OpenShop matches.
	Keeper string
	NPCID  int
	// SellMultiplier and Delta are the keeper's shop_sell_multiplier and shop_delta params —
	// the two inputs ~calc_shop_value takes for a purchase.
	SellMultiplier int
	Delta          int
	Lines          []StockLine
}

// Lundail is the Mage Bank's rune shop — the swarm's standard source (owner, 2026-09-19).
//
// Content: mage_arena.inv [magearena_runeshop] scope=shared, restock=yes; npc 903
// magearena_runeshop "Lundail", op3 Trade opens the shop directly (lundail.rs2), params
// shop_sell_multiplier 1000, shop_buy_multiplier 600, shop_delta 30.
//
// Read the numbers before relying on this shop at swarm scale: its stock is a TENTH of Aubury's
// for the elemental runes (200 against 2000) and regrows FIVE times slower (one per 50 ticks
// against one per 10), and its delta of 30 makes each rune bought below base 3% dearer against
// Aubury's 1%. Its chaos and death regrow at exactly Aubury's rate from a quarter of the stock.
// It sells nature, law and cosmic, which Aubury does not, and it is the only source that sits
// next to where Stage 2 leaves the account.
var Lundail = RuneShop{
	Label: "Lundail's Arena-side Rune Shop (Mage Bank)", Keeper: "Lundail", NPCID: 903,
	SellMultiplier: 1000, Delta: 30,
	Lines: []StockLine{
		{"Fire rune", 200, 50, 4},
		{"Water rune", 200, 50, 4},
		{"Air rune", 200, 50, 4},
		{"Earth rune", 200, 50, 4},
		{"Mind rune", 140, 50, 3},
		{"Body rune", 140, 50, 3},
		{"Nature rune", 250, 300, NatureRuneCost},
		{"Chaos rune", 250, 100, 15},
		{"Law rune", 250, 300, 40},
		{"Cosmic rune", 20, 100, CosmicRuneCost},
		{"Death rune", 250, 150, 30},
	},
}

// NatureRuneCost and CosmicRuneCost are runes.obj's own costs for the two runes only Lundail
// stocks. Not used by any combat spell; kept so the Lundail table is complete.
const (
	NatureRuneCost = 20
	CosmicRuneCost = 15
)

// Aubury is Varrock's rune shop (varrock.inv [runeshop], scope=shared; npc 553, op3 Trade;
// shop_sell_multiplier 1000, shop_delta 10). The bigger, cheaper and faster-refilling of the two
// shops — and 400 tiles and two ships from where the swarm lives. See SupplySettings.UseAubury.
var Aubury = RuneShop{
	Label: "Aubury's Rune Shop (Varrock)", Keeper: "Aubury", NPCID: 553,
	SellMultiplier: 1000, Delta: 10,
	Lines: []StockLine{
		{"Fire rune", 2000, 10, 4},
		{"Water rune", 2000, 10, 4},
		{"Air rune", 2000, 10, 4},
		{"Earth rune", 2000, 10, 4},
		{"Mind rune", 1000, 10, 3},
		{"Body rune", 1000, 10, 3},
		{"Chaos rune", 1000, 100, 15},
		{"Death rune", 1000, 150, 30},
	},
}

// WizardsGuild is the Magic Guild Store in the Yanille Wizards' Guild (content: area_yanille/
// configs/magic_guild/magic_guild.inv [magicguildshop], scope=shared; npc magic_store_owner "Magic
// Store owner", op3 Trade, shop_sell_multiplier 1000, shop_delta 10). Behind the guild doors,
// which magic_guild.rs2 opens only at stat(magic) >= 66 — the CURRENT level, so a boost counts —
// hence WizardsGuildMagic. The biggest shop in content: 3000 of each elemental, mind and body
// rune regrowing one per 18 ticks (333/hour each), plus chaos, death, law and nature at 1000 and
// the only blood runes any shop sells (1000, one per 300 ticks). It also stocks soul runes (50,
// one per 100 ticks; no spell here costs one, so they are left out of Lines), a Staff of air and
// the other three elemental staffs (2 each, one per 1000 ticks — the same as Zaff) and
// battlestaves (5, one per 90). Members.
//
// Re-read for the wizguild work (2026-10-04) from the local Content checkout:
//
//   - pack/npc.pack: 461=magic_store_owner (460 wizard_frumscone, 462 guild_wizard). The keeper
//     spawns UPSTAIRS: maps/m40_48.jm2 NPC "1 35 15: 461", level 1 at 2595,3087 (WizardsGuildStand).
//   - the doors: pack/loc.pack 1600=magicguild_door_l, 1601=magicguild_door_r, placed in pairs at
//     2584,3087-3088 (west, angle 2) and 2597,3087-3088 (east, angle 0). magic_guild.rs2
//     [label,open_mageguild_door] refuses only ENTERING below stat(magic) 66 (Wizard Distentor
//     says "You need a magic level of 66"); leaving is never checked. There is no members check in
//     the script: Yanille is members ground, and production is a members world (the Mage Bank
//     trip already depends on that). internal/webwalk/graph.json carries all four door pairs gated
//     on magic 66 in both directions, and the staircase loc 1722 at 2590,3089 (climb up to
//     2589,3091 or 2590,3092 on level 1; stairstop 1723 at 2590,3090 back down).
//   - the nearest bank: Yanille's bankers 494/495 at 2615,3091-3094 (thieverplan.YanilleBank,
//     2612,3092), about 20 tiles east of the guild's east door.
//   - teleports: Watchtower Teleport (Magic 58) refuses without the Watchtower quest and Ardougne
//     Teleport without Plague City (skill_magic/scripts/spells/teleport.rs2), neither of which the
//     fleet does, so the guild is reached on foot: south from East Ardougne's bank,
//     about 190 tiles as the crow flies.
var WizardsGuild = RuneShop{
	Label: "Magic Guild Store (Yanille Wizards' Guild)", Keeper: "Magic Store owner", NPCID: 461,
	SellMultiplier: 1000, Delta: 10,
	Lines: []StockLine{
		{"Fire rune", 3000, 18, 4},
		{"Water rune", 3000, 18, 4},
		{"Air rune", 3000, 18, 4},
		{"Earth rune", 3000, 18, 4},
		{"Mind rune", 3000, 18, 3},
		{"Body rune", 3000, 18, 3},
		{"Nature rune", 1000, 300, NatureRuneCost},
		{"Chaos rune", 1000, 100, 15},
		{"Blood rune", 1000, 300, 50},
		{"Law rune", 1000, 300, 40},
		{"Death rune", 1000, 150, 30},
	},
}

// WizardsGuildMagic is the guild door's gate (magic_guild.rs2: stat(magic) < 66 is refused).
const WizardsGuildMagic = 66

// WizardsGuildStand is the Magic Store owner's own tile, upstairs in the guild (m40_48.jm2 NPC
// "1 35 15: 461": mapsquare 40,48 has origin 2560,3072, so local 35,15 on level 1 is 2595,3087).
// A walk there crosses a magic-66 door and climbs the staircase; the webwalk graph has both.
var WizardsGuildStand = tile.Tile{X: 2595, Z: 3087, Level: 1}

// WizardsGuildHoldTTL is how long a guild hold on the shop board lasts: the walk from East
// Ardougne is about 190 tiles each way as the crow flies, shorter than Aubury's two ships but well
// past a Mage Bank round trip, so Aubury's bound is the safe one.
const WizardsGuildHoldTTL = AuburyHoldTTL

// Line finds a rune's stock line.
func (sh RuneShop) Line(rune string) (StockLine, bool) {
	for _, l := range sh.Lines {
		if strings.EqualFold(l.Rune, rune) {
			return l, true
		}
	}
	return StockLine{}, false
}

// Sells reports whether the shop stocks the rune at all.
func (sh RuneShop) Sells(rune string) bool {
	_, ok := sh.Line(rune)
	return ok
}

// PriceAt is what ONE rune costs when the shop holds `stock` of it — a verbatim port of
// shop.rs2's purchase path:
//
//	price_mod           diff = stock - base
//	calc_shop_value     int5 = min(1000, max(-5000, diff*delta))
//	                    int5 = max(100, sellMultiplier - int5)
//	                    price = scale(int5, 1000, cost) = int5*cost/1000 (truncated)
//	adjusted_item_cost_buying: a purchase price below 1 is raised to 1.
//
// So at base stock a rune costs exactly its obj cost; every rune BELOW base adds delta/10 percent
// (3% at Lundail, 1% at Aubury), capped at six times the cost once the stock is far enough down.
func (sh RuneShop) PriceAt(rune string, stock int) int {
	l, ok := sh.Line(rune)
	if !ok {
		return 0
	}
	return priceAt(l, sh.SellMultiplier, sh.Delta, stock)
}

func priceAt(l StockLine, sellMultiplier, delta, stock int) int {
	diff := stock - l.Base
	int5 := diff * delta
	if int5 > 1000 {
		int5 = 1000
	}
	if int5 < -5000 {
		int5 = -5000
	}
	int5 = sellMultiplier - int5
	if int5 < 100 {
		int5 = 100
	}
	price := int5 * l.Cost / 1000
	if price < 1 {
		price = 1
	}
	return price
}

// CostToBuy is the exact coin cost of buying n runes starting from `stock`, walking the stock
// down one item at a time exactly as shop.rs2's @buy_item loop does (each item is priced at the
// stock level it leaves). n is clamped to the stock actually there.
func (sh RuneShop) CostToBuy(rune string, stock, n int) int {
	l, ok := sh.Line(rune)
	if !ok || n <= 0 || stock <= 0 {
		return 0
	}
	if n > stock {
		n = stock
	}
	total := 0
	for i := 0; i < n; i++ {
		total += priceAt(l, sh.SellMultiplier, sh.Delta, stock-i)
	}
	return total
}

// Affordable is how many runes `coins` buys starting from `stock`, capped at `want` — the same
// walk @buy_item makes before it stops with "You don't have enough coins".
func (sh RuneShop) Affordable(rune string, stock, coins, want int) (n, cost int) {
	return sh.AffordableCapped(rune, stock, coins, want, 0)
}

// AffordableCapped is Affordable that also stops at the first rune dearer than maxPrice (0 means
// no cap). The cap is a spending policy, not a shop rule: at Lundail's delta of 30, the last runes
// of a line cost up to six times the first, and an account that stops at, say, three times the
// base price leaves the dearest runes for the stock to regrow under.
func (sh RuneShop) AffordableCapped(rune string, stock, coins, want, maxPrice int) (n, cost int) {
	l, ok := sh.Line(rune)
	if !ok {
		return 0, 0
	}
	for n < want && n < stock {
		p := priceAt(l, sh.SellMultiplier, sh.Delta, stock-n)
		if maxPrice > 0 && p > maxPrice {
			break
		}
		if cost+p > coins {
			break
		}
		cost += p
		n++
	}
	return n, cost
}

// RegenPerHour is how many of a rune the whole world gets back per hour of real server time from
// this one shop, once it is being bought faster than it refills — the number that decides whether
// a shop can feed a swarm at all. tick is the engine tick (600ms at real speed).
func (l StockLine) RegenPerHour(tick time.Duration) float64 {
	if l.RateTicks <= 0 || tick <= 0 {
		return 0
	}
	return float64(time.Hour) / float64(tick) / float64(l.RateTicks)
}

// EstimateStock projects a stock count observed at `seenAt` forward to `now` by the content's own
// restock rule, capped at base. It is an ESTIMATE of an upper bound: the shop is shared, so any
// other account buying in between makes the real number lower, and the runner must treat the
// count it reads in the open shop as the truth. It is what "time the shop for restock" means
// in practice — the account does not walk back until the estimate says enough has regrown.
func (l StockLine) EstimateStock(seen int, seenAt, now time.Time, tick time.Duration) int {
	if seenAt.IsZero() {
		return l.Base
	}
	if seen >= l.Base {
		return seen
	}
	if now.Before(seenAt) || tick <= 0 || l.RateTicks <= 0 {
		return seen
	}
	ticks := int(now.Sub(seenAt) / tick)
	est := seen + ticks/l.RateTicks
	if est > l.Base {
		est = l.Base
	}
	return est
}

// RestockWait is how long a line observed at `seen` takes to regrow to `want`, at worst: want-seen
// increments at one per RateTicks ticks. The first increment can land up to RateTicks sooner
// (the rule keys on the global tick counter, not on when the item was bought), so this is an
// upper bound, which is the safe direction for "come back when it has regrown".
func (l StockLine) RestockWait(seen, want int, tick time.Duration) time.Duration {
	if want > l.Base {
		want = l.Base
	}
	if want <= seen || l.RateTicks <= 0 {
		return 0
	}
	if tick <= 0 {
		tick = GameTick
	}
	return time.Duration((want-seen)*l.RateTicks) * tick
}

// SupplySettings are the operator's knobs for Stage 3's supply.
type SupplySettings struct {
	// UseAubury adds Aubury's shop as a secondary source for the runes it sells. Default false:
	// see this file's doc comment.
	UseAubury bool `json:"useAubury"`
	// PickpocketRunes lets Paladins/Heroes take over chaos/death/blood once the level allows.
	// Default true (owner, 2026-09-19).
	PickpocketRunes bool `json:"pickpocketRunes"`
}

// DefaultSupplySettings is the owner's policy. UseAubury is ON since the owner's 2026-09-23
// design ("they should buy runes from aubury and mage bank rune shop to get minds"); it is still
// tried only after a Lundail trip is not due (DecideStage3Phase step 3b), and the stage3UseAubury
// setting still turns it off.
func DefaultSupplySettings() SupplySettings {
	return SupplySettings{UseAubury: true, PickpocketRunes: true}
}

// AuburyStand is Aubury's own spawn tile in south-east Varrock (npc 553).
//
// READ FROM CONTENT, not remembered: content/maps/m50_53.jm2's NPC section carries
// "0 53 10: 553" — level 0, local 53,10 in mapsquare 50,53 — and a mapsquare's origin is
// (64*50, 64*53) = (3200, 3392), so the tile is 3253,3402. His own config
// (area_varrock/configs/varrock.npc [aubury]) gives op3=Trade, which is the shop.
var AuburyStand = tile.Tile{X: 3253, Z: 3402}
