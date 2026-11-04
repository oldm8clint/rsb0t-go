package questmods

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// GnomeAgility is a training rung: gnome course laps until Agility reaches target. Its Done is
// the server's own level, since a training rung has no quest tab. id is the ladder's rung id
// (agility25, agility35).
func GnomeAgility(id string, target int) questrunner.Quest {
	custom := "gnome-laps"
	decide := func(s questplan.Snapshot) questplan.Step {
		if s.Level("agility") >= target {
			return questplan.Step{Kind: questplan.StepDone}
		}
		return questplan.Step{Kind: questplan.StepCustom, Custom: custom, Reason: fmt.Sprintf("gnome course laps to Agility %d", target)}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("Agility %d", target)},
			Anchor: questplan.GnomeStart, Decide: decide},
		Customs: map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
			return gnomeLaps(e.Deps(), target, gnomeBudget)
		}},
		Done: func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())["agility"] >= target },
	}
}

const (
	// gnomeBudget is how long one run of laps goes before it hands the account back to the engine (which looks at deaths,
	// the park rules and the slot between runs). It ends on the ground, never mid-lap on the platforms (gnomeLapsOn).
	gnomeBudget = 15 * time.Minute
	// gnomeLandWait is how long an obstacle is given to set the account down: the rope and the pipe walk it for six or
	// seven ticks, a net or a branch for two.
	gnomeLandWait = 25 * time.Second
	// gnomeLandedFor is how long the account stands on its landing tile before the next obstacle is approached: the
	// server has finished the movement, and the next approach plans from a tile the collision pack holds.
	gnomeLandedFor = time.Second
	// gnomeStableFor is the fallback when an obstacle sets the account down somewhere questplan.GnomeCourse does not
	// list: standing still that long in front of another obstacle counts as landed.
	gnomeStableFor = 3 * time.Second
	// gnomePoll is the landing watch's poll.
	gnomePoll = 200 * time.Millisecond
	// gnomeFinishWithin is the longest the laps go on past their budget (or past reaching the level) to bring an account
	// down off the platforms. A whole upper half takes about twenty-five seconds.
	gnomeFinishWithin = 4 * time.Minute
	// gnomeMaxFails is how many obstacles in a row may fail before the laps give up.
	gnomeMaxFails = 6
	// gnomePipeLockWait is the pause before trying the pipe again: its lock is twelve ticks.
	gnomePipeLockWait = 3 * time.Second
)

var errGnomeNoLoc = errors.New("no obstacle loc in reach")

// gnomeWorld is what the laps read and do. The account's Deps implement it (gnomeDeps); a test implements it over a
// scripted course.
type gnomeWorld interface {
	Now() time.Time
	// Delay waits d and reports false when the account is being stopped.
	Delay(d time.Duration) bool
	Tile() tile.Tile
	Agility() int
	// Operate walks to a tile the engine accepts obstacle i's op from and sends it. errGnomeNoLoc: the loc is not in reach.
	Operate(i int) error
	// Said is the game chat since t, lower-cased.
	Said(since time.Time) string
	WalkNear(dest tile.Tile, radius int) bool
	Travel(dest tile.Tile, radius int) bool
	Logf(format string, args ...any)
}

type gnomeDeps struct{ d *questrunner.Deps }

func (g gnomeDeps) Now() time.Time                   { return time.Now() }
func (g gnomeDeps) Delay(d time.Duration) bool       { return g.d.Delay(d) }
func (g gnomeDeps) Tile() tile.Tile                  { return questrunner.CurrentTile(g.d) }
func (g gnomeDeps) Agility() int                     { return questrunner.BaseLevels(g.d)["agility"] }
func (g gnomeDeps) WalkNear(t tile.Tile, r int) bool { return questrunner.WalkNear(g.d, t, r) }
func (g gnomeDeps) Travel(t tile.Tile, r int) bool   { return questrunner.Travel(g.d, t, r) }
func (g gnomeDeps) Logf(format string, args ...any)  { logf(g.d, format, args...) }
func (g gnomeDeps) Said(since time.Time) (said string) {
	if g.d.Chat != nil {
		said = strings.ToLower(g.d.Chat.Since(since))
	}
	return said
}

