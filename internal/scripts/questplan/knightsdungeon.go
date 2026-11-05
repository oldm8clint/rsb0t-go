package questplan

import (
	"fmt"
	"math"

	"rsb0t-go/internal/scripts/tile"
)

// The Asgarnian Ice Dungeon, for the Knight's Sword's blurite ore. The deadliest ground on the fleet's record: Ice warriors and Ice giants
// killed more than twenty-five accounts in October 2026 (COLLAB.md, the deaths-icedungeon-1017 workflow), all of them standing in it to
// fight, and the Stage 5 trainer's Ice warrior rung is gated at 60 hitpoints for it. The quest never fights here. It walks in, mines one
// ore and walks out, and what it does about the hunters is price the visit and refuse an account that cannot pay it.
//
// THE HUNTERS (content m46_149, m47_149, m48_149 and _unpack/225/all.npc, checked by TestSquireContentTheHunters and TestSquireContentSpawns):
//
//   - 11 Ice warriors (npc 125: level 57, attack, strength and defence 47, 59 hitpoints, hits up to 6 every four ticks, huntmode
//     cowardly, huntrange 5, wanderrange 3) at 3044,9581 3046,9575 3048,9583 3049,9590 3052,9568 3053,9576 3056,9583 3056,9587
//     3057,9566 3062,9576 3062,9581, and 3 Ice giants (npc 111: level 53, 70 hitpoints, attackbonus 29, strengthbonus 31, hits up to 7
//     every five ticks, huntrange 2, wanderrange 3, size 2) at 3054,9564 3057,9568 3065,9571. "Cowardly" skips an account whose combat
//     level is over twice the npc's (HuntCheckNotTooStrong), so the warriors hunt every account of combat 114 or less and the giants 106
//     or less: every account the fleet has.
//   - Near the ladder's landing and on the way: 6 unarmed Hobgoblins (npc 122: level 28, wander 15, hunt combat 56 and under), the armed
//     one at 3019,9591 (npc 123: level 42, up to 84), 9 Pirates (npc 184: level 26, up to 52) west of the route and 4 Muggers (npc 175:
//     level 6, up to 12) beside the landing. The quest refuses an account of combat 57 or less (SqMinCombat, a level more for a combat
//     read that is stale), which takes the Hobgoblins, the Pirates and the Muggers out of the price; the armed Hobgoblin hunts up to 84 but
//     holds no tile of the route to the rocks (TestSquireTheRouteIsHeldOnlyByTheWarriorsAndThePlanPinsTheCount).
//   - The dungeon is single-way combat (maps/multiway.csv has no row for squares 46_149 to 48_149): while one monster is on the account
//     the others skip it (check_notcombat, 8 ticks after the last blow), so the damage taken is one attacker's, not the herd's. The price
//     takes the one attacker and no fewer.
//
// THE ROCKS: four (loc 2110 at 3049,9566 3059,9564 3067,9581 3067,9582, Mining 10). The two in the east are the ones used. Two Ice warriors
// hold their stands (3062,9576 and 3062,9581) and no giant does (a hit of 6 every four ticks and never 7 every five), they are a pair beside
// each other (when another account has mined one out the other is at the same stand, where each of the northern two is a single rock whose
// stand is held by a giant, and the second of them is reached through the giants' ground), and the visit priced over them is within a tenth
// of the cheapest rock's (the first of the northern pair, a walk ten tiles shorter under a giant and a warrior: the real-pack test of the
// route's held tiles prices all four). Every route to every rock crosses the herd's ground; the walk is the planner's
// shortest (a route that avoids the herd's reach costs more tiles inside the herd's hold: the cave is a corridor), and the tiles on it that
// an Ice warrior holds (scarabplan.PenThreat.Holds: within its wander range, its size and two more of its spawn, minus the corner) are
// counted by the real-pack test and pinned below.
//
// THE PRICE. One tile of the walk inside a warrior's hold is one tick of its attacks at walking speed (the account may have no run left;
// running halves it), a quarter attack a tick, each at FoePerAttack for the account's Defence scaled up a quarter (FightIncomingScale); the
// stand is held for as long as the mining is allowed to take (the 99th percentile of the rolls the rock needs at the account's Mining level
// with its pickaxe, SqMineTicks); and the way out costs what the way in did. The account must carry twice that in food (SqFoodMargin)
// and the maximum hitpoints and combat level of SqMinHitpoints and SqMinCombat. Nothing about this is a fight: the account is never to
// strike, and the eat line is two hits and more above the foe's hardest (SqEatAt).
//
// THE WALKER. Both walks (in and out) are the engine's webwalker, with auto-retaliate off for the run (the module's NoRetaliate) and the walk
// running when attacked. Its hostile avoidance (WalkOptions.AvoidHostiles) knows only the bats and wolves of other places, not these
// hunters, and could not use them if it did, because no route avoids the herd (TestSquireNoRouteToTheRocksAvoidsTheHerd): what the visit
// does about the hunters is the price, the top-up on the surface, and the break-off (SqBreakOff) that ends the walk and the mining when the
// account is hurt with nothing left to eat.

