package controlpanel

import (
	"sort"

	"rsb0t-go/internal/deathloot"
	"rsb0t-go/internal/loot"
)

// THE DEATHS VIEW OF THE LOOT PANEL (a user request: "we should have a loot panel that shows deaths and items that are lost and
// never picked up by the account that died or another account in our fleet").
//
// The fleet keeps a ledger of every death (internal/deathloot): the items the death dropped, and every pickup of them it saw by the dead
// account or any other (fleet.Manager.DeathEvents). This aggregator polls that with the loot totals, keeps its own copy in the stats file
// (lootFile.Deaths, so it survives a panel restart and a fleet restart alike), and values each stack with the PriceBook the rest of the
// panel uses (market median, then high alchemy, then cache cost) when /api/loot is read.
//
// A stack is RECOVERED for the items a pickup was seen for, OPEN while its pile can still be taken (the drop lives 200 ticks) and LOST
// FOR GOOD once that window has closed with items left. Whether a gain was a pickup of the pile is deathloot's rule; the panel says it
// is inferred, not read off the ground.

// DeathSource is optional: a Source that reports the fleet's deaths feeds the Deaths view. fleet.Manager does, through FleetSource.
type DeathSource interface {
	DeathEvents() []deathloot.Death
}

// DeathEvents answers DeathSource from the fleet manager's ledger.
func (f *FleetSource) DeathEvents() []deathloot.Death {
	if f.manager == nil {
		return nil
	}
	return f.manager.DeathEvents()
}

const (
	// maxDeathRows bounds the per-death rows /api/loot sends per period; the totals and the breakdowns cover every death.
	maxDeathRows = 200
	// maxDeathGroups bounds each breakdown.
	maxDeathGroups = 15
	// maxLostItems bounds the "lost for good" item list.
	maxLostItems = 12
)

// Item statuses and death statuses.
const (
	DeathRecovered = "recovered"
	DeathPartial   = "partial"
	DeathOpen      = "open"
	DeathLost      = "lost"
	DeathNothing   = "nothing"
)

// DeathTaker is who took how many of a stack back.
type DeathTaker struct {
	Account string `json:"account"`
	Count   int64  `json:"count"`
	At      int64  `json:"at"`
	// Self is true for the account that died.
	Self bool `json:"self,omitempty"`
}

// DeathItemView is one lost stack.
type DeathItemView struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	Recovered int64  `json:"recovered"`
	// Unit is what one is worth, Value the whole stack, RecoveredValue the part picked up.
	Unit           int64        `json:"unit"`
	Value          int64        `json:"value"`
	RecoveredValue int64        `json:"recoveredValue"`
	Status         string       `json:"status"`
	By             []DeathTaker `json:"by,omitempty"`
}

// DeathKept is an item the server handed back to the account (not lost).
type DeathKept struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// DeathView is one death.
type DeathView struct {
	ID       string `json:"id"`
	Account  string `json:"account"`
	Script   string `json:"script,omitempty"`
	At       int64  `json:"at"`
	HaveTile bool   `json:"haveTile"`
	X        int    `json:"x"`
	Z        int    `json:"z"`
	Level    int    `json:"level"`
	Place    string `json:"place"`
	Cause    string `json:"cause"`
	Status   string `json:"status"`
	// Value is what the pile was worth; Recovered the part of it picked up; Lost the part that is gone for good (the window has closed);
	// Open the part still lying there inside the window. Recovered + Lost + Open = Value.
	Value     int64           `json:"value"`
	Recovered int64           `json:"recovered"`
	Lost      int64           `json:"lost"`
	Open      int64           `json:"open"`
	Items     []DeathItemView `json:"items"`
	Kept      []DeathKept     `json:"kept,omitempty"`
}

// DeathGroup is one row of a breakdown: the deaths that share a place, a cause or an account.
type DeathGroup struct {
	Name      string `json:"name"`
	Deaths    int64  `json:"deaths"`
	Value     int64  `json:"value"`
	Recovered int64  `json:"recovered"`
	Lost      int64  `json:"lost"`
	Open      int64  `json:"open"`
}

// DeathLostItem is one item kind summed over the deaths of the period, for what is gone for good.
type DeathLostItem struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Value int64  `json:"value"`
}

