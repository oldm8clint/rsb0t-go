package woodcutplan

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

func TestTargetIndexAndTier(t *testing.T) {
	if i := TargetIndex("Yew"); i != 4 {
		t.Errorf("TargetIndex(Yew) = %d, want 4", i)
	}
	if i := TargetIndex("yew"); i != 4 {
		t.Errorf("TargetIndex is case-insensitive: got %d", i)
	}
	if i := TargetIndex("nonsense"); TIERS[i].Label != DefaultTarget {
		t.Errorf("TargetIndex(unknown) falls back to default, got %q", TIERS[i].Label)
	}
	if tr := TargetTier("Magic"); tr.Level != 75 {
		t.Errorf("TargetTier(Magic).Level = %d, want 75", tr.Level)
	}
}

// TestStartAtNeverBelowTheContentRequirement pins Tier.StartAt's own stated invariant. A tier
// whose raised gate fell below the content's own requirement would hand CurrentTier a tree the
// server refuses to let the account chop, which presents as an endless silent swing rather than
// as an error — so every tier is checked, not a spot sample.
func TestStartAtNeverBelowTheContentRequirement(t *testing.T) {
	for _, tr := range TIERS {
		if tr.StartAt < tr.Level {
			t.Errorf("%s: StartAt %d is below the content requirement %d", tr.Label, tr.StartAt, tr.Level)
		}
		if tr.StartAt < 1 {
			t.Errorf("%s: StartAt %d must be at least 1", tr.Label, tr.StartAt)
		}
	}
}

// TestTiersCarryTheOwnersRaisedGates pins the fleet owner's own numbers (2026-09-20) alongside
// the content's own requirements, so a later edit that "tidies" StartAt back down to Level —
// undoing the entire point of the raised ladder — fails here loudly rather than silently
// costing logs per hour.
//
// The content requirements are MEASURED from
// content/scripts/skill_woodcutting/configs/trees/*.loc via the woodcutting content, not
// assumed; the StartAt column is the owner's preference and has no content source.
func TestTiersCarryTheOwnersRaisedGates(t *testing.T) {
	want := map[string]struct{ level, startAt int }{
		"Normal tree": {1, 1},
		"Oak":         {15, 25},
		"Willow":      {30, 40},
		"Maple":       {45, 60},
		"Yew":         {60, 70},
		"Magic":       {75, 85},
	}
	if len(want) != len(TIERS) {
		t.Fatalf("the ladder has %d tiers, this test knows %d — update both together", len(TIERS), len(want))
	}
	for _, tr := range TIERS {
		w, ok := want[tr.Label]
		if !ok {
			t.Errorf("unexpected tier %q", tr.Label)
			continue
		}
		if tr.Level != w.level || tr.StartAt != w.startAt {
			t.Errorf("%s: got level %d / startAt %d, want %d / %d", tr.Label, tr.Level, tr.StartAt, w.level, w.startAt)
		}
	}
}

func TestCurrentTierStopsAtTarget(t *testing.T) {
	cases := []struct {
		level  int
		target string
		want   string
		why    string
	}{
		{1, "Yew", "Normal tree", ""},
		{14, "Yew", "Normal tree", ""},
		// The gap between the content's requirement and the owner's raised gate is the whole
		// point of StartAt, so every rung that has one is covered on both sides of it.
		{15, "Yew", "Normal tree", "oak is legal at 15 but the ladder waits for 25"},
		{24, "Yew", "Normal tree", "one below the oak gate"},
		{25, "Yew", "Oak", "the oak gate exactly"},
		{30, "Yew", "Oak", "willow is legal at 30 but the ladder waits for 40"},
		{39, "Yew", "Oak", "one below the willow gate"},
		{40, "Yew", "Willow", "the willow gate exactly"},
		{45, "Yew", "Willow", "maple is legal at 45 but the ladder waits for 60"},
		{59, "Yew", "Willow", "one below the maple gate"},
		{60, "Yew", "Maple", "maple gate exactly; yew is legal at 60 but the ladder waits for 70"},
		{69, "Yew", "Maple", "one below the yew gate"},
		{70, "Yew", "Yew", "the yew gate exactly"},
		{99, "Yew", "Yew", "stops at target even though the level allows magic"},
		{75, "Magic", "Yew", "magic is legal at 75 but the ladder waits for 85"},
		{84, "Magic", "Yew", "one below the magic gate"},
		{85, "Magic", "Magic", "the magic gate exactly"},
		{99, "Magic", "Magic", ""},
	}
	for _, c := range cases {
		if got := CurrentTier(c.level, c.target).Label; got != c.want {
			t.Errorf("CurrentTier(%d,%q) = %q, want %q (%s)", c.level, c.target, got, c.want, c.why)
		}
	}
}

