package mastercraftplan

import (
	"strings"
	"testing"
	"time"
)

var cannonNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// cannonSnap is a snapshot a cannonball round may start from: Smithing 60, the mule holding steel, the own bank read and holding a
// mould and nothing else.
func cannonSnap(mule map[string]int) Snapshot {
	s := readySnapshot(mule)
	s.Now = cannonNow
	s.SmithingLevel = 60
	s.Cannon = CannonballInput{OwnKnown: true, Mould: true, QuestKnown: true, QuestDone: true}
	return s
}

func cannonSet() Settings {
	set := DefaultSettings()
	set.Arrows, set.Herbs, set.Superheat, set.Smith = false, false, false, false
	return set
}

// The content's numbers (cannonballs.rs2): 4 balls and 37.5 XP a bar, Smithing 35, the mould obj 4, the ball obj 2.
func TestCannonballFactsAreContents(t *testing.T) {
	if CannonballsPerBar != 4 || CannonballXPTenths != 375 || CannonballLevel != 35 || CannonballID != 2 || AmmoMouldID != 4 || SteelBarID != 2353 {
		t.Fatal("a content fact changed")
	}
	if got := BarsFor(2000); got != 500 {
		t.Errorf("2000 balls take %d bars", got)
	}
	if got := BarsFor(2001); got != 501 {
		t.Errorf("2001 balls take %d bars (rounded up)", got)
	}
	if BarsFor(0) != 0 || BarsFor(-5) != 0 {
		t.Error("no balls, no bars")
	}
	// The pack holds 27 bars beside the mould, and one load is 270 ticks.
	if CannonPackBars != 27 || CannonBarTicks != 10 {
		t.Error("pack or pace")
	}
}

