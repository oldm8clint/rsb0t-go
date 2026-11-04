package randomevents

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// A staircase or a ladder as the way out of an event monster's leash.
//
// LIVE DEATH, 17 Oct 2026 08:34-08:44 (ScarabSwarm, Stage 5 quest hold after Imp Catcher, 54 hitpoints, no food, no
// runes): the account finished the quest in the Wizards' Tower top room (3103,3162 level 2) and idled there. A Swarm
// event monster appeared beside it, and for 9 minutes 51 seconds the Guardian logged, forty times, "evade threat ...
// threatens=true" and "cornered at 3103,3162,2 with no escape and npc ... attacking: fighting back, and the script
// may eat". Never one "evade: local escape".
//
// The leash walk (event_leash.go) looks for a tile on the account's own level more than the leash from where the
// monster began. The Swarm's is 15 and the top room is 33 tiles wide (3103-3107 by 3156-3165, real pack): no tile
// qualifies, the ring search finds none either, and HandleEvade fell into the cornered path, which attacked a
// monster the account cannot kill and handed the lock to a script with nothing to eat. But the engine drops a
// hostile event monster's target when the target changes LEVEL (Npc.validateTarget, event_leash.go's facts), and the
// down staircase (loc 1740 at 3104,3160) is three steps from where the account stood. The walker knows that
// staircase (webwalk's transport graph has it, "Climb-down" from 3104,3161 to 3103,3158 on level 1), and the
// Guardian did not.
//
// So a leashed event monster that cannot be left along the ground is left through a hop of the transport graph:
// a staircase or ladder between levels, or a ladder climbing out of a dungeon, the nearest by steps on the live
// pack, away from anything else that attacks. The graph is the walker's (internal/webwalk registers it with
// SetLevelExitSource; this package cannot import it, as webwalk imports this one), so an exit exists wherever the
// walker has one and no room needs a table of its own.

// LevelExit is one hop of the transport graph that puts an account on another level, or out of a dungeon, from a
// stand tile.
type LevelExit struct {
	// Stand is the tile the loc is operated from; Land is where the hop sets the account down.
	Stand, Land StrandTile
	// LocID, LocX and LocZ are the staircase's or ladder's placement.
	LocID, LocX, LocZ int
	// Label is the option's label ("Climb-down"); the option NUMBER is read from the loc's config when it is used.
	Label string
}

var levelExitSource struct {
	mu      sync.Mutex
	fn      func() []LevelExit
	loaded  bool
	byLevel map[int][]LevelExit
}

// SetLevelExitSource registers where the level exits come from (internal/webwalk's graph). The source is read once,
// the first time an event monster needs one, so a process that never meets one never parses the graph here.
// Process-wide, like SetNameResolver; replacing the source drops what was read.
func SetLevelExitSource(fn func() []LevelExit) {
	levelExitSource.mu.Lock()
	levelExitSource.fn, levelExitSource.loaded, levelExitSource.byLevel = fn, false, nil
	levelExitSource.mu.Unlock()
}

// levelExitsOn is the exits whose stand tile is on level.
func levelExitsOn(level int) []LevelExit {
	levelExitSource.mu.Lock()
	defer levelExitSource.mu.Unlock()
	if !levelExitSource.loaded {
		levelExitSource.loaded = true
		levelExitSource.byLevel = map[int][]LevelExit{}
		if levelExitSource.fn != nil {
			for _, e := range levelExitSource.fn() {
				levelExitSource.byLevel[e.Stand.Level] = append(levelExitSource.byLevel[e.Stand.Level], e)
			}
		}
	}
	return levelExitSource.byLevel[level]
}

const (
	// levelExitRadius is how far from the account a level exit may be sought: a room's own stairs, not a walk to
	// the next building.
	levelExitRadius = 40

	// levelExitUpPenalty is added to the steps of an exit that goes UP: up is deeper into the building, and the
	// way down is the way to the street. It still beats having no way out.
	levelExitUpPenalty = 8

	// levelExitMaxRoutes bounds the route searches a plan may run when the nearest exits turn out to have no route.
	levelExitMaxRoutes = 4
)

// The clocks of a level exit's execution; variables only so a test can shorten them.
var (
	levelExitPoll      = 250 * time.Millisecond
	levelExitWalkWait  = 12 * time.Second // the longest the walk to the stand tile is waited on
	levelExitClickWait = 5 * time.Second  // how long after a click the account is watched for a change of level
	levelExitClicks    = 3                // clicks on the loc before the exit is given up
)

