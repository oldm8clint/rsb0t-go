package fleet

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// RouteCap names one route a pin can point at and how many accounts it may hold.
type RouteCap struct {
	ID  string
	Max int
}

// PinKey is the key an account's route pin is stored under: its name lower-cased with runs of
// spaces collapsed, so "Acct12", "acct12" and "acct  12" share one pin.
func PinKey(username string) string {
	return strings.Join(strings.Fields(strings.ToLower(username)), " ")
}

// PlanRoutePins gives every username exactly one route and keeps it there across restarts, so
// each account only ever logs in from one address. A pin in existing that still names a route in
// routes is kept, up to that route's Max (live 5 Nov 2026: warp's Max dropped from 90 to 30 and the
// 60 pins over it waited for a slot for hours while other routes had room); every other account, in
// the order given, goes to the route with the most room left (Max minus pins), ties to the earlier
// route. When every route is full the account still
// gets the least-loaded route rather than none: it then waits for a slot there instead of moving.
// Pins for accounts not in usernames are dropped.
func PlanRoutePins(existing map[string]string, usernames []string, routes []RouteCap) map[string]string {
	out := make(map[string]string, len(usernames))
	if len(routes) == 0 {
		return out
	}
	capOf := make(map[string]int, len(routes))
	for _, r := range routes {
		capOf[r.ID] = r.Max
	}
	load := make(map[string]int, len(routes))
	var todo []string
	seen := make(map[string]bool, len(usernames))
	for _, u := range usernames {
		k := PinKey(u)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if id, ok := existing[k]; ok && load[id] < capOf[id] {
			out[k] = id
			load[id]++
			continue
		}
		todo = append(todo, k)
	}
	for _, k := range todo {
		best := routes[0].ID
		bestRoom := routes[0].Max - load[best]
		for _, r := range routes[1:] {
			if room := r.Max - load[r.ID]; room > bestRoom {
				best, bestRoom = r.ID, room
			}
		}
		out[k] = best
		load[best]++
	}
	return out
}

// RouteCaps lists the pool's enabled routes in the order the pool tries them.
func (cfg *RoutePool) RouteCaps() []RouteCap {
	var caps []RouteCap
	if cfg.Direct.Enabled {
		caps = append(caps, RouteCap{ID: "direct", Max: cfg.Direct.Max})
	}
	for _, r := range cfg.Routes {
		if r.Enabled {
			caps = append(caps, RouteCap{ID: r.ID, Max: r.Max})
		}
	}
	return caps
}

// LoadRoutePins reads a pin file (a JSON object of PinKey to route id). A missing file is an
// empty set of pins.
func LoadRoutePins(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	pins := map[string]string{}
	if err := json.Unmarshal(b, &pins); err != nil {
		return nil, err
	}
	return pins, nil
}

// SaveRoutePins writes pins as a sorted, indented JSON object through a temporary file and rename.
func SaveRoutePins(path string, pins map[string]string) error {
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(pins[k])
		sb.WriteString("  ")
		sb.Write(kb)
		sb.WriteString(": ")
		sb.Write(vb)
		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
