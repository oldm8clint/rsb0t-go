package essenceplan

import "testing"

func TestPickaxeHeadPieces(t *testing.T) {
	if !IsPickaxeHandle(" pickaxe handle") || !IsPickaxeHead("Pickaxe head ") || IsPickaxeHead("Axe head") || IsPickaxeHandle("Bronze pickaxe") {
		t.Fatal("the event's pieces are matched by content's names only")
	}
	if PickaxeHeadSearchRadius < 7 {
		t.Fatalf("content throws the head up to 7 tiles away; the search covers %d", PickaxeHeadSearchRadius)
	}
	for _, c := range []struct {
		held, worn bool
		want       int
	}{{false, false, 1}, {true, false, 0}, {false, true, 2}, {true, true, 1}} {
		if got := PickaxeHeadSlotsNeeded(c.held, c.worn); got != c.want {
			t.Errorf("PickaxeHeadSlotsNeeded(%v, %v) = %d, want %d", c.held, c.worn, got, c.want)
		}
	}
}
