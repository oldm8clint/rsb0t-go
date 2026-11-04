package wcfletchplan

// The chop-and-fletch supply model the maple-rung board prices its camps with.
//
// Everything a camp is worth comes from two numbers: how many logs its trees can GIVE an hour
// whoever stands there (supply), and how many one account can TAKE an hour when nobody else is
// in the way (demand). The first is woodcutplan.CampSupply, already read out of content. The
// second is new here, and every constant it rests on is quoted from the file that sets it:
//
//   - The swing. content/scripts/skill_woodcutting/scripts/woodcut.rs2 [label,cut_tree] sets
//     `%action_delay = calc(map_clock + 3)` on one tick and rolls get_logs on the tick where
//     `%action_delay = map_clock`; the tick after the roll finds %action_delay behind the clock
//     and re-arms it. One roll every FOUR ticks, ChopRollTicks.
//   - The roll. get_logs calls `stat_random(woodcutting, $low, $high)` with the tree table's
//     `successchance` pair for the axe the account swings (configs/trees.dbrow), and the engine's
//     STAT_RANDOM (engine/src/engine/script/handlers/PlayerOps.ts) succeeds when
//     floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1 > floor(random*256). SwingChance is that,
//     exactly, and treeChance below is the dbrow's pairs, verbatim.
//   - The cut. content/scripts/skill_fletching/scripts/cut_logs.rs2 [label,process_fletch_logs]
//     queues each bow `%action_delay = add(map_clock, 2)` after the last: one bow per TWO ticks,
//     FletchTicksPerLog.
//
// Three numbers are NOT content and are said so where they are declared: the logs a pack holds
// between bank trips, the seconds a bank visit and a make menu cost, and walking at one tile a
// tick. They move every camp's demand the same way, so they barely move which camp the board
// prefers; they are there so a far camp costs its walk.

import (
	"math"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/woodcutplan"
)

// ChopRollTicks is how many ticks apart woodcut.rs2 rolls get_logs for one account.
const ChopRollTicks = 4

// FletchTicksPerLog is how many ticks cut_logs.rs2 spends on each bow of a batch.
const FletchTicksPerLog = 2

// Not content: the account's own round trip.
const (
	// PackLogs is how many logs a load holds: 28 slots, less the knife and the axe.
	PackLogs = 26
	// BankVisit is the whole bank stop: open the booth, deposit, read, close. An estimate.
	BankVisit = 10 * time.Second
	// FletchMenu is one knife-on-log use and its make menu, before the first bow. An estimate.
	FletchMenu = 3 * time.Second
	// WalkTicksPerTile is walking pace. Running is twice as fast, but the live maple cohort
	// stood at 11% run energy (2026-09-25 17:20 snapshot), so walking is what it actually does.
	WalkTicksPerTile = 1
)

// treeChance is trees.dbrow's `data=successchance,<axe>,<low>,<high>` for the tables the boards
// price (the maple rung and its willow overflow, the yew rung and the magic rung), keyed by
// woodcutplan tier label and axe name.
var treeChance = map[string]map[string][2]int{
	"Willow": { // [willow_tree_table]
		"Bronze axe": {16, 50}, "Iron axe": {24, 75}, "Steel axe": {32, 100}, "Black axe": {36, 112},
		"Mithril axe": {40, 125}, "Adamant axe": {48, 150}, woodcutplan.RuneAxe: {56, 175},
	},
	"Maple": { // [maple_tree_table]
		"Bronze axe": {8, 25}, "Iron axe": {12, 37}, "Steel axe": {16, 50}, "Black axe": {18, 56},
		"Mithril axe": {20, 62}, "Adamant axe": {24, 75}, woodcutplan.RuneAxe: {28, 87},
	},
	"Yew": { // [yew_tree_table]
		"Bronze axe": {4, 12}, "Iron axe": {6, 19}, "Steel axe": {8, 25}, "Black axe": {9, 28},
		"Mithril axe": {10, 31}, "Adamant axe": {12, 37}, woodcutplan.RuneAxe: {14, 44},
	},
	"Magic": { // [magic_tree_table]; the black and mithril axes share the low 5
		"Bronze axe": {2, 6}, "Iron axe": {3, 9}, "Steel axe": {4, 12}, "Black axe": {5, 13},
		"Mithril axe": {5, 15}, "Adamant axe": {6, 18}, woodcutplan.RuneAxe: {7, 21},
	},
}