func TestAtTarget(t *testing.T) {
	if AtTarget(1, "Yew") {
		t.Error("level 1 should not be at the Yew target")
	}
	// 60 is the CONTENT's yew requirement but not the ladder's gate, so it is deliberately
	// still short of the target — this is the case that regressed when StartAt was introduced.
	if AtTarget(60, "Yew") {
		t.Error("level 60 is legal for yew but below the ladder's own gate of 70")
	}
	if !AtTarget(70, "Yew") {
		t.Error("level 70 should be at the Yew target")
	}
	if !AtTarget(99, "Yew") {
		t.Error("level 99 should still read as at the Yew target (capped)")
	}
}

func TestNextTier(t *testing.T) {
	if n := NextTier(1, "Yew"); n == nil || n.Label != "Oak" {
		t.Errorf("NextTier(1,Yew) = %+v, want Oak", n)
	}
	if n := NextTier(60, "Yew"); n == nil || n.Label != "Yew" {
		t.Errorf("NextTier(60,Yew) = %+v, want Yew — 60 works maple, yew is still ahead", n)
	}
	if n := NextTier(70, "Yew"); n != nil {
		t.Errorf("NextTier at target should be nil, got %+v", n)
	}
}

func TestLadderView(t *testing.T) {
	// Level 30: legal for willow, but the ladder is still on oak (gate 40) — so oak is "now"
	// and willow is "next".
	rows := LadderView(30, "Willow")
	if len(rows) != 3 {
		t.Fatalf("LadderView(30,Willow) = %d rows, want 3", len(rows))
	}
	if rows[0].State != TierDone || rows[1].State != TierNow || rows[2].State != TierNext {
		t.Errorf("states = %+v", rows)
	}
	if !rows[2].Target {
		t.Error("the last row (Willow itself) should be marked Target")
	}
	// Both numbers travel to the panel, so it can say "needs 30, we start at 40" rather than
	// implying the raised gate is the game's own rule.
	if rows[2].Level != 30 || rows[2].StartAt != 40 {
		t.Errorf("the willow row should carry both gates, got level %d startAt %d", rows[2].Level, rows[2].StartAt)
	}
}

func TestAtCamp(t *testing.T) {
	tier := TIERS[0]
	if !AtCamp(tier.Anchor, tier) {
		t.Error("the anchor itself should be at camp")
	}
	far := tile.Tile{X: tier.Anchor.X + tier.Leash + 1, Z: tier.Anchor.Z, Level: tier.Anchor.Level}
	if AtCamp(far, tier) {
		t.Error("a tile past the leash should not be at camp")
	}
	upstairs := tier.Anchor
	upstairs.Level = 1
	if AtCamp(upstairs, tier) {
		t.Error("a different level should never be at camp")
	}
}

func TestIsLogsAndLogCount(t *testing.T) {
	if !IsLogs("Yew logs") || !IsLogs("logs") || IsLogs("Yew") {
		t.Error("IsLogs mismatched")
	}
	items := []PackLine{{Name: "Yew logs", Count: 5}, {Name: "Yew logs", Count: 0}, {Name: "Coins", Count: 100}}
	if got := LogCount(items, "Yew logs"); got != 6 { // 5 + (0 counted as 1)
		t.Errorf("LogCount = %d, want 6", got)
	}
}