func (g gnomeDeps) Operate(i int) error {
	ob := questplan.GnomeCourse[i]
	m, ok := findGnome(g.d, ob)
	if !ok {
		return errGnomeNoLoc
	}
	return questrunner.OperateLoc(g.d, m, ob.Op)
}

// gnomeLaps runs obstacles until Agility reaches target or the budget ends. Each obstacle is placed from the account's
// own tile (GnomeNext), and cleared when the account has landed.
func gnomeLaps(d *questrunner.Deps, target int, budget time.Duration) bool {
	return gnomeLapsOn(gnomeDeps{d}, target, budget)
}

// gnomeLapsOn is the laps over any world.
//
// Two rules, from the live fleet of 16 Oct 2026 (between 05:35 and 07:46, 29 agility25 and 8 agility35 runs and 20 runs of the
// quests after them, on 19 accounts, parked "stranded in the Gnome agility course's upper floors"):
//
//   - An obstacle is cleared when the account has LANDED (questplan.GnomeLanded) and stood still, not when its tile first
//     reads as in front of the next obstacle. The rope and the pipe carry the account over six tiles, a tile a tick, and the
//     old check fired on the rope's second half (2481,3420,2, an island between two blocked tiles) and inside the pipe
//     (2484,3434,0, walled): the next approach planned from a tile the pack holds no path from. On the rope the walker
//     refused it as "stranded in the Gnome agility course's upper floors" (three of those park a quest run, whatever else
//     the laps did), and in the pipe it spent 35 to 55 seconds of scene walks and backoffs, on every lap (laps 72 to 106
//     seconds apart; the course itself takes about 35).
//   - The laps end only on the ground. The budget used to be checked after any obstacle, which left the account on the
//     level-1 or level-2 platform after three of the seven (runs ended on 2483,3420,2 and on 2473,3420,2, and the quests
//     after them were refused their first walks); the Guardian takes three minutes to act. Past the budget (or
//     once the level is reached) the laps go on until the account is down, at most gnomeFinishWithin longer.
func gnomeLapsOn(w gnomeWorld, target int, budget time.Duration) bool {
	deadline := w.Now().Add(budget)
	fails, lap := 0, 0
	lapStart := w.Now()
	var finishBy time.Time
	for {
		here := w.Tile()
		upper := questplan.InGnomeUpper(here)
		over := w.Agility() >= target || !w.Now().Before(deadline)
		if over && !upper {
			break
		}
		if over {
			if finishBy.IsZero() {
				finishBy = w.Now().Add(gnomeFinishWithin)
				w.Logf("on the Gnome course's upper floors at %v with the laps over: finishing the lap down by its own obstacles", here)
			} else if w.Now().After(finishBy) {
				w.Logf("still on the Gnome course's upper floors at %v after %s: leaving the account to the Guardian", here, gnomeFinishWithin)
				return false
			}
		}
		if !questplan.OnGnomeCourse(here) {
			if !w.Travel(questplan.GnomeStart, 1) {
				return false
			}
			continue
		}
		i := questplan.GnomeNext(here)
		ob := questplan.GnomeCourse[i]
		if i == 0 {
			lapStart = w.Now()
			if here.X != ob.Stand.X || here.Z != ob.Stand.Z {
				w.WalkNear(ob.Stand, 1)
			}
		}
		mark := w.Now()
		if err := w.Operate(i); err != nil {
			if errors.Is(err, errGnomeNoLoc) {
				w.Logf("no %s (%s) in reach at %v", ob.Name, ob.Op, w.Tile())
				w.WalkNear(ob.Stand, 1)
			} else {
				w.Logf("%s %s: %v", ob.Op, ob.Name, err)
			}
			fails++
			if fails > gnomeMaxFails {
				return false
			}
			continue
		}
		switch awaitLanding(w, i, mark, gnomeLandWait) {
		case landingStopped:
			return false
		case landingLocked:
			if !w.Delay(gnomePipeLockWait) { // a world-shared 12-tick lock per pipe
				return false
			}
		case landingNone:
			fails++
			if fails > gnomeMaxFails {
				w.Logf("the %s would not clear (%q)", ob.Name, w.Said(mark))
				return false
			}
		default:
			fails = 0
			if i == len(questplan.GnomeCourse)-1 {
				lap++
				w.Logf("gnome lap %d done in %s (agility %d, target %d)", lap, w.Now().Sub(lapStart).Round(time.Second), w.Agility(), target)
				lapStart = w.Now()
			}
		}
	}
	return w.Agility() >= target
}

