package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// bytesAfter appends byte-aligned extended data to a bit-packed prefix.
func bytesAfter(bits []byte, build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(128)
	build(p)
	return append(append([]byte(nil), bits...), p.Data[:p.Pos]...)
}

func npcByIndex(t *testing.T, s *State, index int) NPC {
	t.Helper()
	for _, n := range s.NPCs() {
		if n.Index == index {
			return n
		}
	}
	t.Fatalf("NPC %d not tracked", index)
	return NPC{}
}

func TestNpcInfoExtendedKeepsAnimFaceHealthAndType(t *testing.T) {
	s := newZoneState()
	// Two new NPCs (index 5 type 41, index 6 type 41), both with extended info.
	bits := bitPack(12,
		bitField{8, 0},
		bitField{14, 5}, bitField{11, 41}, bitField{5, 2}, bitField{5, 0}, bitField{1, 0}, bitField{1, 1},
		bitField{14, 6}, bitField{11, 41}, bitField{5, 1}, bitField{5, 1}, bitField{1, 0}, bitField{1, 1},
		bitField{14, 16383},
	)
	s.Handle(serverprot.NPC_INFO, bytesAfter(bits, func(p *packet.Packet) {
		// NPC 5: SAY, CHANGETYPE, SPOTANIM, FACESQUARE — all skipped or folded, must stay aligned.
		p.P1(npcUpdateSay | npcUpdateChangeType | npcUpdateSpotAnim | npcUpdateFaceSquare)
		p.PJStr("hi")
		p.P2(520)
		p.P2(100)
		p.P4(0)
		p.P2(3200)
		p.P2(3200)
		// NPC 6: HITMARK2, ANIM, FACEENTITY.
		p.P1(npcUpdateHitmark2 | npcUpdateAnim | npcUpdateFaceEntity)
		p.P1(3)
		p.P1(0)
		p.P1(7)
		p.P1(10)
		p.P2(806)
		p.P1(0)
		p.P2(playerFaceBase + 5)
	}))

	n5 := npcByIndex(t, s, 5)
	if n5.Speech != "hi" || n5.Type != 520 || n5.Anim != -1 || n5.FaceEntity != -1 || n5.InCombat {
		t.Fatalf("NPC 5 = %+v, want type 520 and no anim/face/combat", n5)
	}
	n6 := npcByIndex(t, s, 6)
	if n6.Anim != 806 || n6.FaceEntity != playerFaceBase+5 || n6.Health != 7 || n6.TotalHealth != 10 || !n6.InCombat {
		t.Fatalf("NPC 6 = %+v", n6)
	}
	if !n6.TargetsPlayer(5) || n6.TargetsAnotherPlayer(5) || !n6.TargetsAnotherPlayer(9) {
		t.Fatal("face-target helpers disagree with FaceEntity")
	}

	// OldVis op 0 always queues an update: NPC 5 starts an animation, NPC 6 idles.
	bits = bitPack(4, bitField{8, 2}, bitField{1, 1}, bitField{2, 0}, bitField{1, 0}, bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, bytesAfter(bits, func(p *packet.Packet) {
		p.P1(npcUpdateAnim)
		p.P2(808)
		p.P1(0)
	}))
	if got := npcByIndex(t, s, 5).Anim; got != 808 {
		t.Fatalf("NPC 5 anim after op 0 = %d, want 808", got)
	}
	if got := npcByIndex(t, s, 6); got.Health != 7 || got.Anim != 806 {
		t.Fatalf("NPC 6 lost its state across ticks: %+v", got)
	}

	for i := 0; i < combatWindowTicks; i++ {
		idlePlayerInfo(s)
	}
	if npcByIndex(t, s, 6).InCombat {
		t.Fatal("NPC 6 still in combat after the window")
	}
}