// SwingChance is the chance one get_logs roll pays a log: the engine's STAT_RANDOM over the tree
// table's pair for this axe. Zero for a tier or axe the table does not hold.
func SwingChance(tierLabel string, woodLevel int, axe string) float64 {
	pairs, ok := treeChance[tierLabel]
	if !ok {
		return 0
	}
	var pair [2]int
	found := false
	for name, p := range pairs {
		if strings.EqualFold(name, strings.TrimSpace(axe)) {
			pair, found = p, true
			break
		}
	}
	if !found {
		return 0
	}
	l := woodLevel
	if l > 99 {
		l = 99
	}
	if l < 1 {
		l = 1
	}
	v := pair[0]*(99-l)/98 + pair[1]*(l-1)/98 + 1
	if v > 256 {
		v = 256
	}
	return float64(v) / 256
}

// Chopper is one account as the model sees it.
type Chopper struct {
	Wood, Fletch int
	Axe          string
}

// ReferenceChopper is the account the board prices camps for: the middle of the live maple
// cohort (Woodcutting 61-74, Fletching 50-63 on 2026-09-25) with the rune axe every one of them
// is configured to buy. Pricing with one reference rather than each account's own levels keeps
// the board a pure function of who is seated where; which camp is better barely depends on it.
var ReferenceChopper = Chopper{Wood: 65, Fletch: 55, Axe: woodcutplan.RuneAxe}

// BoardPlayers is the world population the board scales respawns by (woodcutplan's
// scale_by_playercount): the live world held 263 of this fleet's accounts at the 2026-09-25
// snapshot. It shortens every respawn by the same 6.6%, so it moves no camp against another.
const BoardPlayers = 270

// XPPerLog is Woodcutting plus Fletching experience for one log of rung idx, cut into what
// Choose picks at this Fletching level under the shafts policy (maple and willow logs have no
// shaft, so that is the best bow the level allows).
func XPPerLog(idx, fletchLevel int) float64 {
	xp := Ladder[idx].Tier.XP
	if p, ok := Choose(idx, fletchLevel, NormalShafts); ok {
		xp += p.XP() // Product.XP10 is per log, whatever the log becomes
	}
	return xp
}

// AccountLogsPerHour is what one account takes from a camp when no other account is in its
// way: a pack of PackLogs chopped, cut (when fletching), carried steps tiles to the Seers bank
// and back, and banked.
func AccountLogsPerHour(tierLabel string, c Chopper, steps int, fletching bool) float64 {
	p := SwingChance(tierLabel, c.Wood, c.Axe)
	if p <= 0 {
		return 0
	}
	tick := woodcutplan.ProductionTick.Seconds()
	chop := float64(PackLogs) * ChopRollTicks * tick / p
	cut := 0.0
	if fletching {
		cut = FletchMenu.Seconds() + float64(PackLogs*FletchTicksPerLog)*tick
	}
	walk := float64(2*steps*WalkTicksPerTile) * tick
	cycle := chop + cut + walk + BankVisit.Seconds()
	return float64(PackLogs) * 3600 / cycle
}

// CampPrice is one camp's economics for one kind of account.
type CampPrice struct {
	// Supply is the logs an hour the camp's trees give, however many accounts chop them.
	Supply float64
	// Account is the logs an hour one uncontended account takes, round trip included.
	Account float64
	// XPPerLog is Woodcutting plus Fletching experience per log.
	XPPerLog float64
}

