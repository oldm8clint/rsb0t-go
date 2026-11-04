package fleet

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/proxydial"
)

// This file is route admission: which address an account logs in from.
//
// Why it exists. The live server allows at most 90 accounts logged in at once per IP address (its rules
// page, 2026-09-19), and the live fleet measured that ceiling as login response 9 at 89-90 in
// game (loginceiling.go). The server owner agreed the fleet may divert connections through
// other addresses. A route is one address: "direct" (the home connection) or one proxy.
//
// The rules this enforces:
//
//   - Caps are never exceeded. An account holds a slot on one route from admission until it
//     is released (a deliberate disconnect, a permanent login failure, a response 9 on that
//     route, or its route going unhealthy). A route never has more slot holders than its max,
//     so it never has more live sessions than its max either.
//   - Direct first, then the routes in file order. An account that fits nowhere waits in
//     acquire, sending nothing to the server — the queue, rather than a response-9 retry loop.
//   - Sticky. An account keeps its slot across reconnects while its route is healthy, so a
//     dropped account comes back from the same address.
//   - Pacing is per route, because the server's login-rate and connection verdicts are per
//     address: each route has its own loginRateGate (direct keeps the Manager's), a response
//     16 penalizes only that route's gate, and a response 9 closes only that route to new
//     admissions for a while. The per-account ceiling backoff (loginceiling.go) still applies.
//   - A proxy that fails (cannot be reached, refuses the credentials, fails the handshake,
//     keeps failing logins at the transport level, or keeps dropping fresh sessions) goes
//     unhealthy for a backoff that doubles on each repeat. Its unconnected accounts are
//     released to the queue and fail over to another route; sessions still running on it are
//     left alone. When the backoff ends it is on probation: one login at a time until one
//     succeeds.
//
// With no pool configured none of this exists (Manager.routes is nil) and the login path is
// byte-for-byte what it was.

// Route health backoff: 30s doubling to 10 minutes.
const (
	routeUnhealthyBase = 30 * time.Second
	routeUnhealthyMax  = 10 * time.Minute
)

// routeSoftFailLimit is how many consecutive transport-level login failures (not a proxy
// handshake fault, not a login response) a proxy route may have before it counts as broken.
// A login response of any kind resets it: the server answered, so the path works.
const routeSoftFailLimit = 3

// A proxy route whose sessions keep dying young is not holding connections: routeShortDrops
// drops of sessions younger than routeShortSession inside routeShortDropWindow mark it
// unhealthy. A server restart drops long-lived sessions, which do not count.
const (
	routeShortSession    = 2 * time.Minute
	routeShortDrops      = 3
	routeShortDropWindow = 5 * time.Minute
)

// routeBurstWindow: two unexpected drops on one route this close together are read as the proxy
// itself reconnecting, which re-arms a latency gate.
const routeBurstWindow = 10 * time.Second

// routeFullHold is how long a response 9 on a route closes it to new admissions: the same
// first-probe interval loginceiling.go uses for a full address.
const routeFullHold = connectionCeilingBackoffStart

type routeHealth string

const (
	routeHealthy   routeHealth = "healthy"
	routeUnhealthy routeHealth = "unhealthy"
	routeProbation routeHealth = "probation"
)

type route struct {
	id     string
	kind   string // "direct", "socks5" or "http"
	host   string // the proxy host label (no port, no credentials); "direct" for the home address
	direct bool
	max    int

	connectTimeout time.Duration
	slowConnect    time.Duration
	localDNS       bool
	gate           *loginRateGate
	ownGate        bool
	cfg            ProxyRoute // the proxy's settings, for rebuilding the dialer on rotation

	// link is the dialer and HTTP client currently in use. Swapped (not mutated) when a
	// torIsolation route rotates its credentials, so a login holding the old one is unaffected.
	link atomic.Pointer[routeLink]

	// Everything below is guarded by routePool.mu.
	held, connected, inflight int
	health                    routeHealth
	unhealthyUntil            time.Time
	failStreak                int
	softFails                 int
	fullUntil                 time.Time
	nextFirst                 time.Time // RoutePool.Spacing: no first login on this route before it
	burst                     int       // attempts since the route was last quiet for LoginQuiet
	lastAttempt               time.Time
	shortDrops                []time.Time
	lastDropAt                time.Time
	lastErr                   string

	loginsOK, loginsFailed, proxyFaults, drops, slowConnects int64
	responses                                                map[int]int64
	lastConnect, totalConnect                                time.Duration
	connectSamples                                           int64

	// The latency gate (routelatency.go). gateState is "" when the route has no gate.
	gateState      gateState
	generation     int // isolation credential generation; 1 is the configured username
	latencySamples []time.Duration
	latencyMedian  time.Duration
	latencyAt      time.Time
	latencyChecks  int64
	latencyFails   int64
}

