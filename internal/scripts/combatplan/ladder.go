package combatplan

import "rsb0t-go/internal/scripts/tile"

// The training ladder: which stat to train next, and where to train it.
//
// This is milestone 2 of docs/combat_trainer_plan.md. Milestone 1 (gear.go, kill.go, death.go)
// built the primitives; this file is the schedule that drives them.
//
// Every number below was read out of content 289 in this session, not carried over from the
// plan. The monsters come from content/scripts/_unpack/225/all.npc; the spawn tiles and the
// spawn COUNTS come from the `==== NPC ====` sections of content/maps/*.jm2, cross-checked
// against content/maps/free2play.csv (per 8x8 zone) and the engine's own wilderness test
// (engine/src/engine/entity/Player.ts isInWilderness, which is TWO boxes: x 2944-3392 with
// z 3520-6400, and the same x band with z 9920-12800 for the dungeon layer — the second box
// is easy to miss and it is what puts five of the Fire giant spawns out of bounds).
//
// Counts this session measured, non-wilderness only:
//
//	Monster      free-to-play   members   wilderness
//	Giant                  12         3           12
//	Moss giant              7        32            3
//	Ice giant               3        11            9
//	Fire giant              0        23            5
//	Ogre                    0        77            0
//	Al-Kharid warrior       9         0            0
//	Cow                    34        18            0
//	Goblin                 88       133            0
//
// The free-to-play giant ladder is 12 + 7 + 3 monsters, which cannot support a cohort — this
// is why the account owner chose the members ladder. Ogres and Fire giants have ZERO
// free-to-play spawns, so the plan's original 45-60 and 60-75 ogre rungs are members-only.
//
// Aggression and the leash. Every giant and the Ogre are huntmode=cowardly with huntrange 2
// and wanderrange 3. The engine's Npc.targetWithinMaxRange measures the chase against the
// SPAWN tile (startX/startZ), and maxrange defaults to wanderrange + 2, so these monsters
// disengage past 5 tiles from where they spawned. Ogre states maxrange=5 outright. A rung
// runtime that wanders further than that simply loses the fight it started, which is why
// every Rung carries its own Leash and DisengageTiles rather than a shared constant.

// Monster is one trainable NPC, named exactly as cachecfg.Npc.Name returns it so a runner can
// match visible NPCs without a second mapping.
type Monster struct {
	Name string
	// Also names the other NPCs of the same herd that a trainer attacks as the same monster (herd.go): the
	// Barbarian women stand among the Barbarians, with the same hitpoints, respawn and drops.
	Also []string
	// CombatLevel is the npc config's own vislevel.
	CombatLevel int
	// Hitpoints is the config's hitpoints= line.
	Hitpoints int
	// MaxHit is derived from the content combat scripts, not measured live — treat it as the
	// food budget's input, not a promise.
	MaxHit int
	// BigBones is whether every kill drops one, from param=death_drop,big_bones. The drop is
	// unconditional: giant.rs2 and its siblings add it before rolling any random table.
	BigBones bool
	// PlainBones is whether every kill drops one plain Bones (4.5 Prayer XP). The content default
	// of the death_drop param is `bones` (skill_combat/configs/npc_combat.param:29-31), so every
	// monster whose config does not name another drop leaves one: the Cow, Goblin, Al-Kharid
	// warrior, Barbarian, White Knight and Ice warrior all do, and an earlier note here that cows
	// "drop no bones" was wrong (cow.rs2 reads that same param).
	PlainBones bool
	// DisengageTiles is how far from ITS OWN SPAWN tile the monster will chase before giving
	// up (maxrange, defaulting to wanderrange + 2).
	DisengageTiles int
	// RespawnTicks is the config's respawnrate (600 ms ticks): how long a spawn tile stays empty
	// after a kill. It, not the kill time, decides how much experience one spawn can feed an
	// hour, which is what the spread across rungs is weighted by (spread.go). 0 reads as 60.
	RespawnTicks int
}

// BoneItem is the item name a kill of this monster leaves for burying, "" when none is known.
func (m Monster) BoneItem() string {
	switch {
	case m.BigBones:
		return "Big bones"
	case m.PlainBones:
		return "Bones"
	}
	return ""
}

