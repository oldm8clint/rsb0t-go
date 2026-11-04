package controlpanel

// What the fleet's banks are WORTH, and what experience is sitting in them unprocessed.
//
// The account owner's ask for the Combined bank panel was three things: "combined bank should
// follow same market pricing/falling back in game pricing/high alch price and displayed some
// where in that panel ... combined bank should also show banked exp from processing all our
// goods in various means."
//
// Pricing reuses the Loot panel's PriceBook (marketprice.go) rather than adding a second price
// source: the market's own median traded price, then high alchemy (60% of the cache's unit
// cost, the game's own rule), then that cost. The difference is the whole point here — an uncut
// sapphire trades around 800 against a cache cost of 25, and this fleet banks millions of raw
// fish — so every figure the panel shows carries the source that answered, and the panel is
// built to show a high-alch estimate as an estimate rather than as a quote.
//
// Experience comes from internal/bankxp, whose table is GENERATED from the content checkout
// this server runs. Nothing here invents an XP number.
//
// Cost of all this: zero extra fleet snapshots. Both sit on the single AllBanks() pass
// handleBankAll already makes (see bankall.go and cfbdf17's own note about the 267-snapshot
// regression), and the route table is static, so it is served from its own endpoint and fetched
// once per panel rather than every five seconds.

import (
	"net/http"
	"sort"

	"rsb0t-go/internal/bankxp"
	"rsb0t-go/internal/cachecfg"
)

// priceEntry is one item's unit value as the panel receives it.
type priceEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Unit is the value of ONE of this item, in coins. Absent (0) with Source "none" means
	// the value is genuinely unknown, which the panel must show as unknown and never as zero
	// — the same rule marketprice.go states for a missing price.
	Unit   int64  `json:"unit"`
	Source string `json:"source"`
	// Trades and Units are the market sample behind a "market" figure, so a viewer can see
	// that a price came from two trades before trusting it. Absent for the other sources.
	Trades int   `json:"trades,omitempty"`
	Units  int64 `json:"units,omitempty"`
	// Cost is the cache's own unit cost, kept so the panel can show what the old figure
	// would have been beside what the item is really worth.
	Cost int64 `json:"cost,omitempty"`
}

// priceBookInfo is the panel's "where these numbers came from" line.
type priceBookInfo struct {
	// Enabled is false when the fleet was started without -market-prices; every value is then
	// high alchemy and then cache cost, and the panel says so instead of implying a market.
	Enabled bool `json:"enabled"`
	// Items is how many items the market table holds; 0 with Enabled true means it has not
	// come back yet (the first fetch is a background refresh, see PriceBook.Refresh).
	Items int `json:"items"`
	// FetchedUnixMs is when the table was last refreshed, 0 when never.
	FetchedUnixMs int64 `json:"fetchedAt"`
	// Error is the last refresh failure, empty when the last one worked.
	Error string `json:"error,omitempty"`
	// Cache is false when this process has no config archive, which means no cache costs and
	// therefore no high-alchemy fallback — a state the panel has to admit to rather than
	// quietly valuing half the bank at nothing.
	Cache bool `json:"cache"`
}

// objNames is the item-name/cost source for valuation: the same config archive FleetSource
// already keeps for the bank panel's own item names.
func (s *Server) objNames() *cachecfg.Config {
	if c := s.streamNamesConfig(); c != nil {
		return c
	}
	// A panel started with a baked loc index but no fleet (the POI/map path) still has a
	// config archive, and it holds obj records too.
	return s.cfg.LocTypes
}

