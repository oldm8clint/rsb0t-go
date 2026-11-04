package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
)

// A kit asked for by QUALIFIED name ("Yew shortbow id68", the unstrung bow) is counted by obj id:
// the unstrung bow and its note, never the strung bow that shares its display name (content 289:
// obj 68 and obj 857 are both "Yew shortbow", each with its own note, 69 and 858). The plain name
// still counts every obj of the name, as it always did.
func TestPackCountsReadAQualifiedNameByObjId(t *testing.T) {
	cfg := realCfg(t)
	const (
		unstrung, unstrungNote = 68, 69
		strung, strungNote     = 857, 858
	)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: unstrungNote, Count: 200}, {ID: unstrung, Count: 1}, {ID: strung, Count: 1}, {ID: strung, Count: 1}, {ID: strungNote, Count: 70},
	})
	d := &Deps{Session: sim, Config: cfg}
	for _, tc := range []struct {
		name          string
		named, unnote int
	}{
		{"Yew shortbow id68", 201, 1},
		{"Yew shortbow id857", 72, 2},
		{"Yew shortbow", 273, 3},
		{"yew shortbow ID68", 201, 1},
		{"Yew longbow id66", 0, 0},
		{"Cake", 0, 0},
	} {
		if got := CountPackNamed(d, tc.name); got != tc.named {
			t.Errorf("countPackNamed(%q) = %d, want %d", tc.name, got, tc.named)
		}
		if got := CountPackUnnoted(d, tc.name); got != tc.unnote {
			t.Errorf("countPackUnnoted(%q) = %d, want %d", tc.name, got, tc.unnote)
		}
	}
	// What a kit ask leaves behind: the notes of the line that was asked, apart from the items.
	got, notes := kitGotFromPack(d, []fleetplan.ItemCount{{Item: "Yew shortbow id68", Count: 200}}, map[string]int{})
	if got["Yew shortbow id68"] != 1 || notes["Yew shortbow id68"] != 200 {
		t.Errorf("kitGotFromPack: got %v notes %v, want the one unstrung bow and 200 unstrung notes", got, notes)
	}
}
