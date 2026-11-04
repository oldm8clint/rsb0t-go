package runerunnerplan

// Delivery to the mule (station 1): when a trade is worth making, when it must wait, and what
// crosses the trade screen.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fleetplan"
)

// DeliverAction is DeliverDecision's verb.
type DeliverAction string

const (
	// DeliverSkip: not worth a trade this round; the runes stay banked.
	DeliverSkip DeliverAction = "skip"
	// DeliverNow: withdraw the runes and trade them.
	DeliverNow DeliverAction = "deliver"
	// DeliverDefer: worth a trade, but the mule cannot take it now; wait at the post up to
	// muleWaitMinutes, then bank the runes and carry on.
	DeliverDefer DeliverAction = "defer"
	// DeliverHop: worth a trade and the mule is on another known world: hop there first.
	DeliverHop DeliverAction = "hop"
)

// DeliverInput is what DeliverDecision reads.
type DeliverInput struct {
	// Held is the deliverable runes: listed runes in the pack plus the last bank reading.
	Held int
	// HeldAmmo is the ammo parts held the same way. They never make a delivery due by size (DeliverDueAmmo):
	// 3,000 Feathers alone must not send a hop or a trade for zero runes.
	HeldAmmo int
	// LastDelivery is the last trade that moved runes; zero for never.
	LastDelivery time.Time
	Now          time.Time
	MinRunes     int
	MaxAge       time.Duration
	// MuleWorld is the mule's world when MuleWorldKnown. An unknown world is never read as "the
	// same world as me".
	MuleWorld      int
	MuleWorldKnown bool
	MyWorld        int
	// MuleHeard is a heartbeat from the mule on the bus inside the TTL, from any world.
	MuleHeard bool
	// QueueTooLong is the mule's published queue at its cycle outlasting the wait; QueueWhy
	// says so.
	QueueTooLong bool
	QueueWhy     string
	// HopAllowed is MayHop's verdict, for a hop to the mule's world.
	HopAllowed bool
	HopWhy     string
}

// DeliverVerdict is DeliverDecision's answer.
type DeliverVerdict struct {
	Action DeliverAction
	Reason string
}

// DeliverDue is the threshold half: at least minRunes held, or anything held once the last
// delivery is older than maxAge (never delivered counts as old).
func DeliverDue(held, minRunes int, last, now time.Time, maxAge time.Duration) (bool, string) {
	switch {
	case held <= 0:
		return false, "nothing held"
	case held >= minRunes:
		return true, fmt.Sprintf("%d held (deliverMinRunes %d)", held, minRunes)
	case last.IsZero():
		return true, fmt.Sprintf("%d held and nothing delivered yet", held)
	case now.Sub(last) >= maxAge:
		return true, fmt.Sprintf("%d held and the last delivery was %s ago", held, now.Sub(last).Round(time.Minute))
	}
	return false, fmt.Sprintf("%d held, under deliverMinRunes %d, last delivery %s ago", held, minRunes, now.Sub(last).Round(time.Minute))
}

// DeliverDueAmmo is DeliverDue with ammo parts held beside the runes. The size threshold reads the runes
// alone: a pack and bank holding 3,000 Feathers and 20 runes is not worth a trade or a hop. The ammo is
// still delivered whenever a delivery is due (it travels with it), and on its own once nothing has been
// delivered yet or the last delivery is older than maxAge -- the same age rule runes below the threshold use.
func DeliverDueAmmo(held, ammo, minRunes int, last, now time.Time, maxAge time.Duration) (bool, string) {
	if ammo <= 0 {
		return DeliverDue(held, minRunes, last, now, maxAge)
	}
	if held >= minRunes {
		return DeliverDue(held, minRunes, last, now, maxAge)
	}
	switch {
	case last.IsZero():
		return true, fmt.Sprintf("%d runes and %d ammo parts held and nothing delivered yet", held, ammo)
	case now.Sub(last) >= maxAge:
		return true, fmt.Sprintf("%d runes and %d ammo parts held and the last delivery was %s ago", held, ammo, now.Sub(last).Round(time.Minute))
	}
	return false, fmt.Sprintf("%d runes (and %d ammo parts) held, under deliverMinRunes %d, last delivery %s ago", held, ammo, minRunes, now.Sub(last).Round(time.Minute))
}

