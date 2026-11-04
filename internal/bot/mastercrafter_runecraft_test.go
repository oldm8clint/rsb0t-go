package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// simRune plays the levelling round's world as the live handlers leave it: a bank, a pack, the temples, the
// mule's published stock, a miner. It applies the SAME rules the content does (the table's experience, the level
// curve), so a whole levelling from Runecraft 1 is run through the real stage machine and the real loop.
type simRune struct {
	level, xp, target int
	members           bool

	where       runecraftplan.Region
	temple      string
	atRuins     bool
	atPost      bool
	packEssence int
	packTal     map[string]bool

	bankKnown   bool
	bankEssence int
	bankTal     map[string]int

	muleEssence int
	muleTal     map[string]int
	canMine     bool
	mineWhy     string
	source      runecraftplan.Source
	batch       int

	asked, mineFailed bool
	mined, asked_     int

	// The market lot: a lot is allowed up to buyQty, buys credit the bank, buyFailed is the round's flag.
	canBuy, buyFailed bool
	buyQty, bought    int

	// scripting
	failFirst  map[runecraftplan.Stage]int // fail this many calls of a stage first
	failAlways map[runecraftplan.Stage]bool
	bankNoop   bool // a bank visit that "succeeds" and draws nothing
	stopAfter  int
	expired    bool
	calls      map[runecraftplan.Stage]int
	total      int

	crafted []string // the altar of every craft, in order
	essence int      // essence bound
	log     []string
}

func newSimRune() *simRune {
	return &simRune{level: 1, target: 44, members: true, atPost: true, source: runecraftplan.SourceAuto, batch: 1000,
		packTal: map[string]bool{}, bankTal: map[string]int{}, muleTal: map[string]int{},
		failFirst: map[runecraftplan.Stage]int{}, failAlways: map[runecraftplan.Stage]bool{}, calls: map[runecraftplan.Stage]int{}}
}

func (s *simRune) Read() runecraftplan.Snapshot {
	snap := runecraftplan.Snapshot{
		Level: s.level, XP: s.xp, Target: s.target, Members: s.members, Where: s.where, Temple: s.temple, AtRuins: s.atRuins, AtPost: s.atPost,
		PackEssence: s.packEssence, PackTalisman: s.packTal,
		BankKnown: s.bankKnown, BankEssence: s.bankEssence, BankTalisman: s.bankTal,
		MuleKnown: true, MuleEssence: s.muleEssence, MuleTalisman: s.muleTal,
		Source: s.source, CanMine: s.canMine, MineWhy: s.mineWhy, MineFailed: s.mineFailed, Batch: s.batch, Asked: s.asked,
		CanBuy: s.canBuy, BuyQty: s.buyQty, BuyFailed: s.buyFailed,
	}
	return snap
}

func (s *simRune) Fingerprint() string {
	return fmt.Sprintf("%d|%d|%v|%s|%v|%d|%d|%v|%v|%d|%v|%d", s.level, s.xp, s.where, s.temple, s.atRuins, s.packEssence, s.bankEssence, s.asked, s.mineFailed, s.mined, s.buyFailed, s.bought)
}

func (s *simRune) Prepare() bool { return true }

func (s *simRune) Stopped() bool {
	return s.stopAfter > 0 && s.total >= s.stopAfter
}
func (s *simRune) Expired() bool       { return s.expired }
func (s *simRune) Pause(time.Duration) {}

// enter is the common start of every stage: count it and apply the scripted failures.
func (s *simRune) enter(stage runecraftplan.Stage) (runeStepResult, bool) {
	s.total++
	s.calls[stage]++
	if s.failAlways[stage] || s.failFirst[stage] >= s.calls[stage] {
		return runeFail("scripted failure of " + string(stage)), true
	}
	return runeStepResult{}, false
}

