// Package proxydial opens TCP tunnels through an operator-supplied proxy: SOCKS5 (RFC 1928,
// with RFC 1929 username/password authentication) or HTTP CONNECT. It exists so the fleet can
// send accounts beyond one address's connection cap out through other addresses.
//
// A Dialer only builds the tunnel. Whatever runs over it — the game WebSocket, TLS for a wss
// URL, an HTTP request for /crc0 — runs end to end through the tunnel exactly as it would over
// a direct TCP connection; the proxy sees only the bytes it would see on the wire.
//
// Credentials never leave this package in text: String, Error and every log-facing value name
// the proxy by its type and host:port only. Errors are *Error, which also says whether the
// failure was the proxy's own (so a caller can mark the route unhealthy) or the target's (the
// proxy worked, the game server refused — not the proxy's fault).
package proxydial

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Kind is the proxy protocol.
type Kind string

const (
	SOCKS5 Kind = "socks5"
	HTTP   Kind = "http"
)

// DefaultHandshakeTimeout bounds the TCP connect to the proxy plus its whole handshake when the
// caller's context has no earlier deadline.
const DefaultHandshakeTimeout = 20 * time.Second

// keepAlive is the TCP keepalive period on the connection to the proxy. The game protocol is
// never idle for long (the server writes every tick, the client a NO_TIMEOUT after a second
// of silence), so this only matters for noticing a proxy that vanished without a FIN.
const keepAlive = 15 * time.Second

