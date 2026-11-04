package chatlog

import (
	"fmt"
	"testing"
	"time"
)

func TestCursorReadsHaveNoGapsOrDuplicates(t *testing.T) {
	l := New()
	l.Record("old")
	l.lines[0].at = time.Now().Add(-time.Second)
	since := time.Now()
	l.Record("new")
	rows, cursor := l.ReadAfter(0, since)
	if len(rows) != 1 || rows[0] != "new" {
		t.Fatalf("initial rows %v", rows)
	}
	for i := 0; i < 200; i++ {
		want := fmt.Sprint(i)
		l.Record(want)
		rows, cursor = l.ReadAfter(cursor, time.Time{})
		if len(rows) != 1 || rows[0] != want {
			t.Fatalf("iteration %d rows %v", i, rows)
		}
		rows, cursor = l.ReadAfter(cursor, time.Time{})
		if len(rows) != 0 {
			t.Fatalf("duplicate rows %v", rows)
		}
	}
}
