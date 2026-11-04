package bankfletchplan

import "testing"

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		in   Snapshot
		want Stage
	}{
		{"nothing held banks", Snapshot{}, StageBank},
		{"only knife held banks", Snapshot{HasKnife: true}, StageBank},
		{"only logs held banks", Snapshot{HasLogs: true}, StageBank},
		{"both held fletches", Snapshot{HasKnife: true, HasLogs: true}, StageFletch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.in); got != c.want {
				t.Errorf("DecideStage(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsLogsIsKnife(t *testing.T) {
	if !IsLogs("logs") || !IsLogs(" Logs ") {
		t.Error("IsLogs should match case-insensitively and trim whitespace")
	}
	if IsLogs("Oak logs") || IsLogs("") {
		t.Error("IsLogs should not match other names")
	}
	if !IsKnife("KNIFE") {
		t.Error("IsKnife should match case-insensitively")
	}
	if IsKnife("Dagger") {
		t.Error("IsKnife should not match other names")
	}
}

func TestSettingsNormalised(t *testing.T) {
	s := Settings{}.Normalised()
	if s.Material != Logs {
		t.Errorf("blank material should fall back to Logs, got %q", s.Material)
	}
	custom := Settings{Material: " Oak logs "}.Normalised()
	if custom.Material != "Oak logs" {
		t.Errorf("custom material should be trimmed, not replaced, got %q", custom.Material)
	}
}

func TestDescribeStage(t *testing.T) {
	for _, s := range []Stage{StageBank, StageFletch} {
		if DescribeStage(s) == "" {
			t.Errorf("DescribeStage(%v) should not be empty", s)
		}
	}
}
