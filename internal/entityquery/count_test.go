package entityquery

import "testing"

func TestCountAndExistsEvaluateFullFreshSnapshot(t *testing.T) {
	rows := []Entity{{Index: 1}, {Index: 2}, {Index: 3}}
	calls, evaluated := 0, 0
	q := FromFunc(func() []Entity { calls++; return rows }).Where(func(e Entity) bool { evaluated++; return e.Index != 2 })
	if q.Count() != 2 || calls != 1 || evaluated != 3 {
		t.Fatal("count changed filter evaluation")
	}
	if !q.Exists() || calls != 2 || evaluated != 6 {
		t.Fatal("exists skipped filter evaluation")
	}
	rows = nil
	if q.Count() != 0 || q.Exists() || calls != 4 {
		t.Fatal("empty snapshot was not refreshed")
	}
	rows = []Entity{{Index: 2}}
	if q.Count() != 0 || q.Exists() {
		t.Fatal("rejected entity counted")
	}
}

var countResult int
var existsResult bool

func BenchmarkCountTerminals(b *testing.B) {
	rows := make([]Entity, 300)
	for i := range rows {
		rows[i].Index = i
	}
	q := FromEntities(rows).Where(func(e Entity) bool { return e.Index%2 == 0 })
	b.Run("count", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			countResult = q.Count()
		}
	})
	b.Run("exists", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			existsResult = q.Exists()
		}
	})
}
