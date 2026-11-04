package fleetplan

import (
	"rsb0t-go/internal/scripts/tile"
)

// The reference spell and the rates the estimate uses (see the file comment).
const (
	refSpellBaseXP  = 22.5
	refDamagePerHit = 6.0 // Fire Bolt's max 12, uniform 0..12
	RefAccuracy     = 0.9
	// SafespotCastSeconds is one cast per CastGap; AutocastSeconds one per 5 ticks.
	SafespotCastSeconds = 3.6
	AutocastSeconds     = 3.0
	// walkUpRetargetSeconds is a walk-up pen's walk to the next target after a kill.
	walkUpRetargetSeconds = 3.0
	// defaultRespawnTicks is the engine's NpcType respawnrate default.
	defaultRespawnTicks = 100
)

// dmgPerCast is the expected damage of one cast of the reference spell.
func dmgPerCast() float64 { return RefAccuracy * refDamagePerHit }

// castSeconds is the pen's cast cadence: a hand cast per CastGap at a safespot, autocast elsewhere.
func (p TrainPen) castSeconds() float64 {
	if p.HasSafeSpots() {
		return SafespotCastSeconds
	}
	return AutocastSeconds
}

// KillCycle is one kill at the pen: casts (the last one's overkill counted), the time they take
// (plus a walk-up pen's walk to the next target) and the xp they pay.
func (p TrainPen) KillCycle() (casts, seconds, xp float64) {
	casts = float64(p.HP)/dmgPerCast() + 0.5
	seconds = casts * p.castSeconds()
	if !p.HasSafeSpots() {
		seconds += walkUpRetargetSeconds
	}
	return casts, seconds, casts*refSpellBaseXP + 2*float64(p.HP)
}

// MaxMagicXPh is what one account earns at the pen when a target is always there for it: kill
// after kill with no wait for a respawn.
func (p TrainPen) MaxMagicXPh() float64 {
	if p.HP <= 0 {
		return 3600 / p.castSeconds() * (refSpellBaseXP + 2*dmgPerCast())
	}
	_, sec, xp := p.KillCycle()
	return 3600 / sec * xp
}

// SpawnMagicXPh is one spawn's yield an hour at the pen, kept busy: kills an hour times xp a kill
// (see the file comment). 0 for a pen without its NPC's hitpoints.
func (p TrainPen) SpawnMagicXPh() float64 {
	if p.HP <= 0 {
		return 0
	}
	_, kill, xp := p.KillCycle()
	respawn := p.RespawnTicks
	if respawn <= 0 {
		respawn = defaultRespawnTicks
	}
	return 3600 / (kill + float64(respawn)*0.6) * xp
}

// DefaultGiantSeatsPerSpawn is how many accounts one giant-tier spawn seats by default (setting
// magicGiantSeatsPerSpawn, 1-6). A 60-hitpoint giant or Ogre dies in ~40 s of casts and respawns in
// 36 s (60 ticks), so one account alone waits nearly half of every cycle on its respawn; a second
// fills that wait and keeps the spawn under attack (~8,700 xp/h each). A third adds no xp to the
// herd but shares it (~5,800 xp/h each, still above the ~3,500 a trooper paid 76 accounts on 4
// Oct) and takes the Big bones and the drops off the troopers: the owner's "should be more on
// giants and beyond".
//
// 1 SINCE 7 OCT 2026 ("scarabpens", owner: "accounts are still crowding at NPC training spots like
// ogres"). Live that evening the ogre pens held 12 to 13 accounts on 7 to 8 spawns (about 1.6 a spawn,
// 190 accounts on four herds) while the Khazard troopers held none: at two seats a spawn the pens ran
// at their cap, and a spawn pays one kill per ~76 s to ONE account however many wait on it, so each
// account earned ~11,000 xp/h where an account alone on a trooper herd earns ~31,000 (ExpectedMagicXPh)
// and the ogre's one caster ~17,600. One a spawn saturates every herd and sends the rest to the pens
// below the giants (PenCap: one a spawn there too).
//
// 2 FROM 5 OCT 2026 (owner: "yes" to 1-2). The ogrepens measurement: a spawn yields one kill per
// ~76 s however many cast at it (content refuses a second caster on an NPC another player hit in the
// last 8 ticks outside multiway), so a third account adds nothing but refusals; a second still
// covers the wander ground one safespot cannot see.
const DefaultGiantSeatsPerSpawn = 1

