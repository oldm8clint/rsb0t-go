package fleetplan

import (
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// TrainPen is one training herd.
type TrainPen struct {
	Name    string
	Monster string // the npc NAME the target search matches
	// Also names the other NPCs of the same herd the target search matches (the Barbarian women among the
	// Barbarians): the same hitpoints, respawn and drops, counted in Spawns. Read through Herd and IsHerd.
	Also   []string
	Stand  tile.Tile
	Leash  int // targets are looked for within this Chebyshev distance of Stand
	Spawns int // how many of Monster content places here
	// Drops, when set, is everything Monster can drop (content: its death_drop and its drop table
	// script). A pile taken at the pen that no kill claims goes on the monster's card only when it
	// is one of these (scarabrunner.penSource): anything else lying at the pen is another player's,
	// arrows or runes, and is recorded as a pickup of its own. Empty: not listed, any pile counts.
	Drops []string
	// Retreat is where a fight broken off at the health floor walks to: out of the herd's chase
	// range and on the way every walk in has taken.
	Retreat tile.Tile
	// Hazards is ground inside the leash where a target must not be taken, because something that
	// kills a training account reaches it (SafeTarget).
	Hazards []PenHazard
	// MagicOnly is a herd no melee reaches (the Ducks on the Ardougne lake): Stage 3's spells
	// train on it, Stage 5 never gets it (PensFor, ForMelee).
	MagicOnly bool
	// CastFrom is a magic-only pen's land: the walkable tiles, reachable from the East Ardougne
	// bank and on none of its Hazards, that the account casts from (CastTile). A target no tile
	// here reaches is not this pen's.
	CastFrom []tile.Tile
	// Tier is the pen's Magic band ("magicgiants", 3 Oct 2026; magicpens.go): PenLow for every
	// pen before that day (the Cows, the goblins, the chickens, the lake Ducks), PenMid for the
	// Khazard troopers, Warrior women, Barbarians and Al-Kharid warriors, and a giant tier per
	// herd. The tier's Magic level comes from the settings (MagicPenLevels); melee never gets a pen
	// above PenLow.
	Tier PenTier
	// TopTier, when above Tier, is the tier the pen ranks in for an account that clears that
	// tier's Magic level (MagicPensFor raises Tier to it): the Ogres are combat 53 with 60
	// hitpoints, an Ice giant's level, so a Magic 50+ account takes their seats before any other
	// giant pen's (a user request: "yeah use the ogres yanille", for the Ice giant tier whose
	// only safe herd is deep in the Wilderness). Below that level the pen is in Tier as before.
	TopTier PenTier
	// MinHitpoints gates the pen by the account's BASE Hitpoints: an account below it never gets
	// the pen (Eligible), and an unknown level (0) is below. Zero is no gate.
	MinHitpoints int
	// SafeSpots, when set, are the ONLY tiles the pen trains from: verified offline (the real
	// collision pack, content's spawns, maxranges and sizes; magicgiants_pack_test.go) as tiles
	// from which a spell reaches some of the herd while no NPC there can melee them. The account
	// stands on one, hand-casts at a target its spell reaches from there, never steps off, checks
	// the tile after every cast and leaves the pen when it is hit (safespot.go).
	SafeSpots []tile.Tile
	// Threats are the NPCs that can melee near a safespot pen (its herd's spawns and every other
	// hunting NPC close by): the step-off loot rule (safeloot.go, StepOffSafe) walks only where none
	// of them could follow.
	Threats []PenThreat
	// HP and RespawnTicks are the herd NPC's hitpoints and respawn (content; 0 respawn is the
	// engine's 100-tick default): the Magic xp a seat pays (magicpens_capacity.go). Only the Magic
	// pens above the low ones carry them.
	HP, RespawnTicks int
	// Far is a pen outside Stage 3's home region, with its own Bank: the account draws its kit and
	// food there (the bank is one account-wide bank) and counts the pen's Base as home while it
	// trains there (AtFarBase), instead of walking back to East Ardougne for every refill.
	Far  bool
	Bank tile.Tile
	// Base is a far pen's home ground besides the pen and the bank: the walk between them.
	Base []PenHazard
	// RunePen is a pen that is in the ladder for the runes its herd drops ("runepens", 13 Oct 2026: the
	// owner's "npcs that drop runes most frequently ... so we have an extra rune source"), the Falador
	// Dwarven Mine dwarves and the Asgarnian ice dungeon Ice warriors. The setting magicRunePens off drops
	// every one of them from MagicPensFor.
	RunePen bool
	// SpillOnly is a pen of the top tier that takes an account only when every other pen of that tier is
	// full (ChooseMagicPen): the Ice dungeon pen ranks with the Ogres, which the owner put first (5 Oct), and
	// pays about the same Magic xp a seat, so it must not draw accounts off an Ogre pen with a seat free.
	SpillOnly bool
}

// FareStand is the tile the ships' fares of a walk to the pen are judged on: the Stand, or for a Far pen
// whose stand is underground (the Falador Dwarven Mine, the Asgarnian ice dungeon: no landmass, so no
// crossing by freshstart.FareToCross) its Bank on the surface, which is on the landmass the walk ends on.
func (p TrainPen) FareStand() tile.Tile {
	if p.Far && freshstart.LandmassOf(p.Stand) == freshstart.LandUnknown && p.Bank != (tile.Tile{}) {
		return p.Bank
	}
	return p.Stand
}

// Herd is the key every target search matches NPC names by: Monster, then Also (combatplan.HerdKey), so a
// pen that names one NPC is a herd of one.
func (p TrainPen) Herd() string {
	return combatplan.HerdKey(append([]string{p.Monster}, p.Also...)...)
}

// IsHerd reports whether name (case-insensitive) is the pen's monster or one of Also.
func (p TrainPen) IsHerd(name string) bool { return combatplan.HerdMatch(p.Herd(), name) }

// Gated reports whether the pen is above the old low pens: a Magic pen with a tier gate.
func (p TrainPen) Gated() bool { return p.Tier > PenLow }

// Eligible reports whether an account at these base levels may train at the pen: its Magic level
// clears the tier's level and its Hitpoints the pen's own gate.
func (p TrainPen) Eligible(magic, hitpoints int, lv MagicPenLevels) bool {
	return magic >= lv.Normalised().MinMagic(p.Tier) && hitpoints >= p.MinHitpoints
}

// ForMelee is p, or the Cow pen when p is magic-only or a Magic pen above the low ones (a giant
// herd, the troopers): the guard a melee step keeps even when its caller hands it a pen chosen
// for Magic.
func (p TrainPen) ForMelee() TrainPen {
	if p.MagicOnly || p.Gated() {
		return CowPen
	}
	return p
}

// PenHazard is a rectangle of a pen's ground (inclusive bounds, level 0) that a target standing on
// it is not taken from: the monster that owns it, its wander range, its size and its hunt range,
// with a margin so the walk to the target never ends inside the hunt range either.
type PenHazard struct {
	Name                   string
	MinX, MaxX, MinZ, MaxZ int
}

// Contains reports whether t lies in the hazard.
func (h PenHazard) Contains(t tile.Tile) bool {
	return t.Level == 0 && t.X >= h.MinX && t.X <= h.MaxX && t.Z >= h.MinZ && t.Z <= h.MaxZ
}

// HazardAt is the hazard t lies on, if any.
func (p TrainPen) HazardAt(t tile.Tile) (PenHazard, bool) {
	for _, h := range p.Hazards {
		if h.Contains(t) {
			return h, true
		}
	}
	return PenHazard{}, false
}

// SafeTarget reports whether a target standing on t may be fought from this pen: on no hazard.
func (p TrainPen) SafeTarget(t tile.Tile) bool {
	for _, h := range p.Hazards {
		if h.Contains(t) {
			return false
		}
	}
	return true
}

// MossGiantsWestOfTheGoblins is the North Ardougne goblins' hazard. Content m39_53 places four Moss
// giants (npc 112) at 2549,3408 2554,3401 2554,3409 2556,3406: wanderrange 3, size 2, huntrange 2,
// huntmode cowardly (it attacks a low-level player on sight), 60 hitpoints and 30 strength. Their
// ground plus wander, size and hunt range ends at x 2562 and z 3396-3414; the rectangle adds four
// tiles more east and eight north and south for the chase. Goblins spawn inside it (2563,3398,
// 2563,3416, 2564,3407) and others wander in.
//
// LIVE 2026-09-26 21:45 (c1ff7c2f): acct37 (Magic 19, 13 hitpoints) broke off a fight at 5/13
// "facing: Moss giant(112)", ate, walked back into the same pen and died at 2560,3418 -- the
// 2566-2587,3409-3437 death cluster the nodeaths work could not attribute.
var MossGiantsWestOfTheGoblins = PenHazard{Name: "Moss giants (content m39_53)", MinX: 2540, MaxX: 2566, MinZ: 3388, MaxZ: 3422}

// CowPen is the pen Stage 3 and Stage 5 always used (TrainStand/TrainLeash).
var CowPen = TrainPen{Name: "East Ardougne cow pen", Monster: "Cow", Stand: TrainStand, Leash: TrainLeash, Spawns: 10,
	Retreat: tile.Tile{X: 2674, Z: 3350}}

// TrainPens is every pen, the Cow pen first (it is the default and the one a zero TrainPen means).
var TrainPens = []TrainPen{
	CowPen,
	{Name: "North Ardougne goblins", Monster: "Goblin", Stand: tile.Tile{X: 2575, Z: 3420}, Leash: 30, Spawns: 24,
		Retreat: tile.Tile{X: 2580, Z: 3385}, Hazards: []PenHazard{MossGiantsWestOfTheGoblins}},
	{Name: "East Ardougne chickens", Monster: "Chicken", Stand: tile.Tile{X: 2692, Z: 3274}, Leash: 6, Spawns: 4,
		Retreat: tile.Tile{X: 2680, Z: 3286}},
	RangingGuildChickens,
	LakeDucksWest,
	LakeDucksSouthEast,
	LakeDucksNorthEast,
	MossGiantsNorthArdougne,
	// The Magic pens above the low ones ("magicgiants", 3 Oct 2026; magicpens.go). Last, so the
	// melee order (PensFor(false)) is the one every earlier test pinned.
	KhazardTroopers,
	WarriorWomenNorth,
	// HillGiantsEdgeville is retired (5 Oct 2026): see its comment.
	BarbarianVillage,
	AlKharidWarriors,
	// 13 Oct (runepens): the Falador Dwarven Mine dwarves, a Far mid pen for the Chaos and Nature runes they
	// drop. After the other mid pens; the chooser weighs the Magic xp a seat pays (ChooseMagicPen), so the
	// dwarves take the accounts the Khazard troopers crowd out.
	DwarvesFaladorMine,
	// 4 Oct (giantloot): the giant-tier seats (magicpens_capacity.go).
	YanilleWestOgres,
	FeldipOgresWest,
	FeldipOgresEast,
	// 13 Oct (runepens): the Ice warriors of the Asgarnian ice dungeon, a safespot pen of the top tier that
	// takes the spill of the Ogres (SpillOnly). Appended last so every order the earlier tests pinned holds.
	IceWarriorsAsgarnianDungeon,
}

// RangingGuildChickens is the fourth pen ("scarabpens", 2026-09-26): four Chickens (41, 3
// hitpoints, no attack, respawn 50 ticks, wanderrange 5) at 2650,3441 2650,3442 2651,3441
// 2651,3442 (content m41_53), west of the Ranging Guild. The walk from the East Ardougne bank is
// plain land (webwalkplan -auto -combat 3: cost 167, the same as the goblins' 164), and every
// chicken tile is reachable from the stand in at most four steps.
//
// Nothing that attacks a low-level account reaches it: the nearest hunting monster is a Guard dog
// (99, huntrange 1, wanderrange 3) at 2665,3471, thirty tiles north; the Ranging Guild's guards
// (678) do not hunt. The retreat is on the walk in, fifteen tiles south.
//
// Not added, from the same content survey (private evidence notes): the three
// Farmers beside the Cow pen are what Scarab's own thieving pickpockets at 2641,3372; the rats
// north of the Fishing Guild sit among Giant bats and Guard dogs; the Unicorns (19 hitpoints,
// max hit 2) and the Khazard troopers and arena guards (22-25 hitpoints, max hit 3) would hurt
// the 10-14 hitpoint accounts that are most of Stage 3. The 59 Ducks beside the goblins are on
// water; they are the magic-only lake pens below.
var RangingGuildChickens = TrainPen{Name: "Ranging Guild chickens", Monster: "Chicken",
	Stand: tile.Tile{X: 2650, Z: 3438}, Leash: 10, Spawns: 4, Retreat: tile.Tile{X: 2645, Z: 3423}}

// THE LAKE DUCKS ("scarabducks", 2026-09-27): three magic-only pens on the shores of the lake
// between the North Ardougne goblins and the Cow pen, for Stage 3's Magic only.
//
// Content m40_53 places 59 Ducks (npc 44 "duck" x20 and 45 "duck_female" x39: 3
// hitpoints, no attack, no huntmode, wanderrange 35, respawnrate 100 ticks, moverestrict blocked)
// on the lake's water at 2593-2621,3403-3454. No walk reaches one, so combatrunner.Reachable and a
// walk to an adjacent tile refuse every one; a spell does not need either (castreach.go: an
// autocast attack or a hand cast reaches ten tiles with line of sight).
//
// The shore tiles below were chosen offline over the real collision pack
// (private evidence notes): the land connected to the East Ardougne bank by a plain walk
// (webwalkplan -auto -combat 3 from 2655,3283: 181-190 to the west shore, 132 to the south-east,
// 202 to the north-east, the goblins' 164 for scale), and for each the spawns within CastRange less
// CastMargin in line of sight (walls as sight blockers; the pack cannot tell a tree from water).
// 34 of the 59 spawns are covered: 17 from the west shore, 9 from the south-east, 8 from the
// north-east. The other 25 are mid-lake or beside the Fishing Guild's piers (behind its Fishing 68
// door at 2611,3394), and their Ducks count only when they wander within reach, which a 35-tile
// wanderrange lets them do.
//
// Spawns is the covered count, which is what ChoosePen weighs a pen's room by. The three are last
// in TrainPens so an account takes them only when the herds that pay more per kill are full.
//
// WHY, IN XP (content: a combat spell pays its base xp a cast plus 2 Magic xp a point of damage,
// capped at the target's remaining hitpoints). A Duck pays 6 damage xp a kill plus the casts it
// takes at ~85% accuracy: Wind Strike ~4.0 casts, 28 xp a kill; Fire Strike ~1.7 casts, 25 xp. A
// Cow (8 hp) pays 70 / 51 and the four existing pens average about 42 xp a kill with Fire Strike.
// But live 2026-09-26 those pens were RESPAWN-bound: ~2,650 kills an hour a world shared by ~46
// trainers, about 58 kills and 2,400 Magic xp an hour each. Respawned at their spawns every 100
// ticks, the 34 covered Ducks add up to ~2,040 kills an hour a world (more as the other 25 wander
// in), ~52,000 Magic xp an hour at 25 a kill: +47% on the pens' ~111,000. Spread by ChoosePen's
// per-spawn crowd, every trainer gains -- a Duck trainer's ~100 kills an hour is ~2,600 xp, and
// the ~26 left at the pens split their 2,650 kills for ~4,300.
//
// Nothing that hunts reaches the west or south-east shore (scarabpens' haz.py, 25 tiles). The
// north-east shore has a Guard dog (99, huntrange 1, wanderrange 3) at 2631,3472; its ground plus
// wander, hunt and a four-tile margin is the hazard below, six tiles north of the nearest cast
// tile.
var (
	LakeDucksWest = TrainPen{Name: "Ardougne lake ducks (west shore)", Monster: "Duck", MagicOnly: true,
		Stand: tile.Tile{X: 2590, Z: 3438}, Leash: 16, Spawns: 17, Retreat: tile.Tile{X: 2582, Z: 3435},
		CastFrom: []tile.Tile{{X: 2590, Z: 3438}, {X: 2588, Z: 3442}, {X: 2591, Z: 3431}, {X: 2585, Z: 3449}, {X: 2586, Z: 3449}, {X: 2593, Z: 3426}}}
	LakeDucksSouthEast = TrainPen{Name: "Ardougne lake ducks (south-east shore)", Monster: "Duck", MagicOnly: true,
		Stand: tile.Tile{X: 2624, Z: 3402}, Leash: 12, Spawns: 9, Retreat: tile.Tile{X: 2630, Z: 3398},
		CastFrom: []tile.Tile{{X: 2624, Z: 3402}, {X: 2625, Z: 3403}, {X: 2624, Z: 3401}}}
	LakeDucksNorthEast = TrainPen{Name: "Ardougne lake ducks (north-east shore)", Monster: "Duck", MagicOnly: true,
		Stand: tile.Tile{X: 2624, Z: 3456}, Leash: 12, Spawns: 8, Retreat: tile.Tile{X: 2630, Z: 3456},
		CastFrom: []tile.Tile{{X: 2624, Z: 3456}, {X: 2623, Z: 3458}},
		Hazards:  []PenHazard{GuardDogNorthOfTheLake}}
)

// GuardDogNorthOfTheLake is the north-east duck shore's hazard: the Guard dog (99) content m41_54
// places at 2631,3472, wanderrange 3, huntrange 1, plus a four-tile margin.
var GuardDogNorthOfTheLake = PenHazard{Name: "Guard dog (content m41_54)", MinX: 2623, MaxX: 2639, MinZ: 3464, MaxZ: 3480}

// OrCowPen is p, or CowPen for the zero pen, so a caller that never chose one keeps the old field.
func (p TrainPen) OrCowPen() TrainPen {
	if p.Name == "" {
		return CowPen
	}
	return p
}

// Near reports whether t is inside the pen's training area, with the same five-tile margin the Cow
// pen always had in RegionOf.
func (p TrainPen) Near(t tile.Tile) bool {
	return t.Level == 0 && tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, p.Stand) <= p.Leash+5
}

// NearAnyPen reports whether t is inside any pen's training area: Stage 3's home region includes
// every pen, or an account training at the goblins would be sent "home" from its own pen. A Far
// pen is not: it is home only to an account that trains there (AtFarBase, Stage3Snapshot.AtFarBase).
func NearAnyPen(t tile.Tile) bool {
	for _, p := range TrainPens {
		if !p.Far && p.Near(t) {
			return true
		}
	}
	return false
}
