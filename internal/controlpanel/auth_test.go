package controlpanel

import (
	"net/http"
	"strings"
	"testing"
)

func TestTokenIsRequired(t *testing.T) {
	const token = "s3cret token"
	h := newHarness(t, Config{Token: token}, testOptions(), nil)

	refused := []struct {
		path   string
		header http.Header
	}{
		{"/api/status", nil},
		{"/api/status", http.Header{"Authorization": {"Bearer wrong"}}},
		{"/api/status", http.Header{"Cookie": {cookieName + "=" + token}}},
		{"/api/stream", nil},
		{"/", nil},
		{"/app.js", nil},
		{"/?token=wrong", nil},
	}
	for _, c := range refused {
		res := h.request(http.MethodGet, c.path, nil, c.header)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %v: %d, want 401", c.path, c.header, res.StatusCode)
		}
	}
	if res := h.get("/api/status"); !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("API refusal is %q, want JSON", res.Header.Get("Content-Type"))
	}
	if res := h.request(http.MethodGet, "/api/status", nil, http.Header{"Authorization": {"Bearer " + token}}); res.StatusCode != http.StatusOK {
		t.Errorf("bearer token: %d", res.StatusCode)
	}

	res := h.get("/api/events?since=2&token=s3cret+token")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/api/events?since=2" {
		t.Fatalf("token in the query: %d, Location %q", res.StatusCode, res.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || strings.Contains(cookie.Value, "s3cret") {
		t.Fatalf("cookie %+v", cookie)
	}
	withCookie := http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}
	if res := h.request(http.MethodGet, "/api/status", nil, withCookie); res.StatusCode != http.StatusOK {
		t.Errorf("cookie: %d", res.StatusCode)
	}
	r, sres, _ := h.openStream(withCookie)
	if sres.StatusCode != http.StatusOK {
		t.Fatalf("stream with cookie: %d", sres.StatusCode)
	}
	if m := r.next(); m.event != sseHello {
		t.Errorf("stream with cookie sent %q first", m.event)
	}
}

func TestLoopbackRefusesOtherHostNames(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	for host, want := range map[string]int{
		"evil.example:7274": http.StatusForbidden,
		"127.0.0.2.nip.io":  http.StatusForbidden,
		"localhost:7274":    http.StatusOK,
		"127.0.0.1":         http.StatusOK,
		"[::1]:7274":        http.StatusOK,
	} {
		if res := h.request(http.MethodGet, "/api/meta", nil, http.Header{"Host": {host}}); res.StatusCode != want {
			t.Errorf("Host %s: %d, want %d", host, res.StatusCode, want)
		}
	}
}

func TestCrossOriginWritesAreRefused(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	body := func() *strings.Reader { return strings.NewReader(`{"v":1,"panels":[]}`) }
	for _, header := range []http.Header{
		{"Origin": {"http://evil.example"}, "Content-Type": {"application/json"}},
		{"Sec-Fetch-Site": {"cross-site"}, "Content-Type": {"application/json"}},
	} {
		if res := h.request(http.MethodPut, "/api/layout", body(), header); res.StatusCode != http.StatusForbidden {
			t.Errorf("%v: %d, want 403", header, res.StatusCode)
		}
	}
	same := http.Header{"Origin": {h.ts.URL}, "Sec-Fetch-Site": {"same-origin"}, "Content-Type": {"application/json"}}
	if res := h.request(http.MethodPut, "/api/layout", body(), same); res.StatusCode != http.StatusConflict {
		t.Errorf("same-origin PUT with no layout path: %d, want 409 from the handler", res.StatusCode)
	}
}

func TestExposedListenerNeedsAToken(t *testing.T) {
	for listen, ok := range map[string]bool{
		"127.0.0.1:0":      true,
		"localhost:0":      true,
		"[::1]:0":          true,
		"0.0.0.0:7274":     false,
		":7274":            false,
		"192.168.1.5:7274": false,
		"nonsense":         false,
	} {
		_, err := Config{Listen: listen}.normalised()
		if (err == nil) != ok {
			t.Errorf("listen %q without a token: err %v", listen, err)
		}
	}
	if _, err := (Config{Listen: "0.0.0.0:7274", Token: "t"}).normalised(); err != nil {
		t.Errorf("exposed listener with a token: %v", err)
	}
	c, _ := Config{EventHistory: 1, MemoryHistory: 1 << 20}.normalised()
	if c.Listen != DefaultListen || c.EventHistory != MinHistory || c.MemoryHistory != MaxMemoryHistory {
		t.Errorf("defaults and clamps: %+v", c)
	}
}

// TestAllowRemoteHostNeedsAToken covers Config.AllowRemoteHost's own doc comment: Listen
// staying loopback (as it does for every deployment tonight — a tunnel always connects to
// 127.0.0.1 from this process's own point of view) must not let AllowRemoteHost silently skip
// the "a token is required before this is reachable from the internet" rule that
// TestExposedListenerNeedsAToken above already covers for a non-loopback Listen.
func TestAllowRemoteHostNeedsAToken(t *testing.T) {
	if _, err := (Config{Listen: "127.0.0.1:0", AllowRemoteHost: true}).normalised(); err == nil {
		t.Error("AllowRemoteHost with no Token: want an error, got nil")
	}
	if _, err := (Config{Listen: "127.0.0.1:0", AllowRemoteHost: true, Token: "t"}).normalised(); err != nil {
		t.Errorf("AllowRemoteHost with a Token: %v", err)
	}
	// Off by default: a loopback Listen with no AllowRemoteHost and no Token is exactly
	// today's normal local-only deployment, still fine with neither set.
	if _, err := (Config{Listen: "127.0.0.1:0"}).normalised(); err != nil {
		t.Errorf("plain loopback, neither set: %v", err)
	}
}

// TestAllowRemoteHostSkipsTheHostCheck proves the actual guard behavior change: with
// AllowRemoteHost set (and a Token, as normalised now requires), a request whose Host header
// is a tunnel's public hostname is no longer refused by the loopback check that
// TestLoopbackRefusesOtherHostNames above exercises for the default (AllowRemoteHost false)
// case — the token check further down in guard.wrap is what gates the request instead.
func TestAllowRemoteHostSkipsTheHostCheck(t *testing.T) {
	const token = "s3cret token"
	h := newHarness(t, Config{Token: token, AllowRemoteHost: true}, testOptions(), nil)

	// No token at all: still refused, now with 401 (the token check), not 403 (the Host
	// check) — either way unauthenticated, but confirms the token is what is doing the
	// gating now that the Host check is skipped.
	if res := h.request(http.MethodGet, "/api/meta", nil, http.Header{"Host": {"example.trycloudflare.com"}}); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token, remote Host: %d, want 401", res.StatusCode)
	}
	// The right token from a non-loopback Host header now succeeds — this is the exact
	// scenario a cloudflared quick tunnel produces (see cmd/controlpanel-remote-proxy).
	res := h.request(http.MethodGet, "/api/meta", nil, http.Header{
		"Host":          {"example.trycloudflare.com"},
		"Authorization": {"Bearer " + token},
	})
	if res.StatusCode != http.StatusOK {
		t.Errorf("right token, remote Host: %d, want 200", res.StatusCode)
	}
}
