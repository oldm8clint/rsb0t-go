package gamestate

import "rsb0t-go/internal/packet"

// HINT_ARROW: the yellow arrow the server points at an NPC, a tile or a player (tutorials,
// quests, and the Brimhaven agility arena's active pillar, which upstream reads through
// reader.hintTile). Ported from Client.ts: type:u1, then for type 1 an NPC index (u2), for types
// 2-6 a tile x (u2), z (u2) and height (u1) (the client folds 2-6, which only differ in where
// on the tile the arrow sits, into 2), and for type 10 a player index (u2). Any other type,
// including 0, means no arrow.

type hintArrow struct {
	kind         int
	npc, player  int
	tileX, tileZ int
}

const (
	hintNpc    = 1
	hintTile   = 2
	hintPlayer = 10
)

func (s *State) applyHintArrow(r *packet.Packet) {
	h := hintArrow{kind: int(r.G1())}
	switch {
	case h.kind == hintNpc && r.Available() >= 2:
		h.npc = int(r.G2())
	case h.kind >= 2 && h.kind <= 6 && r.Available() >= 5:
		h.kind = hintTile
		h.tileX = int(r.G2())
		h.tileZ = int(r.G2())
		r.G1() // height
	case h.kind == hintPlayer && r.Available() >= 2:
		h.player = int(r.G2())
	default:
		h = hintArrow{}
	}
	s.hint = h
}

// HintTile ports reader.hintTile: the tile a tile hint arrow points at, on the current level.
func (s *State) HintTile() (x, z, level int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hint.kind != hintTile || s.hint.tileX <= 0 || s.hint.tileZ <= 0 {
		return 0, 0, 0, false
	}
	return s.hint.tileX, s.hint.tileZ, s.level, true
}

// HintNpc returns the index of the NPC a hint arrow points at.
func (s *State) HintNpc() (index int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hint.npc, s.hint.kind == hintNpc
}

// HintPlayer returns the index of the player a hint arrow points at.
func (s *State) HintPlayer() (index int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hint.player, s.hint.kind == hintPlayer
}
