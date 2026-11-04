// Package worlds turns a world NUMBER into the two endpoints a login needs: the game
// WebSocket URL and the HTTP base. It is pure — no network, no client, no fleet — so the
// mapping can be unit-tested and so every session-build site can share one answer.
//
// WHY THIS IS THE WHOLE MECHANISM, measured 2026-09-22 rather than assumed:
//
//   - w2.example.invalid is live, and its /crc0's first 36 bytes (the nine archive checksums the
//     login block carries) are BYTE-IDENTICAL to w1.example.invalid's. So there is no per-world
//     CRC table: a fleet that bakes crcTable once can still send it on either world.
//   - Its RSA login modulus, scraped from /client/client.js the way
//     fleet.FetchProductionModulus does, is also byte-identical to w1's. So there is no
//     per-world key handling either. (This package still lets a world carry its own HTTP
//     base, because that is where a key would be re-fetched from if the two ever diverge —
//     see Map.Endpoints.)
//   - The login block carries NO world number. grep internal/login: nothing writes one.
//     config.Server.World exists and is documented as client-side state only ("the server
//     picks the world by the host dialled"), and nothing in the login path reads it.
//
// So a world is purely a different wsURL. Everything in this package is about getting the
// right string to client.Config.WSURL, and nothing else.
//
// DERIVATION AND WHY IT IS NOT THE ONLY SOURCE. Production hosts are w1.example.invalid,
// w2.example.invalid, ... so Derive rewrites the leading "w<digits>." label of the fleet's own
// configured host. That is a guess about a naming convention, and a guess is exactly the
// wrong thing to point real accounts at, so an explicit per-world entry in the roster
// always wins over it (see Map.Endpoints). Derivation exists so the common case —
// "everything is wN.example.invalid" — needs no extra config at all.
//
// A HOST WITH NO wN LABEL (a local Engine-TS at ws://localhost/, or any custom host) has no
// derivable second world: Derive returns the SAME endpoints for every world number and says
// so via Derived.Distinct. That is not a failure — it is the honest answer, and it is what
// makes a local-engine world-hop proof a proof of the logout/login cycle only, never a proof
// that two worlds were involved. Callers that need to know the difference ask Distinct.
package worlds

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Min and Max bound a world number. The upper bound matches internal/config's own
// validate.go ("server.world", 1..255); the lower bound is 1 because there is no world 0.
const (
	Min = 1
	Max = 255
)

// Default is the world an account that names none belongs to.
const Default = 1

// Endpoints is one world's two login endpoints.
type Endpoints struct {
	// World is the number these endpoints belong to.
	World int
	// WSURL is what client.Config.WSURL is set to for an account on this world.
	WSURL string
	// HTTPBase is where /crc0 and the RSA modulus would be read from for this world.
	// Measured identical across w1/w2 today (see the package doc), kept per-world so a
	// future divergence is a config change rather than a code change.
	HTTPBase string
}

// hostWorld matches a leading "w<digits>" label, e.g. the "w1" of "w1.example.invalid".
var hostWorld = regexp.MustCompile(`^w([0-9]+)\.`)

// HostWorld reports the world number a host's leading "wN." label names, and whether it had
// one. "w2.example.invalid" -> 2, true. "localhost" -> 0, false.
func HostWorld(host string) (int, bool) {
	m := hostWorld.FindStringSubmatch(host)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < Min || n > Max {
		return 0, false
	}
	return n, true
}

// URLWorld reports the world a full URL's host names, and whether it had one.
func URLWorld(raw string) (int, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return 0, false
	}
	return HostWorld(u.Hostname())
}

// rewriteHost returns raw with its host's leading "wN." label replaced by "w<world>.",
// preserving scheme, port, path and query. ok is false when there was no such label.
func rewriteHost(raw string, world int) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := u.Hostname()
	if !hostWorld.MatchString(host) {
		return "", false
	}
	newHost := hostWorld.ReplaceAllString(host, fmt.Sprintf("w%d.", world))
	if port := u.Port(); port != "" {
		newHost += ":" + port
	}
	u.Host = newHost
	return u.String(), true
}

