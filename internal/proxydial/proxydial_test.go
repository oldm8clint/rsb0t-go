package proxydial_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/proxydial"
	"rsb0t-go/internal/proxydial/proxytest"
)

const (
	testUser = "svc-user"
	testPass = "s3cret-pass-do-not-print"
)

// echoServer accepts connections and echoes every byte back.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String()
}

func roundTrip(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo through tunnel: %q %v", buf, err)
	}
}

func newProxy(t *testing.T, kind proxydial.Kind, user, pass string) *proxytest.Server {
	t.Helper()
	var s *proxytest.Server
	var err error
	if kind == proxydial.SOCKS5 {
		s, err = proxytest.NewSOCKS5(user, pass)
	} else {
		s, err = proxytest.NewHTTP(user, pass)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestTunnelWithAuthentication(t *testing.T) {
	target := echoServer(t)
	for _, kind := range []proxydial.Kind{proxydial.SOCKS5, proxydial.HTTP} {
		t.Run(string(kind), func(t *testing.T) {
			srv := newProxy(t, kind, testUser, testPass)
			d, err := proxydial.New(kind, srv.Addr(), testUser, testPass)
			if err != nil {
				t.Fatal(err)
			}
			c, err := d.DialContext(context.Background(), "tcp", target)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundTrip(t, c)
			if srv.Opened() != 1 || srv.Active() != 1 {
				t.Fatalf("opened=%d active=%d, want 1/1", srv.Opened(), srv.Active())
			}
		})
	}
}

func TestSOCKS5SendsHostNamesUnresolved(t *testing.T) {
	target := echoServer(t)
	_, port, _ := net.SplitHostPort(target)
	srv := newProxy(t, proxydial.SOCKS5, testUser, testPass)
	d, _ := proxydial.New(proxydial.SOCKS5, srv.Addr(), testUser, testPass)
	c, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("localhost", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundTrip(t, c)
	if got := srv.Requests(net.JoinHostPort("localhost", port)); got != 1 {
		t.Fatalf("proxy saw %d CONNECTs for the host name, want 1 (the name must reach the proxy unresolved)", got)
	}
}

func TestAuthFailuresAreProxyFaultsAndNeverPrintTheSecret(t *testing.T) {
	target := echoServer(t)
	for _, kind := range []proxydial.Kind{proxydial.SOCKS5, proxydial.HTTP} {
		for _, tc := range []struct{ name, user, pass string }{
			{"wrong password", testUser, "wrong-" + testPass},
			{"no credentials", "", ""},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				srv := newProxy(t, kind, testUser, testPass)
				d, err := proxydial.New(kind, srv.Addr(), tc.user, tc.pass)
				if err != nil {
					t.Fatal(err)
				}
				_, err = d.DialContext(context.Background(), "tcp", target)
				var pe *proxydial.Error
				if !errors.As(err, &pe) || pe.Stage != proxydial.StageAuth || !pe.ProxyFault() {
					t.Fatalf("got %v, want an auth-stage proxy fault", err)
				}
				for _, s := range []string{err.Error(), fmt.Sprintf("%v %+v %#v %s", d, d, d, d)} {
					if strings.Contains(s, testPass) || strings.Contains(s, testUser) {
						t.Fatalf("credentials leaked into %q", s)
					}
				}
			})
		}
	}
}

func TestTargetRefusalIsNotTheProxysFault(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().String()
	ln.Close()
	srv := newProxy(t, proxydial.SOCKS5, testUser, testPass)
	d, _ := proxydial.New(proxydial.SOCKS5, srv.Addr(), testUser, testPass)
	_, err := d.DialContext(context.Background(), "tcp", closed)
	var pe *proxydial.Error
	if !errors.As(err, &pe) || pe.Stage != proxydial.StageTarget || pe.ProxyFault() || proxydial.IsProxyFault(err) {
		t.Fatalf("got %v, want a target-stage error that does not blame the proxy", err)
	}
}

func TestUnreachableProxyIsAConnectFault(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	d, _ := proxydial.New(proxydial.SOCKS5, dead, testUser, testPass)
	_, err := d.DialContext(context.Background(), "tcp", "example.invalid:443")
	var pe *proxydial.Error
	if !errors.As(err, &pe) || pe.Stage != proxydial.StageConnect || !proxydial.IsProxyFault(err) {
		t.Fatalf("got %v, want a connect-stage proxy fault", err)
	}
}

func TestHandshakeHonoursTheContext(t *testing.T) {
	// A listener that accepts and never answers: the greeting read must end with the context.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	d, _ := proxydial.New(proxydial.SOCKS5, ln.Addr().String(), testUser, testPass)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := d.DialContext(ctx, "tcp", "example.invalid:443")
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("err=%v after %s, want a prompt failure", err, time.Since(start))
	}
	if !proxydial.IsProxyFault(err) {
		t.Fatalf("a proxy that never answers is the proxy's fault, got %v", err)
	}
}