// The monsters this ladder uses, straight from content/scripts/_unpack/225/all.npc and
// (for the Ogre) content/scripts/areas/area_yanille/configs/ogre.npc.
var (
	// MonsterCow, MonsterGoblin and MonsterWarrior drop plain Bones: cow.rs2 and the config both
	// read death_drop, whose default is `bones` (see Monster.PlainBones). Only the giants and the
	// Ogre name big_bones, so only they feed the Prayer goal fast.
	MonsterCow     = Monster{Name: "Cow", CombatLevel: 2, Hitpoints: 8, MaxHit: 1, PlainBones: true, DisengageTiles: 5, RespawnTicks: 90}
	MonsterGoblin  = Monster{Name: "Goblin", CombatLevel: 2, Hitpoints: 5, MaxHit: 1, PlainBones: true, DisengageTiles: 5, RespawnTicks: 140}
	MonsterWarrior = Monster{Name: "Al-Kharid warrior", CombatLevel: 9, Hitpoints: 19, MaxHit: 2, PlainBones: true, DisengageTiles: 5, RespawnTicks: 50}

	// Barbarian (Barbarian Village), lvl 7: 14 hp, attack/strength 6, defence 2, strength bonus 7
	// so a max hit of 2; not aggressive (no huntmode), wanderrange 10, respawn 50. Config:
	// scripts/_unpack/225/all.npc [barbarian]. Drops: bones, chaos 2 at 5/128, coins.
	//
	// The Barbarian WOMAN (npc 17, lvl 8) is the same herd (Also): 14 hp, respawn 50, wander 10 and chase
	// 12, defence 5 against 2, strength 5 +10 against 6 +7 (max hit 2 as well), one attack every 6 ticks
	// against every 5, the same drop table ([ai_queue3,_barbarian]: "Level 7 & 8 in Barbarian Village").
	// Five of them stand among the twelve Barbarians (content m48_53).
	MonsterBarbarian = Monster{Name: "Barbarian", Also: []string{"Barbarian woman"}, CombatLevel: 7, Hitpoints: 14, MaxHit: 2, PlainBones: true, DisengageTiles: 12, RespawnTicks: 50}

	MonsterGiant     = Monster{Name: "Giant", CombatLevel: 28, Hitpoints: 35, MaxHit: 4, BigBones: true, DisengageTiles: 5, RespawnTicks: 60}
	MonsterMossGiant = Monster{Name: "Moss giant", CombatLevel: 42, Hitpoints: 60, MaxHit: 6, BigBones: true, DisengageTiles: 5, RespawnTicks: 60}
	MonsterOgre      = Monster{Name: "Ogre", CombatLevel: 53, Hitpoints: 60, MaxHit: 7, BigBones: true, DisengageTiles: 5, RespawnTicks: 60}
	MonsterIceGiant  = Monster{Name: "Ice giant", CombatLevel: 53, Hitpoints: 70, MaxHit: 7, BigBones: true, DisengageTiles: 5, RespawnTicks: 60}
	MonsterFireGiant = Monster{Name: "Fire giant", CombatLevel: 86, Hitpoints: 111, MaxHit: 11, BigBones: true, DisengageTiles: 5, RespawnTicks: 60}

	// White Knight (Falador castle), lvl 36: 52 hp, attack 27, strength 29 (+31 bonus: max hit
	// 5.5, rounded up for the food budget), defence 21; not aggressive, respawn 100 (a minute),
	// plain Bones. Config: scripts/areas/area_falador/configs/falador.npc [white_knight]; drops
	// scripts/drop tables/scripts/white_knight.rs2 (mind, nature, chaos, blood, coins, herbs).
	MonsterWhiteKnight = Monster{Name: "White Knight", CombatLevel: 36, Hitpoints: 52, MaxHit: 6, PlainBones: true, DisengageTiles: 5, RespawnTicks: 100}
	// Ice warrior (Asgarnian ice dungeon), lvl 57: 59 hp, 47/47/47, NO strength bonus so a max hit
	// of 5.5 (rounded up), aggressive to any player under combat 114, respawn 60, plain Bones.
	// Config: scripts/_unpack/225/all.npc [icewarrior]; drops drop tables/scripts/ice_warrior.rs2
	// (chaos 3 at 8/128, death 2 at 3/128, nature, law, cosmic, blood 2 at 1/128).
	MonsterIceWarrior = Monster{Name: "Ice warrior", CombatLevel: 57, Hitpoints: 59, MaxHit: 6, PlainBones: true, DisengageTiles: 5, RespawnTicks: 60}
)

