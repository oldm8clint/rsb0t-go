package entityquery

import (
	"reflect"
	"testing"
)

func TestNearestPreservesOrderFiltersAndRefresh(t *testing.T) {
	entities := []Entity{{Index: 1, Distance: 9}, {Index: 2, Distance: 2}, {Index: 3, Distance: 2}, {Index: 4, Distance: 0}}
	for _, radius := range []int{-1, 0, 1, 2, 20} {
		var seen []int
		calls := 0
		q := FromFunc(func() []Entity { calls++; return entities }).Where(func(e Entity) bool { seen = append(seen, e.Index); return e.Index != 4 })
		got, ok := q.NearestPreferLocal(radius)
		if !ok || got.Index != 2 || calls != 1 || !reflect.DeepEqual(seen, []int{1, 2, 3, 4}) {
			t.Fatalf("radius %d: %+v/%v, calls %d, seen %v", radius, got, ok, calls, seen)
		}
		entities[0].Distance = 1
		if got, ok = q.Nearest(); !ok || got.Index != 1 || calls != 2 {
			t.Fatal("source not refreshed")
		}
		entities[0].Distance = 9
	}
	for _, entities := range [][]Entity{nil, {{Index: 1}}} {
		q := FromEntities(entities).Where(func(Entity) bool { return false })
		if got, ok := q.Nearest(); ok || got != (Entity{}) {
			t.Fatal("empty result changed")
		}
	}
}

var nearestEntity Entity
var nearestFound bool

func BenchmarkNearestQuery(b *testing.B) {
	entities := make([]Entity, 300)
	for i := range entities {
		entities[i] = Entity{Index: i, Distance: 300 - i}
	}
	q := FromEntities(entities).Where(func(e Entity) bool { return e.Index%2 == 0 })
	b.Run("nearest", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			nearestEntity, nearestFound = q.Nearest()
		}
	})
	b.Run("prefer-local", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			nearestEntity, nearestFound = q.NearestPreferLocal(20)
		}
	})
}