// GiantSeats is how many accounts the pen seats at perSpawn a spawn when it is a giant-tier pen,
// else 0. perSpawn below 1 is DefaultGiantSeatsPerSpawn.
func (p TrainPen) GiantSeats(perSpawn int) int {
	if p.Tier < PenHillGiant {
		return 0
	}
	if perSpawn < 1 {
		perSpawn = DefaultGiantSeatsPerSpawn
	}
	return p.Spawns * perSpawn
}

// yanilleBank is the Yanille bank booth the combatplan "ogres" rung banks at.
var yanilleBank = tile.Tile{X: 2613, Z: 3094}

// yanilleAndFeldip is Yanille, its west gate and the Feldip Hills south of it: the ground a far
// pen banking at Yanille walks.
var yanilleAndFeldip = PenHazard{Name: "Yanille and the Feldip Hills", MinX: 2488, MaxX: 2625, MinZ: 2945, MaxZ: 3130}

// YanilleWestOgres: the Ogres west of Yanille's west gate (2496-2508,3089-3119) and the nearest of
// the Combat Training Camp's penned four (2509,3087), cast at from 2510,3096 (four of them),
// 2510,3112 (two) and 2503,3123 (two): 8 spawns. From the Yanille bank 109-126 through the two Large
// doors of the west gate. Hitpoints 30: the last tiles of the walk pass within an Ogre's hunt.
//
// 5 Oct ("ogrepens"): nine accounts a world stood on 2503,3123, which covers 0.39 spawns' worth of
// the herd (an Ogre wanders three tiles round its spawn and from there the spell reaches it one
// position in four), so the pen now lists six more tiles found by the same collision-aware search
// (magicpens_ogres_pack_test.go, TestOgrePenSpotSurvey: no Ogre can melee any of them, each reached
// from the first by an exact walk, the arrival walk no more exposed than the old ones'). Four tiles,
// the first among them, are in the east pocket (2510,3096 to 2511,3100: spawns 2, 3 and 5 at 41-100%,
// covering 1.8 to 2.9 spawns' worth each), the rest on the two reaches south of it (2509,3110 beside
// 2510,3112, 2507,3122 and 2509,3123 beside 2503,3123). scarabplan's chooser (spotchoice.go) weighs them by the
// share of the herd each leaves an account.
// OgreDrops is everything an Ogre drops: content's ogre and ogre2 (area_yanille/configs/ogre.npc) have
// death_drop big_bones and no drop table script. Live on 1306c66a (5 Oct 2026) the pen fallback filed
// 13 Death runes and Steel arrows taken at the Feldip pens on the Ogre card; neither an Ogre nor the
// Jogres of those hills (content drop tables/scripts/jogre.rs2) drop them.
var OgreDrops = []string{"Big bones"}

var YanilleWestOgres = TrainPen{Name: "Yanille west ogres", Monster: "Ogre", Drops: OgreDrops, Tier: PenHillGiant, TopTier: PenIceGiant,
	Stand: tile.Tile{X: 2505, Z: 3105}, Leash: 22, Spawns: 8, Retreat: tile.Tile{X: 2530, Z: 3092},
	SafeSpots: []tile.Tile{{X: 2510, Z: 3096}, {X: 2510, Z: 3112}, {X: 2503, Z: 3123},
		{X: 2509, Z: 3101}, {X: 2510, Z: 3098}, {X: 2511, Z: 3100}, {X: 2509, Z: 3110}, {X: 2507, Z: 3122}, {X: 2509, Z: 3123}},
	Threats: YanilleWestOgreThreats, HP: 60, RespawnTicks: 60,
	MinHitpoints: 30, Far: true, Bank: yanilleBank, Base: []PenHazard{yanilleAndFeldip}}

