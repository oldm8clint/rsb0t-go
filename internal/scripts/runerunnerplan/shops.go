// Package runerunnerplan is RuneRunner's pure half: a dedicated buyer account, not the mule,
// that buys out the runes the ScarabSwarm cohort casts from the rune shops the fleet can reach,
// on the mule's world and one other, and trades them to the MuleReceiver at its post.
//
// The owner's directive (2026-09-26): "the shops probably hold plenty of minds, maybe mule stays
// and another account buys them out. should be buying all runes from all 3 stores when we can,
// wizards guild locked till 66 magic". The three stores are Aubury (Varrock), Betty (Port Sarim)
// and Lundail (the Mage Bank); the Wizards' Guild in Yanille is a fourth store behind a Magic 66
// door (a user request: "make sure when we hit 66 magic the wizards guild is considered as a
// place to buy runes from"). It is in the default shops, and a runner whose CURRENT Magic level is
// below 66 leaves it out of each round (EligibleShops) and never walks to it.
//
// Everything here is decided from plain values: no client, actions, fleet or bot imports and no
// I/O, only scarabplan (the shop model and the Mage Bank trip), donorplan (the hand-over rules),
// tile and the standard library. The I/O half is internal/scripts/runerunner and the bot glue is
// internal/bot/runerunner.go.
package runerunnerplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Betty is Port Sarim's rune shop, defined here rather than in scarabplan (claimed by the
// ScarabSwarm lanes). RuneShop's fields are exported and ShopBoard keys by keeper name, so this
// literal works with the shared board unchanged.
//
// READ FROM CONTENT (the local Content checkout, branch 289), not remembered:
//
//   - area_port_sarim/configs/port_sarim.npc [betty]: name=Betty, op3=Trade,
//     param=owned_shop,magicshop, shop_sell_multiplier 1000, shop_buy_multiplier 600,
//     shop_delta 10 -- the same pricing parameters as Aubury.
//   - area_port_sarim/configs/port_sarim.inv [magicshop]: scope=shared, restock=yes, and the
//     eight rune lines below (`stockN=obj,count,rate`, the third field in ticks). The shop also
//     stocks eye of newt and two wizard hats, which are not runes and are left out.
//   - pack/npc.pack: 583=betty.
//   - costs: skill_magic/configs/runes.obj (elementals 4, mind 3, body 3, chaos 15, death 30).
//
// An older content copy stocks Betty's chaos at 500 and prices her at 1300/30; production may
// run it. The table only drives estimates: every purchase is planned from the real shelf, and
// the runner's price check (PriceMismatch) disables a pair whose real spend disagrees.
var Betty = fleetplan.RuneShop{
	Label: "Betty's Magic Emporium (Port Sarim)", Keeper: "Betty", NPCID: 583,
	SellMultiplier: 1000, Delta: 10,
	Lines: []fleetplan.StockLine{
		{Rune: "Fire rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Water rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Air rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Earth rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Mind rune", Base: 1000, RateTicks: 10, Cost: 3},
		{Rune: "Body rune", Base: 1000, RateTicks: 10, Cost: 3},
		{Rune: "Chaos rune", Base: 1000, RateTicks: 100, Cost: 15},
		{Rune: "Death rune", Base: 1000, RateTicks: 150, Cost: 30},
	},
}

// BettyStand is Betty's spawn tile: content/maps/m47_50.jm2 carries "0 4 59: 583" -- level 0,
// local 4,59 in mapsquare 47,50, whose origin is (3008, 3200).
var BettyStand = tile.Tile{X: 3012, Z: 3259}

// BettyHoldTTL is how long a Betty hold on the shared board lasts: the same bound as Aubury's,
// since both sit at the far end of the boat chain.
const BettyHoldTTL = 30 * time.Minute