// Rung is one step of the ladder: a monster, a place to fight it, and the band of the lowest
// melee stat it covers.
type Rung struct {
	// Key names the rung in settings and in the log.
	Key     string
	Monster Monster
	// Stand is where the runtime fights from, chosen to sit inside several spawns' own
	// 5-tile leash.
	Stand tile.Tile
	// Spawns are the verified spawn tiles, read from content/maps. Their count is the rung's
	// real supply, and the runtime logs it so a thin rung is visible rather than mysterious.
	Spawns []tile.Tile
	// Leash is how far from Stand the runtime will chase a target. Kept at or under the
	// monster's own DisengageTiles plus the spread of Spawns, so a fight cannot be walked out
	// of by the script itself.
	Leash int
	// FromLevel and ToLevel are the band of the LOWEST melee stat this rung covers.
	// ToLevel is exclusive.
	FromLevel, ToLevel int
	// MinCombatLevel refuses the rung until the account can survive the trip. This is not
	// caution in the abstract: the Edgeville dungeon's only route from the trapdoor landing
	// runs north through the Varrock sewers past level 42 Hobgoblins, and a fresh level-3
	// account died on it during the route proof.
	MinCombatLevel int
	// Members is whether the rung needs a members world.
	Members bool
	// TravelProven records whether this client has actually walked to the rung. Only the
	// three dungeon entrances have been (Edgeville trapdoor, Varrock manhole, Asgarnian ice
	// dungeon ladder); the members rungs beyond them have not, and the runtime says so and
	// stops rather than spinning.
	TravelProven bool
	// NeedsWieldedBlade is the Varrock sewer web: the server reads the RIGHT-HAND slot only,
	// so a knife in the pack does not cut it. Any bladed melee weapon this trainer wears
	// satisfies it, but it has to be worn.
	NeedsWieldedBlade bool
	// Bank is the nearest usable bank, confirmed against the baked loc index (id 2213
	// "Bank booth", op "Use-quickly").
	Bank tile.Tile
	// Entrance describes the way in, for the log.
	Entrance string
	// Note carries anything an operator reading the log should know.
	Note string

	// MinHitpoints refuses the rung to an account whose BASE Hitpoints level is below it, and
	// MinHitpointsBelow, when set, limits that gate to accounts whose lowest melee stat is still
	// below it: the giants rung's early band (spread.go) admits melee 20-24 only at 30 hitpoints,
	// and from 25 it is the rung it always was. Zero is no gate. Only the spread picker reads these
	// (PickRung has no hitpoints to compare), and only spread rungs set them.
	MinHitpoints, MinHitpointsBelow int
	// Wilderness is the deepest Wilderness level the rung's spawns stand at (content's
	// wilderness_zones: one level per 8 tiles north of z 3520), 0 outside it. A Wilderness rung is
	// on PvP ground the whole way in and out, and SpreadLadderWith leaves every one out unless the
	// caller opts in (stage5WildernessRungs).
	Wilderness int
	// WestSide marks a rung on Scarab's own side of the map (Ardougne, Yanille: west of White Wolf
	// Mountain and off Karamja). CombatTrainer's walks plan without ships, so only an account that
	// already stands on that side (WestSide(tile)) is offered one; the rest of the ladder needs the
	// ship crossing scarabplan.TrainerCrossWest pays for.
	WestSide bool
	// Herd marks a rung whose monsters are aggressive and crowd the stand, a cave full of them: a random
	// event beside the account there has no clear way out, so the trainer does not bury bones at it (a
	// buried bone is the Shade's and the Zombie's trigger, content bury_bone.rs2 -> macro_event_prayer) and
	// does not train there on a thin food load (HerdFoodFloor). Set only where the bones are plain: a big-bone
	// rung is there to bury them.
	Herd bool
	// SafeSpot is a tile from which a spell reaches SafeSpotSpawns of the rung's spawns while no
	// monster there can melee it: every hostile spawn nearby is more than its maxrange+1 tiles away
	// (Chebyshev; Npc.targetWithinMaxRange drops a target that far from the NPC's own spawn, so the
	// NPC turns back however it is attacked), and the line of sight was checked against the
	// collision pack's walls and the loc index's projectile blockers. The zero tile is "none found".
	// Recorded for a magic step at the rung; nothing casts from it yet.
	SafeSpot       tile.Tile
	SafeSpotSpawns int
	// Pen, when set, says the rung's ground is a fenced pen (pen.go): the account is at the rung only while it stands inside the pen,
	// it walks in by the gate to the pen's Enter tile, it picks only the monsters a walk over open ground reaches, nearest by that
	// walk, and when none is in reach it walks the pen's Beat. Nil is every other rung, unchanged.
	Pen *Enclosure
}

// SpawnCount is the rung's verified supply.
func (r Rung) SpawnCount() int { return len(r.Spawns) }

// HasSafeSpot reports whether a verified magic safespot is recorded for the rung.
func (r Rung) HasSafeSpot() bool { return r.SafeSpotSpawns > 0 }

// Account is what a rung's gates are judged against.
type Account struct {
	// LowestMelee is the band level: the lowest of Attack, Strength and Defence.
	LowestMelee int
	// CombatLevel is the engine's own combat level.
	CombatLevel int
	// Hitpoints is the BASE Hitpoints level; 0 (unknown) fails every MinHitpoints gate.
	Hitpoints int
	// Members is whether the account is on a members world.
	Members bool
	// West is whether the account stands on Scarab's side of the map (WestSide).
	West bool
}

// Admits reports whether the rung may be chosen for this account: the band holds its lowest melee
// stat, the world and the route allow it, and it clears the combat-level, hitpoints and side gates.
func (r Rung) Admits(a Account) bool {
	if a.LowestMelee < r.FromLevel || a.LowestMelee >= r.ToLevel {
		return false
	}
	if (r.Members && !a.Members) || !r.TravelProven || a.CombatLevel < r.MinCombatLevel {
		return false
	}
	if r.MinHitpoints > 0 && (r.MinHitpointsBelow <= 0 || a.LowestMelee < r.MinHitpointsBelow) && a.Hitpoints < r.MinHitpoints {
		return false
	}
	return !r.WestSide || a.West
}

// WestSide reports whether a tile is on Scarab's own side of the map for a walk with no ships: on
// the surface, west of White Wolf Mountain (x < 2840) and not on Karamja (x 2740 and over at z 3240
// and under). East Ardougne, Yanille, the Tree Gnome Village, Seers' Village and Catherby are; the
// Edgeville dungeon, Falador, Varrock and Brimhaven are not.
func WestSide(t tile.Tile) bool {
	if t.Z >= DungeonZOffset || t.X >= 2840 || t.X <= 0 {
		return false
	}
	return !(t.X >= 2740 && t.Z <= 3240)
}