// PriceCamp prices c for chopper ch at a world of players.
func PriceCamp(c Camp, ch Chopper, players int) CampPrice {
	tier := Ladder[c.Rung].Tier
	_, fletches := Choose(c.Rung, ch.Fletch, NormalShafts)
	return CampPrice{
		Supply:   woodcutplan.CampSupply(c.Trees, tier.Label, woodcutplan.ProductionTick, players) * 60,
		Account:  AccountLogsPerHour(tier.Label, ch, c.Steps, fletches),
		XPPerLog: XPPerLog(c.Rung, ch.Fletch),
	}
}

// Throughput is the logs an hour a camp gives when accounts wanting `demand` logs an hour between
// them stand at it: S(1 - e^(-D/S)). Near D for a few accounts, near S for a crowd, and never
// more than either. The smoothing is a model choice, not content; it is what makes the tenth
// account at four maples worth almost nothing, which is the whole finding this file acts on.
func Throughput(supply, demand float64) float64 {
	if supply <= 0 || demand <= 0 {
		return 0
	}
	return supply * (1 - math.Exp(-demand/supply))
}

// Share is one account's experience an hour at a camp with n accounts seated there, itself
// included: the camp's throughput split evenly, times the experience per log.
func (p CampPrice) Share(n int) float64 {
	if n < 1 {
		n = 1
	}
	return p.XPPerLog * Throughput(p.Supply, float64(n)*p.Account) / float64(n)
}

// Capacity is how many accounts the camp feeds at full speed: its supply over what one account
// alone takes (woodcutplan.CampAccounts' rule), at least one. Past it every seat takes logs from
// the others; the board still seats there when nowhere else pays better, and says so.
func (p CampPrice) Capacity() int {
	if p.Account <= 0 || p.Supply <= 0 {
		return 1
	}
	n := int(p.Supply / p.Account)
	if n < 1 {
		n = 1
	}
	return n
}

// CampXP is the whole camp's experience an hour with n accounts seated there.
func (p CampPrice) CampXP(n int) float64 {
	if n < 1 {
		return 0
	}
	return p.Share(n) * float64(n)
}

// CohortRow is one camp's line in a CohortXP pricing.
type CohortRow struct {
	Camp     string
	Accounts int
	// Supply and Used are logs an hour: what the trees can give and what the seated accounts take.
	Supply, Used float64
	// XP is the camp's Woodcutting plus Fletching experience an hour.
	XP float64
}

// CohortXP prices an arrangement with each account's own levels and axe: accounts[i] chops at
// camps[at[i]]. It is what the doc comment's option table is computed with, so those numbers
// and the board's choices come from one model.
func CohortXP(camps []Camp, accounts []Chopper, at []int, players int) (float64, []CohortRow) {
	demand := make([]float64, len(camps))
	for i, a := range accounts {
		c := camps[at[i]]
		_, fletches := Choose(c.Rung, a.Fletch, NormalShafts)
		demand[at[i]] += AccountLogsPerHour(Ladder[c.Rung].Tier.Label, a, c.Steps, fletches)
	}
	rows := make([]CohortRow, len(camps))
	total := 0.0
	for ci, c := range camps {
		supply := woodcutplan.CampSupply(c.Trees, Ladder[c.Rung].Tier.Label, woodcutplan.ProductionTick, players) * 60
		used := Throughput(supply, demand[ci])
		rows[ci] = CohortRow{Camp: c.Name, Supply: supply, Used: used}
	}
	for i, a := range accounts {
		ci := at[i]
		c := camps[ci]
		if demand[ci] <= 0 {
			rows[ci].Accounts++
			continue
		}
		_, fletches := Choose(c.Rung, a.Fletch, NormalShafts)
		d := AccountLogsPerHour(Ladder[c.Rung].Tier.Label, a, c.Steps, fletches)
		xp := rows[ci].Used * d / demand[ci] * XPPerLog(c.Rung, a.Fletch)
		rows[ci].Accounts++
		rows[ci].XP += xp
		total += xp
	}
	return total, rows
}
