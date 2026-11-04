package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

func keys(ring []Station) string {
	out := make([]string, len(ring))
	for i, s := range ring {
		out[i] = s.Key()
	}
	return strings.Join(out, " ")
}

func allWorth(n int) func(string, int) int { return func(string, int) int { return n } }

// Test 7: two worlds, starting at Catherby: the exact order, exactly two hops, ending on M.
func TestTwoWorldRing(t *testing.T) {
	p := PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Worth: allWorth(500)})
	want := "deliver@1 draw@1 Betty@1 Aubury@1 hop2 Aubury@2 Betty@2 Lundail@2 hop1 Lundail@1"
	if got := keys(p.Ring); got != want || !p.OBlock {
		t.Fatalf("ring %q (O block %v), want %q", got, p.OBlock, want)
	}
	if Hops(p.Ring) != 2 {
		t.Errorf("%d hops, want 2", Hops(p.Ring))
	}
	if last := p.Ring[len(p.Ring)-1]; last.World != 1 {
		t.Errorf("the round ends on world %d, want the mule's world 1", last.World)
	}
	open, close, ok := OBlock(p.Ring)
	if !ok || open != 4 || close != 8 {
		t.Errorf("O block %d..%d %v, want 4..8", open, close, ok)
	}
	if i, _ := Entry(p.Ring, AreaWest, 1, 1); i != 0 {
		t.Errorf("Catherby on world 1 starts at %d, want the delivery", i)
	}
	// The world the ring is on never changes except at a hop.
	world := 1
	for _, s := range p.Ring {
		if s.Kind == KindHop {
			world = s.World
			continue
		}
		if s.World != world {
			t.Errorf("%s runs on world %d while the account is on %d", s.Key(), s.World, world)
		}
	}
}

// Test 8: the other world under hopMinGain drops the whole O block, and Lundail on M still runs.
func TestOBlockElision(t *testing.T) {
	worth := func(site string, world int) int {
		if world == 2 {
			return 40
		}
		return 900
	}
	p := PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Worth: worth})
	if p.OBlock || Hops(p.Ring) != 0 {
		t.Fatalf("O block kept at worth 120: %q", keys(p.Ring))
	}
	if got, want := keys(p.Ring), "deliver@1 draw@1 Betty@1 Aubury@1 Lundail@1"; got != want {
		t.Errorf("ring %q, want %q", got, want)
	}
	if p.OWorth != 120 || !strings.Contains(p.Why, "under hopMinGain") {
		t.Errorf("OWorth %d, why %q", p.OWorth, p.Why)
	}
	// maxHopsPerRound 0 and hops unavailable also elide it, whatever the worth.
	for _, in := range []RoundInput{
		{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 0, HopsAvailable: true, Worth: allWorth(5000)},
		{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopsAvailable: false, Worth: allWorth(5000)},
	} {
		if p := PlanRound(in); p.OBlock {
			t.Errorf("%+v kept the O block (%s)", in, p.Why)
		}
	}
}

// Test 9: one world never has a hop station.
func TestOneWorldRing(t *testing.T) {
	o := OtherWorld(1, []int{1})
	if o != 0 {
		t.Fatalf("worlds [1] with the mule on 1: other world %d, want none", o)
	}
	p := PlanRound(RoundInput{M: 1, O: o, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 0, HopsAvailable: true, Worth: allWorth(5000)})
	if Hops(p.Ring) != 0 || p.OBlock {
		t.Errorf("one-world ring %q", keys(p.Ring))
	}
	if OtherWorld(1, nil) != 2 || OtherWorld(2, nil) != 1 || OtherWorld(1, []int{1, 3}) != 3 {
		t.Error("OtherWorld defaults")
	}
}

func TestRingLeavesOutDisabledShops(t *testing.T) {
	ring := BuildRing(RingInput{M: 1, O: 2, Shops: []string{SiteLundail}, OBlock: true})
	if got, want := keys(ring), "deliver@1 hop2 Lundail@2 hop1 Lundail@1"; got != want {
		t.Errorf("Lundail-only ring %q, want %q (no draw: nothing east is paid from the pack)", got, want)
	}
	ring = BuildRing(RingInput{M: 1, O: 2, Shops: []string{SiteAubury}, OBlock: false})
	if got, want := keys(ring), "deliver@1 draw@1 Aubury@1"; got != want {
		t.Errorf("Aubury-only ring %q, want %q", got, want)
	}
}

// Test 10: where a round picks up.
func TestEntryPoints(t *testing.T) {
	ring := BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true})
	at := func(x, z int) *tile.Tile { return &tile.Tile{X: x, Z: z} }
	lumbridge := at(3222, 3218)
	for _, c := range []struct {
		name  string
		where *tile.Tile
		world int
		want  string
	}{
		{"Catherby on M", at(2809, 3441), 1, "deliver@1"},
		{"Seers on M", at(2726, 3485), 1, "deliver@1"},
		{"Ardougne on O", at(2655, 3283), 2, "Lundail@2"},
		{"Ardougne lever on M", at(2562, 3311), 1, "Lundail@1"},
		{"Port Sarim on M", at(3012, 3259), 1, "Betty@1"},
		{"Varrock on O", at(3253, 3402), 2, "Aubury@2"},
		{"Lumbridge respawn on M", lumbridge, 1, "Betty@1"},
		{"Lumbridge respawn on O", lumbridge, 2, "Aubury@2"},
		{"Mage Bank on O", at(2534, 4714), 2, "Lundail@2"},
		{"Wilderness on M", at(3154, 3924), 1, "Lundail@1"},
		{"Catherby on O", at(2809, 3441), 2, "Lundail@2"},
	} {
		i, why := Entry(ring, AreaOf(c.where), c.world, 1)
		if got := ring[i].Key(); got != c.want {
			t.Errorf("%s: entry %s (%s), want %s", c.name, got, why, c.want)
		}
	}
	if AreaOf(lumbridge) != AreaEast {
		t.Errorf("a Lumbridge respawn is east: %s", AreaOf(lumbridge))
	}
	if AreaOf(nil) != AreaUnknown {
		t.Error("no tile is unknown")
	}
	// Without the O block, east on O has nothing of its own: the delivery hops it home.
	short := BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: false})
	if i, _ := Entry(short, AreaEast, 2, 1); short[i].Key() != "deliver@1" {
		t.Errorf("east on O with no O block starts at %s", short[i].Key())
	}
}