// SqMinCombat is the combat level the quest asks for: the unarmed Hobgoblins hunt 56 and under (twice their level), and a combat level read
// from a stale stat can be a level out, which is the margin Demon Slayer keeps too.
const SqMinCombat = 2*28 + 2

// SqFoodMargin is how many times the expected damage of the visit the pack must hold in food.
const SqFoodMargin = 2.0

// SqMaxFoodItems is the most of one kind of food the dungeon's kit asks for, and SqMaxFoodHeal the most hitpoints of food a visit is
// allowed to need: an account whose Defence prices the trip above it is refused (the pack has other things to carry).
const (
	SqMaxFoodItems = 14
	SqMaxFoodHeal  = SqMaxFoodItems * 20
)

// FoeIceWarrior is _unpack/225/all.npc [icewarrior] (npc 125): level 57, attack, defence and strength 47, 59 hitpoints, no attackrate (4)
// and no bonus params, so a hit of up to 6; huntmode cowardly, huntrange 5, wanderrange 3.
var FoeIceWarrior = MeleeFoe{Name: "Ice warrior", Attack: 47, Strength: 47, Defence: 47, Magic: 1, Hitpoints: 59, AttackTicks: 4}

// FoeIceGiant is [icegiant] (npc 111): level 53, attack, defence and strength 40, 70 hitpoints, attackrate 5, attackbonus 29,
// strengthbonus 31, so a hit of up to 7; size 2, huntrange 2, wanderrange 3.
var FoeIceGiant = MeleeFoe{Name: "Ice giant", Attack: 40, Strength: 40, Defence: 40, Magic: 1, Hitpoints: 70, AttackTicks: 5, AttackBonus: 29, StrengthBonus: 31}

// SqIceWarriorSpawns are the Ice warriors (npc 125) of the dungeon, content m47_149.
var SqIceWarriorSpawns = []tile.Tile{
	{X: 3044, Z: 9581}, {X: 3046, Z: 9575}, {X: 3048, Z: 9583}, {X: 3049, Z: 9590}, {X: 3052, Z: 9568}, {X: 3053, Z: 9576},
	{X: 3056, Z: 9583}, {X: 3056, Z: 9587}, {X: 3057, Z: 9566}, {X: 3062, Z: 9576}, {X: 3062, Z: 9581},
}

// SqIceGiantSpawns are the Ice giants (npc 111), content m47_149.
var SqIceGiantSpawns = []tile.Tile{{X: 3054, Z: 9564}, {X: 3057, Z: 9568}, {X: 3065, Z: 9571}}

// SqHunter is a kind of hunter the dungeon holds and the combat level above which it leaves an account alone (twice its level).
type SqHunter struct {
	Name    string
	NPC     int
	Level   int
	Ceiling int
}

// SqHunters are the kinds of hunting npc in the box of the dungeon (content m46_149 to m48_149), by npc id.
var SqHunters = []SqHunter{
	{"Ice warrior", 125, 57, 114}, {"Ice giant", 111, 53, 106}, {"Hobgoblin (armed)", 123, 42, 84},
	{"Hobgoblin", 122, 28, 56}, {"Pirate", 184, 26, 52}, {"Mugger", 175, 6, 12},
}

// SqStand is a place a blurite rock is mined from, with the number of tiles of the planner's route from the landing that an Ice warrior holds
// (HeldIn: an upper bound the real-pack test measures against).
type SqStand struct {
	Rock, Stand tile.Tile
	HeldIn      int
}

// SqStands are the east rocks' stands, nearest the shortest walk first. Both rocks stand beside each other, and both stands are held by the
// same two warriors.
var SqStands = []SqStand{
	{Rock: tile.Tile{X: 3067, Z: 9581}, Stand: tile.Tile{X: 3066, Z: 9581}, HeldIn: 36},
	{Rock: tile.Tile{X: 3067, Z: 9582}, Stand: tile.Tile{X: 3066, Z: 9582}, HeldIn: 37},
}

// SqHeldWalkTiles is the most tiles of the walk in that a warrior holds over the stands the module uses, and the walk out costs the same.
func SqHeldWalkTiles() int {
	most := 0
	for _, st := range SqStands {
		most = max(most, st.HeldIn)
	}
	return most
}

// sqDungeonBox is the cave: the Asgarnian Ice Dungeon's maps (m46_149 to m48_149) at the dungeon's z, level 0.
var sqDungeonBox = Rect{MinX: 2976, MaxX: 3080, MinZ: 9536, MaxZ: 9599}

// InSquireDungeon reports a tile of the Asgarnian Ice Dungeon.
func InSquireDungeon(t tile.Tile) bool { return sqDungeonBox.Contains(t) }

