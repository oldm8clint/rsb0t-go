package essencerunner

import (
	"math/rand"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/npcsim"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE REAL HALLS. The click is sent from beside the NPC; these tests ask the real collision pack (RSB0T_COLLISION_PACK) what that
// means in the three rooms the gateways stand in: the Wizards' Guild's hall, Aubury's shop and Wizard Cromperty's house. Each NPC
// wanders (Gateway.Wander tiles from where he is spawned, a new destination in that square one tick in eight, Npc.wanderMode), the
// rooms are full of furniture, and the account stops wherever its walk to "within two tiles of his stand" leaves it.

var (
	realOnce sync.Once
	realPack *pathfind.Pack
	realErr  error
)

func loadRealPack(t testing.TB) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("set RSB0T_COLLISION_PACK")
	}
	realOnce.Do(func() { realPack, realErr = pathfind.Load(path) })
	if realErr != nil {
		t.Fatal(realErr)
	}
	return realPack
}

func reachable(pk *pathfind.Pack, from, to tile.Tile) bool {
	res := pk.FindPath(navPoint(from), navPoint(to))
	if !res.OK || len(res.Waypoints) == 0 {
		return false
	}
	last := res.Waypoints[len(res.Waypoints)-1]
	return last.X == to.X && last.Z == to.Z
}

// wanderTiles is every tile an NPC spawned at gw.Stand can be standing on: within gw.Wander of the spawn on both axes, walkable, and
// reachable from the spawn over the pack. (A superset of where he really goes: Aubury and Cromperty are also bound to roofed tiles.)
func wanderTiles(pk *pathfind.Pack, gw essenceplan.Gateway) []tile.Tile {
	var out []tile.Tile
	for x := gw.Stand.X - gw.Wander; x <= gw.Stand.X+gw.Wander; x++ {
		for z := gw.Stand.Z - gw.Wander; z <= gw.Stand.Z+gw.Wander; z++ {
			at := tile.Tile{X: x, Z: z}
			if pk.Walkable(x, z, 0) && reachable(pk, gw.Stand, at) {
				out = append(out, at)
			}
		}
	}
	return out
}

// arrivalTiles is where the walk to "within two tiles of his stand" can leave the account: every tile within two of the stand that
// the account can walk to from the stand.
func arrivalTiles(pk *pathfind.Pack, gw essenceplan.Gateway) []tile.Tile {
	var out []tile.Tile
	for x := gw.Stand.X - 2; x <= gw.Stand.X+2; x++ {
		for z := gw.Stand.Z - 2; z <= gw.Stand.Z+2; z++ {
			at := tile.Tile{X: x, Z: z}
			if pk.Walkable(x, z, 0) && reachable(pk, gw.Stand, at) {
				out = append(out, at)
			}
		}
	}
	return out
}

// For every tile an NPC can wander to, and every tile the old walk could leave the account on, the engine's rule has a tile beside
// him that the account can walk to; the nearest such stand is one the engine takes the click from; and stepping onto it makes the
// account beside him. So "step beside him" always has somewhere to go, whatever the hall's walls and tables are.
func TestEveryTileTheGatewayNpcsWanderToHasAStandTheAccountCanWalkTo(t *testing.T) {
	pk := loadRealPack(t)
	for _, gw := range essenceplan.Gateways() {
		t.Run(gw.Key, func(t *testing.T) {
			npcs, arrivals := wanderTiles(pk, gw), arrivalTiles(pk, gw)
			if len(npcs) < 10 || len(arrivals) < 3 {
				t.Fatalf("%d wander tiles and %d arrival tiles around %v: the fixture is wrong", len(npcs), len(arrivals), gw.Stand)
			}
			pairs, beside, stepped, longest := 0, 0, 0, 0
			for _, at := range npcs {
				for _, here := range arrivals {
					pairs++
					reached, stands := ApproachStands(pk, here, at)
					if reached {
						beside++
						if !reachesNpc(pk, here, at) {
							t.Errorf("%v is reported beside %v and does not reach him", here, at)
						}
						continue
					}
					if len(stands) == 0 {
						t.Errorf("%s on %v from %v: no tile beside him at all", gw.NPC, at, here)
						continue
					}
					s := stands[0]
					if !reachable(pk, here, s) {
						t.Errorf("%s on %v from %v: the nearest stand %v cannot be walked to", gw.NPC, at, here, s)
						continue
					}
					if !reachesNpc(pk, s, at) {
						t.Errorf("%s on %v: the stand %v does not reach him", gw.NPC, at, s)
					}
					if res := pk.FindPath(navPoint(here), navPoint(s)); res.Steps() > longest {
						longest = res.Steps()
					}
					stepped++
				}
			}
			t.Logf("%s: %d tiles he can be on x %d tiles the account can stand on = %d cases: already beside him in %d (%.0f%%), a step needed in %d (the longest %d tiles)",
				gw.Key, len(npcs), len(arrivals), pairs, beside, 100*float64(beside)/float64(pairs), stepped, longest)
			if stepped == 0 {
				t.Error("the account is beside him wherever he stands: the live failures cannot have happened")
			}
			if longest > 30 {
				t.Errorf("a step of %d tiles: a stand should be a short walk away", longest)
			}
		})
	}
}

