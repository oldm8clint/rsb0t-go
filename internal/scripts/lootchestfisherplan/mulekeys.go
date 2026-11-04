package lootchestfisherplan

// The mule side of the cycle: how many crystal keys the mule is holding, how many of them this
// account may actually ask for, and whether a trip to the mule is worth making at all.
//
// THE FLEET OWNER'S POLICY, 2026-09-21, in their own words across three messages:
//
//	"at handover, if the mule has over 100keys those should be traded in return for defi3lds
//	 bank and then spare104 should move back to looting the chest, when spare104 finishes that
//	 and comes back to drop off all the items to the mule and they dont have over 100 keys then
//	 spare104 should go back to fishing and only come back to the mule when they reach 100+ keys"
//
//	"donor asked the mule for 500 crystal keys instead of checking how many keys the mule has if
//	 it is over 100 and request that amount"
//
//	"the mule gave spare104 13 unnoted keys and spare104 was happy with that, that should be fixed
//	 as the mule still has 102 crystal keys available"
//
// And on 2026-09-26, after the mule banked 110 and spare104 received 10:
//
//	"it should have taken 110 keys"
//
// ---- THE FLOOR IS A GATE, NOT A RESERVE (owner's ruling, 2026-09-26) ----
//
// Once the mule holds MORE than 100 keys, ALL of them are handed over, bounded only by
// orderplan.KeysPerOrder. At or below 100, nothing is. There is exactly one key consumer
// (spare104), so nobody else is left for a reserve to protect.
//
// HISTORY, because the wrong reading lasted long enough to bite four times. From 2026-09-21
// orderplan.Decide held the floor back as a RESERVE (after one asker took 418 keys in one go),
// so a mule would part with only banked - 100: 13 from 113, which also crossed UNNOTED because
// 13 is below the notes line (28). orderplan went back to a gate on 2026-09-22, but Available
// below kept subtracting the floor, so this account kept ASKING for the remainder and the mule
// kept filling exactly that: 110 banked, 10 asked, 10 received. Both sides now agree.
//
// Two consequences this file encodes, so no caller has to rediscover them:
//
//   - ASKING FOR A BLANKET 500 CANNOT WORK. Available() is the mule's whole stock once it is
//     over the gate, and Request() is the most of it this account can actually carry home --
//     which is all of it, because anything over 28 crosses as notes in one slot.
//   - A TRIP IS ONLY WORTH IT ABOVE THE FLOOR. WorthVisiting() is the gate, and
//     MuleKeyThreshold is deliberately orderplan.KeysFloor itself — the owner's "over 100" and
//     the mule's own floor are the SAME number, and a test pins them together so raising one
//     cannot silently leave the other behind.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// MuleKeyThreshold is the owner's "over 100": the mule must hold MORE than this before a trip
// to collect keys is worth making.
//
// It is orderplan.KeysFloor rather than a literal 100, because the mule will part with nothing
// at or below it. Written as a separate name because the two are separate ideas that happen to
// share a value — one is the mule's gate, the other is this account's go/no-go — and
// TestMuleKeyThresholdIsTheMulesOwnFloor is what stops them drifting apart.
const MuleKeyThreshold = orderplan.KeysFloor

// NotesAbove mirrors mulerunner's own unexported notesAbove: a hand-out larger than a backpack
// crosses as BANK NOTES, anything at or below it crosses as the ordinary item.
//
// Duplicated rather than exported from there on purpose — this package is pure and imports no
// runner — and it is duplicated as a DECISION rather than as a constant: what this side needs
// is "how many pack slots will the answer take", which is what SlotsNeeded below answers. If
// the mule's own rule ever changes, the symptom is a short delivery that Shortfall already
// reports, not a silent loss.
//
// Crystal keys do not stack (content: [crystal_key] carries no stackable=yes, while its note
// cert_crystal_key inherits template_for_cert which does), so the stackable half of the mule's
// mustUseNotes test is false for them and quantity alone decides.
const NotesAbove = PackSlots

// SlotsNeeded is how many pack slots a hand-out of qty crystal keys will occupy when it lands.
//
// One, if it is large enough to cross as notes; qty otherwise. This is the whole reason a big
// order is EASIER to carry than a small one, which is the opposite of the intuition: 400 keys
// ride home in a single slot and 13 need thirteen.
func SlotsNeeded(qty int) int {
	if qty <= 0 {
		return 0
	}
	if qty > NotesAbove {
		return 1
	}
	return qty
}