// levelExitPlan is the chosen exit and the walk to its stand tile.
type levelExitPlan struct {
	Exit   LevelExit
	Route  []pathfind.NavPoint // from the account's tile to the stand tile; one point when it already stands there
	Steps  int
	Margin int // the exposure margin from other attackers that left a way (leashExposureLayers); 0 when none are in view
	Others int // other attackers in view
}

// exitLeavesMonster reports whether using e shakes a monster that began at spawnX,spawnZ on e's stand level: the
// landing is on another level (the engine drops the target), or on this one more than breakDist from the spawn
// (a ladder out of a dungeon). A landing in the dangerous ground the flee already refuses, in wolf territory, or in
// deeper Wilderness than the stand (the Mage Bank's ladder up) is no way out.
func exitLeavesMonster(e LevelExit, spawnX, spawnZ, breakDist int) bool {
	land := pathfind.NavPoint{X: e.Land.X, Z: e.Land.Z, Level: e.Land.Level}
	if inDangerZone(land) || inWolfTerritory(land) {
		return false
	}
	if WildernessLevel(e.Land.X, e.Land.Z, e.Land.Level) > WildernessLevel(e.Stand.X, e.Stand.Z, e.Stand.Level) {
		return false
	}
	if e.Land.Level != e.Stand.Level {
		return true
	}
	return chebyshev(e.Land.X, e.Land.Z, spawnX, spawnZ) >= breakDist
}

// planLevelExit is the nearest level exit, by steps along the ground the account can reach on its own level
// (the live pack, so a shut door is not walked through), that shakes a monster which began at spawnX,spawnZ. The
// widest margin from other attackers that leaves one is used, as leashPlan does. ok is false when no exit is
// reachable: a room with no stairs, a sealed floor, a pocket whose only way out passes the whole herd.
func planLevelExit(pack *pathfind.Pack, origin pathfind.NavPoint, exits []LevelExit, spawnX, spawnZ, breakDist int, others []hazardSite) (levelExitPlan, bool) {
	if pack == nil || len(exits) == 0 {
		return levelExitPlan{}, false
	}
	layers := leashExposureLayers
	if len(others) == 0 {
		layers = []int{0}
	}
	for _, layer := range layers {
		avoid := leashAvoid(origin, others, layer, nil)
		reach := pack.ReachFrom(origin, levelExitRadius, avoid)
		type cand struct {
			e     LevelExit
			steps int
			cost  int
		}
		var cands []cand
		for _, e := range exits {
			if e.Stand.Level != origin.Level || !exitLeavesMonster(e, spawnX, spawnZ, breakDist) {
				continue
			}
			steps, ok := reach.Steps(e.Stand.X, e.Stand.Z, e.Stand.Level)
			if !ok {
				continue
			}
			cost := steps
			if e.Land.Level > e.Stand.Level {
				cost += levelExitUpPenalty
			}
			cands = append(cands, cand{e, steps, cost})
		}
		sort.Slice(cands, func(i, j int) bool {
			a, b := cands[i], cands[j]
			if a.cost != b.cost {
				return a.cost < b.cost
			}
			if a.e.Stand.X != b.e.Stand.X {
				return a.e.Stand.X < b.e.Stand.X
			}
			if a.e.Stand.Z != b.e.Stand.Z {
				return a.e.Stand.Z < b.e.Stand.Z
			}
			return a.e.LocID < b.e.LocID
		})
		for i, c := range cands {
			if i >= levelExitMaxRoutes {
				break
			}
			stand := pathfind.NavPoint{X: c.e.Stand.X, Z: c.e.Stand.Z, Level: c.e.Stand.Level}
			route := []pathfind.NavPoint{origin}
			if stand != origin {
				res := pack.FindPathAvoiding(origin, stand, avoid)
				if !res.OK || len(res.Waypoints) == 0 {
					continue
				}
				route = res.Waypoints
			}
			return levelExitPlan{Exit: c.e, Route: route, Steps: c.steps, Margin: layer, Others: len(others)}, true
		}
	}
	return levelExitPlan{}, false
}

