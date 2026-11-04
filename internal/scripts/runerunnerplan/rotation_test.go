package runerunnerplan

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

func pairKeys(ps []Pair) string {
	keys := make([]string, len(ps))
	for i, p := range ps {
		keys[i] = p.Key()
	}
	return strings.Join(keys, " ")
}

// The default buy list is every rune the shops stock, in priority order.
func TestFullStockRunes(t *testing.T) {
	if got := strings.Join(FullStockRunes(BuiltSites()), ","); got !=
		"Mind rune,Air rune,Water rune,Earth rune,Fire rune,Chaos rune,Body rune,Death rune,Nature rune,Law rune,Cosmic rune" {
		t.Errorf("all three shops: %s", got)
	}
	if got := strings.Join(FullStockRunes([]string{SiteAubury, SiteBetty}), ","); got !=
		"Mind rune,Air rune,Water rune,Earth rune,Fire rune,Chaos rune,Body rune,Death rune" {
		t.Errorf("Aubury and Betty: %s", got)
	}
	// Every listed rune is sold by one of the shops, and every line they sell is listed.
	for _, key := range BuiltSites() {
		s, _ := Site(key)
		for _, l := range s.Shop.Lines {
			if !strings.Contains(strings.Join(FullStockRunes(BuiltSites()), ","), l.Rune) {
				t.Errorf("%s's %s is not in the list", key, l.Rune)
			}
		}
	}
}

// The derived cap is three times the base price, and at it a full shelf sells its top ~230 at
// Aubury and Betty and ~67 at Lundail -- never anything at the ceiling's six times.
func TestDerivedCaps(t *testing.T) {
	want := map[string]int{"Mind rune": 9, "Body rune": 9, "Air rune": 12, "Fire rune": 12, "Chaos rune": 45,
		"Death rune": 90, "Nature rune": 60, "Law rune": 120, "Cosmic rune": 45}
	for r, c := range want {
		if got := DerivedCap(r); got != c {
			t.Errorf("%s cap %d, want %d", r, got, c)
		}
	}
	if DerivedCap("Blood rune") != 150 {
		t.Error("blood runes are the Wizards' Guild's, 50 coins: cap 150")
	}
	if DerivedCap("Soul rune") != 0 {
		t.Error("no modelled shop line sells soul runes")
	}
	if got := strings.Join(DerivedCaps([]string{"mind RUNE", "Air rune"}), ","); got != "Mind rune=9,Air rune=12" {
		t.Errorf("entries %s", got)
	}
	n, cost := BuyableUnderCap(fleetplan.Aubury, "Mind rune", 1000, DerivedCap("Mind rune"))
	if n < 200 || n > 240 || cost > n*9 {
		t.Errorf("Aubury minds at cap 9: %d for %d", n, cost)
	}
	if n, _ := BuyableUnderCap(fleetplan.Lundail, "Air rune", 200, DerivedCap("Air rune")); n < 60 || n > 75 {
		t.Errorf("Lundail air at cap 12: %d", n)
	}
	// The depth a cap of three times leaves is regrown within 25 minutes at Aubury and Betty: a
	// circuit coming back sooner loses no regrowth to a full shelf.
	l, _ := Betty.Line("Air rune")
	if w := l.RestockWait(1000-240, 1000, 600*time.Millisecond); w > 25*time.Minute {
		t.Errorf("Betty air regrows its capped depth in %s", w)
	}
}

