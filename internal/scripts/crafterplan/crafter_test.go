package crafterplan

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The mule case is translated from rsb0t-scripts test/freshStartPlan.test.ts; the rest are table
// tests for decisions ProgressiveCrafter.ts makes inline.

func TestTheMule(t *testing.T) {
	keep := donorplan.ParseList(donorplan.DefaultKeep)
	bank := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 12},
		{Name: "Shears", ID: 1735, Count: 1},
		{Name: "Uncut sapphire", ID: 1623, Count: 2},
		{Name: "Bow string", ID: 1777, Count: 5200},
	}
	rowNames := func(rs []donorplan.ItemRow) []string {
		out := []string{}
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return out
	}
	if donorplan.DefaultMuleAt != 5000 {
		t.Error("the default threshold is 5000")
	}
	if got := rowNames(MuleRows(bank, keep)); !reflect.DeepEqual(got, []string{"Bow string", "Uncut sapphire"}) {
		t.Errorf("MuleRows(keep) = %v", got)
	}
	if got := rowNames(MuleRows(bank, nil)); !reflect.DeepEqual(got, []string{"Bow string", "Coins", "Shears", "Uncut sapphire"}) {
		t.Errorf("MuleRows(nil) = %v", got)
	}
	pack := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 12},
		{Name: "Uncut sapphire", ID: 1624, Count: 2},
		{Name: "Bow string", ID: 1778, Count: 5200},
	}
	if got := MuleHaul(pack, keep); got != 2 {
		t.Errorf("MuleHaul = %d", got)
	}
	want := []donorplan.LoadGroup{{Name: "Bow string", ID: 1778, Slots: 1}, {Name: "Uncut sapphire", ID: 1624, Slots: 1}}
	if got := MuleLoad(pack, 28, keep); !reflect.DeepEqual(got, want) {
		t.Errorf("MuleLoad(28) = %+v", got)
	}
	if got := MuleLoad(pack, 1, keep); len(got) != 1 || got[0].Name != "Bow string" {
		t.Errorf("MuleLoad(1) = %+v", got)
	}
}

func TestSettingsDecodeFromTheManifestKeys(t *testing.T) {
	s := DefaultSettings()
	raw := `{"mule":"mulecon","muleAt":2000,"flaxSource":"Bank first, then pick","pickpocketTile":{"x":3222,"z":3218,"level":0},"restBelowHp":55}`
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	s = s.Normalised()
	if s.Mule != "mulecon" || s.MuleAt != 2000 || s.FlaxSource != BankFlax || s.PickpocketTile != freshstart.PickpocketSpot || s.RestBelow() != 0.55 {
		t.Errorf("decoded = %+v", s)
	}
	if s.GuideNPC != "RuneScape Guide" || s.Fare != 60 || s.DropAt != 27 || s.MuleKeep != "Coins, Shears" {
		t.Errorf("defaults kept = %+v", s)
	}
	d := s.Donor()
	if !d.Enabled() || !reflect.DeepEqual(d.Keep, []string{"Coins", "Shears", "Flax"}) {
		t.Errorf("donor = %+v", d)
	}
}

func TestSettingsNormalised(t *testing.T) {
	s := Settings{GuideNPC: "  ", Fare: -3, DropAt: 0, SkirtCost: -1, MuleAt: -10, RestBelowHP: 99, LogDetail: "VERBOSE"}.Normalised()
	if s.GuideNPC != freshstart.DefaultGuideNPC || s.SkipOption != freshstart.DefaultSkipOption || s.PickpocketTarget != "Man" {
		t.Errorf("blank names = %+v", s)
	}
	if s.Fare != 0 || s.DropAt != 1 || s.SkirtCost != 0 || s.MuleAt != 0 {
		t.Errorf("bounds = %+v", s)
	}
	if s.RestBelow() != 0.9 || !s.Verbose() {
		t.Errorf("rest %v, verbose %v", s.RestBelow(), s.Verbose())
	}
	if (Settings{RestBelowHP: 1}).RestBelow() != 0.05 {
		t.Error("rest floor")
	}
	if s.Donor().Enabled() {
		t.Error("a zero threshold hands nothing over")
	}
}

