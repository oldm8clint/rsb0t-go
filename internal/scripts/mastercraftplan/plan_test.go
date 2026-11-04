package mastercraftplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/gemplan"
)

func stockMap(m map[string]int) Stock {
	return func(item string) (int, bool) {
		return m[item], true
	}
}

func keys(gems []gemplan.Gem) []string {
	var out []string
	for _, g := range gems {
		out = append(out, g.Key)
	}
	return out
}

// The gem levels are content 289's (gem.dbrow), the owner's list: opal 1, jade 13, red topaz 16,
// sapphire 20, emerald 27, ruby 34, diamond 43, dragonstone 55.
func TestGemLevelsAreContents(t *testing.T) {
	want := map[string]int{"opal": 1, "jade": 13, "red_topaz": 16, "sapphire": 20, "emerald": 27, "ruby": 34, "diamond": 43, "dragonstone": 55}
	for _, g := range gemplan.Gems {
		if want[g.Key] != g.Level {
			t.Errorf("%s: level %d, content says %d", g.Key, g.Level, want[g.Key])
		}
		delete(want, g.Key)
	}
	if len(want) != 0 {
		t.Errorf("gems missing from the table: %v", want)
	}
}

func TestGemsBestFirstByCraftingLevel(t *testing.T) {
	cases := []struct {
		level int
		want  []string
	}{
		{0, nil},
		{1, []string{"opal"}},
		{12, []string{"opal"}},
		{13, []string{"jade", "opal"}},
		{19, []string{"red_topaz", "jade", "opal"}},
		{20, []string{"sapphire", "red_topaz", "jade", "opal"}},
		{26, []string{"sapphire", "red_topaz", "jade", "opal"}},
		{27, []string{"emerald", "sapphire", "red_topaz", "jade", "opal"}},
		{34, []string{"ruby", "emerald", "sapphire", "red_topaz", "jade", "opal"}},
		{43, []string{"diamond", "ruby", "emerald", "sapphire", "red_topaz", "jade", "opal"}},
		{54, []string{"diamond", "ruby", "emerald", "sapphire", "red_topaz", "jade", "opal"}},
		{55, []string{"dragonstone", "diamond", "ruby", "emerald", "sapphire", "red_topaz", "jade", "opal"}},
		{99, []string{"dragonstone", "diamond", "ruby", "emerald", "sapphire", "red_topaz", "jade", "opal"}},
	}
	for _, c := range cases {
		if got := keys(GemsBestFirst(c.level, nil, nil, true)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("level %d: %v, want %v", c.level, got, c.want)
		}
	}
}

func TestGemsBestFirstFiltersAndMembers(t *testing.T) {
	if got := keys(GemsBestFirst(99, []string{"ruby", "Sapphire"}, nil, true)); !reflect.DeepEqual(got, []string{"ruby", "sapphire"}) {
		t.Errorf("kinds filter: %v", got)
	}
	if got := keys(GemsBestFirst(99, nil, map[string]bool{"diamond": true}, true)); got[0] != "dragonstone" || got[1] != "ruby" {
		t.Errorf("denied diamond: %v", got)
	}
	// A free world cannot cut the members-only gems at all.
	if got := keys(GemsBestFirst(99, nil, nil, false)); !reflect.DeepEqual(got, []string{"diamond", "ruby", "emerald", "sapphire"}) {
		t.Errorf("free world: %v", got)
	}
}

func TestGemStockCountsOnlyCuttableKinds(t *testing.T) {
	stock := stockMap(map[string]int{"Uncut opal": 300, "Uncut sapphire": 250, "Uncut diamond": 900, "Chisel": 2})
	gems := GemsBestFirst(20, nil, nil, true) // sapphire, red topaz, jade, opal: not the diamonds
	total, per := GemStock(gems, stock)
	if total != 550 || per["opal"] != 300 || per["sapphire"] != 250 || per["diamond"] != 0 {
		t.Errorf("total %d per %v", total, per)
	}
}

func TestPlanGemAskAsksForEveryCuttableKindBestFirst(t *testing.T) {
	set := orderSettings()
	stock := stockMap(map[string]int{"Uncut sapphire": 250, "Uncut ruby": 1500, "Uncut diamond": 40, "Uncut opal": 3, "Chisel": 1})
	a := PlanGemAsk(43, set, nil, stock, true)
	want := []Line{{"Uncut diamond", 40}, {"Uncut ruby", 1000}, {"Uncut sapphire", 250}, {"Uncut opal", 3}}
	if !reflect.DeepEqual(a.Lines, want) {
		t.Errorf("lines %v, want %v (a kind is asked up to KitSize, the mule clamps an order to 1000)", a.Lines, want)
	}
	if a.Gems != 1293 || a.Why != "" {
		t.Errorf("gems %d why %q", a.Gems, a.Why)
	}
	// Diamonds are beyond Crafting 30: left in the mule.
	a = PlanGemAsk(30, set, nil, stock, true)
	for _, l := range a.Lines {
		if l.Item == "Uncut diamond" {
			t.Errorf("asked for diamonds at Crafting 30: %v", a.Lines)
		}
	}
	set.KitSize = 100
	a = PlanGemAsk(99, set, nil, stock, true)
	if a.Lines[1] != (Line{"Uncut ruby", 100}) {
		t.Errorf("KitSize 100 not applied: %v", a.Lines)
	}
}

