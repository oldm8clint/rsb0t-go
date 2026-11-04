package hops

import (
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/webwalk"
)

// TestEveryGliderHopHasItsMapButton: every Gnome pilot hop on the walk graph lands where a glider
// map button flies, so no flight falls back to closing the map.
func TestEveryGliderHopHasItsMapButton(t *testing.T) {
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Special == nil || e.Special.NPC != "Gnome pilot" {
			continue
		}
		n++
		if _, ok := gliderButton(gliderMapRoot, e.To); !ok {
			t.Errorf("%s lands at %v, which no glider map button flies to", e.Special.Label, e.To)
		}
	}
	if n == 0 {
		t.Fatal("no Gnome pilot hops on the graph: nothing was checked")
	}
}

// TestGliderButtonOnlyOnTheGliderMap: the buttons apply to the glider map alone; any other modal
// is still a message box to close.
func TestGliderButtonOnlyOnTheGliderMap(t *testing.T) {
	hub := webwalk.Point{X: 2465, Z: 3501, Level: 3}
	if com, ok := gliderButton(gliderMapRoot, hub); !ok || com != 825 {
		t.Fatalf("the hub on the glider map = %d, %v; want 825", com, ok)
	}
	if _, ok := gliderButton(gliderMapRoot+1, hub); ok {
		t.Fatal("a glider button was chosen on a modal that is not the glider map")
	}
	if _, ok := gliderButton(gliderMapRoot, webwalk.Point{X: 3222, Z: 3218}); ok {
		t.Fatal("a glider button was chosen for a tile no glider lands on")
	}
}

// TestGliderButtonNeedsTheCacheToAgree: a cache without that plain button keeps the old close.
func TestGliderButtonNeedsTheCacheToAgree(t *testing.T) {
	h := &Handler{d: Deps{Components: archive.Components{
		825: {ID: 825, ButtonType: archive.ButtonOK},
		826: {ID: 826, ButtonType: archive.ButtonClose},
	}}}
	if !h.isOKButton(825) {
		t.Fatal("a plain button the cache knows was refused")
	}
	if h.isOKButton(826) || h.isOKButton(827) {
		t.Fatal("a close button or an unknown component was accepted as the glider destination")
	}
}
