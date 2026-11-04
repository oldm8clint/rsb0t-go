package fleetrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
)

// MuleDeps adapts d to donorrunner's Deps, with the bus and this account's own name.
func MuleDeps(d *Deps, bus *mulebus.Bus, me string) *donorrunner.Deps {
	return &donorrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: donorrunner.Delayer(d.Delay), Log: d.Log,
		Bus: bus, Me: me,
	}
}

// defaultKitPerOrder bounds one kit ask when the caller names no size of its own.
//
// This used to be orderplan.QtyMax, which was 1000 and read as a sane default. QtyMax is now a
// million -- it had to rise so a coin order could fund a 150,000-coin rune axe at all -- and a
// kit ask has no business inheriting that. A kit is armour, food and runes in ordinary counts,
// so the number Scarab was really relying on is written here instead of borrowed from a
// grammar bound that moved underneath it.
const defaultKitPerOrder = 1000

// KitResult is what a kit request actually delivered, by pack delta — never by a trade closing.
type KitResult struct {
	Got      map[string]int
	Declined map[string]string
	// Banked is what a bundled kit delivered into the account's own bank and left there: the notes drawn
	// back as items only as far as the pack had room (donorrunner.UnnoteKit), so a 10,000-line of raw fish
	// is about 20 in the pack (Got) and the rest here. Delivered is Got + Banked. nil for the per-item
	// fallback, which has no bank.
	Banked map[string]int
	// Gave is what this account handed the mule back in the kit's own trade (Deps.KitGive), by obj name; nil when
	// nothing crossed that way.
	Gave map[string]int
}

// RequestKitFromMule asks the mule for each kit item in turn, in orders of at most perOrder (and
// never more than defaultKitPerOrder), standing where the mule can see the
// account. Each item stops at the first decline or silent timeout, so a mule without stock costs
// one ask, not a loop. The account must already be in the mule's sight: its post is where the
// caller walks first.
func RequestKitFromMule(d *Deps, bus *mulebus.Bus, me, mule string, items []fleetplan.ItemCount, perOrder int, timeout time.Duration) KitResult {
	res := KitResult{Got: map[string]int{}, Declined: map[string]string{}}
	if perOrder <= 0 || perOrder > defaultKitPerOrder {
		perOrder = defaultKitPerOrder
	}
	dd := MuleDeps(d, bus, me)
	for _, it := range items {
		left := it.Count
		for left > 0 {
			ask := left
			if ask > perOrder {
				ask = perOrder
			}
			before := CountPackNamed(d, it.Item)
			ans := donorrunner.Await(dd, donorrunner.AwaitOptions{Mule: mule, Me: me, Item: it.Item, Qty: ask, Bus: bus, Timeout: timeout})
			got := CountPackNamed(d, it.Item) - before
			if got > 0 {
				res.Got[it.Item] += got
				left -= got
			}
			if ans.Stopped {
				return res
			}
			if got > 0 && left <= 0 {
				// Judged by this account's own pack by NAME: Await's own success test resolves the
				// item to one object id, and "Coins" names two in this config.
				d.Logf("scarab kit: %s handed %d %s", mule, got, it.Item)
				break
			}
			if got <= 0 {
				reason := ans.Reason
				if ans.Declined {
					reason = "declined: " + reason
				}
				res.Declined[it.Item] = reason
				d.Logf("scarab kit: %s from %s — %s", it.Item, mule, reason)
				break
			}
			d.Logf("scarab kit: %s handed %d %s (%d still wanted)", mule, got, it.Item, left)
		}
	}
	return res
}

// CountPackNamed is how many of name the pack holds, notes included. A qualified name ("Yew shortbow
// id857", cachecfg.QualifiedName) counts that one obj and its note: a strung bow and its unstrung form
// share a display name and a kit asks for one of them.
func CountPackNamed(d *Deps, name string) int {
	n := 0
	for _, it := range PackSlotItems(d) {
		if packItemIs(d, it.ID, name) {
			n += int(it.Count)
		}
	}
	return n
}

// packItemIs reports whether obj id answers to name (cachecfg.Config.IsNamed: a plain name is every obj
// of that display name, a qualified one only that obj and its notes).
func packItemIs(d *Deps, id int32, name string) bool {
	if d.Config == nil {
		return false
	}
	return d.Config.IsNamed(name, int(id))
}

// DescribeKit is the one-line log form of a kit.
func DescribeKit(items []fleetplan.ItemCount) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%d %s", it.Count, it.Item))
	}
	return strings.Join(parts, ", ")
}
