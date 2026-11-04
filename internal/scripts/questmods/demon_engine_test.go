package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Demon Slayer through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module its kits and custom
// steps ran, the park, the completion record), with every step's EXECUTOR stubbed on the simulated pack of fillerRig
// (fillers_engine_test.go). The module's Decide is the real one; its custom steps keep their own names, so the module's reading of
// Failed and Ran is the live one. What the quest tab, the journal stage, the worn slots, the levels and the tile say are read from the
// rig, which no simulated interface can supply. The executors the module relies on live (the dialogue driver, the walker, the fights)
// are the engine's own or are checked on the real pack and loc index (demon_pack_test.go).

type demonRig struct {
	*fillerRig
	wielded bool
	levels  map[string]int
	hp, max int
	at      tile.Tile
	atKnown bool
	// keyGone makes the drain's pour yield no key (the key was taken by another account before the account got there).
	keyGone bool
	// noBones makes the bones kit's source fail.
	noBones bool
}

func newDemonRig(t *testing.T, stage int, pack, bank map[int]int) *demonRig {
	return &demonRig{fillerRig: newFillerRig(t, stage, pack, bank),
		levels: map[string]int{"attack": 40, "strength": 40, "defence": 30, "hitpoints": 45, "prayer": 31, "magic": 55, "ranged": 1}, hp: 45, max: 45}
}

// wrapDemon turns the module into one whose steps are stubs: the real Decide over a snapshot that carries the rig's journal and state.
func (r *demonRig) wrapDemon(q questrunner.Quest, stubs map[string]questrunner.CustomFunc) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case r.stage == 0:
			s.Journal, s.StageKnown = questplan.StatusNotStarted, false
		case r.stage >= questplan.DemonComplete:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, r.stage, true
		}
		s.Levels, s.MaxHP, s.HP = r.levels, r.max, r.hp
		s.FreeSlots = 28 - len(r.sim.Pack())
		if r.wielded {
			s.WornIDs = map[int]int{questplan.ObjSilverlight: 1}
		}
		if r.atKnown {
			s.Tile, s.TileKnown = r.at, true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		st.Reason = st.Describe()
		switch st.Kind {
		case questplan.StepProvision:
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		case questplan.StepCustom:
		default:
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

// demonStubs plays the content's rule for each step on the rig.
func (r *demonRig) demonStubs() map[string]questrunner.CustomFunc {
	stubs := r.provisionStub(questplan.KitDemonCoins, questplan.KitDemonKeys, questplan.KitDemonBucket, questplan.KitDemonFoodSewer,
		questplan.KitDemonFoodCircle)
	stubs["provision:"+questplan.KitDemonBones] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.log = append(r.log, "stub provision "+st.KitName)
		if r.noBones {
			return false
		}
		return r.provision(st.Kit)
	}
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.Stop.ID {
		case questplan.NpcGypsyAris:
			if !r.take(questplan.CoinsID, 1) {
				return false
			}
			r.stage = questplan.DemonTalkedAris
		case questplan.NpcSirPrysin:
			switch r.stage {
			case questplan.DemonTalkedAris:
				r.stage = questplan.DemonKeyHunt
			case questplan.DemonKeyHunt:
				for _, id := range []int{questplan.ObjKeyTraiborn, questplan.ObjKeyRovin, questplan.ObjKeyDrain} {
					if !r.take(id, 1) {
						return false
					}
				}
				r.give(questplan.ObjSilverlight, 1)
				r.stage = questplan.DemonSilverlight
			case questplan.DemonSilverlight:
				if r.held(questplan.ObjSilverlight) == 0 && !r.wielded {
					r.give(questplan.ObjSilverlight, 1)
				}
			}
		case questplan.NpcCaptainRovin:
			if r.stage < questplan.DemonKeyHunt || r.stage >= questplan.DemonSilverlight {
				return false
			}
			r.give(questplan.ObjKeyRovin, 1)
		default:
			r.t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	stubs[questplan.CustomDemonTraiborn] = func(e *questrunner.Engine, st questplan.Step) bool {
		if r.held(questplan.ObjBones) < questplan.DemonBonesNeeded || !r.take(questplan.ObjBones, questplan.DemonBonesNeeded) {
			return false
		}
		r.give(questplan.ObjKeyTraiborn, 1)
		return true
	}
	stubs[questplan.CustomDemonWater] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(questplan.ObjBucket, 1) {
			return false
		}
		r.give(questplan.ObjBucketOfWater, 1)
		return true
	}
	stubs[questplan.CustomDemonDrain] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(questplan.ObjBucketOfWater, 1) {
			return false
		}
		r.give(questplan.ObjBucket, 1)
		if r.keyGone {
			return false
		}
		r.give(questplan.ObjKeyDrain, 1)
		return true
	}
	stubs[questplan.CustomDemonSewer] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.at = questplan.DemonManholeStand
		return true
	}
	stubs[questplan.CustomDemonWield] = func(e *questrunner.Engine, st questplan.Step) bool {
		if r.held(questplan.ObjSilverlight) == 0 {
			return false
		}
		r.wielded = true
		return true
	}
	stubs[questplan.CustomDemonDelrith] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.wielded {
			return false
		}
		r.stage = questplan.DemonComplete
		return true
	}
	return stubs
}

