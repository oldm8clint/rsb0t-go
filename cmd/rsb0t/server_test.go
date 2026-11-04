package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// fakeGameServer answers like a LostCity revision-289 web server: /crc0, a client.js with a stock
// Engine-TS key in it, and the game WebSocket on "/". withKey false leaves the key out of client.js.
func fakeGameServer(t *testing.T, withKey bool) *httptest.Server {
	t.Helper()
	js := "var n=1;"
	if withKey {
		js = `k.Ka(BigInt("` + strings.Repeat("7", 154) + `"),BigInt("` + strings.Repeat("3", 77) + `"))`
	}
	var up websocket.Upgrader
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && websocket.IsWebSocketUpgrade(r):
			if c, err := up.Upgrade(w, r, nil); err == nil {
				c.Close()
			}
		case r.URL.Path == "/crc0":
			w.Write(make([]byte, 36))
		case r.URL.Path == "/client/client.js":
			w.Write([]byte(js))
		case r.URL.Path == "/":
			w.Write([]byte("<html>a game</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAFreshRosterNamesNoServer(t *testing.T) {
	r, err := ensureRoster(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.hasServer() || r.str("wsURL") != "" || r.str("httpBase") != "" {
		t.Fatalf("a fresh roster names a server: %q %q", r.str("wsURL"), r.str("httpBase"))
	}
}

func TestServerCommandChecksAndSavesTheServer(t *testing.T) {
	ts := fakeGameServer(t, true)
	home := t.TempDir()
	if code := run([]string{"-data", home, "check-server", ts.URL}); code != 0 {
		t.Fatalf("check-server exited %d", code)
	}
	r, _ := ensureRoster(home)
	if r.hasServer() {
		t.Fatal("check-server saved the server")
	}
	if code := run([]string{"-data", home, "server", ts.URL}); code != 0 {
		t.Fatalf("server exited %d", code)
	}
	r, err := loadRoster(filepath.Join(home, rosterFile))
	if err != nil {
		t.Fatal(err)
	}
	if r.str("httpBase") != ts.URL+"/" || r.str("wsURL") != "ws://"+strings.TrimPrefix(ts.URL, "http://")+"/" {
		t.Fatalf("saved %q %q", r.str("httpBase"), r.str("wsURL"))
	}
	if r.str("modulus") != "" {
		t.Fatal("a key found in client.js was written into the roster; it should be read at each start")
	}
	if code := run([]string{"-data", home, "server"}); code != 0 {
		t.Fatalf("server with no address exited %d", code)
	}
}

func TestServerCommandKeepsAPastedKey(t *testing.T) {
	ts := fakeGameServer(t, false)
	home := t.TempDir()
	if code := run([]string{"-data", home, "check-server", ts.URL}); code != 1 {
		t.Fatalf("check-server with no key exited %d, want 1", code)
	}
	mod := strings.Repeat("9", 154)
	if code := run([]string{"-data", home, "server", ts.URL, "-modulus", mod, "-exponent", "65537"}); code != 0 {
		t.Fatalf("server with a pasted key exited %d", code)
	}
	r, _ := loadRoster(filepath.Join(home, rosterFile))
	if r.str("modulus") != mod || r.str("exponent") != "65537" {
		t.Fatalf("pasted key not kept: %q %q", r.str("modulus"), r.str("exponent"))
	}
	// A bad pasted key saves nothing.
	home2 := t.TempDir()
	if code := run([]string{"-data", home2, "server", ts.URL, "-modulus", "123"}); code != 1 {
		t.Fatalf("a short key exited %d, want 1", code)
	}
	if r, _ := ensureRoster(home2); r.hasServer() {
		t.Fatal("a server with an unusable pasted key was saved")
	}
}

func TestServerCommandDoesNotSaveSomethingThatIsNotAGameServer(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	home := t.TempDir()
	if code := run([]string{"-data", home, "server", ts.URL}); code != 1 {
		t.Fatalf("exited %d, want 1", code)
	}
	if r, _ := ensureRoster(home); r.hasServer() {
		t.Fatal("saved a server with no /crc0")
	}
}

func TestSetupPageChecksAndSavesTheServer(t *testing.T) {
	ts := fakeGameServer(t, true)
	s, h := newSetup(t)
	code, out := call(t, h, "POST", "/api/setup/check", `{"address":"`+ts.URL+`"}`)
	if code != 200 || out["report"].(map[string]any)["ready"] != true {
		t.Fatalf("check: %d %v", code, out)
	}
	if r, _ := ensureRoster(s.home); r.hasServer() {
		t.Fatal("check saved the server")
	}
	code, out = call(t, h, "POST", "/api/setup/server", `{"address":"`+ts.URL+`"}`)
	if code != 200 || out["saved"] != true || out["server"] == nil {
		t.Fatalf("save: %d %v", code, out)
	}
	if code, _ := call(t, h, "POST", "/api/setup/check", `{"address":""}`); code != 400 {
		t.Fatalf("an empty address was %d", code)
	}
}

func TestSetupServerFlagIsTakenOffThePanelArguments(t *testing.T) {
	a, rest := setupServerFlag([]string{"-server", "example-server.com", "-modulus=12", "-game", "wss://g.example-server.com/", "--", "-listen", "x"})
	if a.Address != "example-server.com" || a.Modulus != "12" || a.Exponent != "" || a.Game != "wss://g.example-server.com/" || len(rest) != 3 || rest[0] != "--" {
		t.Fatalf("got %+v %v", a, rest)
	}
}

func TestChangingServerDropsTheOldServersWorldsAndHeaders(t *testing.T) {
	ts := fakeGameServer(t, true)
	home := t.TempDir()
	r, _ := ensureRoster(home)
	r.set("wsURL", "wss://old.example-server.com/")
	r.set("httpBase", "https://old.example-server.com/")
	r.set("worlds", map[string]any{"2": map[string]string{"wsURL": "wss://w2.old.example-server.com/"}})
	r.set("browserHeaders", true)
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-data", home, "server", ts.URL}); code != 0 {
		t.Fatalf("server exited %d", code)
	}
	r, _ = loadRoster(filepath.Join(home, rosterFile))
	if _, ok := r.fields["worlds"]; ok {
		t.Fatal("the old server's worlds were kept")
	}
	if _, ok := r.fields["browserHeaders"]; ok {
		t.Fatal("browserHeaders was kept for a server that does not need it")
	}
}

func TestServerCommandsHelp(t *testing.T) {
	for _, c := range [][]string{{"server", "-h"}, {"check-server", "--help"}} {
		if code := run(append([]string{"-data", t.TempDir()}, c...)); code != 0 {
			t.Errorf("%v exited %d", c, code)
		}
	}
}
