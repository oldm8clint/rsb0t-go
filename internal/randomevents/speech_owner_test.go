package randomevents

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"testing"
)

func TestSpeechOwnerExactContentTemplates(t *testing.T) {
	for _, tc := range []struct{ kind, text, want string }{
		{"Genie", "Greetings master Aj Bot!", "aj bot"},
		{"Genie", "Are you there, mistress Aj_Bot?", "aj bot"},
		{"Drunken Dwarf", "'Ello der Acct100! *hic*", "acct100"},
		{"Mysterious Old Man", "Here you go Sup, it's all yours!", "sup"},
		{"Mysterious Old Man", "It's really rude to ignore someone, Acct189!", "acct189"},
		{"Genie", "No one ignores me!", ""},
		{"Genie", "Greetings master Sup! more text", ""},
		{"Genie", "Greetings master <invalid>!", ""},
		{"Banker", "Greetings master Sup!", ""},
	} {
		if got := speechOwner(tc.kind, tc.text); got != tc.want {
			t.Errorf("%s %q: got %q want %q", tc.kind, tc.text, got, tc.want)
		}
	}
}

func TestDecodedGreetingOwnershipOverridesFace(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Genie"}})
	for _, tc := range []struct {
		speech string
		face   int
		want   bool
	}{
		{"Greetings master Aj Bot!", 32768 + 9, true},
		{"Greetings master Sup!", 32768 + 5, false},
		{"Hmm... where was I?", 32768 + 5, true}, // names nobody: the face decides
		{"No one ignores me!", 32768 + 5, false}, // its fail line: nothing left to claim
	} {
		s := overlapState()
		blob := packet.NewWriter(64)
		for i := 0; i < 19; i++ {
			blob.P1(0)
		}
		for i := 0; i < 7; i++ {
			blob.P2(0)
		}
		var hash int64
		for _, c := range "aj_bot" {
			hash *= 37
			if c >= 'a' && c <= 'z' {
				hash += int64(c - 'a' + 1)
			}
		}
		blob.P8(hash)
		blob.P1(3)
		blob.P2(0)
		main := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
		ext := payload(func(p *packet.Packet) { p.P1(1); p.P1(byte(blob.Pos)); p.PData(blob.Data, 0, blob.Pos) })
		s.Handle(serverprot.PLAYER_INFO, append(main, ext...))
		if name, ok := s.SelfName(); !ok || name != "Aj Bot" {
			t.Fatalf("appearance name %q %v", name, ok)
		}
		main = bitPack(8, bitField{8, 0}, bitField{14, 1}, bitField{11, 500}, bitField{5, 1}, bitField{5, 0}, bitField{1, 0}, bitField{1, 1}, bitField{14, 16383})
		ext = payload(func(p *packet.Packet) { p.P1(12); p.P2(uint16(tc.face)); p.PJStr(tc.speech) })
		s.Handle(serverprot.NPC_INFO, append(main, ext...))
		if got := Detect(s) != nil; got != tc.want {
			t.Errorf("%q face=%d detected=%v want=%v", tc.speech, tc.face, got, tc.want)
		}
	}
}
