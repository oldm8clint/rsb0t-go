package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"rsb0t-go/internal/config"
	"rsb0t-go/internal/proxydial"
)

// This file reads the proxy pool: the routes accounts beyond one address's connection cap log
// in through. The pool lives in its own gitignored file (proxies*.json), never in the repo or
// in fleet.config.json, and its passwords are config.Secret so every print says [redacted].
//
// The file:
//
//	{
//	  "direct": {"max": 89},
//	  "routes": [
//	    {"id": "warp", "type": "socks5", "addr": "127.0.0.1:40000", "max": 89},
//	    {"id": "paid-1", "type": "socks5", "addr": "proxy.example.net:1080",
//	     "username": "svc-user", "passwordEnv": "RSB0T_PROXY_PAID_1", "max": 89},
//	    {"id": "tor-1", "type": "socks5", "addr": "127.0.0.1:9050", "max": 89,
//	     "torIsolation": true, "latencyGate": true, "connectTimeoutSeconds": 60},
//	    {"id": "tor-2", "type": "socks5", "addr": "127.0.0.1:9050", "max": 89,
//	     "torIsolation": true, "latencyGate": true, "maxLatencyMs": 600, "enabled": false}
//	  ]
//	}
//
// Tor. One Tor process serves several routes: Tor's IsolateSOCKSAuth (on by default) puts each
// distinct SOCKS username/password pair on its own circuit, so each route gets its own exit.
// "torIsolation": true gives the route fixed dummy credentials of its own (its id, unless a
// username is set) and, when the latency gate fails, rotates them to ask Tor for a fresh
// circuit. Two routes may share an addr only with different usernames.
//
// The latency gate. "latencyGate": true (or any "maxLatencyMs") makes the route prove itself
// before it admits anyone, and again after every unhealthy spell: latencySamples (default 5)
// TCP connects through it to the game host, each plus a TLS handshake for a wss URL; the
// median must be at most maxLatencyMs (default 400). A failing route retries latencyRetries
// times (default 3, each with fresh isolation credentials when torIsolation is on), then goes
// unhealthy with the usual backoff.
//
// "direct" is the home address, always tried first; "enabled": false there sends everything
// through the routes. "dns" is "local" (resolve the game host here and send the proxy an IPv4
// address — Cloudflare WARP's proxy needs this) or "remote" (send the name; the proxy resolves
// it, which Tor wants). It defaults to local for a route with no username and remote for one
// with credentials or torIsolation. Unknown keys are errors, as in internal/config.

// DefaultRouteMax is the per-address cap the live server rules state (2026-09-19): at most 90
// accounts logged in at once per IP address. One under, so a reconnect overlapping a
// not-yet-expired server-side session never tips an address over.
const DefaultRouteMax = 89

// Defaults for a proxy route's connect budget and "slow" threshold. A direct route with no
// pool keeps today's unbounded dial; a direct route inside a pool uses the same defaults.
const (
	defaultRouteConnectTimeout = 20 * time.Second
	defaultRouteSlowConnect    = 3 * time.Second
)

// RoutePool is a parsed, validated pool file.
type RoutePool struct {
	Direct DirectRoute
	Routes []ProxyRoute

	// Spacing is the least time between two FIRST logins on one route (not in the file; the
	// caller sets it from its login delay). Routes pace independently, so with N routes the
	// fleet ramps N times as fast while each address still sees the usual pace — the way
	// players on different addresses log in side by side. Reconnects of an account already
	// holding a slot are paced by the route's login gate alone, as before. Zero: no spacing.
	Spacing time.Duration

	// LoginBurst and LoginQuiet follow the server's per-address login counter (Engine-TS
	// World.loginAddressAttempts): every attempt from an address, reconnects included, adds one
	// and restarts a 60s expiry, and the attempt that reaches the limit (30 on the live server, seen
	// live) is refused with response 16 whatever the pacing. So a route makes at most
	// LoginBurst attempts in a row, then stays quiet for LoginQuiet after its last one so the
	// counter expires, instead of tripping response 16 and its longer penalty. Zero: no burst
	// limit (the caller sets these; they are not in the file).
	LoginBurst int
	LoginQuiet time.Duration

	// Pins, when set, ties each account (by PinKey) to one route id, so it always logs in from
	// the same address and never fails over (see PlanRoutePins). Not in the file; the caller
	// builds it. Nil: every account takes the first route with room, as before.
	Pins map[string]string
}

// DirectRoute is the home address.
type DirectRoute struct {
	Enabled        bool
	Max            int
	ConnectTimeout time.Duration
	SlowConnect    time.Duration
}