// DeliverDecision is station 1's decision. The thresholds first (a delivery not worth making is
// skipped wherever the mule is), then the gate: the mule's world must be known, this account on
// it (else a hop, when one is allowed), the mule heard, and its queue short enough.
func DeliverDecision(in DeliverInput) DeliverVerdict {
	due, why := DeliverDueAmmo(in.Held, in.HeldAmmo, in.MinRunes, in.LastDelivery, in.Now, in.MaxAge)
	if !due {
		return DeliverVerdict{Action: DeliverSkip, Reason: why}
	}
	switch {
	case !in.MuleWorldKnown:
		return DeliverVerdict{Action: DeliverDefer, Reason: "the mule's world is unknown"}
	case in.MyWorld != in.MuleWorld:
		if in.HopAllowed {
			return DeliverVerdict{Action: DeliverHop, Reason: fmt.Sprintf("on world %d and the mule is on world %d", in.MyWorld, in.MuleWorld)}
		}
		reason := fmt.Sprintf("on world %d, the mule is on world %d, and a hop is not allowed now", in.MyWorld, in.MuleWorld)
		if in.HopWhy != "" {
			reason += " (" + in.HopWhy + ")"
		}
		return DeliverVerdict{Action: DeliverDefer, Reason: reason}
	case !in.MuleHeard:
		return DeliverVerdict{Action: DeliverDefer, Reason: "the mule is not heard on the bus"}
	case in.QueueTooLong:
		reason := "the mule's queue is too long"
		if in.QueueWhy != "" {
			reason = in.QueueWhy
		}
		return DeliverVerdict{Action: DeliverDefer, Reason: reason}
	}
	return DeliverVerdict{Action: DeliverNow, Reason: why}
}

// Deliverables sums the listed items (runes, and staffs when bought) in the pack and the bank
// reading.
func Deliverables(pack, bank map[string]int, runes []string) (byRune map[string]int, total int) {
	byRune = map[string]int{}
	for _, r := range runes {
		p, _ := countOf(pack, r)
		b, _ := countOf(bank, r)
		if n := p + b; n > 0 {
			byRune[r] = n
			total += n
		}
	}
	return byRune, total
}

// DeliverablesSplit is Deliverables for a delivery that also carries ammo parts: byItem counts every listed
// item (runes, staffs and ammo), runes the total of `runes` alone and ammo the total of `ammo` alone, so the
// delivery's size gate reads the runes and the ammo only rides along.
func DeliverablesSplit(pack, bank map[string]int, runes, ammo []string) (byItem map[string]int, runesTotal, ammoTotal int) {
	byItem, runesTotal = Deliverables(pack, bank, runes)
	byAmmo, ammoTotal := Deliverables(pack, bank, ammo)
	for k, n := range byAmmo {
		byItem[k] = n
	}
	return byItem, runesTotal, ammoTotal
}

// MuleRules are the hand-over rules: only the listed items cross (Resolved.DeliverItems: the
// listed runes, and Staffs of air when the ring buys them), minds first. Coins, the blade, food
// and anything else stay (Skip), so CoinsName is left blank and neither
// scarabrunner.HandSurplusToMule nor thieverplan's rules are used -- both hand coins over first.
func MuleRules(items []string) donorplan.Rules {
	listed := map[string]bool{}
	for _, r := range items {
		listed[strings.ToLower(strings.TrimSpace(r))] = true
	}
	return donorplan.Rules{
		Skip: func(row donorplan.ItemRow) bool {
			return !listed[strings.ToLower(strings.TrimSpace(row.Name))]
		},
		First: func(row donorplan.ItemRow) bool {
			return strings.EqualFold(strings.TrimSpace(row.Name), fleetplan.MindRune)
		},
	}
}

// AllowListRefusals is how many trades in a row the mule may give up on before the buyer says,
// every round, that it is probably missing from the mule's allowedPartners.
const AllowListRefusals = 2

// AllowListHint reports whether that line is due.
func AllowListHint(refusals int) bool { return refusals >= AllowListRefusals }
