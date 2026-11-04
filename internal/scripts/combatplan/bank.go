package combatplan

import "fmt"

// When to go to the bank, and whether the trip that just happened achieved anything.
//
// This is a pure decision with a name attached to every answer, and the name is the point. The
// trainer has grown a zero-progress bank loop THREE times now, each in a different function, and
// every time the log said only that it was banking again. The measured case this file exists to
// make impossible: 2026-09-19 at the giants rung, a run took 20 kills and then made 273 bank
// trips against them, 272 of 273 withdrawals timing out, melee XP frozen at 26,228 for over
// sixteen minutes while the account stood at the Edgeville bank, climbing at about 17 trips a
// minute. (The session that found it reported 262 trips and "114 of 115"; both are miscounts,
// the first from a grep that also matched the heartbeat's own "N bank trips" text.) Nobody
// could say which of the three reasons for banking was the one that stayed true, because the
// decision was three anonymous boolean branches in a row and the log printed none of them.
//
// So NeedsBank answers WHY as well as whether, and TripMadeProgress judges the trip afterwards
// against what actually changed rather than against what was clicked. A run can then refuse to
// walk back into a bank that just failed to help it, and say which reason it is refusing.

// BankReason names why a bank trip is due. The empty value means no trip is needed.
type BankReason string

const (
	// BankNotNeeded is the zero value: keep fighting.
	BankNotNeeded BankReason = ""
	// BankOutOfFood is the pack holding no food at all, with the bank not yet known empty.
	BankOutOfFood BankReason = "out of food"
	// BankAtReserve is the break-off rule: leave while there is still something to eat on the
	// way, rather than fighting the last bites down. See FoodReserve.
	BankAtReserve BankReason = "food down to the reserve"
	// BankPackFull is no room for another drop.
	BankPackFull BankReason = "the pack is full"
)

// BankNeed is the live reading the decision is made from. Every field is a plain number or flag
// so the whole decision can be tested without a session.
type BankNeed struct {
	// Food is how many bites are held.
	Food int
	// FoodPerTrip is this rung's own load, which the reserve is a fraction of.
	FoodPerTrip int
	// PackUsed and PackSize are the backpack's occupied slots and its capacity.
	PackUsed, PackSize int
	// HaveRung is whether a rung has been chosen. The reserve rule needs one, because
	// FoodPerTrip is the rung's number.
	HaveRung bool
	// FoodEmpty latches once the bank has been proven to hold no food.
	FoodEmpty bool
	// FoodScarce latches when the bank cannot fill the pack past the reserve.
	FoodScarce bool
	// PackStuck latches when a completed trip proved this pack cannot be emptied any further.
	//
	// Without it the pack-full branch is the one that never lets go. The food branches both
	// latch — FoodEmpty and FoodScarce each stop their own rule from firing again once the
	// bank has answered — and the pack branch had no equivalent, so a pack the keep-filter
	// cannot empty made NeedsBank true on every single pass for the rest of the run. That
	// asymmetry is the 273-trip spin.
	PackStuck bool
}

// NeedsBank reports whether to break off for the bank, and which rule decided it.
func NeedsBank(n BankNeed) (bool, BankReason) {
	if n.Food == 0 && !n.FoodEmpty {
		return true, BankOutOfFood
	}
	if !n.FoodEmpty && !n.FoodScarce && n.HaveRung && ShouldRestock(n.Food, n.FoodPerTrip) {
		return true, BankAtReserve
	}
	if n.PackSize > 0 && n.PackUsed >= n.PackSize && !n.PackStuck {
		return true, BankPackFull
	}
	return false, BankNotNeeded
}