// valueBanks builds the price table for exactly the item ids present in one bank-all answer.
//
// Only the ids in the answer are priced, so the response grows with the fleet's distinct items
// (a few hundred) rather than with the market's whole catalogue.
func (s *Server) valueBanks(banks []bankBody) ([]priceEntry, priceBookInfo) {
	cache := s.objNames()
	info := priceBookInfo{Enabled: s.cfg.Prices != nil, Cache: cache != nil}
	if s.cfg.Prices != nil {
		// Asking for a refresh here is what the Loot panel does; it returns immediately and
		// the first caller after the TTL starts a background fetch.
		s.cfg.Prices.Refresh()
		items, fetched, err := s.cfg.Prices.Stats()
		info.Items = items
		if !fetched.IsZero() {
			info.FetchedUnixMs = fetched.UnixMilli()
		}
		if err != nil {
			info.Error = err.Error()
		}
	}

	type seen struct{ name string }
	ids := map[int]seen{}
	for _, b := range banks {
		for _, it := range b.Items {
			id := int(it.ID)
			cur, ok := ids[id]
			if !ok || (cur.name == "" && it.Name != "") {
				ids[id] = seen{name: it.Name}
			}
		}
	}

	out := make([]priceEntry, 0, len(ids))
	for id, s0 := range ids {
		name := s0.name
		var cost int64
		if cache != nil {
			if obj, ok := cache.Obj(id); ok {
				if name == "" {
					name = obj.Name
				}
				if obj.Cost > 0 {
					cost = int64(obj.Cost)
				}
			}
		}
		e := priceEntry{ID: id, Name: name, Cost: cost}
		if name == "" && cost == 0 {
			// Nothing to go on: neither a name to look up nor a cost to alch. Saying so is
			// the only honest answer.
			e.Source = "none"
			out = append(out, e)
			continue
		}
		unit, src := s.cfg.Prices.Value(name, cost)
		if unit <= 0 {
			e.Source = "none"
			out = append(out, e)
			continue
		}
		e.Unit, e.Source = unit, string(src)
		if src == ValueFromMarket && s.cfg.Prices != nil {
			if mp, ok := s.cfg.Prices.Lookup(name); ok {
				e.Trades, e.Units = mp.Trades, mp.Units
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, info
}

// ---- banked experience ----

// bankXPAccount is one account's share of the banked experience. Only totals travel per
// account: at 269 accounts the per-item lines would be the biggest thing in the response, and
// the combined plan below already carries them.
type bankXPAccount struct {
	Account string              `json:"account"`
	XP      int64               `json:"xp"`
	Skills  []bankxp.SkillTotal `json:"skills,omitempty"`
	// Unrouted and LevelBlocked are individual items, not kinds, so an operator can see how
	// much of a bank the figure is simply not speaking for.
	Unrouted     int64 `json:"unrouted"`
	LevelBlocked int64 `json:"levelBlocked"`
	// LevelsKnown is false when the fleet reported no skills for this account (it is offline,
	// or has never sent a stat update). With levels requested and none known, every route is
	// gated out, so the panel must be able to say "unknown" rather than "zero".
	LevelsKnown bool `json:"levelsKnown"`
}

// bankXPBody is the whole banked-experience answer.
type bankXPBody struct {
	// Policy and Levels echo what was actually applied, so a screenshot of the panel is
	// self-describing.
	Policy string  `json:"policy"`
	Levels string  `json:"levels"`
	Rate   float64 `json:"rate"`
	// Combined is the fleet's whole banked pool as one plan, with its per-item lines.
	Combined bankxp.Plan `json:"combined"`
	// CombinedLevels is the per-skill level the combined plan was gated at: the HIGHEST any
	// account in the fleet has, because the pool would be processed by whoever is best placed
	// to. Empty when levels were not applied.
	CombinedLevels map[string]int  `json:"combinedLevels,omitempty"`
	Accounts       []bankXPAccount `json:"accounts"`
	// Routes is how many processing routes the table holds, and Mismatches how many of them
	// the running item cache disagrees with (see routeView.NameOK).
	Routes     int `json:"routes"`
	Mismatches int `json:"mismatches"`
}

// bankXPFor builds the banked-experience answer for one set of banks.
//
// It never takes a fleet snapshot of its own: the banks are the ones handleBankAll already has,
// and the skill levels come from the same cached status snapshot /api/status serves.
func (s *Server) bankXPFor(banks []AccountBank, policy bankxp.Policy, useLevels bool, rate float64) bankXPBody {
	cache := s.objNames()
	body := bankXPBody{Policy: string(policy), Levels: "ignore", Rate: rate, Routes: len(bankxp.Routes)}
	if useLevels {
		body.Levels = "current"
	}

	// Refuse to apply a route whose primary item this server's own cache calls something
	// else. That disagreement means the committed table and the running content are out of
	// step, and crediting a bank with the wrong item's experience would be exactly the kind
	// of confident wrong number this repository keeps having to undo.
	blocked := map[int]bool{}
	if cache != nil {
		for _, r := range bankxp.Routes {
			p := r.Primary()
			obj, ok := cache.Obj(p.ID)
			if !ok || !bankxp.NameMatches(obj.Name, p.Name) {
				blocked[p.ID] = true
			}
		}
		body.Mismatches = len(blocked)
	}

	best := bankxp.Levels{}
	for _, b := range banks {
		for skill, level := range b.Levels {
			if level > best[skill] {
				best[skill] = level
			}
		}
	}

	combined := map[int]int64{}
	names := map[int]string{}
	for _, b := range banks {
		have := map[int]int64{}
		for _, it := range b.Items {
			id := int(it.ID)
			if blocked[id] {
				continue
			}
			have[id] += int64(it.Count)
			combined[id] += int64(it.Count)
			if it.Name != "" {
				names[id] = it.Name
			}
		}
		row := bankXPAccount{Account: b.Account}
		opt := bankxp.Options{Policy: policy, XPRate: rate}
		if useLevels {
			row.LevelsKnown = len(b.Levels) > 0
			if !row.LevelsKnown {
				// No levels reported: say so and count nothing, rather than silently
				// reporting zero as if the bank were worthless.
				body.Accounts = append(body.Accounts, row)
				continue
			}
			opt.Levels = bankxp.Levels(b.Levels)
		}
		p := bankxp.Compute(have, names, opt)
		row.XP, row.Skills, row.Unrouted, row.LevelBlocked = p.XP, p.Skills, p.Unrouted, p.LevelBlocked
		body.Accounts = append(body.Accounts, row)
	}

	opt := bankxp.Options{Policy: policy, XPRate: rate}
	if useLevels {
		// The combined pool is not one account's, so it is gated at the BEST level in the
		// fleet for each skill: if the experience is ever collected, it is collected by
		// whoever is placed to collect it. The levels used are reported so this assumption is
		// visible rather than buried.
		opt.Levels = best
		body.CombinedLevels = best
	}
	body.Combined = bankxp.Compute(combined, names, opt)
	return body
}

// ---- the processing-route table ----

// routeView is one bankxp.Route as the panel receives it, plus the one thing the panel cannot
// work out for itself: whether the running server's own item cache agrees with the content
// checkout the table was generated from.
type routeView struct {
	bankxp.Route
	// NameOK is false when the cache resolves this route's primary input id to a DIFFERENT
	// name than content did. That means the table and the live server disagree about what
	// item that id is, and the panel refuses to apply the route rather than quietly crediting
	// a bank with the wrong item's experience.
	NameOK bool `json:"nameOk"`
	// CacheName is what the running cache calls the primary input, when it differs.
	CacheName string `json:"cacheName,omitempty"`
}

type itemRoutesBody struct {
	Routes []routeView `json:"routes"`
	// Skills and Scripts are for the panel's legend and its "route the fleet runs" filter.
	Skills  []string `json:"skills"`
	Scripts []string `json:"scripts"`
	// Cache says whether a config archive was available to check the table against. With no
	// cache every NameOK is reported as true and unchecked; the panel says "unchecked".
	Cache bool `json:"cache"`
	// Mismatches is how many routes failed the name check, so the panel can warn once instead
	// of per row.
	Mismatches int `json:"mismatches"`
	// XPScale documents the unit of Route.XP10 to anyone reading the API by hand.
	XPScale int `json:"xpScale"`
}

// handleItemRoutes answers GET /api/item-routes with the generated processing table.
//
// The table is static for the life of the binary, so the panel fetches it once. It is served
// separately from /api/bank-all for exactly that reason: shipping ~150 routes with every
// five-second bank refresh would be pure waste.
func (s *Server) handleItemRoutes(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	cache := s.objNames()
	body := itemRoutesBody{
		Routes:  make([]routeView, 0, len(bankxp.Routes)),
		Skills:  bankxp.Skills(),
		Scripts: bankxp.FleetScripts(),
		Cache:   cache != nil,
		XPScale: 10,
	}
	for _, route := range bankxp.Routes {
		v := routeView{Route: route, NameOK: true}
		if cache != nil {
			if obj, ok := cache.Obj(route.Primary().ID); ok {
				if !bankxp.NameMatches(obj.Name, route.Primary().Name) {
					v.NameOK = false
					v.CacheName = obj.Name
					body.Mismatches++
				}
			} else {
				v.NameOK = false
				v.CacheName = ""
				body.Mismatches++
			}
		}
		body.Routes = append(body.Routes, v)
	}
	writeJSON(w, http.StatusOK, body)
}
