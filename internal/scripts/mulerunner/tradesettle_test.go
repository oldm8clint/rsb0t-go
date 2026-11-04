package mulerunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

type tradeSession struct {
	state  *gamestate.State
	sent   [][]byte
	onSend func([]byte)
}

func (s *tradeSession) State() *gamestate.State     { return s.state }
func (s *tradeSession) Cheats() *anticheat.Counters { return &anticheat.Counters{} }
func (s *tradeSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(32)
	build(p)
	b := append([]byte(nil), p.Data[:p.Pos]...)
	s.sent = append(s.sent, b)
	if s.onSend != nil {
		s.onSend(b)
	}
	return nil
}
func tradePayload(s *tradeSession, op int, build func(*packet.Packet)) {
	p := packet.NewWriter(128)
	build(p)
	s.state.Handle(op, p.Data[:p.Pos])
}
func tradeScreen(s *tradeSession, id int) {
	tradePayload(s, serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(uint16(id)) })
}
func tradeOffer(s *tradeSession, com int, count int32) {
	tradePayload(s, serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(uint16(com))
		if count == 0 {
			p.P2(0)
			return
		}
		p.P2(1)
		p.P2(996)
		p.P1(byte(count))
	})
}
func readyTrade() *tradeSession {
	s := &tradeSession{state: gamestate.New()}
	tradeScreen(s, actions.TradeOfferScreen)
	return s
}

func TestStableOfferRequiresNonemptyAndSuccessfulWait(t *testing.T) {
	for _, tc := range []struct {
		name   string
		count  int32
		cancel bool
	}{{"empty", 0, false}, {"changing timeout", 100, false}, {"cancelled", 100, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := readyTrade()
			tradeOffer(s, actions.TradeTheirOffer, tc.count)
			d := &Deps{Session: s, Delay: func(time.Duration) bool {
				if tc.cancel {
					return false
				}
				time.Sleep(time.Millisecond)
				return true
			}}
			if got := WaitForStableOffer(d, actions.NewTrade(s), 5*time.Millisecond, 50*time.Millisecond); len(got) != 0 {
				t.Fatalf("unsettled offer returned: %v", got)
			}
		})
	}
}
func TestStableOfferRejectsScreenClosure(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	d := &Deps{Session: s, Delay: func(time.Duration) bool { s.state.Handle(serverprot.IF_CLOSE, nil); return true }}
	if got := WaitForStableOffer(d, actions.NewTrade(s), time.Second, 0); len(got) != 0 {
		t.Fatalf("closed offer returned: %v", got)
	}
}