// WildernessLevel is content's wilderness_zones level of a tile: one level per 8 tiles from z 3520
// on the surface (and from z 9920 on the dungeon layer), between x 2944 and 3391; 0 outside.
func WildernessLevel(t tile.Tile) int {
	if t.X < 2944 || t.X > 3391 {
		return 0
	}
	switch {
	case t.Z >= 3520 && t.Z < DungeonZOffset:
		return (t.Z-3520)/8 + 1
	case t.Z >= 9920 && t.Z <= 12800:
		return (t.Z-9920)/8 + 1
	}
	return 0
}

// Goals are the levels a run is trying to reach.
type Goals struct {
	Attack, Strength, Defence, Ranged, Magic, Prayer int
}

// DefaultGoals is the account owner's target: base 75 melee, Ranged and Magic, plus 43 Prayer.
func DefaultGoals() Goals {
	return Goals{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75, Prayer: 43}
}

// meleeLadder is the default schedule. Ordered by band.
var meleeLadder = []Rung{
	{
		Key: "cows", Monster: MonsterCow,
		// The Lumbridge pasture is a closed pen (pen.go). The stand is the middle of it, and the farthest of its ten spawns is 19 tiles
		// from there, which is the leash. This rung used to stand at the pen's far north end with a leash of 8, so five of the seven
		// spawns it listed were outside the leash and the three spawns by the gate were not listed at all.
		Stand:  tile.Tile{X: 3257, Z: 3276},
		Spawns: cowPenSpawns,
		Leash:  19, FromLevel: 1, ToLevel: 10, MinCombatLevel: 3,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 3253, Z: 3419},
		Entrance: "a fenced pasture east of Lumbridge: the one way in is the double Gate at 3253,3266 and 3253,3267, which the walk opens",
		Note:     "cows leave plain Bones (death_drop defaults to bones), 4.5 Prayer XP each; only big bones come fast. The pen's ten cows are shared with the Lumbridge quest farms",
		Pen:      &CowPen,
	},
	{
		Key: "alkharid", Monster: MonsterWarrior,
		Stand:  tile.Tile{X: 3288, Z: 3172},
		Spawns: []tile.Tile{{X: 3282, Z: 3176}, {X: 3286, Z: 3170}, {X: 3288, Z: 3174}, {X: 3291, Z: 3168}, {X: 3293, Z: 3175}, {X: 3295, Z: 3171}, {X: 3301, Z: 3170}, {X: 3301, Z: 3174}, {X: 3301, Z: 3177}},
		// This band runs to 25, not to 20, and the extra five levels are not padding. The
		// giants rung below refuses anything under combat level 30, and the engine's own
		// combat formula (Player.getCombatLevel) does not reach 30 until the melee stats are
		// about 25: at melee 20 with the Hitpoints that training brings, the account is combat
		// 24. Ending this band at 20 therefore left lowest-melee 20 to 24 with no rung it
		// could take and no fallback, and PickRung's BlockedCombatLevel is terminal in
		// combattrainer — the run stops rather than training on. Measured, not assumed.
		// Leash 13 (was 10): the three spawns at x 3301 are 13 tiles from the stand, so the old leash
		// left a third of the supply the spread weighs this rung by out of reach (6 Oct 2026).
		Leash: 13, FromLevel: 10, ToLevel: 25, MinCombatLevel: 8,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 3268, Z: 3167},
		Entrance: "Al Kharid palace; the toll gate costs 10 coins",
		Note:     "warriors leave plain Bones (death_drop defaults to bones). The band runs to 25 so the account is combat 30 by the time the giants rung will have it",
	},
	{
		Key: "giants", Monster: MonsterGiant,
		Stand: tile.Tile{X: 3113, Z: 9836},
		Spawns: []tile.Tile{
			{X: 3099, Z: 9832}, {X: 3100, Z: 9836}, {X: 3106, Z: 9827}, {X: 3107, Z: 9835},
			{X: 3110, Z: 9845}, {X: 3111, Z: 9841}, {X: 3113, Z: 9831}, {X: 3116, Z: 9832},
			{X: 3117, Z: 9835}, {X: 3117, Z: 9843}, {X: 3117, Z: 9850}, {X: 3122, Z: 9844},
		},
		// The band runs to 60, not to 45, and that is a measured decision rather than a widening
		// for tidiness. A 3h7m window on 2026-09-20 fought this rung and the sewers moss giants
		// back to back, and the one BELOW won on every axis that was counted: 449 XP/min against
		// 220, 2 of the 7 deaths against 5, and 12 spawns inside a 12-tile leash against a rung
		// whose own note admits only two of its four are reachable from its stand. A rung that
		// trains at half the rate of the one beneath it, in a place that kills twice as often,
		// is not a step up.
		//
		// Nothing is deleted by this: mossgiants still covers the same band, so it stays
		// pinnable with "rung": "mossgiants", and PickRung's existing ranking resolves the
		// overlap with no special case, since both are free and travel-proven and this one has
		// the deeper supply. Honest limit: this rung's rate at 45-60 specifically is NOT
		// established. 449 XP/min was measured across 25-45, and a level-28 Giant with 35
		// hitpoints dies faster to a level-55 account rather than slower, so the rate should
		// hold or rise — but no window has watched it.
		// Leash 14 (was 12): 3099,9832, 3100,9836 and 3117,9850 are 13-14 tiles from the stand, so
		// three of the twelve spawns were never picked (6 Oct 2026).
		Leash: 14, FromLevel: 25, ToLevel: 60,
		// The route proof killed a level-3 account on the way in, so 30 is the lowest combat
		// level this ladder will attempt it at. The band starts at 25 rather than 20 because
		// that gate has to be reachable: by the engine's own formula (Player.getCombatLevel)
		// an account with melee 20 is combat 24, so a 20-45 band left a five-level hole where
		// the only rung in band refused itself, PickRung answered BlockedCombatLevel, and
		// combattrainer turned that into a stopped run. Melee 25 is the first level that
		// clears combat 30.
		MinCombatLevel: 30,
		Members:        false, TravelProven: true, NeedsWieldedBlade: true,
		Bank:     tile.Tile{X: 3095, Z: 3491},
		Entrance: "Edgeville trapdoor at 3097,3468 (loc 1568 to open, 1570 to climb down)",
		Note:     "the way in from the landing chamber runs north through the Varrock sewers past level 42 Hobgoblins, and the web there needs a bladed weapon WORN, not carried. First rung that drops big bones",
	},
	{
		Key: "mossgiants", Monster: MonsterMossGiant,
		// The stand was 3163,9884, and that tile is STRANDED in the baked collision pack: a
		// walk from it reaches neither the manhole landing nor any of this rung's own four
		// spawns. Measured with cmd/webwalkplan, which needs no server: from 3163,9884 every
		// plan dies after expanding the same 53,405 tiles — its whole closed component — while
		// the main component the manhole lands in expands 537,551. That is what made a band
		// transition at 45 end in "unreachable (best 38 tiles)" three times and stop the run,
		// and it was read as a routing failure from inside the Giant chamber. It is not: the
		// chamber plans out to the Edgeville exit ladder at 3116,9853 in 13 cost, and the
		// manhole route reaches THIS tile in 245.
		//
		// 3159,9900 is one of the rung's own verified spawns, so it also satisfies the
		// disengage rule at distance 0. Honest limit: only the northern pair of spawns falls
		// inside the 14-tile leash from here (3157,9905 is 5 away, 3159,9900 is 0). The
		// southern pair is reachable on foot (cost 36 and 34 from here) but sits 21 and 22
		// tiles out, so this rung fights two spawns, not four.
		Stand:  tile.Tile{X: 3159, Z: 9900},
		Spawns: []tile.Tile{{X: 3157, Z: 9905}, {X: 3159, Z: 9900}, {X: 3163, Z: 9878}, {X: 3167, Z: 9879}},
		Leash:  14, FromLevel: 45, ToLevel: 60, MinCombatLevel: 45,
		Members: false, TravelProven: true, NeedsWieldedBlade: true,
		Bank:     tile.Tile{X: 3253, Z: 3419},
		Entrance: "Varrock manhole at 3237,3458 (loc 881 to open, 882 to climb down)",
		Note:     "only FOUR spawns in the free-to-play sewers and only the northern TWO are inside the leash. Measured against the giants rung over one window it earned 220 XP/min to their 449 and took 5 deaths to their 2, so the free ladder no longer prefers it: the giants band now runs to 60 and outranks this on spawn supply. Kept because it is real, reachable and pinnable, and because the members Brimhaven rung shares its band",
	},
	{
		Key: "icegiants", Monster: MonsterIceGiant,
		Stand:  tile.Tile{X: 3058, Z: 9568},
		Spawns: []tile.Tile{{X: 3054, Z: 9564}, {X: 3057, Z: 9568}, {X: 3065, Z: 9571}},
		Leash:  12, FromLevel: 60, ToLevel: 75, MinCombatLevel: 60,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 3013, Z: 3354},
		Entrance: "Asgarnian ice dungeon ladder at 3008,3150 (loc 1759), approachable from the EAST only",
		Note:     "three spawns. Prayer 43 lands during this band from the big bones the 20-75 giant kills drop",
	},

	// Members rungs. Real, content-verified supply, and deliberately marked unproven: no
	// route to any of them has been walked with this client, so the runtime refuses them and
	// says why rather than walking an account into the sea.
	{
		Key: "mossgiants-brimhaven", Monster: MonsterMossGiant,
		Stand:  tile.Tile{X: 2693, Z: 3210},
		Spawns: []tile.Tile{{X: 2691, Z: 3215}, {X: 2693, Z: 3209}, {X: 2695, Z: 3212}, {X: 2697, Z: 3204}, {X: 2699, Z: 3216}},
		Leash:  14, FromLevel: 45, ToLevel: 60, MinCombatLevel: 45,
		Members: true, TravelProven: false,
		Bank:     tile.Tile{X: 2809, Z: 3441},
		Entrance: "Brimhaven, reached by ship — NOT walked with this client",
		Note:     "five spawns here; 32 Moss giant spawns exist on members ground in total",
	},
	{
		Key: "ogres", Monster: MonsterOgre,
		// West of Yanille's west gate, re-read from content m39_48 ("trainspots-1003"): the spawn list
		// this rung carried before did not match the map file (2500,3085, 2506,3081 ... are not Ogre
		// tiles). Of the 13 Ogres within 25 tiles of 2505,3095, these seven are reachable on foot from
		// the stand (cmd/webwalkplan over the collision pack); the four at 2509-2514,3081-3087 are in
		// a pen no walk enters, and 2508,3119 is 23 steps round. ogre is maxrange 5, ogre2 the default
		// wanderrange 3 + 2, so neither chases more than 6 tiles from its spawn.
		Stand: tile.Tile{X: 2503, Z: 3100},
		Spawns: []tile.Tile{
			{X: 2496, Z: 3092}, {X: 2497, Z: 3089}, {X: 2500, Z: 3092}, {X: 2500, Z: 3097},
			{X: 2503, Z: 3095}, {X: 2507, Z: 3108}, {X: 2505, Z: 3111},
		},
		Leash: 12, FromLevel: 60, ToLevel: 75, MinCombatLevel: 60,
		Members: true, TravelProven: false,
		Bank:     tile.Tile{X: 2613, Z: 3094},
		Entrance: "Yanille's west gate (Large doors 1519 at 2539,3092 and 1516 at 2532,3092), 116 from the Yanille bank, planned offline only",
		Note:     "77 Ogre spawns on members ground, none free-to-play and none in the wilderness: the deepest supply on the ladder. Big bones every kill. The Stage 5 spread ladder takes this rung from melee 45 (spread.go); this ladder still treats the route as unwalked",
		SafeSpot: tile.Tile{X: 2498, Z: 3104}, SafeSpotSpawns: 2,
	},
	{
		Key: "firegiants", Monster: MonsterFireGiant,
		Stand:  tile.Tile{X: 2574, Z: 9893},
		Spawns: []tile.Tile{{X: 2568, Z: 9889}, {X: 2573, Z: 9895}, {X: 2575, Z: 9891}, {X: 2577, Z: 9890}, {X: 2577, Z: 9897}, {X: 2578, Z: 9895}, {X: 2580, Z: 9890}, {X: 2581, Z: 9895}},
		Leash:  12, FromLevel: 70, ToLevel: 75, MinCombatLevel: 75,
		Members: true, TravelProven: false,
		Bank:     tile.Tile{X: 2946, Z: 3368},
		Entrance: "Taverley dungeon — NOT walked with this client",
		Note:     "max hit 11: the only rung on this ladder that can take a trainer apart. 23 members spawns outside the wilderness; five more are INSIDE it and are excluded",
	},
}