func TestPlanGemAskChisel(t *testing.T) {
	set := orderSettings()
	stock := stockMap(map[string]int{"Uncut sapphire": 600, "Chisel": 3})
	a := PlanGemAsk(30, set, nil, stock, false)
	if n := len(a.Lines); n != 2 || a.Lines[n-1] != (Line{"Chisel", 1}) {
		t.Errorf("no chisel held: %v", a.Lines)
	}
	// No chisel anywhere: nothing can be cut, so nothing is asked.
	stock = stockMap(map[string]int{"Uncut sapphire": 600})
	a = PlanGemAsk(30, set, nil, stock, false)
	if len(a.Lines) != 0 || !strings.Contains(a.Why, "no chisel") {
		t.Errorf("no chisel anywhere: %v %q", a.Lines, a.Why)
	}
	if a := PlanGemAsk(30, set, nil, stock, true); len(a.Lines) != 1 {
		t.Errorf("a chisel held is not asked for: %v", a.Lines)
	}
}

func TestPlanGemAskNothingToAsk(t *testing.T) {
	set := orderSettings()
	if a := PlanGemAsk(30, set, nil, nil, true); len(a.Lines) != 0 || a.Why == "" {
		t.Errorf("unpublished bank: %+v", a)
	}
	// Only gems above the level: nothing worth the walk.
	stock := stockMap(map[string]int{"Uncut dragonstone": 900})
	if a := PlanGemAsk(30, set, nil, stock, true); len(a.Lines) != 0 || !strings.Contains(a.Why, "none of the gems Crafting 30 can cut") {
		t.Errorf("dragonstones at 30: %+v", a)
	}
	if a := PlanGemAsk(0, set, nil, stock, true); len(a.Lines) != 0 || a.Why == "" {
		t.Errorf("level 0: %+v", a)
	}
}

func TestPickGemTakesTheBestHeld(t *testing.T) {
	gems := GemsBestFirst(60, nil, nil, true)
	g, ok := PickGem(gems, map[string]int{"opal": 5, "ruby": 9})
	if !ok || g.Key != "ruby" {
		t.Errorf("picked %v %v", g.Key, ok)
	}
	if _, ok := PickGem(gems, map[string]int{"opal": 0}); ok {
		t.Error("picked a gem from an empty bank")
	}
	if g, ok := PickGem(GemsBestFirst(15, nil, nil, true), map[string]int{"ruby": 9, "jade": 1}); !ok || g.Key != "jade" {
		t.Errorf("a gem above the level was picked: %v %v", g.Key, ok)
	}
}

// Five gems a game tick, the engine's USER_EVENT limit.
func TestBurstsAreFiveATick(t *testing.T) {
	if gemplan.BurstSize != 5 {
		t.Fatalf("the burst is %d, the engine reads five user events a tick", gemplan.BurstSize)
	}
	if got := Bursts(GemLoad()); !reflect.DeepEqual(got, []int{5, 5, 5, 5, 5, 2}) {
		t.Errorf("a pack of %d: %v", GemLoad(), got)
	}
	for _, c := range []struct {
		n    int
		want []int
	}{{0, nil}, {-3, nil}, {1, []int{1}}, {5, []int{5}}, {6, []int{5, 1}}, {10, []int{5, 5}}} {
		if got := Bursts(c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Bursts(%d) = %v, want %v", c.n, got, c.want)
		}
	}
	if GemLoad() != 27 {
		t.Errorf("a bank visit draws %d, want 27 (the pack less the chisel)", GemLoad())
	}
}

func TestLedgerStrungBows(t *testing.T) {
	var l Ledger
	l.NoteStrungDelivered("Maple longbow", 300)
	l.NoteStrungDelivered("maple longbow ", 50)
	l.NoteStrungDelivered("Yew shortbow", 0)
	if l.Strung("Maple longbow") != 350 || l.Strung("Yew shortbow") != 0 {
		t.Errorf("ledger %v", l.StrungAtMule)
	}
	if UnstrungEstimate(500, 350) != 150 || UnstrungEstimate(100, 350) != 0 || UnstrungEstimate(100, -5) != 100 {
		t.Error("UnstrungEstimate")
	}
	// Somebody took strung bows away: the ledger follows the mule's count down.
	l.Reconcile(stockMap(map[string]int{"Maple longbow": 120}))
	if l.Strung("Maple longbow") != 120 {
		t.Errorf("reconciled to %d, want 120", l.Strung("Maple longbow"))
	}
	l.Reconcile(stockMap(map[string]int{"Maple longbow": 0}))
	if _, ok := l.StrungAtMule["maple longbow"]; ok {
		t.Error("an empty count stays in the ledger")
	}
	// An unpublished bank changes nothing.
	l.NoteStrungDelivered("Oak longbow", 7)
	l.Reconcile(nil)
	if l.Strung("Oak longbow") != 7 {
		t.Error("nil stock changed the ledger")
	}
	var nilLed *Ledger
	if nilLed.Strung("Oak longbow") != 0 {
		t.Error("nil ledger")
	}
}

