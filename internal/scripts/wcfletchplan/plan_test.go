package wcfletchplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

const top = "Magic" // the whole ladder, so a rung is never capped by the target in these tests

func snap(wood, fletch int) LadderSnapshot {
	return LadderSnapshot{WoodLevel: wood, FletchLevel: fletch, Target: top, Gate: GateStartAt, Fletching: true}
}

// TestFletchRungIndexIsTheOwnersProgression walks the whole ladder by FLETCHING level, with the
// woodcutting level held high enough never to bind. This is the spec, rung by rung: stay on a
// tree until the next tree's logs can be cut into a shortbow.
func TestFletchRungIndexIsTheOwnersProgression(t *testing.T) {
	cases := []struct {
		fletch int
		want   int
		why    string
	}{
		{1, 0, "a brand-new account cuts plain logs into arrow shafts"},
		{4, 0, "still normal trees"},
		{19, 0, "one level below the oak shortbow — still normal trees"},
		{20, 1, "oak shortbow unlocks, so move to oaks"},
		{24, 1, "oaks, still on shortbows"},
		{25, 1, "oak LONGBOW unlocks, but that is an upgrade within the rung, not a move"},
		{34, 1, "one below the willow shortbow — still oaks"},
		{35, 2, "willow shortbow unlocks"},
		{49, 2, "one below the maple shortbow — still willows"},
		{50, 3, "maple shortbow unlocks"},
		{64, 3, "one below the yew shortbow — still maples"},
		{65, 4, "yew shortbow unlocks"},
		{79, 4, "one below the magic shortbow — still yews"},
		{80, 5, "magic shortbow unlocks: the top rung"},
		{85, 5, "magic longbow unlocks; there is nowhere above magic to go"},
		{99, 5, "still magic"},
	}
	for _, c := range cases {
		if got := FletchRungIndex(c.fletch, len(Ladder)-1); got != c.want {
			t.Errorf("Fletching %d: rung %d (%s), want %d (%s)", c.fletch, got, Ladder[got].Tier.Label, c.want, c.why)
		}
	}
}

// TestTierIndexNeverExceedsTheAxe is the min. A fletching level alone must never send an account
// to a tree its woodcutting cannot cut: that is an endless silent swing, not an error.
func TestTierIndexNeverExceedsTheAxe(t *testing.T) {
	for wood := 1; wood <= 99; wood++ {
		for fletch := 1; fletch <= 99; fletch++ {
			idx := TierIndex(snap(wood, fletch))
			if idx > AxeRungIndex(wood, GateStartAt, len(Ladder)-1) {
				t.Fatalf("Woodcutting %d / Fletching %d chose rung %d, above what the woodcutting level allows", wood, fletch, idx)
			}
			if idx > FletchRungIndex(fletch, len(Ladder)-1) {
				t.Fatalf("Woodcutting %d / Fletching %d chose rung %d, above what the fletching level allows", wood, fletch, idx)
			}
			if Ladder[idx].Tier.StartAt > wood && idx != 0 {
				t.Fatalf("Woodcutting %d chose %s, whose gate is %d", wood, Ladder[idx].Tier.Label, Ladder[idx].Tier.StartAt)
			}
		}
	}
}

// TestTierIndexWorkedExamples spells out the interesting corners of that min.
func TestTierIndexWorkedExamples(t *testing.T) {
	cases := []struct {
		wood, fletch int
		want         int
		why          string
	}{
		{99, 1, 0, "a master woodcutter who cannot fletch still cuts normal logs into shafts"},
		{1, 99, 0, "a master fletcher who cannot chop an oak stays on normal trees"},
		{25, 20, 1, "both gates met at oak"},
		{25, 19, 0, "the fletching gate binds: one level short of the oak shortbow"},
		{24, 20, 0, "the woodcutting gate binds: one level short of woodcutplan's oak StartAt"},
		{85, 50, 3, "fletching binds at maple though the axe could take magic"},
		{60, 80, 3, "woodcutting binds at maple though fletching could take magic"},
		{85, 80, 5, "both at the top"},
	}
	for _, c := range cases {
		if got := TierIndex(snap(c.wood, c.fletch)); got != c.want {
			t.Errorf("Woodcutting %d / Fletching %d: rung %d (%s), want %d (%s) — %s",
				c.wood, c.fletch, got, Ladder[got].Tier.Label, c.want, Ladder[c.want].Tier.Label, c.why)
		}
	}
}