func (r *demonRig) run(t *testing.T, budget time.Duration) (*questrunner.Engine, questrunner.Result) {
	t.Helper()
	q := r.wrapDemon(DemonSlayer(), r.demonStubs())
	if q.ID() != questplan.DemonSlayerID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	return e, e.Run(q, budget)
}

func TestDemonSlayerRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newDemonRig(t, 0, map[int]int{questplan.CoinsID: 100}, nil)
	e, res := r.run(t, 60*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.DemonComplete {
		t.Errorf("stage %d at the end", r.stage)
	}
	cs := e.Store.Record(questplan.DemonSlayerID).Completions
	if len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	order := []string{
		"talk to Gypsy Aris", "talk to Sir Prysin", "provision demon-keys", "provision demon-bones", "demon-traiborn", "talk to Captain Rovin",
		"provision demon-food-sewer", "provision demon-bucket", "demon-water", "demon-drain", "talk to Sir Prysin", "provision demon-food-circle",
		"demon-wield", "demon-delrith", "[demon] COMPLETED",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(joined[max(last, 0):], want)
		if i < 0 {
			t.Fatalf("the log has no %q after position %d:\n%s", want, last, joined)
		}
		last += i + len(want)
	}
	// The three keys are given to Sir Prysin and the coin to the Gypsy: nothing else is spent.
	if r.held(questplan.ObjKeyTraiborn)+r.held(questplan.ObjKeyRovin)+r.held(questplan.ObjKeyDrain) != 0 || r.held(questplan.ObjBones) != 0 ||
		r.held(questplan.CoinsID) != 99 {
		t.Errorf("pack %v at the end", r.pack())
	}
	// Silverlight is the one new thing the account holds.
	if r.held(questplan.ObjSilverlight) != 1 {
		t.Errorf("the account holds %d Silverlights", r.held(questplan.ObjSilverlight))
	}
}

// A trainer that banked the keys the last run found is not sent for them again: the first kit draws them, and the engine goes
// straight on to the bucket, the drain and Sir Prysin. Rovin's and Traiborn's talks and the bones never happen.
func TestDemonSlayerResumesFromBankedKeys(t *testing.T) {
	r := newDemonRig(t, questplan.DemonKeyHunt, map[int]int{questplan.CoinsID: 100},
		map[int]int{questplan.ObjKeyTraiborn: 1, questplan.ObjKeyRovin: 1})
	_, res := r.run(t, 60*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	for _, absent := range []string{"talk to Captain Rovin", "demon-traiborn", "provision demon-bones", "talk to Gypsy Aris"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q, a step the bank made unnecessary:\n%s", absent, joined)
		}
	}
	for _, want := range []string{"provision demon-keys", "demon-drain", "demon-wield", "demon-delrith"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log has no %q:\n%s", want, joined)
		}
	}
}