func TestAxeHelpers(t *testing.T) {
	if !IsAxe("rune axe") || IsAxe("Rune essence") {
		t.Error("IsAxe mismatched")
	}
	if got := BestAxe([]string{"Bronze axe", "Mithril axe"}); got != "Mithril axe" {
		t.Errorf("BestAxe = %q, want Mithril axe", got)
	}
	if got := BestAxe(nil); got != "" {
		t.Errorf("BestAxe(nil) = %q, want empty", got)
	}
	if got := AxeToKeep([]string{"Steel axe", "Coins"}); got != "Steel axe" {
		t.Errorf("AxeToKeep = %q", got)
	}
	if got := AxeToWithdraw([]string{"Bronze axe"}, []string{"Rune axe"}); got != "Rune axe" {
		t.Errorf("AxeToWithdraw should prefer the better banked axe, got %q", got)
	}
	if got := AxeToWithdraw([]string{"Rune axe"}, []string{"Bronze axe"}); got != "" {
		t.Errorf("AxeToWithdraw should not downgrade, got %q", got)
	}
	if got := AxeToWithdraw(nil, nil); got != "" {
		t.Errorf("AxeToWithdraw with nothing banked = %q, want empty", got)
	}
}

func TestWantsRuneAxe(t *testing.T) {
	cases := []struct {
		name string
		s    WoodSnapshot
		want bool
	}{
		{"bank unread", WoodSnapshot{BankRead: false}, false},
		{"noted in pack", WoodSnapshot{BankRead: true, RuneAxeNoted: true}, false},
		{"held", WoodSnapshot{BankRead: true, AxesHeld: []string{"Rune axe"}}, false},
		{"banked", WoodSnapshot{BankRead: true, BankAxes: []string{"Rune axe"}}, false},
		{"none anywhere", WoodSnapshot{BankRead: true, AxesHeld: []string{"Bronze axe"}, BankAxes: []string{}}, true},
	}
	for _, c := range cases {
		if got := WantsRuneAxe(c.s); got != c.want {
			t.Errorf("%s: WantsRuneAxe = %v, want %v", c.name, got, c.want)
		}
	}
}

func lumbridgeTile() tile.Tile { return freshstart.LumbridgeCentre }

func TestDecideWoodPhaseAcceptTutorialWait(t *testing.T) {
	if got := DecideWoodPhase(WoodSnapshot{MainModal: freshstart.DesignModal}); got != PhaseAccept {
		t.Errorf("accept: got %v", got)
	}
	if got := DecideWoodPhase(WoodSnapshot{Tile: nil}); got != PhaseWait {
		t.Errorf("wait: got %v", got)
	}
	tut := freshstart.TutorialIsland.MinX + 1
	tt := tile.Tile{X: tut, Z: freshstart.TutorialIsland.MinZ + 1}
	if got := DecideWoodPhase(WoodSnapshot{Tile: &tt}); got != PhaseTutorial {
		t.Errorf("tutorial: got %v", got)
	}
}

func TestDecideWoodPhaseBrightBeforeEverythingElse(t *testing.T) {
	lb := lumbridgeTile()
	s := WoodSnapshot{Tile: &lb, Bright: false, Sellable: 5, Coins: 999}
	if got := DecideWoodPhase(s); got != PhaseBright {
		t.Errorf("bright should win before store/fund, got %v", got)
	}
}

func TestDecideWoodPhaseStoreFundTravel(t *testing.T) {
	lb := lumbridgeTile()
	s := WoodSnapshot{Tile: &lb, Bright: true, Sellable: 3, Coins: 0, Fare: 60}
	if got := DecideWoodPhase(s); got != PhaseStore {
		t.Errorf("store: got %v", got)
	}
	s.Sellable = 0
	if got := DecideWoodPhase(s); got != PhaseFund {
		t.Errorf("fund: got %v", got)
	}
	s.Coins = 60
	if got := DecideWoodPhase(s); got != PhaseTravel {
		t.Errorf("travel: got %v", got)
	}
}