func TestAdjustedStockTakesStrungBowsOffBowNamesOnly(t *testing.T) {
	var l Ledger
	l.NoteStrungDelivered("Maple longbow", 400)
	adj := AdjustedStock(stockMap(map[string]int{"Maple longbow": 450, "Bow string": 900, "Uncut ruby": 7}), &l, nil)
	if n, _ := adj("Maple longbow"); n != 50 {
		t.Errorf("maple longbow %d, want 50", n)
	}
	if n, _ := adj("Bow string"); n != 900 {
		t.Errorf("bow string %d", n)
	}
	if n, _ := adj("Uncut ruby"); n != 7 {
		t.Errorf("uncut ruby %d", n)
	}
	if AdjustedStock(nil, &l, nil) != nil {
		t.Error("a nil stock must stay nil")
	}
}

func TestBowsReady(t *testing.T) {
	set := orderSettings() // threshold 100
	var l Ledger
	stock := stockMap(map[string]int{"Maple longbow": 300, "Willow longbow": 50, "Yew shortbow": 80, "Bow string": 1000})
	// Fletching 55: maple (55), willow (40) but not yew (65).
	r := BowsReady(set, 55, stock, &l)
	if !r.Ready || r.Pairs != 350 || r.Best.Name != "Maple longbow" {
		t.Errorf("%+v", r)
	}
	// Fletching 65 takes the yew shortbow, the best one with stock.
	if r := BowsReady(set, 65, stock, &l); r.Best.Name != "Yew shortbow" || r.Pairs != 430 {
		t.Errorf("%+v", r)
	}
	// Strings are the limit.
	short := stockMap(map[string]int{"Maple longbow": 300, "Bow string": 40})
	if r := BowsReady(set, 55, short, &l); r.Ready || r.Pairs != 40 || !strings.Contains(r.Why, "under the threshold") {
		t.Errorf("%+v", r)
	}
	if r := BowsReady(set, 55, stockMap(map[string]int{"Maple longbow": 300}), &l); r.Ready || !strings.Contains(r.Why, "no Bow string") {
		t.Errorf("%+v", r)
	}
	if r := BowsReady(set, 5, stock, &l); r.Ready || !strings.Contains(r.Why, "no unstrung bow") {
		t.Errorf("Fletching 5: %+v", r)
	}
	if r := BowsReady(set, 55, nil, &l); r.Ready || r.Why == "" {
		t.Errorf("unpublished: %+v", r)
	}
	// The strung bows this account handed back are not bows to string.
	l.NoteStrungDelivered("Maple longbow", 280)
	if r := BowsReady(set, 55, stock, &l); r.Ready || r.Pairs != 70 {
		t.Errorf("with 280 strung at the mule: %+v", r)
	}
	// A bow not in the list is not strung.
	set.BowNames = []string{"Willow longbow"}
	if r := BowsReady(set, 99, stock, &Ledger{}); r.Pairs != 50 || r.Best.Name != "Willow longbow" {
		t.Errorf("%+v", r)
	}
}

func readySnap(now time.Time) Snapshot {
	return Snapshot{
		Now: now, MuleHeard: true, MuleSameWorld: true,
		Stock: stockMap(map[string]int{
			"Uncut sapphire": 600, "Chisel": 1,
			"Maple longbow": 500, "Bow string": 500,
		}),
		CraftingLevel: 30, FletchingLevel: 60, FletchingKnown: true,
		Ledger: &Ledger{},
	}
}

func TestDecideGemsBeforeBows(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	d := Decide(set, readySnap(now))
	if d.Mode != ModeGems || d.Gems != 600 || !strings.Contains(d.Why, "600 uncut gems") {
		t.Errorf("%+v", d)
	}
	// Under the threshold: the bows.
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 499, "Maple longbow": 500, "Bow string": 500})
	if d := Decide(set, s); d.Mode != ModeBows || d.Pairs != 500 {
		t.Errorf("%+v", d)
	}
	// Neither.
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 499, "Maple longbow": 99, "Bow string": 500})
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "499") || !strings.Contains(d.Why, "99") {
		t.Errorf("%+v", d)
	}
}