// Available is how many crystal keys the mule will actually part with, given what its bank
// holds. Never negative.
//
// This is the number users asked for — "check how many keys the mule has ... and request that
// amount" — and above the gate it IS the banked count: 110 banked is 110 available (owner's
// ruling, 2026-09-26: "it should have taken 110 keys"). At or below MuleKeyThreshold it is 0,
// because orderplan.Decide declines every key order there. It used to be banked minus the
// floor, which is how 110 banked became an ask for 10.
func Available(muleBanked int) int {
	if muleBanked <= MuleKeyThreshold {
		return 0
	}
	return muleBanked
}

// Request is exactly how many keys to ask for, clamped to every real ceiling in the path.
//
// The ceilings, each one measured rather than guessed, in the order they bind:
//
//  1. The mule's gate — Available above: nothing at or below the floor, the whole stock above.
//  2. orderplan.KeysPerOrder, the mule's own per-order cap. Asking above it is not an error
//     (Prescreen clamps rather than declining) but it makes the log lie about what was wanted.
//  3. THE PACK. A hand-out that crosses as notes needs one slot whatever its size, so this only
//     bites for an order of 28 or fewer. Since Available is gated above 100 that no longer
//     happens for a real reading, but the clamp stays for a mule whose floor is ever lowered.
//
// Returns 0 when there is nothing worth asking for, which a caller must treat as "do not ask",
// not as "ask for zero": orderplan.Parse rejects a quantity of zero outright.
func Request(muleBanked, packFree int) int {
	qty := Available(muleBanked)
	if qty <= 0 || packFree <= 0 {
		return 0
	}
	if qty > orderplan.KeysPerOrder {
		qty = orderplan.KeysPerOrder
	}
	if SlotsNeeded(qty) <= packFree {
		return qty
	}
	// Only reachable for an order small enough to cross unnoted: one slot per key, so the pack
	// is the cap. A note-sized order needs one slot and packFree is at least one here.
	return packFree
}

// Shortfall is how many fewer keys arrived than were asked for. Zero when the order was filled.
//
// It exists so "happy with that" cannot happen again. The owner's report was not that thirteen
// keys is a bad number — it is what the mule could spare — but that the account accepted it
// SILENTLY and walked away while the mule still held a hundred more. A short delivery is a
// fact worth printing every time, whether or not it changes what the account does next.
func Shortfall(asked, got int) int {
	if got >= asked {
		return 0
	}
	if got < 0 {
		got = 0
	}
	return asked - got
}

// ---- what this account knows about the mule's bank ----

// MuleStockTTL is how long a reading of the mule's own key count is still worth acting on.
//
// The mule's stock only ever RISES between trips (the fleet's caskets make halves faster than
// one account spends keys), so a stale reading errs low, and erring low costs a trip not taken
// rather than a trip wasted. Sized well above the ask cadence so an ordinary fishing stretch
// does not forget what it learned, and short enough that a mule which has been restarted,
// reconfigured or drained by something else is not believed for an afternoon.
const MuleStockTTL = 45 * time.Minute

// MuleStock is the last thing this account heard about the mule's crystal-key bank, and when.
//
// Banked is the mule's own count of banked keys, NOT what it would hand over — Available turns
// one into the other. A zero-valued MuleStock is "never heard", which is deliberately different
// from "heard, and it was zero": the first is a reason to go and find out, the second is a
// reason to stay away.
type MuleStock struct {
	Banked int
	At     time.Time
	Heard  bool
}

// Fresh reports whether the reading is worth acting on at now.
func (m MuleStock) Fresh(now time.Time) bool {
	return m.Heard && !m.At.IsZero() && now.Sub(m.At) < MuleStockTTL
}

// WorthVisiting reports whether the mule is holding enough keys to justify walking to it —
// the owner's "only come back to the mule when they reach 100+ keys in the bank".
//
// A reading that has gone stale, or one this account has never taken, answers FALSE here and
// is handled by the caller's own blind-probe clock instead. That split matters: this function
// must never be the thing that sends an account on a trip for a number nobody has read, and
// the caller must never be left with no way to ever read one. See ShouldProbe.
func (m MuleStock) WorthVisiting(now time.Time) bool {
	return m.Fresh(now) && m.Banked > MuleKeyThreshold
}