// Map answers "which endpoints does world N use", from explicit entries first and the
// fleet's own configured endpoints second. Its zero value is not usable — build one with
// New.
type Map struct {
	// fleetWS/fleetHTTP are the roster's own wsURL/httpBase: the endpoints of whichever
	// world the fleet is configured for.
	fleetWS, fleetHTTP string
	// fleetWorld is the world fleetWS names, Default when its host has no wN label.
	fleetWorld int
	// distinct records whether derivation can actually produce different hosts, i.e.
	// whether fleetWS's host carried a wN label at all.
	distinct bool
	// explicit is the roster's own per-world overrides, which always win over derivation.
	explicit map[int]Endpoints
	// listed is the worlds an operator said this deployment has, over and above the ones
	// with explicit endpoints. See Available.
	listed map[int]bool
}

// New builds a Map from the fleet's own configured endpoints plus any explicit per-world
// overrides (nil is fine — derivation alone). It fails rather than guessing when an
// override names an out-of-range world or carries neither endpoint, because a silently
// ignored world entry would point accounts at the fleet default while the operator believed
// they were split.
func New(fleetWS, fleetHTTP string, explicit map[int]Endpoints) (*Map, error) {
	if strings.TrimSpace(fleetWS) == "" {
		return nil, fmt.Errorf("worlds: the fleet's own wsURL is empty")
	}
	m := &Map{fleetWS: fleetWS, fleetHTTP: fleetHTTP, fleetWorld: Default, explicit: map[int]Endpoints{}}
	if n, ok := URLWorld(fleetWS); ok {
		m.fleetWorld, m.distinct = n, true
	}
	m.listed = map[int]bool{}
	for n, e := range explicit {
		if n < Min || n > Max {
			return nil, fmt.Errorf("worlds: world %d is out of range %d..%d", n, Min, Max)
		}
		if strings.TrimSpace(e.WSURL) == "" && strings.TrimSpace(e.HTTPBase) == "" {
			return nil, fmt.Errorf("worlds: world %d names neither wsURL nor httpBase", n)
		}
		e.World = n
		m.explicit[n] = e
	}
	return m, nil
}

// List records the worlds this deployment has, for a Map whose hosts are derivable and so
// need no explicit endpoints — the live server case, where "worlds": {...} would be pure
// boilerplate but the fleet still has to know that 1, 2 and 3 exist and 4 does not.
//
// THE NUMBER OF WORLDS IS CONFIGURATION, NOT CODE. w2 opened on 2026-09-22 and w3 was
// announced the same day; nothing here caps the count, and adding a world is an edit to a
// roster plus a restart. An out-of-range entry is an error rather than being skipped, for
// the same reason New rejects a bad explicit entry.
func (m *Map) List(ns []int) error {
	for _, n := range ns {
		if n < Min || n > Max {
			return fmt.Errorf("worlds: world %d is out of range %d..%d", n, Min, Max)
		}
		m.listed[n] = true
		if n != m.fleetWorld {
			// An operator naming a second world is asserting this deployment has more than
			// one, which is what Distinct answers. Without this a derivable host that was
			// listed but not overridden would still report a single world.
			m.distinct = true
		}
	}
	return nil
}

