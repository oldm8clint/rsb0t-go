package serverprobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, ws, http string }{
		{"example-server.com", "wss://example-server.com/", "https://example-server.com/"},
		{"  https://Example-Server.com/play?x=1 ", "wss://example-server.com/", "https://example-server.com/"},
		{"https://example-server.com", "wss://example-server.com/", "https://example-server.com/"},
		{"wss://w1.example-server.com/", "wss://w1.example-server.com/", "https://w1.example-server.com/"},
		{"wss://w1.example-server.com/ws", "wss://w1.example-server.com/ws", "https://w1.example-server.com/"},
		{"localhost", "ws://localhost/", "http://localhost/"},
		{"localhost:8888", "ws://localhost:8888/", "http://localhost:8888/"},
		{"127.0.0.1:80", "ws://127.0.0.1:80/", "http://127.0.0.1:80/"},
		{"http://192.168.1.20:8888/rs2.cgi", "ws://192.168.1.20:8888/", "http://192.168.1.20:8888/"},
		{"game.test", "ws://game.test/", "http://game.test/"},
		{"::1", "ws://[::1]/", "http://[::1]/"},
		{"[::1]:8888", "ws://[::1]:8888/", "http://[::1]:8888/"},
		{"203.0.113.7:8888", "ws://203.0.113.7:8888/", "http://203.0.113.7:8888/"},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if err != nil {
			t.Errorf("Normalize(%q): %v", c.in, err)
			continue
		}
		if got.WSURL != c.ws || got.HTTPBase != c.http {
			t.Errorf("Normalize(%q) = %+v, want %s %s", c.in, got, c.ws, c.http)
		}
	}
	for _, bad := range []string{"", "   ", "ftp://example-server.com", "https://user:pw@example-server.com", "https://"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q): want an error", bad)
		}
	}
}

var (
	stockModulus  = strings.Repeat("7", 154)
	stockExponent = strings.Repeat("3", 77)
)

// fakeServer is what a LostCity web server serves, as far as the checks look: /crc0, the web client
// and the game WebSocket on "/".
type fakeServer struct {
	crc        int    // bytes of /crc0; 0 is a 404
	clientJS   string // "" is a 404
	wantOrigin bool
}

func (f fakeServer) handler() http.Handler {
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return !f.wantOrigin || r.Header.Get("Origin") != "" }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && websocket.IsWebSocketUpgrade(r):
			c, err := up.Upgrade(w, r, nil)
			if err == nil {
				c.Close()
			}
		case r.URL.Path == "/":
			http.Redirect(w, r, "/rs2.cgi", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/crc") && f.crc > 0:
			w.Write(make([]byte, f.crc))
		case r.URL.Path == "/client/client.js" && f.clientJS != "":
			w.Write([]byte(f.clientJS))
		default:
			http.NotFound(w, r)
		}
	})
}

func stockClientJS() string {
	return `a(H),this.Eq.Ea($),this.Eq.Ka(BigInt("` + stockModulus + `"),BigInt("` + stockExponent + `")),this.xu.la=0;`
}

func step(r Report, name string) Step {
	for _, s := range r.Steps {
		if s.Name == name {
			return s
		}
	}
	return Step{}
}

func quick() Options { return Options{Timeout: 3 * time.Second, MaxWorlds: 3} }

func TestCheckAStockEngineIsReadyAndItsKeyIsFound(t *testing.T) {
	ts := httptest.NewServer(fakeServer{crc: 40, clientJS: stockClientJS()}.handler())
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	if !r.Ready {
		t.Fatalf("not ready: %s %+v", r.Verdict, r.Steps)
	}
	if !r.KeyFound || r.Modulus != stockModulus || r.Exponent != stockExponent {
		t.Fatalf("key: found=%v mod=%d digits exp=%s", r.KeyFound, len(r.Modulus), r.Exponent)
	}
	if r.NeedsBrowserHeaders {
		t.Fatal("browser headers asked for by a server that does not check Origin")
	}
	if !strings.HasPrefix(r.Verdict, "Ready.") || !strings.Contains(r.Verdict, "response 6") {
		t.Fatalf("verdict %q", r.Verdict)
	}
}

