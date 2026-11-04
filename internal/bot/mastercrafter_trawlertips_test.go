package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/mastercraftplan"
)

// The anchor tips the best arrows first: rune, adamant, mithril, steel, then pearl bolts (a user request).
func TestTrawlerTipsBestFirst(t *testing.T) {
	want := []int32{892, 890, 888, 886}
	for i, id := range want {
		if trawlerTips[i].product != id {
			t.Fatalf("trawlerTips[%d] makes %d, want %d", i, trawlerTips[i].product, id)
		}
	}
	if p := trawlerTips[len(trawlerTips)-2]; p.name != "Pearl bolts" {
		t.Fatalf("next to last pair makes %q, want pearl bolts", p.name)
	}
	if last := trawlerTips[len(trawlerTips)-1]; last.name != "Headless arrow" {
		t.Fatalf("last pair makes %q, want headless arrows (feathers on shafts)", last.name)
	}
	for _, p := range trawlerTips {
		if !trawlerTipItem(p.tip) || !trawlerTipItem(p.base) {
			t.Fatalf("%s: tip and base stay in the pack at the bank", p.name)
		}
	}
}

func TestTrawlerTipsWithoutTheirOtherHalfAreBanked(t *testing.T) {
	pack := map[int32]int{}
	held := func(id int32) int { return pack[id] }
	if trawlerTipUsable(44, held) {
		t.Fatal("rune arrowtips with no headless arrows stay in the pack")
	}
	pack[mastercraftplan.FeatherID], pack[mastercraftplan.ArrowShaftID] = 500, 500
	if !trawlerTipUsable(44, held) || !trawlerTipUsable(mastercraftplan.FeatherID, held) {
		t.Fatal("feathers and shafts make the headless arrows the tips go on")
	}
	pack = map[int32]int{mastercraftplan.HeadlessID: 100}
	if trawlerTipUsable(mastercraftplan.HeadlessID, held) {
		t.Fatal("headless arrows with no tips and no feathers stay in the pack")
	}
	pack[43] = 30
	if !trawlerTipUsable(mastercraftplan.HeadlessID, held) || !trawlerTipUsable(43, held) {
		t.Fatal("adamant tips and headless arrows are a pair")
	}
}

func TestMasterXPShort(t *testing.T) {
	for n, want := range map[int]string{512345: "512k", 1234567: "1.23M", 999: "999", 0: "0"} {
		if got := masterXPShort(n); got != want {
			t.Fatalf("masterXPShort(%d) = %q, want %q", n, got, want)
		}
	}
}