func TestRotationSettings(t *testing.T) {
	// An empty object rotates: four shops (the Wizards' Guild left out below Magic 66 at run time),
	// worlds 1-3, every rune (blood, the guild's alone, last), derived caps.
	s, err := decode(t, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Rotation || fmt.Sprint([]int(r.Worlds)) != "[1 2 3]" || len(r.RuneList) != 12 || r.CapTable["Death rune"] != 90 || r.CapTable["Blood rune"] != 150 ||
		r.MaxHopsPerHour != MaxMaxHopsPerHour || r.RotationPhase != -1 || !r.SwarmOn() || r.SwarmCheckMinutes != 20 || r.SwarmResumeRunes != 1500 {
		t.Errorf("rotation defaults: rot %v worlds %v runes %v caps %v hops %d phase %d", r.Rotation, r.Worlds, r.RuneList, r.CapTable, r.MaxHopsPerHour, r.RotationPhase)
	}
	// The live runners' settings (one shop, seven runes with caps, homeWorld 3) keep the classic
	// ring and load: homeWorld was refused before this change.
	live := `{"mule": "mule", "homeWorld": 3, "shops": ["Lundail"], "zaffStaffs": 2,
		"runes": ["Mind rune", "Air rune", "Water rune", "Earth rune", "Fire rune", "Chaos rune", "Body rune"],
		"priceCaps": ["Mind rune=18", "Air rune=24", "Water rune=24", "Earth rune=24", "Fire rune=24", "Chaos rune=90", "Body rune=30"]}`
	s, err = decode(t, live)
	if err != nil {
		t.Fatal(err)
	}
	r, _ = s.Resolve()
	if r.Rotation || len(r.Worlds) != 0 || len(r.RuneList) != 7 || r.CapTable["Body rune"] != 30 || r.HomeWorld != 3 {
		t.Errorf("live classic: %+v", r)
	}
	// rotate on keeps the listed runes and caps.
	s, err = decode(t, strings.Replace(live, `"homeWorld": 3`, `"homeWorld": 3, "rotate": "on"`, 1))
	if err != nil {
		t.Fatal(err)
	}
	r, _ = s.Resolve()
	if !r.Rotation || len(r.RuneList) != 7 || r.CapTable["Mind rune"] != 18 || len(r.Worlds) != 3 {
		t.Errorf("live rotate on: %+v", r)
	}
	// Classic defaults are unchanged.
	s, _ = decode(t, `{"rotate": "off"}`)
	r, _ = s.Resolve()
	if r.Rotation || strings.Join(r.RuneList, ",") != "Mind rune,Air rune,Chaos rune" || r.CapTable["Air rune"] != 5 {
		t.Errorf("classic defaults: %v %v", r.RuneList, r.CapTable)
	}
	// Runes listed without caps get the derived ones, on either ring.
	s, _ = decode(t, `{"shops": ["Betty"], "runes": ["Death rune"]}`)
	if r, err = s.Resolve(); err != nil || r.CapTable["Death rune"] != 90 {
		t.Errorf("derived cap for a listed rune: %v %v", r.CapTable, err)
	}
	// Three worlds are fine rotating; seven are not.
	if _, err := decode(t, `{"worlds": [1, 2, 3]}`); err != nil {
		t.Errorf("three rotating worlds: %v", err)
	}
	for _, c := range []struct{ raw, want string }{
		{`{"rotate": "off", "worlds": [1, 2, 3]}`, "plus one other"},
		{`{"worlds": [1, 2, 3, 4, 5, 6, 7]}`, "at most 6"},
		{`{"rotate": "sometimes"}`, "rotate:"},
		{`{"rotationPhase": 100}`, "rotationPhase"},
		{`{"rotationPhase": -2}`, "rotationPhase"},
		{`{"maxHopsPerHour": 0}`, "maxHopsPerHour"},
		{`{"maxHopsPerRound": 0}`, "maxHopsPerRound: 0 with rotate"},
		{`{"homeWorld": -1}`, "homeWorld"},
		{`{"swarmCheckMinutes": 1}`, "swarmCheckMinutes"},
		{`{"swarmResumeRunes": 0}`, "swarmResumeRunes"},
	} {
		if _, err := decode(t, c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.raw, err, c.want)
		}
	}
	// One world with maxHopsPerRound 0 rotates without hopping.
	if _, err := decode(t, `{"worlds": [2], "maxHopsPerRound": 0}`); err != nil {
		t.Errorf("one-world rotation: %v", err)
	}
}

// The circuit snakes the worlds and runs the loop forwards or backwards by phase.
func TestRotationCircuit(t *testing.T) {
	w := []int{1, 2, 3}
	for phase, want := range map[int]string{
		0: "Betty@1 Betty@2 Betty@3 Aubury@3 Aubury@2 Aubury@1 Lundail@1 Lundail@2 Lundail@3",
		1: "Betty@2 Betty@3 Betty@1 Aubury@1 Aubury@3 Aubury@2 Lundail@2 Lundail@3 Lundail@1",
		3: "Lundail@1 Lundail@2 Lundail@3 Aubury@3 Aubury@2 Aubury@1 Betty@1 Betty@2 Betty@3",
		6: "Betty@1 Betty@2 Betty@3 Aubury@3 Aubury@2 Aubury@1 Lundail@1 Lundail@2 Lundail@3",
	} {
		if got := pairKeys(RotationCircuit(BuiltSites(), w, phase)); got != want {
			t.Errorf("phase %d:\n got %s\nwant %s", phase, got, want)
		}
	}
	if got := pairKeys(RotationCircuit([]string{SiteLundail}, []int{2, 3}, 1)); got != "Lundail@3 Lundail@2" {
		t.Errorf("Lundail only: %s", got)
	}
	if RotationCircuit(BuiltSites(), nil, 0) != nil {
		t.Error("no worlds, no circuit")
	}
}

