package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// LEAVING A PLANT BEFORE IT TURNS (plantfisher-1018, 18 Oct 2026).
//
// An angry Strange plant loses its target at maxrange+1 = 4 tiles from the tile it was added at (event_leash.go has the facts), so
// an account that is 5 tiles from the seed when it turns is never touched, and the plant is deleted by its own timer a few
// ticks later. Walking out of the leash is a handful of steps the account takes while the plant is still passive; walking out
// of it after the plant has turned, as the evade does, is a walk under attack by something that poisons for severity 10 on every
// hit. The death at the Fishing Guild bank 18 Oct 20:03:36 was the second: 97 ticks beside the seed, then eight "moved 0 tiles"
// legs in the leash of an angry one.
//
// So an unpicked seed is left at plantLeaveAtTicks (plantclock.go) -- a quarter of a minute before it turns, lag included -- by the
// shortest walk that ends 5 tiles from where it began and clear of anything else that attacks (leashPlan, the same planner the
// evade uses), and the account stays out: the Guardian does not approach it again, Busy holds the script, and the account's input
// is held, until the plant is gone or well past the tick it turns (inputhold.go).

// plantLeaveWalkBudget bounds the walk out of the leash. A lagging view pushes it out (fleeLagHoldMax) as it does the flee's. A
// variable only so a test can shorten it; nothing else assigns it.
var plantLeaveWalkBudget = 15 * time.Second

// plantStayOutDefer is how long the Guardian leaves a plant it has walked away from before looking at it again.
const plantStayOutDefer = 6 * time.Second

// plantReplyBase and plantReplyMax bound how long a pick click is waited on for its reply (plantReplyWaitFor). Variables only so a
// test can shorten them; nothing else assigns them.
var (
	plantReplyBase = 2 * time.Second
	plantReplyMax  = 8 * time.Second
)

// plantReplyWait is how long the click just sent is waited on: the tick-perfect two seconds, longer by the lag the view has seen
// recently and the slowest bite the account has had in the last three minutes (a bite and a pick travel the same route).
func plantReplyWait(s actions.Session) time.Duration {
	st := s.State()
	var biteLag time.Duration
	if b, ok := actions.LastBiteLag(st); ok && time.Since(b.At) < 3*time.Minute {
		biteLag = b.Took
	}
	return plantReplyWaitFor(plantReplyBase, st.RecentViewLag().Ticks, biteLag, plantReplyMax)
}

// plantClockDiag is the log fragment that makes a missing reply classifiable: the plant's age in ticks against its clock, how
// long the click was waited on, the lag the account has shown, and the two things that make the engine drop or sit on an interaction
// click without a word: a modal interface open (Player.canAccess is false while one is) and a delay a rub or a cast is known to hold
// (OpNpcHandler drops the click of a delayed player). Live 18 Oct the log could not say which of lag, a stall or a modal it was.
func plantClockDiag(s actions.Session, npcIndex int, waited time.Duration) string {
	st := s.State()
	age, ok := plantAgeTicks(st, npcIndex)
	ageText := "age unknown"
	if ok {
		ageText = fmt.Sprintf("age %dt of %d (pickable from %d, leave at %d)", age, plantTurnTicks, plantGrowTicks, plantLeaveAtTicks)
	}
	lag := st.ViewLag()
	main, chat, side := st.Modals()
	out := fmt.Sprintf("plant %s, waited %s, view lag %dt, view current %t, modals %d/%d/%d, held %s",
		ageText, waited.Round(100*time.Millisecond), lag.Recent.Ticks, !lag.Lagging, main, chat, side, actions.DelayLeft(s, time.Now()).Round(100*time.Millisecond))
	if b, ok := actions.LastBiteLag(st); ok && time.Since(b.At) < 3*time.Minute {
		out += fmt.Sprintf(", last bite %s late", b.Took.Round(100*time.Millisecond))
	}
	return out
}

// plantNotToBeApproached reports whether the Guardian must not walk up to ev's plant: the account has already walked out of its leash, or
// the plant is old enough that it is left instead of picked (plantMustLeave).
func plantNotToBeApproached(s actions.Session, ev *Event) bool {
	if p := plantsOf(s); p != nil && p.hasLeft(ev.NpcIndex) {
		return true
	}
	age, ok := plantAgeTicks(s.State(), ev.NpcIndex)
	return ok && plantMustLeave(age, s.State().RecentViewLag().Ticks)
}

// forgetPlant drops npcIndex from the session's engaged plants: it is gone, or not this account's.
func forgetPlant(s actions.Session, npcIndex int) {
	if p := plantsOf(s); p != nil {
		p.forget(npcIndex)
	}
}

