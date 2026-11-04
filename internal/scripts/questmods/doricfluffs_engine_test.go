package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// Doric's Quest and Gertrude's Cat through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the
// module its kits ran, the park for a gate that will not open, the completion record), with every step's EXECUTOR stubbed, as the
// fillers' engine tests do (fillers_engine_test.go): the module's Decide is the real one, the provisioner's kits run the real
// questplan.PlanProvision over a simulated pack and bank, and only the quest tab's colour, the journal stage and the Mining level,
// which no simulated interface can supply, come from the rig. The live executors (the miner, the brothers, the yard) are checked
// against the real map in doricfluffs_pack_test.go.

// notingProvision is provisionStub that also tells the engine's bank view what the rig's bank holds afterwards, as a real bank
// visit does (a module that reads Snapshot.BankKnown, like the pickaxe kit, needs it).
func (r *fillerRig) notingProvision(names ...string) map[string]questrunner.CustomFunc {
	m := map[string]questrunner.CustomFunc{}
	for _, n := range names {
		m["provision:"+n] = func(e *questrunner.Engine, st questplan.Step) bool {
			r.log = append(r.log, "stub provision "+st.KitName)
			ok := r.provision(st.Kit)
			bank := map[int]int{}
			for id, n := range r.bank {
				bank[id] = n
			}
			e.Provisioner().Bank.Note(bank)
			return ok
		}
	}
	return m
}

// withMining wraps a quest so its decisions see Mining level mining (the simulated session reports no levels).
func withMining(q questrunner.Quest, mining int) questrunner.Quest {
	inner := q.Module.Decide
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		s.Levels = map[string]int{"mining": mining}
		return inner(s)
	}
	return q
}

// reasonIs reports whether a wrapped step is the custom step name (wrap turns every custom step into "stub:custom" and keeps what
// it was in Reason).
func reasonIs(st questplan.Step, custom string) bool { return strings.HasPrefix(st.Reason, custom) }

// ---- Doric's Quest ----

func (r *fillerRig) doricStubs(mining int, mined *[]map[int]int) map[string]questrunner.CustomFunc {
	stubs := r.notingProvision(questplan.KitDoricOres, questplan.KitDoricPick)
	stubs["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch r.stage {
		case questplan.DoricNotStarted:
			r.stage = questplan.DoricStarted
		case questplan.DoricStarted:
			if r.held(questplan.DoricClay) >= 6 && r.held(questplan.DoricCopperOre) >= 4 && r.held(questplan.DoricIronOre) >= 2 {
				r.take(questplan.DoricClay, 6)
				r.take(questplan.DoricCopperOre, 4)
				r.take(questplan.DoricIronOre, 2)
				r.give(questplan.CoinsID, 180)
				r.stage = questplan.DoricComplete
			}
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:custom"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !reasonIs(st, questplan.CustomDoricMine) {
			r.stubLog("unexpected custom step %q", st.Reason)
			return false
		}
		if r.held(1265)+r.held(1267) == 0 {
			return false // the miner refuses without a pickaxe
		}
		owed := map[int]int{
			questplan.DoricClay:      max(6-r.held(questplan.DoricClay), 0),
			questplan.DoricCopperOre: max(4-r.held(questplan.DoricCopperOre), 0),
		}
		if mining >= questplan.DoricIronLevel {
			owed[questplan.DoricIronOre] = max(2-r.held(questplan.DoricIronOre), 0)
		}
		*mined = append(*mined, owed)
		gained := false
		for id, n := range owed {
			if n > 0 {
				r.give(id, n)
				gained = true
			}
		}
		r.stubLog("mined %v", owed)
		return gained
	}
	return stubs
}

func (r *fillerRig) doricQuest(mining int, mined *[]map[int]int) questrunner.Quest {
	return withMining(r.wrap(DoricsQuest(), r.doricStubs(mining, mined), questplan.DoricComplete), mining)
}

func TestDoricsQuestRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, 0, nil, nil)
	var mined []map[int]int
	q := r.doricQuest(15, &mined)
	if q.ID() != questplan.DoricsQuestID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.DoricComplete || r.held(questplan.CoinsID) != 180 {
		t.Errorf("stage %d, %d coins", r.stage, r.held(questplan.CoinsID))
	}
	if cs := e.Store.Record(questplan.DoricsQuestID).Completions; len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	if len(mined) != 1 || mined[0][questplan.DoricClay] != 6 || mined[0][questplan.DoricCopperOre] != 4 || mined[0][questplan.DoricIronOre] != 2 {
		t.Errorf("mining runs %v, want one for everything (a bank and a mule that held nothing)", mined)
	}
	joined := strings.Join(r.log, "\n")
	idx := func(s string) int { return strings.Index(joined, s) }
	// The order: the start, the bank and the mule for the ores, the pickaxe, the mine, then the hand-in.
	want := []string{"talk to Doric", "provision doric-ores", "provision doric-pick", "doric-mine", "talk to Doric", "[doric] COMPLETED"}
	at := -1
	for _, w := range want {
		i := strings.Index(joined[at+1:], w)
		if i < 0 {
			t.Fatalf("the log has no %q after position %d:\n%s", w, at, joined)
		}
		at += 1 + i
	}
	if idx("provision doric-ores") > idx("provision doric-pick") {
		t.Errorf("the ores were asked for after the pickaxe:\n%s", joined)
	}
}

// What the bank holds is not mined: copper and iron in the bank leave the clay for the mine, and a pickaxe the bank holds and the
// account can use is drawn instead of a Bronze one.
func TestDoricsQuestDrawsTheBankBeforeItMinesAndTheBankedPickaxe(t *testing.T) {
	r := newFillerRig(t, questplan.DoricStarted, nil, map[int]int{questplan.DoricCopperOre: 4, questplan.DoricIronOre: 2, 1267: 1})
	var mined []map[int]int
	q := r.doricQuest(1, &mined)
	res := r.engine().Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if len(mined) != 1 || mined[0][questplan.DoricClay] != 6 || mined[0][questplan.DoricCopperOre] != 0 || len(mined[0]) != 2 {
		t.Errorf("mining runs %v, want the clay alone (Mining 1 mines no iron and the bank gave the rest)", mined)
	}
	if r.held(1267) != 1 {
		t.Errorf("the banked iron pickaxe was not drawn: pack %v", r.pack())
	}
	if r.held(1265) != 0 {
		t.Errorf("a Bronze pickaxe was sourced although the bank held a better one: pack %v", r.pack())
	}
}

// Below Mining 15 an account whose bank and mule hold no iron parks for hours with the reason, rather than loop or train.
func TestDoricsQuestParksForIronItCannotMine(t *testing.T) {
	r := newFillerRig(t, questplan.DoricStarted, map[int]int{questplan.DoricClay: 6, questplan.DoricCopperOre: 4, 1265: 1}, nil)
	var mined []map[int]int
	q := r.doricQuest(14, &mined)
	e := r.engine()
	res := e.Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "iron") || !strings.Contains(res.Reason, "15") {
		t.Fatalf("the run ended %s (%s), want a park on the iron: %v", res.Outcome, res.Reason, r.log)
	}
	if len(mined) != 0 {
		t.Errorf("mined %v at Mining 14 with only iron owed", mined)
	}
	if parked, why := e.Store.Parked(questplan.DoricsQuestID, time.Now().Add(5*time.Hour)); !parked || !strings.Contains(why, "iron") {
		t.Errorf("not parked for hours: %v %q", parked, why)
	}
	if parked, _ := e.Store.Parked(questplan.DoricsQuestID, time.Now().Add(questplan.DoricIronParkFor+time.Hour)); parked {
		t.Error("parked past the iron park")
	}
}

// A miner that gains nothing is not retried for ever: the watchdog parks the quest after the same snapshot eight times.
func TestDoricsQuestParksWhenTheMinerGainsNothing(t *testing.T) {
	r := newFillerRig(t, questplan.DoricStarted, map[int]int{1265: 1}, nil)
	stubs := r.notingProvision(questplan.KitDoricOres, questplan.KitDoricPick)
	stubs["stub:custom"] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.stubLog("the rocks are all taken")
		return false
	}
	q := withMining(r.wrap(DoricsQuest(), stubs, questplan.DoricComplete), 1)
	res := r.engine().Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s), want the watchdog's park: %v", res.Outcome, res.Reason, r.log)
	}
}

// ---- Gertrude's Cat ----