func TestDecideThresholdIsAtLeast(t *testing.T) {
	now := time.Now()
	set := orderSettings()
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 500, "Chisel": 1})
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Errorf("exactly the threshold: %+v", d)
	}
	set.GemThreshold = 501
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("one under: %+v", d)
	}
}

func TestDecideNeedsTheMule(t *testing.T) {
	now := time.Now()
	set := orderSettings()
	s := readySnap(now)
	s.MuleHeard = false
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "not heard") {
		t.Errorf("%+v", d)
	}
	s = readySnap(now)
	s.MuleSameWorld = false
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "another world") {
		t.Errorf("%+v", d)
	}
	s = readySnap(now)
	s.Stock = nil
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("no published bank must never start a round: %+v", d)
	}
	// A pending hand-over also needs the mule there.
	s = readySnap(now)
	s.Ledger.PendingDeliver = true
	s.MuleHeard = false
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("%+v", d)
	}
}

func TestDecideDeliversFirstAndHonoursPauses(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	s := readySnap(now)
	s.Ledger.PendingDeliver = true
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Errorf("%+v", d)
	}
	s = readySnap(now)
	s.Ledger.GemPauseUntil = now.Add(time.Hour)
	if d := Decide(set, s); d.Mode != ModeBows {
		t.Errorf("gems paused: %+v", d)
	}
	s.Ledger.BowPauseUntil = now.Add(time.Hour)
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "paused") {
		t.Errorf("both paused: %+v", d)
	}
	s.Now = now.Add(2 * time.Hour)
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Errorf("pause over: %+v", d)
	}
}

func TestDecideSwitchesOffByKind(t *testing.T) {
	now := time.Now()
	set := orderSettings()
	set.GemCutting = false
	if d := Decide(set, readySnap(now)); d.Mode != ModeBows {
		t.Errorf("gems off: %+v", d)
	}
	set = orderSettings()
	set.BowStringing = false
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Maple longbow": 500, "Bow string": 500})
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("bows off: %+v", d)
	}
	s = readySnap(now)
	s.FletchingKnown = false
	s.Stock = stockMap(map[string]int{"Maple longbow": 500, "Bow string": 500})
	if d := Decide(orderSettings(), s); d.Mode != ModeSwarm {
		t.Errorf("Fletching unknown: %+v", d)
	}
}

func TestDecideCountsOnlyGemsTheLevelCanCut(t *testing.T) {
	now := time.Now()
	s := readySnap(now)
	s.CraftingLevel = 10 // opal only
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 5000, "Uncut opal": 100, "Chisel": 1})
	if d := Decide(orderSettings(), s); d.Mode != ModeSwarm {
		t.Errorf("5000 sapphires are no use at Crafting 10: %+v", d)
	}
}

func TestSafePoint(t *testing.T) {
	ok := SafeInput{TileKnown: true, HP: 40, MaxHP: 40}
	if safe, why := SafePoint(ok, 50); !safe || why != "" {
		t.Fatalf("the plain case: %v %q", safe, why)
	}
	cases := []struct {
		name string
		mut  func(*SafeInput)
		want string
	}{
		{"no tile", func(s *SafeInput) { s.TileKnown = false }, "position"},
		{"dead", func(s *SafeInput) { s.Dead = true }, "dead"},
		{"fight", func(s *SafeInput) { s.InCombat = true }, "fight"},
		{"hurt", func(s *SafeInput) { s.Hurt = true }, "hurt"},
		{"trade or bank", func(s *SafeInput) { s.ModalOpen = true }, "interface"},
		{"random event", func(s *SafeInput) { s.RandomEvent = true }, "random event"},
		{"errand", func(s *SafeInput) { s.Errand = true }, "errand"},
		{"rune trip", func(s *SafeInput) { s.Trip = true }, "trip"},
		{"quest", func(s *SafeInput) { s.Quest = true }, "quest"},
		{"hazard", func(s *SafeInput) { s.Hazard = true }, "Wilderness"},
		{"low hp", func(s *SafeInput) { s.HP = 19 }, "hitpoints"},
	}
	for _, c := range cases {
		in := ok
		c.mut(&in)
		safe, why := SafePoint(in, 50)
		if safe || !strings.Contains(why, c.want) {
			t.Errorf("%s: %v %q, want unsafe naming %q", c.name, safe, why, c.want)
		}
	}
	// Exactly half is allowed; the floor is a percentage.
	in := ok
	in.HP = 20
	if safe, _ := SafePoint(in, 50); !safe {
		t.Error("20/40 at a 50% floor")
	}
	in.HP, in.MaxHP = 0, 0 // not read: the other rules decide
	if safe, _ := SafePoint(in, 50); !safe {
		t.Error("unknown hitpoints")
	}
	if safe, _ := SafePoint(SafeInput{TileKnown: true, HP: 1, MaxHP: 99}, 0); !safe {
		t.Error("a floor of 0 allows any hitpoints")
	}
}

