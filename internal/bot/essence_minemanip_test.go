package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
)

// kitSupply is the supply world of an account with mineTickManip on: ReadBank reports the kit lines the pack still lacks after the
// visit drew what the bank had, and the mule answers an ask for them.
type kitSupply struct {
	*fakeSupply
	missing []string
	gives   bool
	why     string
	walks   bool
	asks    [][]string
}

func (k *kitSupply) ReadBank() (supplyBank, bool) {
	b, ok := k.fakeSupply.ReadBank()
	b.KitMissing = k.missing
	return b, ok
}

func (k *kitSupply) AskKit(missing []string) (string, string, bool) {
	k.asks = append(k.asks, append([]string(nil), missing...))
	if k.gives {
		k.missing = nil // it is in the pack now, and the next visit keeps it
		return "Knife and Logs", "", true
	}
	return "", k.why, k.walks
}

func TestTheSupplyLoopAsksTheMuleForTheBatchKitWhenNeitherPackNorBankHasIt(t *testing.T) {
	f := newFakeSupply()
	k := &kitSupply{fakeSupply: f, missing: []string{"Knife", "Logs"}, gives: true}
	st := guildRun(t, map[string]interface{}{"mineTickManip": true})
	if !st.set.MineTickManip {
		t.Fatal("mineTickManip did not load")
	}
	st.step(k, f.logf)
	if len(k.asks) != 1 || len(k.asks[0]) != 2 || k.asks[0][0] != "Knife" || k.asks[0][1] != "Logs" {
		t.Fatalf("asks %v, want one for the Knife and the Logs", k.asks)
	}
	if f.logged("the mule gave Knife and Logs for the mining batch") != 1 {
		t.Errorf("the arrival is not logged: %v", f.log)
	}
	if st.bankKnown {
		t.Error("the bank should be read again: the kit is in the pack and the next visit keeps it")
	}
	// Once it is in the pack nothing more is asked.
	st.step(k, f.logf)
	if len(k.asks) != 1 {
		t.Errorf("%d asks after the kit arrived", len(k.asks))
	}
}

func TestAKitAskThatBroughtNothingIsRepeatedNoSoonerThanHalfAnHour(t *testing.T) {
	f := newFakeSupply()
	k := &kitSupply{fakeSupply: f, missing: []string{"Knife"}, why: "the mule publishes no Knife"}
	st := guildRun(t, map[string]interface{}{"mineTickManip": true})
	for i := 0; i < 4; i++ {
		st.bankKnown = false // every step reads the bank, as a barren stretch makes it
		st.step(k, f.logf)
		f.now = f.now.Add(5 * time.Minute)
	}
	if len(k.asks) != 1 {
		t.Fatalf("%d asks in 20 minutes, want 1", len(k.asks))
	}
	if f.logged("mining plain, asking again in") != 1 {
		t.Errorf("the refusal is not said once: %v", f.log)
	}
	f.now = f.now.Add(supplyKitAskEvery)
	st.bankKnown = false
	st.step(k, f.logf)
	if len(k.asks) != 2 {
		t.Fatalf("%d asks after half an hour more, want 2", len(k.asks))
	}
}

// An ask that walked to the mule's post (about 450 tiles from the Guild) and came back empty is not repeated for hours: the walk is
// the cost, a missing kit costs only the gain.
func TestAKitAskThatWalkedAndFailedWaitsHours(t *testing.T) {
	f := newFakeSupply()
	k := &kitSupply{fakeSupply: f, missing: []string{"Knife", "Logs"}, why: "the mule declined", walks: true}
	st := guildRun(t, map[string]interface{}{"mineTickManip": true})
	st.step(k, f.logf)
	if len(k.asks) != 1 {
		t.Fatalf("%d asks", len(k.asks))
	}
	f.now = f.now.Add(supplyKitAskEvery + time.Minute)
	st.bankKnown = false
	st.step(k, f.logf)
	if len(k.asks) != 1 {
		t.Fatalf("asked again %s after a walk that brought nothing", supplyKitAskEvery)
	}
	f.now = f.now.Add(supplyKitAskAfterWalk)
	st.bankKnown = false
	st.step(k, f.logf)
	if len(k.asks) != 2 {
		t.Fatalf("%d asks after %s, want 2", len(k.asks), supplyKitAskAfterWalk)
	}
	// Each failed ask says how long the next waits: 3h after a walk, both times.
	if f.logged("asking again in 3h0m0s") != 2 {
		t.Errorf("the long wait is not said after each failed ask: %v", f.log)
	}
}

func TestNoKitAskWithoutAMuleAndNoneForAWorldThatCannotAsk(t *testing.T) {
	f := newFakeSupply()
	k := &kitSupply{fakeSupply: f, missing: []string{"Knife", "Logs"}, gives: true}
	st := guildRun(t, map[string]interface{}{"mineTickManip": true, "mule": ""})
	st.step(k, f.logf)
	if len(k.asks) != 0 {
		t.Fatalf("asked a mule that is not set: %v", k.asks)
	}
	if f.logged("no mule is set to ask") != 1 {
		t.Errorf("the missing mule is not said: %v", f.log)
	}
	// A world that does not implement the ask (the fake of every other test) is left alone.
	g := newFakeSupply()
	st2 := guildRun(t, map[string]interface{}{"mineTickManip": true})
	plain := &struct{ *fakeSupply }{g}
	st2.askKit(plain, g.logf, []string{"Knife"})
	if g.logged("for the mining batch") != 0 {
		t.Errorf("a world without the ask logged: %v", g.log)
	}
}

