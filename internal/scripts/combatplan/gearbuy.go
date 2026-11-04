package combatplan

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The gear-buying loop: which shop sells the best thing this account may wear, and where that
// shop is.
//
// gear.go already holds what every item requires and which shop stocks it, read out of
// content/scripts/levelrequire/scripts/tier*.rs2 and the areas' .inv files. What it does not
// have is where to stand, so this file adds the keepers.
//
// Each keeper was resolved from content rather than guessed. A shop is bound to its keeper by
// an npc param, not by the .inv name: content/scripts/shop/scripts/shop.rs2's
// [proc,openshop_activenpc] reads npc_param(owned_shop), and the trigger above it is
// [opnpc3,_shop_keeper] — option 3, which is why every keeper below carries op3=Trade and why
// entityquery.OpenShop's own Action("Trade") match is the right one. The spawn tiles come from
// the `==== NPC ====` sections of content/maps/*.jm2, the same source as the ladder's.
//
// What this deliberately does NOT do is price anything. The buy price is content's
// ~calc_shop_value haggle formula over live stock, and this client cannot read an item's cost
// off the shop screen, so a pure "can we afford it" answer would be a guess dressed as a fact.
// The runner instead attempts the purchase and reports what the server did. Affordability is
// therefore an observation, never a prediction — which is also why BuyPlan returns what to try,
// in priority order, rather than a basket it claims will fit the purse.

// ShopSite is a shop's keeper and where it stands.
type ShopSite struct {
	// Shop is the shop constant from gear.go.
	Shop string
	// Keeper is the NPC display name, as cachecfg.Npc.Name returns it.
	Keeper string
	// Stand is the keeper's own spawn tile.
	Stand tile.Tile
	// Bank is the nearest usable bank booth, confirmed against the baked loc index.
	Bank tile.Tile
	// Note carries a gate an operator should know about.
	Note string
}