func (s *simRune) Bank(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StageBank); failed {
		return r
	}
	s.bankKnown = true
	if s.bankNoop {
		return runeOK()
	}
	a := st.Altar
	// Bank the pack (the talisman of another altar, leftovers), keep this altar's talisman, draw the load.
	for k, held := range s.packTal {
		if held && k != a.Key {
			s.bankTal[k]++
			delete(s.packTal, k)
		}
	}
	s.bankEssence += s.packEssence
	s.packEssence = 0
	if !s.packTal[a.Key] && s.bankTal[a.Key] > 0 {
		s.bankTal[a.Key]--
		s.packTal[a.Key] = true
	}
	draw := min(s.bankEssence, runecraftplan.LoadSize)
	s.bankEssence -= draw
	s.packEssence = draw
	s.atPost = false
	return runeOK()
}

func (s *simRune) AskMule(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StageAskMule); failed {
		return r
	}
	for _, l := range st.Lines {
		switch {
		case l.Item == runecraftplan.EssenceName:
			s.muleEssence -= l.Qty
			s.bankEssence += l.Qty
			s.asked_ += l.Qty
		default:
			for _, a := range runecraftplan.Altars() {
				if a.Talisman == l.Item {
					s.muleTal[a.Key] -= l.Qty
					s.bankTal[a.Key] += l.Qty
				}
			}
		}
	}
	s.asked = true
	s.bankKnown = true
	return runeOK()
}

func (s *simRune) Mine(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StageMine); failed {
		s.mineFailed = true
		return r
	}
	s.bankEssence += st.Goal
	s.mined += st.Goal
	return runeOK()
}

func (s *simRune) Buy(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StageBuy); failed {
		s.buyFailed = true
		return r
	}
	s.bankEssence += st.Goal
	s.bought += st.Goal
	s.canBuy, s.buyQty = false, 0 // the budget is spent
	s.bankKnown = true
	return runeOK()
}

func (s *simRune) ToRuins(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(st.Stage); failed {
		return r
	}
	if !s.packTal[st.Altar.Key] || s.packEssence == 0 {
		return runeFail("nothing to use at the ruins")
	}
	s.where, s.temple, s.atRuins, s.atPost = runecraftplan.RegionTemple, st.Altar.Key, false, false
	return runeOK()
}

func (s *simRune) Craft(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StageCraft); failed {
		return r
	}
	a := st.Altar
	if s.level < a.Level {
		return runeFail("level too low")
	}
	n := s.packEssence
	s.xp += runecraftplan.XPFor(a, n)
	s.level = runecraftplan.LevelForXP(s.xp)
	s.packEssence = 0
	s.essence += n
	s.crafted = append(s.crafted, a.Key)
	return runeOK()
}

func (s *simRune) PortalOut(st runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.StagePortalOut); failed {
		return r
	}
	s.where, s.temple = runecraftplan.RegionOutside, ""
	return runeOK()
}

func (s *simRune) run(maxSteps int) runeLoopResult {
	return runLevelLoop(s, func(f string, a ...any) { s.log = append(s.log, fmt.Sprintf(f, a...)) }, maxSteps)
}

func (s *simRune) rich() *simRune {
	s.bankKnown = true
	s.bankEssence = 9000
	s.bankTal[runecraftplan.KeyAir], s.bankTal[runecraftplan.KeyBody] = 2, 2
	return s
}