// ProxyRoute is one proxy. Password prints as [redacted].
type ProxyRoute struct {
	ID             string
	Type           proxydial.Kind
	Addr           string
	Username       string
	Password       config.Secret
	LocalDNS       bool
	Max            int
	Enabled        bool
	ConnectTimeout time.Duration
	SlowConnect    time.Duration

	// TorIsolation rotates the SOCKS username on a failed latency gate (see the file doc).
	TorIsolation bool
	// MaxLatency > 0 turns the latency gate on; LatencySamples and LatencyRetries tune it.
	MaxLatency     time.Duration
	LatencySamples int
	LatencyRetries int
}

// Latency gate defaults.
const (
	DefaultMaxLatency     = 400 * time.Millisecond
	defaultLatencySamples = 5
	defaultLatencyRetries = 3
)

// torIsolationPassword is the dummy SOCKS password a torIsolation route uses when none is
// given. It authenticates nothing: Tor accepts any pair and uses it only to pick a circuit.
const torIsolationPassword = "rsb0t-isolation"

// String names the route without credentials.
func (p ProxyRoute) String() string { return p.ID + " (" + string(p.Type) + " " + p.Addr + ")" }

// GoString keeps %#v from printing the credentials.
func (p ProxyRoute) GoString() string { return "fleet.ProxyRoute(" + p.String() + ")" }

// MarshalJSON writes the route without its username or password.
func (p ProxyRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Addr string `json:"addr"`
		Max  int    `json:"max"`
	}{p.ID, string(p.Type), p.Addr, p.Max})
}

type routeFileJSON struct {
	Direct *routeCommonJSON `json:"direct"`
	Routes []routeJSON      `json:"routes"`
}

type routeCommonJSON struct {
	Max                   *int     `json:"max"`
	Enabled               *bool    `json:"enabled"`
	ConnectTimeoutSeconds *float64 `json:"connectTimeoutSeconds"`
	SlowConnectMs         *int     `json:"slowConnectMs"`
}