func TestCheckWithoutCRCSaysItIsNotALostCityServer(t *testing.T) {
	ts := httptest.NewServer(fakeServer{clientJS: stockClientJS()}.handler())
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	if r.Ready || step(r, "Game files (/crc0)").Status != Fail || !strings.Contains(r.Verdict, "LostCity revision-289") {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckWithAShortCRCFails(t *testing.T) {
	ts := httptest.NewServer(fakeServer{crc: 12, clientJS: stockClientJS()}.handler())
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	if r.Ready || !strings.Contains(step(r, "Game files (/crc0)").Detail, "12 bytes") {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckWithoutAKeyExplainsHowToPasteOne(t *testing.T) {
	ts := httptest.NewServer(fakeServer{crc: 36, clientJS: "var n=12345;"}.handler())
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	if r.Ready || r.KeyFound || step(r, "Login key").Status != Fail {
		t.Fatalf("got %+v", r)
	}
	if !strings.Contains(r.Verdict, "BigInt(") || !strings.Contains(r.Verdict, "client/client.js") {
		t.Fatalf("verdict does not say how to find the key: %q", r.Verdict)
	}
	// Pasting the key makes it ready.
	o := quick()
	o.Modulus, o.Exponent = " "+stockModulus+"\n", stockExponent
	r = Check(context.Background(), ts.URL, o)
	if !r.Ready || !r.KeyPasted || r.Modulus != stockModulus {
		t.Fatalf("with the key pasted: %+v", r)
	}
}

func TestCheckRejectsAShortPastedKey(t *testing.T) {
	if _, _, err := ParseKey("12345", ""); err == nil {
		t.Fatal("a 5-digit modulus was accepted")
	}
	if _, e, err := ParseKey(stockModulus, ""); err != nil || e.Int64() != 65537 {
		t.Fatalf("default exponent: %v %v", e, err)
	}
}

func TestCheckUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close()
	r := Check(context.Background(), url, quick())
	if r.Ready || step(r, "Reachable").Status != Fail || !strings.Contains(r.Verdict, "could not reach") {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckNoticesAServerThatWantsAnOrigin(t *testing.T) {
	ts := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS(), wantOrigin: true}.handler())
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	if !r.Ready || !r.NeedsBrowserHeaders {
		t.Fatalf("got %+v", r)
	}
}

// dialTo sends every connection for the named hosts to addr and refuses the rest.
func dialTo(addr string, hosts ...string) func(ctx context.Context, network, a string) (net.Conn, error) {
	return func(ctx context.Context, network, a string) (net.Conn, error) {
		h, _, _ := net.SplitHostPort(a)
		for _, want := range hosts {
			if h == want {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{Err: "no such host", Name: h, IsNotFound: true}}
	}
}

func TestCheckFindsWorldsAndTheGameHostBehindAWebsite(t *testing.T) {
	game := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS()}.handler())
	defer game.Close()
	site := httptest.NewServer(http.NotFoundHandler()) // the website: pages, but no game files
	defer site.Close()
	addr := strings.TrimPrefix(game.URL, "http://")
	siteDial := dialTo(addr, "w1.game.test", "w2.game.test")
	o := quick()
	o.DialContext = func(ctx context.Context, network, a string) (net.Conn, error) {
		if h, _, _ := net.SplitHostPort(a); h == "game.test" {
			var d net.Dialer
			return d.DialContext(ctx, network, strings.TrimPrefix(site.URL, "http://"))
		}
		return siteDial(ctx, network, a)
	}
	r := Check(context.Background(), "game.test", o)
	if !r.Ready {
		t.Fatalf("not ready: %+v", r)
	}
	if r.Endpoints.HTTPBase != "http://w1.game.test/" || r.Endpoints.WSURL != "ws://w1.game.test/" {
		t.Fatalf("endpoints %+v", r.Endpoints)
	}
	if len(r.Worlds) != 2 || r.Worlds[0] != 1 || r.Worlds[1] != 2 {
		t.Fatalf("worlds %v", r.Worlds)
	}
}

func TestCheckSaysAWebPageIsNotTheGameConnection(t *testing.T) {
	// A server whose web port answers but whose game WebSocket is elsewhere: "/" is a plain page.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/crc"):
			w.Write(make([]byte, 36))
		case r.URL.Path == "/client/client.js":
			w.Write([]byte(stockClientJS()))
		default:
			w.Write([]byte("<html>hello</html>"))
		}
	})
	ts := httptest.NewServer(h)
	defer ts.Close()
	r := Check(context.Background(), ts.URL, quick())
	g := step(r, "Game connection")
	if r.Ready || g.Status != Fail || strings.Contains(g.Detail, "nothing is listening") || !strings.Contains(g.Detail, "HTTP 200") {
		t.Fatalf("game step %+v", g)
	}
	if strings.Contains(r.Verdict, "restarting") || !strings.Contains(r.Verdict, "game connection address") {
		t.Fatalf("verdict %q", r.Verdict)
	}
	// Given the real game address, it is ready.
	game := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS()}.handler())
	defer game.Close()
	o := quick()
	o.GameURL = strings.Replace(game.URL, "http://", "ws://", 1)
	r = Check(context.Background(), ts.URL, o)
	if !r.Ready || !r.GameURLGiven || r.Endpoints.WSURL != o.GameURL+"/" {
		t.Fatalf("with the game address: %+v", r)
	}
}

