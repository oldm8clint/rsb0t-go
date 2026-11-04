package questmods

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The two quest-point fillers through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells
// the module its kits ran, the completion record), with every step's EXECUTOR stubbed. Each step the module decides is
// turned into a custom step whose stub plays the content's own rule for it on a simulated pack (actionsim): the
// provisioner's kits through the real questplan.PlanProvision over that pack and a simulated bank, the talks as the
// scripts answer them. The module's Decide is the real one; only the quest tab's colour and the journal stage, which no
// simulated interface can supply, are read from the rig. The executors the module relies on live (the dialogue driver, the
// walker, the fight loop) are the engine's own and have their own tests.

type fillerRig struct {
	t      *testing.T
	sim    *actionsim.Session
	bank   map[int]int
	stage  int
	log    []string
	picked int
}

func newFillerRig(t *testing.T, stage int, pack, bank map[int]int) *fillerRig {
	r := &fillerRig{t: t, stage: stage, bank: map[int]int{}}
	r.sim = actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID || id == questplan.ObjWoadLeaf }}, nil)
	for id, n := range pack {
		r.sim.Give(int32(id), n)
	}
	for id, n := range bank {
		r.bank[id] = n
	}
	return r
}

func (r *fillerRig) held(id int) int {
	n := 0
	for _, row := range r.sim.Pack() {
		if int(row.ID) == id {
			n += row.Count
		}
	}
	return n
}

func (r *fillerRig) pack() map[int]int {
	out := map[int]int{}
	for _, row := range r.sim.Pack() {
		out[int(row.ID)] += row.Count
	}
	return out
}

func (r *fillerRig) give(id, n int) { r.sim.Give(int32(id), n) }

func (r *fillerRig) take(id, n int) bool {
	if r.held(id) < n {
		return false
	}
	r.sim.Remove(int32(id), n)
	return true
}

