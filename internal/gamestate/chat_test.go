package gamestate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

type wordPackVector struct {
	Input        string `json:"input"`
	Packed       []int  `json:"packed"`
	ClientPacked []int  `json:"clientPacked"`
	Unpacked     string `json:"unpacked"`
}

func toBytes(v []int) []byte {
	out := make([]byte, len(v))
	for i, b := range v {
		out[i] = byte(b)
	}
	return out
}

func TestWordPackMatchesTheReferenceVectors(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "wordpack_vector.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var vectors []wordPackVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 10 {
		t.Fatalf("fixture has %d vectors", len(vectors))
	}
	for _, v := range vectors {
		if got := wordPackUnpack(toBytes(v.Packed)); got != v.Unpacked {
			t.Errorf("unpack(%q's engine bytes) = %q, want %q", v.Input, got, v.Unpacked)
		}
		if got := wordPackPack(v.Input); !bytes.Equal(got, toBytes(v.ClientPacked)) {
			t.Errorf("pack(%q) = %v, want the client's %v", v.Input, got, v.ClientPacked)
		}
		if !bytes.Equal(toBytes(v.Packed), toBytes(v.ClientPacked)) {
			t.Errorf("%q: engine and client pack differ in the fixture", v.Input)
		}
	}
}

// chatTestUserhash is JString.toUserhash for the ASCII names these tests use.
func chatTestUserhash(name string) int64 {
	var l int64
	for i := 0; i < len(name) && i < 12; i++ {
		c := name[i]
		l *= 37
		switch {
		case c >= 'A' && c <= 'Z':
			l += int64(1 + c - 'A')
		case c >= 'a' && c <= 'z':
			l += int64(1 + c - 'a')
		case c >= '0' && c <= '9':
			l += int64(27 + c - '0')
		}
	}
	for l%37 == 0 && l != 0 {
		l /= 37
	}
	return l
}

func privateMessage(from string, id int32, staff byte, text string) []byte {
	p := packet.NewWriter(64)
	p.P8(chatTestUserhash(from))
	p.P4(id)
	p.P1(staff)
	for _, b := range wordPackPack(text) {
		p.P1(b)
	}
	return p.Data[:p.Pos]
}

func TestMessagePrivateEmitsChatAndDropsRepeats(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	s.Handle(serverprot.MESSAGE_PRIVATE, privateMessage("mule_con", 77, 0, "got the strings"))
	s.Handle(serverprot.MESSAGE_PRIVATE, privateMessage("mule_con", 77, 0, "got the strings"))
	s.Handle(serverprot.MESSAGE_PRIVATE, privateMessage("jmod", 78, 2, "hi there"))

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (the repeat id dropped): %+v", len(got), got)
	}
	if e := got[0]; e.Kind != EventChatMessage || e.ChatType != ChatPrivate || e.Username != "Mule Con" || e.Text != "Got the strings " || e.Crown != 0 {
		t.Fatalf("first PM = %+v", e)
	}
	// An even count of one-nibble characters leaves no trailing carry, so no trailing space.
	if e := got[1]; e.ChatType != ChatPrivateMod || e.Crown != 2 || e.Username != "Jmod" || e.Text != "Hi there" {
		t.Fatalf("staff PM = %+v", e)
	}
	s.Handle(serverprot.MESSAGE_PRIVATE, []byte{1, 2, 3})
	if len(got) != 2 {
		t.Fatal("a short MESSAGE_PRIVATE must be ignored")
	}
}

func TestMessageGameTypesTradeAndDuelRequests(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))
	for _, line := range []string{"Welcome to RuneScape.", "Mule Con:tradereq:", "Bob:duelreq:"} {
		p := packet.NewWriter(32)
		p.PJStr(line)
		s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])
	}
	if len(got) != 3 {
		t.Fatalf("got %d events", len(got))
	}
	if got[0].ChatType != ChatGame || got[0].Username != "" {
		t.Fatalf("game line = %+v", got[0])
	}
	if got[1].ChatType != ChatTradeRequest || got[1].Username != "Mule Con" || got[1].Text != "Mule Con:tradereq:" {
		t.Fatalf("trade request = %+v", got[1])
	}
	if got[2].ChatType != ChatDuelRequest || got[2].Username != "Bob" {
		t.Fatalf("duel request = %+v", got[2])
	}
}

// extendedBlock writes one entity's PLAYER_INFO extended block with SAY and/or CHAT, then a
// FACEENTITY on the next entity so the test also proves the cursor stays aligned.
func TestPublicChatAndSayDecodeInTheExtendedBlock(t *testing.T) {
	s := New()
	s.self.name = "Me"
	s.players[5] = &playerEntry{name: "Bob", faceEntity: -1}
	s.players[6] = &playerEntry{faceEntity: -1} // name not known yet

	p := packet.NewWriter(64)
	// Local player (-1): SAY.
	p.P1(0x08)
	p.PJStr("Hello from the server")
	// Player 5: SAY and CHAT with rights 1.
	p.P1(0x48)
	p.PJStr("forced")
	chat := wordPackPack("selling lobs")
	p.P2(0x0102)
	p.P1(1)
	p.P1(byte(len(chat)))
	for _, b := range chat {
		p.P1(b)
	}
	// Player 6: CHAT while nameless is dropped, then player 5 again: FACEENTITY 32768+6.
	p.P1(0x40)
	p.P2(0)
	p.P1(0)
	p.P1(byte(len(chat)))
	for _, b := range chat {
		p.P1(b)
	}
	p.P1(0x04)
	p.P2(32774)

	s.applyPlayerInfoExtended(packet.NewReader(p.Data[:p.Pos]), []int{-1, 5, 6, 5})
	events := s.takeChatEvents()

	if len(events) != 3 {
		t.Fatalf("got %d chat events, want 3: %+v", len(events), events)
	}
	if e := events[0]; e.Username != "Me" || e.Text != "Hello from the server" || e.ChatType != ChatPublic {
		t.Fatalf("local SAY = %+v", e)
	}
	if e := events[1]; e.Username != "Bob" || e.Text != "forced" || e.ChatType != ChatPublic {
		t.Fatalf("Bob SAY = %+v", e)
	}
	if e := events[2]; e.Username != "Bob" || e.Text != "Selling lobs" || e.ChatType != ChatPublicMod || e.Crown != 1 {
		t.Fatalf("Bob CHAT = %+v", e)
	}
	if s.players[5].faceEntity != 32774 {
		t.Fatalf("the block after the chat decoded face %d: cursor misaligned", s.players[5].faceEntity)
	}
	if text, _ := s.SelfChat(); text != "Hello from the server" {
		t.Fatalf("SelfChat = %q", text)
	}
	if len(s.takeChatEvents()) != 0 {
		t.Fatal("takeChatEvents must drain the queue")
	}
}

func TestSelfChatEmptyBeforeAnySay(t *testing.T) {
	if text, tick := New().SelfChat(); text != "" || tick != 0 {
		t.Fatalf("SelfChat = %q, %d", text, tick)
	}
}
