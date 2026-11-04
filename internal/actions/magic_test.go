package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestCastOnNpcSendsOpNpcT(t *testing.T) {
	for _, name := range []string{"Wind Strike", "wind_strike", "Cast @gre@wind strike"} {
		f := newFakeSession()
		if err := CastOnNpc(f, name, 42); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		want := payload(func(p *packet.Packet) {
			p.P1Enc(clientprot.OPNPCT)
			p.P2(42)
			p.P2(1152)
		})
		if !bytes.Equal(f.lastSent(), want) {
			t.Errorf("%q: sent %x, want %x", name, f.lastSent(), want)
		}
	}

	f := newFakeSession()
	if err := CastOnNpc(f, "Varrock Teleport", 1); err != ErrUnknownSpell || f.sendCount() != 0 {
		t.Fatalf("a non-combat spell: err = %v, sends = %d", err, f.sendCount())
	}
}

func TestTeleportComponentAcceptsTheButtonTextForms(t *testing.T) {
	cases := map[string]int{
		"Varrock":                    1164,
		"lumbridge teleport":         1167,
		"Cast @gre@Falador teleport": 1170,
		"CAMELOT":                    1174,
		"Ardougne Teleport":          1540,
		"watchtower":                 1541,
		"Trollheim Teleport":         7455,
	}
	for name, want := range cases {
		if got, ok := TeleportComponent(name); !ok || got != want {
			t.Errorf("TeleportComponent(%q) = %d,%v, want %d", name, got, ok, want)
		}
	}
	if _, ok := TeleportComponent("Paddewwa"); ok {
		t.Error("an ancient teleport resolved on the standard spellbook")
	}
}

func TestItemSpellComponentResolvesTheAlchemies(t *testing.T) {
	cases := map[string]int{
		"High Level Alchemy": 1178, "high_level_alchemy": 1178, "Cast @gre@High Level Alchemy": 1178,
		"Low Level Alchemy": 1162, "low level alchemy": 1162,
	}
	for name, want := range cases {
		if got, ok := ItemSpellComponent(name); !ok || got != want {
			t.Errorf("ItemSpellComponent(%q) = %d,%v, want %d", name, got, ok, want)
		}
	}
	if _, ok := ItemSpellComponent("Wind Strike"); ok {
		t.Error("a combat spell resolved as an item spell")
	}
}

// Lvl-1 Enchant and Superheat Item resolve (MasterCrafter's jewellery and superheat rounds), under the
// names the rounds send and the pack's own spelling, and the enchant is cast with OPHELDT like the rest.
func TestItemSpellComponentResolvesEnchantAndSuperheat(t *testing.T) {
	for name, want := range map[string]int{
		"Lvl-1 Enchant": 1155, "lvl-1 enchant": 1155, "Cast @gre@Lvl-1 Enchant": 1155, "enchant_lvl1": 1155,
		"Superheat Item": 1173,
	} {
		if got, ok := ItemSpellComponent(name); !ok || got != want {
			t.Errorf("ItemSpellComponent(%q) = %d,%v, want %d", name, got, ok, want)
		}
	}
	for name, want := range map[string]int{"Lvl-2 Enchant": 1165, "lvl-2 enchant": 1165, "enchant_lvl2": 1165} {
		if got, ok := ItemSpellComponent(name); !ok || got != want {
			t.Errorf("ItemSpellComponent(%q) = %d,%v, want %d", name, got, ok, want)
		}
	}
	for _, name := range []string{"Lvl-3 Enchant", "Enchant", "Lvl-1"} {
		if _, ok := ItemSpellComponent(name); ok {
			t.Errorf("%q resolved: only the level 1 and 2 enchants are known", name)
		}
	}
	f := newFakeSession()
	if err := CastOnItem(f, "Lvl-1 Enchant", 1656, 9, 3214); err != nil {
		t.Fatal(err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.OPHELDT)
		p.P2(1656)
		p.P2(9)
		p.P2(3214)
		p.P2(1155)
	})
	if !bytes.Equal(f.lastSent(), want) {
		t.Errorf("sent %x, want %x", f.lastSent(), want)
	}
}

func TestCastOnItemSendsOphedltWithSpellComponent(t *testing.T) {
	f := newFakeSession()
	if err := CastOnItem(f, "High Level Alchemy", 1127, 4, 3214); err != nil {
		t.Fatalf("CastOnItem: %v", err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.OPHELDT)
		p.P2(1127)
		p.P2(4)
		p.P2(3214)
		p.P2(1178)
	})
	if !bytes.Equal(f.lastSent(), want) {
		t.Errorf("sent %x, want %x", f.lastSent(), want)
	}

	f2 := newFakeSession()
	if err := CastOnItem(f2, "not a spell", 1127, 4, 3214); err != ErrUnknownSpell || f2.sendCount() != 0 {
		t.Fatalf("an unknown spell: err = %v, sends = %d", err, f2.sendCount())
	}
}

func TestTeleportSendsTheSpellButton(t *testing.T) {
	f := newFakeSession()
	if err := Teleport(f, "Camelot"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(1174)) {
		t.Fatalf("sent %x, want IF_BUTTON 1174", f.lastSent())
	}
	if err := Teleport(f, "nowhere"); err != ErrUnknownSpell {
		t.Fatalf("unknown teleport: err = %v", err)
	}
}
