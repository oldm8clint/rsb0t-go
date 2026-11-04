package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// setSelfName decodes a real PLAYER_INFO appearance block naming the local player, the
// same bytes TestDecodedGreetingOwnershipOverridesFace builds.
func setSelfName(t *testing.T, s *gamestate.State, username string) {
	t.Helper()
	blob := packet.NewWriter(64)
	for i := 0; i < 19; i++ {
		blob.P1(0)
	}
	for i := 0; i < 7; i++ {
		blob.P2(0)
	}
	var hash int64
	for _, c := range username {
		hash *= 37
		switch {
		case c >= 'a' && c <= 'z':
			hash += int64(c - 'a' + 1)
		case c >= '0' && c <= '9':
			hash += int64(c - '0' + 27)
		}
	}
	blob.P8(hash)
	blob.P1(3)
	blob.P2(0)
	main := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
	ext := payload(func(p *packet.Packet) { p.P1(1); p.P1(byte(blob.Pos)); p.PData(blob.Data, 0, blob.Pos) })
	s.Handle(serverprot.PLAYER_INFO, append(main, ext...))
	if _, ok := s.SelfName(); !ok {
		t.Fatal("self name not decoded")
	}
}

// addGreetingNPC adds NPC index 1 of typeID one tile east, facing face, whose latest
// overhead speech is speech.
func addGreetingNPC(s *gamestate.State, typeID, face int, speech string) {
	main := bitPack(8, bitField{8, 0}, bitField{14, 1}, bitField{11, uint32(typeID)}, bitField{5, 1}, bitField{5, 0}, bitField{1, 0}, bitField{1, 1}, bitField{14, 16383})
	ext := payload(func(p *packet.Packet) { p.P1(12); p.P2(uint16(face)); p.PJStr(speech) })
	s.Handle(serverprot.NPC_INFO, append(main, ext...))
}

// The live chain (android 02:37:54, craft 03:23:12, 2026-09-19): the Old Man greeted the
// account by name, another player's Talk-to turned its face away (content ~chatnpc sets
// playerfaceclose on whoever talks), and the handler rejected it on the face alone. It
// then fail-teleported its real owner to Taverley 48s later.
func TestOwnGreetingBeatsFaceTurnedByAnotherPlayersTalkTo(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Mysterious Old Man"}})
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "android")
	addGreetingNPC(s.state, 500, 32768+9, "Greetings android!")
	g := NewGuardian(s, nil)
	defer g.Stop()

	ev := g.nextEvent()
	if ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("Detect must select the Old Man its greeting names: %+v", ev)
	}
	if targetsAnotherAccount(s, ev) {
		t.Fatal("handler treated a greeting naming this account as another account's event because of the face")
	}
	if err := HandleDialogNpc(s, nil, ev, nil); err != nil {
		t.Fatal(err)
	}
	if g.isRejected(ev) {
		t.Fatal("own Old Man was rejected")
	}
	talked := false
	for _, op := range s.sent {
		if op == clientprot.OPNPC1 {
			talked = true
		}
	}
	if !talked {
		t.Fatal("handler never talked to its own Old Man")
	}
}

// A face-only rejection made before any greeting was seen must not hide the NPC once a
// nag names this account: each 20-tick macro_mage timer repeats the owner's name, and the
// fourth one is the fail-teleport.
func TestGreetingNamingSelfLiftsEarlierFaceRejection(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Mysterious Old Man"}})
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "android")
	addGreetingNPC(s.state, 500, 32768+9, "")
	g := NewGuardian(s, nil)
	defer g.Stop()
	markPlantRejected(s, 1)
	if ev := g.nextEvent(); ev != nil {
		t.Fatalf("rejected, unnamed NPC must stay hidden: %+v", ev)
	}
	addGreetingNPC(s.state, 500, 32768+9, "It's really rude to ignore someone, android!")
	if ev := g.nextEvent(); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("greeting naming this account must lift the face rejection: %+v", ev)
	}
}

// The reverse still holds: a greeting naming someone else is rejected even when the NPC
// faces this account (for example after this account's own probe turned it).
func TestOtherAccountsGreetingRejectedEvenWhenFacingSelf(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Mysterious Old Man"}})
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "android")
	addGreetingNPC(s.state, 500, 32768+5, "Greetings craft!")
	ev := newDialogEvent(1, 500, "Mysterious Old Man")
	if !targetsAnotherAccount(s, ev) {
		t.Fatal("greeting naming another account must be treated as theirs")
	}
	g := NewGuardian(s, nil)
	defer g.Stop()
	markPlantRejected(s, 1)
	if next := g.nextEvent(); next != nil {
		t.Fatalf("another account's Old Man must stay rejected: %+v", next)
	}
}

// A rejection logs what decided it, so the next live case does not have to be inferred.
func TestRejectionLogsItsEvidence(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Mysterious Old Man"}})
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "android")
	addGreetingNPC(s.state, 500, 32768+9, "Greetings craft!")
	var lines []string
	if err := rejectDialogNPC(s, newDialogEvent(1, 500, "Mysterious Old Man"), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	want := "not this account's Mysterious Old Man (self slot 5, face 32777, smokepuff heard=false, sounds seen=false, greeting names craft) — checking other events"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("logged %q, want %q", lines, want)
	}
}
