package fleetplan

// The Asgarnian ice dungeon's Ice warriors, a safespot pen for the runes they drop ("runepens", 13 Oct
// 2026). Pure.
//
// THE OWNER (13 Oct): "we can also find npcs that drop runes most frequently and put them into our
// training ladder some place so we have an extra rune source". Of the herds a Magic account can stand
// beside, the Ice warrior leaves the most scarce runes on the ground: nature 4 at 10/128, chaos 3 at 8/128,
// death 2 at 3/128 and, on a members world, blood 2 at 1/128 (content drop tables/scripts/ice_warrior.rs2),
// 0.5625 runes a kill and a scarce rune on 17% of kills (runeyield.go); a Moss giant leaves 0.336, a Dwarf
// 0.125, an Ogre none. Law, cosmic and death drop beside them.
//
// THE HERD. Eleven Ice warriors (npc 125: combat 57, 59 hitpoints, attack, strength and defence 47, max hit
// 6, magic defence +10, huntrange 5, wanderrange 3 so maxrange 5 and a chase of 6, size 1, respawn 60 ticks)
// at 3044,9581 3046,9575 3048,9583 3049,9590 3052,9568 3053,9576 3056,9583 3056,9587 3057,9566 3062,9576
// 3062,9581 (content m47_149), with three Ice giants among them. They hit too hard for the accounts that
// train here, so the pen is a SAFESPOT pen like the Ogres: the account stands on a tile no hostile can
// melee (the repo's collision-aware model, magicgiants_pack_test.go) and hand-casts at the warriors the
// spell reaches, and Telekinetic Grab takes the runes off the ground from the tile (safeloot.go).
//
// THE SAFESPOTS (real pack, runtime pick rule SafeCastableMoving; TestIceWarriorPenSpotsOnTheRealPack):
//
//	3041,9588  reaches 3044,9581 (86% of its wander ground), 3048,9583 (69%), 3049,9590 (35%): 1.89 spawns' worth
//	3040,9588  the same three at 86 / 60 / 15%: 1.61
//	3040,9589  3044,9581 and 3048,9583 at 71 / 43%: 1.14
//	3037,9584  3044,9581 at 76% (and a sliver of two more): 0.99
//	3037,9579 3036,9581 3036,9583  3044,9581 alone at 64 / 38 / 33%: the west pocket, where the walk in
//	           passes no Ice warrior at all
//
// The three spawns they reach are the pen's Spawns, so its cap is three seats a world (PenCap) and about
// 2.3 spawns' worth of herd: this is a small, pure source, not a supply. 3056,9594 reaches a fourth
// (3049,9590) but its walk in is 20 tiles inside the warriors' chase, and is left out
// (TestIceWarriorPenSpotsOnTheRealPack).
//
// THE WALK IN IS THE DANGER, and it is the most exposed of any safespot pen. From the ladder landing
// (3008,9551) the planned walk to the east spots is 83 steps, 79 of them on tiles some hostile could melee
// by the same model: the unarmed Hobgoblins at 3010-3016,9578-9594 (wanderrange 15, so a chase of 18; combat
// 28, they ignore an account above 56), the Muggers at 2993-2999,9544-9550 beside the landing (combat 6, chase
// 15), the Pirates west of the Hobgoblins (chase 8) and, for the east spots only, four tiles of the Ice
// warriors' own chase. The west spots have 75 and no warrior tile. The retired Edgeville dungeon pen died of a
// walk like this one (magicpens.go: spare14 twice and acct146 at Hitpoints 25), so the gate is
// Hitpoints 30 and the exposure is pinned in a test (iceWalkInBudget), which fails if a spot is added whose
// approach is worse. Those Hobgoblins and Muggers hit for 1 to 4; the Ice warriors hit for 6.
//
// SPILL ONLY (SpillOnly). A seat here pays about what an Ogre seat does (a 59-hitpoint herd on a 60-tick
// respawn, cast at from a safespot), and the Ogres were put first by the owner (5 Oct: "yeah use the ogres
// yanille"): ChooseMagicPen takes this pen only when no Ogre pen has a seat. Every Ogre seat was taken in
// every world on 13 Oct, so the spill is the whole of it. Tier PenIceGiant (Magic 50, setting
// magicIceGiantsAt), no TopTier; appended last in TrainPens.
//
// FAR (its own bank, the Falador east bank, and the ladder at 3008,3150 down to the landing 3008,9551): from
// 3012,3355 the planner gives one dungeon hop; the booth tile 3013,3354 is not a standable planner start, so the
// bank is the tile beside it. The pen's stand is underground and has no landmass, so the ships' fares of a walk
// there are judged on the bank (TrainPen.FareStand).

import "rsb0t-go/internal/scripts/tile"

// IceWarriorDrops is everything an Ice warrior can leave on the ground on a members world (content
// ice_warrior.rs2: the default Bones, the table of 128, a medium clue scroll at 1/128, ~randomherb and
// ~randomjewel): a pile taken at the pen that no kill claims goes on the Ice warrior's card only when it is
// one of these (scarabrunner.penSource).
var IceWarriorDrops = []string{"Bones", "Iron battleaxe", "Mithril mace", "Nature rune", "Chaos rune", "Law rune",
	"Cosmic rune", "Mithril arrow", "Adamant arrow", "Death rune", "Blood rune", "Herb", "Coins",
	"Uncut sapphire", "Uncut emerald", "Uncut ruby", "Uncut diamond", "Rune javelin", "Half of a key",
	"Chaos talisman", "Nature talisman", "Clue scroll"}

// faladorMainBase is the ground between the Port Sarim ships, the Draynor road and the Falador east bank
// that a Far pen of the Falador side counts as home: an account walking from the ships to the ladder or the
// mine is on the pen's Base, not "away" (AtFarBase).
var faladorMainBase = PenHazard{Name: "Port Sarim, the Draynor road and Falador's east bank", MinX: 2995, MaxX: 3100, MinZ: 3140, MaxZ: 3395}

// IceWarriorsAsgarnianDungeon is the pen (see the file comment).
var IceWarriorsAsgarnianDungeon = TrainPen{Name: "Asgarnian ice dungeon ice warriors", Monster: "Ice warrior", Drops: IceWarriorDrops,
	Tier: PenIceGiant, RunePen: true, SpillOnly: true,
	Stand: tile.Tile{X: 3041, Z: 9588}, Leash: 16, Spawns: 3, Retreat: tile.Tile{X: 3008, Z: 9551},
	SafeSpots: []tile.Tile{{X: 3041, Z: 9588}, {X: 3040, Z: 9588}, {X: 3040, Z: 9589}, {X: 3037, Z: 9584},
		{X: 3037, Z: 9579}, {X: 3036, Z: 9581}, {X: 3036, Z: 9583}},
	Threats: IceWarriorThreats, HP: 59, RespawnTicks: 60,
	MinHitpoints: 30, Far: true, Bank: tile.Tile{X: 3012, Z: 3355},
	Base: []PenHazard{faladorMainBase,
		{Name: "Asgarnian ice dungeon, landing to the Ice warriors", MinX: 2980, MaxX: 3075, MinZ: 9540, MaxZ: 9600}}}