// THE WHOLE LEVELLING, Runecraft 1 to 44 on a bank that holds the essence: Air until level 20, Body from there,
// the target reached, and the essence used is the plan's 7,718 within one load.
func TestTheLoopLevelsRunecraftFromOneToFortyFour(t *testing.T) {
	s := newSimRune().rich()
	res := s.run(5000)
	if !res.Done || res.Stuck || res.Transient || s.level < 44 {
		t.Fatalf("%+v, level %d xp %d\n%s", res, s.level, s.xp, strings.Join(s.log, "\n"))
	}
	// Whole loads of 27 overshoot the Air-to-20 switch and the target a little (the plan is 7,718).
	if s.essence < 7718 || s.essence > 7718+100 {
		t.Errorf("%d essence bound, the plan says 7,718 and a little over for whole loads", s.essence)
	}
	// Air first, then Body, switching at level 20 and never back.
	switched := false
	for i, k := range s.crafted {
		if k == runecraftplan.KeyBody {
			switched = true
		}
		if switched && k == runecraftplan.KeyAir {
			t.Fatalf("Air crafted after Body at craft %d", i)
		}
	}
	if !switched || s.crafted[0] != runecraftplan.KeyAir {
		t.Errorf("crafts %v ...", s.crafted[:3])
	}
	if s.calls[runecraftplan.StageAskMule] != 0 || s.calls[runecraftplan.StageMine] != 0 {
		t.Errorf("asked %d, mined %d: the bank had everything", s.calls[runecraftplan.StageAskMule], s.calls[runecraftplan.StageMine])
	}
	// 286 or so trips: a bank, a ruins, a craft and a portal each.
	if c := s.calls[runecraftplan.StageCraft]; c < 280 || c > 300 || s.calls[runecraftplan.StagePortalOut] != c {
		t.Errorf("%d crafts, %d portals", c, s.calls[runecraftplan.StagePortalOut])
	}
	if res.Steps > 1500 {
		t.Errorf("%d steps", res.Steps)
	}
	if !strings.Contains(res.Reason, "44") {
		t.Errorf("reason %q", res.Reason)
	}
}

