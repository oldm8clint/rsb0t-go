package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// collect returns a Subscriber that appends every Event it receives to *out, plus a
// helper to read them back — used across this file instead of channels since every
// Handle call here happens synchronously on the test goroutine.
func collect(out *[]Event) Subscriber {
	return func(ev Event) { *out = append(*out, ev) }
}

// --- EventLevelUp: the boundary this task's task description explicitly calls out ---

func TestSubscribeLevelUpFiresOnRealIncrease(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(levelUpsOnly(collect(&got))) // skill.xp events are covered in scene_test.go

	// First UPDATE_STAT for Fishing (stat 6): must NOT fire a level-up, even though
	// level (37) is "greater than" the zero-valued previous — there is no real
	// previous observation yet (Generation was 0).
	p := packet.NewWriter(6)
	p.P1(6)
	p.P4(123450)
	p.P1(37)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	if len(got) != 0 {
		t.Fatalf("first-ever UPDATE_STAT fired an event: %+v", got)
	}

	// Second UPDATE_STAT, same level (37): no level-up, level didn't rise.
	p = packet.NewWriter(6)
	p.P1(6)
	p.P4(123460)
	p.P1(37)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	if len(got) != 0 {
		t.Fatalf("same-level UPDATE_STAT fired an event: %+v", got)
	}

	// Third UPDATE_STAT, level rises 37 -> 38: real level-up.
	p = packet.NewWriter(6)
	p.P1(6)
	p.P4(140000)
	p.P1(38)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	if len(got) != 1 {
		t.Fatalf("expected exactly one level-up event, got %d: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != EventLevelUp || ev.Skill != 6 || ev.Level != 38 || ev.PreviousLevel != 37 || ev.XP != 140000 {
		t.Fatalf("unexpected level-up event: %+v", ev)
	}
}

func TestSubscribeLevelUpDoesNotFireOnDecreaseOrSameLevel(t *testing.T) {
	s := New()
	// Prime a previous observation at level 50.
	p := packet.NewWriter(6)
	p.P1(1)
	p.P4(1000000)
	p.P1(50)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])

	var got []Event
	s.Subscribe(levelUpsOnly(collect(&got))) // skill.xp events are covered in scene_test.go

	// A drop to 49 (e.g. a stat-draining effect) must not report a level-up.
	p = packet.NewWriter(6)
	p.P1(1)
	p.P4(999000)
	p.P1(49)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	if len(got) != 0 {
		t.Fatalf("level decrease fired a level-up event: %+v", got)
	}
}

func TestSubscribeLevelUpIgnoresInvalidStatIndex(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	p := packet.NewWriter(6)
	p.P1(255) // out of range
	p.P4(100)
	p.P1(1)
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	if len(got) != 0 {
		t.Fatalf("invalid stat index fired an event: %+v", got)
	}
}

// --- EventInventoryChanged ---

func TestSubscribeInventoryChangedOnFull(t *testing.T) {
	s := New()

	// Establish a baseline: comId 3214, one slot holding item 4 (wire id 5) x1.
	base := packet.NewWriter(16)
	base.P2(3214)
	base.P2(2) // size
	base.P2(5) // wire id (obj 4 + 1)
	base.P1(1) // count
	base.P2(0) // slot 1 empty
	base.P1(0)
	s.Handle(serverprot.UPDATE_INV_FULL, base.Data[:base.Pos])

	var got []Event
	s.Subscribe(collect(&got))

	// Resend full: slot 0 count goes 1 -> 5, slot 1 gains item 9 (wire 10) x2.
	full := packet.NewWriter(16)
	full.P2(3214)
	full.P2(2)
	full.P2(5)
	full.P1(5)
	full.P2(10)
	full.P1(2)
	s.Handle(serverprot.UPDATE_INV_FULL, full.Data[:full.Pos])

	if len(got) != 2 {
		t.Fatalf("expected 2 inventory-changed events, got %d: %+v", len(got), got)
	}
	for _, ev := range got {
		if ev.Kind != EventInventoryChanged || ev.Comp != 3214 {
			t.Fatalf("unexpected event: %+v", ev)
		}
	}
	if got[0].Slot != 0 || got[0].ItemID != 4 || got[0].PreviousID != 4 || got[0].Count != 5 || got[0].PreviousCount != 1 {
		t.Fatalf("unexpected slot 0 event: %+v", got[0])
	}
	if got[1].Slot != 1 || got[1].ItemID != 9 || got[1].PreviousID != 0 || got[1].Count != 2 || got[1].PreviousCount != 0 {
		t.Fatalf("unexpected slot 1 event: %+v", got[1])
	}
}

