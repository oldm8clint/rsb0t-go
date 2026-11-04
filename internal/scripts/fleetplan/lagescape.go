package fleetplan

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fighterfood"
)

// LEAVING INSTEAD OF OUT-EATING IT (lagescape-1018, 18 Oct 2026).
//
// 15:14:33, a Stage 5 magic-half account (53 hitpoints, sixteen Bass left) died at the Feldip ogres (east) pen, three steps from the
// safespot it had been walking to. From 15:13:30 the log holds one thing for it: "bite: the bite of Bass has not shown after 6s at 14/53
// hitpoints ... sending another", every six or seven seconds, at 14, 7, 6, 6, 6, 6, 7, 7, 7 hitpoints. Two hunters held the tile it
// stood on (the Ogre chieftain that spawns at 2615,2987, whose chase box covers z 2981 to 2993, and the Ogre at 2608,2980; a hit of up to 9
// and one of up to 7 every four ticks, sixteen a volley), the server was running its bites five to seven seconds late, and each bite that
// landed healed about what the next volley took: a treadmill at 6 or 7 hitpoints that one volley of 16 ended. Every layer that could have
// ended it was eating, because every layer's answer to "hurt and being hit" is a bite:
//
//   - survivalTick eats (survivalLeave is for an account with no food, and not on a lagging view);
//   - Phase3Eat and the lag hold's eat (stage3BiteHeld) bite in place, and returnToSpot bites BEFORE it takes a step to the nearest
//     ground no hunter holds (three steps from where it died: 2613,2982, 2614,2981, 2615,2980);
//   - the guardian never moves a Stage 5 account that is being hit (Guardian.CombatExpected is on for the whole run, which makes every
//     hit an expected cost), and its cornered rules are for a flee that found nowhere to go;
//   - the thief's step-off (lageat-1018) needs ten hidden ticks of lag, and is the thieving spot's.
//
// The rule here is the one thing they all lacked: how many hits can land before a bite sent now can. A bite takes as long to land as the
// slowest of the view's recent lag and the bites' own measured delay (the bite ledger's evidence); every hitter lands one hit in four ticks;
// so an account whose hitpoints are under what the hitters can deal in that time is not going to out-eat it, whatever its pack holds.
// It leaves: by a teleport it can pay for, else by a walk out of the hitters' reach (the pen exit rules), eating on the way, and it eats in
// place only once it is out of reach. This file is the pure decision; scarabrunner.LagEscape is the I/O.

const (
	// EscapeCapPercent bounds the line: above half the hitpoints an account eats (EatLine, up to 60%; LagEatLine, up to 70%) and nothing the
	// hitters could land in a lag window is worth a teleport and a walk back. Half is where the 18 Oct 04:41 thief stood when the Watchman
	// event took it (27 of 54) and where the 15:14 account was a long way under (14 of 53), and it keeps a teleport out of the fleet's
	// ordinary hurt: a thief at 60% after a failed steal is eating, not leaving.
	EscapeCapPercent = EatBasePercent
	// EscapeUnseenFloorPercent is the least line when something is hitting the account that nothing names (nothing faces it and nothing
	// attackable stands near it): "under about 30%", the owner's number, because an unnamed hitter is assumed hard (fighterfood.LagUnseenHit).
	EscapeUnseenFloorPercent = 30
	// EscapeBiteFresh is how recent the bite ledger's evidence of a slow route must be to count (actions.LastBiteLag).
	EscapeBiteFresh = 2 * time.Minute
	// EscapeMinHorizonTicks is the fewest ticks of hits the line covers: two of the fastest attackers' gaps, so with no lag evidence of a
	// size, "under two volleys" is the line.
	EscapeMinHorizonTicks = 2 * fighterfood.LagAttackTicks

	escapeTick = 600 * time.Millisecond
)

// EscapeHorizonTicks is how many ticks of hits an account that bites now has to ride out: the longest of EscapeMinHorizonTicks, the worst
// view lag episode of the last three minutes (gamestate.RecentLag.Ticks) and the longest bite delay the ledger saw (a bite that took six
// seconds is ten ticks of hits).
func EscapeHorizonTicks(lagTicks int, biteDelay time.Duration) int {
	h := max(EscapeMinHorizonTicks, lagTicks)
	if biteDelay > 0 {
		h = max(h, int((biteDelay+escapeTick-1)/escapeTick))
	}
	return h
}

