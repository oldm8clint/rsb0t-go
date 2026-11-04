package mulereceiverplan

// Serving pickups in batches, grouped by the place they were asked from.
//
// The account owner's own instruction (2026-09-20): "the mule script should navigate to where the
// request was made — for the fishers, likely Catherby bank; for wood cutters, Seers' Village
// bank", and then "go ahead and do batch requests". The point of batching is that the mule must
// never shuttle one request at a time: 44 fishers and a handful of woodcutters share one mule, and
// a trip per request would spend the whole run walking.
//
// Camps are discovered from the requests themselves rather than configured. Two donors standing at
// the same bank are within a few tiles of each other, and the two banks this fleet actually uses
// are 84 tiles apart, so clustering on proximity separates them with no roster key to keep in sync
// and no list to go stale when a camp moves. The camp's anchor is the EARLIEST request's tile, so
// "where the mule goes" is literally where the longest-waiting account asked from.

import (
	"fmt"
	"sort"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Camp is a cluster of pickup requests close enough together to be served on one trip.
type Camp struct {
	// At is where the mule travels: the tile of the camp's earliest request.
	At tile.Tile
	// Requests are the camp's requests, earliest ask first (GroupByCamp preserves the order
	// PickupQueue.Pending hands it).
	Requests []PickupRequest
	// Oldest is the earliest FirstAt in the camp — what ChooseCamp ranks on.
	Oldest time.Time
	// Slots is every request's slots added up: how much the camp wants to hand over.
	Slots int
}

// Names is the camp's requesters, in queue order — for a log line.
func (c Camp) Names() []string {
	out := make([]string, 0, len(c.Requests))
	for _, r := range c.Requests {
		out = append(out, r.From)
	}
	return out
}

// Holds reports whether name is one of this camp's requesters.
func (c Camp) Holds(name string) bool {
	key := pickupKey(name)
	for _, r := range c.Requests {
		if pickupKey(r.From) == key {
			return true
		}
	}
	return false
}

// GroupByCamp clusters pending requests into camps. A request joins the first camp whose anchor is
// on its level and within radius of it, otherwise it starts a new camp. Feed it
// PickupQueue.Pending's output, which is earliest ask first, and the result is deterministic: camps
// come out in the order their earliest request did, and so does each camp's own list.
func GroupByCamp(pending []PickupRequest, radius int) []Camp {
	if radius < 0 {
		radius = 0
	}
	var camps []Camp
	for _, r := range pending {
		joined := false
		for i := range camps {
			if camps[i].At.Level != r.Tile.Level || tile.Chebyshev(camps[i].At, r.Tile) > radius {
				continue
			}
			camps[i].Requests = append(camps[i].Requests, r)
			camps[i].Slots += r.Slots
			if r.FirstAt.Before(camps[i].Oldest) {
				camps[i].Oldest = r.FirstAt
			}
			joined = true
			break
		}
		if !joined {
			camps = append(camps, Camp{At: r.Tile, Requests: []PickupRequest{r}, Oldest: r.FirstAt, Slots: r.Slots})
		}
	}
	return camps
}

// SameCamp reports whether a camp is the one anchored at at — how "did I just serve this camp"
// is asked. A radius of 0 or less means the same tile exactly.
func SameCamp(c Camp, at tile.Tile, radius int) bool {
	if radius < 0 {
		radius = 0
	}
	return c.At.Level == at.Level && tile.Chebyshev(c.At, at) <= radius
}

// CampChoice is the camp to serve next and the rule that chose it.
type CampChoice struct {
	Camp Camp
	// Reason names the rule, so the log says why the mule went where it went.
	Reason string
}

// ChooseCampInput is what ChooseCamp reads.
type ChooseCampInput struct {
	Camps []Camp
	// Here is where the mule stands, used only to break a tie between equally old camps.
	Here tile.Tile
	// ServedAt is the anchor of the camp the last batch was served at, nil if none, and InARow
	// is how many batches in a row were served there.
	ServedAt *tile.Tile
	InARow   int
	// MaxInARow caps consecutive batches at one camp while another camp is waiting; 0 or less
	// switches the cap off and leaves the choice purely on age.
	MaxInARow int
	// SameCampRadius decides which camps count as ServedAt's own.
	SameCampRadius int
}

// ChooseCamp picks the camp to serve next. The rule, stated plainly because starvation is the
// thing that goes wrong here:
//
//  1. **The longest-waiting request wins.** Camps are ranked by their oldest request's first ask,
//     not by how many requests they hold. Ranking by size would let 44 fishers at one bank
//     out-vote two woodcutters at another for as long as the fleet runs.
//  2. **A camp cannot be served more than MaxInARow times in a row while another camp is
//     waiting.** Age alone is nearly enough on its own, but "nearly" is what starvation is made
//     of: a request that is cancelled and re-made (a donor that gave up and came back after
//     donorplan.HandoffRetry) gets a fresh FirstAt, so a busy camp keeps renewing its own age.
//     The cap is the backstop that does not depend on that reasoning holding.
//
// Ties go to the camp nearer Here, then to the lower coordinates, so the same queue always
// produces the same choice.
func ChooseCamp(in ChooseCampInput) (CampChoice, bool) {
	if len(in.Camps) == 0 {
		return CampChoice{}, false
	}
	if in.MaxInARow > 0 && in.ServedAt != nil && in.InARow >= in.MaxInARow {
		var others []Camp
		for _, c := range in.Camps {
			if !SameCamp(c, *in.ServedAt, in.SameCampRadius) {
				others = append(others, c)
			}
		}
		if len(others) > 0 {
			return CampChoice{
				Camp:   oldestFirst(others, in.Here),
				Reason: fmt.Sprintf("%d batch(es) in a row at the last camp and %d other camp(s) waiting", in.InARow, len(others)),
			}, true
		}
	}
	return CampChoice{Camp: oldestFirst(in.Camps, in.Here), Reason: "it holds the longest-waiting request"}, true
}

// oldestFirst is the camp whose earliest request is earliest, ties broken by distance from here
// and then by coordinate so the answer never depends on map order.
func oldestFirst(camps []Camp, here tile.Tile) Camp {
	ranked := append([]Camp(nil), camps...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if !a.Oldest.Equal(b.Oldest) {
			return a.Oldest.Before(b.Oldest)
		}
		da, db := tile.Chebyshev(a.At, here), tile.Chebyshev(b.At, here)
		if da != db {
			return da < db
		}
		if a.At.X != b.At.X {
			return a.At.X < b.At.X
		}
		return a.At.Z < b.At.Z
	})
	return ranked[0]
}
