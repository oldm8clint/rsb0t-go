package tickmanip

import (
	"reflect"
	"testing"
	"time"
)

func TestServerTick(t *testing.T) {
	if got := ServerTick(Processing, 7); got != 8 {
		t.Errorf("a message that arrived with 7 PLAYER_INFO seen belongs to tick 8, got %d", got)
	}
	if got := ServerTick(Flush, 7); got != 7 {
		t.Errorf("an inventory change that arrived with 7 PLAYER_INFO seen belongs to tick 7, got %d", got)
	}
}

func TestRowNet(t *testing.T) {
	r := NewRow(5)
	r.Inv = []InvDelta{
		{Slot: 0, ID: 1511, PrevID: 0, Count: 1},                  // a log appears
		{Slot: 1, ID: 1511, PrevID: 1511, Count: 6, PrevCount: 3}, // a stack grows by 3
		{Slot: 2, ID: 0, PrevID: 995, Count: 0, PrevCount: 40},    // coins leave
		{Slot: 3, ID: 946, PrevID: 1511, Count: 1, PrevCount: 1},  // a log is replaced by a knife
	}
	want := map[int32]int{1511: 1 + 3 - 1, 995: -40, 946: 1}
	if got := r.Net(); !reflect.DeepEqual(got, want) {
		t.Errorf("Net = %v, want %v", got, want)
	}
	// A change that cancels out disappears.
	r.Inv = []InvDelta{{ID: 1511, PrevID: 0, Count: 1}, {ID: 0, PrevID: 1511, PrevCount: 1}}
	if got := r.Net(); len(got) != 0 {
		t.Errorf("a gain and a loss of the same item should net to nothing, got %v", got)
	}
}

func TestRowSetAt(t *testing.T) {
	set := &RowSet{Base: 100, Rows: map[int]*Row{102: NewRow(102)}}
	set.Rows[102].Msgs = []string{"hi"}
	if got := set.At(2).Msgs; len(got) != 1 {
		t.Errorf("At(2) = %v, want the recorded row", got)
	}
	if got := set.At(5); got == nil || got.N != 105 || len(got.Msgs) != 0 {
		t.Errorf("At of an unseen tick should be an empty row for tick 105, got %+v", got)
	}
	if !reflect.DeepEqual(set.Ticks(), []int{2}) {
		t.Errorf("Ticks = %v, want [2]", set.Ticks())
	}
}

func TestIntervalsOnlyAcrossConsecutiveTicks(t *testing.T) {
	t0 := time.Unix(1000, 0)
	set := &RowSet{Base: 10, Rows: map[int]*Row{}}
	for _, n := range []int{10, 11, 12, 14, 15} { // 13 is missing, so 12->14 is not one gap
		r := NewRow(n)
		r.At = t0.Add(time.Duration(n-10) * 200 * time.Millisecond)
		set.Rows[n] = r
	}
	gaps := set.Intervals()
	if len(gaps) != 3 {
		t.Fatalf("gaps = %v, want 3 (10->11, 11->12, 14->15)", gaps)
	}
	mean, sd := DurationStats(gaps)
	if mean < 199.9 || mean > 200.1 || sd > 0.001 {
		t.Errorf("mean=%.3f sd=%.3f, want 200 and 0", mean, sd)
	}
}

func TestDescribe(t *testing.T) {
	r := NewRow(42)
	r.Sent = []string{"knife", "make1", "click"}
	r.Anim = 879
	r.Msgs = []string{"You get some oak logs."}
	r.XP = map[int]int{8: 37}
	r.Inv = []InvDelta{{ID: 1521, Count: 1}}
	got := r.Describe(2)
	for _, want := range []string{"t+2 (tick 42)", "sent[knife,make1,click]", "anim=879", `msg="You get some oak logs."`, "xp=map[8:37]", "inv=map[1521:1]"} {
		if !contains(got, want) {
			t.Errorf("Describe = %q, missing %q", got, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
