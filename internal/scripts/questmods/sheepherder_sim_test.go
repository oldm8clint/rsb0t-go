package questmods

// Sheep Herder's push loop against a simulation of the content's sheep on the real collision pack (RSB0T_COLLISION_PACK; skipped
// without it). The simulation is the engine's, line for line where the content and the engine are short enough to copy:
//
//   - A Prod runs from a tile orthogonally beside the sheep with no wall between; from anywhere else the engine closes the distance with
//     its NAIVE approach (Environment.node.clientRoutefinder: Player.naivePathToTarget), which pathfind.Pack.NaiveApproach replays.
//   - prod_sheep: the sheep's mode goes to NONE and it is told to walk ONE tile along coord_direction(player, sheep) (away from the
//     player; the longer axis of the offset, east or west on a tie, north or south when dz is longer); the jump over the fence is read from
//     the sheep's coordinate BEFORE it moves.
//   - ai_timer fires every 10 to 29 ticks (the script sets npc_settimer(10 + random(20)); the config's first is 25) and
//     npc_setmode(null) returns the sheep to WANDER, whose every tick has a 1 in 8 chance of queueing a waypoint within wanderrange 3 of its
//     spawn (Npc.wanderMode): the sheep walks back the way it came between pushes.
//   - A walk is one tile a tick, two when running.
//
// The assertion is the one the loop exists for: from each of the four sheep's fields, with the three copies standing as the map puts
// them, the account herds a sheep over the fence inside the push budget and a quarter of an hour of game time.

import (
	"math/rand"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
)

type simSheep struct {
	x, z         int
	spawn        questplan.HerdPos
	wander       bool // WANDER mode; false is NONE
	wx, wz       int  // the queued waypoint
	hasWP        bool
	timer        int // ticks until ai_timer
	penned       bool
	penX, penZ   int
	pennedAtTick int
}

type simHerd struct {
	t      *testing.T
	pk     *pathfind.Pack
	grid   packHerdGrid
	rng    *rand.Rand
	tick   int
	limit  int
	meX    int
	meZ    int
	sheep  []*simSheep
	n      int
	prods  int
	refuse int
	log    bool
}

func newSimHerd(t *testing.T, pk *pathfind.Pack, n int, seed int64) *simHerd {
	s := &simHerd{t: t, pk: pk, grid: packHerdGrid{pk: pk}, rng: rand.New(rand.NewSource(seed)), n: n, limit: 1500}
	// The map puts three copies of each sheep within a tile of the middle one.
	mid := questplan.HerdSheepSpawns[n-1]
	for _, off := range [][2]int{{-1, 0}, {0, -1}, {0, 1}} {
		x, z := mid.X+off[0], mid.Z+off[1]
		if n == 2 {
			x, z = mid.X+off[0], mid.Z+off[1]
		}
		s.sheep = append(s.sheep, &simSheep{x: x, z: z, spawn: questplan.HerdPos{X: x, Z: z}, timer: 25,
			penX: questplan.HerdPenSpots[n-1].X, penZ: questplan.HerdPenSpots[n-1].Z})
	}
	// The account starts a few tiles from the first copy, on foot.
	s.meX, s.meZ = mid.X-5, mid.Z-3
	for !pk.Walkable(s.meX, s.meZ, 0) {
		s.meX++
	}
	return s
}

// step advances every sheep by one game tick.
func (s *simHerd) step() {
	s.tick++
	for _, sh := range s.sheep {
		if sh.penned {
			continue
		}
		sh.timer--
		if sh.timer <= 0 {
			sh.wander = true // npc_setmode(null)
			sh.timer = 10 + s.rng.Intn(20)
		}
		if sh.wander && s.rng.Float64() < 0.125 {
			dx := int(float64(s.rng.Float64()*6 - 3 + 0.5))
			dz := int(float64(s.rng.Float64()*6 - 3 + 0.5))
			wx, wz := sh.spawn.X+dx, sh.spawn.Z+dz
			if wx != sh.x || wz != sh.z {
				sh.wx, sh.wz, sh.hasWP = wx, wz, true
			}
		}
		if sh.hasWP {
			to, moved := s.pk.EngineStep(pathfind.NavPoint{X: sh.x, Z: sh.z}, pathfind.NavPoint{X: sh.wx, Z: sh.wz})
			if moved && !s.occupied(to.X, to.Z, sh) {
				sh.x, sh.z = to.X, to.Z
			}
			if sh.x == sh.wx && sh.z == sh.wz || !moved {
				sh.hasWP = false
			}
		}
	}
}

func (s *simHerd) occupied(x, z int, except *simSheep) bool {
	for _, o := range s.sheep {
		if o != except && !o.penned && o.x == x && o.z == z {
			return true
		}
	}
	return x == s.meX && z == s.meZ
}

func (s *simHerd) advance(ticks int) {
	for i := 0; i < ticks; i++ {
		s.step()
	}
}

func (s *simHerd) Copies() []questplan.HerdPos {
	var out []questplan.HerdPos
	for _, sh := range s.sheep {
		if sh.penned {
			out = append(out, questplan.HerdPos{X: sh.penX, Z: sh.penZ})
		} else {
			out = append(out, questplan.HerdPos{X: sh.x, Z: sh.z})
		}
	}
	return out
}

// Find picks the copy with the shortest push route (they are all in view in the simulation).
func (s *simHerd) Find() (questplan.HerdPos, bool) {
	return questplan.HerdPick(s.grid, questplan.HerdGateZone, s.Copies())
}