// routeLink is one dialer and the HTTP client built on it.
type routeLink struct {
	dialer     *proxydial.Dialer
	httpClient *http.Client
}

// RouteStatus is one route's read-only counters: no address beyond the host label, never a
// credential.
type RouteStatus struct {
	ID                 string           `json:"id"`
	Kind               string           `json:"kind"`
	Host               string           `json:"host"`
	Max                int              `json:"max"`
	Held               int              `json:"held"`
	Sessions           int              `json:"sessions"`
	InFlight           int              `json:"inFlight"`
	Health             string           `json:"health"`
	UnhealthyUntilMs   int64            `json:"unhealthyUntil,omitempty"`
	FullUntilMs        int64            `json:"fullUntil,omitempty"`
	RestUntilMs        int64            `json:"restUntil,omitempty"` // resting after a login burst (RoutePool.LoginBurst)
	PenalizedUntilMs   int64            `json:"penalizedUntil,omitempty"`
	LoginsOK           int64            `json:"loginsOk"`
	LoginsFailed       int64            `json:"loginsFailed"`
	ProxyFaults        int64            `json:"proxyFaults"`
	Drops              int64            `json:"drops"`
	SlowConnects       int64            `json:"slowConnects"`
	Responses          map[string]int64 `json:"responses,omitempty"`
	LastConnectMs      int64            `json:"lastConnectMs"`
	AvgConnectMs       int64            `json:"avgConnectMs"`
	LastError          string           `json:"lastError,omitempty"`
	ConnectTimeoutSecs float64          `json:"connectTimeoutSecs"`

	// The latency gate, when the route has one: its state ("checking", "passed", "failed"),
	// the threshold, the last check's median and samples, and the isolation generation.
	LatencyGate      string  `json:"latencyGate,omitempty"`
	MaxLatencyMs     int64   `json:"maxLatencyMs,omitempty"`
	LatencyMedianMs  int64   `json:"latencyMedianMs,omitempty"`
	LatencySamplesMs []int64 `json:"latencySamplesMs,omitempty"`
	LatencyAtMs      int64   `json:"latencyAt,omitempty"`
	LatencyChecks    int64   `json:"latencyChecks,omitempty"`
	LatencyFails     int64   `json:"latencyFails,omitempty"`
	Generation       int     `json:"generation,omitempty"`
}

type routePool struct {
	mu      sync.Mutex
	routes  []*route
	assign  map[*account]*route
	up      map[*account]*route
	changed chan struct{}
	now     func() time.Time
	log     func(string)
	spacing time.Duration // RoutePool.Spacing
	burst   int           // RoutePool.LoginBurst
	quiet   time.Duration // RoutePool.LoginQuiet

	// probeTarget is the game host:port the latency gate measures, probeTLS whether the game
	// URL is wss (so each sample includes a TLS handshake) and probeName the TLS server name.
	probeTarget string
	probeTLS    bool
	probeName   string
	// probe measures one route; replaced in tests. retryPause separates gate retries.
	probe      func(r *route) ([]time.Duration, error)
	retryPause time.Duration
	closed     chan struct{}
	closeOnce  sync.Once

	// pins maps PinKey(username) to the one route id that account may use (RoutePool.Pins). A
	// pinned account never fails over: it waits for its own route. Unpinned accounts, and pins
	// naming a route this pool does not have, use the routes in order as before.
	pins map[string]string
}

