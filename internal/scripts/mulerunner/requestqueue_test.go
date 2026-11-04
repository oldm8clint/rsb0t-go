package mulerunner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// playersInView is a PLAYER_INFO that adds each name at indices 1..n, the same encoding
// TestDelayedTradeScreenReservesPartnerAcrossCycles builds by hand for two players.
func playersInView(names ...string) []byte {
	var fields [][2]uint32
	fields = append(fields, [2]uint32{1, 0}, [2]uint32{8, 0})
	for i := range names {
		fields = append(fields, [2]uint32{11, uint32(i + 1)}, [2]uint32{5, 1}, [2]uint32{5, 0}, [2]uint32{1, 0}, [2]uint32{1, 1})
	}
	fields = append(fields, [2]uint32{11, 2047})
	bits := make([]byte, 64+len(names)*8)
	pos := 0
	for _, f := range fields {
		for n := int(f[0]) - 1; n >= 0; n-- {
			bits[pos/8] |= byte((f[1]>>uint(n))&1) << uint(7-pos%8)
			pos++
		}
	}
	bits = bits[:(pos+7)/8]
	for _, name := range names {
		p := packet.NewWriter(64)
		for i := 0; i < 19; i++ {
			p.P1(0)
		}
		for i := 0; i < 7; i++ {
			p.P2(0)
		}
		p.P8(userhash(name))
		p.P1(3)
		p.P2(0)
		bits = append(bits, 1, byte(p.Pos))
		bits = append(bits, p.Data[:p.Pos]...)
	}
	return bits
}

// inView replaces who the mule can see.
func inView(s *tradeSession, names ...string) {
	s.state = gamestate.New()
	s.state.Handle(serverprot.PLAYER_INFO, playersInView(names...))
}

