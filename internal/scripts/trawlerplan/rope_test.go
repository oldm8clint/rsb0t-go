package trawlerplan

import (
	"strings"
	"testing"
)

// Rope is the menders' kit: Crafting NetCrafting and up buys KitMendRope (the net crew NetCrewRope), the rest buy KitRope, which is 0.
func TestKitRopeForGoesToTheMenders(t *testing.T) {
	set := DefaultSettings()
	if set.KitRope != 0 || set.KitMendRope != 8 {
		t.Fatalf("defaults: kitRope %d kitMendRope %d", set.KitRope, set.KitMendRope)
	}
	for _, c := range []struct {
		name     string
		crafting int
		net      bool
		want     int
	}{
		{"unknown Crafting takes the plain kit", 0, false, 0},
		{"Crafting 1 cannot mend", 1, false, 0},
		{"Crafting 39 cannot mend", 39, false, 0},
		{"Crafting 40 mends", 40, false, 8},
		{"Crafting 96 mends", 96, false, 8},
		{"a net crew member carries more", 55, true, 10},
		{"net crew below the mending level is no net crew", 20, true, 0},
	} {
		if got := KitRopeFor(c.crafting, set, c.net); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	// A written kitRope still buys for the rest, and a mender never carries less than it.
	set.KitRope, set.KitMendRope = 2, 1
	if KitRopeFor(10, set, false) != 2 || KitRopeFor(60, set, false) != 2 || KitRopeFor(60, set, true) != NetCrewRope {
		t.Errorf("kitRope 2, kitMendRope 1: %d %d %d", KitRopeFor(10, set, false), KitRopeFor(60, set, false), KitRopeFor(60, set, true))
	}
	// kitMendRope 0 turns the menders' kit off, the net crew's with it.
	set.KitRope, set.KitMendRope = 0, 0
	if KitRopeFor(60, set, true) != 0 {
		t.Errorf("kitMendRope 0: %d", KitRopeFor(60, set, true))
	}
}

func TestNeedsRopeIsAMenderUnderTheLowMark(t *testing.T) {
	set := DefaultSettings()
	for _, c := range []struct {
		crafting, held int
		want           bool
	}{
		{60, 0, true}, {60, MendRopeLow - 1, true}, {60, MendRopeLow, false}, {60, 8, false},
		{39, 0, false}, {0, 0, false},
	} {
		if got := NeedsRope(c.crafting, c.held, set); got != c.want {
			t.Errorf("Crafting %d holding %d: %v, want %v", c.crafting, c.held, got, c.want)
		}
	}
	set.KitMendRope = 0
	if NeedsRope(60, 0, set) {
		t.Error("kitMendRope 0 asks for no Rope")
	}
	set.KitMendRope = 2
	if NeedsRope(60, 2, set) || !NeedsRope(60, 1, set) {
		t.Error("a kit of 2 is low under 2, not under 3")
	}
}

func TestMendRopeSettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{"kitMendRope": "12", "kitRope": 1})
	if err != nil || s.KitMendRope != 12 || s.KitRope != 1 {
		t.Fatalf("decoded %+v %v", s, err)
	}
	for raw, want := range map[string]map[string]interface{}{
		"outside 0..20":   {"kitMendRope": 21},
		"leaves no room":  {"kitMendRope": 20, "kitBuckets": 10},
		"not a whole num": {"kitMendRope": "lots"},
		"unknown key":     {"kitMendRopes": 3},
	} {
		if _, err := DecodeSettings(want); err == nil || !strings.Contains(err.Error(), strings.Split(raw, " ")[0]) {
			t.Errorf("%v: %v, want %q", want, err, raw)
		}
	}
}

func TestRopeShortIsTheMendersMissingKit(t *testing.T) {
	set := DefaultSettings()
	for _, c := range []struct {
		crafting, held int
		net            bool
		want           int
	}{
		{60, 0, false, 8}, {60, 5, false, 3}, {60, 8, false, 0}, {60, 12, false, 0}, {60, 4, true, 6},
		{39, 0, false, 0}, {0, 0, false, 0},
	} {
		if got := RopeShort(c.crafting, c.held, set, c.net); got != c.want {
			t.Errorf("Crafting %d holding %d net %v: short %d, want %d", c.crafting, c.held, c.net, got, c.want)
		}
	}
}
