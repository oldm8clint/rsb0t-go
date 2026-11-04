package fleetplan

// The Falador Dwarven Mine's Dwarves, a walk-up pen for the Chaos and Nature runes they drop ("runepens",
// 13 Oct 2026). Pure.
//
// THE OWNER (13 Oct): "we can also find npcs that drop runes most frequently and put them into our
// training ladder some place so we have an extra rune source". A Dwarf leaves Chaos 2 on 4/128 of its kills
// and Nature 2 on another 4/128 (content drop tables/scripts/dwarf.rs2), 0.125 scarce runes a kill, a rune
// on one kill in eight; the Khazard troopers beside which most of the mid tier trains leave none (the rest
// of the table is Bones, a Hammer, bars and ore, Bolts, a few Bronze pieces and Coins).
//
// THE HERD. 36 dwarf_normal (npc 118: combat 10, 16 hitpoints, attack and strength 8, max hit 2, no huntmode,
// wanderrange 13, respawn 100 ticks) at 2984-3054,9796-9851 (content m46_153, m47_153), every one reached by a
// walk from the stand. Not the other NPCs named Dwarf: the Dwarven shopkeeper at 2993,9828 and the Mining Guild
// dwarves offer no Attack (combatrunner.MonstersNear needs it), and the Cannon guards and the mountain dwarves
// are far from here. Walk-up and not aggressive, so it is trained the way the Barbarians are, on autocast, with
// LootValuables taking the runes (and Bones buried) after each kill.
//
// A kill is 3.5 casts, 13.4 s and ~110 Magic xp at the reference spell against the troopers' 16.7 s and ~147
// (killCycle): the xp an hour of one account is level with the troopers' (29.5k against 31.6k a cycle at a
// free target), so the seat costs nothing in xp and the rune a kill in eight is the whole gain. It is a Far
// mid pen (Magic 20, setting magicMidPensAt) the chooser reaches by share: the troopers are the near pen, and
// their crowd drives accounts here (ChooseMagicPen).
//
// THE SCORPIONS. Seven Scorpions (combat 14, 17 hitpoints, size 2, huntrange 2, wanderrange 12 so a chase of
// 15) at 3039-3047,9781-9802, six more and two King Scorpions (combat 32) south of them at 3038-3056,9767-9778,
// sit between the mine's entrance and the dwarves. They hit for 2 and 4 and hunt an account of combat 28 and
// below (King Scorpion: 64 and below; Npc.ts check_nottoostrong). Three dwarf spawns (3028,9816 3050,9813
// 3051,9817) are inside their notice box, and the pen leaves a target standing there alone (the Hazard,
// SafeTarget). And the WALK IN crosses them: the planner's route from the Falador east bank takes the stairs at
// 3059,3376 (the Large door at 3063,3380 first, 155 in all, no web) down to 3058,9776, in the middle of the
// Scorpions, and 44 of its dungeon tiles are inside a Scorpion's chase (dwarfWalkInBudget). The retired
// Edgeville dungeon pen died of a walk like this at Hitpoints 25 (magicpens.go), though what is on this one
// hits for 2 to 4, not the Skeletons' and Zombies' 8: Hitpoints 20 for the walk in (the design said 12,
// before the entrance was measured).
//
// FAR: banked at the Falador east bank (the tile beside the booth, 3012,3355: the booth tile is not a standable
// planner start), from which the planner reaches the stand at 3017,9826 in 155 with one door and one dungeon
// hop. The stand is underground, so the ships' fares are judged on the bank (TrainPen.FareStand).

import "rsb0t-go/internal/scripts/tile"

// DwarfDrops is everything a Dwarf can leave on the ground on a members world (content dwarf.rs2: the default
// Bones, the table of 128 and ~randomjewel); see IceWarriorDrops.
var DwarfDrops = []string{"Bones", "Bronze pickaxe", "Bronze med helm", "Bronze battleaxe", "Iron battleaxe", "Bolts",
	"Chaos rune", "Nature rune", "Coins", "Hammer", "Bronze bar", "Iron ore", "Tin ore", "Copper ore", "Iron bar", "Coal",
	"Uncut sapphire", "Uncut emerald", "Uncut ruby", "Uncut diamond", "Rune javelin", "Half of a key",
	"Chaos talisman", "Nature talisman"}

// DwarvenMineScorpions is the dwarves' hazard: the Scorpions' notice box (content m47_152, m47_153: the
// Scorpions at 3039-3047,9781-9802 wander 12 and notice 2 beyond that, size 2, with a margin). A Dwarf standing
// there is not taken. The King Scorpions and the other Scorpions are further south than any dwarf.
var DwarvenMineScorpions = PenHazard{Name: "Scorpions (content m47_153)", MinX: 3022, MaxX: 3062, MinZ: 9764, MaxZ: 9818}

// DwarvesFaladorMine is the pen (see the file comment).
var DwarvesFaladorMine = TrainPen{Name: "Falador Dwarven Mine dwarves", Monster: "Dwarf", Drops: DwarfDrops,
	Tier: PenMid, RunePen: true,
	Stand: tile.Tile{X: 3017, Z: 9826}, Leash: 37, Spawns: 33, Retreat: tile.Tile{X: 3000, Z: 9826},
	Hazards: []PenHazard{DwarvenMineScorpions}, Threats: DwarfMineThreats, HP: 16, RespawnTicks: 100,
	MinHitpoints: 20, Far: true, Bank: tile.Tile{X: 3012, Z: 3355},
	Base: []PenHazard{faladorMainBase,
		{Name: "Falador Dwarven Mine, the stairs to the dwarves", MinX: 2975, MaxX: 3075, MinZ: 9735, MaxZ: 9860}}}
