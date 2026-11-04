package actions

import (
	"errors"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// fakeSession is a Session that records every packet a call under test writes, instead
// of putting it on a real socket — the exact bytes a live round trip would send, per
// COLLAB.md's suggested approach for this package. It carries a real *gamestate.State
// and *anticheat.Counters so State()-reading and anticheat-counting logic under test
// behaves identically to production.
type fakeSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
	sent   [][]byte

	sendErr error

	// afterSend, when set, runs after each successful Send — a test's way of standing
	// in for the server's own response to a click (e.g. removing a traded-away item),
	// without which a retry loop like Trade.RemoveAll would spin its full guard count
	// sleeping between every attempt.
	afterSend func(f *fakeSession)
}

func newFakeSession() *fakeSession {
	return &fakeSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (f *fakeSession) Send(build func(w *packet.Packet)) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	w := packet.NewWriter(32)
	build(w)
	f.sent = append(f.sent, append([]byte(nil), w.Data[:w.Pos]...))
	if f.afterSend != nil {
		f.afterSend(f)
	}
	return nil
}

func (f *fakeSession) State() *gamestate.State     { return f.state }
func (f *fakeSession) Cheats() *anticheat.Counters { return f.cheats }
func (f *fakeSession) sendCount() int              { return len(f.sent) }
func (f *fakeSession) lastSent() []byte {
	if len(f.sent) == 0 {
		return nil
	}
	return f.sent[len(f.sent)-1]
}

var errBoom = errors.New("fakeSession: send failed")

// payload builds a raw packet payload the same way internal/gamestate's own tests do
// (see internal/gamestate/loc_test.go's payload helper), for driving fakeSession.state
// via State.Handle with synthetic server packets.
func payload(build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(64)
	build(p)
	return p.Data[:p.Pos]
}

// openMainSide opens both the main and side modal, mirroring IF_OPENMAIN_SIDE — used to
// put Bank/Trade/Shop's fixed-id screens "on screen" for a test without a live server.
func openMainSide(s *gamestate.State, main, side int) {
	s.Handle(serverprot.IF_OPENMAIN_SIDE, payload(func(p *packet.Packet) {
		p.P2(uint16(main))
		p.P2(uint16(side))
	}))
}

// openMain opens only the main modal, mirroring IF_OPENMAIN.
func openMain(s *gamestate.State, main int) {
	s.Handle(serverprot.IF_OPENMAIN, payload(func(p *packet.Packet) { p.P2(uint16(main)) }))
}

// openChat opens the chat modal, mirroring IF_OPENCHAT.
func openChat(s *gamestate.State, chat int) {
	s.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(uint16(chat)) }))
}

// closeModal mirrors IF_CLOSE.
func closeModal(s *gamestate.State) {
	s.Handle(serverprot.IF_CLOSE, nil)
}

// setText mirrors IF_SETTEXT for the given component.
func setText(s *gamestate.State, comID int, text string) {
	s.Handle(serverprot.IF_SETTEXT, payload(func(p *packet.Packet) {
		p.P2(uint16(comID))
		p.PJStr(text)
	}))
}

// setFullInventory mirrors UPDATE_INV_FULL: each item is (objID, count); objID 0 means
// an empty slot the same way the wire protocol does.
func setFullInventory(s *gamestate.State, comID int, items ...[2]int32) {
	s.Handle(serverprot.UPDATE_INV_FULL, payload(func(p *packet.Packet) {
		p.P2(uint16(comID))
		p.P2(uint16(len(items)))
		for _, it := range items {
			id, count := it[0], it[1]
			if id != 0 {
				p.P2(uint16(id + 1))
			} else {
				p.P2(0)
			}
			if count >= 255 {
				p.P1(255)
				p.P4(count)
			} else {
				p.P1(byte(count))
			}
		}
	}))
}

// openCountDialog mirrors P_COUNTDIALOG.
func openCountDialog(s *gamestate.State) {
	s.Handle(serverprot.P_COUNTDIALOG, nil)
}
