package controlpanel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests exercise the HTTP contract using the package's own FakeSource, which already
// implements Source correctly, instead of depending on fleet internals.

func TestHandlerSmokeAPI(t *testing.T) {
	s, err := newServer(Config{Enabled: true}, NewFakeSource(3, 1), defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/meta", http.StatusOK},
		{"/api/status", http.StatusOK},
		{"/api/events", http.StatusOK},
		{"/api/memory", http.StatusOK},
	} {
		resp, err := ts.Client().Get(ts.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("GET %s: got %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}

func TestHandlerRejectsUnexpectedMethods(t *testing.T) {
	s, err := newServer(Config{Enabled: true}, NewFakeSource(3, 1), defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/meta", nil)
	req.Host = "localhost" // the guard's loopback-only check refuses a non-loopback Host first
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/meta: got %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestStartDisabledDoesNotListen(t *testing.T) {
	s, err := Start(context.Background(), Config{Enabled: false}, NewFakeSource(3, 1))
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Fatal("disabled control panel returned a server")
	}
}