// leavePlantLeash walks the account out of the passive plant's leash, and reports whether it did (or whether the account was
// already clear): false when no way out was found, so the pick carries on. age and lagTicks are for the log.
func leavePlantLeash(s actions.Session, pack *pathfind.Pack, ev *Event, age, lagTicks int, log func(string)) (left bool, err error) {
	st := s.State()
	x, z, level, ok := st.Tile()
	if !ok {
		return false, nil
	}
	maxrange, _ := eventLeashOf(angryPlantType)
	spawnX, spawnZ := noteLeashSpawn(s, ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ, level)
	clear := maxrange + 2 // the first tile the plant's leash is lost at
	untilClear := time.Duration(max(plantClearTicks-age, 0)) * 600 * time.Millisecond
	stayOut := func() {
		if p := plantsOf(s); p != nil {
			p.left(ev.NpcIndex)
		}
		deferEvent(s, ev, max64(untilClear, plantStayOutDefer))
	}
	if chebyshev(x, z, spawnX, spawnZ) >= clear {
		stayOut()
		if log != nil {
			log(fmt.Sprintf("plant is %dt old and unpicked (view lag %dt): already %d tiles from where it began (%d,%d), staying out of its leash until it is gone",
				age, lagTicks, chebyshev(x, z, spawnX, spawnZ), spawnX, spawnZ))
		}
		return true, nil
	}
	if pack == nil {
		return false, nil
	}
	origin := pathfind.NavPoint{X: x, Z: z, Level: level}
	res, found := leashPlan(fleePack(s, pack), origin, spawnX, spawnZ, ev.ThreatX, ev.ThreatZ, leashBreakFor(maxrange, 1), otherAttackers(st, origin), nil)
	if !found {
		if log != nil {
			log(fmt.Sprintf("plant is %dt old and unpicked (view lag %dt) but there is no way out of its leash from %d,%d: picking on", age, lagTicks, x, z))
		}
		return false, nil
	}
	if log != nil {
		log(fmt.Sprintf("plant is %dt old and unpicked (view lag %dt; it turns at %dt): leaving its leash instead of clicking on; %s", age, lagTicks, plantTurnTicks, describeLeash(s, res)))
	}
	stayOut()
	return true, walkOutOfLeash(s, pack, ev, res, spawnX, spawnZ, clear, log)
}

func max64(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// walkOutOfLeash sends res's route and watches it in the server's time (fleelag.go) until the account is clear tiles from the seed's
// tile, the plant is gone, or something hits the account (the plant has turned: the evade is the handler's then). A leg that has had
// its time and left the account short of the goal is planned again from the tile the account is really on and sent again, once per
// leg, never while the view is silent or catching up.
func walkOutOfLeash(s actions.Session, pack *pathfind.Pack, ev *Event, res leashResult, spawnX, spawnZ, clear int, log func(string)) error {
	st := s.State()
	budget := time.Now().Add(plantLeaveWalkBudget)
	var lagHeld time.Duration
	var clock legClock
	sent := false
	for time.Now().Before(budget) {
		x, z, level, ok := st.Tile()
		if !ok {
			return nil
		}
		if chebyshev(x, z, spawnX, spawnZ) >= clear {
			if log != nil {
				log(fmt.Sprintf("out of the plant's leash at %d,%d (%d tiles from %d,%d)", x, z, chebyshev(x, z, spawnX, spawnZ), spawnX, spawnZ))
			}
			return nil
		}
		if !npcStillPresent(s, ev.NpcIndex) || st.InCombat() {
			return nil
		}
		lagging := st.ViewLagging()
		if lagging {
			lagHeld += fleeWalkPollInterval
			if lagHeld <= fleeLagHoldMax {
				budget = budget.Add(fleeWalkPollInterval)
			}
			clock = newLegClock(st, time.Now())
		}
		if !lagging && (!sent || clock.judgeable(st, time.Now(), fleeWalkLegInterval)) {
			if sent {
				// The last leg has had its ticks and the account is still inside: plan from where it really stands.
				origin := pathfind.NavPoint{X: x, Z: z, Level: level}
				again, found := leashPlan(fleePack(s, pack), origin, spawnX, spawnZ, ev.ThreatX, ev.ThreatZ, clear+1, otherAttackers(st, origin), nil)
				if !found {
					return nil
				}
				res = again
			}
			if _, err := walkFleeRoute(s, res.Route); err != nil {
				return err
			}
			sent = true
			clock = newLegClock(st, time.Now())
		}
		time.Sleep(fleeWalkPollInterval)
	}
	return nil
}