// Dialer dials targets through one proxy. The zero value is not usable; build one with New.
type Dialer struct {
	kind     Kind
	address  string // host:port of the proxy, as configured
	username string
	password string

	// LocalDNS resolves a target host name here and sends the proxy an IPv4 address instead
	// of the name. Some proxies refuse names (Cloudflare WARP's local SOCKS5 proxy closed
	// every remote-DNS CONNECT in the owner's test, 0/10, and accepted every IP one, 10/10).
	// TLS and the WebSocket Host header still use the real host name: they come from the
	// URL, not from the tunnel's address. False sends the name for the proxy to resolve.
	LocalDNS bool
	// Lookup resolves for LocalDNS; nil uses net.DefaultResolver. Replaced in tests.
	Lookup func(ctx context.Context, host string) ([]net.IPAddr, error)

	// forward dials the proxy itself; replaced in tests.
	forward func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New validates the settings and returns a Dialer. A password without a username (or the
// reverse) is refused, since SOCKS5 has no way to send just one of them.
func New(kind Kind, address, username, password string) (*Dialer, error) {
	switch kind {
	case SOCKS5, HTTP:
	default:
		return nil, fmt.Errorf("proxydial: unknown proxy type %q (want %q or %q)", kind, SOCKS5, HTTP)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("proxydial: address %q is not host:port", address)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("proxydial: address %q has no valid port", address)
	}
	if (username == "") != (password == "") {
		return nil, fmt.Errorf("proxydial: %s %s needs both a username and a password, or neither", kind, address)
	}
	if kind == SOCKS5 && (len(username) > 255 || len(password) > 255) {
		return nil, fmt.Errorf("proxydial: %s %s: SOCKS5 usernames and passwords are at most 255 bytes", kind, address)
	}
	d := &net.Dialer{KeepAlive: keepAlive}
	return &Dialer{kind: kind, address: address, username: username, password: password, forward: d.DialContext}, nil
}

// Kind returns the proxy protocol.
func (d *Dialer) Kind() Kind { return d.kind }

// Address returns the configured proxy host:port (no credentials).
func (d *Dialer) Address() string { return d.address }

// Host returns the proxy's host name as configured, for a log or panel label.
func (d *Dialer) Host() string {
	host, _, _ := net.SplitHostPort(d.address)
	return host
}

// String names the proxy without its credentials.
func (d *Dialer) String() string { return string(d.kind) + "://" + d.address }

// GoString keeps %#v from printing the credentials.
func (d *Dialer) GoString() string { return "proxydial.Dialer(" + d.String() + ")" }

// Stage says how far a failed dial got.
type Stage string

const (
	StageConnect   Stage = "connect"   // could not reach the proxy
	StageAuth      Stage = "auth"      // the proxy refused our credentials or offered no usable method
	StageHandshake Stage = "handshake" // a protocol error or a proxy-side failure answering CONNECT
	StageTarget    Stage = "target"    // the proxy worked and the target refused the connection
	StageResolve   Stage = "resolve"   // LocalDNS could not resolve the target here
)

// Error is every error a Dialer returns. It never contains credentials.
type Error struct {
	Proxy string // "socks5://host:port"
	Stage Stage
	Err   error
}

func (e *Error) Error() string {
	return fmt.Sprintf("proxy %s: %s: %v", e.Proxy, e.Stage, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// ProxyFault reports whether the proxy itself is to blame: anything but the target refusing
// or a local lookup of the target failing.
func (e *Error) ProxyFault() bool { return e.Stage != StageTarget && e.Stage != StageResolve }

// Timeout lets net.Error-style checks see a handshake that ran out of time.
func (e *Error) Timeout() bool {
	var ne net.Error
	return errors.As(e.Err, &ne) && ne.Timeout()
}

// IsProxyFault reports whether err is a Dialer error blaming the proxy.
func IsProxyFault(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.ProxyFault()
}

func (d *Dialer) fail(stage Stage, err error) error {
	return &Error{Proxy: d.String(), Stage: stage, Err: err}
}

// DialContext opens a TCP tunnel to target (host:port) through the proxy. The host name is
// sent to the proxy unresolved, so the proxy does the lookup, unless LocalDNS is set. The
// returned connection has no deadline set.
func (d *Dialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, d.fail(StageHandshake, fmt.Errorf("network %q is not supported", network))
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHandshakeTimeout)
		defer cancel()
	}
	if d.LocalDNS {
		resolved, err := d.resolveTarget(ctx, target)
		if err != nil {
			return nil, err
		}
		target = resolved
	}
	conn, err := d.forward(ctx, "tcp", d.address)
	if err != nil {
		return nil, d.fail(StageConnect, err)
	}
	// The handshake honours ctx through the connection deadline, and a cancel mid-handshake
	// closes the connection so a blocked read returns.
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	var tunnel net.Conn
	switch d.kind {
	case SOCKS5:
		err = d.socks5(conn, target)
		tunnel = conn
	case HTTP:
		tunnel, err = d.connect(conn, target)
	}
	if !stop() && err == nil {
		err = d.fail(StageHandshake, ctx.Err())
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return tunnel, nil
}

// resolveTarget turns host:port into ipv4:port for LocalDNS. Only IPv4 is used: the game server
// has no AAAA record and the owner's connection has no IPv6. A literal IP passes through.
func (d *Dialer) resolveTarget(ctx context.Context, target string) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", d.fail(StageHandshake, fmt.Errorf("target %q is not host:port", target))
	}
	if ip := net.ParseIP(host); ip != nil {
		return target, nil
	}
	lookup := d.Lookup
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return "", d.fail(StageResolve, err)
	}
	for _, a := range addrs {
		if ip4 := a.IP.To4(); ip4 != nil {
			return net.JoinHostPort(ip4.String(), port), nil
		}
	}
	return "", d.fail(StageResolve, fmt.Errorf("%s has no IPv4 address", host))
}

// SOCKS5 reply codes (RFC 1928 section 6).
var socksReplies = map[byte]string{
	1: "general SOCKS server failure",
	2: "connection not allowed by ruleset",
	3: "network unreachable",
	4: "host unreachable",
	5: "connection refused",
	6: "TTL expired",
	7: "command not supported",
	8: "address type not supported",
}

