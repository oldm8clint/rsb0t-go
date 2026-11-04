package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// fakeSupply is a simulated world for the supply loop: a bank, a mule and a clock that only the loop's own
// actions and pauses move.
type fakeSupply struct {
	now     time.Time
	stopped bool

	eligible    bool
	eligibleWhy string
	inMine      bool

	mule      int
	muleKnown bool
	bank      int
	readFails int
	// noPick says no usable pickaxe is held; askItem and askWhy are what AskPickaxe answers; asked counts the asks.
	noPick  bool
	owned   []string
	coins   int
	level   int
	shelf   map[string]bool
	tripWhy string
	trips   [][]essenceplan.UpgradeOffer
	askItem string
	askWhy  string
	asked   int
	// mineBanks is how many essence a stretch banks per call (0: the goal); mineFail turns a stretch into a failure.
	mineBanks int
	mineRes   func(goal int) runecraftrunner.StretchResult
	// handoffFail makes the next n hand-overs fail.
	handoffFail int

	pauses   []time.Duration
	reads    int
	goals    []int
	sends    []int
	handoffs int
	log      []string
}

func newFakeSupply() *fakeSupply {
	return &fakeSupply{now: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), eligible: true, muleKnown: true}
}

func (f *fakeSupply) Now() time.Time { return f.now }
func (f *fakeSupply) Stopped() bool  { return f.stopped }
func (f *fakeSupply) Pause(d time.Duration) bool {
	f.pauses = append(f.pauses, d)
	f.now = f.now.Add(d)
	return !f.stopped
}
func (f *fakeSupply) Eligible() (bool, string) { return f.eligible, f.eligibleWhy }
func (f *fakeSupply) InMine() bool             { return f.inMine }
func (f *fakeSupply) MuleEssence() (int, bool) { return f.mule, f.muleKnown }
func (f *fakeSupply) ReadBank() (supplyBank, bool) {
	f.reads++
	if f.readFails > 0 {
		f.readFails--
		return supplyBank{}, false
	}
	return supplyBank{Essence: f.bank, Pick: !f.noPick, Owned: f.owned, Coins: f.coins}, true
}

func (f *fakeSupply) MiningLevel() int { return f.level }

func (f *fakeSupply) BuyPickaxe(offers []essenceplan.UpgradeOffer) (string, string) {
	f.trips = append(f.trips, offers)
	f.now = f.now.Add(25 * time.Minute) // the walk to Falador, the shop, the ladder, the walk back
	for _, o := range offers {
		if f.shelf[o.Item] {
			f.owned = append(f.owned, o.Item)
			f.coins -= o.Cost
			return o.Item, ""
		}
	}
	return "", f.tripWhy
}

func (f *fakeSupply) AskPickaxe() (string, string) {
	f.asked++
	if f.askItem != "" {
		f.noPick = false
	}
	return f.askItem, f.askWhy
}

func (f *fakeSupply) Mine(goal int) runecraftrunner.StretchResult {
	f.goals = append(f.goals, goal)
	if f.mineRes != nil {
		return f.mineRes(goal)
	}
	n := goal
	if f.mineBanks > 0 {
		n = f.mineBanks
	}
	// Bronze: 261 ticks of 600 ms a load.
	took := time.Duration(n/essenceplan.LoadSize) * 261 * 600 * time.Millisecond
	f.now = f.now.Add(took)
	f.bank += n
	return runecraftrunner.StretchResult{Banked: n, BankEssence: f.bank, Loads: n / essenceplan.LoadSize, Teleports: n / essenceplan.LoadSize,
		Done: n >= goal, Reason: "goal reached", Took: took}
}

func (f *fakeSupply) Handoff(send int) supplyHandoff {
	f.sends = append(f.sends, send)
	f.now = f.now.Add(8 * time.Minute) // the walk to the post, the trade, the bank
	if f.handoffFail > 0 {
		f.handoffFail--
		return supplyHandoff{Drawn: send, BankKnown: true, BankEssence: f.bank, Why: "the mule did not trade"}
	}
	n := min(send, f.bank)
	f.bank -= n
	f.mule += n
	f.handoffs++
	return supplyHandoff{Drawn: n, Crossed: n, Traded: true, BankKnown: true, BankEssence: f.bank}
}

func (f *fakeSupply) logf(format string, args ...any) {
	f.log = append(f.log, fmt.Sprintf(format, args...))
}

