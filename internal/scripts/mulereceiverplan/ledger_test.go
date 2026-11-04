package mulereceiverplan

// Translated from rsb0t-scripts test/mulePlan.test.ts's tally/queue coverage (this port's own
// edge cases, following the crafterplan/fisherplan/barbarianplan test style).

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestPackTotals(t *testing.T) {
	got := PackTotals([]ItemCount{
		{Name: "Coins", Count: 5000},
		{Name: "Coins", Count: 250},
		{Name: "", Count: 0},         // skipped: no name, no count
		{Name: "", Count: 12},        // folded into "unknown item"
		{Name: "Yew logs", Count: 0}, // count<1 treated as 1
	})
	want := map[string]int{"Coins": 5250, "unknown item": 12, "Yew logs": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PackTotals = %v, want %v", got, want)
	}
}

func TestGainedItemsSortedMostFirstTiesByName(t *testing.T) {
	before := map[string]int{"Coins": 100, "Shark": 5}
	after := map[string]int{"Coins": 150, "Shark": 5, "Lobster": 10, "Swordfish": 10}
	got := GainedItems(before, after)
	want := []ItemCount{{Name: "Coins", Count: 50}, {Name: "Lobster", Count: 10}, {Name: "Swordfish", Count: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GainedItems = %+v, want %+v", got, want)
	}
}

func TestGainedItemsIgnoresUnchangedAndLost(t *testing.T) {
	before := map[string]int{"Coins": 100}
	after := map[string]int{"Coins": 90}
	if got := GainedItems(before, after); len(got) != 0 {
		t.Fatalf("GainedItems with a loss = %+v, want none", got)
	}
}

func TestTotalsByIDSkipsNegative(t *testing.T) {
	got := TotalsByID([]IDCount{{ID: 995, Count: 100}, {ID: 995, Count: 50}, {ID: -1, Count: 999}})
	want := map[int]int{995: 150}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TotalsByID = %v, want %v", got, want)
	}
}

func TestGainedIDsDeterministicAscending(t *testing.T) {
	before := map[int]int{1: 5}
	after := map[int]int{1: 5, 3: 1, 2: 1}
	got := GainedIDs(before, after)
	want := []int{2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GainedIDs = %v, want %v (ascending, deterministic)", got, want)
	}
}

func TestDescribeItems(t *testing.T) {
	items := []ItemCount{{Name: "yew logs", Count: 5000}, {Name: "coins", Count: 120}, {Name: "nature rune", Count: 40}, {Name: "law rune", Count: 10}}
	got := DescribeItems(items, 3)
	want := "5,000 yew logs, 120 coins, 40 nature rune and 1 more"
	if got != want {
		t.Fatalf("DescribeItems = %q, want %q", got, want)
	}
	if got := DescribeItems(items[:2], 3); got != "5,000 yew logs, 120 coins" {
		t.Fatalf("DescribeItems under the cap = %q", got)
	}
	if got := DescribeItems(nil, 3); got != "" {
		t.Fatalf("DescribeItems(nil) = %q, want empty", got)
	}
}

func TestLabelIDsFirstSeenWins(t *testing.T) {
	got := LabelIDs([]NamedID{{ID: 995, Name: "Coins"}, {ID: 996, Name: "Coins"}, {ID: -1, Name: "Junk"}, {ID: 1, Name: ""}})
	want := map[string]int{"Coins": 995}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LabelIDs = %v, want %v", got, want)
	}
}