// TestFletchingOffFallsBackToTheWoodcuttingClimb: a run that cannot fletch must behave as
// ProgressiveWoodcutter does, or a knifeless account would be pinned to normal trees for ever.
func TestFletchingOffFallsBackToTheWoodcuttingClimb(t *testing.T) {
	for wood := 1; wood <= 99; wood++ {
		s := snap(wood, 1)
		s.Fletching = false
		got := TierIndex(s)
		want := woodcutplan.CurrentTierIndex(wood, top)
		if got != want {
			t.Errorf("Woodcutting %d with fletching off: rung %d, want woodcutplan's own %d", wood, got, want)
		}
	}
}

// TestTargetCapsTheLadder: the target setting is a ceiling on both halves.
func TestTargetCapsTheLadder(t *testing.T) {
	s := snap(99, 99)
	s.Target = "Willow"
	if got := TierIndex(s); got != woodcutplan.TargetIndex("Willow") {
		t.Errorf("target Willow with every level maxed chose rung %d, want %d", got, woodcutplan.TargetIndex("Willow"))
	}
}

// TestGateContentUsesTheServersOwnRequirement shows the two gates differ where content allows a
// tree earlier than woodcutplan's raised StartAt.
func TestGateContentUsesTheServersOwnRequirement(t *testing.T) {
	// Oak: content 15, woodcutplan StartAt 25.
	if got := AxeRungIndex(15, GateContent, len(Ladder)-1); got != 1 {
		t.Errorf("Woodcutting 15 under the content gate = rung %d, want 1 (oak's own requirement)", got)
	}
	if got := AxeRungIndex(15, GateStartAt, len(Ladder)-1); got != 0 {
		t.Errorf("Woodcutting 15 under the startAt gate = rung %d, want 0 (oak's raised gate is 25)", got)
	}
}

// TestEffectivePairRoundTripsThroughWoodcutplan is the contract behind handing this script's rung
// to woodcutplan.DecideWoodPhase without forking it: for every rung, the (level, target) pair must
// make woodcutplan's OWN CurrentTierIndex return that exact rung.
func TestEffectivePairRoundTripsThroughWoodcutplan(t *testing.T) {
	for i := range Ladder {
		lvl, tgt := EffectiveWoodLevel(i), EffectiveTarget(i)
		if got := woodcutplan.CurrentTierIndex(lvl, tgt); got != i {
			t.Errorf("rung %d (%s) hands woodcutplan (level %d, target %q) and gets rung %d back", i, Ladder[i].Tier.Label, lvl, tgt, got)
		}
		if got := woodcutplan.CurrentTier(lvl, tgt); got != Ladder[i].Tier {
			t.Errorf("rung %d: woodcutplan.CurrentTier returned %q, not this rung's own tier", i, got.Label)
		}
	}
}

// TestEffectivePairDrivesTheStartCampGate: the Draynor camp must end exactly when the ladder
// leaves the first rung, and not before.
func TestEffectivePairDrivesTheStartCampGate(t *testing.T) {
	if woodcutplan.AtOakGate(EffectiveWoodLevel(0), EffectiveTarget(0)) {
		t.Error("rung 0 (normal trees) should NOT be past the oak gate — the start camp is where the arrow shafts are cut")
	}
	for i := 1; i < len(Ladder); i++ {
		if !woodcutplan.AtOakGate(EffectiveWoodLevel(i), EffectiveTarget(i)) {
			t.Errorf("rung %d (%s) should be past the oak gate so the camp hands over to the Seers ladder", i, Ladder[i].Tier.Label)
		}
	}
}

// ---- the product ----

// TestChooseLongbowDisplacesShortbowExactlyAtTheLevel: at every rung, one level below the
// longbow gate the answer is the shortbow and at the gate it is the longbow.
func TestChooseLongbowDisplacesShortbowExactlyAtTheLevel(t *testing.T) {
	for i, r := range Ladder {
		if i == 0 {
			continue // the first rung is shafts by default; covered separately below
		}
		below, _ := Choose(i, r.Longbow.Level-1, NormalShafts)
		if below.Kind != KindShortbow {
			t.Errorf("%s at Fletching %d: %v, want a shortbow", r.Tier.Label, r.Longbow.Level-1, below.Kind)
		}
		at, _ := Choose(i, r.Longbow.Level, NormalShafts)
		if at.Kind != KindLongbow {
			t.Errorf("%s at Fletching %d: %v, want a longbow", r.Tier.Label, r.Longbow.Level, at.Kind)
		}
		if at.ItemID != r.Longbow.ItemID {
			t.Errorf("%s: chose obj %d, want the unstrung longbow %d", r.Tier.Label, at.ItemID, r.Longbow.ItemID)
		}
	}
}

