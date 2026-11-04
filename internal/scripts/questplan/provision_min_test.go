package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Fletching 25 rung's kit, live 15 Oct 2026: 26 logs and a knife for an account whose run staller's kit (runes, a
// ring, a necklace: five slots) rides through every bank visit. The bank came up three logs short, "the pack filled",
// for sixteen passes, and the rung parked 16 times of 16.
const (
	testFletchKnife = 946
	testFletchLogs  = 1511
)

func fletchTestKit(min int) []KitLine {
	return []KitLine{
		{ID: testFletchKnife, Name: "Knife", Qty: 1, Sources: []Source{{Kind: SourceGround, Tile: tile.Tile{X: 2704, Z: 3475}, RespawnTicks: 100}}},
		{ID: testFletchLogs, Name: "Logs", Qty: 26, Min: min},
	}
}

// packRig is a 28-slot pack: the staller's kit stays through a deposit (Deps.AtBank draws it back), the other
// things the account carries are banked, and a withdrawal stops where the pack is full.
type packRig struct {
	staller, other int
	pack, bank     map[int]int
	mule           map[int]int
	muleAsked      bool
}

func (r *packRig) used() int {
	n := r.staller + r.other
	for _, c := range r.pack {
		n += c // every kit item here is unstackable
	}
	return n
}

// run drives PlanProvision the way questrunner.Provision does for up to 16 passes and reports the last plan.
func (r *packRig) run(kit []KitLine) (final ProvisionPlan, passes int) {
	exhausted := map[string]bool{}
	bankRead := false
	for passes = 1; passes <= 16; passes++ {
		st := ProvisionState{
			Pack: r.pack, Bank: r.bank, BankKnown: bankRead, Mule: r.mule, MuleKnown: true, MuleAsked: r.muleAsked,
			// What a quest deposit frees is the other things only: the staller's kit is left in the pack or drawn back.
			FreeSlots: 28 - r.used(), Depositable: r.other, Exhausted: exhausted,
			Levels: map[string]int{"woodcutting": 40},
		}
		final = PlanProvision(kit, st)
		switch final.Phase {
		case PhaseSatisfied, PhaseBlocked:
			return
		case PhaseBank:
			r.other = 0
			bankRead = true
			for _, w := range final.Withdraw {
				take := minInt(w.Count, minInt(r.bank[w.ID], 28-r.used()))
				r.bank[w.ID] -= take
				r.pack[w.ID] += take
			}
		case PhaseMule:
			r.muleAsked = true
			for _, a := range final.MuleAsk {
				take := minInt(a.Count, 28-r.used())
				r.pack[a.ID] += take
			}
		case PhaseSource:
			for _, task := range final.Tasks {
				exhausted[SourceKey(task.Line, task.Source)] = true // no spawn is up
			}
		}
	}
	passes = 16
	return
}

func liveFletchRig() *packRig {
	return &packRig{staller: 5, other: 6, pack: map[int]int{}, bank: map[int]int{testFletchLogs: 3000}, mule: map[int]int{testFletchKnife: 186, testFletchLogs: 7000}}
}

func TestAFlexibleLogLineFillsTheRoomThePackHasAndEndsSatisfied(t *testing.T) {
	r := liveFletchRig()
	plan, passes := r.run(fletchTestKit(1))
	if plan.Phase != PhaseSatisfied {
		t.Fatalf("ended %s after %d passes (%s), want satisfied", plan.Phase, passes, plan.Summary())
	}
	if r.pack[testFletchKnife] != 1 || r.pack[testFletchLogs] < 20 || r.used() > 28 {
		t.Fatalf("pack %v with %d slots used: want the knife and a pack of logs", r.pack, r.used())
	}
	if passes > 4 {
		t.Errorf("%d passes: the bank, the mule and done should take 3", passes)
	}
}

// The same rig and the old kit (26 logs, no floor) is the bug: the plan keeps asking the bank for the logs that do not
// fit and never reaches the knife.
func TestAnInflexibleLogLineNeverEndsInAPackTheStallerSharesWith(t *testing.T) {
	r := liveFletchRig()
	plan, passes := r.run(fletchTestKit(0))
	if plan.Phase == PhaseSatisfied {
		t.Fatalf("satisfied after %d passes with pack %v: the fixture no longer reproduces the stall", passes, r.pack)
	}
	if plan.Phase != PhaseBank || passes != 16 || r.pack[testFletchKnife] != 0 {
		t.Fatalf("ended %s after %d passes with pack %v, want the bank loop of 16 passes with no knife", plan.Phase, passes, r.pack)
	}
}

// With the knife taken from the ground rather than the mule, the logs may not take the slot it needs.
func TestAFlexibleLineLeavesTheRoomAGroundItemNeeds(t *testing.T) {
	r := liveFletchRig()
	st := ProvisionState{Pack: r.pack, Bank: r.bank, BankKnown: true, MuleKnown: true, Mule: map[int]int{}, FreeSlots: 22, Depositable: 0,
		Exhausted: map[string]bool{}}
	p := PlanProvision(fletchTestKit(1), st)
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != testFletchLogs || p.Withdraw[0].Count != 21 {
		t.Fatalf("plan %s, want 21 logs (22 free less the knife's slot)", p.Summary())
	}
}

// Once the floor is held, a flexible line is not bought, worked or asked of the mule for: it is satisfied.
func TestAFlexibleLineAtItsFloorIsNotWorkedFor(t *testing.T) {
	line := KitLine{ID: testFletchLogs, Name: "Logs", Qty: 26, Min: 1, Sources: []Source{{Kind: SourceSkill, Skill: "woodcutting", Level: 1, Method: "chop-logs", Tile: tile.Tile{X: 2730, Z: 3486}}}}
	st := ProvisionState{Pack: map[int]int{testFletchLogs: 5}, Bank: map[int]int{}, BankKnown: true, Mule: map[int]int{testFletchLogs: 7000}, MuleKnown: true,
		FreeSlots: 20, Exhausted: map[string]bool{}, Levels: map[string]int{"woodcutting": 40}}
	if p := PlanProvision([]KitLine{line}, st); p.Phase != PhaseSatisfied {
		t.Fatalf("5 logs with a floor of 1 and an empty bank: %s, want satisfied", p.Summary())
	}
	// With the floor not yet met the line is worked for like any other, but only for what fits.
	st.Pack = map[int]int{}
	st.Mule = map[int]int{}
	p := PlanProvision([]KitLine{line}, st)
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Qty != 20 {
		t.Fatalf("no logs anywhere: %s, want to chop the 20 the pack holds", p.Summary())
	}
}

// A top-up from the bank still happens while the floor is held and the bank has more.
func TestAFlexibleLineAtItsFloorIsToppedUpFromTheBank(t *testing.T) {
	line := KitLine{ID: testFletchLogs, Name: "Logs", Qty: 26, Min: 1}
	st := ProvisionState{Pack: map[int]int{testFletchLogs: 5}, Bank: map[int]int{testFletchLogs: 100}, BankKnown: true,
		FreeSlots: 20, Exhausted: map[string]bool{}}
	p := PlanProvision([]KitLine{line}, st)
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].Count != 20 {
		t.Fatalf("plan %s, want 20 more logs from the bank", p.Summary())
	}
}

