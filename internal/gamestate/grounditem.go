package gamestate

import (
	"rsb0t-go/internal/packet"
)

// GroundItem is a snapshot of one item stack lying on the ground (a dropped item, a
// monster drop, a resource spawn), ported from Client.ts's this.groundObj[level][x][z]
// linked list. Like locs, ground items ride the same zone-scoped packet family: a tile
// can hold several stacks, including several of the same item id with different counts
// (e.g. two players each drop the same item), so they are addressed as a plain list per
// tile rather than a single slot the way LocInstance addresses one loc per layer.
type GroundItem struct {
	ID    int
	Count int
	X, Z  int
	Level int
}

// groundItemKey addresses one tile's ground-item list, mirroring locKey's level/x/z
// addressing (ground items have no layer).
type groundItemKey struct {
	level, x, z int
}

// applyObjAdd ports the OBJ_ADD branch of zonePacket (Client.ts ~line 7431): a new stack
// appears on a tile, immediately visible (e.g. dropped by us, or a fresh spawn).
func (s *State) applyObjAdd(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	id := int(r.G2())
	count := int(r.G2())
	key := groundItemKey{level: s.level, x: x, z: z}
	s.groundItems[key] = append(s.groundItems[key], GroundItem{ID: id, Count: count, X: x, Z: z, Level: s.level})
}

// applyObjDel ports the OBJ_DEL branch: removes the first stack on the tile matching the
// item id (masked with 0x7fff, matching Client.ts's own comparison), mirroring the
// client's LinkList unlink-on-first-match.
func (s *State) applyObjDel(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	id := int(r.G2()) & 0x7fff
	key := groundItemKey{level: s.level, x: x, z: z}
	items := s.groundItems[key]
	for i, it := range items {
		if it.ID == id {
			s.groundItems[key] = append(items[:i:i], items[i+1:]...)
			break
		}
	}
	if len(s.groundItems[key]) == 0 {
		delete(s.groundItems, key)
	}
}

// applyObjCount ports the OBJ_COUNT branch: a stack's count changes (partial pickup, or
// more of the same item landing on an existing stack). The server addresses the stack by
// its current (id, old count) pair, since a tile can hold several stacks of the same item
// with different counts at once.
func (s *State) applyObjCount(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	id := int(r.G2()) & 0x7fff
	oldCount := int(r.G2())
	newCount := int(r.G2())
	key := groundItemKey{level: s.level, x: x, z: z}
	for i, it := range s.groundItems[key] {
		if it.ID == id && it.Count == oldCount {
			s.groundItems[key][i].Count = newCount
			break
		}
	}
}

// applyObjReveal ports the OBJ_REVEAL branch: a stack becomes visible to everyone (a
// drop's private-to-the-dropper visibility window has ended). Skipped when pid is us,
// matching Client.ts's `pid !== this.selfSlot` guard — our own OBJ_ADD already tracked it
// when we dropped it.
func (s *State) applyObjReveal(r *packet.Packet) {
	pos := r.G1()
	x, z := s.zoneTile(pos)
	id := int(r.G2())
	count := int(r.G2())
	pid := int(r.G2())
	if pid == s.SelfSlot {
		return
	}
	key := groundItemKey{level: s.level, x: x, z: z}
	s.groundItems[key] = append(s.groundItems[key], GroundItem{ID: id, Count: count, X: x, Z: z, Level: s.level})
}

// GroundItems returns a snapshot of every currently-known ground item stack, copied out
// from under the lock the same way Locs() does — never a reference to internal state.
func (s *State) GroundItems() []GroundItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]GroundItem, 0, len(s.groundItems))
	for _, items := range s.groundItems {
		out = append(out, items...)
	}
	return out
}
