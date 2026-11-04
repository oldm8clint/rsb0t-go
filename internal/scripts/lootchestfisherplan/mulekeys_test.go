package lootchestfisherplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// The owner's number and the mule's floor must be the SAME number. They are separate names
// because they are separate ideas, and this is what stops them drifting apart: raising
// orderplan.KeysFloor without meaning to change this script's go/no-go fails here.
func TestMuleKeyThresholdIsTheMulesOwnFloor(t *testing.T) {
	if MuleKeyThreshold != orderplan.KeysFloor {
		t.Fatalf("MuleKeyThreshold = %d, orderplan.KeysFloor = %d — they must be the same number", MuleKeyThreshold, orderplan.KeysFloor)
	}
	if MuleKeyThreshold != 100 {
		t.Fatalf("MuleKeyThreshold = %d, the fleet owner's stated number is 100", MuleKeyThreshold)
	}
}

// Available is the number users asked for — what the mule will actually part with. It
// replaces TestAvailableIsBankedMinusTheReserve, which pinned banked - 100 (113 gave 13): the
// users ruled on 2026-09-26, after 110 banked became an ask for 10, that the floor is a GATE and
// everything above it crosses ("it should have taken 110 keys").
func TestAvailableIsTheWholeStockAboveTheGate(t *testing.T) {
	for _, c := range []struct{ banked, want int }{
		{0, 0},
		{99, 0},
		{100, 0},     // at the floor exactly: not "over 100", so the mule declines
		{101, 101},   // "over 100": all of it
		{110, 110},   // the live reading of 2026-09-26 that was asked for as 10
		{113, 113},   // the 2026-09-21 reading that crossed as 13
		{418, 418},   // Available is the stock; KeysPerOrder is Request's clamp, not this one's
		{1000, 1000}, // likewise
	} {
		if got := Available(c.banked); got != c.want {
			t.Errorf("Available(%d) = %d, want %d", c.banked, got, c.want)
		}
	}
}

// SlotsNeeded is why a BIG order is easier to carry than a small one, which is the opposite of
// the intuition and the thing the owner's "13 unnoted keys" report turns on.
func TestSlotsNeededFlipsToOneAboveAPack(t *testing.T) {
	for _, c := range []struct{ qty, want int }{
		{0, 0}, {1, 1}, {13, 13}, {28, 28}, {29, 1}, {500, 1},
	} {
		if got := SlotsNeeded(c.qty); got != c.want {
			t.Errorf("SlotsNeeded(%d) = %d, want %d", c.qty, got, c.want)
		}
	}
}

func TestRequestAsksForWhatTheMuleHasNotAFlatFiveHundred(t *testing.T) {
	// The 2026-09-21 live case. This used to want 13 (113 minus a 100 reserve); the owner's
	// 2026-09-26 ruling makes the floor a gate, so a mule holding 113 hands over all 113 — and
	// the ask must be for that, not 500 and not the remainder.
	if got := Request(113, PackSlots); got != 113 {
		t.Errorf("Request(113, 28) = %d, want 113 (the whole stock above the gate)", got)
	}
	// This used to want 5 (13 unnoted keys into five free slots). 113 crosses as notes in ONE
	// slot, so a nearly-full pack takes the whole order.
	if got := Request(113, 5); got != 113 {
		t.Errorf("Request(113, 5) = %d, want 113 (notes need one slot)", got)
	}
	// The case that makes the whole feature work: a big stock crosses as ONE stacked note, so
	// a nearly-full pack is no obstacle at all.
	if got := Request(1000, 1); got != orderplan.KeysPerOrder {
		t.Errorf("Request(1000, 1) = %d, want %d — a note-sized order needs one slot", got, orderplan.KeysPerOrder)
	}
	// Nothing to ask for. Zero must mean "do not ask": orderplan.Parse rejects a quantity of
	// zero outright, so publishing it would be a guaranteed decline.
	if got := Request(100, PackSlots); got != 0 {
		t.Errorf("Request(100, 28) = %d, want 0 — the mule is at its floor, not over it", got)
	}
	if got := Request(500, 0); got != 0 {
		t.Errorf("Request(500, 0) = %d, want 0 — no room in the pack", got)
	}
}