func (f *fakeSupply) logged(sub string) int {
	n := 0
	for _, l := range f.log {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func guildRun(t *testing.T, raw map[string]interface{}) *supplyRun {
	t.Helper()
	base := map[string]interface{}{"gateway": "guild", "mule": "themule"}
	for k, v := range raw {
		base[k] = v
	}
	set, err := essenceplan.DecodeSettings(base)
	if err != nil {
		t.Fatal(err)
	}
	return newSupplyRun(set, time.Now())
}

func runSteps(st *supplyRun, f *fakeSupply, n int) {
	for i := 0; i < n && !f.stopped; i++ {
		st.step(f, f.logf)
	}
}

// The loop reads the bank once, mines stretches toward muleAt (the last one just the loads short of it), hands over
// the WHOLE banked stack the moment muleAt is reached, and goes on mining.
func TestSupplyLoopMinesThenHandsOverThenMinesOn(t *testing.T) {
	f := newFakeSupply()
	st := guildRun(t, nil) // muleAt 5,000, batch 270
	runSteps(st, f, 1)
	if f.reads != 1 || !st.bankKnown || st.bankEssence != 0 {
		t.Fatalf("first step reads the bank: reads %d known %v bank %d", f.reads, st.bankKnown, st.bankEssence)
	}
	for f.handoffs == 0 && len(f.goals) < 100 {
		runSteps(st, f, 1)
	}
	if f.handoffs != 1 || len(f.sends) != 1 || f.sends[0] != f.mule || f.sends[0] < 5000 || f.sends[0] > 5000+27 {
		t.Fatalf("one hand-over of the whole stack: sends %v mule %d handoffs %d", f.sends, f.mule, f.handoffs)
	}
	// 18 full batches of 270 = 4,860, then the 140 short of 5,000 as 6 loads... = 162 (rounded up to whole loads).
	for i, g := range f.goals {
		if g%essenceplan.LoadSize != 0 || g > 270 || g < 27 {
			t.Fatalf("stretch %d asked for %d", i, g)
		}
	}
	total := 0
	for _, g := range f.goals {
		total += g
	}
	if total != f.mule || total < 5000 {
		t.Fatalf("mined %d, mule holds %d", total, f.mule)
	}
	if st.handoffs != 1 || st.delivered != f.mule || f.bank != 0 {
		t.Fatalf("run %d handoffs, %d delivered; bank %d", st.handoffs, st.delivered, f.bank)
	}
	// It mines on after the hand-over and hands over again at the next 5,000.
	for f.handoffs < 2 && len(f.goals) < 200 {
		runSteps(st, f, 1)
	}
	if f.handoffs != 2 || f.logged("HANDOVER #2") != 1 || f.logged("HANDOFF due") != 2 {
		t.Fatalf("second hand-over: handoffs %d, log %v", f.handoffs, f.log)
	}
	if f.logged("stretch over") == 0 || f.logged("mining 270 essence at the guild gateway") == 0 {
		t.Errorf("the live lines are missing: %v", f.log[:min(6, len(f.log))])
	}
}

// muleStopAt: with the mule's own published stock near the cap the loop mines to it, hands over the last of it
// (only the room the cap leaves), and then holds - mining nothing - until the mule uses some.
func TestSupplyLoopStopsAtTheMuleCap(t *testing.T) {
	f := newFakeSupply()
	f.mule = 149_200
	st := guildRun(t, map[string]interface{}{"muleAt": 5000})
	runSteps(st, f, 40)
	if f.mule > 150_000 {
		t.Fatalf("the mule holds %d, over the 150,000 cap", f.mule)
	}
	if f.handoffs == 0 {
		t.Fatalf("the last 800 should have gone over: %v", f.log)
	}
	minedBefore := len(f.goals)
	runSteps(st, f, 20)
	if len(f.goals) != minedBefore || f.logged("HOLDING") == 0 {
		t.Fatalf("at the cap it holds: %d stretches then %d, log %v", minedBefore, len(f.goals), f.log)
	}
	// The mule uses 30,000 of it: mining resumes.
	f.mule -= 30_000
	runSteps(st, f, 1)
	if len(f.goals) != minedBefore+1 {
		t.Fatalf("the mule used some and the miner must resume: %d stretches", len(f.goals))
	}
}

// A hold is said once a quarter hour, not every minute.
func TestSupplyHoldIsLoggedOncePerQuarterHour(t *testing.T) {
	f := newFakeSupply()
	f.mule = 150_000
	f.bank = 0
	st := guildRun(t, nil)
	runSteps(st, f, 1) // read
	for i := 0; i < 30; i++ {
		runSteps(st, f, 1)
	}
	// 30 minutes of 60 s holds: the first at once and one more at 15 minutes (and 30).
	if n := f.logged("HOLDING"); n < 2 || n > 3 {
		t.Fatalf("%d HOLDING lines in 30 minutes: %v", n, f.log)
	}
	if len(f.goals) != 0 || len(f.sends) != 0 {
		t.Fatalf("a full mule gets nothing: %v %v", f.goals, f.sends)
	}
}

// The mule's bank not published: the miner's own deliveries stand in for its stock, so the cap still holds.
func TestSupplyLoopCountsOwnDeliveriesWhenTheMuleDoesNotPublish(t *testing.T) {
	f := newFakeSupply()
	f.muleKnown = false
	st := guildRun(t, map[string]interface{}{"muleAt": 1000, "muleStopAt": 3000})
	runSteps(st, f, 80)
	if st.delivered < 3000 || st.delivered > 3000+27 {
		t.Fatalf("delivered %d against a 3,000 cap: %v", st.delivered, f.log)
	}
	goals := len(f.goals)
	runSteps(st, f, 10)
	if len(f.goals) != goals || f.logged("this run's deliveries") == 0 {
		t.Fatalf("it must hold once its own deliveries reach the cap: %v", f.log)
	}
}

// A hand-over the mule did not take is not retried for HandoffRetry; mining goes on in between, and the retry comes.
func TestSupplyFailedHandoffWaitsOutItsRetry(t *testing.T) {
	f := newFakeSupply()
	f.bank = 5200
	f.handoffFail = 1
	st := guildRun(t, nil)
	runSteps(st, f, 2) // read, hand-over (fails: it took 8 minutes of the 5)
	if len(f.sends) != 1 || f.handoffs != 0 || f.logged("hand-over failed") != 1 {
		t.Fatalf("sends %v log %v", f.sends, f.log)
	}
	if !st.nextHandoff.After(f.now.Add(-time.Second)) && !st.nextHandoff.After(f.now.Add(-10*time.Minute)) {
		t.Fatalf("no retry wait set: %v", st.nextHandoff)
	}
	// The next steps mine (the wait runs) and, once it has passed, the stack goes over.
	for i := 0; i < 20 && f.handoffs == 0; i++ {
		runSteps(st, f, 1)
	}
	if f.handoffs != 1 || len(f.sends) != 2 {
		t.Fatalf("the retry never came: sends %v handoffs %d", f.sends, f.handoffs)
	}
}

// An account that cannot use its gateway (Magic 65, Rune Mysteries open) mines nothing, says why once, and waits.
func TestSupplyIneligibleAccountWaitsAndWarnsOnce(t *testing.T) {
	f := newFakeSupply()
	f.eligible, f.eligibleWhy = false, "Wizard Distentor is behind the Wizards' Guild door, which needs Magic 66 (have 65)"
	st := guildRun(t, nil)
	runSteps(st, f, 15)
	if len(f.goals) != 0 || f.reads != 0 || len(f.sends) != 0 {
		t.Fatalf("nothing may be done: goals %v reads %d", f.goals, f.reads)
	}
	if n := f.logged("cannot use the guild gateway"); n != 2 { // 15 pauses of 30 s = 7.5 min: at 0 and at 5
		t.Fatalf("%d warnings: %v", n, f.log)
	}
	if !strings.Contains(f.log[0], "Magic 66") {
		t.Errorf("the reason: %q", f.log[0])
	}
	// It becomes eligible and goes to work.
	f.eligible = true
	runSteps(st, f, 2)
	if f.reads != 1 || len(f.goals) != 1 {
		t.Fatalf("reads %d goals %v", f.reads, f.goals)
	}
}

// A gateway that refused the teleport (Rune Mysteries) puts the miner to sleep for a while instead of mining at it again.
func TestSupplyRefusedGatewayBacksOff(t *testing.T) {
	f := newFakeSupply()
	f.mineRes = func(goal int) runecraftrunner.StretchResult {
		return runecraftrunner.StretchResult{Refused: true, Fails: 1, BankEssence: -1, Reason: "Wizard Distentor refused: Rune Mysteries is not complete", Took: time.Second}
	}
	st := guildRun(t, nil)
	runSteps(st, f, 2) // read, mine (refused)
	if len(f.goals) != 1 || f.logged("refused the teleport") != 1 {
		t.Fatalf("goals %v log %v", f.goals, f.log)
	}
	runSteps(st, f, 1) // the sleep
	if len(f.goals) != 1 || len(f.pauses) == 0 || f.pauses[len(f.pauses)-1] > supplyRefusedPause {
		t.Fatalf("a refused gateway is not mined at again at once: goals %v pauses %v", f.goals, f.pauses)
	}
	// Five minutes on it tries again (the quest may have been finished meanwhile).
	runSteps(st, f, 1)
	if len(f.goals) != 2 {
		t.Fatalf("goals %v", f.goals)
	}
}

// A stretch that banks nothing waits twice as long each time, to ten minutes, and one that banks something starts the
// count again.
func TestSupplyBarrenStretchesBackOff(t *testing.T) {
	f := newFakeSupply()
	barren := true
	f.mineRes = func(goal int) runecraftrunner.StretchResult {
		if barren {
			return runecraftrunner.StretchResult{Fails: 4, BankEssence: -1, Reason: "4 failures in a row", Took: time.Minute}
		}
		f.bank += goal
		return runecraftrunner.StretchResult{Banked: goal, BankEssence: f.bank, Loads: goal / 27, Done: true, Took: time.Minute}
	}
	st := guildRun(t, nil)
	runSteps(st, f, 1) // read
	f.pauses = nil
	// Each barren stretch makes the next step read the bank again (a pickaxe may be gone): 8 stretches, 8 pauses.
	for len(f.goals) < 8 {
		runSteps(st, f, 1)
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	if len(f.pauses) != len(want) {
		t.Fatalf("pauses %v", f.pauses)
	}
	for i := range want {
		if f.pauses[i] != want[i] {
			t.Errorf("pause %d = %s, want %s", i, f.pauses[i], want[i])
		}
	}
	barren = false
	runSteps(st, f, 2) // the re-read of the bank, then a stretch that banks
	if st.barren != 0 {
		t.Errorf("a stretch that banked resets the count: %d", st.barren)
	}
	if f.logged("banked nothing") == 0 {
		t.Errorf("log %v", f.log)
	}
}

// A bank that will not open is retried, not mined around.
func TestSupplyBankReadFailureRetries(t *testing.T) {
	f := newFakeSupply()
	f.readFails = 2
	st := guildRun(t, nil)
	runSteps(st, f, 2)
	if st.bankKnown || len(f.goals) != 0 || f.pauses[0] != supplyReadRetry {
		t.Fatalf("known %v goals %v pauses %v", st.bankKnown, f.goals, f.pauses)
	}
	runSteps(st, f, 2)
	if !st.bankKnown || len(f.goals) != 1 {
		t.Fatalf("known %v goals %v", st.bankKnown, f.goals)
	}
}

// A hand-over asked for from the control panel goes with what is banked, whatever muleAt says; muleAt can be moved live.
func TestSupplyManualHandoffAndLiveMuleAt(t *testing.T) {
	f := newFakeSupply()
	f.bank = 600
	st := guildRun(t, nil)
	runSteps(st, f, 1) // read
	st.manual = true
	runSteps(st, f, 1)
	if len(f.sends) != 1 || f.sends[0] != 600 || st.manual {
		t.Fatalf("sends %v manual %v", f.sends, st.manual)
	}
	// muleAt lowered to 300 live: 400 banked goes at once.
	f.bank = 400
	st.bankEssence = 400
	st.muleAt = 300
	runSteps(st, f, 1)
	if len(f.sends) != 2 || f.sends[1] != 400 {
		t.Fatalf("sends %v", f.sends)
	}
}

// A stopped bot does nothing at all.
func TestSupplyStoppedDoesNothing(t *testing.T) {
	f := newFakeSupply()
	f.stopped = true
	st := guildRun(t, nil)
	st.step(f, f.logf)
	if f.reads != 0 || len(f.goals) != 0 || len(f.pauses) != 0 {
		t.Fatalf("reads %d goals %v pauses %v", f.reads, f.goals, f.pauses)
	}
}

// A hand-over that crossed nothing and a trade that ended halfway are both failures with a retry wait.
func TestSupplyHandoffThatCrossedNothingIsAFailure(t *testing.T) {
	f := newFakeSupply()
	f.bank = 5000
	st := guildRun(t, nil)
	runSteps(st, f, 1)
	f.handoffFail = 5
	runSteps(st, f, 1)
	if st.handoffs != 0 || st.delivered != 0 || st.nextHandoff.IsZero() {
		t.Fatalf("%+v", st)
	}
}

func TestSupplyBanner(t *testing.T) {
	set, err := essenceplan.DecodeSettings(map[string]interface{}{"gateway": "guild", "mule": "m", "pickaxe": "Rune"})
	if err != nil {
		t.Fatal(err)
	}
	b := supplyBanner(set, "m")
	for _, want := range []string{"guild gateway", "Wizard Distentor", "Yanille", "Magic 66", "270", "5,000", "150,000", "one note stack", "rune pickaxe"} {
		if !strings.Contains(b, want) {
			t.Errorf("banner %q lacks %q", b, want)
		}
	}
	if !strings.Contains(b, "pickaxe upgrades off") {
		t.Errorf("banner %q", b)
	}
	set.BuyPickaxe = true
	if on := supplyBanner(set, "m"); !strings.Contains(on, "buyPickaxe ON") {
		t.Errorf("banner %q", on)
	}
	crom, err := essenceplan.DecodeSettings(map[string]interface{}{"gateway": "cromperty"})
	if err != nil {
		t.Fatal(err)
	}
	if cb := supplyBanner(crom, ""); !strings.Contains(cb, "East Ardougne south") || !strings.Contains(cb, "no Magic level needed") {
		t.Errorf("banner %q", cb)
	}
	noMule := supplyBanner(essenceplan.Settings{Gateway: "guild", Pickaxe: essenceplan.BestAvailable, Batch: 270}.Normalised(), "")
	if !strings.Contains(noMule, "nothing is handed over") || !strings.Contains(noMule, "bronze") {
		t.Errorf("%q", noMule)
	}
}

// An account with no pickaxe anywhere asks the mule for one - once, then again no sooner than half an hour - and mines
// with it; one the mule cannot supply is told to put one in the bank, loudly but not every step.
func TestSupplyAsksTheMuleForAPickaxeWhenItHasNone(t *testing.T) {
	f := newFakeSupply()
	f.noPick, f.askItem = true, "Bronze pickaxe"
	st := guildRun(t, nil)
	runSteps(st, f, 2) // read (no pickaxe) and the ask; the next step reads again
	if f.asked != 1 || f.logged("the mule gave a Bronze pickaxe") != 1 {
		t.Fatalf("asked %d: %v", f.asked, f.log)
	}
	runSteps(st, f, 3)
	if f.asked != 1 || len(f.goals) == 0 {
		t.Fatalf("with a pickaxe it mines: asked %d goals %v", f.asked, f.goals)
	}

	f = newFakeSupply()
	f.noPick, f.askWhy = true, "the mule publishes no pickaxe this account can use at Mining 1"
	st = guildRun(t, nil)
	runSteps(st, f, 1)
	if f.asked != 1 || f.logged("put one in the bank") != 1 || f.logged("the mule publishes no pickaxe") != 1 {
		t.Fatalf("asked %d: %v", f.asked, f.log)
	}
	// It does not ask again within half an hour, and does after it.
	st.bankKnown = false
	runSteps(st, f, 1)
	if f.asked != 1 {
		t.Fatalf("asked %d times inside half an hour", f.asked)
	}
	f.now = f.now.Add(supplyPickAskEvery)
	st.bankKnown = false
	runSteps(st, f, 1)
	if f.asked != 2 {
		t.Fatalf("asked %d times after half an hour", f.asked)
	}
}

// No mule set: nothing is asked and the account is told.
func TestSupplyPickaxeNeedsAMuleToAsk(t *testing.T) {
	f := newFakeSupply()
	f.noPick = true
	set, err := essenceplan.DecodeSettings(map[string]interface{}{"gateway": "guild"})
	if err != nil {
		t.Fatal(err)
	}
	st := newSupplyRun(set, time.Now())
	runSteps(st, f, 1)
	if f.asked != 0 || f.logged("no mule is set to ask") != 1 {
		t.Fatalf("asked %d: %v", f.asked, f.log)
	}
}

// The original (Aubury) loop's share of the stop: it holds before teleporting in while the mule's published bank has
// the cap, and not otherwise; and it meets the mule where muleAtTile says, else at the Varrock east bank.
func TestTheOriginalLoopHoldsAtTheCapAndMeetsWhereItIsTold(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Rune essence": 150_000})
	b := &essenceBot{settings: essenceplan.DefaultSettings()}
	b.settings.Mule = "mule"
	if n, reached := b.atCap(r.ctx); !reached || n != 150_000 {
		t.Fatalf("at the cap: %d %v", n, reached)
	}
	b.settings.MuleStopAt = 200_000
	if n, reached := b.atCap(r.ctx); reached || n != 150_000 {
		t.Fatalf("under the cap: %d %v", n, reached)
	}
	b.settings.MuleStopAt = 0
	if _, reached := b.atCap(r.ctx); reached {
		t.Fatal("muleStopAt 0 never holds")
	}
	b.settings.MuleStopAt, b.settings.Mule = 100, "somebody-else"
	if _, reached := b.atCap(r.ctx); reached {
		t.Fatal("a mule that has not published never holds")
	}
	if got := b.legacyStand(); got != essenceplan.BankStand {
		t.Errorf("stand %v", got)
	}
	b.settings.MuleAtTile = "2809,3441"
	if got := b.legacyStand(); got.X != 2809 || got.Z != 3441 {
		t.Errorf("stand %v", got)
	}
	if b.handoffWhat() != "the whole bank" {
		t.Errorf("%q", b.handoffWhat())
	}
	b.settings.MuleItems = essenceplan.MuleItemsEssence
	if b.handoffWhat() != "one note stack of Rune essence" {
		t.Errorf("%q", b.handoffWhat())
	}
}

func upgradeRun(t *testing.T) *supplyRun {
	t.Helper()
	return guildRun(t, map[string]interface{}{"buyPickaxe": true})
}

// buyPickaxe off (the default): the shop is never visited, whatever the level and the coins.
func TestSupplyNeverShopsUnlessAskedTo(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 90_000, 45
	st := guildRun(t, nil)
	runSteps(st, f, 4)
	if len(f.trips) != 0 || f.logged("PICKAXE") != 0 {
		t.Fatalf("trips %v log %v", f.trips, f.log)
	}
}

// With buyPickaxe on, the level and the coins make ONE trip for the best tier; the tier is then not looked at again
// until Mining moves it.
func TestSupplyBuysTheBestPickaxeTheLevelAndCoinsAllow(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 40_000, 41
	f.shelf = map[string]bool{"Rune pickaxe": true}
	st := upgradeRun(t)
	runSteps(st, f, 1) // read, and the trip
	if len(f.trips) != 1 || f.trips[0][0].Item != "Rune pickaxe" || f.trips[0][0].Carry != 40_000 {
		t.Fatalf("trips %+v", f.trips)
	}
	if st.pickBought != 1 || st.checkedPick != "Rune pickaxe" || f.logged("PICKAXE bought - a Rune pickaxe") != 1 || f.logged("trying rune pickaxe, then adamant pickaxe") != 1 {
		t.Fatalf("log %v checked %q", f.log, st.checkedPick)
	}
	// It mines on, reads the bank once more (the pack and bank changed), and never goes back for the same tier.
	runSteps(st, f, 8)
	if len(f.trips) != 1 {
		t.Fatalf("a second trip: %+v", f.trips)
	}
}

// A shelf that is out falls to the next tier down; a lower tier bought means the best is looked for again later.
func TestSupplyFallsToTheNextTierWhenTheShelfIsOut(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 80_000, 41
	f.shelf = map[string]bool{"Adamant pickaxe": true}
	st := upgradeRun(t)
	runSteps(st, f, 1)
	if st.pickBought != 1 || st.checkedPick != "" || !st.upgradeNext.After(f.now) {
		t.Fatalf("bought %d checked %q next %v now %v", st.pickBought, st.checkedPick, st.upgradeNext, f.now)
	}
	// The wait is half an hour from the trip; inside it nothing is tried, after it the rune pickaxe is.
	runSteps(st, f, 4)
	if len(f.trips) != 1 {
		t.Fatalf("trips inside the wait: %d", len(f.trips))
	}
	f.now = f.now.Add(essenceplan.UpgradeRetry)
	f.shelf["Rune pickaxe"] = true
	st.bankKnown = false
	runSteps(st, f, 1)
	if len(f.trips) != 2 || st.checkedPick != "Rune pickaxe" {
		t.Fatalf("trips %d checked %q", len(f.trips), st.checkedPick)
	}
}

// A trip that finds nothing waits half an hour, and says so once.
func TestSupplyFailedPickaxeTripWaits(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 40_000, 41
	f.tripWhy = "nothing was bought (tried Rune pickaxe, Adamant pickaxe)"
	st := upgradeRun(t)
	runSteps(st, f, 1)
	if len(f.trips) != 1 || f.logged("the pickaxe trip bought nothing") != 1 || st.pickBought != 0 {
		t.Fatalf("log %v", f.log)
	}
	runSteps(st, f, 3)
	if len(f.trips) != 1 {
		t.Fatalf("a trip inside the wait")
	}
}

// Poor, or already holding the best the level allows: no trip, and not looked at again at that level.
func TestSupplyDoesNotShopWhenThereIsNothingToBuy(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 100, 41
	st := upgradeRun(t)
	runSteps(st, f, 1)
	if len(f.trips) != 0 || st.checkedPick != "Rune pickaxe" || f.logged("no affordable upgrade") != 1 {
		t.Fatalf("log %v checked %q", f.log, st.checkedPick)
	}
	f = newFakeSupply()
	f.owned, f.coins, f.level = []string{"Rune pickaxe"}, 90_000, 45
	st = upgradeRun(t)
	runSteps(st, f, 1)
	if len(f.trips) != 0 || f.logged("already owned") != 1 {
		t.Fatalf("log %v", f.log)
	}
}

// Mining moving the best tier makes the loop read the bank again after a productive stretch: the level-up is noticed
// without waiting for a barren one.
func TestSupplyNoticesALevelThatOpensABetterTier(t *testing.T) {
	f := newFakeSupply()
	f.owned, f.coins, f.level = []string{"Bronze pickaxe"}, 90_000, 1
	f.shelf = map[string]bool{"Mithril pickaxe": true}
	st := upgradeRun(t)
	runSteps(st, f, 1) // read at Mining 1: iron (Mining 1) is affordable, the shelf has none; trip fails
	f.trips = nil
	f.now = f.now.Add(essenceplan.UpgradeRetry)
	st.checkedPick = "Iron pickaxe" // looked at; nothing more to do at this level
	f.tripWhy = ""
	runSteps(st, f, 1) // a stretch (the bank is known)
	if len(f.trips) != 0 {
		t.Fatalf("trips %v", f.trips)
	}
	f.level = 21 // the stretch's experience
	runSteps(st, f, 2)
	if len(f.trips) != 1 || f.trips[0][0].Item != "Mithril pickaxe" {
		t.Fatalf("trips %+v log %v", f.trips, f.log)
	}
}

// A start inside the mine (a restart mid-stretch): no walk reaches the bank from there, so the loop mines a load, comes out
// through the stretch and reads the bank from the Yanille side - it does not retry a bank it cannot reach.
func TestSupplyStartedInsideTheMineFinishesALoadFirst(t *testing.T) {
	f := newFakeSupply()
	f.inMine = true
	st := guildRun(t, nil)
	runSteps(st, f, 1)
	if f.reads != 0 || len(f.goals) != 1 || f.goals[0] != essenceplan.LoadSize || !st.bankKnown {
		t.Fatalf("reads %d goals %v known %v: %v", f.reads, f.goals, st.bankKnown, f.log)
	}
	if f.logged("starts inside the essence mine") != 1 {
		t.Errorf("log %v", f.log)
	}
	// Out of the mine, the bank is known from the stretch and nothing is read.
	f.inMine = false
	runSteps(st, f, 1)
	if f.reads != 0 {
		t.Errorf("reads %d", f.reads)
	}
}

// A hand-over asked for with nothing banked is dropped, not kept waiting for the next load to turn up.
func TestSupplyManualHandoffWithNothingBankedIsDropped(t *testing.T) {
	f := newFakeSupply()
	st := guildRun(t, nil)
	runSteps(st, f, 1) // read: 0 banked
	st.manual = true
	runSteps(st, f, 1)
	if st.manual || len(f.sends) != 0 || f.logged("nothing to hand over") != 1 {
		t.Fatalf("manual %v sends %v log %v", st.manual, f.sends, f.log)
	}
	runSteps(st, f, 3)
	if len(f.sends) != 0 {
		t.Fatalf("a dropped request came back: %v", f.sends)
	}
}

// noMuleRun is a Guild miner with no mule named (or a threshold of 0 with one), 6,000 essence already in its bank - more than
// the default muleAt of 5,000, the case that used to draw the stack and carry it to a player named "".
func noMuleRun(t *testing.T, raw map[string]interface{}) (*supplyRun, *fakeSupply) {
	t.Helper()
	base := map[string]interface{}{"gateway": "guild"}
	for k, v := range raw {
		base[k] = v
	}
	set, err := essenceplan.DecodeSettings(base)
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeSupply()
	f.bank = 6000
	return newSupplyRun(set, time.Now()), f
}

// With no mule set, nothing is ever drawn, walked or traded: not at muleAt (5,000 by default at a Guild miner), not at the cap,
// not for a hand-over asked for from the control panel. Mining goes on, in batches, and holds at the cap with the essence banked.
func TestSupplyBlankMuleNeverHandsOver(t *testing.T) {
	for name, raw := range map[string]map[string]interface{}{
		"blank":      nil,
		"whitespace": {"mule": "   "},
		"empty list": {"mule": " , "},
	} {
		t.Run(name, func(t *testing.T) {
			st, f := noMuleRun(t, raw)
			if st.handoverOff() != "no mule is set" {
				t.Fatalf("handoverOff = %q", st.handoverOff())
			}
			runSteps(st, f, 1) // read: 6,000 banked, over the default muleAt
			runSteps(st, f, 6)
			if len(f.sends) != 0 || f.handoffs != 0 || f.logged("HANDOFF due") != 0 {
				t.Fatalf("a blank mule drew a stack: sends %v log %v", f.sends, f.log)
			}
			if len(f.goals) != 6 {
				t.Fatalf("mining goes on at the bank's pace: goals %v", f.goals)
			}
			for _, g := range f.goals {
				if g != 270 {
					t.Fatalf("a full batch every time (muleAt is not a goal without a mule): goals %v", f.goals)
				}
			}

			// A hand-over asked for from the control panel is dropped, and says why.
			st.manual = true
			runSteps(st, f, 1)
			if st.manual || len(f.sends) != 0 || f.logged("no mule is set - nothing is handed over") != 1 {
				t.Fatalf("manual %v sends %v log %v", st.manual, f.sends, f.log)
			}

			// At the cap (the miner's own bank stands in for the mule's stock) it holds; the stack is never handed over.
			f.bank = 149_900
			st.bankEssence = 149_900
			runSteps(st, f, 1) // a stretch of the 100 left: four loads
			if len(f.goals) != 8 || f.goals[len(f.goals)-1] != 4*essenceplan.LoadSize {
				t.Fatalf("goals %v", f.goals)
			}
			stretches := len(f.goals)
			runSteps(st, f, 10)
			if len(f.goals) != stretches || len(f.sends) != 0 || f.logged("HOLDING") == 0 || f.logged("hand-overs are off") == 0 {
				t.Fatalf("at the cap it holds with the essence in the bank: goals %d->%d sends %v log %v", stretches, len(f.goals), f.sends, f.log)
			}
		})
	}
}

// A mule with muleAt 0 is the same: 0 never hands over, whatever the bank holds or the cap says - until the control panel moves
// muleAt live, which is the original loop's rule too.
func TestSupplyMuleAtZeroNeverHandsOverUntilMovedLive(t *testing.T) {
	st, f := noMuleRun(t, map[string]interface{}{"mule": "themule", "muleAt": 0})
	if st.handoverOff() != "muleAt is 0" {
		t.Fatalf("handoverOff = %q", st.handoverOff())
	}
	runSteps(st, f, 1)
	st.manual = true
	runSteps(st, f, 4)
	if len(f.sends) != 0 || st.manual || f.logged("muleAt is 0 - nothing is handed over") != 1 {
		t.Fatalf("sends %v manual %v log %v", f.sends, st.manual, f.log)
	}
	f.mule, f.bank, st.bankEssence = 149_900, 6000, 6000 // the mule's cap is reached
	runSteps(st, f, 3)
	if len(f.sends) != 0 || f.logged("HOLDING") == 0 {
		t.Fatalf("the cap holds instead of handing over: sends %v log %v", f.sends, f.log)
	}

	// The control panel moves muleAt: the stack goes (the mule has room again).
	f.mule = 0
	st.muleAt = 5000
	if st.handoverOff() != "" {
		t.Fatalf("a live muleAt must switch hand-overs on: %q", st.handoverOff())
	}
	runSteps(st, f, 1)
	if len(f.sends) != 1 || f.sends[0] != 6000 {
		t.Fatalf("sends %v log %v", f.sends, f.log)
	}
}

// The live world refuses a blank mule too: scarabMulePresenceOf answers "heard, at the Catherby post" for a blank name, so
// without its own guard Handoff would draw the stack from the bank and walk it there. The guard returns before touching
// anything (the nil Context here would panic otherwise).
func TestSupplyWorldHandoffRefusesABlankMule(t *testing.T) {
	for _, mule := range []string{"", "  ", ", "} {
		set := essenceplan.Settings{Gateway: "guild", Mule: mule, MuleAt: 5000, Pickaxe: essenceplan.BestAvailable, Batch: 270}.Normalised()
		w := &essenceSupplyWorld{b: &essenceBot{settings: set}}
		r := w.Handoff(6000)
		if r.Why != "no mule is set" || r.Drawn != 0 || r.Crossed != 0 || r.Traded || r.BankKnown {
			t.Errorf("mule %q: %+v", mule, r)
		}
	}
}
