package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

func ifButtonBytes(comID int) []byte {
	return payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.IF_BUTTON)
		p.P2(uint16(comID))
	})
}

func setIcon(f *fakeSession, tab, comID int) {
	f.state.Handle(serverprot.IF_SETICON, payload(func(p *packet.Packet) {
		p.P2(uint16(comID))
		p.P1(byte(tab))
	}))
}

func TestSetRunAndAutoRetaliateSendTheControlsButtons(t *testing.T) {
	cases := []struct {
		call func(*fakeSession) error
		com  int
	}{
		{func(f *fakeSession) error { return SetRun(f, true) }, 153},
		{func(f *fakeSession) error { return SetRun(f, false) }, 152},
		{func(f *fakeSession) error { return SetAutoRetaliate(f, true) }, 150},
		{func(f *fakeSession) error { return SetAutoRetaliate(f, false) }, 151},
	}
	for _, c := range cases {
		f := newFakeSession()
		if err := c.call(f); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(f.lastSent(), ifButtonBytes(c.com)) {
			t.Errorf("sent %x, want IF_BUTTON %d", f.lastSent(), c.com)
		}
	}
}

func TestSetCombatModeUsesTheLoadedWeaponTab(t *testing.T) {
	f := newFakeSession()
	if err := SetCombatMode(f, 0); err != ErrNotOnScreen {
		t.Fatalf("no combat tab: err = %v, want ErrNotOnScreen", err)
	}

	setIcon(f, CombatTab, 5855) // combat_unarmed
	if err := SetCombatMode(f, 1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(5862)) {
		t.Fatalf("mode 1 on unarmed sent %x, want IF_BUTTON 5862 (unarmed1)", f.lastSent())
	}
	if err := SetCombatMode(f, 3); err != ErrNoCombatStyle {
		t.Fatalf("mode 3 on a three-style tab: err = %v", err)
	}
	if got := CombatModes(f); len(got) != 3 || got[2].Label != "Defensive" {
		t.Fatalf("CombatModes = %+v", got)
	}
}

func TestResolveCombatStyle(t *testing.T) {
	cases := []struct {
		root      int
		style     CombatStyle
		ok        bool
		mode      int
		effective CombatStyle
	}{
		{2423, StyleControlled, true, 2, StyleControlled}, // hacksword Lunge
		{2423, StyleStrength, true, 1, StyleStrength},     // first Aggressive wins
		{8460, StyleAttack, true, 2, StyleDefence},        // polearm has no Accurate: last Defensive
		{8460, StyleControlled, true, 0, StyleControlled}, // polearm Jab
		{4679, StyleStrength, true, 3, StyleDefence},      // spear: controlled x3 then Defensive
		{5855, StyleControlled, true, 2, StyleDefence},    // unarmed has no Controlled
		{1764, StyleAttack, true, 0, StyleAttack},         // bow "Accurate" parses like the melee caption, as upstream
		{1764, StyleStrength, false, 0, ""},               // Rapid/Longrange are not melee styles and there is no Defensive
	}
	for _, c := range cases {
		res, ok := ResolveCombatStyle(c.style, CombatModesFor(c.root))
		if ok != c.ok || (ok && (res.Mode != c.mode || res.Effective != c.effective || res.Requested != c.style)) {
			t.Errorf("root %d %s: got %+v ok=%v, want mode %d effective %s ok=%v", c.root, c.style, res, ok, c.mode, c.effective, c.ok)
		}
	}
}

func TestSetAndHasCombatStyle(t *testing.T) {
	f := newFakeSession()
	if _, err := SetCombatStyle(f, StyleAttack); err != ErrNotOnScreen {
		t.Fatalf("no tab: err = %v", err)
	}
	setIcon(f, CombatTab, 425) // combat_blunt
	res, err := SetCombatStyle(f, StyleStrength)
	if err != nil || res.Mode != 1 {
		t.Fatalf("SetCombatStyle = %+v, %v", res, err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(432)) {
		t.Fatalf("sent %x, want IF_BUTTON 432 (blunt1)", f.lastSent())
	}
	if HasCombatStyle(f, StyleStrength) {
		t.Fatal("HasCombatStyle before the server confirms com_mode")
	}
	f.state.Handle(serverprot.VARP_SMALL, payload(func(p *packet.Packet) { p.P2(43); p.P1(1) }))
	if !HasCombatStyle(f, StyleStrength) || HasCombatStyle(f, StyleAttack) {
		t.Fatal("HasCombatStyle does not follow com_mode")
	}
}

func TestParseCombatStyle(t *testing.T) {
	for in, want := range map[string]CombatStyle{
		"Attack": StyleAttack, " accurate ": StyleAttack, "aggressive": StyleStrength,
		"shared": StyleControlled, "defense": StyleDefence, "Defensive": StyleDefence,
	} {
		if got, ok := ParseCombatStyle(in); !ok || got != want {
			t.Errorf("ParseCombatStyle(%q) = %q,%v, want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseCombatStyle("magic"); ok {
		t.Error("ParseCombatStyle accepted magic")
	}
}

func TestCombatTabsAreWellFormed(t *testing.T) {
	seen := map[int]int{}
	for root, buttons := range combatTabs {
		if len(buttons) < 3 || len(buttons) > 4 {
			t.Errorf("root %d has %d buttons", root, len(buttons))
		}
		for i, b := range buttons {
			if b.Mode != i {
				t.Errorf("root %d button %d has mode %d", root, i, b.Mode)
			}
			if b.ComID <= root || b.ComID > root+20 {
				t.Errorf("root %d button %d component %d is not inside the interface", root, i, b.ComID)
			}
			if other, dup := seen[b.ComID]; dup {
				t.Errorf("component %d used by roots %d and %d", b.ComID, other, root)
			}
			seen[b.ComID] = root
		}
	}
}