// shopSites maps every shop gear.go names to its keeper. Champions' Guild entries are present
// and deliberately unusable by this trainer: the door needs 32 quest points, which a pure
// combat run never earns.
var shopSites = map[string]ShopSite{
	ShopVarrockSword:     {Shop: ShopVarrockSword, Keeper: "Shop keeper", Stand: tile.Tile{X: 3203, Z: 3397}, Bank: tile.Tile{X: 3253, Z: 3419}},
	ShopVarrockArmour:    {Shop: ShopVarrockArmour, Keeper: "Horvik", Stand: tile.Tile{X: 3229, Z: 3438}, Bank: tile.Tile{X: 3253, Z: 3419}},
	ShopVarrockArchery:   {Shop: ShopVarrockArchery, Keeper: "Lowe", Stand: tile.Tile{X: 3232, Z: 3423}, Bank: tile.Tile{X: 3253, Z: 3419}},
	ShopBarbarianHelmet:  {Shop: ShopBarbarianHelmet, Keeper: "Peksa", Stand: tile.Tile{X: 3076, Z: 3429}, Bank: tile.Tile{X: 3095, Z: 3491}},
	ShopAlKharidScimitar: {Shop: ShopAlKharidScimitar, Keeper: "Zeke", Stand: tile.Tile{X: 3288, Z: 3190}, Bank: tile.Tile{X: 3268, Z: 3167}},
	ShopAlKharidLegs:     {Shop: ShopAlKharidLegs, Keeper: "Louie Legs", Stand: tile.Tile{X: 3321, Z: 3188}, Bank: tile.Tile{X: 3268, Z: 3167}},
	ShopAlKharidSkirt:    {Shop: ShopAlKharidSkirt, Keeper: "Ranael", Stand: tile.Tile{X: 3315, Z: 3167}, Bank: tile.Tile{X: 3268, Z: 3167}},
	ShopFaladorShield:    {Shop: ShopFaladorShield, Keeper: "Cassie", Stand: tile.Tile{X: 2979, Z: 3383}, Bank: tile.Tile{X: 3013, Z: 3354}},
	ShopFaladorChain:     {Shop: ShopFaladorChain, Keeper: "Wayne", Stand: tile.Tile{X: 2973, Z: 3312}, Bank: tile.Tile{X: 3013, Z: 3354}},
	ShopFaladorMace:      {Shop: ShopFaladorMace, Keeper: "Flynn", Stand: tile.Tile{X: 2951, Z: 3386}, Bank: tile.Tile{X: 3013, Z: 3354}},
	ShopChampionsGuild:   {Shop: ShopChampionsGuild, Keeper: "Valaine", Stand: tile.Tile{X: 3193, Z: 3361, Level: 1}, Bank: tile.Tile{X: 3253, Z: 3419}, Note: noteQuestPoints},
	ShopRuniteGuild:      {Shop: ShopRuniteGuild, Keeper: "Scavvo", Stand: tile.Tile{X: 3191, Z: 3351, Level: 1}, Bank: tile.Tile{X: 3253, Z: 3419}, Note: noteQuestPoints},

	// Oziach stands outside Edgeville and is the only source of a rune platebody. The shop
	// itself has no door gate, so it is Reachable — but the platebody needs Dragon Slayer to
	// WEAR (gear.go's questGates), and Wearable refuses a quest-gated item whenever questsDone
	// does not name that quest, which for a combat-only run is always. So BuyPlan never picks
	// it, and the honest reason is "the account cannot wear it", not "the shop is shut".
	ShopEdgevilleOziach: {Shop: ShopEdgevilleOziach, Keeper: "Oziach", Stand: tile.Tile{X: 3069, Z: 3517}, Bank: tile.Tile{X: 3095, Z: 3491}, Note: "Dragon Slayer to wear what it sells"},

	// Aubury and Zaff, the two shops a Magic run lives on, both in Varrock and both banking at
	// the Varrock east booth the melee ladder already uses. Spawn tiles from the `==== NPC ====`
	// section of content/maps/m50_53.jm2 (mapsquare origin 3200,3392): Aubury is npc 553 at local
	// 53,10 and Zaff is npc 546 at local 3,41. One spawn each.
	//
	// Betty's Port Sarim shop (port_sarim.inv [magicshop], npc 583 at 3012,3259) stocks the
	// identical eight rune lines and is deliberately NOT listed. Aubury is the same shelf beside
	// the sword, armour and archery shops this run already walks to, while Port Sarim has no bank
	// of its own, so adding her would mean guessing a bank tile for a trip that buys nothing
	// Aubury cannot sell. She is worth knowing about only if Aubury's route ever fails.
	ShopVarrockRune:  {Shop: ShopVarrockRune, Keeper: "Aubury", Stand: tile.Tile{X: 3253, Z: 3402}, Bank: tile.Tile{X: 3253, Z: 3419}},
	ShopVarrockStaff: {Shop: ShopVarrockStaff, Keeper: "Zaff", Stand: tile.Tile{X: 3203, Z: 3433}, Bank: tile.Tile{X: 3253, Z: 3419}},

	// The Ranging Guild's bow shop. Listed for completeness because gear.go's bow and arrow
	// rows name it; nothing in this milestone buys from it, since MeleeSlots has no bow or
	// ammo slot and Ranged is milestone 4. Its door needs Ranged 40 (ranging_guild_door.rs2),
	// and no route to it has been walked with this client, so Bank is deliberately left unset
	// rather than guessed — whoever builds the Ranged ladder confirms it then.
	ShopRangingGuild: {Shop: ShopRangingGuild, Keeper: "Bow and Arrow salesman", Stand: tile.Tile{X: 2673, Z: 3434}, Note: noteRangingGuild},
}

// ShopSiteByShop returns the keeper site for a named shop.
//
// ShopSiteFor covers everything in the gear table, which is everything a run WEARS. This is for
// what a run SPENDS: runes are consumables with no wear slot and no upgrade ladder, so they are
// deliberately not items, and a caller buying them needs the keeper by shop name instead.
func ShopSiteByShop(shop string) (ShopSite, bool) {
	s, ok := shopSites[shop]
	return s, ok
}