type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *lineLog) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func queueDeps(s *tradeSession, chat *chatlog.Log, logs *lineLog) *Deps {
	return &Deps{Session: s, Chat: chat, Log: logs.add, Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
}

// TestStaleRequestsArePrunedDuringBusyCycles is the 00:50:32 flush (263cceda: 51 "never became
// visible" lines in one second, every one a donor that had left minutes before). Four queued
// donors walk away while the mule keeps serving fresh arrivals. The old RunCycle carried them
// through every busy cycle unchecked (queued stayed 4) and dropped them all in the first quiet
// tick; now the first busy cycle drops them, in one line.
func TestStaleRequestsArePrunedDuringBusyCycles(t *testing.T) {
	s := &tradeSession{}
	inView(s, "donora", "donorb", "donorc", "donord", "donore")
	chat, logs := chatlog.New(), &lineLog{}
	w := NewRequestWatcher()
	d := queueDeps(s, chat, logs)
	allowed := AllowList([]string{"donora", "donorb", "donorc", "donord", "donore", "fresha"})
	for _, n := range []string{"Donora", "Donorb", "Donorc", "Donord", "Donore"} {
		chat.Record(n + ":tradereq:")
	}
	RunCycle(d, w, allowed, 5*time.Millisecond)
	if got := len(w.pending); got != 4 {
		t.Fatalf("after the first cycle %d queued, want 4", got)
	}
	for i := range w.pending {
		w.pending[i].deadline = time.Now().Add(-time.Minute)
	}
	inView(s, "fresha") // the four have gone; only a newcomer is in view
	w.awaitingUntil = time.Time{}
	chat.Record("Fresha:tradereq:")
	RunCycle(d, w, allowed, 5*time.Millisecond)
	if got := len(w.pending); got != 0 {
		t.Errorf("after one busy cycle %d dead entries are still queued, want 0", got)
	}
	if n := logs.count("dropped 4 stale trade request(s)"); n != 1 {
		t.Errorf("want one line dropping all four, got %d; log %v", n, logs.lines)
	}
	if w.awaitingPartner != "Fresha" {
		t.Errorf("the fresh, visible requester was not served: awaiting %q", w.awaitingPartner)
	}
}

// TestAnExpiredRequestFromADonorStillInViewIsKept guards the other direction: a donor beside the
// mule whose window has lapsed is still somebody the mule can serve.
func TestAnExpiredRequestFromADonorStillInViewIsKept(t *testing.T) {
	s := &tradeSession{}
	inView(s, "donora")
	w := NewRequestWatcher()
	w.pending = []pendingRequest{{name: "Donora", deadline: time.Now().Add(-time.Minute)}}
	w.awaitingUntil = time.Now().Add(time.Minute) // busy: nothing is reciprocated this cycle
	w.awaitingPartner = "Someoneelse"
	RunCycle(queueDeps(s, chatlog.New(), &lineLog{}), w, AllowList([]string{"donora"}), 5*time.Millisecond)
	if len(w.pending) != 1 {
		t.Fatalf("a visible requester was pruned: %d queued", len(w.pending))
	}
}

// TestAReRequestRefreshesTheWindowAndKeepsThePlace: the old uniquePending kept the OLDEST entry,
// so a donor asking every three seconds still "expired" ten seconds after its first ask.
func TestAReRequestRefreshesTheWindowAndKeepsThePlace(t *testing.T) {
	w := NewRequestWatcher()
	t0 := time.Now()
	w.queue("Donora", t0)
	w.queue("Donorb", t0)
	w.queue("donora", t0.Add(8*time.Second))
	if len(w.pending) != 2 || w.pending[0].name != "Donora" {
		t.Fatalf("queue = %+v, want Donora still first and no duplicate", w.pending)
	}
	if want := t0.Add(8*time.Second + reciprocateRetryWindow); !w.pending[0].deadline.Equal(want) {
		t.Errorf("deadline = %v, want the later ask's %v", w.pending[0].deadline, want)
	}
	w.queue("Donora", t0) // an older ask never shortens it
	if want := t0.Add(8*time.Second + reciprocateRetryWindow); !w.pending[0].deadline.Equal(want) {
		t.Errorf("an older ask moved the deadline back to %v", w.pending[0].deadline)
	}
}

// TestForgetUsesTheSameNameRuleAsTheAllowList: a donor cancels over the bus under its login name
// ("big_net") and asked to trade under its display name ("Big net").
func TestForgetUsesTheSameNameRuleAsTheAllowList(t *testing.T) {
	w := NewRequestWatcher()
	w.queue("Big net", time.Now())
	w.queue("Donora", time.Now())
	if !w.Forget("big_net") {
		t.Fatal("Forget(big_net) did not find Big net")
	}
	if len(w.pending) != 1 || w.pending[0].name != "Donora" {
		t.Errorf("queue = %+v, want only Donora left", w.pending)
	}
	if w.Forget("nobody") {
		t.Error("Forget reported a request that was never queued")
	}
}

// TestTheHandoutPathPrunes: hand-outs chain with no RunCycle between them, and DropRequestsFrom
// used to append without ever checking a window.
func TestTheHandoutPathPrunes(t *testing.T) {
	s := &tradeSession{}
	inView(s, "donorb")
	chat, logs := chatlog.New(), &lineLog{}
	w := NewRequestWatcher()
	w.pending = []pendingRequest{{name: "Donora", deadline: time.Now().Add(-time.Second)}}
	chat.Record("Donorb:tradereq:")
	chat.Record("Handee:tradereq:")
	if dropped := w.DropRequestsFrom(queueDeps(s, chat, logs), "handee"); dropped != 1 {
		t.Fatalf("dropped %d of the hand-out partner's own lines, want 1", dropped)
	}
	if len(w.pending) != 1 || w.pending[0].name != "Donorb" {
		t.Errorf("queue = %+v, want only the live Donorb", w.pending)
	}
	if logs.count("dropped 1 stale trade request(s), no longer in view: Donora") != 1 {
		t.Errorf("the prune was not logged: %v", logs.lines)
	}
}

func TestQueuedRequestsCountsOnlyLiveEntries(t *testing.T) {
	w := NewRequestWatcher()
	w.pending = []pendingRequest{
		{name: "live", deadline: time.Now().Add(5 * time.Second)},
		{name: "lapsed", deadline: time.Now().Add(-time.Second)},
	}
	if got := w.QueuedRequests(); got != 1 {
		t.Errorf("QueuedRequests = %d, want 1: a lapsed ask is not a donor waiting", got)
	}
}

// TestTheEarliestAnnouncedVisibleDonorIsServedFirst. The chat log's order is whoever happened to
// re-request last, so it cannot be the queue order. Control: the old RunCycle reciprocated the
// first line it read, Donora.
func TestTheEarliestAnnouncedVisibleDonorIsServedFirst(t *testing.T) {
	s := &tradeSession{}
	inView(s, "donora", "donorb")
	chat := chatlog.New()
	w := NewRequestWatcher()
	d := queueDeps(s, chat, &lineLog{})
	// donorc announced first but is not in view, so it blocks nobody; donorb announced before
	// donora.
	queue := []string{"donorc", "donorb", "donora"}
	d.Queue = func() []string { return queue }
	for _, n := range []string{"Donora", "Donorc", "Donorb"} {
		chat.Record(n + ":tradereq:")
	}
	RunCycle(d, w, AllowList([]string{"donora", "donorb", "donorc"}), 5*time.Millisecond)
	if w.awaitingPartner != "Donorb" {
		t.Fatalf("reciprocated %q, want Donorb: the earliest announced donor in view", w.awaitingPartner)
	}
	// A requester that never announced ranks after every one that did, but is still served.
	queue = []string{"donorb"}
	w2 := NewRequestWatcher()
	chat.Record("Donora:tradereq:")
	chat.Record("Donorb:tradereq:")
	RunCycle(d, w2, AllowList([]string{"donora", "donorb"}), 5*time.Millisecond)
	if w2.awaitingPartner != "Donorb" {
		t.Errorf("reciprocated %q, want the announced Donorb ahead of the unannounced Donora", w2.awaitingPartner)
	}
}

// muleAmong puts the mule on a known tile (build origin 3152,3152, local 60,60 = 3212,3212) with
// the named players around it, each dx tiles east of it.
func muleAmong(s *tradeSession, players map[string]int) {
	s.state = gamestate.New()
	s.state.Handle(serverprot.REBUILD_NORMAL, func() []byte {
		p := packet.NewWriter(8)
		p.P2(400)
		p.P2(400)
		return p.Data[:p.Pos]
	}())
	names := make([]string, 0, len(players))
	for n := range players {
		names = append(names, n)
	}
	var fields [][2]uint32
	fields = append(fields, [2]uint32{1, 1}, [2]uint32{2, 3}, [2]uint32{2, 0}, [2]uint32{7, 60}, [2]uint32{7, 60}, [2]uint32{1, 0}, [2]uint32{1, 0})
	fields = append(fields, [2]uint32{8, 0})
	for i, n := range names {
		fields = append(fields, [2]uint32{11, uint32(i + 1)}, [2]uint32{5, uint32(players[n])}, [2]uint32{5, 0}, [2]uint32{1, 0}, [2]uint32{1, 1})
	}
	fields = append(fields, [2]uint32{11, 2047})
	bits := make([]byte, 64+len(names)*8)
	pos := 0
	for _, f := range fields {
		for n := int(f[0]) - 1; n >= 0; n-- {
			bits[pos/8] |= byte((f[1]>>uint(n))&1) << uint(7-pos%8)
			pos++
		}
	}
	bits = bits[:(pos+7)/8]
	for _, name := range names {
		p := packet.NewWriter(64)
		for i := 0; i < 19; i++ {
			p.P1(0)
		}
		for i := 0; i < 7; i++ {
			p.P2(0)
		}
		p.P8(userhash(name))
		p.P1(3)
		p.P2(0)
		bits = append(bits, 1, byte(p.Pos))
		bits = append(bits, p.Data[:p.Pos]...)
	}
	s.state.Handle(serverprot.PLAYER_INFO, bits)
}

// TestTheHeadOfTheQueueIsAskedEvenIfItsRequestNeverGotThrough is the lab's finding, pinned. The
// engine answers a trade request to a busy player with "is busy at the moment" and never delivers
// it, so a queued donor's own request only lands in the gaps between the mule's trades. The mule
// asks the head of the queue itself when it is standing beside it -- and never one out of reach,
// because a trade request walks the requester to its target.
func TestTheHeadOfTheQueueIsAskedEvenIfItsRequestNeverGotThrough(t *testing.T) {
	s := &tradeSession{}
	muleAmong(s, map[string]int{"donora": 1, "donorb": 1, "donorc": 10})
	if x, z, _, ok := s.state.Tile(); !ok || x != 3212 || z != 3212 {
		t.Fatalf("fixture: mule at %d,%d ok=%v", x, z, ok)
	}
	chat, logs := chatlog.New(), &lineLog{}
	d := queueDeps(s, chat, logs)
	d.Queue = func() []string { return []string{"donorc", "donorb", "donora"} }
	w := NewRequestWatcher()
	chat.Record("Donora:tradereq:") // only the LAST in the queue got a request through
	RunCycle(d, w, AllowList([]string{"donora", "donorb", "donorc"}), 5*time.Millisecond)
	// Donorb as the game shows it, not the queue key "donorb": completeTrade matches the screen's
	// name against this, and the log and the dashboard should name the player one way.
	if w.awaitingPartner != "Donorb" || !w.proactive {
		t.Fatalf("awaiting %q (asked first: %v), want Donorb: next in the queue and beside the mule; donorc is ten tiles off", w.awaitingPartner, w.proactive)
	}
	if logs.count("asking \"Donorb\" to trade: it is next in the queue") != 1 {
		t.Errorf("the first request was not logged as such: %v", logs.lines)
	}
	if len(s.sent) != 1 {
		t.Errorf("%d packets sent, want exactly one trade request", len(s.sent))
	}
	// Two first requests that no screen follows, and the mule waits for donorb's own request.
	for i := 0; i < maxProactiveMisses; i++ {
		w.awaitingUntil = time.Now().Add(-time.Second)
		RunCycle(d, w, AllowList([]string{"donora", "donorb", "donorc"}), 5*time.Millisecond)
	}
	if w.awaitingPartner != "Donora" || w.proactive {
		t.Errorf("after %d unanswered first requests awaiting %q (asked first: %v), want the requester Donora", maxProactiveMisses, w.awaitingPartner, w.proactive)
	}
}
