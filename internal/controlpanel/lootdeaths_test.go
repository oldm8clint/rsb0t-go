package controlpanel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"rsb0t-go/internal/deathloot"
)

// The Deaths view of the Loot panel (a user request): internal/deathloot proves the matching of pickups to piles; these prove the
// panel values what the fleet reports, keeps it across a restart, and delivers it on /api/loot.

var dBase = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func dBook() *PriceBook {
	return auditBook(
		MarketPrice{Name: "Yew longbow", Coins: 1000},
		MarketPrice{Name: "Nature rune", Coins: 200},
		MarketPrice{Name: "Rune axe", Coins: 8000},
	)
}

// pile is a death at the Yanille ogres dropping 4 Yew longbows (1,000 each), 50 Nature runes (200 each) and 100 Coins, with the
// given pickups recorded against the longbows and the runes.
func pile(id, account string, at time.Time, longbowTakers, runeTakers []deathloot.Recovery) deathloot.Death {
	return deathloot.Death{
		ID: id, Account: account, Script: "AdvancedFighter", At: at.UnixMilli(), HaveTile: true, X: 2508, Z: 3097,
		Place: "Yanille west ogres", Cause: "Ogre",
		Items: []deathloot.Stack{
			{ID: 855, Name: "Yew longbow", Count: 4, Cost: 80, Recovered: longbowTakers},
			{ID: 561, Name: "Nature rune", Count: 50, Cost: 4, Recovered: runeTakers},
			{ID: 995, Name: "Coins", Count: 100, Cost: 1},
		},
		Kept: []deathloot.Stack{{ID: 1, Name: "Rune full helm", Count: 1, Cost: 21000}},
	}
}

func TestADeathIsValuedAndSplitIntoRecoveredOpenAndLost(t *testing.T) {
	prices := dBook()
	at := dBase
	self := []deathloot.Recovery{{By: "alice", Count: 1, At: at.UnixMilli() + 30_000}}
	other := []deathloot.Recovery{{By: "bob", Count: 3, At: at.UnixMilli() + 90_000}}
	d := pile("alice@1", "alice", at, append(append([]deathloot.Recovery(nil), self...), other...), nil)

	// While the pile can still be taken, what is left is open.
	open := deathView(d, prices, at.UnixMilli()+60_000)
	if open.Value != 4*1000+50*200+100 || open.Recovered != 4000 || open.Lost != 0 || open.Open != 50*200+100 || open.Status != DeathOpen {
		t.Fatalf("open death = %+v", open)
	}
	// Once the window has closed, the same remainder is lost for good.
	closed := deathView(d, prices, at.UnixMilli()+deathloot.DespawnMs+deathloot.SlackMs+1)
	if closed.Lost != 50*200+100 || closed.Open != 0 || closed.Recovered != 4000 || closed.Status != DeathPartial {
		t.Fatalf("closed death = %+v", closed)
	}
	// The longbows were taken by the dead account (1) and by another account of the fleet (3).
	bows := closed.Items[1]
	if bows.Name != "Yew longbow" || bows.Status != DeathRecovered || len(bows.By) != 2 || !bows.By[0].Self || bows.By[1].Self || bows.By[1].Account != "bob" {
		t.Fatalf("longbows = %+v", bows)
	}
	// Items are valued with the market price and ordered by value: runes (10,000), longbows (4,000), coins (100).
	if closed.Items[0].Name != "Nature rune" || closed.Items[0].Unit != 200 || closed.Items[0].Status != DeathLost || closed.Items[2].Name != "Coins" {
		t.Fatalf("items = %+v", closed.Items)
	}
	if len(closed.Kept) != 1 || closed.Kept[0].Name != "Rune full helm" {
		t.Fatalf("kept = %+v", closed.Kept)
	}
}

