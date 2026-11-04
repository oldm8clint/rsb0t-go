package mulerunner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// userhash is JString.toUserhash: a-z are 1..26, 0-9 are 27..36, and '_' (a space in a display
// name) is 0. gamestate decodes it back and shows it as the screen name, "hof_q2" -> "Hof Q2".
func userhash(name string) int64 {
	var hash int64
	for _, c := range strings.ToLower(name) {
		hash *= 37
		switch {
		case c >= 'a' && c <= 'z':
			hash += int64(c-'a') + 1
		case c >= '0' && c <= '9':
			hash += int64(c-'0') + 27
		}
	}
	return hash
}

// firstRequestTrade drives the real RunCycle through the path the reviewer found (2026-09-23,
// round 2 of handoff0923): the queued donor's own request never got through, so the mule asks
// it first; the donor answers and the screen opens. header is what the screen's "Trading With:"
// line says when it opens, and switchTo, if set, is the header a few polls later (a stale one
// from the previous trade being replaced). It returns how long after the screen opened the mule
// read its partner, the name it read, and the lines it logged.
func firstRequestTrade(t *testing.T, queueKey, fixtureName, header, switchTo string) (time.Duration, string, *lineLog) {
	t.Helper()
	s := &tradeSession{}
	muleAmong(s, map[string]int{"donora": 1, fixtureName: 1})
	if header != "" {
		tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: " + header) })
	}
	var (
		mu       sync.Mutex
		openedAt time.Time
		readAt   time.Time
		read     string
		polls    int
		stop     bool
	)
	s.onSend = func(b []byte) {
		if len(b) >= 3 && b[0] == clientprot.IF_BUTTON && (int(b[1])<<8|int(b[2])) == actions.TradeDecline {
			s.state.Handle(serverprot.IF_CLOSE, nil)
			return
		}
		mu.Lock()
		first := openedAt.IsZero()
		if first {
			openedAt = time.Now()
		}
		mu.Unlock()
		if first { // the mule's trade request: the donor answers at once
			tradeScreen(s, actions.TradeOfferScreen)
		}
	}
	logs := &lineLog{}
	start := time.Now()
	d := &Deps{Session: s, Chat: chatlog.New(), Log: func(line string) {
		logs.add(line)
		const open = "trade offer screen open with "
		if i := strings.Index(line, open); i >= 0 {
			mu.Lock()
			if readAt.IsZero() {
				readAt, read, stop = time.Now(), strings.Trim(line[i+len(open):], `"`), true
			}
			mu.Unlock()
		}
	}, Delay: func(dur time.Duration) bool {
		if dur > 10*time.Millisecond {
			dur = 10 * time.Millisecond
		}
		time.Sleep(dur)
		mu.Lock()
		defer mu.Unlock()
		if !openedAt.IsZero() && switchTo != "" {
			if polls++; polls == 3 {
				tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: " + switchTo) })
			}
		}
		return !stop && time.Since(start) < 8*time.Second
	}}
	d.Queue = func() []string { return []string{queueKey} }
	RunCycle(d, NewRequestWatcher(), AllowList([]string{"donora", fixtureName}), 8*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if openedAt.IsZero() {
		t.Fatalf("the mule never asked %q first: %v", queueKey, logs.lines)
	}
	if readAt.IsZero() {
		t.Fatalf("the mule never read a partner: %v", logs.lines)
	}
	return readAt.Sub(openedAt), read, logs
}

// TestAFirstRequestTradeReadsItsPartnerAtOnce is the reviewer's blocker, through the real
// RunCycle. The queue hands pick a muleKey ("donorb", "hof q2"); the trade screen shows the
// display name ("Donorb", "Hof Q2"). completeTrade compared the two with ==, so every trade the
// mule asked for itself waited the full three seconds and logged "timed out waiting for trade
// partner name" -- copy B's hof-queue lab logs show exactly one such line per first request, and
// none for a reciprocated one. On the old code this reads the partner after ~3s and logs the
// timeout, and the name the mule asked is the lower-case key.
func TestAFirstRequestTradeReadsItsPartnerAtOnce(t *testing.T) {
	for _, tc := range []struct{ queueKey, fixture, shown string }{
		{"donorb", "donorb", "Donorb"},
		{"hof q2", "hof_q2", "Hof Q2"}, // a login name with an underscore: muleKey folds it to a space
	} {
		t.Run(tc.shown, func(t *testing.T) {
			waited, read, logs := firstRequestTrade(t, tc.queueKey, tc.fixture, tc.shown, "")
			if read != tc.shown {
				t.Errorf("read partner %q, want %q", read, tc.shown)
			}
			if n := logs.count("timed out waiting for trade partner name"); n != 0 || waited > time.Second {
				t.Errorf("read the partner %s after the screen opened with %d partner-name timeout(s); want at once and none", waited.Round(10*time.Millisecond), n)
			}
			if logs.count("asking \""+tc.shown+"\" to trade: it is next in the queue") != 1 {
				t.Errorf("the first request did not name the player as the game shows it (%q): %v", tc.shown, logs.lines)
			}
		})
	}
}

// TestAFirstRequestTradeStillWaitsOutTheLastTradesHeader keeps what the partner-name wait is for
// (Trade.Partner reads a header nothing clears, so it names the PREVIOUS partner until this
// trade's own IF_SETTEXT lands) on the first-request path: with the header still saying
// "Donora" when the screen opens, the mule must not take the trade for Donora's, and must read
// Donorb as soon as the header changes rather than at the three-second timeout.
func TestAFirstRequestTradeStillWaitsOutTheLastTradesHeader(t *testing.T) {
	waited, read, logs := firstRequestTrade(t, "donorb", "donorb", "Donora", "Donorb")
	if read != "Donorb" {
		t.Fatalf("read partner %q from a stale header, want Donorb", read)
	}
	if n := logs.count("timed out waiting for trade partner name"); n != 0 || waited > time.Second {
		t.Errorf("read the partner %s after the screen opened with %d partner-name timeout(s); want it as soon as the header changed", waited.Round(10*time.Millisecond), n)
	}
}

// TestCompleteTradeMatchesThePartnerUnderAnySpelling pins the comparison itself: expected in
// queue-key, login or display form all match the screen's display name at once, and a different
// player's name never does.
func TestCompleteTradeMatchesThePartnerUnderAnySpelling(t *testing.T) {
	for _, tc := range []struct {
		expected string
		atOnce   bool
	}{{"Big Net", true}, {"big net", true}, {"big_net", true}, {" BIG_NET ", true}, {"", true}, {"big nets", false}, {"donor", false}} {
		s := readyTrade()
		tradePayload(s, serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: Big Net") })
		s.onSend = func(b []byte) {
			if len(b) >= 3 && b[0] == clientprot.IF_BUTTON && (int(b[1])<<8|int(b[2])) == actions.TradeDecline {
				s.state.Handle(serverprot.IF_CLOSE, nil)
			}
		}
		logs := &lineLog{}
		start := time.Now()
		var readAt time.Time
		d := &Deps{Session: s, Log: func(line string) {
			logs.add(line)
			if readAt.IsZero() && strings.Contains(line, "trade offer screen open with") {
				readAt = time.Now()
			}
		}, Delay: func(dur time.Duration) bool {
			if dur > 10*time.Millisecond {
				dur = 10 * time.Millisecond
			}
			time.Sleep(dur)
			return readAt.IsZero() && time.Since(start) < 5*time.Second
		}}
		completeTrade(d, actions.NewTrade(s), AllowList([]string{"big net"}), tc.expected)
		timedOut := logs.count("timed out waiting for trade partner name") > 0
		if timedOut == tc.atOnce {
			t.Errorf("expected %q against the screen's \"Big Net\": timed out=%v after %s, want timed out=%v", tc.expected, timedOut, readAt.Sub(start).Round(10*time.Millisecond), !tc.atOnce)
		}
	}
}

// TestResolvePlayerSlotFindsALoginNameWithAnUnderscore: a hand-out resolves the donor by the
// bus message's From, a LOGIN name, where the game shows the display name. It compared lower
// case only, so "hof_q2" never found "Hof Q2" and the hand-out was declined as not in sight.
// Latent on the live roster (its spaced names are configured with spaces), but the same fold
// every other name comparison in this package makes.
func TestResolvePlayerSlotFindsALoginNameWithAnUnderscore(t *testing.T) {
	s := &tradeSession{}
	muleAmong(s, map[string]int{"hof_q2": 1})
	d := &Deps{Session: s}
	for _, name := range []string{"hof_q2", "Hof Q2", "hof q2", " HOF_Q2 "} {
		if _, ok := ResolvePlayerSlot(d, name); !ok {
			t.Errorf("ResolvePlayerSlot(%q) did not find the player shown as \"Hof Q2\"", name)
		}
	}
	if _, ok := ResolvePlayerSlot(d, "hofq2"); ok {
		t.Errorf("ResolvePlayerSlot(\"hofq2\") matched \"Hof Q2\", a different account")
	}
}
