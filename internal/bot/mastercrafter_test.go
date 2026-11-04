package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
)

func TestMasterCrafterRegistered(t *testing.T) {
	b, ok := New("MasterCrafter")
	if !ok {
		t.Fatal("MasterCrafter is not registered")
	}
	if _, ok := b.(*masterCrafterBot); !ok {
		t.Fatalf("MasterCrafter builds %T", b)
	}
	if skill, ok := PrimarySkill("MasterCrafter"); !ok || skill != "Crafting" {
		t.Errorf("PrimarySkill(MasterCrafter) = %q/%v, want Crafting/true", skill, ok)
	}
}

// A typo in a craft key is an error before anything starts, not a default left silently in place.
func TestMasterCrafterRefusesATypoedCraftKey(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{"craftGemThreshhold": 5.0}}
	b := &masterCrafterBot{newSwarm: func() masterSwarm { t.Fatal("ScarabSwarm started on bad settings"); return nil }}
	err := b.OnStart(ctx)
	if err == nil || !strings.Contains(err.Error(), "craftGemThreshhold") {
		t.Fatalf("OnStart = %v, want a refusal naming craftGemThreshhold", err)
	}
}

func TestMasterCrafterRefusesWithoutAPack(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{}}
	if err := (&masterCrafterBot{}).OnStart(ctx); err == nil || !strings.Contains(err.Error(), "collision pack") {
		t.Fatalf("OnStart = %v", err)
	}
}

// ScarabSwarm's own keys in the same map are not this script's business.
func TestMasterCrafterIgnoresScarabSwarmKeys(t *testing.T) {
	_, err := mastercraftplan.DecodeSettings(map[string]interface{}{
		"mule": "mule", "maxStage": 6.0, "stage5Quests": []interface{}{"waterfall"}, "homeWorld": 1.0,
		"consolidateToMule": true, "stage3MagicTarget": 66.0, "pinWorld": 1.0,
	})
	if err != nil {
		t.Fatalf("a Scarab settings map must load: %v", err)
	}
}

type fakeMasterSwarm struct {
	rig  *masterRig
	safe mastercraftplan.SafeInput

	starts, stops, loops int
}

func (f *fakeMasterSwarm) OnStart(*Context) error {
	f.starts++
	f.rig.events = append(f.rig.events, "start")
	return nil
}
func (f *fakeMasterSwarm) Loop(*Context) error {
	f.loops++
	f.rig.events = append(f.rig.events, "loop")
	return nil
}
func (f *fakeMasterSwarm) OnStop(*Context) { f.stops++; f.rig.events = append(f.rig.events, "stop") }
func (f *fakeMasterSwarm) safeInput(*Context) mastercraftplan.SafeInput {
	return f.rig.safe
}

type masterRig struct {
	b        *masterCrafterBot
	ctx      *Context
	events   []string
	safe     mastercraftplan.SafeInput
	decision mastercraftplan.Decision
	outcome  masterRoundOutcome
	rounds   []mastercraftplan.Mode
	checks   int
	logs     []string
	// sim is the bank-and-pack simulator of a newSimRig.
	sim *actionsim.Session
}

// masterOrderSettings is the default settings with the thresholds these tests were written against
// (500 gems, 100 bow pairs, 100,000 pearls, one Bolt, 1,000 arrows): they test the rounds, their
// order and their backoffs, not the owner's 20,000s (mastercraftplan.TestDefaultThresholdsAreTheOwners).
func masterOrderSettings() mastercraftplan.Settings {
	s := mastercraftplan.DefaultSettings()
	s.GemThreshold, s.BowThreshold, s.PearlThreshold, s.BoltThreshold, s.ArrowThreshold = 500, 100, 100000, 1, 1000
	return s
}

