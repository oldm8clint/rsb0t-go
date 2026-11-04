package fleetplan

import "testing"

func TestAntipoisonDosesReadsEveryForm(t *testing.T) {
	for want, name := range map[int]string{1: "Antipoison(1)", 2: "antipoison(2)", 3: " Antipoison(3) ", 4: "Antipoison(4)"} {
		if got := AntipoisonDoses(name); got != want {
			t.Errorf("AntipoisonDoses(%q) = %d, want %d", name, got, want)
		}
	}
	if AntipoisonDoses("Shark") != 0 {
		t.Error("a shark is not antipoison")
	}
}

func TestAntipoisonDueOnlyForAPoisonHitAfterTheLastDose(t *testing.T) {
	cases := []struct {
		poisoned  bool
		hit, dose int
		want      bool
		why       string
	}{
		{true, 100, 0, true, "poisoned, never dosed"},
		{true, 100, 120, false, "the dose came after the hit"},
		{true, 150, 120, true, "poisoned again after the dose"},
		{false, 150, 120, false, "the hit is too old to be ticking"},
	}
	for _, c := range cases {
		if got := AntipoisonDue(c.poisoned, c.hit, c.dose); got != c.want {
			t.Errorf("%s: AntipoisonDue = %v, want %v", c.why, got, c.want)
		}
	}
}

func TestKQSettingsCarryTwoAntipoisonByDefaultAndClamp(t *testing.T) {
	if got := DefaultKQSettings().Antipoison; got != 2 {
		t.Fatalf("default kqAntipoison %d, want 2", got)
	}
	s := DefaultKQSettings()
	s.Antipoison = 40
	if got := s.Normalised().Antipoison; got != 2 {
		t.Fatalf("kqAntipoison 40 normalised to %d, want the default 2", got)
	}
	s.Antipoison = 0
	if got := s.Normalised().Antipoison; got != 0 {
		t.Fatalf("kqAntipoison 0 must stay 0 (carry none), got %d", got)
	}
}
