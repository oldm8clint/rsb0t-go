package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

// The default post must stay the Catherby bank. A second fleet runs this same code with its
// mule posted there and never names postTile, so a changed default would relocate a running
// mule without anybody asking for it.
func TestMulePostDefaultsToCatherbyWhenUnset(t *testing.T) {
	catherby := fisherplan.Catherby().Bank
	for _, c := range []struct {
		name     string
		settings map[string]interface{}
	}{
		{"no settings at all", map[string]interface{}{}},
		{"nil settings", nil},
		{"other keys only", map[string]interface{}{"bank": true, "idleTimeoutSec": 60.0}},
		{"an unreadable tile", map[string]interface{}{"postTile": "nonsense"}},
		{"a wrong-typed value", map[string]interface{}{"postTile": true}},
	} {
		if got := fisherTileSetting(c.settings, "postTile", catherby); got != catherby {
			t.Errorf("%s: post = %+v, want the Catherby bank %+v", c.name, got, catherby)
		}
	}
	// A muleBot that never ran OnStart still posts somewhere real rather than at 0,0.
	if got := (&muleBot{}).postTile(); got != catherby {
		t.Errorf("a muleBot with no post reads %+v, want the Catherby bank %+v", got, catherby)
	}
}

// Every shape a JSON roster can produce for the Seers stand, the tile AJ's fleet posts at.
func TestMulePostReadsTheSeersStandInEveryShape(t *testing.T) {
	catherby := fisherplan.Catherby().Bank
	want := mulereceiverplan.SeersStand
	for _, v := range []interface{}{
		"2725,3491",
		"2725,3491,0",
		" 2725 , 3491 ",
		map[string]interface{}{"x": 2725.0, "z": 3491.0},
		map[string]interface{}{"x": 2725.0, "z": 3491.0, "level": 0.0},
		[]interface{}{2725.0, 3491.0},
		tile.Tile{X: 2725, Z: 3491},
	} {
		got := fisherTileSetting(map[string]interface{}{"postTile": v}, "postTile", catherby)
		if got != want {
			t.Errorf("postTile %v = %+v, want the Seers stand %+v", v, got, want)
		}
		if got == catherby {
			t.Errorf("postTile %v silently fell back to the Catherby default", v)
		}
	}
	b := &muleBot{post: want}
	if got := b.postTile(); got != want {
		t.Errorf("postTile() = %+v, want %+v", got, want)
	}
}

// The three cases OnStart tells apart out loud. Getting this wrong is the silent-typo bug:
// the bag hands back the default either way, so only this distinguishes "you asked for
// Catherby" from "your key did nothing".
func TestTileSettingSetTellsAKeyThatParsedFromOneThatDidNot(t *testing.T) {
	catherby := fisherplan.Catherby().Bank
	cases := []struct {
		name     string
		settings map[string]interface{}
		set      bool
		present  bool
	}{
		{"absent", map[string]interface{}{}, false, false},
		{"nil map", nil, false, false},
		{"readable", map[string]interface{}{"postTile": "2725,3491"}, true, true},
		{"readable and equal to the default", map[string]interface{}{"postTile": "2809,3441"}, true, true},
		{"present but unreadable", map[string]interface{}{"postTile": "nonsense"}, false, true},
		{"present but the wrong type", map[string]interface{}{"postTile": 4.0}, false, true},
		{"one coordinate only", map[string]interface{}{"postTile": "2725"}, false, true},
		{"too many coordinates", map[string]interface{}{"postTile": "2725,3491,0,0"}, false, true},
		{"a near-miss key name", map[string]interface{}{"postTiles": "2725,3491"}, false, false},
	}
	for _, c := range cases {
		if got := tileSettingSet(c.settings, "postTile"); got != c.set {
			t.Errorf("%s: tileSettingSet = %v, want %v", c.name, got, c.set)
		}
		if got := hasSetting(c.settings, "postTile"); got != c.present {
			t.Errorf("%s: hasSetting = %v, want %v", c.name, got, c.present)
		}
		// Whatever the case, an unreadable or absent key must leave the post on the default.
		if !c.set {
			if got := fisherTileSetting(c.settings, "postTile", catherby); got != catherby {
				t.Errorf("%s: post = %+v, want the default %+v", c.name, got, catherby)
			}
		}
	}
}

// The short walk home is about distance, not about Catherby: a mule posted at Seers gets it
// too. A nil pack is still never "safe", and a Catherby post is unaffected because everything
// within muleLocalRadius of that bank is already inside InCatherby's radius.
func TestMuleLocalWalkSafeShortHopDoesNotDependOnCatherby(t *testing.T) {
	seers := mulereceiverplan.SeersStand
	catherby := fisherplan.Catherby().Bank
	if muleLocalWalkSafe(nil, seers, seers, nil) {
		t.Error("no collision pack can never be a safe local walk")
	}
	// The distance branch is reached before any pack lookup, so a nil pack would crash if the
	// order were wrong; these cases prove the ordering as much as the answer.
	if !fisherplan.InCatherby(tile.Tile{X: catherby.X + 4, Z: catherby.Z}) {
		t.Fatal("a tile four east of the Catherby bank should be inside InCatherby")
	}
	if fisherplan.InCatherby(seers) {
		t.Fatal("the Seers stand is not in Catherby; this test assumes it is not")
	}
	if got := tile.Chebyshev(tile.Tile{X: seers.X + muleLocalRadius, Z: seers.Z}, seers); got != muleLocalRadius {
		t.Fatalf("Chebyshev = %d, want %d", got, muleLocalRadius)
	}
}

// A mule a tile off its post (in front of the next booth, or a step towards a donor) is at its
// post: walking that tile back is what closed trades it had just opened.
func TestAtPostAllowsOneTile(t *testing.T) {
	home := tile.Tile{X: 2809, Z: 3441}
	for _, c := range []struct {
		here tile.Tile
		want bool
	}{
		{tile.Tile{X: 2809, Z: 3441}, true},
		{tile.Tile{X: 2810, Z: 3441}, true},
		{tile.Tile{X: 2809, Z: 3440}, true},
		{tile.Tile{X: 2808, Z: 3440}, true},
		{tile.Tile{X: 2807, Z: 3441}, false},
		{tile.Tile{X: 2816, Z: 3438}, false},
		{tile.Tile{X: 2809, Z: 3441, Level: 1}, false},
	} {
		if got := atPost(c.here, home); got != c.want {
			t.Errorf("atPost(%v) = %v, want %v", c.here, got, c.want)
		}
	}
}
