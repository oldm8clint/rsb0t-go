package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsb0t-go/internal/bot"
)

// call makes a request with the header and content type the page sends.
func call(t *testing.T, h http.Handler, method, url, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Host = "127.0.0.1:7274"
	req.Header.Set(setupHeader, "1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func newSetup(t *testing.T) (*setupServer, http.Handler) {
	s := &setupServer{home: t.TempDir(), start: make(chan struct{}, 1)}
	return s, s.handler()
}

func TestSetupAddsListsAndRemovesAnAccountWithoutEchoingThePassword(t *testing.T) {
	s, h := newSetup(t)
	code, out := call(t, h, "POST", "/api/setup/accounts", `{"username":"Alice_1","password":"placeholder-pw","script":"ProgressiveFisher"}`)
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	if b, _ := json.Marshal(out); strings.Contains(string(b), "placeholder-pw") {
		t.Fatalf("the response repeats the password: %s", b)
	}
	accts := out["accounts"].([]any)
	if len(accts) != 1 || accts[0].(map[string]any)["hasPassword"] != true {
		t.Fatalf("accounts %v", accts)
	}
	creds, _ := loadCredentials(s.home)
	if creds.passwordFor("alice_1") != "placeholder-pw" {
		t.Fatal("the password was not stored")
	}
	if b, _ := os.ReadFile(filepath.Join(s.home, rosterFile)); strings.Contains(string(b), "placeholder-pw") {
		t.Fatal("the roster holds the password")
	}
	if code, _ := call(t, h, "POST", "/api/setup/accounts", `{"username":"alice_1","password":"x","script":""}`); code != 409 {
		t.Fatalf("a duplicate was %d, want 409", code)
	}
	if code, out := call(t, h, "POST", "/api/setup/accounts", `{"username":"bad name!","password":"x"}`); code != 400 || out["error"] == nil {
		t.Fatalf("a bad name was %d %v", code, out)
	}
	if code, _ := call(t, h, "POST", "/api/setup/accounts", `{"username":"Bob","password":"x","script":"NoSuchScript"}`); code != 400 {
		t.Fatalf("an unknown script was %d", code)
	}
	if code, out := call(t, h, "DELETE", "/api/setup/accounts?name=Alice_1", ""); code != 200 || len(out["accounts"].([]any)) != 0 {
		t.Fatalf("remove: %d %v", code, out)
	}
	if creds, _ := loadCredentials(s.home); len(creds) != 0 {
		t.Fatal("the stored password outlived the account")
	}
}

func TestSetupSettingsAreCheckedAgainstTheScriptsSchema(t *testing.T) {
	_, h := newSetup(t)
	call(t, h, "POST", "/api/setup/accounts", `{"username":"A","password":"x","script":""}`)
	if code, out := call(t, h, "PUT", "/api/setup/settings", `{"username":"A","script":"GemCutter","settings":{"noSuchKey":1}}`); code != 400 {
		t.Fatalf("an unknown key was %d %v", code, out)
	}
	if code, out := call(t, h, "PUT", "/api/setup/settings", `{"username":"A","script":"GemCutter","settings":{}}`); code != 200 {
		t.Fatalf("empty settings were %d %v", code, out)
	}
	// A script with no typed schema takes any object.
	if code, out := call(t, h, "PUT", "/api/setup/settings", `{"username":"A","script":"ProgressiveFisher","settings":{"anything":true}}`); code != 200 {
		t.Fatalf("free settings were %d %v", code, out)
	}
	if code, _ := call(t, h, "PUT", "/api/setup/settings", `{"username":"Nobody","script":"GemCutter","settings":{}}`); code != 404 {
		t.Fatalf("an unknown account was %d, want 404", code)
	}
}

func TestSetupStateListsScriptsWithFormFields(t *testing.T) {
	_, h := newSetup(t)
	_, out := call(t, h, "GET", "/api/setup/state", "")
	typed := 0
	for _, s := range out["scripts"].([]any) {
		m := s.(map[string]any)
		if len(m["fields"].([]any)) > 0 {
			typed++
		}
	}
	if len(out["scripts"].([]any)) < 20 || typed < 5 {
		t.Fatalf("%d scripts, %d with a form", len(out["scripts"].([]any)), typed)
	}
}

func TestSetupStartNeedsAnAccount(t *testing.T) {
	s, h := newSetup(t)
	if code, _ := call(t, h, "POST", "/api/setup/start", `{}`); code != 400 {
		t.Fatalf("start with no account was %d, want 400", code)
	}
	call(t, h, "POST", "/api/setup/accounts", `{"username":"A","password":"x"}`)
	if code, out := call(t, h, "POST", "/api/setup/start", `{}`); code != 400 || !strings.Contains(out["error"].(string), "server") {
		t.Fatalf("start with no server was %d %v, want 400 naming the server", code, out)
	}
	ro, _ := ensureRoster(s.home)
	ro.set("wsURL", "ws://localhost/")
	ro.set("httpBase", "http://localhost/")
	if err := ro.save(); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, h, "POST", "/api/setup/start", `{}`); code != 200 {
		t.Fatalf("start was %d", code)
	}
	select {
	case <-s.start:
	default:
		t.Fatal("start did not signal")
	}
}

func TestSetupRefusesForeignHostsAndRequestsWithoutTheHeader(t *testing.T) {
	_, h := newSetup(t)
	req := httptest.NewRequest("GET", "/api/setup/state", nil)
	req.Host = "evil.example:7274"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("a foreign Host was %d, want 403", w.Code)
	}
	req = httptest.NewRequest("POST", "/api/setup/accounts", strings.NewReader(`{"username":"A","password":"x"}`))
	req.Host = "127.0.0.1:7274"
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("a change without the header was %d, want 403", w.Code)
	}
	req = httptest.NewRequest("POST", "/api/setup/accounts", strings.NewReader(`username=A`))
	req.Host = "127.0.0.1:7274"
	req.Header.Set(setupHeader, "1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a form post was %d, want 415", w.Code)
	}
}

func TestSetupOnlyServesLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:7274": true, "localhost:7274": true, "[::1]:7274": true, "0.0.0.0:7274": false, "192.168.1.5:7274": false, ":7274": false} {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestNeedsMapDataNamesRealScripts(t *testing.T) {
	known := map[string]bool{}
	for _, n := range bot.Names() {
		known[n] = true
	}
	for n := range needsMapData {
		if !known[n] {
			t.Errorf("needsMapData names %q, which is not a registered script", n)
		}
	}
}
