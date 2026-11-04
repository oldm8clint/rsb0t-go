package fleetplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
)

// CastGap is the least time between two cast packets at a safespot: content's %action_delay is
// map_clock + 5 after a combat spell, and a cast inside it is re-queued (p_opnpct) instead of
// cast, re-checking the reach every tick. Six ticks, so the next one is read after the delay.
const CastGap = 6 * 600 * time.Millisecond

// SafeCastSteps is how many steps of the target's own walk a safespot cast must survive: the
// client sees the giant a tick late, the server reads the cast a tick or two later, and a giant
// walks a tile a tick.
const SafeCastSteps = 1

// SafeCastableMoving is SafeCastable that also survives the target's walk: the spell reaches the
// body where it stands with CastMargin to spare, and still reaches it (within CastRange, in line
// of sight) wherever SafeCastSteps steps in any direction could put it. The server judges the cast
// at the tick it reads the packet, not the tick the client looked.
func SafeCastableMoving(from, at tile.Tile, size int, sight func(x, z int) Sight) bool {
	if !SafeCastable(from, at, size, sight) {
		return false
	}
	for dx := -SafeCastSteps; dx <= SafeCastSteps; dx++ {
		for dz := -SafeCastSteps; dz <= SafeCastSteps; dz++ {
			moved := tile.Tile{X: at.X + dx, Z: at.Z + dz, Level: at.Level}
			near := NearestBodyTile(from, moved, size)
			if from.Level != near.Level || tile.Chebyshev(tile.Tile{X: from.X, Z: from.Z}, tile.Tile{X: near.X, Z: near.Z}) > CastRange {
				return false
			}
			if !LineOfSight(from, near, sight) {
				return false
			}
		}
	}
	return true
}

// SafeLootSettings are a safespot pen's loot rules (settings safespotLoot, safespotGrabMinCoins,
// safespotGrabMinValue, safespotGrabLawKit).
type SafeLootSettings struct {
	// On is safespotLoot (default true).
	On bool
	// MinCoins is the least coin pile worth a law rune (default 100).
	MinCoins int
	// MinValue is the least value (content oc_cost times count) of any other drop worth a law rune
	// (default 500); runes and gems are taken whatever their value.
	MinValue int
	// LawKit is how many law runes the account asks the mule for while it trains at a safespot
	// pen at Magic 33+ (default 60; 0 asks for none).
	LawKit int
	// PrayerGoal is Stage 5's Prayer goal: big bones are grabbed while Prayer is below it (0: always).
	PrayerGoal int
}

// Normalised clamps the numbers: no negative threshold or kit, a kit of at most 500.
func (s SafeLootSettings) Normalised() SafeLootSettings {
	s.MinCoins = max(s.MinCoins, 0)
	s.MinValue = max(s.MinValue, 0)
	s.LawKit = min(max(s.LawKit, 0), 500)
	return s
}

// SafeDrop is one ground item at a safespot pen, as the loot rule reads it.
type SafeDrop struct {
	Name      string
	Count     int
	Cost      int // content oc_cost of one
	Stackable bool
	At        tile.Tile
	// ID is the obj id (0 unknown): grimy herbs are all named "Herb" and told apart only by it.
	ID int
}

// HerbGrabMaxRank is the worst herb (combatplan.HerbIDs rank) worth a Telekinetic Grab's law
// rune: irit and better (owner, 4 Oct: "looting herbs especially ranarrs"). The low herbs
// (harralander down to guam) are picked up by hand only.
const HerbGrabMaxRank = 9

// IsGemName reports an uncut or cut gem (Uncut sapphire, Ruby, Dragonstone...).
func IsGemName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "uncut ") {
		return true
	}
	switch n {
	case "sapphire", "emerald", "ruby", "diamond", "dragonstone", "opal", "jade", "red topaz":
		return true
	}
	return false
}

// IsBigBones reports the giants' Big bones.
func IsBigBones(name string) bool { return strings.EqualFold(strings.TrimSpace(name), "Big bones") }

// WorthGrab reports whether a drop is worth a Telekinetic Grab's law rune: Big bones while Prayer
// is below the goal, any runes, gems, coins from MinCoins, and anything else worth MinValue.
func (s SafeLootSettings) WorthGrab(d SafeDrop, prayer int) bool {
	n := strings.TrimSpace(d.Name)
	if rank, herb := combatplan.HerbRank(d.ID); herb {
		return rank <= HerbGrabMaxRank
	}
	switch {
	case n == "":
		return false
	case IsBigBones(n):
		return s.PrayerGoal <= 0 || prayer < s.PrayerGoal
	case strings.Contains(strings.ToLower(n), "bones"):
		return false
	case IsRuneName(n), IsGemName(n):
		return true
	case strings.EqualFold(n, "Coins"):
		return d.Count >= s.MinCoins
	}
	return d.Cost*max(d.Count, 1) >= s.MinValue
}

// PenThreat is an NPC spawn that can melee near a safespot pen: its spawn (south-west tile), its
// body size, its chase cut (maxrange + 1; maxrange defaults to wanderrange + 2) and whether it is
// the pen's own herd.
type PenThreat struct {
	At   tile.Tile
	Size int
	Cut  int
	Name string
	Herd bool
}

// Holds reports whether the NPC keeps a melee target on t: Npc.targetWithinMaxRange, the exact
// corner excluded.
func (p PenThreat) Holds(t tile.Tile) bool {
	if t.Level != p.At.Level {
		return false
	}
	dx, dz := Abs(t.X-p.At.X), Abs(t.Z-p.At.Z)
	return max(dx, dz) <= p.Cut && !(dx == p.Cut && dz == p.Cut)
}
