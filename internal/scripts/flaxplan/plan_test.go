package flaxplan

import "testing"

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		in   Snapshot
		want Stage
	}{
		{"full pack always banks", Snapshot{PackFull: true, FlaxNearby: true}, StageBank},
		{"startup leftovers bank once", Snapshot{StartupPending: true, Haul: 3}, StageBank},
		{"startup empty pack does not force a bank trip", Snapshot{StartupPending: true, Haul: 0, FlaxNearby: true}, StagePick},
		{"flax nearby picks", Snapshot{FlaxNearby: true}, StagePick},
		{"no flax nearby travels", Snapshot{FlaxNearby: false}, StageTravel},
		{"full pack beats flax nearby", Snapshot{PackFull: true, FlaxNearby: true, Haul: 28}, StageBank},
		{"startup pending but pack full still banks", Snapshot{StartupPending: true, PackFull: true, Haul: 28}, StageBank},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.in); got != c.want {
				t.Errorf("DecideStage(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsFlax(t *testing.T) {
	if !IsFlax("flax") || !IsFlax("  Flax  ") || !IsFlax("FLAX") {
		t.Error("IsFlax should match case-insensitively and trim whitespace")
	}
	if IsFlax("Flax seed") || IsFlax("") {
		t.Error("IsFlax should not match other names")
	}
}

func TestSettingsNormalised(t *testing.T) {
	s := Settings{}.Normalised()
	if s.FlaxName != Flax || s.PickOp != PickOp || s.BankBooth != BoothName {
		t.Errorf("blank settings should fall back to defaults, got %+v", s)
	}
	custom := Settings{FlaxName: " Special Flax ", PickOp: " Harvest ", BankBooth: " My Booth "}.Normalised()
	if custom.FlaxName != "Special Flax" || custom.PickOp != "Harvest" || custom.BankBooth != "My Booth" {
		t.Errorf("custom settings should be trimmed, not replaced, got %+v", custom)
	}
}

func TestSettingsVerbose(t *testing.T) {
	normal := Settings{LogDetail: "Normal"}
	if normal.Verbose() {
		t.Error("Normal should not be verbose")
	}
	verbose := Settings{LogDetail: "Verbose"}
	if !verbose.Verbose() {
		t.Error("Verbose should be verbose")
	}
	spaced := Settings{LogDetail: " verbose "}
	if !spaced.Verbose() {
		t.Error("Verbose should be case/whitespace insensitive")
	}
}

func TestDescribeStage(t *testing.T) {
	for _, s := range []Stage{StageBank, StagePick, StageTravel} {
		if DescribeStage(s) == "" {
			t.Errorf("DescribeStage(%v) should not be empty", s)
		}
	}
	if DescribeStage("bogus") != "bogus" {
		t.Error("DescribeStage should fall back to the raw string for an unknown stage")
	}
}
