package fastopen

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

const (
	pack                     = 3214
	casket, oyster           = 405, 407
	coins, sapphire          = 995, 1623
	pearl, emptyOyster       = 411, 409
	opHeldLen, closeModalLen = 7, 1
	opHeldULen               = 13
	// The mixed-burst tests' items: junk to drop, the two key halves and the crystal key they make,
	// the chisel and the Pearl bolttips a chisel cuts an Oyster pearl into.
	junk, halfA, halfB, crystalKey = 500, 985, 987, 989
	chisel, tips                   = 1755, 46
)

// engine plays the engine's side of opening, tick by tick, as content 289 and engine 289 run it
// (see the package comment): five handled user events read a tick, the rest queued; an OPHELD whose
// slot does not hold its item is refused and does not count; a casket removes its own slot, adds a
// reward (coins stack, an uncut gem does not) to the lowest empty slot and opens a reward box; an
// oyster removes the lowest shell and adds a pearl or an empty oyster; an OPHELD on the backpack
// closes an open box first.
type engine struct {
	st      *gamestate.State
	cheats  anticheat.Counters
	pack    [28]struct{ id, n int32 }
	frames  [][][]byte
	pending [][]byte
	box     bool
	opened  int
	dropped int
	cut     int
	joined  int
	n       int // a counter that picks the rewards in turn
	// sentSlots is the clicked slot of every packet sent, in the order they were sent.
	sentSlots []int

	ticks       int
	perTick     map[int]int
	mostQueued  int
	strayAtTick map[int]int // that many extra handled events (a stray click) at the head of that tick
	refuse      map[int]bool
}

func (e *engine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		n := opHeldLen
		switch int(b[0]) {
		case clientprot.CLOSE_MODAL:
			n = closeModalLen
		case clientprot.OPHELDU:
			n = opHeldULen
		}
		if int(b[0]) != clientprot.CLOSE_MODAL {
			e.sentSlots = append(e.sentSlots, g2(b, 3)) // the clicked (target) slot of an OPHELD and an OPHELDU
		}
		pkts = append(pkts, b[:n])
		b = b[n:]
	}
	e.frames = append(e.frames, pkts)
	return nil
}
func (e *engine) State() *gamestate.State     { return e.st }
func (e *engine) Cheats() *anticheat.Counters { return &e.cheats }

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (e *engine) add(id int32) {
	if id == coins {
		for i := range e.pack {
			if e.pack[i].id == coins {
				e.pack[i].n += 20
				return
			}
		}
	}
	for i := range e.pack {
		if e.pack[i].id == 0 {
			e.pack[i].id, e.pack[i].n = id, 1
			return
		}
	}
}

// addStack puts n of id in the pack the way inv_add does: onto its stack when it has one (stackable),
// else the lowest empty slot.
func (e *engine) addStack(id int32, n int32, stackable bool) {
	if stackable {
		for i := range e.pack {
			if e.pack[i].id == id {
				e.pack[i].n += n
				return
			}
		}
	}
	for i := range e.pack {
		if e.pack[i].id == 0 {
			e.pack[i].id, e.pack[i].n = id, n
			return
		}
	}
}

// removeFirst deletes one of id from the FIRST slot holding it, as inv_del does.
func (e *engine) removeFirst(id int32) bool {
	for i := range e.pack {
		if e.pack[i].id == id {
			e.pack[i] = struct{ id, n int32 }{}
			return true
		}
	}
	return false
}

