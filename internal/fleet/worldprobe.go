package fleet

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/worlds"
)

// Probing whether a world is actually there.
//
// WHY. The fleet owner's 2026-09-22 note: "world 1 has a limit of 1k players now, world 2 is
// out, world 3 is coming". A world that is announced but not yet running must be a clean
// "not available" line at startup, not a fleet that refuses to start — and equally, an
// account must not be pointed at a world nobody has checked exists.
//
// WHAT THIS DOES, AND WHAT IT DELIBERATELY DOES NOT. It makes ONE ordinary GET of each
// world's /crc0 — the same read-only route every login already performs, the one the fleet
// does dozens of times a minute in normal operation. It does NOT log in, does NOT open a
// game WebSocket, does NOT send credentials, and cannot consume a login-rate budget, take a
// connection slot, or disturb a session. That property is the whole point: it is safe to run
// against production while the live fleet is up.
//
// It answers "is there a server here", not "may this account log in". A world that is FULL
// (login response 7) answers /crc0 perfectly happily, and so does a world this address is at
// its connection ceiling on. Those are login-time answers and are handled where they arrive
// — see internal/scripts/worldhopplan.

// probeTimeout bounds one world's check. Short: /crc0 is 36 bytes off a CDN-fronted host,
// and a world that cannot answer that in five seconds is not a world this fleet should be
// putting accounts on.
const probeTimeout = 5 * time.Second

// WorldProbe is one world's result.
type WorldProbe struct {
	World int
	// HTTPBase is what was asked.
	HTTPBase string
	// Available is true when the world answered its /crc0 with a usable checksum block.
	Available bool
	// Err says why not, empty when Available.
	Err string
}

// ProbeWorlds checks every world a Map knows about (worlds.Map.Available) and reports which
// answered. Concurrent, bounded by probeTimeout each, and read-only.
//
// It never returns an error for an unavailable world: "world 3 is not answering yet" is an
// ordinary fact about a world that has been announced and not launched, and the caller's job
// is to log it and carry on with the worlds that did answer. The only thing that stops the
// fleet is having no world at all, which is a decision for the caller, not for this function.
func ProbeWorlds(wm *worlds.Map) []WorldProbe {
	if wm == nil {
		return nil
	}
	list := wm.Available()
	out := make([]WorldProbe, len(list))
	var wg sync.WaitGroup
	for i, n := range list {
		e, err := wm.Endpoints(n)
		if err != nil {
			out[i] = WorldProbe{World: n, Err: err.Error()}
			continue
		}
		out[i] = WorldProbe{World: n, HTTPBase: e.HTTPBase}
		if e.HTTPBase == "" {
			out[i].Err = "no httpBase configured for this world"
			continue
		}
		wg.Add(1)
		go func(i int, base string) {
			defer wg.Done()
			if err := probeOne(base); err != nil {
				out[i].Err = err.Error()
				return
			}
			out[i].Available = true
		}(i, e.HTTPBase)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].World < out[j].World })
	return out
}

// probeOne fetches one world's /crc0 and checks it is long enough to be the archive
// checksum block a login sends. Nothing is kept: this asks whether a server is there, and
// the login path re-reads /crc0 for itself.
func probeOne(httpBase string) error {
	url := strings.TrimRight(httpBase, "/") + "/crc0"
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered HTTP %d", url, resp.StatusCode)
	}
	// The login block carries the first 36 bytes (nine 32-bit checksums). A shorter body is
	// the 2026-09-20 outage's own signature — an HTTP layer up and answering while the game
	// server behind it restarts — and is exactly the case this must not call "available".
	var buf [36]byte
	n, _ := io.ReadFull(resp.Body, buf[:])
	if n < len(buf) {
		return fmt.Errorf("%s returned %d bytes, want at least %d", url, n, len(buf))
	}
	return nil
}

// AvailableWorlds is ProbeWorlds reduced to the world numbers that answered.
func AvailableWorlds(probes []WorldProbe) []int {
	var out []int
	for _, p := range probes {
		if p.Available {
			out = append(out, p.World)
		}
	}
	return out
}

// DescribeWorlds turns probe results into one startup line per world, the same shape
// RouteDescriptions already uses. No credentials, nothing but a host and a verdict.
func DescribeWorlds(probes []WorldProbe) []string {
	out := make([]string, 0, len(probes))
	for _, p := range probes {
		if p.Available {
			out = append(out, fmt.Sprintf("world %d: available (%s)", p.World, p.HTTPBase))
			continue
		}
		out = append(out, fmt.Sprintf("world %d: NOT AVAILABLE (%s) — accounts assigned to it will not connect: %s", p.World, p.HTTPBase, p.Err))
	}
	return out
}