// levelExitOp is the option number of the exit's label on its loc, read from the registered loc configs; 0 when the
// configs are not registered or the loc has no such option.
func levelExitOp(e LevelExit) int {
	locs, ok := currentResolver().(LocResolver)
	if !ok {
		return 0
	}
	loc, ok := locs.Loc(e.LocID)
	if !ok {
		return 0
	}
	return cachecfg.OpIndex(loc.Ops, e.Label)
}

// describeLevelExit is the log fragment for a level exit.
func describeLevelExit(p levelExitPlan) string {
	clear := "no other attacker in view"
	if p.Margin > 0 {
		clear = fmt.Sprintf("%d tiles clear of the %d other attackers in view", p.Margin, p.Others)
	}
	return fmt.Sprintf("level exit: %s on loc %d at %d,%d from %d,%d,%d (%d steps) to %d,%d,%d, %s",
		p.Exit.Label, p.Exit.LocID, p.Exit.LocX, p.Exit.LocZ, p.Exit.Stand.X, p.Exit.Stand.Z, p.Exit.Stand.Level, p.Steps,
		p.Exit.Land.X, p.Exit.Land.Z, p.Exit.Land.Level, clear)
}

// hopSeen reports whether the account was carried off the ground it was walking by something that is no walk: it is
// on another level than origin, or this reading is further from the one before it (px,pz, taken at prevAt) than a run
// covers in the time between them. A ladder out of a dungeon is such a jump (the graph admits one only when it lands
// 3,000 or more tiles away: webwalk.levelExitOf), and so is a summons or a teleport that something else cast.
//
// It is NOT a distance from where the account began. The stand may be 40 steps from there, and an account part-way
// there is walking, not gone: the first version counted eight tiles from the origin as "left", so a stand twelve
// tiles off was reported as left after eight, with the staircase never clicked and the monster still on the account.
func hopSeen(px, pz int, prevAt time.Time, x, z, level int, origin pathfind.NavPoint) bool {
	if level != origin.Level {
		return true
	}
	return chebyshev(x, z, px, pz) > maxTilesPerPoll*(1+int(time.Since(prevAt)/pollInterval))
}

// runLevelExit walks to the exit's stand tile and operates it, and reports whether the account left. It eats on the
// flee's own rule while it walks (the handler holds the lock, so the account's eating is its to do), clicks the loc up
// to levelExitClicks times, and judges each click by where the account is afterwards (hopSeen). The hop is announced
// to the displacement watcher, so a ladder out of a dungeon is not read as a summons.
func runLevelExit(s actions.Session, origin pathfind.NavPoint, plan levelExitPlan, log func(string)) bool {
	st := s.State()
	stand := pathfind.NavPoint{X: plan.Exit.Stand.X, Z: plan.Exit.Stand.Z, Level: plan.Exit.Stand.Level}
	var lastHold time.Time
	eat := func() {
		if time.Since(lastHold) >= fleeHoldGroundEvery && fleeEatIfLow(s, log) {
			lastHold = time.Now()
		}
	}
	// The reading each poll is compared with: where the account was the poll before.
	px, pz, _, ok := st.Tile()
	if !ok {
		return false
	}
	prevAt := time.Now()
	if len(plan.Route) > 1 {
		if _, err := walkFleeRoute(s, plan.Route); err != nil {
			if log != nil {
				log("evade: level exit: the walk to the stand failed: " + err.Error())
			}
			return false
		}
		wait := time.Duration(plan.Steps+4) * 700 * time.Millisecond
		if wait > levelExitWalkWait {
			wait = levelExitWalkWait
		}
		for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(levelExitPoll) {
			x, z, level, ok := st.Tile()
			if !ok {
				return false
			}
			if hopSeen(px, pz, prevAt, x, z, level, origin) {
				return true // something else carried the account off
			}
			px, pz, prevAt = x, z, time.Now()
			if x == stand.X && z == stand.Z {
				break
			}
			eat()
		}
	}
	op := levelExitOp(plan.Exit)
	if op == 0 {
		if log != nil {
			log(fmt.Sprintf("evade: level exit: loc %d has no %q option in the registered configs", plan.Exit.LocID, plan.Exit.Label))
		}
		return false
	}
	for click := 1; click <= levelExitClicks; click++ {
		withdraw := ExpectHop(s, "event level exit", stand.X, stand.Z, stand.Level, plan.Exit.Land.X, plan.Exit.Land.Z, plan.Exit.Land.Level, 20*time.Second)
		if err := actions.OpLoc(s, op, plan.Exit.LocX, plan.Exit.LocZ, plan.Exit.LocID); err != nil {
			withdraw()
			if log != nil {
				log(fmt.Sprintf("evade: level exit: %s on loc %d failed: %v", plan.Exit.Label, plan.Exit.LocID, err))
			}
			return false
		}
		for deadline := time.Now().Add(levelExitClickWait); time.Now().Before(deadline); time.Sleep(levelExitPoll) {
			x, z, level, ok := st.Tile()
			if !ok {
				return false
			}
			if hopSeen(px, pz, prevAt, x, z, level, origin) {
				if log != nil {
					log(fmt.Sprintf("evade: level exit: left %d,%d,%d for %d,%d,%d by %s on loc %d (click %d)",
						origin.X, origin.Z, origin.Level, x, z, level, plan.Exit.Label, plan.Exit.LocID, click))
				}
				return true
			}
			px, pz, prevAt = x, z, time.Now()
			eat()
		}
		withdraw()
	}
	if log != nil {
		x, z, level, _ := st.Tile()
		log(fmt.Sprintf("evade: level exit: still at %d,%d,%d after %d clicks of %s on loc %d", x, z, level, levelExitClicks, plan.Exit.Label, plan.Exit.LocID))
	}
	return false
}

