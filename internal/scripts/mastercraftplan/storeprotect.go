package mastercraftplan

import (
	"strings"
	"sync"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/consolidateplan"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// WHAT THE STORAGE ROUND NEVER MOVES (the plan: "anything a fleet script asks the mule for or
// MasterCrafter uses"). No package exports "the items the fleet asks the mule for", so
// StoreProtected says yes to a name when ANY of these holds, each read from a definition the scripts
// themselves use, not from a list kept here:
//
//   - Coins, every rune (a name ending " rune": the ScarabSwarm kits' spell, teleport and alchemy
//     runes, the mule's own stock) and every food (fighterfood.BaseFood, which also reads a partial
//     cake or pie as its cake and so covers every rung of scarabplan.FoodLadderFor);
//   - what consolidateplan.Classify keeps on a ScarabSwarm account whatever its bank holds, the
//     loot hand-off's own kit-versus-loot verdict: any staff, the tools and kit pieces (KitItems:
//     Rope, Shantay pass, the waterskins, Knife, Tinderbox, Hammer, Chisel, buckets, pickaxes, axes
//     ...), raw food, the alchemy stock (scarabplan.DefaultAlchItems, the bows ScarabSwarm's Magic
//     training casts High Alchemy on and asks the mule for) and every item a quest kit in questplan,
//     questmods or waterfallplan names (QuestItems, "read from their KitLine names"; the real names
//     are literals in unexported functions of about twenty files and exist nowhere else, so the hand
//     copy is the only derivation there is);
//   - the mule's own hand-out catalogue (orderplan.DefaultCatalog: the fishing tools, a knife, Coins,
//     Crystal key, every axe) and what its open path never serves (orderplan.NeverOpen);
//   - what the other scripts ask it for by constant: every fishing rung's tool (fisherplan), every
//     axe and the Knife (woodcutplan), Flax (crafterplan), the Strange fruit Stage 5 eats, the Rope,
//     Shantay pass and Waterskin(4) of the Kalphite kit, the raw and cooked fish of the self-cook
//     ladder;
//   - MasterCrafter's own items: the gems (uncut, cut and crushed) and the chisel, the bows and the
//     Bow string, the pearls, tips, Bolts and Pearl bolts, the bars and the ores, coal and runes that
//     make them, the Staff of fire, the arrow parts and arrows, the grimy herbs (every one is named
//     "Herb", published again as "Herb idNNN") and the clean ones, the Crystal key. A published
//     per-obj row ("Yew shortbow id857") is protected exactly when the name it is a row of is.
//
// The bot adds what only a runner package knows: the mule's own jobs (the caskets and oysters its
// idle openables pass opens, fisherrunner.IsOpenable).
//
// What is left, at the 5 Oct 2026 the mule, is gear, jewellery, kitchen and quest leftovers that no
// kit names: Mime top, Dragon longsword, Steel battleaxe, Rune scimitar, Amulet of glory, Ruby ring,
// Snape grass, Beer glass, Eye patch, Doogle leaves ... StoreProtected is a name test and does not
// know a script nobody has written yet, or a kit item a roster setting adds (stage3AlchItems,
// handoutItems); the hand-out record (Context.MuleOut) is the second net, and the round moves
// nothing the mule handed out in the quiet days.

// StoreProtected reports whether the storage round must leave an item of this name in the mule's
// bank. An empty name is protected.
func StoreProtected(item string) bool {
	n := strings.TrimSpace(item)
	l := strings.ToLower(n)
	switch {
	case l == "":
		return true
	case strings.HasSuffix(l, " rune"):
		return true
	case storeProtectedNames()[l]:
		return true
	case fleetplan.IsFilledWaterskin(n):
		return true
	}
	if base, _, ok := cachecfg.SplitQualified(n); ok {
		// A per-obj row of the mule's published bank ("Herb id199" a grimy herb, "Yew shortbow id857" a
		// strung bow, which peerrunner.IDKeyedNames publishes besides the merged row): protected when the
		// name it is a row of is.
		return StoreProtected(base)
	}
	if _, food := fighterfood.BaseFood(n); food {
		return true
	}
	kind, _ := consolidateplan.Classify(n, storeKit())
	return kind != consolidateplan.KindLoot
}

// storeKit is the loot hand-off's account reading for the question "is this item kit?": the quest
// ladder is taken as pending (its items are kept), clue scrolls as loot, and the names ScarabSwarm
// keeps whatever the settings (consolidateplan.Defaults().KeepItems) plus the alchemy stock are on
// the keep list. Built once.
var storeKit = sync.OnceValue(func() consolidateplan.KitInput {
	keep := append([]string(nil), consolidateplan.Defaults().KeepItems...)
	keep = append(keep, fleetplan.DefaultAlchItems...)
	return consolidateplan.KitInput{Keep: keep, QuestsPending: true, LootClues: true}
})

// storeProtectedNames is the lower-case names the catalogue, the other scripts' constants and
// MasterCrafter's own plans give, built once.
var storeProtectedNames = sync.OnceValue(func() map[string]bool {
	m := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
				m[n] = true
			}
		}
	}
	// The mule's catalogue and what its open path keeps back.
	for _, h := range orderplan.DefaultCatalog() {
		add(h.Item)
	}
	add(orderplan.NeverOpen()...)
	// What the other scripts ask for by constant.
	add(fisherplan.ToolNames()...)
	add(woodcutplan.Axes...)
	add(woodcutplan.Knife, crafterplan.Flax, fleetplan.StrangeFruit,
		fleetplan.RopeItem, fleetplan.ShantayPassItem, fleetplan.WaterskinItem)
	add(fleetplan.FoodLadderFor(99)...)
	add(fleetplan.DefaultAlchItems...)
	for _, f := range fleetplan.CookFishes {
		add(f.Raw, f.Cooked)
	}
	// Gems: the uncut and the cut kinds, the chisel, what a failed cut leaves.
	for _, g := range gemplan.Gems {
		add(g.Name, g.UncutName)
	}
	add(gemplan.Chisel, gemplan.CrushedGemstone)
	// Bows and the string.
	for _, b := range bowstringplan.Bows {
		add(b.Name)
	}
	add(bowstringplan.BowString)
	// Pearls, tips, Bolts and Pearl bolts.
	add(pearlplan.Pearl, pearlplan.Pearls, pearlplan.Tips, pearlplan.Bolt, pearlplan.PearlBolt)
	// The superheat round: the bars, their ores, coal, the runes (by suffix) and the staff.
	for _, b := range Bars {
		add(b.Name, b.Ore, b.Second)
	}
	add(Coal, StaffOfFire)
	// The arrow round.
	add(ArrowShaft, Feather, Headless, ArrowTips, SteelArrow)
	// The anvil round: every arrowtip it makes, the Iron platebody (also on the alch list) and the hammer.
	for _, p := range SmithAllProducts() {
		add(p.Name, p.Bar)
	}
	add(Hammer)
	// The herb round: the grimy ones all share one name, and the clean ones are its product.
	add("Herb")
	for _, h := range herbplan.Herbs {
		add(h.Name)
	}
	// The crystal chest round.
	add(ChestKeyItem)
	return m
})
