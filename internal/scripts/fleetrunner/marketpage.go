package fleetrunner

// The public market page (example.invalid/market/inventory), fetched read-only and cached for the
// whole process: one GET of the index plus one per market character, at most once per maxAge,
// however many Scarab accounts ask. A failed fetch keeps the last good listings.

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// MarketPageURL is the page's address.
const MarketPageURL = "https://example.invalid/market/inventory"

var marketCache struct {
	mu   sync.Mutex
	at   time.Time
	rows []fleetplan.MarketListing
}

var marketClient = &http.Client{Timeout: 15 * time.Second}

func fetchPage(url string) (string, error) {
	resp, err := marketClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(body), err
}

// MarketListings returns every market character's listings, refetching when the cache is older
// than maxAge. log receives one line per refresh (nil drops it).
func MarketListings(maxAge time.Duration, log func(string)) []fleetplan.MarketListing {
	marketCache.mu.Lock()
	defer marketCache.mu.Unlock()
	if !marketCache.at.IsZero() && time.Since(marketCache.at) < maxAge {
		return marketCache.rows
	}
	marketCache.at = time.Now() // even on failure: never hammer the page
	index, err := fetchPage(MarketPageURL)
	if err != nil {
		if log != nil {
			log(fmt.Sprintf("market page: %v (keeping %d cached listings)", err, len(marketCache.rows)))
		}
		return marketCache.rows
	}
	chars, _, first := fleetplan.ParseMarketPage(index)
	rows := append([]fleetplan.MarketListing{}, first...)
	shown := map[string]bool{}
	for _, r := range first {
		shown[r.Market] = true
	}
	for _, c := range chars {
		if shown[c.Name] {
			continue
		}
		page, err := fetchPage(MarketPageURL + "?character=" + strconv.Itoa(c.ID))
		if err != nil {
			continue
		}
		_, _, more := fleetplan.ParseMarketPage(page)
		rows = append(rows, more...)
	}
	marketCache.rows = rows
	if log != nil {
		log(fmt.Sprintf("market page: %d listings from %d market characters", len(rows), len(chars)))
	}
	return rows
}
