package orderplan

// Pack space on a hand-out, and the trade screen's own refusal when there is none.
//
// THE DEFECT THIS FILE EXISTS FOR (2026-09-23). A non-stackable hand-out "failed" with the mule
// logging "the trade closed before the confirm screen", and the mule was never the problem. When
// both players accept the first screen, content runs ~enough_trade_space
// (content/scripts/interface_trade/scripts/trade.rs2, [if_button,trademain:accept] and
// [proc,enough_trade_space]) against the RECEIVER's free slots: one slot per non-stackable item,
// one per stackable the receiver does not already hold. If the offer does not fit it prints
//
//	giver:    "Other player doesn't have enough inventory space for this trade."
//	receiver: "You don't have enough inventory space for this trade"
//
// (the two lines swap, and lose or gain the full stop, when the other side is short) and closes
// BOTH screens. Nothing on either side compared the order with the donor's free slots, and
// nothing read that line, so the refusal was reported under a generic reason. Lab, hofdonor1/
// hofmule1: 10 Cake into 9 free slots refused, 9 into 9 delivered, 6 into 5 refused, 13 into 18
// delivered — the donor's free slots, never the count or the mule, decide it.
//
// The fix is on the asking side, because only the donor can see its own pack: FitAskToPack sizes
// the ask before it is published. TradeSpaceRefusal names the refusal on both sides for whatever
// still slips through (the pack changed between the ask and the trade).

import (
	"strings"
	"time"
)

// NotesAbove is the order size above which a hand-out of a NON-stackable item travels as bank
// notes: a full backpack. The mule's own rule (mulerunner.mustUseNotes) and the donor's fit below
// must agree on it exactly, or the donor sizes its ask for unnoted items the mule then notes (or
// the reverse), which is why it lives here, beside the grammar both sides already share.
const NotesAbove = 28

// TravelsAsNotes reports whether a hand-out of qty crosses as bank notes: more than a pack of an
// item that does not stack. A stackable item always fits in one slot and never needs notes.
func TravelsAsNotes(qty int, stackable bool) bool { return qty > NotesAbove && !stackable }

// FitAskToPack is how many of an item the asking account can actually receive in one trade,
// applying content's own ~enough_trade_space rule to its free pack slots; 0 means there is no
// room at all.
//
//   - A stackable item takes one slot, or none when the pack already holds that item.
//   - A non-stackable order above NotesAbove travels as ONE stack of notes (see TravelsAsNotes),
//     so it needs one slot, or none when the pack already holds that note.
//   - Anything else takes one slot per unit and is cut to the free slots.
//
// free must come from a pack that was actually READ; a caller that cannot read its pack must not
// call this (an unreadable pack is not an empty one, and would read as 28 free).
func FitAskToPack(qty int, stackable, heldAsItem, heldAsNote bool, free int) int {
	if qty < 1 {
		return 0
	}
	switch {
	case stackable:
		if heldAsItem || free >= 1 {
			return qty
		}
		return 0
	case TravelsAsNotes(qty, stackable):
		if heldAsNote || free >= 1 {
			return qty
		}
		return 0
	case free <= 0:
		return 0
	case qty > free:
		return free
	default:
		return qty
	}
}

// DeclineNoRoom is the mule's answer when the trade screen refused a hand-out because the donor's
// pack could not hold it. Short enough for mulebus.ReasonMax, and deliberately not about the item,
// so donorplan.ClassifyRefusal leaves it out of the refusal memory: a full pack says nothing about
// the mule's bank.
const DeclineNoRoom = "you have no room in your pack for that"

// TradeSpaceRefusal classifies one game message as content's trade-space refusal, by side:
// partnerLacksRoom is "Other player doesn't have enough inventory space for this trade" and
// iLackRoom is "You don't have enough inventory space for this trade", each with or without the
// full stop content gives one of the two (trade.rs2). Anything else is neither.
func TradeSpaceRefusal(line string) (partnerLacksRoom, iLackRoom bool) {
	l := strings.TrimSuffix(strings.TrimSpace(line), ".")
	switch l {
	case "Other player doesn't have enough inventory space for this trade":
		return true, false
	case "You don't have enough inventory space for this trade":
		return false, true
	}
	return false, false
}

// RepeatWindow is how long after the mule finishes serving a donor's order it treats the same
// order text from the same donor as a leftover repeat rather than a new ask. It must be longer
// than donorrunner.AskRepeatEvery (10 s), the interval a waiting donor re-publishes at, which a
// donorrunner test pins.
const RepeatWindow = 15 * time.Second

// StaleRepeat reports whether an order polled at now, with text, is a leftover re-publish of the
// order the mule finished serving at servedAt with servedText.
//
// THE LOOP THIS ENDS (live, 2026-09-22). The mule takes an order off the bus and serves it, which
// takes a bank trip and a trade, ten seconds or more. The donor re-publishes the same order every
// AskRepeatEvery while it waits, so the next bus poll after the hand-out re-queued the order that
// had just been filled, and the mule served it again to a donor that had moved on:
//
//	15:43:43 [mule]  handed 15 Cake to "acct64"
//	15:43:44 [mule]  wall bus: "acct64" needs 15 Cake — queued
//	15:43:45 [acct64] donor: asked "mule" over the wall bus for need 50 Chaos rune
//	15:43:48 [acct64] donor: declining the hand-out screen with "mule": offered something I did not ask for
//
// (fleet-20260922-101543 log.) The handoff0923 investigation counted 41 of 67 donor-declined
// hand-outs in the archived logs with that shape. Only the SAME text inside
// the window is dropped: a different item, a different quantity or a genuinely later re-ask is a
// new order.
func StaleRepeat(servedText string, servedAt time.Time, text string, now time.Time) bool {
	if servedAt.IsZero() || now.Sub(servedAt) > RepeatWindow {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(servedText), strings.TrimSpace(text))
}