// encodeUsername37 ports JString.toUserhash for lowercase names.
func encodeUsername37(name string) int64 {
	var v int64
	for i := 0; i < len(name); i++ {
		c := name[i]
		v *= 37
		switch {
		case c >= 'a' && c <= 'z':
			v += int64(c-'a') + 1
		case c >= '0' && c <= '9':
			v += int64(c-'0') + 27
		}
	}
	return v
}

// appearanceBlob is a minimal APPEARANCE sub-block: no equipment, a name and a combat level.
func appearanceBlob(name string, combatLevel int) []byte {
	p := packet.NewWriter(64)
	p.P1(0) // gender
	p.P1(0) // headicons
	for i := 0; i < 12; i++ {
		p.P1(0)
	}
	for i := 0; i < 5; i++ {
		p.P1(0)
	}
	for i := 0; i < 7; i++ {
		p.P2(0)
	}
	h := encodeUsername37(name)
	p.P4(int32(h >> 32))
	p.P4(int32(h))
	p.P1(byte(combatLevel))
	p.P2(0)
	return p.Data[:p.Pos]
}

func TestLocalPlayerExtendedInfo(t *testing.T) {
	s := newZoneState()
	if _, ok := s.SelfName(); ok {
		t.Fatal("SelfName before any appearance")
	}
	if seq, _ := s.SelfAnim(); seq != -1 || s.InCombat() || s.AttackedByPlayer() || s.SelfFaceEntity() != -1 {
		t.Fatal("fresh local extended state is not empty")
	}

	// Local op 0 (still, extended queued), no other players.
	bits := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
	blob := appearanceBlob("aj_bot", 42)
	s.Handle(serverprot.PLAYER_INFO, bytesAfter(bits, func(p *packet.Packet) {
		p.P1(playerUpdateAppearance | playerUpdateAnim | playerUpdateFaceEntity | playerUpdateHitmark)
		p.P1(byte(len(blob)))
		p.PData(blob, 0, len(blob))
		p.P2(808)
		p.P1(0)
		p.P2(playerFaceBase + 3)
		p.P1(2)
		p.P1(1)
		p.P1(8)
		p.P1(10)
	}))

	if name, ok := s.SelfName(); !ok || name != "Aj Bot" {
		t.Fatalf("SelfName = %q,%v", name, ok)
	}
	if lvl, ok := s.CombatLevel(); !ok || lvl != 42 {
		t.Fatalf("CombatLevel = %d,%v", lvl, ok)
	}
	if seq, tick := s.SelfAnim(); seq != 808 || tick != 1 || tick != s.Tick() {
		t.Fatalf("SelfAnim = %d@%d, Tick() = %d", seq, tick, s.Tick())
	}
	if !s.Animating(3) || !s.InCombat() || !s.AttackedByPlayer() || s.SelfFaceEntity() != playerFaceBase+3 {
		t.Fatal("local anim/combat/face not reported")
	}
	if len(s.Players()) != 0 {
		t.Fatal("the local player's block must not create an other-player entry")
	}

	for i := 0; i < 3; i++ {
		idlePlayerInfo(s)
	}
	if s.Animating(3) {
		t.Fatal("Animating(3) three ticks later")
	}
	for i := 0; i < combatWindowTicks; i++ {
		idlePlayerInfo(s)
	}
	if s.InCombat() {
		t.Fatal("still in combat after the window")
	}
}

// A poison hitmark (type 2) on the local player is stamped as such; an ordinary one is not.
func TestLocalPlayerPoisonHitmark(t *testing.T) {
	s := newZoneState()
	hit := func(typ byte) {
		bits := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
		s.Handle(serverprot.PLAYER_INFO, bytesAfter(bits, func(p *packet.Packet) {
			p.P1(playerUpdateHitmark)
			p.P1(3)
			p.P1(typ)
			p.P1(7)
			p.P1(10)
		}))
	}
	hit(1)
	if _, ok := s.LastPoisonHit(); ok || !s.InCombat() {
		t.Fatal("an ordinary hit read as poison")
	}
	if !s.HitWithin(2) {
		t.Fatal("HitWithin(2) is false on the tick of a hit")
	}
	idlePlayerInfo(s)
	idlePlayerInfo(s)
	if s.HitWithin(2) || !s.InCombat() {
		t.Fatalf("two ticks after a hit: HitWithin(2) = %v, InCombat = %v; want false, true", s.HitWithin(2), s.InCombat())
	}
	hit(HitmarkPoison)
	if tick, ok := s.LastPoisonHit(); !ok || tick != s.Tick() {
		t.Fatalf("LastPoisonHit = %d,%v at tick %d", tick, ok, s.Tick())
	}
}

