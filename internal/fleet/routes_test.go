package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/proxydial"
)

const poolSecret = "Pr0xy-S3cret-never-printed"

// clockSlack is the tolerance on the LOWER-bound timing assertions below — "this account really
// did wait out its hold".
//
// Only the lower bounds still measure time at all, because only they are measuring the thing
// under test. They are also the safe direction: a loaded machine makes a wait LONGER, never
// shorter, so scheduler jitter pushes these further from failing rather than nearer. The slack
// covers clock granularity alone, and it is what the literals it replaces already allowed
// (290ms against a 300ms spacing, 390ms against a 400ms quiet).
//
// The upper bounds — "these were admitted without waiting" — no longer use a stopwatch at all;
// see acquireQueued.
const clockSlack = 10 * time.Millisecond

func mustPool(t *testing.T, js string) *RoutePool {
	t.Helper()
	p, err := ParseRoutePool([]byte(js), func(k string) string {
		if k == "PROXY_PW" {
			return poolSecret
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// newTestPool builds a pool whose proxies point at closed loopback ports: admission never
// dials, so nothing here touches the network.
func newTestPool(t *testing.T, js string) (*routePool, func()) {
	t.Helper()
	direct := newLoginRateGate()
	p := newRoutePool(mustPool(t, js), direct, "ws://localhost/", func(string) {})
	p.retryPause = 10 * time.Millisecond
	return p, func() { p.close(); direct.Close() }
}

// acquireNow acquires without blocking the test forever: it fails the test if a route is not
// granted within a second.
func acquireNow(t *testing.T, p *routePool, a *account) *route {
	t.Helper()
	done := make(chan struct{})
	got := make(chan *route, 1)
	go func() {
		r, _ := p.acquire(done, a, func() bool { return false }, func(bool) {})
		got <- r
	}()
	select {
	case r := <-got:
		return r
	case <-time.After(time.Second):
		close(done)
		t.Fatalf("%s was not admitted", a.username)
		return nil
	}
}

// acquireQueued is acquireNow, plus the one fact the spacing tests actually care about:
// whether the account BLOCKED waiting for a route, or was admitted straight away.
//
// p.acquire calls setWaiting(true) exactly once, and only when every route has refused it and
// it is about to sleep on a timer. So this reads "did it queue?" off the production code's own
// signal rather than inferring it from a stopwatch.
//
// That distinction is the whole point. "Three accounts were admitted in under 200ms" was
// standing in for "none of them queued", and the two are not the same claim: the first is a
// statement about this machine's scheduler, and it FAILED on 2026-09-21 at 230.7ms with ~117
// test packages running at once, while the property it was protecting held perfectly. The
// second is a statement about the pool, and no amount of load can move it.
func acquireQueued(t *testing.T, p *routePool, a *account) (*route, bool) {
	t.Helper()
	done := make(chan struct{})
	got := make(chan *route, 1)
	var queued atomic.Bool
	go func() {
		r, _ := p.acquire(done, a, func() bool { return false }, func(waiting bool) {
			if waiting {
				queued.Store(true)
			}
		})
		got <- r
	}()
	select {
	case r := <-got:
		return r, queued.Load()
	case <-time.After(time.Second):
		close(done)
		t.Fatalf("%s was not admitted", a.username)
		return nil, false
	}
}

// checkCaps asserts the invariant this whole file exists for.
func checkCaps(t *testing.T, p *routePool) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	held := map[*route]int{}
	for _, r := range p.assign {
		held[r]++
	}
	for _, r := range p.routes {
		if r.held != held[r] || r.held > r.max || r.connected > r.held || r.inflight < 0 {
			t.Fatalf("route %s: held=%d (counted %d) connected=%d inflight=%d max=%d", r.id, r.held, held[r], r.connected, r.inflight, r.max)
		}
	}
}

func acct(name string) *account { return &account{username: name} }

const threeRoutes = `{
  "direct": {"max": 2},
  "routes": [
    {"id": "warp", "type": "socks5", "addr": "127.0.0.1:1", "max": 2},
    {"id": "paid", "type": "socks5", "addr": "127.0.0.1:2", "username": "u", "passwordEnv": "PROXY_PW", "max": 1}
  ]
}`

func TestAdmissionFillsDirectThenRoutesInOrderAndQueuesTheRest(t *testing.T) {
	p, stop := newTestPool(t, threeRoutes)
	defer stop()
	want := []string{"direct", "direct", "warp", "warp", "paid"}
	var accts []*account
	for i, id := range want {
		a := acct(fmt.Sprintf("a%d", i))
		accts = append(accts, a)
		if r := acquireNow(t, p, a); r.id != id {
			t.Fatalf("account %d went to %s, want %s", i, r.id, id)
		}
		p.finish(a, p.assign[a], nil, 10*time.Millisecond)
		checkCaps(t, p)
	}

	// Everything is full: the sixth account queues, reports that it is waiting, and sends
	// nothing (it holds no route and no attempt).
	sixth := acct("a5")
	var waiting atomic.Bool
	done := make(chan struct{})
	got := make(chan *route, 1)
	go func() {
		r, _ := p.acquire(done, sixth, func() bool { return false }, func(w bool) { waiting.Store(w) })
		got <- r
	}()
	time.Sleep(100 * time.Millisecond)
	if !waiting.Load() || p.routeOf(sixth) != "" {
		t.Fatalf("sixth account should be queued: waiting=%v route=%q", waiting.Load(), p.routeOf(sixth))
	}
	checkCaps(t, p)

	// A warp session ends and its account leaves: the queued account takes exactly that slot.
	p.sessionDown(accts[2], time.Hour, true)
	p.release(accts[2])
	select {
	case r := <-got:
		if r.id != "warp" {
			t.Fatalf("queued account got %s, want the freed warp slot", r.id)
		}
	case <-time.After(time.Second):
		t.Fatal("queued account was not admitted when a slot freed")
	}
	if waiting.Load() {
		t.Fatal("waiting flag was not cleared on admission")
	}
	close(done)
	checkCaps(t, p)
}

func TestCapsHoldUnderConcurrentChurn(t *testing.T) {
	p, stop := newTestPool(t, threeRoutes)
	defer stop()
	done := make(chan struct{})
	var wg sync.WaitGroup
	var maxSeen atomic.Int64
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := acct(fmt.Sprintf("c%d", i))
			for round := 0; round < 5; round++ {
				r, ok := p.acquire(done, a, func() bool { return false }, func(bool) {})
				if !ok {
					return
				}
				p.finish(a, r, nil, time.Millisecond)
				p.mu.Lock()
				total := int64(0)
				for _, rr := range p.routes {
					if rr.connected > rr.max || rr.held > rr.max {
						t.Errorf("route %s over its cap: held %d connected %d max %d", rr.id, rr.held, rr.connected, rr.max)
					}
					total += int64(rr.connected)
				}
				p.mu.Unlock()
				if total > maxSeen.Load() {
					maxSeen.Store(total)
				}
				time.Sleep(time.Millisecond)
				p.sessionDown(a, time.Hour, true)
				p.release(a)
			}
		}(i)
	}
	wg.Wait()
	close(done)
	checkCaps(t, p)
	if maxSeen.Load() > 5 {
		t.Fatalf("saw %d sessions at once across routes capped at 5", maxSeen.Load())
	}
}

func TestStickyRouteAcrossReconnects(t *testing.T) {
	p, stop := newTestPool(t, threeRoutes)
	defer stop()
	fill := []*account{acct("d1"), acct("d2")}
	for _, a := range fill {
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	w := acct("w1")
	if r := acquireNow(t, p, w); r.id != "warp" {
		t.Fatalf("got %s", r.id)
	}
	p.finish(w, p.assign[w], nil, time.Millisecond)
	// A direct slot frees up, then w's session drops: w comes back through warp, the route it
	// holds, not the now-free direct slot.
	p.sessionDown(fill[0], time.Hour, true)
	p.release(fill[0])
	p.sessionDown(w, time.Hour, true)
	if r := acquireNow(t, p, w); r.id != "warp" {
		t.Fatalf("reconnect went to %s, want sticky warp", r.id)
	}
	checkCaps(t, p)
}

func TestProxyFaultFailsOverAndLeavesLiveSessionsAlone(t *testing.T) {
	p, stop := newTestPool(t, threeRoutes)
	defer stop()
	for _, n := range []string{"d1", "d2"} {
		a := acct(n)
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	live := acct("live")
	p.finish(live, acquireNow(t, p, live), nil, time.Millisecond) // warp, connected
	pending := acct("pending")
	r := acquireNow(t, p, pending) // warp, about to fail
	if r.id != "warp" {
		t.Fatalf("got %s", r.id)
	}
	fault := &proxydial.Error{Proxy: "socks5://127.0.0.1:1", Stage: proxydial.StageConnect, Err: errors.New("connection refused")}
	p.finish(pending, r, fmt.Errorf("client: dial: %w", fault), time.Millisecond)

	st := statusByID(p.statuses())
	if st["warp"].Health != "unhealthy" || st["warp"].ProxyFaults != 1 || st["warp"].Sessions != 1 {
		t.Fatalf("warp after a proxy fault: %+v", st["warp"])
	}
	if p.routeOf(pending) != "" {
		t.Fatal("the failed account kept its slot on the broken route")
	}
	if p.routeOf(live) != "warp" {
		t.Fatal("a live session on the broken route lost its slot")
	}
	// The failed account fails over to the next route with room.
	if r := acquireNow(t, p, pending); r.id != "paid" {
		t.Fatalf("failover went to %s, want paid", r.id)
	}
	// When the live warp session drops, its account does not go back to the broken route.
	p.sessionDown(live, time.Hour, true)
	done := make(chan struct{})
	defer close(done)
	got := make(chan *route, 1)
	go func() {
		r, _ := p.acquire(done, live, func() bool { return false }, func(bool) {})
		got <- r
	}()
	select {
	case r := <-got:
		t.Fatalf("account was admitted to %s while every healthy route is full", r.id)
	case <-time.After(150 * time.Millisecond):
	}
	if p.routeOf(live) != "" {
		t.Fatal("the dropped account is still holding the unhealthy route")
	}
	checkCaps(t, p)
}

func TestUnhealthyRouteBacksOffThenProbationAdmitsOneAtATime(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"enabled": false}, "routes": [{"id": "warp", "type": "socks5", "addr": "127.0.0.1:1", "max": 5}]}`)
	defer stop()
	now := time.Now()
	p.now = func() time.Time { return now }
	a := acct("a")
	r := acquireNow(t, p, a)
	fault := &proxydial.Error{Proxy: "x", Stage: proxydial.StageHandshake, Err: errors.New("general SOCKS server failure")}
	p.finish(a, r, fault, time.Millisecond)
	if st := statusByID(p.statuses())["warp"]; st.Health != "unhealthy" || st.UnhealthyUntilMs != now.Add(routeUnhealthyBase).UnixMilli() {
		t.Fatalf("after one fault: %+v", st)
	}
	// Second fault in a row doubles the backoff.
	now = now.Add(routeUnhealthyBase)
	r = acquireNow(t, p, a)
	p.finish(a, r, fault, time.Millisecond)
	if st := statusByID(p.statuses())["warp"]; st.UnhealthyUntilMs != now.Add(2*routeUnhealthyBase).UnixMilli() {
		t.Fatalf("after two faults: %+v", st)
	}
	now = now.Add(2 * routeUnhealthyBase)
	// Probation: one attempt at a time.
	r1 := acquireNow(t, p, a)
	b := acct("b")
	done := make(chan struct{})
	got := make(chan *route, 1)
	go func() { r, _ := p.acquire(done, b, func() bool { return false }, func(bool) {}); got <- r }()
	select {
	case <-got:
		t.Fatal("probation admitted a second login while the first was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	p.finish(a, r1, nil, time.Millisecond)
	select {
	case r := <-got:
		if r.id != "warp" {
			t.Fatal(r.id)
		}
	case <-time.After(time.Second):
		t.Fatal("route did not reopen after a successful probation login")
	}
	close(done)
	if st := statusByID(p.statuses())["warp"]; st.Health != "healthy" {
		t.Fatalf("after probation success: %+v", st)
	}
}

func TestPenaltiesAreScopedToTheirRoute(t *testing.T) {
	p, stop := newTestPool(t, threeRoutes)
	defer stop()
	for _, n := range []string{"d1", "d2"} {
		a := acct(n)
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	a := acct("rate")
	r := acquireNow(t, p, a)
	p.finish(a, r, &client.LoginRejectedError{Response: client.LoginAttemptsExceededResponse}, time.Millisecond)
	st := statusByID(p.statuses())
	if st["warp"].PenalizedUntilMs == 0 {
		t.Fatal("response 16 on warp did not pause warp's gate")
	}
	if st["direct"].PenalizedUntilMs != 0 || st["paid"].PenalizedUntilMs != 0 {
		t.Fatalf("response 16 on warp paused other routes: %+v %+v", st["direct"], st["paid"])
	}
	if p.routeOf(a) != "warp" {
		t.Fatal("a rate verdict should not cost the account its slot")
	}

	// Response 9 on warp: that account gives its slot back and warp takes no NEW accounts for
	// the hold, while other routes are untouched.
	full := acct("full")
	r = acquireNow(t, p, full)
	if r.id != "warp" {
		t.Fatalf("got %s", r.id)
	}
	p.finish(full, r, &client.LoginRejectedError{Response: connectionCeilingResponse}, time.Millisecond)
	st = statusByID(p.statuses())
	if st["warp"].FullUntilMs == 0 || p.routeOf(full) != "" || st["warp"].Responses["9"] != 1 {
		t.Fatalf("after response 9: %+v route=%q", st["warp"], p.routeOf(full))
	}
	if r := acquireNow(t, p, full); r.id != "paid" {
		t.Fatalf("new admission during warp's hold went to %s, want paid", r.id)
	}
	// The account already holding a warp slot keeps it (sticky) despite the hold.
	if r := acquireNow(t, p, a); r.id != "warp" {
		t.Fatalf("sticky account moved to %s", r.id)
	}
	checkCaps(t, p)
}

func TestRepeatedYoungDropsMarkAProxyUnhealthyButNotDirect(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"max": 5}, "routes": [{"id": "warp", "type": "socks5", "addr": "127.0.0.1:1", "max": 5}]}`)
	defer stop()
	// Fill direct so the next logins go to warp.
	var directs []*account
	for i := 0; i < 5; i++ {
		a := acct(fmt.Sprintf("d%d", i))
		directs = append(directs, a)
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	for i := 0; i < routeShortDrops; i++ {
		a := acct(fmt.Sprintf("w%d", i))
		r := acquireNow(t, p, a)
		if r.id != "warp" {
			t.Fatal(r.id)
		}
		p.finish(a, r, nil, time.Millisecond)
		p.sessionDown(a, 10*time.Second, true) // WARP-style drop shortly after login
	}
	if st := statusByID(p.statuses())["warp"]; st.Health != "unhealthy" || st.Drops != routeShortDrops {
		t.Fatalf("warp after %d young drops: %+v", routeShortDrops, st)
	}
	// The same pattern on direct never marks it (its failures are the server's).
	for _, a := range directs {
		p.sessionDown(a, 10*time.Second, true)
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	if st := statusByID(p.statuses())["direct"]; st.Health != "healthy" {
		t.Fatalf("direct: %+v", st)
	}
}

func TestLatencyGateRotatesTorCredentialsUntilACircuitIsFastEnough(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"enabled": false}, "routes": [
		{"id": "tor-1", "type": "socks5", "addr": "127.0.0.1:9050", "torIsolation": true, "latencyGate": true, "latencySamples": 3, "latencyRetries": 3}]}`)
	defer stop()
	var users []string
	var mu sync.Mutex
	p.probe = func(r *route) ([]time.Duration, error) {
		link := r.currentLink()
		mu.Lock()
		users = append(users, link.dialer.GoString()+"|"+strings.Repeat("~", r.generation))
		mu.Unlock()
		if r.generation < 3 {
			return []time.Duration{900 * time.Millisecond, 950 * time.Millisecond, 800 * time.Millisecond}, nil
		}
		return []time.Duration{120 * time.Millisecond, 180 * time.Millisecond, 150 * time.Millisecond}, nil
	}
	a := acct("a")
	r := acquireNow(t, p, a) // blocks until the gate passes on generation 3
	if r.id != "tor-1" {
		t.Fatal(r.id)
	}
	st := statusByID(p.statuses())["tor-1"]
	if st.LatencyGate != "passed" || st.Generation != 3 || st.LatencyMedianMs != 150 || st.LatencyChecks != 3 || st.LatencyFails != 2 {
		t.Fatalf("gate status: %+v", st)
	}
	if len(st.LatencySamplesMs) != 3 {
		t.Fatalf("samples: %v", st.LatencySamplesMs)
	}
	if got := r.currentLink().dialer; !strings.Contains(fmt.Sprintf("%#v", got), "127.0.0.1:9050") {
		t.Fatalf("dialer: %#v", got)
	}
}

func TestLatencyGateGivesUpAndMarksTheRouteUnhealthy(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"max": 1}, "routes": [
		{"id": "tor-1", "type": "socks5", "addr": "127.0.0.1:9050", "torIsolation": true, "maxLatencyMs": 400, "latencySamples": 3, "latencyRetries": 1}]}`)
	defer stop()
	var probes atomic.Int32
	p.probe = func(r *route) ([]time.Duration, error) {
		probes.Add(1)
		return []time.Duration{time.Second, time.Second, time.Second}, nil
	}
	d := acct("d")
	p.finish(d, acquireNow(t, p, d), nil, time.Millisecond)
	a := acct("a")
	done := make(chan struct{})
	defer close(done)
	go p.acquire(done, a, func() bool { return false }, func(bool) {})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st := statusByID(p.statuses())["tor-1"]; st.Health == "unhealthy" {
			if st.LatencyGate != "failed" || probes.Load() != 2 || st.Generation != 2 || !strings.Contains(st.LastError, "above the 400ms limit") {
				t.Fatalf("after giving up: %+v probes=%d", st, probes.Load())
			}
			if p.routeOf(a) != "" {
				t.Fatal("an account was admitted to a route that failed its latency gate")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("route never went unhealthy: %+v", statusByID(p.statuses())["tor-1"])
}

func TestPoolFileValidationAndDefaults(t *testing.T) {
	p := mustPool(t, `{"routes": [
		{"id": "warp", "type": "socks5", "addr": "127.0.0.1:40000"},
		{"id": "tor-1", "type": "socks5", "addr": "127.0.0.1:9050", "torIsolation": true},
		{"id": "tor-2", "type": "socks5", "addr": "127.0.0.1:9050", "torIsolation": true, "latencyGate": true},
		{"id": "paid", "type": "http", "addr": "proxy.example.net:3128", "username": "u", "passwordEnv": "PROXY_PW", "dns": "local"}]}`)
	if !p.Direct.Enabled || p.Direct.Max != DefaultRouteMax {
		t.Fatalf("direct defaults: %+v", p.Direct)
	}
	byID := map[string]ProxyRoute{}
	for _, r := range p.Routes {
		byID[r.ID] = r
	}
	if w := byID["warp"]; !w.LocalDNS || w.Max != 89 || w.Username != "" || w.MaxLatency != 0 {
		t.Fatalf("warp: %#v local=%v max=%d", w, w.LocalDNS, w.Max)
	}
	if t1 := byID["tor-1"]; t1.LocalDNS || t1.Username != "tor-1" || t1.Password == "" {
		t.Fatalf("tor-1 should get remote DNS and its own isolation username")
	}
	if t2 := byID["tor-2"]; t2.MaxLatency != DefaultMaxLatency || t2.LatencySamples != 5 || t2.LatencyRetries != 3 {
		t.Fatalf("tor-2 gate defaults: %v %d %d", t2.MaxLatency, t2.LatencySamples, t2.LatencyRetries)
	}
	if pd := byID["paid"]; !pd.LocalDNS || pd.Password.Reveal() != poolSecret {
		t.Fatal("paid: passwordEnv or dns override not applied")
	}

	for name, js := range map[string]string{
		"same proxy twice":    `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1"}, {"id": "b", "type": "socks5", "addr": "H:1"}]}`,
		"reserved id":         `{"routes": [{"id": "direct", "type": "socks5", "addr": "h:1"}]}`,
		"unknown key":         `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "pasword": "x"}]}`,
		"bad type":            `{"routes": [{"id": "a", "type": "socks4", "addr": "h:1"}]}`,
		"user without pass":   `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "username": "u"}]}`,
		"both password kinds": `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "username": "u", "password": "` + poolSecret + `", "passwordEnv": "PROXY_PW"}]}`,
		"unset env":           `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "username": "u", "passwordEnv": "NOPE"}]}`,
		"bad dns":             `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "dns": "doh"}]}`,
		"cap too big":         `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "max": 100000}]}`,
		"nothing enabled":     `{"direct": {"enabled": false}}`,
		"samples no gate":     `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "latencySamples": 3}]}`,
		"type mismatch":       `{"routes": [{"id": "a", "type": "socks5", "addr": "h:1", "password": 5}]}`,
	} {
		_, err := ParseRoutePool([]byte(js), func(string) string { return "" })
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), poolSecret) {
			t.Errorf("%s: error leaked the password: %v", name, err)
		}
	}
}

