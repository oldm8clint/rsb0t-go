package questmods

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The Prayer rungs' bones step end to end on the simulated game (actionsim): the real bank code (deposit, read, withdraw), the real
// burying (OPHELD1 on the real item config's "Bury" op, scripted to advance Prayer as content's bury_bones does), the real
// bones loop and the real Engine hooks, over a mule whose published bank is keyed as the live mule's is. Only the walk to a
// booth and the mule's trade are replaced. Skipped without RSB0T_CONFIG_JAG.

const (
	simPrayerStat = 5 // gamestate.SkillName(5) is Prayer
	simCoins      = 995
	simShark      = 385
)

type simMule struct {
	sim     *actionsim.Session
	pub     map[int]int // what Published returns, keyed as donorMuleAsker.Published keys the live mule
	stock   map[int]int // what the mule really holds, by the object id the bank holds it under
	asked   [][]questplan.ItemCount
	nothing bool
}

func (m *simMule) Published() (map[int]int, bool) { return m.pub, true }

// Ask plays the kit bundle: a pack's worth of each line arrives as the item, the rest is banked at the post (the un-note).
func (m *simMule) Ask(_ string, lines []questplan.ItemCount) (map[int]int, string) {
	m.asked = append(m.asked, append([]questplan.ItemCount(nil), lines...))
	if m.nothing {
		return map[int]int{}, "the mule's line is too long"
	}
	got := map[int]int{}
	rows := m.sim.Bank()
	free := actions.BackpackCapacity - len(m.sim.Pack())
	for _, l := range lines {
		n := min(l.Count, m.stock[l.ID])
		m.stock[l.ID] -= n
		inPack := min(n, free)
		free -= inPack
		if inPack > 0 {
			m.sim.Give(int32(l.ID), inPack)
		}
		if rest := n - inPack; rest > 0 {
			rows = append(rows, actionsim.Stack{ID: int32(l.ID), Count: rest})
		}
		got[l.ID] = inPack
	}
	m.sim.OpenBank(rows)
	return got, ""
}

type boneRig struct {
	sim  *actionsim.Session
	e    *questrunner.Engine
	mule *simMule
	logs []string
	open func() (*actions.Bank, bool)
}