// MeleeLadder returns the default melee schedule.
func MeleeLadder() []Rung {
	out := make([]Rung, len(meleeLadder))
	copy(out, meleeLadder)
	return out
}

// MaxTrainableGoal is the highest goal this ladder can actually finish, for the five
// disciplines that band on it: Attack, Strength, Defence, Ranged and Magic.
//
// A run trains at the band holding its CURRENT level and stops once that level reaches the
// goal, so the highest level it ever needs a rung for is goal-1. Bands are [FromLevel,
// ToLevel), so the highest band level any rung covers is max(ToLevel)-1 and the highest
// finishable goal is max(ToLevel) itself — 75, because icegiants ends at 75 exclusive.
//
// That off-by-one is the defect this exists to stop, and it is a cliff rather than a slope. A
// goal of exactly 75 works, but only because AllGoalsMet fires before the band is ever asked.
// A goal of 76 asks PickRung for a rung covering level 75, gets BlockedNoBand, and chooseRung
// turns that into a returned error — so the run trains for hours, reaches 75, and only then
// dies with "no rung available". The settings schema nevertheless offered 99 on all five.
//
// Only rungs the runtime would actually accept are counted. An unproven route and a members
// rung on a free world are both refused by PickRung, so counting either would promise a
// ceiling no run could reach.
//
// Honest limit: this is the TOP of the ladder, not a proof that the bands below it are
// contiguous. The 20-to-25 hole that left BlockedCombatLevel stopping runs was exactly such a
// gap, and this function would not have caught it — TestLadderBandsCoverEveryLevel does that.
func MaxTrainableGoal(ladder []Rung, membersWorld bool) int {
	best := 0
	for _, r := range ladder {
		if !r.TravelProven || (r.Members && !membersWorld) {
			continue
		}
		if r.ToLevel > best {
			best = r.ToLevel
		}
	}
	return best
}

