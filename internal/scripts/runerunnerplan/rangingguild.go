package runerunnerplan

import "fmt"

// The Ranging Guild's bow shop on the circuit, and the Ranged level that opens it.
//
// a user request: "we also should get acct 40 ranged for the ranged guild shop asap" and "all our arrowtips will accumilate from
// shop purchasing which we need to optimise and have rangeguild shop unlocked asap for more arrowtip purchases per hour".
//
// READ FROM CONTENT 289 (minigames/game_ranging/scripts/ranging_guild_door.rs2: the door reads stat(ranged) >= 40; the shop is
// ranging_guild_bowshop, npc ranging_guild_bow_salesman 683 at 2673,3434; the stock is Dargaud in ammo.go):
//
//   - Arrow shaft 1,000 (one back every 50 ticks: 72 an hour a world), Steel arrowtips 300 (30 ticks: 120 an hour), Mithril 200 (40: 90),
//     Adamant 200 (50: 72) and Rune 150 (60: 60). Hickton's own tips regrow at 90, 90, 90 and 90 an hour, so the guild roughly doubles the
//     arrowtips a world yields, and it is the ONLY source of Arrow shafts.
//   - It is 136 tiles west of the Catherby bank, so a visit is a walk of about two minutes each way: placed once per world, after the
//     Hickton stop of that world (three a round, one for each world's shelf).

const (
	// RangingGuildRanged is the Ranged level of the guild's door.
	RangingGuildRanged = 40
	// DargaudMinVisitCoins is the smallest purchase the guild stop is made for: its shelf is worth ten times that and the walk is four
	// minutes, so a visit that finds the shelf mostly bought is skipped.
	DargaudMinVisitCoins = 3000
	// DefaultRangingGuildShop is the rangingGuildShop setting's default: on (the stop is placed only once Ranged reaches 40).
	DefaultRangingGuildShop = true
	// DefaultRangedTo40 is the rangedTo40 setting's default: on.
	DefaultRangedTo40 = true
)

// RangingGuildSites is the placed ammo sites with the Ranging Guild's shop in it exactly when the shop is wanted, Hickton is placed (the
// guild stop follows each Hickton stop) and the account's CURRENT Ranged level opens the door. A listed Dargaud the level does not open
// is taken out, and one the setting asks for is added.
func RangingGuildSites(placed []string, ranged int, on bool) []string {
	open := on && ranged >= RangingGuildRanged && enabled(placed, SiteHickton)
	out := make([]string, 0, len(placed)+1)
	for _, k := range placed {
		if k == SiteDargaud {
			continue
		}
		out = append(out, k)
	}
	if open {
		out = append(out, SiteDargaud)
	}
	return out
}

// RangedTrainingKeys are the ScarabSwarm settings that run its cannon-free Ranged stint (scarabswarm_bowrun.go), with the values
// RuneRunner gives them while it trains.
func RangedTrainingKeys() map[string]interface{} {
	return map[string]interface{}{
		"endgameTrainRanged": true, "endgameRangedRun": true, "endgameRangedBow": true,
		"endgameRangedGoal": float64(RangingGuildRanged), "endgameRangedCannonGoal": float64(RangingGuildRanged),
		// Rest minutes between stints, as the stint's own default is an hour: the account has rounds to make.
		"endgameRangedEveryMinutes": float64(20),
	}
}

// WithRangedTraining is the ScarabSwarm settings with the Ranged stint switched on while the account's Ranged is under the guild's
// door and training is wanted: only keys the settings object did not write, so an operator's own value always stands. The second result
// is the keys it added (for the log), nil when it changed nothing.
func WithRangedTraining(m map[string]interface{}, ranged int, on bool) (map[string]interface{}, []string) {
	if !on || ranged <= 0 || ranged >= RangingGuildRanged {
		return m, nil
	}
	out := make(map[string]interface{}, len(m)+6)
	for k, v := range m {
		out[k] = v
	}
	var added []string
	for k, v := range RangedTrainingKeys() {
		if _, ok := out[k]; !ok {
			out[k] = v
			added = append(added, k)
		}
	}
	return out, added
}

// DargaudWhy says why the guild stop is skipped at this Ranged level, "" when it is not.
func DargaudWhy(ranged int) string {
	if ranged < RangingGuildRanged {
		return fmt.Sprintf("the Ranging Guild door needs Ranged %d (this account has %d)", RangingGuildRanged, ranged)
	}
	return ""
}
