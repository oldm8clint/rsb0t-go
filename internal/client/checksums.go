package client

import (
	"net/http"
	"sync"
	"time"
)

// jagChecksumCacheTTL is deliberately short: /crc0 is shared by all accounts in
// a process, but a restarted engine may publish a new cache table while the fleet
// is still running. A short TTL avoids one HTTP request per account without making
// the cache effectively permanent.
const jagChecksumCacheTTL = 30 * time.Second

type jagChecksumCacheEntry struct {
	checksums [9]int32
	fetchedAt time.Time
}

var jagChecksumCache struct {
	sync.Mutex
	entries map[string]jagChecksumCacheEntry
}

// InvalidateChecksums drops every cached /crc0 table, so the next login for any host or
// route fetches a fresh one instead of waiting out jagChecksumCacheTTL.
//
// It exists for one specific recovery. The engine answers login response 6 both when the
// nine CRCs do not match its own and when the RSA block fails to decrypt (see the engine's
// World.ts, which comments the latter as "sending out of date intentionally") — so a
// response 6 means "something the fleet caches about this server is stale", and the two
// candidates are the cache table and the login key. internal/fleet asks for both to be
// refreshed on a response 6 rather than guessing which one it was.
func InvalidateChecksums() {
	jagChecksumCache.Lock()
	defer jagChecksumCache.Unlock()
	jagChecksumCache.entries = nil
}

// fetchJagChecksumsCached coalesces concurrent checksum requests for the same
// HTTP base. The lock is held during the fetch intentionally: fleet startup tends
// to launch many accounts together, and one slow /crc0 request is preferable to a
// request stampede against the cache server.
func fetchJagChecksumsCached(httpBase string) ([9]int32, error) {
	jagChecksumCache.Lock()
	defer jagChecksumCache.Unlock()

	if jagChecksumCache.entries == nil {
		jagChecksumCache.entries = make(map[string]jagChecksumCacheEntry)
	}
	if entry, ok := jagChecksumCache.entries[httpBase]; ok && time.Since(entry.fetchedAt) < jagChecksumCacheTTL {
		return entry.checksums, nil
	}

	return fetchJagChecksumsLocked(httpBase, httpBase, nil)
}

// fetchJagChecksumsCachedVia is fetchJagChecksumsCached for one route: the fetch goes through
// hc, and the cache entry is kept per route so each route fetches for itself.
func fetchJagChecksumsCachedVia(httpBase, routeKey string, hc *http.Client) ([9]int32, error) {
	jagChecksumCache.Lock()
	defer jagChecksumCache.Unlock()

	if jagChecksumCache.entries == nil {
		jagChecksumCache.entries = make(map[string]jagChecksumCacheEntry)
	}
	key := httpBase + "|route:" + routeKey
	if entry, ok := jagChecksumCache.entries[key]; ok && time.Since(entry.fetchedAt) < jagChecksumCacheTTL {
		return entry.checksums, nil
	}
	return fetchJagChecksumsLocked(key, httpBase, hc)
}

// fetchJagChecksumsLocked fetches and stores one cache entry; the caller holds the cache lock.
func fetchJagChecksumsLocked(key, httpBase string, hc *http.Client) ([9]int32, error) {
	checksums, err := fetchJagChecksumsWith(hc, httpBase)
	if err != nil {
		return [9]int32{}, err
	}
	jagChecksumCache.entries[key] = jagChecksumCacheEntry{
		checksums: checksums,
		fetchedAt: time.Now(),
	}
	return checksums, nil
}
