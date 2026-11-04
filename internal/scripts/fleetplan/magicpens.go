package fleetplan

import (
	"rsb0t-go/internal/scripts/tile"
)

// PenTier is a pen's Magic band.
type PenTier int

const (
	PenLow PenTier = iota
	PenMid
	PenHillGiant
	PenMossGiant
	PenIceGiant
)

func (t PenTier) String() string {
	switch t {
	case PenMid:
		return "mid"
	case PenHillGiant:
		return "hill giants"
	case PenMossGiant:
		return "moss giants"
	case PenIceGiant:
		return "ice giants"
	}
	return "low"
}

// MagicPenLevels are the Magic levels each tier opens at (settings magicMidPensAt,
// magicHillGiantsAt, magicMossGiantsAt, magicIceGiantsAt).
type MagicPenLevels struct {
	Mid, HillGiants, MossGiants, IceGiants int
	// RunePensOff drops every rune pen (TrainPen.RunePen) from MagicPensFor: setting magicRunePens false,
	// the one switch that undoes the Dwarven Mine and the Ice dungeon pens without a rebuild.
	RunePensOff bool
}

// DefaultMagicPenLevels are the owner's 3 Oct levels.
var DefaultMagicPenLevels = MagicPenLevels{Mid: 20, HillGiants: 30, MossGiants: 40, IceGiants: 50}

// Normalised fills a zero level with its default and clamps each to 1-99.
func (l MagicPenLevels) Normalised() MagicPenLevels {
	fix := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return min(v, 99)
	}
	d := DefaultMagicPenLevels
	return MagicPenLevels{Mid: fix(l.Mid, d.Mid), HillGiants: fix(l.HillGiants, d.HillGiants),
		MossGiants: fix(l.MossGiants, d.MossGiants), IceGiants: fix(l.IceGiants, d.IceGiants), RunePensOff: l.RunePensOff}
}

// MinMagic is the Magic level a tier opens at; PenLow is open to every account.
func (l MagicPenLevels) MinMagic(t PenTier) int {
	switch t {
	case PenMid:
		return l.Mid
	case PenHillGiant:
		return l.HillGiants
	case PenMossGiant:
		return l.MossGiants
	case PenIceGiant:
		return l.IceGiants
	}
	return 0
}

// HasSafeSpots reports whether the pen trains only from safespots.
func (p TrainPen) HasSafeSpots() bool { return len(p.SafeSpots) > 0 }

// OnSafeSpot reports whether t is one of the pen's safespots.
func (p TrainPen) OnSafeSpot(t *tile.Tile) bool {
	if t == nil || t.Level != 0 {
		return false
	}
	for _, s := range p.SafeSpots {
		if s.X == t.X && s.Z == t.Z {
			return true
		}
	}
	return false
}

// SafeCastable reports whether a spell from `from` reaches an NPC of `size` whose south-west tile
// is at, the way the engine judges it: Player.inApproachDistance takes the distance to the body's
// NEAREST tile (CoordGrid.distanceTo) and rsmod's hasLineOfSight casts its ray to the tile of the
// body nearest the caster (the caster's coordinates clamped into the body), so both are judged
// on that one tile, with CastMargin kept inside CastRange because the target moves.
func SafeCastable(from, at tile.Tile, size int, sight func(x, z int) Sight) bool {
	return CastableFrom(from, NearestBodyTile(from, at, size), sight)
}

// NearestBodyTile is the tile of an NPC body of `size` (south-west at `at`) nearest `from`: from's
// coordinates clamped into the body.
func NearestBodyTile(from, at tile.Tile, size int) tile.Tile {
	if size < 1 {
		size = 1
	}
	return tile.Tile{X: max(at.X, min(from.X, at.X+size-1)), Z: max(at.Z, min(from.Z, at.Z+size-1)), Level: at.Level}
}