type routeJSON struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Addr        string `json:"addr"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PasswordEnv string `json:"passwordEnv"`
	DNS         string `json:"dns"`

	TorIsolation   bool `json:"torIsolation"`
	LatencyGate    bool `json:"latencyGate"`
	MaxLatencyMs   *int `json:"maxLatencyMs"`
	LatencySamples *int `json:"latencySamples"`
	LatencyRetries *int `json:"latencyRetries"`
	routeCommonJSON
}

var routeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)

// LoadRoutePool reads and validates a pool file. Errors name the route and field, never a
// password.
func LoadRoutePool(path string) (*RoutePool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fleet: proxy pool: %w", err)
	}
	return ParseRoutePool(data, os.Getenv)
}

// ParseRoutePool is LoadRoutePool over bytes, with getenv for passwordEnv.
func ParseRoutePool(data []byte, getenv func(string) string) (*RoutePool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f routeFileJSON
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("fleet: proxy pool: %s", scrubJSONError(err))
	}
	if dec.More() {
		return nil, fmt.Errorf("fleet: proxy pool: trailing data after the pool object")
	}

	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	pool := &RoutePool{Direct: DirectRoute{Enabled: true, Max: DefaultRouteMax, ConnectTimeout: defaultRouteConnectTimeout, SlowConnect: defaultRouteSlowConnect}}
	if f.Direct != nil {
		c := f.Direct
		applyCommon(c, "direct", &pool.Direct.Max, &pool.Direct.Enabled, &pool.Direct.ConnectTimeout, &pool.Direct.SlowConnect, bad)
	}

	ids := map[string]bool{"direct": true}
	addrs := map[string]string{}
	for i, r := range f.Routes {
		where := fmt.Sprintf("routes[%d]", i)
		if r.ID != "" {
			where += " (" + r.ID + ")"
		}
		switch {
		case !routeIDPattern.MatchString(r.ID):
			bad("%s: id must be 1-32 letters, digits, '.', '_' or '-'", where)
		case ids[strings.ToLower(r.ID)]:
			bad("%s: id is used twice (\"direct\" is reserved)", where)
		}
		ids[strings.ToLower(r.ID)] = true
		p := ProxyRoute{ID: r.ID, Type: proxydial.Kind(strings.ToLower(r.Type)), Addr: r.Addr, Username: r.Username,
			Max: DefaultRouteMax, Enabled: true, ConnectTimeout: defaultRouteConnectTimeout, SlowConnect: defaultRouteSlowConnect}
		applyCommon(&r.routeCommonJSON, where, &p.Max, &p.Enabled, &p.ConnectTimeout, &p.SlowConnect, bad)
		switch {
		case r.Password != "" && r.PasswordEnv != "":
			bad("%s: give password or passwordEnv, not both", where)
		case r.PasswordEnv != "":
			pw := getenv(r.PasswordEnv)
			if pw == "" {
				if p.Enabled {
					bad("%s: passwordEnv %s is not set", where, r.PasswordEnv)
				}
				// A disabled route is never dialled; it need not have its secret yet.
				pw = "unset"
			}
			p.Password = config.Secret(pw)
		default:
			p.Password = config.Secret(r.Password)
		}
		p.TorIsolation = r.TorIsolation
		if p.TorIsolation && p.Username == "" {
			if p.Password != "" {
				bad("%s: a password needs a username", where)
			}
			p.Username = r.ID
			p.Password = torIsolationPassword
		}
		if r.LatencyGate || r.MaxLatencyMs != nil {
			p.MaxLatency = DefaultMaxLatency
			p.LatencySamples = defaultLatencySamples
			p.LatencyRetries = defaultLatencyRetries
		}
		if r.MaxLatencyMs != nil {
			if *r.MaxLatencyMs < 1 || *r.MaxLatencyMs > 60000 {
				bad("%s: maxLatencyMs must be 1-60000, got %d", where, *r.MaxLatencyMs)
			}
			p.MaxLatency = time.Duration(*r.MaxLatencyMs) * time.Millisecond
		}
		if r.LatencySamples != nil {
			if *r.LatencySamples < 1 || *r.LatencySamples > 21 || p.MaxLatency == 0 {
				bad("%s: latencySamples must be 1-21, with the latency gate on", where)
			}
			p.LatencySamples = *r.LatencySamples
		}
		if r.LatencyRetries != nil {
			if *r.LatencyRetries < 0 || *r.LatencyRetries > 10 || p.MaxLatency == 0 {
				bad("%s: latencyRetries must be 0-10, with the latency gate on", where)
			}
			p.LatencyRetries = *r.LatencyRetries
		}
		switch strings.ToLower(r.DNS) {
		case "":
			p.LocalDNS = r.Username == "" && !r.TorIsolation
		case "local":
			p.LocalDNS = true
		case "remote":
			p.LocalDNS = false
		default:
			bad("%s: dns must be \"local\" or \"remote\", got %q", where, r.DNS)
		}
		// proxydial.New validates type, address and the credential pair; its errors never
		// include the password.
		if _, err := proxydial.New(p.Type, p.Addr, p.Username, p.Password.Reveal()); err != nil {
			bad("%s: %s", where, strings.TrimPrefix(err.Error(), "proxydial: "))
		}
		// One proxy address is one egress address — unless the routes are isolated from each
		// other by credentials (Tor's IsolateSOCKSAuth), which is the only reason to list an
		// address twice.
		if key := strings.ToLower(r.Addr) + "|" + p.Username; r.Addr != "" {
			if other, ok := addrs[key]; ok {
				bad("%s: addr and username are also used by %s — list a proxy once, or give each route its own username (torIsolation does this)", where, other)
			}
			addrs[key] = where
		}
		pool.Routes = append(pool.Routes, p)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("fleet: proxy pool: %s", strings.Join(problems, "; "))
	}
	if !pool.Direct.Enabled && enabledRoutes(pool) == 0 {
		return nil, fmt.Errorf("fleet: proxy pool: direct is disabled and no route is enabled, so nothing could log in")
	}
	return pool, nil
}

func enabledRoutes(p *RoutePool) int {
	n := 0
	for _, r := range p.Routes {
		if r.Enabled {
			n++
		}
	}
	return n
}

func applyCommon(c *routeCommonJSON, where string, max *int, enabled *bool, connect, slow *time.Duration, bad func(string, ...any)) {
	if c.Max != nil {
		if *c.Max < 1 || *c.Max > 10000 {
			bad("%s: max must be 1-10000, got %d", where, *c.Max)
		}
		*max = *c.Max
	}
	if c.Enabled != nil {
		*enabled = *c.Enabled
	}
	if c.ConnectTimeoutSeconds != nil {
		if *c.ConnectTimeoutSeconds < 1 || *c.ConnectTimeoutSeconds > 300 {
			bad("%s: connectTimeoutSeconds must be 1-300, got %g", where, *c.ConnectTimeoutSeconds)
		}
		*connect = time.Duration(*c.ConnectTimeoutSeconds * float64(time.Second))
	}
	if c.SlowConnectMs != nil {
		if *c.SlowConnectMs < 1 || *c.SlowConnectMs > 300000 {
			bad("%s: slowConnectMs must be 1-300000, got %d", where, *c.SlowConnectMs)
		}
		*slow = time.Duration(*c.SlowConnectMs) * time.Millisecond
	}
}

// scrubJSONError keeps a decode error from echoing file content: a *json.UnmarshalTypeError or
// *json.SyntaxError never carries values, but an unknown-field error names only the key.
func scrubJSONError(err error) string {
	switch e := err.(type) {
	case *json.SyntaxError:
		return fmt.Sprintf("not valid JSON at byte %d", e.Offset)
	case *json.UnmarshalTypeError:
		return fmt.Sprintf("field %s wants a %s", e.Field, e.Type)
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "json: unknown field ") {
		return strings.TrimPrefix(msg, "json: ")
	}
	return "could not be read as a proxy pool"
}