func newMasterRig(t *testing.T) *masterRig {
	t.Helper()
	r := &masterRig{safe: mastercraftplan.SafeInput{TileKnown: true, HP: 40, MaxHP: 40}}
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeSwarm, Why: "nothing to do"}
	s := newAPISession()
	r.ctx = NewContext(s, "main", nil, nil, nil, nil, mulebus.NewBus(), nil, nil, map[string]interface{}{}, func(l string) { r.logs = append(r.logs, l) })
	r.b = &masterCrafterBot{
		set: masterOrderSettings(), mule: "mule",
		denied: map[string]bool{}, rounds: map[mastercraftplan.Mode]int{},
		store: settings.NewStore(t.TempDir()),
	}
	r.b.every = r.b.set.CheckEvery
	r.b.newSwarm = func() masterSwarm { return &fakeMasterSwarm{rig: r} }
	r.b.farFromMuleSeam = func(*Context) bool { return false } // the account stands by the mule
	r.b.decide = func(*Context) mastercraftplan.Decision { r.checks++; return r.decision }
	r.b.round = func(_ *Context, dec mastercraftplan.Decision) masterRoundOutcome {
		r.events = append(r.events, "round:"+string(dec.Mode))
		r.rounds = append(r.rounds, dec.Mode)
		out := r.outcome
		out.Mode = dec.Mode
		return out
	}
	if err := r.b.startSwarm(r.ctx); err != nil {
		t.Fatal(err)
	}
	r.events = nil
	return r
}

func (r *masterRig) dueNow() { r.b.lastCheck = time.Time{} }

func (r *masterRig) loop(t *testing.T) {
	t.Helper()
	if err := r.b.Loop(r.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMasterCrafterCarriesOnWhenNothingIsDue(t *testing.T) {
	r := newMasterRig(t)
	r.loop(t) // first pass: a check is due, it says swarm
	r.loop(t)
	r.loop(t)
	if r.checks != 1 {
		t.Fatalf("%d checks in three passes, want 1 (every 20 minutes)", r.checks)
	}
	if len(r.rounds) != 0 || strings.Join(r.events, ",") != "loop,loop,loop" {
		t.Fatalf("events %v, rounds %v", r.events, r.rounds)
	}
}

// THE CORE: a due round, at a safe point, stops ScarabSwarm (OnStop saves its checkpoint), runs the
// round, and starts a fresh ScarabSwarm.
func TestMasterCrafterSwitchesAtASafePointAndResumes(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems, Why: "600 uncut gems", Gems: 600}
	r.outcome = masterRoundOutcome{Produced: 600, Handed: 600}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:gems,start" {
		t.Fatalf("events %q, want ScarabSwarm stopped, the round, ScarabSwarm started again", got)
	}
	if r.b.pending.Mode != "" {
		t.Fatalf("pending %v after the round", r.b.pending)
	}
	if l := r.b.ledger; l.GemRounds != 1 || l.GemsCut != 600 || !l.GemPauseUntil.IsZero() {
		t.Fatalf("ledger %+v: a round that cut gems sets no pause", l)
	}
	// The next check is a whole interval away.
	r.events = nil
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems}
	r.loop(t)
	if strings.Join(r.events, ",") != "loop" {
		t.Fatalf("events %v: the round ran again at once", r.events)
	}
	// The ledger is on disk.
	if _, err := os.Stat(filepath.Join(r.b.store.Dir, "main", MasterCrafterStoreID+".json")); err != nil {
		t.Fatalf("ledger not saved: %v", err)
	}
}

