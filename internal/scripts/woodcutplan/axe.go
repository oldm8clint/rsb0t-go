package woodcutplan

// Getting a better axe: where one can actually be bought on this server, what it costs, and how
// many coins to ask the mule for. Pure, like the rest of this package — no I/O, no session.
//
// EVERYTHING HERE IS MEASURED FROM THE GAME CONTENT, not assumed, because all three of the
// fleet owner's own stated beliefs about axes needed checking and two of them were wrong in a
// way that changes the design:
//
//  1. "A rune axe works from Woodcutting 1." TRUE, and confirmed.
//     content/scripts/skill_woodcutting/scripts/woodcut.rs2's `woodcutting_axe_checker` has its
//     Woodcutting level checks COMMENTED OUT, with the comment "no wc req in 2004"; the live
//     code is `if (($obj1 = rune_axe | inv_total(inv, rune_axe) > 0))` with no level term at all
//     (`def_int $level = stat(woodcutting)` is assigned and then never read — a dead variable,
//     which is what proves the checks were disabled rather than moved). Note `inv_total(inv,...)`:
//     the axe counts FROM THE INVENTORY, so it does not even need to be wielded. The
//     `param=levelrequire,41` on rune_axe is an ATTACK requirement enforced only by
//     `[opheld2,rune_axe] @levelrequire_attack(40, last_slot)` — it gates WIELDING and nothing
//     else. So a level-1 account chops at rune speed with the axe loose in its pack.
//
//  2. "Bob's Axes stocks up to about steel." TRUE, and steel is exactly the ceiling.
//     content/scripts/areas/area_lumbridge/configs/lumbridge.inv's `[axeshop]` section:
//     bronze_axe x10, iron_axe x5, steel_axe x3 (plus a bronze pickaxe and three battleaxes,
//     which are weapons and not chopping axes). Nothing above steel.
//
//  3. "Axes are bought at the Seers market." TRUE but NOT as a shop.
//     There is no shop of any kind in Seers' Village on this build: no `.inv` file anywhere
//     under content/scripts/areas/area_seers/, no `param=owned_shop` on any of its four NPCs,
//     and no `openshop` call in the area at all. The Seers Market is a PLAYER-run shop. Buying
//     means saying `buying <n> <item>` in public chat and waiting for the person on the other
//     side to tally the price, draw it from their own bank and open a trade. That is why
//     MarketAsk exists and why there is no shop-interface path to the market.
//
//     There is also more than ONE market, which changes where to LOOK but not the mechanism.
//     The fleet owner named Tradevault as a second player market in Seers' Village, and the
//     inventory page's own character selector lists five: Seers Market 795, Tradevault 2169,
//     Constructor 864, Edge Market 941, Sil Stuff 2148 — all five fetched and parsed on
//     2026-09-20, none guessed. Because Seers Market and Tradevault stand at the SAME Seers
//     bank, one public-chat line reaches both, so BuyAtMarket needs no per-market variation and
//     deliberately has none. What the extra markets change is the lookup — "is there an axe for
//     sale anywhere" is a question across markets — which is what the control panel's market
//     panel answers. Measured on that same reading, and worth knowing before configuring
//     anything: the Seers Market was the ONLY one stocking a chopping axe above bronze (black
//     15, iron 172, mithril 10, rune 93, steel 1), while Tradevault, Constructor and Edge
//     Market each held exactly one bronze axe and Sil Stuff none. So the Seers-village pair is
//     the realistic source and Bob's remains the only NPC fallback.
//
// A consequence worth stating plainly, because it decides what is worth attempting at all:
// NO NPC SHOP ON THIS SERVER SELLS AN AXE ABOVE STEEL. Black, mithril, adamant and rune axes
// come only from smithing, drops, or a player market. Bob's is therefore a fallback that can
// only ever reach steel.

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// BobsAxes is where Bob himself stands, inside Bob's Brilliant Axes in Lumbridge.
//
// MEASURED, not looked up: content/maps/m50_50.jm2's own `==== NPC ====` section contains the
// row `0 32 3: 519`, i.e. npc 519 (Bob) at level 0, offset 32,3 inside mapsquare 50,50 — so
// 50*64+32, 50*64+3 = 3232,3203. lumbridge.npc's `[bob]` block sets `wanderrange=0`, so he does
// not move off it.
//
// This replaced freshstart.StoreStand (3209,3247) as the BobStand default, which was simply the
// wrong building: that is the Lumbridge GENERAL STORE, 44 tiles away, and an account sent there
// to buy or repair an axe would have found a shopkeeper who sells neither. Nothing had ever
// reached the shop live, so the wrong constant had never shown itself.
var BobsAxes = tile.Tile{X: 3232, Z: 3203}