// newBoneRig is an account at a bank with coins and a shark in its pack, Prayer at startXP, the given bank rows, and a mule that
// publishes pub and holds stock.
func newBoneRig(t *testing.T, startXP int, bank []actionsim.Stack, pub, stock map[int]int) *boneRig {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable }},
		[]actionsim.Stack{{ID: simCoins, Count: 500}, {ID: simShark, Count: 3}})
	sim.ShowTabs()
	sim.StandAt(questplan.CatherbyBank.X, questplan.CatherbyBank.Z)
	sim.OpenBank(bank)
	sim.Bury(simPrayerStat, startXP, map[int32]int{questplan.ObjBones: 45, questplan.ObjBigBones: 150, questplan.ObjDragonBones: 720})
	comps := actionsim.TabComponents()
	r := &boneRig{sim: sim}
	d := &questrunner.Deps{Session: sim, Config: cfg, Components: comps, Log: func(s string) { r.logs = append(r.logs, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	r.mule = &simMule{sim: sim, pub: pub, stock: stock}
	r.e = &questrunner.Engine{D: d, Prov: &questrunner.Provisioner{Bank: &questrunner.BankView{}, Mule: r.mule}}
	// The booth: the simulated bank opens again each time, as a booth would.
	r.open = func() (*actions.Bank, bool) {
		sim.OpenBank(sim.Bank())
		return actions.NewBank(sim, comps), true
	}
	return r
}

func (r *boneRig) xp() int {
	sk, _ := r.sim.State().Skill(simPrayerStat)
	return sk.XP
}

func (r *boneRig) bones() (pack, bank int) {
	for _, row := range r.sim.Pack() {
		if _, ok := questplan.BoneKindOf(int(row.ID)); ok {
			pack += row.Count
		}
	}
	for _, row := range r.sim.Bank() {
		if _, ok := questplan.BoneKindOf(int(row.ID)); ok {
			bank += row.Count
		}
	}
	return pack, bank
}

func (r *boneRig) logged(sub string) bool {
	for _, l := range r.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// The account of the task: Prayer 29 (3,133 xp short of 31), nothing in its bank, the live mule (Bones published under the members'
// obj 280, as donorMuleAsker keys them). It asks once for the big bones that cover the need, buries the pack's share where it stands,
// draws the rest of the kit from the bank a pack at a time and buries it: Prayer 31, no cow in sight.
func TestAPrayer31AccountBuriesTheMulesBigBonesToTheLevel(t *testing.T) {
	r := newBoneRig(t, 11_700, nil,
		map[int]int{280: 17154, questplan.ObjBigBones: 1488}, map[int]int{questplan.ObjBones: 17154, questplan.ObjBigBones: 1488})
	r.e.Provisioner().Bank.Note(map[int]int{simCoins: 500}) // read in an earlier run: no bones
	if !prayerBonesAt(r.e, "prayer31", 31, true, r.open) {
		t.Fatalf("the step did not reach the level: xp %d, log %v", r.xp(), r.logs)
	}
	if r.xp() < levels.XPAtLevel(31) {
		t.Errorf("Prayer xp %d, want at least %d", r.xp(), levels.XPAtLevel(31))
	}
	if len(r.mule.asked) != 1 || len(r.mule.asked[0]) != 1 || r.mule.asked[0][0].Name != "Big bones" || r.mule.asked[0][0].Count != 209 {
		t.Errorf("the mule was asked %+v, want one ask for 209 Big bones", r.mule.asked)
	}
	if pack, bank := r.bones(); pack != 0 || bank > 26 {
		t.Errorf("bones left: %d in the pack, %d banked (the last pack's overshoot at most)", pack, bank)
	}
	if got := r.mule.stock[questplan.ObjBigBones]; got != 1488-209 {
		t.Errorf("the mule has %d big bones left, want %d", got, 1488-209)
	}
	for _, want := range []string{"asking the mule", "burying", "to the bank", "prayer bones: +"} {
		if !r.logged(want) {
			t.Errorf("the log has no %q line: %v", want, r.logs)
		}
	}
	for _, l := range r.logs {
		if strings.Contains(l, "prayer bones") {
			t.Log(l)
		}
	}
}

// The own bank first: bones the account banked itself are buried before the mule is asked (it is not asked at all when they suffice).
func TestTheOwnBanksBonesAreBuriedBeforeAnyAsk(t *testing.T) {
	r := newBoneRig(t, 14_700, []actionsim.Stack{{ID: questplan.ObjBones, Count: 44}},
		map[int]int{280: 17154, questplan.ObjBigBones: 1488}, map[int]int{questplan.ObjBones: 17154, questplan.ObjBigBones: 1488})
	r.e.Provisioner().Bank.Note(map[int]int{questplan.ObjBones: 44}) // the hint says the bank holds bones
	if !prayerBonesAt(r.e, "prayer31", 31, true, r.open) {
		t.Fatalf("not at Prayer 31: xp %d, log %v", r.xp(), r.logs)
	}
	if len(r.mule.asked) != 0 {
		t.Errorf("the mule was asked %+v for a need the own bank covers", r.mule.asked)
	}
	if _, bank := r.bones(); bank != 44-30 { // 133 xp short: 30 bones (135 xp)
		t.Errorf("%d bones left in the bank, want %d", bank, 44-30)
	}
}

// A mule that gives nothing, and a rung that may not ask it, leave the step failed: the engine then sends the account to the cows.
func TestWithNoBoneTheStepFailsAndTheEngineGoesToTheCows(t *testing.T) {
	for _, c := range []struct {
		name     string
		nothing  bool
		askMule  bool
		wantAsks int
	}{{"the mule gives nothing", true, true, 1}, {"the rung does not ask the mule", false, false, 0}} {
		r := newBoneRig(t, 11_700, nil, map[int]int{280: 17154, questplan.ObjBigBones: 1488}, map[int]int{questplan.ObjBones: 17154, questplan.ObjBigBones: 1488})
		r.mule.nothing = c.nothing
		r.e.Provisioner().Bank.Note(map[int]int{simCoins: 500})
		if prayerBonesAt(r.e, "prayerkq", 37, c.askMule, r.open) {
			t.Errorf("%s: the step reported success with %d xp", c.name, r.xp())
		}
		if len(r.mule.asked) != c.wantAsks || r.xp() != 11_700 {
			t.Errorf("%s: asked %d time(s), xp %d", c.name, len(r.mule.asked), r.xp())
		}
		if !r.logged("cows:") {
			t.Errorf("%s: the log does not say it is leaving for the cows: %v", c.name, r.logs)
		}
	}
}

// An account that has not read its Prayer yet is not short of any level: nothing is asked.
func TestAnUnreadPrayerAsksNothing(t *testing.T) {
	r := newBoneRig(t, 0, nil, map[int]int{280: 17154, questplan.ObjBigBones: 1488}, map[int]int{questplan.ObjBones: 17154, questplan.ObjBigBones: 1488})
	// A fresh session: no UPDATE_STAT yet.
	r.e.D.Session = actionsim.New(actionsim.Objs{}, nil)
	if !prayerBonesAt(r.e, "prayerkq", 37, true, r.open) || len(r.mule.asked) != 0 {
		t.Errorf("asked %v for a skill nobody had read", r.mule.asked)
	}
}