// RungByKey finds a rung by its key.
func RungByKey(ladder []Rung, key string) (Rung, bool) {
	for _, r := range ladder {
		if r.Key == key {
			return r, true
		}
	}
	return Rung{}, false
}

// LowestMelee returns the melee skill furthest behind and its level. Ties go to Attack, then
// Strength, then Defence, matching fighterplan.NextMeleeStyle's own tie order so the style the
// fighter trains and the rung it stands on never disagree.
func LowestMelee(l Levels) (Skill, int) {
	best, bestLevel := SkillAttack, l.Attack
	if l.Strength < bestLevel {
		best, bestLevel = SkillStrength, l.Strength
	}
	if l.Defence < bestLevel {
		best, bestLevel = SkillDefence, l.Defence
	}
	return best, bestLevel
}

// StatToTrain picks the stat furthest below its goal, as a fraction of the distance still to
// go rather than by raw level, so a run does not park on one stat while another sits at 1.
// ok is false once every melee goal is met.
func StatToTrain(l Levels, g Goals) (Skill, bool) {
	type cand struct {
		skill   Skill
		short   int
		current int
	}
	cands := []cand{
		{SkillAttack, g.Attack - l.Attack, l.Attack},
		{SkillStrength, g.Strength - l.Strength, l.Strength},
		{SkillDefence, g.Defence - l.Defence, l.Defence},
	}
	best, found := cand{}, false
	for _, c := range cands {
		if c.short <= 0 {
			continue
		}
		// Furthest from its goal wins; on a tie the lower current level wins, then the
		// earlier stat in Attack/Strength/Defence order.
		if !found || c.short > best.short || (c.short == best.short && c.current < best.current) {
			best, found = c, true
		}
	}
	return best.skill, found
}