// AxeSource is where a wanted axe can actually come from.
type AxeSource string

// The places an axe can come from, in the order DecideAxeBuy prefers them.
const (
	// AxeFromBank is an axe the account already owns, sitting in its own bank. Free.
	AxeFromBank AxeSource = "bank"
	// AxeFromMarket is the Seers player market: a public-chat ask and a trade, not a shop.
	AxeFromMarket AxeSource = "market"
	// AxeFromBob is Bob's Brilliant Axes in Lumbridge, a real shop interface, steel at best.
	AxeFromBob AxeSource = "bob"
	// AxeFromNowhere means nothing reachable sells it.
	AxeFromNowhere AxeSource = "nowhere"
)

// BobStock is every woodcutting axe Bob's Brilliant Axes actually stocks, worst to best —
// MEASURED from lumbridge.inv's `[axeshop]` section (see this file's own header). The
// battleaxes and the bronze pickaxe in the same section are deliberately absent: a battleaxe is
// a weapon and a pickaxe mines, and neither chops.
var BobStock = []string{"Bronze axe", "Iron axe", "Steel axe"}

// BobSells reports whether Bob stocks name.
func BobSells(name string) bool {
	for _, a := range BobStock {
		if same(a, name) {
			return true
		}
	}
	return false
}

// BobBest is the best axe Bob can supply.
const BobBest = "Steel axe"

// AxelessRescue is what an account holding NOTHING TO CHOP WITH should try to buy from Bob,
// best first, when the market it was configured against cannot supply the axe it wants. Empty
// for an account that already holds an axe, because that one is upgrading and can afford to
// wait -- it is chopping the whole time.
//
// THE DISTINCTION THIS DRAWS, and why the tier ladder above does not already cover it. That
// ladder only moves down a tier when the market REFUSES for a stock reason
// (MarketRefusalIsStock), which is right: a timeout is not evidence the stall has none. But
// live 2026-09-22 acct134 sat at Seers for 25 minutes at Woodcutting 1 with NO axe, a Knife,
// and 150,000 coins, asking a market that never answered for a rune axe and then waiting out
// axeRetry, over and over. "Nobody answered" is not a stock refusal, so the ladder never fired,
// and Bob does not sell rune, so the Bob fallback did not either. The account had the money for
// any axe in the game and nothing to cut a tree with.
//
// For an account in that state the question is not "which axe is best" but "how soon can it
// chop at all". Any axe chops from Woodcutting 1 on this build -- woodcut.rs2's level checks
// are commented out, see this file's header -- so a Steel axe bought now beats a rune axe
// bought whenever a player market happens to answer. The upgrade is not abandoned: the next
// axeStep sees the new axe in hand and goes back to asking for the configured one.
func AxelessRescue(held []string) []string {
	for _, h := range held {
		if strings.TrimSpace(h) != "" {
			return nil
		}
	}
	out := make([]string, 0, len(BobStock))
	for i := len(BobStock) - 1; i >= 0; i-- {
		out = append(out, BobStock[i])
	}
	return out
}