// Zaff is Varrock's staff shop, modelled with the rune-shop price walk because content prices it
// with the same shop.rs2 formula. It is NOT a rune site (Site does not know it, and the shops
// setting refuses it); the ring visits it only when the zaffStaffs setting asks for staffs.
//
// READ FROM CONTENT, not remembered:
//
//   - area_varrock/scripts/zaff.rs2: op3 Trade opens the shop directly, as
//     openshop(staffshop or memberstaffshop, 550, 1000, 20, ...) -- sell multiplier 1000, delta 20
//     (the 550 is what he pays when a player sells).
//   - area_varrock/configs/varrock.inv [staffshop] and [memberstaffshop] (members worlds, which
//     the Mage Bank makes production): scope=shared, restock=yes, staff_of_air,2,1000 in both.
//   - skill_combat/configs/magic/staves.obj [staff_of_air]: cost 1500.
//   - maps/m50_53.jm2 "0 3 41: 546": npc 546 (zaff) at 3203,3433, which is fleetplan.ZaffStand.
//
// So a full shelf is two staffs, one back every 1000 ticks (ten minutes at 600 ms), the first at
// 1500 and the second at 1530.
//
// Battlestaffs (a user request: "buy battlestaff's also on the route for use with crafting"): the members'
// shelf stocks five (varrock.inv stock1=battlestaff,5,100: one back every 100 ticks), and the obj costs 7000
// (staves.obj [battlestaff]), so a full shelf is 7000, 7140, 7280, 7420 and 7560.
var Zaff = fleetplan.RuneShop{
	Label: "Zaff's Superior Staffs! (Varrock)", Keeper: "Zaff", NPCID: 546,
	SellMultiplier: 1000, Delta: 20,
	Lines: []fleetplan.StockLine{
		{Rune: fleetplan.StaffOfAir, Base: 2, RateTicks: 1000, Cost: 1500},
		{Rune: Battlestaff, Base: 5, RateTicks: 100, Cost: 7000},
		// Staff of fire (a user request: "Zaff: Staff of fire / fire staffs (ScarabSwarm alchers need fire staffs)"): varrock.inv
		// [staffshop] stock6 and [memberstaffshop] stock7, staff_of_fire,2,1000 -- the same shelf as the Staff of air (two, one back
		// every 1000 ticks), obj cost 1500 (staves.obj), so 1500 and 1530 a full shelf.
		{Rune: StaffOfFire, Base: 2, RateTicks: 1000, Cost: 1500},
	},
}

// StaffOfFire is the Zaff line the alchers (alchplan) cast High Level Alchemy with.
const StaffOfFire = "Staff of fire"

// Battlestaff is the Zaff line the crafter's battlestaves are made from.
const Battlestaff = "Battlestaff"

// ZaffStand is Zaff's own tile.
var ZaffStand = fleetplan.ZaffStand

// Site keys, the spelling the settings use. SiteZaff names the staff station, not a rune site.
const (
	SiteAubury       = "Aubury"
	SiteBetty        = "Betty"
	SiteLundail      = "Lundail"
	SiteWizardsGuild = "WizardsGuild"
	SiteZaff         = "Zaff"
)

// Banks the buyer uses. Each is the tile another package already proves live; a test pins them
// against those packages so a move there is noticed here.
var (
	// CatherbyBank is marketbulkplan.CatherbyPost, which is also the mule's post.
	CatherbyBank = tile.Tile{X: 2809, Z: 3441}
	// ArdougneBank is thieverplan.ArdougneBank, where fleetrunner.PrepareKit banks for the lever.
	ArdougneBank = tile.Tile{X: 2655, Z: 3283}
	// DraynorBank is fleetplan.DraynorBank, the nearest bank to Betty.
	DraynorBank = tile.Tile{X: 3092, Z: 3245}
	// VarrockEastBank is thieverplan.VarrockEastBank, the nearest bank to Aubury.
	VarrockEastBank = tile.Tile{X: 3253, Z: 3420}
	// YanilleBank is thieverplan.YanilleBank, the nearest bank to the Wizards' Guild (its bankers
	// stand at 2615,3091-3094, about 20 tiles east of the guild's east door).
	YanilleBank = tile.Tile{X: 2612, Z: 3092}
	// DefaultMulePost is where the mule posts when no heartbeat says otherwise.
	DefaultMulePost = CatherbyBank
)

