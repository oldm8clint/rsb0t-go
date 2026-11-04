package fleet

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"sort"
	"time"

	"rsb0t-go/internal/proxydial"
)

// This file is the latency gate a proxy route can be given (routeconfig.go: "latencyGate",
// "maxLatencyMs"). It exists for Tor, whose circuits vary from tens of milliseconds to
// seconds: a route has to show a usable round trip to the game host before any account logs
// in through it, and again each time it comes back from an unhealthy spell.
//
// One sample is a TCP connect through the route to the game host plus, for a wss URL, a TLS
// handshake — the connect costs one round trip through the proxy (the SOCKS CONNECT reply
// only comes back once the far end has connected) and a TLS 1.3 handshake one more. No game
// login is made and nothing is sent after the handshake. The route passes when the median of
// latencySamples samples is at most maxLatencyMs, with at least half of them succeeding.
//
// On a failure the route retries up to latencyRetries times, latency.retryPause apart. With
// torIsolation each retry first rotates the route's SOCKS username (its "generation"), which
// makes Tor build a fresh circuit for it — so a slow exit is swapped for another rather than
// re-measured. Running out of retries marks the route unhealthy with the ordinary backoff;
// probation afterwards re-runs the gate.
//
// Not measured: the in-game round trip once logged in. The protocol has no ping/pong the
// client can time, and a login's own duration (routes.go's slowConnects) is the nearest proxy.

type gateState string

const (
	gateUnverified gateState = "pending"
	gateChecking   gateState = "checking"
	gatePassed     gateState = "passed"
	gateFailed     gateState = "failed"
)

// probeTargetOf turns the game URL into the host:port the gate dials, the TLS server name, and
// whether each sample includes a TLS handshake.
func probeTargetOf(wsURL string) (target, serverName string, useTLS bool) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Hostname() == "" {
		return "", "", false
	}
	useTLS = u.Scheme == "wss" || u.Scheme == "https"
	port := u.Port()
	if port == "" {
		port = "80"
		if useTLS {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), u.Hostname(), useTLS
}

// startProbeLocked starts the gate check for r in the background; the caller holds p.mu.
func (p *routePool) startProbeLocked(r *route) {
	if r.gateState != gateUnverified {
		return
	}
	r.gateState = gateChecking
	p.logf("[route %s] %s %s: checking latency (median of %d under %s)", r.id, r.kind, r.host, r.cfg.LatencySamples, r.cfg.MaxLatency)
	go p.runProbe(r)
}

func (p *routePool) runProbe(r *route) {
	for attempt := 0; ; attempt++ {
		samples, err := p.probe(r)
		p.mu.Lock()
		r.latencyChecks++
		r.latencyAt = p.now()
		r.latencySamples = samples
		r.latencyMedian = medianDuration(samples)
		need := (r.cfg.LatencySamples + 1) / 2
		if err == nil && len(samples) >= need && r.latencyMedian <= r.cfg.MaxLatency {
			r.gateState = gatePassed
			p.logf("[route %s] %s %s: latency ok, median %s over %d samples (limit %s)%s", r.id, r.kind, r.host,
				r.latencyMedian.Round(time.Millisecond), len(samples), r.cfg.MaxLatency, r.generationNote())
			p.broadcastLocked()
			p.mu.Unlock()
			return
		}
		r.latencyFails++
		var why string
		switch {
		case err != nil:
			why = fmt.Sprintf("latency check failed: %v", err)
		case len(samples) < need:
			why = fmt.Sprintf("latency check: only %d of %d samples succeeded", len(samples), r.cfg.LatencySamples)
		default:
			why = fmt.Sprintf("latency median %s over %d samples is above the %s limit", r.latencyMedian.Round(time.Millisecond), len(samples), r.cfg.MaxLatency)
		}
		select {
		case <-p.closed:
			p.mu.Unlock()
			return
		default:
		}
		if proxydial.IsProxyFault(err) || attempt >= r.cfg.LatencyRetries {
			// A proxy that cannot even be reached is not going to get faster.
			p.markUnhealthyLocked(r, why)
			p.mu.Unlock()
			return
		}
		note := ""
		if r.cfg.TorIsolation {
			r.generation++
			if link, lerr := r.newLink(); lerr == nil {
				r.link.Store(link)
				note = fmt.Sprintf(" — new isolation credentials (generation %d) for a fresh Tor circuit", r.generation)
			}
		}
		p.logf("[route %s] %s %s: %s; retry %d of %d%s", r.id, r.kind, r.host, why, attempt+1, r.cfg.LatencyRetries, note)
		p.mu.Unlock()
		select {
		case <-p.closed:
			return
		case <-time.After(p.retryPause):
		}
	}
}

func (r *route) generationNote() string {
	if !r.cfg.TorIsolation {
		return ""
	}
	return fmt.Sprintf(", circuit generation %d", r.generation)
}

// measure takes r.cfg.LatencySamples samples through the route's current dialer. It stops early
// on a proxy fault (the route is down) and returns the samples that succeeded otherwise.
func (p *routePool) measure(r *route) ([]time.Duration, error) {
	link := r.currentLink()
	if link == nil || p.probeTarget == "" {
		return nil, fmt.Errorf("no probe target")
	}
	var out []time.Duration
	var lastErr error
	for i := 0; i < r.cfg.LatencySamples; i++ {
		select {
		case <-p.closed:
			return out, fmt.Errorf("shutting down")
		default:
		}
		d, err := p.sample(link.dialer, r.connectTimeout)
		if err != nil {
			if proxydial.IsProxyFault(err) {
				return out, err
			}
			lastErr = err
			continue
		}
		out = append(out, d)
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// sample is one connect (plus TLS handshake for wss) through d.
func (p *routePool) sample(d *proxydial.Dialer, timeout time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", p.probeTarget)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if p.probeTLS {
		tc := tls.Client(conn, &tls.Config{ServerName: p.probeName, MinVersion: tls.VersionTLS12})
		if err := tc.HandshakeContext(ctx); err != nil {
			return 0, fmt.Errorf("tls handshake: %w", err)
		}
		defer tc.Close()
	}
	return time.Since(start), nil
}

func medianDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