// Approach walks to the sheep: the herding loop's first stand is walked to anyway, so a few tiles is all the simulation grants.
func (s *simHerd) Approach(p questplan.HerdPos) bool { return true }

func (s *simHerd) Me() (questplan.HerdPos, bool) { return questplan.HerdPos{X: s.meX, Z: s.meZ}, true }

// WalkTo walks the pack's route at two tiles a tick; a walk that does not arrive in 16 ticks has stopped.
func (s *simHerd) WalkTo(x, z int) bool {
	if s.meX == x && s.meZ == z {
		return true
	}
	res := s.pk.FindPath(pathfind.NavPoint{X: s.meX, Z: s.meZ}, pathfind.NavPoint{X: x, Z: z})
	if !res.OK {
		return false
	}
	wps := res.Waypoints
	idx := 0
	for t := 0; t < 16; t++ {
		for k := 0; k < 2; k++ {
			if s.meX == x && s.meZ == z {
				return true
			}
			for idx < len(wps) && wps[idx].X == s.meX && wps[idx].Z == s.meZ {
				idx++
			}
			if idx >= len(wps) {
				break
			}
			to, moved := s.pk.EngineStep(pathfind.NavPoint{X: s.meX, Z: s.meZ}, wps[idx])
			if !moved {
				s.advance(1)
				return s.meX == x && s.meZ == z
			}
			s.meX, s.meZ = to.X, to.Z
		}
		s.step()
	}
	return s.meX == x && s.meZ == z
}

// coordDirection is the content's ~coord_direction for (player, sheep).
func coordDirection(px, pz, sx, sz int) (int, int) {
	dx, dz := sx-px, sz-pz
	if dx > 0 {
		if abs(dz) > abs(dx) {
			if dz > 0 {
				return 0, 1
			}
			return 0, -1
		}
		return 1, 0
	}
	if abs(dz) > abs(dx) {
		if dz > 0 {
			return 0, 1
		}
		return 0, -1
	}
	return -1, 0
}

func (s *simHerd) Prod(at questplan.HerdPos) questplan.HerdProd {
	var target *simSheep
	for _, sh := range s.sheep {
		if !sh.penned && sh.x == at.X && sh.z == at.Z {
			target = sh
		}
	}
	if target == nil {
		return questplan.HerdProd{}
	}
	s.prods++
	// The approach: the engine's naive walk to the neighbour on the account's side, then the op runs.
	end, reached := s.pk.NaiveApproach(pathfind.NavPoint{X: s.meX, Z: s.meZ}, pathfind.NavPoint{X: at.X, Z: at.Z}, 30)
	steps := max(abs(end.X-s.meX), abs(end.Z-s.meZ))
	s.meX, s.meZ = end.X, end.Z
	s.advance(max(steps, 1))
	if !reached || target.x != at.X || target.z != at.Z {
		s.refuse++
		s.advance(10)
		return questplan.HerdProd{Sent: true}
	}
	dx, dz := coordDirection(s.meX, s.meZ, target.x, target.z)
	// prod_sheep: mode NONE, one tile along the direction, the jump read at the sheep's coordinate before it moves.
	target.wander = false
	target.wx, target.wz, target.hasWP = target.x+dx, target.z+dz, true
	if questplan.HerdGateZone.Contains(target.x, target.z) {
		target.penned = true
		target.pennedAtTick = s.tick
		s.advance(2)
		return questplan.HerdProd{Sent: true, Moved: true}
	}
	before := questplan.HerdPos{X: target.x, Z: target.z}
	for i := 0; i < 10; i++ {
		s.step()
		if target.x != before.X || target.z != before.Z {
			return questplan.HerdProd{Sent: true, Moved: true, After: questplan.HerdPos{X: target.x, Z: target.z}, Seen: true}
		}
	}
	return questplan.HerdProd{Sent: true}
}

func (s *simHerd) Wait(ticks int) { s.advance(ticks) }
func (s *simHerd) Stop() bool     { return s.tick > s.limit }
func (s *simHerd) Logf(format string, args ...any) {
	if s.log {
		s.t.Logf("[t%4d] "+format, append([]any{s.tick}, args...)...)
	}
}

// Every sheep of every field is herded over the fence on the real pack, whatever the wander does between the pushes.
func TestSheepHerderLoopPensEverySheepInTheSimulation(t *testing.T) {
	pk, _, _ := csData(t)
	grid := packHerdGrid{pk: pk}
	for n := 1; n <= 4; n++ {
		worst, total := 0, 0
		const seeds = 12
		for seed := int64(1); seed <= seeds; seed++ {
			s := newSimHerd(t, pk, n, seed*7919+int64(n))
			if _, ok := s.Find(); !ok {
				t.Fatalf("sheep %d: no copy has a push route to the gate zone", n)
			}
			res := questplan.HerdLeg(s, grid, questplan.HerdPen, questplan.HerdGateZone)
			if !res.Penned {
				t.Errorf("sheep %d seed %d: not penned after %d push(es) and %d ticks: %s (%d prods, %d refused)", n, seed, res.Pushes, s.tick, res.Why, s.prods, s.refuse)
				continue
			}
			total += s.tick
			worst = max(worst, s.tick)
		}
		t.Logf("sheep %d: penned in all %d runs, %d ticks on average, %d at worst", n, seeds, total/seeds, worst)
	}
}
