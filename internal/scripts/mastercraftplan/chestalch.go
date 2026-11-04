package mastercraftplan

// THE CHEST ROUND'S ALCHEMY AND THE GAMES ROOM ROUTE (a user request):
//
//	"Crystal chest round: after looting and banking keys, teleport to the Burthorpe Games Room with a
//	Games necklace and travel to the chest at Taverley from there (the Games Room exit stairs are now
//	catalogued: 4622 at 2207,4935 then 4627 at 2205,4935,1 to Burthorpe 2899,3564). High-alch chest loot
//	while the chest cannot be looted yet and while walking (run stall): cut diamonds, cut rubies, Adamant
//	sq shield, Rune platelegs; always carry noted rubies to alch on walks; wield a Staff of fire and carry
//	Air runes for teleports."
//
// THE CONTENT (content/scripts, branch 289):
//
//   - The chest (areas/area_taverly/scripts/crystal_chest.rs2) adds an uncut dragonstone every open and one
//     of: spinach roll and 2,000 coins; 50 each of six runes and 10 each of five; 2 rubies and 2 diamonds
//     (cut, unstackable: four slots); 3 runite bars; 750 coins and a key half; 150 iron ore or 100 coal as
//     notes; 5 raw swordfish and 1,000 coins; an Adamant sq shield (2/128); Rune platelegs (a male account)
//     or a Rune plateskirt (a female one), 1/128. The rubies, diamonds, shield and legs are the alchables.
//   - High Level Alchemy (skill_magic/scripts/spells/alchemy.rs2): Magic 55, one Nature rune and five Fire
//     runes (a worn Staff of fire pays the Fire: magic_staff.dbrow), 0.6 of the item's cost in coins
//     (max(scale(6, 10, cost), 1)), p_delay(3): a cast every five ticks. A bank note is alchable. It
//     replaces the item with coins, so each cut gem, shield or pair of legs cast away frees the pack slot
//     it held, and the chest, which only opens while the pack has room for the worst reward
//     (lootchestfisherplan.CanOpenWithKeyInHand), can be opened again at once instead of after a trip to
//     the Falador bank.
//   - The Games necklace (general/scripts/enchanted_jewellry/necklace_of_minigames.rs2): Rub (option 4)
//     opens "Where would you like to teleport to?" with "Burthorpe Games Rooms." and "Nowhere."; the first
//     teleports to map_findsquare(0_34_77_31_12), the room at 2207..2211,4936 (level 0) and uses a charge.
//     The only way out is the room's own stairs (the Guardian's catalogue, randomevents.strandsites.go,
//     "the Burthorpe Games Room"): 4622 at 2207,4935 up to the gallery, then the grey stairs 4627 at
//     2205,4935 (level 1) to Burthorpe at 2899,3564.
//
// THE ROUTE (planned over the real collision pack, mastercrafter_gamesroute_test.go): Burthorpe to the chest
// stand 141, the Falador west bank to it 159, the Falador Teleport's landing to it 144, the Catherby post
// to it 793 (seven hops, the ships). A leg from the post is the whole gain; from the Falador bank the
// necklace saves 18 tiles of walking against the two flights of stairs and a charge, which is why
// craftChestGamesRoute has a middle value ("post") as well as the owner's ("always").

import (
	"strings"
)

// The settings' names.
const (
	// KeyChestGamesRoute is the Games Room route: "always" (every leg to the chest: from the post and from
	// the Falador west bank, the owner's wording), "post" (only the long leg from the mule's post) or "off".
	KeyChestGamesRoute = "craftChestGamesRoute"
	// KeyChestAlch is the chest round's High Level Alchemy: the loot at the chest while it cannot be opened
	// for want of room, and the run staller on its walks.
	KeyChestAlch = "craftChestAlch"
)

func init() {
	knownKeys[KeyChestGamesRoute] = true
	knownKeys[KeyChestAlch] = true
}

// The values of craftChestGamesRoute.
const (
	GamesRouteAlways = "always"
	GamesRoutePost   = "post"
	GamesRouteOff    = "off"
)

// GamesRoutes lists the values.
func GamesRoutes() []string { return []string{GamesRouteAlways, GamesRoutePost, GamesRouteOff} }

