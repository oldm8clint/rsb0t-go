package fisherplan

import "strings"

// BassOysterCaskets is ProgressiveFisher narrowed to the Big net at the Fishing Guild, for the most
// Raw bass, Oyster pearls, Empty oysters and Caskets (and casket loot) per hour (owner, 2026-10-04).
// It is the same bot (internal/bot/fisher.go) with these rules switched on; everything else, the
// Guild, the world hop, the mule hand-off, random events and survival, is ProgressiveFisher's own.
//
// The catch table is content 289's [proc,fish_roll_big_net]
// (skill_fishing/scripts/fishing_spots/memberfish.rs2): Leather boots, Seaweed, Leather gloves,
// Raw mackerel (two rolls), Oyster, Casket, then Raw cod from Fishing 23 and Raw bass from 46. The
// display names are the obj configs' own: leather_boots and leather_gloves in
// skill_crafting/configs/leather/leather_gear.obj, the rest in skill_fishing/configs/fishing.obj
// (oystershell is "Oyster", smalloysterpearls "Oyster pearl", oysterempty "Empty oyster").

// BassOysterRung is the only rung the script fishes.
const BassOysterRung = "Big net"

// BassOysterTarget is the target the script runs with whatever the settings say: the Big net,
// pinned so Fishing 90 never climbs it to the shark harpoon (PinTarget), which catches none of
// the things this script is for.
func BassOysterTarget() string { return PinTarget(BassOysterRung) }

// BassOysterJunk is what is dropped rather than carried to the bank, so only the valuable catch
// fills the pack: the Big net's low-value catch and the Empty oyster an opened oyster leaves (owner,
// 6 Oct 2026: "they should also just drop the empty oyster to save inventory space also as empty
// oysters dont have a use"). Everything else the net brings up is kept: Raw bass, Oyster and Casket,
// and anything a future content change adds. The Empty oyster is also what keeps a dropped item off
// the Loot panel: the oyster card shows the pearls an opened oyster gave, never the shells thrown away.
var BassOysterJunk = []string{"Raw mackerel", "Raw cod", "Leather boots", "Leather gloves", "Seaweed", EmptyOyster}

// IsBassOysterJunk reports whether name is one of BassOysterJunk, ignoring case and spaces at the ends.
func IsBassOysterJunk(name string) bool {
	return hasName(BassOysterJunk, strings.TrimSpace(name))
}

// JunkDropFreeSlots is how few free pack slots trigger a drop. Dropping interrupts fishing (an
// OPHELD on the backpack clears the player's interaction: engine OpHeldHandler
// clearPendingAction), so junk is dropped in one burst when the pack is nearly full rather than
// after every catch: one re-click of the spot per pack of junk. Two rather than zero because one
// Big net roll can land more than one item (boots, seaweed and a mackerel together), and what does
// not fit falls on the floor (engine INV_ADD overflow).
//
// Four since 7 Oct 2026: with the fish manipulation landing several rolls a batch, 144 of 386 live drop
// cycles in an hour started at 0 free slots, so the catch overflowed onto the floor and content's
// "You can't carry any more fish." mesbox stopped the fishing before the drop (owner: "there's a delay
// from that display and anything happening after it").
const JunkDropFreeSlots = 4

// JunkDropDue reports whether the junk in the pack should be dropped now.
func JunkDropDue(freeSlots, junkSlots int) bool {
	return junkSlots > 0 && freeSlots <= JunkDropFreeSlots
}

// JunkSlots is the indexes into names (the pack's slots, in slot order, by display name; noted
// marks a bank note) that a drop takes: the ones isJunk names (IsBassOysterJunk for this script).
// A note is never dropped: it is a stack of something, not a catch.
func JunkSlots(names []string, noted []bool, isJunk func(name string) bool) []int {
	var out []int
	for i, n := range names {
		if i < len(noted) && noted[i] {
			continue
		}
		if isJunk(n) {
			out = append(out, i)
		}
	}
	return out
}

// HandoffPrep is one step of getting the bank ready for the mule: what the drop cycle (cycle.go) did
// not get to, a casket or an oyster banked unopened by an older run or caught since the last cycle.
type HandoffPrep string

const (
	// PrepOpenBanked opens every banked casket (and any oyster still closed), so the coins, gems,
	// talismans and key halves inside cross rather than the caskets.
	PrepOpenBanked HandoffPrep = "open banked caskets and oysters"
	// PrepJoinKeyHalves joins the key halves the caskets gave into crystal keys.
	PrepJoinKeyHalves HandoffPrep = "join key halves into crystal keys"
	// PrepTrade draws the load and trades it to the mule.
	PrepTrade HandoffPrep = "trade the haul to the mule"
)

// HandoffPrepOrder is the order a hand-off runs in (internal/bot/fisher.go's handoffStep follows
// it). Caskets first because opening them is what produces the key halves; the halves next, so
// two dead half rows cross as one usable key; the trade last, so it carries all of it.
func HandoffPrepOrder() []HandoffPrep {
	return []HandoffPrep{PrepOpenBanked, PrepJoinKeyHalves, PrepTrade}
}