// An account that has the bucket of water already goes to the drain at once, and one standing in the sewer is taken out of it before
// anything else.
func TestDemonSlayerStartsFromTheSewerAndTheBucket(t *testing.T) {
	r := newDemonRig(t, questplan.DemonKeyHunt, map[int]int{questplan.ObjKeyTraiborn: 1, questplan.ObjKeyRovin: 1, questplan.ObjBucketOfWater: 1}, nil)
	r.at, r.atKnown = questplan.DemonSewerLanding, true
	_, res := r.run(t, 60*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	// The sewer first (before the bank read), then the drain with the bucket the account carried: no bucket kit, no sink.
	if i, j := strings.Index(joined, "demon-sewer"), strings.Index(joined, "provision demon-keys"); i < 0 || j < 0 || i > j {
		t.Errorf("the sewer step did not come first:\n%s", joined)
	}
	for _, absent := range []string{"provision demon-bucket", "demon-water"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q:\n%s", absent, joined)
		}
	}
}

// An account that lost its sword at stage 29 asks Sir Prysin for another and goes on.
func TestDemonSlayerAsksForALostSword(t *testing.T) {
	r := newDemonRig(t, questplan.DemonSilverlight, map[int]int{questplan.CoinsID: 5}, nil)
	_, res := r.run(t, 60*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	i, j := strings.Index(joined, "talk to Sir Prysin"), strings.Index(joined, "demon-wield")
	if i < 0 || j < 0 || i > j {
		t.Errorf("the account did not ask Sir Prysin before it wielded:\n%s", joined)
	}
}

// A banked sword is drawn from the bank, not asked for.
func TestDemonSlayerDrawsABankedSword(t *testing.T) {
	r := newDemonRig(t, questplan.DemonSilverlight, nil, map[int]int{questplan.ObjSilverlight: 1})
	_, res := r.run(t, 60*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if joined := strings.Join(r.log, "\n"); strings.Contains(joined, "talk to Sir Prysin") {
		t.Errorf("the account asked Sir Prysin for a sword the bank held:\n%s", joined)
	}
}

// An account with too few hitpoints for Delrith never spends a coin on the Gypsy: it parks at the start, for hours, and says why.
func TestDemonSlayerParksAWeakAccountBeforeTheGypsy(t *testing.T) {
	r := newDemonRig(t, 0, map[int]int{questplan.CoinsID: 100}, nil)
	r.levels["hitpoints"], r.hp, r.max = 15, 15, 15
	e, res := r.run(t, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "15 hitpoints") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.held(questplan.CoinsID) != 100 || strings.Contains(strings.Join(r.log, "\n"), "talk to") {
		t.Errorf("the account spent its coin or talked: %v", r.log)
	}
	if rec := e.Store.Record(questplan.DemonSlayerID); time.UnixMilli(rec.BackoffUntil).Before(time.Now().Add(questplan.DemonFitRetry - time.Minute)) {
		t.Errorf("parked until %v, want about %s from now", time.UnixMilli(rec.BackoffUntil), questplan.DemonFitRetry)
	}
}

// An account whose swing cannot fell Delrith in time carries the sword to the door of the circle and no further: it parks after the
// sword is in hand and before it is wielded.
func TestDemonSlayerParksAtTheCircleWhenTheAccountCannotBeatIt(t *testing.T) {
	r := newDemonRig(t, questplan.DemonSilverlight, map[int]int{questplan.ObjSilverlight: 1}, nil)
	r.levels["attack"], r.levels["strength"] = 5, 5
	_, res := r.run(t, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "swing") {
		t.Errorf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if joined := strings.Join(r.log, "\n"); strings.Contains(joined, "demon-wield") || strings.Contains(joined, "demon-delrith") || r.wielded {
		t.Errorf("the account wielded the sword or went to the circle:\n%s", joined)
	}
}

// An account the circle's dark wizards hunt (combat 41 and under) is parked wherever it is in the quest, for the hours a combat level
// takes, and says why: the circle is single-way combat, so the server would refuse every attack on Delrith while a wizard casts. It
// spends no coin, fetches no bones or keys, wields nothing and goes nowhere, whatever food it carries.
func TestDemonSlayerParksAnAccountTheWizardsHunt(t *testing.T) {
	hunted := func(r *demonRig) {
		r.levels = map[string]int{"attack": 20, "strength": 20, "defence": 15, "hitpoints": 45, "prayer": 1, "magic": 20, "ranged": 1}
		r.hp, r.max = 45, 45
	}
	for name, c := range map[string]struct {
		stage int
		held  map[int]int
	}{
		"before the Gypsy, with a coin": {questplan.DemonNotStarted, map[int]int{questplan.CoinsID: 100}},
		"on the key hunt":               {questplan.DemonKeyHunt, map[int]int{questplan.CoinsID: 100}},
		"with the sword and the sharks": {questplan.DemonSilverlight, map[int]int{questplan.ObjSilverlight: 1, questplan.ObjShark: 28}},
	} {
		r := newDemonRig(t, c.stage, c.held, nil)
		hunted(r)
		e, res := r.run(t, 30*time.Second)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "single-way") {
			t.Errorf("%s: the run ended %s (%s): %v", name, res.Outcome, res.Reason, r.log)
			continue
		}
		joined := strings.Join(r.log, "\n")
		for _, bad := range []string{"talk to", "stub provision", "demon-wield", "demon-delrith", "demon-traiborn", "demon-drain"} {
			if strings.Contains(joined, bad) {
				t.Errorf("%s: the log has %q:\n%s", name, bad, joined)
			}
		}
		if r.held(questplan.CoinsID) != c.held[questplan.CoinsID] || r.wielded {
			t.Errorf("%s: the account spent a coin or wielded the sword", name)
		}
		if rec := e.Store.Record(questplan.DemonSlayerID); time.UnixMilli(rec.BackoffUntil).Before(time.Now().Add(questplan.DemonFitRetry - time.Minute)) {
			t.Errorf("%s: parked until %v, want about %s from now", name, time.UnixMilli(rec.BackoffUntil), questplan.DemonFitRetry)
		}
	}
}

// No bones from the bank, the mule or the cows: the quest parks for hours rather than looping, and the bones kit is tried once.
func TestDemonSlayerParksWhenThereAreNoBones(t *testing.T) {
	r := newDemonRig(t, questplan.DemonKeyHunt, map[int]int{questplan.CoinsID: 100}, nil)
	r.noBones = true
	e, res := r.run(t, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "25 bones") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if n := strings.Count(strings.Join(r.log, "\n"), "stub provision demon-bones"); n != 1 {
		t.Errorf("the bones kit ran %d times", n)
	}
	if rec := e.Store.Record(questplan.DemonSlayerID); time.UnixMilli(rec.BackoffUntil).Before(time.Now().Add(questplan.DemonBonesRetry - time.Minute)) {
		t.Errorf("parked until %v", time.UnixMilli(rec.BackoffUntil))
	}
}

// A key that had gone when the account got there fails the drain step once, and the quest parks for hours: it does not pour a second
// bucket the same run (the pour is the quest's one clock).
func TestDemonSlayerParksWhenTheDrainKeyIsGone(t *testing.T) {
	r := newDemonRig(t, questplan.DemonKeyHunt, map[int]int{questplan.ObjKeyTraiborn: 1, questplan.ObjKeyRovin: 1, questplan.ObjBucketOfWater: 1}, nil)
	r.keyGone = true
	_, res := r.run(t, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "drain key") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if n := strings.Count(strings.Join(r.log, "\n"), "demon-drain"); n < 1 || n > 2 {
		t.Errorf("the drain step ran %d times: %v", n, r.log)
	}
}

// No coin anywhere: the Gypsy cannot be paid and the quest parks (two hours) with the reason.
func TestDemonSlayerParksWithoutACoin(t *testing.T) {
	r := newDemonRig(t, 0, nil, nil)
	_, res := r.run(t, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "coin") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}
