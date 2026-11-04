package entityquery

import (
	"reflect"
	"testing"
)

func TestFirstPreservesSnapshotAndPredicateOrder(t *testing.T) {
	rows := []Entity{{Index: 1}, {Index: 2}, {Index: 3}}
	var visits []int
	calls := 0
	q := FromFunc(func() []Entity { calls++; return rows }).Where(func(e Entity) bool {
		visits = append(visits, e.Index)
		return e.Index != 1
	}).Where(func(e Entity) bool { visits = append(visits, -e.Index); return true })
	got, ok := q.First()
	if !ok || got.Index != 2 || calls != 1 || !reflect.DeepEqual(visits, []int{1, 2, -2, 3, -3}) {
		t.Fatalf("got %+v/%v, calls %d, visits %v", got, ok, calls, visits)
	}
	rows = []Entity{{Index: 4}}
	if got, ok = q.First(); !ok || got.Index != 4 || calls != 2 {
		t.Fatal("source not refreshed")
	}
	for _, input := range [][]Entity{nil, {{Index: 1}}} {
		rows = input
		if got, ok = q.First(); ok || got != (Entity{}) {
			t.Fatal("empty/rejected result changed")
		}
	}
}

func BenchmarkFirstQuery(b *testing.B) {
	rows := make([]Entity, 300)
	for i := range rows {
		rows[i].Index = i
	}
	q := FromEntities(rows).Where(func(e Entity) bool { return e.Index%2 == 0 })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nearestEntity, nearestFound = q.First()
	}
}