func TestDecideWoodPhaseAtSeersBankAndAxes(t *testing.T) {
	here := Seers
	base := WoodSnapshot{Tile: &here, Bright: true, Coins: 999, Fare: 60}

	s := base
	s.BankRead = false
	if got := DecideWoodPhase(s); got != PhaseBank {
		t.Errorf("unread bank: got %v, want bank", got)
	}

	s = base
	s.BankRead = true
	s.RuneAxeNoted = true
	if got := DecideWoodPhase(s); got != PhaseBank {
		t.Errorf("noted axe: got %v, want bank", got)
	}

	s = base
	s.BankRead = true
	s.BankAxes = []string{}
	s.HasMule = false
	if got := DecideWoodPhase(s); got != PhaseStuck {
		t.Errorf("no axe, no mule: got %v, want stuck", got)
	}

	s.HasMule = true
	if got := DecideWoodPhase(s); got != PhaseOrder {
		t.Errorf("no axe, has mule: got %v, want order", got)
	}

	s = base
	s.BankRead = true
	s.BankAxes = []string{"Bronze axe"}
	if got := DecideWoodPhase(s); got != PhaseBank {
		t.Errorf("axe banked, none held: got %v, want bank", got)
	}

	s = base
	s.BankRead = true
	s.AxesHeld = []string{"Bronze axe"}
	s.PackFull = true
	if got := DecideWoodPhase(s); got != PhaseBank {
		t.Errorf("pack full: got %v, want bank", got)
	}

	s = base
	s.BankRead = true
	s.AxesHeld = []string{"Bronze axe"}
	s.BankAxes = []string{"Rune axe"}
	if got := DecideWoodPhase(s); got != PhaseBank {
		t.Errorf("a rune axe waiting in the bank should be worth the walk from bronze, got %v", got)
	}

	far := tile.Tile{X: Seers.X + 70, Z: Seers.Z} // inSeers (radius 80) but past every tier's leash
	s = base
	s.Tile = &far
	s.BankRead = true
	s.AxesHeld = []string{"Rune axe"}
	s.Level = 1
	s.Target = "Yew"
	if got := DecideWoodPhase(s); got != PhaseWalk {
		t.Errorf("far from camp: got %v, want walk", got)
	}

	s.Tile = &TIERS[0].Anchor
	if got := DecideWoodPhase(s); got != PhaseChop {
		t.Errorf("at camp: got %v, want chop", got)
	}
}

func TestDescribeWoodPhase(t *testing.T) {
	for _, p := range []WoodPhase{PhaseAccept, PhaseTutorial, PhaseWait, PhaseBright, PhaseStore, PhaseStart, PhaseFund, PhaseTravel, PhaseBank, PhaseOrder, PhaseWalk, PhaseChop, PhaseStuck} {
		if DescribeWoodPhase(p) == "" {
			t.Errorf("DescribeWoodPhase(%v) is empty", p)
		}
	}
}

func TestDefaultSettingsAndNormalised(t *testing.T) {
	s := DefaultSettings()
	if s.Target != DefaultTarget || s.Mule != "Mulecon" || s.MuleAt != MuleLogsAt {
		t.Errorf("DefaultSettings = %+v", s)
	}
	n := Settings{Target: "  ", MuleAt: donorplan.MaxMuleAt + 1, Fare: -1, RestBelowHP: 999}.Normalised()
	if n.Target != DefaultTarget {
		t.Errorf("blank target should fall back, got %q", n.Target)
	}
	if n.MuleAt != donorplan.MaxMuleAt {
		t.Errorf("muleAt should clamp to MaxMuleAt, got %d", n.MuleAt)
	}
	if n.Fare != 0 {
		t.Errorf("negative fare should clamp to 0, got %d", n.Fare)
	}
	if n.RestBelowHP != 90 {
		t.Errorf("restBelowHp should clamp to 90, got %d", n.RestBelowHP)
	}
}