// A line that only serves another (the axe for chopping the logs) is wanted while the logs are below their floor, and not
// once they are held.
func TestAnAxeServingAFlexibleLineIsWantedOnlyWhileTheLogsAreShort(t *testing.T) {
	logs := KitLine{ID: testFletchLogs, Name: "Logs", Qty: 26, Min: 1}
	axe := KitLine{ID: ObjIronAxe, Name: "Iron axe", Qty: 1, OnlyIfShort: testFletchLogs, Sources: []Source{{Kind: SourceShop, Shop: AemadShop, Price: 56}}}
	st := ProvisionState{Pack: map[int]int{testFletchLogs: 3}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, Exhausted: map[string]bool{}}
	if p := PlanProvision([]KitLine{logs, axe}, st); p.Phase != PhaseSatisfied {
		t.Fatalf("logs held: %s, want satisfied with no axe bought", p.Summary())
	}
	st.Pack = map[int]int{CoinsID: 1000}
	st.Mule, st.MuleKnown = map[int]int{}, true
	p := PlanProvision([]KitLine{logs, axe}, st)
	if len(p.Short) != 2 {
		t.Fatalf("no logs: short %v, want the logs and the axe that fells them", p.Short)
	}
}

func TestAKitLineMinimumMustFitItsQuantity(t *testing.T) {
	if why := ValidateKitLine(KitLine{ID: 1, Name: "x", Qty: 5, Min: 6}); why == "" {
		t.Error("a minimum above the quantity validated")
	}
	if why := ValidateKitLine(KitLine{ID: 1, Name: "x", Qty: 5, Min: -1}); why == "" {
		t.Error("a negative minimum validated")
	}
	if why := ValidateKitLine(KitLine{ID: 1, Name: "x", Qty: 5, Min: 1}); why != "" {
		t.Errorf("a minimum of 1 in 5 is refused: %s", why)
	}
}