func TestCollectionAddRowsAndTotals(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCollection(start)
	t1 := start.Add(time.Minute)
	t2 := start.Add(2 * time.Minute)
	c.Add(t1, []ItemCount{{Name: "Yew logs", Count: 5000}}, "Donor1", map[string]int{"Yew logs": 1515})
	c.Add(t2, []ItemCount{{Name: "yew logs", Count: 500}, {Name: "Coins", Count: 10}}, "Donor2", nil)

	if got := c.Total(); got != 5510 {
		t.Fatalf("Total = %d, want 5510", got)
	}
	if got := c.Stacks(); got != 2 {
		t.Fatalf("Stacks = %d, want 2", got)
	}
	rows := c.Rows()
	if len(rows) != 2 || rows[0].Name != "Yew logs" || rows[0].Count != 5500 {
		t.Fatalf("Rows()[0] = %+v, want the merged (case-insensitive) Yew logs row at 5500", rows[0])
	}
	if rows[0].Trades != 2 {
		t.Fatalf("Rows()[0].Trades = %d, want 2", rows[0].Trades)
	}
	if !reflect.DeepEqual(rows[0].Accounts, []string{"Donor1", "Donor2"}) {
		t.Fatalf("Rows()[0].Accounts = %v, want both donors sorted", rows[0].Accounts)
	}
	if rows[0].ID == nil || *rows[0].ID != 1515 {
		t.Fatalf("Rows()[0].ID = %v, want the id from the first trade that named one", rows[0].ID)
	}
	if c.Since() != start {
		t.Fatalf("Since() = %v, want %v", c.Since(), start)
	}
}

func TestCollectionClearResets(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCollection(start)
	c.Add(start, []ItemCount{{Name: "Coins", Count: 100}}, "", nil)
	resetAt := start.Add(time.Hour)
	c.Clear(resetAt)
	if c.Total() != 0 || c.Stacks() != 0 {
		t.Fatalf("Clear should empty the tally, got total=%d stacks=%d", c.Total(), c.Stacks())
	}
	if c.Since() != resetAt {
		t.Fatalf("Clear should restart the tally clock at the reset time")
	}
}

func TestCollectionSnapshotRoundTrips(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCollection(start)
	c.Add(start.Add(time.Minute), []ItemCount{{Name: "Shark", Count: 10}}, "Donor1", map[string]int{"Shark": 385})
	snap := c.Snapshot()

	restored := NewCollection(time.Time{})
	restored.Restore(snap)
	if restored.Total() != c.Total() || restored.Stacks() != c.Stacks() || restored.Since() != c.Since() {
		t.Fatalf("Restore should reproduce Total/Stacks/Since exactly")
	}
	if !reflect.DeepEqual(restored.Rows(), c.Rows()) {
		t.Fatalf("Restore should reproduce Rows exactly, got %+v want %+v", restored.Rows(), c.Rows())
	}
}

