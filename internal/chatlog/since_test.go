package chatlog

import (
	"strings"
	"testing"
	"time"
)

func TestSinceBoundariesAndOutput(t *testing.T) {
	at := time.Unix(100, 0)
	l := &Log{lines: []entry{{at.Add(-time.Second), "old"}, {at, ""}, {at, "hello\nworld"}, {at.Add(time.Second), "new"}}}
	for _, c := range []struct {
		cutoff time.Time
		want   string
	}{
		{time.Time{}, "old\n\nhello\nworld\nnew\n"},
		{at, "\nhello\nworld\nnew\n"},
		{at.Add(time.Nanosecond), "new\n"},
		{at.Add(2 * time.Second), ""},
	} {
		if got := l.Since(c.cutoff); got != c.want {
			t.Fatalf("Since(%v) = %q, want %q", c.cutoff, got, c.want)
		}
	}
	if got := New().Since(time.Time{}); got != "" {
		t.Fatalf("empty log: %q", got)
	}
	kept := l.Since(at)
	l.Record("later")
	if kept != "\nhello\nworld\nnew\n" {
		t.Fatal("result changed after recording")
	}
}

var sinceResult string

func BenchmarkSince(b *testing.B) {
	for _, n := range []int{1, 100} {
		name := "one"
		if n == 100 {
			name = "full"
		}
		b.Run(name, func(b *testing.B) {
			l := New()
			for i := 0; i < n; i++ {
				l.Record(strings.Repeat("x", 80))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sinceResult = l.Since(time.Time{})
			}
		})
	}
}
