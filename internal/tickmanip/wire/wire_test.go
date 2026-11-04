package wire

import (
	"bytes"
	"errors"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/tickmanip"
)

// fake records every Send as the raw bytes it wrote (no ISAAC, so opcodes are plain).
type fake struct {
	state  *gamestate.State
	cheats *anticheat.Counters
	sent   [][]byte
}

func newFake() *fake { return &fake{state: gamestate.New(), cheats: &anticheat.Counters{}} }

func (f *fake) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(32)
	build(w)
	f.sent = append(f.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (f *fake) State() *gamestate.State     { return f.state }
func (f *fake) Cheats() *anticheat.Counters { return f.cheats }

const (
	knife  = 946
	logs   = 1511
	horsey = 2999
	pack   = 3214
)

// stock puts a knife in slot 0, logs in slot 1 and a horsey in slot 2.
func stock(f *fake) {
	f.state.Handle(serverprot.UPDATE_INV_FULL, func() []byte {
		w := packet.NewWriter(64)
		w.P2(uint16(pack))
		w.P2(3)
		for _, id := range []int{knife, logs, horsey} {
			w.P2(uint16(id + 1))
			w.P1(1)
		}
		return w.Data[:w.Pos]
	}())
}

func env(f *fake) *Env {
	return &Env{
		Batch: NewBatch(f), InvCom: pack, KnifeID: knife, LogID: logs, MakeComp: 8889, HorseyID: horsey,
		Click: func(s actions.Session) error { return actions.OpNpc(s, 1, 77) },
	}
}

func TestBatchIsOneWriteInPacketOrder(t *testing.T) {
	f := newFake()
	stock(f)
	e := env(f)
	if err := e.Write(append(tickmanip.Knife(), tickmanip.Click)); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("a batch must be ONE write, got %d", len(f.sent))
	}
	got := f.sent[0]
	// OPHELDU: opcode 200 + 12 bytes; IF_BUTTON: 86 + 2; OPNPC1: 252 + 2.
	if len(got) != 13+3+3 {
		t.Fatalf("batch is %d bytes, want 19: % x", len(got), got)
	}
	if got[0] != clientprot.OPHELDU || got[13] != clientprot.IF_BUTTON || got[16] != clientprot.OPNPC1 {
		t.Errorf("opcodes out of order: % x", got)
	}
	// OPHELDU names the LOG as the target and the KNIFE as the used item, both in the backpack component.
	want := []byte{
		clientprot.OPHELDU,
		byte(logs >> 8), byte(logs & 0xff), 0, 1, byte(pack >> 8), byte(pack & 0xff), // obj, slot 1, com
		byte(knife >> 8), byte(knife & 0xff), 0, 0, byte(pack >> 8), byte(pack & 0xff), // useObj, useSlot 0, useCom
	}
	if !bytes.Equal(got[:13], want) {
		t.Errorf("OPHELDU bytes % x, want % x", got[:13], want)
	}
	if got[14] != byte(8889>>8) || got[15] != byte(8889&0xff) {
		t.Errorf("IF_BUTTON component bytes % x, want 8889", got[14:16])
	}
	if got[17] != 0 || got[18] != 77 {
		t.Errorf("OPNPC1 npc index bytes % x, want 77", got[17:19])
	}
}

// TestBatchEqualsTheUnbatchedBytes: collecting builders must not change a single byte against sending
// the same actions one at a time.
func TestBatchEqualsTheUnbatchedBytes(t *testing.T) {
	f1, f2 := newFake(), newFake()
	stock(f1)
	stock(f2)
	e := env(f1)
	steps := []tickmanip.Step{tickmanip.MoveOwn, tickmanip.KnifeUse, tickmanip.MakeShafts, tickmanip.Click}
	// Give the own-tile move a position.
	for _, f := range []*fake{f1, f2} {
		f.state.Handle(serverprot.REBUILD_NORMAL, []byte{0x19, 0x7a, 0x19, 0x7a})
	}
	if _, _, _, ok := f1.state.Tile(); !ok {
		steps = steps[1:] // no position in this harness: drop the move, the rest still compare
	}
	if err := e.Write(steps); err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	e2 := env(f2)
	for _, st := range steps {
		if err := e2.Queue(st); err != nil {
			t.Fatal(err)
		}
		if err := e2.Batch.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range f2.sent {
		plain.Write(b)
	}
	if len(f2.sent) != len(steps) {
		t.Fatalf("unbatched wrote %d sends for %d steps", len(f2.sent), len(steps))
	}
	if !bytes.Equal(f1.sent[0], plain.Bytes()) {
		t.Errorf("batched % x != unbatched % x", f1.sent[0], plain.Bytes())
	}
}

func TestHorseyUsesOpHeld1(t *testing.T) {
	f := newFake()
	stock(f)
	if err := env(f).Write([]tickmanip.Step{tickmanip.Horsey}); err != nil {
		t.Fatal(err)
	}
	got := f.sent[0]
	if got[0] != clientprot.OPHELD1 || got[1] != byte(horsey>>8) || got[2] != byte(horsey&0xff) || got[4] != 2 {
		t.Errorf("horsey packet % x: want OPHELD1, obj %d, slot 2", got, horsey)
	}
}

func TestMissingItemWritesNothing(t *testing.T) {
	f := newFake()
	// An empty pack: the knife is not there.
	f.state.Handle(serverprot.UPDATE_INV_FULL, []byte{byte(pack >> 8), byte(pack & 0xff), 0, 0})
	e := env(f)
	err := e.Write(append(tickmanip.Knife(), tickmanip.Click))
	if !errors.Is(err, ErrNotInPack) {
		t.Fatalf("err = %v, want ErrNotInPack", err)
	}
	if len(f.sent) != 0 || e.Batch.Pending() != 0 {
		t.Errorf("a failed batch must write nothing and leave nothing queued: sent %d pending %d", len(f.sent), e.Batch.Pending())
	}
}

func TestEmptyBatchWritesNothing(t *testing.T) {
	f := newFake()
	e := env(f)
	if err := e.Write(nil); err != nil || len(f.sent) != 0 {
		t.Errorf("empty write: err %v sent %d", err, len(f.sent))
	}
}

func TestNoHorseyConfigured(t *testing.T) {
	f := newFake()
	stock(f)
	e := env(f)
	e.HorseyID = 0
	if err := e.Write([]tickmanip.Step{tickmanip.Horsey}); err == nil {
		t.Errorf("a horsey step with no horsey configured must fail loudly")
	}
}