func TestResume(t *testing.T) {
	st := NewState()
	st.StartRound(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true}), 0, time.Unix(0, 0))
	// A hop to O landed: the cursor (Aubury@2) stands.
	st.JumpTo(Index(st.Ring, "Aubury@2"))
	if i, _ := Resume(st, AreaEast, 2, 1); st.Ring[i].Key() != "Aubury@2" {
		t.Errorf("after a landed hop: %s", st.Ring[i].Key())
	}
	// The hop to O did not land: still on M inside the O block, skip to Lundail@1.
	if i, why := Resume(st, AreaEast, 1, 1); st.Ring[i].Key() != "Lundail@1" {
		t.Errorf("after a failed hop: %s (%s)", st.Ring[i].Key(), why)
	}
	// The hop home did not land: on O at Lundail@1 -- left to the station, which skips it.
	st.JumpTo(Index(st.Ring, "Lundail@1"))
	if i, _ := Resume(st, AreaArdougne, 2, 1); st.Ring[i].Key() != "Lundail@1" {
		t.Errorf("hop home failed: %s", st.Ring[i].Key())
	}
	// No ring yet: Entry.
	if i, _ := Resume(NewState(), AreaWest, 1, 1); i != 0 {
		t.Errorf("fresh state resumes at %d", i)
	}
}

// Test 11: the skip rule, a shop nobody has looked at, and the lab tick.
func TestWorthAndSkip(t *testing.T) {
	if ok, why := WorthVisiting(49, 50); ok || !strings.Contains(why, "minVisitRunes 50") {
		t.Errorf("49 runes under a minimum of 50: %v %q", ok, why)
	}
	if ok, _ := WorthVisiting(50, 50); !ok {
		t.Error("50 runes meets a minimum of 50")
	}
	board := fleetplan.NewShopBoard()
	now := time.Unix(1_000_000, 0)
	stock := map[string]int{}
	for _, r := range DefaultRunes() {
		stock[r] = board.Available(Betty, r, "buyer", now, fleetplan.GameTick)
	}
	if stock["Mind rune"] != 1000 {
		t.Fatalf("no sighting should estimate the base: %v", stock)
	}
	if ok, _ := WorthVisiting(Worth(Betty, stock, DefaultRunes(), defaultCaps), DefaultMinVisitRunes); !ok {
		t.Error("an unseen shop is visited")
	}
	// The lab's 200 ms tick regrows three times as fast.
	l, _ := Betty.Line("Mind rune")
	real := l.EstimateStock(0, now, now.Add(10*time.Minute), 600*time.Millisecond)
	lab := l.EstimateStock(0, now, now.Add(10*time.Minute), 200*time.Millisecond)
	if real != 100 || lab != 300 {
		t.Errorf("10 minutes regrow %d at 600 ms and %d at 200 ms, want 100 and 300", real, lab)
	}
}

// Test 20: the idle wait.
func TestIdleWait(t *testing.T) {
	lines := []WaitLine{
		{Shop: Betty, Rune: "Mind rune", Est: 0, Cap: 18},             // 50 minds: 500 ticks = 5 min
		{Shop: fleetplan.Lundail, Rune: "Mind rune", Est: 0, Cap: 18}, // 50 minds: 2500 ticks = 25 min
		{Shop: fleetplan.Aubury, Rune: "Chaos rune", Est: 0, Cap: 90}, // 50 chaos: 5000 ticks = 50 min
	}
	if got := IdleWait(20*time.Minute, lines, 50, 600*time.Millisecond); got != 5*time.Minute {
		t.Errorf("idle %s, want the smallest RestockWait 5m", got)
	}
	if got := IdleWait(20*time.Minute, lines[1:], 50, 600*time.Millisecond); got != 20*time.Minute {
		t.Errorf("idle %s, want idleMaxMinutes 20m", got)
	}
	// A cap under the rune's base cost can never buy; it does not shorten the wait.
	capped := []WaitLine{{Shop: Betty, Rune: "Mind rune", Est: 0, Cap: 2}}
	if got := IdleWait(20*time.Minute, capped, 50, 600*time.Millisecond); got != 20*time.Minute {
		t.Errorf("an unbuyable line set the wait to %s", got)
	}
	// Never below MinIdle.
	full := []WaitLine{{Shop: Betty, Rune: "Mind rune", Est: 1000, Cap: 18}}
	if got := IdleWait(20*time.Minute, full, 50, 600*time.Millisecond); got != MinIdle {
		t.Errorf("idle %s, want the %s floor", got, MinIdle)
	}
}

func TestPlanString(t *testing.T) {
	ring := BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true})
	if got := PlanString(ring, 2); got != "[Betty@1 Aubury@1 hop2 Aubury@2 Betty@2 Lundail@2 hop1 Lundail@1]" {
		t.Errorf("PlanString = %s", got)
	}
}
