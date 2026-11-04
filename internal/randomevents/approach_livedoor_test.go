package randomevents

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// setTileAt is setTile with the local player placed lx,lz tiles into a build area whose origin
// is baseX,baseZ (both multiples of 8), so a fixture can also address zone tiles south or west of
// the player.
func setTileAt(s *gamestate.State, baseX, baseZ, lx, lz, level int) {
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(baseX/8 + 6))
		p.P2(uint16(baseZ/8 + 6))
	}))
	s.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1}, bitField{2, 3}, bitField{2, uint32(level)},
		bitField{7, uint32(lx)}, bitField{7, uint32(lz)},
		bitField{1, 0}, bitField{1, 0},
	))
}

// liveLocChange sends one UPDATE_ZONE_PARTIAL_FOLLOWS plus LOC_DEL (id < 0) or LOC_ADD_CHANGE
// for the absolute tile x,z, given the build-area origin baseX,baseZ.
func liveLocChange(s *gamestate.State, baseX, baseZ, x, z, id, shape, angle int) {
	lx, lz := x-baseX, z-baseZ
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) {
		p.P1(byte(lx &^ 7))
		p.P1(byte(lz &^ 7))
	}))
	pos := byte((lx&7)<<4 | lz&7)
	if id < 0 {
		s.Handle(serverprot.LOC_DEL, payload(func(p *packet.Packet) { p.P1(pos); p.P1(byte(shape<<2 | angle)) }))
		return
	}
	s.Handle(serverprot.LOC_ADD_CHANGE, payload(func(p *packet.Packet) {
		p.P1(pos)
		p.P1(byte(shape<<2 | angle))
		p.P2(uint16(id))
	}))
}

// TestOldManApproachUsesTheOpenDoor replays the mule on build e35420b6 (2026-09-24 18:16): cooking
// in the Catherby range house, whose door (loc 1530, 2816,3438, facing north) stood open, while
// its own Mysterious Old Man waited in the bank at 2809,3439. On the static pack the house is
// sealed — the map files place that door closed — so every approach answered "no approach path",
// the guardian gave up four seconds later and the Old Man fail-teleported the mule to the
// Lumbridge basement, taking the fleet's only mule out for three hours and eighteen minutes.
//
// The door is opened the way content 289's open_door proc does it: loc_del at the door's tile,
// then loc_add of the open stage (loc 1531) one tile north with the angle turned once.
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestOldManApproachUsesTheOpenDoor(t *testing.T) {
	pack := realCollisionPack(t)
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	withResolver(t, cfg)
	prevIx := currentLocIndex()
	SetLocIndex(ix)
	t.Cleanup(func() { SetLocIndex(prevIx) })

	const baseX, baseZ = 2808, 3432
	// Control: on the static pack the house is sealed — no interaction tile of the Old Man has a
	// route from inside.
	for _, g := range pack.InteractTiles(pathfind.NavPoint{X: 2809, Z: 3439}) {
		if pack.FindPath(pathfind.NavPoint{X: 2816, Z: 3443}, g).OK {
			t.Fatal("control: the range house is not sealed on the static pack — the reproduction does not hold")
		}
	}
	state := gamestate.New()
	setTileAt(state, baseX, baseZ, 8, 11, 0) // 2816,3443: inside the range house
	state.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	if x, z, _, _ := state.Tile(); x != 2816 || z != 3443 {
		t.Fatalf("fixture tile %d,%d", x, z)
	}
	liveLocChange(state, baseX, baseZ, 2816, 3438, -1, 0, 1)
	liveLocChange(state, baseX, baseZ, 2816, 3439, 1531, 0, 2)
	s := &overlapSession{state: state}
	g := NewGuardian(s, pack)
	ev := newEvent(KindDialogNpc, "Mysterious Old Man")
	ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = 1, 410, 2809, 3439
	_, err = approachDialogNPC(s, pack, ev, 2816, 3443, 0, nil)
	g.Stop()
	if err != nil {
		t.Fatalf("door standing open: %v", err)
	}
	if !sentMove(s.sent) {
		t.Fatal("door standing open: no walk toward the Old Man was sent")
	}
}

// TestOldManApproachOpensAShutDoor is the same room with the door shut, the account standing
// just inside it at 2816,3439: no route exists on the live overlay either, so the approach must
// open the door (the session plays content's open_door back on the click) and hand the next
// attempt a way through, rather than answer "no approach path" and let the Old Man teleport it.
func TestOldManApproachOpensAShutDoor(t *testing.T) {
	pack := realCollisionPack(t)
	ix, cfg := realLocData(t)
	withResolver(t, cfg)
	prevIx := currentLocIndex()
	SetLocIndex(ix)
	t.Cleanup(func() { SetLocIndex(prevIx) })

	const baseX, baseZ = 2808, 3432
	state := gamestate.New()
	setTileAt(state, baseX, baseZ, 8, 7, 0) // 2816,3439: inside the range house, at its door
	state.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	s := &overlapSession{state: state}
	opened := false
	s.onSend = func(op byte) {
		if op == clientprot.OPLOC1 && !opened {
			opened = true
			liveLocChange(state, baseX, baseZ, 2816, 3438, -1, 0, 1)
			liveLocChange(state, baseX, baseZ, 2816, 3439, 1531, 0, 2)
		}
	}
	g := NewGuardian(s, pack)
	defer g.Stop()
	ev := newEvent(KindDialogNpc, "Mysterious Old Man")
	ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = 1, 410, 2809, 3439
	interrupted, err := approachDialogNPC(s, pack, ev, 2816, 3439, 0, nil)
	if !opened {
		t.Fatalf("the shut door was not opened: interrupted=%v err=%v", interrupted, err)
	}
	if err != nil || !interrupted {
		t.Fatalf("after opening the door: interrupted=%v err=%v, want a clean hand-back to the next attempt", interrupted, err)
	}
	// The next attempt has a route through it.
	if _, err := approachDialogNPC(s, pack, ev, 2816, 3439, 0, nil); err != nil {
		t.Fatalf("next attempt: %v", err)
	}
}
