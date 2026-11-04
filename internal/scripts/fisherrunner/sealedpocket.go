package fisherrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// ---- sealed same-level pocket recovery ----
//
// Real, live-found gap this file closes (2026-09-16, production accounts "acct63" and
// "acct32" — confirmed from their own live /api/status entries, both "bot":
// "ProgressiveFisher"): ReturnToCatherby's own WalkNear attempts and its LegPier fallback
// (see runner.go, just above/below the call into tryEscapeSealedPocket below) only ever
// cover two shapes of stranding — an ordinary nearby pathfind hiccup, and a genuine
// cross-water displacement freshstartrunner's pickpocket+boat route can resolve. Neither
// covers a small, genuinely disconnected SAME-LEVEL interior pocket (mapsquare 40,50, an
// ordinary Ardougne Monastery ground floor — not one of freshstart's cross-water legs at
// all, so TravelLeg never reports LegPier and travelViaFreshStart never even gets tried):
// acct32's own log line "[guardian] drifted far from home while waiting" (which only
// fires when emergencyFleeDriftTiles > 0 — this package's own WaitForGuardian call, not
// freshstartrunner's, which hardcodes it to 0) confirms this package's own WalkNear was
// genuinely driving the account when it froze here, not the fresh-start fallback.
//
// This is the identical incident (same mapsquare, same door chain) that motivated
// internal/scripts/freshstartrunner/runner.go's own tryEscapeSealedPocket — see that
// function's doc comment for the full ground-truthed evidence (cmd/doordiag against the
// real collision pack): the account's own sealed room (2611-2620,3204-3214, 61 tiles) has
// a real "Door" (id 1530) at 2610,3209 whose far side is only a SECOND sealed room
// (2602-2610,3207-3218, 64 tiles), and THAT room's own real exit is a second "Door" (id
// 1530) at 2606,3219, whose far side is a genuine 109,716-tile outdoor component. Every
// runner package in this project keeps its own copy of this kind of stranding/door-search
// machinery rather than sharing it (see WalkNear's own doc comment above on why
// freshstartrunner/barbarianrunner/mulerunner each tune their own WalkNear independently) —
// this file is fisherrunner's own copy of exactly that approach, adapted to the primitives
// this package already has (its own plain WalkNear in place of walkAdjacent, since this
// package's WalkNear has no door-recovery of its own to recurse into — unlike
// freshstartrunner's walkNearCore, there is no stack-overflow risk here needing a split
// "recovery-disabled" walker).
//
// Ranks candidate rooms by raw connected-component SIZE, not distance to a destination
// that may not even be on the same landmass — see largeComponentThreshold's own doc
// comment for why that discriminates a small interior room from the real outdoor world
// reliably, and tryEscapeSealedPocket's own doc comment (mirrored from
// freshstartrunner's) for the full reasoning this file's logic was built from.

// maxPocketChainHops bounds how many doors in a row tryEscapeSealedPocket's own room-BFS
// will cross while hunting for the real way out of a small, disconnected interior —
// matches freshstartrunner.tryEscapeSealedPocket's own identical constant (see its doc
// comment): generous enough for the deepest real incident ground-truthed so far (two
// closed doors in a row) with real headroom, without letting a dense multi-door complex
// explode into an unbounded search.
const maxPocketChainHops = 6

// pocketChainRoomCap bounds localComponent's own per-room flood-fill size during the chain
// search below — matches freshstartrunner's identical constant: large enough to fully
// measure any real interior room/suite ground-truthed so far, while far cheaper than
// flood-filling an entire real outdoor landmass (109,716+ tiles) for every intermediate
// room tried.
const pocketChainRoomCap = 4000

// largeComponentThreshold is how big a room's own raw ExitMask-connected component has to
// be before tryEscapeSealedPocket treats it as "the real outdoor world" rather than just
// another interior room worth chaining through — matches freshstartrunner's identical
// constant, ground-truthed against the same incident: sealed interior pockets/rooms range
// from 24 to 244 tiles, while a real outdoor component is 109,716 tiles, several orders of
// magnitude apart.
const largeComponentThreshold = 2000

