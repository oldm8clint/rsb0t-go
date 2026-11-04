package trawlerplan

// THE CATCH: the anchor's claim at the net, and the crew's pickup of what the claim dumps.
//
// Content 289 (game_trawler/scripts/trawler_win.rs2): Inspect on trawlernetland (2166) at 2666,3167 rolls the whole share into
// trawler_rewardinv (40 slots, every obj stacks) and opens trawler_catch (interface 4564). Its inv component (4640) offers
// "Withdraw 1" (inv_button1: one of the obj into the pack) and "Withdraw All" (inv_button2: inv_movefromslot, the whole stack;
// what the pack cannot hold is dropped at the player's feet). Closing the window ("Dump Items", or any close) drops every
// stack left with inv_dropslot at the player's coord.
//
// WHO CAN SEE THE FLOOR (engine 289, World.addObj and Obj.turn): an obj dropped to a receiver is revealed to everyone after
// Obj.REVEAL = 100 ticks. The close's inv_dropslot drops for lootdrop_duration/2 = 100 ticks, so the reveal and the despawn
// fall on the same tick and nobody but the anchor ever sees that dump. Withdraw All's overflow (INV_MOVEFROMSLOT) drops each
// fish singly for 200 ticks: private for the first minute, then on the pier for everyone for a minute. So the claim spills
// every fish stack with Withdraw All (PlanClaim) and the crew's pickup below takes them to the bank (live 4 Nov 2026 00:12:
// the old claim and relay saved 24 of 901 fish; the rest were dumped privately and despawned).
//
// A ZONE HOLDS 129 DROPS (engine 289, Zone.OBJS = (8 * 8 << 1) + 1; ZoneDrops): each drop past that removes the zone's oldest,
// so of the thousands of items a spill drops only the last 129 stay. The first spill (live 4 Nov 2026 04:19, 689 fish) clicked the best
// stack first and left the junk for the close, whose drops then removed every fish: no crew member picked one up. So the
// junk goes first and the fish stacks last, cheapest first, and the 129 left on the pier are the best of what master leaves.

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The catch's ids (content 289).
const (
	LocNetLand        = 2166 // trawlernetland, op "Inspect"
	CatchInterface    = 4564 // trawler_catch
	CatchInvComponent = 4640 // trawler_catch:inv
	// CatchWithdrawOne and CatchWithdrawAll are the inv component's options (inv_button1, inv_button2).
	CatchWithdrawOne = 1
	CatchWithdrawAll = 2
)

// NetLand is the net the catch is claimed at.
var NetLand = tile.Tile{X: 2666, Z: 3167, Level: 1}

// PickupRadius is how far from the net a crew member looks for the dumped fish.
const PickupRadius = 8

// LootStand is where Looter accounts wait by the pier for the spilled catch (a user request: "have master pick up the best
// loot first, dropping on the ground for the next account to pick up").
var LootStand = tile.Tile{X: 2665, Z: 3162, Level: 0}

// LooterShare is the share (percent) of the crew, picked by name, that never sails and takes the spilled catch to the bank. 5 since
// live 4 Nov 2026, 19:17-19:19: at 2 five accounts took a pile of some 129 fish, most with 3 or 4 free slots, and 88 fish were
// left to despawn ("pack full: 88 dumped fish left on the floor"). A looter is not free: trawler_catch_fish rolls once per player
// aboard, so each one ashore costs the anchor about 20 fish a game its watch sails (some 10 a game for 19 looters over two
// watches, about 6% of the catch). The off watch cannot do it instead: at a claim it is still walking back from the end-of-game
// landing, and the boarding watch sails before the spill shows. 6 for the owner's "hold back 20 accounts from next round
// to loot the fish dropped to the ground while master kicks off another trawler game" (4 Nov 2026): 19 of the 343 crew of 19:00.
const LooterShare = 6

// LooterKit reports whether obj id is trawler kit a looter banks: Swamp paste, Rope and the bailing buckets are for the boat,
// which a looter never boards, and their slots are the catch's (a user request: "optimize the looting portion"; live
// 19:17-19:19 the three looters picked with 16 to 22 slots free).
func LooterKit(id int) bool {
	return id == PasteID || id == RopeID || id == BucketID || id == BucketFullID
}

