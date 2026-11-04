package combatplan

import (
	"slices"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The cows rung carries the pen, no other rung does, and the spread ladder has the same one.
func TestOnlyTheCowsRungIsAFencedPen(t *testing.T) {
	for name, ladder := range map[string][]Rung{"melee": MeleeLadder(), "spread": SpreadLadder(), "spread with the Wilderness": SpreadLadderWith(SpreadOptions{Wilderness: true})} {
		penned := 0
		for _, r := range ladder {
			if r.Penned() != (r.Pen != nil) {
				t.Errorf("%s: rung %q: Penned() disagrees with Pen", name, r.Key)
			}
			if !r.Penned() {
				continue
			}
			penned++
			if r.Key != "cows" {
				t.Errorf("%s: rung %q is a fenced pen: only the cows are", name, r.Key)
			}
			if r.Pen != &CowPen {
				t.Errorf("%s: the cows rung's pen is not CowPen", name)
			}
		}
		if penned != 1 {
			t.Errorf("%s ladder has %d penned rungs, want exactly the cows", name, penned)
		}
	}
}

// The rung's own figures are consistent with its pen: every spawn is inside the leash (the count is the supply the spread seats
// accounts by, and the old rung overstated it: only 2 of its 7 listed spawns were inside its leash of 8), the pen's entry and beat are
// near enough to the stand to be inside the rung, and the pen's figures are well formed.
func TestTheCowsRungCoversItsWholePen(t *testing.T) {
	r, ok := RungByKey(MeleeLadder(), "cows")
	if !ok {
		t.Fatal("no cows rung")
	}
	if got := r.SpawnCount(); got != 10 {
		t.Errorf("the cows rung lists %d spawns, want the pen's ten", got)
	}
	seen := map[tile.Tile]bool{}
	for _, s := range r.Spawns {
		if seen[s] {
			t.Errorf("spawn %v is listed twice", s)
		}
		seen[s] = true
		if d := tile.Chebyshev(r.Stand, s); d > r.Leash {
			t.Errorf("spawn %v is %d from the stand %v, outside the %d leash, so the count overstates the supply", s, d, r.Stand, r.Leash)
		}
	}
	// And the leash is no wider than the pen needs (19 is the least that covers all ten from any tile).
	far := 0
	for _, s := range r.Spawns {
		far = max(far, tile.Chebyshev(r.Stand, s))
	}
	if far != r.Leash {
		t.Errorf("the farthest spawn is %d from the stand and the leash is %d: the leash should be exactly what the pen needs", far, r.Leash)
	}
	if r.Monster.DisengageTiles == 0 {
		t.Error("fixture: the cow has no disengage range")
	}
	pen := r.Pen
	if pen == nil {
		t.Fatal("the cows rung has no pen")
	}
	if len(pen.Gates) != 2 {
		t.Errorf("the pen lists %d gate leaves, want the double gate's two", len(pen.Gates))
	}
	if pen.EnterRadius < 1 || pen.EnterRadius > 4 {
		t.Errorf("EnterRadius %d: a walk in that may stop 5 tiles from the entry could stop on the road", pen.EnterRadius)
	}
	if d := tile.Chebyshev(pen.Enter, r.Stand); d > r.Leash {
		t.Errorf("the pen's entry is %d from the stand, outside the %d leash: a walk in would not end at the rung", d, r.Leash)
	}
	for i, stop := range pen.Beat {
		if d := tile.Chebyshev(stop, r.Stand); d > r.Leash {
			t.Errorf("stop %d of the beat, %v, is %d from the stand, outside the %d leash", i, stop, d, r.Leash)
		}
	}
	for _, g := range pen.Gates {
		if g.X >= pen.Enter.X {
			t.Errorf("gate leaf %v is not west of the entry %v", g, pen.Enter)
		}
	}
	// The beat reaches every spawn's neighbourhood: some stop is within the client's view (15) of each.
	for _, s := range r.Spawns {
		seeing := slices.ContainsFunc(pen.Beat, func(stop tile.Tile) bool { return tile.Chebyshev(stop, s) <= 15 })
		if !seeing {
			t.Errorf("no stop of the beat sees the cow spawned at %v", s)
		}
	}
}

// NextBeat steps round the ring, skips the stops the account is already at, wraps, and says so when it is at all of them.
func TestNextBeatSkipsTheStopsTheAccountIsAt(t *testing.T) {
	e := Enclosure{Beat: []tile.Tile{{X: 0, Z: 0}, {X: 20, Z: 0}, {X: 40, Z: 0}}}
	at := 0
	got, ok := e.NextBeat(&at, tile.Tile{X: 100, Z: 100})
	if !ok || got != (tile.Tile{X: 0, Z: 0}) || at != 1 {
		t.Fatalf("first stop = %v ok %v, index %d; want 0,0 and index 1", got, ok, at)
	}
	got, ok = e.NextBeat(&at, tile.Tile{X: 0, Z: 0})
	if !ok || got != (tile.Tile{X: 20, Z: 0}) || at != 2 {
		t.Fatalf("second stop = %v ok %v, index %d; want 20,0 and index 2", got, ok, at)
	}
	// Standing within BeatNear of the next stop skips it for the one after.
	got, ok = e.NextBeat(&at, tile.Tile{X: 40 - BeatNear, Z: 0})
	if !ok || got != (tile.Tile{X: 0, Z: 0}) || at != 4 {
		t.Fatalf("a stop %d away was not skipped: %v ok %v, index %d", BeatNear, got, ok, at)
	}
	// A beat whose every stop is within reach has nowhere to go.
	short := Enclosure{Beat: []tile.Tile{{X: 10, Z: 10}, {X: 12, Z: 11}}}
	at = 0
	if got, ok := short.NextBeat(&at, tile.Tile{X: 11, Z: 10}); ok {
		t.Errorf("a beat the account is in the middle of gave the stop %v", got)
	}
	if got, ok := (Enclosure{}).NextBeat(new(int), tile.Tile{}); ok {
		t.Errorf("an empty beat gave the stop %v", got)
	}
}

// Every stop of the real beat is reached in turn from the pen's entry, and the ring comes round.
func TestTheCowPenBeatComesRound(t *testing.T) {
	at := 0
	here := CowPen.Enter
	visited := map[tile.Tile]bool{}
	for i := 0; i < 2*len(CowPen.Beat); i++ {
		stop, ok := CowPen.NextBeat(&at, here)
		if !ok {
			t.Fatalf("no stop after %d walks, from %v", i, here)
		}
		visited[stop] = true
		here = stop
	}
	for _, stop := range CowPen.Beat {
		if !visited[stop] {
			t.Errorf("the stop %v was never walked to", stop)
		}
	}
}
