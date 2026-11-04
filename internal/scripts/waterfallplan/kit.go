package waterfallplan

import (
	"fmt"
	"sort"
	"strings"
)

// Trip is which loadout a bank visit prepares.
type Trip int

const (
	// TripTomb covers stages 0 to 3: Almera, the raft, the book, the Tree Gnome Village dungeon
	// and Glarial's tomb, on ONE bank visit. It is tomb-safe (see TombSafe), because a pack that
	// is not is refused at the tombstone with "But nothing happens." — after the account has
	// walked all the way there.
	TripTomb Trip = iota
	// TripDungeon covers stage 4 onward: the rope, the eighteen puzzle runes, the amulet in the
	// pack, the full urn and the dungeon's food.
	TripDungeon
)

func (t Trip) String() string {
	if t == TripTomb {
		return "tomb trip"
	}
	return "dungeon trip"
}

// Loadout is what a trip must carry.
type Loadout struct {
	Trip Trip
	// Required must be in the pack, by exact object id, before the trip starts.
	Required map[int]int
	// Optional is taken from the bank when it is there (a quest item a previous run banked) and
	// allowed in the pack, but never waited for.
	Optional map[int]int
	// Food is how many food items to draw; FoodMin is the fewest the trip may start with.
	Food, FoodMin int
	// TombSafe requires TombSafe(pack, worn) as well.
	TombSafe bool
}

// PuzzleRuneCount is how many of each rune the dungeon trip carries: one per pillar. A pair the
// varp already holds consumes nothing, so six always covers every pillar.
const PuzzleRuneCount = PillarCount

// TombLoadout is the loadout for stages 0-3 and for recovering lost relics at any later stage.
func TombLoadout(stage Stage, set Settings) Loadout {
	l := Loadout{
		Trip:     TripTomb,
		Required: map[int]int{},
		// Anything the quest has already handed over and a previous pass banked comes back out,
		// so a restart never repeats the Tree Gnome Village leg for a pebble it owns.
		Optional: map[int]int{ItemPebble: 1, ItemGolrieKey: 1, ItemAmulet: 1, ItemFullUrn: 1, ItemRope: 1},
		Food:     set.TombFood,
		FoodMin:  set.TombFoodMin,
		TombSafe: true,
	}
	if stage < StageReadBook {
		// Almera, the raft, Hudon and the bookcase need no food; the kit still draws it for
		// the Tree Gnome Village leg that follows, but a short larder does not hold these up.
		l.FoodMin = 0
	}
	if stage == StageSpokenToHudon {
		l.Optional[ItemBook] = 1
	}
	return l
}

// DungeonLoadout is the loadout for stage 4 onward.
func DungeonLoadout(stage Stage, set Settings) Loadout {
	l := Loadout{
		Trip:     TripDungeon,
		Required: map[int]int{ItemRope: 1, ItemAmulet: 1, ItemFullUrn: 1},
		Optional: map[int]int{ItemBaxKey: 1},
		Food:     set.DungeonFood,
		FoodMin:  set.DungeonFoodMin,
	}
	if stage < StageRaisedFloor {
		for _, r := range PuzzleRunes {
			l.Required[r] = PuzzleRuneCount
		}
	}
	return l
}

// tombAllowed is every object the tomb trip may carry besides food: what waterfall_can_enter_
// glarials_tomb does not refuse and the quest actually uses. The content proc is a DENYLIST of
// categories (armour, every weapon category, arrows, runes, leathers, logs, bows, clue scrolls,
// cannon parts, ...), and categories are server-side config the client cannot read, so this is
// an ALLOWLIST instead: stricter than the server, never looser.
var tombAllowed = map[int]bool{
	ItemCoins: true, ItemRope: true, ItemBook: true, ItemGolrieKey: true, ItemPebble: true,
	ItemAmulet: true, ItemFullUrn: true, ItemEmptyUrn: true, ItemBaxKey: true,
}

// TombSafe reports whether a pack and a worn set would be let into Glarial's tomb, and why not.
// Nothing may be worn (content allows boots and amulets, but a strict rule costs one extra
// deposit and can never be wrong); the pack may hold only tombAllowed and food.
func TombSafe(inv, worn map[int]int, isFood func(id int) bool) (bool, string) {
	for id, n := range worn {
		if n > 0 {
			return false, fmt.Sprintf("wearing obj %d", id)
		}
	}
	for _, id := range sortedIDs(inv) {
		if inv[id] <= 0 || tombAllowed[id] || (isFood != nil && isFood(id)) {
			continue
		}
		return false, fmt.Sprintf("carrying obj %d", id)
	}
	return true, ""
}

// FoodHeal is each food's healing in hitpoints (content's), a cake counted whole (three bites of
// four). Food is sized by healing, not by count (the lead's ruling after the eighth EMPTY run):
// a cake in one slot is worth two and a bit breads.
var FoodHeal = map[int]int{385: 20, 373: 14, 379: 12, 365: 13, 361: 10, 329: 9, 1891: 12, 1893: 8, 1895: 4, 333: 7, 2309: 5, 2142: 3}

