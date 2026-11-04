package questplan

import (
	"math/rand/v2"
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// A model of the bead farm, for the numbers the change is argued from. It is NOT a measurement: it plays the content's
// own imp rules (imp.rs2, the drop table, respawnrate 100) on the real collision pack with twelve accounts, a bot whose
// per-fight overhead is a parameter (0 is a bot with no reflexes at all), and the same policy knobs the change turns on.
// With no overhead at all the OLD policy (twelve accounts on the Ardougne strip, nearest imp, idle at the middle) gives
// 2.5 beads an account hour against the fleet's measured 2.9 or more (33 beads in 11.5 account hours, 16 Oct 2026
// 09:02-10:37, counting only what a provisioning line confirmed): the strip is short of imps, not of effort, which is the
// finding. The comparison between policies is what the model is for; the absolute figures are good to the 20 or 30 percent
// that the gap to the measurement shows.
//
// Run it with:  RSB0T_COLLISION_PACK=... go test -run TestImpFarmModel -v ./internal/scripts/questplan/
//
// What it models (one tick = 0.6 s):
//   - imps: 8 hitpoints, killed -> back on the spawn tile 100 ticks later; every 50-200 ticks half the time they teleport
//     to a random free tile within 20; a hit that does not kill teleports them one time in ten (imp.rs2).
//   - drops: one roll in 128 per kill; 5 each of black, red, white, yellow (imp.rs2). A bead is private to the killer for
//     100 ticks and then everyone's until it goes at 200 (engine Obj.ts, lootdrop_duration).
//   - accounts: see 15 tiles; move 1.5 tiles a tick; swing every 5 ticks, 90% to hit for 0..8; need one bead of each kind;
//     after four they hand in and start again (modelRestart ticks, the same for both policies).
//   - single combat: an imp one account is fighting is not a target.
//
// What it leaves out: the walker's doors (a pocket imp costs OLD a fixed detour), the monastery's monks, other players
// than these twelve, deaths, random events.

const (
	modelTicksPerHour = 6000
	modelHours        = 12
	modelSeeds        = 6
	modelAccounts     = 12
	modelVisible      = 15
	modelSpeed        = 1.5
	modelSwing        = 5
	modelRestart      = 1000
	modelDoorDetour   = 60
)

type modelPolicy struct {
	name          string
	twoSites      bool // spread over the sites by crowding, not all on the Ardougne strip
	reachableOnly bool // fight only imps over open ground
	patrol        bool // walk the spawns when none is in view
	scavenge      bool // take the beads needed from the ground, wherever they lie in view
}

type modelSite struct {
	site    ImpSite
	walk    []tile.Tile        // tiles an imp can teleport onto
	open    map[tile.Tile]bool // open ground from the stand
	pockets map[tile.Tile]bool // walkable, not open
}

type modelImp struct {
	site       int
	spawn      tile.Tile
	alive      bool
	at         tile.Tile
	hp         int
	nextTimer  int
	respawnAt  int
	lastCombat int
	engagedBy  int
}

type modelBead struct {
	at     tile.Tile
	kind   int
	killer int
	born   int
}

type modelAcct struct {
	site      int
	at        tile.Tile
	have      [4]bool
	busyUntil int
	state     int // 0 idle, 1 approaching, 2 fighting, 3 looting, 4 taking
	target    *modelImp
	goal      tile.Tile // where the approach was aimed
	bead      *modelBead
	patrolAt  int
	collected int
	startedAt int
}

func modelSites(t *testing.T, pk *pathfind.Pack) []modelSite {
	var out []modelSite
	for _, s := range ImpSites {
		ms := modelSite{site: s, open: map[tile.Tile]bool{}, pockets: map[tile.Tile]bool{}}
		reach := pk.ReachFrom(pathfind.NavPoint{X: s.Stand.X, Z: s.Stand.Z}, 70, nil)
		for x := s.Stand.X - 60; x <= s.Stand.X+60; x++ {
			for z := s.Stand.Z - 60; z <= s.Stand.Z+60; z++ {
				if !pk.Walkable(x, z, 0) {
					continue
				}
				tl := tile.Tile{X: x, Z: z}
				ms.walk = append(ms.walk, tl)
				if _, ok := reach.Steps(x, z, 0); ok {
					ms.open[tl] = true
				} else {
					ms.pockets[tl] = true
				}
			}
		}
		out = append(out, ms)
	}
	return out
}

type modelRun struct {
	rng     *rand.Rand
	sites   []modelSite
	pol     modelPolicy
	imps    []*modelImp
	accts   []*modelAcct
	beads   []*modelBead
	now     int
	secured int
	kills   int
}

func travelTicks(a, b tile.Tile) int {
	d := tile.Chebyshev(a, b)
	return int(float64(d)/modelSpeed) + 1
}

func (r *modelRun) rollDrop() int {
	n := r.rng.IntN(128)
	if n < 20 {
		return n / 5 // 0 black, 1 red, 2 white, 3 yellow
	}
	return -1
}

func (r *modelRun) teleportImp(im *modelImp) {
	ms := r.sites[im.site]
	for tries := 0; tries < 40; tries++ {
		c := ms.walk[r.rng.IntN(len(ms.walk))]
		if tile.Chebyshev(c, im.at) <= 20 {
			im.at = c
			return
		}
	}
}

func (r *modelRun) tickImps() {
	for _, im := range r.imps {
		if !im.alive {
			if r.now >= im.respawnAt {
				im.alive, im.at, im.hp, im.nextTimer, im.engagedBy = true, im.spawn, 8, r.now+70, -1
			}
			continue
		}
		if r.now >= im.nextTimer {
			if r.rng.IntN(2) == 0 && im.lastCombat+8 < r.now && im.engagedBy < 0 {
				r.teleportImp(im)
			}
			im.nextTimer = r.now + 50 + r.rng.IntN(151)
		}
	}
	// beads despawn at 200 ticks
	kept := r.beads[:0]
	for _, b := range r.beads {
		if r.now-b.born < 200 {
			kept = append(kept, b)
		}
	}
	r.beads = kept
}

func (r *modelRun) chooseSite(a *modelAcct) int {
	if !r.pol.twoSites {
		return 0
	}
	farmers := map[string]int{}
	for _, o := range r.accts {
		if o != a {
			farmers[ImpSites[o.site].ID]++
		}
	}
	pick := PickImpSite(farmers, tile.Tile{}, false)
	for i, s := range ImpSites {
		if s.ID == pick.ID {
			return i
		}
	}
	return 0
}

// target picks the imp to fight, nil when there is none in view that the policy would take.
func (r *modelRun) target(a *modelAcct) *modelImp {
	var best *modelImp
	bestD := 1 << 30
	for _, im := range r.imps {
		if !im.alive || im.site != a.site || im.engagedBy >= 0 {
			continue
		}
		d := tile.Chebyshev(a.at, im.at)
		if d > modelVisible {
			continue
		}
		if r.pol.reachableOnly {
			if !r.sites[a.site].open[im.at] || !r.sites[a.site].site.Has(im.at) {
				continue
			}
		}
		if d < bestD {
			best, bestD = im, d
		}
	}
	return best
}

func (r *modelRun) neededBead(a *modelAcct) *modelBead {
	var best *modelBead
	bestD := 1 << 30
	for _, b := range r.beads {
		if a.have[b.kind] {
			continue
		}
		// private to the killer for 100 ticks
		if b.killer != r.indexOf(a) && r.now-b.born < 100 {
			continue
		}
		d := tile.Chebyshev(a.at, b.at)
		if d > modelVisible || d > 24 {
			continue
		}
		if r.pol.reachableOnly && !r.sites[a.site].open[b.at] {
			continue
		}
		if d < bestD {
			best, bestD = b, d
		}
	}
	return best
}

func (r *modelRun) indexOf(a *modelAcct) int {
	for i, o := range r.accts {
		if o == a {
			return i
		}
	}
	return -1
}

func (r *modelRun) removeBead(b *modelBead) {
	for i, o := range r.beads {
		if o == b {
			r.beads = append(r.beads[:i], r.beads[i+1:]...)
			return
		}
	}
}

func (r *modelRun) finish(a *modelAcct) {
	got := 0
	for _, h := range a.have {
		if h {
			got++
		}
	}
	if got == 4 {
		a.have = [4]bool{}
		a.busyUntil = r.now + modelRestart
		a.state = 0
	}
}

func (r *modelRun) stepAcct(a *modelAcct, overhead int) {
	if r.now < a.busyUntil {
		return
	}
	idx := r.indexOf(a)
	switch a.state {
	case 0: // idle
		if r.pol.scavenge {
			if b := r.neededBead(a); b != nil {
				a.bead, a.state = b, 4
				a.busyUntil = r.now + travelTicks(a.at, b.at) + 2
				return
			}
		}
		if t := r.target(a); t != nil {
			a.target, a.goal, a.state = t, t.at, 1
			cost := travelTicks(a.at, t.at) + overhead
			if !r.sites[a.site].open[t.at] && r.sites[a.site].pockets[t.at] {
				cost += modelDoorDetour
			}
			a.busyUntil = r.now + cost
			return
		}
		// nothing in view
		ms := r.sites[a.site]
		if r.pol.patrol {
			sp := ms.site.Spawns[a.patrolAt%len(ms.site.Spawns)]
			a.patrolAt++
			a.busyUntil = r.now + travelTicks(a.at, sp) + 2
			a.at = sp
		} else {
			a.busyUntil = r.now + travelTicks(a.at, ms.site.Stand) + 5
			a.at = ms.site.Stand
		}
	case 1: // arrived where the imp was
		t := a.target
		if !t.alive || t.at != a.goal || t.engagedBy >= 0 {
			a.state, a.target = 0, nil
			a.busyUntil = r.now + 2
			return
		}
		t.engagedBy = idx
		a.at = t.at
		a.state = 2
		a.busyUntil = r.now + modelSwing
	case 2: // swing
		t := a.target
		if !t.alive {
			a.state, a.target = 0, nil
			return
		}
		t.lastCombat = r.now
		if r.rng.Float64() < 0.9 {
			t.hp -= r.rng.IntN(9)
		}
		if t.hp <= 0 {
			t.alive, t.respawnAt, t.engagedBy = false, r.now+100, -1
			r.kills++
			if k := r.rollDrop(); k >= 0 {
				r.beads = append(r.beads, &modelBead{at: t.at, kind: k, killer: idx, born: r.now})
			}
			a.state, a.target = 3, nil
			a.busyUntil = r.now + overhead/2 + 2
			return
		}
		if r.rng.IntN(10) == 0 { // a survived hit: the imp teleports
			t.engagedBy = -1
			r.teleportImp(t)
			a.target, a.goal, a.state = t, t.at, 1
			a.busyUntil = r.now + 5 + travelTicks(a.at, t.at)
			return
		}
		a.busyUntil = r.now + modelSwing
	case 3: // looting: the bead the kill dropped, if the account lacks that kind
		for _, b := range r.beads {
			if b.killer == idx && b.at == a.at && !a.have[b.kind] && r.now-b.born < 100+200 {
				a.have[b.kind] = true
				a.collected++
				r.secured++
				r.removeBead(b)
				break
			}
		}
		a.state = 0
		r.finish(a)
	case 4: // walked to a bead on the ground
		b := a.bead
		a.at = b.at
		still := false
		for _, o := range r.beads {
			if o == b {
				still = true
			}
		}
		if still && !a.have[b.kind] {
			a.have[b.kind] = true
			a.collected++
			r.secured++
			r.removeBead(b)
		}
		a.state = 0
		r.finish(a)
	}
}

// modelRunOnce plays modelHours hours with modelAccounts accounts and returns beads secured per account hour and kills an
// hour from all of them.
func modelRunOnce(sites []modelSite, pol modelPolicy, overhead int, seed uint64) (float64, float64) {
	r := &modelRun{rng: rand.New(rand.NewPCG(seed, 99)), sites: sites, pol: pol}
	for si, ms := range sites {
		if !pol.twoSites && si > 0 {
			break
		}
		for _, sp := range ms.site.Spawns {
			r.imps = append(r.imps, &modelImp{site: si, spawn: sp, alive: true, at: sp, hp: 8, nextTimer: r.rng.IntN(120), engagedBy: -1})
		}
	}
	for i := 0; i < modelAccounts; i++ {
		a := &modelAcct{}
		a.site = r.chooseSite(a)
		a.at = sites[a.site].site.Stand
		r.accts = append(r.accts, a)
	}
	total := modelTicksPerHour * modelHours
	warm := modelTicksPerHour // the first hour is not counted
	base, baseKills := 0, 0
	for r.now = 0; r.now < total; r.now++ {
		if r.now == warm {
			base, baseKills = r.secured, r.kills
		}
		r.tickImps()
		for _, a := range r.accts {
			r.stepAcct(a, overhead)
		}
	}
	return float64(r.secured-base) / float64(modelAccounts) / float64(modelHours-1), float64(r.kills-baseKills) / float64(modelHours-1)
}

func modelMean(sites []modelSite, pol modelPolicy, overhead int) (beads, kills float64) {
	for s := 0; s < modelSeeds; s++ {
		b, k := modelRunOnce(sites, pol, overhead, uint64(s+1))
		beads += b
		kills += k
	}
	return beads / modelSeeds, kills / modelSeeds
}

func TestImpFarmModel(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sites := modelSites(t, pk)
	old := modelPolicy{name: "old: one strip, nearest imp, idle at the middle"}
	spread := modelPolicy{name: "two sites only", twoSites: true}
	open := modelPolicy{name: "two sites + open ground only", twoSites: true, reachableOnly: true}
	patrol := modelPolicy{name: "two sites + open ground + patrol", twoSites: true, reachableOnly: true, patrol: true}
	full := modelPolicy{name: "new: two sites + open ground + patrol + scavenging", twoSites: true, reachableOnly: true, patrol: true, scavenge: true}

	for _, overhead := range []int{0, 10, 20} {
		t.Logf("bot overhead %d ticks (%.0f s) a fight:", overhead, float64(overhead)*0.6)
		results := map[string]float64{}
		for _, p := range []modelPolicy{old, spread, open, patrol, full} {
			beads, kills := modelMean(sites, p, overhead)
			results[p.name] = beads
			t.Logf("  %-56s %5.2f beads per account hour = %5.1f an hour from %d accounts (%4.0f kills an hour)",
				p.name, beads, beads*modelAccounts, modelAccounts, kills)
		}
		oldV, fullV := results[old.name], results[full.name]
		t.Logf("  modelled gain x%.2f; four beads take %.0f minutes where they took %.0f", fullV/oldV, 60*4/fullV, 60*4/oldV)
		if fullV < oldV*1.3 {
			t.Errorf("overhead %d: the new farm gives %.2f beads an account hour against the old one's %.2f, less than a 30%% gain", overhead, fullV, oldV)
		}
		if results[spread.name] < oldV {
			t.Errorf("overhead %d: spreading over two sites alone lowered the yield: %.2f against %.2f", overhead, results[spread.name], oldV)
		}
	}
}