func TestProxyCredentialsNeverPrint(t *testing.T) {
	pool := mustPool(t, `{"direct": {"enabled": false}, "routes": [
		{"id": "inline", "type": "socks5", "addr": "127.0.0.1:1", "username": "user-inline", "password": "`+poolSecret+`", "max": 1},
		{"id": "env", "type": "http", "addr": "127.0.0.1:2", "username": "user-env", "passwordEnv": "PROXY_PW", "max": 1}]}`)
	js, _ := json.Marshal(pool)
	outputs := []string{fmt.Sprintf("%v %+v %#v %s", pool, pool, pool, pool.Routes), string(js)}
	for _, r := range pool.Routes {
		outputs = append(outputs, fmt.Sprintf("%v %+v %#v %s", r, r, r, r), r.String(), r.GoString())
	}

	var lines []string
	var mu sync.Mutex
	direct := newLoginRateGate()
	defer direct.Close()
	p := newRoutePool(pool, direct, "ws://localhost/", func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() })
	defer p.close()
	for _, r := range p.routes {
		outputs = append(outputs, r.describe(), fmt.Sprintf("%#v", r.currentLink().dialer), r.currentLink().dialer.String())
		// A proxy fault on the route is logged and shown on the panel. Each route has room
		// for one, so the accounts land on the routes in order.
		a := acct("x" + r.id)
		got := acquireNow(t, p, a)
		if got != r {
			t.Fatalf("expected %s, got %s", r.id, got.id)
		}
		_, err := got.currentLink().dialer.DialContext(context.Background(), "tcp", "127.0.0.1:9")
		p.finish(a, got, err, time.Millisecond)
	}
	st, _ := json.Marshal(p.statuses())
	outputs = append(outputs, string(st))
	mu.Lock()
	outputs = append(outputs, lines...)
	mu.Unlock()
	for _, out := range outputs {
		if strings.Contains(out, poolSecret) {
			t.Fatalf("password leaked: %s", out)
		}
		if strings.Contains(out, "user-inline") || strings.Contains(out, "user-env") {
			t.Fatalf("username leaked: %s", out)
		}
	}
	if !strings.Contains(string(st), `"proxyFaults":1`) {
		t.Fatalf("expected the dial failures to be recorded: %s", st)
	}
}