func TestSubscribeInventoryChangedNoEventWhenUnchanged(t *testing.T) {
	s := New()
	frame := func() []byte {
		p := packet.NewWriter(16)
		p.P2(3214)
		p.P2(1)
		p.P2(5)
		p.P1(1)
		return p.Data[:p.Pos]
	}
	s.Handle(serverprot.UPDATE_INV_FULL, frame())

	var got []Event
	s.Subscribe(collect(&got))
	s.Handle(serverprot.UPDATE_INV_FULL, frame())

	if len(got) != 0 {
		t.Fatalf("identical full resync fired events: %+v", got)
	}
}

func TestSubscribeInventoryChangedOnPartial(t *testing.T) {
	s := New()
	full := packet.NewWriter(16)
	full.P2(3214)
	full.P2(1)
	full.P2(5) // obj 4
	full.P1(1)
	s.Handle(serverprot.UPDATE_INV_FULL, full.Data[:full.Pos])

	var got []Event
	s.Subscribe(collect(&got))

	// Partial: slot 0 drops to 0 (removed), matching a real drop/consume.
	partial := packet.NewWriter(16)
	partial.P2(3214)
	partial.P1(0) // gsmart slot 0
	partial.P2(0) // wire id 0 = empty
	partial.P1(0)
	s.Handle(serverprot.UPDATE_INV_PARTIAL, partial.Data[:partial.Pos])

	if len(got) != 1 {
		t.Fatalf("expected 1 inventory-changed event, got %d: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != EventInventoryChanged || ev.Comp != 3214 || ev.Slot != 0 || ev.ItemID != 0 || ev.PreviousID != 4 || ev.Count != 0 || ev.PreviousCount != 1 {
		t.Fatalf("unexpected partial-diff event: %+v", ev)
	}
}

// --- EventChatMessage ---

func TestSubscribeChatMessage(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	p := packet.NewWriter(32)
	p.PJStr("Hello, world!")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])

	if len(got) != 1 {
		t.Fatalf("expected 1 chat event, got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventChatMessage || got[0].Text != "Hello, world!" {
		t.Fatalf("unexpected chat event: %+v", got[0])
	}
}

// --- EventModalChanged ---

func TestSubscribeModalChangedOnOpenAndClose(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	open := packet.NewWriter(4)
	open.P2(1234)
	s.Handle(serverprot.IF_OPENMAIN, open.Data[:open.Pos])

	if len(got) != 1 {
		t.Fatalf("expected 1 modal-changed event after open, got %d: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != EventModalChanged || ev.Main != 1234 || ev.PreviousMain != NoModal || ev.Side != NoModal || ev.Chat != NoModal {
		t.Fatalf("unexpected open event: %+v", ev)
	}

	s.Handle(serverprot.IF_CLOSE, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 modal-changed events after close, got %d: %+v", len(got), got)
	}
	ev = got[1]
	if ev.Kind != EventModalChanged || ev.Main != NoModal || ev.PreviousMain != 1234 {
		t.Fatalf("unexpected close event: %+v", ev)
	}
}

func TestSubscribeModalChangedNoEventWhenAlreadyClosed(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	// State starts with everything NoModal; IF_CLOSE on top of that changes nothing.
	s.Handle(serverprot.IF_CLOSE, nil)
	if len(got) != 0 {
		t.Fatalf("closing an already-closed modal fired an event: %+v", got)
	}
}

func TestSubscribeModalChangedFromLocalCloseModal(t *testing.T) {
	s := New()
	open := packet.NewWriter(4)
	open.P2(999)
	s.Handle(serverprot.IF_OPENSIDE, open.Data[:open.Pos])

	var got []Event
	s.Subscribe(collect(&got))
	s.LocalCloseModal()

	if len(got) != 1 {
		t.Fatalf("expected 1 event from LocalCloseModal, got %d: %+v", len(got), got)
	}
	if got[0].Kind != EventModalChanged || got[0].PreviousSide != 999 || got[0].Side != NoModal {
		t.Fatalf("unexpected LocalCloseModal event: %+v", got[0])
	}
}

// --- Subscribe/cancel plumbing ---

func TestSubscribeCancelStopsDelivery(t *testing.T) {
	s := New()
	var got []Event
	cancel := s.Subscribe(collect(&got))
	cancel()
	cancel() // must be a safe no-op the second time

	p := packet.NewWriter(32)
	p.PJStr("should not be delivered")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])

	if len(got) != 0 {
		t.Fatalf("event delivered after cancel: %+v", got)
	}
}

func TestSubscribeMultipleSubscribersAllReceive(t *testing.T) {
	s := New()
	var a, b []Event
	s.Subscribe(collect(&a))
	s.Subscribe(collect(&b))

	p := packet.NewWriter(32)
	p.PJStr("fan-out")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])

	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected both subscribers to receive the event: a=%d b=%d", len(a), len(b))
	}
}