func TestMineTickManipIsOffByDefaultAndStrictlyLoaded(t *testing.T) {
	if essenceplan.DefaultSettings().MineTickManip {
		t.Fatal("mineTickManip must be opt-in")
	}
	if _, err := essenceplan.DecodeSettings(map[string]interface{}{"mineTickManip": "perhaps"}); err == nil {
		t.Fatal("a value that is not true or false must be an error")
	}
	s, err := essenceplan.DecodeSettings(map[string]interface{}{"gateway": "guild", "mineTickManip": "true"})
	if err != nil || !s.MineTickManip {
		t.Fatalf("the panel form's \"true\": %v %+v", err, s)
	}
}

// kitHeldSupply is the world of an account whose pack holds the whole kit.
type kitHeldSupply struct{ *fakeSupply }

func (k *kitHeldSupply) ReadBank() (supplyBank, bool) {
	b, ok := k.fakeSupply.ReadBank()
	b.Kit = true
	return b, ok
}

// An account that has lost its pickaxe (the lost-pickaxe event leaves a handle) and that the mule cannot supply buys one at
// Nurmof's shop even with the batch kit held: the kit skip is for upgrades, and without a pickaxe nothing is mined at all.
func TestAMinerWithNoPickaxeBuysOneWhileTheBatchHasItsKit(t *testing.T) {
	f := newFakeSupply()
	f.noPick, f.askWhy, f.coins, f.level = true, "the mule publishes no pickaxe this account can use at Mining 61", 117_080, 61
	f.shelf = map[string]bool{"Rune pickaxe": true, "Bronze pickaxe": true}
	st := guildRun(t, map[string]interface{}{"buyPickaxe": true, "mineTickManip": true})
	st.checkedPick = "Rune pickaxe" // the tier was looked at while the old pickaxe was still owned
	st.step(&kitHeldSupply{f}, f.logf)
	if f.asked != 1 || len(f.trips) != 1 || f.trips[0][0].Item != "Bronze pickaxe" || f.logged("PICKAXE bought - a Bronze pickaxe") != 1 {
		t.Fatalf("asked %d, trips %+v: %v", f.asked, f.trips, f.log)
	}
	if f.logged("pickaxe upgrades are skipped") != 0 {
		t.Errorf("the kit skip was applied to an account with no pickaxe: %v", f.log)
	}

	// buyPickaxe off: the account is only told.
	g := newFakeSupply()
	g.noPick, g.askWhy, g.coins, g.level = true, "none published", 117_080, 61
	g.shelf = map[string]bool{"Rune pickaxe": true}
	st2 := guildRun(t, map[string]interface{}{"mineTickManip": true})
	st2.step(&kitHeldSupply{g}, g.logf)
	if len(g.trips) != 0 || g.logged("put one in the bank") != 1 {
		t.Fatalf("trips %+v: %v", g.trips, g.log)
	}
}

// The trip carries the dearest offer's coins and the fares, so a 1-coin bronze pickaxe trip can still pay its ships.
func TestAPickaxeTripCarriesItsFares(t *testing.T) {
	offers := []essenceplan.UpgradeOffer{{Item: "Bronze pickaxe", Carry: 1}, {Item: "Iron pickaxe", Carry: 175}}
	if got := pickaxeTripCoins(offers); got != 175+essenceplan.NurmofFareCoins {
		t.Fatalf("carries %d", got)
	}
	if essenceplan.NurmofFareCoins < 200 {
		t.Fatalf("NurmofFareCoins %d does not pay two ships each way", essenceplan.NurmofFareCoins)
	}
}

// With the batch every pickaxe rolls every 3 ticks, so the 25-minute trip to Nurmof buys nothing; without the kit the upgrade is
// wanted again.
func TestNoPickaxeTripWhileTheMiningBatchHasItsKit(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 40_000, 41
	f.shelf = map[string]bool{"Rune pickaxe": true}
	k := &kitHeldSupply{f}
	st := guildRun(t, map[string]interface{}{"buyPickaxe": true, "mineTickManip": true})
	st.step(k, f.logf)
	st.step(k, f.logf)
	if len(f.trips) != 0 {
		t.Fatalf("a trip with the kit held: %+v", f.trips)
	}
	if f.logged("pickaxe upgrades are skipped while the mining batch has its kit") != 1 {
		t.Errorf("the skip is not said once: %v", f.log)
	}

	// The kit gone (a leaked carrier, a bank with none): the trip is made.
	f2 := newFakeSupply()
	f2.owned, f2.coins, f2.level = []string{"Bronze pickaxe"}, 40_000, 41
	f2.shelf = map[string]bool{"Rune pickaxe": true}
	st2 := guildRun(t, map[string]interface{}{"buyPickaxe": true, "mineTickManip": true})
	st2.step(f2, f2.logf)
	if len(f2.trips) != 1 {
		t.Fatalf("no trip without the kit: %+v (log %v)", f2.trips, f2.log)
	}

	// And with the manipulation off the kit flag means nothing.
	f3 := newFakeSupply()
	f3.owned, f3.coins, f3.level = []string{"Bronze pickaxe"}, 40_000, 41
	f3.shelf = map[string]bool{"Rune pickaxe": true}
	st3 := guildRun(t, map[string]interface{}{"buyPickaxe": true})
	st3.step(&kitHeldSupply{f3}, f3.logf)
	if len(f3.trips) != 1 {
		t.Fatalf("buyPickaxe alone stopped shopping: %+v", f3.trips)
	}
}