// DeathsPeriod is the Deaths view of one period (lifetime or since restart).
type DeathsPeriod struct {
	Deaths int64 `json:"deaths"`
	// Value is everything the deaths dropped; Recovered what was picked up again (by the dead account, RecoveredSelf, or by another
	// account of the fleet, RecoveredOthers); Lost what is gone for good; Open what can still be taken.
	Value           int64 `json:"value"`
	Recovered       int64 `json:"recovered"`
	RecoveredSelf   int64 `json:"recoveredSelf"`
	RecoveredOthers int64 `json:"recoveredOthers"`
	Lost            int64 `json:"lost"`
	Open            int64 `json:"open"`
	First           int64 `json:"first,omitempty"`
	Last            int64 `json:"last,omitempty"`

	ByPlace   []DeathGroup    `json:"byPlace"`
	ByCause   []DeathGroup    `json:"byCause"`
	ByAccount []DeathGroup    `json:"byAccount"`
	LostItems []DeathLostItem `json:"lostItems"`
	// Rows are the newest deaths first, at most maxDeathRows; RowsShown is how many were cut off (Deaths - len(Rows)).
	Rows []DeathView `json:"rows"`
}

// deathsView reads the stored deaths into one period's view: the deaths from since on (0 for all), valued at now.
func deathsView(deaths []deathloot.Death, prices *PriceBook, now, since int64) *DeathsPeriod {
	p := &DeathsPeriod{ByPlace: []DeathGroup{}, ByCause: []DeathGroup{}, ByAccount: []DeathGroup{}, LostItems: []DeathLostItem{}, Rows: []DeathView{}}
	byPlace, byCause, byAcct := map[string]*DeathGroup{}, map[string]*DeathGroup{}, map[string]*DeathGroup{}
	lostItems := map[int]*DeathLostItem{}
	add := func(m map[string]*DeathGroup, name string, v DeathView) {
		g := m[name]
		if g == nil {
			g = &DeathGroup{Name: name}
			m[name] = g
		}
		g.Deaths++
		g.Value += v.Value
		g.Recovered += v.Recovered
		g.Lost += v.Lost
		g.Open += v.Open
	}
	var rows []DeathView
	for _, d := range deaths {
		if d.At < since {
			continue
		}
		v := deathView(d, prices, now)
		p.Deaths++
		p.Value += v.Value
		p.Recovered += v.Recovered
		p.Lost += v.Lost
		p.Open += v.Open
		if p.First == 0 || d.At < p.First {
			p.First = d.At
		}
		if d.At > p.Last {
			p.Last = d.At
		}
		for _, it := range v.Items {
			for _, by := range it.By {
				if by.Self {
					p.RecoveredSelf += it.Unit * by.Count
				} else {
					p.RecoveredOthers += it.Unit * by.Count
				}
			}
			if gone := it.Count - it.Recovered; gone > 0 && now > d.WindowEnd() {
				l := lostItems[it.ID]
				if l == nil {
					l = &DeathLostItem{ID: it.ID, Name: it.Name}
					lostItems[it.ID] = l
				}
				l.Count += gone
				l.Value += gone * it.Unit
			}
		}
		place := v.Place
		if place == "" {
			place = "unknown tile"
		}
		add(byPlace, place, v)
		add(byCause, v.Cause, v)
		add(byAcct, v.Account, v)
		rows = append(rows, v)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At > rows[j].At })
	if len(rows) > maxDeathRows {
		rows = rows[:maxDeathRows]
	}
	if rows != nil {
		p.Rows = rows
	}
	p.ByPlace, p.ByCause, p.ByAccount = groupList(byPlace), groupList(byCause), groupList(byAcct)
	for _, l := range lostItems {
		p.LostItems = append(p.LostItems, *l)
	}
	sort.Slice(p.LostItems, func(i, j int) bool {
		a, b := p.LostItems[i], p.LostItems[j]
		return a.Value > b.Value || a.Value == b.Value && (a.Count > b.Count || a.Count == b.Count && a.ID < b.ID)
	})
	if len(p.LostItems) > maxLostItems {
		p.LostItems = p.LostItems[:maxLostItems]
	}
	return p
}

