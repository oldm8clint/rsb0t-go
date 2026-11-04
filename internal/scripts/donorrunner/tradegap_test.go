package donorrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/donorplan"
)

// openOfferScreen puts a session on the trade offer screen with the mule as the partner
// and the whole planned load already sitting on our side of the offer.
func openOfferScreen(s *confirmSession) {
	s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.TradeOfferScreen) })
	s.feed(serverprot.IF_SETTEXT, func(p *packet.Packet) {
		p.P2(actions.TradePartner)
		p.PJStr("Trading With: mule")
	})
	s.inv(actions.TradeMyOffer, 100)
}

func tradeButton(b []byte) (int, bool) {
	if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
		return 0, false
	}
	return int(b[1])<<8 | int(b[2]), true
}

func coinLoad() []donorplan.LoadGroup {
	// 995 is what the wire's 996 decodes to (readObjID subtracts one), so this group
	// matches the stack confirmSession.inv writes onto the trade screen.
	return []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}}
}

func tradingHandover() donorplan.Handover {
	h := donorplan.Begin(time.Now())
	h.Step = donorplan.StepTrade
	h.Since = time.Now()
	return h
}

// The offer->confirm transition is two separate server packets: the offer interface is
// torn down (IF_CLOSE, which gamestate maps to MainModalID = NoModal) and the confirm
// interface is opened (IF_OPENMAIN 3443) as much as a game tick later. actions.Trade's
// Active() is a single snapshot of that one field, so a poll landing in the gap reads
// false on a trade that is perfectly alive — and driveTrade used to believe it, abandon a
// trade it had already accepted, and let the calling script walk away from the bank.
//
// This is the donor half of the two-sided handshake desync that held live hand-over
// success at roughly 1-in-9. the mule (which got the same debounce in cbea997) rode the gap
// out, reached the confirm screen and accepted alone, then timed out waiting for items
// that the departed donor was never going to send. See stillClosed's own doc comment for
// the correlated run/fleet.log evidence from both sides of the same trade.
func TestDonorRidesOutTheOfferToConfirmInterfaceGap(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	chat := chatlog.New()
	openOfferScreen(s)

	haul := 1
	inGap, gapPolls := false, 0
	offerAccepts, confirmAccepts := 0, 0
	s.onSend = func(b []byte) {
		com, ok := tradeButton(b)
		if !ok {
			return
		}
		switch com {
		case actions.TradeAcceptOffer:
			offerAccepts++
			// The mule has accepted too (live: the mule's "offer changed — accepting
			// the new settled offer"), so the server tears the offer interface
			// down. The confirm interface does NOT arrive in the same breath.
			s.state.Handle(serverprot.IF_CLOSE, nil)
			inGap = true
		case actions.TradeAcceptConfirm:
			confirmAccepts++
			chat.Record("Accepted trade.")
			haul = 0
			s.state.Handle(serverprot.IF_CLOSE, nil)
		default:
			t.Errorf("unexpected trade button %d", com)
		}
	}

	var logs []string
	d := &Deps{Session: s, Chat: chat, Log: func(m string) { logs = append(logs, m) },
		Delay: func(time.Duration) bool {
			if !inGap {
				return true
			}
			gapPolls++
			if gapPolls == 4 { // roughly one game tick after the offer screen went away
				inGap = false
				s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.TradeConfirmScreen) })
				s.inv(actions.TradeConfirmMineSmall, 100)
			}
			return true
		}}
	o := Options{Mule: "mule", Load: coinLoad, HaulLeft: func() int { return haul }}

	h := tradingHandover()
	closed, traded, slots, stopped, reason := driveTrade(d, &h, actions.NewTrade(s), o, map[int]bool{})
	if reason != "" {
		t.Fatalf("a successful trade must carry no failure reason, got %q", reason)
	}
	if !closed || !traded || slots != 1 || stopped {
		t.Fatalf("closed=%v traded=%v slots=%d stopped=%v — the donor abandoned a live trade in the interface gap", closed, traded, slots, stopped)
	}
	if offerAccepts != 1 || confirmAccepts != 1 {
		t.Fatalf("offerAccepts=%d confirmAccepts=%d, want 1 and 1", offerAccepts, confirmAccepts)
	}
	if !containsSubstring(logs, "briefly went inactive") {
		t.Fatalf("the flicker was never logged: %v", logs)
	}
	if !containsSubstring(logs, "reached the confirm screen") {
		t.Fatalf("the donor never reached the confirm screen: %v", logs)
	}
}

