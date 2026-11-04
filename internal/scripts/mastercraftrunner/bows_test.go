package mastercraftrunner

import (
	"fmt"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
)

func fakeIDs(t *testing.T) {
	t.Helper()
	old := ResolveBowIDs
	t.Cleanup(func() { ResolveBowIDs = old })
	ResolveBowIDs = func(_ *cachecfg.Config, bow string) (bowstringrunner.IDs, error) {
		switch bow {
		case "Maple longbow":
			return bowstringrunner.IDs{Unstrung: 62, Strung: 851, String: 1777}, nil
		case "Yew shortbow":
			return bowstringrunner.IDs{Unstrung: 68, Strung: 857, String: 1777}, nil
		}
		return bowstringrunner.IDs{}, fmt.Errorf("cannot tell the unstrung %s from the strung one", bow)
	}
}

// A bow round hands the mule its strung bows, by their strung ids: a strung and an unstrung bow
// share a name, and the unstrung ones stay in the bank for the next round.
func TestBowProductsAreTheStrungBows(t *testing.T) {
	fakeIDs(t)
	strung := BowProducts(&fleetrunner.Deps{}, []string{"Maple longbow", "Mystery bow", "Yew shortbow"})
	if len(strung) != 2 || strung[0] != (Product{ID: 851, Name: "Maple longbow"}) || strung[1] != (Product{ID: 857, Name: "Yew shortbow"}) {
		t.Fatalf("strung %+v", strung)
	}
}
