package actions

import (
	"errors"
	"testing"
	"time"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// blockingWaiter reports every wait as completed without sleeping, so these tests run instantly.
type blockingWaiter struct{ ticks int }

func (b *blockingWaiter) Delay(time.Duration) bool { return true }
func (b *blockingWaiter) DelayUntil(cond func() bool, _ time.Duration) bool {
	return cond()
}
func (b *blockingWaiter) DelayTicks(n int) bool {
	b.ticks += n
	return true
}

func TestInteractionBlockedMirrorsTheEnginesOwnMask(t *testing.T) {
	f := newFakeSession()
	if InteractionBlocked(f) {
		t.Fatal("nothing is open, so nothing blocks")
	}

	openChat(f.state, 306)
	if !InteractionBlocked(f) {
		t.Fatal("a chat modal blocks an interaction")
	}
	closeModal(f.state)

	openMain(f.state, 3824)
	if !InteractionBlocked(f) {
		t.Fatal("a main modal blocks an interaction")
	}
	closeModal(f.state)

	// A side modal is deliberately not blocking: the engine's containsModalInterface() masks
	// only MAIN and CHAT, so the backpack tab being on screen never stops a click.
	f.state.Handle(serverprot.IF_OPENSIDE, payload(func(p *packet.Packet) { p.P2(3213) }))
	if InteractionBlocked(f) {
		t.Fatal("a side modal does not block an interaction")
	}
}

func TestClearInteractionBlockSendsCloseModalAndWaitsForTheEngine(t *testing.T) {
	f := newFakeSession()
	w := &blockingWaiter{}

	if err := ClearInteractionBlock(f, w); err != nil {
		t.Fatalf("nothing open: %v", err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("nothing open should send nothing, sent %d", f.sendCount())
	}

	openChat(f.state, 306)
	if err := ClearInteractionBlock(f, w); err != nil {
		t.Fatalf("clearing an open box: %v", err)
	}
	if f.sendCount() != 1 {
		t.Fatalf("sent %d packets, want one CLOSE_MODAL", f.sendCount())
	}
	if got := f.lastSent()[0]; int(got) != clientprot.CLOSE_MODAL {
		t.Fatalf("sent opcode %d, want CLOSE_MODAL %d", got, clientprot.CLOSE_MODAL)
	}
	// The wait is the point: CloseModalHandler only sets requestModalClose, and the engine
	// turns that into a real close on its next tick.
	if w.ticks < modalClearTicks {
		t.Fatalf("waited %d ticks, want at least %d for the engine to act on CLOSE_MODAL", w.ticks, modalClearTicks)
	}
}

// reopeningWaiter stands in for a server that keeps pushing a new box at us, the level-up case.
type reopeningWaiter struct {
	f    *fakeSession
	chat int
}

func (r *reopeningWaiter) Delay(time.Duration) bool                          { return true }
func (r *reopeningWaiter) DelayUntil(cond func() bool, _ time.Duration) bool { return cond() }
func (r *reopeningWaiter) DelayTicks(int) bool {
	openChat(r.f.state, r.chat)
	return true
}

func TestClearInteractionBlockGivesUpRatherThanClickingIntoAModal(t *testing.T) {
	f := newFakeSession()
	openChat(f.state, 306)

	err := ClearInteractionBlock(f, &reopeningWaiter{f: f, chat: 306})
	if !errors.Is(err, ErrInteractionBlocked) {
		t.Fatalf("a box that reopens forever must be reported, got %v", err)
	}
	if f.sendCount() != modalClearAttempts {
		t.Fatalf("sent %d CLOSE_MODALs, want %d", f.sendCount(), modalClearAttempts)
	}
}

func TestInteractionRejectionsCountsTheServersOwnRefusals(t *testing.T) {
	f := newFakeSession()
	before := InteractionRejections(f)
	f.state.Handle(serverprot.UNSET_MAP_FLAG, nil)
	if got := InteractionRejections(f); got != before+1 {
		t.Fatalf("rejections %d -> %d, want one more", before, got)
	}
}
