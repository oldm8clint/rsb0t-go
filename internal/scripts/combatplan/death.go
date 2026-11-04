package combatplan

import "sort"

// Death: what the account keeps, where it wakes up, and what it has to put back on.
//
// Every rule here is read out of content/scripts/player/scripts/death.rs2, not from memory of
// the live game:
//
//   - [proc,player_death_lose_items] calls ~move_priciest_item_on_hero_to_death three times
//     when the player is NOT skulled, so three items survive, chosen by price.
//   - The Protect Item prayer (%prayer8) moves a fourth. A skulled player with Protect Item
//     therefore keeps exactly one.
//   - The respawn is p_teleport(map_findsquare(0_50_50_21_18, ...)), which is map square
//     50,50 plus offset 21,18 — tile 3221,3218, Lumbridge.
//   - ~prayer_deactivate_all runs on death, so every prayer is off afterwards. A script that
//     re-enters combat expecting Protect from Melee still on would be wrong.
//   - The whole loss step is skipped for staff: `if (staffmodlevel > 1 & map_live = true)
//     { return; }`. A local test engine that grants staffModLevel 2 can therefore drop
//     NOTHING on death, which makes a local "did we lose our gear" proof unreliable unless
//     map_live is false there — worth knowing before trusting a local re-gear test.

// LumbridgeRespawnX and LumbridgeRespawnZ are where death puts the player, from death.rs2's
// own map_findsquare(0_50_50_21_18): 50*64+21 and 50*64+18.
const (
	LumbridgeRespawnX     = 3221
	LumbridgeRespawnZ     = 3218
	LumbridgeRespawnLevel = 0
)

// KeptUnskulled is how many items death.rs2 saves for an unskulled player.
const KeptUnskulled = 3

// ValuedItem is one held item and the price death sorts by.
type ValuedItem struct {
	Name string
	// Value is the price the server would compare. A caller without real prices should
	// pass its own ordering and treat the result as an estimate, not a promise.
	Value int
}

// KeptOnDeath returns the items the player keeps, highest value first, and the ones lost.
//
// skulled drops everything but a Protect Item save; protectItem adds exactly one more slot,
// matching death.rs2's fourth ~move_priciest_item_on_hero_to_death call.
func KeptOnDeath(items []ValuedItem, skulled, protectItem bool) (kept, lost []ValuedItem) {
	keep := KeptUnskulled
	if skulled {
		keep = 0
	}
	if protectItem {
		keep++
	}

	ordered := append([]ValuedItem(nil), items...)
	// Stable, so equal-value items keep the caller's order rather than an arbitrary one.
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Value > ordered[j].Value })

	if keep > len(ordered) {
		keep = len(ordered)
	}
	return ordered[:keep], ordered[keep:]
}

// ReGearStep is one action the re-gear plan asks for.
type ReGearStep struct {
	// Item is the item's display name.
	Item string
	// Have is true when it is already somewhere the account can reach right now (its pack
	// or what it is still wearing), so the step is just "put it back on".
	Have bool
}

// ReGearPlan works out what to put back on after a death, and what is genuinely gone.
//
// wanted is the loadout the script wants to be wearing. worn is what survived on the body,
// available is what the account can still reach without shopping (its pack, and whatever a
// caller chooses to include from the bank). Everything in wanted that is neither worn nor
// available is returned as missing — the list a caller turns into a shop trip or a mule
// request, which is milestone 2's job, not this function's.
func ReGearPlan(wanted, worn, available []string) (equip []ReGearStep, missing []string) {
	isWorn := toSet(worn)
	pool := countOf(available)

	for _, item := range wanted {
		if isWorn[lower(item)] {
			continue // already on the body; death left it alone
		}
		key := lower(item)
		if pool[key] > 0 {
			pool[key]--
			equip = append(equip, ReGearStep{Item: item, Have: true})
			continue
		}
		missing = append(missing, item)
	}
	return equip, missing
}

// LostItems returns everything that was worn or carried before death and is not held after —
// the honest "what did this death actually cost" list, computed from two observations rather
// than from the rules, so it stays right even where the rules do not apply (a staff account
// on a local engine loses nothing at all).
func LostItems(before, after []string) []string {
	remaining := countOf(after)
	var lost []string
	for _, item := range before {
		key := lower(item)
		if remaining[key] > 0 {
			remaining[key]--
			continue
		}
		lost = append(lost, item)
	}
	return lost
}

// AtRespawn reports whether a tile is the Lumbridge respawn, within slack tiles. The server
// places the player on the first walkable tile near the square, so an exact match is the
// wrong test.
func AtRespawn(x, z, level, slack int) bool {
	if level != LumbridgeRespawnLevel {
		return false
	}
	dx := x - LumbridgeRespawnX
	if dx < 0 {
		dx = -dx
	}
	dz := z - LumbridgeRespawnZ
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx <= slack
	}
	return dz <= slack
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[lower(it)] = true
	}
	return out
}

func countOf(items []string) map[string]int {
	out := make(map[string]int, len(items))
	for _, it := range items {
		out[lower(it)]++
	}
	return out
}

// lower is a local ASCII lower-caser, so item-name matching is case-insensitive without
// pulling the whole strings package in for one call.
func lower(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
