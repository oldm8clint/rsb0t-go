package loot

import (
	"testing"
	"time"
)

func testWindow(t *testing.T, src Source, skip func(int) bool) (*Window, *Counts, *[]Drop, *time.Time) {
	t.Helper()
	pack := Counts{}
	var got []Drop
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	w := newWindow(func(d Drop) { got = append(got, d) }, nil,
		func() Counts {
			out := Counts{}
			for k, v := range pack {
				out[k] = v
			}
			return out
		},
		src, skip, func() time.Time { return now })
	return w, &pack, &got, &now
}

var testTree = Woodcutting(nil, 1276, "Tree")

// TestWindowReportsOnlyWhatArrived is the base case every gathering source relies on: the
// positive pack delta since the window opened, and nothing else.
func TestWindowReportsOnlyWhatArrived(t *testing.T) {
	w, pack, got, _ := testWindow(t, testTree, nil)
	(*pack)[1511] = 3 // three Logs
	w.Got(3)
	w.Flush()
	if len(*got) != 1 {
		t.Fatalf("reported %d drops; want 1", len(*got))
	}
	d := (*got)[0]
	if d.Events != 3 || len(d.Items) != 1 || d.Items[0].ID != 1511 || d.Items[0].Count != 3 {
		t.Fatalf("drop = %+v; want 3 events and 3 of obj 1511", d)
	}
	// A second flush with no further gain reports nothing rather than the same logs again.
	w.Flush()
	if len(*got) != 1 {
		t.Fatalf("an unchanged pack reported again: %+v", *got)
	}
	// Only the NEW logs are reported next time, not the running total.
	(*pack)[1511] = 5
	w.Got(2)
	w.Flush()
	if len(*got) != 2 || (*got)[1].Items[0].Count != 2 {
		t.Fatalf("second drop = %+v; want 2 more logs, not the running total", (*got)[1])
	}
}

// TestTaintedWindowIsDiscardedAndReopened is the rule that generalises: a window something
// interrupted reports nothing, and the NEXT one still measures correctly from where the pack
// actually stands rather than double-counting what the tainted one saw.
func TestTaintedWindowIsDiscardedAndReopened(t *testing.T) {
	w, pack, got, _ := testWindow(t, testTree, nil)
	(*pack)[1511] = 4
	(*pack)[3062] = 1 // a Strange box handed over by an event mid-window
	w.Got(4)
	w.Taint()
	w.Flush()
	if len(*got) != 0 {
		t.Fatalf("a tainted window reported %+v; it must report nothing", *got)
	}
	// The next window is clean, and measures only what arrives after the taint.
	(*pack)[1511] = 6
	w.Got(2)
	w.Flush()
	if len(*got) != 1 {
		t.Fatalf("the window after a taint reported %d drops; want 1", len(*got))
	}
	d := (*got)[0]
	if d.Events != 2 || len(d.Items) != 1 || d.Items[0].ID != 1511 || d.Items[0].Count != 2 {
		t.Fatalf("drop = %+v; want only the 2 logs that arrived after the taint", d)
	}
}

// TestWindowSkipSetRefusesWhatTheActivityCannotProduce.
func TestWindowSkipSetRefusesWhatTheActivityCannotProduce(t *testing.T) {
	w, pack, got, _ := testWindow(t, testTree, func(id int) bool { return id == 1359 }) // a Mithril axe
	(*pack)[1511] = 2
	(*pack)[1359] = 1
	w.Got(2)
	w.Flush()
	if len(*got) != 1 {
		t.Fatalf("reported %d drops; want 1", len(*got))
	}
	for _, it := range (*got)[0].Items {
		if it.ID == 1359 {
			t.Fatalf("the axe was reported as something the tree produced: %+v", (*got)[0].Items)
		}
	}
	if len((*got)[0].Items) != 1 {
		t.Fatalf("items = %+v; want the logs only", (*got)[0].Items)
	}
}

// TestWindowReportsEventsWithNoItems: a Big net haul that lands nothing still counts the swing,
// and a window with neither events nor items stays silent.
func TestWindowReportsEventsWithNoItems(t *testing.T) {
	w, _, got, _ := testWindow(t, testTree, nil)
	w.Flush()
	if len(*got) != 0 {
		t.Fatalf("an empty window reported %+v", *got)
	}
	w.Got(1)
	w.Flush()
	if len(*got) != 1 || (*got)[0].Events != 1 || len((*got)[0].Items) != 0 {
		t.Fatalf("drop = %+v; want one event and no items", *got)
	}
}

// TestWindowDue only fires once the window has been open long enough, so a long uninterrupted
// stretch reports while it runs without reporting on every tick.
func TestWindowDue(t *testing.T) {
	w, _, _, now := testWindow(t, testTree, nil)
	if w.Due(30 * time.Second) {
		t.Fatal("a window just opened is not due")
	}
	*now = now.Add(31 * time.Second)
	if !w.Due(30 * time.Second) {
		t.Fatal("a window open for 31s should be due at 30s")
	}
	w.Flush()
	if w.Due(30 * time.Second) {
		t.Fatal("flushing reopens the window, so it is not due again immediately")
	}
}

// TestNilWindowIsSafe: a script with no Loot sink measures nothing and cannot panic.
func TestNilWindowIsSafe(t *testing.T) {
	if w := NewWindow(nil, nil, func() Counts { return Counts{} }, testTree, nil); w != nil {
		t.Fatal("a nil sink should give a nil window")
	}
	if w := NewWindow(func(Drop) {}, nil, nil, testTree, nil); w != nil {
		t.Fatal("a nil pack reader should give a nil window")
	}
	var w *Window
	w.Got(3)
	w.Taint()
	w.Flush()
	if got := w.Done(9); got != 9 {
		t.Fatalf("Done on a nil window returned %d; want 9", got)
	}
	if w.Due(time.Second) {
		t.Fatal("a nil window is never due")
	}
	if w.Source().Kind != "" {
		t.Fatal("a nil window has no source")
	}
}
