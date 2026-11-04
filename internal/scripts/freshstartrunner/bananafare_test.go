package freshstartrunner

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/tile"
	"testing"
)

type bananaTypes map[int]cachecfg.Loc

func (types bananaTypes) Loc(id int) (cachecfg.Loc, bool) { v, ok := types[id]; return v, ok }

// An exhausted tree still offers Search. A live replacement must remove the
// old productive map tree from consideration and allow the next tree to win.
func TestBananaTreeSkipsLiveExhaustedTree(t *testing.T) {
	types := bananaTypes{}
	for id := 2073; id <= 2078; id++ {
		types[id] = cachecfg.Loc{ID: id, Name: "Banana Tree", Width: 2, Length: 2, Ops: [5]string{"Search"}}
	}
	ix, err := locindex.New([]locindex.Loc{{ID: 2073, X: 2935, Z: 3154, Shape: 10}, {ID: 2073, X: 2940, Z: 3154, Shape: 10}})
	if err != nil {
		t.Fatal(err)
	}
	q := bananaTreeQuery(tile.Tile{X: 2934, Z: 3154})
	live := []locindex.Change{{ID: 2078, X: 2935, Z: 3154, Shape: 10}}
	m, ok := ix.Nearest(types, q, live)
	if !ok || m.X != 2940 || m.Op != 1 {
		t.Fatalf("selected %+v, found %v; wanted productive Search tree", m, ok)
	}
}

func TestBananaApproachesStayOutsideRotatedFootprint(t *testing.T) {
	for _, angle := range []int{0, 1} {
		m := locindex.Match{Loc: locindex.Loc{X: 100, Z: 200, Level: 1, Angle: angle}}
		typ := cachecfg.Loc{Width: 2, Length: 3}
		w, h := 2, 3
		if angle == 1 {
			w, h = h, w
		}
		approaches := bananaApproaches(m, typ)
		if len(approaches) != 2*(w+h) {
			t.Fatalf("missing perimeter tiles: %+v", approaches)
		}
		for _, p := range approaches {
			inside := p.X >= 100 && p.X < 100+w && p.Z >= 200 && p.Z < 200+h
			if inside || p.Level != 1 {
				t.Fatalf("invalid approach %+v", p)
			}
			orthogonal := (p.X == 99 || p.X == 100+w) && p.Z >= 200 && p.Z < 200+h || (p.Z == 199 || p.Z == 200+h) && p.X >= 100 && p.X < 100+w
			if !orthogonal {
				t.Fatalf("nonadjacent approach %+v", p)
			}
		}
	}
}

func TestBananaFareRouting(t *testing.T) {
	karamja := tile.Tile{X: 2939, Z: 3154}
	mainland := tile.Tile{X: 3220, Z: 3218}
	rimmington := tile.Tile{X: 2965, Z: 3214}
	for _, tc := range []struct {
		name                string
		here                *tile.Tile
		coins, need, target int
		active, want        bool
	}{
		{"stranded", &karamja, 0, 30, -1, false, true},
		{"resuming with no Man in reach", &karamja, 0, 60, -1, true, true},
		{"a Man in reach wins over a started banana job", &karamja, 0, 60, 42, true, false},
		{"nearby thief target", &karamja, 0, 30, 42, false, false},
		{"funded", &karamja, 30, 30, -1, true, false},
		{"mainland", &mainland, 0, 30, -1, true, false},
		{"Rimmington random displacement", &rimmington, 0, 30, -1, false, false},
		{"Rimmington stale island job", &rimmington, 0, 30, -1, true, false},
		{"unknown position", nil, 0, 30, -1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsBananaFare(tc.here, tc.coins, tc.need, tc.target, tc.active); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// Only two of Luthas's branches set %hunt_store_employed in content 289 (luthas_employment
// and luthas_another). Selecting any other one and then packing bananas is what produced the
// lab's "the crate never takes a banana" — the crate answers "Why would I want to do that?"
// and takes nothing, which was read as OPLOCU silently failing. Proven live 2026-09-20:
// without the bit, all five tiles around crate 2072 at 2943,3151 were refused with that
// message; with `::setvar hunt_store_employed 1`, all five packed a banana.
func TestOnlyTheTwoHiringBranchesCountAsEmployment(t *testing.T) {
	for _, tc := range []struct {
		want string
		hire bool
	}{
		{"offer me employment", true},
		{"pay me for another crate", true},
		{"what did i have to do again", false},
		{"thank you, i'll be on my way", false},
		{"", false},
	} {
		if got := hiringChoice(tc.want); got != tc.hire {
			t.Errorf("hiringChoice(%q) = %v, want %v", tc.want, got, tc.hire)
		}
	}
	// Every hiring branch must be one bananaJobChoice can actually return, or the check
	// above silently never fires.
	for _, tc := range []struct {
		text         string
		paid, enough bool
	}{
		{"Could you offer me employment on your plantation?", false, false},
		{"Will you pay me for another crate full?", true, false},
	} {
		if got := bananaJobChoice([]actions.Option{{Text: tc.text}}, tc.paid, tc.enough); !hiringChoice(got) {
			t.Errorf("bananaJobChoice(%q) = %q, which hiringChoice does not recognise", tc.text, got)
		}
	}
}

func TestBananaJobChoiceResumesOrRenewsEmployment(t *testing.T) {
	for _, tc := range []struct {
		text         string
		paid, enough bool
		want         string
	}{
		{"Could you offer me employment on your plantation?", false, false, "offer me employment"},
		{"What did I have to do again?", false, false, "what did i have to do again"},
		{"Will you pay me for another crate full?", true, false, "pay me for another crate"},
		{"Thank you, I'll be on my way", true, true, "thank you, i'll be on my way"},
		{"That customs officer is annoying isn't she?", false, false, ""},
	} {
		if got := bananaJobChoice([]actions.Option{{Text: tc.text}}, tc.paid, tc.enough); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.text, got, tc.want)
		}
	}
}