// The ladder: the bank holds nothing, the mule holds 3,000 essence and the talismans, the account can mine. It
// asks ONCE, trips on the mule's essence, then mines (the mule is asked no more), banking a batch at a time.
func TestTheLoopAsksTheMuleOnceThenMines(t *testing.T) {
	s := newSimRune()
	s.bankKnown = true
	s.muleEssence = 3000
	s.muleTal[runecraftplan.KeyAir], s.muleTal[runecraftplan.KeyBody] = 1541, 677
	s.canMine = true
	res := s.run(8000)
	if !res.Done || s.level < 44 {
		t.Fatalf("%+v level %d\n%s", res, s.level, strings.Join(s.log, "\n"))
	}
	if s.calls[runecraftplan.StageAskMule] != 1 || s.asked_ != 3000 {
		t.Errorf("asked %d time(s) for %d essence", s.calls[runecraftplan.StageAskMule], s.asked_)
	}
	if s.mined < 7718-3000 || s.calls[runecraftplan.StageMine] < 4 {
		t.Errorf("mined %d in %d stretch(es)", s.mined, s.calls[runecraftplan.StageMine])
	}
	if s.bankTal[runecraftplan.KeyAir]+s.bankTal[runecraftplan.KeyBody]+boolInt(s.packTal[runecraftplan.KeyAir])+boolInt(s.packTal[runecraftplan.KeyBody]) != 4 {
		t.Errorf("talismans lost: bank %v pack %v", s.bankTal, s.packTal)
	}
	// The ask came first, before any mining.
	askedAt, minedAt := -1, -1
	for i, l := range s.log {
		if strings.HasPrefix(l, "stage ask-mule") && askedAt < 0 {
			askedAt = i
		}
		if strings.HasPrefix(l, "stage mine") && minedAt < 0 {
			minedAt = i
		}
	}
	if askedAt < 0 || minedAt < 0 || askedAt > minedAt {
		t.Errorf("ask at %d, mine at %d:\n%s", askedAt, minedAt, strings.Join(s.log, "\n"))
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// craftRuneEssence "mine": the mule is not asked even though it has essence.
func TestTheLoopHonoursTheEssenceSource(t *testing.T) {
	s := newSimRune()
	s.bankKnown, s.muleEssence, s.canMine, s.source = true, 5000, true, runecraftplan.SourceMine
	s.bankTal[runecraftplan.KeyAir], s.bankTal[runecraftplan.KeyBody] = 1, 1
	s.target = 20
	res := s.run(3000)
	if !res.Done || s.calls[runecraftplan.StageAskMule] != 0 || s.mined == 0 {
		t.Fatalf("%+v asked %d mined %d", res, s.calls[runecraftplan.StageAskMule], s.mined)
	}
	s2 := newSimRune()
	s2.bankKnown, s2.canMine, s2.source = true, true, runecraftplan.SourceBank
	s2.bankTal[runecraftplan.KeyAir] = 1
	res = s2.run(100)
	if !res.Stuck || !strings.Contains(res.Reason, "no essence source") {
		t.Fatalf("bank only and an empty bank: %+v", res)
	}
}

func TestTheLoopStopsStuckWhenNothingIsLeftToWorkWith(t *testing.T) {
	s := newSimRune()
	s.bankKnown, s.mineWhy = true, "Rune Mysteries is not complete"
	s.bankTal[runecraftplan.KeyAir] = 1
	res := s.run(100)
	if !res.Stuck || res.Transient || !strings.Contains(res.Reason, "Rune Mysteries") {
		t.Fatalf("no essence, no miner: %+v", res)
	}
	s = newSimRune()
	s.bankKnown, s.bankEssence = true, 500
	res = s.run(100)
	if !res.Stuck || !strings.Contains(res.Reason, "Air talisman") {
		t.Fatalf("no talisman: %+v", res)
	}
	// A mine that fails once is not tried again this round.
	s = newSimRune()
	s.bankKnown, s.canMine = true, true
	s.bankTal[runecraftplan.KeyAir] = 1
	s.failAlways[runecraftplan.StageMine] = true
	res = s.run(100)
	if !res.Stuck && !res.Transient {
		t.Fatalf("a failing mine: %+v", res)
	}
	if s.calls[runecraftplan.StageMine] > masterRuneStepFails {
		t.Errorf("mined %d times", s.calls[runecraftplan.StageMine])
	}
}

// Three failures of one stage in a row end the round as a passing failure (a backoff, not a pause).
func TestTheLoopEndsOnThreeFailuresOfOneStage(t *testing.T) {
	s := newSimRune().rich()
	s.failAlways[runecraftplan.StageWalkToRuins] = true
	res := s.run(100)
	if !res.Transient || res.Done || s.calls[runecraftplan.StageWalkToRuins] != 3 || !strings.Contains(res.Reason, "walk-to-ruins failed 3 times in a row") {
		t.Fatalf("%+v calls %v", res, s.calls)
	}
	// Failures that clear are forgiven.
	s = newSimRune().rich()
	s.target = 5
	s.failFirst[runecraftplan.StageWalkToRuins] = 2
	s.failFirst[runecraftplan.StageCraft] = 2
	res = s.run(200)
	if !res.Done {
		t.Fatalf("two failures then success: %+v\n%s", res, strings.Join(s.log, "\n"))
	}
	var warnings int
	for _, l := range s.log {
		if strings.HasPrefix(l, "WARNING:") {
			warnings++
		}
	}
	if warnings != 4 {
		t.Errorf("%d warnings logged, want 4", warnings)
	}
}

// A stage that "succeeds" over and over with nothing changing is a failure: a bank that draws nothing.
func TestTheLoopCatchesAStageThatChangesNothing(t *testing.T) {
	s := newSimRune()
	s.bankTal[runecraftplan.KeyAir], s.bankEssence = 1, 500
	s.bankKnown = true
	s.bankNoop = true
	res := s.run(100)
	if !res.Transient || !strings.Contains(res.Reason, "without anything changing") || s.calls[runecraftplan.StageBank] != masterRuneStall {
		t.Fatalf("%+v bank calls %d", res, s.calls[runecraftplan.StageBank])
	}
}

// The round's clock ends it outside; inside a temple the account is taken out first.
func TestTheLoopExpiresOutsideNeverInside(t *testing.T) {
	s := newSimRune().rich()
	s.expired = true
	res := s.run(100)
	if !res.Expired || res.Transient || res.Done || s.total != 0 {
		t.Fatalf("expired at the start: %+v", res)
	}
	s = newSimRune().rich()
	s.where, s.temple, s.packEssence, s.packTal[runecraftplan.KeyAir] = runecraftplan.RegionTemple, runecraftplan.KeyAir, 27, true
	s.expired = true
	res = s.run(100)
	if !res.Expired || s.where != runecraftplan.RegionOutside || len(s.crafted) != 1 || s.level < 2 {
		t.Fatalf("expired inside: %+v where %v crafted %v", res, s.where, s.crafted)
	}
}

// A restart in a temple carries on: craft what the pack holds, leave, and go on with the rest.
func TestTheLoopResumesInsideATemple(t *testing.T) {
	s := newSimRune().rich()
	s.level, s.xp = 25, runecraftplan.XPForLevel(25)
	s.where, s.temple, s.packEssence = runecraftplan.RegionTemple, runecraftplan.KeyBody, 27
	s.packTal[runecraftplan.KeyBody] = true
	s.target = 26
	res := s.run(200)
	if !res.Done || s.crafted[0] != runecraftplan.KeyBody || s.where != runecraftplan.RegionOutside {
		t.Fatalf("%+v crafted %v\n%s", res, s.crafted, strings.Join(s.log, "\n"))
	}
	// The first stage logged is the craft inside, not a bank.
	if !strings.HasPrefix(s.log[0], "stage craft") {
		t.Errorf("first stage: %q", s.log[0])
	}
}

func TestTheLoopStopsWhenTheBotIsStopped(t *testing.T) {
	s := newSimRune().rich()
	s.stopAfter = 7
	res := s.run(1000)
	if !res.Stopped || res.Reason != "stopped" || s.total != 7 {
		t.Fatalf("%+v total %d", res, s.total)
	}
}

func TestTheLoopIsDoneAtOnceAtTheTarget(t *testing.T) {
	s := newSimRune().rich()
	s.level, s.xp = 44, runecraftplan.XPForLevel(44)
	res := s.run(100)
	if !res.Done || s.total != 0 {
		t.Fatalf("%+v", res)
	}
}

// The log: each stage once, as it starts, so the live log reads as the round's story.
func TestTheLoopLogsEachStageOnce(t *testing.T) {
	s := newSimRune().rich()
	s.target = 3
	s.run(100)
	var stages []string
	for _, l := range s.log {
		if strings.HasPrefix(l, "stage ") {
			stages = append(stages, strings.Fields(l)[1])
		}
	}
	got := strings.Join(stages, " ")
	if !strings.HasPrefix(got, "bank: walk-to-ruins: craft: portal-out:") {
		t.Errorf("stages: %s", got)
	}
}

// ---- the bot's hooks ----

func TestRecordWaitsForARuneRound(t *testing.T) {
	r := newMasterRig(t)
	r.b.set.Rune.On = true
	now := time.Date(2026, 10, 13, 21, 0, 0, 0, time.UTC)
	led := &r.b.ledger
	// A round that made something clears the waits.
	led.Fail(mastercraftplan.ModeRunecraft, now.Add(-time.Hour), r.b.set)
	led.Rune.PauseUntil = now.Add(time.Hour)
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeRunecraft, Produced: 540}, now)
	if led.Fails(mastercraftplan.ModeRunecraft) != 0 || !led.Rune.PauseUntil.IsZero() {
		t.Errorf("after a round that crafted: fails %d pause %v", led.Fails(mastercraftplan.ModeRunecraft), led.Rune.PauseUntil)
	}
	// A passing failure backs off.
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeRunecraft, Transient: true, Why: "bank failed 3 times"}, now)
	if until, ok := led.BackedOff(mastercraftplan.ModeRunecraft, now); !ok || until != now.Add(30*time.Minute) {
		t.Errorf("backoff %v %v", until, ok)
	}
	// A round with nothing to work with is paused and says why.
	r2 := newMasterRig(t)
	r2.b.recordWaits(r2.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeRunecraft, Why: "no essence source"}, now)
	l2 := &r2.b.ledger
	if l2.Rune.PauseUntil != now.Add(r2.b.set.FruitlessPause) || l2.Rune.LastStop != "no essence source" || l2.Fails(mastercraftplan.ModeRunecraft) != 0 {
		t.Errorf("stuck: %+v", l2.Rune)
	}
}