// BrokenAxeRepairCost is what Bob charges to repair each tier's broken axe, in coins, keyed by
// the axe he hands back.
//
// MEASURED from `oc_cost` on the seven broken hatchets in
// content/scripts/macro events/configs/antimacro.obj (objs 494, 496, 498, 500, 502, 504, 506 —
// one per metal, every one of them named "Broken axe" and carrying
// `param=to_be_fixed_by_bob,^true` plus a `param=fixed` naming the axe it becomes). bob.rs2's
// `[opnpcu,bob]` charges exactly `oc_cost($item)`.
//
// The first three are FREE, which matters for a fleet: a bronze, iron or steel axe breaking
// costs an account nothing but the trip, and content answers the free case with its own
// "Would you like me to repair it for free?" wording ahead of the same two-option question.
// A rune axe at 427gp is the only one worth thinking about.
//
// Bob will NOT touch a broken PICKAXE: objs 468-478 carry `param=to_be_fixed_by_nurmof`
// instead, and bob.rs2's else-branch answers "Sorry friend, but I can't do anything with
// that." That is Nurmof's job in the Dwarven Mine, and nothing in this script goes there.
var BrokenAxeRepairCost = map[string]int{
	"Bronze axe":  0,
	"Iron axe":    0,
	"Steel axe":   0,
	"Black axe":   10,
	"Mithril axe": 18,
	"Adamant axe": 43,
	"Rune axe":    427,
}

// MaxBrokenAxeRepairCost is the most Bob can ask for any one repair (the rune axe's 427).
// Carrying at least this many coins means no repair can be refused for want of money.
const MaxBrokenAxeRepairCost = 427

// RepairTripCoins is what an account must be carrying before it sets out for Bob's: his fee
// plus the fare BOTH WAYS.
//
// The return leg is included deliberately. Measured offline against the real transport graph
// (cmd/webwalkplan, 2026-09-20): a quest-less account — which every fleet woodcutter is, since
// the spirit trees need Tree Gnome Village and the gliders need The Grand Tree — has exactly
// one Seers <-> Lumbridge route, Ardougne -> Brimhaven -> Karamja -> Port Sarim, and it is two
// paid fares in each direction. An account that spends its last coins on the outward leg and
// the repair arrives at Bob's with a working axe and no way home, and the phase machine then
// answers with PhaseFund — pickpocketing Men in Lumbridge for the fare, which works but costs
// the fleet an account for a long while. Carrying the return fare avoids the whole detour.
func RepairTripCoins(repairedAxe string, fare int) int {
	if fare < 0 {
		fare = 0
	}
	return BrokenAxeRepairCost[repairedAxe] + 2*fare
}

// AxeBuy is one decision about getting a better axe than the account currently has.
type AxeBuy struct {
	// Want is the axe to get, or "" when nothing needs doing.
	Want string
	// From is where to get it.
	From AxeSource
	// Need is how many coins short the account is, given the price it was told. Zero means it
	// can already afford it (or the price is unknown — see Priced).
	Need int
	// Priced is false when no price was supplied for Want, in which case Need is meaningless
	// and a caller must not treat "0 short" as "affordable".
	Priced bool
	// Why is a short human-readable reason, for the log line.
	Why string
}