// BalanceStrOverAtt and BalanceAttOverDef are the owner's melee spread (7 Oct 2026): "keep Strength 5 above
// Attack and Attack 5 above Defence", so Strength is the highest of the three, 10 over Defence.
const (
	BalanceStrOverAtt = 5
	BalanceAttOverDef = 5
)

// BalancedMelee is the melee stat to raise to keep Strength = Attack+5 and Attack = Defence+5, from the
// three base levels alone: Strength while it is under Attack+5, else Attack while it is under Defence+5 or
// Strength is over Attack+5, else Defence (Attack over Defence+5, or all balanced, which starts the next
// cycle: Defence, then Attack, then Strength). The three climb together at the offsets, which "lowest
// first" and "furthest from its goal" do not do (those equalise them).
func BalancedMelee(attack, strength, defence int) Skill {
	e1 := strength - (attack + BalanceStrOverAtt)
	e2 := attack - (defence + BalanceAttOverDef)
	switch {
	case e1 < 0:
		return SkillStrength
	case e2 < 0, e1 > 0:
		return SkillAttack
	}
	return SkillDefence
}

// BalancedStatToTrain is StatToTrain under the spread above: the stat BalancedMelee names unless its goal is
// already met, then StatToTrain's pick (the furthest from its goal). ok is false once every melee goal is met.
func BalancedStatToTrain(l Levels, g Goals) (Skill, bool) {
	if MeleeGoalsMet(l, g) {
		return "", false
	}
	switch want := BalancedMelee(l.Attack, l.Strength, l.Defence); {
	case want == SkillAttack && l.Attack < g.Attack,
		want == SkillStrength && l.Strength < g.Strength,
		want == SkillDefence && l.Defence < g.Defence:
		return want, true
	}
	return StatToTrain(l, g)
}

// MeleeGoalsMet reports whether Attack, Strength and Defence have all reached their goals.
func MeleeGoalsMet(l Levels, g Goals) bool {
	return l.Attack >= g.Attack && l.Strength >= g.Strength && l.Defence >= g.Defence
}

// Blocked says why no rung is available, so the caller can log one clear line instead of
// spinning. An empty Blocked means a rung was found.
type Blocked string

const (
	// NotBlocked is the zero value: a rung was chosen.
	NotBlocked Blocked = ""
	// BlockedNoBand means no rung covers this level at all.
	BlockedNoBand Blocked = "no rung covers this level"
	// BlockedCombatLevel means the band's rung refuses the account's combat level.
	BlockedCombatLevel Blocked = "combat level too low for the rung's route"
	// BlockedMembers means every rung for this band needs a members world.
	BlockedMembers Blocked = "the only rungs for this level are members-only"
	// BlockedUnproven means the only rung left has no proven route.
	BlockedUnproven Blocked = "no route to this rung has been walked with this client"
	// BlockedHitpoints means the band's rungs want more base hitpoints (Rung.MinHitpoints; the
	// spread picker only).
	BlockedHitpoints Blocked = "hitpoints too low for the rung"
	// BlockedSide means the band's only rung is on Scarab's side of the map and the account is not
	// (Rung.WestSide; the spread picker only).
	BlockedSide Blocked = "the rung is across the water from the account"
)

// RungChoice is what PickRung decided and why.
type RungChoice struct {
	Rung Rung
	// OK is whether Rung is usable.
	OK bool
	// Why is the reason when OK is false.
	Why Blocked
	// Detail names the rung that was rejected, when one was.
	Detail string
	// Full is set by PickRungSpread with seat caps (Crowd.SeatsPerSpawn): every usable rung was at its cap,
	// and this is the least full one. The caller takes the seat anyway, but does not move an account to it
	// from one it already holds.
	Full bool
}

// PickRung chooses the rung for the lowest melee stat.
//
// The order is deliberate: among the rungs whose band contains lowestMelee, prefer a proven
// route over an unproven one, and a free-to-play rung over a members one when both are
// otherwise equal — a proven free rung with four spawns still trains, while an unproven
// members rung with seventy-seven does not. Among equals, the deeper supply wins.
//
// When nothing qualifies, OK is false and Why/Detail carry a reason a log line can state
// outright. That is the whole point: this trainer has no mule to ask for help, so an
// unreachable rung has to stop the run visibly rather than spin.
func PickRung(ladder []Rung, lowestMelee, combatLevel int, membersWorld bool) RungChoice {
	var inBand []Rung
	for _, r := range ladder {
		if lowestMelee >= r.FromLevel && lowestMelee < r.ToLevel {
			inBand = append(inBand, r)
		}
	}
	if len(inBand) == 0 {
		return RungChoice{Why: BlockedNoBand}
	}

	var best Rung
	found := false
	var sawMembersOnly, sawUnproven, sawCombatGate bool
	var gateDetail string
	for _, r := range inBand {
		if r.Members && !membersWorld {
			sawMembersOnly = true
			continue
		}
		if !r.TravelProven {
			sawUnproven = true
			continue
		}
		if combatLevel < r.MinCombatLevel {
			sawCombatGate = true
			gateDetail = r.Key
			continue
		}
		if !found || better(r, best) {
			best, found = r, true
		}
	}
	if found {
		return RungChoice{Rung: best, OK: true}
	}
	switch {
	case sawCombatGate:
		return RungChoice{Why: BlockedCombatLevel, Detail: gateDetail}
	case sawUnproven:
		return RungChoice{Why: BlockedUnproven, Detail: inBand[0].Key}
	case sawMembersOnly:
		return RungChoice{Why: BlockedMembers, Detail: inBand[0].Key}
	}
	return RungChoice{Why: BlockedNoBand}
}