// The alchemy's numbers.
const (
	// ChestAlchMagic is High Level Alchemy's Magic level (alchplan.HighAlch).
	ChestAlchMagic = 55
	// ChestNaturesCarry is the Nature runes a visit draws: a cast a pack slot of loot and one every five
	// ticks of a walk, about thirty a trip.
	ChestNaturesCarry = 40
	// ChestFodderCarry is the noted rubies (or diamonds) a visit draws to alch on the walks: one slot,
	// any count.
	ChestFodderCarry = 60
	// ChestFodderAsk is how many of them the round's ask takes from the mule, as notes.
	ChestFodderAsk = 400
	// ChestNaturesAsk is the Nature runes the round's ask takes from the mule.
	ChestNaturesAsk = 2000
	// ChestNecklaceCarry is the Games necklaces a visit carries: one, for the route and the run staller.
	ChestNecklaceCarry = 1
)

// ChestAlchNames are what the round alchs, best first: the chest's cut gems, shield and legs (the
// plateskirt a female account's) and nothing else. The noted rubies it carries are the same names.
func ChestAlchNames() []string {
	return []string{"Rune platelegs", "Rune plateskirt", "Adamant sq shield", "Diamond", "Ruby"}
}

// ChestFodderNames are the gems carried as notes for the walks: the owner's "always carry noted rubies to alch on
// walks" (a Diamond costs twice a Ruby and is alched only as the chest's own loot).
func ChestFodderNames() []string { return []string{"Ruby"} }

// ChestAlchable reports whether the item's name is one the chest round alchs.
func ChestAlchable(name string) bool {
	for _, n := range ChestAlchNames() {
		if strings.EqualFold(strings.TrimSpace(name), n) {
			return true
		}
	}
	return false
}

// ChestAlchOK reports whether the account can alch at all: Magic 55, the setting on.
func ChestAlchOK(set Settings, magic int) bool { return set.ChestAlch && magic >= ChestAlchMagic }

// ChestRoomWithAlch is ChestRoom if every alchable loot item in the pack were cast away: each cast
// empties the slot it was in (the cut gems, the shield and the legs do not stack, and the coins
// join the stack already there).
func ChestRoomWithAlch(free, alchables int, held func(id int) int) bool {
	return ChestRoom(free+max(0, alchables), held)
}

// GamesRouteFrom reports whether a leg to the chest that starts at where takes the Games Room route:
// the setting allows it for that place (set.ChestGamesRoute), the account is not already past the
// teleport (anywhere but the post and the Falador west bank walks), and a Games necklace is held.
func GamesRouteFrom(mode string, where ChestPlace, haveNecklace bool) bool {
	if !haveNecklace {
		return false
	}
	switch mode {
	case GamesRouteAlways:
		return where == ChestAtPost || where == ChestAtFalador
	case GamesRoutePost:
		return where == ChestAtPost
	}
	return false
}

// ChestKitOptions are what the settings add to a bank visit's kit.
type ChestKitOptions struct {
	// Alch carries the Nature runes and the noted gems of the walks (Magic 55 and craftChestAlch).
	Alch bool
	// Route carries a Games necklace (craftChestGamesRoute not off).
	Route bool
	// PostRoute says the leg from the post takes the Games Room route (the setting is not off), so the keys
	// ride from the post whether or not a teleport is castable.
	PostRoute bool
}

// ChestKitWantFor is ChestKitWant with the extras the options add. The Games necklace and the alchemy kit
// are drawn only for the extras the ids name (a bank without them draws nothing).
func ChestKitWantFor(opt ChestKitOptions, atPost bool, magic int, paid map[string]bool, expired bool, banked int) KitWant {
	w := ChestKitWant(atPost, magic, paid, expired, banked)
	if opt.PostRoute && atPost && !expired && w.Keys == 0 {
		// The long walk from the post is the Games Room route's flight: the keys ride with it.
		w.Keys = ChestKeysToDraw(false, magic, banked, 0)
	}
	w.Extras = map[string]int{}
	if opt.Route {
		w.Extras[GamesNecklaceKit] = ChestNecklaceCarry
	}
	if opt.Alch {
		w.Extras[NatureKit] = ChestNaturesCarry
		for _, name := range ChestFodderNames() {
			w.Extras[name] = ChestFodderCarry
		}
	}
	return w
}