func statusByID(sts []RouteStatus) map[string]RouteStatus {
	out := map[string]RouteStatus{}
	for _, s := range sts {
		out[s.ID] = s
	}
	return out
}

func TestDeliberateLogoutsAreNotDropsAndABurstReArmsTheLatencyGate(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"enabled": false}, "routes": [
		{"id": "psiphon", "type": "socks5", "addr": "127.0.0.1:40100", "latencyGate": true, "maxLatencyMs": 1000, "latencySamples": 3}]}`)
	defer stop()
	var probes atomic.Int32
	p.probe = func(*route) ([]time.Duration, error) {
		probes.Add(1)
		return []time.Duration{500 * time.Millisecond, 520 * time.Millisecond, 540 * time.Millisecond}, nil
	}
	var as []*account
	for i := 0; i < 4; i++ {
		a := acct(fmt.Sprintf("p%d", i))
		as = append(as, a)
		p.finish(a, acquireNow(t, p, a), nil, time.Millisecond)
	}
	if probes.Load() != 1 {
		t.Fatalf("gate ran %d times for one route, want 1", probes.Load())
	}
	// Deliberate logouts, however young, are not drops and do not touch health or the gate.
	for _, a := range as[:3] {
		p.sessionDown(a, time.Second, false)
		p.release(a)
	}
	st := statusByID(p.statuses())["psiphon"]
	if st.Drops != 0 || st.Health != "healthy" || st.LatencyGate != "passed" {
		t.Fatalf("after deliberate logouts: %+v", st)
	}
	// Two unexpected drops together (the proxy reconnected): the gate re-runs before the
	// next login on the route.
	b := acct("b")
	p.finish(b, acquireNow(t, p, b), nil, time.Millisecond)
	p.sessionDown(as[3], time.Hour, true)
	p.sessionDown(b, time.Hour, true)
	if st := statusByID(p.statuses())["psiphon"]; st.LatencyGate != "pending" {
		t.Fatalf("a burst of drops should re-arm the gate: %+v", st)
	}
	acquireNow(t, p, b) // sticky, but only after the gate passes again
	if probes.Load() != 2 {
		t.Fatalf("gate ran %d times, want 2", probes.Load())
	}
}

// The committed example pool is the owner's starting point: it must parse with no environment
// set, and its enabled routes (direct, WARP, Psiphon) must cover the 266-account roster.
func TestExamplePoolFileParses(t *testing.T) {
	data, err := os.ReadFile("../../proxies.example.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseRoutePool(data, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	total, ids := p.Direct.Max, []string{"direct"}
	for _, r := range p.Routes {
		if r.Enabled {
			total += r.Max
			ids = append(ids, r.ID)
		}
	}
	if total != 266 || strings.Join(ids, ",") != "direct,warp,psiphon" {
		t.Fatalf("example pool enables %v for %d sessions, want direct,warp,psiphon for 266", ids, total)
	}
}

func TestSpacingRampsEveryRouteSideBySide(t *testing.T) {
	direct := newLoginRateGate()
	cfg := mustPool(t, threeRoutes)
	cfg.Spacing = 300 * time.Millisecond
	p := newRoutePool(cfg, direct, "ws://localhost/", func(string) {})
	defer func() { p.close(); direct.Close() }()

	// Back-to-back first logins spread across the routes instead of filling direct first.
	//
	// "Side by side" means NONE OF THE THREE QUEUED, which is what acquireQueued reports. The
	// wall-clock version of this assertion is in that helper's own comment, along with the
	// load flake that retired it.
	start := time.Now()
	for i, id := range []string{"direct", "warp", "paid"} {
		a := acct(fmt.Sprintf("a%d", i))
		r, queued := acquireQueued(t, p, a)
		if r.id != id {
			t.Fatalf("account %d went to %s, want %s", i, r.id, id)
		}
		if queued {
			t.Fatalf("account %d queued for %s instead of being admitted side by side — the routes are filling one at a time", i, id)
		}
		p.finish(a, p.assign[a], nil, time.Millisecond)
	}
	// The fourth waits out a route's spacing, then takes the first one free again. Here the
	// wait IS the property, so it stays a timing assertion — but a LOWER bound, which load can
	// only ever push further into the safe direction, and stated against cfg.Spacing rather
	// than a literal standing in for it.
	a := acct("a3")
	r, queued := acquireQueued(t, p, a)
	if r.id != "direct" {
		t.Fatalf("fourth account went to %s, want direct", r.id)
	}
	if !queued {
		t.Fatal("fourth account was admitted without queueing — nothing made it wait out the spacing")
	}
	if waited := time.Since(start); waited < cfg.Spacing-clockSlack {
		t.Fatalf("fourth account was admitted after %s, want at least one spacing interval (%s)", waited, cfg.Spacing)
	}
	p.finish(a, p.assign[a], nil, time.Millisecond)

	// A reconnect of an account already holding a slot is not held by the spacing.
	p.sessionDown(a, time.Hour, true)
	if r, queued := acquireQueued(t, p, a); r.id != "direct" || queued {
		t.Fatalf("sticky reconnect went to %s (queued=%v), want direct with no wait", r.id, queued)
	}
	checkCaps(t, p)
}

func TestRouteRestsAfterALoginBurstSoTheServerCounterClears(t *testing.T) {
	direct := newLoginRateGate()
	cfg := mustPool(t, `{"direct": {"enabled": false}, "routes": [{"id": "warp", "type": "socks5", "addr": "127.0.0.1:1", "max": 10}]}`)
	cfg.LoginBurst, cfg.LoginQuiet = 3, 400*time.Millisecond
	p := newRoutePool(cfg, direct, "ws://localhost/", func(string) {})
	defer func() { p.close(); direct.Close() }()

	var last time.Time
	for i := 0; i < 3; i++ {
		a := acct(fmt.Sprintf("a%d", i))
		if _, queued := acquireQueued(t, p, a); queued {
			t.Fatalf("account %d of the burst queued — a burst below LoginBurst should go straight through", i)
		}
		p.finish(a, p.assign[a], nil, time.Millisecond)
		last = time.Now()
	}
	// The fourth attempt waits out the quiet measured from the last attempt's end.
	a := acct("a3")
	acquireNow(t, p, a)
	if waited := time.Since(last); waited < cfg.LoginQuiet-clockSlack {
		t.Fatalf("fourth login came %s after the burst, want the %s rest", waited, cfg.LoginQuiet)
	}
	p.finish(a, p.assign[a], nil, time.Millisecond)

	// A sticky reconnect counts as an attempt too: two more fill the new burst, then it rests.
	p.sessionDown(a, time.Hour, true)
	acquireNow(t, p, a)
	p.finish(a, p.assign[a], nil, time.Millisecond)
	b := acct("a4")
	acquireNow(t, p, b)
	p.finish(b, p.assign[b], nil, time.Millisecond)
	p.mu.Lock()
	resting := p.resting(p.routes[0], time.Now())
	p.mu.Unlock()
	if !resting {
		t.Fatal("three attempts including a reconnect should put the route at rest")
	}
	checkCaps(t, p)
}

// TestALateLoginAfterAFailedGateDoesNotWedgeTheRoute replays route psiphon2 on build e35420b6
// (2026-09-24 04:18:34 Perth): three logins failed in transit and marked the route unhealthy (gate
// "failed") while a slower login was still in flight; that one then came back OK and set the route
// healthy again with the gate still "failed". Nothing re-ran the gate from there, and a sticky
// account is only let go by an unhealthy route, so when the route's sessions all dropped four
// hours later its 18 accounts sat queued for 32 hours without a single attempt.
func TestALateLoginAfterAFailedGateDoesNotWedgeTheRoute(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"enabled": false}, "routes": [
		{"id": "psiphon2", "type": "socks5", "addr": "127.0.0.1:40200", "latencyGate": true, "maxLatencyMs": 2500, "latencySamples": 3, "max": 10}]}`)
	defer stop()
	var probes atomic.Int32
	p.probe = func(*route) ([]time.Duration, error) {
		probes.Add(1)
		return []time.Duration{time.Second, time.Second, time.Second}, nil
	}
	live := acct("live")
	p.finish(live, acquireNow(t, p, live), nil, time.Millisecond) // connected before the trouble
	slow := acct("slow")
	rSlow := acquireNow(t, p, slow) // still in flight when the route is marked unhealthy
	transit := errors.New("client: dial: context deadline exceeded")
	for i := 0; i < routeSoftFailLimit; i++ {
		a := acct(fmt.Sprintf("f%d", i))
		p.finish(a, acquireNow(t, p, a), transit, 20*time.Second)
	}
	if st := statusByID(p.statuses())["psiphon2"]; st.Health != "unhealthy" || st.LatencyGate != "failed" {
		t.Fatalf("setup: three transit failures should mark the route unhealthy with a failed gate: %+v", st)
	}
	p.finish(slow, rSlow, nil, 42*time.Second) // the late success
	if st := statusByID(p.statuses())["psiphon2"]; st.Health != "healthy" || st.LatencyGate == "failed" {
		t.Fatalf("a healthy route must not keep a failed gate (nothing re-runs it): %+v", st)
	}
	if p.routeOf(slow) != "psiphon2" {
		t.Fatal("the late session is not counted against the route's cap")
	}
	checkCaps(t, p)

	// Four hours later every session on the route drops at once ("transport: read timed out").
	p.sessionDown(live, 4*time.Hour, true)
	p.sessionDown(slow, 4*time.Hour, true)
	before := probes.Load()
	for _, a := range []*account{live, slow} {
		if r := acquireNow(t, p, a); r.id != "psiphon2" {
			t.Fatalf("%s reconnected on %s", a.username, r.id)
		}
	}
	if probes.Load() == before {
		t.Fatal("the route admitted logins without measuring its latency again")
	}
	checkCaps(t, p)
}

// TestAWedgedGateIsMeasuredAgain covers the same state however it is reached: a gate left
// "failed" on a route that is not unhealthy is measured again at the next admission instead of
// refusing everyone for ever.
func TestAWedgedGateIsMeasuredAgain(t *testing.T) {
	p, stop := newTestPool(t, `{"direct": {"enabled": false}, "routes": [
		{"id": "psiphon2", "type": "socks5", "addr": "127.0.0.1:40200", "latencyGate": true, "maxLatencyMs": 2500, "latencySamples": 3, "max": 10}]}`)
	defer stop()
	p.probe = func(*route) ([]time.Duration, error) {
		return []time.Duration{time.Second, time.Second, time.Second}, nil
	}
	p.mu.Lock()
	p.routes[0].gateState = gateFailed // health stays healthy
	p.mu.Unlock()
	if r := acquireNow(t, p, acct("a")); r.id != "psiphon2" {
		t.Fatal(r.id)
	}
	if st := statusByID(p.statuses())["psiphon2"]; st.LatencyGate != "passed" {
		t.Fatalf("gate: %+v", st)
	}
}
