package essenceplan

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestInEssenceMine(t *testing.T) {
	in := tile.Tile{X: 45 << 6, Z: 75 << 6}
	if !InEssenceMine(&in) {
		t.Errorf("InEssenceMine(%+v) = false, want true", in)
	}
	// The far corner of the same mapsquare is still inside it.
	inCorner := tile.Tile{X: 45<<6 + 63, Z: 75<<6 + 63}
	if !InEssenceMine(&inCorner) {
		t.Errorf("InEssenceMine(%+v) = false, want true", inCorner)
	}
	out := tile.Tile{X: 3251, Z: 3420}
	if InEssenceMine(&out) {
		t.Errorf("InEssenceMine(%+v) = true, want false", out)
	}
	if InEssenceMine(nil) {
		t.Error("InEssenceMine(nil) = true, want false")
	}
}

func TestPickOptions(t *testing.T) {
	want := []string{BestAvailable, "Rune", "Adamant", "Mithril", "Steel", "Iron", "Bronze"}
	if got := PickOptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("PickOptions() = %v, want %v", got, want)
	}
}

func TestIsPickaxe(t *testing.T) {
	cases := map[string]bool{
		"Rune pickaxe":    true,
		"rune pickaxe":    true,
		"RUNE PICKAXE":    true,
		"Bronze pickaxe":  true,
		"Rune essence":    false,
		"":                false,
		"Adamant pickaxe": true,
	}
	for name, want := range cases {
		if got := IsPickaxe(name); got != want {
			t.Errorf("IsPickaxe(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRequiredMiningLevel(t *testing.T) {
	if lvl, ok := RequiredMiningLevel("Rune"); !ok || lvl != 41 {
		t.Errorf("RequiredMiningLevel(Rune) = (%d, %v), want (41, true)", lvl, ok)
	}
	if lvl, ok := RequiredMiningLevel("bronze"); !ok || lvl != 1 {
		t.Errorf("RequiredMiningLevel(bronze) = (%d, %v), want (1, true)", lvl, ok)
	}
	if _, ok := RequiredMiningLevel(BestAvailable); ok {
		t.Error("RequiredMiningLevel(BestAvailable) ok = true, want false")
	}
	if _, ok := RequiredMiningLevel("Not a tier"); ok {
		t.Error("RequiredMiningLevel(garbage) ok = true, want false")
	}
}

func TestDesiredPickaxe(t *testing.T) {
	// A specific tier the level allows.
	if got, ok := DesiredPickaxe("Rune", 41); !ok || got.Item != "Rune pickaxe" {
		t.Errorf("DesiredPickaxe(Rune, 41) = (%+v, %v), want Rune pickaxe", got, ok)
	}
	// A specific tier the level does not allow: no fallback to a lower tier.
	if got, ok := DesiredPickaxe("Rune", 40); ok {
		t.Errorf("DesiredPickaxe(Rune, 40) = (%+v, true), want ok=false", got)
	}
	// Best available at exactly a tier's boundary picks that tier.
	if got, ok := DesiredPickaxe(BestAvailable, 41); !ok || got.Item != "Rune pickaxe" {
		t.Errorf("DesiredPickaxe(BestAvailable, 41) = (%+v, %v), want Rune pickaxe", got, ok)
	}
	if got, ok := DesiredPickaxe(BestAvailable, 40); !ok || got.Item != "Adamant pickaxe" {
		t.Errorf("DesiredPickaxe(BestAvailable, 40) = (%+v, %v), want Adamant pickaxe", got, ok)
	}
	if got, ok := DesiredPickaxe(BestAvailable, 1); !ok || got.Item != "Iron pickaxe" {
		// Iron sorts before Bronze in PickTiers and both need level 1, so the tie goes to Iron.
		t.Errorf("DesiredPickaxe(BestAvailable, 1) = (%+v, %v), want Iron pickaxe", got, ok)
	}
	if got, ok := DesiredPickaxe(BestAvailable, 0); ok {
		t.Errorf("DesiredPickaxe(BestAvailable, 0) = (%+v, true), want ok=false", got)
	}
}

func TestNeedsPickaxeCheck(t *testing.T) {
	if !NeedsPickaxeCheck("", BestAvailable, 41) {
		t.Error("NeedsPickaxeCheck(never checked) = false, want true")
	}
	if NeedsPickaxeCheck("Rune pickaxe", BestAvailable, 41) {
		t.Error("NeedsPickaxeCheck(already the desired tier) = true, want false")
	}
	if !NeedsPickaxeCheck("Rune pickaxe", BestAvailable, 40) {
		t.Error("NeedsPickaxeCheck(level dropped below the checked tier) = false, want true")
	}
	// A Mining level of 0 desires nothing; a previous check of "" already agrees.
	if NeedsPickaxeCheck("", BestAvailable, 0) {
		t.Error("NeedsPickaxeCheck(nothing desired, nothing checked) = true, want false")
	}
}

func TestResolvePick(t *testing.T) {
	cases := []struct {
		name       string
		selection  string
		level      int
		held, bank []string
		want       PickResolution
	}{
		{
			name: "specific tier under level stops even if a lower tier is held", selection: "Rune", level: 30,
			held: []string{"Iron pickaxe"},
			want: PickResolution{Kind: PickStop, Reason: "Mining 41 required for the Rune pickaxe (have 30)"},
		},
		{
			name: "specific tier held", selection: "Adamant", level: 31,
			held: []string{"Adamant pickaxe"},
			want: PickResolution{Kind: PickHeld, Item: "Adamant pickaxe"},
		},
		{
			name: "specific tier only in the bank", selection: "Adamant", level: 31,
			bank: []string{"Adamant pickaxe"},
			want: PickResolution{Kind: PickWithdraw, Item: "Adamant pickaxe"},
		},
		{
			name: "specific tier nowhere", selection: "Adamant", level: 31,
			want: PickResolution{Kind: PickStop, Reason: "no Adamant pickaxe in inventory, equipment, or bank"},
		},
		{
			name: "best available holds the highest usable tier even if a lower one is also held", selection: BestAvailable, level: 21,
			held: []string{"Iron pickaxe", "Mithril pickaxe"},
			want: PickResolution{Kind: PickHeld, Item: "Mithril pickaxe"},
		},
		{
			name: "best available withdraws the highest usable tier from the bank", selection: BestAvailable, level: 21,
			bank: []string{"Iron pickaxe", "Mithril pickaxe"},
			want: PickResolution{Kind: PickWithdraw, Item: "Mithril pickaxe"},
		},
		{
			name: "best available prefers a held lower tier over a banked higher one", selection: BestAvailable, level: 21,
			held: []string{"Iron pickaxe"}, bank: []string{"Mithril pickaxe"},
			want: PickResolution{Kind: PickHeld, Item: "Iron pickaxe"},
		},
		{
			name: "best available with nothing usable anywhere", selection: BestAvailable, level: 1,
			want: PickResolution{Kind: PickStop, Reason: "no usable pickaxe (Iron pickaxe, Bronze pickaxe) in inventory, equipment, or bank"},
		},
		{
			name: "best available with Mining 0 has no usable tier at all", selection: BestAvailable, level: 0,
			held: []string{"Bronze pickaxe"},
			want: PickResolution{Kind: PickStop, Reason: "no usable pickaxe () in inventory, equipment, or bank"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolvePick(c.selection, c.level, c.held, c.bank); got != c.want {
				t.Errorf("ResolvePick(%q, %d, %v, %v) = %+v, want %+v", c.selection, c.level, c.held, c.bank, got, c.want)
			}
		})
	}
}

func TestHeldPickaxeToKeep(t *testing.T) {
	if got := HeldPickaxeToKeep(BestAvailable, 41, []string{"Rune pickaxe"}); got != "Rune pickaxe" {
		t.Errorf("HeldPickaxeToKeep(held) = %q, want Rune pickaxe", got)
	}
	if got := HeldPickaxeToKeep(BestAvailable, 41, nil); got != "" {
		t.Errorf("HeldPickaxeToKeep(none held) = %q, want \"\"", got)
	}
	// Held but the wrong tier for the selection (a specific tier not held) is not "kept".
	if got := HeldPickaxeToKeep("Rune", 41, []string{"Iron pickaxe"}); got != "" {
		t.Errorf("HeldPickaxeToKeep(specific tier not held) = %q, want \"\"", got)
	}
}

func TestPickaxeCost(t *testing.T) {
	cases := []struct {
		item     string
		wantCost int
		wantOK   bool
	}{
		{"Bronze pickaxe", 1, true},
		{"bronze pickaxe", 1, true}, // case-insensitive
		{"Rune pickaxe", 32000, true},
		{"Iron pickaxe", 140, true},
		{"Steel pickaxe", 500, true},
		{"Mithril pickaxe", 1300, true},
		{"Adamant pickaxe", 3200, true},
		{"Dragon pickaxe", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		cost, ok := PickaxeCost(c.item)
		if cost != c.wantCost || ok != c.wantOK {
			t.Errorf("PickaxeCost(%q) = (%d, %v), want (%d, %v)", c.item, cost, ok, c.wantCost, c.wantOK)
		}
	}
	// Every tier PickTiers knows about must have a real Nurmof price, or upgradePickaxeStep's
	// bank/Nurmof fallback would silently treat a real tier as unaffordable forever.
	for _, t2 := range PickTiers {
		if _, ok := PickaxeCost(t2.Item); !ok {
			t.Errorf("PickTiers has %q but PickaxeShopCost has no entry for it", t2.Item)
		}
	}
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		s    MinerSnapshot
		want MinerStage
	}{
		{"mining", MinerSnapshot{InMine: true, PackFull: false}, StageMine},
		{"pack full in the mine", MinerSnapshot{InMine: true, PackFull: true}, StagePortal},
		{"haul outside takes priority over a missing pickaxe", MinerSnapshot{InMine: false, Haul: 1, PickHeld: false}, StageBank},
		{"no pickaxe, empty pack, outside", MinerSnapshot{InMine: false, Haul: 0, PickHeld: false}, StagePickaxe},
		{"pickaxe held, empty pack, outside", MinerSnapshot{InMine: false, Haul: 0, PickHeld: true}, StageTeleport},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.s); got != c.want {
				t.Errorf("DecideStage(%+v) = %v, want %v", c.s, got, c.want)
			}
		})
	}
}