// TestChooseAtTheShortbowGate: exactly at the shortbow level the rung produces a shortbow.
func TestChooseAtTheShortbowGate(t *testing.T) {
	for i, r := range Ladder {
		if i == 0 {
			continue
		}
		got, ok := Choose(i, r.Shortbow.Level, NormalShafts)
		if !ok || got.Kind != KindShortbow || got.ItemID != r.Shortbow.ItemID {
			t.Errorf("%s at Fletching %d: %v obj %d ok=%v, want the unstrung shortbow %d", r.Tier.Label, r.Shortbow.Level, got.Kind, got.ItemID, ok, r.Shortbow.ItemID)
		}
	}
}

// TestChooseRefusesBelowTheShortbowGate: a rung whose own shortbow is out of reach must return
// ok=false so the caller BANKS the logs, never opens a menu.
//
// This is the trap from ladder.go's header: cut_logs.rs2 offers both bows whatever the level and
// only refuses after the choice, with a chat message nothing here reads. A "pick whatever the
// menu shows" runner would produce nothing for ever.
func TestChooseRefusesBelowTheShortbowGate(t *testing.T) {
	for i, r := range Ladder {
		if i == 0 {
			continue
		}
		if _, ok := Choose(i, r.Shortbow.Level-1, NormalShafts); ok {
			t.Errorf("%s at Fletching %d returned a product; content refuses below %d", r.Tier.Label, r.Shortbow.Level-1, r.Shortbow.Level)
		}
	}
}

// TestNormalLogsNeverBecomeBowsByDefault is the owner's own instruction: "normal logs only cut
// arrow shafts". Content offers a plain-log shortbow at 5 and a longbow at 10, so this is a
// policy the default has to enforce rather than something the game does for us.
func TestNormalLogsNeverBecomeBowsByDefault(t *testing.T) {
	for lvl := 1; lvl <= 99; lvl++ {
		got, ok := Choose(0, lvl, NormalShafts)
		if !ok {
			t.Fatalf("Fletching %d: plain logs produced nothing; arrow shafts are level 1", lvl)
		}
		if got.Kind != KindShaft {
			t.Fatalf("Fletching %d: plain logs became %v, want arrow shafts", lvl, got.Kind)
		}
		if got.ItemID != ArrowShaftID {
			t.Fatalf("Fletching %d: plain logs became obj %d, want the arrow shaft %d", lvl, got.ItemID, ArrowShaftID)
		}
	}
	// The blank policy a settings map with no key produces must behave as the default does.
	if got, _ := Choose(0, 99, NormalPolicy("")); got.Kind != KindShaft {
		t.Errorf("a blank normalLogs policy made %v, want arrow shafts", got.Kind)
	}
}

// TestNormalBowsPolicyIsOptInAndLadderedCorrectly covers the non-default branch.
func TestNormalBowsPolicyIsOptInAndLadderedCorrectly(t *testing.T) {
	cases := []struct {
		lvl  int
		want ProductKind
	}{
		{1, KindShaft},    // below the plain shortbow's level 5
		{4, KindShaft},    //
		{5, KindShortbow}, // cut_logs.dbrow: shortbow,unstrung_shortbow,5,50
		{9, KindShortbow},
		{10, KindLongbow}, // longbow,unstrung_longbow,10,100
		{99, KindLongbow},
	}
	for _, c := range cases {
		got, ok := Choose(0, c.lvl, NormalBows)
		if !ok || got.Kind != c.want {
			t.Errorf("normalLogs=bows at Fletching %d: %v ok=%v, want %v", c.lvl, got.Kind, ok, c.want)
		}
	}
}

// ---- when to fletch ----

