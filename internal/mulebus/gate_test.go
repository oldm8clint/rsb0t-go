package mulebus

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// put adds a message to the bus stamped at `at`, the way Publish does at now.
func put(b *Bus, at time.Time, m Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	b.entries = append(b.entries, stamped{at: at, seq: b.seq, msg: m})
}

func TestGateMessageRoundTrip(t *testing.T) {
	b := NewBus()
	now := time.Now()
	at := tile.Tile{X: 2866, Z: 3021}
	put(b, now.Add(-4*time.Second), GateMessage("Main Crafter", at, GateOpening, 90*time.Second, 0))
	put(b, now.Add(-2*time.Second), GateMessage("Main Crafter", at, GateOpen, 0, 2300))
	v := ReadGate(b, "main_crafter", now, 0)
	if !v.Heard || v.State != GateOpen || v.Surplus != 2300 || v.Tile != at {
		t.Fatalf("view: %+v", v)
	}
	if !v.Usable() {
		t.Fatal("an open gate is usable")
	}
	if v.Age < 1500*time.Millisecond || v.Age > 2500*time.Millisecond {
		t.Fatalf("age %s, want about 2s", v.Age)
	}
}

func TestGateOpeningIsUsableClosedAndAwayAreNot(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		state  GateState
		usable bool
	}{{GateOpening, true}, {GateOpen, true}, {GateAway, false}, {GateClosed, false}} {
		b := NewBus()
		put(b, now.Add(-time.Second), GateMessage("main", tile.Tile{X: 1, Z: 1}, tc.state, time.Minute, 0))
		if v := ReadGate(b, "main", now, 0); v.Usable() != tc.usable || v.State != tc.state {
			t.Errorf("%s: %+v", tc.state, v)
		}
	}
}

func TestGateStaleOrOtherCrafterIsNotHeard(t *testing.T) {
	b := NewBus()
	now := time.Now()
	put(b, now.Add(-GateTTL-time.Second), GateMessage("main", tile.Tile{X: 1, Z: 1}, GateOpen, 0, 0))
	put(b, now.Add(-time.Second), GateMessage("someone", tile.Tile{X: 1, Z: 1}, GateOpen, 0, 0))
	if v := ReadGate(b, "main", now, 0); v.Heard {
		t.Fatalf("a stale message from main and a fresh one from someone else were heard as main's: %+v", v)
	}
	if v := ReadGate(b, "", now, 0); v.Heard {
		t.Fatal("a blank crafter name hears nothing")
	}
	if v := ReadGate(nil, "main", now, 0); v.Heard {
		t.Fatal("no bus hears nothing")
	}
	// An unreadable state is not a gate.
	put(b, now, Message{Kind: KindRuneGate, From: "main", Text: "wide open"})
	if v := ReadGate(b, "main", now, 0); v.Heard {
		t.Fatalf("a state that is none of the four: %+v", v)
	}
}

func TestReadCarriersKeepsTheNewestPerCarrier(t *testing.T) {
	b := NewBus()
	now := time.Now()
	put(b, now.Add(-30*time.Second), CarrierMessage("acct1", "main", tile.Tile{X: 2767, Z: 3122}, CarrierTransit, 25))
	put(b, now.Add(-20*time.Second), CarrierMessage("acct2", "main", tile.Tile{X: 2865, Z: 3022}, CarrierWaiting, 25))
	put(b, now.Add(-5*time.Second), CarrierMessage("Acct1", "main", tile.Tile{X: 2865, Z: 3023}, CarrierCourier, 0))
	put(b, now.Add(-5*time.Second), CarrierMessage("other", "elsewhere", tile.Tile{}, CarrierWaiting, 25))
	put(b, now.Add(-CarrierTTL-time.Second), CarrierMessage("old", "main", tile.Tile{}, CarrierWaiting, 25))
	got := ReadCarriers(b, "main", now, 0)
	if len(got) != 2 {
		t.Fatalf("carriers: %+v", got)
	}
	if got[0].Role != CarrierCourier || got[0].Name != "Acct1" || got[1].Name != "acct2" || got[1].Held != 25 {
		t.Fatalf("carriers: %+v", got)
	}
}

func TestGateMessagesClampTheirNumbers(t *testing.T) {
	m := GateMessage("a very long account name", tile.Tile{}, GateOpen, -time.Second, -5)
	if m.EtaMs != 0 || m.Qty != 0 || len(m.From) > NameMax {
		t.Fatalf("%+v", m)
	}
	if c := CarrierMessage("a very long account name", "main", tile.Tile{}, CarrierHome, -3); c.Qty != 0 || len(c.From) > NameMax {
		t.Fatalf("%+v", c)
	}
}