func TestAResumedRuneRoundIsStartedWhereTheAccountStands(t *testing.T) {
	r := newMasterRig(t)
	r.b.set.Rune.On = true
	if !r.b.resumesHere(r.ctx, mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft, Resume: true}) {
		t.Error("a levelling round under way is not started where the account stands")
	}
	if r.b.resumesHere(r.ctx, mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft}) {
		t.Error("a new round is started where the account stands")
	}
}

func TestStoreKeepKeepsTheRuneItemsWhileTheRoundIsUnfinished(t *testing.T) {
	r := newMasterRig(t)
	if got := r.b.storeKeep(); len(got) != 0 {
		t.Fatalf("round off: %v", got)
	}
	r.b.set.Rune.On = true
	keep := strings.Join(r.b.storeKeep(), ",")
	for _, want := range []string{"Rune essence", "Air talisman", "Body talisman"} {
		if !strings.Contains(keep, want) {
			t.Errorf("%q is not kept: %s", want, keep)
		}
	}
	r.b.runeLevel = 44
	if got := r.b.storeKeep(); len(got) != 0 {
		t.Errorf("at the target: %v", got)
	}
}

// The decision's reading of the account: where it stands, what the pack holds, and the Runecrafting stat.
func TestRuneSnapshotReadsTheAccount(t *testing.T) {
	r := newMasterRig(t)
	r.b.set.Rune.On = true
	sim := actionsim.New(actionsim.Objs{Stackable: func(int32) bool { return false }}, append([]actionsim.Stack{{ID: 1438, Count: 1}}, essenceStacks(27)...))
	r.ctx.Session = sim
	sim.StandAt(2841, 4830) // the Air temple's arrival tile
	sim.State().Handle(serverprot.UPDATE_STAT, statBytes(20, 4470, 20))
	r.b.ledger.Rune.NoteBank(300, map[string]int{runecraftplan.KeyBody: 1}, runecraftplan.Kit{Law: 4}, time.Now())

	var snap mastercraftplan.Snapshot
	r.b.runeSnapshot(r.ctx, &snap)
	in := snap.Rune
	if !in.Known || in.Level != 20 || in.XP != 4470 || in.Where != runecraftplan.RegionTemple || in.Temple != runecraftplan.KeyAir {
		t.Fatalf("%+v", in)
	}
	if in.PackEssence != 27 || !in.PackTalisman[runecraftplan.KeyAir] || !in.BankKnown || in.BankEssence != 300 || in.BankTalisman[runecraftplan.KeyBody] != 1 || in.BankKit.Law != 4 {
		t.Fatalf("%+v", in)
	}
	if r.b.runeLevel != 20 {
		t.Errorf("runeLevel %d", r.b.runeLevel)
	}
	// Standing in a temple is a round under way: the decision resumes it, mule heard or not.
	snap.Ledger = &r.b.ledger
	snap.Now = time.Now()
	if d := mastercraftplan.Decide(r.b.set, snap); d.Mode != mastercraftplan.ModeRunecraft || !d.Resume {
		t.Errorf("decision %+v", d)
	}
	// Off: nothing is read.
	r.b.set.Rune.On = false
	var off mastercraftplan.Snapshot
	r.b.runeSnapshot(r.ctx, &off)
	if off.Rune.Known {
		t.Errorf("read while off: %+v", off.Rune)
	}
}