// EscapeLine is the hitpoints at or under which an account that is being hit and whose bites run late leaves rather than eats: what its
// hitters can deal in horizonTicks (the sum of their worst single hits times the volleys one NPC lands in that time,
// fighterfood.LagVolleys), at least EscapeUnseenFloorPercent of the maximum when one of them is unnamed (unseen), and at most
// EscapeCapPercent of it. 0 with nothing that can hit it, or no maximum.
func EscapeLine(maxHP int, hits []int, unseen bool, horizonTicks int) int {
	if maxHP <= 0 {
		return 0
	}
	line := SumHits(hits...) * fighterfood.LagVolleys(horizonTicks)
	if unseen {
		line = max(line, (maxHP*EscapeUnseenFloorPercent+99)/100)
	}
	return min(line, (maxHP*EscapeCapPercent+99)/100)
}

// EscapeInput is what DecideEscape reads. Hits are the worst single hit of everything that can hit the account where it stands: the NPCs
// facing it and, while it is being hit, the attackable ones near enough to join in (scarabrunner.ReachHits), with fighterfood.LagUnseenHit
// for a hit nothing explains (Unseen).
type EscapeInput struct {
	HP, MaxHP int
	// Known is whether the hitpoints have been read.
	Known bool
	Hits  []int
	// Unseen is whether Hits holds an assumed hitter that nothing named.
	Unseen bool
	// Threatened is whether the account is being hit: a hitmark in the last ten ticks, or an NPC facing it. A tile a hunter holds is not
	// enough on its own: nothing has hit the account yet, and the ordinary rules are for that.
	Threatened bool
	// LagTicks is gamestate.RecentLag.Ticks (0 for no lag episode in the last three minutes), ViewLagging whether the view is silent or
	// catching up now, BiteLate whether the bite ledger saw a bite run late or lost within EscapeBiteFresh, and BiteDelay how late.
	LagTicks    int
	ViewLagging bool
	BiteLate    bool
	BiteDelay   time.Duration
}

// EscapeVerdict is DecideEscape's answer: Go to leave now, the line it judged hitpoints against and the ticks that line covers, and why
// (or why not), for the log.
type EscapeVerdict struct {
	Go      bool
	Line    int
	Horizon int
	Why     string
}

// DecideEscape is the rule above, pure: leave when the account is being hit, its view or its bites are running late, and its hitpoints
// are at or under EscapeLine. With a current view, current bites, nothing hitting it, or hitpoints over the line, it does not: the
// ordinary eat rules are right for those.
func DecideEscape(in EscapeInput) EscapeVerdict {
	switch {
	case !in.Known || in.MaxHP <= 0 || in.HP <= 0:
		return EscapeVerdict{Why: "hitpoints unknown"}
	case !in.Threatened:
		return EscapeVerdict{Why: "nothing is hitting the account"}
	case in.LagTicks <= 0 && !in.ViewLagging && !in.BiteLate:
		return EscapeVerdict{Why: "the view and the bites are current"}
	}
	h := EscapeHorizonTicks(in.LagTicks, in.BiteDelay)
	line := EscapeLine(in.MaxHP, in.Hits, in.Unseen, h)
	if line <= 0 || in.HP > line {
		return EscapeVerdict{Line: line, Horizon: h, Why: fmt.Sprintf("hitpoints %d/%d are over the escape line %d", in.HP, in.MaxHP, line)}
	}
	named := "named"
	if in.Unseen {
		named = "some unnamed"
	}
	return EscapeVerdict{Go: true, Line: line, Horizon: h, Why: fmt.Sprintf(
		"hitpoints %d/%d are at or under %d: %s hitters for up to %d a volley, %d volleys in the %d ticks a bite or a view runs late",
		in.HP, in.MaxHP, line, named, SumHits(in.Hits...), fighterfood.LagVolleys(h), h)}
}
