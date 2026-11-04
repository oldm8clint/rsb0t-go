package questmods

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

func fletchSnap(level int) questplan.Snapshot {
	return questplan.Snapshot{Levels: map[string]int{"fletching": level, "woodcutting": 40}, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		Failed: map[string]bool{}, Ran: map[string]bool{}, BankIDs: map[int]int{}}
}

func lineFor(kit []questplan.KitLine, id int) (questplan.KitLine, bool) {
	for _, l := range kit {
		if l.ID == id {
			return l, true
		}
	}
	return questplan.KitLine{}, false
}

// The Fletching 25 rung parked on every one of the 16 accounts that reached its logs on 15 Oct 2026 ("withdraw 3 Logs: the
// pack filled"): its kit asked for exactly 26 logs of a pack the run staller shares, with the Seers knife spawn the only
// fallback and no way to fell a log.
func TestTheFletchingKitTakesWhatFitsAndHasEverySource(t *testing.T) {
	kit := fletchKit(fletchSnap(1), questplan.ObjLogs)
	for _, l := range kit {
		if why := questplan.ValidateKitLine(l); why != "" {
			t.Errorf("line %s: %s", l.Name, why)
		}
	}
	logs, ok := lineFor(kit, questplan.ObjLogs)
	if !ok || logs.Qty != fletchLogsPerTrip || logs.Min != 1 {
		t.Fatalf("logs line %+v, want up to %d with a floor of 1", logs, fletchLogsPerTrip)
	}
	if len(logs.Sources) != 1 || logs.Sources[0].Kind != questplan.SourceSkill || logs.Sources[0].Method != questplan.MethodChopLogs ||
		logs.Sources[0].Tile != woodcutplan.TIERS[0].Anchor || logs.Sources[0].Level != 1 {
		t.Errorf("logs are not felled at the Seers normal trees: %+v", logs.Sources)
	}
	knife, ok := lineFor(kit, objKnife)
	if !ok || knife.Min != 0 || knife.Qty != 1 {
		t.Fatalf("knife line %+v", knife)
	}
	var spawns []tile.Tile
	for _, s := range knife.Sources {
		if s.Kind != questplan.SourceGround {
			t.Errorf("the knife has a %s source", s.Kind)
		}
		spawns = append(spawns, s.Tile)
	}
	want := []tile.Tile{woodcutplan.SeersKnife, woodcutplan.CatherbyKnife, woodcutplan.SouthSeersKnife}
	if !reflect.DeepEqual(spawns, want) {
		t.Errorf("knife spawns %v, want the Seers, Catherby and south Seers ones in that order (not Lumbridge's) %v", spawns, want)
	}
	// With no axe the kit carries one, wanted only while the logs are short; with one it carries none.
	axe, ok := lineFor(kit, questplan.ObjIronAxe)
	if !ok || axe.OnlyIfShort != questplan.ObjLogs || len(axe.Sources) != 1 || axe.Sources[0].Kind != questplan.SourceShop {
		t.Errorf("axe line %+v %v, want an iron axe from the shop that serves the logs", axe, ok)
	}
	held := fletchSnap(1)
	held.InvIDs[questplan.ObjIronAxe] = 1
	for _, a := range questplan.Axes {
		if _, ok := lineFor(fletchKit(held, questplan.ObjLogs), a); ok {
			t.Errorf("axe %d is in the kit of an account that holds an iron axe", a)
		}
	}
	// A banked axe is the kit's axe, with no shop behind it.
	banked := fletchSnap(1)
	banked.BankKnown, banked.BankIDs = true, map[int]int{questplan.ObjSteelAxe: 1, questplan.ObjRuneAxe: 1}
	if a, ok := lineFor(fletchKit(banked, questplan.ObjLogs), questplan.ObjSteelAxe); !ok || len(a.Sources) != 0 {
		t.Errorf("banked steel axe: %+v %v", a, ok)
	}
}

