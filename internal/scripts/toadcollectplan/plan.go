// Package toadcollectplan is the ToadCollector's decisions: which step comes next for an account that picks up the
// Swamp toad ground spawns in the Gnome swamp, pulls the legs off at once and hands the Toad's legs to the fleet's
// mule (Toad's legs are the second ingredient of an Agility potion). It is pure: no client, actions, fleet or bot
// imports and no I/O.
//
// Facts (content maps, OBJ section "0 x z: 2150 1", world X = mapX*64+x, Z = mapZ*64+z): the Swamp toad (obj 2150)
// spawns are listed in Spawns. The toad's first inventory option "Remove-legs" (skill_cooking gnome_cooking
// swamp_toad.rs2) deletes it and adds Toad's legs (obj 2152) with no delay. Ground spawns are per world, so one
// collector serves one world.
package toadcollectplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// Item names.
const (
	ToadName = "Swamp toad"
	LegsName = "Toad's legs"
)

// Spot is one ground spawn.
type Spot struct {
	Name string
	Tile tile.Tile
}

func sp(x, z int) Spot { return Spot{Name: "Gnome swamp", Tile: tile.Tile{X: x, Z: z, Level: 0}} }

// Spawns are the Swamp toad spawns of content map m37_54 (the Gnome swamp, x 2407..2428, z 3508..3519), the densest
// cluster. The m37_53 spawns to the south (2381,3414 .. 2408,3408) and the three at Taverley (2903,3400, 2907,3394,
// 2908,3410) are not used.
var Spawns = []Spot{
	sp(2407, 3516), sp(2409, 3514), sp(2411, 3512), sp(2412, 3519), sp(2413, 3511), sp(2415, 3518),
	sp(2416, 3512), sp(2417, 3508), sp(2417, 3512), sp(2417, 3515), sp(2417, 3516), sp(2418, 3511),
	sp(2418, 3517), sp(2421, 3509), sp(2421, 3519), sp(2424, 3514), sp(2424, 3517), sp(2428, 3510),
}

// Post is where the collector idles: the middle of the cluster, so every spawn is inside the loaded scene.
var Post = tile.Tile{X: 2418, Z: 3514, Level: 0}

// Defaults and bounds.
const (
	DefaultMule           = "mule"
	DefaultHandOffAt      = 24
	DefaultHandOffMinutes = 30
	// PackSlots is a full pack.
	PackSlots = 28
	// TravelRadius is how far from Post the collector may drift before it walks back.
	TravelRadius = 20
	// PickRadius is how close to a spawn tile a pickup is walked to first.
	PickRadius = 1
	// HeartbeatEvery is the longest the script stays silent (the stall watchdog acts on ten minutes).
	HeartbeatEvery = 4 * time.Minute
)

// StepKind is what the collector does next.
type StepKind string

// Steps.
const (
	// StepClear banks a previous script's kit at the nearest ordinary bank.
	StepClear StepKind = "clear"
	// StepSkin pulls the legs off every Swamp toad in the pack.
	StepSkin    StepKind = "skin"
	StepHandOff StepKind = "handoff"
	// StepBank banks a pack full of legs at the bank by the mule's post, while a failed hand-off cools down.
	StepBank   StepKind = "bank"
	StepPick   StepKind = "pick"
	StepTravel StepKind = "travel"
	StepWait   StepKind = "wait"
)

// Step is one decision. Spot is set for StepPick.
type Step struct {
	Kind StepKind
	Spot Spot
}

// Snapshot is everything Decide reads.
type Snapshot struct {
	Here tile.Tile
	// HeldLegs is the Toad's legs in the pack, HeldToads the Swamp toads still whole.
	HeldLegs, HeldToads int
	// BankLegs is the legs this account's own bank held at the last visit; the hand-off draws them.
	BankLegs int
	// Kit is true while the pack holds anything that is neither a toad nor legs.
	Kit       bool
	FreeSlots int
	// Visible are the spawns with a Swamp toad on the ground right now.
	Visible []Spot
	// MuleName is the mule to hand to; blank means never hand over (then a full pack goes to the bank).
	MuleName string
	// HandOffAt is the legs held that make a hand-off due; HandOffEvery the time since the last hand-off after
	// which any legs held are handed over.
	HandOffAt    int
	HandOffEvery time.Duration
	SinceHandOff time.Duration
	// HandOffRetryOK is false while a failed hand-off is cooling down.
	HandOffRetryOK bool
}

// HandOffDue says a hand-off is wanted now: a mule is set, legs are held or banked, the cooldown allows it, and
// either the count or the timer says so.
func HandOffDue(s Snapshot) bool {
	if strings.TrimSpace(s.MuleName) == "" || s.HeldLegs+s.BankLegs <= 0 || !s.HandOffRetryOK {
		return false
	}
	return s.HeldLegs+s.BankLegs >= s.HandOffAt || s.SinceHandOff >= s.HandOffEvery
}

// Decide is the next step. Order: bank a previous script's kit; pull legs off held toads; hand over what is due;
// unload a full pack at the bank; take a visible spawn (nearest first); come back to the post; otherwise wait.
func Decide(s Snapshot) Step {
	if s.Kit {
		return Step{Kind: StepClear}
	}
	if s.HeldToads > 0 {
		return Step{Kind: StepSkin}
	}
	if HandOffDue(s) {
		return Step{Kind: StepHandOff}
	}
	if s.FreeSlots <= 0 {
		if s.HeldLegs > 0 {
			return Step{Kind: StepBank}
		}
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

// IsToad reports whether an item name is a whole Swamp toad.
func IsToad(name string) bool { return strings.EqualFold(name, ToadName) }

// IsLegs reports whether an item name is Toad's legs.
func IsLegs(name string) bool { return strings.EqualFold(name, LegsName) }

func rules() donorplan.Rules {
	return donorplan.Rules{Skip: func(r donorplan.ItemRow) bool { return !IsLegs(r.Name) }}
}

// MuleHaul is how many pack slots still hold something for the mule.
func MuleHaul(pack []donorplan.ItemRow) int { return donorplan.HaulOf(pack, rules()) }

// MuleLoad is the trade-screen plan, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, rules())
}

// Count totals the legs and whole toads in a set of pack rows.
func Count(pack []donorplan.ItemRow) (legs, toads int) {
	for _, r := range pack {
		c := max(1, r.Count)
		switch {
		case IsLegs(r.Name):
			legs += c
		case IsToad(r.Name):
			toads += c
		}
	}
	return legs, toads
}
