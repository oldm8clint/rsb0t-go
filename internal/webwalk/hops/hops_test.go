package hops

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/webwalk"
)

func TestPickChoiceIsTheTSSubstringMatch(t *testing.T) {
	opts := []actions.Option{{ComID: 1, Text: "No thank you."}, {ComID: 2, Text: "Yes please."}}
	if got := pickChoice(opts, []string{"yes please."}); got != "Yes please." {
		t.Errorf("pickChoice = %q", got)
	}
	if got := pickChoice(opts, []string{"Search away"}); got != "" {
		t.Errorf("pickChoice matched %q with no listed choice", got)
	}
	if got := pickChoice(opts, []string{""}); got != "" {
		t.Errorf("an empty choice matched %q", got)
	}
}

func TestSupports(t *testing.T) {
	h := New(Deps{Config: &cachecfg.Config{}, Components: archive.Components{}})
	ship := &webwalk.Edge{Kind: webwalk.KindShip, LocName: "Seaman Thresnor", Action: "Pay-fare", LocID: -1, Special: &webwalk.Special{Label: "Port Sarim->Musa ship", NPC: "Seaman Thresnor", Choose: []string{"Yes please."}}}
	gate := &webwalk.Edge{Kind: webwalk.KindDoor, LocName: "Gate", Action: "Open", LocID: 2438, Special: &webwalk.Special{Label: "Karamja shipyard gate", Choose: []string{"Ka."}}}
	dig := &webwalk.Edge{Kind: webwalk.KindDungeon, LocName: "Mud patch", Action: "Dig", LocID: 2531, Special: &webwalk.Special{Label: "Plague City mud dig", UseItem: "Spade"}}
	cart := &webwalk.Edge{Kind: webwalk.KindShip, LocName: "Hajedy", Action: "Talk-to", LocID: -1}
	for _, c := range []struct {
		e    *webwalk.Edge
		want bool
	}{{ship, true}, {gate, true}, {dig, false}, {cart, true}} {
		if got := h.Supports(c.e); got != c.want {
			t.Errorf("Supports(%s) = %v, want %v", c.e.String(), got, c.want)
		}
	}
	if New(Deps{Config: &cachecfg.Config{}}).Supports(ship) {
		t.Error("a handler without the interface archive claimed a dialogue crossing")
	}
}

// Walking AT an npc cannot work: nothing stands on an occupied tile. Production logged
// "could not get beside Captain Barnaby at 2677,3275,0" followed by "not interactable",
// then burned a repath and ~24s, because approach aimed at the npc's own square.
func TestApproachTargetPicksANeighbourNotTheNpc(t *testing.T) {
	npc := webwalk.Point{X: 2677, Z: 3275, Level: 0}
	for _, from := range []webwalk.Point{
		{X: 2683, Z: 3272, Level: 0}, // the real approach in that log
		{X: 2670, Z: 3275, Level: 0},
		{X: 2677, Z: 3290, Level: 0},
		{X: 2690, Z: 3260, Level: 0},
	} {
		got := approachTargets(from, npc)[1] // [0] is the npc tile; [1] is the best neighbour
		if got == npc {
			t.Errorf("from %v the target is the npc's own tile", from)
		}
		if d := webwalk.Chebyshev(got, npc); d != 1 {
			t.Errorf("from %v target %v is %d tiles from the npc, want exactly 1", from, got, d)
		}
		if got.Level != npc.Level {
			t.Errorf("from %v target changed level", from)
		}
	}
}

// And it picks the SIDE we are coming from, so the walk is not longer than it needs to be.
func TestApproachTargetIsTheNearestNeighbour(t *testing.T) {
	npc := webwalk.Point{X: 100, Z: 100, Level: 0}
	for _, c := range []struct {
		from webwalk.Point
		want webwalk.Point
	}{
		{webwalk.Point{X: 90, Z: 100}, webwalk.Point{X: 99, Z: 100}},   // due west
		{webwalk.Point{X: 110, Z: 100}, webwalk.Point{X: 101, Z: 100}}, // due east
		{webwalk.Point{X: 100, Z: 90}, webwalk.Point{X: 100, Z: 99}},   // due south
		{webwalk.Point{X: 100, Z: 110}, webwalk.Point{X: 100, Z: 101}}, // due north
	} {
		if got := approachTargets(c.from, npc)[1]; got != c.want {
			t.Errorf("from %v got %v, want %v", c.from, got, c.want)
		}
	}
}