// Available is every world this deployment knows about, ascending: the fleet's own, plus
// every explicitly configured world, plus every listed one. Always contains at least the
// fleet's own world.
//
// It is what the fleet may choose among — it is NOT a claim that each is reachable. A world
// that is announced but not yet running answers nothing; probing that is
// internal/fleet.ProbeWorlds's job, deliberately separate so this package stays pure and so
// an unreachable world is a "not available" line at startup rather than a refusal to start.
func (m *Map) Available() []int {
	seen := map[int]bool{m.fleetWorld: true}
	for n := range m.explicit {
		seen[n] = true
	}
	for n := range m.listed {
		seen[n] = true
	}
	out := make([]int, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// FleetWorld is the world the fleet's own wsURL names — the world an account that names
// none ends up on. Default when the host carries no wN label.
func (m *Map) FleetWorld() int { return m.fleetWorld }

// Distinct reports whether this Map can produce genuinely different hosts for different
// world numbers. False for a local engine or any host with no wN label AND no explicit
// per-world entries: every world then resolves to the same endpoints.
//
// This exists so a harness cannot dress a single-world logout/login cycle up as a two-world
// proof. Ask it, and say what it answered.
func (m *Map) Distinct() bool { return m.distinct || len(m.explicit) > 0 }

// Explicit reports whether world n was configured by hand rather than derived.
func (m *Map) Explicit(n int) bool { _, ok := m.explicit[n]; return ok }

// Endpoints resolves world n. n <= 0 means "the fleet's own world" and returns the fleet's
// configured endpoints unchanged — the exact bytes a single-world fleet has always used, so
// an account that names no world is byte-for-byte unaffected by this package existing.
//
// Resolution order: an explicit entry (its blank half filled from the fleet's own), then
// derivation from the fleet's host, then — for a host with no wN label — the fleet's own
// endpoints again. The last case is why Distinct exists.
func (m *Map) Endpoints(n int) (Endpoints, error) {
	if n <= 0 {
		return Endpoints{World: m.fleetWorld, WSURL: m.fleetWS, HTTPBase: m.fleetHTTP}, nil
	}
	if n > Max {
		return Endpoints{}, fmt.Errorf("worlds: world %d is out of range %d..%d", n, Min, Max)
	}
	if e, ok := m.explicit[n]; ok {
		if e.WSURL == "" {
			e.WSURL = m.derivedWS(n)
		}
		if e.HTTPBase == "" {
			e.HTTPBase = m.derivedHTTP(n)
		}
		return e, nil
	}
	return Endpoints{World: n, WSURL: m.derivedWS(n), HTTPBase: m.derivedHTTP(n)}, nil
}

func (m *Map) derivedWS(n int) string {
	if s, ok := rewriteHost(m.fleetWS, n); ok {
		return s
	}
	return m.fleetWS
}

func (m *Map) derivedHTTP(n int) string {
	if m.fleetHTTP == "" {
		return ""
	}
	if s, ok := rewriteHost(m.fleetHTTP, n); ok {
		return s
	}
	return m.fleetHTTP
}

// Normalise turns an account's configured world number into the one to actually use: 0 or
// absent becomes the fleet's own world, anything else is returned as given. Out-of-range
// numbers are an error rather than a silent clamp, for the same reason New rejects one.
func (m *Map) Normalise(n int) (int, error) {
	if n <= 0 {
		return m.fleetWorld, nil
	}
	if n > Max {
		return 0, fmt.Errorf("worlds: world %d is out of range %d..%d", n, Min, Max)
	}
	return n, nil
}

// Entry is the JSON shape a roster's optional "worlds" object uses:
//
//	"worlds": { "2": { "wsURL": "wss://w2.example.invalid/", "httpBase": "https://w2.example.invalid/" } }
//
// Keyed by world number as a string because that is what a JSON object key is. Both halves
// are optional: whichever is omitted is derived from the fleet's own endpoints.
//
// The whole object is optional too, and on the live server it is unnecessary — Derive already turns
// w1.example.invalid into w2.example.invalid. It exists so a deployment whose hosts do not follow that
// convention can state them rather than have this package guess.
type Entry struct {
	WSURL    string `json:"wsURL"`
	HTTPBase string `json:"httpBase"`
}

// ParseEntries turns a roster's "worlds" object into the map New takes. A key that is not a
// world number is an error rather than being skipped, for the reason New rejects a bad
// entry: an operator who wrote it believes those accounts moved.
func ParseEntries(m map[string]Entry) (map[int]Endpoints, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[int]Endpoints, len(m))
	for k, v := range m {
		n, err := strconv.Atoi(strings.TrimSpace(k))
		if err != nil {
			return nil, fmt.Errorf("worlds: %q is not a world number", k)
		}
		if n < Min || n > Max {
			return nil, fmt.Errorf("worlds: world %d is out of range %d..%d", n, Min, Max)
		}
		out[n] = Endpoints{World: n, WSURL: strings.TrimSpace(v.WSURL), HTTPBase: strings.TrimSpace(v.HTTPBase)}
	}
	return out, nil
}