// pinnedLocked returns the route a is pinned to, or nil when it has no usable pin.
func (p *routePool) pinnedLocked(a *account) *route {
	if len(p.pins) == 0 {
		return nil
	}
	id, ok := p.pins[PinKey(a.username)]
	if !ok {
		return nil
	}
	for _, r := range p.routes {
		if r.id == id {
			return r
		}
	}
	return nil
}

// newRoutePool builds the runtime routes. direct is the Manager's own gate, reused for the
// direct route so the no-proxy pacing is unchanged. Proxy routes get their own gates. wsURL is
// the game URL, the target the latency gate measures.
func newRoutePool(cfg *RoutePool, direct *loginRateGate, wsURL string, log func(string)) *routePool {
	p := &routePool{assign: map[*account]*route{}, up: map[*account]*route{}, changed: make(chan struct{}), now: time.Now, log: log,
		retryPause: 2 * time.Second, closed: make(chan struct{}), spacing: cfg.Spacing,
		burst: cfg.LoginBurst, quiet: cfg.LoginQuiet, pins: cfg.Pins}
	p.probeTarget, p.probeName, p.probeTLS = probeTargetOf(wsURL)
	p.probe = p.measure
	if cfg.Direct.Enabled {
		p.routes = append(p.routes, &route{id: "direct", kind: "direct", host: "direct", direct: true, max: cfg.Direct.Max,
			connectTimeout: cfg.Direct.ConnectTimeout, slowConnect: cfg.Direct.SlowConnect, gate: direct,
			health: routeHealthy, responses: map[int]int64{}})
	}
	for _, pr := range cfg.Routes {
		if !pr.Enabled {
			continue
		}
		r := &route{id: pr.ID, kind: string(pr.Type), max: pr.Max, connectTimeout: pr.ConnectTimeout, slowConnect: pr.SlowConnect,
			health: routeHealthy, responses: map[int]int64{}, cfg: pr, localDNS: pr.LocalDNS, generation: 1}
		if pr.MaxLatency > 0 {
			r.gateState = gateUnverified
		}
		link, err := r.newLink()
		if err != nil {
			// ParseRoutePool already validated this; keep the route visible but never usable.
			r.host = pr.ID
			r.health = routeUnhealthy
			r.unhealthyUntil = time.Now().Add(100 * 365 * 24 * time.Hour)
			r.lastErr = err.Error()
		} else {
			r.link.Store(link)
			r.host = link.dialer.Host()
			r.gate = newLoginRateGate()
			r.ownGate = true
		}
		p.routes = append(p.routes, r)
	}
	return p
}

// newLink builds the dialer for the route's current isolation generation.
func (r *route) newLink() (*routeLink, error) {
	user := r.cfg.Username
	if r.cfg.TorIsolation && r.generation > 1 {
		user = fmt.Sprintf("%s~%d", user, r.generation)
	}
	d, err := proxydial.New(r.cfg.Type, r.cfg.Addr, user, r.cfg.Password.Reveal())
	if err != nil {
		return nil, err
	}
	d.LocalDNS = r.cfg.LocalDNS
	return &routeLink{dialer: d, httpClient: d.HTTPClient(r.cfg.ConnectTimeout + 10*time.Second)}, nil
}

// currentLink is the dialer a login should use now (nil for the direct route).
func (r *route) currentLink() *routeLink { return r.link.Load() }

func (p *routePool) logf(format string, args ...any) {
	if p.log != nil {
		p.log(fmt.Sprintf(format, args...))
	}
}

// broadcastLocked wakes every acquire waiter; caller holds p.mu.
func (p *routePool) broadcastLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// refreshLocked moves a route whose unhealthy backoff has ended onto probation.
func (p *routePool) refreshLocked(r *route, now time.Time) {
	if r.health == routeUnhealthy && !now.Before(r.unhealthyUntil) {
		r.health = routeProbation
		if r.gateState != "" {
			// A route coming back must prove its latency again before anyone logs in on it.
			r.gateState = gateUnverified
		}
		p.logf("[route %s] %s %s: backoff over — on probation, one login at a time", r.id, r.kind, r.host)
	}
}