func TestKeepLists(t *testing.T) {
	pick := DefaultSettings()
	if got := MuleKeep(pick); !reflect.DeepEqual(got, []string{"Coins", "Shears"}) {
		t.Errorf("MuleKeep = %v", got)
	}
	if got := PackKeep(pick); !reflect.DeepEqual(got, []string{"Coins", "Coins", "Shears"}) {
		t.Errorf("PackKeep = %v", got)
	}
	if got := StoreKeep(); len(got) != 5 {
		t.Errorf("StoreKeep = %v", got)
	}
}

func TestHandoffAfterBanking(t *testing.T) {
	s := DefaultSettings()
	now := time.UnixMilli(10_000)
	if HandoffAfterBanking(s, nil, now, time.Time{}) {
		t.Error("an uncounted bank")
	}
	if HandoffAfterBanking(s, intp(4999), now, time.Time{}) || !HandoffAfterBanking(s, intp(5000), now, time.Time{}) {
		t.Error("the threshold")
	}
	if HandoffAfterBanking(s, intp(6000), now, now.Add(time.Minute)) {
		t.Error("before the retry time")
	}
	s.Mule = ""
	if HandoffAfterBanking(s, intp(6000), now, time.Time{}) {
		t.Error("no mule")
	}
}

// The strings round draws the flax in the same visit only when the run draws flax from the bank, the bank holds some, no
// hand-over falls due on the strings it will hold, and it is not the last cycle of a bounded run (twopertick-1012).
func TestDrawFlaxWithStrings(t *testing.T) {
	s := DefaultSettings()
	s.FlaxSource = BankFlax
	now := time.UnixMilli(10_000)
	if !DrawFlaxWithStrings(s, 300, 100, 27, now, time.Time{}, 0, 0) {
		t.Error("flax in the bank, no hand-over due: draw it in the same visit")
	}
	if DrawFlaxWithStrings(s, 0, 100, 27, now, time.Time{}, 0, 0) {
		t.Error("no flax in the bank: nothing to draw")
	}
	s.FlaxSource = PickFlax
	if DrawFlaxWithStrings(s, 300, 100, 27, now, time.Time{}, 0, 0) {
		t.Error("a run that picks its flax never draws it")
	}
	s.FlaxSource = MuleFlax
	if !DrawFlaxWithStrings(s, 300, 100, 27, now, time.Time{}, 0, 0) {
		t.Error("the mule-flax source draws the bank first too")
	}
	s.FlaxSource = BankFlax
	// The hand-over is tested on the strings the bank will hold afterwards: 4990 + 27 crosses 5000.
	if DrawFlaxWithStrings(s, 300, 4990, 27, now, time.Time{}, 0, 0) {
		t.Error("a hand-over falls due on this deposit: the pack must stay empty for it")
	}
	if !DrawFlaxWithStrings(s, 300, 4990, 27, now, now.Add(time.Hour), 0, 0) {
		t.Error("a hand-over not allowed yet does not stop the draw")
	}
	if DrawFlaxWithStrings(s, 300, 100, 27, now, time.Time{}, 2, 3) {
		t.Error("the last cycle of a bounded run stops after banking: no flax to carry")
	}
	if !DrawFlaxWithStrings(s, 300, 100, 27, now, time.Time{}, 1, 3) {
		t.Error("a bounded run with cycles left draws")
	}
}

func TestCrafterRoadAndPhases(t *testing.T) {
	r := Road()
	if r.Destination != Seers().Bank || r.WoolUntil != BowStringLevel || r.Radius != freshstart.DestinationRadius {
		t.Errorf("Road = %+v", r)
	}
	bank := Seers().Bank
	s := freshstart.Snapshot{MainModal: -1, Tile: &bank, Crafting: 10}
	if got := DecidePhase(s); got != freshstart.PhaseArrived || DescribePhase(got) != ArrivedDescription {
		t.Errorf("at the Seers bank = %s", got)
	}
	if got := DropNames(9); !reflect.DeepEqual(got, []string{"Ball of wool"}) {
		t.Errorf("DropNames(9) = %v", got)
	}
	if got := DropNames(10); !reflect.DeepEqual(got, []string{"Ball of wool", "Wool"}) {
		t.Errorf("DropNames(10) = %v", got)
	}
	for xp, want := range map[int]int{0: 1154, 1000: 154, 5000: 0} {
		if got := XPToBowStrings(xp); got != want {
			t.Errorf("XPToBowStrings(%d) = %d, want %d", xp, got, want)
		}
	}
}

