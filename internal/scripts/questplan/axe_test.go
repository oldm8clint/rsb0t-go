package questplan

import "testing"

func axeSnap(wc int, bank map[int]int) Snapshot {
	return Snapshot{Levels: map[string]int{"woodcutting": wc}, BankKnown: bank != nil, BankIDs: bank,
		InvIDs: map[int]int{}, WornIDs: map[int]int{}}
}

// Olaf's kit asked for an iron axe by id with no source but the bank and the mule. The mule holds no iron axe (17
// steel, 19 bronze, 17 rune on 16 Oct 2026), so an account owning any other axe was "blocked" on it.
func TestTheAxeLineTakesAnyAxeTheBankHoldsThatTheLevelCanUse(t *testing.T) {
	for _, c := range []struct {
		name string
		wc   int
		bank map[int]int
		want int
	}{
		{"a steel axe at 40", 40, map[int]int{ObjSteelAxe: 1}, ObjSteelAxe},
		{"the best of several at 40", 40, map[int]int{ObjBronzeAxe: 1, ObjSteelAxe: 1, ObjAdamantAxe: 1}, ObjAdamantAxe},
		{"a rune axe is above 40", 40, map[int]int{ObjRuneAxe: 1, ObjMithrilAxe: 1}, ObjMithrilAxe},
		{"a rune axe at 41", 41, map[int]int{ObjRuneAxe: 1, ObjMithrilAxe: 1}, ObjRuneAxe},
		{"steel is above 5", 5, map[int]int{ObjSteelAxe: 1, ObjBronzeAxe: 1}, ObjBronzeAxe},
		{"an unknown level judges none too high", 0, map[int]int{ObjRuneAxe: 1}, ObjRuneAxe},
	} {
		l := AxeLine(axeSnap(c.wc, c.bank))
		if l.ID != c.want || len(l.Sources) != 0 || !l.Worn {
			t.Errorf("%s: line %+v, want the banked axe %d with no source", c.name, l, c.want)
		}
	}
}

func TestTheAxeLineBuysAnIronAxeWhenNoUsableAxeIsOwned(t *testing.T) {
	for name, s := range map[string]Snapshot{
		"an unread bank":     axeSnap(40, nil),
		"an empty bank":      axeSnap(40, map[int]int{}),
		"only a rune axe":    axeSnap(40, map[int]int{ObjRuneAxe: 1}),
		"a bank of no axes":  axeSnap(40, map[int]int{ObjLogs: 400}),
		"the empty snapshot": {},
	} {
		l := AxeLine(s)
		if l.ID != ObjIronAxe || len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Sources[0].Shop.NpcID != AemadShop.NpcID {
			t.Errorf("%s: line %+v, want an iron axe with Aemad's shop", name, l)
		}
	}
}

func TestAxeHeldSeesThePackAndTheWornSlot(t *testing.T) {
	s := axeSnap(40, nil)
	if AxeHeld(s) {
		t.Fatal("an empty pack holds an axe")
	}
	s.InvIDs[ObjSteelAxe] = 1
	if !AxeHeld(s) {
		t.Error("a steel axe in the pack is not held")
	}
	s = axeSnap(40, nil)
	s.WornIDs[ObjAdamantAxe] = 1
	if !AxeHeld(s) {
		t.Error("a worn adamant axe is not held")
	}
	// A banked axe is not a held one.
	if AxeHeld(axeSnap(40, map[int]int{ObjSteelAxe: 1})) {
		t.Error("a banked axe counts as held")
	}
}

// Every axe in Axes has a level, so AxeLine can never fail open on a new id.
func TestEveryAxeHasALevel(t *testing.T) {
	for _, a := range Axes {
		if AxeLevel[a] < 1 {
			t.Errorf("axe %d has no Woodcutting level", a)
		}
	}
}