func TestDeathStatuses(t *testing.T) {
	prices := dBook()
	at := dBase.UnixMilli()
	after := at + deathloot.DespawnMs + deathloot.SlackMs + 1
	all := []deathloot.Recovery{{By: "x", Count: 1000}}
	cases := []struct {
		name string
		d    deathloot.Death
		now  int64
		want string
	}{
		{"everything taken", deathloot.Death{ID: "a", Account: "a", At: at, Items: []deathloot.Stack{{ID: 855, Name: "Yew longbow", Count: 2, Recovered: all}}}, after, DeathRecovered},
		{"nothing taken, window closed", deathloot.Death{ID: "b", Account: "b", At: at, Items: []deathloot.Stack{{ID: 855, Name: "Yew longbow", Count: 2}}}, after, DeathLost},
		{"nothing taken, window open", deathloot.Death{ID: "c", Account: "c", At: at, Items: []deathloot.Stack{{ID: 855, Name: "Yew longbow", Count: 2}}}, at + 10_000, DeathOpen},
		{"nothing dropped", deathloot.Death{ID: "d", Account: "d", At: at}, after, DeathNothing},
	}
	for _, c := range cases {
		if got := deathView(c.d, prices, c.now).Status; got != c.want {
			t.Errorf("%s: status %q, want %q", c.name, got, c.want)
		}
	}
}

func TestThePeriodTotalsAndBreakdowns(t *testing.T) {
	prices := dBook()
	now := dBase.Add(time.Hour).UnixMilli() // every window below has closed
	bobTakes := []deathloot.Recovery{{By: "bob", Count: 4, At: dBase.UnixMilli() + 90_000}}
	deaths := []deathloot.Death{
		pile("alice@1", "alice", dBase, bobTakes, nil),
		pile("carol@1", "carol", dBase.Add(10*time.Minute), nil, nil),
	}
	deaths[1].Place, deaths[1].Cause = "Mage Bank", "player"

	p := deathsView(deaths, prices, now, 0)
	each := int64(4*1000 + 50*200 + 100)
	if p.Deaths != 2 || p.Value != 2*each || p.Recovered != 4000 || p.RecoveredOthers != 4000 || p.RecoveredSelf != 0 ||
		p.Lost != 2*each-4000 || p.Open != 0 {
		t.Fatalf("totals = %+v", p)
	}
	if p.Recovered+p.Lost+p.Open != p.Value {
		t.Fatalf("recovered + lost + open = %d, value %d", p.Recovered+p.Lost+p.Open, p.Value)
	}
	if len(p.ByPlace) != 2 || p.ByPlace[0].Name != "Mage Bank" || p.ByPlace[0].Lost != each {
		t.Fatalf("by place = %+v", p.ByPlace)
	}
	if len(p.ByCause) != 2 || len(p.ByAccount) != 2 {
		t.Fatalf("by cause/account = %+v / %+v", p.ByCause, p.ByAccount)
	}
	if len(p.Rows) != 2 || p.Rows[0].Account != "carol" {
		t.Fatalf("rows are newest first: %+v", p.Rows)
	}
	// Lost for good, by item: the two stacks of runes and coins that nobody took, and carol's longbows.
	if len(p.LostItems) == 0 || p.LostItems[0].Name != "Yew longbow" && p.LostItems[0].Name != "Nature rune" {
		t.Fatalf("lost items = %+v", p.LostItems)
	}
	// Since the restart: only the later death.
	s := deathsView(deaths, prices, now, dBase.Add(5*time.Minute).UnixMilli())
	if s.Deaths != 1 || s.Rows[0].Account != "carol" {
		t.Fatalf("since restart = %+v", s)
	}
}

// stubDeaths is a Source that reports deaths, to poll the way the stats ticker does.
type stubDeaths struct{ deaths []deathloot.Death }

func (s *stubDeaths) Accounts(dst []AccountStatus) []AccountStatus { return dst }
func (s *stubDeaths) Logins() LoginStatus                          { return LoginStatus{} }
func (s *stubDeaths) MemStats() MemStats                           { return MemStats{} }
func (s *stubDeaths) Subscribe(chan<- Event) (cancel func())       { return func() {} }
func (s *stubDeaths) DeathEvents() []deathloot.Death               { return s.deaths }