// BlockerSight is the line of sight a safespot cast trusts, the engine's own sources as far as
// the baked data has them: the collision pack's wall faces (walls in the pack's bit order: 0
// north, 1 east, 2 south, 3 west), and blocked(x, z) for a tile under a projectile-blocking
// centrepiece loc (a loc type with blockrange, over its whole footprint: the engine's
// LOC_PROJ_BLOCKER). A wall that stops a walk but not a spell reads as blocking, which only ever
// refuses a cast. nil when either reader is missing: without them no safespot cast is judged.
func BlockerSight(walls func(x, z int) uint8, blocked func(x, z int) bool) func(x, z int) Sight {
	if walls == nil || blocked == nil {
		return nil
	}
	return func(x, z int) Sight {
		s := Sight(walls(x, z) & 0xf)
		if blocked(x, z) {
			s |= SightFull
		}
		return s
	}
}

// MossGiantsNorthArdougne: content m39_53's four Moss giants (npc 112: combat 42, 60 hitpoints,
// attack/strength/defence 30, max hit 6, huntmode cowardly with huntrange 2, wanderrange 3 so
// maxrange 5, size 2) at 2549,3408 2554,3401 2554,3409 2556,3406. FROM THE SAFESPOTS ONLY: 2563,3401
// and 2562,3399 reach 2554,3401 and 2556,3406, 2563,3409 reaches 2554,3409 and 2556,3406, judged as
// the engine judges a cast (SafeCastable: the body's nearest tile, walls and the loc index's
// projectile blockers); no Moss giant's body can stand beside any of them within its chase (the
// collision-aware search). The fourth giant (2549,3408) is reached only from tiles north-west of
// the herd whose walk passes it (2549-2550,3415-3416, a 48-tile walk round), so it counts only
// when it wanders into reach. Bank: East Ardougne (plain land, cost 152). Hitpoints 15 for the
// walk in: a giant that hunts the account on the last tiles hits 6 at most before the account is
// on a tile it cannot follow to.
//
// Before 3 Oct this pen fought the herd at melee distance, gated Magic 35 / Hitpoints 45; the 45
// kept 45 live accounts at the goblins' hazard edge and most of the rest on the Cows.
var MossGiantsNorthArdougne = TrainPen{Name: "North Ardougne moss giants", Monster: "Moss giant", Tier: PenMossGiant,
	Stand: tile.Tile{X: 2563, Z: 3405}, Leash: 16, Spawns: 3, Retreat: tile.Tile{X: 2580, Z: 3385},
	SafeSpots: []tile.Tile{{X: 2563, Z: 3401}, {X: 2563, Z: 3409}, {X: 2562, Z: 3399}},
	Threats:   ArdougneMossGiantThreats, HP: 60, RespawnTicks: 60,
	MinHitpoints: 15}

// ArdougneMossGiantThreats are the NPCs that can melee near the North Ardougne Moss giants (content
// m39_53; nothing else that hunts is within 32 tiles).
var ArdougneMossGiantThreats = []PenThreat{
	{tile.Tile{X: 2549, Z: 3408}, 2, 6, "Moss giant", true},
	{tile.Tile{X: 2554, Z: 3401}, 2, 6, "Moss giant", true},
	{tile.Tile{X: 2554, Z: 3409}, 2, 6, "Moss giant", true},
	{tile.Tile{X: 2556, Z: 3406}, 2, 6, "Moss giant", true},
}

// KhazardTroopers: the battlefield south-west of West Ardougne, 39 Khazard troopers (npc 475 and
// the spearmen 476: combat 19, 22 hitpoints, attack 17, strength 18 +5/+10, max hit 3, magic
// defence -7, no huntmode, wanderrange 8-9) at 2502-2544,3232-3255. The biggest herd near Stage 3's
// home: plain land from the East Ardougne bank, cost 154. Its hazard is the Khazard commander
// (478 at 2509,3254: combat 48, attack 50, strength 45, huntrange 1, wanderrange 3): his ground,
// wander, hunt and a four-tile margin. Nothing else that hunts is within 25 tiles (the Zombies and
// Ghosts of West Ardougne's graveyard are 28+ north, the Wolves 40 west). Hitpoints 12: three
// max hits of 3 leave the floor's bite in hand.
var KhazardTroopers = TrainPen{Name: "Khazard troopers", Monster: "Khazard trooper", Tier: PenMid,
	Stand: tile.Tile{X: 2522, Z: 3243}, Leash: 14, Spawns: 39, Retreat: tile.Tile{X: 2545, Z: 3262},
	Hazards: []PenHazard{KhazardCommander}, HP: 22,
	MinHitpoints: 12}