func TestTimers(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if !CheckDue(time.Time{}, now, 20*time.Minute) {
		t.Error("never checked is due")
	}
	if CheckDue(now.Add(-19*time.Minute), now, 20*time.Minute) || !CheckDue(now.Add(-20*time.Minute), now, 20*time.Minute) {
		t.Error("CheckDue boundary")
	}
	if !PauseAfter(5, now, time.Hour).IsZero() || !PauseAfter(0, now, 0).IsZero() {
		t.Error("a round that produced something, or no pause, sets no pause")
	}
	if got := PauseAfter(0, now, time.Hour); !got.Equal(now.Add(time.Hour)) {
		t.Errorf("pause until %v", got)
	}
	if RoundExpired(time.Time{}, now, time.Hour) || RoundExpired(now.Add(-30*time.Minute), now, time.Hour) || !RoundExpired(now.Add(-time.Hour), now, time.Hour) {
		t.Error("RoundExpired")
	}
}

// orderSettings is DefaultSettings with the thresholds the order, pause and backoff tests were written
// against (500 gems, 100 bow pairs, 100,000 pearls, one Bolt, 1,000 arrows): they test which round
// comes first and when one is held off, not the owner's 20,000s (TestDefaultThresholdsAreTheOwners).
func orderSettings() Settings {
	s := DefaultSettings()
	s.GemThreshold, s.BowThreshold, s.PearlThreshold, s.BoltThreshold, s.ArrowThreshold = 500, 100, 100000, 1, 1000
	return s
}

// The owner's thresholds (6 Oct 2026): 20,000 of everything, each counted over the mule's bank and
// this account's together, and ScarabSwarm between rounds.
func TestDefaultThresholdsAreTheOwners(t *testing.T) {
	set := DefaultSettings()
	if set.GemThreshold != 20000 || set.BowThreshold != 20000 || set.PearlThreshold != 20000 || set.BoltThreshold != 20000 ||
		set.ArrowThreshold != 20000 || set.Idle != IdleSwarm || ChestStartKeys != 400 {
		t.Fatalf("defaults: %+v, chest %d", set, ChestStartKeys)
	}
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	snap := func(stock map[string]int, ownUncut int) Snapshot {
		return Snapshot{Now: now, MuleHeard: true, MuleSameWorld: true, Stock: stockMap(stock), OwnUncut: ownUncut,
			CraftingLevel: 92, FletchingLevel: 99, FletchingKnown: true, Ledger: &Ledger{}}
	}
	// The mule's 94 gems of 6 Oct are no round, and neither are 19,999 over the two banks.
	if d := Decide(set, snap(map[string]int{"Uncut sapphire": 94, "Chisel": 1}, 0)); d.Mode == ModeGems {
		t.Errorf("94 gems: %+v", d)
	}
	if d := Decide(set, snap(map[string]int{"Uncut sapphire": 9999, "Chisel": 1}, 10000)); d.Mode == ModeGems {
		t.Errorf("19,999 gems: %+v", d)
	}
	d := Decide(set, snap(map[string]int{"Uncut sapphire": 10000, "Chisel": 1}, 10000))
	if d.Mode != ModeGems || d.Resume || d.Gems != 20000 || !strings.Contains(d.Why, "together") {
		t.Errorf("20,000 gems over the two banks: %+v", d)
	}
	if d := Decide(set, snap(map[string]int{"Chisel": 1}, 20000)); d.Mode != ModeGems || !d.Resume {
		t.Errorf("20,000 gems in this account's own bank: %+v", d)
	}
}