// FishRank orders the trawler fish a crew member picks up first, dearest first: the floor holds more than the pickers'
// packs (live 4 Nov 2026: "pack full: 88 dumped fish left"), so what is left should be the cheap ones.
func FishRank(id int) int {
	for i, f := range []int{389, 395, 383, 371, 377, 359, 327, 321, 317} {
		if f == id {
			return i
		}
	}
	return 99
}

// Looter reports whether the crew member named name is a looter (a stable pick by name, apart from the watch pick).
func Looter(name string) bool {
	h := uint32(2166136261)
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		h = (h ^ uint32(c)) * 16777619
	}
	return int(h%100) >= 100-LooterShare
}

// Banks are the two banks a crew member or the anchor takes the catch to: East Ardougne's south bank and Yanille's.
// Catherby (2809,3440) is there for the anchor, which starts a round from the mule's post: live 2 Nov 2026 a door at the
// Catherby bank failed its walk to Ardougne and cost the block a 30-minute backoff.
var Banks = []tile.Tile{{X: 2655, Z: 3286, Level: 0}, {X: 2613, Z: 3094, Level: 0}, {X: 2809, Z: 3440, Level: 0}}

// NearestBank is the bank nearest t.
func NearestBank(t tile.Tile) tile.Tile {
	best := Banks[0]
	for _, b := range Banks[1:] {
		if tile.Chebyshev(t, b) < tile.Chebyshev(t, best) {
			best = b
		}
	}
	return best
}

// fishIDs are the raw fish trawler_reward rolls (not the junk, the seaweed or the oyster shells).
var fishIDs = map[int]bool{
	317: true, // raw_shrimp
	321: true, // raw_anchovies
	327: true, // raw_sardine
	359: true, // raw_tuna
	371: true, // raw_swordfish
	377: true, // raw_lobster
	383: true, // raw_shark
	389: true, // raw_mantaray
	395: true, // raw_seaturtle
}

// IsTrawlerFish reports whether obj id is a raw fish the trawler pays.
func IsTrawlerFish(id int) bool { return fishIDs[id] }

// NearNet reports whether t is within PickupRadius of the net (any level: the dump lies where the anchor stood).
func NearNet(t tile.Tile) bool {
	return t.X >= NetLand.X-PickupRadius && t.X <= NetLand.X+PickupRadius && t.Z >= NetLand.Z-PickupRadius && t.Z <= NetLand.Z+PickupRadius
}

// ZoneDrops is how many dropped objs an 8x8 zone keeps (engine 289, Zone.OBJS = (8 * 8 << 1) + 1): each drop past it removes the
// zone's oldest. And a drop shows to its dropper alone for its first 100 ticks (World.addObj: receiver64, revealed at Obj.REVEAL),
// so no crew member can take one before the next drops push it out: of a burst in one zone, only the last ZoneDrops are ever
// picked up. The window cannot spread its spill (a step closes it, [if_close,trawler_catch] dropping the rest where the anchor
// stood), so each claim of some 3000 items leaves the crew at most ZoneDrops of them by the net, junk included (live 4 Nov 2026:
// the crew banked about 100 fish a game), and the anchor's pack is the other place a catch can survive.
const ZoneDrops = 129

// MuleFish are the trawler fish the crew hands the mule from its bank, which cooks them and hands them out as food (owner,
// 4 Nov 2026: "raw manta ray or raw sea turtle get cooked by mule1 ... and then they should be handed out as a food source"):
// the two no crew member can cook (Cooking 91 and 82; mule1 has 99, mulereceiverplan.FishTiers).
var MuleFish = []string{"Raw manta ray", "Raw sea turtle"}

// MuleFishMin is how many MuleFish a crew member's bank holds before it walks them to the mule: one trade of notes, a trip of
// some minutes that may cost the account one game.
const MuleFishMin = 100