// BankLoopLimit is how many bank trips in a row may buy no melee XP at all before the run gives
// up on the rung and ends itself.
//
// Six, and the number matters less than the fact that there is one. This script has now grown
// FIVE zero-progress bank loops, each a different pair of individually-reasonable rules that
// deadlocked, and each was found afterwards by reading a log. Every one of them would have been
// caught by this single rule, because they all share one observable shape that no healthy run
// has: bank trips accumulating while the server's own stat table does not move.
//
// Why six rather than two. A healthy cycle banks once and then fights, so consecutive trips are
// already unusual; but a couple in a row are legitimate — a full pack banked immediately before
// a restock, or a trip followed by one fight that timed out. Six is far above anything a working
// run produces and far below the 351 the 2026-09-20 run made. Why not a time limit instead: a
// trip to the giants bank is a 95-tile round trip through a trapdoor, so a wall-clock rule would
// have to be minutes long to be safe, and minutes are exactly what the stall costs.
//
// Melee XP is the signal on purpose. It comes off the server's own stat table and the script
// cannot fabricate it, which is the same reason docs/trustworthy_harnesses.md insists a harness
// assert XP and kills rather than a location reached.
const BankLoopLimit = 6

// BankLoop is the reading the loop guard is made from.
type BankLoop struct {
	// TripsSinceXP is how many completed bank trips have happened since melee XP last rose.
	TripsSinceXP int
	// Limit is BankLoopLimit, passed in so a caller can test the rule at other values.
	Limit int
	// Why is the rule that asked for the most recent trip, so the stop can name it.
	Why BankReason
	// Food is how many bites were held walking away from that trip, and BreakOff the number the
	// break-off reserve is a fraction of. Printed together because their disagreement is what
	// every one of these loops has been made of.
	Food, BreakOff int
	// PackUsed and PackSize are the pack the run walked away with.
	PackUsed, PackSize int
}

// BankLoopExhausted reports whether the run has banked its way through the limit without earning
// anything, and gives the sentence that says so.
//
// A run that stops promptly and says why is strictly better than one that spins for 44 minutes
// and then stops, so the reason carries every reading that decided it: which rule kept asking,
// what was being carried, and against which number.
func BankLoopExhausted(l BankLoop) (bool, string) {
	limit := l.Limit
	if limit < 1 {
		limit = BankLoopLimit
	}
	if l.TripsSinceXP < limit {
		return false, ""
	}
	why := string(l.Why)
	if why == "" {
		why = "no reason recorded"
	}
	return true, fmt.Sprintf("%d bank trips in a row bought no melee XP at all. The last was for %q, and it left the run holding %d bite(s) against a break-off of %d with the pack at %d/%d. That combination cannot improve by banking again, so this rung is not sustainable on what is available here",
		l.TripsSinceXP, why, l.Food, l.BreakOff, l.PackUsed, l.PackSize)
}

// BankTrip is what one completed trip actually changed, read either side of it.
type BankTrip struct {
	// SlotsBefore and SlotsAfter are the pack's occupied slots either side of the deposit.
	SlotsBefore, SlotsAfter int
	// Withdrew is how many items actually arrived, which is a delta rather than a total.
	//
	// The distinction is not pedantic. The spinning run logged "carrying 3 Trout" on every one
	// of its 273 trips, and 3 was the total it walked IN with: it withdrew nothing at all, and
	// a total can never say so.
	Withdrew int
}

// TripMadeProgress reports whether the trip changed anything about the account's position. A
// trip that freed no slot and gained no item leaves the world exactly as it found it, so
// repeating it immediately cannot do anything except spin.
func TripMadeProgress(t BankTrip) bool {
	return t.SlotsAfter < t.SlotsBefore || t.Withdrew > 0
}

// PackIsStuck reports whether a trip proved the pack cannot be emptied further: it freed
// nothing, and the pack is still full.
//
// Kept separate from TripMadeProgress because the two answer different questions. A trip that
// withdrew food but freed no slot made progress AND left the pack stuck, and a run needs to know
// both: carry on fighting with the food it just got, and stop treating the full pack as a reason
// to come back.
func PackIsStuck(t BankTrip, packSize int) bool {
	return packSize > 0 && t.SlotsAfter >= packSize && t.SlotsAfter >= t.SlotsBefore
}
