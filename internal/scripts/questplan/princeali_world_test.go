package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The account and the little of the world Prince Ali Rescue touches: enough to run DecidePrinceAli step by step to the end and see
// what it does on the way. Every step is applied as the content does (the scripts named in the comments), so a decision that
// asks for something the content would refuse, or that the account cannot do, fails the test where it is made. Provisioning runs
// the real PlanProvision over the world's pack, bank and mule.

type paWorld struct {
	t         *testing.T
	stage     int
	keystatus int // %prince_keystatus: 0 nothing, 1 the key is forged, 2 it was handed over
	inv       map[int]int
	bank      map[int]int
	mule      map[int]int
	ran       map[string]bool
	failed    map[string]bool
	log       []string
	tile      tile.Tile
	maxHP, hp int
	defence   int
	shops     map[string]int // shop sources used, by item name
	grounds   map[string]int
	keliBack  bool
}

func newPAWorld(t *testing.T, stage int, inv, bank, mule map[int]int) *paWorld {
	cp := func(m map[int]int) map[int]int {
		out := map[int]int{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	w := &paWorld{t: t, stage: stage, inv: cp(inv), bank: cp(bank), ran: map[string]bool{}, failed: map[string]bool{},
		tile: DraynorBank, maxHP: 50, hp: 50, defence: 40, shops: map[string]int{}, grounds: map[string]int{}}
	if mule != nil {
		w.mule = cp(mule)
	}
	return w
}

func (w *paWorld) snapshot() Snapshot {
	s := Snapshot{InvIDs: map[int]int{}, Inv: map[string]int{}, WornIDs: map[int]int{}, Worn: map[string]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Tile: w.tile, TileKnown: true,
		BankKnown: true, BankIDs: map[int]int{}, MaxHP: w.maxHP, HP: w.hp,
		Levels: map[string]int{"defence": w.defence, "attack": 40, "strength": 40, "hitpoints": w.maxHP}}
	used := 0
	for id, n := range w.inv {
		if n > 0 {
			s.InvIDs[id] = n
			if id == CoinsID {
				used++
			} else {
				used += n
			}
		}
	}
	for id, n := range w.bank {
		s.BankIDs[id] = n
	}
	s.FreeSlots = 28 - used
	for k, v := range w.ran {
		s.Ran[k] = v
	}
	for k, v := range w.failed {
		s.Failed[k] = v
	}
	switch {
	case w.stage == PrinceNotStarted:
		s.Journal = StatusNotStarted
	case w.stage >= PrinceComplete:
		s.Journal = StatusComplete
	default:
		s.Journal, s.Stage, s.StageKnown = StatusInProgress, w.stage, true
	}
	return s
}

// requireOdds fails the test when a visit to the compound is made by an account the odds model refuses: the decision must have
// fetched food or parked instead.
func (w *paWorld) requireOdds(st Step, ticks int) {
	if ok, why := PrinceVisitOdds(w.snapshot(), ticks); !ok {
		w.t.Fatalf("%s at stage %d for an account the model refuses: %s", st.Custom, w.stage, why)
	}
}

func (w *paWorld) take(id, n int) bool {
	if w.inv[id] < n {
		return false
	}
	w.inv[id] -= n
	return true
}

func (w *paWorld) logf(format string, args ...any) {
	w.log = append(w.log, fmt.Sprintf(format, args...))
}

// provision runs the real planner over the world: the bank's lines are drawn, the mule's one bundle is taken, a source task's
// quantity arrives from its ground spawn, skill or shop (a shop is paid for).
func (w *paWorld) provision(st Step) bool {
	asked := false
	for pass := 0; pass < 16; pass++ {
		s := w.snapshot()
		plan := PlanProvision(st.Kit, ProvisionState{Pack: s.InvIDs, Bank: w.bank, BankKnown: true, FreeSlots: s.FreeSlots,
			Mule: w.mule, MuleKnown: w.mule != nil, MuleAsked: asked, Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case PhaseSatisfied:
			return true
		case PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := minInt(c.Count, w.bank[c.ID])
				if n > 0 {
					w.bank[c.ID] -= n
					w.inv[c.ID] += n
					moved = true
				}
			}
			if !moved {
				return false
			}
		case PhaseMule:
			asked = true
			for _, c := range plan.MuleAsk {
				n := minInt(c.Count, w.mule[c.ID])
				w.mule[c.ID] -= n
				w.inv[c.ID] += n
			}
			w.logf("mule ask %s", describeCounts(plan.MuleAsk))
		case PhaseSource:
			task := plan.Tasks[0]
			switch task.Source.Kind {
			case SourceShop:
				cost := task.Source.Price * task.Qty
				if w.inv[CoinsID] < cost {
					return false
				}
				w.inv[CoinsID] -= cost
				w.shops[task.Line.Name] += task.Qty
			case SourceGround:
				w.grounds[task.Line.Name] += task.Qty
			default:
				w.t.Fatalf("source %v is not one this quest lists", task.Source.Kind)
			}
			w.inv[task.Line.ID] += task.Qty
		default:
			return false
		}
	}
	return false
}

// leela is leela.rs2's leela_help.
func (w *paWorld) leela() {
	if w.stage < PrinceSpokeOsman || w.stage >= PrinceSaved {
		return
	}
	if w.keystatus == 1 {
		w.inv[ObjPrinceKey]++
		w.keystatus = 2
		w.logf("leela gives the key")
	} else if w.keystatus == 2 && w.inv[ObjPrinceKey] == 0 && w.bank[ObjPrinceKey] == 0 && w.inv[CoinsID] >= PrinceKeyFee {
		w.inv[CoinsID] -= PrinceKeyFee
		w.inv[ObjPrinceKey]++
		w.logf("leela reissues the key")
	}
	if w.stage == PrinceSpokeOsman && w.inv[ObjPrinceKey] > 0 && w.inv[ObjBlondWig] > 0 && w.inv[ObjPinkSkirt] > 0 && w.inv[ObjSkinPaste] > 0 {
		w.stage = PrinceHasEquipment
		w.logf("stage 30")
	}
}

func (w *paWorld) talk(st Step) {
	t := w.t
	switch st.Stop.ID {
	case NpcHassan:
		switch w.stage {
		case PrinceNotStarted:
			w.stage = PrinceStarted
		case PrinceSaved:
			w.stage = PrinceComplete
			w.inv[CoinsID] += 700
		}
		w.logf("hassan -> %d", w.stage)
	case NpcOsman:
		if w.stage != PrinceStarted {
			t.Fatalf("Osman talked to at stage %d outside the key executor", w.stage)
		}
		w.stage = PrinceSpokeOsman
		w.logf("osman -> 20")
	case NpcLeela:
		w.leela()
	case NpcAggie:
		switch {
		case strings.Contains(st.Stop.Prefer[len(st.Stop.Prefer)-1], "skin paste"):
			// aggie_mix_paste: ash, flour, redberries and a bucket or jug of water, the bucket first.
			water := ObjJugOfWater
			if w.inv[ObjBucketOfWater] > 0 {
				water = ObjBucketOfWater
			}
			if st.UntilItemID != ObjSkinPaste || w.inv[ObjRedberries] < 1 || w.inv[ObjPotOfFlour] < 1 || w.inv[ObjAshes] < 1 || w.inv[water] < 1 {
				t.Fatalf("Aggie asked for the paste with berries %d flour %d ashes %d water %d (until %d)",
					w.inv[ObjRedberries], w.inv[ObjPotOfFlour], w.inv[ObjAshes], w.inv[water], st.UntilItemID)
			}
			w.inv[water]--
			w.inv[ObjRedberries]--
			w.inv[ObjPotOfFlour]--
			w.inv[ObjAshes]--
			w.inv[ObjSkinPaste]++
			w.logf("aggie paste")
		case strings.Contains(st.Stop.Prefer[1], "yellow dye"):
			if st.UntilItemID != ObjYellowDye || w.inv[ObjOnion] < PrinceOnions || w.inv[CoinsID] < PrinceDyeFee {
				t.Fatalf("Aggie asked for the yellow dye with %d onions and %d coins", w.inv[ObjOnion], w.inv[CoinsID])
			}
			w.inv[ObjOnion] -= PrinceOnions
			w.inv[CoinsID] -= PrinceDyeFee
			w.inv[ObjYellowDye]++
			w.logf("aggie dye")
		default:
			t.Fatalf("Aggie's talk is unreadable from %v", st.Stop.Prefer)
		}
	case NpcNed:
		// ned_wig: three balls of wool make a plain wig.
		if st.UntilItemID != ObjPlainWig || w.inv[ObjBallOfWool] < PrinceBalls {
			t.Fatalf("Ned asked for a wig with %d balls of wool (until %d)", w.inv[ObjBallOfWool], st.UntilItemID)
		}
		w.inv[ObjBallOfWool] -= PrinceBalls
		w.inv[ObjPlainWig]++
		w.logf("ned wig")
	case NpcLadyKeli:
		// The print is a visit to the compound with a top-up in front of it (CustomPrincePrint), never a plain talk.
		t.Fatalf("a plain talk to Lady Keli at stage %d: the print is priced and topped up by %s", w.stage, CustomPrincePrint)
	default:
		t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
	}
}

func (w *paWorld) useOn(st Step) {
	t := w.t
	switch {
	case st.ItemID == ObjYellowDye && st.TargetID == ObjPlainWig:
		// make_blonde_wig.
		if !w.take(ObjYellowDye, 1) || !w.take(ObjPlainWig, 1) {
			t.Fatalf("dyeing a wig with %d dye and %d plain wigs", w.inv[ObjYellowDye], w.inv[ObjPlainWig])
		}
		w.inv[ObjBlondWig]++
		w.logf("blond wig")
	case (st.ItemID == ObjJugOfWater || st.ItemID == ObjBucketOfWater) && st.TargetID == ObjClay:
		// make_softclay: the water goes, its empty container stays.
		if !w.take(st.ItemID, 1) || !w.take(ObjClay, 1) {
			t.Fatalf("softening clay with %d water and %d clay", w.inv[st.ItemID], w.inv[ObjClay])
		}
		w.inv[ObjSoftClay]++
		w.inv[1935]++ // a jug, or a bucket: one empty container either way
		w.logf("soft clay")
	default:
		t.Fatalf("unexpected use-on %s", st.Describe())
	}
}

// custom plays one custom step.
func (w *paWorld) custom(st Step) bool {
	t := w.t
	switch st.Custom {
	case CustomSheepWool:
		// The Sheep Shearer executor: shear till the pack holds Qty wool, spin it all. Shears are needed.
		if w.inv[ObjShears] < 1 && w.inv[ObjWool] < st.Qty {
			t.Fatalf("shearing without shears")
		}
		w.inv[ObjBallOfWool] += st.Qty
		w.logf("wool")
	case CustomPrinceKey:
		if w.inv[ObjPrinceKey] > 0 {
			t.Fatalf("the key step ran with the key in the pack")
		}
		if st.Qty > 0 {
			// osman_items: forge from a print and a bar, or re-issue a lost key for 15 coins.
			switch {
			case w.keystatus == 2 && w.inv[ObjPrinceKey] == 0 && w.bank[ObjPrinceKey] == 0 && w.inv[CoinsID] >= PrinceKeyFee:
				w.inv[CoinsID] -= PrinceKeyFee
				w.keystatus = 1
			case w.keystatus == 0 && w.inv[ObjKeyPrint] >= 1 && w.inv[ObjBronzeBar] >= 1:
				w.inv[ObjKeyPrint]--
				w.inv[ObjBronzeBar]--
				w.keystatus = 1
				w.logf("osman forges")
			default:
				t.Fatalf("Osman asked to forge with %d prints, %d bars, keystatus %d", w.inv[ObjKeyPrint], w.inv[ObjBronzeBar], w.keystatus)
			}
		}
		w.leela()
	case CustomPrinceAshes:
		if !w.take(ObjLogs, 1) || w.inv[ObjTinderbox] < 1 {
			t.Fatalf("burning with %d logs and %d tinderboxes", w.inv[ObjLogs]+1, w.inv[ObjTinderbox])
		}
		w.inv[ObjAshes]++
		w.logf("ashes")
	case CustomPrinceClay:
		hasPick := false
		for _, id := range princePickaxeIDs {
			hasPick = hasPick || w.inv[id] > 0
		}
		if !hasPick {
			t.Fatalf("mining clay without a pickaxe")
		}
		w.inv[ObjClay] += st.Qty
		w.logf("clay")
	case CustomPrinceRope:
		fee := PrinceRopeFee
		for w.inv[ObjRope] < st.Qty && w.inv[CoinsID] >= fee {
			w.inv[CoinsID] -= fee
			w.inv[ObjRope]++
		}
		w.logf("ropes %d", w.inv[ObjRope])
	case CustomPrinceBeerBuy:
		for w.inv[ObjBeer] < st.Qty && w.inv[CoinsID] >= 2 {
			w.inv[CoinsID] -= 2
			w.inv[ObjBeer]++
		}
		w.logf("beers %d", w.inv[ObjBeer])
	case CustomPrincePrint:
		// lady_keli_touch_key: only at stage 20, with soft clay, and only for an account the guards do not kill.
		w.requireOdds(st, PrinceKeliPrintVisitTicks)
		if w.stage != PrinceSpokeOsman || w.inv[ObjSoftClay] < 1 {
			t.Fatalf("Keli asked for the print at stage %d with %d soft clay", w.stage, w.inv[ObjSoftClay])
		}
		w.inv[ObjSoftClay]--
		w.inv[ObjKeyPrint]++
		w.logf("keli print")
	case CustomPrinceBeer:
		w.requireOdds(st, PrinceBeerVisitTicks)
		if w.stage != PrinceHasEquipment || w.inv[ObjBeer] < PrinceBeers {
			t.Fatalf("Joe's beers at stage %d with %d beers", w.stage, w.inv[ObjBeer])
		}
		w.inv[ObjBeer] -= PrinceBeers
		w.stage = PrinceGuardDrunk
		w.logf("joe drunk")
	case CustomPrinceBreakout:
		w.requireOdds(st, PrinceBreakoutVisitTicks)
		if w.stage < PrinceGuardDrunk || w.stage >= PrinceSaved {
			t.Fatalf("the break-out at stage %d", w.stage)
		}
		// The decision's flag says whether the journal had Keli tied when the step began.
		if st.Qty != b2i(w.stage >= PrinceTiedKeli) {
			t.Fatalf("break-out flag %d at stage %d", st.Qty, w.stage)
		}
		if w.stage == PrinceGuardDrunk {
			if !w.take(ObjRope, 1) {
				t.Fatalf("the break-out at stage 40 with no rope")
			}
			w.stage = PrinceTiedKeli
			w.logf("keli tied")
		}
		for _, id := range []int{ObjPrinceKey, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste} {
			if !w.take(id, 1) {
				t.Fatalf("prince_rescue without obj %d", id)
			}
		}
		w.stage = PrinceSaved
		w.logf("prince saved")
	case CustomPrinceLeave:
		w.tile = PrisonDoorStand
		w.logf("left the cell")
	default:
		t.Fatalf("unknown custom step %s", st.Custom)
	}
	return true
}

func (w *paWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepTalk:
		w.talk(st)
	case StepPickLoc:
		if st.LocID != LocOnionPlant || st.ExpectItemID != ObjOnion {
			t.Fatalf("pickLoc %s: not the onion patch", st.Describe())
		}
		w.inv[ObjOnion]++
		w.logf("onion")
	case StepUseOn:
		w.useOn(st)
	case StepProvision:
		ok := w.provision(st)
		key := "provision:" + st.KitName
		if ok {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
			delete(w.ran, key)
		}
		w.logf("%s", key)
	case StepCustom:
		key := st.Custom
		w.custom(st)
		w.ran[key] = true
	case StepDeposit:
		keep := map[int]bool{}
		for _, id := range st.KeepIDs {
			keep[id] = true
		}
		for id, n := range w.inv {
			if n > 0 && !keep[id] {
				w.bank[id] += n
				w.inv[id] = 0
			}
		}
		w.logf("deposit")
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

// run plays DecidePrinceAli to the end (or until it waits, which the caller expects or not).
func (w *paWorld) run() (waited string) {
	w.t.Helper()
	for i := 0; i < 120; i++ {
		st := DecidePrinceAli(w.snapshot())
		switch st.Kind {
		case StepDone:
			return ""
		case StepWait:
			return st.Reason
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("120 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return ""
}

func (w *paWorld) logged(what string) int {
	for i, l := range w.log {
		if l == what || strings.HasPrefix(l, what) {
			return i
		}
	}
	return -1
}

func paIDs(m map[int]int, ids ...int) map[int]int {
	out := map[int]int{}
	for k, v := range m {
		out[k] = v
	}
	for _, id := range ids {
		out[id]++
	}
	return out
}

// paDisguise is a pack that holds the key and every piece of the disguise, and 300 coins, with the extra items.
func paDisguise(extra ...int) map[int]int {
	return paIDs(map[int]int{CoinsID: 300}, append([]int{ObjPrinceKey, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste}, extra...)...)
}

// paMule is the mule's published bank (17 Oct 2026) as far as this quest reads it.
func paMule() map[int]int {
	return map[int]int{ObjTinderbox: 12, ObjBeer: 13403, ObjPinkSkirt: 15, ObjBronzeBar: 2320, ObjBucketOfWater: 3, ObjWool: 234,
		ObjLogs: 7, ObjShears: 109, ObjBallOfWool: 10, ObjRedberries: 6, CoinsID: 43826865}
}