// The owner's bug report of 2026-09-26, end to end on this side: the mule banked 110 Crystal
// keys and spare104 asked for 10 ("10 above its reserve — asking for 10"). "It should have
// taken 110 keys." The ask must be 110, it must fit in one slot as notes, and the mule's own
// orderplan.Decide must fill all 110 of it.
func TestHundredAndTenBankedAsksForAndReceivesAllHundredAndTen(t *testing.T) {
	if got := Request(110, PackSlots); got != 110 {
		t.Fatalf("Request(110, 28) = %d, want 110", got)
	}
	if got := Request(110, 1); got != 110 {
		t.Fatalf("Request(110, 1) = %d, want 110 — 110 keys cross as notes in one slot", got)
	}
	if got := SlotsNeeded(110); got != 1 {
		t.Fatalf("SlotsNeeded(110) = %d, want 1", got)
	}
	pol := orderplan.DefaultPolicy([]string{"spare104"})
	d := orderplan.Decide(time.Now(), "spare104", orderplan.Format("Crystal key", Request(110, PackSlots)),
		pol, orderplan.NewLedger(orderplan.DefaultWindow), func(string) int { return 110 })
	if d.Verdict != orderplan.Fulfil || d.Qty != 110 {
		t.Fatalf("the mule's answer to our ask against 110 banked = %+v, want all 110", d)
	}
	// And afterwards the mule is empty, so the account goes back to fishing, not back to it.
	if left := AfterDelivery(110, d.Qty); left != 0 {
		t.Errorf("AfterDelivery(110, 110) = %d, want 0", left)
	}
}

func TestShortfallNamesAShortDelivery(t *testing.T) {
	if got := Shortfall(500, 13); got != 487 {
		t.Errorf("Shortfall(500, 13) = %d, want 487", got)
	}
	if got := Shortfall(13, 13); got != 0 {
		t.Errorf("Shortfall(13, 13) = %d, want 0", got)
	}
	if got := Shortfall(13, 20); got != 0 {
		t.Errorf("Shortfall(13, 20) = %d, want 0 — more than asked is not a shortfall", got)
	}
}

// The coupling to orderplan's own decline wording, TESTED rather than assumed. If orderplan
// rewords that sentence this fails here instead of silently blinding the script.
func TestParseBankedCountRoundTripsARealOrderplanDecline(t *testing.T) {
	now := time.Now()
	pol := orderplan.DefaultPolicy([]string{"spare104"})
	led := orderplan.NewLedger(orderplan.DefaultWindow)
	text := orderplan.Format("Crystal key", orderplan.KeysPerOrder)

	d := orderplan.Decide(now, "spare104", text, pol, led, func(string) int { return 57 })
	if d.Verdict != orderplan.Decline {
		t.Fatalf("a mule holding 57 keys against a floor of %d should decline, got %q (%s)", orderplan.KeysFloor, d.Verdict, d.Reason)
	}
	n, ok := ParseBankedCount(d.Reason, "Crystal key")
	if !ok {
		t.Fatalf("ParseBankedCount could not read orderplan's own decline %q", d.Reason)
	}
	if n != 57 {
		t.Fatalf("ParseBankedCount(%q) = %d, want 57", d.Reason, n)
	}
}

func TestParseBankedCountRefusesAnythingThatIsNotAStockAnswer(t *testing.T) {
	// The one that matters: a positional decline says NOTHING about stock. Reading it as one
	// is the mistake orderplan.DeclineIsPositional was written after.
	if _, ok := ParseBankedCount(orderplan.DeclineNotInSight, "Crystal key"); ok {
		t.Errorf("a positional decline was read as a stock count")
	}
	for _, reason := range []string{
		"",
		"not on the allow list",
		"already handed you one recently",
		"none in the bank",
		"capped to 13",
		"only many Crystal key banked, holding until 100",
	} {
		if _, ok := ParseBankedCount(reason, "Crystal key"); ok {
			t.Errorf("ParseBankedCount(%q) read a count where there is none", reason)
		}
	}
	// A count for a DIFFERENT item is not this item's count.
	if _, ok := ParseBankedCount("only 57 Big fishing net banked, holding until 100", "Crystal key"); ok {
		t.Errorf("a count for another item was accepted as a crystal key count")
	}
}