// THE LIVE FAILURE, ON THE REAL GUILD. 11:24 on 14 Oct 2026: the walk to Distentor's stand left the account on the tile inside the
// Guild's east door (2596,3088), within two tiles of the stand; four clicks from there, all four unanswered. From that tile the engine
// takes a click on him on only a few of the tiles he wanders to, so the old click failed on most of them.
func TestFromTheGuildDoorTileTheOldClickReachesHimOnFewOfTheTilesHeWandersTo(t *testing.T) {
	pk := loadRealPack(t)
	gw := gateway(t, essenceplan.GatewayGuild)
	tiles := wanderTiles(pk, gw)
	reached := 0
	var where []tile.Tile
	for _, at := range tiles {
		if reachesNpc(pk, guildDoor, at) {
			reached++
			where = append(where, at)
		}
	}
	t.Logf("from the door tile %v the engine takes a click on Wizard Distentor on %d of the %d tiles he can be on: %v", guildDoor, reached, len(tiles), where)
	if len(tiles) < 30 {
		t.Fatalf("only %d wander tiles: the fixture is wrong", len(tiles))
	}
	if reached*10 > len(tiles) {
		t.Errorf("the door tile reaches him on %d of %d tiles (more than a tenth): the live failures are not explained", reached, len(tiles))
	}
}

// ---- a wandering NPC ----

// wanderer moves an NPC the way the engine's wander mode does: one tick in eight a new destination in the square of range tiles
// around its spawn, then a tile a tick toward it along the pack's route (and a new destination replaces the old one at once).
type wanderer struct {
	rng    *rand.Rand
	e      *npcsim.Engine
	pk     *pathfind.Pack
	index  int
	spawn  tile.Tile
	rangeN int
	// region is the tiles he can be on (wanderTiles).
	region map[tile.Tile]bool
	at     tile.Tile
	route  []tile.Tile
	moves  int
}

func sign(a, b int) int {
	switch {
	case b > a:
		return 1
	case b < a:
		return -1
	}
	return 0
}

// routeTo is the tiles from w.at to d, one king move each.
func (w *wanderer) routeTo(d tile.Tile) []tile.Tile {
	res := w.pk.FindPath(navPoint(w.at), navPoint(d))
	if !res.OK || len(res.Waypoints) < 2 {
		return nil
	}
	var out []tile.Tile
	cur := w.at
	for _, wp := range res.Waypoints[1:] {
		for cur.X != wp.X || cur.Z != wp.Z {
			cur = tile.Tile{X: cur.X + sign(cur.X, wp.X), Z: cur.Z + sign(cur.Z, wp.Z)}
			out = append(out, cur)
		}
	}
	return out
}

func (w *wanderer) tick() {
	if w.rng.Intn(8) == 0 {
		d := tile.Tile{X: w.spawn.X + w.rng.Intn(2*w.rangeN+1) - w.rangeN, Z: w.spawn.Z + w.rng.Intn(2*w.rangeN+1) - w.rangeN}
		if d != w.at && w.region[d] {
			w.route = w.routeTo(d)
		}
	}
	if len(w.route) == 0 {
		return
	}
	w.at, w.route = w.route[0], w.route[1:]
	w.moves++
	w.e.MoveNPC(w.index, w.at)
}

// settle runs the wanderer for n ticks on its own.
func (w *wanderer) settle(n int) {
	for i := 0; i < n; i++ {
		w.e.Step()
	}
}

