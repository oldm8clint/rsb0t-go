// Package cannonfacts holds the few pure facts about the Dwarf Cannon that more than one script's plan reads: its life, its
// parts, the set's price, how much one stint carries, and the record an account keeps of a cannon it may still have standing.
// They live here, apart from any one script's plan, so the scripts that plan around a cannon share them without importing each other.
package cannonfacts

import "time"

const (
	// CannonDecay is a cannon's life after its last stage change: ^cannon_decay_length = 2500 ticks of 600 ms.
	CannonDecay = 2500 * 600 * time.Millisecond
	// CannonPartsNeeded is the parts: base, stand, barrels, furnace.
	CannonPartsNeeded = 4
	// CannonSetPrice is the full set's price in coins (nulodion.rs2 [label,ill_take_a_cannon]).
	CannonSetPrice = 750000
	// MinRangedCoinReserve is the least coins an account keeps through a purchase, whatever the KQ kit says: a walk home and a toll.
	MinRangedCoinReserve = 25000
	// ClaimGrace is how long past the decay a claim waits: the decay timer checks every five ticks and the record's time is the
	// client's reading of the last stage message.
	ClaimGrace = 2 * time.Minute
	// RangedBallsCarry is the most Cannonballs one stint draws from the bank: at about one a tick of fire they are the stint's
	// clock, and a stack is one slot.
	RangedBallsCarry = 3000
)

// RangedRecord is what an account must remember about its cannon across a restart: whether one of its own may still stand in the
// world. It is persisted in the checkpoint. Its absence (the zero value) means no cannon out.
type RangedRecord struct {
	// PlacedAt is the RFC3339 time of the last stage message of a cannon set up and not yet picked up ("" none). The decay runs
	// from it (CannonDecay).
	PlacedAt string `json:"placedAt,omitempty"`
	// X and Z are the tile the account stood on to set it up (the cannon's centre).
	X int `json:"x,omitempty"`
	Z int `json:"z,omitempty"`
	// Bought counts the sets this account has bought from Nulodion; BoughtAt is the last (RFC3339).
	Bought   int    `json:"bought,omitempty"`
	BoughtAt string `json:"boughtAt,omitempty"`
	// Claimed counts the cannons Nulodion has given back.
	Claimed int `json:"claimed,omitempty"`
}

// Placed reports a cannon of this account's that may still stand (or have decayed without Nulodion clearing it).
func (r RangedRecord) Placed() bool { return r.PlacedAt != "" }

// PlacedTime is PlacedAt parsed, zero when unset or unreadable.
func (r RangedRecord) PlacedTime() time.Time {
	if r.PlacedAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, r.PlacedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// DecayedBy is when the recorded cannon has certainly decayed: its last stage plus the life, plus ClaimGrace. An unreadable time
// reads as "now plus the whole life" by the caller's clock via Wait; here it is the zero time.
func (r RangedRecord) DecayedBy() time.Time {
	t := r.PlacedTime()
	if t.IsZero() {
		return time.Time{}
	}
	return t.Add(CannonDecay + ClaimGrace)
}

// WithPlaced is the record with a cannon set up at the tile at the time.
func (r RangedRecord) WithPlaced(x, z int, at time.Time) RangedRecord {
	r.PlacedAt, r.X, r.Z = at.UTC().Format(time.RFC3339), x, z
	return r
}

// WithoutPlaced is the record with the cannon out of the world: picked up, or claimed back.
func (r RangedRecord) WithoutPlaced() RangedRecord {
	r.PlacedAt, r.X, r.Z = "", 0, 0
	return r
}
