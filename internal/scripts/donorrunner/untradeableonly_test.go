package donorrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// The live spare104 loop, 2026-09-26 06:40-06:43: after one good trade the pack held three runes
// (Cosmic 564, Nature 561, Law 563) that the previous round had learned "never reached the trade
// screen", so they were in KnownUntradable. The pre-meet guard read the haul NET of an empty skip
// set, saw 3 slots, planned the 3 groups, and set off: the mule answered "on its way here by walk",
// the donor stripped every group as skipped, declined a trade with nothing on it, and the caller
// tried again 5 s later -- more than ten times, pulling the fleet's only mule off its post.
//
// A load made ENTIRELY of groups already known not to cross is not a hand-off. Run must refuse it
// before it walks, sends a packet or says a word on the bus.
func untradeableRunesPack() *confirmSession {
	ids := []int{564, 561, 563}
	s := &confirmSession{state: gamestate.New()}
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(uint16(PlayerPack))
		p.P2(uint16(len(ids)))
		for _, id := range ids {
			p.P2(uint16(id + 1)) // the wire carries id+1
			p.P1(50)
		}
	})
	return s
}

func untradeableRunesOptions(known map[int]bool) Options {
	groups := []donorplan.LoadGroup{
		{Name: "Cosmic rune", ID: 564, Slots: 1},
		{Name: "Nature rune", ID: 561, Slots: 1},
		{Name: "Law rune", ID: 563, Slots: 1},
	}
	return Options{
		Mule: "mule", Stand: tile.Tile{X: 2809, Z: 3441},
		RowsInBank: 3, Drawn: 3,
		Load:            func() []donorplan.LoadGroup { return groups },
		HaulLeft:        func() int { return 3 },
		KnownUntradable: known,
	}
}

func TestEmptyPlanReasonRefusesALoadThatIsAllKnownUntradeable(t *testing.T) {
	d := &Deps{Session: untradeableRunesPack()}
	skip := map[int]bool{564: true, 561: true, 563: true}
	why := emptyPlanReason(d, untradeableRunesOptions(skip), skip)
	if !strings.Contains(why, "untradeable") {
		t.Fatalf("emptyPlanReason = %q, want a refusal naming the untradeable load", why)
	}
	// CONTROL: the same pack with nothing learned yet is an ordinary, plannable load.
	if why := emptyPlanReason(d, untradeableRunesOptions(nil), nil); why != "" {
		t.Fatalf("with nothing known untradeable emptyPlanReason = %q, want empty", why)
	}
	// And one tradeable group left beside the skipped ones is still worth the meeting.
	o := untradeableRunesOptions(map[int]bool{564: true, 561: true})
	if why := emptyPlanReason(d, o, map[int]bool{564: true, 561: true}); why != "" {
		t.Fatalf("with Law rune still offerable emptyPlanReason = %q, want empty", why)
	}
}

func TestRunDoesNotSummonTheMuleForAnAllUntradeableLoad(t *testing.T) {
	s := untradeableRunesPack()
	sent := 0
	s.onSend = func([]byte) { sent++ }
	delays := 0
	bus := mulebus.NewBus()
	watch := mulebus.NewWatcher()
	d := &Deps{
		Session: s, Chat: chatlog.New(), Log: func(string) {}, Bus: bus, Me: "spare104",
		Delay: func(time.Duration) bool {
			delays++
			return delays < 50 // a stop request, so the old code returns instead of spinning
		},
	}
	known := map[int]bool{564: true, 561: true, 563: true}
	res := Run(d, untradeableRunesOptions(known))
	if !res.GaveUp || res.Stopped || res.Traded {
		t.Fatalf("Result = %+v, want GaveUp only (the caller banks the load and goes back to work)", res)
	}
	if !strings.Contains(res.Reason, "untradeable") {
		t.Errorf("Reason = %q, want it to say the load is untradeable", res.Reason)
	}
	if delays != 0 || sent != 0 {
		t.Errorf("the hand-off waited %d time(s) and sent %d packet(s) before refusing; want neither", delays, sent)
	}
	if msgs := watch.Poll(bus); len(msgs) != 0 {
		t.Errorf("the refused hand-off still put %d message(s) on the mule bus (%+v); it must not summon the mule", len(msgs), msgs)
	}
}
