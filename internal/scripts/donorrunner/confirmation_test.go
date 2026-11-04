package donorrunner

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/donorplan"
	"testing"
	"time"
)

type confirmSession struct {
	state  *gamestate.State
	onSend func([]byte)
}

func (s *confirmSession) State() *gamestate.State     { return s.state }
func (s *confirmSession) Cheats() *anticheat.Counters { return &anticheat.Counters{} }
func (s *confirmSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(32)
	build(p)
	if s.onSend != nil {
		s.onSend(p.Data[:p.Pos])
	}
	return nil
}
func (s *confirmSession) feed(op int, build func(*packet.Packet)) {
	p := packet.NewWriter(64)
	build(p)
	s.state.Handle(op, p.Data[:p.Pos])
}
func (s *confirmSession) inv(id int, count byte) {
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(uint16(id))
		if count == 0 {
			p.P2(0)
		} else {
			p.P2(1)
			p.P2(996)
			p.P1(count)
		}
	})
}
func TestProductionDonorUsesConfirmationInventoryWhenOfferIsCleared(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	chat := chatlog.New()
	s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.TradeConfirmScreen) })
	s.feed(serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: mule") })
	s.inv(actions.TradeMyOffer, 0)
	left, clicks, waits := 1, 0, 0
	s.onSend = func(b []byte) {
		if len(b) >= 3 && b[0] == clientprot.IF_BUTTON {
			com := int(b[1])<<8 | int(b[2])
			if com != actions.TradeAcceptConfirm {
				t.Fatalf("unexpected button %d", com)
			}
			clicks++
			left = 0
			chat.Record("Accepted trade.")
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	d := &Deps{Session: s, Chat: chat, Delay: func(time.Duration) bool { waits++; s.inv(actions.TradeConfirmMineSmall, 100); return true }}
	o := Options{Mule: "mule", Load: func() []donorplan.LoadGroup { return []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}} }, HaulLeft: func() int { return left }}
	h := donorplan.Begin(time.Now())
	h.Step = donorplan.StepTrade
	h.Since = time.Now()
	closed, traded, slots, stopped, reason := driveTrade(d, &h, actions.NewTrade(s), o, map[int]bool{})
	if !closed || !traded || slots != 1 || stopped || clicks != 1 || waits == 0 {
		t.Fatalf("closed=%v traded=%v slots=%d stopped=%v clicks=%d waits=%d", closed, traded, slots, stopped, clicks, waits)
	}
	if reason != "" {
		t.Fatalf("a successful trade must carry no failure reason, got %q", reason)
	}
}
func TestAcceptedTradeProofRejectsStaleAndPublicMessages(t *testing.T) {
	l := chatlog.New()
	l.Record("Accepted trade.")
	cursor := l.Total()
	l.Record("Player: Accepted trade.")
	l.Record("Other player declined trade.")
	if serverAcceptedTrade(l, cursor) {
		t.Fatal("cancel/public/stale message counted")
	}
	l.Record("Accepted trade.")
	if !serverAcceptedTrade(l, cursor) {
		t.Fatal("fresh system receipt ignored")
	}
	if serverAcceptedTrade(nil, 0) {
		t.Fatal("missing proof counted")
	}
}