func TestCannonballDefaultsAndSettings(t *testing.T) {
	d := DefaultSettings()
	if d.CannonballTarget != 60000 || d.CannonballBatch != 500 || d.CannonballMinBars != 100 {
		t.Fatalf("defaults %+v", d)
	}
	for name, raw := range map[string]map[string]interface{}{
		"negative target":     {KeyCannonballTarget: -1},
		"huge target":         {KeyCannonballTarget: MaxCannonballTarget + 1},
		"zero batch":          {KeyCannonballBatch: 0},
		"zero min bars":       {KeyCannonballMinBars: 0},
		"min over batch":      {KeyCannonballBatch: 50, KeyCannonballMinBars: 51},
		"target not a number": {KeyCannonballTarget: "lots"},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
	s, err := DecodeSettings(map[string]interface{}{KeyCannonballTarget: 0})
	if err != nil || s.CannonballTarget != 0 {
		t.Errorf("0 turns the round off: %+v %v", s, err)
	}
}

func TestCannonballsReadyTriggerAndBatchSizing(t *testing.T) {
	set := cannonSet()
	// 6000 steel, nothing on hand: the batch (500 bars) bounds the round.
	s := cannonSnap(map[string]int{"Steel bar": 6000})
	p, ok := CannonballsReady(set, s, false)
	if !ok || p.Bars != 500 || p.Stock != 0 || p.Short != 60000 || p.Steel != 6000 || p.Resume {
		t.Fatalf("%+v %v", p, ok)
	}
	// Few bars: the bars bound it, down to the minimum that starts a round.
	s = cannonSnap(map[string]int{"Steel bar": 100})
	if p, ok := CannonballsReady(set, s, false); !ok || p.Bars != 100 {
		t.Errorf("exactly the minimum: %+v %v", p, ok)
	}
	s = cannonSnap(map[string]int{"Steel bar": 99})
	if p, ok := CannonballsReady(set, s, false); ok || !strings.Contains(p.Why, "99 Steel bars") {
		t.Errorf("under the minimum: %+v %v", p, ok)
	}
	// The bars of this account's own bank count with the mule's.
	s.Cannon.OwnSteel = 1
	if p, ok := CannonballsReady(set, s, false); !ok || p.Bars != 100 || p.Steel != 100 {
		t.Errorf("own bank counted: %+v %v", p, ok)
	}
	// Balls on hand, the mule's and the own bank's, come off what is short: 59,000 on hand needs 250 bars, under the minimum
	// of 100 only because the target is nearly met, and a round still starts for them.
	s = cannonSnap(map[string]int{"Steel bar": 5000, "Cannonball": 55000})
	s.Cannon.OwnBalls = 4000
	p, ok = CannonballsReady(set, s, false)
	if !ok || p.Stock != 59000 || p.Short != 1000 || p.Bars != 250 {
		t.Errorf("nearly there: %+v %v", p, ok)
	}
	// A shortfall under the minimum still starts a round for what is short (the minimum is for bars, not balls).
	s = cannonSnap(map[string]int{"Steel bar": 5000, "Cannonball": 59999})
	if p, ok := CannonballsReady(set, s, false); !ok || p.Bars != 1 {
		t.Errorf("one ball short: %+v %v", p, ok)
	}
	// At or over the target: nothing.
	s = cannonSnap(map[string]int{"Steel bar": 5000, "Cannonball": 60000})
	if p, ok := CannonballsReady(set, s, false); ok || !strings.Contains(p.Why, "at the target") {
		t.Errorf("at target: %+v %v", p, ok)
	}
	// Target 0 is off; no published bank is no decision.
	set.CannonballTarget = 0
	if _, ok := CannonballsReady(set, cannonSnap(map[string]int{"Steel bar": 6000}), false); ok {
		t.Error("target 0 must be off")
	}
	set = cannonSet()
	s = cannonSnap(nil)
	s.Stock = nil
	if p, ok := CannonballsReady(set, s, false); ok || !strings.Contains(p.Why, "not published") {
		t.Errorf("no bank: %+v %v", p, ok)
	}
	// A smaller batch setting bounds the round.
	set.CannonballBatch, set.CannonballMinBars = 80, 10
	if p, ok := CannonballsReady(set, cannonSnap(map[string]int{"Steel bar": 6000}), false); !ok || p.Bars != 80 {
		t.Errorf("batch 80: %+v %v", p, ok)
	}
}

// No blind attempts: each missing requirement says why and nothing starts.
func TestCannonballsReadyRefusesWithAReason(t *testing.T) {
	set := cannonSet()
	mule := map[string]int{"Steel bar": 6000}
	for name, tc := range map[string]struct {
		mutate func(*Snapshot, *Settings)
		want   string
	}{
		"free world":       {func(_ *Snapshot, st *Settings) { st.MembersWorld = false }, "members world"},
		"Smithing 34":      {func(s *Snapshot, _ *Settings) { s.SmithingLevel = 34 }, "Smithing 34"},
		"Smithing unknown": {func(s *Snapshot, _ *Settings) { s.SmithingLevel = 0 }, "Smithing 0"},
		"quest unfinished": {func(s *Snapshot, _ *Settings) { s.Cannon.QuestDone = false }, "Dwarf Cannon is not complete"},
		"no mould":         {func(s *Snapshot, _ *Settings) { s.Cannon.Mould = false }, "no Ammo mould"},
		"Smithing 200m":    {func(s *Snapshot, _ *Settings) { s.Cannon.SmithingXP = MaxSkillXP }, "200000000 experience"},
	} {
		s := cannonSnap(mule)
		st := set
		tc.mutate(&s, &st)
		p, ok := CannonballsReady(st, s, false)
		if ok || !strings.Contains(p.Why, tc.want) {
			t.Errorf("%s: %+v %v", name, p, ok)
		}
		// A round under way does not get past them either.
		s.Cannon.OwnSteel = 54
		if p, ok := CannonballsReady(st, s, true); ok {
			t.Errorf("%s (resume): %+v", name, p)
		}
	}
	// The quest tab not read yet and a bank not read yet are not refusals: the round starts and reads the bank itself.
	s := cannonSnap(mule)
	s.Cannon = CannonballInput{}
	if p, ok := CannonballsReady(set, s, false); !ok {
		t.Errorf("nothing read yet: %+v", p)
	}
	// Just under the cap still goes.
	s = cannonSnap(mule)
	s.Cannon.SmithingXP = MaxSkillXP - 1
	if _, ok := CannonballsReady(set, s, false); !ok {
		t.Error("under the cap")
	}
}

func TestCannonballsResumeFromTheOwnBankWithNoTrigger(t *testing.T) {
	set := cannonSet()
	s := cannonSnap(nil)
	s.Stock = stockMap(map[string]int{}) // the mule holds nothing
	s.Cannon.OwnSteel = 37               // far under the minimum
	p, ok := CannonballsReady(set, s, true)
	if !ok || !p.Resume || p.Bars != 37 {
		t.Fatalf("%+v %v", p, ok)
	}
	// Not under way: the same bank is no trigger.
	if _, ok := CannonballsReady(set, s, false); ok {
		t.Error("a new round needs the minimum")
	}
	// A bound by the batch.
	s.Cannon.OwnSteel = 900
	if p, ok := CannonballsReady(set, s, true); !ok || p.Bars != set.CannonballBatch {
		t.Errorf("batch: %+v", p)
	}
	// No bars left in the read bank: the round is over, nothing resumes.
	s.Cannon.OwnSteel = 0
	if p, ok := CannonballsReady(set, s, true); ok || p.Resume {
		t.Errorf("bank empty: %+v", p)
	}
	// A restart (bank not read) carries a round on: it reads the bank itself.
	s.Cannon = CannonballInput{}
	if p, ok := CannonballsReady(set, s, true); !ok || !p.Resume {
		t.Errorf("unread: %+v", p)
	}
}

func TestPlanCannonAskTakesOnlyWhatTheBankLacksAndTheMuleHolds(t *testing.T) {
	mule := stockMap(map[string]int{"Steel bar": 300})
	p := CannonballPlan{Bars: 500}
	if got := PlanCannonAsk(p, mule, 0); len(got) != 1 || got[0] != (Line{Item: "Steel bar", Qty: 300}) {
		t.Errorf("the mule holds less: %v", got)
	}
	if got := PlanCannonAsk(p, mule, 250); len(got) != 1 || got[0].Qty != 250 {
		t.Errorf("own bank part: %v", got)
	}
	if got := PlanCannonAsk(p, mule, 500); got != nil {
		t.Errorf("own bank holds it all: %v", got)
	}
	if got := PlanCannonAsk(p, nil, 0); got != nil {
		t.Errorf("no bank: %v", got)
	}
	if CannonDraw(27, 500, 1000) != 27 || CannonDraw(27, 10, 1000) != 10 || CannonDraw(27, 500, 3) != 3 || CannonDraw(0, 5, 5) != 0 || CannonDraw(27, -4, 9) != 0 {
		t.Error("CannonDraw")
	}
}

func TestCannonballRefusal(t *testing.T) {
	for said, want := range map[string]string{
		"You need to be on a members' world to make cannonballs.": "members",
		"You need a Smithing level of 35 to make cannonballs.":    "level",
		"You need a cannonball mould to make cannonballs.":        "mould",
		"You need a steel bar to make cannonballs.":               "bar",
		"You heat the steel bar into a liquid state.":             "",
		"": "",
	} {
		if got := CannonballRefusal(said); got != want {
			t.Errorf("%q: %q, want %q", said, got, want)
		}
	}
}

// Ranking: below the trawler and the Nature rounds and the gem, bow and pearl rounds, above the anvil round and the ScarabSwarm
// fallback; the skill goal does not hold it; products waiting for the mule come first.
func TestDecideRanksTheCannonballRound(t *testing.T) {
	set := cannonSet()
	s := cannonSnap(map[string]int{"Steel bar": 8001})
	d := Decide(set, s)
	if d.Mode != ModeCannonballs || d.Cannon.Bars != 500 || d.Resume {
		t.Fatalf("%+v", d)
	}
	// Above the anvil round, which would smith the same bars (and is what 8,001 Steel starts without it).
	set.Smith = true
	if d := Decide(set, s); d.Mode != ModeCannonballs {
		t.Errorf("before the anvil: %+v", d)
	}
	set.CannonballTarget = 0
	if d := Decide(set, s); d.Mode != ModeSmith {
		t.Errorf("target 0 leaves the anvil round: %+v", d)
	}
	set = cannonSet()
	// Above ScarabSwarm, below a hand-over, below the gem round.
	s.Ledger.PendingDeliver = true
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Errorf("deliver first: %+v", d)
	}
	s.Ledger.PendingDeliver = false
	s.Stock = stockMap(map[string]int{"Steel bar": 8001, "Uncut sapphire": 600, "Chisel": 1})
	s.CraftingLevel = 30
	if d := Decide(orderSettings(), s); d.Mode != ModeGems {
		t.Errorf("a gem round first: %+v", d)
	}
	// Not decided when the mule is not heard (every round ends in a trade with it).
	s = cannonSnap(map[string]int{"Steel bar": 8001})
	s.MuleHeard = false
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("mule away: %+v", d)
	}
	// The trawler round (a block is on) comes before it.
	set.Trawler = true
	s = cannonSnap(map[string]int{"Steel bar": 8001})
	if d := Decide(set, s); d.Mode != ModeTrawler {
		t.Errorf("trawler first: %+v", d)
	}
	// The skill goal does not hold it: Smithing 99 with craftSkillGoal 99 still smelts.
	set = cannonSet()
	set.SkillGoal = 99
	s = cannonSnap(map[string]int{"Steel bar": 8001})
	s.SmithingLevel = 99
	if d := Decide(set, s); d.Mode != ModeCannonballs {
		t.Errorf("the goal must not hold a supply round: %+v", d)
	}
	// The 200 million cap does.
	s.Cannon.SmithingXP = MaxSkillXP
	if d := Decide(set, s); d.Mode == ModeCannonballs || !strings.Contains(d.Why, "cannonballs: Smithing is at 200000000") {
		t.Errorf("the cap: %+v", d)
	}
}