func essenceStacks(n int) []actionsim.Stack {
	out := make([]actionsim.Stack, n)
	for i := range out {
		out[i] = actionsim.Stack{ID: runecraftplan.EssenceID, Count: 1}
	}
	return out
}

func statBytes(stat, xp, level int) []byte {
	return []byte{byte(stat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)}
}

// The ladder's last rung through the real loop: nothing in the bank, nothing on the mule, no miner. A lot of 3,000 is
// bought once (the budget is then spent), banked, and levelled on; with it gone the round stops stuck and says the lot
// is off - it does not buy again.
func TestTheLoopBuysOneMarketLotWhenNothingElseGivesEssence(t *testing.T) {
	s := newSimRune()
	s.bankKnown = true
	s.bankTal[runecraftplan.KeyAir], s.bankTal[runecraftplan.KeyBody] = 2, 2
	s.canBuy, s.buyQty = true, 3000
	res := s.run(4000)
	if !res.Stuck || res.Transient || s.calls[runecraftplan.StageBuy] != 1 || s.bought != 3000 {
		t.Fatalf("%+v buys %d bought %d\n%s", res, s.calls[runecraftplan.StageBuy], s.bought, strings.Join(s.log, "\n"))
	}
	if s.essence < 2970 || s.essence > 3000 || s.level < 20 {
		t.Errorf("%d essence bound, level %d: the lot's 3,000 should be crafted", s.essence, s.level)
	}
	if !strings.Contains(res.Reason, "no essence source") || !strings.Contains(res.Reason, "market lot: off") {
		t.Errorf("reason %q", res.Reason)
	}
	if got := s.calls[runecraftplan.StageMine]; got != 0 {
		t.Errorf("mined %d times without a miner", got)
	}
}

