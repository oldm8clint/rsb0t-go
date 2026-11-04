package controlpanel

import (
	"encoding/json"
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
)

// A dialogue that opens AFTER the view is already open reaches the browser only as a delta.
// Before 2026-09-21 the delta had no head/anim section at all, so the head arrived only if
// the dialogue happened to be open when the view was first opened — which is almost never.
func TestUIDeltaCarriesADialogueThatOpensMidView(t *testing.T) {
	prev := gamestate.RenderUI{Version: 1, Main: -1, Side: -1, ChatModal: -1, Overlay: -1}
	next := gamestate.RenderUI{
		Version: 1, Main: -1, Side: -1, ChatModal: 4882, Overlay: -1,
		Texts: map[int]string{4884: "Hans"},
		Heads: map[int]gamestate.InterfaceHead{4883: {Type: 2, ID: 0}},
		Anims: map[int]int{4883: 588},
	}
	d := diffStreamUI(&prev, &next)
	if d.empty() {
		t.Fatal("the delta is empty for a dialogue that just opened")
	}
	if d.Heads[4883] == nil || d.Heads[4883].Type != 2 {
		t.Fatalf("head not in the delta: %+v", d.Heads)
	}
	if d.Anims[4883] == nil || *d.Anims[4883] != 588 {
		t.Fatalf("anim not in the delta: %+v", d.Anims)
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"heads"`, `"anims"`, `"4883"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("delta json %s is missing %s", body, want)
		}
	}
}

// The other direction: closing the dialogue must remove the keys, not leave a stale head
// drawn over the next interface.
func TestUIDeltaRemovesClearedVisualState(t *testing.T) {
	prev := gamestate.RenderUI{Version: 1, ChatModal: 4882, Overlay: 197,
		Heads:  map[int]gamestate.InterfaceHead{4883: {Type: 2, ID: 0}},
		Hidden: map[int]bool{1234: true}}
	next := gamestate.RenderUI{Version: 1, ChatModal: -1, Overlay: -1}
	d := diffStreamUI(&prev, &next)
	if v, ok := d.Heads[4883]; !ok || v != nil {
		t.Fatalf("head %v,%v; want an explicit null", v, ok)
	}
	if v, ok := d.Hidden[1234]; !ok || v != nil {
		t.Fatalf("hidden %v,%v; want an explicit null", v, ok)
	}
	if d.Overlay == nil || *d.Overlay != -1 {
		t.Fatalf("overlay = %v, want -1", d.Overlay)
	}
}

// An unchanged observation must still cost nothing: the new sections must not make every
// delta non-empty, which would defeat the whole transport.
func TestUIDeltaStaysEmptyWhenNothingChanged(t *testing.T) {
	ui := gamestate.RenderUI{Version: 1, Main: -1, Side: -1, ChatModal: -1, Overlay: -1,
		Heads: map[int]gamestate.InterfaceHead{4883: {Type: 2}}, Anims: map[int]int{4883: 588},
		Hidden: map[int]bool{12: false}, ScrollPos: map[int]int{9: 3},
		Positions: map[int]gamestate.InterfacePoint{4: {X: 1, Y: 2}}}
	same := ui
	if d := diffStreamUI(&ui, &same); !d.empty() {
		t.Fatalf("delta is not empty for an unchanged observation: %+v", d)
	}
}