func (e *engine) read(b []byte) bool {
	switch int(b[0]) {
	case clientprot.CLOSE_MODAL:
		e.box = false
		return true
	case clientprot.OPHELD5: // Drop: the slot empties and nothing delays
		obj, slot := int32(g2(b, 1)), g2(b, 3)
		if slot >= 28 || e.pack[slot].id != obj || e.refuse[slot] {
			return false
		}
		e.box = false
		e.pack[slot] = struct{ id, n int32 }{}
		e.dropped++
		e.perTick[e.ticks]++
		return true
	case clientprot.OPHELDU: // the target (obj, slot, com), then the used item (obj, slot, com)
		target, tslot := int32(g2(b, 1)), g2(b, 3)
		used, uslot := int32(g2(b, 7)), g2(b, 9)
		if tslot >= 28 || uslot >= 28 || e.pack[tslot].id != target || e.pack[uslot].id != used || e.refuse[tslot] {
			return false
		}
		e.box = false
		switch {
		case target == pearl && used == chisel, target == chisel && used == pearl:
			e.removeFirst(pearl) // [label,make_bolt_tips]: one pearl a use, six tips onto the stack
			e.addStack(tips, 6, true)
			e.cut++
		case target == halfA && used == halfB, target == halfB && used == halfA:
			e.removeFirst(halfA) // [label,join_keys]: one of each half, one crystal key
			e.removeFirst(halfB)
			e.addStack(crystalKey, 1, false)
			e.joined++
		default:
			return false
		}
		e.perTick[e.ticks]++
		return true
	case clientprot.OPHELD1:
		obj, slot := int32(g2(b, 1)), g2(b, 3)
		if slot >= 28 || e.pack[slot].id != obj || e.refuse[slot] {
			return false
		}
		e.box = false // clearPendingAction: the backpack is not the main modal
		e.n++
		e.opened++
		e.perTick[e.ticks]++
		switch obj {
		case casket:
			e.pack[slot] = struct{ id, n int32 }{}
			if e.n%2 == 0 {
				e.add(coins)
			} else {
				e.add(sapphire)
			}
			e.box = true
		case oyster:
			for i := range e.pack {
				if e.pack[i].id == oyster {
					e.pack[i] = struct{ id, n int32 }{}
					break
				}
			}
			if e.n%4 == 0 {
				e.add(emptyOyster)
			} else {
				e.add(pearl)
			}
		}
		return true
	}
	return false
}

func (e *engine) tick() {
	e.ticks++
	for len(e.frames) > 0 { // every frame sent before the tick is in the buffer
		e.pending = append(e.pending, e.frames[0]...)
		e.frames = e.frames[1:]
	}
	e.mostQueued = max(e.mostQueued, len(e.pending))
	user, client := e.strayAtTick[e.ticks], 0
	wasBox := e.box
	for len(e.pending) > 0 && user < 5 && client < 20 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		if e.read(b) {
			user++
		} else {
			client++
		}
	}
	e.push()
	if e.box && !wasBox {
		e.st.Handle(serverprot.IF_OPENCHAT, []byte{0x01, 0x00})
	} else if !e.box && wasBox {
		e.st.Handle(serverprot.IF_CLOSE, nil)
	}
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *engine) push() {
	p := packet.NewWriter(256)
	p.P2(pack)
	p.P2(28)
	for _, s := range e.pack {
		if s.id == 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(s.id + 1))
		p.P1(byte(s.n))
	}
	e.st.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

func (e *engine) count(id int32) int {
	n := 0
	for _, s := range e.pack {
		if s.id == id {
			n++
		}
	}
	return n
}

func newEngine(items ...int32) *engine {
	e := &engine{st: gamestate.New(), perTick: map[int]int{}}
	e.cheats.SetEnabled(false)
	for i, id := range items {
		e.pack[i].id, e.pack[i].n = id, 1
	}
	e.push()
	return e
}

func (e *engine) held() []gamestate.SlotItem {
	return e.st.Inventory(pack).NonEmpty()
}

func (e *engine) delay(time.Duration) bool { e.tick(); return true }

func of(id int32, n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = id
	}
	return out
}

func only(items []gamestate.SlotItem, id int32) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for _, it := range items {
		if it.ID == id {
			out = append(out, it)
		}
	}
	return out
}

// THE POINT: 28 caskets open in six ticks, five a tick, each tick's five in one message; the box
// each casket opens is closed by the next click, and the last one by Open itself.
func TestOpenCasketsFiveATick(t *testing.T) {
	e := newEngine(of(casket, 28)...)
	r := Open(e, e.delay, pack, only(e.held(), casket))
	if r.Reason != "done" || r.Clicked != 28 || r.Left != 0 || r.Messages != 6 || e.opened != 28 {
		t.Fatalf("%+v, %d opened", r, e.opened)
	}
	if e.count(casket) != 0 || e.count(sapphire) != 14 {
		t.Fatalf("%d caskets, %d sapphires left", e.count(casket), e.count(sapphire))
	}
	busy := 0
	for tick, n := range e.perTick {
		if n > PerTick {
			t.Fatalf("tick %d opened %d", tick, n)
		}
		busy++
	}
	if busy != 6 {
		t.Fatalf("%d ticks opened something, want 6", busy)
	}
	e.tick() // the close goes out
	if e.box {
		t.Fatal("the last reward box was left open")
	}
}