func (r *fillerRig) fluffsStubs(fees *int) map[string]questrunner.CustomFunc {
	stubs := r.notingProvision(questplan.KitFluffsCoins, questplan.KitFluffsItems, questplan.KitFluffsSard, questplan.KitFluffsBucket, questplan.KitFluffsKitten)
	stubs["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch r.stage {
		case questplan.FluffsNotStarted:
			r.stage = questplan.FluffsStarted
		case questplan.FluffsRescued:
			r.stage = questplan.FluffsComplete
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:grabGround"] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.give(questplan.FluffsDoogleLeaves, 1)
		return true
	}
	stubs["stub:custom"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch {
		case reasonIs(st, questplan.CustomFluffsPay):
			if !r.take(questplan.CoinsID, questplan.FluffsBoyFee) {
				return false
			}
			*fees++
			r.stage = questplan.FluffsPaidBoy
		case reasonIs(st, questplan.CustomFluffsSeason):
			if !r.take(questplan.FluffsDoogleLeaves, 1) || !r.take(questplan.FluffsRawSardine, 1) {
				return false
			}
			r.give(questplan.FluffsSeasonedSardine, 1)
		case reasonIs(st, questplan.CustomFluffsMilk):
			if !r.take(questplan.ObjBucket, 1) {
				return false
			}
			r.give(questplan.ObjBucketOfMilk, 1)
		case reasonIs(st, questplan.CustomFluffsOffer):
			switch {
			case st.ItemID == questplan.ObjBucketOfMilk && r.stage == questplan.FluffsPaidBoy && r.take(st.ItemID, 1):
				r.give(questplan.ObjBucket, 1)
				r.stage = questplan.FluffsGaveMilk
			case st.ItemID == questplan.FluffsSeasonedSardine && r.stage == questplan.FluffsGaveMilk && r.take(st.ItemID, 1):
				r.stage = questplan.FluffsGaveSardine
			case st.ItemID == questplan.FluffsKitten && r.stage == questplan.FluffsGaveSardine && r.take(st.ItemID, 1):
				r.stage = questplan.FluffsRescued
			default:
				return false
			}
		case reasonIs(st, questplan.CustomFluffsCrates):
			if r.stage != questplan.FluffsGaveSardine || r.bank[questplan.FluffsKitten] > 0 {
				return false // the crates give nothing at the wrong stage or while a copy is in the bank
			}
			r.give(questplan.FluffsKitten, 1)
		case reasonIs(st, questplan.CustomFluffsReport):
			r.stage = questplan.FluffsComplete
		default:
			r.stubLog("unexpected custom step %q", st.Reason)
			return false
		}
		r.stubLog("%s -> stage %d", strings.SplitN(st.Reason, " ", 2)[0], r.stage)
		return true
	}
	return stubs
}

func (r *fillerRig) fluffsQuest(fees *int) questrunner.Quest {
	return r.wrap(GertrudesCat(), r.fluffsStubs(fees), questplan.FluffsComplete)
}

func TestGertrudesCatRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, 0, map[int]int{questplan.CoinsID: 200}, nil)
	fees := 0
	q := r.fluffsQuest(&fees)
	if q.ID() != questplan.GertrudesCatID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.FluffsComplete || fees != 1 || r.held(questplan.CoinsID) != 100 {
		t.Errorf("stage %d, %d fee(s), %d coins left of 200", r.stage, fees, r.held(questplan.CoinsID))
	}
	if cs := e.Store.Record(questplan.GertrudesCatID).Completions; len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	// The order of the chain: the start, the brothers, the bank and the mule once, the leaves behind Gertrude's house, the sardine
	// from Port Sarim, the seasoning, the bucket and the milk, then the cat's three gifts in their stages with the crates between
	// the sardine and the kitten, and Gertrude last.
	want := []string{"talk to Gertrude", "fluffs-pay", "provision fluffs-items", "grab Doogle leaves", "provision fluffs-sardine", "fluffs-season",
		"provision fluffs-bucket", "fluffs-milk", "give Fluffs the bucket of milk", "give Fluffs the seasoned sardine", "provision fluffs-kitten",
		"search the six crates", "give Fluffs her kitten", "fluffs-report", "[fluffs] COMPLETED"}
	at := -1
	for _, w := range want {
		i := strings.Index(joined[at+1:], w)
		if i < 0 {
			t.Fatalf("the log has no %q after position %d:\n%s", w, at, joined)
		}
		at += 1 + i
	}
	if strings.Count(joined, "stub provision fluffs-items") != 1 {
		t.Errorf("the bank and mule pass ran more than once:\n%s", joined)
	}
}

// An account the trainer left at stage 3 with the seasoned sardine in the bank draws it and goes straight on: no leaves, no
// Port Sarim, no milk.
func TestGertrudesCatResumesFromABankedSardine(t *testing.T) {
	r := newFillerRig(t, questplan.FluffsGaveMilk, nil, map[int]int{questplan.FluffsSeasonedSardine: 1})
	fees := 0
	res := r.engine().Run(r.fluffsQuest(&fees), 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	for _, absent := range []string{"fluffs-pay", "grab Doogle leaves", "fluffs-sardine", "fluffs-season", "fluffs-bucket", "fluffs-milk", "give Fluffs the bucket"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q, a source the bank made unnecessary:\n%s", absent, joined)
		}
	}
	if !strings.Contains(joined, "give Fluffs the seasoned sardine") || fees != 0 {
		t.Errorf("fees %d:\n%s", fees, joined)
	}
}