func TestPlayerAppearanceCombatLevelIsOptional(t *testing.T) {
	blob := appearanceBlob("bob", 3)
	if name, lvl, ok := playerAppearance(blob); name != "Bob" || lvl != 3 || !ok {
		t.Fatalf("playerAppearance = %q %d %v", name, lvl, ok)
	}
	// A blob cut right after the name still yields the name.
	if name, _, ok := playerAppearance(blob[:len(blob)-3]); name != "Bob" || ok {
		t.Fatalf("short blob = %q %v", name, ok)
	}
}

func TestHintArrow(t *testing.T) {
	s := newZoneState()
	if _, _, _, ok := s.HintTile(); ok {
		t.Fatal("hint tile before HINT_ARROW")
	}
	s.Handle(serverprot.HINT_ARROW, payload(func(p *packet.Packet) { p.P1(4); p.P2(2780); p.P2(9579); p.P1(60) }))
	if x, z, _, ok := s.HintTile(); !ok || x != 2780 || z != 9579 {
		t.Fatalf("HintTile = %d,%d,%v", x, z, ok)
	}
	s.Handle(serverprot.HINT_ARROW, payload(func(p *packet.Packet) { p.P1(1); p.P2(77) }))
	if n, ok := s.HintNpc(); !ok || n != 77 {
		t.Fatalf("HintNpc = %d,%v", n, ok)
	}
	if _, _, _, ok := s.HintTile(); ok {
		t.Fatal("an NPC hint must replace the tile hint")
	}
	s.Handle(serverprot.HINT_ARROW, payload(func(p *packet.Packet) { p.P1(10); p.P2(12) }))
	if pl, ok := s.HintPlayer(); !ok || pl != 12 {
		t.Fatalf("HintPlayer = %d,%v", pl, ok)
	}
	s.Handle(serverprot.HINT_ARROW, payload(func(p *packet.Packet) { p.P1(0) }))
	if _, ok := s.HintPlayer(); ok {
		t.Fatal("type 0 must clear the arrow")
	}
}

// Another player's combat level comes from its appearance, and survives it leaving view and coming
// back (the server does not resend the appearance on a re-sighting).
func TestOtherPlayerCombatLevelFromAppearance(t *testing.T) {
	s := New()
	newVis := func(ext []byte) {
		var extended uint32
		if ext != nil {
			extended = 1
		}
		bits := bitPack(6,
			bitField{1, 0}, bitField{8, 0},
			bitField{11, 5}, bitField{5, 1}, bitField{5, 1}, bitField{1, 0}, bitField{1, extended},
			bitField{11, 2047},
		)
		s.Handle(serverprot.PLAYER_INFO, append(bits, ext...))
	}
	blob := appearanceBlob("pker", 57)
	newVis(append([]byte{0x01, byte(len(blob))}, blob...))
	if ps := s.Players(); len(ps) != 1 || ps[0].CombatLevel != 57 {
		t.Fatalf("Players() = %+v, want one at combat 57", ps)
	}
	// Op 3: leaves view.
	s.Handle(serverprot.PLAYER_INFO, bitPack(3, bitField{1, 0}, bitField{8, 1}, bitField{1, 1}, bitField{2, 3}, bitField{11, 2047}))
	newVis(nil)
	if ps := s.Players(); len(ps) != 1 || ps[0].CombatLevel != 57 {
		t.Fatalf("re-sighted Players() = %+v, want combat 57 from the cached appearance", ps)
	}
}
