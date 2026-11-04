package randomevents

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
)

// A FLEE UNDER A SLOW OR SILENT LINK (plantfisher-1018, from the 18 Oct 2026 20:03:36 death).
//
// The walking flee judged a leg in wall time: fleeWalkLegInterval, three seconds, "moved 0 tile(s) ... marking it bad and
// redirecting". On the night of 18 Oct the server was lagging and the death's eight legs, 20:03:06 to 20:03:35, moved the account
// no tile at all: nothing it sent was acted on for minutes (its pick clicks, a bank booth click, six walks and the flee
// walks). A leg of three seconds is five ticks at best and two at the 1.2 s per tick that night, and the fleet measures bites
// landing 7 to 14 seconds after the click (actions/bite.go), so the judgement "this destination does not work" was passed on legs
// the server had not read yet. Each verdict then threw the destination away, banned the ground round it (withoutBadDests) and sent a
// walk of another shape, which on the server replaces the queue it has not yet started: the flee never had a leg that was awaited.
// webwalk got this fix on 17 and 18 Oct (tick-counted stalls, holdForLag); the flee did not.
//
// So a leg is judged in the server's time, as webwalk's stalls are: it must be old enough in wall time (fleeWalkLegInterval) AND, while
// the view is ticking, fleeLegMinTicks game ticks old, or fleeLegValve wall intervals old whatever the tick count says; and not at all
// while the view is silent or catching up on a backlog (State.ViewLagging), when the tile it shows may be minutes old and a verdict on
// it is a verdict on nothing. The flee's own deadline is pushed out by the time it waited for a lagging view, up to fleeLagHoldMax.
//
// And a flee whose legs have judged-zero progress fleeStuckLegs in a row is stuck, not merely redirected: nothing it sends is moving
// the account. It then leaves by the teleport it can pay for, a spell or a rub of the jewellery it carries (rubteleport.go), when what
// hits it is more than food can answer (fleeStuckDanger), instead of planning the same corridor again.
const (
	// fleeLegMinTicks is how many game ticks a leg's walk must have been in the server's hands before a leg that moved nothing is
	// judged: five, the ticks a three-second leg holds at the server's pace.
	fleeLegMinTicks = 5
	// fleeLegValve is how many leg intervals of wall time judge a leg whatever the ticks say (a client whose ticks stopped counting).
	fleeLegValve = 3
	// fleeStuckLegs is how many judged legs in a row that moved the account less than fleeProgressTiles make the flee stuck.
	fleeStuckLegs = 2
	// fleeStuckHPPercent is the hitpoints share under which a stuck flee leaves by teleport whatever hits it.
	fleeStuckHPPercent = 70
)

// fleeLagHoldMax bounds how much a lagging view can push a flee's deadline out. A variable only so a test can shorten it; nothing else
// assigns it.
var fleeLagHoldMax = 40 * time.Second

// legClock times one leg of the flee: when its window opened and what the game tick count was.
type legClock struct {
	openedAt time.Time
	tick     int
}

func newLegClock(st *gamestate.State, now time.Time) legClock {
	return legClock{openedAt: now, tick: st.Tick()}
}

// judgeable reports whether a leg opened at c has had its time at now: interval of wall time and, if the view is ticking, the ticks
// fleeLegMinTicks, or fleeLegValve intervals whatever the ticks say. A view that is not ticking at all since the leg opened (no tick
// has arrived) is judged on the wall alone: the silence check (State.ViewLagging) is the caller's, and a client that does not count
// ticks reads like that.
func (c legClock) judgeable(st *gamestate.State, now time.Time, interval time.Duration) bool {
	elapsed := now.Sub(c.openedAt)
	if elapsed < interval {
		return false
	}
	if elapsed >= fleeLegValve*interval {
		return true
	}
	ticks := st.Tick() - c.tick
	return ticks < 1 || ticks >= fleeLegMinTicks
}

// unfeedableEventType reports whether an event monster hits harder than food heals, so that a flee which is not moving has no
// time to be answered by eating: the angry Strange plant (strength 10 and a poison of severity 10 on every hit) and the Shade
// (level 120 for combat 61 to 90).
func unfeedableEventType(npcType int) bool {
	return npcType == angryPlantType || (npcType >= 425 && npcType <= 430)
}

// fleeStuckDanger reports whether a flee that has stopped moving is in enough danger to leave by teleport: something is hitting the
// account, and either it is one food cannot answer or the hitpoints are already under fleeStuckHPPercent.
func fleeStuckDanger(s actions.Session) bool {
	st := s.State()
	if st == nil || !st.InCombat() {
		return false
	}
	if hp, ok := st.HP(); ok {
		if max, ok := st.MaxHP(); ok && max > 0 && hp*100 < max*fleeStuckHPPercent {
			return true
		}
	}
	att, found := currentAttacker(st)
	return found && unfeedableEventType(att.Type)
}

// fleeStuckEscape leaves by the spell the account can pay for, else by a rub of its jewellery, and reports whether it did.
func fleeStuckEscape(s actions.Session, log func(string)) bool {
	const why = "combat flee: stuck"
	if EscapeTeleport(s, why, lastResortGap, log) {
		return true
	}
	return rubTeleport(s, why, lastResortGap, log)
}