// gateWedged reports a route whose latency gate says "failed" although the route is not
// unhealthy. Nothing ever leaves that state on its own: startProbeLocked only runs from
// "pending", refreshLocked only acts on an unhealthy route, sessionDown only re-arms a "passed"
// gate, and acquire only lets a sticky account go when its route is unhealthy. So every account
// holding the route waited for ever.
//
// Live on build e35420b6 (2026-09-24 04:18:34 Perth): route psiphon2 was marked unhealthy by
// three failed logins while slower ones were still in flight; two seconds later one of those
// came back OK and set the route healthy again, leaving the gate failed. Its 18 connected
// accounts kept playing until they all dropped with "transport: read timed out" at 08:48:52,
// and then sat "queued" with their bots stopped for the next 32 hours — no probe, no failover,
// not one log line — while the proxy's port was still listening.
func gateWedged(r *route) bool {
	return r.gateState == gateFailed && r.health != routeUnhealthy
}

// admitsLocked reports whether r can take an attempt now. sticky means the account already
// holds a slot here, so the cap and a response-9 hold do not apply to it. A route with a
// latency gate that has not passed admits nobody; the first look at it starts the check.
func (p *routePool) admitsLocked(r *route, now time.Time, sticky bool) bool {
	p.refreshLocked(r, now)
	if r.health == routeUnhealthy {
		return false
	}
	if gateWedged(r) {
		p.logf("[route %s] %s %s: latency gate still marked failed on a %s route — measuring again", r.id, r.kind, r.host, r.health)
		r.gateState = gateUnverified
	}
	switch r.gateState {
	case gateUnverified:
		p.startProbeLocked(r)
		return false
	case gateChecking, gateFailed:
		return false
	}
	switch r.health {
	case routeUnhealthy:
		return false
	case routeProbation:
		if r.inflight > 0 {
			return false
		}
	}
	if p.resting(r, now) {
		return false
	}
	if sticky {
		return true
	}
	return r.held < r.max && !now.Before(r.fullUntil) && !now.Before(r.nextFirst)
}

// resting reports whether r has made LoginBurst attempts in a row and must stay quiet until the
// server's per-address counter expires.
func (p *routePool) resting(r *route, now time.Time) bool {
	return p.burst > 0 && r.burst >= p.burst && now.Sub(r.lastAttempt) < p.quiet
}

// attemptLocked counts one login attempt on r against the server's per-address counter.
func (p *routePool) attemptLocked(r *route, now time.Time) {
	if p.burst <= 0 {
		return
	}
	if now.Sub(r.lastAttempt) >= p.quiet {
		r.burst = 0
	}
	r.burst++
	r.lastAttempt = now
	if r.burst == p.burst {
		p.logf("[route %s] %s %s: %d logins in a row — resting %s so the server's per-address login counter clears", r.id, r.kind, r.host, r.burst, p.quiet)
	}
}