// sqPickRates are the ticks between two rolls of each pickaxe (pickaxes.obj mining_rate).
var sqPickRates = map[int]int{1265: 7, 1267: 6, 1269: 5, 1273: 4, 1271: 3, 1275: 2}

// SqPickaxeRates returns the ticks between rolls of each pickaxe, by object id (a copy).
func SqPickaxeRates() map[int]int {
	out := make(map[int]int, len(sqPickRates))
	for id, r := range sqPickRates {
		out[id] = r
	}
	return out
}

// SqPickRate is the ticks between rolls of the best pickaxe the account holds and can use, else the best one its bank holds, else the bronze
// one the kit would bring.
func SqPickRate(s Snapshot) int {
	if id := DoricUsablePickaxe(s); id != 0 {
		return sqPickRates[id]
	}
	if id := DoricBankedPickaxe(s); id != 0 {
		return sqPickRates[id]
	}
	return sqPickRates[ObjBronzePickaxe]
}

// SqRockChance is the chance one roll at the blurite rock lands at a Mining level: stat_random(mining, 95, 350) is
// floor(95*(99-level)/98) + floor(350*(level-1)/98) + 1 against a draw of 256 (PlayerOps.ts STAT_RANDOM).
func SqRockChance(mining int) float64 {
	lvl := min(max(mining, 1), 99)
	v := (95*(99-lvl))/98 + (350*(lvl-1))/98 + 1
	return float64(min(v, 256)) / 256
}

// SqMineTicks is how long the mining is allowed to take at a Mining level with a pickaxe that rolls every rollTicks: three ticks to take the
// stand and the rolls that make the 99th percentile of the first success.
func SqMineTicks(mining, rollTicks int) int {
	p := SqRockChance(mining)
	rolls := 1
	if p < 1 {
		rolls = int(math.Ceil(math.Log(0.01) / math.Log(1-p)))
	}
	return 3 + rollTicks*max(rolls, 1)
}

// SqTrip is the price of one visit to the dungeon.
type SqTrip struct {
	// OK is false when the account cannot pay it; Why says why.
	OK  bool
	Why string
	// WalkTicks is the ticks of the walk in that a warrior holds (the same again out), MineTicks the ticks at the stand.
	WalkTicks, MineTicks int
	// PerAttack is the hitpoints one Ice warrior attack is expected to take (scaled), Damage the visit's expected total and FoodWanted the
	// hitpoints of food the pack must hold for it (SqFoodMargin times Damage).
	PerAttack, Damage float64
	FoodWanted        int
}

// SquireTripPrice prices the visit for the account in the snapshot: the Defence it takes the blows with, the Mining and pickaxe the rock
// takes, and the food it holds (the caller compares the pool with FoodWanted). It is judged on the account's levels alone; the levels the
// quest asks for are SquireLevelsShort's.
func SquireTripPrice(s Snapshot) SqTrip {
	f := fighterOf(s)
	per := FoePerAttack(FoeIceWarrior, f) * FightIncomingScale
	t := SqTrip{WalkTicks: SqHeldWalkTiles(), PerAttack: per}
	t.MineTicks = SqMineTicks(max(s.Level("mining"), SqMining), SqPickRate(s))
	attacks := float64(2*t.WalkTicks+t.MineTicks) / float64(FoeIceWarrior.AttackTicks)
	t.Damage = attacks * per
	t.FoodWanted = int(math.Ceil(SqFoodMargin * t.Damage))
	t.OK = true
	if t.FoodWanted > SqMaxFoodHeal {
		t.OK = false
		t.Why = fmt.Sprintf("not going down to the Asgarnian Ice Dungeon: at Defence %d the visit is priced at %d hitpoints taken from Ice warriors (hits up to %d), twice that is %d hitpoints of food and the quest carries at most %d",
			max(s.Level("defence"), 1), int(t.Damage+0.5), FoeIceWarrior.PeakHit(), t.FoodWanted, SqMaxFoodHeal)
	}
	return t
}

// SqFoodHeld is the hitpoints of food the pack holds.
func SqFoodHeld(s Snapshot) int { return FoodPoolHeal(s.InvIDs, sqFoodIDs) }

// SqBreakOffPercent is the share of its hitpoints under which an account that holds no food gives the visit up and walks out: with food it
// eats (the eat line is SqEatAt), without it every further tick in the cave is a tick at the herd's mercy.
const SqBreakOffPercent = 35

// SqBreakOff reports that the visit is to be given up: the account is under SqBreakOffPercent of its hitpoints and has nothing left to eat.
func SqBreakOff(hp, maxHP, foodHeal int) bool {
	return maxHP > 0 && foodHeal <= 0 && hp*100 < maxHP*SqBreakOffPercent
}

// SqStartHP is the hitpoints the account goes down with: nine tenths of its maximum, topped up from the pack on the surface (the cave is no
// place to rest, and no bite is eaten during a conversation, so there is none to plan for there).
func SqStartHP(maxHP int) int { return maxHP * 9 / 10 }