// pocketChainDoorRadius is how far past a room's own bounding box the door search looks for
// the door/gate that leads out of it — matches freshstartrunner's identical constant.
const pocketChainDoorRadius = 20

// doorNames/doorOps mirror freshstartrunner's own identical vars (see its doc comment):
// this era's content is not consistent about a door/gate's exact display name, and "Open"
// covers every closed door/gate confirmed live so far against a real baked loc index.
var doorNames = []string{"Door", "Gate", "Large door", "Iron gate", "Wooden door", "Metal door"}
var doorOps = []string{"Open"}

// doorOpenOps are the OPENED-state ops for the same door names above — used only to
// recognize "this fixture is already open, just walk through it," never to click (clicking
// "Close" would toggle a door someone else just opened back shut). Matches
// freshstartrunner's identical var.
var doorOpenOps = []string{"Close"}

// doorChainOps is every op the door search below tries on each name in doorNames — both
// the closed-door "Open" ops and the already-open "Close" ops, so a chain search never
// misses a door only because it happens to already be open.
var doorChainOps = append(append([]string{}, doorOps...), doorOpenOps...)

// doorApproach lists a door's four orthogonal neighbours, tried in turn.
var doorApproach = [4]struct{ dx, dz int }{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}

// componentDx/componentDz are the 8 directions localComponent expands through, matching
// pathfind.Pack's own ExitMask bit order (see internal/pathfind/pack.go and
// freshstartrunner's identical arrays).
var componentDx = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var componentDz = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

// doorChainHop is one link in the door chain tryEscapeSealedPocket's own room-BFS finds —
// the door/gate loc crossed, the tile just inside it (approached from to open it) and the
// tile just outside it (in the next room), so the walk phase can retrace the exact same
// chain the search already verified room-by-room.
type doorChainHop struct {
	match locindex.Match
	from  tile.Tile
	to    tile.Tile
}

// roomBBox returns the smallest box containing every tile in room — used both to bound the
// next door search and to recognize a room already visited, rather than comparing the full
// tile sets themselves.
func roomBBox(room map[tile.Tile]bool) [4]int {
	first := true
	var bbox [4]int
	for t := range room {
		if first {
			bbox = [4]int{t.X, t.Z, t.X, t.Z}
			first = false
			continue
		}
		if t.X < bbox[0] {
			bbox[0] = t.X
		}
		if t.Z < bbox[1] {
			bbox[1] = t.Z
		}
		if t.X > bbox[2] {
			bbox[2] = t.X
		}
		if t.Z > bbox[3] {
			bbox[3] = t.Z
		}
	}
	return bbox
}