// wizguild: at Magic 66 the circuit gains a Yanille block after Lundail (south of Ardougne), its
// worlds snaking on from Lundail's last; below 66 EligibleShops leaves the circuit exactly as the
// three-shop one, whatever the shops setting lists.
func TestRotationCircuitWithTheWizardsGuild(t *testing.T) {
	w := []int{1, 2, 3}
	for phase, want := range map[int]string{
		0: "Betty@1 Betty@2 Betty@3 Aubury@3 Aubury@2 Aubury@1 Lundail@1 Lundail@2 Lundail@3 WizardsGuild@3 WizardsGuild@2 WizardsGuild@1",
		3: "WizardsGuild@1 WizardsGuild@2 WizardsGuild@3 Lundail@3 Lundail@2 Lundail@1 Aubury@1 Aubury@2 Aubury@3 Betty@3 Betty@2 Betty@1",
	} {
		if got := pairKeys(RotationCircuit(EligibleShops(DefaultSites(), 66), w, phase)); got != want {
			t.Errorf("Magic 66, phase %d:\n got %s\nwant %s", phase, got, want)
		}
	}
	for phase := 0; phase < RotationPhases(len(w)); phase++ {
		if got, want := pairKeys(RotationCircuit(EligibleShops(DefaultSites(), 65), w, phase)), pairKeys(RotationCircuit(BuiltSites(), w, phase)); got != want {
			t.Errorf("Magic 65, phase %d: %s, want the three-shop circuit %s", phase, got, want)
		}
	}
	// The guild is pack-paid: a guild-first round draws its coins at the mule's post, and its
	// stations are ordinary buy stations.
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: w, Shops: EligibleShops(DefaultSites(), 66), Phase: 3})
	if ring[1].Kind != KindDraw {
		t.Errorf("a guild-first round opens %s, want a draw", PlanString(ring, 0))
	}
	for _, s := range ring {
		if s.Site == SiteWizardsGuild && s.Kind != KindBuy {
			t.Errorf("%s is a %s station, want buy", s.Key(), s.Kind)
		}
	}
	if got := strings.Join(FullStockRunes(DefaultSites()), ","); !strings.HasPrefix(got, "Blood rune,") || !strings.HasSuffix(got, ",Cosmic rune") {
		t.Errorf("full stock with the guild: %s", got)
	}
}

// Six runners on phases 0-5 that step through the circuit together are never on the same pair,
// except a forward and a reverse runner with the same turn of the worlds in the middle shop.
func TestRotationSpread(t *testing.T) {
	w := []int{1, 2, 3}
	circuits := make([][]Pair, RotationPhases(len(w)))
	for p := range circuits {
		circuits[p] = RotationCircuit(BuiltSites(), w, p)
	}
	for i := range circuits[0] {
		for a := 0; a < len(circuits); a++ {
			for b := a + 1; b < len(circuits); b++ {
				if circuits[a][i] != circuits[b][i] {
					continue
				}
				middle := circuits[a][i].Site == SiteAubury && a%3 == b%3
				if !middle {
					t.Errorf("step %d: phases %d and %d are both at %s", i, a, b, circuits[a][i].Key())
				}
			}
		}
	}
	// Each phase visits every pair exactly once.
	for p, c := range circuits {
		seen := map[Pair]bool{}
		for _, x := range c {
			seen[x] = true
		}
		if len(seen) != 9 {
			t.Errorf("phase %d visits %d pairs", p, len(seen))
		}
	}
	// A name's phase is stable and in range; a setting wins.
	if a, b := RotationPhaseFor("Acct211", 3), RotationPhaseFor("acct211 ", 3); a != b || a < 0 || a >= 6 {
		t.Errorf("name phases %d %d", a, b)
	}
	if ResolvePhase(4, "x", 3) != 4 || ResolvePhase(9, "x", 3) != 3 || ResolvePhase(-1, "x", 3) != RotationPhaseFor("x", 3) {
		t.Error("ResolvePhase")
	}
}

