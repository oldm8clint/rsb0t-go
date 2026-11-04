package questplan

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// This file pins the Death Plateau loops seen live on 15 and 16 Oct 2026, against content's own
// rules for the items the quest hands out.
//
// Content re-issues a quest item (Harold's IOU and combination, Tenzing's climbing boots and secret
// way map, Denulth's certificate) only when ~obj_gettotal(item) = 0, and that proc is inv + bank +
// worn. A deposit-all from another quest puts these items in the bank; the module looked only at the
// pack, so it asked an NPC for what the NPC would never give (24 of 41 accounts held the combination
// in the bank, all of the eight whose bank was read in the incident; 63 of one account's 68 runs
// ended in "death-reclaim"). deathWorld below is those rules and nothing else, enough to play
// DecideDeathPlateau from any start to its end and to see a step repeat without effect.

const packSlots = 28

// deathWorld is the account, its bank, the mule and the NPCs, as the quest sees them.
type deathWorld struct {
	t         *testing.T
	equip, mp int
	pack      map[int]int
	bank      map[int]int
	junk      int // pack slots held by things that are none of the quest's
	bankRead  bool
	lost      map[int]int // items an inv_add dropped because the pack was full
	failed    map[string]bool
	blocked   map[string]bool

	mule     map[int]int // the mule's published bank
	muleOK   bool        // an ask is answered; false is a world errand carrying it
	shop     int         // loaves on the Baker's shelf
	refuses  bool        // the NPCs hand out nothing (a re-issue that does not come)
	handed   map[int]bool
	finished bool
	parked   *Step
	kits     []string
	steps    []string
	decide   func(Snapshot) Step // nil is DecideDeathPlateau; a test may play another decision against the same world
}

func newDeathWorld(t *testing.T, equip, mp int) *deathWorld {
	return &deathWorld{t: t, equip: equip, mp: mp, pack: map[int]int{}, bank: map[int]int{}, lost: map[int]int{},
		failed: map[string]bool{}, blocked: map[string]bool{}, mule: map[int]int{}, handed: map[int]bool{}, muleOK: true}
}

func (w *deathWorld) owned(id int) int { return w.pack[id] + w.bank[id] }

func (w *deathWorld) free() int {
	n := w.junk
	for _, c := range w.pack {
		if c > 0 {
			n++
		}
	}
	if f := packSlots - n; f > 0 {
		return f
	}
	return 0
}

// add is content's inv_add: a full pack drops the item at the player's feet.
func (w *deathWorld) add(id int) {
	if w.free() == 0 {
		w.lost[id]++
		return
	}
	w.pack[id]++
}

func (w *deathWorld) take(id, n int) {
	w.pack[id] -= n
	if w.pack[id] <= 0 {
		delete(w.pack, id)
	}
}