func TestRestBelow(t *testing.T) {
	if got := (Settings{RestBelowHP: 0}).RestBelow(); got != 0.05 {
		t.Errorf("RestBelow(0) = %v", got)
	}
	if got := (Settings{RestBelowHP: 100}).RestBelow(); got != 0.9 {
		t.Errorf("RestBelow(100) = %v", got)
	}
}

func TestMuleRowsHaulAndLoad(t *testing.T) {
	keep := []string{"Coins"}
	bank := []donorplan.ItemRow{
		{Name: "Bronze axe", ID: 1351, Count: 1},
		{Name: "Coins", ID: 995, Count: 5000},
		{Name: "Yew logs", ID: 1515, Count: 200},
		{Name: "Uncut sapphire", ID: 1623, Count: 4},
	}
	rows := MuleRows(bank, keep)
	if len(rows) != 2 || rows[0].Name != "Yew logs" || rows[1].Name != "Uncut sapphire" {
		t.Errorf("MuleRows should put logs first and keep the axe and coins back: %+v", rows)
	}

	pack := []donorplan.ItemRow{{Name: "Yew logs", ID: 1515, Count: 25}, {Name: "Rune axe", ID: 1359, Count: 1}}
	if got := MuleHaul(pack, keep); got != 1 {
		t.Errorf("MuleHaul should not count the axe: got %d", got)
	}
	load := MuleLoad(pack, 28, keep)
	if len(load) != 1 || load[0].Name != "Yew logs" {
		t.Errorf("MuleLoad = %+v", load)
	}
}

func TestMuleDue(t *testing.T) {
	now := time.Now()
	if MuleDue(0, 0, MuleLogsAt, now, time.Time{}) {
		t.Error("zero logs should never be due")
	}
	if !MuleDue(MuleLogsAt, 0, MuleLogsAt, now, time.Time{}) {
		t.Error("bank alone at the threshold should be due")
	}
	if !MuleDue(MuleLogsAt-1, 1, MuleLogsAt, now, time.Time{}) {
		t.Error("bank + pack together at the threshold should be due")
	}
	if MuleDue(MuleLogsAt, 0, MuleLogsAt, now, now.Add(time.Hour)) {
		t.Error("before nextAllowedAt should not be due")
	}
}

func TestProgressPassesThrough(t *testing.T) {
	if !reflect.DeepEqual(Progress(60, 300000), Progress(60, 300000)) {
		t.Error("Progress should be deterministic")
	}
}

// Reported live by the fleet owner (2026-09-21): twelve woodcutters carried the mithril axe the
// mule handed them while the Seers market listed 94 rune axes, and none of them ever looked.
// PhaseOrder was reachable only from the `held == ""` branch, so owning ANY axe ended the search.
func TestAnAxeUpgradeIsWorthAnOrder(t *testing.T) {
	here := Seers
	base := WoodSnapshot{
		Tile:     &here,
		Level:    30,
		BankRead: true,
		AxesHeld: []string{"Mithril axe"},
		HasMule:  true,
	}

	up := base
	up.AxeUpgradeDue = true
	if got := seersPhase(up); got != PhaseOrder {
		t.Errorf("holding a mithril axe with an upgrade available -> %v, want %v", got, PhaseOrder)
	}

	// Without the flag nothing changes: an account that cannot actually obtain a better axe
	// must carry on chopping rather than walking to a market it cannot buy from.
	if got := seersPhase(base); got == PhaseOrder {
		t.Error("no upgrade available, yet it still went to order")
	}
}

// A free axe in the bank must always beat buying one, so the upgrade branch has to sit AFTER
// the banked-rune check. If the order were reversed the account would spend 150,000 coins on
// something it already owned.
func TestABankedRuneAxeBeatsBuyingOne(t *testing.T) {
	here := Seers
	s := WoodSnapshot{
		Tile:          &here,
		Level:         30,
		BankRead:      true,
		AxesHeld:      []string{"Mithril axe"},
		BankAxes:      []string{RuneAxe},
		HasMule:       true,
		AxeUpgradeDue: true, // even with a purchase on the table
	}
	if got := seersPhase(s); got != PhaseBank {
		t.Errorf("a rune axe is already in the bank -> %v, want %v", got, PhaseBank)
	}
}

