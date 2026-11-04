package randomevents

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// payloadSession records every packet whole (opcode and payload), and runs onSend after each.
type payloadSession struct {
	state    *gamestate.State
	counters anticheat.Counters
	mu       sync.Mutex
	sent     [][]byte
	onSend   func(op byte)
}

func (s *payloadSession) State() *gamestate.State     { return s.state }
func (s *payloadSession) Cheats() *anticheat.Counters { return &s.counters }
func (s *payloadSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(256)
	build(p)
	s.mu.Lock()
	s.sent = append(s.sent, append([]byte(nil), p.Data[:p.Pos]...))
	s.mu.Unlock()
	if s.onSend != nil {
		s.onSend(p.Data[0])
	}
	return nil
}

func (s *payloadSession) ops() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, len(s.sent))
	for i, b := range s.sent {
		out[i] = b[0]
	}
	return out
}

// The rub menu: the chat modal the jewellery opens, with the two options a Ring of dueling offers (and a third, "Nowhere.").
const (
	rubMenu  = 7000
	rubOptA  = 7001 // Al Kharid Duel Arena.
	rubOptB  = 7002 // Castle Wars Arena.
	rubOptC  = 7003 // Nowhere.
	ringID   = 2552
	necklace = 3853
)

func rubArchive() archive.Components {
	return archive.Components{
		rubMenu: {ID: rubMenu, Children: []int{rubOptA, rubOptB, rubOptC}},
		rubOptA: {ID: rubOptA, ButtonType: archive.ButtonOK, Text: "option1"},
		rubOptB: {ID: rubOptB, ButtonType: archive.ButtonOK, Text: "option2"},
		rubOptC: {ID: rubOptC, ButtonType: archive.ButtonOK, Text: "option3"},
	}
}

func setLabel(s *gamestate.State, comID int, text string) {
	s.Handle(serverprot.IF_SETTEXT, payload(func(p *packet.Packet) { p.P2(uint16(comID)); p.PJStr(text) }))
}

// rubScene is an account with the jewellery in its pack and a server that answers a rub with the menu.
func rubScene(t *testing.T, items ...[2]int32) (*payloadSession, *Guardian) {
	t.Helper()
	withResolver(t, fakeResolver{objNames: map[int]string{ringID: "Ring of dueling(8)", necklace: "Games necklace(8)", 333: "Kebab"}})
	prev := currentInterfaceArchive()
	SetInterfaceArchive(rubArchive())
	t.Cleanup(func() { SetInterfaceArchive(prev) })
	s := &payloadSession{state: overlapState()}
	setFullInventory(s.state, 3214, items...)
	g := NewGuardian(s, nil)
	t.Cleanup(g.Stop)
	t.Cleanup(func() { lastResortCasts.Delete(s) })
	s.onSend = func(op byte) {
		if op == clientprot.OPHELD4 {
			s.state.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(rubMenu) }))
			setLabel(s.state, rubOptA, "Al Kharid Duel Arena.")
			setLabel(s.state, rubOptB, "Castle Wars Arena.")
			setLabel(s.state, rubOptC, "Nowhere.")
		}
	}
	return s, g
}

func TestRubItemInPrefersTheRingAndSkipsNotes(t *testing.T) {
	items := []packItem{
		{Obj: necklace, Slot: 0, Name: "Games necklace(3)"},
		{Obj: ringID, Slot: 4, Name: "Ring of dueling(8)", Noted: true},
		{Obj: ringID, Slot: 9, Name: "Ring of dueling(2)"},
	}
	pick, ok := rubItemIn(items)
	if !ok || pick.slot != 9 || pick.option != "Duel Arena" {
		t.Fatalf("pick = %+v, %v; want the un-noted ring in slot 9", pick, ok)
	}
	if pick, ok = rubItemIn(items[:1]); !ok || pick.option != "Burthorpe" {
		t.Fatalf("necklace only = %+v, %v", pick, ok)
	}
	if _, ok = rubItemIn([]packItem{{Name: "Ring of forging(5)"}, {Name: "Amulet of glory(4)"}, {Name: "Ring of dueling(8)", Noted: true}}); ok {
		t.Fatal("a ring that is not for the arena, or a note, was picked")
	}
}

