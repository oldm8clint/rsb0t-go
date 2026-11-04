package actionsim

// The held-item and ground-item half of the simulator: the backpack and worn tabs, a position,
// ground items, take (OPOBJ), drop (OPHELD5), item-on-item (OPHELDU) and remove (INV_BUTTON1 on
// the worn panel). Enough for a woodcutter's axe-head recovery to run end to end; each content
// rule a test needs is scripted by the test (Combine), never assumed.

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
)

// The fake sidebar tab roots: the backpack's child is actions.PlayerPack and the worn panel's is
// actions.PlayerWorn, the ids the real interfaces use.
const (
	PackTabRoot = 9100
	WornTabRoot = 9200
)

// TabComponents is Components plus the backpack and worn tabs. Use it with ShowTabs.
func TabComponents() archive.Components {
	c := Components()
	c[PackTabRoot] = &archive.Component{ID: PackTabRoot, Children: []int{actions.PlayerPack}}
	c[actions.PlayerPack] = &archive.Component{ID: actions.PlayerPack, LayerID: PackTabRoot, Type: archive.TypeInv, ObjOps: true}
	c[WornTabRoot] = &archive.Component{ID: WornTabRoot, Children: []int{actions.PlayerWorn}}
	c[actions.PlayerWorn] = &archive.Component{ID: actions.PlayerWorn, LayerID: WornTabRoot, Type: archive.TypeInv}
	return c
}

// ShowTabs loads the backpack (tab 3) and worn (tab 4) roots, as IF_SETICON does at login.
func (s *Session) ShowTabs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tab, root := range map[int]int{3: PackTabRoot, 4: WornTabRoot} {
		s.feed(serverprot.IF_SETICON, func(p *packet.Packet) { p.P2(uint16(root)); p.P1(byte(tab)) })
	}
	s.pushWorn()
}

// wornSlots is the worn panel's size; the right hand is slot 3.
const wornSlots = 14

// SetWearPos says which worn slot a wearable item goes to (content's oc_wearpos), so an OPHELD2 on it
// equips: the clicked pack slot is swapped with the worn one, and a displaced piece goes to the FIRST
// EMPTY pack slot (equip.rs2 try_equip: inv_movetoslot, then inv_movefromslot).
func (s *Session) SetWearPos(id int32, slot int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wearPos == nil {
		s.wearPos = map[int32]int{}
	}
	s.wearPos[id] = slot
}

// equip applies [opheld2,_] ~equip(last_slot) for a piece with a known wear position.
func (s *Session) equip(id int32, slot int) bool {
	pos, known := s.wearPos[id]
	if !known || !at(s.pack, slot, id) {
		return false
	}
	if s.worn == nil {
		s.worn = make([]Stack, wornSlots)
	}
	previous := s.worn[pos]
	s.worn[pos] = Stack{ID: id, Count: 1}
	s.pack[slot] = Stack{}
	if previous.ID != 0 {
		s.addPack(previous.ID, 1) // the first empty slot, as inv_movefromslot does
	}
	s.pushWorn()
	s.pushPack()
	return true
}

// RightHand is the worn slot an axe (and an axe handle) occupies.
const RightHand = 3

// Wear puts id in worn slot.
func (s *Session) Wear(slot int, id int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worn == nil {
		s.worn = make([]Stack, wornSlots)
	}
	s.worn[slot] = Stack{ID: id, Count: 1}
	s.pushWorn()
}

// Worn is the worn panel's rows, empty slots dropped.
func (s *Session) Worn() []Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nonEmpty(s.worn)
}

func (s *Session) pushWorn() {
	if s.worn == nil {
		s.worn = make([]Stack, wornSlots)
	}
	rows := append([]Stack(nil), s.worn...)
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.PlayerWorn, rows) })
}

// StandAt puts the player on world tile (x, z) at level 0: a REBUILD_NORMAL whose origin keeps
// the tile in the middle of the scene, then a PLAYER_INFO teleport there.
func (s *Session) StandAt(x, z int) { s.StandAtLevel(x, z, 0) }

// StandAtLevel is StandAt on the given level (0 to 3): a ladder's climb lands on the same x,z a level up.
func (s *Session) StandAtLevel(x, z, level int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.here = [2]int{x, z}
	zx, zz := x>>3, z>>3
	s.origin = [2]int{(zx - 6) << 3, (zz - 6) << 3}
	s.feed(serverprot.REBUILD_NORMAL, func(p *packet.Packet) { p.P2(uint16(zx)); p.P2(uint16(zz)) })
	var b bits
	b.put(1, 1)                     // has an update
	b.put(2, 3)                     // teleport
	b.put(2, uint32(level))         // level
	b.put(7, uint32(x-s.origin[0])) // local x
	b.put(7, uint32(z-s.origin[1])) // local z
	b.put(1, 0)                     // jump
	b.put(1, 0)                     // no extended info
	b.put(8, 0)                     // no other players
	b.put(11, 2047)                 // end of the add list
	s.st.Handle(serverprot.PLAYER_INFO, b.buf)
}

// DropOnGround puts one id on world tile (x, z), as obj_add does.
func (s *Session) DropOnGround(id int32, x, z int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objAdd(id, x, z)
}

// Ground is every ground item the simulator holds, as [id, x, z].
func (s *Session) Ground() [][3]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][3]int(nil), s.ground...)
}