func TestDeathsSurviveARestartAndKeepTheirPickups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	src := &stubDeaths{deaths: []deathloot.Death{pile("alice@1", "alice", dBase, nil, nil)}}

	a1 := newStatsAggregator(path, src, fixedNow(dBase.Add(time.Minute)))
	a1.pollLoot(dBase.Add(time.Minute))
	// A pickup arrives on the next poll: the stored copy follows the live ledger.
	src.deaths = []deathloot.Death{pile("alice@1", "alice", dBase, []deathloot.Recovery{{By: "bob", Count: 2, At: dBase.UnixMilli() + 80_000}}, nil)}
	a1.pollLoot(dBase.Add(2 * time.Minute))
	a1.close()

	// The panel restarts (the fleet's ledger is empty or gone): the death and its pickups are read back.
	a2 := newStatsAggregator(path, &stubDeaths{}, fixedNow(dBase.Add(time.Hour)))
	a2.loot.prices = dBook()
	a2.pollLoot(dBase.Add(time.Hour))
	body := a2.lootSnapshot()
	life := body.Lifetime.Deaths
	if life == nil || life.Deaths != 1 || life.Recovered != 2000 || len(life.Rows) != 1 || !hasTaker(life.Rows[0], "Yew longbow", "bob") {
		t.Fatalf("lifetime deaths after a restart = %+v", life)
	}
	if sd := body.SinceRestart.Deaths; sd != nil && sd.Deaths != 0 {
		t.Fatalf("since restart must not hold the previous process's death: %+v", body.SinceRestart.Deaths)
	}
	// A fleet that restarted reports the same death without its pickups: the stored pickups win.
	a2.pollLoot(dBase.Add(time.Hour))
	src3 := &stubDeaths{deaths: []deathloot.Death{pile("alice@1", "alice", dBase, nil, nil)}}
	a2.src = src3
	a2.pollLoot(dBase.Add(time.Hour))
	if got := a2.lootSnapshot().Lifetime.Deaths.Recovered; got != 2000 {
		t.Fatalf("a rebuilt ledger erased the pickups: recovered %d", got)
	}
}

func TestTheLootAPIReturnsTheDeathsCard(t *testing.T) {
	a := newStatsAggregator("", &stubDeaths{}, fixedNow(dBase.Add(time.Hour)))
	a.loot.prices = dBook()
	a.loot.noteDeaths([]deathloot.Death{
		pile("alice@1", "alice", dBase.Add(30*time.Minute), []deathloot.Recovery{{By: "alice", Count: 4, At: dBase.UnixMilli() + 30*60_000 + 20_000}}, nil),
	})
	s := &Server{stats: a}
	w := httptest.NewRecorder()
	s.handleLoot(w, httptest.NewRequest(http.MethodGet, "/api/loot", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Lifetime struct {
			Deaths *struct {
				Deaths          int64 `json:"deaths"`
				Value           int64 `json:"value"`
				Recovered       int64 `json:"recovered"`
				RecoveredSelf   int64 `json:"recoveredSelf"`
				RecoveredOthers int64 `json:"recoveredOthers"`
				Lost            int64 `json:"lost"`
				ByPlace         []struct {
					Name   string `json:"name"`
					Deaths int64  `json:"deaths"`
				} `json:"byPlace"`
				Rows []struct {
					Account string `json:"account"`
					Place   string `json:"place"`
					Cause   string `json:"cause"`
					Status  string `json:"status"`
					Items   []struct {
						Name   string `json:"name"`
						Status string `json:"status"`
					} `json:"items"`
				} `json:"rows"`
			} `json:"deaths"`
		} `json:"lifetime"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	d := body.Lifetime.Deaths
	if d == nil || d.Deaths != 1 || d.Recovered != 4000 || d.RecoveredSelf != 4000 || d.RecoveredOthers != 0 || d.Lost != 50*200+100 {
		t.Fatalf("deaths block = %+v", d)
	}
	if len(d.ByPlace) != 1 || d.ByPlace[0].Name != "Yanille west ogres" || len(d.Rows) != 1 {
		t.Fatalf("breakdown/rows = %+v", d)
	}
	r := d.Rows[0]
	if r.Account != "alice" || r.Cause != "Ogre" || r.Status != DeathPartial || len(r.Items) != 3 {
		t.Fatalf("row = %+v", r)
	}
}

// hasTaker is whether the row's item of that name was taken by that account.
func hasTaker(r DeathView, item, by string) bool {
	for _, it := range r.Items {
		if it.Name != item {
			continue
		}
		for _, b := range it.By {
			if b.Account == by {
				return true
			}
		}
	}
	return false
}
