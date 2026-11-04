package donorplan

import "testing"

// The items live donor spare1 stalled on (2026-09-19) stay in the bank; fish and coins still go.
func TestContentUntradeablesStayWithTheDonor(t *testing.T) {
	r := Rules{}
	for _, row := range []ItemRow{
		{Name: "Fishing pass", ID: 27}, {Name: "Ghostspeak amulet", ID: 552}, {Name: "Mime mask", ID: 3057},
		{Name: "Glarial's pebble", ID: 294}, {Name: "Iron key", ID: 2945},
	} {
		if !r.Stays(row) {
			t.Errorf("%s (%d) would be handed to the mule", row.Name, row.ID)
		}
	}
	for _, row := range []ItemRow{{Name: "Raw shark", ID: 383}, {Name: "Raw lobster", ID: 377}, {Name: "Coins", ID: 995}} {
		if r.Stays(row) {
			t.Errorf("%s (%d) wrongly kept as untradeable", row.Name, row.ID)
		}
	}
	rows := HandoffRows([]ItemRow{{Name: "Raw shark", ID: 383, Count: 50}, {Name: "Fishing pass", ID: 27, Count: 1}}, r)
	if len(rows) != 1 || rows[0].ID != 383 {
		t.Fatalf("hand-off rows = %+v, want only the shark", rows)
	}
}
