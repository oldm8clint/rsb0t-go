package randomevents

// A jump the account made ON PURPOSE (teleport-walks, 13 Oct 2026: scarabrunner.Travel casts a travel teleport before a
// walk the planner prices as slower on foot; 14 Oct 2026: spirit trees, the runecraft ruins and portals, the essence mine's
// teleporters and portals, the Holy Grail whistle).
//
// watchDisplacement (displacement.go) reads every jump it did not see coming. A deliberate one looks like one: Camelot,
// Varrock and Falador land in mapsquares the fail-teleport enum lists, Lumbridge lands on the respawn tile, and Ardougne
// (mapsquare 41,51) is in no table at all, so it reads UNEXPLAINED, and twelve accounts landing unexplained in one mapsquare
// within 90 seconds teach the mass learner a twelve-hour STRAND SITE there (massThreshold). A burst of Ardougne casts would have
// taught the Guardian that the East Ardougne market is a place the server dumps cohorts. The 14 Oct log (04:05-04:45, build
// d5362bb2) held 179 such lines for jumps nobody should have to explain: 151 spirit-tree hops, ten Air and Body altar ruins and
// portals, a dozen essence-mine teleports, the Titan whistle. So whoever makes the jump says what it is about to do, and the
// watcher, which still samples the tile and still writes its line, reads a jump that lands where it was told to expect one as
// the fixture it is and never counts it toward a site.
//
// Per session, short-lived, and bypassing only the label: ClassifyDisplacement stays pure, and a real fail-teleport that lands
// somewhere else in the same seconds is still reported as one.

import (
	"sync"
	"time"

	"rsb0t-go/internal/actions"
)

// ExpectedTeleportName is the Name a displacement carries when it matched a travel teleport's expectation (the default of
// ExpectTeleport; every other announcer names itself).
const ExpectedTeleportName = "travel teleport (scarabrunner.Travel)"

// expectedTeleportNear is how close to the announced landing a jump has to end to count as it: the engine sets a teleport
// down within two tiles of the square (map_findsquare, radius 2), so three covers it with a tile to spare.
const expectedTeleportNear = 3

// Landing is a box of tiles an announced jump may end in. The watcher reads a position, so the box carries no level.
type Landing struct{ MinX, MaxX, MinZ, MaxZ int }

// LandingNear is the tiles within r of x,z.
func LandingNear(x, z, r int) Landing {
	return Landing{MinX: x - r, MaxX: x + r, MinZ: z - r, MaxZ: z + r}
}

// LandingSquare is the whole mapsquare mx,mz: where a jump lands somewhere in a region (the Rune essence mine's teleporters set
// an account down on one of several tiles of its square).
func LandingSquare(mx, mz int) Landing {
	return Landing{MinX: mx << 6, MaxX: mx<<6 + 63, MinZ: mz << 6, MaxZ: mz<<6 + 63}
}

// Contains reports whether the tile is in the box.
func (l Landing) Contains(x, z int) bool {
	return x >= l.MinX && x <= l.MaxX && z >= l.MinZ && z <= l.MaxZ
}

type expectedTeleport struct {
	id       uint64
	name     string
	landings []Landing
	until    time.Time
}

var expectedTeleports = struct {
	mu        sync.Mutex
	by        map[actions.Session][]expectedTeleport
	next      uint64
	lastSweep time.Time
}{by: map[actions.Session][]expectedTeleport{}}

// expectedSweepEvery is how often a call drops the expired expectations of EVERY session, so that an account that announced once
// and stopped (a logout, a world hop) does not keep its session alive in this map.
const expectedSweepEvery = 30 * time.Second

// sweepExpectedLocked drops every expired expectation. Called with the lock held.
func sweepExpectedLocked(now time.Time) {
	if now.Sub(expectedTeleports.lastSweep) < expectedSweepEvery {
		return
	}
	expectedTeleports.lastSweep = now
	for s, list := range expectedTeleports.by {
		live := list[:0]
		for _, e := range list {
			if now.Before(e.until) {
				live = append(live, e)
			}
		}
		if len(live) == 0 {
			delete(expectedTeleports.by, s)
		} else {
			expectedTeleports.by[s] = live
		}
	}
}

// ExpectTeleport announces that session is about to cast a teleport that lands at x,z, for ttl. Call it just before the
// cast is sent. Expired expectations are dropped on the next call.
func ExpectTeleport(session actions.Session, x, z int, ttl time.Duration) {
	ExpectLandings(session, ExpectedTeleportName, ttl, LandingNear(x, z, expectedTeleportNear))
}