func TestMasterCrafterWaitsForTheSafePoint(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeBows, Why: "500 pairs", Pairs: 500}
	r.safe.InCombat = true
	r.loop(t)
	r.loop(t)
	r.loop(t)
	if len(r.rounds) != 0 || strings.Join(r.events, ",") != "loop,loop,loop" {
		t.Fatalf("a round started in a fight: events %v", r.events)
	}
	if r.checks != 1 {
		t.Fatalf("%d checks: a pending round is not re-decided every pass", r.checks)
	}
	if r.b.pending.Mode != mastercraftplan.ModeBows {
		t.Fatalf("pending %v", r.b.pending)
	}
	// Every unsafe reason holds ScarabSwarm.
	for name, mut := range map[string]func(*mastercraftplan.SafeInput){
		"hurt": func(s *mastercraftplan.SafeInput) { s.Hurt = true }, "errand": func(s *mastercraftplan.SafeInput) { s.Errand = true },
		"trip": func(s *mastercraftplan.SafeInput) { s.Trip = true }, "quest": func(s *mastercraftplan.SafeInput) { s.Quest = true },
		"modal": func(s *mastercraftplan.SafeInput) { s.ModalOpen = true },
	} {
		r.safe = mastercraftplan.SafeInput{TileKnown: true, HP: 40, MaxHP: 40}
		mut(&r.safe)
		r.events = nil
		r.loop(t)
		if len(r.rounds) != 0 {
			t.Fatalf("%s: a round started", name)
		}
	}
	// Safe at last: the pending round runs without another check first; the one decision after it
	// is the chain's (mastercraftplan.Chain), which here finds the same kind of round and waits.
	r.safe = mastercraftplan.SafeInput{TileKnown: true, HP: 40, MaxHP: 40}
	r.events = nil
	before := r.checks
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:bows,start" || r.checks != before+1 {
		t.Fatalf("events %q, checks %d -> %d", got, before, r.checks)
	}
}

// The next job follows as soon as it is ready (a user request): a gem round, then straight on to
// the pearl round and the bow round from the mule's post, with no ScarabSwarm between, then
// ScarabSwarm once nothing is ready. A round of the same kind decided on the mule's bank waits for
// the next check; one that failed for a passing reason ends the chain.
func TestMasterCrafterChainsTheNextReadyRound(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems, Why: "uncut gems"}
	next := []mastercraftplan.Decision{
		{Mode: mastercraftplan.ModePearls, Why: "100,000 pearls"},
		{Mode: mastercraftplan.ModeBows, Why: "10,000 pairs"},
		{Mode: mastercraftplan.ModeBows, Why: "the mule's bank still shows the bows"},
	}
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		if len(r.rounds) == 0 {
			return r.decision
		}
		d := next[0]
		if len(next) > 1 {
			next = next[1:]
		}
		return d
	}
	r.outcome = masterRoundOutcome{Produced: 27}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:gems,round:pearls,round:bows,start" {
		t.Fatalf("events %q", got)
	}
	// A passing failure ends the chain at once.
	r = newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems}
	r.outcome = masterRoundOutcome{Transient: true, Why: "the mule is away"}
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		if len(r.rounds) == 0 {
			return r.decision
		}
		return mastercraftplan.Decision{Mode: mastercraftplan.ModePearls}
	}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:gems,start" {
		t.Fatalf("after a passing failure: events %q", got)
	}
}

func TestMasterCrafterDropsADueRoundWhenTheMuleEmpties(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems, Why: "600 uncut gems"}
	r.safe.InCombat = true
	r.loop(t)
	if r.b.pending.Mode != mastercraftplan.ModeGems {
		t.Fatal("no pending round")
	}
	// Another account took the gems before the safe point came: the next check withdraws the round.
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeSwarm, Why: "the mule holds 3 cuttable uncut gems"}
	r.dueNow()
	r.safe.InCombat = false
	r.events = nil
	r.loop(t)
	if len(r.rounds) != 0 || r.b.pending.Mode != "" {
		t.Fatalf("a round the mule can no longer fund ran: events %v", r.events)
	}
}

func TestMasterCrafterLooksAgainSoonWithoutData(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeSwarm, Why: "the mule is not heard on the bus", Inconclusive: true}
	r.loop(t)
	if r.b.every != masterCrafterRecheck {
		t.Fatalf("recheck after %s, want %s", r.b.every, masterCrafterRecheck)
	}
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeSwarm, Why: "under the threshold"}
	r.dueNow()
	r.loop(t)
	if r.b.every != r.b.set.CheckEvery {
		t.Fatalf("a conclusive check waits %s, want %s", r.b.every, r.b.set.CheckEvery)
	}
}

