package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// opSession records the opcode of every packet sent.
type opSession struct {
	st     *gamestate.State
	cheats anticheat.Counters
	ops    []int
}

func (o *opSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(64)
	build(p)
	o.ops = append(o.ops, int(p.Data[0]))
	return nil
}
func (o *opSession) State() *gamestate.State     { return o.st }
func (o *opSession) Cheats() *anticheat.Counters { return &o.cheats }

// Before every Talk-to, Gundai's prelude sends a CLOSE_MODAL even though the client shows nothing
// open (its own close is optimistic; the engine may still hold one), then waits for the engine to
// act on it. Live 26 Sep: half of Gundai's Talk-tos opened nothing.
func TestGundaiTalkPreludeClosesAnyModalAndWaitsForTheEngine(t *testing.T) {
	s := &opSession{st: gamestate.New()}
	var waited time.Duration
	d := &Deps{Session: s, Delay: func(dur time.Duration) bool { waited += dur; return true }}
	gundaiTalkPrelude(d)
	if len(s.ops) != 1 || s.ops[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("sent opcodes %v, want exactly one CLOSE_MODAL (%d)", s.ops, clientprot.CLOSE_MODAL)
	}
	if waited < 2*600*time.Millisecond/3 { // two ticks, at the lab's 200 ms tick or longer
		t.Fatalf("waited %s after the close, want two ticks", waited)
	}
	if gundaiTalkAttempts < 5 {
		t.Fatalf("%d Talk-to attempts: a give-up wastes a whole Wilderness trip", gundaiTalkAttempts)
	}
}
