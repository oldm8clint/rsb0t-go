package boneplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestParseNames(t *testing.T) {
	got := ParseNames([]string{" Bones, Big Bones ", "bones", "Dragon bones"})
	want := []string{"bones", "big bones", "dragon bones"}
	if len(got) != len(want) {
		t.Fatalf("ParseNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseNames = %v, want %v", got, want)
		}
	}
}

func TestIsBoneName(t *testing.T) {
	names := []string{"bones"}
	cases := []struct {
		name string
		want bool
	}{
		{"Bones", true},
		{"Big bones", true},
		{"Wolf bones", true},
		{"Ashes", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsBoneName(c.name, names); got != c.want {
			t.Errorf("IsBoneName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBurialOp(t *testing.T) {
	if op, ok := BurialOp([]string{"Wear", "Bury", "Drop"}); !ok || op != "Bury" {
		t.Errorf("BurialOp = %q,%v, want Bury,true", op, ok)
	}
	if op, ok := BurialOp([]string{"Wear", "Scatter", "Drop"}); !ok || op != "Scatter" {
		t.Errorf("BurialOp = %q,%v, want Scatter,true", op, ok)
	}
	if _, ok := BurialOp([]string{"Wear", "Drop"}); ok {
		t.Error("BurialOp found an op on an item offering neither")
	}
}

func TestDisposalFor(t *testing.T) {
	names := []string{"bones"}
	bankNames := []string{"big bones"}
	if _, ok := DisposalFor("Shrimps", DisposalBury, names, bankNames); ok {
		t.Error("DisposalFor matched a non-bone")
	}
	if d, ok := DisposalFor("Bones", DisposalBury, names, bankNames); !ok || d != ToBury {
		t.Errorf("bury mode: got %v,%v", d, ok)
	}
	if d, ok := DisposalFor("Bones", DisposalBank, names, bankNames); !ok || d != ToBank {
		t.Errorf("bank mode: got %v,%v", d, ok)
	}
	if d, ok := DisposalFor("Big bones", DisposalSplit, names, bankNames); !ok || d != ToBank {
		t.Errorf("split mode, listed: got %v,%v", d, ok)
	}
	if d, ok := DisposalFor("Bones", DisposalSplit, names, bankNames); !ok || d != ToBury {
		t.Errorf("split mode, unlisted: got %v,%v", d, ok)
	}
}

func TestInRange(t *testing.T) {
	anchor := tile.Tile{X: 3200, Z: 3200, Level: 0}
	if !InRange(tile.Tile{X: 3210, Z: 3200, Level: 0}, &anchor, 15) {
		t.Error("within radius should be in range")
	}
	if InRange(tile.Tile{X: 3220, Z: 3200, Level: 0}, &anchor, 15) {
		t.Error("beyond radius should not be in range")
	}
	if InRange(tile.Tile{X: 3200, Z: 3200, Level: 1}, &anchor, 15) {
		t.Error("a different level should never be in range")
	}
	if !InRange(tile.Tile{X: 9999, Z: 9999, Level: 0}, &anchor, 0) {
		t.Error("radius 0 means the whole loaded scene")
	}
	if !InRange(tile.Tile{X: 1, Z: 1, Level: 0}, nil, 15) {
		t.Error("a nil anchor should always match")
	}
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		s    StageState
		want Stage
	}{
		{"bank source, holding bury bones", StageState{Source: SourceBank, BuryInPack: 2}, StageBury},
		{"bank source, empty", StageState{Source: SourceBank}, StageWithdraw},
		{"bury immediately when held", StageState{Source: SourceGround, BuryInPack: 1, BuryWhenFull: false}, StageBury},
		{"hold for a full pack while ground has bones", StageState{Source: SourceGround, BuryInPack: 1, BuryWhenFull: true, GroundBone: true}, StageCollect},
		{"pack full forces bury even when filling", StageState{Source: SourceGround, BuryInPack: 1, BuryWhenFull: true, PackFull: true}, StageBury},
		{"clearing forces bury", StageState{Source: SourceGround, BuryInPack: 1, BuryWhenFull: true, Clearing: true}, StageBury},
		{"no ground bone left forces bury even filling", StageState{Source: SourceGround, BuryInPack: 1, BuryWhenFull: true, GroundBone: false}, StageBury},
		{"bank bound bones, pack full", StageState{Source: SourceGround, BankInPack: 1, PackFull: true}, StageBank},
		{"bank bound bones, forced", StageState{Source: SourceGround, BankInPack: 1, ForceBank: true}, StageBank},
		{"returning", StageState{Source: SourceGround, Returning: true}, StageReturn},
		{"collect available", StageState{Source: SourceGround, GroundBone: true}, StageCollect},
		{"nothing left but bury bones held (not full, ground bare)", StageState{Source: SourceGround, BuryInPack: 1}, StageBury},
		{"nothing to do", StageState{Source: SourceGround}, StageWait},
	}
	for _, c := range cases {
		if got := DecideStage(c.s); got != c.want {
			t.Errorf("%s: DecideStage = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestStillClearing(t *testing.T) {
	if StillClearing(false, 0, true, true) {
		t.Error("nothing held should never be clearing")
	}
	if StillClearing(true, 3, false, false) {
		t.Error("burying as they come up never latches")
	}
	if !StillClearing(false, 3, true, true) {
		t.Error("a full pack should start clearing")
	}
	if !StillClearing(true, 1, false, true) {
		t.Error("clearing should hold once started, even with room to spare")
	}
}

func TestGoal(t *testing.T) {
	g := Goal(1000, 1000, 0, 10)
	if !g.Reached || g.Fraction != 1 {
		t.Errorf("reached goal: %+v", g)
	}
	g2 := Goal(1000, 100, 0, 0.5)
	if g2.Reached || g2.MinutesKnown {
		t.Errorf("too early to estimate: %+v", g2)
	}
	g3 := Goal(1000, 500, 0, 5)
	if g3.Reached || !g3.MinutesKnown || g3.Minutes <= 0 {
		t.Errorf("mid-run estimate: %+v", g3)
	}
}

func TestSettingsNormalised(t *testing.T) {
	s := Settings{Radius: 0, IdleMins: -5, GoalLevel: 500, Names: "", BankNames: " Big Bones "}.Normalised()
	if s.Radius != DefaultRadius {
		t.Errorf("radius fallback = %d", s.Radius)
	}
	if s.IdleMins != 0 {
		t.Errorf("idleMins clamp = %d", s.IdleMins)
	}
	if s.GoalLevel != MaxPrayerLevel {
		t.Errorf("goalLevel clamp = %d", s.GoalLevel)
	}
	if s.NameList()[0] != "bones" {
		t.Errorf("blank names should fall back to the default, got %v", s.NameList())
	}
	if s.BankNameList()[0] != "big bones" {
		t.Errorf("bankNames should be tidied, got %v", s.BankNameList())
	}
}

func TestSettingsMode(t *testing.T) {
	s := DefaultSettings()
	s.Source = "bank"
	s.Disposal = "bank"
	if s.Mode() != DisposalBury {
		t.Errorf("bank-sourced bones must always bury, got %s", s.Mode())
	}
	s2 := DefaultSettings()
	s2.Disposal = "split"
	if s2.Mode() != DisposalSplit {
		t.Errorf("ground-sourced split mode should stay split, got %s", s2.Mode())
	}
}