func TestMasterCrafterPausesAfterAFruitlessRoundOnly(t *testing.T) {
	r := newMasterRig(t)
	run := func(out masterRoundOutcome) {
		r.outcome = out
		r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems}
		r.dueNow()
		r.loop(t)
	}
	run(masterRoundOutcome{Produced: 0, Why: "the mule has no chisel"})
	if !r.b.ledger.GemPauseUntil.After(time.Now().Add(100 * time.Minute)) {
		t.Fatalf("a round that did nothing set no pause: %v", r.b.ledger.GemPauseUntil)
	}
	run(masterRoundOutcome{Produced: 12})
	if !r.b.ledger.GemPauseUntil.IsZero() {
		t.Fatal("a round that cut gems must clear the pause")
	}
	run(masterRoundOutcome{Produced: 0, Transient: true, Why: "the mule is away"})
	if !r.b.ledger.GemPauseUntil.IsZero() {
		t.Fatal("a passing failure must not pause the round")
	}
	if r.b.ledger.GemRounds != 3 {
		t.Fatalf("%d gem rounds counted", r.b.ledger.GemRounds)
	}
}

func TestMasterCrafterDoesNotRestartAfterAStop(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems}
	r.b.round = func(ctx *Context, dec mastercraftplan.Decision) masterRoundOutcome {
		ctx.requestStop() // the panel's Stop during the round
		return masterRoundOutcome{Mode: dec.Mode}
	}
	r.loop(t)
	if strings.Join(r.events, ",") != "stop" {
		t.Fatalf("events %v: ScarabSwarm must not start after a Stop", r.events)
	}
	r.b.OnStop(r.ctx) // with no swarm running, OnStop must not touch one
	if strings.Contains(strings.Join(r.events, ","), "stop,stop") {
		t.Fatalf("events %v", r.events)
	}
}

func TestMasterCrafterLedgerRoundTripAndUnreadableFile(t *testing.T) {
	r := newMasterRig(t)
	r.b.ledger.NoteStrungDelivered("Maple longbow", 300)
	r.b.ledger.PendingDeliver = true
	r.b.ledger.GemsCut = 77
	r.b.saveLedger(r.ctx)
	r.b.ledger = mastercraftplan.Ledger{}
	r.b.loadLedger(r.ctx)
	if r.b.ledger.Strung("Maple longbow") != 300 || !r.b.ledger.PendingDeliver || r.b.ledger.GemsCut != 77 {
		t.Fatalf("ledger %+v", r.b.ledger)
	}
	// A file that cannot be read is moved aside, never written over.
	path := filepath.Join(r.b.store.Dir, "main", MasterCrafterStoreID+".json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.b.loadLedger(r.ctx)
	if r.b.ledger.Strung("Maple longbow") != 0 || r.b.ledgerBlocked != "" {
		t.Fatalf("ledger %+v blocked %q", r.b.ledger, r.b.ledgerBlocked)
	}
	matches, _ := filepath.Glob(path + ".bad-*")
	if len(matches) != 1 {
		t.Fatalf("the unreadable ledger was not kept aside: %v", matches)
	}
}

// ---- ScarabSwarm's safe-point reading ----

func masterPut(t *testing.T, st *gamestate.State, x, z int) {
	t.Helper()
	rebuildAt(st, x>>3, z>>3)
	placeAt(st, 0, x-((x>>3)-6)*8, z-((z>>3)-6)*8)
	if hx, hz, _, ok := st.Tile(); !ok || hx != x || hz != z {
		t.Fatalf("fixture: at %d,%d, want %d,%d", hx, hz, x, z)
	}
}

// ---- the live decision ----

func TestMasterCrafterDecidesFromTheBusAndThePublishedBank(t *testing.T) {
	r := newMasterRig(t)
	r.b.decide = nil // the real one
	r.ctx.CurrentWorld = 1
	r.ctx.Peers = peerbank.New()
	mindHoldStat(r.ctx.Session.State(), gemrunner.CraftingStat, levels.XPAtLevel(30), 30)
	mindHoldStat(r.ctx.Session.State(), bowstringrunner.FletchingStat, levels.XPAtLevel(60), 60)

	publish := func(bank map[string]int) {
		var hs []peerbank.Holding
		for k, v := range bank {
			hs = append(hs, peerbank.Holding{Item: k, Count: v})
		}
		r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, hs, nil, false)
	}
	// No heartbeat: the mule is not there.
	publish(map[string]int{"Uncut sapphire": 900, "Chisel": 1})
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeSwarm || !d.Inconclusive {
		t.Fatalf("no heartbeat: %+v", d)
	}
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeGems || d.Gems != 900 {
		t.Fatalf("900 sapphires at Crafting 30: %+v", d)
	}
	publish(map[string]int{"Uncut sapphire": 100, "Maple longbow": 400, "Bow string": 400})
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeBows || d.Pairs != 400 {
		t.Fatalf("400 maple longbows at Fletching 60: %+v", d)
	}
	// 380 of those are the strung ones this account handed the mule.
	r.b.ledger.NoteStrungDelivered("Maple longbow", 380)
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeSwarm {
		t.Fatalf("only 20 unstrung bows: %+v", d)
	}
	// The mule on another world.
	r.ctx.CurrentWorld = 2
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeSwarm || !strings.Contains(d.Why, "another world") {
		t.Fatalf("another world: %+v", d)
	}
}