// FeldipOgresWest and FeldipOgresEast: the Ogres of the Feldip Hills south of Yanille, in two pens
// so the seats spread over the hills (an account casts at what it can see, about fifteen tiles
// round it): 7 spawns each from the safespots below. Plain land from the Yanille bank, 163-221.
// Wolves (wanderrange 8-10), Bears and two Giant spiders share the hills; every safespot is out of
// their melee too. Hitpoints 35: the walk crosses the Ogres' ground. The north Feldip Ogres are
// left out: one reachable spawn from a safespot (2567,3046 from 2577,3043).
//
// 5 Oct ("ogrepens"): the West pen lists six more tiles beside its five (TestOgrePenSpotSurvey): the
// pocket of 2534,2984 (Ogres 2525,2986, 2533,2977 and 2536,2975 at 78-100%) gains 2532,2984 and
// 2532,2987, 2530,2979's gains 2531,2976, the southern pocket that reaches 2533,2977 and 2536,2975 from
// the south (67-88%) 2532,2968 and 2537,2968, and the pocket of the pair at 2544,2958-2959 gains
// 2545,2961. Each has the arrival route's exposure of the old ones or less.
var (
	FeldipOgresWest = TrainPen{Name: "Feldip ogres (west)", Monster: "Ogre", Drops: OgreDrops, Tier: PenHillGiant, TopTier: PenIceGiant,
		Stand: tile.Tile{X: 2525, Z: 2972}, Leash: 28, Spawns: 7, Retreat: tile.Tile{X: 2576, Z: 2995},
		SafeSpots: []tile.Tile{{X: 2530, Z: 2979}, {X: 2534, Z: 2984}, {X: 2544, Z: 2959}, {X: 2544, Z: 2958}, {X: 2498, Z: 2976},
			{X: 2532, Z: 2984}, {X: 2531, Z: 2976}, {X: 2532, Z: 2987}, {X: 2532, Z: 2968}, {X: 2537, Z: 2968}, {X: 2545, Z: 2961}},
		Threats: FeldipOgreThreats, HP: 60, RespawnTicks: 60,
		MinHitpoints: 35, Far: true, Bank: yanilleBank, Base: []PenHazard{yanilleAndFeldip}}
	// FeldipOgresEast: seven of its eight Ogres were reached from one spot each, and the one that held
	// seven accounts a world (2568,2998) covers 48% of ONE Ogre's wander ground. Eight more tiles
	// (TestOgrePenSpotSurvey) give the two Ogres at 2584,2967 and 2585,2977 four tiles in the west
	// pocket (2575-2578,2969-2976: 61-94% of each) and one in the middle (2592,2974 covers 2585,2977
	// whole), the two at 2608,2980 and 2609,2990 a tile each beside the old ones (2615,2980 and
	// 2605,2985: 96% and 100%) and 2593,2969, which covers 31% of 2592,2965 and 55% of 2585,2977.
	FeldipOgresEast = TrainPen{Name: "Feldip ogres (east)", Monster: "Ogre", Drops: OgreDrops, Tier: PenHillGiant, TopTier: PenIceGiant,
		Stand: tile.Tile{X: 2592, Z: 2982}, Leash: 25, Spawns: 7, Retreat: tile.Tile{X: 2576, Z: 2995},
		SafeSpots: []tile.Tile{{X: 2578, Z: 2976}, {X: 2574, Z: 2997}, {X: 2601, Z: 2992}, {X: 2615, Z: 2978}, {X: 2595, Z: 2963}, {X: 2568, Z: 2998},
			{X: 2578, Z: 2973}, {X: 2577, Z: 2970}, {X: 2575, Z: 2969}, {X: 2592, Z: 2974}, {X: 2587, Z: 2982}, {X: 2615, Z: 2980}, {X: 2605, Z: 2985}, {X: 2593, Z: 2969}},
		Threats: FeldipOgreThreats, HP: 60, RespawnTicks: 60,
		MinHitpoints: 35, Far: true, Bank: yanilleBank, Base: []PenHazard{yanilleAndFeldip}}
)