// Mining comes first: with a miner the lot is never bought; a miner that fails once leaves the lot as the next rung.
func TestTheLoopMinesBeforeItBuysAndBuysWhenMiningFails(t *testing.T) {
	s := newSimRune()
	s.bankKnown = true
	s.bankTal[runecraftplan.KeyAir] = 2
	s.canMine, s.canBuy, s.buyQty = true, true, 3000
	s.target = 10
	if res := s.run(2000); !res.Done || s.calls[runecraftplan.StageBuy] != 0 || s.mined == 0 {
		t.Fatalf("%+v buys %d mined %d", res, s.calls[runecraftplan.StageBuy], s.mined)
	}
	s = newSimRune()
	s.bankKnown = true
	s.bankTal[runecraftplan.KeyAir] = 2
	s.canMine, s.canBuy, s.buyQty = true, true, 3000
	s.failAlways[runecraftplan.StageMine] = true
	s.target = 10
	res := s.run(2000)
	if !res.Done || s.calls[runecraftplan.StageBuy] != 1 || s.bought != 3000 {
		t.Fatalf("%+v buys %d bought %d\n%s", res, s.calls[runecraftplan.StageBuy], s.bought, strings.Join(s.log, "\n"))
	}
}

// A lot that fails is not tried again this round: the round ends, stuck, naming it.
func TestTheLoopDoesNotRetryAFailedLot(t *testing.T) {
	s := newSimRune()
	s.bankKnown = true
	s.bankTal[runecraftplan.KeyAir] = 2
	s.canBuy, s.buyQty = true, 3000
	s.failAlways[runecraftplan.StageBuy] = true
	res := s.run(200)
	if !(res.Stuck || res.Transient) || s.calls[runecraftplan.StageBuy] > masterRuneStepFails {
		t.Fatalf("%+v buys %d", res, s.calls[runecraftplan.StageBuy])
	}
}