// Oysters take the lowest shell, so the slots clicked lowest first are the ones that open.
func TestOpenOystersAndCasketsMixed(t *testing.T) {
	items := append(of(oyster, 10), of(casket, 8)...)
	items = append(items, of(oyster, 10)...)
	e := newEngine(items...)
	targets := append(only(e.held(), oyster), only(e.held(), casket)...)
	r := Open(e, e.delay, pack, targets)
	if r.Reason != "done" || r.Left != 0 || e.opened != 28 || e.count(oyster) != 0 || e.count(casket) != 0 {
		t.Fatalf("%+v, %d opened, %d oysters %d caskets left", r, e.opened, e.count(oyster), e.count(casket))
	}
	if e.count(pearl)+e.count(emptyOyster) != 20 {
		t.Fatalf("%d pearls, %d empty", e.count(pearl), e.count(emptyOyster))
	}
}

// A stray click spends part of a tick's five: the clicks behind it wait a tick in the buffer, so
// everything still opens, a tick later. Nothing to open is "none".
func TestOpenAStrayClickOnlyDelays(t *testing.T) {
	e := newEngine(of(casket, 12)...)
	e.strayAtTick = map[int]int{1: 3}
	r := Open(e, e.delay, pack, only(e.held(), casket))
	if r.Reason != "done" || r.Left != 0 || e.opened != 12 {
		t.Fatalf("%+v", r)
	}
	if r := Open(e, e.delay, pack, nil); r.Reason != "none" {
		t.Fatalf("%+v", r)
	}
}

// Clicks the engine refuses (the items are not where they were clicked) are left for the caller:
// "partial", with how many are still held.
func TestOpenReportsWhatIsLeft(t *testing.T) {
	e := newEngine(of(casket, 10)...)
	e.refuse = map[int]bool{5: true, 6: true, 7: true, 8: true, 9: true}
	r := Open(e, e.delay, pack, only(e.held(), casket))
	if r.Reason != "partial" || e.opened != 5 || e.count(casket) != 5 {
		t.Fatalf("%+v, %d opened", r, e.opened)
	}
}

func TestOpenStops(t *testing.T) {
	e := newEngine(of(casket, 28)...)
	r := Open(e, func(time.Duration) bool { return false }, pack, only(e.held(), casket))
	if r.Reason != "stopped" || r.Messages != lookahead {
		t.Fatalf("%+v", r)
	}
}

// opSteps builds a burst over the engine's pack: every slot holding id gets the held op, lowest slot
// first.
func (e *engine) opSteps(id int32, op int) []Step {
	var out []Step
	for slot, sl := range e.pack {
		if sl.id == id {
			out = append(out, Step{ID: id, Slot: slot, Op: op})
		}
	}
	return out
}

func (e *engine) slotOf(id int32) int {
	for slot, sl := range e.pack {
		if sl.id == id {
			return slot
		}
	}
	return -1
}

// THE POINT OF RUN: opens, drops, a join and a cut are all user events of the same five-a-tick budget,
// so a burst of ten of them is two messages over two ticks, and no tick reads more than five.
func TestRunMixesOpensDropsJoinsAndCutsInTheFiveATickBudget(t *testing.T) {
	items := []int32{casket, junk, chisel, junk, oyster, halfA, junk, pearl, casket, junk, halfB, junk, junk}
	e := newEngine(items...)
	var steps []Step
	steps = append(steps, e.opSteps(casket, 1)...)
	steps = append(steps, e.opSteps(junk, 5)...)
	steps = append(steps, Step{ID: halfB, Slot: e.slotOf(halfB), Use: true, UseID: halfA, UseSlot: e.slotOf(halfA)})
	steps = append(steps, Step{ID: pearl, Slot: e.slotOf(pearl), Use: true, UseID: chisel, UseSlot: e.slotOf(chisel)})
	if len(steps) != 10 {
		t.Fatalf("test setup: %d steps, want 2 opens + 6 drops + a join + a cut", len(steps))
	}
	r := Run(e, e.delay, pack, steps, Options{CloseBox: true})
	if r.Reason != "done" || r.Clicked != 10 || r.Left != 0 || r.Messages != 2 {
		t.Fatalf("%+v", r)
	}
	if e.opened != 2 || e.dropped != 6 || e.joined != 1 || e.cut != 1 {
		t.Fatalf("engine: %d opened, %d dropped, %d joined, %d cut", e.opened, e.dropped, e.joined, e.cut)
	}
	if e.count(junk) != 0 || e.count(halfA) != 0 || e.count(halfB) != 0 || e.count(crystalKey) != 1 || e.count(pearl) != 0 {
		t.Fatalf("pack: %d junk, %d keys, %d pearls left", e.count(junk), e.count(crystalKey), e.count(pearl))
	}
	if e.count(tips) != 1 {
		t.Fatalf("%d slots of Pearl bolttips, want one stack", e.count(tips))
	}
	for tick, n := range e.perTick {
		if n > PerTick {
			t.Fatalf("tick %d handled %d events", tick, n)
		}
	}
	e.tick()
	if e.box {
		t.Fatal("the last casket's reward box was left open")
	}
}