// startWandering gives the rig's NPC a wanderer over the rig's pack and starts it moving with the engine's ticks.
func (r *rig) startWandering(seed int64, spawn tile.Tile, rangeN int, region map[tile.Tile]bool) *wanderer {
	w := &wanderer{rng: rand.New(rand.NewSource(seed)), e: r.e, pk: r.d.Pack, index: rigNpcIndex, spawn: spawn, rangeN: rangeN, region: region, at: spawn}
	r.e.OnTick = func(int) { w.tick() }
	return w
}

// oldTeleport is the click as it was before the step: read the NPC, click, wait for the landing with the four-tick resend and
// no step ever (batch2-1007, 72a7057c).
func oldTeleport(r *rig) TeleportResult {
	d, gw := r.d, r.gw
	npc, ok := FindGateway(d, gw)
	if !ok {
		return TeleportResult{Reason: "not in sight"}
	}
	clickAt, tick0 := time.Now(), d.Session.State().Tick()
	send := func(index int) bool { return actions.OpNpc(d.Session, r.op, index) == nil }
	if !send(npc.Index) {
		return TeleportResult{Reason: "click"}
	}
	reclick := func(resend) (chatMark, bool) {
		again, ok := FindGateway(d, gw)
		return markChat(d), ok && send(again.Index)
	}
	return awaitLanding(d, gw, clickAt, tick0, 100*time.Millisecond, reclick)
}

// THE CLICK LANDS WHILE HE WANDERS. On each real hall, for many seeds: the NPC wanders on his own (the strict server takes a click
// only from a touching tile, and drops a click while the account is in its cast), the account starts on a tile the old walk leaves
// it on, and the teleport must land -- every time -- with the click that landed taken from beside him. The same seeds run through
// the old click (no step) fail on most.
func TestTheClickLandsWhileTheNpcWandersOnTheRealHalls(t *testing.T) {
	pk := loadRealPack(t)
	for _, gw := range essenceplan.Gateways() {
		t.Run(gw.Key, func(t *testing.T) {
			arrivals := arrivalTiles(pk, gw)
			if gw.Key == essenceplan.GatewayGuild {
				arrivals = []tile.Tile{guildDoor} // where the live failures stood
			}
			region := map[tile.Tile]bool{}
			for _, at := range wanderTiles(pk, gw) {
				region[at] = true
			}
			const seeds = 40
			landed, oldLanded, clicks, maxClicks, walks := 0, 0, 0, 0, 0
			var ticks []int
			for seed := int64(1); seed <= seeds; seed++ {
				rng := rand.New(rand.NewSource(seed))
				from := arrivals[rng.Intn(len(arrivals))]
				for _, old := range []bool{false, true} {
					r := newRig(t, gw.Key, pk, from, gw.Stand)
					w := r.startWandering(seed, gw.Stand, gw.Wander, region)
					w.settle(20 + rng.Intn(60)) // he has been wandering for a while
					var res TeleportResult
					if old {
						res = oldTeleport(r)
						if res.Landed {
							oldLanded++
						}
						continue
					}
					res = teleportIn(r.d, r.gw, TeleportOptions{}, 2*time.Second)
					if !res.Landed {
						t.Errorf("seed %d from %v (he is on %v): %+v\nlog:\n%s", seed, from, w.at, res, r.log())
						continue
					}
					landed++
					clicks += res.Clicks
					if res.Clicks > maxClicks {
						maxClicks = res.Clicks
					}
					walks += len(r.e.Walks())
					ticks = append(ticks, res.Ticks)
					if got := taken(r.e.Clicks()); len(got) != 1 || !touches(got[0].From, got[0].NPC) {
						t.Errorf("seed %d: clicks taken %+v", seed, r.e.Clicks())
					}
				}
			}
			sort.Ints(ticks)
			t.Logf("%s: %d of %d teleports landed (the old click, same seeds: %d of %d); %.1f clicks and %.1f walks each, at most %d clicks; ticks median %d, slowest %d",
				gw.Key, landed, seeds, oldLanded, seeds, float64(clicks)/float64(max(landed, 1)), float64(walks)/float64(max(landed, 1)), maxClicks, ticks[len(ticks)/2], ticks[len(ticks)-1])
			if landed != seeds {
				t.Errorf("%d of %d teleports landed", landed, seeds)
			}
			if gw.Key == essenceplan.GatewayGuild && oldLanded*3 > seeds {
				t.Errorf("the old click landed %d of %d from the door tile: the test does not reproduce the live failures", oldLanded, seeds)
			}
		})
	}
}