func base() FletchSnapshot {
	return FletchSnapshot{Enabled: true, HasKnife: true, Product: KindShortbow, Logs: 1}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		in   FletchSnapshot
		want Action
	}{
		{"switched off does nothing", FletchSnapshot{Enabled: false, HasKnife: true, Product: KindShaft, Logs: 28, PackFull: true}, ActNone},
		{"nothing fletchable on this rung does nothing", FletchSnapshot{Enabled: true, HasKnife: true, Product: KindNone, Logs: 28, PackFull: true}, ActNone},
		{"no knife but one in reach fetches it", FletchSnapshot{Enabled: true, KnifeInReach: true, Product: KindShaft}, ActKnife},
		{"no knife and none in reach does nothing", FletchSnapshot{Enabled: true, Product: KindShaft, Logs: 28, PackFull: true}, ActNone},
		{"no logs does nothing", FletchSnapshot{Enabled: true, HasKnife: true, Product: KindShaft, Logs: 0, Idle: true}, ActNone},
		{"a full pack fletches", withF(base(), func(s *FletchSnapshot) { s.PackFull = true }), ActFletch},
		{"idle at the trees fletches", withF(base(), func(s *FletchSnapshot) { s.Idle = true }), ActFletch},
		{"about to bank fletches", withF(base(), func(s *FletchSnapshot) { s.Banking = true }), ActFletch},
		{"a worthwhile batch of a STACKING product fletches", withF(base(), func(s *FletchSnapshot) { s.Stacking, s.Logs = true, FletchBatch }), ActFletch},
		{"one below the batch carries on chopping", withF(base(), func(s *FletchSnapshot) { s.Stacking, s.Logs = true, FletchBatch-1 }), ActNone},
		{"a batch of BOWS mid-chop does not interrupt the chopping", withF(base(), func(s *FletchSnapshot) { s.Logs = 28 }), ActNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(c.in); got != c.want {
				t.Errorf("Decide(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func withF(s FletchSnapshot, f func(*FletchSnapshot)) FletchSnapshot {
	f(&s)
	return s
}

// TestNoKnifeNeverSpins: with no knife and none in reach, every pass must answer ActNone so the
// caller falls through to the ordinary chop-and-bank. A retry here would be a busy loop against a
// thing that cannot succeed.
func TestNoKnifeNeverSpins(t *testing.T) {
	s := FletchSnapshot{Enabled: true, HasKnife: false, KnifeInReach: false, Product: KindShortbow, Logs: 28, PackFull: true, Idle: true, Banking: true}
	for i := 0; i < 100; i++ {
		if got := Decide(s); got != ActNone {
			t.Fatalf("pass %d with no knife and none in reach returned %v, want %v", i, got, ActNone)
		}
	}
}

// TestFletchingAFullPackTerminates: at oak and above a log becomes ONE bow, so fletching a full
// pack does not free a slot. The loop must still end — it does because the logs are consumed, so
// the next pass sees none.
func TestFletchingAFullPackTerminates(t *testing.T) {
	s := base()
	s.PackFull, s.Logs = true, 28
	if Decide(s) != ActFletch {
		t.Fatal("a full pack of fletchable logs should fletch")
	}
	s.Logs = 0 // what the pack looks like after the batch, still full of bows
	if got := Decide(s); got != ActNone {
		t.Errorf("after the batch the pack is still full but holds no logs: %v, want %v", got, ActNone)
	}
}

// TestEarlyCuttingOnlyWhereItFreesASlot is the throughput rule that separates the first rung from
// every other one. Fifteen arrow shafts land in ONE slot, so cutting early at the first rung keeps
// the pack from filling and the account never has to stop. Every bow is one log to one
// unstackable item, so cutting early above the first rung stops the chopping and frees nothing.
func TestEarlyCuttingOnlyWhereItFreesASlot(t *testing.T) {
	midChop := FletchSnapshot{Enabled: true, HasKnife: true, Product: KindShaft, Logs: 28}
	midChop.Stacking = true
	if got := Decide(midChop); got != ActFletch {
		t.Errorf("mid-chop with 28 logs and a stacking product: %v, want %v", got, ActFletch)
	}
	midChop.Stacking, midChop.Product = false, KindLongbow
	if got := Decide(midChop); got != ActNone {
		t.Errorf("mid-chop with 28 logs and an unstackable bow: %v, want %v — cutting early frees no slot and costs chop time", got, ActNone)
	}
	// ...but the three "the account was going to stop anyway" triggers still fire for a bow.
	for _, f := range []func(*FletchSnapshot){
		func(s *FletchSnapshot) { s.PackFull = true },
		func(s *FletchSnapshot) { s.Idle = true },
		func(s *FletchSnapshot) { s.Banking = true },
	} {
		s := midChop
		f(&s)
		if got := Decide(s); got != ActFletch {
			t.Errorf("an unstackable bow at a natural stop: %v, want %v (%+v)", got, ActFletch, s)
		}
	}
}

func TestDescribeAction(t *testing.T) {
	for _, a := range []Action{ActNone, ActKnife, ActFletch} {
		if DescribeAction(a) == "" {
			t.Errorf("DescribeAction(%v) is empty", a)
		}
	}
}

// ---- the mule ----

// TestHandoverSendsBowsNotLogs is the owner's whole point: "at mule hand over we are handing over
// (u)'s ready for an account to string them".
func TestHandoverSendsBowsNotLogs(t *testing.T) {
	bank := []donorplan.ItemRow{
		{Name: "Oak logs", ID: OakLogsID, Count: 40},
		{Name: "Coins", ID: 995, Count: 500},
		{Name: "Oak shortbow", ID: 54, Count: 120},
		{Name: "Rune axe", ID: 1359, Count: 1},
		{Name: "Arrow shaft", ID: ArrowShaftID, Count: 900},
	}
	rows := MuleRows(bank, []string{"Coins"})
	if len(rows) == 0 {
		t.Fatal("nothing planned for the mule")
	}
	// The two fletched rows come first, in bank order, before the uncut logs.
	if !IsHandoverProduct(rows[0].ID) || !IsHandoverProduct(rows[1].ID) {
		t.Fatalf("the first two rows are %v/%v, want the fletched stock first", rows[0], rows[1])
	}
	sawLogs := false
	for _, r := range rows {
		if r.ID == OakLogsID {
			sawLogs = true
		}
		if r.Name == "Coins" {
			t.Error("Coins were on the keep list and must not go to the mule")
		}
		if r.Name == "Rune axe" {
			t.Error("the axe must never go to the mule")
		}
	}
	if !sawLogs {
		t.Error("uncut logs should still go to the mule, just after the fletched stock")
	}
}

// TestTheKnifeNeverGoesToTheMule: four ground spawns feed the whole cohort, so a knife handed
// away is not one the account gets back quickly.
func TestTheKnifeNeverGoesToTheMule(t *testing.T) {
	bank := []donorplan.ItemRow{
		{Name: "Knife", ID: 946, Count: 1},
		{Name: "Yew shortbow", ID: 68, Count: 30},
	}
	for _, r := range MuleRows(bank, nil) {
		if r.Name == "Knife" {
			t.Fatal("the knife was planned for the mule")
		}
	}
	pack := []donorplan.ItemRow{{Name: "Knife", ID: 946, Count: 1}}
	if got := MuleHaul(pack, nil); got != 0 {
		t.Errorf("a pack holding only the knife hauls %d slots, want 0 — otherwise it requests an empty trade for ever", got)
	}
}

// TestHandoverStockCountsByIDNotName is the counting trap: the strung Oak shortbow (obj 843) has
// the identical display name to the unstrung one (obj 54). A name-keyed count would include it
// and trigger a hand-over against stock that is not there.
func TestHandoverStockCountsByIDNotName(t *testing.T) {
	rows := []donorplan.ItemRow{
		{Name: "Oak shortbow", ID: 54, Count: 10},  // unstrung — ours
		{Name: "Oak shortbow", ID: 843, Count: 99}, // strung — a different item, same name
		{Name: "Oak logs", ID: OakLogsID, Count: 50},
		{Name: "Arrow shaft", ID: ArrowShaftID, Count: 5},
	}
	if got := HandoverStock(rows); got != 15 {
		t.Errorf("HandoverStock = %d, want 15 (10 unstrung bows + 5 shafts; the 99 STRUNG bows are a different obj)", got)
	}
}

// TestHandoverStockCountsAZeroCountRowAsOne matches every sibling plan package's convention for
// an unstackable item whose count the server left at zero.
func TestHandoverStockCountsAZeroCountRowAsOne(t *testing.T) {
	if got := HandoverStock([]donorplan.ItemRow{{Name: "Yew longbow", ID: 66, Count: 0}}); got != 1 {
		t.Errorf("a zero-count unstackable row counted %d, want 1", got)
	}
}

func TestMuleDue(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	if !MuleDue(900, 100, 1000, now, past) {
		t.Error("bank 900 + pack 100 against a threshold of 1000 should be due")
	}
	if MuleDue(900, 99, 1000, now, past) {
		t.Error("999 against a threshold of 1000 should not be due")
	}
	if MuleDue(2000, 0, 1000, now, future) {
		t.Error("the retry cooldown should hold a hand-over back however much stock there is")
	}
	if MuleDue(-5, 0, 1, now, past) {
		t.Error("a negative bank reading should not trigger a hand-over")
	}
}

// TestMuleLoadPutsTheFletchedStockOnTheTradeScreenFirst covers the trade-screen half.
func TestMuleLoadPutsTheFletchedStockOnTheTradeScreenFirst(t *testing.T) {
	pack := []donorplan.ItemRow{
		{Name: "Maple logs", ID: MapleLogsID, Count: 1},
		{Name: "Maple longbow", ID: 62, Count: 1},
		{Name: "Rune axe", ID: 1359, Count: 1},
	}
	load := MuleLoad(pack, donorplan.DefaultMuleFreeSlots, nil)
	if len(load) == 0 {
		t.Fatal("nothing planned for the trade screen")
	}
	if load[0].ID != 62 {
		t.Errorf("the trade screen leads with obj %d, want the unstrung maple longbow 62", load[0].ID)
	}
	for _, g := range load {
		if g.ID == 1359 {
			t.Error("the rune axe reached the trade screen")
		}
	}
}

// ---- settings ----

func TestDefaultSettingsAreTheOwnersSpec(t *testing.T) {
	d := DefaultSettings()
	if !d.Fletch {
		t.Error("fletching should default ON — that is what this script is")
	}
	if d.NormalLogs != NormalShafts {
		t.Errorf("normalLogs defaults to %q, want %q", d.NormalLogs, NormalShafts)
	}
	if d.TreeGate != GateStartAt {
		t.Errorf("treeGate defaults to %q, want %q so the two scripts chop in the same places", d.TreeGate, GateStartAt)
	}
	if d.Wood.Target != woodcutplan.DefaultTarget {
		t.Errorf("the woodcutting half should keep its own default target, got %q", d.Wood.Target)
	}
}

func TestSettingsNormalised(t *testing.T) {
	got := Settings{}.Normalised()
	if got.NormalLogs != NormalShafts {
		t.Errorf("a blank normalLogs normalised to %q, want %q", got.NormalLogs, NormalShafts)
	}
	if got.TreeGate != GateStartAt {
		t.Errorf("a blank treeGate normalised to %q, want %q", got.TreeGate, GateStartAt)
	}
	if got.MuleAt <= 0 {
		t.Errorf("a zero muleAtFletched normalised to %d, want a positive fallback", got.MuleAt)
	}
	if got := (Settings{NormalLogs: "BOWS"}).Normalised().NormalLogs; got != NormalBows {
		t.Errorf("normalLogs %q normalised to %q, want %q — the settings file's case must not matter", "BOWS", got, NormalBows)
	}
	if got := (Settings{TreeGate: " Content "}).Normalised().TreeGate; got != GateContent {
		t.Errorf("treeGate %q normalised to %q, want %q", " Content ", got, GateContent)
	}
	if got := (Settings{NormalLogs: "nonsense"}).Normalised().NormalLogs; got != NormalShafts {
		t.Errorf("an unknown normalLogs normalised to %q, want the safe default %q", got, NormalShafts)
	}
	if got := (Settings{MuleAt: donorplan.MaxMuleAt * 2}).Normalised().MuleAt; got != donorplan.MaxMuleAt {
		t.Errorf("muleAtFletched clamped to %d, want %d", got, donorplan.MaxMuleAt)
	}
}

// TestDonorUsesTheFletchedThreshold: the hand-over must be driven by this script's own unit, not
// by the woodcutter's log count.
func TestDonorUsesTheFletchedThreshold(t *testing.T) {
	s := DefaultSettings()
	s.MuleAt = 250
	s.Wood.MuleAt = 1000
	if got := s.Donor().MuleAt; got != 250 {
		t.Errorf("the donor threshold is %d, want this script's own 250", got)
	}
	if !s.Donor().Enabled() {
		t.Error("a settings naming the default mule should have the hand-over enabled")
	}
}

func TestLadderSnapshotFor(t *testing.T) {
	s := DefaultSettings()
	s.Fletch = false
	if got := s.LadderSnapshotFor(50, 50, true); got.Fletching {
		t.Error("fletch=false in the settings must win over the caller's own flag")
	}
	s.Fletch = true
	if got := s.LadderSnapshotFor(50, 50, false); got.Fletching {
		t.Error("the caller giving up on fletching must win over the setting")
	}
	got := s.LadderSnapshotFor(50, 60, true)
	if got.WoodLevel != 50 || got.FletchLevel != 60 || got.Gate != GateStartAt || got.Target != s.Wood.Target {
		t.Errorf("snapshot = %+v", got)
	}
}