// acquire blocks until a has a route it may attempt a login on, returning it with an attempt
// counted in flight (end it with finish or abandon). It returns false on shutdown or when
// cancelled reports true. setWaiting reports whether a is queued for want of a free route.
func (p *routePool) acquire(done <-chan struct{}, a *account, cancelled func() bool, setWaiting func(bool)) (*route, bool) {
	waiting := false
	defer func() {
		if waiting {
			setWaiting(false)
		}
	}()
	for {
		if cancelled() {
			return nil, false
		}
		p.mu.Lock()
		now := p.now()
		if cur := p.assign[a]; cur != nil {
			if p.admitsLocked(cur, now, true) {
				cur.inflight++
				p.attemptLocked(cur, now)
				p.mu.Unlock()
				return cur, true
			}
			if cur.health == routeUnhealthy {
				// Its route is broken: give the slot back and fail over below.
				p.releaseLocked(a)
			}
		}
		if p.assign[a] == nil {
			pinned := p.pinnedLocked(a)
			for _, r := range p.routes {
				if pinned != nil && r != pinned {
					continue
				}
				if p.admitsLocked(r, now, false) {
					p.assign[a] = r
					r.held++
					r.inflight++
					p.attemptLocked(r, now)
					if p.spacing > 0 {
						r.nextFirst = now.Add(p.spacing)
					}
					p.mu.Unlock()
					return r, true
				}
			}
		}
		// Nothing admits a now: wait for a change, or for the earliest timed hold to lapse.
		wake := p.changed
		next := time.Duration(0)
		for _, r := range p.routes {
			rest := time.Time{}
			if p.resting(r, now) {
				rest = r.lastAttempt.Add(p.quiet)
			}
			for _, t := range []time.Time{r.unhealthyUntil, r.fullUntil, r.nextFirst, rest} {
				if d := t.Sub(now); d > 0 && (next == 0 || d < next) {
					next = d
				}
			}
		}
		// Re-look at least every 30s whatever the holds say, so a far-future hold can never
		// overflow the timer and a cancel is noticed even if no broadcast comes.
		if next == 0 || next > 30*time.Second {
			next = 30 * time.Second
		}
		p.mu.Unlock()
		if !waiting {
			waiting = true
			setWaiting(true)
		}
		var timer <-chan time.Time
		var t *time.Timer
		if next > 0 {
			t = time.NewTimer(next + 10*time.Millisecond)
			timer = t.C
		}
		stop := false
		select {
		case <-done:
			stop = true
		case <-wake:
		case <-timer:
		}
		if t != nil {
			t.Stop()
		}
		if stop {
			return nil, false
		}
	}
}

// abandon ends an acquired attempt that never reached the server (shutdown, a disconnect
// request, the gate closing).
func (p *routePool) abandon(r *route) {
	p.mu.Lock()
	r.inflight--
	p.broadcastLocked()
	p.mu.Unlock()
}

func (p *routePool) releaseLocked(a *account) {
	if r := p.assign[a]; r != nil {
		delete(p.assign, a)
		r.held--
		p.broadcastLocked()
	}
}

// release gives a's slot back (idempotent) and wakes the queue.
func (p *routePool) release(a *account) {
	p.mu.Lock()
	p.releaseLocked(a)
	p.mu.Unlock()
}

// markUnhealthyLocked puts a proxy route into backoff. The direct route is never marked: its
// failures are the server's, and there is nowhere better for its accounts to go.
func (p *routePool) markUnhealthyLocked(r *route, why string) {
	if r.direct {
		return
	}
	r.failStreak++
	backoff := routeUnhealthyBase
	for i := 1; i < r.failStreak && backoff < routeUnhealthyMax; i++ {
		backoff *= 2
	}
	if backoff > routeUnhealthyMax {
		backoff = routeUnhealthyMax
	}
	r.health = routeUnhealthy
	r.unhealthyUntil = p.now().Add(backoff)
	if r.gateState != "" {
		r.gateState = gateFailed
	}
	r.softFails = 0
	r.shortDrops = nil
	r.lastErr = why
	// Accounts on this route that are not connected fail over now; connected ones keep
	// running and fail over only if they drop.
	for a, ar := range p.assign {
		if ar == r && p.up[a] != r {
			delete(p.assign, a)
			r.held--
		}
	}
	p.broadcastLocked()
	p.logf("[route %s] %s %s: UNHEALTHY for %s (failure %d in a row): %s — its waiting accounts are back in the queue", r.id, r.kind, r.host, backoff, r.failStreak, why)
}

