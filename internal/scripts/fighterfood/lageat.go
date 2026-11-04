package fighterfood

import "math"

// EATING THROUGH A LAG (lageat-1018, 18 Oct 2026).
//
// Every eat rule reads the hitpoints the client last saw. When the view goes silent or replays a backlog (gamestate.ViewLag) those
// hitpoints are up to a dozen ticks old, and what hits the account in them is not in them. 18 Oct 04:41:36 to 04:41:54, a
// ScarabSwarm account thieving Paladins in Ardougne Castle stood at 27 of 54 hitpoints, 50%, over the thieving rule's 40% line:
// no bite was due. Its view then read nothing for seven seconds and fifteen ticks at once. A steal had raised a Watchman (content
// macro_watchman5, the event for combat 61 to 90: strength 100, so hits of up to 11 every four ticks) and the readings when the view
// came back were 27, 16, 7 and 0. A margin that covers what a lag window can hide would have had it at 38 (70%) before the silence
// began, and it would have lived through all three hits.
//
// So an eat line that is judged against a view that has lagged recently is raised to what the worst recent episode could have
// delivered: every NPC that can hit the account, at its worst single hit, for as many volleys as the episode had room for (an NPC
// hits at most once in LagAttackTicks, and an episode that hid H ticks held ceil(H/LagAttackTicks) volleys), up to LagCapPercent of
// the maximum hitpoints. A line is only ever raised, never lowered, and an account whose view has not lagged in
// gamestate.ViewLagMemory keeps every line it had.

const (
	// LagAttackTicks is the fewest ticks between two hits of one NPC: content's attackrate default (skill_combat/configs/combat.param,
	// 4; 70 configs of 175 are 6, 49 are 4 and 38 are 5). A faster NPC would only make the margin too small by a volley.
	LagAttackTicks = 4
	// LagCapPercent is the most of the maximum hitpoints a lag raises a line to ("toward about 70%", the owner's ask): above it an
	// account spends its life eating, and a pack of food a trip carries is gone in a few minutes.
	LagCapPercent = 70
	// LagBadTicks is the size of an episode that counts as bad lag: gamestate.ViewCatchUpTicks, ten ticks, a silence of six seconds
	// or a backlog two seconds ahead of the clock. Smaller episodes still raise the lines (by fewer volleys); a bad one is what an
	// account at a thieving spot also steps off and tops up for (bot.stage3LagHold). A test pins it to the gamestate constant.
	LagBadTicks = 10
	// LagUnseenHit is the worst single hit assumed for something that is hitting the account and cannot be named (nothing faces
	// it, or its type has no entry): scarabplan.DefaultHunterMaxHit, more than any of the pens' hunters but the fire giants.
	LagUnseenHit = 8
)

// LevelMaxHit is the most one melee hit of an NPC of this combat level deals, estimated from the level alone for an NPC whose strength
// the client cache does not carry: a strength of five sixths of the level, no strength bonus, content's formula
// ((strength + 9) * 64 + 320) / 640. It reproduces the six tiers of content's macro_watchman exactly (levels 14, 29, 49, 79, 120 and 159
// have strengths 9, 22, 38, 65, 100 and 130: hits of 2, 3, 5, 7, 11 and 14). 0 for an unknown level.
func LevelMaxHit(level int) int {
	if level <= 0 {
		return 0
	}
	return ((level*5/6+9)*64 + 320) / 640
}

// LagVolleys is how many hits one NPC can land in an episode that hid hiddenTicks ticks: one every LagAttackTicks, the first at
// once. 0 for no lag.
func LagVolleys(hiddenTicks int) int {
	if hiddenTicks <= 0 {
		return 0
	}
	return (hiddenTicks + LagAttackTicks - 1) / LagAttackTicks
}

// LagCap is LagCapPercent of the maximum hitpoints, rounded up: the highest line a lag raises to.
func LagCap(maxHP int) int {
	if maxHP <= 0 {
		return 0
	}
	return (maxHP*LagCapPercent + 99) / 100
}

// LagEatLine is the hitpoints at or under which an account eats when its view has recently hidden hiddenTicks ticks. base is the
// line it eats at on a current view, hitSum the sum of the worst single hit of every NPC that can hit it (the attackers facing it,
// plus at a thieving spot what a steal can raise), maxHP its maximum. The result is base, or what hitSum for LagVolleys(hiddenTicks)
// volleys would take, up to LagCap(maxHP): never below base, and exactly base when there is no lag, no maximum or nothing that can
// hit it.
func LagEatLine(maxHP, base, hitSum, hiddenTicks int) int {
	if maxHP <= 0 || hiddenTicks <= 0 || hitSum <= 0 {
		return base
	}
	return max(base, min(hitSum*LagVolleys(hiddenTicks), LagCap(maxHP)))
}

// LagEatFrac is LagEatLine for the eaters that keep a share of the maximum (EatInput.ThresholdFrac: a bite when hp/maxHP < frac).
// The base share is returned untouched unless the lag raises the line above it.
func LagEatFrac(maxHP int, base float64, hitSum, hiddenTicks int) float64 {
	if maxHP <= 0 || hiddenTicks <= 0 || hitSum <= 0 {
		return base
	}
	// hp/maxHP < base is hp <= baseLine.
	baseLine := int(math.Ceil(base*float64(maxHP)-1e-9)) - 1
	line := LagEatLine(maxHP, baseLine, hitSum, hiddenTicks)
	if line <= baseLine {
		return base
	}
	return float64(line+1) / float64(maxHP)
}
