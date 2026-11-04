package gamestate

import (
	"fmt"
	"reflect"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// addSpeakingNPC adds NPC 5 of typeID saying line (a fresh NewVis add with SAY).
func addSpeakingNPC(s *State, typeID int, line string) {
	bits := bitPack(8, bitField{8, 0},
		bitField{14, 5}, bitField{11, uint32(typeID)}, bitField{5, 1}, bitField{5, 0}, bitField{1, 0}, bitField{1, 1},
		bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, bytesAfter(bits, func(p *packet.Packet) {
		p.P1(npcUpdateSay)
		p.PJStr(line)
	}))
}

// npcSays has the one tracked NPC say line (OldVis op 0, SAY).
func npcSays(s *State, line string) {
	bits := bitPack(4, bitField{8, 1}, bitField{1, 1}, bitField{2, 0}, bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, bytesAfter(bits, func(p *packet.Packet) {
		p.P1(npcUpdateSay)
		p.PJStr(line)
	}))
}

// A random-event NPC's later line must not erase what its greeting said: the log keeps
// every distinct line in order, and Speech stays the newest.
func TestNpcSpeechLogKeepsEveryLineItSaid(t *testing.T) {
	s := newZoneState()
	addSpeakingNPC(s, 41, "Greetings Sprinter!")
	npcSays(s, "Greetings Sprinter!") // a repeat is not a new line
	npcSays(s, "I lose more friends that way.")
	n := npcByIndex(t, s, 5)
	if want := []string{"Greetings Sprinter!", "I lose more friends that way."}; !reflect.DeepEqual(n.SpeechLog, want) {
		t.Fatalf("SpeechLog = %q, want %q", n.SpeechLog, want)
	}
	if n.Speech != "I lose more friends that way." {
		t.Fatalf("Speech = %q, want the newest line", n.Speech)
	}
}

// The log is bounded, and a snapshot taken earlier never changes under its holder.
func TestNpcSpeechLogIsBoundedAndSnapshotsAreStable(t *testing.T) {
	s := newZoneState()
	addSpeakingNPC(s, 41, "line 0")
	before := npcByIndex(t, s, 5).SpeechLog
	for i := 1; i < maxSpeechLog+3; i++ {
		npcSays(s, fmt.Sprintf("line %d", i))
	}
	if !reflect.DeepEqual(before, []string{"line 0"}) {
		t.Fatalf("an earlier snapshot changed: %q", before)
	}
	log := npcByIndex(t, s, 5).SpeechLog
	if len(log) != maxSpeechLog || log[0] != "line 3" || log[len(log)-1] != fmt.Sprintf("line %d", maxSpeechLog+2) {
		t.Fatalf("SpeechLog = %q, want the newest %d lines", log, maxSpeechLog)
	}
}

// An index that another NPC type takes over while still tracked does not inherit the old
// NPC's lines (a CHANGETYPE, or a re-add under a different type).
func TestNpcSpeechLogResetsWhenTheIndexChangesType(t *testing.T) {
	s := newZoneState()
	addSpeakingNPC(s, 41, "Greetings Sprinter!")
	addSpeakingNPC(s, 42, "Hello")
	if log := npcByIndex(t, s, 5).SpeechLog; !reflect.DeepEqual(log, []string{"Hello"}) {
		t.Fatalf("re-added under a new type: SpeechLog = %q", log)
	}
	bits := bitPack(4, bitField{8, 1}, bitField{1, 1}, bitField{2, 0}, bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, bytesAfter(bits, func(p *packet.Packet) {
		p.P1(npcUpdateChangeType)
		p.P2(43)
	}))
	// The latest line goes too: npcSpeechOwner falls back to it when the log is empty, and the
	// old NPC's words must not be read as the new one's.
	if n := npcByIndex(t, s, 5); n.Type != 43 || len(n.SpeechLog) != 0 || n.Speech != "" {
		t.Fatalf("after CHANGETYPE: type %d, Speech %q, SpeechLog %q", n.Type, n.Speech, n.SpeechLog)
	}
}