// A pause after a round that found nothing, and a backoff after a failed one, hold the round off; so does the quest or mould missing,
// and the log line says so.
func TestDecideCannonballsPauseAndBackoff(t *testing.T) {
	set := cannonSet()
	s := cannonSnap(map[string]int{"Steel bar": 8001})
	s.Ledger.Cannon.PauseUntil = cannonNow.Add(time.Hour)
	d := Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "cannonball rounds are paused until") {
		t.Errorf("paused: %+v", d)
	}
	s.Ledger.Cannon.PauseUntil = time.Time{}
	s.Ledger.Fail(ModeCannonballs, cannonNow, set)
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "cannonball rounds are backing off") {
		t.Errorf("backoff: %+v", d)
	}
	s = cannonSnap(map[string]int{"Steel bar": 8001})
	s.Cannon.Mould = false
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "cannonballs: no Ammo mould") {
		t.Errorf("no mould: %+v", d)
	}
	// A round under way is carried on from the own bank even with the mule empty, ahead of the anvil round.
	set.Smith = true
	s = cannonSnap(map[string]int{})
	s.Cannon.OwnSteel = 54
	s.Ledger.Cannon.Active = true
	d = Decide(set, s)
	if d.Mode != ModeCannonballs || !d.Resume || d.Cannon.Bars != 54 {
		t.Errorf("resume: %+v", d)
	}
	if !Chain(ModeCannonballs, d) {
		t.Error("a resume chains")
	}
	if Chain(ModeCannonballs, Decision{Mode: ModeCannonballs}) {
		t.Error("a new round of the same kind waits for the next check")
	}
}