// ShopSiteFor returns where to buy an item, and whether a shop sells it at all.
func ShopSiteFor(it Item) (ShopSite, bool) {
	if it.Shop == "" {
		return ShopSite{}, false
	}
	site, ok := shopSites[it.Shop]
	return site, ok
}

// Reachable reports whether this trainer can actually get into the shop. Both Champions' Guild
// shops sit behind a 32-quest-point door that a combat-only run never opens, and the Ranging
// Guild behind a Ranged 40 door on a route nobody has walked, so all three are listed for
// honesty and refused in practice.
func (s ShopSite) Reachable() bool {
	return s.Note != noteQuestPoints && s.Note != noteRangingGuild
}

// MeleeSlots are the slots the melee ladder equips, in the order a run should buy them:
// the weapon first, because it is what makes the next kill faster, then armour by how much it
// keeps the account alive.
var MeleeSlots = []Slot{SlotWeapon, SlotBody, SlotLegs, SlotHelm, SlotShield}

// Purchase is one thing to buy and wear.
type Purchase struct {
	Slot Slot
	Item Item
	Site ShopSite
	// Replaces is what is currently worn in that slot, empty when nothing is.
	Replaces string
}

// WornTier returns the best item currently worn in a slot and its requirement level. Matching
// is by name against gear.go's own table, so anything the table does not know about (a random
// event reward, a quest item) reads as nothing worn — the safe direction, since the worst it
// causes is one redundant upgrade attempt.
func WornTier(worn []string, slot Slot) (Item, bool) {
	best, found := Item{}, false
	for _, name := range worn {
		it, ok := ItemByName(name)
		if !ok || it.Slot != slot {
			continue
		}
		if !found || it.Level > best.Level {
			best, found = it, true
		}
	}
	return best, found
}

// BuyPlan is the gear-buying loop's decision: for every melee slot, the best item the account
// may wear that a reachable shop actually stocks, skipping any slot already holding something
// as good.
//
// held is everything in the pack, so an item already bought but not yet worn is not bought
// twice. The result is ordered by MeleeSlots, which is the order a run should attempt them in;
// a caller that runs out of coins partway simply stops, and what it did buy is still the most
// valuable part of the plan.
func BuyPlan(l Levels, questsDone map[string]bool, worn, held []string) []Purchase {
	return BuyPlanExcept(l, questsDone, worn, held, nil)
}

// BuyPlanExcept is BuyPlan with a set of item names the run has learned it cannot actually buy,
// keyed by lower-case name.
//
// Why a run needs this at all: this package knows which shop LISTS an item, because that was
// read from the shop's own .inv, but an .inv line also carries a stock count and several of
// them are zero. Cassie's shield shop lists a mithril square shield and stocks none of it, so
// the best shield this table can name is one the shelf never has. A run that discovers that in
// game puts the name in this set, and the plan drops to the next tier the shop does have
// instead of walking to Falador again for something that is not there.
//
// Purely additive: with a nil set this is exactly BuyPlan, so nothing that does not learn
// anything behaves differently.
func BuyPlanExcept(l Levels, questsDone map[string]bool, worn, held []string, unavailable map[string]bool) []Purchase {
	haveHeld := map[string]bool{}
	for _, n := range held {
		haveHeld[strings.ToLower(strings.TrimSpace(n))] = true
	}

	var out []Purchase
	for _, slot := range MeleeSlots {
		want, ok := bestReachableBuyable(slot, l, questsDone, unavailable)
		if !ok {
			continue
		}
		if haveHeld[strings.ToLower(want.Name)] {
			continue // already bought, just not worn yet
		}
		current, wearing := WornTier(worn, slot)
		if wearing && current.Level >= want.Level {
			continue
		}
		site, ok := ShopSiteFor(want)
		if !ok {
			continue
		}
		p := Purchase{Slot: slot, Item: want, Site: site}
		if wearing {
			p.Replaces = current.Name
		}
		out = append(out, p)
	}
	return out
}