// The rub is a teleport once its menu is answered: OPHELD op 4 on the ring, then the first option, not "Nowhere.".
func TestRubTeleportRubsTheRingAndAnswersTheMenu(t *testing.T) {
	s, _ := rubScene(t, [2]int32{333, 1}, [2]int32{ringID, 1})
	var lines []string
	if !rubTeleport(s, "combat flee: stuck", lastResortGap, func(m string) { lines = append(lines, m) }) {
		t.Fatalf("no rub: %v", lines)
	}
	ops := s.ops()
	if len(ops) != 2 || ops[0] != clientprot.OPHELD4 || ops[1] != clientprot.IF_BUTTON {
		t.Fatalf("sent %v, want OPHELD4 then IF_BUTTON", ops)
	}
	if com := int(s.sent[1][1])<<8 | int(s.sent[1][2]); com != rubOptA {
		t.Fatalf("pressed component %d, want %d (Al Kharid Duel Arena)", com, rubOptA)
	}
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("log %v", lines)
	}
	// A rub's p_delay(1) drops an eat sent inside it: the bite ledger is told.
	if actions.DelayLeft(s, time.Now()) <= 0 {
		t.Error("the rub's delay was not noted for the eater")
	}
	// Not again inside the gap, whichever of the spell and the rub went first.
	if rubTeleport(s, "again", lastResortGap, nil) || len(s.ops()) != 2 {
		t.Fatal("a second rub went out inside the gap")
	}
}

func TestRubTeleportWithNothingToRubOrNoMenuSendsNothingFurther(t *testing.T) {
	s, _ := rubScene(t, [2]int32{333, 1})
	if rubTeleport(s, "x", lastResortGap, nil) || len(s.ops()) != 0 {
		t.Fatalf("rubbed with no jewellery: %v", s.ops())
	}
	// A pack with the necklace but a server that never opens the menu: one rub, no button, no repeat.
	s2, _ := rubScene(t, [2]int32{necklace, 1})
	s2.onSend = nil
	prev := rubMenuWait
	rubMenuWait = 250 * time.Millisecond
	t.Cleanup(func() { rubMenuWait = prev })
	if rubTeleport(s2, "x", lastResortGap, nil) {
		t.Fatal("reported a teleport without a menu")
	}
	if ops := s2.ops(); len(ops) != 1 || ops[0] != clientprot.OPHELD4 {
		t.Fatalf("sent %v, want the one rub", ops)
	}
}

// The Games necklace's menu has one teleport; a ring whose menu offers neither is closed again.
func TestRubTeleportWithAMenuThatOffersNothingWeWantClosesIt(t *testing.T) {
	s, _ := rubScene(t, [2]int32{necklace, 1})
	s.onSend = func(op byte) {
		if op == clientprot.OPHELD4 {
			s.state.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(rubMenu) }))
			setLabel(s.state, rubOptA, "Somewhere else.")
			setLabel(s.state, rubOptB, "Nowhere.")
			setLabel(s.state, rubOptC, "")
		}
	}
	if rubTeleport(s, "x", lastResortGap, nil) {
		t.Fatal("pressed an option it does not want")
	}
	ops := s.ops()
	if len(ops) != 2 || ops[1] != clientprot.CLOSE_MODAL {
		t.Fatalf("sent %v, want the rub then a CLOSE_MODAL", ops)
	}
}

// The ladder: the spell the account can pay for, then the rub; an account hurt enough that the last resort wants a teleport, and with
// no runes, rubs.
func TestLastResortRubsWhenThereAreNoRunes(t *testing.T) {
	s, _ := rubScene(t, [2]int32{ringID, 1})
	setHP(s.state, 5, 24)
	if !lastResortExit(s, "test", nil) {
		t.Fatal("no teleport at 5 of 24 with a ring and no food")
	}
	if ops := s.ops(); len(ops) != 2 || ops[0] != clientprot.OPHELD4 {
		t.Fatalf("sent %v", ops)
	}
}

func TestEscapeTeleportRubsWhenThereAreNoRunes(t *testing.T) {
	s, _ := rubScene(t, [2]int32{ringID, 1})
	setHP(s.state, 24, 24)
	if !EscapeTeleport(s, "lag escape", time.Minute, nil) {
		t.Fatal("the caller-judged escape did not rub")
	}
	if ops := s.ops(); len(ops) != 2 || ops[0] != clientprot.OPHELD4 {
		t.Fatalf("sent %v", ops)
	}
}