func TestCheckFollowsARedirectToTheRealAddress(t *testing.T) {
	game := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS()}.handler())
	defer game.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, game.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer front.Close()
	r := Check(context.Background(), front.URL, quick())
	want := game.URL + "/"
	if !r.Ready || r.Endpoints.HTTPBase != want || r.Endpoints.WSURL != strings.Replace(want, "http://", "ws://", 1) {
		t.Fatalf("got %+v", r)
	}
	if step(r, "Redirect").Status != OK {
		t.Fatalf("no redirect step: %+v", r.Steps)
	}
}

func TestCheckABadPastedKeyIsNamedAndTheFoundKeyKept(t *testing.T) {
	ts := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS()}.handler())
	defer ts.Close()
	o := quick()
	o.Modulus = "12345"
	r := Check(context.Background(), ts.URL, o)
	if !r.Ready || !r.KeyPastedBad || r.KeyPasted || !r.KeyFound || r.Modulus != stockModulus || step(r, "Login key").Status != Warn {
		t.Fatalf("with the key in the bundle: %+v", r)
	}
	ts2 := httptest.NewServer(fakeServer{crc: 36}.handler())
	defer ts2.Close()
	r = Check(context.Background(), ts2.URL, o)
	if r.Ready || !r.KeyPastedBad || !strings.Contains(r.Verdict, "you pasted") {
		t.Fatalf("without: %+v", r)
	}
}

func TestCheckTriesWorldOneWithoutWWW(t *testing.T) {
	game := httptest.NewServer(fakeServer{crc: 36, clientJS: stockClientJS()}.handler())
	defer game.Close()
	site := httptest.NewServer(http.NotFoundHandler())
	defer site.Close()
	gameDial := dialTo(strings.TrimPrefix(game.URL, "http://"), "w1.game.test")
	o := quick()
	o.MaxWorlds = 1
	o.DialContext = func(ctx context.Context, network, a string) (net.Conn, error) {
		if h, _, _ := net.SplitHostPort(a); h == "www.game.test" {
			var d net.Dialer
			return d.DialContext(ctx, network, strings.TrimPrefix(site.URL, "http://"))
		}
		return gameDial(ctx, network, a)
	}
	r := Check(context.Background(), "www.game.test", o)
	if !r.Ready || r.Endpoints.HTTPBase != "http://w1.game.test/" || r.Endpoints.WSURL != "ws://w1.game.test/" {
		t.Fatalf("got %+v", r)
	}
}

func TestGameEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"wss://Example-Server.com:43595/game": "wss://example-server.com:43595/game",
		"https://example-server.com":          "wss://example-server.com/",
		"ws://localhost:43594":                "ws://localhost:43594/",
		"example-server.com:43595":            "wss://example-server.com:43595/",
	} {
		if got, err := GameEndpoint(in); err != nil || got != want {
			t.Errorf("GameEndpoint(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := GameEndpoint("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
}
