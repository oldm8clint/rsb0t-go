package randomevents

import (
	"fmt"
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// PvP escape (a user request: "pvp kills for the stuck accounts"). In the underground Wilderness
// the only other ways out are a fail-teleport or a death, so stranded accounts attack one another:
// every death respawns an account in Lumbridge, on open ground. A fighting account neither eats nor
// flees (recoverStrand holds the Guardian, so EmergencyFlee and every script's eating stay idle), and
// the last account standing keeps the old ways out. A death keeps three items; the rest drops.
//
// The server only lets two players fight when their combat levels differ by no more than the lower of
// their two Wilderness levels (pvp_combat.rs2's pvp_level_check), and one Wilderness level is eight
// tiles. Live 3 Oct 11:17-11:40 the accounts sat at levels 1-9 with combat levels 2-90, so 343 attack
// clicks killed 10 (owner: "some accounts need to run north so they can fight each other"). An account
// with nobody it may fight in range walks north to pvpRally, where nearly any two can.

const (
	// pvpRange is how far (Chebyshev, tiles) a target may stand.
	pvpRange = 12
	// pvpSettle is how long one attack click is given to start a fight before another is tried.
	pvpSettle = 6 * time.Second
	// pvpRefused is how long a target that never fought back (out of level range, already fighting
	// someone else, or out of reach) is skipped.
	pvpRefused = time.Minute
	// pvpRallyRadius is how close to pvpRally an account stops walking and waits for others.
	pvpRallyRadius = 4
	// pvpRallyFight is how close to pvpRally an account must be before it picks a fight: everyone
	// walks to the rally first, where the Wilderness level (43-47) lets nearly any two fight (owner,
	// 3 Oct 12:30: "all accounts should walk to acct111 and fight there deep in the wilderness").
	pvpRallyFight = 16
)

// pvpRally is where every stranded account walks before it fights (acct111 stood at 3088,10270): 3079-3097,10268-10288 in mapsquare 48,160 is
// where the accounts summoned to 3040,10336 wandered on 3 Oct, at Wilderness level 45.
var pvpRally = StrandTile{X: 3088, Z: 10278, Level: 0}

// pvpRallySouth is the rally for an account south of the void (z below 10048): live 3 Oct 13:15 no
// account ever crossed north from there (they sit at 3084-3088,10047), so they gather on the deepest
// real map instead, at Wilderness level 16, reached by a pack route around the dungeon's walls.
var pvpRallySouth = StrandTile{X: 3092, Z: 10044, Level: 0}

// voidSouthEdge is the first z of the void below the Wilderness (mapsquare row 157).
const voidSouthEdge = 157 * 64

// pvpLeg is how many tiles (Chebyshev) one pack-routed rally walk click covers.
const pvpLeg = 20

// pvpDeep is where an account with nobody it may fight at its rally keeps walking: Wilderness
// level 55 (owner, 3 Oct 16:25: "if you keep heading north youll be at 55 wilderness"). Live 16:23
// spare104 (combat 84) and acct52 (52) stood at the level 16 rally with a gap of 32.
var pvpDeep = StrandTile{X: 3088, Z: 10360, Level: 0}

// pvpIdleNorth is how long an account stands at its rally with nobody to fight before heading north.
const pvpIdleNorth = 90 * time.Second

// rallyFor is the account's current rally: pvpDeep once it has given up on its first rally.
func (g *Guardian) rallyFor(z int) StrandTile {
	if g.pvp.north {
		return pvpDeep
	}
	return pvpRallyFor(z)
}

// pvpRallyFor is the first rally for an account at z.
func pvpRallyFor(z int) StrandTile {
	if z < voidSouthEdge {
		return pvpRallySouth
	}
	return pvpRally
}

// pvpState is the PvP escape's memory; touched only under actionMu.
type pvpState struct {
	site    string
	target  int
	at      time.Time
	refused map[int]time.Time
	clicks  int
	rally   bool
	// idleSince is when the account first stood at its rally with nobody it may fight; north is
	// set once that has lasted pvpIdleNorth, and from then on it heads for pvpDeep.
	idleSince time.Time
	north     bool
}

// WildernessLevel ports the content's wilderness_level proc over wilderness_zones.dbrow: one level per
// eight tiles north of the zone's south edge, 0 outside both zones. The main Wilderness is mapsquares
// 46-52 by 55-99 on every plane; the dungeons below it are 46-52 by 155-199 on plane 0 only.
func WildernessLevel(x, z, level int) int {
	if x < 46*64 || x >= 53*64 {
		return 0
	}
	switch {
	case z >= 55*64 && z < 100*64:
		return (z-55*64)/8 + 1
	case level == 0 && z >= 155*64 && z < 200*64:
		return (z-155*64)/8 + 1
	}
	return 0
}

// PvPAllowed is pvp_level_check: both players in the Wilderness, and their combat levels no further
// apart than the lower of their Wilderness levels. An unknown combat level (0) is allowed; the refusal
// timer catches it if the server disagrees.
func PvPAllowed(selfCombat, selfWild, otherCombat, otherWild int) bool {
	wild := min(selfWild, otherWild)
	if wild < 1 {
		return false
	}
	if selfCombat <= 0 || otherCombat <= 0 {
		return true
	}
	return abs(selfCombat-otherCombat) <= wild
}

// PickPvPTarget is the player strandPvP attacks: the nearest one within pvpRange on the same level
// that the server would let selfCombat fight, is not in a fight and is not refused, ties to the lower
// index. ok is false when there is none.
func PickPvPTarget(selfX, selfZ, level, selfCombat int, players []gamestate.Player, refused map[int]time.Time, now time.Time) (gamestate.Player, bool) {
	selfWild := WildernessLevel(selfX, selfZ, level)
	var cands []gamestate.Player
	for _, p := range players {
		if p.Level != level || p.InCombat {
			continue
		}
		dx, dz := p.X-selfX, p.Z-selfZ
		if max(abs(dx), abs(dz)) > pvpRange {
			continue
		}
		if !PvPAllowed(selfCombat, selfWild, p.CombatLevel, WildernessLevel(p.X, p.Z, p.Level)) {
			continue
		}
		if until, ok := refused[p.Index]; ok && now.Before(until) {
			continue
		}
		cands = append(cands, p)
	}
	if len(cands) == 0 {
		return gamestate.Player{}, false
	}
	sort.Slice(cands, func(i, j int) bool {
		di := max(abs(cands[i].X-selfX), abs(cands[i].Z-selfZ))
		dj := max(abs(cands[j].X-selfX), abs(cands[j].Z-selfZ))
		if di != dj {
			return di < dj
		}
		return cands[i].Index < cands[j].Index
	})
	return cands[0], true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// strandPvP runs one poll of the PvP escape. Called with actionMu held; always takes the poll, so
// nothing else (a flee, a script's eat) acts while the account is down here.
func (g *Guardian) strandPvP(site *StrandSite, x, z, level int) bool {
	v := &g.pvp
	if v.site != site.Name {
		*v = pvpState{site: site.Name, target: -1, refused: map[int]time.Time{}}
		g.void = voidWalk{site: site.Name}
		g.log(fmt.Sprintf("strand: %s at %d,%d,%d — PvP escape: attacking other stranded accounts, no eating, no fleeing (a death respawns in Lumbridge)", site.Name, x, z, level))
	}
	st := g.session.State()
	players := st.Players()
	if !st.InCombat() && !st.AttackedByPlayer() && g.pvpHop(site) {
		return true
	}
	if st.InCombat() || st.AttackedByPlayer() || targetFightsBack(players, v.target, st.Self()) {
		// Fighting: let it run its course.
		v.at = time.Now()
		return true
	}
	if !v.north && pvpWalksToRally(x, z, g.void.stuckCount()) {
		v.target = -1
		g.pvpRallyHop(site, x, z, level)
		return true
	}
	now := time.Now()
	if v.target >= 0 && now.Sub(v.at) < pvpSettle {
		return true
	}
	if v.target >= 0 {
		// The last click started no fight: skip that player for a while.
		v.refused[v.target] = now.Add(pvpRefused)
		v.target = -1
	}
	combat, _ := st.CombatLevel()
	p, ok := PickPvPTarget(x, z, level, combat, players, v.refused, now)
	if !ok {
		r := g.rallyFor(z)
		if !v.north && max(abs(r.X-x), abs(r.Z-z)) <= pvpRallyRadius {
			if v.idleSince.IsZero() {
				v.idleSince = now
			} else if now.Sub(v.idleSince) >= pvpIdleNorth {
				v.north, v.rally = true, false
				g.void = voidWalk{site: site.Name}
				g.log(fmt.Sprintf("strand: %s — nobody to fight at the rally for %s; heading north for %d,%d (Wilderness level %d), fighting anyone in range on the way",
					site.Name, now.Sub(v.idleSince).Round(time.Second), pvpDeep.X, pvpDeep.Z, WildernessLevel(pvpDeep.X, pvpDeep.Z, 0)))
			}
		}
		g.pvpRallyHop(site, x, z, level)
		return true
	}
	v.idleSince = time.Time{}
	v.rally = false
	if err := actions.OpPlayer2(g.session, int32(p.Index)); err != nil {
		g.log(fmt.Sprintf("strand: %s — attacking %q failed: %v", site.Name, p.Name, err))
		return true
	}
	v.target, v.at = p.Index, now
	v.clicks++
	if v.clicks%10 == 1 {
		g.log(fmt.Sprintf("strand: %s — PvP attack %d on %q (combat %d vs %d, Wilderness level %d) at %d,%d",
			site.Name, v.clicks, p.Name, combat, p.CombatLevel, WildernessLevel(x, z, level), p.X, p.Z))
	}
	return true
}

// pvpWorld is the world every stranded account fights on. Live 3 Oct 14:30 the five accounts at the
// southern rally were split over worlds 1 and 3, where no two were within Wilderness level 16 of
// each other's combat level, so none of them could fight.
const pvpWorld = 1

// pvpHopEvery is how often a stranded account off pvpWorld asks for the hop again.
const pvpHopEvery = 10 * time.Minute

// pvpHop asks the fleet to move an account on another world to pvpWorld, at most every pvpHopEvery,
// and reports whether it asked this poll. The account logs back in on the same tile; ScarabSwarm's
// hop home waits until it is out of the Wilderness and no longer held (scarabswarm_world.go).
func (g *Guardian) pvpHop(site *StrandSite) bool {
	if g.HopWorld == nil || g.World == nil {
		return false
	}
	w := g.World()
	if w <= 0 || w == pvpWorld || (!g.pvpHopAt.IsZero() && time.Since(g.pvpHopAt) < pvpHopEvery) {
		return false
	}
	g.pvpHopAt = time.Now()
	if err := g.HopWorld(pvpWorld, "stranded in "+site.Name+": the PvP escape fights on one world"); err != nil {
		g.log(fmt.Sprintf("strand: %s — hop to world %d for the PvP escape failed: %v", site.Name, pvpWorld, err))
		return false
	}
	g.log(fmt.Sprintf("strand: %s — on world %d; hopping to world %d, where the other stranded accounts fight", site.Name, w, pvpWorld))
	return true
}

// pvpWalksToRally is whether an account at x,z walks for the rally instead of fighting: it is farther
// than pvpRallyFight from it, and no wall has turned back every heading (voidStuckSlow hops without
// moving). Live 3 Oct 12:53, 22 accounts stopped at 3155,9950, 3156,9979 and 3128,9994; they fight
// where they are.
func pvpWalksToRally(x, z, stuck int) bool {
	r := pvpRallyFor(z)
	return max(abs(r.X-x), abs(r.Z-z)) > pvpRallyFight && stuck < voidStuckSlow
}

// pvpRallyHop walks one hop toward pvpRally when there is nobody the account may fight in range.
func (g *Guardian) pvpRallyHop(site *StrandSite, x, z, level int) {
	rally := g.rallyFor(z)
	if max(abs(rally.X-x), abs(rally.Z-z)) <= pvpRallyRadius {
		return
	}
	if !g.pvp.rally {
		g.pvp.rally = true
		g.log(fmt.Sprintf("strand: %s — at Wilderness level %d; heading for the rally at %d,%d (level %d) to fight there",
			site.Name, WildernessLevel(x, z, level), rally.X, rally.Z, WildernessLevel(rally.X, rally.Z, rally.Level)))
	}
	if rally == pvpRallySouth && g.packHop(site.Name, x, z, level, rally) {
		return
	}
	g.voidHop(site.Name, x, z, level, rally)
}

// packHop walks one pvpLeg of a pack route toward to, on voidHop's clock and stuck count (straight
// hops bounced off the dungeon walls: live 3 Oct 13:15, 22 accounts at 3155,9954, 3156,9979 and
// 3128,9994, 50-70 hops each). False when there is no pack or no route, for voidHop to try instead.
func (g *Guardian) packHop(name string, x, z, level int, to StrandTile) bool {
	if g.pack == nil {
		return false
	}
	v := &g.void
	wait := voidHopWait
	if v.slow() {
		wait = voidStuckWait
	}
	if !v.at.IsZero() && time.Since(v.at) < wait {
		return true
	}
	res := g.pack.FindPath(pathfind.NavPoint{X: x, Z: z, Level: level}, pathfind.NavPoint{X: to.X, Z: to.Z, Level: to.Level})
	if !res.OK || len(res.Waypoints) < 2 {
		return false
	}
	if !v.at.IsZero() {
		if x == v.lastX && z == v.lastZ {
			v.stuck++
		} else {
			v.stuck = 0
		}
	}
	// A route winds, so its progress is the length still to walk, not the straight distance.
	v.progress(routeLength(res.Waypoints))
	leg := LegWithin(res.Waypoints, pvpLeg)
	v.lastX, v.lastZ, v.at = x, z, time.Now()
	v.hops++
	if err := actions.Walk(g.session, leg, true); err != nil {
		g.log(fmt.Sprintf("strand: %s — route walk failed: %v", name, err))
		return true
	}
	if v.hops%10 == 1 || v.stuck > 0 || v.flat == voidFlatHops {
		end := leg[len(leg)-1]
		g.log(fmt.Sprintf("strand: %s — route walk %d from %d,%d to %d,%d (stuck %d, %d walks without progress, %d tiles of route at best)",
			name, v.hops, x, z, end.X, end.Z, v.stuck, v.flat, v.best))
	}
	return true
}

// routeLength is the tiles of a corner-compressed route: the Chebyshev length of each segment, summed.
func routeLength(wps []pathfind.NavPoint) int {
	n := 0
	for i := 1; i < len(wps); i++ {
		n += max(abs(wps[i].X-wps[i-1].X), abs(wps[i].Z-wps[i-1].Z))
	}
	return n
}

// LegWithin is the start of a corner-compressed route that stays within n tiles (Chebyshev) of its
// first waypoint, cutting the first segment that leaves that box at its edge. Route segments run in
// one of eight directions, so the cut point lies on the route.
func LegWithin(wps []pathfind.NavPoint, n int) []pathfind.NavPoint {
	if len(wps) == 0 {
		return nil
	}
	start := wps[0]
	out := []pathfind.NavPoint{start}
	for i := 1; i < len(wps); i++ {
		w := wps[i]
		if max(abs(w.X-start.X), abs(w.Z-start.Z)) <= n {
			out = append(out, w)
			continue
		}
		prev := out[len(out)-1]
		dx, dz := sgn(w.X-prev.X), sgn(w.Z-prev.Z)
		p := prev
		for max(abs(p.X+dx-start.X), abs(p.Z+dz-start.Z)) <= n && (p.X != w.X || p.Z != w.Z) {
			p.X += dx
			p.Z += dz
		}
		if p != prev {
			out = append(out, p)
		}
		break
	}
	return out
}

// targetFightsBack reports whether the player the account last clicked is in a fight facing it: the
// account's own hits land on the target, not on itself, so InCombat alone misses the first ticks.
func targetFightsBack(players []gamestate.Player, target, self int) bool {
	if target < 0 {
		return false
	}
	for _, p := range players {
		if p.Index == target {
			return p.InCombat && p.TargetsPlayer(self)
		}
	}
	return false
}
