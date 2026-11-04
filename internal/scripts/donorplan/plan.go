// Package donorplan is a skilling account's side of handing its bank to the mule, decided from
// plain records: when a hand-over is due, what leaves the bank, what fits on the trade screen, and
// the in-game steps of the hand-over itself. plan.go is ported from rsb0t-scripts' donorPlan.ts;
// handover.go replaces the browser wall's bus with in-game steps only (see its comment).
//
// It is pure: no client, actions, fleet or bot imports and no I/O. Items are named by strings.
package donorplan

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// MuleSeenTTL is how long a sighting of the mule still says where it is.
const MuleSeenTTL = 15 * time.Second

// HandoffDue reports whether a hand-over is due: at the threshold, and not before nextAllowed
// after a refused or failed one. A threshold of 0 never hands over.
func HandoffDue(banked, threshold int, now, nextAllowed time.Time) bool {
	return threshold > 0 && banked >= threshold && !now.Before(nextAllowed)
}

// ItemRow is one bank or pack row. An empty Name is an empty slot; ID tells notes from items.
type ItemRow struct {
	Name  string
	ID    int
	Count int
}

// Rules say what stays with the donor and what goes first.
type Rules struct {
	// Keep names stay with the donor, matched case-insensitively.
	Keep []string
	// Skip keeps anything else by rule (a tool tier, say).
	Skip func(ItemRow) bool
	// First goes to the front of the withdrawal and the trade.
	First func(ItemRow) bool
	// Untradeable reports items the game will not let cross a trade; nil treats everything as
	// tradeable. Why: an untradeable item on the trade screen only stalls the hand-over.
	Untradeable func(ItemRow) bool
}

// InKeep reports whether name is on the keep list. A blank name always stays.
func InKeep(name string, keep []string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	for _, k := range keep {
		if strings.ToLower(strings.TrimSpace(k)) == n {
			return true
		}
	}
	return false
}

// Stays reports whether row stays with the donor. Content's own untradeable items always stay:
// live 2026-09-19, donor spare1 drew 24 bank rows including 15 quest and random-event items,
// spent four offers on each, and ran past the mule's 30s confirm deadline.
func (r Rules) Stays(row ItemRow) bool {
	return InKeep(row.Name, r.Keep) || (r.Skip != nil && r.Skip(row)) || (r.Untradeable != nil && r.Untradeable(row)) || Untradeable(row.ID)
}

// Untradeable reports whether content marks obj id tradeable=no (see untradeable_gen.go).
// Id 0 is treated as unknown: rows built without an id must not match obj 0.
func Untradeable(id int) bool { return id > 0 && untradeableIDs[id] }

func (r Rules) first(row ItemRow) bool {
	return r.First != nil && r.First(row)
}

// HandoffRows is what leaves the bank for the mule: the priority rows first, then everything else
// not kept, each group in bank order.
func HandoffRows(bank []ItemRow, r Rules) []ItemRow {
	var first, rest []ItemRow
	for _, row := range bank {
		if row.Count <= 0 || r.Stays(row) {
			continue
		}
		if r.first(row) {
			first = append(first, row)
		} else {
			rest = append(rest, row)
		}
	}
	return append(first, rest...)
}

// HaulOf counts pack slots holding something for the mule.
func HaulOf(pack []ItemRow, r Rules) int {
	n := 0
	for _, row := range pack {
		if !r.Stays(row) {
			n++
		}
	}
	return n
}

// LoadGroup is one item on the trade screen and the pack slots it occupies; a stack is one.
type LoadGroup struct {
	Name  string
	ID    int
	Slots int
}

// PlanLoad is what goes on the trade screen: the haul grouped by item, the priority item first,
// cut off where the mule's free slots run out. A group is offered whole, so one that does not fit
// waits for the next load rather than being split.
func PlanLoad(pack []ItemRow, freeSlots int, r Rules) []LoadGroup {
	type group struct {
		LoadGroup
		first bool
	}
	var groups []*group
	index := map[string]*group{}
	for _, item := range pack {
		if item.Name == "" || r.Stays(item) {
			continue
		}
		key := strings.ToLower(item.Name) + "#" + strconv.Itoa(item.ID)
		if g, ok := index[key]; ok {
			g.Slots++
			continue
		}
		g := &group{LoadGroup: LoadGroup{Name: item.Name, ID: item.ID, Slots: 1}, first: r.first(item)}
		index[key] = g
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].first && !groups[j].first })
	out := []LoadGroup{}
	used := 0
	for _, g := range groups {
		if used+g.Slots > freeSlots {
			continue
		}
		out = append(out, g.LoadGroup)
		used += g.Slots
	}
	return out
}

// MuleNear reports whether the mule is on our level within range tiles.
func MuleNear(mule, me *tile.Tile, rangeTiles int) bool {
	return mule != nil && me != nil && mule.Level == me.Level && tile.Chebyshev(*mule, *me) <= rangeTiles
}

// SeenFresh reports whether a sighting at `at` is younger than ttl. A zero time was never seen.
func SeenFresh(at, now time.Time, ttl time.Duration) bool {
	return !at.IsZero() && now.Sub(at) < ttl
}
