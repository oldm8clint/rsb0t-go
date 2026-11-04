package client

import (
	"hash/fnv"
	"net/http"
	"net/url"
	"strings"
)

// browserAgents are User-Agent strings of current desktop browsers. An account keeps one
// for good, as a player keeps their browser.
var browserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36 Edg/152.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:155.0) Gecko/20100101 Firefox/155.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:155.0) Gecko/20100101 Firefox/155.0",
}

var browserLanguages = []string{
	"en-US,en;q=0.9",
	"en-GB,en;q=0.9",
	"en-AU,en;q=0.9",
	"en-US,en;q=0.9,es;q=0.8",
	"en-CA,en;q=0.9,fr;q=0.8",
}

// BrowserHeader returns the upgrade headers a browser would send when the web client at
// httpBase opens its game WebSocket: an Origin (every browser sends one) and a User-Agent and
// Accept-Language picked stably from username, so an account always looks like the same
// browser across restarts.
func BrowserHeader(username, httpBase string) http.Header {
	h := fnv.New32a()
	h.Write([]byte("rsb0t-browser:"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(username))))
	n := h.Sum32()
	hdr := http.Header{}
	hdr.Set("User-Agent", browserAgents[n%uint32(len(browserAgents))])
	hdr.Set("Accept-Language", browserLanguages[(n/7)%uint32(len(browserLanguages))])
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Pragma", "no-cache")
	if u, err := url.Parse(httpBase); err == nil && u.Scheme != "" && u.Host != "" {
		hdr.Set("Origin", u.Scheme+"://"+u.Host)
	}
	return hdr
}
