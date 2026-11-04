package client

import (
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCrc0ErrorPageIsNotMistakenForChecksums is the hole the status check closes. A 502
// whose HTML body happens to reach 36 bytes used to be decoded as nine archive CRCs and
// then cached for the full TTL, so every account logging in during that window would
// have been rejected with response 6 by a server that was about to be perfectly happy to
// admit them — a self-inflicted version of the very stranding this work exists to fix.
func TestCrc0ErrorPageIsNotMistakenForChecksums(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			// Comfortably past the 36-byte length check that used to be the
			// only thing standing between this and the login packet.
			_, _ = w.Write([]byte("<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>"))
			return
		}
		body := make([]byte, 36)
		binary.BigEndian.PutUint32(body, 42)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	InvalidateChecksums()
	defer InvalidateChecksums()

	_, err := fetchJagChecksumsCached(server.URL + "/")
	if err == nil {
		t.Fatal("a 502 error page was accepted as the cache checksum table")
	}
	var status *HTTPStatusError
	if !errors.As(err, &status) {
		t.Fatalf("error %v is not an *HTTPStatusError, so the fleet cannot classify it", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error %q does not say what the server answered", err)
	}

	// And it must not have been cached: the very next login has to see the real table.
	checksums, err := fetchJagChecksumsCached(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if checksums[0] != 42 {
		t.Fatalf("checksum[0] = %d, want 42", checksums[0])
	}
}

// TestInvalidateChecksumsForcesARefetch pins the lever internal/fleet pulls when a login
// is rejected as out of date: the cached table must be dropped rather than waiting out
// its TTL, since a server that just restarted may have published a new one.
func TestInvalidateChecksumsForcesARefetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := requests.Add(1)
		body := make([]byte, 36)
		binary.BigEndian.PutUint32(body, uint32(n))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	InvalidateChecksums()
	defer InvalidateChecksums()

	first, err := fetchJagChecksumsCached(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	cached, err := fetchJagChecksumsCached(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if cached != first || requests.Load() != 1 {
		t.Fatalf("second fetch was not served from the cache (%d requests)", requests.Load())
	}

	InvalidateChecksums()
	fresh, err := fetchJagChecksumsCached(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if fresh == first || requests.Load() != 2 {
		t.Fatalf("invalidation did not force a refetch: %d requests, checksum[0]=%d", requests.Load(), fresh[0])
	}
}

// TestConnectReportsItsStage proves the two live failure classes really do come back
// from Connect carrying the type internal/fleet classifies on — asserted against a real
// server refusing in the two ways the production one did, not against a constructed
// error value.
func TestConnectReportsItsStage(t *testing.T) {
	t.Run("empty crc0", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK) // 200 with no body, exactly as live
		}))
		defer server.Close()
		InvalidateChecksums()
		defer InvalidateChecksums()

		_, err := Connect(Config{WSURL: "ws://127.0.0.1:1", HTTPBase: server.URL + "/"})
		var connErr *ConnectError
		if !errors.As(err, &connErr) || connErr.Stage != StageChecksums {
			t.Fatalf("error %v is not a checksums-stage ConnectError", err)
		}
		if got := err.Error(); !strings.HasPrefix(got, "client: checksums: ") {
			t.Fatalf("error %q lost its live wording", got)
		}
	})

	t.Run("bad handshake", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}))
		defer server.Close()

		_, err := Connect(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), Checksums: &[9]int32{}})
		var connErr *ConnectError
		if !errors.As(err, &connErr) || connErr.Stage != StageDial {
			t.Fatalf("error %v is not a dial-stage ConnectError", err)
		}
		if got := err.Error(); got != "client: dial: websocket: bad handshake" {
			t.Fatalf("error %q is not the string the live fleet logged", got)
		}
	})
}
