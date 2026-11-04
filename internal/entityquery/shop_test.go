package entityquery

import (
	"errors"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// shopSession opens the shop when it sees OPNPC3 (Trade) and open is set.
type shopSession struct {
	st   *gamestate.State
	c    anticheat.Counters
	sent int
	// opcodes records every opcode written, so a test can assert on the ORDER of what went out
	// rather than only how much did.
	opcodes []int
	open    bool
}

func (s *shopSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(8)
	build(w)
	s.sent++
	s.opcodes = append(s.opcodes, int(w.Data[0]))
	if s.open && int(w.Data[0]) == clientprot.OPNPC3 {
		handle(s.st, serverprot.IF_OPENMAIN_SIDE, func(p *packet.Packet) { p.P2(actions.ShopRoot); p.P2(3822) })
	}
	return nil
}
func (s *shopSession) State() *gamestate.State     { return s.st }
func (s *shopSession) Cheats() *anticheat.Counters { return &s.c }

type onceWaiter struct{}

func (onceWaiter) Delay(time.Duration) bool                          { return true }
func (onceWaiter) DelayUntil(cond func() bool, _ time.Duration) bool { return cond() }
func (onceWaiter) DelayTicks(int) bool                               { return true }

func TestOpenShopTradesWithTheNamedKeeper(t *testing.T) {
	st := scene()
	addNpcs(st, [4]int{9, 520, 2, 1}) // a Shop keeper two tiles east
	s := &shopSession{st: st, open: true}

	if err := OpenShop(s, fakeTypes{}, "shop keeper", onceWaiter{}); err != nil {
		t.Fatal(err)
	}
	if s.sent != 1 || !actions.NewShop(s).IsOpen() {
		t.Fatalf("sent %d, open %v", s.sent, actions.NewShop(s).IsOpen())
	}
	if err := OpenShop(s, fakeTypes{}, "shop keeper", onceWaiter{}); err != nil || s.sent != 1 {
		t.Fatalf("already open: err = %v, sent = %d", err, s.sent)
	}
}

func TestOpenShopFailures(t *testing.T) {
	st := scene()
	// Adjacent, so this test stays about an interface that will not open rather than about a
	// keeper we cannot reach, which is TestOpenShopStopsWhenTheKeeperIsOutOfReach's job.
	addNpcs(st, [4]int{9, 520, 1, 1})
	s := &shopSession{st: st}
	err := OpenShop(s, fakeTypes{}, "shop keeper", onceWaiter{})
	if !errors.Is(err, actions.ErrNotConfirmed) {
		t.Fatalf("keeper never opens the shop: err = %v", err)
	}
	if s.sent != shopOpenAttempts {
		t.Fatalf("sent %d, want %d attempts", s.sent, shopOpenAttempts)
	}
	if err := OpenShop(s, fakeTypes{}, "chicken", onceWaiter{}); err != ErrNoShopkeeper {
		t.Fatalf("a chicken does not trade: err = %v", err)
	}
}

// TestOpenShopStopsWhenTheKeeperIsOutOfReach covers the case measured live at the Varrock sword
// shop: the engine walks the account partway and then stops outside the shop wall, because its
// own routefinder will not take it through the door. Spending the remaining clicks on that
// cannot help, so the primitive says which distance beat it and lets the caller walk.
func TestOpenShopStopsWhenTheKeeperIsOutOfReach(t *testing.T) {
	st := scene()
	addNpcs(st, [4]int{9, 520, 8, 0}) // a Shop keeper eight tiles east
	s := &shopSession{st: st}

	err := OpenShop(s, fakeTypes{}, "shop keeper", onceWaiter{})
	if !errors.Is(err, ErrKeeperOutOfReach) {
		t.Fatalf("a keeper we never get closer to: err = %v, want ErrKeeperOutOfReach", err)
	}
	// Two clicks prove the point: the first establishes how close we can get, the second shows
	// that it did not improve. The remaining two would only repeat it.
	if s.sent != 2 {
		t.Fatalf("sent %d clicks at an unreachable keeper, want 2", s.sent)
	}
}

// TestOpenShopClosesADialogueFirst is the regression for the bug this primitive kept hitting
// live: the engine accepts a Trade click sent while a chat modal is open and then never runs it,
// so the shop could never appear however long the old code waited.
func TestOpenShopClosesADialogueFirst(t *testing.T) {
	st := scene()
	addNpcs(st, [4]int{9, 520, 2, 1})
	// A level-up box, which content opens with if_openchat + p_pausebutton on every level-up.
	handle(st, serverprot.IF_OPENCHAT, func(p *packet.Packet) { p.P2(306) })
	s := &shopSession{st: st, open: true}

	if err := OpenShop(s, fakeTypes{}, "shop keeper", onceWaiter{}); err != nil {
		t.Fatal(err)
	}
	if len(s.opcodes) != 2 {
		t.Fatalf("sent %v, want a CLOSE_MODAL then the Trade click", s.opcodes)
	}
	if s.opcodes[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("first packet was opcode %d, want CLOSE_MODAL %d — the Trade click would have been swallowed", s.opcodes[0], clientprot.CLOSE_MODAL)
	}
	if s.opcodes[1] != clientprot.OPNPC3 {
		t.Fatalf("second packet was opcode %d, want OPNPC3 %d", s.opcodes[1], clientprot.OPNPC3)
	}
}