// A genuine close is still a close: once the debounce window really does elapse with the
// interface gone, the donor must stop waiting and report the trade closed.
func TestDonorStillBelievesAGenuineClose(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	chat := chatlog.New()
	openOfferScreen(s)

	closedByMule := false
	s.onSend = func(b []byte) {
		if com, ok := tradeButton(b); ok && com == actions.TradeAcceptOffer {
			s.state.Handle(serverprot.IF_CLOSE, nil) // the mule declined; nothing crosses
			closedByMule = true
		}
	}
	d := &Deps{Session: s, Chat: chat, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	o := Options{Mule: "mule", Load: coinLoad, HaulLeft: func() int { return 1 }}

	h := tradingHandover()
	closed, traded, slots, stopped, reason := driveTrade(d, &h, actions.NewTrade(s), o, map[int]bool{})
	// The whole point of the reason: the caller's log line used to read "()" here.
	if reason == "" {
		t.Fatal("a trade that closed with nothing crossing reported no reason")
	}
	if !closedByMule {
		t.Fatal("the test never got as far as the mule closing the trade")
	}
	if !closed || traded || slots != 0 || stopped {
		t.Fatalf("closed=%v traded=%v slots=%d stopped=%v, want a plain closed-nothing-crossed", closed, traded, slots, stopped)
	}
}

// When nothing crossed, the entire offered load is still inside the trade box at the
// instant the screen closes, so HaulLeft reads far too low — usually zero. Handover.Traded
// reads that zero as "nothing left for the mule" and moves to StepLoad, which makes Run
// return and the calling script walk away from the bank with its whole load intact.
//
// Live evidence (run/fleet.log, donor "acct"): "closed without anything crossing" at
// 19:53:38 followed, the same second, by "fisher: hand-off ended without trading ()" and a
// walk back to the fishing spot with all 13 stacks still to hand over. The donor must wait
// for the returned inventory and then meet the mule again.
func TestDonorMeetsTheMuleAgainOnceTheFailedLoadComesBack(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	chat := chatlog.New()
	openOfferScreen(s)

	s.onSend = func(b []byte) {
		if com, ok := tradeButton(b); ok && com == actions.TradeAcceptOffer {
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	// The engine hands the offered stack back a couple of reads after the screen shuts:
	// read 1 is driveTrade's own haulBefore, reads 2 and 3 still see the trade box
	// holding it, and only then does the returned inventory land.
	packReads := 0
	haulLeft := func() int {
		packReads++
		if packReads == 1 || packReads > 3 {
			return 1
		}
		return 0
	}
	d := &Deps{Session: s, Chat: chat, Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	o := Options{Mule: "mule", Load: coinLoad, HaulLeft: haulLeft}

	h := tradingHandover()
	closed, traded, _, stopped, reason := driveTrade(d, &h, actions.NewTrade(s), o, map[int]bool{})
	if reason == "" {
		t.Fatal("a trade that closed with nothing crossing reported no reason")
	}
	if !closed || traded || stopped {
		t.Fatalf("closed=%v traded=%v stopped=%v", closed, traded, stopped)
	}
	if h.Step != donorplan.StepMeet {
		t.Fatalf("hand-over went to %q after a failed trade, want %q — the donor walks away from the bank still holding the load",
			h.Step, donorplan.StepMeet)
	}
}

// A stop request during the debounce must surface as stopped, never as a closed trade.
func TestDonorDebounceHonoursAStopRequest(t *testing.T) {
	s := &confirmSession{state: gamestate.New()}
	openOfferScreen(s)
	s.state.Handle(serverprot.IF_CLOSE, nil)

	d := &Deps{Session: s, Chat: chatlog.New(), Log: func(string) {}, Delay: func(time.Duration) bool { return false }}
	o := Options{Mule: "mule", Load: coinLoad, HaulLeft: func() int { return 1 }}

	h := tradingHandover()
	closed, traded, _, stopped, _ := driveTrade(d, &h, actions.NewTrade(s), o, map[int]bool{})
	if !stopped || closed || traded {
		t.Fatalf("closed=%v traded=%v stopped=%v, want stopped only", closed, traded, stopped)
	}
}

func containsSubstring(lines []string, want string) bool {
	for _, l := range lines {
		for i := 0; i+len(want) <= len(l); i++ {
			if l[i:i+len(want)] == want {
				return true
			}
		}
	}
	return false
}
