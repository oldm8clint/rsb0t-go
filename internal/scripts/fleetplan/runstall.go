package fleetplan

// The run-energy staller (a user request: "we want tick manipulation and run stalling utilized
// to its fullest potential for ultimate exp perhour and no downtime spent walking").
//
// Content 289 (general/scripts/enchanted_jewellry): op 4 on a Games necklace or a Ring of dueling,
// Rub, says "You rub the ...", runs p_delay(1), then asks where to teleport. Answering nothing, or
// "Nowhere", teleports nowhere and uses no charge. The engine drains no run energy on a tick the
// player is delayed (Player.updateEnergy), and OpHeldHandler clears moveClickRequest, so a running
// walk keeps moving through the delay, zone triggers included (a spell's OpHeldT does not clear it:
// walkalch.go, zonetrigger). The tick lab (claude/tickmanip-1005, 6 Oct 2026) measured a rub every 3
// ticks over 136 tiles: 120-122 tiles in 62 ticks against 122 without, and 0.125-0.139% run energy
// a tile against 0.351-0.393%, a 65% cut. A move click the server reads inside the delay is refused
// (MoveClickHandler), so a rub keeps clear of the walker's clicks the way a cast does.
//
// Pure: the caller reads the pack, the run toggle and the walk poll.

import (
	"strings"
	"time"
)

// RubItems are the names a rub works on, any charge: content's necklace_of_minigames objs
// ("Games necklace(8)" to "(1)") and category 136 ("Ring of dueling(8)" to "(1)").
var RubItems = []string{"Games necklace", "Ring of dueling"}

// IsRubItem reports whether an obj name is a rub item.
func IsRubItem(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range RubItems {
		if strings.HasPrefix(n, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// RubEveryTicks is the ticks from one rub to the next the server accepts: p_delay(1) holds the tick
// it is read in and the next, and the next op is read the tick after (PlayerOps P_DELAY:
// delayedUntil = tick+1+n).
const RubEveryTicks = 3

// WalkAlchClickSteps is how far along the route the walk's clicks aim while the account casts or rubs on the walk
// (webwalk.WalkOptions.ClickSteps; its default is 40): 100 tiles, so a 130-tile walk is one click and a 400-tile one
// four, not ten. Every click is a tick the stalling cannot use and the tiles before the next are tiles a cast cannot be
// sent on (WalkAlchAhead); on the lab engine's 135-tile route the walk's four clicks left 7 casts where 12 fit.
const WalkAlchClickSteps = 100

// The ticks one op holds the player for, which the engine does not drain run energy through (Player.updateEnergy): a
// cast's p_delay(3) holds the tick it is read in and the three after, a rub's p_delay(1) that tick and the next.
const (
	CastHeldTicks = 4
	RubHeldTicks  = 2
)

// HeldPercent is the share of a walk's ticks that casts and rubs held, as a whole percent, at most 100.
func HeldPercent(casts, rubs, ticks int) int {
	if ticks <= 0 {
		return 0
	}
	return min(100, 100*(CastHeldTicks*casts+RubHeldTicks*rubs)/ticks)
}

// PlainDrainPerTile is the run energy, in percent, one tile costs a running account carrying kg kilograms when
// nothing holds it: Player.updateEnergy takes 67 + 67*kg/64 hundredths of a percent a tick (kg between 0 and 64) and a
// run covers two tiles a tick. 0.385% at 10 kg.
func PlainDrainPerTile(kg int) float64 {
	kg = min(max(kg, 0), 64)
	return float64(67+67*kg/64) / 200
}

// RubAhead is the least distance (tiles) the walk's click target must still be ahead for a rub, so
// the walker's next click is not read inside the rub's delay: its re-click radius plus the tiles
// covered in a rub's ticks.
func RubAhead(running bool) int {
	per := 1
	if running {
		per = 2
	}
	return walkAlchArrive + per*RubEveryTicks
}

// RubPoll is one walk poll as the rub decision sees it.
type RubPoll struct {
	// SinceClick is how long ago the walk's last movement click went; SinceRub the last rub (a
	// large value when there was none).
	SinceClick, SinceRub time.Duration
	// Tick is the engine tick (600 ms when zero).
	Tick time.Duration
	// Ahead is tiles from the account to the click's target.
	Ahead int
	// Running is the run toggle: a walk drains nothing, so a rub would save nothing.
	Running bool
	// InCombat and Modal (a main or side interface: a bank, a shop; the rub's own menu is a chat
	// one) each stop a rub.
	InCombat, Modal bool
	// HP and MaxHP are the account's hitpoints (0 MaxHP when unread): below StallMinHPPercent the
	// rub waits, so a bite can land (StallHPLow).
	HP, MaxHP int
	// NoFood is the pack holding nothing to eat: there is no bite to make room for, so the hitpoints gate stands down
	// (StallStandAside). The zero value keeps the gate.
	NoFood bool
}

// StallMinHPPercent is the hitpoints, as a percent of the maximum, below which the run staller (the
// walk's casts and rubs) stands aside. A cast's p_delay(3) and a rub's p_delay(1) hold the player, and
// OpHeldHandler refuses an eat while it is delayed, so an account that was stalling on every third tick
// never got a bite in: live 6 Oct 2026, Shame logged "a bite of Kebab did not register" 15 times between
// 22:10 and 23:45 at 5-14 of 25-27 hitpoints, the walks beside it rubbing 19-23 times a leg, and died at
// 2580,3385. Without the staller the walk costs run energy and keeps the account alive.
const StallMinHPPercent = 65

// StallHPLow reports hitpoints under StallMinHPPercent of the maximum. An unread maximum, or a dead
// account (0 hitpoints, a stat packet not yet read), is not low: the gate only ever holds the staller back.
func StallHPLow(hp, maxHP int) bool {
	return maxHP > 0 && hp > 0 && hp*100 < maxHP*StallMinHPPercent
}

// ShouldRub decides one poll. The caller checks the pack holds a rub item.
func ShouldRub(p RubPoll) bool {
	tick := p.Tick
	if tick <= 0 {
		tick = 600 * time.Millisecond
	}
	switch {
	case !p.Running, p.InCombat, p.Modal:
		return false
	case StallStandAside(p.HP, p.MaxHP, p.NoFood):
		return false
	case p.SinceClick < WalkAlchAfterClick:
		return false
	case p.SinceRub < time.Duration(RubEveryTicks)*tick:
		return false
	case p.Ahead < RubAhead(p.Running):
		return false
	}
	return true
}

// RubAskLine is the rub item line for a mule ask that is going anyway: one Games necklace or Ring of
// dueling, any charge (a rub uses none), when the account holds none. A Rider: it never starts a
// walk, and a mule that holds none answers it with nothing.
func RubAskLine(held bool) (AskLine, bool) {
	if held {
		return AskLine{}, false
	}
	var choices []string
	for _, base := range RubItems {
		for c := 8; c >= 1; c-- {
			choices = append(choices, base+"("+Itoa(c)+")")
		}
	}
	return AskLine{Choices: choices, Count: 1, Rider: true}, true
}
