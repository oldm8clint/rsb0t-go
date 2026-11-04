package alchplan

import "testing"

func TestIsFireStaff(t *testing.T) {
	if !IsFireStaff("mystic fire staff") {
		t.Error("case-insensitive match should work")
	}
	if IsFireStaff("Staff of air") {
		t.Error("a non-fire staff should not match")
	}
}

func TestChooseSpell(t *testing.T) {
	if s := ChooseSpell(AutoSpell, 10); s != nil {
		t.Errorf("magic 10 should castable neither spell, got %+v", s)
	}
	if s := ChooseSpell(AutoSpell, 21); s == nil || s.Name != LowAlch.Name {
		t.Errorf("magic 21 auto should pick low alch, got %+v", s)
	}
	if s := ChooseSpell(AutoSpell, 55); s == nil || s.Name != HighAlch.Name {
		t.Errorf("magic 55 auto should pick high alch, got %+v", s)
	}
	if s := ChooseSpell(HighAlch.Name, 30); s != nil {
		t.Errorf("explicit high alch below level should be nil, got %+v", s)
	}
	if s := ChooseSpell(LowAlch.Name, 99); s == nil || s.Name != LowAlch.Name {
		t.Errorf("explicit low alch at any sufficient level should stay low alch, got %+v", s)
	}
}

func TestParseItems(t *testing.T) {
	got := ParseItems(" Rune platebody, Rune platebody ,Nature rune, , Adamant kiteshield")
	want := []string{"Rune platebody", "Adamant kiteshield"}
	if len(got) != len(want) {
		t.Fatalf("ParseItems = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseItems = %v, want %v", got, want)
		}
	}
}

func TestCastsAffordable(t *testing.T) {
	if got := CastsAffordable(HighAlch, 10, 100, false); got != 10 {
		t.Errorf("nature-bound (5 fire/cast * 10 = 50, held 100): got %d, want 10", got)
	}
	if got := CastsAffordable(HighAlch, 10, 20, false); got != 4 {
		t.Errorf("fire-bound (20/5=4): got %d, want 4", got)
	}
	if got := CastsAffordable(HighAlch, 10, 0, true); got != 10 {
		t.Errorf("with a fire staff, fire runes should not bound the count: got %d, want 10", got)
	}
	if got := CastsAffordable(HighAlch, 0, 100, false); got != 0 {
		t.Errorf("no nature runes should mean zero casts: got %d", got)
	}
}

func TestRunesToWithdraw(t *testing.T) {
	nature, fire := RunesToWithdraw(HighAlch, 100, 10, 20, false)
	if nature != 90 || fire != 480 {
		t.Errorf("RunesToWithdraw = %d,%d, want 90,480", nature, fire)
	}
	nature2, fire2 := RunesToWithdraw(HighAlch, 100, 10, 0, true)
	if nature2 != 90 || fire2 != 0 {
		t.Errorf("with a fire staff no fire runes should be requested: got %d,%d", nature2, fire2)
	}
}

func TestNextItem(t *testing.T) {
	counts := map[string]int{"A": 0, "B": 3}
	got, ok := NextItem([]string{"A", "B"}, func(n string) int { return counts[n] })
	if !ok || got != "B" {
		t.Errorf("NextItem = %q,%v, want B,true", got, ok)
	}
	if _, ok := NextItem([]string{"A"}, func(n string) int { return 0 }); ok {
		t.Error("an exhausted list should report false")
	}
}

func TestItemToWithdraw(t *testing.T) {
	bank := map[string]int{"A": 0, "B": 40}
	name, count, ok := ItemToWithdraw([]string{"A", "B"}, func(n string) int { return bank[n] }, 100)
	if !ok || name != "B" || count != 40 {
		t.Errorf("ItemToWithdraw = %q,%d,%v, want B,40,true", name, count, ok)
	}
	_, _, ok2 := ItemToWithdraw([]string{"A"}, func(n string) int { return bank[n] }, 100)
	if ok2 {
		t.Error("an exhausted bank should report false")
	}
}

func TestChatLineClassifiers(t *testing.T) {
	if !IsNotEnoughRunesMessage("You do not have enough Nature Runes to cast this spell.") {
		t.Error("should classify a not-enough-runes refusal")
	}
	if !IsLevelTooLowMessage("You need a magic level of 55 to cast this spell.") {
		t.Error("should classify a level-too-low refusal")
	}
	if IsNotEnoughRunesMessage("You cast High Level Alchemy.") || IsLevelTooLowMessage("You cast High Level Alchemy.") {
		t.Error("an ordinary line should not classify as either refusal")
	}
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		want Stage
	}{
		{"bank and pack both empty", Snapshot{BankEmpty: true, ItemInPack: false}, StageDone},
		{"staff wanted, not checked", Snapshot{StaffWanted: true, HasStaff: false, StaffChecked: false, ItemInPack: true, CastsInPack: 5}, StageGear},
		{"staff already checked, restock", Snapshot{StaffWanted: true, StaffChecked: true, ItemInPack: false}, StageRestock},
		{"no runes in pack", Snapshot{ItemInPack: true, CastsInPack: 0}, StageRestock},
		{"ready to alch", Snapshot{ItemInPack: true, CastsInPack: 5}, StageAlch},
	}
	for _, c := range cases {
		if got := DecideStage(c.s); got != c.want {
			t.Errorf("%s: DecideStage = %s, want %s", c.name, got, c.want)
		}
	}
}