// groupList orders a breakdown by what the deaths cost (value dropped, then deaths), biggest first.
func groupList(m map[string]*DeathGroup) []DeathGroup {
	out := make([]DeathGroup, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		if a.Deaths != b.Deaths {
			return a.Deaths > b.Deaths
		}
		return a.Name < b.Name
	})
	if len(out) > maxDeathGroups {
		out = out[:maxDeathGroups]
	}
	return out
}

// deathView values one death at now.
func deathView(d deathloot.Death, prices *PriceBook, now int64) DeathView {
	v := DeathView{
		ID: d.ID, Account: d.Account, Script: d.Script, At: d.At, HaveTile: d.HaveTile, X: d.X, Z: d.Z, Level: d.Level,
		Place: d.Place, Cause: d.Cause, Items: []DeathItemView{},
	}
	if v.Cause == "" {
		v.Cause = "unknown"
	}
	closed := now > d.WindowEnd()
	for _, s := range d.Items {
		unit := lootUnit(prices, loot.Item{ID: s.ID, Name: s.Name, Value: s.Cost})
		it := DeathItemView{ID: s.ID, Name: s.Name, Count: s.Count, Recovered: s.Taken(), Unit: unit}
		it.Value = unit * s.Count
		it.RecoveredValue = unit * it.Recovered
		left := s.Count - it.Recovered
		switch {
		case left <= 0:
			it.Status = DeathRecovered
		case it.Recovered > 0:
			it.Status = DeathPartial
		case closed:
			it.Status = DeathLost
		default:
			it.Status = DeathOpen
		}
		// Recoveries past the stack (a merge of two piles' worth) are cut at the count.
		budget := it.Recovered
		for _, r := range s.Recovered {
			n := r.Count
			if n > budget {
				n = budget
			}
			if n <= 0 {
				continue
			}
			budget -= n
			it.By = append(it.By, DeathTaker{Account: r.By, Count: n, At: r.At, Self: r.By == d.Account})
		}
		v.Value += it.Value
		v.Recovered += it.RecoveredValue
		if closed {
			v.Lost += unit * left
		} else {
			v.Open += unit * left
		}
		v.Items = append(v.Items, it)
	}
	sort.SliceStable(v.Items, func(i, j int) bool { return v.Items[i].Value > v.Items[j].Value })
	for _, k := range d.Kept {
		v.Kept = append(v.Kept, DeathKept{ID: k.ID, Name: k.Name, Count: k.Count})
	}
	switch {
	case len(v.Items) == 0:
		v.Status = DeathNothing
	case v.Open > 0 || (!closed && anyLeft(v.Items)):
		v.Status = DeathOpen
	case allRecovered(v.Items):
		v.Status = DeathRecovered
	case v.Recovered > 0 || anyRecovered(v.Items):
		v.Status = DeathPartial
	default:
		v.Status = DeathLost
	}
	return v
}

func anyLeft(items []DeathItemView) bool {
	for _, it := range items {
		if it.Count > it.Recovered {
			return true
		}
	}
	return false
}

func allRecovered(items []DeathItemView) bool { return !anyLeft(items) }

func anyRecovered(items []DeathItemView) bool {
	for _, it := range items {
		if it.Recovered > 0 {
			return true
		}
	}
	return false
}

// ---- the aggregator's half: keep, persist, poll ----

// noteDeaths folds the fleet's reading of its deaths into the stored copy, and marks the file dirty when something changed.
func (l *lootAggregator) noteDeaths(live []deathloot.Death) {
	if len(live) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	merged := deathloot.Merge(l.deaths, live, 0)
	if deathsDiffer(l.deaths, merged) {
		l.deaths = merged
		l.dirty = true
	}
}

// deathsDiffer is whether two oldest-first death lists differ in what the file would hold: a new death, or a pickup more.
func deathsDiffer(a, b []deathloot.Death) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Taken() != b[i].Taken() {
			return true
		}
	}
	return false
}

// deathsViews is the Deaths view of the lifetime and of the period since processStart, valued at now.
func (l *lootAggregator) deathsViews(now, processStart int64) (lifetime, session *DeathsPeriod) {
	l.mu.Lock()
	deaths := append([]deathloot.Death(nil), l.deaths...)
	prices := l.prices
	l.mu.Unlock()
	if len(deaths) == 0 {
		return nil, nil
	}
	lifetime = deathsView(deaths, prices, now, 0)
	session = deathsView(deaths, prices, now, processStart)
	return lifetime, session
}
