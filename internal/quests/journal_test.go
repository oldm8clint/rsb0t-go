package quests

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

func emptyState() *gamestate.State { return gamestate.New() }

// journalSession records sends and plays the server: IF_BUTTON on Cook's Assistant opens the
// journal scroll with a name and two lines, leaving qj3 cleared.
type journalSession struct {
	st    *gamestate.State
	c     anticheat.Counters
	sent  [][]byte
	reply bool
}

func (j *journalSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(16)
	build(w)
	j.sent = append(j.sent, append([]byte(nil), w.Data[:w.Pos]...))
	if !j.reply {
		return nil
	}
	handle := func(opcode int, fill func(p *packet.Packet)) {
		p := packet.NewWriter(64)
		fill(p)
		j.st.Handle(opcode, p.Data[:p.Pos])
	}
	setText := func(com int, text string) {
		handle(serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(uint16(com)); p.PJStr(text) })
	}
	setText(8144, "@dre@Cook's Assistant")
	setText(8145, "I should speak to the cook in Lumbridge Castle.")
	setText(8147, "He needs milk, flour and an egg.")
	setText(8148, "")
	handle(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(JournalScroll) })
	return nil
}
func (j *journalSession) State() *gamestate.State     { return j.st }
func (j *journalSession) Cheats() *anticheat.Counters { return &j.c }

type instantWaiter struct{}

func (instantWaiter) Delay(time.Duration) bool                          { return true }
func (instantWaiter) DelayUntil(cond func() bool, _ time.Duration) bool { return cond() }
func (instantWaiter) DelayTicks(int) bool                               { return true }

var journalComponents = archive.Components{
	JournalScroll: {ID: JournalScroll, Type: archive.TypeLayer, Children: []int{8136, 8144, 8143}},
	8136:          {ID: 8136, Type: archive.TypeText, Text: "Close Window"},
	8144:          {ID: 8144, Type: archive.TypeText},
	8143:          {ID: 8143, Type: archive.TypeLayer, Children: []int{8145, 8147, 8148, 8149}},
	8145:          {ID: 8145, Type: archive.TypeText, Text: "qj1"},
	8147:          {ID: 8147, Type: archive.TypeText, Text: "qj2"},
	8148:          {ID: 8148, Type: archive.TypeText, Text: "qj3"}, // cleared by the server
	8149:          {ID: 8149, Type: archive.TypeText, Text: "qj4"}, // never set: archive text
}

func TestJournalClicksTheRowAndReadsTheModal(t *testing.T) {
	s := &journalSession{st: gamestate.New(), reply: true}
	lines, err := Journal(s, journalComponents, "Cook's Assistant", instantWaiter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Close Window",
		"@dre@Cook's Assistant",
		"I should speak to the cook in Lumbridge Castle.",
		"He needs milk, flour and an egg.",
		"qj4",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %q\nwant    %q", lines, want)
	}
	wantPacket := packet.NewWriter(4)
	wantPacket.P1Enc(clientprot.IF_BUTTON)
	wantPacket.P2(7333)
	if len(s.sent) != 1 || !reflect.DeepEqual(s.sent[0], wantPacket.Data[:wantPacket.Pos]) {
		t.Fatalf("sent %x, want IF_BUTTON 7333", s.sent)
	}
}

func TestJournalErrors(t *testing.T) {
	s := &journalSession{st: gamestate.New()}
	if _, err := Journal(s, journalComponents, "Recipe for Disaster", instantWaiter{}); err != ErrUnknownQuest {
		t.Fatalf("unknown quest: err = %v", err)
	}
	if len(s.sent) != 0 {
		t.Fatal("an unknown quest must not send")
	}
	if _, err := Journal(s, journalComponents, "Dragon Slayer", instantWaiter{}); err == nil {
		t.Fatal("no modal opened: want ErrNotConfirmed")
	}
}