func (r *fillerRig) engine() *questrunner.Engine {
	d := &questrunner.Deps{
		Session: r.sim, Components: actionsim.Components(),
		Log:   func(s string) { r.log = append(r.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	return &questrunner.Engine{D: d, Prov: &questrunner.Provisioner{Bank: &questrunner.BankView{}}, Store: questplan.NewStore(),
		Account: "Test Player", Build: strings.Repeat("a", 40)}
}

// provision runs the real planner over the rig: the bank's lines are drawn, a source task's quantity arrives from it.
func (r *fillerRig) provision(kit []questplan.KitLine) bool {
	for pass := 0; pass < 16; pass++ {
		pack := r.pack()
		used := 0
		for id, n := range pack {
			if id == questplan.CoinsID || id == questplan.ObjWoadLeaf {
				used++
			} else {
				used += n
			}
		}
		plan := questplan.PlanProvision(kit, questplan.ProvisionState{Pack: pack, Bank: r.bank, BankKnown: true, FreeSlots: 28 - used,
			Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case questplan.PhaseSatisfied:
			return true
		case questplan.PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := min(c.Count, r.bank[c.ID])
				if n > 0 {
					r.bank[c.ID] -= n
					r.give(c.ID, n)
					moved = true
				}
			}
			if !moved {
				return false
			}
		case questplan.PhaseSource:
			task := plan.Tasks[0]
			r.give(task.Line.ID, task.Qty)
			if task.Line.ID == questplan.ObjCadavaBerries {
				r.picked += task.Qty
			}
		default:
			return false
		}
	}
	return false
}

// wrap turns a module into one whose steps are stubs: the real Decide over a snapshot that carries the rig's journal.
func (r *fillerRig) wrap(q questrunner.Quest, stubs map[string]questrunner.CustomFunc, done int) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case r.stage == 0:
			s.Journal, s.StageKnown = questplan.StatusNotStarted, false
		case r.stage >= done:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, r.stage, true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		st.Reason = st.Describe() // the engine logs a custom step as its name and reason: keep what the step really is
		switch st.Kind {
		case questplan.StepProvision:
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		default:
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

func (r *fillerRig) provisionStub(names ...string) map[string]questrunner.CustomFunc {
	m := map[string]questrunner.CustomFunc{}
	for _, n := range names {
		m["provision:"+n] = func(e *questrunner.Engine, st questplan.Step) bool {
			r.log = append(r.log, "stub provision "+st.KitName)
			return r.provision(st.Kit)
		}
	}
	return m
}

func (r *fillerRig) stubLog(format string, args ...any) {
	r.log = append(r.log, "stub "+fmt.Sprintf(format, args...))
}

// ---- Goblin Diplomacy ----

func (r *fillerRig) gobdipStubs() map[string]questrunner.CustomFunc {
	stubs := r.provisionStub(questplan.KitGobdipPantry, questplan.KitGobdipBerries, questplan.KitGobdipMail)
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.Stop.ID {
		case questplan.NpcRustyAnchorBartender:
			r.stage = questplan.GobdipStarted
		case questplan.NpcWyson:
			if !r.take(questplan.CoinsID, questplan.GobdipWoadFee) {
				return false
			}
			r.give(questplan.ObjWoadLeaf, 2)
		case questplan.NpcAggie:
			dye, ing, n := questplan.ObjBlueDye, questplan.ObjWoadLeaf, 2
			switch {
			case strings.Contains(st.Stop.Prefer[1], "red dye"):
				dye, ing, n = questplan.ObjRedDye, questplan.ObjRedberries, 3
			case strings.Contains(st.Stop.Prefer[1], "yellow dye"):
				dye, ing, n = questplan.ObjYellowDye, questplan.ObjOnion, 2
			}
			if !r.take(ing, n) || !r.take(questplan.CoinsID, 5) {
				return false
			}
			r.give(dye, 1)
		case questplan.NpcGeneralWartface:
			switch r.stage {
			case questplan.GobdipStarted:
				r.stage = questplan.GobdipWillBringArmour
			case questplan.GobdipWillBringArmour:
				if r.take(questplan.ObjGoblinMailOrange, 1) {
					r.stage = questplan.GobdipGaveOrange
				}
			case questplan.GobdipGaveOrange:
				if r.take(questplan.ObjGoblinMailBlue, 1) {
					r.stage = questplan.GobdipGaveBlue
				}
			case questplan.GobdipGaveBlue:
				if r.take(questplan.ObjGoblinMail, 1) {
					r.stage = questplan.GobdipComplete
				}
			}
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:pickLoc"] = func(e *questrunner.Engine, st questplan.Step) bool { r.give(questplan.ObjOnion, 1); return true }
	stubs["stub:useOn"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch {
		case st.ItemID == questplan.ObjRedDye:
			if !r.take(questplan.ObjRedDye, 1) || !r.take(questplan.ObjYellowDye, 1) {
				return false
			}
			r.give(questplan.ObjOrangeDye, 1)
		case st.TargetID == questplan.ObjGoblinMail:
			out := questplan.ObjGoblinMailBlue
			if st.ItemID == questplan.ObjOrangeDye {
				out = questplan.ObjGoblinMailOrange
			}
			if !r.take(st.ItemID, 1) || !r.take(questplan.ObjGoblinMail, 1) {
				return false
			}
			r.give(out, 1)
		}
		return true
	}
	return stubs
}

func TestGoblinDiplomacyRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, 0, map[int]int{questplan.CoinsID: 100}, nil)
	q := r.wrap(GoblinDiplomacy(), r.gobdipStubs(), questplan.GobdipComplete)
	if q.ID() != questplan.GoblinDiplomacyID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.GobdipComplete {
		t.Errorf("stage %d at the end", r.stage)
	}
	cs := e.Store.Record(questplan.GoblinDiplomacyID).Completions
	if len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	for _, want := range []string{
		"talk to Bartender", "provision gobdip-pantry", "provision gobdip-berries", "Pick Onion",
		"talk to Wyson the gardener", "talk to Aggie", "use Red dye on Yellow dye", "provision gobdip-mail",
		"use Orange dye on Goblin mail", "use Blue dye on Goblin mail", "talk to General Wartface", "[gobdip] COMPLETED",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log has no %q:\n%s", want, joined)
		}
	}
	// The order the geography wants, as the engine logged it: Aggie's visit before the mail farm, the farm before the generals.
	idx := func(s string) int { return strings.Index(joined, s) }
	if !(idx("talk to Aggie") < idx("provision gobdip-mail") && idx("provision gobdip-mail") < idx("talk to General Wartface")) {
		t.Errorf("the quest's order is wrong:\n%s", joined)
	}
	if got := r.held(questplan.CoinsID); got != 100-20-15 {
		t.Errorf("%d coins left of 100", got)
	}
}

// A half-done account whose trainer banked its dyes is not made to do them again: the pantry draws them, and the engine
// goes straight to the mail and the generals.
func TestGoblinDiplomacyResumesFromAHalfDoneBank(t *testing.T) {
	r := newFillerRig(t, questplan.GobdipWillBringArmour, nil, map[int]int{questplan.ObjGoblinMailOrange: 1, questplan.ObjBlueDye: 1, questplan.ObjGoblinMail: 2})
	q := r.wrap(GoblinDiplomacy(), r.gobdipStubs(), questplan.GobdipComplete)
	res := r.engine().Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	for _, absent := range []string{"talk to Aggie", "talk to Wyson", "provision gobdip-berries", "provision gobdip-mail", "Pick Onion"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q, a source the bank made unnecessary:\n%s", absent, joined)
		}
	}
}

// A quest with nothing to buy the woad with waits, and the engine parks it rather than loop.
func TestGoblinDiplomacyWithoutCoinsParksInsteadOfLooping(t *testing.T) {
	r := newFillerRig(t, questplan.GobdipGaveOrange, nil, nil)
	q := r.wrap(GoblinDiplomacy(), r.gobdipStubs(), questplan.GobdipComplete)
	res := r.engine().Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Wyson") {
		t.Fatalf("the run ended %s (%s), want a park on Wyson's fee: %v", res.Outcome, res.Reason, r.log)
	}
}