// The fare ladder's third rung: a Man standing closer than the mule is worth robbing, because
// the mule's real cost is the queue behind it and not the walk.
func TestRobbingBeatsTheMuleOnlyWhenItIsPlainlyCloser(t *testing.T) {
	// The live case: a woodcutter at the Seers trees, a Man a couple of tiles off, the mule 83
	// tiles away at Catherby.
	if !PreferRobbing(2, 83) {
		t.Error("a Man 2 tiles away against a mule 83 tiles away: should rob, and this is the whole point of the rung")
	}
	// Level pegging is not a reason to gamble: a Man carries 3-25 coins a pocket and can miss,
	// while the mule's hand-out is certain.
	if PreferRobbing(10, 10) {
		t.Error("a target the same distance as the mule should not displace a certain hand-out")
	}
	if PreferRobbing(9, 10) {
		t.Error("one tile closer is not 'plainly closer' — the margin exists so this does not flip on noise")
	}
	if !PreferRobbing(7, 10) {
		t.Error("three tiles closer should qualify")
	}
	// No target: a caller signals that with a distance past its own leash, and negative
	// distances are nonsense rather than an invitation.
	if PreferRobbing(-1, 83) || PreferRobbing(2, -1) {
		t.Error("a negative distance must never read as a reason to rob")
	}
}

// The fleet ran axePrice 150,000 and axeBudget 165,000 — right for a rune axe, and the only two
// prices the script knew. So a market with no rune axe left acct134 looping instead of buying,
// and had a lower tier ever been chosen it would have been offered 150,000 for it. Measured on
// the live market the same night: rune 150,000 (98 trades), adamant 3,150 (7), mithril 600 (4).
func TestALowerAxeIsBoughtAtItsOwnPrice(t *testing.T) {
	s := Settings{AxeWanted: RuneAxe, AxePrice: 150000, AxeBudget: 165000}.Normalised()

	for _, c := range []struct {
		axe  string
		want int
	}{{RuneAxe, 150000}, {"Adamant axe", 3150}, {"Mithril axe", 600}} {
		got, ok := s.PriceOfAxe(c.axe)
		if !ok || got != c.want {
			t.Errorf("PriceOfAxe(%q) = %d,%v — want %d", c.axe, got, ok, c.want)
		}
	}

	// THE SAFETY PROPERTY users asked for: a cheap axe can never cost the expensive one's
	// budget. 165,000 was the ceiling on every purchase, including a 600-coin one.
	if got := s.MaxCoinsForAxe("Mithril axe"); got > 700 {
		t.Errorf("MaxCoinsForAxe(Mithril axe) = %d — a 600-coin axe must not carry a rune axe's ceiling", got)
	}
	if got := s.MaxCoinsForAxe(RuneAxe); got != 165000 {
		t.Errorf("MaxCoinsForAxe(Rune axe) = %d, want the configured budget 165000", got)
	}

	// An unpriced tier stays unknown, and unknown must never read as affordable — that is what
	// stops an axe nobody priced being bought at whatever a seller asks.
	if _, ok := s.PriceOfAxe("Steel axe"); ok {
		t.Error("Steel axe answered a price — the mule hands those out free, so buying one is never right")
	}
	if got := s.MaxCoinsForAxe("Steel axe"); got != 0 {
		t.Errorf("MaxCoinsForAxe(Steel axe) = %d, want 0 — an unknown price must buy nothing", got)
	}

	// The operator still wins, and case-insensitively: a roster says "rune axe", not "Rune axe".
	own := Settings{AxeWanted: RuneAxe, AxePrice: 150000, AxeBudget: 165000,
		AxePrices: map[string]int{"adamant axe": 4000}}.Normalised()
	if got, _ := own.PriceOfAxe("Adamant axe"); got != 4000 {
		t.Errorf("an operator's own adamant price = %d, want 4000", got)
	}
}