func TestTheOakTierFellsOaksAndEverySkillSourceHasAnExecutor(t *testing.T) {
	kit := fletchKit(fletchSnap(20), objOakLogs)
	logs, ok := lineFor(kit, objOakLogs)
	if !ok || len(logs.Sources) != 1 || logs.Sources[0].Method != questplan.MethodChopOak ||
		logs.Sources[0].Tile != woodcutplan.TIERS[1].Anchor || logs.Sources[0].Level != woodcutplan.TIERS[1].Level {
		t.Fatalf("oak logs line %+v", logs)
	}
	if logs.Name != "Oak logs" {
		t.Errorf("oak logs are called %q", logs.Name)
	}
	// Every skill method the kit names is one the provisioner has.
	methods := SkillMethods()
	for _, lvl := range []int{1, 10, 20, 24} {
		logID, _ := fletchTier(lvl)
		for _, l := range fletchKit(fletchSnap(lvl), logID) {
			for _, s := range l.Sources {
				if s.Kind == questplan.SourceSkill && methods[s.Method] == nil {
					t.Errorf("fletching %d: no executor for the skill source %q", lvl, s.Method)
				}
			}
		}
	}
}

// The plan the live account of 15 Oct 2026 would have made: 17 free slots, the staller's kit and six other things in the
// pack, the bank full of logs and the mule holding knives. The first visit draws as many logs as the pack holds less the
// knife's slot, not 26.
func TestTheFletchingKitPlansWhatTheLiveAccountsPackHolds(t *testing.T) {
	kit := fletchKit(fletchSnap(1), questplan.ObjLogs)
	st := questplan.ProvisionState{
		Pack: map[int]int{questplan.ObjIronAxe: 1}, Worn: map[int]int{},
		Bank: map[int]int{questplan.ObjLogs: 3000}, BankKnown: true,
		Mule: map[int]int{objKnife: 186, questplan.ObjLogs: 7000}, MuleKnown: true,
		FreeSlots: 17, Depositable: 6, Exhausted: map[string]bool{}, Levels: map[string]int{"woodcutting": 40},
	}
	p := questplan.PlanProvision(kit, st)
	if p.Phase != questplan.PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != questplan.ObjLogs || p.Withdraw[0].Count != fletchLogsPerTrip {
		t.Fatalf("plan %s, want %d logs from the bank (the trip's ceiling, under the 22 slots the pack has left)", p.Summary(), fletchLogsPerTrip)
	}
	// A pack with less room takes less, never the 20 that do not fit.
	st.FreeSlots, st.Depositable = 10, 0
	p = questplan.PlanProvision(kit, st)
	if p.Phase != questplan.PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].Count != 9 {
		t.Fatalf("plan %s with 10 free slots, want 9 logs (one slot is the knife's)", p.Summary())
	}
	if len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != objKnife {
		t.Errorf("mule ask %v, want the knife", p.MuleAsk)
	}
}

func TestTheFletchingRungDecides(t *testing.T) {
	q := FletchingRung("fletching25", 25)
	decide := q.Module.Decide
	s := fletchSnap(1)
	st := decide(s)
	if st.Kind != questplan.StepProvision || st.KitName != "fletch-1511" {
		t.Fatalf("empty pack: %+v, want the Logs kit", st)
	}
	for _, id := range append([]int{objKnife, questplan.ObjLogs, questplan.CoinsID}, questplan.Axes...) {
		found := false
		for _, k := range st.KeepIDs {
			found = found || k == id
		}
		if !found {
			t.Errorf("the deposit would bank %d, which the rung works with", id)
		}
	}
	s.InvIDs[objKnife], s.InvIDs[questplan.ObjLogs] = 1, 20
	if st := decide(s); st.Kind != questplan.StepCustom {
		t.Fatalf("knife and logs held: %+v, want the fletching step", st)
	}
	s = fletchSnap(21)
	if st := decide(s); st.Kind != questplan.StepProvision || st.KitName != "fletch-1521" {
		t.Fatalf("Fletching 21: %+v, want the Oak logs kit", st)
	}
	s.Failed["provision:fletch-1521"] = true
	if st := decide(s); st.Kind != questplan.StepWait {
		t.Fatalf("a failed kit: %+v, want a wait", st)
	}
	if st := decide(fletchSnap(25)); st.Kind != questplan.StepDone {
		t.Fatalf("Fletching 25: %+v, want done", st)
	}
}