// The extras' names in the kit.
const (
	GamesNecklaceKit = "Games necklace"
	NatureKit        = "Nature rune"
	WeaponKit        = "weapon"
)

// ChestExtrasFor are the kit's extras as ExtraKit rows, from the cache's ids: nature runes, a Games
// necklace (any charge), the gems as notes (the bank rows' unnoted ids drawn as notes, the notes' ids
// recognised in the pack) and the weapon a Staff of fire replaced. A row with no ids is left out.
func ChestExtrasFor(natures, necklaces []int, gems map[string][]int, notes map[string][]int, weapon int) []ExtraKit {
	var out []ExtraKit
	if len(natures) > 0 {
		out = append(out, ExtraKit{Name: NatureKit, Bank: natures, Pack: natures, Carry: ChestNaturesCarry})
	}
	if len(necklaces) > 0 {
		out = append(out, ExtraKit{Name: GamesNecklaceKit, Bank: necklaces, Pack: necklaces, Carry: ChestNecklaceCarry})
	}
	for _, name := range ChestFodderNames() {
		if len(gems[name]) > 0 && len(notes[name]) > 0 {
			out = append(out, ExtraKit{Name: name, Bank: gems[name], Pack: notes[name], Noted: true, Carry: ChestFodderCarry, Group: "fodder"})
		}
	}
	if weapon > 0 {
		out = append(out, ExtraKit{Name: WeaponKit, Bank: []int{weapon}, Pack: []int{weapon}, Carry: 1, Keep: true})
	}
	return out
}

// ChestAskFor is ChestAsk with the lines the options add: the Games necklace of the route and the
// alchemy kit, each only for what the account's own bank is short of. own is how many of an item this
// account's bank holds, by name.
func ChestAskFor(opt ChestKitOptions, stock Stock, magic int, paid map[string]bool, own func(item string) int) []Line {
	lines := ChestAsk(stock, magic, paid, own)
	if own == nil {
		own = func(string) int { return 0 }
	}
	if opt.Route {
		held := 0
		for _, name := range KindNecklace.DemandNames() {
			held += own(name)
		}
		lines = append(lines, ChestRubAsk(stock, held)...)
	}
	if opt.Alch {
		lines = append(lines, ChestAlchAsk(stock, own)...)
	}
	return lines
}

// ChestRubAsk is the mule line for the Games necklace the round carries: one of the charge the mule holds
// the most of, when the account's own bank holds none (ownRub: how many of the 8 charge names it holds).
func ChestRubAsk(stock Stock, ownNecklaces int) []Line {
	if ownNecklaces > 0 {
		return nil
	}
	best, bestN := "", 0
	for _, name := range KindNecklace.DemandNames() {
		if n := stockOf(stock, name); n > bestN {
			best, bestN = name, n
		}
	}
	if best == "" {
		return nil
	}
	return []Line{{Item: best, Qty: 1}}
}

// ChestAlchAsk are the mule lines for the alchemy kit: Nature runes up to ChestNaturesAsk less what the
// own bank holds, and the noted gems (ChestFodderAsk) the mule publishes, when the own bank holds fewer than
// four visits carry. The gems only against Nature runes to cast them: the own bank holds some or this ask takes
// some from the mule (a user request: "if accounts dont have nature runes they shouldnt take things to alch");
// with none anywhere the ask is for nothing.
func ChestAlchAsk(stock Stock, own func(item string) int) []Line {
	if own == nil {
		own = func(string) int { return 0 }
	}
	var lines []Line
	natures := own(NatureKit) > 0
	if q := min(stockOf(stock, NatureKit), ChestNaturesAsk-own(NatureKit)); q > 0 && own(NatureKit) < ChestNaturesCarry*4 {
		lines = append(lines, Line{Item: NatureKit, Qty: q})
		natures = true
	}
	if !natures {
		return lines
	}
	for _, name := range ChestFodderNames() {
		have := own(name)
		if have >= ChestFodderCarry*4 {
			return lines
		}
		if q := min(stockOf(stock, name), ChestFodderAsk-have); q > 0 {
			lines = append(lines, Line{Item: name, Qty: q})
			break
		}
	}
	return lines
}