func TestBuildRotationRing(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: BuiltSites(), Phase: 0, Staffs: true})
	want := "[deliver@1 draw@1 hop1 Betty@1 hop2 Betty@2 hop3 Betty@3 hop3 Aubury@3 Zaff@3 hop2 Aubury@2 Zaff@2 hop1 Aubury@1 Zaff@1 " +
		"hop1 Lundail@1 hop2 Lundail@2 hop3 Lundail@3 hop1]"
	if got := PlanString(ring, 0); got != want {
		t.Fatalf("ring\n got %s\nwant %s", got, want)
	}
	// Seven real hops: a hop to the world the last one went to is no hop.
	real, at := 0, 1
	for _, s := range ring {
		if s.Kind == KindHop {
			if s.World != at {
				real++
			}
			at = s.World
		}
	}
	if real != 7 {
		t.Errorf("%d real hops", real)
	}
	// Lundail first: no draw (Gundai pays), but the purse station (the mule top-up alone) after the
	// delivery, and the shops' coins come from their banks' top-up.
	back := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: BuiltSites(), Phase: 3})
	if back[1].Kind != KindPurse || back[2].Kind != KindHop || back[3].Kind != KindLundail {
		t.Errorf("reverse ring starts %s", PlanString(back, 0))
	}
	// Blocks: each hop opens the stations up to the next hop.
	i := Index(ring, "Aubury@3") - 1
	if ring[i].Kind != KindHop || PlanString(BlockShops(ring, i), 0) != "[Aubury@3]" || ring[BlockEnd(ring, i)].Key() != "hop2" {
		t.Errorf("block at %d: %s end %d", i, PlanString(BlockShops(ring, i), 0), BlockEnd(ring, i))
	}
	if last := len(ring) - 1; BlockEnd(ring, last) != len(ring) || len(BlockShops(ring, last)) != 0 {
		t.Error("the closing hop opens no shop block")
	}
}

func TestRotationEntryAndResume(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: BuiltSites(), Phase: 0})
	for _, c := range []struct {
		area  Area
		world int
		want  string
	}{
		{AreaWild, 2, "Lundail@2"},
		{AreaArdougne, 3, "Lundail@3"},
		{AreaArdougne, 5, "hop1"}, // no Lundail on world 5: the hop before the first Lundail
		{AreaEast, 2, "Betty@2"},
		{AreaWest, 2, "deliver@1"},
		{AreaUnknown, 1, "deliver@1"},
	} {
		i, why := RotationEntry(ring, c.area, c.world)
		if ring[i].Key() != c.want {
			t.Errorf("%s on %d: %s (%s), want %s", c.area, c.world, ring[i].Key(), why, c.want)
		}
	}
	if i, _ := RotationEntry(ring, AreaArdougne, 5); ring[i+1].Key() != "Lundail@1" {
		t.Error("the hop before Lundail@1")
	}
	// A rotation's saved cursor always stands, even on the mule's world between other worlds' stations.
	st := NewState()
	st.StartRound(ring, Index(ring, "Betty@3"), time.Now())
	st.Rotation = true
	if i, _ := Resume(st, AreaEast, 1, 1); i != st.Cursor {
		t.Errorf("rotation resume moved the cursor to %d", i)
	}
}

func TestHopBudget(t *testing.T) {
	now := time.Now()
	if w, _ := HopBudget(nil, now, 12); w != 0 {
		t.Errorf("no hops: %s", w)
	}
	if w, why := HopBudget([]time.Time{now.Add(-30 * time.Second)}, now, 12); w != 90*time.Second || !strings.Contains(why, "apart") {
		t.Errorf("30s after a hop: %s %s", w, why)
	}
	var hours []time.Time
	for i := 0; i < 12; i++ {
		hours = append(hours, now.Add(-time.Duration(50-i*4)*time.Minute))
	}
	if w, why := HopBudget(hours, now, 12); w != 10*time.Minute || !strings.Contains(why, "maxHopsPerHour 12") {
		t.Errorf("12 in the hour: %s %s", w, why)
	}
	if w, _ := HopBudget(append(hours, now.Add(-2*time.Hour)), now, 13); w != 0 {
		t.Errorf("an old hop does not count: %s", w)
	}
	st := NewState()
	st.NoteHop(now.Add(-2 * time.Hour))
	st.NoteHop(now)
	if len(st.HopTimes) != 1 {
		t.Errorf("NoteHop prunes: %v", st.HopTimes)
	}
	st.UnnoteHop(now)
	if len(st.HopTimes) != 0 {
		t.Errorf("UnnoteHop: %v", st.HopTimes)
	}
}