func TestDecideSeers(t *testing.T) {
	flaxes := func(n int) []freshstart.PackItem {
		out := []freshstart.PackItem{{Name: "Coins", Count: 5}}
		for i := 0; i < n; i++ {
			out = append(out, freshstart.PackItem{Name: "Flax", Count: 1})
		}
		return out
	}
	cases := []struct {
		name string
		v    SeersView
		want SeersAction
	}{
		{"shears left from the trip", SeersView{Pack: []freshstart.PackItem{{Name: "Coins", Count: 5}, {Name: "Shears", Count: 1}}}, SeersBankAll},
		{"a full pack of flax", SeersView{Pack: flaxes(27), PackFull: true}, SeersSpin},
		{"a full pack of bow strings", SeersView{Pack: []freshstart.PackItem{{Name: "Bow string", Count: 27}}, PackFull: true}, SeersBankStrings},
		{"strings and no flax", SeersView{Pack: []freshstart.PackItem{{Name: "Bow string", Count: 5}}}, SeersBankStrings},
		{"strings and flax, not full", SeersView{Pack: append(flaxes(4), freshstart.PackItem{Name: "Bow string", Count: 3}), FlaxSource: PickFlax}, SeersPick},
		{"part-full of flax", SeersView{Pack: flaxes(10), FlaxSource: PickFlax}, SeersPick},
		{"draw from an unread bank", SeersView{Pack: flaxes(0), FlaxSource: BankFlax}, SeersDrawFlax},
		{"the bank has run dry", SeersView{Pack: flaxes(0), FlaxSource: BankFlax, FlaxInBank: intp(0)}, SeersPick},
		{"the bank has flax", SeersView{Pack: flaxes(0), FlaxSource: BankFlax, FlaxInBank: intp(5)}, SeersDrawFlax},
	}
	for _, c := range cases {
		if got := DecideSeers(c.v); got != c.want {
			t.Errorf("%s: DecideSeers = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRoutes(t *testing.T) {
	l := Seers()
	if RouteToBank(l.Wheel) != MoveClimbDown || RouteToBank(l.Field) != MoveHere {
		t.Error("RouteToBank")
	}
	if RouteToField(l.Wheel) != MoveClimbDown || RouteToField(l.Bank) != MoveWalk || RouteToField(tile.Tile{X: l.Field.X + 5, Z: l.Field.Z}) != MoveHere {
		t.Error("RouteToField")
	}
	if RouteToWheel(l.Bank) != MoveWalk || RouteToWheel(tile.Tile{X: l.Ladder.X + 1, Z: l.Ladder.Z}) != MoveClimbUp || RouteToWheel(l.Wheel) != MoveHere {
		t.Error("RouteToWheel")
	}
}

func TestChooseFlax(t *testing.T) {
	f := Seers().Field
	locs := []Loc{
		{Name: "Flax", Tile: tile.Tile{X: f.X + 6, Z: f.Z}, Ops: []string{"Pick"}},
		{Name: "Flax", Tile: tile.Tile{X: f.X + 2, Z: f.Z}, Ops: []string{"Pick"}},
		{Name: "Flax", Tile: tile.Tile{X: f.X + 1, Z: f.Z}, Ops: []string{"Pick"}},
		{Name: "Flax", Tile: tile.Tile{X: f.X + 20, Z: f.Z}, Ops: []string{"Pick"}},
		{Name: "Spinning wheel", Tile: f, Ops: []string{"Spin"}},
	}
	me := tile.Tile{X: f.X + 3, Z: f.Z}
	skipped := map[string]bool{FlaxKey(locs[1].Tile): true}
	if got := ChooseFlax(locs, me, func(k string) bool { return skipped[k] }); got != 2 {
		t.Errorf("ChooseFlax = %d, want the nearest unskipped stalk in scope", got)
	}
	if got := ChooseFlax(locs[3:], me, nil); got != -1 {
		t.Errorf("out of scope = %d", got)
	}
	if FlaxKey(tile.Tile{X: 1, Z: 2, Level: 0}) != "flax:1,2,0" {
		t.Error("FlaxKey")
	}
	if AfterPick(true, true) != PickGot || AfterPick(false, true) != PickUnreachable || AfterPick(false, false) != PickTaken {
		t.Error("AfterPick")
	}
}