// localComponent returns every tile reachable from start by walking through open ExitMask
// edges only (no doors/stairs — the same raw-collision view cmd/doordiag's own connected-
// component check uses offline), capped at maxTiles so a large open area can't make this
// expensive.
func localComponent(pk *pathfind.Pack, start tile.Tile, maxTiles int) map[tile.Tile]bool {
	seen := map[tile.Tile]bool{start: true}
	queue := []tile.Tile{start}
	for len(queue) > 0 && len(seen) < maxTiles {
		cur := queue[0]
		queue = queue[1:]
		mask := pk.ExitMask(cur.X, cur.Z, cur.Level)
		for i := 0; i < 8; i++ {
			if mask&(1<<uint(i)) == 0 {
				continue
			}
			n := tile.Tile{X: cur.X + componentDx[i], Z: cur.Z + componentDz[i], Level: cur.Level}
			if seen[n] || !pk.Walkable(n.X, n.Z, n.Level) {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return seen
}

// findDoorsBoundingRoom searches every door/gate name+op combination (see doorChainOps)
// within pocketChainDoorRadius of room's own bounding box, centered and sized off that box
// rather than a single tile — a real room can be many tiles across, and its exit doors can
// sit anywhere on its boundary. Deduplicates by (X, Z, ID) since the same physical door can
// match more than one name/op combination search.
func findDoorsBoundingRoom(d *Deps, live []locindex.Change, bbox [4]int, level int) []locindex.Match {
	cx := (bbox[0] + bbox[2]) / 2
	cz := (bbox[1] + bbox[3]) / 2
	halfW := (bbox[2] - bbox[0]) / 2
	halfH := (bbox[3] - bbox[1]) / 2
	radius := halfW
	if halfH > radius {
		radius = halfH
	}
	radius += pocketChainDoorRadius
	type key struct {
		x, z, id int
	}
	seen := map[key]bool{}
	var out []locindex.Match
	for _, name := range doorNames {
		for _, op := range doorChainOps {
			for _, m := range d.Locs.Find(d.Config, locindex.Query{X: cx, Z: cz, Level: level, Radius: radius, Name: name, Op: op}, live) {
				k := key{m.X, m.Z, m.ID}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// tryEscapeSealedPocket looks for a real chain of doors/gates leading out of a small,
// genuinely disconnected interior — fisherrunner's own copy of
// freshstartrunner.tryEscapeSealedPocket, ported for the identical live incident (see this
// file's own package-level doc comment above): a real multi-room interior where no single
// nearby fixture leads directly back to the wider world, but a real, walkable chain of two
// or more doors does. Ranks candidate rooms by connected-component SIZE (see
// largeComponentThreshold's own doc comment) rather than distance to a destination that may
// not even be on the same landmass, since every real interior room ground-truthed so far
// (24-244 tiles) is several orders of magnitude smaller than any real outdoor area
// (109,716 tiles).
//
// Tried only once, from ReturnToCatherby, precisely because a real multi-room BFS (walking
// every nearby door's far side, not just picking one) costs more than is worth paying on
// every ordinary WalkNear iteration — this package's own WalkNear already gave up several
// times before ReturnToCatherby ever calls this. The first room-chain search whose final
// component clears largeComponentThreshold is treated as "found the real world" and its
// exact door chain is walked and opened in order; a genuinely sealed complex (every
// reachable room via every door still small, out to maxPocketChainHops) returns false,
// falling through to the LegPier / travelViaFreshStart fallback unchanged.
func tryEscapeSealedPocket(d *Deps, from tile.Tile) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	startRoom := localComponent(d.Pack, from, pocketChainRoomCap)
	if len(startRoom) >= largeComponentThreshold {
		// Not actually a small sealed pocket at all (e.g. called on an ordinary open-world
		// stranding for some other reason) — nothing for this function to usefully do.
		return false
	}
	type queued struct {
		bbox  [4]int
		chain []doorChainHop
	}
	startBBox := roomBBox(startRoom)
	visited := map[[4]int]bool{startBBox: true}
	queue := []queued{{startBBox, nil}}
	var winner []doorChainHop
search:
	for hop := 0; hop < maxPocketChainHops && len(queue) > 0; hop++ {
		var nextQueue []queued
		for _, q := range queue {
			// Re-derive this room's own tile set fresh each time rather than carrying it
			// alongside queue — only the bbox is kept across iterations, so this is the one
			// place that needs the full set, cheaply recomputed from any tile already known
			// to be inside it.
			var anyTile tile.Tile
			for t := range startRoom {
				anyTile = t
				break
			}
			if hop > 0 {
				// q is not the start room; recover a representative tile from its own
				// chain's last hop instead.
				anyTile = q.chain[len(q.chain)-1].to
			}
			room := localComponent(d.Pack, anyTile, pocketChainRoomCap)
			for _, m := range findDoorsBoundingRoom(d, live, q.bbox, from.Level) {
				var insideTile, outsideTile tile.Tile
				foundInside, foundOutside := false, false
				for _, off := range doorApproach {
					cand := tile.Tile{X: m.X + off.dx, Z: m.Z + off.dz, Level: from.Level}
					if !d.Pack.Walkable(cand.X, cand.Z, cand.Level) {
						continue
					}
					if room[cand] {
						insideTile, foundInside = cand, true
					} else if !foundOutside {
						outsideTile, foundOutside = cand, true
					}
				}
				if !foundInside || !foundOutside {
					continue
				}
				nextRoom := localComponent(d.Pack, outsideTile, pocketChainRoomCap)
				nextBBox := roomBBox(nextRoom)
				if visited[nextBBox] {
					continue
				}
				visited[nextBBox] = true
				chain := append(append([]doorChainHop{}, q.chain...), doorChainHop{match: m, from: insideTile, to: outsideTile})
				if len(nextRoom) >= largeComponentThreshold {
					winner = chain
					break search
				}
				nextQueue = append(nextQueue, queued{nextBBox, chain})
			}
		}
		queue = nextQueue
	}
	if winner == nil {
		return false
	}
	d.logf("pocket escape: found a %d-door chain to a real %d+-tile area — walking it", len(winner), largeComponentThreshold)
	for _, hop := range winner {
		if !WalkNear(d, hop.from, 0) {
			d.logf("pocket escape: could not reach %q at %d,%d,%d to open it", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level)
			return false
		}
		if err := actions.OpLoc(d.Session, hop.match.Op, hop.match.X, hop.match.Z, hop.match.ID); err != nil {
			d.logf("pocket escape: OpLoc on %q: %v", hop.match.Name, err)
			return false
		}
		if !d.Delay(900 * time.Millisecond) {
			return false
		}
		// Real bug, ground-truthed against production account "acct82" (2026-09-17,
		// mapsquare 48,49 — a real sealed room with real doors near 3105,3160): this used to
		// be a second WalkNear(d, hop.to, 0) call here, routing the crossing step through
		// FindPath exactly like the approach walk above. That's wrong for this one specific
		// step — internal/pathfind's collision Pack is a static file baked once from the
		// map's own default (closed) door state and has no live-override hook at all. The
		// OpLoc call just above genuinely opens the door server-side, but FindPath keeps
		// reporting the edge it just crossed as unreachable forever, no matter how long this
		// waits, so every fisher account whose own FishUntil/ReturnToCatherby escalation
		// reached this door-chain escape found a real, walkable chain and then could never
		// actually finish crossing it.
		//
		// freshstartrunner's own copy of this exact escape routine already carries this fix
		// (commit bf6eec7, "Fix tryEscapeSealedPocket crossing a just-opened door via
		// FindPath", ground-truthed there against account "acct14"). This file's own copy
		// was ported from the pre-fix version (commit d5c9000, earlier the same night) and
		// never got the follow-up — confirmed live against a reproduction of acct82's own
		// stranding (cmd/scratchfishpockettest, teleported into the identical pocket and
		// driven through the real registered ProgressiveFisher bot): it opened the correct
		// door every time and then failed this exact WalkNear(hop.to) call every time, never
		// once landing. Fixed the same way: one raw two-waypoint actions.Walk that bypasses
		// FindPath entirely for the single edge just opened — the same MOVE_GAMECLICK a real
		// player's own next click sends.
		here := currentTile(d)
		if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}, {X: hop.to.X, Z: hop.to.Z, Level: hop.to.Level}}, false); err != nil {
			d.logf("pocket escape: stepping through %q at %d,%d,%d: %v", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level, err)
			return false
		}
		if !waitFor(d, "the step through the door chain", 5*time.Second, func() bool {
			cur := currentTile(d)
			return cur.X == hop.to.X && cur.Z == hop.to.Z
		}) {
			d.logf("pocket escape: opened %q at %d,%d,%d but the step through to %d,%d,%d did not land", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level, hop.to.X, hop.to.Z, hop.to.Level)
			return false
		}
	}
	if !d.Delay(1 * time.Second) {
		return false
	}
	return true
}
