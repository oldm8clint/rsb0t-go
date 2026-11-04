package chatlog

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadersAcrossRepeatedRollover(t *testing.T) {
	var l Log // zero-value construction must remain supported
	for i := 0; i < 3*maxLines+17; i++ {
		l.Record(fmt.Sprint(i))
		start := i + 1 - maxLines
		if start < 0 {
			start = 0
		}
		var want []string
		for n := start; n <= i; n++ {
			want = append(want, fmt.Sprint(n))
		}
		if got := l.Recent(1000); !reflect.DeepEqual(got, want) {
			t.Fatalf("Recent at %d: %v", i, got)
		}
		if got := l.Since(time.Time{}); got != strings.Join(want, "\n")+"\n" {
			t.Fatalf("Since at %d", i)
		}
		got, next := l.ReadAfter(0, time.Time{})
		if !reflect.DeepEqual(got, want) || next != i+1 || l.Total() != i+1 {
			t.Fatalf("cursor at %d: %v/%d", i, got, next)
		}
	}
	before := l.Recent(2)
	l.Record("next")
	if !reflect.DeepEqual(before, []string{"315", "316"}) {
		t.Fatal("returned slice changed")
	}
}

func BenchmarkRecordFullLog(b *testing.B) {
	l := New()
	for i := 0; i < maxLines; i++ {
		l.Record("server message")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Record("server message")
	}
}