// bestReachableBuyable is BestBuyable restricted to shops this trainer can actually enter, and
// to items a run has not already found missing from the shelf.
func bestReachableBuyable(slot Slot, l Levels, questsDone map[string]bool, unavailable map[string]bool) (Item, bool) {
	best, found := Item{}, false
	for _, it := range Items() {
		if it.Slot != slot || it.Shop == "" || !Wearable(it, l, questsDone) {
			continue
		}
		if unavailable[strings.ToLower(it.Name)] {
			continue
		}
		site, ok := ShopSiteFor(it)
		if !ok || !site.Reachable() {
			continue
		}
		if !found || it.Level > best.Level {
			best, found = it, true
		}
	}
	return best, found
}

// EquipPlan is what to put on out of what is held, best-first per slot. It is separate from
// BuyPlan because wearing is also what happens after a death, with nothing bought at all.
func EquipPlan(l Levels, questsDone map[string]bool, worn, held []string) []Item {
	bySlot := map[Slot]Item{}
	for _, name := range held {
		it, ok := ItemByName(name)
		if !ok || !Wearable(it, l, questsDone) {
			continue
		}
		if !isMeleeSlot(it.Slot) {
			continue
		}
		if cur, seen := bySlot[it.Slot]; seen && cur.Level >= it.Level {
			continue
		}
		bySlot[it.Slot] = it
	}
	var out []Item
	for _, slot := range MeleeSlots {
		want, ok := bySlot[slot]
		if !ok {
			continue
		}
		if cur, wearing := WornTier(worn, slot); wearing && cur.Level >= want.Level {
			continue
		}
		out = append(out, want)
	}
	return out
}

func isMeleeSlot(s Slot) bool {
	for _, m := range MeleeSlots {
		if m == s {
			return true
		}
	}
	return false
}

// GearCeiling is the tier this trainer can reach on its own, and why it stops there.
//
// This is the consequence of running with no mule, and it is stated in code rather than left
// as a surprise in a log: every shop-buyable melee tier tops out below rune. Rune scimitar,
// two-hander, full helm, med helm, kiteshield and square shield are sold NOWHERE in content
// 289; the rune longsword, sword, mace, chainbody, platelegs and plateskirt are sold only by
// Scavvo behind the Champions' Guild's 32-quest-point door; and the one rune platebody
// (Oziach) additionally needs Dragon Slayer to wear. A combat-only run earns no quest points,
// so from Attack or Defence 40 onward the account is wearing adamant while its levels would
// allow rune.
//
// Stalled is true once a governing stat has passed a tier the shops cannot supply. The run
// does not stop — adamant still trains fine to 75 — but the log says it plainly so nobody
// mistakes a ceiling for a bug.
type GearCeiling struct {
	Stalled bool
	// BestBuyable is the highest tier a reachable shop stocks for weapons and for armour.
	BestWeaponTier, BestArmourTier string
	Reason                         string
}

// CheckGearCeiling reports whether the account has outgrown what the shops can sell it.
func CheckGearCeiling(l Levels, questsDone map[string]bool) GearCeiling {
	c := GearCeiling{}
	if w, ok := bestReachableBuyable(SlotWeapon, l, questsDone, nil); ok {
		c.BestWeaponTier = w.Tier
	}
	if a, ok := bestReachableBuyable(SlotBody, l, questsDone, nil); ok {
		c.BestArmourTier = a.Tier
	}
	runeLevel, _ := MetalTierLevel("Rune")
	if l.Attack >= runeLevel || l.Defence >= runeLevel {
		c.Stalled = true
		c.Reason = "Attack/Defence have reached " + itoa(runeLevel) + ", but no reachable shop sells rune melee gear in content 289: " +
			"rune scimitar, 2h, full helm, med helm, kiteshield and sq shield are sold nowhere, and the rest sit behind the Champions' Guild's 32 quest points. " +
			"Without the mule item path this run stays on " + c.BestWeaponTier + "/" + c.BestArmourTier + " to 75"
	}
	return c
}

// itoa is a local integer formatter, so this file need not pull strconv in for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append(b, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// ShopSites returns every shop site, sorted by shop name, for a log line or a doc.
func ShopSites() []ShopSite {
	out := make([]ShopSite, 0, len(shopSites))
	for _, s := range shopSites {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Shop < out[j].Shop })
	return out
}
