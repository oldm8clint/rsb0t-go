package mulereceiverplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestDecideHome(t *testing.T) {
	cases := []struct {
		name string
		v    HomeView
		want HomeStep
	}{
		{"on the post", HomeView{AtHome: true, LocalSafe: true, SafeRoute: true}, HomeArrived},
		{"range room or beach", HomeView{LocalSafe: true, SafeRoute: true, Fare: 60}, HomeWalkLocal},
		{"Ardougne side, no coins, on foot", HomeView{SafeRoute: true, Fare: 0}, HomeWalk},
		{"Taverley with the fare", HomeView{SafeRoute: true, RouteFare: 60, Coins: 60, Fare: 60}, HomeWalk},
		{"Lumbridge with one fare of two", HomeView{SafeRoute: true, RouteFare: 60, Coins: 30, Fare: 60}, HomeFund},
		{"Taverley with one fare of two", HomeView{SafeRoute: true, RouteFare: 60, Coins: 30, Fare: 60, FarFromFundSpot: true}, HomeToFundSpot},
		{"Karamja with one fare of two", HomeView{SafeRoute: true, RouteFare: 60, Coins: 30, Fare: 30, OnKaramja: true}, HomeFund},
		{"Brimhaven dock, no coins", HomeView{OnKaramja: true, Coins: 0, Fare: 30}, HomeFund},
		{"Brimhaven dock, fare on hand but no plan", HomeView{OnKaramja: true, Coins: 30, Fare: 30}, HomeFund},
		{"Taverley gate, no coins", HomeView{FarFromFundSpot: true, Fare: 60}, HomeToFundSpot},
		{"eastern foothills, some coins", HomeView{FarFromFundSpot: true, Coins: 12, Fare: 60}, HomeToFundSpot},
		{"Lumbridge respawn, no coins", HomeView{Fare: 60}, HomeFund},
		{"fare on hand, planner found nothing", HomeView{Coins: 75, Fare: 60, FarFromFundSpot: true}, HomeWalk},
	}
	for _, c := range cases {
		if got := DecideHome(c.v); got != c.want {
			t.Errorf("%s: DecideHome(%+v) = %q, want %q", c.name, c.v, got, c.want)
		}
	}
}

// DecideHome never chooses a walk when neither the local walk nor the planner is wolf-free and
// the fare is short: that is exactly the state the live mule was in when it kept walking over
// White Wolf Mountain.
func TestDecideHomeNeverWalksWithoutASafeRoute(t *testing.T) {
	for _, karamja := range []bool{false, true} {
		for _, far := range []bool{false, true} {
			for coins := 0; coins < 60; coins += 7 {
				v := HomeView{Coins: coins, Fare: 60, OnKaramja: karamja, FarFromFundSpot: far}
				if got := DecideHome(v); got == HomeWalk || got == HomeWalkLocal {
					t.Fatalf("DecideHome(%+v) = %q with no safe route and the fare short", v, got)
				}
			}
		}
	}
}

func TestPlainRouteSafe(t *testing.T) {
	// A 3x3 danger square centred on 10,10.
	danger := func(p tile.Tile) bool { return p.Level == 0 && p.X >= 9 && p.X <= 11 && p.Z >= 9 && p.Z <= 11 }
	from := tile.Tile{X: 0, Z: 10}
	if PlainRouteSafe(from, false, nil, danger) {
		t.Fatal("a failed route is not safe")
	}
	if !PlainRouteSafe(from, true, nil, danger) {
		t.Fatal("an empty route from a safe tile is safe")
	}
	// Straight through the square: every corner is outside, but the tiles between are not.
	through := []tile.Tile{{X: 20, Z: 10}}
	if PlainRouteSafe(from, true, through, danger) {
		t.Fatal("a route whose corners skip over the danger square must still be unsafe")
	}
	// Around it.
	around := []tile.Tile{{X: 5, Z: 15}, {X: 15, Z: 15}, {X: 20, Z: 10}}
	if !PlainRouteSafe(from, true, around, danger) {
		t.Fatal("a route around the square is safe")
	}
	// Starting inside and walking out is judged on the tiles walked, start excluded.
	out := []tile.Tile{{X: 10, Z: 20}}
	if PlainRouteSafe(tile.Tile{X: 10, Z: 10}, true, out, danger) {
		t.Fatal("11 is still in the square on the way out")
	}
	if !PlainRouteSafe(tile.Tile{X: 10, Z: 11}, true, []tile.Tile{{X: 10, Z: 12}, {X: 10, Z: 20}}, func(p tile.Tile) bool { return danger(p) && p != (tile.Tile{X: 10, Z: 11}) }) {
		t.Fatal("the start tile itself must not count")
	}
	// A level change lands on the corner directly.
	up := []tile.Tile{{X: 10, Z: 10, Level: 1}, {X: 20, Z: 10, Level: 1}}
	if !PlainRouteSafe(from, true, up, danger) {
		t.Fatal("danger is level 0 only here; a route on level 1 is safe")
	}
}