func (d *Dialer) socks5(conn net.Conn, target string) error {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return d.fail(StageHandshake, fmt.Errorf("target %q is not host:port", target))
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return d.fail(StageHandshake, fmt.Errorf("target %q has no valid port", target))
	}

	// Greeting: offer username/password only when we hold credentials, so a proxy that
	// requires them cannot silently accept "no authentication" and a proxy that does not is
	// never sent them.
	method := byte(0x00)
	if d.username != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{5, 1, method}); err != nil {
		return d.fail(StageHandshake, err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return d.fail(StageHandshake, err)
	}
	if reply[0] != 5 {
		return d.fail(StageHandshake, fmt.Errorf("not a SOCKS5 server (version byte %d)", reply[0]))
	}
	switch {
	case reply[1] == 0xFF && method == 0x02:
		return d.fail(StageAuth, errors.New("proxy does not accept username/password authentication"))
	case reply[1] == 0xFF:
		return d.fail(StageAuth, errors.New("proxy requires authentication and no credentials are configured"))
	case reply[1] != method:
		return d.fail(StageAuth, fmt.Errorf("proxy chose authentication method %d, which was not offered", reply[1]))
	}
	if method == 0x02 {
		// RFC 1929: VER=1, ULEN, UNAME, PLEN, PASSWD.
		msg := make([]byte, 0, 3+len(d.username)+len(d.password))
		msg = append(msg, 1, byte(len(d.username)))
		msg = append(msg, d.username...)
		msg = append(msg, byte(len(d.password)))
		msg = append(msg, d.password...)
		_, err := conn.Write(msg)
		clear(msg)
		if err != nil {
			return d.fail(StageAuth, err)
		}
		if _, err := io.ReadFull(conn, reply[:]); err != nil {
			return d.fail(StageAuth, err)
		}
		if reply[1] != 0 {
			return d.fail(StageAuth, errors.New("proxy rejected the username/password"))
		}
	}

	// CONNECT request. A literal IP goes as an address, anything else as a domain name for
	// the proxy to resolve.
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return d.fail(StageHandshake, errors.New("target host name is longer than 255 bytes"))
		}
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return d.fail(StageHandshake, err)
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return d.fail(StageHandshake, err)
	}
	if head[0] != 5 {
		return d.fail(StageHandshake, fmt.Errorf("bad reply version %d", head[0]))
	}
	if head[1] != 0 {
		msg, ok := socksReplies[head[1]]
		if !ok {
			msg = fmt.Sprintf("reply code %d", head[1])
		}
		stage := StageHandshake
		if head[1] == 5 {
			stage = StageTarget
		}
		return d.fail(stage, errors.New(msg))
	}
	// Skip the bound address, whose length depends on its type.
	var skip int
	switch head[3] {
	case 1:
		skip = 4
	case 4:
		skip = 16
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return d.fail(StageHandshake, err)
		}
		skip = int(n[0])
	default:
		return d.fail(StageHandshake, fmt.Errorf("bad bound address type %d", head[3]))
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip+2)); err != nil {
		return d.fail(StageHandshake, err)
	}
	return nil
}

func (d *Dialer) connect(conn net.Conn, target string) (net.Conn, error) {
	if _, _, err := net.SplitHostPort(target); err != nil {
		return nil, d.fail(StageHandshake, fmt.Errorf("target %q is not host:port", target))
	}
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if d.username != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(d.username+":"+d.password)) + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return nil, d.fail(StageHandshake, err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, d.fail(StageHandshake, err)
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return nil, d.fail(StageAuth, errors.New("proxy rejected the credentials (407)"))
	case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusServiceUnavailable:
		// The proxy could not reach the target. Ambiguous (the proxy's own network or the
		// game server), so it is counted against the proxy: that route is not getting through.
		return nil, d.fail(StageHandshake, fmt.Errorf("proxy could not reach the target (%s)", resp.Status))
	default:
		return nil, d.fail(StageHandshake, fmt.Errorf("proxy answered CONNECT with %s", resp.Status))
	}
	if br.Buffered() > 0 {
		// The target's first bytes arrived in the same read as the proxy's reply.
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.r != nil {
		if c.r.Buffered() > 0 {
			return c.r.Read(p)
		}
		c.r = nil
	}
	return c.Conn.Read(p)
}

// HTTPClient returns an http.Client whose connections all go through the proxy (TLS for an
// https URL runs end to end through the tunnel). Environment proxy settings are ignored, and
// no connection is kept idle: the fleet fetches one small file per login at most, so a tunnel
// lives only as long as its request.
func (d *Dialer) HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         d.DialContext,
			ForceAttemptHTTP2:   false,
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: DefaultHandshakeTimeout,
		},
	}
}