func copyIDs(m map[int]int) map[int]int {
	out := make(map[int]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyFlags(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (w *deathWorld) snapshot() Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: DeathStage(w.equip, w.mp), StageKnown: true,
		InvIDs: copyIDs(w.pack), WornIDs: map[int]int{}, FreeSlots: w.free(),
		Failed: copyFlags(w.failed), Blocked: copyFlags(w.blocked), Ran: map[string]bool{}}
	if w.bankRead {
		s.BankKnown, s.BankIDs = true, copyIDs(w.bank)
	}
	return s
}

func (w *deathWorld) signature() string {
	var parts []string
	for _, m := range []map[int]int{w.pack, w.bank, w.lost} {
		var ks []int
		for k, v := range m {
			if v > 0 {
				ks = append(ks, k*1000+v)
			}
		}
		sort.Ints(ks)
		parts = append(parts, fmt.Sprint(ks))
	}
	return fmt.Sprintf("%d/%d|%s|read %v|junk %d|flags %v%v", w.equip, w.mp, strings.Join(parts, "|"), w.bankRead, w.junk, w.failed, w.blocked)
}

// run plays the decision until it finishes, parks, or repeats a step that changed nothing.
func (w *deathWorld) run(max int) {
	w.t.Helper()
	last := ""
	for i := 0; i < max && !w.finished && w.parked == nil; i++ {
		before := w.signature()
		decide := w.decide
		if decide == nil {
			decide = DecideDeathPlateau
		}
		st := decide(w.snapshot())
		if why := ValidateStep(st); why != "" {
			w.t.Fatalf("malformed step %q: %s\n%s", st.Describe(), why, strings.Join(w.steps, "\n"))
		}
		desc := st.Describe()
		w.steps = append(w.steps, desc)
		if desc+before == last {
			w.t.Fatalf("a step repeated with no effect (the loop): %q at %s\n%s", desc, before, strings.Join(w.steps, "\n"))
		}
		last = desc + before
		w.apply(st)
	}
}

func (w *deathWorld) apply(st Step) {
	switch st.Kind {
	case StepDone:
		w.finished = true
	case StepWait:
		if st.ParkFor == 0 {
			w.t.Fatalf("a plain wait (the engine parks it after 15 ticks): %q\n%s", st.Reason, strings.Join(w.steps, "\n"))
		}
		s := st
		w.parked = &s
	case StepScanBank:
		w.bankRead = true
	case StepWithdraw:
		w.bankRead = true
		if w.free() < len(st.Items) {
			w.junk = 0 // the engine banks what the step does not keep first
		}
		for _, it := range st.Items {
			if w.bank[it.ID] >= it.Qty {
				w.bank[it.ID] -= it.Qty
				w.add(it.ID)
			}
		}
	case StepDeposit:
		w.junk = 0
		w.bankRead = true
	case StepProvision:
		w.provision(st)
	case StepTalk:
		w.talk(st)
	case StepCustom:
		w.custom(st)
	default:
		w.t.Fatalf("the world does not play %s", st.Describe())
	}
}

// provision is questrunner's Provisioner for these kits, driven by the real PlanProvision: the
// bank is read first, then the plan's phases run one at a time (a withdrawal, the one mule ask, a
// shop) until it is satisfied or blocked, and the kit is recorded the way the engine records it.
func (w *deathWorld) provision(st Step) {
	w.kits = append(w.kits, st.KitName)
	muleAsked, banked := false, false
	exhausted := map[string]bool{}
	key := "provision:" + st.KitName
	for pass := 0; pass < 16; pass++ {
		plan := PlanProvision(st.Kit, ProvisionState{
			Pack: copyIDs(w.pack), Worn: map[int]int{}, Bank: copyIDs(w.bank), BankKnown: banked,
			Mule: copyIDs(w.mule), MuleKnown: true, MuleAsked: muleAsked, Levels: map[string]int{},
			FreeSlots: w.free(), Depositable: w.junk, Exhausted: exhausted,
		})
		switch plan.Phase {
		case PhaseSatisfied:
			delete(w.failed, key)
			delete(w.blocked, key)
			return
		case PhaseBlocked:
			w.failed[key], w.blocked[key] = true, true
			return
		case PhaseBank:
			w.bankRead, banked = true, true
			w.junk = 0 // deposit-first: what is neither kit nor kept goes to the bank
			for _, c := range plan.Withdraw {
				for i := 0; i < c.Count && w.bank[c.ID] > 0; i++ {
					w.bank[c.ID]--
					w.add(c.ID)
				}
			}
		case PhaseMule:
			muleAsked = true
			if !w.muleOK {
				continue // a world errand carries the ask: nothing arrives now
			}
			for _, c := range plan.MuleAsk {
				for i := 0; i < c.Count && w.mule[c.ID] > 0; i++ {
					w.mule[c.ID]--
					w.add(c.ID)
				}
			}
		case PhaseSource:
			task := plan.Tasks[0]
			bought := 0
			if task.Source.Kind == SourceShop {
				for bought < task.Qty && w.shop > 0 {
					w.shop--
					w.add(task.Line.ID)
					bought++
				}
			}
			if bought == 0 {
				exhausted[SourceKey(task.Line, task.Source)] = true
			}
		}
	}
	w.failed[key], w.blocked[key] = true, true
}

func (w *deathWorld) custom(st Step) {
	ok := true
	switch st.Custom {
	case CustomDeathReclaim:
		// harold_reclaim_iou: stage 55 and later, nothing owned anywhere.
		if !w.refuses && w.equip >= DeathIOU && w.owned(ObjDeathIOU)+w.owned(ObjCombination) == 0 {
			if w.equip >= DeathCombo {
				w.add(ObjCombination)
			} else {
				w.add(ObjDeathIOU)
			}
		}
		ok = w.pack[ObjDeathIOU] > 0 || w.pack[ObjCombination] > 0
	case CustomDeathReadIOU:
		w.take(ObjDeathIOU, 1)
		w.add(ObjCombination)
		w.equip = DeathCombo
	case CustomDeathBalls:
		if w.free() < 5 {
			w.t.Fatalf("the balls were played with %d free slots", w.free())
		}
		w.equip = DeathUnlocked
	case CustomDeathSaba:
		w.mp = MapSaba
	case CustomDeathScout:
		w.mp = MapScouted
	case CustomDeathTenzing:
		w.tenzing()
		ok = st.ExpectItemID == 0 || w.pack[st.ExpectItemID] > 0
	default:
		w.t.Fatalf("the world does not play %s", st.Describe())
	}
	w.note(st.Custom, ok)
}

func (w *deathWorld) note(key string, ok bool) {
	if ok {
		delete(w.failed, key)
		return
	}
	w.failed[key] = true
}

// tenzing is death_sherpa.rs2 by the map track.
func (w *deathWorld) tenzing() {
	switch w.mp {
	case MapSaba:
		w.add(ObjClimbingBoots)
		w.mp = MapTenzing
	case MapGaveCert:
		if w.owned(ObjClimbingBoots) == 0 && w.owned(ObjSpikedBoots) == 0 && !w.refuses {
			w.add(ObjClimbingBoots)
			return
		}
		if w.pack[ObjSpikedBoots] > 0 && w.pack[ObjBread] >= 10 && w.pack[ObjTrout] >= 10 {
			w.take(ObjSpikedBoots, 1)
			w.take(ObjBread, 10)
			w.take(ObjTrout, 10)
			w.mp = MapGotMap
			w.add(ObjSecretMap)
		}
	case MapSupplies, MapGotMap, MapScouted:
		if w.owned(ObjSecretMap) == 0 && !w.refuses {
			w.add(ObjSecretMap)
		}
	}
}

// talk is Dunstan and Denulth.
func (w *deathWorld) talk(st Step) {
	switch st.Stop.ID {
	case NpcDunstan:
		switch w.mp {
		case MapTenzing:
			w.mp = MapSmithy
		case MapCert:
			if w.pack[ObjCertificate] > 0 {
				w.take(ObjCertificate, 1)
				w.mp = MapGaveCert
			}
		case MapGaveCert:
			// dunstan_bargain: nothing while a pair is owned anywhere, else a bar and the boots make one.
			if w.owned(ObjSpikedBoots) == 0 && w.pack[ObjIronBar] > 0 && w.pack[ObjClimbingBoots] > 0 {
				w.take(ObjIronBar, 1)
				w.take(ObjClimbingBoots, 1)
				w.add(ObjSpikedBoots)
			}
		}
	case NpcDenulth:
		switch {
		case w.equip >= DeathUnlocked && w.mp >= MapScouted:
			if w.pack[ObjSecretMap] > 0 {
				w.take(ObjSecretMap, 1)
				w.handed[ObjSecretMap] = true
			}
			if w.pack[ObjCombination] > 0 {
				w.take(ObjCombination, 1)
				w.handed[ObjCombination] = true
			}
			if w.handed[ObjSecretMap] && w.handed[ObjCombination] {
				w.finished = true
			}
		case w.mp == MapSmithy:
			w.add(ObjCertificate)
			w.mp = MapCert
		case w.mp == MapCert:
			if w.owned(ObjCertificate) == 0 && !w.refuses {
				w.add(ObjCertificate)
			}
		}
	}
}

// plenty stocks the supplies Tenzing wants where the bank holds them.
func (w *deathWorld) plenty() *deathWorld {
	w.bank[ObjTrout], w.bank[ObjBread] = 10, 10
	return w
}

func lastSteps(w *deathWorld) string { return strings.Join(w.steps, "\n") }

func TestDeathLoopsLive(t *testing.T) {
	t.Run("combination banked at stage 60 is not asked of Harold (the reclaim loop)", func(t *testing.T) {
		w := newDeathWorld(t, DeathCombo, MapScouted).plenty()
		w.bank[ObjCombination] = 1
		w.pack[ObjSecretMap] = 1
		w.run(40)
		if !w.finished {
			t.Fatalf("not finished:\n%s", lastSteps(w))
		}
		for _, s := range w.steps {
			if strings.Contains(s, "again from Harold") {
				t.Fatalf("Harold was asked for what the bank holds:\n%s", lastSteps(w))
			}
		}
	})
	t.Run("a full pack at the reclaim banks first, so the re-issue is not dropped", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapScouted).plenty()
		w.pack[ObjSecretMap] = 1
		w.junk = packSlots - 1
		w.run(40)
		if !w.finished || w.lost[ObjCombination] != 0 {
			t.Fatalf("finished %v, combinations dropped %d:\n%s", w.finished, w.lost[ObjCombination], lastSteps(w))
		}
		if bank, reclaim := strings.Index(lastSteps(w), "bank all but"), strings.Index(lastSteps(w), "death-reclaim"); bank < 0 || reclaim < bank {
			t.Fatalf("the pack was not emptied before the re-issue:\n%s", lastSteps(w))
		}
	})
	t.Run("spiked boots banked are not made again, and Tenzing is not asked for climbing boots (the boots loop)", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGaveCert).plenty()
		w.bank[ObjSpikedBoots], w.bank[ObjCombination] = 1, 1
		w.bank[ObjIronBar] = 1
		w.run(60)
		if !w.finished {
			t.Fatalf("not finished:\n%s", lastSteps(w))
		}
		for _, s := range w.steps {
			if strings.Contains(s, "again from Tenzing") || strings.Contains(s, "talk to Dunstan") {
				t.Fatalf("a bar or a re-issue was asked for with a pair in the bank:\n%s", lastSteps(w))
			}
		}
	})
	t.Run("a spare pair on the mule saves the bar", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGaveCert).plenty()
		w.bank[ObjCombination] = 1
		w.mule[ObjSpikedBoots] = 289
		w.run(60)
		if !w.finished || w.mule[ObjSpikedBoots] != 288 {
			t.Fatalf("finished %v, mule pairs left %d:\n%s", w.finished, w.mule[ObjSpikedBoots], lastSteps(w))
		}
		if strings.Contains(lastSteps(w), "talk to Dunstan") {
			t.Fatalf("a pair was forged with a spare on the mule:\n%s", lastSteps(w))
		}
	})
	t.Run("no trout anywhere parks the quest a day before any bar, boots or bread is fetched", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGaveCert)
		w.bank[ObjCombination], w.bank[ObjBread] = 1, 10
		w.mule[ObjIronBar] = 14145
		w.run(30)
		if w.parked == nil || w.parked.ParkFor != DeathTroutRetry || !strings.Contains(w.parked.Reason, "trout") {
			t.Fatalf("not parked on the trout: %+v\n%s", w.parked, lastSteps(w))
		}
		if len(w.kits) != 1 || w.kits[0] != KitDeathTrout {
			t.Fatalf("kits run before the park: %v (want only the trout kit)", w.kits)
		}
	})
	t.Run("a mule ask on a world errand parks the bar, not eight ticks of banking", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGaveCert).plenty()
		w.mule[ObjIronBar], w.mule[ObjSpikedBoots] = 14145, 289
		w.muleOK = false
		w.run(30)
		if w.parked == nil || w.parked.ParkFor != DeathBarRetry {
			t.Fatalf("not parked on the bar: %+v\n%s", w.parked, lastSteps(w))
		}
		if len(w.kits) > 4 {
			t.Fatalf("too many provisioning rounds before the park: %v", w.kits)
		}
	})
	t.Run("no bread parks the quest after the boots are made", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGaveCert)
		w.bank[ObjCombination], w.bank[ObjTrout], w.bank[ObjSpikedBoots] = 1, 10, 1
		w.run(30)
		if w.parked == nil || w.parked.ParkFor != DeathBreadRetry {
			t.Fatalf("not parked on the bread: %+v\n%s", w.parked, lastSteps(w))
		}
	})
	t.Run("a re-issue that does not come parks the quest at once", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapScouted).plenty()
		w.pack[ObjSecretMap] = 1
		w.refuses = true
		w.run(30)
		if w.parked == nil || w.parked.ParkFor != DeathReissueRetry {
			t.Fatalf("not parked on the re-issue: %+v\n%s", w.parked, lastSteps(w))
		}
	})
	t.Run("the IOU banked at stage 55 is withdrawn and read", func(t *testing.T) {
		w := newDeathWorld(t, DeathIOU, 0).plenty()
		w.bank[ObjDeathIOU] = 1
		w.mule[ObjSpikedBoots] = 5
		w.run(80)
		if !w.finished {
			t.Fatalf("not finished:\n%s", lastSteps(w))
		}
		for _, s := range w.steps {
			if strings.Contains(s, "again from Harold") {
				t.Fatalf("Harold was asked for the IOU the bank holds:\n%s", lastSteps(w))
			}
		}
	})
	t.Run("the certificate banked at map 4 is withdrawn, not asked of Denulth", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapCert).plenty()
		w.bank[ObjCertificate], w.bank[ObjCombination] = 1, 1
		w.mule[ObjSpikedBoots] = 5
		w.run(80)
		if !w.finished {
			t.Fatalf("not finished:\n%s", lastSteps(w))
		}
	})
	t.Run("the secret way map banked at map 7 is withdrawn with the combination in one visit", func(t *testing.T) {
		w := newDeathWorld(t, DeathUnlocked, MapGotMap).plenty()
		w.bank[ObjSecretMap], w.bank[ObjCombination] = 1, 1
		w.run(40)
		if !w.finished {
			t.Fatalf("not finished:\n%s", lastSteps(w))
		}
		withdraws := 0
		for _, s := range w.steps {
			if strings.HasPrefix(s, "withdraw") {
				withdraws++
			}
		}
		if withdraws != 1 {
			t.Fatalf("%d withdrawals, want one for both papers:\n%s", withdraws, lastSteps(w))
		}
	})
}

