package webwalk

// The Fremennik basalt rocks: ten stepping stones in the sea that join the Barbarian Outpost shore (2522,3594) to the
// Lighthouse shore (2514,3620), whose north coast runs east to Rellekka over the Broken bridge (2596..2599,3608). For
// an account that rellekkawolves.go keeps off the overland road they are the only way between the mainland and
// Rellekka, so a trip to Olaf's trial (2738,3638) crosses them twice. Read from content
// (quests/quest_horror/scripts/basalt_rocks.rs2 and configs/quest_horror.loc, branch 289) on 14 October 2026, after
// two ScarabSwarm accounts spent hours on the north shore at 2515,3619:
//
//   - Ten locs, horror_jumping_spot1..10 = loc 4550..4559, all "Jump-across" (oploc1), blockwalk=no. The script has
//     no quest, level or membership check: any account that can reach one may jump. The graph's "Agility 35" on
//     all ten was never content's.
//   - Each rock carries the jumper from its own tile ($start) over the water to a landing ($final). These are
//     exactly the ten edges graph.json has; their tiles and loc ids were checked against the script and none is wrong:
//     northbound  1 2522,3595 -> 2522,3598   3 2522,3600 -> 2522,3603   5 2518,3611 -> 2515,3611
//     7 2514,3613 -> 2514,3616   9 2514,3617 -> 2514,3620
//     southbound  10 2514,3619 -> 2514,3616  8 2514,3615 -> 2514,3612   6 2516,3611 -> 2518,3610
//     4 2522,3602 -> 2522,3599   2 2522,3597 -> 2522,3594
//     A trip is five jumps, with the causeway islands between them walked.
//   - Exactly two rocks roll dice, each in one direction only: spot 3 (2522,3600) going north and spot 8
//     (2514,3615) going south run `stat_random(agility, 5, 255)` first. A miss is "You slip on the slimy
//     causeway": the jumper is swept BACK THE WAY IT CAME ("The tide sweeps you back to shore": 5 south from spot 3 to
//     2521,3595, 4 north from spot 8 to 2515,3619), takes random_range(1, 10) damage and gains 5 agility experience.
//     The roll is STAT_RANDOM's: 6/256 at level 1, 92/256 at 35, 105/256 at 40, 130/256 at 50, 155/256 at 60 and a
//     certainty at 99 (Slip.Chance). The other eight rocks have no roll and cannot fail.
//   - Hitpoints come back at one a minute (timer health_regen, 100 ticks), so a streak of misses costs minutes, not
//     seconds: an account at 5/44 hitpoints needs seven minutes before it may risk the next slip.
//
// What the live fleet did with that (run/logs/fleet-20261014-093639, 09:36-17:35): 25 accounts missed at least one of
// the two dice rocks, 40 of the 67 jumps at spot 8 and 62 of the 104 at spot 3 (about 40% success, a level 38-40
// account's odds), and:
//
//   - A miss at spot 8 puts an account back on the NORTH shore, and noteRefusal banned the rock for 30 minutes after
//     four misses "even if nothing else reaches" the destination. The strike count survived the ban, so each attempt
//     after it was one more miss over the line and banned the rock again: one try in 30 minutes at about 40%. Four
//     accounts were banned that way; the two worst stood at 2515,3619 for about two hours (11:41 and 12:16 to 13:42),
//     every walk home answering "unreachable (best 336 tiles)". Neither had a law rune, and the wolf road was shut
//     to their combat level, so the rocks were the whole way home.
//   - The guard that refuses a hurting shortcut at 11 hitpoints or under counted as a refusal too, and the graph's
//     Agility 35 sat on all ten rocks, so spot 10 (2514,3619), which cannot slip, was refused at 5/44 hitpoints and
//     then banned for 30 minutes as well.
//   - The gate on the southbound rocks meant an account that reached the north shore any other way, or before its
//     stats had synced (live.WorldState reads a skill the server has not sent as 0, guildDoorWhy), had no route home
//     at all, where content lets it try at any level.
//
// So the graph is corrected, not the walker alone (basaltCorrections): the northbound dice rock keeps the Agility 35
// gate that stops an account going over at hopeless odds, every other rock is members-only, and the two dice rocks
// carry their Slip, which is what lets the walker keep the way home in the plans and rest until a slip is survivable
// (Walker.restForSlip, noteRefusal) instead of banning it.

// slipShortcutPenalty prices a crossing that can slip (Slip), on top of its cost of 10. Before the rocks were priced
// one by one every rock carried hurtingShortcutPenalty, 160 apiece, so a trip over them cost 5 x 160 = 800 and the
// planner took the road unless the rocks saved more than that. A trip still passes five rocks of which exactly one
// can slip, so that one carries what the five did together: 4 x 10 + 760 = 800, and no walk that chose the rocks or
// the road before chooses the other now.
const slipShortcutPenalty = 750

// basaltRocks are the ten rocks' placements, spot 1 to 10 (loc id 4549 + spot).
var basaltRocks = [10][2]int{
	{2522, 3595}, {2522, 3597}, {2522, 3600}, {2522, 3602}, {2518, 3611},
	{2516, 3611}, {2514, 3613}, {2514, 3615}, {2514, 3617}, {2514, 3619},
}

// basaltSlips are the two rocks that roll dice, by loc id: spot 3 (northbound; the tide sweeps the jumper back to the
// Barbarian Outpost shore) and spot 8 (southbound; it sweeps the jumper back to the Lighthouse shore).
var basaltSlips = map[int]Slip{
	4552: {Skill: "agility", Low: 5, High: 255, MaxHit: 10, Lands: Point{X: 2521, Z: 3595}},
	4557: {Skill: "agility", Low: 5, High: 255, MaxHit: 10, Lands: Point{X: 2515, Z: 3619}},
}

// basaltWhy is every basalt correction's reason.
const basaltWhy = "Fremennik basalt rocks (quest_horror/scripts/basalt_rocks.rs2): only spot 3 (loc 4552, northbound) and " +
	"spot 8 (loc 4557, southbound) roll stat_random(agility, 5, 255) and sweep a miss back the way it came for 1-10 " +
	"damage; nothing else is gated, so the graph's Agility 35 stays on the northbound dice rock alone and the way " +
	"home is members-only. See basalt.go"

// basaltCorrections are the ten rocks' corrections: the Slip on the two dice rocks and the skill gate off every rock
// but the northbound one.
func basaltCorrections() []Correction {
	out := make([]Correction, 0, len(basaltRocks))
	for i, at := range basaltRocks {
		id := 4550 + i
		c := Correction{LocID: id, LocX: at[0], LocZ: at[1], Level: 0, Action: "Jump-across", ClearSkills: id != 4552, Why: basaltWhy}
		if s, ok := basaltSlips[id]; ok {
			c.SetSlip = &s
		}
		out = append(out, c)
	}
	return out
}

func init() {
	EdgeCorrections = append(EdgeCorrections, basaltCorrections()...)
}
