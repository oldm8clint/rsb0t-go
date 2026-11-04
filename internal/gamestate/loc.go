package gamestate

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// locShapeToLayer ports LocShape.ts's LOC_SHAPE_TO_LAYER table exactly: shape codes
// 0-3 are walls (layer 0), 4-8 wall decorations (layer 1), 9-21 ground-level objects —
// the layer scripts overwhelmingly care about, trees/bank booths/furnaces/obstacles are
// all here — (layer 2), and 22 ground decoration (layer 3).
var locShapeToLayer = [23]int{
	0, 0, 0, 0,
	1, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	3,
}

// locKey addresses one loc slot: a tile can carry a wall, a wall decoration, a ground
// object, and ground decoration simultaneously, on any of 4 levels.
type locKey struct {
	level, x, z, layer int
}

// LocInstance is a snapshot of one visible loc (a placed scenery object — trees, bank
// booths, doors, agility obstacles), in absolute world coordinates. Type resolves to a
// name/interaction-options via a cache lookup this client doesn't have yet (see the
// plan file's C2 notes) — callers match on ID for now.
type LocInstance struct {
	Type   int
	Shape  int
	Rotate int
	X, Z   int
	Level  int
	// Deleted marks a LOC_DEL: the server emptied this tile's layer, which hides a loc
	// the map files place there (a picked flax, an opened gate). Type is -1. Locs()
	// leaves these out; LocChanges() includes them for internal/locindex to merge.
	Deleted bool
}

// applyZoneFollows ports UPDATE_ZONE_(FULL|PARTIAL)_FOLLOWS: both just record the 8x8
// zone base that every LOC_ADD_CHANGE/LOC_DEL/OBJ_* packet arriving afterward resolves
// its tile offset against; FULL additionally clears whatever locs and ground items this
// client had recorded in that zone, since a full zone re-load means "forget what was
// here, it's coming again if it still exists" (Client.ts clears both this.locChanges and
// this.groundObj in the same FULL_FOLLOWS branch, ~line 7284).
func (s *State) applyZoneFollows(r *packet.Packet, full bool) {
	s.zoneUpdateX = int(r.G1())
	s.zoneUpdateZ = int(r.G1())
	if !full {
		return
	}
	// Why: locs/ground items are keyed by absolute tile while zoneUpdateX/Z are
	// build-area local, so the zone's bounds are shifted by the build origin before
	// comparing.
	minX, minZ := s.mapBuildBaseX+s.zoneUpdateX, s.mapBuildBaseZ+s.zoneUpdateZ
	for key := range s.locs {
		if key.level == s.level && key.x >= minX && key.x < minX+8 && key.z >= minZ && key.z < minZ+8 {
			delete(s.locs, key)
		}
	}
	for key := range s.groundItems {
		if key.level == s.level && key.x >= minX && key.x < minX+8 && key.z >= minZ && key.z < minZ+8 {
			delete(s.groundItems, key)
		}
	}
}

// zoneOpSizes are the payload lengths of the zone sub-packets UPDATE_ZONE_PARTIAL_ENCLOSED
// can carry (ServerProt.ts sizes), so the ones not decoded here can be skipped.
var zoneOpSizes = map[byte]int{
	serverprot.LOC_ADD_CHANGE: 4,
	serverprot.LOC_DEL:        2,
	serverprot.LOC_ANIM:       4,
	serverprot.OBJ_ADD:        5,
	serverprot.OBJ_DEL:        3,
	serverprot.OBJ_COUNT:      7,
	serverprot.OBJ_REVEAL:     7,
	serverprot.MAP_ANIM:       6,
	serverprot.MAP_PROJANIM:   15,
	serverprot.P_LOCMERGE:     14,
}

// applyZoneEnclosed ports UPDATE_ZONE_PARTIAL_ENCLOSED: a zone base followed by zone
// sub-packets until the payload ends. The engine sends every live loc add, change and
// delete this way (Zone.ts queues them as ENCLOSED events), so without it a picked flax
// or an opened door would never be seen.
func (s *State) applyZoneEnclosed(r *packet.Packet) {
	s.zoneUpdateX = int(r.G1())
	s.zoneUpdateZ = int(r.G1())
	for r.Available() > 0 {
		op := r.G1()
		size, ok := zoneOpSizes[op]
		if !ok || r.Available() < size {
			return
		}
		switch op {
		case serverprot.LOC_ADD_CHANGE:
			s.applyLocAddChange(r)
		case serverprot.LOC_DEL:
			s.applyLocDel(r)
		case serverprot.MAP_PROJANIM:
			s.applyEffect(r, true)
		case serverprot.MAP_ANIM:
			s.applyEffect(r, false)
		case serverprot.OBJ_ADD:
			s.applyObjAdd(r)
		case serverprot.OBJ_DEL:
			s.applyObjDel(r)
		case serverprot.OBJ_COUNT:
			s.applyObjCount(r)
		case serverprot.OBJ_REVEAL:
			s.applyObjReveal(r)
		default:
			r.Skip(size)
		}
	}
}

// zoneTile decodes a zone-packet's leading position byte into absolute world
// coordinates, ported from Client.ts's zonePacket: the low nibble pair addresses a
// tile within the current 8x8 zone, added to zoneUpdateX/Z (local, build-area-relative)
// and then to the build area's own absolute origin.
func (s *State) zoneTile(pos byte) (x, z int) {
	localX := s.zoneUpdateX + int((pos>>4)&0x7)
	localZ := s.zoneUpdateZ + int(pos&0x7)
	return s.mapBuildBaseX + localX, s.mapBuildBaseZ + localZ
}

// applyLocAddChange ports the LOC_ADD_CHANGE branch of zonePacket.
func (s *State) applyLocAddChange(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	info := r.G1()
	id := int(r.G2())
	shape := int(info >> 2)
	rotate := int(info & 0x3)
	if shape < 0 || shape >= len(locShapeToLayer) {
		return
	}
	layer := locShapeToLayer[shape]
	s.locs[locKey{level: s.level, x: x, z: z, layer: layer}] = &LocInstance{
		Type: id, Shape: shape, Rotate: rotate, X: x, Z: z, Level: s.level,
	}
}

// applyLocDel ports the LOC_DEL branch of zonePacket: removes whatever loc occupies
// that tile's layer, matching the client's own locChangeCreate(..., id=-1, ...) clear.
func (s *State) applyLocDel(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	info := r.G1()
	shape := int(info >> 2)
	if shape < 0 || shape >= len(locShapeToLayer) {
		return
	}
	layer := locShapeToLayer[shape]
	s.locs[locKey{level: s.level, x: x, z: z, layer: layer}] = &LocInstance{
		Type: -1, Shape: shape, Rotate: int(info & 0x3), X: x, Z: z, Level: s.level, Deleted: true,
	}
}

// Locs returns a snapshot of every currently-known loc the server added or changed.
func (s *State) Locs() []LocInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LocInstance, 0, len(s.locs))
	for _, l := range s.locs {
		if !l.Deleted {
			out = append(out, *l)
		}
	}
	return out
}

// LocChanges returns every live loc record, including LOC_DEL tombstones, for merging
// over the static loc index (internal/locindex.ChangesFrom).
func (s *State) LocChanges() []LocInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LocInstance, 0, len(s.locs))
	for _, l := range s.locs {
		out = append(out, *l)
	}
	return out
}
