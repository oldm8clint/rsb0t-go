package mulerunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/orderplan"
)

const cake = 1891 // content/pack/obj.pack: 1891=cake

func stackOf(s *tradeSession, com int, id int32, n int) {
	tradePayload(s, serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(uint16(com))
		p.P2(uint16(n))
		for i := 0; i < n; i++ {
			p.P2(uint16(id + 1))
			p.P1(1)
		}
	})
}

// handoutTrade plays content's side of a hand-out that the DONOR's pack cannot hold: the screen
// opens on the mule's request, the offer registers, and when the mule accepts, trade.rs2's
// ~enough_trade_space fails, content prints its line to the giver and closes the screen.
func handoutTrade(chat *chatlog.Log, refusal string) *tradeSession {
	s := &tradeSession{state: gamestate.New()}
	stackOf(s, PlayerPack, cake, 10)
	opened := false
	s.onSend = func(b []byte) {
		if !opened {
			opened = true
			tradeScreen(s, actions.TradeOfferScreen)
			stackOf(s, actions.TradeOfferInv, cake, 10) // the pack, as the trade screen shows it
			tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) {
				p.P2(actions.TradePartner)
				p.PJStr("Trading With: Hofdon")
			})
			return
		}
		if len(b) >= 3 && b[0] == clientprot.IF_BUTTON && int(b[1])<<8|int(b[2]) == actions.TradeAcceptOffer {
			if refusal != "" {
				chat.Record(refusal)
			}
			s.state.Handle(serverprot.IF_CLOSE, nil)
			return
		}
		if len(actions.NewTrade(s).MyOffer()) == 0 {
			stackOf(s, actions.TradeMyOffer, cake, 10)
		}
	}
	return s
}

// The live failure's shape (hofdonor1/hofmule1, 2026-09-23): content refused the trade for the
// donor's pack space and the mule reported "the trade closed before the confirm screen". It must
// now say what content said. CONTROL: the second half is the old answer, which a close with no
// such line must still give.
func TestAHandoutRefusedForTheDonorsPackSpaceSaysSo(t *testing.T) {
	chat := chatlog.New()
	s := handoutTrade(chat, "Other player doesn't have enough inventory space for this trade.")
	d := &Deps{Session: s, Chat: chat, Log: func(string) {}, Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	res := deliver(d, "hofdon", 7, cake, 10)
	if res.Reason != orderplan.DeclineNoRoom || !res.Declined || res.Delivered {
		t.Fatalf("result %+v, want Declined with %q", res, orderplan.DeclineNoRoom)
	}

	chat = chatlog.New()
	s = handoutTrade(chat, "")
	d = &Deps{Session: s, Chat: chat, Log: func(string) {}, Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	if res := deliver(d, "hofdon", 7, cake, 10); res.Reason != "the trade closed before the confirm screen" {
		t.Fatalf("a close with no space refusal: result %+v, want the ordinary close reason", res)
	}
}