// KhazardCommander is the troopers' hazard (content m39_50, npc 478 at 2509,3254).
var KhazardCommander = PenHazard{Name: "Khazard commander (content m39_50)", MinX: 2501, MaxX: 2517, MinZ: 3246, MaxZ: 3262}

// WarriorWomenNorth: four Warrior women (npc 15: combat 24, 20 hitpoints, attack/strength 22 +7,
// max hit 3, magic defence -10, no huntmode) at 2569-2575,3383-3388, beside the goblins' retreat.
// The Moss giants' ground is two tiles west of the westernmost, so the goblins' hazard comes too.
// The other five Warrior women in Ardougne are the pickpocket rung's (the west gate) and the
// market's, and are left to the thieves.
var WarriorWomenNorth = TrainPen{Name: "North Ardougne warrior women", Monster: "Warrior woman", Tier: PenMid,
	Stand: tile.Tile{X: 2572, Z: 3386}, Leash: 8, Spawns: 4, Retreat: tile.Tile{X: 2580, Z: 3385},
	Hazards: []PenHazard{MossGiantsWestOfTheGoblins}, HP: 20,
	MinHitpoints: 12}

// BarbarianVillage: twelve Barbarians on the ground floor (npc 12: combat 7, 14 hitpoints, max
// hit 2, no huntmode, wanderrange 10) at 3076-3096,3409-3444, and five Barbarian WOMEN among them
// (npc 17: combat 8, 14 hitpoints, respawn 50, max hit 2 with one attack every 6 ticks, the same drop
// table; content m48_53), which the pen's herd names too (Also; a user request: "accounts
// training at barbarians dont consider attacking the barbarian women"). FAR: banked at Edgeville
// (cost 75), the walk across from Ardougne the one Stage 3's Aubury and Zaff trips take. Nothing
// hunts within 25 tiles.
var BarbarianVillage = TrainPen{Name: "Barbarian Village barbarians", Monster: "Barbarian", Also: []string{"Barbarian woman"}, Tier: PenMid,
	Stand: tile.Tile{X: 3081, Z: 3425}, Leash: 14, Spawns: 17, Retreat: tile.Tile{X: 3094, Z: 3449},
	HP: 14, RespawnTicks: 50,
	MinHitpoints: 10, Far: true, Bank: tile.Tile{X: 3095, Z: 3491},
	Base: []PenHazard{{Name: "Barbarian Village to the Edgeville bank", MinX: 3070, MaxX: 3112, MinZ: 3400, MaxZ: 3500}}}

// AlKharidWarriors: nine Al-Kharid warriors (npc 18: combat 9, 19 hitpoints, attack 7, strength
// 5 +9, max hit 2, no huntmode) in the palace at 3282-3301,3168-3177. FAR: the Al Kharid bank is a
// 38-cost walk. They come to each other's aid, so Hitpoints 12.
var AlKharidWarriors = TrainPen{Name: "Al-Kharid warriors", Monster: "Al-Kharid warrior", Tier: PenMid,
	Stand: tile.Tile{X: 3290, Z: 3172}, Leash: 12, Spawns: 9, Retreat: tile.Tile{X: 3277, Z: 3169},
	HP: 19, RespawnTicks: 50,
	MinHitpoints: 12, Far: true, Bank: tile.Tile{X: 3269, Z: 3167},
	Base: []PenHazard{{Name: "Al Kharid bank to the palace", MinX: 3260, MaxX: 3305, MinZ: 3160, MaxZ: 3182}}}
