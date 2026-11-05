package randomevents

import "testing"

func TestNextToolHeadStep(t *testing.T) {
	cases := []struct {
		name string
		s    toolHeadSituation
		want toolHeadStep
	}{
		{"no handle", toolHeadSituation{HeadOnGround: true, Free: 5}, stepNothing},
		{"handle but no head anywhere", toolHeadSituation{HandleInPack: true, Free: 5}, stepNothing},
		{"head on the ground, room", toolHeadSituation{HandleInPack: true, HeadOnGround: true, Free: 3}, stepTake},
		{"head on the ground, full pack, something to drop", toolHeadSituation{HandleInPack: true, HeadOnGround: true, CanMakeRoom: true}, stepMakeRoom},
		{"head on the ground, full pack, nothing to drop", toolHeadSituation{HandleInPack: true, HeadOnGround: true}, stepNoRoom},
		{"worn handle needs two slots for a head on the ground", toolHeadSituation{HandleWorn: true, HeadOnGround: true, Free: 1, CanMakeRoom: true}, stepMakeRoom},
		{"worn handle, head taken", toolHeadSituation{HandleWorn: true, HeadInPack: true, Free: 1}, stepUnequip},
		{"worn handle, head in pack, full pack", toolHeadSituation{HandleWorn: true, HeadInPack: true}, stepNoRoom},
		{"both in the pack", toolHeadSituation{HandleInPack: true, HeadInPack: true}, stepAttach},
		{"handle in the pack and worn: attach", toolHeadSituation{HandleInPack: true, HandleWorn: true, HeadInPack: true}, stepAttach},
	}
	for _, c := range cases {
		if got := nextToolHeadStep(c.s); got != c.want {
			t.Errorf("%s: step %d, want %d", c.name, got, c.want)
		}
	}
}

func TestToolPieceNames(t *testing.T) {
	for name, want := range map[string]string{"Axe handle": "axe", "pickaxe HANDLE ": "pickaxe", "Rune axe": "", "Pickaxe head": ""} {
		if got := toolOfHandle(name); got != want {
			t.Errorf("toolOfHandle(%q) = %q", name, got)
		}
	}
	for name, want := range map[string]string{"Axe head": "axe", "Pickaxe head": "pickaxe", "Axe handle": ""} {
		if got := toolOfHead(name); got != want {
			t.Errorf("toolOfHead(%q) = %q", name, got)
		}
	}
	if droppableForHead("Rune pickaxe") || droppableForHead("Axe head") || droppableForHead("Pickaxe handle") || !droppableForHead("Oak logs") {
		t.Error("droppableForHead")
	}
}
