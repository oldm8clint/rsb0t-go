package mulebus

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestBusSincePicksUpNewMessagesOnly(t *testing.T) {
	b := NewBus()
	b.Publish(CollectMessage("donor1", tile.Tile{X: 1, Z: 2}, 5, ""))
	// Windows' clock resolution is coarse enough that back-to-back time.Now() calls can
	// read back identical values; a real sleep between the checkpoint and the next
	// Publish is what actually guarantees they land on different timestamps, matching
	// how a real Loop poll (100ms+ apart) would never hit this in practice.
	time.Sleep(20 * time.Millisecond)
	checkpoint := time.Now()
	time.Sleep(20 * time.Millisecond)
	b.Publish(CollectMessage("donor2", tile.Tile{X: 3, Z: 4}, 3, ""))

	got := b.Since(checkpoint)
	if len(got) != 1 || got[0].From != "donor2" {
		t.Fatalf("Since(checkpoint) = %+v, want only donor2's message", got)
	}
}

func TestWatcherPollAdvancesCheckpoint(t *testing.T) {
	b := NewBus()
	w := NewWatcher()

	b.Publish(HereMessage("alice", tile.Tile{X: 10, Z: 10}))
	time.Sleep(20 * time.Millisecond)
	first := w.Poll(b)
	if len(first) != 1 || first[0].From != "alice" {
		t.Fatalf("first Poll = %+v, want alice's message", first)
	}

	// A second poll with nothing new published must return nothing — the whole point of
	// a checkpoint-based watcher is that the same message is never handled twice.
	time.Sleep(20 * time.Millisecond)
	second := w.Poll(b)
	if len(second) != 0 {
		t.Fatalf("second Poll = %+v, want none (already consumed)", second)
	}

	b.Publish(HereMessage("bob", tile.Tile{X: 20, Z: 20}))
	time.Sleep(20 * time.Millisecond)
	third := w.Poll(b)
	if len(third) != 1 || third[0].From != "bob" {
		t.Fatalf("third Poll = %+v, want bob's message", third)
	}
}

func TestBusPublishIsConcurrencySafe(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup
	const n = 200
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Publish(HereMessage("racer", tile.Tile{X: i}))
		}(i)
	}
	wg.Wait()
	if got := len(b.Since(time.Time{})); got != n {
		t.Fatalf("got %d messages published concurrently, want %d", got, n)
	}
}

func TestBusTrimsToMaxEntries(t *testing.T) {
	b := NewBus()
	for i := 0; i < maxEntries+50; i++ {
		b.Publish(HereMessage("spammer", tile.Tile{X: i}))
	}
	got := b.Since(time.Time{})
	if len(got) != maxEntries {
		t.Fatalf("Since after overflow = %d entries, want capped at %d", len(got), maxEntries)
	}
	// The oldest entries should have been dropped — the first surviving X should be 50,
	// not 0.
	if got[0].Tile.X != 50 {
		t.Fatalf("oldest surviving entry has X=%d, want 50 (first 50 dropped)", got[0].Tile.X)
	}
}

func TestCollectMessageClampsSlots(t *testing.T) {
	if got := CollectMessage("d", tile.Tile{}, 0, "").Slots; got != 1 {
		t.Errorf("slots=0 clamped to %d, want 1", got)
	}
	if got := CollectMessage("d", tile.Tile{}, 99, "").Slots; got != 28 {
		t.Errorf("slots=99 clamped to %d, want 28", got)
	}
	if got := CollectMessage("d", tile.Tile{}, 10, "").Slots; got != 10 {
		t.Errorf("slots=10 clamped to %d, want 10", got)
	}
}

func TestOrderMessageTruncatesText(t *testing.T) {
	long := ""
	for i := 0; i < OrderTextMax+40; i++ {
		long += "x"
	}
	msg := OrderMessage("orderer", long, "")
	if len(msg.Text) != OrderTextMax {
		t.Fatalf("order text length = %d, want %d", len(msg.Text), OrderTextMax)
	}
}

func TestDeclinedMessageTruncatesReason(t *testing.T) {
	long := ""
	for i := 0; i < ReasonMax+20; i++ {
		long += "y"
	}
	msg := DeclinedMessage("mule", "donor", long)
	if len(msg.Reason) != ReasonMax {
		t.Fatalf("declined reason length = %d, want %d", len(msg.Reason), ReasonMax)
	}
}

func TestReadyMessageNeedOptional(t *testing.T) {
	noNeed := ReadyMessage("d", -1)
	if noNeed.Need != nil {
		t.Fatalf("ReadyMessage(-1).Need = %v, want nil (not carried)", *noNeed.Need)
	}
	withNeed := ReadyMessage("d", 5)
	if withNeed.Need == nil || *withNeed.Need != 5 {
		t.Fatalf("ReadyMessage(5).Need = %v, want pointer to 5", withNeed.Need)
	}
}

func TestNamesMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Alice", "alice", true},
		{" Bob ", "bob", true},
		{"", "", false},
		{"", "alice", false},
		{"alice", "bob", false},
		{"big_net", "Big net", true}, // a login name and the display name the game shows
		{"big_net", "bignet", false},
	}
	for _, c := range cases {
		if got := NamesMatch(c.a, c.b); got != c.want {
			t.Errorf("NamesMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestAddressedTo(t *testing.T) {
	broadcast := CollectMessage("d", tile.Tile{}, 5, "")
	if !AddressedTo(broadcast, "anyone") {
		t.Error("an unaddressed (To == \"\") message should be addressed to everyone")
	}
	addressed := CollectMessage("d", tile.Tile{}, 5, "Mulebot")
	if !AddressedTo(addressed, "mulebot") {
		t.Error("addressed message should match its recipient case-insensitively")
	}
	if AddressedTo(addressed, "someoneElse") {
		t.Error("addressed message should not match a different recipient")
	}
}

func TestRosterExpiry(t *testing.T) {
	r := NewRoster(10 * time.Second)
	t0 := time.Now()
	r.Mark(t0, "donor1")

	if !r.Has(t0.Add(5*time.Second), "donor1") {
		t.Error("donor1 should still be on the roster 5s into a 10s ttl")
	}
	if !r.Has(t0.Add(5*time.Second), "DONOR1") {
		t.Error("Has should be case-insensitive")
	}
	if r.Has(t0.Add(11*time.Second), "donor1") {
		t.Error("donor1 should have expired after 11s on a 10s ttl")
	}
}

func TestRosterForgetAndSize(t *testing.T) {
	r := NewRoster(time.Minute)
	now := time.Now()
	r.Mark(now, "a")
	r.Mark(now, "b")
	if got := r.Size(now); got != 2 {
		t.Fatalf("Size = %d, want 2", got)
	}
	r.Forget("a")
	if got := r.Size(now); got != 1 {
		t.Fatalf("Size after Forget = %d, want 1", got)
	}
	if r.Has(now, "a") {
		t.Error("a should be gone after Forget")
	}
}

func TestLatestReadsTheNewestHeartbeatOfOneSender(t *testing.T) {
	b := NewBus()
	start := time.Now()
	b.Publish(WithQueue(Heartbeat("mule", tile.Tile{X: 1}, 20, StateHome, nil), 3, 30*time.Second))
	b.Publish(Heartbeat("othermule", tile.Tile{X: 2}, 20, StateHome, nil))
	b.Publish(WithQueue(Heartbeat("mule", tile.Tile{X: 3}, 20, StateHome, nil), 5, 15*time.Second))
	b.Publish(ReserveMessage("acct1", "mule"))
	m, ok := b.Latest(KindMuleHere, "mule", start)
	if !ok || m.Tile.X != 3 || m.Queue != 5 || m.CycleMs != 15000 {
		t.Fatalf("Latest = %+v, %v; want the second mule heartbeat with queue 5 and a 15s cycle", m, ok)
	}
	if _, ok := b.Latest(KindMuleHere, "mule", time.Now().Add(time.Second)); ok {
		t.Error("a heartbeat older than since must not be returned")
	}
	if r, ok := b.Latest(KindReserve, "acct1", start); !ok || r.To != "mule" {
		t.Errorf("reserve = %+v, %v", r, ok)
	}
}

// TestWatcherNeverSkipsAMessagePublishedWhilePolling targets the old checkpoint race: Poll read
// the bus, released the lock, and only then set checkpoint = time.Now(), so a message published
// in that gap was stamped before the new checkpoint and never delivered.
func TestWatcherNeverSkipsAMessagePublishedWhilePolling(t *testing.T) {
	b := NewBus()
	w := NewWatcher()
	const writers, each = 4, 200
	var wg sync.WaitGroup
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				b.Publish(ReadyMessage(string(rune('a'+g)), i))
			}
		}(g)
	}
	seen := map[[2]int]int{}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	collect := func() {
		for _, m := range w.Poll(b) {
			seen[[2]int{int(m.From[0] - 'a'), *m.Need}]++
		}
	}
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		collect()
	}
	collect()
	if len(seen) != writers*each {
		t.Fatalf("saw %d distinct messages, want %d: some were skipped", len(seen), writers*each)
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("message %v delivered %d times, want exactly once", k, n)
		}
	}
}

func TestKitMessagesCarryTheirLinesAndTheQueuedAck(t *testing.T) {
	var lines []KitLine
	for i := 0; i < KitLinesMax+3; i++ {
		lines = append(lines, KitLine{Item: "Cake", Qty: i + 1})
	}
	lines[1].Noted = true // a donor asking for that line as notes
	o := KitOrderMessage("acct1", "mule", "acct1-1", lines, 19, 5*time.Minute)
	if o.Kind != KindKitOrder || len(o.Kit) != KitLinesMax || o.FreeSlots != 19 || o.KitID != "acct1-1" || o.EtaMs != 300000 {
		t.Fatalf("order %+v", o)
	}
	if o.Kit[0].Noted || !o.Kit[1].Noted {
		t.Fatalf("the order must carry which lines were asked as notes: %+v", o.Kit[:2])
	}
	ack := KitAnswerMessage("mule", "acct1", "acct1-1", nil, "", 30*time.Second)
	if !IsKitQueued(ack) || ack.EtaMs != 30000 {
		t.Fatalf("ack %+v", ack)
	}
	plan := KitAnswerMessage("mule", "acct1", "acct1-1", []KitLine{{Item: "Cake", Qty: 5, Noted: true}}, "", 0)
	if IsKitQueued(plan) || !plan.Kit[0].Noted {
		t.Fatalf("plan %+v", plan)
	}
	no := KitAnswerMessage("mule", "acct1", "acct1-1", nil, "hand-outs are switched off", 0)
	if IsKitQueued(no) {
		t.Fatal("a refusal read as a queued ack")
	}
	if c := KitCancelMessage("acct1", "mule", "acct1-1"); c.Kind != KindCancel || c.KitID != "acct1-1" {
		t.Fatalf("kit cancel %+v", c)
	}
}