// A caller that shares the tick lowers the budget: the burst then goes one message of that size a tick,
// so no tick reads more than the budget (the default lets two messages queue ahead of the server).
func TestRunPerTickBudgetIsNeverExceeded(t *testing.T) {
	e := newEngine(of(junk, 12)...)
	r := Run(e, e.delay, pack, e.opSteps(junk, 5), Options{PerTick: 2})
	if r.Reason != "done" || r.Left != 0 || e.dropped != 12 || r.Messages != 6 {
		t.Fatalf("%+v, %d dropped", r, e.dropped)
	}
	busy := 0
	for tick, n := range e.perTick {
		if n > 2 {
			t.Fatalf("tick %d handled %d events with a budget of 2", tick, n)
		}
		busy++
	}
	if busy != 6 {
		t.Fatalf("%d busy ticks, want 6 (twelve events at two a tick)", busy)
	}
	if e.mostQueued > 2 {
		t.Fatalf("%d events were queued ahead of a tick with a budget of 2", e.mostQueued)
	}
	// A budget above the engine's five is the engine's five.
	e2 := newEngine(of(junk, 12)...)
	if r := Run(e2, e2.delay, pack, e2.opSteps(junk, 5), Options{PerTick: 9}); r.Messages != 3 {
		t.Fatalf("a budget of 9 sent %d messages, want 3 (five a message)", r.Messages)
	}
}

// A use the engine refuses leaves its target in the slot: Left says how many, and nothing is
// reported as done.
func TestRunReportsAJoinOrCutTheEngineRefused(t *testing.T) {
	e := newEngine(chisel, pearl, halfA, halfB)
	e.refuse = map[int]bool{1: true}
	steps := []Step{
		{ID: pearl, Slot: 1, Use: true, UseID: chisel, UseSlot: 0},
		{ID: halfB, Slot: 3, Use: true, UseID: halfA, UseSlot: 2},
	}
	r := Run(e, e.delay, pack, steps, Options{})
	if r.Reason != "partial" || r.Left != 1 || e.joined != 1 || e.cut != 0 {
		t.Fatalf("%+v, %d joined, %d cut", r, e.joined, e.cut)
	}
}

// Run keeps the caller's order, which is the order the engine reads, while Open and OpHeld sort their
// items lowest slot first: an oyster's inv_del takes the lowest slot holding one, so the clicks that
// name the lowest slots are the ones that find their oyster where they said. Out of order, the second
// click names a slot that already holds a reward and the engine refuses it.
func TestRunKeepsTheCallersOrderAndOpenSortsBySlot(t *testing.T) {
	e := newEngine(junk, junk, junk, oyster)
	steps := []Step{{ID: junk, Slot: 2, Op: 5}, {ID: junk, Slot: 0, Op: 5}, {ID: oyster, Slot: 3, Op: 1}, {ID: junk, Slot: 1, Op: 5}}
	if r := Run(e, e.delay, pack, steps, Options{}); r.Reason != "done" {
		t.Fatalf("%+v", r)
	}
	if want := []int{2, 0, 3, 1}; !equalInts(e.sentSlots, want) {
		t.Fatalf("Run sent slots %v, want the caller's order %v", e.sentSlots, want)
	}

	// Oysters named highest slot first: the first click's inv_del takes the LOWEST shell, so the
	// click that follows finds a reward in the slot it named.
	e = newEngine(oyster, oyster)
	out := []Step{{ID: oyster, Slot: 1, Op: 1}, {ID: oyster, Slot: 0, Op: 1}}
	if r := Run(e, e.delay, pack, out, Options{}); r.Reason != "partial" || r.Left != 1 {
		t.Fatalf("an out-of-order pair of oysters: %+v, want one left", r)
	}

	e = newEngine(oyster, junk, oyster, oyster)
	held := e.held()
	rev := []gamestate.SlotItem{held[3], held[2], held[0]}
	if r := Open(e, e.delay, pack, rev); r.Reason != "done" || e.count(oyster) != 0 {
		t.Fatalf("%+v, %d oysters left", r, e.count(oyster))
	}
	if want := []int{0, 2, 3}; !equalInts(e.sentSlots, want) {
		t.Fatalf("Open sent slots %v, want ascending %v", e.sentSlots, want)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