// ---- the mule ask ----

func TestMasterCrafterAskCountsNotesAndItemsAndReportsRefusals(t *testing.T) {
	r := newMasterRig(t)
	r.ctx.CurrentWorld = 1
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	walked := 0
	oldWalk, oldKit := masterWalkTo, scarabKitBundle
	defer func() { masterWalkTo, scarabKitBundle = oldWalk, oldKit }()
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { walked++; return true }
	var asked []string
	scarabKitBundle = func(d *fleetrunner.Deps, bus *mulebus.Bus, me, mule string, items []fleetplan.ItemCount, opts fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		for _, it := range items {
			asked = append(asked, it.Item)
		}
		if !opts.KeepNotes || opts.Bank == (tile.Tile{}) || opts.PerOrder != bowstringplan.KitMax {
			t.Errorf("kit options %+v: the notes must stay notes (the next bank visit banks them), at the mule's bank", opts)
		}
		return fleetrunner.KitResult{Got: map[string]int{"Chisel": 1}, Declined: map[string]string{"Uncut ruby": "declined: none in the bank"}},
			fleetrunner.KitBundleInfo{Bundled: true, NotedHeld: map[string]int{"Uncut sapphire": 600}}
	}
	d := r.b.deps(r.ctx)
	a := r.b.askMule(r.ctx, d, []mastercraftplan.Line{{Item: "Uncut sapphire", Qty: 600}, {Item: "Uncut ruby", Qty: 5}, {Item: "Chisel", Qty: 1}})
	if walked != 1 || strings.Join(asked, ",") != "Uncut sapphire,Uncut ruby,Chisel" {
		t.Fatalf("walked %d, asked %v", walked, asked)
	}
	if a.Got["Uncut sapphire"] != 600 || a.Got["Chisel"] != 1 || a.Traded != 601 || a.Why != "" {
		t.Fatalf("%+v", a)
	}
	if a.Declined["Uncut ruby"] == "" || a.declinedEmpty() == false {
		t.Fatalf("declined %v empty=%v", a.Declined, a.declinedEmpty())
	}
	// A mule that is not here is not walked to.
	r2 := newMasterRig(t)
	r2.ctx.CurrentWorld = 1
	walked = 0
	a = r2.b.askMule(r2.ctx, r2.b.deps(r2.ctx), []mastercraftplan.Line{{Item: "Chisel", Qty: 1}})
	if walked != 0 || a.Traded != 0 || !strings.Contains(a.Why, "no heartbeat") {
		t.Fatalf("no heartbeat: walked %d, %+v", walked, a)
	}
}