func TestDecodeSettings(t *testing.T) {
	s, err := DecodeSettings(nil)
	if err != nil || !reflect.DeepEqual(s, DefaultSettings()) {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	if s.GemThreshold != 20000 || s.CheckEvery != 20*time.Minute || !s.GemCutting || !s.BowStringing {
		t.Errorf("defaults are the owner's: %+v", s)
	}
	// ScarabSwarm's keys are ignored; the panel sends numbers as strings.
	s, err = DecodeSettings(map[string]interface{}{
		"mule": "mule", "maxStage": float64(5), "stage5Quests": []interface{}{"waterfall"},
		KeyGemThreshold: "800", KeyCheckEvery: float64(10), KeyBowThreshold: 50,
		KeyGems: "Ruby, Diamond", KeyBows: []interface{}{"Maple longbow"}, KeyGemCutting: "false",
		KeySafeHp: float64(70), KeyMaxRound: "60", KeyKitSize: float64(300), KeyMembersWorld: false,
		KeyFruitlessPause: float64(30),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.GemThreshold != 800 || s.CheckEvery != 10*time.Minute || s.BowThreshold != 50 || s.GemCutting ||
		!reflect.DeepEqual(s.GemKinds, []string{"Ruby", "Diamond"}) || !reflect.DeepEqual(s.BowNames, []string{"Maple longbow"}) ||
		s.SafeHpPercent != 70 || s.MaxRound != time.Hour || s.KitSize != 300 || s.MembersWorld || s.FruitlessPause != 30*time.Minute {
		t.Errorf("%+v", s)
	}
}

func TestDecodeSettingsRefusals(t *testing.T) {
	for name, raw := range map[string]map[string]interface{}{
		"typo in a craft key":      {"craftGemThreshhold": 5},
		"not a number":             {KeyGemThreshold: "lots"},
		"fractional minutes":       {KeyCheckEvery: 2.5},
		"not a bool":               {KeyBowStringing: "maybe"},
		"zero threshold":           {KeyGemThreshold: 0},
		"check under a minute":     {KeyCheckEvery: 0},
		"kit above the mule cap":   {KeyKitSize: 100001},
		"hp over 100":              {KeySafeHp: 120},
		"unknown gem":              {KeyGems: []interface{}{"Onyx"}},
		"unknown bow":              {KeyBows: []interface{}{"Magic crossbow"}},
		"every round off":          {KeyGemCutting: false, KeyBowStringing: false, KeyPearls: false, KeySuperheat: false, KeyArrows: false, KeyHerbs: false, KeySmith: false},
		"unknown supply shop":      {KeyShops: []interface{}{"Gulluck"}},
		"unknown bar":              {KeySuperheatBars: []interface{}{"Blurite bar"}},
		"zero shop price":          {KeyShopMaxPrice: 0},
		"shelf over 100":           {KeyShopMinShelf: 101},
		"zero herb threshold":      {KeyHerbThreshold: 0},
		"zero pearl threshold":     {KeyPearlThreshold: 0},
		"pearls not a bool":        {KeyPearls: "often"},
		"round shorter than five":  {KeyMaxRound: 4},
		"backoff under a minute":   {KeyBackoff: 0},
		"backoff cap below start":  {KeyBackoff: 60, KeyBackoffMax: 30},
		"negative hand-over limit": {KeyMaxHandover: -1},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	if _, err := DecodeSettings(map[string]interface{}{"craftNope": 1}); err == nil || !strings.Contains(err.Error(), "craftNope") {
		t.Errorf("an unknown craft key must be named: %v", err)
	}
}

func TestKnownKeysAreAllDecoded(t *testing.T) {
	// Every documented key must be accepted by the decoder (a key in knownKeys with no case in
	// the switch would be silently ignored).
	sample := map[string]interface{}{
		KeyCrystalChest:    false,  // chest.go
		KeyJewelleryBatch:  77,     // jewellery.go
		KeyRingBatch:       78,     // jewellerykind.go
		KeyChestGamesRoute: "post", // chestalch.go
		KeyChestAlch:       false,  // chestalch.go
		KeyWalkEnchant:     false,  // jewellerywalk.go

		KeyGemCutting: true, KeyBowStringing: true, KeyGemThreshold: 7, KeyBowThreshold: 8, KeyCheckEvery: 9,
		KeyGems: []interface{}{"Ruby"}, KeyBows: []interface{}{"Yew longbow"}, KeyKitSize: 11, KeyMaxRound: 12,
		KeySafeHp: 13, KeyMembersWorld: false, KeyFruitlessPause: 14,
		KeyBackoff: 15, KeyBackoffMax: 150, KeyMaxHandover: 5, KeyBulkSapphires: 60000,
		KeyPearls: false, KeyPearlThreshold: 2500, KeyBoltThreshold: 2600, KeyFastGems: false, KeyFastPearls: false, KeyFastBows: false,
		KeyBuyBolts: false, KeyBuyArrowTips: false, KeyShops: []interface{}{"Lowe"}, KeyShopMaxPrice: 9, KeyShopMinShelf: 70,
		KeyShopMinWaiting: 3, KeyShopBuyBelow: 4, KeySuperheat: false, KeySuperheatThreshold: 5,
		KeySuperheatBars: []interface{}{"Steel bar"}, KeySuperheatGold: true, KeyArrows: false, KeyArrowThreshold: 6,
		KeyArrowKit: 7, KeyHerbs: false, KeyHerbThreshold: 8, KeyBuyFireStaff: false, KeyFireStaffMaxPrice: 9000,
		KeyStoreOverflow: false, KeyStoreAtRows: 200, KeyStoreToRows: 100, KeyStoreMaxCount: 12, KeyStoreQuietDays: 3, // store.go
		KeyIdle:  "post",
		KeySmith: false, KeySmithMinSteelBars: 9001, KeySmithArrowtips: false, KeySmithIronBodies: false, KeySmithKit: 4321, // smith.go
		KeySuperheatLowBars: true,
		// runecraft.go
		KeyRune: true, KeyRuneLevelTo: 30, KeyRuneEssence: "mine", KeyRuneBatch: 540, KeyRuneGateway: "aubury", KeyRuneTeleport: false, KeyRuneRound: 90, KeyRuneBuyBudget: 123456,
		// nature.go
		KeyRuneManual: false, KeyRuneStation: "gate", KeyRuneRunners: []interface{}{"Acct 1", "Acct 2"}, KeyRuneKeep: 2500, KeyRuneCourier: false, KeyRuneCourierMax: 1200, KeyRuneMaxHours: 6, KeyRuneGateAlch: false, KeyRuneAlchBows: 1200, KeyRuneAlchHold: true, KeyRuneDirectOps: false, KeyRuneOneWrite: false, KeyRuneLegCasts: "high2",
		// campaign.go (craftRuneManual is off above: the two are alternatives)
		KeyRuneCampaign: true, KeyRuneStartBelow: 1500, KeyRuneStopAbove: 70000, KeyRuneTarget: 90000, KeyRuneMinEssence: 4000, KeyRuneMinRunners: 4,
		KeyRuneHandOverAt: 15000, KeyRuneHandOverMin: 6000, KeyRuneCourierWait: 90, KeyRunePause: 30, KeyRuneBuyAll: false, KeyRuneMuleCoinFloor: 20000, KeyRuneBuyTrip: 3000000,
		// goal.go
		KeySkillGoal: 98, KeyPastGoal: "bows",
		// trawler.go
		KeyTrawler: true, KeyTrawlerQuorum: 35, KeyTrawlerFoodLow: 4000, KeyTrawlerFoodHigh: 15000,
		// cannonballs.go
		KeyCannonballTarget: 40000, KeyCannonballBatch: 250, KeyCannonballMinBars: 50,
	}
	if len(sample) != len(KnownKeys()) {
		t.Fatalf("sample has %d keys, KnownKeys %d: %v", len(sample), len(KnownKeys()), KnownKeys())
	}
	s, err := DecodeSettings(sample)
	if err != nil {
		t.Fatal(err)
	}
	d := DefaultSettings()
	if s.GemThreshold == d.GemThreshold || s.BowThreshold == d.BowThreshold || s.CheckEvery == d.CheckEvery ||
		s.KitSize == d.KitSize || s.MaxRound == d.MaxRound || s.SafeHpPercent == d.SafeHpPercent ||
		s.MembersWorld == d.MembersWorld || s.FruitlessPause == d.FruitlessPause ||
		s.Backoff == d.Backoff || s.BackoffMax == d.BackoffMax || s.MaxHandoverFailures == d.MaxHandoverFailures ||
		s.Pearls == d.Pearls || s.PearlThreshold == d.PearlThreshold || s.BoltThreshold == d.BoltThreshold || s.BulkAtSapphires == d.BulkAtSapphires ||
		s.FastGems == d.FastGems || s.FastPearls == d.FastPearls || s.FastBows == d.FastBows ||
		len(s.GemKinds) == 0 || reflect.DeepEqual(s.BowNames, d.BowNames) ||
		s.BuyBolts == d.BuyBolts || s.BuyArrowTips == d.BuyArrowTips || reflect.DeepEqual(s.ShopNames, d.ShopNames) ||
		s.ShopMaxPrice == d.ShopMaxPrice || s.ShopMinShelfPercent == d.ShopMinShelfPercent ||
		s.ShopMinWaiting == d.ShopMinWaiting || s.ShopBuyBelow == d.ShopBuyBelow || s.Superheat == d.Superheat ||
		s.SuperheatThreshold == d.SuperheatThreshold || len(s.SuperheatBars) == 0 || s.SuperheatGold == d.SuperheatGold ||
		s.Arrows == d.Arrows || s.ArrowThreshold == d.ArrowThreshold || s.ArrowKit == d.ArrowKit ||
		s.Herbs == d.Herbs || s.HerbThreshold == d.HerbThreshold || s.BuyFireStaff == d.BuyFireStaff ||
		s.FireStaffMaxPrice == d.FireStaffMaxPrice ||
		s.StoreOverflow == d.StoreOverflow || s.StoreAtRows == d.StoreAtRows || s.StoreToRows == d.StoreToRows ||
		s.StoreMaxCount == d.StoreMaxCount || s.StoreQuietDays == d.StoreQuietDays || s.JewelleryBatch == d.JewelleryBatch ||
		s.RingBatch == d.RingBatch || s.ChestGamesRoute == d.ChestGamesRoute || s.ChestAlch == d.ChestAlch || s.WalkEnchant == d.WalkEnchant ||
		s.Smith == d.Smith || s.SmithMinSteelBars == d.SmithMinSteelBars || s.SmithArrowtips == d.SmithArrowtips ||
		s.SmithIronPlatebodies == d.SmithIronPlatebodies || s.SmithKit == d.SmithKit || s.SuperheatLowBars == d.SuperheatLowBars ||
		s.Rune.On == d.Rune.On || s.Rune.LevelTo == d.Rune.LevelTo || s.Rune.Essence == d.Rune.Essence || s.Rune.Batch == d.Rune.Batch ||
		s.Rune.Gateway == d.Rune.Gateway || s.Rune.Teleport == d.Rune.Teleport || s.Rune.Round == d.Rune.Round || s.Rune.BuyBudget == d.Rune.BuyBudget ||
		s.Rune.DirectOps == d.Rune.DirectOps || s.Rune.OneWrite == d.Rune.OneWrite || s.Rune.LegCasts == d.Rune.LegCasts ||
		s.SkillGoal == d.SkillGoal || len(s.PastGoal) == 0 || s.Trawler == d.Trawler || s.TrawlerQuorum == d.TrawlerQuorum || s.TrawlerFoodLow == d.TrawlerFoodLow || s.TrawlerFoodHigh == d.TrawlerFoodHigh ||
		s.CannonballTarget == d.CannonballTarget || s.CannonballBatch == d.CannonballBatch || s.CannonballMinBars == d.CannonballMinBars {
		t.Errorf("a key was ignored: %+v", s)
	}
}

// From BulkAtSapphires uncut sapphires in the mule, a gem round asks for its whole stock.
func TestPlanGemAskBulkFromBulkAtSapphires(t *testing.T) {
	set := orderSettings()
	set.BulkAtSapphires = 50000
	under := stockMap(map[string]int{"Uncut sapphire": 49999, "Uncut ruby": 14641})
	if a := PlanGemAsk(60, set, nil, under, true); a.Bulk || a.Lines[0] != (Line{"Uncut ruby", 1000}) {
		t.Fatalf("under the threshold: bulk %v lines %v", a.Bulk, a.Lines)
	}
	over := stockMap(map[string]int{"Uncut sapphire": 58001, "Uncut ruby": 14641, "Uncut diamond": 3624})
	a := PlanGemAsk(60, set, nil, over, true)
	want := []Line{{"Uncut diamond", 3624}, {"Uncut ruby", 14641}, {"Uncut sapphire", 58001}}
	if !a.Bulk || !reflect.DeepEqual(a.Lines, want) || a.Gems != 3624+14641+58001 {
		t.Fatalf("bulk %v lines %v gems %d, want the whole stock", a.Bulk, a.Lines, a.Gems)
	}
	set.BulkAtSapphires = 0
	if a := PlanGemAsk(60, set, nil, over, true); a.Bulk {
		t.Fatal("BulkAtSapphires 0 still asked in bulk")
	}
}

// The owner's sapphire trigger (13 Oct 2026): from craftBulkAtSapphires (10,000) uncut sapphires in the mule a
// gem round is due under GemThreshold too, and its ask is the mule's whole stock of every kind.
func TestDecideGemsAtTenThousandSapphires(t *testing.T) {
	if DefaultSettings().BulkAtSapphires != 10000 {
		t.Fatalf("craftBulkAtSapphires default %d, want the owner's 10000", DefaultSettings().BulkAtSapphires)
	}
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	set := orderSettings()
	set.GemThreshold, set.BulkAtSapphires = 20000, 10000
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 10000, "Uncut emerald": 3601, "Chisel": 1})
	d := Decide(set, s)
	if d.Mode != ModeGems || d.Gems != 13601 || !strings.Contains(d.Why, "10000 uncut sapphires") {
		t.Fatalf("10,000 sapphires under the 20,000 threshold: %+v", d)
	}
	if a := PlanGemAsk(60, set, nil, s.Stock, true); !a.Bulk || a.Gems != 13601 {
		t.Fatalf("the round's ask: bulk %v gems %d, want every uncut gem the mule holds", a.Bulk, a.Gems)
	}
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 9999, "Uncut emerald": 3601, "Chisel": 1})
	if d := Decide(set, s); d.Mode == ModeGems {
		t.Fatalf("9,999 sapphires and 13,600 gems: %+v", d)
	}
	set.BulkAtSapphires = 0
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 15000, "Chisel": 1})
	if d := Decide(set, s); d.Mode == ModeGems {
		t.Fatalf("craftBulkAtSapphires 0 turns the trigger off: %+v", d)
	}
}

// Uncut gems in the account's own bank make a gem round due even when the mule holds none.
func TestDecideGemsFromOwnBank(t *testing.T) {
	set := orderSettings()
	led := &Ledger{}
	s := Snapshot{Now: time.Now(), MuleHeard: true, MuleSameWorld: true, Stock: stockMap(map[string]int{}),
		CraftingLevel: 68, OwnUncut: 100931, Ledger: led}
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Fatalf("own bank 100931 uncut, mule empty: %+v, want a gem round", d)
	}
	s.OwnUncut = 10
	if d := Decide(set, s); d.Mode == ModeGems {
		t.Fatalf("own bank 10 uncut: %+v, want no gem round", d)
	}
}
