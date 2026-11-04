package webwalk

import (
	"strings"
	"time"
)

// Wall faces, as Collision.WallMask's bits.
const (
	WallNorth byte = 1 << iota
	WallEast
	WallSouth
	WallWest
)

// WallClient is optional on a Client: walls that live loc changes add to what the collision
// pack baked. An opened door's leaves are the common case. Lumbridge castle's Large doors
// open onto 3216,3218 and 3216,3219 and block steps inside the hall that the pack allows, so
// a walk planned from the pack alone stalls there until the ladder escalates.
type WallClient interface {
	// ExtraWalls returns the wall faces live locs add within radius of at, keyed by tile,
	// with both sides of every wall set (as CollisionEngine.changeWall applies them).
	ExtraWalls(at Point, radius int) map[Point]byte
}

// AddWallFaces adds a wall loc's faces to walls, as Engine-TS CollisionEngine.changeWall:
// shape 0 (straight) and shape 2 (L) on angle 0 west, 1 north, 2 east, 3 south. Corner and
// diagonal shapes do not block orthogonal steps and are ignored.
func AddWallFaces(walls map[Point]byte, x, z, level, shape, angle int) {
	add := func(dx, dz int, faces byte) {
		p := Point{X: x + dx, Z: z + dz, Level: level}
		walls[p] |= faces
	}
	switch shape {
	case 0:
		switch angle & 3 {
		case 0:
			add(0, 0, WallWest)
			add(-1, 0, WallEast)
		case 1:
			add(0, 0, WallNorth)
			add(0, 1, WallSouth)
		case 2:
			add(0, 0, WallEast)
			add(1, 0, WallWest)
		case 3:
			add(0, 0, WallSouth)
			add(0, -1, WallNorth)
		}
	case 2:
		switch angle & 3 {
		case 0:
			add(0, 0, WallNorth|WallWest)
			add(-1, 0, WallEast)
			add(0, 1, WallSouth)
		case 1:
			add(0, 0, WallNorth|WallEast)
			add(0, 1, WallSouth)
			add(1, 0, WallWest)
		case 2:
			add(0, 0, WallSouth|WallEast)
			add(1, 0, WallWest)
			add(0, -1, WallNorth)
		case 3:
			add(0, 0, WallSouth|WallWest)
			add(0, -1, WallNorth)
			add(-1, 0, WallEast)
		}
	}
}

// orthBlocked reports whether one orthogonal step from p is walled by extra.
func orthBlocked(extra map[Point]byte, p Point, dx, dz int) bool {
	to := Point{X: p.X + dx, Z: p.Z + dz, Level: p.Level}
	switch {
	case dz > 0:
		return extra[p]&WallNorth != 0 || extra[to]&WallSouth != 0
	case dz < 0:
		return extra[p]&WallSouth != 0 || extra[to]&WallNorth != 0
	case dx > 0:
		return extra[p]&WallEast != 0 || extra[to]&WallWest != 0
	case dx < 0:
		return extra[p]&WallWest != 0 || extra[to]&WallEast != 0
	}
	return false
}

// wallBox bounds the tiles of an extra-walls map, so a search can skip StepWalled's map lookups
// for every tile nowhere near one. Live walls only cover liveWallRadius around the player while a
// long search expands thousands of tiles far from it: on 4 Oct those lookups were a fifth of the
// planner's time, and the planner most of the control panel's CPU.
type wallBox struct {
	ok                     bool
	minX, maxX, minZ, maxZ int
}

func wallBoxOf(extra map[Point]byte) wallBox {
	var b wallBox
	for p := range extra {
		if !b.ok {
			b = wallBox{ok: true, minX: p.X, maxX: p.X, minZ: p.Z, maxZ: p.Z}
			continue
		}
		b.minX, b.maxX = min(b.minX, p.X), max(b.maxX, p.X)
		b.minZ, b.maxZ = min(b.minZ, p.Z), max(b.maxZ, p.Z)
	}
	return b
}

// near reports whether a step from p can touch a wall in the box: StepWalled reads only p and the
// tiles one step from it.
func (b wallBox) near(p Point) bool {
	return b.ok && p.X >= b.minX-1 && p.X <= b.maxX+1 && p.Z >= b.minZ-1 && p.Z <= b.maxZ+1
}

// StepWalled reports whether a step of (dx, dz) from p crosses one of the extra walls. A
// diagonal step needs both of its orthogonal halves clear from either side, as the engine's
// step validation does.
func StepWalled(extra map[Point]byte, p Point, dx, dz int) bool {
	if len(extra) == 0 {
		return false
	}
	if dx == 0 || dz == 0 {
		return orthBlocked(extra, p, dx, dz)
	}
	return orthBlocked(extra, p, dx, 0) || orthBlocked(extra, p, 0, dz) ||
		orthBlocked(extra, Point{X: p.X + dx, Z: p.Z, Level: p.Level}, 0, dz) ||
		orthBlocked(extra, Point{X: p.X, Z: p.Z + dz, Level: p.Level}, dx, 0)
}