// ExpectLandings announces that session is about to make a jump that ends in one of landings, for ttl, and names it for the
// log line ("expected: name"). One announcement is one jump: the first jump that ends in any of the landings uses it up. The
// returned func withdraws it (the jump was refused or never came); calling it after the jump was seen is harmless.
func ExpectLandings(session actions.Session, name string, ttl time.Duration, landings ...Landing) (withdraw func()) {
	if session == nil || ttl <= 0 || len(landings) == 0 {
		return func() {}
	}
	now := time.Now()
	expectedTeleports.mu.Lock()
	defer expectedTeleports.mu.Unlock()
	sweepExpectedLocked(now)
	expectedTeleports.next++
	id := expectedTeleports.next
	live := expectedTeleports.by[session][:0]
	for _, e := range expectedTeleports.by[session] {
		if now.Before(e.until) {
			live = append(live, e)
		}
	}
	expectedTeleports.by[session] = append(live, expectedTeleport{id: id, name: name, landings: landings, until: now.Add(ttl)})
	return func() { withdrawExpected(session, id) }
}

func withdrawExpected(session actions.Session, id uint64) {
	expectedTeleports.mu.Lock()
	defer expectedTeleports.mu.Unlock()
	list := expectedTeleports.by[session]
	live := list[:0]
	for _, e := range list {
		if e.id != id {
			live = append(live, e)
		}
	}
	if len(live) == 0 {
		delete(expectedTeleports.by, session)
	} else {
		expectedTeleports.by[session] = live
	}
}

// ExpectHop announces a hop of the walk graph, operated at from and landing at to (a spirit tree, a portal, a cave), named for
// the log. It is ExpectLandings with one filter: a hop the classifier already reads as the ordinary thing it is (a ladder or a
// staircase, a dungeon entrance and its exit, a ship's deck, a catalogued fixture) or one a walk could have covered is left
// alone, so those lines keep their more exact wording. What is left, a jump the classifier would call a fail-teleport, the
// spawn point, UNEXPLAINED or a strand site, is announced. A strand site is on the list because a hop INTO one is the walker's
// own doing, not a summon: every account that climbs out of Sedridor's basement lands in the Wizards' Tower west room, which the
// log called a place "the server summons whole cohorts" into twelve times in forty minutes.
func ExpectHop(session actions.Session, name string, fromX, fromZ, fromLevel, toX, toZ, toLevel int, ttl time.Duration) (withdraw func()) {
	d, moved := ClassifyDisplacement(fromX, fromZ, fromLevel, toX, toZ, toLevel, pollInterval)
	if !moved {
		return func() {}
	}
	switch d.Kind {
	case DisplacementFailTeleport, DisplacementSpawnPoint, DisplacementUnexplained, DisplacementStrandSite:
	default:
		return func() {}
	}
	return ExpectLandings(session, name, ttl, LandingNear(toX, toZ, expectedTeleportNear))
}

// ExpectedJumps names the jumps session has announced and the Guardian has not yet seen, in the order announced (for tests and
// diagnostics). Expired announcements are not listed.
func ExpectedJumps(session actions.Session) []string {
	now := time.Now()
	expectedTeleports.mu.Lock()
	defer expectedTeleports.mu.Unlock()
	var out []string
	for _, e := range expectedTeleports.by[session] {
		if now.Before(e.until) {
			out = append(out, e.name)
		}
	}
	return out
}

// ForgetTeleports drops every expectation of session (the account is gone, or the cast was refused).
func ForgetTeleports(session actions.Session) {
	expectedTeleports.mu.Lock()
	delete(expectedTeleports.by, session)
	expectedTeleports.mu.Unlock()
}

// takeExpectedTeleport reports whether a jump that ended at x,z was announced, and uses the expectation up: one announcement is
// one jump. name is the announcer's own.
func takeExpectedTeleport(session actions.Session, x, z int, now time.Time) (name string, hit bool) {
	expectedTeleports.mu.Lock()
	defer expectedTeleports.mu.Unlock()
	list := expectedTeleports.by[session]
	live := list[:0]
	for _, e := range list {
		if !now.Before(e.until) {
			continue
		}
		if !hit {
			for _, l := range e.landings {
				if l.Contains(x, z) {
					hit, name = true, e.name
					break
				}
			}
			if hit {
				continue
			}
		}
		live = append(live, e)
	}
	if len(live) == 0 {
		delete(expectedTeleports.by, session)
	} else {
		expectedTeleports.by[session] = live
	}
	return name, hit
}
