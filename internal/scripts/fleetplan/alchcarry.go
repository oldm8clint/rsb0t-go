package fleetplan

// How much alchemy stock an account carries (deaths-1008, 8 Oct 2026). Owner: "deaths cost us our high alchs".
// A death outside the safe area keeps three items, ONE of each (death.rs2 moves a single unit of the priciest object
// three times), so a pack of noted bows is gone to the last note: live on 8 Oct 17:51 an account died at a Yanille ogre
// safespot carrying 721 noted Yew shortbows and 225 noted Yew longbows -- 946 casts, ~500,000 coins -- and the pile
// lay on the ground for another account's Telekinetic Grab. Nobody had decided to carry 946 casts: the mule's hand-over
// (stage3AlchKit 1000), the walk-alch kit (walkAlchKit 200, endgameKit 400) and the deposits that keep "the kit" through
// a bank visit (Travel.AlchKeeps keeps the whole stack of whatever is on the list) each added to the pack, and nothing
// ever took the surplus out.
//
// The rule is a carry cap per purpose, in casts (one cast burns one alchable): the pack holds at most what the next
// stretch of the account's day will cast before its next bank visit, the rest stays in the bank, and the places a
// death is likely carry none at all:
//
//	walk (walkAlchCarry 120)      the walks between bank visits: ~20 casts a minute at best, a long walk is a few minutes.
//	pen  (walkAlchFightCarry 60)  a safespot pen's alchemy between the combat casts, from the bank to the next one.
//	alch mode (stage3AlchCarry 300)  Stage 3's hand-cast alchemy at the Ardougne bank, a bank visit every ~25 minutes.
//	endgame (endgameCarry 200)    the Heroes loop, which banks every 15 minutes and alchs ~9 casts a minute.
//	no safespot, the melee trainer, a quest, the Wilderness: none. Those banks keep the runes and the jewellery
//	(a few coins of nature runes) and send every alchable to the bank.

import (
	"fmt"
	"sort"
)

// Carry defaults, in casts.
const (
	DefaultWalkAlchCarry      = 120
	DefaultWalkAlchFightCarry = 60
	DefaultStage3AlchCarry    = 300
	DefaultEndgameCarry       = 200
)

// StockSlot is one pack slot of alchemy stock: its slot, how many casts it funds and its rank on the allow-list
// (0 pays the most).
type StockSlot struct {
	Slot, Count, Rank int
}

// StockKeepSlots picks which slots of alchemy stock a deposit leaves in the pack so that they total at most budget
// casts: the best-ranked slots first, a slot kept whole or not at all (a slot bigger than what is left of the budget
// is banked, and the kit draw takes the budget's worth back out of the bank). It reports the slots to keep and how
// many casts they fund. A budget of zero keeps nothing.
func StockKeepSlots(slots []StockSlot, budget int) (keep map[int]bool, kept int) {
	keep = map[int]bool{}
	if budget <= 0 {
		return keep, 0
	}
	order := append([]StockSlot(nil), slots...)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].Rank != order[j].Rank {
			return order[i].Rank < order[j].Rank
		}
		return order[i].Slot < order[j].Slot
	})
	for _, s := range order {
		if s.Count <= 0 || kept+s.Count > budget {
			continue
		}
		keep[s.Slot] = true
		kept += s.Count
	}
	return keep, kept
}

// CarryLine is the log line for a bank visit that cut the stock: held casts in the pack before, kept after.
// where names what the visit is for ("a safespot pen", "the melee trainer"); budget 0 means the stock stays banked.
func CarryLine(where string, kept, held, budget int) string {
	if budget <= 0 {
		return fmt.Sprintf("alch stock: carrying 0 of %d (%s is combat or the Wilderness: the stock stays banked)", held, where)
	}
	return fmt.Sprintf("alch stock: carrying %d of %d (a trip's worth, up to %d casts, for %s); the rest stays banked", kept, held, budget, where)
}
