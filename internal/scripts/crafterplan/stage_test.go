package crafterplan

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Translated from rsb0t-scripts test/craftStage.test.ts.

func TestStageFor(t *testing.T) {
	cases := []struct {
		level int
		pin   string
		want  Stage
	}{
		{1, "Auto", StageWool},
		{9, "Auto", StageWool},
		{BowStringLevel, "Auto", StageFlax},
		{50, "Auto", StageFlax},
		{99, "Wool", StageWool},
		{10, "Flax", StageFlax},
		{9, "Flax", StageWool},
		{1, "Flax", StageWool},
	}
	for _, c := range cases {
		if got := StageFor(c.level, c.pin); got != c.want {
			t.Errorf("StageFor(%d, %q) = %s, want %s", c.level, c.pin, got, c.want)
		}
	}
	if !reflect.DeepEqual(StageOptions(), []string{"Auto", "Wool", "Flax"}) {
		t.Error("stage options")
	}
}

func TestLevelsToFlax(t *testing.T) {
	want := map[int]int{1: 9, 9: 1, 10: 0, 80: 0}
	for level, owed := range want {
		if got := LevelsToFlax(level); got != owed {
			t.Errorf("LevelsToFlax(%d) = %d, want %d", level, got, owed)
		}
	}
}

func TestSeersLayout(t *testing.T) {
	l := Seers()
	if l.Wheel.Level != 1 || l.Ladder.Level != 0 || l.Field.Level != 0 || l.Bank.Level != 0 {
		t.Error("the wheel is upstairs and everything else on the ground")
	}
	if tile.Chebyshev(l.Ladder, l.Wheel) > 4 {
		t.Error("the ladder stands below the wheel")
	}
}

func intp(v int) *int { return &v }

func TestWhereTheFlaxComesFrom(t *testing.T) {
	cases := []struct {
		name   string
		source string
		bank   *int
		want   bool
	}{
		{"picking ignores a full bank", PickFlax, intp(500), false},
		{"picking ignores an empty bank", PickFlax, intp(0), false},
		{"picking ignores an unread bank", PickFlax, nil, false},
		{"drawing from a stocked bank", BankFlax, intp(500), true},
		{"drawing the last stalk", BankFlax, intp(1), true},
		{"an empty bank sends it to the field", BankFlax, intp(0), false},
		{"an unread bank is worth one look", BankFlax, nil, true},
		{"flax put in later draws again", BankFlax, intp(120), true},
		{"an unknown setting picks", "", intp(500), false},
		{"a near-miss setting picks", "Bank", intp(500), false},
	}
	for _, c := range cases {
		if got := DrawsFlaxFromBank(c.source, c.bank); got != c.want {
			t.Errorf("%s: DrawsFlaxFromBank = %v", c.name, got)
		}
	}
	if !reflect.DeepEqual(FlaxSourceOptions(), []string{PickFlax, BankFlax, MuleFlax}) {
		t.Error("three options, picking first")
	}
	if KeepsBankFlax(PickFlax) || !KeepsBankFlax(BankFlax) {
		t.Error("only a bank being spun through holds its flax back")
	}
}