// evadeLevelExit is HandleEvade's probe for a leashed event monster the leash walk found no tile to leave: plan a
// level exit on the live pack from the monster as the state sees it, and take it. It reports whether the account
// left (the handler is then done: the monster has lost its target and the account is on another floor, where the
// walk-back to the old tile means nothing).
func evadeLevelExit(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, ev *Event, attempt int, log func(string)) bool {
	maxrange, ok := eventLeashOf(ev.NpcType)
	if !ok {
		return false
	}
	monster := gamestate.NPC{Index: ev.NpcIndex, Type: ev.NpcType, X: ev.ThreatX, Z: ev.ThreatZ, Level: origin.Level}
	for _, npc := range s.State().NPCs() {
		if npc.Index == ev.NpcIndex && npc.Type == ev.NpcType {
			monster = npc
			break
		}
	}
	spawnX, spawnZ := noteLeashSpawn(s, monster.Index, monster.Type, monster.X, monster.Z, monster.Level)
	plan, ok := planLevelExit(live, origin, levelExitsOn(origin.Level), spawnX, spawnZ, leashBreakFor(maxrange, attempt), otherAttackers(s.State(), origin))
	if !ok {
		return false
	}
	if log != nil {
		log(fmt.Sprintf("evade: no tile on this floor is outside the leash of %s (%d from %d,%d) — %s", ev.Name, maxrange, spawnX, spawnZ, describeLevelExit(plan)))
	}
	return runLevelExit(s, origin, plan, log)
}

// fleeExitBreakDist is how far from the attacker a same-level exit (a ladder out of a dungeon) has to land for the
// walking flee to take it: the flee's own ring distance.
const fleeExitBreakDist = 20

// fleeLevelExit is the walking flee's way out before it holds its ground: a room whose only exit is shut or
// guarded is not a dead end if a staircase or ladder stands in it, because an NPC drops its target when the
// target changes level (Npc.validateTarget). Planned away from the attacker's tile. It reports whether the account
// left.
func fleeLevelExit(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, anchorX, anchorZ int, log func(string)) bool {
	plan, ok := planLevelExit(live, origin, levelExitsOn(origin.Level), anchorX, anchorZ, fleeExitBreakDist, otherAttackers(s.State(), origin))
	if !ok {
		return false
	}
	if log != nil {
		log("combat flee (walking): nowhere to walk on this floor — " + describeLevelExit(plan))
	}
	return runLevelExit(s, origin, plan, log)
}

// LevelExitForLab exposes the plan to the loopback harnesses and the real-pack tests: the nearest level exit from
// origin for a monster that began at spawnX,spawnZ with a leash of maxrange, with nothing else attacking. It uses the
// registered source (SetLevelExitSource).
func LevelExitForLab(pack *pathfind.Pack, origin pathfind.NavPoint, spawnX, spawnZ, maxrange int) (exit LevelExit, steps int, ok bool) {
	plan, ok := planLevelExit(pack, origin, levelExitsOn(origin.Level), spawnX, spawnZ, leashBreakFor(maxrange, 1), nil)
	return plan.Exit, plan.Steps, ok
}
