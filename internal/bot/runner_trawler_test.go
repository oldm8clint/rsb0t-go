package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// Every script but the trawler's own, the anchor's and the mule's joins master's blocks; ScarabSwarm crew join on their own.
func TestRunnerTrawlerEligible(t *testing.T) {
	bus := mulebus.NewBus()
	run := func(script, user string, settings map[string]interface{}) *Runner {
		ctx := &Context{Username: user, Bus: bus, Settings: settings}
		return NewRunner(script, nil, ctx)
	}
	cases := []struct {
		script, user string
		settings     map[string]interface{}
		want         bool
	}{
		{"WCFletch", "acct1", nil, true},
		{"BassOysterCaskets", "fisher", map[string]interface{}{}, true},
		{"ScarabSwarm", "acct2", map[string]interface{}{"trawlerCrew": true}, false},
		{"ScarabSwarm", "acct3", map[string]interface{}{}, true},
		{"FishingTrawler", "acct4", nil, false},
		{"MasterCrafter", "master", nil, false},
		{"MuleReceiver", "mule1", nil, false},
		{"WCFletch", "Mule1", nil, false},
	}
	for _, c := range cases {
		if got := run(c.script, c.user, c.settings).trawlerEligible(); got != c.want {
			t.Errorf("%s on %s: eligible %v, want %v", c.script, c.user, got, c.want)
		}
	}
	r := run("WCFletch", "acct1", nil)
	r.ctx.Bus = nil
	if r.trawlerEligible() {
		t.Error("no wall bus: no block to follow")
	}
}

// gatedBot is a script with a trawler gate and a hand-back hook.
type gatedBot struct {
	ready bool
	why   string
	asked int
	backs int
}

func (g *gatedBot) OnStart(*Context) error { return nil }
func (g *gatedBot) Loop(*Context) error    { return nil }
func (g *gatedBot) OnStop(*Context)        {}
func (g *gatedBot) TrawlerReady(*Context) (bool, string) {
	g.asked++
	return g.ready, g.why
}
func (g *gatedBot) TrawlerBack(*Context) { g.backs++ }

type plainBot struct{}

func (plainBot) OnStart(*Context) error { return nil }
func (plainBot) Loop(*Context) error    { return nil }
func (plainBot) OnStop(*Context)        {}

// A script whose gate says no keeps its account, with the reason logged at most every five minutes; a yes logs the join.
// A script without a gate joins as before. The hand-back hook runs when the crew gives the account back.
func TestRunnerTrawlerGate(t *testing.T) {
	var lines []string
	ctx := &Context{Username: "runner1", Bus: mulebus.NewBus(), Log: func(s string) { lines = append(lines, s) }}
	g := &gatedBot{why: "round=764 is under way at Betty@1 (2 of 30 stations done)"}
	r := NewRunner("RuneRunner", g, ctx)
	r.trawler.world = 1
	now := time.Now()
	if r.trawlerGateAllows(now) || r.trawlerGateAllows(now.Add(time.Minute)) {
		t.Fatal("a gate that says no must keep the account")
	}
	if g.asked != 2 || len(lines) != 1 || !strings.Contains(lines[0], "RuneRunner is not ready: round=764 is under way") {
		t.Errorf("asked %d, lines %q", g.asked, lines)
	}
	r.trawlerGateAllows(now.Add(trawlerGateSayEvery))
	if len(lines) != 2 {
		t.Errorf("the hold line repeats after %s: %q", trawlerGateSayEvery, lines)
	}
	g.ready, g.why = true, "round=764 done and round=765's delivery made"
	if !r.trawlerGateAllows(now.Add(10 * time.Minute)) {
		t.Fatal("a gate that says yes must let the crew join")
	}
	if last := lines[len(lines)-1]; last != "trawler crew: round=764 done and round=765's delivery made: joining the trawler crew" {
		t.Errorf("join line %q", last)
	}
	r.trawlerHandBack()
	if g.backs != 1 || r.trawler.crew != nil {
		t.Errorf("hand-back hook ran %d times", g.backs)
	}
	plain := NewRunner("WCFletch", plainBot{}, &Context{Username: "acct1", Bus: mulebus.NewBus()})
	if !plain.trawlerGateAllows(now) {
		t.Error("a script without a gate joins whenever the block is on")
	}
	plain.trawlerHandBack()
	// RuneRunner implements both.
	var _ trawlerGate = (*runeRunnerBot)(nil)
	var _ trawlerHandBack = (*runeRunnerBot)(nil)
}

// RuneRunner's gate: a hop asked for holds; the ScarabSwarm phase is between circuits; otherwise the plan's answer, with
// the join noted for the stint's clock.
func TestRuneRunnerTrawlerReady(t *testing.T) {
	ctx := &Context{}
	b := &runeRunnerBot{}
	b.set.TrawlerStintMinutes, b.set.TrawlerRoundHoldMaxMinutes = 20, 180
	b.hopAsked, b.hopTo = time.Now(), 2
	if ok, why := b.TrawlerReady(ctx); ok || !strings.Contains(why, "hop to world 2") {
		t.Errorf("hop asked: %v %q", ok, why)
	}
	b.hopAsked = time.Time{}
	b.st.Swarm = true
	if ok, _ := b.TrawlerReady(ctx); !ok {
		t.Error("the ScarabSwarm phase is between circuits")
	}
	b.st.Swarm = false
	b.st.Round, b.st.RoundStarted, b.st.Cursor = 9, time.Now(), 1
	b.st.Ring = []runerunnerplan.Station{{Kind: runerunnerplan.KindDeliver, World: 1}, {Kind: runerunnerplan.KindBuy, Site: "Betty", World: 1}}
	b.st.Done = map[string]runerunnerplan.StationResult{"deliver@1": {Outcome: runerunnerplan.OutcomeDelivered}}
	if ok, why := b.TrawlerReady(ctx); !ok || !b.st.TrawlerSince.IsZero() {
		t.Errorf("boundary: %v %q; the stint clock starts only on TrawlerJoined", ok, why)
	}
	b.TrawlerJoined(ctx)
	if ok, why := b.TrawlerReady(ctx); !ok || b.st.TrawlerRound != 9 || b.st.TrawlerSince.IsZero() {
		t.Errorf("boundary: %v %q, stint round %d", ok, why, b.st.TrawlerRound)
	}
	b.st.Cursor = 1
	b.st.Done = map[string]runerunnerplan.StationResult{}
	if ok, _ := b.TrawlerReady(ctx); ok {
		t.Error("a round whose delivery is not done holds the account")
	}
	b.resumed = true
	b.TrawlerBack(ctx)
	if b.resumed {
		t.Error("TrawlerBack must make Resume place the cursor again")
	}
}
