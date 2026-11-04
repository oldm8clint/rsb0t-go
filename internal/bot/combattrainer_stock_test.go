package bot

import "testing"

// Telling an empty stock line apart from an empty purse is the difference between filling the
// shield slot and never filling it.
//
// Content answers a buy against an empty line with "The shop has run out of stock."
// (shop/scripts/shop.rs2:80), and several of this build's lines are permanently empty: Cassie
// lists a mithril square shield at stock 0, and World.ts:1183 tops a line up only TOWARD its
// stockcount, so a 0 never rises. Read as an affordability problem it cost three cross-map trips
// to Falador, each of which has to be paid for with a kill, before the plan dropped a tier —
// longer than any measured run has lasted. An empty purse must still get its retries, because
// that refusal really does come right once the drops have paid for it.
func TestOutOfStockSaidSeparatesAnEmptyShelfFromAnEmptyPurse(t *testing.T) {
	for _, c := range []struct {
		name string
		said string
		want bool
	}{
		{"content's own line, as the server sends it", "The shop has run out of stock.\n", true},
		{"the same line without the newline", "The shop has run out of stock.", true},
		{"case is not meaningful on the wire", "THE SHOP HAS RUN OUT OF STOCK.", true},
		{"nothing was said at all", "", false},
		{"an empty purse is not an empty shelf", "You don't have enough coins.", false},
		{"a level refusal is neither", "You are not a high enough level to use this item.", false},
	} {
		if got := outOfStockSaid(c.said); got != c.want {
			t.Errorf("%s: outOfStockSaid(%q) = %v, want %v", c.name, c.said, got, c.want)
		}
	}
}