// finish records the outcome of a login attempt on r and applies the route-level verdicts.
// It returns nothing: per-account consequences (retry schedule) stay with the caller.
func (p *routePool) finish(a *account, r *route, err error, took time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r.inflight--
	defer p.broadcastLocked()
	if p.burst > 0 {
		// The server counted the attempt somewhere between admission and now; measure the
		// quiet from its end so the wait is never short.
		r.lastAttempt = p.now()
	}

	var rejected *client.LoginRejectedError
	switch {
	case err == nil:
		r.loginsOK++
		r.responses[2]++
		r.lastConnect = took
		r.totalConnect += took
		r.connectSamples++
		if took > r.slowConnect {
			r.slowConnects++
			p.logf("[route %s] %s %s: slow login, %s (tolerance %s)", r.id, r.kind, r.host, took.Round(time.Millisecond), r.slowConnect)
		}
		r.failStreak = 0
		r.softFails = 0
		if r.health != routeHealthy {
			r.health = routeHealthy
			r.lastErr = ""
			p.logf("[route %s] %s %s: healthy again (login ok in %s)", r.id, r.kind, r.host, took.Round(time.Millisecond))
			if r.gateState == gateFailed {
				// A login that was already in flight when the gate failed has just come back
				// OK. The route works, but its latency is unknown: measure again before the
				// next login. Leaving the gate "failed" on a healthy route admitted nobody for
				// ever, since only an unhealthy route re-runs the gate or lets its accounts go
				// (see gateWedged).
				r.gateState = gateUnverified
			}
		}
		if p.assign[a] != r {
			// markUnhealthyLocked gave this account's slot back while its login was in flight.
			// The session is real, so it holds the slot again: held is what admission counts
			// against the route's cap, and a session outside it lets the route overfill.
			p.releaseLocked(a)
			p.assign[a] = r
			r.held++
		}
		r.connected++
		p.up[a] = r
		return
	case proxydial.IsProxyFault(err):
		r.loginsFailed++
		r.proxyFaults++
		p.releaseLocked(a)
		p.markUnhealthyLocked(r, err.Error())
		return
	case errors.As(err, &rejected):
		r.loginsFailed++
		r.responses[rejected.Response]++
		r.softFails = 0
		if r.health == routeProbation {
			// The server answered through this route, so the route itself works.
			r.health = routeHealthy
			r.failStreak = 0
			r.lastErr = ""
			p.logf("[route %s] %s %s: healthy again (the server answered with response %d)", r.id, r.kind, r.host, rejected.Response)
		}
		switch {
		case rejected.Response == client.LoginAttemptsExceededResponse:
			r.gate.Penalize()
			if p.burst > 0 {
				r.burst = p.burst // rest until the counter expires as well
			}
			p.logf("[route %s] %s %s: login rate exceeded (response 16) — this route's logins pause", r.id, r.kind, r.host)
		case rejected.Response == connectionCeilingResponse:
			r.fullUntil = p.now().Add(routeFullHold)
			p.releaseLocked(a)
			p.logf("[route %s] %s %s: server says this address is full (response 9) at %d sessions — no new admissions for %s", r.id, r.kind, r.host, r.connected, routeFullHold)
		case !retryableLoginError(err):
			p.releaseLocked(a)
		}
		return
	default:
		r.loginsFailed++
		r.lastErr = err.Error()
		if !retryableLoginError(err) {
			p.releaseLocked(a)
		}
		if !r.direct {
			r.softFails++
			if r.softFails >= routeSoftFailLimit || r.health == routeProbation {
				p.releaseLocked(a)
				p.markUnhealthyLocked(r, fmt.Sprintf("%d logins in a row failed in transit: %v", max(r.softFails, 1), err))
			}
		}
	}
}

// sessionDown records that a's session ended after lived. Idempotent: only a session finish
// counted as up is counted down. Only an unexpected end counts as a drop; a deliberate logout
// says nothing about the route.
func (p *routePool) sessionDown(a *account, lived time.Duration, unexpected bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.up[a]
	if r == nil {
		return
	}
	delete(p.up, a)
	r.connected--
	defer p.broadcastLocked()
	if !unexpected {
		return
	}
	r.drops++
	now := p.now()
	if r.gateState == gatePassed {
		// Several sessions on one route dropping together means the proxy itself reconnected
		// (Psiphon does, and may come back on a different egress server): its latency is
		// unknown again, so check it before the next login on it.
		if !r.lastDropAt.IsZero() && now.Sub(r.lastDropAt) < routeBurstWindow {
			r.gateState = gateUnverified
			p.logf("[route %s] %s %s: sessions dropped together — the proxy may have reconnected; re-checking latency before the next login", r.id, r.kind, r.host)
		}
	}
	r.lastDropAt = now
	if !r.direct && lived < routeShortSession {
		kept := r.shortDrops[:0]
		for _, t := range r.shortDrops {
			if now.Sub(t) < routeShortDropWindow {
				kept = append(kept, t)
			}
		}
		r.shortDrops = append(kept, now)
		if len(r.shortDrops) >= routeShortDrops && r.health != routeUnhealthy {
			p.markUnhealthyLocked(r, fmt.Sprintf("%d sessions dropped within %s of logging in", len(r.shortDrops), routeShortSession))
		}
	}
}

