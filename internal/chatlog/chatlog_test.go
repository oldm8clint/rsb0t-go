package chatlog

import "testing"

func TestTotalCountsEveryRecordNotJustWhatsRetained(t *testing.T) {
	l := New()
	for i := 0; i < maxLines+10; i++ {
		l.Record("line")
	}
	if got := l.Total(); got != maxLines+10 {
		t.Errorf("Total() = %d, want %d", got, maxLines+10)
	}
	if got := len(l.Recent(1000)); got != maxLines {
		t.Errorf("Recent kept %d lines, want the cap %d", got, maxLines)
	}
}

func TestRecentReturnsOldestFirst(t *testing.T) {
	l := New()
	l.Record("a")
	l.Record("b")
	l.Record("c")
	got := l.Recent(2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("Recent(2) = %v, want [b c]", got)
	}
}