func TestPublishedGateIsReadBack(t *testing.T) {
	b := NewBus()
	b.Publish(GateMessage("main", tile.Tile{X: 5, Z: 6}, GateOpen, 0, 10))
	v := ReadGate(b, "main", time.Now().Add(time.Millisecond), 0)
	if !v.Heard || v.Surplus != 10 {
		t.Fatalf("%+v", v)
	}
}

// A carrier between shifts says it stands by; the crafter counts it ready, not at work, and the newest message decides.
func TestStandbyIsReadyNotAtWork(t *testing.T) {
	b := NewBus()
	now := time.Now()
	put(b, now.Add(-90*time.Second), StandbyMessage("acct1", "main", tile.Tile{X: 2809, Z: 3441}))
	put(b, now.Add(-40*time.Second), StandbyMessage("Acct 2", "main", tile.Tile{X: 2700, Z: 3400}))
	put(b, now.Add(-20*time.Second), CarrierMessage("acct3", "main", tile.Tile{X: 2865, Z: 3022}, CarrierWaiting, 25))
	put(b, now.Add(-5*time.Second), StandbyMessage("elsewhere", "someone else", tile.Tile{}))
	standby := ReadStandby(b, "main", now, 0)
	if len(standby) != 2 || standby[0].Name != "acct1" || standby[1].Name != "Acct 2" || standby[0].Role != CarrierStandby {
		t.Fatalf("standby: %+v", standby)
	}
	work := ReadCarriers(b, "main", now, 0)
	if len(work) != 1 || work[0].Name != "acct3" {
		t.Fatalf("at work: %+v (a standby is not at work)", work)
	}
	// A shift begins: its first message takes the carrier off the standby list; its end (a standby again) takes it off the work list.
	put(b, now.Add(-2*time.Second), CarrierMessage("acct1", "main", tile.Tile{}, CarrierTransit, 0))
	put(b, now.Add(-time.Second), StandbyMessage("acct3", "main", tile.Tile{}))
	standby = ReadStandby(b, "main", now, 0)
	if len(standby) != 2 || standby[0].Name != "Acct 2" || standby[1].Name != "acct3" {
		t.Fatalf("after the shifts changed hands: standby %+v", standby)
	}
	work = ReadCarriers(b, "main", now, 0)
	if len(work) != 1 || work[0].Name != "acct1" || work[0].Role != CarrierTransit {
		t.Fatalf("after the shifts changed hands: at work %+v", work)
	}
}

func TestStandbyGoesStale(t *testing.T) {
	b := NewBus()
	now := time.Now()
	put(b, now.Add(-StandbyTTL-time.Second), StandbyMessage("acct1", "main", tile.Tile{}))
	if got := ReadStandby(b, "main", now, 0); len(got) != 0 {
		t.Fatalf("a standby older than %s is not heard: %+v", StandbyTTL, got)
	}
	put(b, now.Add(-2*time.Minute), StandbyMessage("acct2", "main", tile.Tile{}))
	if got := ReadStandby(b, "main", now, 0); len(got) != 1 || got[0].Name != "acct2" {
		t.Fatalf("a standby two minutes old is heard: %+v", got)
	}
	if got := ReadStandby(b, "", now, 0); got != nil {
		t.Fatal("a blank crafter hears nothing")
	}
	if got := ReadStandby(nil, "main", now, 0); got != nil {
		t.Fatal("no bus hears nothing")
	}
	if !CarrierStandby.Valid() {
		t.Fatal("standby is a role")
	}
}

// The gate message says how many essence the crafter takes in one trade (a stack of bows to alch is a 4th pack slot: 24), and a gate that
// says nothing takes a full load.
func TestGateCapacityRoundTrip(t *testing.T) {
	b := NewBus()
	now := time.Now()
	b.Publish(GateMessage("MasterCrafter", tile.Tile{X: 2867, Z: 3020}, GateOpen, 0, 0).WithCapacity(24))
	if g := ReadGate(b, "MasterCrafter", now.Add(time.Millisecond), 0); !g.Heard || g.Capacity != 24 {
		t.Fatalf("%+v", g)
	}
	b2 := NewBus()
	b2.Publish(GateMessage("MasterCrafter", tile.Tile{}, GateOpen, 0, 0))
	if g := ReadGate(b2, "MasterCrafter", now.Add(time.Millisecond), 0); !g.Heard || g.Capacity != 0 {
		t.Fatalf("an unsaid capacity is 0: %+v", g)
	}
	if m := GateMessage("c", tile.Tile{}, GateOpen, 0, 0).WithCapacity(-4); m.FreeSlots != 0 {
		t.Fatalf("a negative capacity is clamped: %+v", m)
	}
}
