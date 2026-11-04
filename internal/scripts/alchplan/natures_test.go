package alchplan

import (
	"reflect"
	"testing"
)

// The Alcher's restock draws its notes only against Nature runes (a user request: "if accounts dont have nature
// runes they shouldnt take things to alch"): the pack's or the bank's.
func TestItemsForNaturesAreTheListOnlyWhileNaturesExist(t *testing.T) {
	items := []string{"Rune platebody", "Adamant platelegs"}
	for _, c := range []struct {
		pack, bank int
		want       []string
	}{
		{1, 0, items}, {0, 1, items}, {40, 900, items},
		{0, 0, nil}, {-2, 0, nil},
	} {
		if got := ItemsForNatures(items, c.pack, c.bank); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ItemsForNatures(pack %d, bank %d) = %v, want %v", c.pack, c.bank, got, c.want)
		}
	}
	if got := ItemsForNatures(nil, 5, 5); len(got) != 0 {
		t.Errorf("an empty list stays empty: %v", got)
	}
}