// ---- BowStringer's embed hooks ----

func TestBowStringerEmbedHandsControlBackInsteadOfEndingTheRun(t *testing.T) {
	ctx := NewContext(newAPISession(), "main", nil, nil, nil, nil, nil, nil, nil, nil, func(string) {})
	var reasons []string
	bs := &bowStringerBot{
		set:    bowstringplan.DefaultSettings(),
		said:   map[string]time.Time{},
		strung: map[string]int{},
		embed:  &bowStringerEmbed{set: bowstringplan.DefaultSettings(), ended: func(r string) { reasons = append(reasons, r) }},
	}
	bs.stop(ctx, "no bow left")
	bs.stop(ctx, "again")
	if _, finished := ctx.FinishRequested(); finished || ctx.Stopped() {
		t.Fatal("an embedded round must never finish the run or stop the context")
	}
	if !bs.embedEnded() || len(reasons) != 1 || reasons[0] != "no bow left" {
		t.Fatalf("reasons %v: the round ends once", reasons)
	}
	// A deferred kit ends the round rather than waiting out the back-off at the bank.
	bs.embed.done, reasons = false, nil
	bs.backoff(ctx, "the mule is busy")
	if !bs.embedEnded() || len(reasons) != 1 || !strings.HasPrefix(reasons[0], "deferred: ") {
		t.Fatalf("reasons %v", reasons)
	}
}

// THE REGISTERED SCRIPT IS UNCHANGED: with no embed, stop still finishes the run for good.
func TestBowStringerWithoutEmbedStillFinishesTheRun(t *testing.T) {
	ctx := NewContext(newAPISession(), "bower", nil, nil, nil, nil, nil, nil, nil, nil, func(string) {})
	bs := &bowStringerBot{set: bowstringplan.DefaultSettings(), said: map[string]time.Time{}, strung: map[string]int{}}
	bs.stop(ctx, "no bow left")
	if _, finished := ctx.FinishRequested(); !finished || !ctx.Stopped() {
		t.Fatal("the registered BowStringer must still end the run")
	}
	bs2 := &bowStringerBot{set: bowstringplan.DefaultSettings(), said: map[string]time.Time{}, strung: map[string]int{}}
	bs2.backoff(NewContext(newAPISession(), "bower2", nil, nil, nil, nil, nil, nil, nil, nil, func(string) {}), "x")
	if bs2.nextOrder.IsZero() {
		t.Fatal("the registered backoff must still set the next order time")
	}
}

func TestBowStringerEmbedSettingsAndStock(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{"muleAt": 5.0}}
	set := bowstringplan.DefaultSettings()
	bs := &bowStringerBot{embed: &bowStringerEmbed{set: set}}
	got, err := bs.settingsFor(ctx)
	if err != nil || got.Mule != set.Mule {
		t.Fatalf("the host's settings must replace ctx.Settings: %+v %v", got, err)
	}
	if _, err := (&bowStringerBot{}).settingsFor(ctx); err == nil {
		t.Fatal("the registered script must still decode ctx.Settings strictly")
	}
	// The stock hook adjusts only a published reading.
	ctx.Peers = peerbank.New()
	bs.embed.stock = func(base func(string) (int, bool)) func(string) (int, bool) {
		return func(item string) (int, bool) { n, ok := base(item); return n - 1, ok }
	}
	if f := bs.muleStock(ctx, "mule"); f != nil {
		t.Fatal("no published bank must stay nil")
	}
	ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, []peerbank.Holding{{Item: "Yew longbow", Count: 10}}, nil, false)
	f := bs.muleStock(ctx, "mule")
	if f == nil {
		t.Fatal("published bank not read")
	}
	if n, _ := f("Yew longbow"); n != 9 {
		t.Fatalf("adjusted count %d, want 9", n)
	}
}