// better ranks two usable rungs: free-to-play first (no membership to lose), then the deeper
// spawn supply.
func better(a, b Rung) bool {
	if a.Members != b.Members {
		return !a.Members
	}
	return a.SpawnCount() > b.SpawnCount()
}

// PrayerRung picks where to fight for bones once every combat goal is met and only Prayer is
// short. It answers a different question from PickRung, and that difference is a defect this
// ladder shipped rather than a refinement.
//
// The case is neither rare nor hypothetical: the owner's goals are base 75 in all five combat
// stats plus 43 Prayer, and Prayer is a by-product of the big bones the giant rungs drop, so
// which of the two lands first is arithmetic nobody schedules. A run that reaches 75/75/75 with
// Prayer at 42 has to keep killing for the last bones — and it cannot ask PickRung where, because
// PickRung bands on the level of the stat being trained and no rung covers level 75 (icegiants
// ends at 75, exclusive). It answers BlockedNoBand, which combattrainer turns into a returned
// error: the run STOPS short of the goal it was two bones from, and stops with an error rather
// than the clean banked logout the finish condition promises.
//
// The cheapest kill wins, and that is the whole ranking. Every rung here drops exactly one big
// bone per kill, unconditionally (param=death_drop,big_bones, added before any random table is
// rolled), so bones per minute is decided by how long a kill takes and not at all by what the
// monster is worth in XP. A Giant has 35 hitpoints against an Ice giant's 70, so the LOWEST bone
// rung the account can reach is both the fastest Prayer on this ladder and the safest one.
func PrayerRung(ladder []Rung, combatLevel int, membersWorld bool) RungChoice {
	var best Rung
	found := false
	var sawMembersOnly, sawUnproven, sawCombatGate bool
	var gateDetail, membersDetail, unprovenDetail string
	for _, r := range ladder {
		if !r.Monster.BigBones {
			continue
		}
		// Every refusal names the FIRST rung it applied to rather than the last. The ladder is
		// ordered by band, so the first is the cheapest kill the account came nearest to
		// qualifying for; naming the top of the ladder instead sends a reader to the rung they
		// were never going to reach anyway.
		if r.Members && !membersWorld {
			if !sawMembersOnly {
				membersDetail = r.Key
			}
			sawMembersOnly = true
			continue
		}
		if !r.TravelProven {
			if !sawUnproven {
				unprovenDetail = r.Key
			}
			sawUnproven = true
			continue
		}
		if combatLevel < r.MinCombatLevel {
			if !sawCombatGate {
				gateDetail = r.Key
			}
			sawCombatGate = true
			continue
		}
		if !found || betterForBones(r, best) {
			best, found = r, true
		}
	}
	if found {
		return RungChoice{Rung: best, OK: true}
	}
	switch {
	case sawCombatGate:
		return RungChoice{Why: BlockedCombatLevel, Detail: gateDetail}
	case sawUnproven:
		return RungChoice{Why: BlockedUnproven, Detail: unprovenDetail}
	case sawMembersOnly:
		return RungChoice{Why: BlockedMembers, Detail: membersDetail}
	}
	return RungChoice{Why: BlockedNoBand}
}

// betterForBones ranks two bone rungs: off the Wilderness first (a bone is worth no PvP ground),
// then the lower band, since that is the cheaper kill, then the shorter-lived monster, then the
// deeper spawn supply.
func betterForBones(a, b Rung) bool {
	if (a.Wilderness > 0) != (b.Wilderness > 0) {
		return a.Wilderness == 0
	}
	if a.FromLevel != b.FromLevel {
		return a.FromLevel < b.FromLevel
	}
	if a.Monster.Hitpoints != b.Monster.Hitpoints {
		return a.Monster.Hitpoints < b.Monster.Hitpoints
	}
	return a.SpawnCount() > b.SpawnCount()
}

// FoodPerTrip sizes a food load from the rung's own max hit: enough bites to absorb a dozen
// maximum hits, capped at a sane fraction of the pack so there is room for bones and loot.
func FoodPerTrip(r Rung, healPerBite int) int {
	if healPerBite < 1 {
		healPerBite = 1
	}
	need := (r.Monster.MaxHit * 12) / healPerBite
	if need < 4 {
		need = 4
	}
	if need > 12 {
		need = 12
	}
	return need
}
