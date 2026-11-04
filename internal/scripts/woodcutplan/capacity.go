package woodcutplan

// How many accounts one tree camp can actually feed, from the content's own respawn rules.
//
// This exists because a 100-account crowd run at the Seers normal-tree camp measured 6
// Woodcutting xp per account per minute against 309 for one account alone, with 22 of 100
// gaining nothing at all and 635 "no tree in sight" lines (COLLAB_ARCHIVE.md, agent "woodkill",
// 2026-09-20). That run could say 100 is far too many and nothing else, and it was taken on a
// saturated lab engine, so part of the collapse was the lab rather than the game. Everything
// here is read out of content instead, so it does not depend on how loaded any engine was.
//
// THE MECHANISM, from content/scripts/skill_woodcutting/scripts/woodcut.rs2 and its own
// configs/trees.dbrow:
//
//	def_int $respawnrate = db_getfield($data, woodcutting_trees:respawnrate, 0);
//	def_int $deplete_chance = 8;              // 1/8 chance to deplete
//	if ($respawnrate = 0) {                   // the NORMAL tree, and only it
//	    $respawnrate = add(120, random(80));  // 120-199 ticks
//	    $deplete_chance = 0;                  // random(0) = 0, so it ALWAYS falls
//	}
//	...
//	if (random($deplete_chance) = 0) {
//	    $respawnrate = ~scale_by_playercount($respawnrate);
//	    loc_change(loc_param(next_loc_stage), $respawnrate);
//
// So the two tiers a cohort starts on behave completely differently, and the difference is the
// whole story:
//
//   - A NORMAL tree gives exactly ONE log and is then a stump for 120-199 ticks (mean 159.5).
//     At a production 600ms tick that is about 96 SECONDS of nothing, per tree, per log, and it
//     is winner-takes-all: the first account to succeed fells it out from under everyone else.
//   - An OAK is a 1-in-8 deplete, so it yields 8 logs on average and its stump lasts 30 ticks
//     (18s). Several accounts can chop the same standing oak at once. Tree for tree, an oak is
//     about 43x the supply of a normal tree.
//
// scale_by_playercount is base*(4000-playercount)/4000, so a busy world respawns slightly
// FASTER: 2.5% at 100 players, 6.7% at 269. It is modelled here for honesty and must not be
// read as a mitigation.
//
// Nothing here reads a session. The caller supplies how many trees the camp actually holds
// (cmd/woodcrowdtest counts them from the loc index) and what one uncontended account achieves.

import "time"

// TreeSupply is one tier's respawn behaviour, read from trees.dbrow and woodcut.rs2.
type TreeSupply struct {
	// RespawnTicks is the mean stump time in game ticks, before scale_by_playercount.
	RespawnTicks float64
	// LogsPerFall is how many logs a tree yields on average before it falls: 1/deplete_chance,
	// except the normal tree, whose deplete chance the script forces to certainty.
	LogsPerFall float64
}

// TierSupply is the supply rule per tier Label. Keyed by label rather than by index so a
// reordered Tiers table cannot silently move a tier onto the wrong respawn rule.
var TierSupply = map[string]TreeSupply{
	// 120 + random(80) ticks => mean 159.5; deplete_chance forced to 0 => it always falls.
	"Normal tree": {RespawnTicks: 159.5, LogsPerFall: 1},
	"Oak":         {RespawnTicks: 30, LogsPerFall: 8},
	"Willow":      {RespawnTicks: 30, LogsPerFall: 8},
	"Maple":       {RespawnTicks: 120, LogsPerFall: 8},
	"Yew":         {RespawnTicks: 200, LogsPerFall: 8},
	"Magic":       {RespawnTicks: 400, LogsPerFall: 8},
	// The Draynor Manor start camp's dead trees are in the SAME
	// skill_woodcutting/configs/trees/normal.loc as ordinary trees, so they run on the normal
	// tree's rule: one log, and a 120-199 tick stump. Dense, but not richer per tree. Keyed on
	// DraynorTier().Label so a census can include the start camp.
	"Dead tree (Draynor Manor)": {RespawnTicks: 159.5, LogsPerFall: 1},
}

// ProductionTick is the live server's tick. The lab engine runs 200ms (3x), which is why every
// rate measured there has to be divided by three before it says anything about the fleet.
const ProductionTick = 600 * time.Millisecond

// CampSupply is how many logs per minute a camp of `trees` trees of this tier can sustain,
// summed over every account standing in it, at a given tick and world population.
//
// Zero when the tier is unknown or there are no trees, which a caller must not read as
// "unlimited".
func CampSupply(trees int, tierLabel string, tick time.Duration, playerCount int) float64 {
	s, ok := TierSupply[tierLabel]
	if !ok || trees <= 0 || tick <= 0 || s.RespawnTicks <= 0 {
		return 0
	}
	cycleMinutes := s.RespawnTicks * PlayerCountScale(playerCount) * tick.Minutes()
	if cycleMinutes <= 0 {
		return 0
	}
	return float64(trees) * s.LogsPerFall / cycleMinutes
}

// PlayerCountScale is scale_by_playercount's own factor: (4000 - min(players, 2000)) / 4000.
// It never falls below 0.5, so a busy world can shorten a respawn by at most half.
func PlayerCountScale(playerCount int) float64 {
	if playerCount < 0 {
		playerCount = 0
	}
	if playerCount > 2000 {
		playerCount = 2000
	}
	return float64(4000-playerCount) / 4000
}

// CampAccounts is how many accounts a camp can feed at full speed: its sustainable supply
// divided by what one uncontended account consumes.
//
// This is a bound on a GOOD outcome, not a limit on how many can stand there. Put more in and
// nobody is stopped: they share a fixed supply, which is the 6 xp/min collapse the 100-account
// run measured. Returns 0 when either input is unknown.
func CampAccounts(supplyLogsPerMin, accountLogsPerMin float64) float64 {
	if supplyLogsPerMin <= 0 || accountLogsPerMin <= 0 {
		return 0
	}
	return supplyLogsPerMin / accountLogsPerMin
}

// LogsPerMinute converts an xp-per-minute reading to logs per minute for a tier.
func LogsPerMinute(xpPerMin float64, tier Tier) float64 {
	if tier.XP <= 0 {
		return 0
	}
	return xpPerMin / tier.XP
}