func TestActivityLogAddSkipsEmptyAndCapsLength(t *testing.T) {
	log := &ActivityLog{}
	log.Add(time.Now(), TallyCollected, "Donor1", nil, nil)
	if len(log.List()) != 0 {
		t.Fatalf("Add with no items should not create an entry")
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < ActivityMax+10; i++ {
		log.Add(base.Add(time.Duration(i)*time.Second), TallyCollected, "Donor1", []ItemCount{{Name: "Coins", Count: 1}}, nil)
	}
	if got := len(log.List()); got != ActivityMax {
		t.Fatalf("ActivityLog should cap at ActivityMax=%d, got %d", ActivityMax, got)
	}
	// Newest first.
	list := log.List()
	if !list[0].At.After(list[1].At) {
		t.Fatalf("ActivityLog.List() should be newest first")
	}
}

func TestActivityLogAnonymousWho(t *testing.T) {
	log := &ActivityLog{}
	log.Add(time.Now(), TallyDelivered, "", []ItemCount{{Name: "Coins", Count: 1}}, nil)
	list := log.List()
	if len(list) != 1 || list[0].Who != "?" {
		t.Fatalf("Add with no who should record '?' , got %+v", list)
	}
}

func TestActivityLogClearByKind(t *testing.T) {
	log := &ActivityLog{}
	now := time.Now()
	log.Add(now, TallyCollected, "A", []ItemCount{{Name: "Coins", Count: 1}}, nil)
	log.Add(now, TallyDelivered, "B", []ItemCount{{Name: "Coins", Count: 1}}, nil)
	log.Clear(TallyCollected)
	list := log.List()
	if len(list) != 1 || list[0].Kind != TallyDelivered {
		t.Fatalf("Clear(collected) should leave only delivered entries, got %+v", list)
	}
}

func TestActivityLogRestoreCapsAtMax(t *testing.T) {
	var entries []ActivityEntry
	for i := 0; i < ActivityMax+5; i++ {
		entries = append(entries, ActivityEntry{At: time.Now(), Kind: TallyCollected, Who: "A", Items: []ActivityItem{{Name: "Coins", Count: 1}}})
	}
	log := &ActivityLog{}
	log.Restore(entries)
	if got := len(log.List()); got != ActivityMax {
		t.Fatalf("Restore should cap at ActivityMax=%d, got %d", ActivityMax, got)
	}
}

func TestPickupQueueOffersOldestFirstAndKeepsFirstAsk(t *testing.T) {
	q := NewPickupQueue(time.Minute)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q.Offer(t0, "Donor2", tile.Tile{X: 1, Z: 1}, 5)
	q.Offer(t0.Add(time.Second), "Donor1", tile.Tile{X: 2, Z: 2}, 3)
	// Donor2 asks again later: its place in the queue (firstAt) should not move, but its tile/slots
	// should refresh.
	q.Offer(t0.Add(2*time.Second), "Donor2", tile.Tile{X: 9, Z: 9}, 10)

	pending := q.Pending(t0.Add(2 * time.Second))
	if len(pending) != 2 || pending[0].From != "Donor2" {
		t.Fatalf("Pending = %+v, want Donor2 first (it asked first)", pending)
	}
	if pending[0].Tile != (tile.Tile{X: 9, Z: 9}) || pending[0].Slots != 10 {
		t.Fatalf("Pending()[0] should carry Donor2's refreshed tile/slots, got %+v", pending[0])
	}
	if !pending[0].FirstAt.Equal(t0) {
		t.Fatalf("Pending()[0].FirstAt should stay at the original ask time, got %v want %v", pending[0].FirstAt, t0)
	}
}

func TestPickupQueueSweepsExpired(t *testing.T) {
	q := NewPickupQueue(10 * time.Second)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q.Offer(t0, "Donor1", tile.Tile{}, 1)
	if got := q.Size(t0.Add(5 * time.Second)); got != 1 {
		t.Fatalf("Size before ttl = %d, want 1", got)
	}
	if got := q.Size(t0.Add(11 * time.Second)); got != 0 {
		t.Fatalf("Size after ttl = %d, want 0 (a silent donor falls out on its own)", got)
	}
}

func TestPickupQueueNextTakeDrop(t *testing.T) {
	q := NewPickupQueue(time.Minute)
	t0 := time.Now()
	if _, ok := q.Next(t0); ok {
		t.Fatal("Next on an empty queue should report ok=false")
	}
	q.Offer(t0, "Donor1", tile.Tile{}, 1)
	next, ok := q.Next(t0)
	if !ok || next.From != "Donor1" {
		t.Fatalf("Next = %+v ok=%v, want Donor1", next, ok)
	}
	taken, ok := q.Take("donor1")
	if !ok || taken.From != "Donor1" {
		t.Fatalf("Take should match case-insensitively, got %+v ok=%v", taken, ok)
	}
	if q.Size(t0) != 0 {
		t.Fatal("Take should remove the request from the queue")
	}
	if _, ok := q.Take("Donor1"); ok {
		t.Fatal("a second Take of the same account should report ok=false")
	}

	q.Offer(t0, "Donor2", tile.Tile{}, 1)
	q.Drop("Donor2")
	if q.Size(t0) != 0 {
		t.Fatal("Drop should remove the request from the queue")
	}
}

func TestPickupQueueHeartbeatIndependentOfQueue(t *testing.T) {
	q := NewPickupQueue(time.Minute)
	t0 := time.Now()
	if _, ok := q.LastHeard("Donor1"); ok {
		t.Fatal("LastHeard should report ok=false before anything is heard")
	}
	q.Heartbeat(t0, "Donor1")
	last, ok := q.LastHeard("Donor1")
	if !ok || !last.Equal(t0) {
		t.Fatalf("LastHeard after a bare heartbeat = %v ok=%v, want %v", last, ok, t0)
	}
	if q.Size(t0) != 0 {
		t.Fatal("a bare heartbeat should not queue a request")
	}
}