// routeOf names a's route ("" when it has none).
func (p *routePool) routeOf(a *account) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r := p.assign[a]; r != nil {
		return r.id
	}
	return ""
}

func (p *routePool) statuses() []RouteStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	out := make([]RouteStatus, 0, len(p.routes))
	for _, r := range p.routes {
		p.refreshLocked(r, now)
		st := RouteStatus{ID: r.id, Kind: r.kind, Host: r.host, Max: r.max, Held: r.held, Sessions: r.connected, InFlight: r.inflight,
			Health: string(r.health), LoginsOK: r.loginsOK, LoginsFailed: r.loginsFailed, ProxyFaults: r.proxyFaults, Drops: r.drops,
			SlowConnects: r.slowConnects, LastConnectMs: r.lastConnect.Milliseconds(), LastError: r.lastErr,
			ConnectTimeoutSecs: r.connectTimeout.Seconds()}
		if r.gateState != "" {
			st.LatencyGate = string(r.gateState)
			st.MaxLatencyMs = r.cfg.MaxLatency.Milliseconds()
			st.LatencyMedianMs = r.latencyMedian.Milliseconds()
			for _, d := range r.latencySamples {
				st.LatencySamplesMs = append(st.LatencySamplesMs, d.Milliseconds())
			}
			if !r.latencyAt.IsZero() {
				st.LatencyAtMs = r.latencyAt.UnixMilli()
			}
			st.LatencyChecks, st.LatencyFails = r.latencyChecks, r.latencyFails
		}
		if r.cfg.TorIsolation {
			st.Generation = r.generation
		}
		if r.connectSamples > 0 {
			st.AvgConnectMs = (r.totalConnect / time.Duration(r.connectSamples)).Milliseconds()
		}
		if r.health == routeUnhealthy {
			st.UnhealthyUntilMs = r.unhealthyUntil.UnixMilli()
		}
		if p.resting(r, now) {
			st.RestUntilMs = r.lastAttempt.Add(p.quiet).UnixMilli()
		}
		if now.Before(r.fullUntil) {
			st.FullUntilMs = r.fullUntil.UnixMilli()
		}
		if r.gate != nil {
			r.gate.mu.Lock()
			if now.Before(r.gate.penalizedUntil) {
				st.PenalizedUntilMs = r.gate.penalizedUntil.UnixMilli()
			}
			r.gate.mu.Unlock()
		}
		if len(r.responses) > 0 {
			st.Responses = make(map[string]int64, len(r.responses))
			codes := make([]int, 0, len(r.responses))
			for c := range r.responses {
				codes = append(codes, c)
			}
			sort.Ints(codes)
			for _, c := range codes {
				st.Responses[strconv.Itoa(c)] = r.responses[c]
			}
		}
		out = append(out, st)
	}
	return out
}

// close stops the gates the pool created (not the Manager's own direct gate).
func (p *routePool) close() {
	p.closeOnce.Do(func() { close(p.closed) })
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.routes {
		if r.ownGate {
			r.gate.Close()
		}
	}
}

// describe is the startup line for one route.
func (r *route) describe() string {
	if r.direct {
		return fmt.Sprintf("[route %s] home address, max %d sessions", r.id, r.max)
	}
	dns := "remote DNS"
	if r.localDNS {
		dns = "local DNS"
	}
	line := fmt.Sprintf("[route %s] %s via %s (%s), max %d sessions, connect timeout %s, slow over %s", r.id, r.kind, r.host, dns, r.max, r.connectTimeout, r.slowConnect)
	if r.cfg.TorIsolation {
		line += ", Tor circuit isolation"
	}
	if r.cfg.MaxLatency > 0 {
		line += fmt.Sprintf(", latency gate: median of %d under %s", r.cfg.LatencySamples, r.cfg.MaxLatency)
	}
	return line
}
