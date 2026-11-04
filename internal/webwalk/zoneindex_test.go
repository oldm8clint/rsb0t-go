package webwalk

import (
	"math/rand/v2"
	"testing"
)

// The index answers exactly what InZones answers, for every known zone set, on random tiles and on
// every rectangle's corners and the tiles just outside them.
func TestZoneIndexMatchesInZones(t *testing.T) {
	var all []Rect
	for _, z := range KnownDangerZones {
		all = append(all, z.Rects...)
	}
	all = append(all, Rect{MinX: 3000, MaxX: 3010, MinZ: 3000, MaxZ: 3010, Level: AllLevels}, Rect{MinX: 5, MaxX: 3, MinZ: 0, MaxZ: 0})
	sets := [][]Rect{nil, all, all[:1], all[len(all)/2:]}
	rng := rand.New(rand.NewPCG(1, 2))
	for si, zones := range sets {
		zi := NewZoneIndex(zones)
		check := func(p Point) {
			if got, want := zi.Contains(p), InZones(p, zones); got != want {
				t.Fatalf("set %d, %+v: index %v, InZones %v", si, p, got, want)
			}
		}
		for i := 0; i < 200000; i++ {
			check(Point{X: rng.IntN(4200), Z: rng.IntN(10600), Level: rng.IntN(4)})
		}
		for _, r := range zones {
			for _, x := range []int{r.MinX - 1, r.MinX, r.MaxX, r.MaxX + 1} {
				for _, z := range []int{r.MinZ - 1, r.MinZ, r.MaxZ, r.MaxZ + 1} {
					for l := 0; l < 4; l++ {
						check(Point{X: x, Z: z, Level: l})
					}
				}
			}
		}
	}
	var nilIndex *ZoneIndex
	if nilIndex.Contains(Point{X: 1, Z: 1}) {
		t.Fatal("a nil index contains a tile")
	}
}

func TestZoneIndexForReusesTheIndex(t *testing.T) {
	zones := []Rect{{MinX: 10, MaxX: 20, MinZ: 10, MaxZ: 20, Level: 0}}
	if zoneIndexFor(zones) != zoneIndexFor(append([]Rect(nil), zones...)) {
		t.Fatal("the same zone set built two indexes")
	}
}

func BenchmarkZoneIndexVsInZones(b *testing.B) {
	var all []Rect
	for _, z := range KnownDangerZones {
		all = append(all, z.Rects...)
	}
	zi := NewZoneIndex(all)
	p := Point{X: 2700, Z: 3450, Level: 0}
	b.Run("index", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			zi.Contains(p)
		}
	})
	b.Run("linear", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			InZones(p, all)
		}
	})
}