// ShouldProbe reports whether it is time to find out what the mule holds, given the last
// reading and a clock the caller advances after each attempt.
//
// This is the EXIT from every "I do not know" state in the script, and it is the reason the
// unknown case cannot strand the one account that runs this. An account that has never heard a
// count, or whose count has gone stale, probes; an account with a fresh count below the
// threshold does not, and simply keeps fishing until the reading ages out.
func ShouldProbe(m MuleStock, now, nextAllowed time.Time) bool {
	if now.Before(nextAllowed) {
		return false
	}
	return !m.Fresh(now)
}

// ---- reading the count out of the mule's own answer ----

// declinePrefix and declineMiddle are the two halves of the sentence orderplan.Decide builds
// when an order is refused for being at or below the batch floor:
//
//	"only 113 Crystal key banked, holding until 100"
//
// Matching a sentence another package formats is a coupling worth naming rather than hiding.
// It is done this way because orderplan's own Handout.Floor doc comment says this IS the
// designed channel — "a decline carrying the real count is the same answer with no extra round
// trip and no window where the number is stale" — and because the alternative, a new message
// kind on a bus another session owns, would be a second transport for one direction of a
// conversation that already has one.
//
// The coupling is TESTED, not assumed: ParseBankedCountRoundTrip in this package's tests feeds
// a real orderplan.Decide decline through ParseBankedCount, so a reword in orderplan fails a
// test here instead of silently blinding the script.
const (
	declinePrefix = "only "
	declineMiddle = " banked, holding until "
)

// ParseBankedCount reads the mule's real banked count out of a KindDeclined reason.
//
// Returns false for every other decline, and that distinction is load-bearing: a positional
// refusal ("you are not in sight of me") says NOTHING about stock, and reading it as a stock
// answer is a mistake this repo has already made once at scale — orderplan.DeclineIsPositional
// exists because a woodcutter read four positional declines as four scarcity answers and
// stepped all the way down its axe ladder for a reason that had nothing to do with what the
// mule held.
//
// item is compared case-insensitively so a caller passes the canonical name it asked for.
func ParseBankedCount(reason, item string) (int, bool) {
	r := strings.TrimSpace(reason)
	if !strings.HasPrefix(strings.ToLower(r), declinePrefix) {
		return 0, false
	}
	rest := r[len(declinePrefix):]
	cut := strings.Index(strings.ToLower(rest), strings.ToLower(declineMiddle))
	if cut < 0 {
		return 0, false
	}
	head := rest[:cut]
	sp := strings.IndexByte(head, ' ')
	if sp <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(head[:sp])
	if err != nil || n < 0 {
		return 0, false
	}
	if !strings.EqualFold(strings.TrimSpace(head[sp+1:]), strings.TrimSpace(item)) {
		return 0, false
	}
	return n, true
}

// AfterDelivery is what the mule's bank still holds once it has handed over got keys, given
// what it held before — arithmetic, so the account does not have to walk back and ask.
//
// Never below zero, and NOT clamped up to orderplan.KeysFloor any more.
//
// That clamp was right while Floor was a RESERVE the mule always kept back, so a drained mule
// genuinely still held a hundred. Floor is now a GATE -- it refuses below the line and hands
// over the whole stock above it -- and the clamp survived the change, which made the account
// believe in a hundred keys that were in its own pack. The fleet owner saw exactly that on
// 2026-09-22: "spare104 thinks the mule holds 100 crystal keys but they have them in their
// inventory."
//
// So a mule that hands over everything now reads as empty, which is the truth and which sends
// the account fishing until the mule has accumulated past the gate again -- the owner's own
// policy. The case that still matters is a stock above the PER-ORDER cap: 1,000 banked hands
// over 500 and keeps 500, and that is precisely when the account should come straight back.
//
// Recording this rather than clearing the reading to "unknown" is what stops the account
// turning round and walking back to a mule it has just drained.
func AfterDelivery(before, got int) int {
	if got < 0 {
		got = 0
	}
	left := before - got
	if left < 0 {
		left = 0
	}
	return left
}