// deathStart is one consistent start: where each quest item is (0 nowhere, 1 pack, 2 bank).
type deathStart struct {
	equip, mp                             int
	iou, comb, boots, spiked, bar, papers int
}

func (st deathStart) String() string {
	return fmt.Sprintf("equip %d map %d iou %d comb %d boots %d spiked %d bar %d papers %d", st.equip, st.mp, st.iou, st.comb, st.boots, st.spiked, st.bar, st.papers)
}

// deathStarts enumerates the starts a real account can be in: the IOU only at stage 55, the
// combination from stage 60, the climbing boots from the second map stage to the fifth (never beside
// a pair of spiked boots, which they were made into), a bar and the spiked boots at the fifth, the
// certificate at the fourth and the secret way map at the seventh and eighth.
func deathStarts() []deathStart {
	var out []deathStart
	equips := []int{DeathIOU, DeathCombo, DeathUnlocked}
	for n := 0; n < len(equips)*9*3*3*3*3*3*3; n++ {
		m := n
		d := func(radix int) int { v := m % radix; m /= radix; return v }
		st := deathStart{equip: equips[d(len(equips))], mp: d(9), iou: d(3), comb: d(3), boots: d(3), spiked: d(3), bar: d(3), papers: d(3)}
		switch {
		case st.iou != 0 && st.equip != DeathIOU,
			st.comb != 0 && st.equip == DeathIOU,
			st.boots != 0 && (st.mp < MapTenzing || st.mp > MapGaveCert),
			st.spiked != 0 && (st.mp != MapGaveCert || st.boots != 0),
			st.bar != 0 && st.mp != MapGaveCert,
			st.papers != 0 && st.mp != MapCert && st.mp != MapGotMap && st.mp != MapScouted:
			continue
		}
		out = append(out, st)
	}
	return out
}

