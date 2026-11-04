package chatlog

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestReadAfterRetainedBoundaries(t *testing.T) {
	l := New()
	at := time.Unix(100, 0)
	for i := 0; i < maxLines+5; i++ {
		l.Record(fmt.Sprint(i))
	}
	for i := range l.lines {
		l.lines[l.index(i)].at = at.Add(time.Duration(i) * time.Second)
	}
	for _, c := range []struct {
		cursor int
		since  time.Time
		want   []string
	}{
		{0, at.Add(98 * time.Second), []string{"103", "104"}},
		{-10, at.Add(99 * time.Second), []string{"104"}},
		{103, time.Time{}, []string{"103", "104"}},
		{104, at.Add(100 * time.Second), nil},
		{105, time.Time{}, nil},
		{999, time.Time{}, nil},
	} {
		got, next := l.ReadAfter(c.cursor, c.since)
		if !reflect.DeepEqual(got, c.want) || next != 105 {
			t.Fatalf("cursor %d: %v/%d, want %v/105", c.cursor, got, next, c.want)
		}
	}
	if rows, next := New().ReadAfter(0, time.Time{}); rows != nil || next != 0 {
		t.Fatal("empty log changed")
	}
}

var cursorRows []string
var cursorNext int

func BenchmarkReadAfterCursor(b *testing.B) {
	l := New()
	for i := 0; i < maxLines; i++ {
		l.Record("server message")
	}
	for _, c := range []struct {
		name   string
		cursor int
	}{{"caught-up", maxLines}, {"one-unread", maxLines - 1}} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cursorRows, cursorNext = l.ReadAfter(c.cursor, time.Time{})
			}
		})
	}
}