func TestMuleStockFreshnessAndTheVisitGate(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	never := MuleStock{}
	if never.Fresh(now) {
		t.Errorf("a MuleStock nobody has ever filled in reads as fresh")
	}
	if never.WorthVisiting(now) {
		t.Errorf("an unheard count sent the account on a trip")
	}

	plenty := MuleStock{Banked: 150, At: now.Add(-time.Minute), Heard: true}
	if !plenty.WorthVisiting(now) {
		t.Errorf("150 banked keys is over the threshold and should be worth the trip")
	}
	// Exactly at the threshold is NOT over it: the mule would hand over nothing.
	atFloor := MuleStock{Banked: MuleKeyThreshold, At: now.Add(-time.Minute), Heard: true}
	if atFloor.WorthVisiting(now) {
		t.Errorf("a mule at its floor exactly is not worth walking to")
	}
	// Stale readings do not send anybody anywhere; the probe clock handles them instead.
	stale := MuleStock{Banked: 150, At: now.Add(-MuleStockTTL - time.Minute), Heard: true}
	if stale.WorthVisiting(now) {
		t.Errorf("a reading older than MuleStockTTL was acted on")
	}
}

func TestShouldProbeIsTheWayOutOfNotKnowing(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	if !ShouldProbe(MuleStock{}, now, past) {
		t.Errorf("an account that has never heard a count, with its clock due, must probe")
	}
	if ShouldProbe(MuleStock{}, now, future) {
		t.Errorf("the probe clock was ignored")
	}
	fresh := MuleStock{Banked: 12, At: now, Heard: true}
	if ShouldProbe(fresh, now, past) {
		t.Errorf("a fresh reading was re-probed; it should simply be believed")
	}
	stale := MuleStock{Banked: 12, At: now.Add(-MuleStockTTL - time.Second), Heard: true}
	if !ShouldProbe(stale, now, past) {
		t.Errorf("a stale reading was never retaken, which is how an account goes blind forever")
	}
}

func TestAfterDeliveryReportsWhatIsReallyLeft(t *testing.T) {
	// This test used to assert the opposite, and it was right at the time: while Floor was a
	// RESERVE the mule always kept back, a drained mule genuinely still held a hundred.
	//
	// Floor is a GATE now — it refuses below the line and hands over the whole stock above it —
	// so a mule that handed everything over is EMPTY, and saying otherwise made the account
	// believe in a hundred keys sitting in its own pack. The fleet owner saw exactly that:
	// "spare104 thinks the mule holds 100 crystal keys but they have them in their inventory."
	if got := AfterDelivery(113, 113); got != 0 {
		t.Errorf("AfterDelivery(113, 113) = %d, want 0 — the mule handed over everything", got)
	}
	// An empty mule reads as empty, and therefore as not worth a trip: below the gate the
	// account fishes until the mule has accumulated past it again, which is the owner's policy.
	if got := AfterDelivery(0, 0); got != 0 {
		t.Errorf("AfterDelivery(0, 0) = %d, want 0", got)
	}
	if (MuleStock{Banked: 0, At: time.Now(), Heard: true}).WorthVisiting(time.Now()) {
		t.Error("a drained mule should not be worth visiting — this is the walk that repeated all night")
	}

	// THE CASE THAT STILL MATTERS, and the reason this is arithmetic rather than a reset to
	// "unknown": a stock above the PER-ORDER cap still has plenty left, and the account should
	// come straight back for it instead of going fishing.
	if got := AfterDelivery(1000, orderplan.KeysPerOrder); got != 500 {
		t.Errorf("AfterDelivery(1000, 500) = %d, want 500", got)
	}
	if !(MuleStock{Banked: AfterDelivery(1000, orderplan.KeysPerOrder), At: time.Now(), Heard: true}).WorthVisiting(time.Now()) {
		t.Errorf("a mule still holding 500 keys after a delivery should still be worth visiting")
	}

	// Never negative, whatever a stale reading claims was there before.
	if got := AfterDelivery(10, 50); got != 0 {
		t.Errorf("AfterDelivery(10, 50) = %d, want 0 — a stale 'before' must not go negative", got)
	}
}