// TestDeathEveryStartFinishesOrParks plays every consistent start with each quest item in the pack,
// in the bank or nowhere. With the supplies to hand each must finish; with no trout each must either
// finish (the supplies are already given) or park on the trout; with the mule's answer lost to a
// world errand, or the mule holding nothing, each must finish or park on the bar; with only bars on
// the mule each must finish (the pair is forged). No start may repeat a step that changed
// nothing, drop a re-issue on a full pack, or wait without a park.
func TestDeathEveryStartFinishesOrParks(t *testing.T) {
	place := func(w *deathWorld, id, at int) {
		switch at {
		case 1:
			w.pack[id] = 1
		case 2:
			w.bank[id] = 1
		}
	}
	starts := deathStarts()
	finished, parked := 0, 0
	for _, st := range starts {
		for _, mode := range []string{"plenty", "no trout", "errand", "bars only", "bare mule"} {
			w := newDeathWorld(t, st.equip, st.mp)
			w.mule[ObjIronBar], w.mule[ObjSpikedBoots] = 100, 100
			place(w, ObjDeathIOU, st.iou)
			place(w, ObjCombination, st.comb)
			place(w, ObjClimbingBoots, st.boots)
			place(w, ObjSpikedBoots, st.spiked)
			place(w, ObjIronBar, st.bar)
			switch st.mp {
			case MapCert:
				place(w, ObjCertificate, st.papers)
			case MapGotMap, MapScouted:
				place(w, ObjSecretMap, st.papers)
			}
			switch mode {
			case "plenty":
				w.plenty()
			case "no trout":
				w.bank[ObjBread] = 10
			case "errand":
				w.plenty()
				w.muleOK = false
			case "bars only":
				w.plenty()
				w.mule[ObjSpikedBoots] = 0
			case "bare mule":
				w.plenty()
				w.mule = map[int]int{}
			}
			name := st.String() + " " + mode
			w.run(120)
			if !w.finished && w.parked == nil {
				t.Fatalf("%s: neither finished nor parked after %d steps:\n%s", name, len(w.steps), lastSteps(w))
			}
			if n := w.lost[ObjCombination] + w.lost[ObjDeathIOU] + w.lost[ObjClimbingBoots] + w.lost[ObjSecretMap] + w.lost[ObjCertificate]; n != 0 {
				t.Fatalf("%s: a re-issue was dropped on a full pack:\n%s", name, lastSteps(w))
			}
			if w.parked == nil {
				finished++
				if mode == "no trout" && st.mp <= MapGaveCert {
					t.Fatalf("%s: finished with no trout to give Tenzing:\n%s", name, lastSteps(w))
				}
				continue
			}
			parked++
			switch mode {
			case "no trout":
				if w.parked.ParkFor != DeathTroutRetry {
					t.Fatalf("%s: parked for %s (%s), want the trout's:\n%s", name, w.parked.ParkFor, w.parked.Reason, lastSteps(w))
				}
			case "errand", "bare mule":
				if w.parked.ParkFor != DeathBarRetry {
					t.Fatalf("%s: parked for %s (%s), want the bar's:\n%s", name, w.parked.ParkFor, w.parked.Reason, lastSteps(w))
				}
			default:
				t.Fatalf("%s: parked with every supply to hand (%s):\n%s", name, w.parked.Reason, lastSteps(w))
			}
		}
	}
	t.Logf("%d starts, %d plays: %d finished, %d parked", len(starts), len(starts)*5, finished, parked)
	if len(starts) < 300 {
		t.Fatalf("only %d starts were enumerated", len(starts))
	}
}

func TestDeathParkLengths(t *testing.T) {
	if DeathTroutRetry < 12*time.Hour {
		t.Errorf("the trout park (%s) is shorter than a tail pass", DeathTroutRetry)
	}
	for _, d := range []time.Duration{DeathTroutRetry, DeathBarRetry, DeathBreadRetry, DeathReissueRetry} {
		if d < time.Hour {
			t.Errorf("a park of %s is the escalating Backoff's job", d)
		}
	}
}