type landing int

const (
	landingNone    landing = iota // the obstacle did not set the account down in time
	landingOK                     // landed, and still
	landingLocked                 // the pipe was in use by another account
	landingStopped                // the account is being stopped
)

// awaitLanding waits for obstacle i to set the account down: on the landing box and still for gnomeLandedFor, or (the
// fallback) still for gnomeStableFor in front of another obstacle. mark is when the op was sent, for the pipe's lock message.
func awaitLanding(w gnomeWorld, i int, mark time.Time, timeout time.Duration) landing {
	deadline := w.Now().Add(timeout)
	last, since := w.Tile(), w.Now()
	for {
		now, cur := w.Now(), w.Tile()
		if cur != last {
			last, since = cur, now
		}
		still := now.Sub(since)
		if questplan.GnomeLanded(i, cur) && still >= gnomeLandedFor {
			return landingOK
		}
		if questplan.GnomeNext(cur) != i && still >= gnomeStableFor {
			return landingOK
		}
		if i == len(questplan.GnomeCourse)-1 && strings.Contains(w.Said(mark), "pipe is being used") {
			return landingLocked
		}
		if !now.Before(deadline) {
			return landingNone
		}
		if !w.Delay(gnomePoll) {
			return landingStopped
		}
	}
}

// LeaveGnomeCourse brings an account standing on the Gnome agility course's upper floors down by the course's own
// obstacles (the next one in lap order, then the next, to the tree branch that sets it on the ground), and reports whether
// it is down. The platforms are a pocket for every quest's walks (the planner holds none of the obstacles, so a walk from
// there is "unreachable" however often it is tried, and the Guardian takes three minutes to act): the account is
// finished forward, never walked off. A no-op anywhere else.
func LeaveGnomeCourse(d *questrunner.Deps) bool {
	w := gnomeDeps{d}
	if !questplan.InGnomeUpper(w.Tile()) {
		return true
	}
	gnomeLapsOn(w, 0, 0)
	return !questplan.InGnomeUpper(w.Tile())
}

// The platforms are a pocket for every quest walk (questrunner.Travel), not only the Agility rungs': an account whose
// laps were cut off up there took three further quests of its ladder pass down with it (16 Oct 2026, Death Plateau
// among them), each parked after three refused walks.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Gnome agility course's upper floors", In: questplan.InGnomeUpper, Leave: LeaveGnomeCourse})
}

// findGnome finds the obstacle's loc near where it stands; for the pipe it tries both pipes,
// since each has its own world lock.
func findGnome(d *questrunner.Deps, ob questplan.GnomeObstacle) (m locindex.Match, ok bool) {
	here := questrunner.CurrentTile(d)
	for _, id := range ob.IDs {
		if m, ok = questrunner.FindLoc(d, id, "", ob.Op, here, 8); ok {
			return m, true
		}
	}
	return questrunner.FindLoc(d, ob.IDs[0], "", ob.Op, ob.Near, 6)
}
