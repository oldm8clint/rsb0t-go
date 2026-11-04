package thieverplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func methodNamed(t *testing.T, label string) Method {
	t.Helper()
	m, ok := MethodByLabel(label)
	if !ok {
		t.Fatalf("no method %q", label)
	}
	return m
}

// The rungs the 11 Oct log shows idle, each with spawns inside its leash, and the leash is what filters: the Knight
// spawned in the castle (2582,3297) is 79 tiles from the Knight rung's anchor and no target of it.
func TestSeekSpawnsAreTheOnesInsideTheLeash(t *testing.T) {
	cases := []struct {
		label string
		want  int
	}{
		{"Knight of Ardougne", 4}, // five exist on the map; the castle's is out of the leash
		{"Hero", 2},               // three exist; 2630,3288 is 27 from the anchor and the leash is 17
		{"Paladin", 7},            // the castle's ground floor; two market Paladins are 73 away
		{"Guard", 7},
	}
	for _, c := range cases {
		m := methodNamed(t, c.label)
		got := SeekSpawns(m)
		if len(got) != c.want {
			t.Errorf("%s: %d spawns in the leash %v, want %d", c.label, len(got), got, c.want)
		}
		for _, s := range got {
			if d := tile.Chebyshev(s, m.Anchor); d > m.Leash {
				t.Errorf("%s: spawn %v is %d from the anchor, outside the leash %d", c.label, s, d, m.Leash)
			}
		}
	}
	if got := SeekSpawns(methodNamed(t, "Man")); got != nil {
		t.Errorf("the courtyard Men have no table: %v", got)
	}
	if got := SeekSpawns(methodNamed(t, "Watchman")); got != nil {
		t.Errorf("an upstairs rung (level 1) must not be given ground-floor spawns: %v", got)
	}
}

// An account that stopped at the Knight leash's edge near the bank, with no Knight in sight, is sent to the nearest
// Knight spawn, and then in turn to the others, never back to one it has just looked at.
func TestSeekTargetToursTheSpawnsNearestFirst(t *testing.T) {
	m := methodNamed(t, "Knight of Ardougne")
	spawns := SeekSpawns(m)
	bank := tile.Tile{X: 2655, Z: 3283}
	now := time.Now()
	went := map[tile.Tile]time.Time{}
	wentAt := func(s tile.Tile) (time.Time, bool) { at, ok := went[s]; return at, ok }

	first, ok := SeekTarget(spawns, bank, wentAt, now)
	if !ok || first != (tile.Tile{X: 2669, Z: 3298}) {
		t.Fatalf("from the bank the nearest Knight spawn is 2669,3298 (15 tiles), got %v ok %v", first, ok)
	}
	went[first] = now
	here := first // looked, found nothing
	seen := map[tile.Tile]bool{first: true}
	at := now
	for i := 0; i < 8; i++ {
		at = at.Add(10 * time.Second)
		next, ok := SeekTarget(spawns, here, wentAt, at)
		if !ok {
			break
		}
		if seen[next] {
			t.Fatalf("spawn %v looked at twice inside the memory", next)
		}
		seen[next] = true
		went[next] = at
		here = next
	}
	if len(seen) != len(spawns) {
		t.Errorf("toured %d of %d spawns", len(seen), len(spawns))
	}
	// Every spawn looked at, memory still running: nowhere to go, the account stays and polls.
	if next, ok := SeekTarget(spawns, here, wentAt, at.Add(time.Second)); ok {
		t.Errorf("sent to %v with every spawn just looked at", next)
	}
	// Once the memory lapses the tour starts again.
	if _, ok := SeekTarget(spawns, here, wentAt, at.Add(SeekMemory)); !ok {
		t.Error("never looks again")
	}
}

// Standing at a spawn counts as looking there; and with no memory (nil) only the arrival distance applies.
func TestSeekTargetSkipsTheSpawnTheAccountStandsAt(t *testing.T) {
	spawns := []tile.Tile{{X: 2647, Z: 3306}, {X: 2667, Z: 3316}}
	// The six accounts of 12 Oct stood on the first Hero spawn with no Hero: the other spawn is the look to take.
	got, ok := SeekTarget(spawns, tile.Tile{X: 2646, Z: 3305}, nil, time.Now())
	if !ok || got != spawns[1] {
		t.Fatalf("standing at the first Hero spawn the seek went to %v ok %v, want the second", got, ok)
	}
	if _, ok := SeekTarget(nil, tile.Tile{X: 2646, Z: 3305}, nil, time.Now()); ok {
		t.Fatal("a seek with no spawns")
	}
}