// IsMuleFish reports whether the obj named name is one of MuleFish.
func IsMuleFish(name string) bool {
	for _, n := range MuleFish {
		if strings.EqualFold(n, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// HeldDropStand is where the anchor drops the fish it still holds before a claim: the walkable tile nearest the claim's own
// (2666,3165-3166, beside the net) that lies in another zone (x 2656-2663), three tiles west. A tile nearer is in the net's zone
// (x 2664-2671, z 3160-3167), whose claim spill would push the drops out before the crew could see them.
var HeldDropStand = tile.Tile{X: 2663, Z: 3164, Level: 0}

// HeldDrops picks the pack slots of trawler fish the anchor drops at HeldDropStand before a claim, when it still holds the last
// one's (the bank skipped for the next watch): every one, dearest first (a pack holds fewer than ZoneDrops), which frees the pack
// for this claim and lies apart from its spill for the crew once revealed. items are the pack's stacks (Count 1 each for fish), Cost
// the unit cost.
func HeldDrops(items []CatchItem) []CatchItem {
	var fish []CatchItem
	for _, it := range items {
		if IsTrawlerFish(it.ID) && it.Count > 0 {
			fish = append(fish, it)
		}
	}
	sort.SliceStable(fish, func(i, j int) bool { return fish[i].Cost > fish[j].Cost })
	return fish
}

// CatchItem is one stack in the reward window: its slot, obj, count and the cache's unit cost.
type CatchItem struct {
	Slot  int
	ID    int
	Count int
	Cost  int
}

// CatchMove is one click on the reward window: Option CatchWithdrawAll moves the stack, CatchWithdrawOne one of it; Times is
// how many clicks of it.
type CatchMove struct {
	Slot, ID int
	Option   int
	Times    int
	// Moves is how many items the clicks move.
	Moves int
}

// PlanClaim chooses the clicks on the window. First the pack, the most valuable stacks first (fish before junk at equal
// cost): Withdraw All on a stack that fits in the free slots, else Withdraw 1 once for each free slot left. Then the floor:
// Withdraw All on every junk stack left, which drops it now, and then on every fish stack left, cheapest first, so the
// zone's 129 drops that stay are the best fish (see A ZONE HOLDS 129 DROPS). The window is empty at the end, so the close
// drops nothing.
func PlanClaim(items []CatchItem, free int) []CatchMove {
	order := append([]CatchItem(nil), items...)
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.Cost != b.Cost {
			return a.Cost > b.Cost
		}
		if IsTrawlerFish(a.ID) != IsTrawlerFish(b.ID) {
			return IsTrawlerFish(a.ID)
		}
		return a.Slot < b.Slot
	})
	var out []CatchMove
	left := make([]int, len(order))
	for i, it := range order {
		left[i] = max(it.Count, 0)
		switch {
		case left[i] == 0 || free <= 0:
		case left[i] <= free:
			out = append(out, CatchMove{Slot: it.Slot, ID: it.ID, Option: CatchWithdrawAll, Times: 1, Moves: left[i]})
			free -= left[i]
			left[i] = 0
		case IsTrawlerFish(it.ID):
			out = append(out, CatchMove{Slot: it.Slot, ID: it.ID, Option: CatchWithdrawOne, Times: free, Moves: free})
			left[i] -= free
			free = 0
		}
	}
	for i, it := range order {
		if left[i] > 0 && !IsTrawlerFish(it.ID) {
			out = append(out, CatchMove{Slot: it.Slot, ID: it.ID, Option: CatchWithdrawAll, Times: 1})
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		if left[i] > 0 && IsTrawlerFish(order[i].ID) {
			out = append(out, CatchMove{Slot: order[i].Slot, ID: order[i].ID, Option: CatchWithdrawAll, Times: 1})
		}
	}
	return out
}

// ShouldCross says a crew member in the lobby crosses the gangplank: only on the anchor's board call (the first crossing
// starts the 100-tick countdown that takes everyone in the lobby aboard, so a crew member crossing first would start a game
// the anchor is not on). With no bus to hear the call on (a harness), it crosses as before.
func ShouldCross(busKnown, anchorCalled bool) bool { return !busKnown || anchorCalled }