// itemIDs mirrors real content ids (Rune essence's unnoted id is EssenceID itself) so that two
// rows of the same name carry the same id, the way real bank/pack rows do — donorplan.PlanLoad
// groups by name+id together, so two same-named rows with different ids would wrongly stay apart.
var itemIDs = map[string]int{
	"Rune essence":   EssenceID,
	"Coins":          995,
	"Rune pickaxe":   1275,
	"Uncut sapphire": 1623,
}

func rows(names []string) []donorplan.ItemRow {
	out := make([]donorplan.ItemRow, len(names))
	for i, n := range names {
		out[i] = donorplan.ItemRow{Name: n, ID: itemIDs[n], Count: 1}
	}
	return out
}

func names(rs []donorplan.ItemRow) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func TestMuleRows(t *testing.T) {
	bank := rows([]string{"Rune essence", "Coins", "Rune pickaxe", "Uncut sapphire"})
	// Essence first, pickaxe never leaves regardless of the keep list, Coins kept by setting.
	if got := names(MuleRows(bank, []string{"Coins"})); !reflect.DeepEqual(got, []string{"Rune essence", "Uncut sapphire"}) {
		t.Errorf("MuleRows = %v", got)
	}
	// No keep list at all: only the pickaxe is protected.
	if got := names(MuleRows(bank, nil)); !reflect.DeepEqual(got, []string{"Rune essence", "Coins", "Uncut sapphire"}) {
		t.Errorf("MuleRows(no keep) = %v", got)
	}
	if got := MuleRows(nil, nil); len(got) != 0 {
		t.Errorf("MuleRows(empty bank) = %v, want empty", got)
	}
}