// The kitten the trainer banked is drawn before the crates are searched (they give none while a copy is in the bank).
func TestGertrudesCatDrawsABankedKittenBeforeTheCrates(t *testing.T) {
	r := newFillerRig(t, questplan.FluffsGaveSardine, nil, map[int]int{questplan.FluffsKitten: 1})
	fees := 0
	res := r.engine().Run(r.fluffsQuest(&fees), 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	if strings.Contains(joined, "search the six crates") {
		t.Errorf("the crates were searched with a kitten in the bank:\n%s", joined)
	}
	if !strings.Contains(joined, "provision fluffs-kitten") || !strings.Contains(joined, "give Fluffs her kitten") {
		t.Errorf("the kitten was not drawn and given:\n%s", joined)
	}
}

// An account with no coins at the brothers' stage and none in the bank or on the mule parks for hours with that said.
func TestGertrudesCatWithoutCoinsParks(t *testing.T) {
	r := newFillerRig(t, questplan.FluffsStarted, nil, nil)
	fees := 0
	e := r.engine()
	res := e.Run(r.fluffsQuest(&fees), 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "coins") {
		t.Fatalf("the run ended %s (%s), want a park on the coins: %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := e.Store.Parked(questplan.GertrudesCatID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
}

// A payment that never goes through is not retried for ever: the watchdog parks the quest on the same snapshot eight times.
func TestGertrudesCatParksWhenTheBrothersNeverTake100Coins(t *testing.T) {
	r := newFillerRig(t, questplan.FluffsStarted, map[int]int{questplan.CoinsID: 150}, nil)
	stubs := r.notingProvision(questplan.KitFluffsCoins)
	stubs["stub:custom"] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.stubLog("the brothers drifted apart")
		return false
	}
	res := r.engine().Run(r.wrap(GertrudesCat(), stubs, questplan.FluffsComplete), 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s), want the watchdog's park: %v", res.Outcome, res.Reason, r.log)
	}
}

// Every custom step the module can ask for has an executor in the quest, and nothing is registered that the module never asks
// for.
func TestTheQuestsRegisterEveryCustomStepTheirModulesAskFor(t *testing.T) {
	asked := map[string]map[string]bool{questplan.DoricsQuestID: {}, questplan.GertrudesCatID: {}}
	for _, mining := range []int{1, 15} {
		for mask := 0; mask < 1<<9; mask++ {
			for stage := -1; stage <= questplan.FluffsComplete; stage++ {
				ids := []int{questplan.DoricClay, questplan.DoricCopperOre, questplan.DoricIronOre, 1265, questplan.FluffsDoogleLeaves,
					questplan.FluffsRawSardine, questplan.FluffsSeasonedSardine, questplan.ObjBucketOfMilk, questplan.ObjBucket}
				snap := questplan.Snapshot{Journal: questplan.StatusInProgress, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20, BankKnown: true,
					Levels: map[string]int{"mining": mining}, Failed: map[string]bool{}, Ran: map[string]bool{"provision:" + questplan.KitDoricOres: true,
						"provision:" + questplan.KitFluffsItems: true, "provision:" + questplan.KitFluffsKitten: true}}
				if stage >= 0 {
					snap.Stage, snap.StageKnown = stage, true
				}
				for i, id := range ids {
					if mask&(1<<i) != 0 {
						snap.InvIDs[id] = 1
					}
				}
				snap.InvIDs[questplan.FluffsKitten] = mask & 1
				snap.InvIDs[questplan.CoinsID] = 150
				if st := questplan.DecideDoricsQuest(snap); st.Kind == questplan.StepCustom {
					asked[questplan.DoricsQuestID][st.Custom] = true
				}
				if st := questplan.DecideGertrudesCat(snap); st.Kind == questplan.StepCustom {
					asked[questplan.GertrudesCatID][st.Custom] = true
				}
			}
		}
	}
	for id, q := range map[string]questrunner.Quest{questplan.DoricsQuestID: DoricsQuest(), questplan.GertrudesCatID: GertrudesCat()} {
		for name := range asked[id] {
			if q.Customs[name] == nil {
				t.Errorf("%s asks for the custom step %q and registers no executor", id, name)
			}
		}
		for name := range q.Customs {
			if !asked[id][name] {
				t.Errorf("%s registers %q, which its module never asks for", id, name)
			}
		}
		if len(asked[id]) == 0 {
			t.Errorf("%s asked for no custom step at all", id)
		}
	}
}