// Live-found 2026-09-18 (real production evidence, independent of the confirm-wait
// loop's own flicker case — see COLLAB.md's mule trade investigation entry): the mule
// logged "trade offer screen open with X" immediately followed, same tick, by "no
// nonempty stable offer arrived — declining", with no random event on the mule's own side
// anywhere nearby — i.e. this early-exit fired on the very first poll, not after any
// real wait, and the very next trade request from the SAME partner hit it again
// instantly. Proves a single false OnOfferScreen() reading right after the screen was
// confirmed open no longer discards the trade outright.
func TestStableOfferSurvivesTransientScreenFlicker(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	flickered := false
	d := &Deps{Session: s, Delay: func(d time.Duration) bool {
		if !flickered {
			flickered = true
			s.state.Handle(serverprot.IF_CLOSE, nil)
			return true
		}
		if d == 100*time.Millisecond && !actions.NewTrade(s).OnOfferScreen() {
			tradeScreen(s, actions.TradeOfferScreen)
			tradeOffer(s, actions.TradeTheirOffer, 100)
		}
		return true
	}}
	got := WaitForStableOffer(d, actions.NewTrade(s), 2*time.Second, 50*time.Millisecond)
	if len(got) != 1 || got[0].Count != 100 {
		t.Fatalf("a transient screen flicker discarded a real offer: %v", got)
	}
}
func TestConfirmationMismatchIsDeclined(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: donor") })
	confirmClicks := 0
	s.onSend = func(b []byte) {
		if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
			return
		}
		com := int(b[1])<<8 | int(b[2])
		switch com {
		case actions.TradeAcceptOffer:
			tradeScreen(s, actions.TradeConfirmScreen)
			tradeOffer(s, actions.TradeConfirmTheirsSmall, 50)
		case actions.TradeAcceptConfirm:
			confirmClicks++
		case actions.TradeDecline:
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	d := &Deps{Session: s, Delay: func(d time.Duration) bool { time.Sleep(d); return true }}
	res := completeTrade(d, actions.NewTrade(s), AllowList([]string{"donor"}), "donor")
	if res.Accepted || confirmClicks != 0 || actions.NewTrade(s).Active() {
		t.Fatalf("mismatched confirmation accepted: %+v confirm=%d", res, confirmClicks)
	}
}

// Live-found 2026-09-18 (real production evidence, see COLLAB.md's mule trade
// investigation entry): dozens of "not completed (declined, or interrupted before it
// closed)" failures against many different donors, right after the fleet restart, with
// no matching "confirmation does not match"/"no nonempty stable offer" log and no random
// event on the mule's own side — but the very next RunCycle call immediately found the
// SAME partner's trade offer screen open again. That means completeTrade's confirm-wait
// loop was reading trade.Active() as false for one tick even though the trade was still
// genuinely open, and (since Decline() is itself a no-op when !Active()) silently walking
// away from an otherwise-good, already-settled trade. This proves the confirm-wait loop
// now debounces a single false Active() reading instead of declining on it immediately.
func TestConfirmWaitSurvivesTransientInactiveFlicker(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: donor") })
	declined := false
	confirmClicks := 0
	flickerRecovered := false
	s.onSend = func(b []byte) {
		if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
			return
		}
		com := int(b[1])<<8 | int(b[2])
		switch com {
		case actions.TradeAcceptOffer:
			// The accept click itself is immediately followed by the interface
			// reporting closed for one tick — not a real close, just a component
			// refresh — before the confirm screen actually opens.
			s.state.Handle(serverprot.IF_CLOSE, nil)
		case actions.TradeAcceptConfirm:
			confirmClicks++
			s.state.Handle(serverprot.IF_CLOSE, nil)
		case actions.TradeDecline:
			declined = true
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	debounceTicks := 0
	d := &Deps{Session: s, Delay: func(d time.Duration) bool {
		time.Sleep(time.Millisecond)
		if d == 100*time.Millisecond {
			// Inside the confirm-wait loop's own debounce re-check specifically
			// (distinct from its 250ms poll and WaitForStableOffer's own waits).
			debounceTicks++
			if !flickerRecovered && debounceTicks == 2 {
				flickerRecovered = true
				tradeScreen(s, actions.TradeConfirmScreen)
				tradeOffer(s, actions.TradeConfirmTheirsSmall, 100)
			}
		}
		return true
	}}
	res := completeTrade(d, actions.NewTrade(s), AllowList([]string{"donor"}), "donor")
	if declined {
		t.Fatalf("a transient interface flicker triggered a decline: %+v", res)
	}
	if !flickerRecovered {
		t.Fatalf("test setup never reached the debounce window")
	}
	if confirmClicks != 1 {
		t.Fatalf("confirm screen accept clicks = %d, want 1 (res=%+v)", confirmClicks, res)
	}
	if res.Partner != "donor" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// Live-found 2026-09-18: TradePartner's text component (3417) is absent from
// TestTradeCloseClearsOffersButKeepsBankAndPack's own cleared-on-close list just above —
// confirmed real, not assumed: it survives a closed trade with the previous partner's name
// still sitting there. A second trade opening for a genuinely different partner before that
// partner's own IF_SETTEXT lands must not be misread as the stale name — completeTrade should
// wait for the real one it was told to expect (RunCycle's watcher.awaitingPartner) rather than
// accepting the first non-empty (but possibly stale) read the old code trusted immediately.
func TestCompleteTradeWaitsForExpectedPartnerNotStaleText(t *testing.T) {
	s := readyTrade()
	// Simulate the leftover text from a PREVIOUS, already-closed trade with "donor" —
	// nothing clears component 3417 when a trade's interface closes (see
	// TestTradeCloseClearsOffersButKeepsBankAndPack above), so this is exactly what a real
	// second trade's very first poll would still see.
	tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: donor") })
	// The real, current trade is actually with "otherdonor" — its own IF_SETTEXT arrives a
	// couple of polls late, exactly the live race this fix closes.
	settTextSent := false
	s.onSend = func(b []byte) {
		if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
			return
		}
		com := int(b[1])<<8 | int(b[2])
		if com == actions.TradeDecline {
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	d := &Deps{Session: s, Delay: func(time.Duration) bool {
		time.Sleep(time.Millisecond)
		if !settTextSent {
			settTextSent = true
			tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: otherdonor") })
		}
		return true
	}}
	res := completeTrade(d, actions.NewTrade(s), AllowList([]string{"donor", "otherdonor"}), "otherdonor")
	if res.Partner != "otherdonor" {
		t.Fatalf("read stale partner %q instead of waiting for the real otherdonor trade", res.Partner)
	}
}

// The offer screen can arrive several polls after Request. A second donor must
// remain queued across RunCycle calls until that reservation expires or opens.
func TestDelayedTradeScreenReservesPartnerAcrossCycles(t *testing.T) {
	s := &tradeSession{state: gamestate.New()}
	fields := [][2]uint32{{1, 0}, {8, 0}, {11, 1}, {5, 1}, {5, 0}, {1, 0}, {1, 1}, {11, 2}, {5, 2}, {5, 0}, {1, 0}, {1, 1}, {11, 2047}}
	bits := make([]byte, 10)
	pos := 0
	for _, f := range fields {
		for n := int(f[0]) - 1; n >= 0; n-- {
			bits[pos/8] |= byte((f[1]>>uint(n))&1) << uint(7-pos%8)
			pos++
		}
	}
	bits = bits[:(pos+7)/8]
	for _, name := range []string{"donora", "donorb"} {
		p := packet.NewWriter(64)
		for i := 0; i < 19; i++ {
			p.P1(0)
		}
		for i := 0; i < 7; i++ {
			p.P2(0)
		}
		var hash int64
		for _, c := range name {
			hash = hash*37 + int64(c-'a'+1)
		}
		p.P8(hash)
		p.P1(3)
		p.P2(0)
		bits = append(bits, 1, byte(p.Pos))
		bits = append(bits, p.Data[:p.Pos]...)
	}
	s.state.Handle(serverprot.PLAYER_INFO, bits)
	chat := chatlog.New()
	w := NewRequestWatcher()
	chat.Record("Donora:tradereq:")
	chat.Record("Donorb:tradereq:")
	chat.Record("Donorb:tradereq:")
	d := &Deps{Session: s, Chat: chat, Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	allowed := AllowList([]string{"donora", "donorb"})
	RunCycle(d, w, allowed, 10*time.Millisecond)
	RunCycle(d, w, allowed, 10*time.Millisecond)
	if len(s.sent) != 1 {
		t.Fatalf("requests before first screen=%d want 1", len(s.sent))
	}
	if len(w.pending) != 1 {
		t.Fatalf("queued partners=%d want 1", len(w.pending))
	}
	w.awaitingUntil = time.Now().Add(-time.Second)
	RunCycle(d, w, allowed, 10*time.Millisecond)
	if len(s.sent) != 2 {
		t.Fatalf("expired reservation did not serve next partner: %d", len(s.sent))
	}
}

func TestIdenticalOfferRefreshRestartsSettlement(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	d := &Deps{Session: s, Delay: func(time.Duration) bool {
		time.Sleep(time.Millisecond)
		tradeOffer(s, actions.TradeTheirOffer, 100)
		return true
	}}
	if got := WaitForStableOffer(d, actions.NewTrade(s), 20*time.Millisecond, 4*time.Millisecond); len(got) != 0 {
		t.Fatalf("accepted continuously refreshed offer: %v", got)
	}
}

func TestTradeCloseClearsOffersButKeepsBankAndPack(t *testing.T) {
	s := readyTrade()
	for _, id := range []int{3415, 3416, 3542, 3538, 3532, 3539, 3214, 5382} {
		tradeOffer(s, id, 100)
	}
	tradeScreen(s, actions.TradeConfirmScreen)
	if len(s.state.Inventory(3416).Slots) == 0 {
		t.Fatal("offer-to-confirm cleared current offer")
	}
	s.state.Handle(serverprot.IF_CLOSE, nil)
	for _, id := range []int{3415, 3416, 3542, 3538, 3532, 3539} {
		if len(s.state.Inventory(id).Slots) != 0 {
			t.Fatalf("stale trade component %d", id)
		}
	}
	for _, id := range []int{3214, 5382} {
		if len(s.state.Inventory(id).Slots) != 1 {
			t.Fatalf("persistent inventory %d lost", id)
		}
	}
	// Fresh packets arrive before opening the next interface.
	tradeOffer(s, 3416, 100)
	tradeScreen(s, actions.TradeOfferScreen)
	if len(s.state.Inventory(3416).Slots) != 1 {
		t.Fatal("new offer erased on opening")
	}
	s.state.LocalCloseModal()
	if len(s.state.Inventory(3416).Slots) != 0 {
		t.Fatal("optimistic close retained old offer")
	}
}

func TestReceiptRequiresFreshSnapshotAndActualIncrease(t *testing.T) {
	before := map[int32]int64{995: 50}
	received := []gamestate.SlotItem{{Item: gamestate.Item{ID: 995, Count: 100}}}
	for _, tc := range []struct {
		name             string
		count            int32
		generation, full int
		want             bool
	}{
		{"stale existing items", 150, 4, 4, false},
		{"partial without new full", 150, 5, 4, false},
		{"cancelled trade", 50, 5, 5, false},
		{"short transfer", 149, 5, 5, false},
		{"actual full receipt", 150, 5, 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &gamestate.Inventory{Slots: []gamestate.Item{{ID: 995, Count: tc.count}}, Generation: tc.generation, FullGeneration: tc.full}
			if got := freshReceivedInventory(inv, 4, before, received); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// Live 2026-09-19: "changed but no new stable nonempty offer arrived before the confirm screen —
// declining" was the most common coded decline. The server re-sent an unchanged offer right after
// our accept (bumping its generation), the mule began re-settling, and the donor's accept then
// moved both sides to the confirm screen mid-wait. A real change would have cleared our accept, so
// that confirm screen is genuine and must be checked, not declined.
func TestConfirmScreenDuringResettleIsCheckedNotDeclined(t *testing.T) {
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: donor") })
	declined, accepted, confirmClicks, polls := false, false, 0, 0
	s.onSend = func(b []byte) {
		if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
			return
		}
		switch int(b[1])<<8 | int(b[2]) {
		case actions.TradeAcceptOffer:
			accepted = true
			tradeOffer(s, actions.TradeTheirOffer, 100) // identical re-send: generation moves, offer does not
		case actions.TradeAcceptConfirm:
			confirmClicks++
			s.state.Handle(serverprot.IF_CLOSE, nil)
		case actions.TradeDecline:
			declined = true
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	d := &Deps{Session: s, Delay: func(d time.Duration) bool {
		time.Sleep(time.Millisecond)
		if accepted && d == 250*time.Millisecond {
			if polls++; polls == 3 {
				tradeScreen(s, actions.TradeConfirmScreen)
				tradeOffer(s, actions.TradeConfirmTheirsSmall, 100)
			}
		}
		return true
	}}
	res := completeTrade(d, actions.NewTrade(s), AllowList([]string{"donor"}), "donor")
	if declined || confirmClicks != 1 {
		t.Fatalf("genuine confirm screen during re-settle: declined=%v confirmClicks=%d res=%+v", declined, confirmClicks, res)
	}
}

// confirmScreenAfterAccept runs completeTrade against a donor whose confirm screen shows
// confirm (built by the caller) after the mule accepts a settled offer of 100.
func confirmScreenAfterAccept(t *testing.T, confirm func(s *tradeSession)) (declined bool, confirmClicks int) {
	t.Helper()
	s := readyTrade()
	tradeOffer(s, actions.TradeTheirOffer, 100)
	tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: donor") })
	s.onSend = func(b []byte) {
		if len(b) < 3 || b[0] != clientprot.IF_BUTTON {
			return
		}
		switch int(b[1])<<8 | int(b[2]) {
		case actions.TradeAcceptOffer:
			tradeScreen(s, actions.TradeConfirmScreen)
			confirm(s)
		case actions.TradeAcceptConfirm:
			confirmClicks++
			s.state.Handle(serverprot.IF_CLOSE, nil)
		case actions.TradeDecline:
			declined = true
			s.state.Handle(serverprot.IF_CLOSE, nil)
		}
	}
	d := &Deps{Session: s, Delay: func(d time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	completeTrade(d, actions.NewTrade(s), AllowList([]string{"donor"}), "donor")
	return declined, confirmClicks
}

// Live 2026-09-19 on 3219bcf: donors still drawing from the bank added stacks after the mule's
// accept, so the confirm screen held more than was settled and the mule declined it (3 of 14
// failures). Receiving a superset while giving nothing is accepted.
func TestConfirmScreenHoldingMoreThanSettledIsAccepted(t *testing.T) {
	declined, clicks := confirmScreenAfterAccept(t, func(s *tradeSession) {
		tradeOffer(s, actions.TradeConfirmTheirsSmall, 150)
	})
	if declined || clicks != 1 {
		t.Fatalf("superset confirm: declined=%v confirmClicks=%d", declined, clicks)
	}
}

func TestConfirmScreenWithADifferentItemIsDeclined(t *testing.T) {
	declined, clicks := confirmScreenAfterAccept(t, func(s *tradeSession) {
		tradePayload(s, serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
			p.P2(uint16(actions.TradeConfirmTheirsSmall))
			p.P2(1)
			p.P2(1000) // a different item id
			p.P1(150)
		})
	})
	if !declined || clicks != 0 {
		t.Fatalf("swapped confirm: declined=%v confirmClicks=%d", declined, clicks)
	}
}