// ---- Romeo & Juliet ----

func (r *fillerRig) romeoStubs() map[string]questrunner.CustomFunc {
	stubs := r.provisionStub(questplan.KitRomeoMessage, questplan.KitRomeoPotion)
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.Stop.ID {
		case questplan.NpcRomeo:
			switch r.stage {
			case questplan.RomeoNotStarted:
				r.stage = questplan.RomeoSpokeRomeo
			case questplan.RomeoSpokeJuliet:
				if r.take(questplan.ObjJulietMessage, 1) {
					r.stage = questplan.RomeoPassedMessage
				}
			case questplan.RomeoJulietCrypt:
				r.stage = questplan.RomeoComplete
			}
		case questplan.NpcJuliet:
			switch r.stage {
			case questplan.RomeoSpokeRomeo:
				r.give(questplan.ObjJulietMessage, 1)
				r.stage = questplan.RomeoSpokeJuliet
			case questplan.RomeoSpokeJuliet:
				if r.held(questplan.ObjJulietMessage) == 0 && r.bank[questplan.ObjJulietMessage] == 0 {
					r.give(questplan.ObjJulietMessage, 1)
				}
			case questplan.RomeoSpokeApothecary:
				if r.take(questplan.ObjCadavaPotion, 1) {
					r.stage = questplan.RomeoJulietCrypt
				}
			}
		case questplan.NpcFatherLawrence:
			if r.stage == questplan.RomeoPassedMessage {
				r.stage = questplan.RomeoSpokeFather
			}
		case questplan.NpcApothecary:
			switch r.stage {
			case questplan.RomeoSpokeFather:
				r.stage = questplan.RomeoSpokeApothecary
			case questplan.RomeoSpokeApothecary:
				if r.held(questplan.ObjCadavaPotion) == 0 && r.take(questplan.ObjCadavaBerries, 1) {
					r.give(questplan.ObjCadavaPotion, 1)
				}
			}
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	return stubs
}

func TestRomeoAndJulietRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, 0, nil, nil)
	q := r.wrap(RomeoJuliet(), r.romeoStubs(), questplan.RomeoComplete)
	if q.ID() != questplan.RomeoJulietID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.RomeoComplete || r.picked != 1 {
		t.Errorf("stage %d, %d berries picked", r.stage, r.picked)
	}
	if cs := e.Store.Record(questplan.RomeoJulietID).Completions; len(cs) != 1 || !cs[0].Earned() {
		t.Errorf("completions %+v", cs)
	}
	joined := strings.Join(r.log, "\n")
	for _, want := range []string{"talk to Romeo", "talk to Juliet", "talk to Father Lawrence", "talk to Apothecary",
		"provision romeo-potion", "[romeo] COMPLETED"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log has no %q:\n%s", want, joined)
		}
	}
	// The message was in the pack when Juliet gave it, so the bank is never read for it.
	if strings.Contains(joined, "provision romeo-message") {
		t.Errorf("the message kit ran with the message in the pack:\n%s", joined)
	}
}

// The trainer banked the message: the engine reads the bank before it asks Juliet, who would give nothing.
func TestRomeoAndJulietDrawsABankedMessageThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, questplan.RomeoSpokeJuliet, nil, map[int]int{questplan.ObjJulietMessage: 1})
	q := r.wrap(RomeoJuliet(), r.romeoStubs(), questplan.RomeoComplete)
	res := r.engine().Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	m, j := strings.Index(joined, "provision romeo-message"), strings.Index(joined, "talk to Juliet")
	if m < 0 || (j >= 0 && j < m) {
		t.Errorf("Juliet was asked before the bank was read:\n%s", joined)
	}
}