func TestMuleHaul(t *testing.T) {
	pack := rows([]string{"Rune essence", "Rune pickaxe", "Coins"})
	if got := MuleHaul(pack, nil); got != 2 {
		t.Errorf("MuleHaul(no keep) = %d, want 2 (pickaxe excluded)", got)
	}
	if got := MuleHaul(pack, []string{"Coins"}); got != 1 {
		t.Errorf("MuleHaul(keep Coins) = %d, want 1", got)
	}
}

func TestMuleLoad(t *testing.T) {
	pack := rows([]string{"Uncut sapphire", "Rune essence", "Rune essence", "Rune pickaxe"})
	got := MuleLoad(pack, 28, nil)
	want := []donorplan.LoadGroup{{Name: "Rune essence", ID: EssenceID, Slots: 2}, {Name: "Uncut sapphire", ID: 1623, Slots: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MuleLoad(28) = %+v, want %+v", got, want)
	}
	// A single free slot cannot fit the 2-slot essence group (offered whole, never split), but
	// the 1-slot sapphire group still fits and is not skipped just because an earlier group was.
	if got := MuleLoad(pack, 1, nil); !reflect.DeepEqual(got, []donorplan.LoadGroup{{Name: "Uncut sapphire", ID: 1623, Slots: 1}}) {
		t.Errorf("MuleLoad(1) = %+v", got)
	}
	if got := MuleLoad(pack, 2, nil); !reflect.DeepEqual(got, []donorplan.LoadGroup{{Name: "Rune essence", ID: EssenceID, Slots: 2}}) {
		t.Errorf("MuleLoad(2) = %+v", got)
	}
}

// A blinded worn read costs essenceBot.handoffStep exactly one thing: the pickaxe name drops out
// of the keep list it builds from HeldNames, because content nulls sidebar tab 4 for the length
// of a macro event and Equipment.Worn() then returns nil. handoffStep runs before the miner's own
// containersBlind gate, so nothing prevents that — it is harmless instead, and this is the
// assertion that says so rather than leaving it argued. muleRules skips every pickaxe whatever
// the keep list holds, so the blind keep list and the readable one must decide the hand-off
// identically.
func TestMuleHandoffIgnoresABlindedWornRead(t *testing.T) {
	readable := []string{"Rune pickaxe"} // what keepInPack returns while the worn panel reads
	var blind []string                   // what it returns while tab 4 is nulled

	bank := rows([]string{"Rune essence", "Rune pickaxe", "Uncut sapphire"})
	if got, want := names(MuleRows(bank, blind)), names(MuleRows(bank, readable)); !reflect.DeepEqual(got, want) {
		t.Errorf("MuleRows(blind keep) = %v, want the readable keep list's own %v: a nulled worn tab must not send a pickaxe to the mule", got, want)
	}
	// Worth asserting only because the pickaxe is really in the rows being compared: a bank
	// holding none would satisfy the equality above trivially.
	if got := names(MuleRows(bank, blind)); !reflect.DeepEqual(got, []string{"Rune essence", "Uncut sapphire"}) {
		t.Errorf("MuleRows(blind keep) = %v, want the essence and the sapphire with the pickaxe left behind", got)
	}

	pack := rows([]string{"Rune essence", "Rune pickaxe"})
	if got, want := MuleHaul(pack, blind), MuleHaul(pack, readable); got != want {
		t.Errorf("MuleHaul(blind keep) = %d, want %d", got, want)
	}
	if got, want := MuleLoad(pack, 28, blind), MuleLoad(pack, 28, readable); !reflect.DeepEqual(got, want) {
		t.Errorf("MuleLoad(blind keep) = %+v, want %+v", got, want)
	}
}

func TestIsNotedEssence(t *testing.T) {
	if IsNotedEssence(donorplan.ItemRow{Name: "Rune essence", ID: EssenceID}) {
		t.Error("IsNotedEssence(unnoted id) = true, want false")
	}
	if !IsNotedEssence(donorplan.ItemRow{Name: "Rune essence", ID: EssenceID + 1}) {
		t.Error("IsNotedEssence(other id) = false, want true")
	}
	if IsNotedEssence(donorplan.ItemRow{Name: "Coins", ID: EssenceID + 1}) {
		t.Error("IsNotedEssence(not essence) = true, want false")
	}
}

func TestHandoffAfterBanking(t *testing.T) {
	base := time.Unix(0, 0)
	s := Settings{Mule: "mulecon", MuleAt: 10_000}
	if !HandoffAfterBanking(10_000, s, base, base) {
		t.Error("HandoffAfterBanking(at threshold) = false, want true")
	}
	if HandoffAfterBanking(9_999, s, base, base) {
		t.Error("HandoffAfterBanking(below threshold) = true, want false")
	}
	if HandoffAfterBanking(10_000, s, base, base.Add(time.Minute)) {
		t.Error("HandoffAfterBanking(before nextAllowed) = true, want false")
	}
	noMule := Settings{Mule: "", MuleAt: 10_000}
	if HandoffAfterBanking(10_000, noMule, base, base) {
		t.Error("HandoffAfterBanking(no mule set) = true, want false")
	}
	zeroThreshold := Settings{Mule: "mulecon", MuleAt: 0}
	if HandoffAfterBanking(1_000_000, zeroThreshold, base, base) {
		t.Error("HandoffAfterBanking(MuleAt 0) = true, want false")
	}
}

func TestMineStalled(t *testing.T) {
	base := time.Unix(0, 0)
	if MineStalled(base, base.Add(19*time.Second)) {
		t.Error("MineStalled(19s) = true, want false")
	}
	if !MineStalled(base, base.Add(20*time.Second)) {
		t.Error("MineStalled(20s) = false, want true")
	}
	if !MineStalled(base, base.Add(time.Minute)) {
		t.Error("MineStalled(1m) = false, want true")
	}
}

func TestTeleportGivingUp(t *testing.T) {
	if TeleportGivingUp(2) {
		t.Error("TeleportGivingUp(2) = true, want false")
	}
	if !TeleportGivingUp(3) {
		t.Error("TeleportGivingUp(3) = false, want true")
	}
	if !TeleportGivingUp(4) {
		t.Error("TeleportGivingUp(4) = false, want true")
	}
}

func TestIsQuestRefusalMessage(t *testing.T) {
	if !IsQuestRefusalMessage("You need to have completed the Rune Mysteries quest to use this.") {
		t.Error("IsQuestRefusalMessage(real refusal) = false, want true")
	}
	if !IsQuestRefusalMessage("YOU NEED TO HAVE COMPLETED THE RUNE MYSTERIES QUEST FIRST") {
		t.Error("IsQuestRefusalMessage(uppercase) = false, want true")
	}
	if IsQuestRefusalMessage("Aubury teleports you to the Rune Essence Mine.") {
		t.Error("IsQuestRefusalMessage(unrelated line) = true, want false")
	}
}
