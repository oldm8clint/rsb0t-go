package retaliateplan

// TicksPerHour is 6000: one tick every 600 ms.
const TicksPerHour = 6000

// DepleteOneIn is the chance a successful log fells a tree: random(8) = 0 (woodcut.rs2:133,149).
const DepleteOneIn = 8

// Tree is one row of content/scripts/skill_woodcutting/configs/trees.dbrow for the rune axe (successchance low and high
// are the dbrow's two numbers; XP is real experience, the dbrow's productexp divided by ten).
type Tree struct {
	Name string
	XP   float64
	// Low and High are the rune axe's (low, high) pair; Adamant the adamant axe's.
	Low, High     int
	ALow, AHigh   int
	RespawnTicks  int
	LevelRequired int
	MultiSites    string // where the 289 maps put one in a multi-combat zone, "" when none
}

// Trees is the table (trees.dbrow:85-146). Respawn is the dbrow's respawnrate before scale_by_playercount.
var Trees = []Tree{
	{Name: "Oak", XP: 37.5, Low: 112, High: 350, ALow: 96, AHigh: 300, RespawnTicks: 30, LevelRequired: 15, MultiSites: "Falador south (3), Draynor jail, Wilderness edge"},
	{Name: "Willow", XP: 67.5, Low: 56, High: 175, ALow: 48, AHigh: 150, RespawnTicks: 30, LevelRequired: 30, MultiSites: "Ardougne necromancer tower (no usable hitters)"},
	{Name: "Maple", XP: 100, Low: 28, High: 87, ALow: 24, AHigh: 75, RespawnTicks: 120, LevelRequired: 45},
	{Name: "Yew", XP: 175, Low: 14, High: 44, ALow: 12, AHigh: 37, RespawnTicks: 200, LevelRequired: 60, MultiSites: "Falador south (1), out of reach of the second rat"},
	{Name: "Magic", XP: 250, Low: 7, High: 21, ALow: 6, AHigh: 18, RespawnTicks: 400, LevelRequired: 75},
}

// RollChance is stat_random (PlayerOps.ts:591-600): floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1 out of 256, capped at 1.
// level is the current (boosted) level, capped at 99 by the engine.
func RollChance(level, low, high int) float64 {
	if level > 99 {
		level = 99
	}
	v := low*(99-level)/98 + high*(level-1)/98 + 1
	if v >= 256 {
		return 1
	}
	return float64(v) / 256
}

// RollsToFell is the mean number of rolls until a tree is felled: 8 successes at chance p.
func RollsToFell(p float64) float64 { return DepleteOneIn / p }

// Uptime is the share of time a single chopper spends chopping with m trees it can reach, where felling a tree takes chop
// ticks, switching to the next costs swap ticks, and a felled tree is back after respawn ticks. The chopper goes round
// the trees in turn; a round cannot be shorter than one tree's chop plus its respawn.
func Uptime(m int, chop, swap, respawn float64) float64 {
	if m <= 0 || chop <= 0 {
		return 0
	}
	c := chop + swap
	round := float64(m) * c
	if r := c + respawn; r > round {
		round = r
	}
	return float64(m) * chop / round
}

// XPPerHour is the experience an hour at one roll every period ticks (6000/period rolls), success chance p, xpPerLog
// real xp a log and the given share of time spent chopping.
func XPPerHour(xpPerLog, p, period, uptime float64) float64 {
	return TicksPerHour / period * p * xpPerLog * uptime
}

// HitChance is P(randominc(attackRoll) > randominc(defenceRoll)): the monster's hit roll (npc_combat_melee.rs2:27).
func HitChance(attackRoll, defenceRoll int) float64 {
	var sum float64
	for a := 0; a <= attackRoll; a++ {
		n := a
		if n > defenceRoll+1 {
			n = defenceRoll + 1
		}
		sum += float64(n)
	}
	return sum / (float64(attackRoll+1) * float64(defenceRoll+1))
}

// MonsterMaxHit is npc_melee_maxhit (npc_combat_melee.rs2:1-8, combat.rs2:4-12): (effective*(bonus+64)+320)/640 with
// effective = strength level + 9.
func MonsterMaxHit(strengthLevel, strengthBonus int) int {
	return ((strengthLevel+9)*(strengthBonus+64) + 320) / 640
}

// MonsterAttackRoll is npc_melee_attack_roll: (attack level + 9) * (attackbonus + 64).
func MonsterAttackRoll(attackLevel, attackBonus int) int {
	return (attackLevel + 9) * (attackBonus + 64)
}

// PlayerDefenceRoll is the player's defence roll with no armour and no prayer: (defence level + 9) * 64.
func PlayerDefenceRoll(defenceLevel int) int { return (defenceLevel + 9) * 64 }