// ShopSite is one rune shop and how the buyer reaches it.
type ShopSite struct {
	Key  string
	Shop fleetplan.RuneShop
	// Stand is the keeper's own tile.
	Stand tile.Tile
	// Approach is where a station walks first: the keeper for Aubury and Betty, the East
	// Ardougne bank for Lundail (the lever trip starts with the kit there).
	Approach tile.Tile
	// Bank is the nearest bank for a coin top-up or a full pack; zero for Lundail, whose coins
	// come out at Gundai inside the Mage Bank.
	Bank     tile.Tile
	BankName string
	HoldTTL  time.Duration
	// Wilderness is true for the one site reached through the Wilderness.
	Wilderness bool
	// MinMagic is the CURRENT Magic level the site's door needs (0 for none).
	MinMagic int
	// Built is false for a site whose route this package does not plan.
	Built bool
}

// Site is the registry of the shops RuneRunner knows.
func Site(key string) (ShopSite, bool) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case strings.ToLower(SiteAubury):
		return ShopSite{Key: SiteAubury, Shop: fleetplan.Aubury, Stand: fleetplan.AuburyStand,
			Approach: fleetplan.AuburyStand, Bank: VarrockEastBank, BankName: "Varrock East",
			HoldTTL: fleetplan.AuburyHoldTTL, Built: true}, true
	case strings.ToLower(SiteBetty):
		return ShopSite{Key: SiteBetty, Shop: Betty, Stand: BettyStand, Approach: BettyStand,
			Bank: DraynorBank, BankName: "Draynor", HoldTTL: BettyHoldTTL, Built: true}, true
	case strings.ToLower(SiteLundail):
		return ShopSite{Key: SiteLundail, Shop: fleetplan.Lundail, Stand: fleetplan.LundailTile,
			Approach: ArdougneBank, HoldTTL: fleetplan.LundailHoldTTL, Wilderness: true, Built: true}, true
	case strings.ToLower(SiteWizardsGuild):
		// Yanille: the Magic Store owner (npc 461) stands upstairs at 2595,3087 behind the Magic
		// 66 door, and the webwalk graph carries both the gated door and the staircase, so a
		// plain station walk reaches him (fleetplan.WizardsGuild has the content facts). Pack-paid
		// like Aubury and Betty: coins and a full pack go through the Yanille bank.
		return ShopSite{Key: SiteWizardsGuild, Shop: fleetplan.WizardsGuild,
			Stand: fleetplan.WizardsGuildStand, Approach: fleetplan.WizardsGuildStand,
			Bank: YanilleBank, BankName: "Yanille",
			HoldTTL: fleetplan.WizardsGuildHoldTTL, MinMagic: fleetplan.WizardsGuildMagic, Built: true}, true
	}
	return ShopSite{}, false
}

// BuiltSites are the built sites open at any level (no door), in the order the settings default
// lists them: the three stores every runner works.
func BuiltSites() []string { return []string{SiteAubury, SiteBetty, SiteLundail} }

// DefaultSites are the shops setting's default: BuiltSites and the Wizards' Guild, which each
// round leaves out until the runner's Magic opens its door (EligibleShops).
func DefaultSites() []string { return append(BuiltSites(), SiteWizardsGuild) }

// Eligible reports whether an account at this CURRENT Magic level may use a site: its route is
// built and its door (if any) opens. The guild door reads stat(magic), the current level.
func Eligible(s ShopSite, magic int) bool {
	return s.Built && magic >= s.MinMagic
}

// EligibleShops is shops less every site an account at this CURRENT Magic level cannot use, in
// the same order: what a round is planned over. An unknown key is dropped too.
func EligibleShops(shops []string, magic int) []string {
	out := make([]string, 0, len(shops))
	for _, key := range shops {
		if s, ok := Site(key); ok && Eligible(s, magic) {
			out = append(out, key)
		}
	}
	return out
}

// CanonicalRune is a rune's spelling as the built shops stock it ("mind RUNE" -> "Mind rune"),
// false when none of them sells it.
func CanonicalRune(name string) (string, bool) {
	for _, key := range DefaultSites() {
		s, _ := Site(key)
		if l, ok := s.Shop.Line(name); ok {
			return l.Rune, true
		}
	}
	return "", false
}
