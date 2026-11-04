package client

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFetchJagChecksumsCachedCoalescesConcurrentRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/crc0" {
			t.Error("unexpected request path: " + r.URL.Path)
			return
		}
		body := make([]byte, 36)
		for i := 0; i < 9; i++ {
			binary.BigEndian.PutUint32(body[i*4:], uint32(i+1))
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	jagChecksumCache.Lock()
	jagChecksumCache.entries = make(map[string]jagChecksumCacheEntry)
	jagChecksumCache.Unlock()

	const callers = 32
	results := make([][9]int32, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = fetchJagChecksumsCached(server.URL + "/")
		}(i)
	}
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("expected one /crc0 request for %d concurrent callers, got %d", callers, got)
	}
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		for skill, got := range results[i] {
			if got != int32(skill+1) {
				t.Fatalf("caller %d checksum %d = %d, want %d", i, skill, got, skill+1)
			}
		}
	}
}

func TestFetchJagChecksumsCachedDoesNotCacheErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "short", http.StatusInternalServerError)
			return
		}
		body := make([]byte, 36)
		binary.BigEndian.PutUint32(body, 42)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	jagChecksumCache.Lock()
	jagChecksumCache.entries = make(map[string]jagChecksumCacheEntry)
	jagChecksumCache.Unlock()

	if _, err := fetchJagChecksumsCached(server.URL + "/"); err == nil {
		t.Fatal("first fetch unexpectedly succeeded")
	}
	checksums, err := fetchJagChecksumsCached(server.URL + "/")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if checksums[0] != 42 {
		t.Fatalf("checksum[0] = %d, want 42", checksums[0])
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("expected failed result not to be cached, got %d requests", got)
	}
}
