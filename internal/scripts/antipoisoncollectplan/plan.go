// Package antipoisoncollectplan is the AntipoisonCollector's decisions: which step comes next for an account that
// picks up the two Antipoison(1) ground spawns at the Observatory (north of Castle Wars) and hands the
// doses to the fleet's mule. It is pure: no client, actions, fleet or bot imports and no I/O.
//
// Facts (content maps m38_49.jm2, OBJ section): the spawns are 2437,3164,0 and 2467,3177,0, each one
// Antipoison(1), no respawnrate set so the engine default (100 ticks, about 60 s) applies. Ground
// spawns are per world, so one collector serves one world.
package antipoisoncollectplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Spot is one ground spawn.
type Spot struct {
	Name string
	Tile tile.Tile
}

// Spawns are the two Antipoison(1) ground spawns.
var Spawns = []Spot{
	{Name: "Observatory west", Tile: tile.Tile{X: 2437, Z: 3164, Level: 0}},
	{Name: "Observatory east", Tile: tile.Tile{X: 2467, Z: 3177, Level: 0}},
}

// Post is where the collector idles: between the spawns (15 tiles from each), so both sit well inside the
// loaded scene and each one's respawn is seen the moment it happens.
var Post = tile.Tile{X: 2452, Z: 3170, Level: 0}

// Defaults and bounds.
const (
	DefaultMule           = "mule"
	DefaultHandOffAt      = 20
	DefaultHandOffMinutes = 30
	// PackSlots is a full pack.
	PackSlots = 28
	// TravelRadius is how far from Post the collector may drift before it walks back: the spawns are
	// each 15 tiles from Post, so a pickup trip never reaches it.
	TravelRadius = 25
	// PickRadius is how close to a spawn tile a pickup is walked to first (0 is standing on it).
	PickRadius = 1
	// HeartbeatEvery is the longest the script stays silent (the stall watchdog acts on ten minutes).
	HeartbeatEvery = 4 * time.Minute
)

// StepKind is what the collector does next.
type StepKind string

// Steps.
const (
	// StepClear banks a previous script's kit (and draws any doses already banked) at the nearest ordinary bank.
	StepClear   StepKind = "clear"
	StepHandOff StepKind = "handoff"
	// StepBank banks a pack full of potions at the bank by the mule's post, while a failed hand-off cools down.
	StepBank   StepKind = "bank"
	StepPick   StepKind = "pick"
	StepTravel StepKind = "travel"
	StepWait   StepKind = "wait"
)

// Step is one decision. Spot is set for StepPick (the spawn to take from).
type Step struct {
	Kind StepKind
	Spot Spot
}

// Snapshot is everything Decide reads.
type Snapshot struct {
	Here tile.Tile
	// HeldPots is the Antipoison potions in the pack (each takes a slot), HeldDoses their doses.
	HeldPots, HeldDoses int
	// BankPots and BankDoses are the antipoison this account's own bank held at the last visit; the hand-off draws them.
	BankPots, BankDoses int
	// Kit is true while the pack holds anything that is not an antipoison.
	Kit       bool
	FreeSlots int
	// Visible are the spawns with an Antipoison on the ground right now.
	Visible []Spot
	// MuleName is the mule to hand to; blank means never hand over (then a full pack goes to the bank).
	MuleName string
	// HandOffAt is the doses held that make a hand-off due; HandOffEvery the time since the last
	// hand-off (or start) after which any doses held are handed over.
	HandOffAt    int
	HandOffEvery time.Duration
	SinceHandOff time.Duration
	// HandOffRetryOK is false while a failed hand-off is cooling down (donorplan.HandoffRetry).
	HandOffRetryOK bool
}

// HandOffDue says a hand-off is wanted now: a mule is set, something is held or banked, the cooldown allows
// it, and either the dose count or the timer says so.
func HandOffDue(s Snapshot) bool {
	if strings.TrimSpace(s.MuleName) == "" || s.HeldPots+s.BankPots <= 0 || !s.HandOffRetryOK {
		return false
	}
	return s.HeldDoses+s.BankDoses >= s.HandOffAt || s.SinceHandOff >= s.HandOffEvery
}

// Decide is the next step. Order: bank a previous script's kit; hand over what is due; unload a full pack
// at the bank; take a visible spawn (nearest first); come back to the post; otherwise wait.
func Decide(s Snapshot) Step {
	if s.Kit {
		return Step{Kind: StepClear}
	}
	if HandOffDue(s) {
		return Step{Kind: StepHandOff}
	}
	if s.FreeSlots <= 0 {
		if s.HeldPots > 0 {
			return Step{Kind: StepBank}
		}
		// Full of something that is not ours: nothing can be picked up.
		return Step{Kind: StepWait}
	}
	if len(s.Visible) > 0 {
		best := s.Visible[0]
		for _, v := range s.Visible[1:] {
			if tile.Chebyshev(s.Here, v.Tile) < tile.Chebyshev(s.Here, best.Tile) {
				best = v
			}
		}
		return Step{Kind: StepPick, Spot: best}
	}
	if s.Here.Level != Post.Level || tile.Chebyshev(s.Here, Post) > TravelRadius {
		return Step{Kind: StepTravel}
	}
	return Step{Kind: StepWait}
}

// IsAntipoison reports whether an item name is an Antipoison(n) potion.
func IsAntipoison(name string) bool { return fleetplan.AntipoisonDoses(name) > 0 }

func rules() donorplan.Rules {
	return donorplan.Rules{Skip: func(r donorplan.ItemRow) bool { return !IsAntipoison(r.Name) }}
}

// MuleHaul is how many pack slots still hold something for the mule.
func MuleHaul(pack []donorplan.ItemRow) int { return donorplan.HaulOf(pack, rules()) }

// MuleLoad is the trade-screen plan, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, rules())
}

// DecantPair picks the next pour among the pack's partial antipoisons (fewer than 4 doses): the smallest is used on the
// largest, so the pack converges on Antipoison(4)s and at most one partial (content skill_herblore decant_potion.rs2:
// using one potion on another of the same kind fills the target to 4 and leaves the rest, or an empty vial, behind).
// ok is false when fewer than two partials are held.
func DecantPair(pack []donorplan.ItemRow) (from, onto donorplan.ItemRow, ok bool) {
	var partial []donorplan.ItemRow
	for _, r := range pack {
		if n := fleetplan.AntipoisonDoses(r.Name); n > 0 && n < 4 {
			for i := 0; i < max(1, r.Count); i++ {
				partial = append(partial, r)
			}
		}
	}
	if len(partial) < 2 {
		return donorplan.ItemRow{}, donorplan.ItemRow{}, false
	}
	lo, hi := 0, 1
	for i, r := range partial {
		n := fleetplan.AntipoisonDoses(r.Name)
		if n < fleetplan.AntipoisonDoses(partial[lo].Name) {
			lo = i
		}
		if n > fleetplan.AntipoisonDoses(partial[hi].Name) {
			hi = i
		}
	}
	if lo == hi {
		hi = (lo + 1) % len(partial)
	}
	return partial[lo], partial[hi], true
}

// Doses totals the doses in a set of pack rows (Count potions of each form).
func Doses(pack []donorplan.ItemRow) (pots, doses int) {
	for _, r := range pack {
		n := fleetplan.AntipoisonDoses(r.Name)
		if n == 0 {
			continue
		}
		c := r.Count
		if c < 1 {
			c = 1
		}
		pots += c
		doses += c * n
	}
	return pots, doses
}