func TestHTTPClientGoesThroughTheProxy(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "crc") }))
	defer web.Close()
	srv := newProxy(t, proxydial.SOCKS5, testUser, testPass)
	d, _ := proxydial.New(proxydial.SOCKS5, srv.Addr(), testUser, testPass)
	resp, err := d.HTTPClient(5 * time.Second).Get(web.URL + "/crc0")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "crc" || srv.Opened() != 1 {
		t.Fatalf("body=%q opened=%d", body, srv.Opened())
	}
}

func TestNewValidates(t *testing.T) {
	for _, tc := range []struct {
		kind       proxydial.Kind
		addr, u, p string
	}{
		{"ftp", "h:1", "", ""},
		{proxydial.SOCKS5, "nohost", "", ""},
		{proxydial.SOCKS5, "h:0", "", ""},
		{proxydial.SOCKS5, "h:1080", "user", ""},
		{proxydial.SOCKS5, "h:1080", "", "zq-9-lonely-secret"},
	} {
		if _, err := proxydial.New(tc.kind, tc.addr, tc.u, tc.p); err == nil {
			t.Errorf("New(%q, %q) accepted", tc.kind, tc.addr)
		} else if tc.p != "" && strings.Contains(err.Error(), tc.p) {
			t.Errorf("error leaked the password: %v", err)
		}
	}
}

// TestLocalDNSGetsThroughAProxyThatRefusesNames models Cloudflare WARP's local proxy, measured
// closing every remote-DNS CONNECT and accepting every IP one: with LocalDNS the name is
// resolved here and an IPv4 address goes to the proxy; without it the dial fails, and the
// failure is the proxy's.
func TestLocalDNSGetsThroughAProxyThatRefusesNames(t *testing.T) {
	target := echoServer(t)
	_, port, _ := net.SplitHostPort(target)
	srv := newProxy(t, proxydial.SOCKS5, "", "")
	srv.RefuseDomainTargets = true
	d, _ := proxydial.New(proxydial.SOCKS5, srv.Addr(), "", "")

	if _, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("localhost", port)); !proxydial.IsProxyFault(err) {
		t.Fatalf("remote DNS through a name-refusing proxy: got %v, want a proxy fault", err)
	}

	d.LocalDNS = true
	var looked []string
	d.Lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		looked = append(looked, host)
		// An IPv6 answer first: only the IPv4 one may be used.
		return []net.IPAddr{{IP: net.ParseIP("::1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	c, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("game.example", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundTrip(t, c)
	if len(looked) != 1 || looked[0] != "game.example" || srv.Requests(net.JoinHostPort("127.0.0.1", port)) != 1 {
		t.Fatalf("looked up %v; proxy saw %d CONNECTs to the IPv4 address", looked, srv.Requests(net.JoinHostPort("127.0.0.1", port)))
	}

	d.Lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("::1")}}, nil
	}
	var pe *proxydial.Error
	if _, err := d.DialContext(context.Background(), "tcp", "v6only.example:443"); !errors.As(err, &pe) || pe.Stage != proxydial.StageResolve || pe.ProxyFault() {
		t.Fatalf("an IPv6-only name: got %v, want a resolve-stage error that does not blame the proxy", err)
	}
}
