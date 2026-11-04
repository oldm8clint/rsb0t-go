package fisherplan

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
)

func TestWhirlpoolIDs(t *testing.T) {
	for _, id := range []int{403, 404, 405, 406} {
		if !IsWhirlpool(id) {
			t.Errorf("%d should be a whirlpool", id)
		}
	}
	if IsWhirlpool(316) || IsWhirlpool(402) || IsWhirlpool(407) {
		t.Error("an ordinary fishing spot type read as a whirlpool")
	}
}

func TestSellCountForRaisesTheNeedFromOneFish(t *testing.T) {
	// Harry pays 70% of 17 for the first mackerel (11), then 69% (11), ... so 20 coins takes two.
	if got := SellCountFor(20, "Raw mackerel", 27); got != 2 {
		t.Errorf("SellCountFor(20, mackerel, 27) = %d, want 2", got)
	}
	// Never more than held.
	if got := SellCountFor(1000, "Raw shrimps", 3); got != 3 {
		t.Errorf("SellCountFor(1000, shrimps, 3) = %d, want 3", got)
	}
	// Offers at least one even when the plan is empty (need already met).
	if got := SellCountFor(0, "Raw cod", 5); got != 1 {
		t.Errorf("SellCountFor(0, cod, 5) = %d, want 1", got)
	}
}

func TestSellOrderMostValuableFirstWithoutRefused(t *testing.T) {
	pack := []freshstart.PackItem{
		{Name: "Raw mackerel", Count: 1},
		{Name: "Small fishing net", Count: 1},
		{Name: "Raw bass", Count: 1},
		{Name: "Raw mackerel", Count: 1},
		{Name: "Raw cod", Count: 1},
		{Name: "Seaweed", Count: 1},
	}
	got := SellOrder(pack, map[string]bool{})
	want := []string{"Raw bass", "Raw cod", "Raw mackerel"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SellOrder = %v, want %v", got, want)
	}
	got = SellOrder(pack, map[string]bool{"raw bass": true})
	want = []string{"Raw cod", "Raw mackerel"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SellOrder with bass refused = %v, want %v", got, want)
	}
}
