package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// lampSession plays the server's half of the xplamp exchange (content xplamp.rs2): Rub (OPHELD1)
// opens the modal, a skill button selects, Confirm spends the Lamp and grants level x 10 experience.
type lampSession struct {
	state    *gamestate.State
	counters anticheat.Counters
	buttons  []int // every IF_BUTTON component clicked, in order
	selected int
	level    int
	statIdx  map[int]int // skill button -> stat index
}

func (s *lampSession) State() *gamestate.State     { return s.state }
func (s *lampSession) Cheats() *anticheat.Counters { return &s.counters }
func (s *lampSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(64)
	build(p)
	switch int(p.Data[0]) {
	case clientprot.OPHELD1:
		s.selected = 0
		s.state.Handle(serverprot.IF_OPENMAIN, payload(func(w *packet.Packet) { w.P2(xplampModalID) }))
	case clientprot.IF_BUTTON:
		id := int(p.Data[1])<<8 | int(p.Data[2])
		s.buttons = append(s.buttons, id)
		if _, isSkill := s.statIdx[id]; isSkill {
			s.selected = id
		}
		if id == xplampConfirmID && s.selected != 0 {
			setFullInventory(s.state, actions.PlayerPack) // the Lamp is gone
			stat := s.statIdx[s.selected]
			s.state.Handle(serverprot.UPDATE_STAT, payload(func(w *packet.Packet) {
				w.P1(byte(stat))
				w.P4(int32(1000 + s.level*10))
				w.P1(byte(s.level))
			}))
		}
	}
	return nil
}

func newLampSession() *lampSession {
	st := overlapState()
	for _, stat := range []int{LampMagic.Stat, LampFishing.Stat} {
		st.Handle(serverprot.UPDATE_STAT, payload(func(w *packet.Packet) { w.P1(byte(stat)); w.P4(1000); w.P1(66) }))
	}
	setFullInventory(st, actions.PlayerPack, [2]int32{831, 1})
	return &lampSession{state: st, level: 66, statIdx: map[int]int{
		LampMagic.ButtonID: LampMagic.Stat, LampFishing.ButtonID: LampFishing.Stat,
	}}
}

func TestLampButtonsAreTheContentsComponents(t *testing.T) {
	// content/pack/interface.pack: 2815=xplamp:magic, 2826=xplamp:fishing, 2831=xplamp:confirm.
	if LampMagic.ButtonID != 2815 || LampFishing.ButtonID != 2826 || xplampConfirmID != 2831 || xplampModalID != 2808 {
		t.Fatalf("buttons %d %d %d modal %d", LampMagic.ButtonID, LampFishing.ButtonID, xplampConfirmID, xplampModalID)
	}
	// gamestate's stat table: 6 Magic, 10 Fishing.
	if gamestate.SkillNames[LampMagic.Stat] != "Magic" || gamestate.SkillNames[LampFishing.Stat] != "Fishing" {
		t.Fatalf("stat indexes: %q %q", gamestate.SkillNames[LampMagic.Stat], gamestate.SkillNames[LampFishing.Stat])
	}
}

func TestLampDefaultsToFishingUntilAScriptChoosesMagic(t *testing.T) {
	s := newLampSession()
	if got := lampSkillFor(s); got != LampFishing {
		t.Fatalf("default skill %+v", got)
	}
	SetLampSkill(s, LampMagic)
	if got := lampSkillFor(s); got != LampMagic {
		t.Fatalf("chosen skill %+v", got)
	}
	SetLampSkill(s, LampFishing)
	if got := lampSkillFor(s); got != LampFishing {
		t.Fatalf("restored skill %+v", got)
	}
	lampSkillMu.Lock()
	n := len(lampSkills)
	lampSkillMu.Unlock()
	if n != 0 {
		t.Fatalf("restoring the default left %d entries", n)
	}
}

// The click sequence: rub, Magic's button, then Confirm; and the line names the xp the stat update granted.
func TestRubHeldLampsChoosesMagicAndLogsTheXP(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{831: "Lamp"}})
	s := newLampSession()
	SetLampSkill(s, LampMagic)
	defer SetLampSkill(s, LampFishing)
	var lines []string
	if got := RubHeldLamps(s, nil, func(l string) { lines = append(lines, l) }); got != 1 {
		t.Fatalf("spent %d lamps, want 1 (lines %v)", got, lines)
	}
	if len(s.buttons) != 2 || s.buttons[0] != 2815 || s.buttons[1] != 2831 {
		t.Fatalf("clicked %v, want Magic 2815 then Confirm 2831", s.buttons)
	}
	want := "lamp: rubbed for Magic (+660 xp)"
	found := false
	for _, l := range lines {
		found = found || strings.Contains(l, want)
	}
	if !found {
		t.Fatalf("lines %v lack %q", lines, want)
	}
	if HeldLamp(s) != nil {
		t.Fatal("a Lamp is still held")
	}
}

func TestRubHeldLampsIsQuietWithoutALamp(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{831: "Lamp", 995: "Coins"}})
	s := newLampSession()
	setFullInventory(s.state, actions.PlayerPack, [2]int32{995, 100})
	if got := RubHeldLamps(s, nil, nil); got != 0 || len(s.buttons) != 0 {
		t.Fatalf("spent %d, clicked %v with no Lamp held", got, s.buttons)
	}
}

// The Guardian's own claim (a fresh Genie Lamp) uses the same choice.
func TestGuardiansLampClaimUsesTheChosenSkill(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{831: "Lamp"}})
	s := newLampSession()
	SetLampSkill(s, LampMagic)
	defer SetLampSkill(s, LampFishing)
	if err := claimGenieLamp(s, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.buttons) != 2 || s.buttons[0] != LampMagic.ButtonID {
		t.Fatalf("clicked %v, want Magic first", s.buttons)
	}
}