func (s *Session) zone(x, z int) {
	lx, lz := x-s.origin[0], z-s.origin[1]
	s.feed(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, func(p *packet.Packet) { p.P1(byte(lx &^ 7)); p.P1(byte(lz &^ 7)) })
}

func (s *Session) objAdd(id int32, x, z int) {
	s.zone(x, z)
	s.feed(serverprot.OBJ_ADD, func(p *packet.Packet) {
		p.P1(byte((x&7)<<4 | (z & 7)))
		p.P2(uint16(id))
		p.P2(1)
	})
	s.ground = append(s.ground, [3]int{int(id), x, z})
}

func (s *Session) objDel(i int) {
	g := s.ground[i]
	s.zone(g[1], g[2])
	s.feed(serverprot.OBJ_DEL, func(p *packet.Packet) {
		p.P1(byte((g[1]&7)<<4 | (g[2] & 7)))
		p.P2(uint16(g[0]))
	})
	s.ground = append(s.ground[:i], s.ground[i+1:]...)
}

// Combine scripts an item-on-item rule both ways round, as content's [opheldu] pairs do: using a
// on b (or b on a) takes one of each and adds one result.
func (s *Session) Combine(a, b, result int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.combine == nil {
		s.combine = map[[2]int32]int32{}
	}
	s.combine[[2]int32{a, b}] = result
	s.combine[[2]int32{b, a}] = result
}

// Bury scripts content's [opheld1,_bones]: an OPHELD1 on one of xp10's items deletes it from the pack and advances stat by that many
// tenths of an experience point, which the server reports whole in UPDATE_STAT (stat_advance). startXP is the stat's experience
// now, reported at once so that the client has read it.
func (s *Session) Bury(stat, startXP int, xp10 map[int32]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bury, s.buryStat, s.buryXP10 = xp10, stat, startXP*10
	s.feedStat()
}

func (s *Session) feedStat() {
	xp := s.buryXP10 / 10
	s.feed(serverprot.UPDATE_STAT, func(p *packet.Packet) { p.P1(byte(s.buryStat)); p.P4(int32(xp)); p.P1(byte(levels.LevelFromXP(xp))) })
}

// handleHeld applies the packets this file models; false for anything else.
func (s *Session) handleHeld(op int, b []byte) bool {
	switch op {
	case clientprot.OPHELD1: // Bury, when Bury scripted it
		if len(b) < 7 || s.bury == nil {
			return false
		}
		id, slot := int32(g2(b, 1)), g2(b, 3)
		x, ok := s.bury[id]
		if !ok || !at(s.pack, slot, id) {
			return false
		}
		s.pack[slot] = Stack{}
		s.buryXP10 += x
		s.pushPack()
		s.feedStat()
		return true
	case clientprot.OPOBJ1, clientprot.OPOBJ2, clientprot.OPOBJ3, clientprot.OPOBJ4, clientprot.OPOBJ5:
		if len(b) < 7 {
			return false
		}
		x, z, id := g2(b, 1), g2(b, 3), int32(g2(b, 5))
		for i, g := range s.ground {
			if int32(g[0]) == id && g[1] == x && g[2] == z {
				if s.addPack(id, 1) == 0 {
					return true // a full pack: content says so in chat and leaves it there
				}
				s.objDel(i)
				s.pushPack()
				return true
			}
		}
		return false
	case clientprot.OPHELD2: // Wield/Wear
		if len(b) < 7 {
			return false
		}
		return s.equip(int32(g2(b, 1)), g2(b, 3))
	case clientprot.OPHELD5: // Drop: actions.HeldOps puts it in option 5
		if len(b) < 7 {
			return false
		}
		id, slot := int32(g2(b, 1)), g2(b, 3)
		if !at(s.pack, slot, id) {
			return false
		}
		s.pack[slot] = Stack{}
		s.pushPack()
		s.objAdd(id, s.here[0], s.here[1])
		return true
	case clientprot.OPHELDU:
		if len(b) < 13 {
			return false
		}
		onto, ontoSlot := int32(g2(b, 1)), g2(b, 3)
		used, usedSlot := int32(g2(b, 7)), g2(b, 9)
		result, ok := s.combine[[2]int32{used, onto}]
		if !ok || !at(s.pack, ontoSlot, onto) || !at(s.pack, usedSlot, used) {
			return false
		}
		s.pack[ontoSlot], s.pack[usedSlot] = Stack{}, Stack{}
		s.addPack(result, 1)
		s.pushPack()
		return true
	}
	return false
}

// unequip moves a worn item into the pack (the worn panel's Remove), refused on a full pack.
func (s *Session) unequip(id int32, slot int) bool {
	if !at(s.worn, slot, id) {
		return false
	}
	if s.addPack(id, 1) == 0 {
		return true
	}
	s.worn[slot] = Stack{}
	s.pushWorn()
	s.pushPack()
	return true
}

// bits packs fields most-significant bit first, as PLAYER_INFO is read.
type bits struct {
	buf []byte
	pos int
}

func (b *bits) put(n int, v uint32) {
	for i := n - 1; i >= 0; i-- {
		if b.pos/8 >= len(b.buf) {
			b.buf = append(b.buf, 0)
		}
		if v&(1<<uint(i)) != 0 {
			b.buf[b.pos/8] |= 0x80 >> uint(b.pos%8)
		}
		b.pos++
	}
}