// FoodUnit is the healing one food unit stands for: a bread's 5. Loadout.Food and FoodMin, and
// the top-ups, are in these units.
const FoodUnit = 5

// FoodUnits is a pack's food healing in FoodUnits (rounded down).
func FoodUnits(inv map[int]int, isFood func(id int) bool) int {
	if isFood == nil {
		return 0
	}
	hp := 0
	for id, c := range inv {
		if c > 0 && isFood(id) {
			h := FoodHeal[id]
			if h == 0 {
				h = FoodUnit
			}
			hp += h * c
		}
	}
	return hp / FoodUnit
}

// ItemsFor is how many of food id make up units FoodUnits (rounded up).
func ItemsFor(id, units int) int {
	h := FoodHeal[id]
	if h == 0 {
		h = FoodUnit
	}
	return (units*FoodUnit + h - 1) / h
}

// FoodCount is how many food items a pack holds.
func FoodCount(inv map[int]int, isFood func(id int) bool) int {
	if isFood == nil {
		return 0
	}
	n := 0
	for id, c := range inv {
		if c > 0 && isFood(id) {
			n += c
		}
	}
	return n
}

// Ready reports whether a pack satisfies the loadout, and the first reason it does not.
func (l Loadout) Ready(inv, worn map[int]int, isFood func(id int) bool) (bool, string) {
	for _, id := range sortedIDs(l.Required) {
		if inv[id] < l.Required[id] {
			return false, fmt.Sprintf("needs %d x obj %d (holds %d)", l.Required[id], id, inv[id])
		}
	}
	if food := FoodUnits(inv, isFood); food < l.FoodMin {
		return false, fmt.Sprintf("needs %d food units of %d hp (holds %d)", l.FoodMin, FoodUnit, food)
	}
	if l.TombSafe {
		if ok, why := TombSafe(inv, worn, isFood); !ok {
			return false, "not tomb-safe: " + why
		}
	}
	return true, ""
}

// ItemCount is one line of a withdrawal or a shortfall.
type ItemCount struct {
	ID    int
	Count int
}

// KitOrder is what one bank visit draws, once the pack and the worn slots are empty.
type KitOrder struct {
	// Withdraw is every Required and Optional line the bank can cover, by exact id.
	Withdraw []ItemCount
	// Missing is every Required line the bank cannot cover, with the shortfall.
	Missing []ItemCount
	// Food is how many food items to draw from whatever food the bank holds.
	Food int
	// FoodShort is how many food items the bank cannot cover below the loadout's Food.
	FoodShort int
}

// PlanKit decides one bank visit from the bank's contents, assuming the pack and the worn slots
// are deposited first (the runner does that: an exact loadout is only reachable from empty,
// because the bank's withdraw buttons cannot take a partial stack back out of the pack).
// bankFood is how many food items the bank holds across every food.
func PlanKit(l Loadout, bank map[int]int, bankFood int) KitOrder {
	var o KitOrder
	for _, id := range sortedIDs(l.Required) {
		want := l.Required[id]
		have := bank[id]
		if have >= want {
			o.Withdraw = append(o.Withdraw, ItemCount{ID: id, Count: want})
			continue
		}
		if have > 0 {
			o.Withdraw = append(o.Withdraw, ItemCount{ID: id, Count: have})
		}
		o.Missing = append(o.Missing, ItemCount{ID: id, Count: want - have})
	}
	for _, id := range sortedIDs(l.Optional) {
		if _, dup := l.Required[id]; dup {
			continue
		}
		if n := min(l.Optional[id], bank[id]); n > 0 {
			o.Withdraw = append(o.Withdraw, ItemCount{ID: id, Count: n})
		}
	}
	o.Food = min(l.Food, bankFood)
	if o.Food < 0 {
		o.Food = 0
	}
	o.FoodShort = l.Food - o.Food
	if o.FoodShort < 0 {
		o.FoodShort = 0
	}
	return o
}

// Describe is the log form of a shortfall list.
func Describe(items []ItemCount) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%d %s", it.Count, ItemName(it.ID)))
	}
	return strings.Join(parts, ", ")
}

// ItemName names the objects this quest handles, for the log and for a mule ask (which is by
// display name).
func ItemName(id int) string {
	switch id {
	case ItemBook:
		return "Book on baxtorian"
	case ItemGolrieKey, ItemBaxKey:
		return "A key"
	case ItemPebble:
		return "Glarial's pebble"
	case ItemAmulet:
		return "Glarial's amulet"
	case ItemFullUrn, ItemEmptyUrn:
		return "Glarial's urn"
	case ItemAirRune, ItemEarthRune, ItemWaterRune:
		return RuneName(id)
	case ItemRope:
		return "Rope"
	case ItemCoins:
		return "Coins"
	case ItemShark:
		return "Shark"
	}
	return fmt.Sprintf("obj %d", id)
}

// Buyable reports whether a missing item has a shop or mule source (the quest's own items are
// only ever collected in the quest).
func Buyable(id int) bool {
	switch id {
	case ItemRope, ItemAirRune, ItemEarthRune, ItemWaterRune:
		return true
	}
	return false
}

func sortedIDs(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}