const (
	// liveWallRadius covers the loaded scene around the player.
	liveWallRadius = 60
	// learnedBlockFor is how long a step the server refused stays out of plans.
	learnedBlockFor = 2 * time.Minute
)

// liveWalls asks a WallClient for the walls live locs add around the player.
func (w *Walker) liveWalls(at Point) map[Point]byte {
	wc, ok := w.Client.(WallClient)
	if !ok {
		return nil
	}
	return wc.ExtraWalls(at, liveWallRadius)
}

// blockedSteps are the steps this Walker saw the server refuse within learnedBlockFor.
func (w *Walker) blockedSteps() map[[2]Point]bool {
	if len(w.learned) == 0 {
		return nil
	}
	now := w.Client.Now()
	out := map[[2]Point]bool{}
	for s, at := range w.learned {
		if now.Sub(at) > learnedBlockFor {
			delete(w.learned, s)
			continue
		}
		out[s] = true
	}
	return out
}

// learnBlockedStep records a step the player stalled on although the plan allowed it, so the
// next plans route around it for learnedBlockFor. Anything the live walls do not model (a
// scripted barrier, a loc the index lacks) is caught here after stallsToLearn stalls in a row
// (lag.go: the first stalls of a walk are often a click that was read late).
// An orthogonal step that is refused means a wall on that face, so the two diagonals through
// the same face are learned with it; a refused diagonal is learned alone, since either of its
// halves could be the blocked one.
func (w *Walker) learnBlockedStep(from, to Point) {
	if w.learned == nil {
		w.learned = map[[2]Point]time.Time{}
	}
	now := w.Client.Now()
	dx, dz := to.X-from.X, to.Z-from.Z
	targets := []Point{to}
	switch {
	case dx == 0 && dz != 0:
		targets = append(targets, Point{X: to.X - 1, Z: to.Z, Level: to.Level}, Point{X: to.X + 1, Z: to.Z, Level: to.Level})
	case dz == 0 && dx != 0:
		targets = append(targets, Point{X: to.X, Z: to.Z - 1, Level: to.Level}, Point{X: to.X, Z: to.Z + 1, Level: to.Level})
	}
	for _, t := range targets {
		w.learned[[2]Point{from, t}] = now
	}
	w.logf("learned that %s -> %s is blocked live; planning around it for %v", from, to, learnedBlockFor)
}

// LiveDoorClient is optional on a Client: doors and gates a player shut live, where the static
// map has them open (or has no door at all), offered as hops across the walls ExtraWalls adds.
//
// Without it a shut door the graph does not know is only a wall, and a wall on the only way
// in seals the destination. Live on 2026-09-26 from 12:40, Aubury's shop door in Varrock
// (statically open, 1531 at 3253,3399) was repeatedly shut to 1530 at 3253,3398: every plan
// made within liveWallRadius of it answered "no path: unreachable", so about 70 ScarabSwarm
// accounts walked from Ardougne to Varrock, failed their first replan inside the house at
// 3236-3240,3406 and walked back without buying.
type LiveDoorClient interface {
	// LiveDoorEdges returns the crossings (both directions) of the live-added straight wall
	// locs within radius of at, on at's level, that offer Open.
	LiveDoorEdges(at Point, radius int) []*Edge
}

// LiveDoorEdges returns the two crossings of one straight (shape 0) wall loc that offers
// action (Open), as the graph's own door edges: stand on one side, open, step to the other.
// The sides follow AddWallFaces: angle 0 walls the west face, 1 north, 2 east, 3 south. Any
// other shape returns nil; an L or corner door is not something a single step crosses.
func LiveDoorEdges(x, z, level, shape, angle, locID int, name, action string) []*Edge {
	if shape != 0 || locID < 0 {
		return nil
	}
	at := Point{X: x, Z: z, Level: level}
	beyond := at
	switch angle & 3 {
	case 0:
		beyond.X--
	case 1:
		beyond.Z++
	case 2:
		beyond.X++
	case 3:
		beyond.Z--
	}
	kind := KindDoor
	if strings.Contains(strings.ToLower(name), "gate") {
		kind = KindGate
	}
	mk := func(from, to Point) *Edge {
		return &Edge{From: from, To: to, Kind: kind, Cost: DefaultCost(kind), LocName: name,
			Action: action, LocID: locID, OpenLocID: -1, LocX: x, LocZ: z}
	}
	return []*Edge{mk(at, beyond), mk(beyond, at)}
}

// liveDoors asks a LiveDoorClient for the live-shut doors around the player.
func (w *Walker) liveDoors(at Point) []*Edge {
	lc, ok := w.Client.(LiveDoorClient)
	if !ok {
		return nil
	}
	return lc.LiveDoorEdges(at, liveWallRadius)
}
