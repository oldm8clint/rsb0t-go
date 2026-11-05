package questplan

// Shades of Mort'ton's stay at the temple: the price asked in the middle of the stay, of the pack in hand (MtStayVerdict), as against the one price the entry's gate makes.

import (
	"strings"
	"testing"
)

func TestMorttonStayVerdictEndsTheStayOnTheBitesAndTheShades(t *testing.T) {
	at := MtTempleInside
	// A strong account with its food stays; the verdict is the entry gate's.
	strong := moSnap(MtUlsquireTemple, at)
	if v := MtStayVerdict(strong); !v.OK {
		t.Errorf("a strong account with twelve sharks: %s", v.Why)
	}
	// Under the floor of bites the stay ends whatever the levels are, saying how many are left.
	for sharks := 0; sharks < MtStayMinBites; sharks++ {
		v := MtStayVerdict(moSnap(MtUlsquireTemple, at, moHold(ObjShark, sharks)))
		if v.OK || !strings.Contains(v.Why, "bite(s) of food left") || !strings.Contains(v.Why, "floor") {
			t.Errorf("%d sharks: %+v", sharks, v)
		}
	}
	// Two bites is the floor itself: the verdict is then the entry gate's alone (the shades' price), and a strong account can pay it only with more food than two sharks.
	two := MtStayVerdict(moSnap(MtUlsquireTemple, at, moHold(ObjShark, MtStayMinBites)))
	if want := MtShadesReady(moSnap(MtUlsquireTemple, at, moHold(ObjShark, MtStayMinBites))); two.OK != want.OK {
		t.Errorf("at the floor the stay's verdict %+v is not the entry gate's %+v", two, want)
	}
	// The pack that entered fit can leave unfit: the same account, food spent down by the shades, no longer takes a pair, and the verdict says which gate it is.
	weak := moSnap(MtUlsquireTemple, at, moLevel("attack", 10), moLevel("strength", 10), moLevel("defence", 5), func(s *Snapshot) { s.MaxHP, s.HP = 30, 30 })
	if v := MtStayVerdict(weak); v.OK || !strings.Contains(v.Why, "two Loar shades") {
		t.Errorf("a weak account at the temple: %+v", v)
	}
	// Low hitpoints are not a reason while there is food to eat them back with (the price tops up from the bites first), and are one when there is not.
	hurt := moSnap(MtUlsquireTemple, at, func(s *Snapshot) { s.HP = 8 })
	if v := MtStayVerdict(hurt); !v.OK {
		t.Errorf("8 of 60 hitpoints with twelve sharks: %s", v.Why)
	}
	if v := MtStayVerdict(moSnap(MtUlsquireTemple, at, func(s *Snapshot) { s.HP = 8 }, moHold(ObjShark, 0))); v.OK {
		t.Error("8 of 60 hitpoints with no food stays at the temple")
	}
	// An unread stat is no reason to leave (as every other gate here treats an unread stat), but the bites are always counted.
	if v := MtStayVerdict(moSnap(MtUlsquireTemple, at, func(s *Snapshot) { s.MaxHP = 0 })); !v.OK {
		t.Errorf("an account whose hitpoints are unread: %+v", v)
	}
	if v := MtStayVerdict(moSnap(MtUlsquireTemple, at, func(s *Snapshot) { s.MaxHP = 0 }, moHold(ObjShark, 1))); v.OK {
		t.Error("an account whose hitpoints are unread stays on its last bite")
	}
}