// Timed by restock: a pair bought down to its cap is not worth a hop, the next one regrown is;
// and the idle wait comes from the regrowth.
func TestRotationRestockSkipping(t *testing.T) {
	runes := FullStockRunes([]string{SiteAubury, SiteBetty})
	caps, err := ParseCaps(DerivedCaps(runes), runes)
	if err != nil {
		t.Fatal(err)
	}
	full, bought := map[string]int{}, map[string]int{}
	for _, r := range runes {
		l, _ := Betty.Line(r)
		full[r] = l.Base
		n, _ := BuyableUnderCap(Betty, r, l.Base, caps.For(r))
		bought[r] = l.Base - n
	}
	if w := Worth(Betty, bought, runes, caps); w != 0 {
		t.Errorf("a shelf just bought down is worth %d", w)
	}
	if w := Worth(Betty, full, runes, caps); w < DefaultHopMinGain || w < 1500 {
		t.Errorf("a full Betty is worth %d", w)
	}
	// Ten minutes of regrowth at 600 ms is 100 of each fast line: worth a hop.
	regrown := map[string]int{}
	for _, r := range runes {
		l, _ := Betty.Line(r)
		regrown[r] = l.EstimateStock(bought[r], time.Now().Add(-10*time.Minute), time.Now(), 600*time.Millisecond)
	}
	if w := Worth(Betty, regrown, runes, caps); w < DefaultHopMinGain {
		t.Errorf("ten minutes' regrowth is worth %d", w)
	}
	var lines []WaitLine
	for _, r := range runes {
		lines = append(lines, WaitLine{Shop: Betty, Rune: r, Est: bought[r], Cap: caps.For(r)})
	}
	if w := IdleWait(20*time.Minute, lines, DefaultMinVisitRunes, 600*time.Millisecond); w != 5*time.Minute {
		t.Errorf("idle wait %s, want 5m (50 runes at one per 10 ticks)", w)
	}
}

func TestSwarmSwitch(t *testing.T) {
	on := SwarmCheck{On: true, Rotation: true, Resume: 1500}
	low, high := on, on
	low.CircuitWorth, high.CircuitWorth = 900, 1500
	if ok, why := SwarmAfterRound(low); !ok || !strings.Contains(why, "under swarmResumeRunes 1500") {
		t.Errorf("a poor circuit switches: %v %s", ok, why)
	}
	if ok, _ := SwarmAfterRound(high); ok {
		t.Error("a circuit worth the threshold keeps going")
	}
	off, classic := low, low
	off.On, classic.Rotation = false, false
	if ok, _ := SwarmAfterRound(off); ok {
		t.Error("swarmWhileWaiting off never switches")
	}
	if ok, _ := SwarmAfterRound(classic); ok {
		t.Error("the classic ring never switches")
	}
	if back, _ := SwarmResume(low); back {
		t.Error("stays on ScarabSwarm while the circuit is poor")
	}
	if back, _ := SwarmResume(high); !back {
		t.Error("goes back once the circuit is worth it")
	}
	if back, _ := SwarmResume(off); !back {
		t.Error("goes back when the setting is turned off")
	}
	worth := map[string]int{"Betty@1": 700, "Aubury@2": 900}
	if n := CircuitWorth(RotationCircuit(BuiltSites(), []int{1, 2}, 0), func(site string, w int) int { return worth[fmt.Sprintf("%s@%d", site, w)] }); n != 1600 {
		t.Errorf("circuit worth %d", n)
	}
	now := time.Now()
	if !SwarmCheckDue(time.Time{}, now, 20*time.Minute) || SwarmCheckDue(now.Add(-time.Minute), now, 20*time.Minute) || !SwarmCheckDue(now.Add(-20*time.Minute), now, 20*time.Minute) {
		t.Error("SwarmCheckDue")
	}
	s := DefaultSettings()
	s.SwarmSettings = map[string]interface{}{"maxStage": 5.0}
	m := s.SwarmSettingsMap()
	if m["mule"] != "mule" || m["maxStage"] != 5.0 {
		t.Errorf("swarm settings %v", m)
	}
	// The circuit's coins stay: no kit-ask hand-over, and the daily trip keeps the round's whole float.
	if m["consolidateOnKitAsk"] != false || m["consolidateCoinsReserve"] != float64(SwarmKeepAllCoins) || SwarmKeepAllCoins < 100*MaxCircuitFunds {
		t.Errorf("swarm hand-over settings %v", m)
	}
	s.SwarmSettings["consolidateOnKitAsk"] = true
	if s.SwarmSettingsMap()["consolidateOnKitAsk"] != true {
		t.Error("an explicit consolidateOnKitAsk stands")
	}
	delete(s.SwarmSettings, "consolidateOnKitAsk")
	m["x"] = 1
	if _, leaked := s.SwarmSettings["x"]; leaked {
		t.Error("SwarmSettingsMap must copy")
	}
	s.SwarmSettings["mule"] = "othermule"
	if s.SwarmSettingsMap()["mule"] != "othermule" {
		t.Error("an explicit swarm mule stands")
	}
	if PlanString(DeliveryRing(1), 0) != "[deliver@1]" {
		t.Error("DeliveryRing")
	}
}