// DecideAxeBuy decides what to do about upgrading the account's axe.
//
// held and banked are axe names (as HeldAxes/BankAxes report them); want is the axe the
// operator is working toward; coins is what the pack holds; priceOf answers the going rate for
// an axe in coins, reporting false when it does not know (an item the market has never traded
// has NO price, which is not the same as being free — so an unknown price never reads as
// affordable).
//
// The order is deliberate and is the cheapest-first order: an axe already in the bank costs
// nothing and beats buying one; the market can reach any tier but needs coins and a person;
// Bob's is the fallback and tops out at steel.
func DecideAxeBuy(held, banked []string, want string, coins int, priceOf func(axe string) (int, bool)) AxeBuy {
	want = strings.TrimSpace(want)
	if want == "" || !IsAxe(want) {
		return AxeBuy{Why: "no axe target configured"}
	}
	// Nothing to do if what is already in hand is as good as, or better than, the target.
	if best := BestAxe(held); best != "" && axeIndex(best) >= axeIndex(want) {
		return AxeBuy{Why: "already holding a " + strings.ToLower(best)}
	}
	// A banked axe that meets the target is free — take that before spending anything.
	if best := BestAxe(banked); best != "" && axeIndex(best) >= axeIndex(want) {
		return AxeBuy{Want: best, From: AxeFromBank, Why: "a " + strings.ToLower(best) + " is already in the bank"}
	}

	price, priced := 0, false
	if priceOf != nil {
		price, priced = priceOf(want)
	}
	need := 0
	if priced && price > coins {
		need = price - coins
	}

	// Above steel, only the player market can supply it at all — no NPC shop on this server
	// stocks a black, mithril, adamant or rune axe.
	if !BobSells(want) {
		return AxeBuy{Want: want, From: AxeFromMarket, Need: need, Priced: priced,
			Why: "no shop on this server sells a " + strings.ToLower(want) + " — only the Seers market"}
	}
	// At or below steel both routes exist. Prefer the market when the account is already at
	// Seers' own bank (which is where this script lives), and fall back to Bob otherwise; the
	// caller decides which by trying the market first and calling again on a refusal.
	return AxeBuy{Want: want, From: AxeFromMarket, Need: need, Priced: priced,
		Why: "the Seers market first, Bob's Axes in Lumbridge as a fallback"}
}

// FallbackFrom is where to try next after From refused, or AxeFromNowhere when nothing is left.
// Only an axe Bob actually stocks has a fallback at all.
func FallbackFrom(from AxeSource, want string) AxeSource {
	if from == AxeFromMarket && BobSells(want) {
		return AxeFromBob
	}
	return AxeFromNowhere
}

// ---- the public-chat ask ----

// MarketVerb is how a buy is phrased to the market, in the fleet owner's own words: "buying x
// amount of x item". The person on the other side reads it, tallies a price and opens a trade.
const MarketVerb = "buying"

// MaxChatLine matches actions.PublicChatLimit — the client's own WordPack ceiling of 80
// characters. Duplicated here rather than imported so this package keeps its "no actions
// import" rule; MarketAsk never produces anything near it, and a test pins that.
const MaxChatLine = 80

// MarketAsk is the public-chat line that buys qty of item from a player market, e.g.
// "buying 1 rune axe". Lower-cased because that is how the line is read by a person, and cut to
// MaxChatLine so the client never silently truncates mid-word.
func MarketAsk(item string, qty int) string {
	if qty < 1 {
		qty = 1
	}
	line := fmt.Sprintf("%s %d %s", MarketVerb, qty, strings.ToLower(strings.TrimSpace(item)))
	if len(line) > MaxChatLine {
		line = strings.TrimSpace(line[:MaxChatLine])
	}
	return line
}

// ---- asking the mule for the money ----

// CoinMargin is added to a market price before asking the mule for coins. The prices this
// script can see are the market's own LAST TRADED figures, not a quote — the person on the
// other side tallies the real price when the ask is made, and arriving short wastes the whole
// trip (they draw the item, we cannot pay, nothing crosses). A margin is much cheaper than a
// second round trip, and unspent coins simply go back to the mule with the next hand-over.
const CoinMargin = 10

// CoinsToRequest is how many coins to ask the mule for, given the price and what is already
// held. Returns 0 when nothing needs asking for.
//
// The margin is applied to the SHORTFALL, not to the price, so an account that is only a few
// coins short does not ask for a whole extra axe's worth.
func CoinsToRequest(price, held int) int {
	if price <= 0 || held >= price {
		return 0
	}
	short := price - held
	short += short * CoinMargin / 100
	if short < 1 {
		short = 1
	}
	return short
}
