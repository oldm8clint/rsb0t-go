package crafterplan

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

var catherbyPost = tile.Tile{X: 2809, Z: 3441}

func TestMuleFlaxSourceDrawsTheBankFirstAndKeepsItsFlax(t *testing.T) {
	if !DrawsFlaxFromBank(MuleFlax, nil) || !DrawsFlaxFromBank(MuleFlax, intp(3)) || DrawsFlaxFromBank(MuleFlax, intp(0)) {
		t.Error("the mule source draws the bank like the bank source")
	}
	if !KeepsBankFlax(MuleFlax) {
		t.Error("flax the mule sent is never handed back to it")
	}
	s := DefaultSettings()
	s.FlaxSource = MuleFlax
	if got := MuleKeep(s); !reflect.DeepEqual(got, []string{"Coins", "Shears", "Flax"}) {
		t.Errorf("MuleKeep = %v", got)
	}
	if !AsksMuleForFlax(MuleFlax) || AsksMuleForFlax(BankFlax) || AsksMuleForFlax(PickFlax) || AsksMuleForFlax("Mule") {
		t.Error("only the exact mule source asks the mule")
	}
}

func TestDecideSeersAsksTheMuleOnlyWhenTheBankAndPackAreDry(t *testing.T) {
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
		{"an unread bank is looked at first", SeersView{Pack: flaxes(0), FlaxSource: MuleFlax, MuleFlaxDue: true}, SeersDrawFlax},
		{"a stocked bank is drawn", SeersView{Pack: flaxes(0), FlaxSource: MuleFlax, FlaxInBank: intp(900), MuleFlaxDue: true}, SeersDrawFlax},
		{"a dry bank and pack go to the mule", SeersView{Pack: flaxes(0), FlaxSource: MuleFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersMuleFlax},
		{"backing off picks", SeersView{Pack: flaxes(0), FlaxSource: MuleFlax, FlaxInBank: intp(0)}, SeersPick},
		{"flax in hand is topped up by picking", SeersView{Pack: flaxes(5), FlaxSource: MuleFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersPick},
		{"a full pack is spun first", SeersView{Pack: flaxes(27), PackFull: true, FlaxSource: MuleFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersSpin},
		{"strings are banked first", SeersView{Pack: []freshstart.PackItem{{Name: "Bow string", Count: 27}}, PackFull: true, FlaxSource: MuleFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersBankStrings},
		{"kit notes in the pack are banked", SeersView{Pack: flaxes(1), NotedFlax: 1000, FlaxSource: MuleFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersBankAll},
		{"notes are banked in every mode", SeersView{Pack: flaxes(1), NotedFlax: 1000, FlaxSource: PickFlax}, SeersBankAll},
		{"the bank source never asks the mule", SeersView{Pack: flaxes(0), FlaxSource: BankFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersPick},
		{"the pick source never asks the mule", SeersView{Pack: flaxes(0), FlaxSource: PickFlax, FlaxInBank: intp(0), MuleFlaxDue: true}, SeersPick},
	}
	for _, c := range cases {
		if got := DecideSeers(c.v); got != c.want {
			t.Errorf("%s: DecideSeers = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMuleFlaxDue(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	if !MuleFlaxDue(MuleFlax, now, time.Time{}) || !MuleFlaxDue(MuleFlax, now, now) {
		t.Error("no back-off running: due")
	}
	if MuleFlaxDue(MuleFlax, now, now.Add(time.Second)) {
		t.Error("a back-off still running is not due")
	}
	if MuleFlaxDue(BankFlax, now, time.Time{}) {
		t.Error("the bank source is never due")
	}
}

func TestClassifyFlaxKit(t *testing.T) {
	cases := []struct {
		bank   int
		reason string
		want   FlaxKitOutcome
	}{
		{1000, "", FlaxKitGained},
		{28, "capped to 28: no room for it in this trade", FlaxKitGained},
		{0, "none in the bank", FlaxKitEmpty},
		{-1, "I cannot identify that item", FlaxKitEmpty},
		{0, "already handed you one recently", FlaxKitDeferred},
		{0, "not on the allow list", FlaxKitDeferred},
		{-1, "", FlaxKitDeferred},
	}
	for _, c := range cases {
		if got := ClassifyFlaxKit(c.bank, c.reason); got != c.want {
			t.Errorf("ClassifyFlaxKit(%d, %q) = %s, want %s", c.bank, c.reason, got, c.want)
		}
	}
}

func TestAfterFlaxKitBacksOffWithoutHammeringTheMule(t *testing.T) {
	base := 15 * time.Minute
	if wait, n := AfterFlaxKit(FlaxKitGained, 3, base); wait != 0 || n != 0 {
		t.Errorf("a gain = %s/%d, want no wait and a reset count", wait, n)
	}
	want := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 2 * time.Hour}
	fruitless := 0
	for i, w := range want {
		var wait time.Duration
		wait, fruitless = AfterFlaxKit(FlaxKitDeferred, fruitless, base)
		if wait != w || fruitless != i+1 {
			t.Errorf("deferred kit %d = %s (%d in a row), want %s", i+1, wait, fruitless, w)
		}
	}
	if wait, n := AfterFlaxKit(FlaxKitEmpty, 0, base); wait != MaxFlaxKitBackoff || n != 1 {
		t.Errorf("an empty mule = %s/%d, want the cap at once", wait, n)
	}
	if wait, _ := AfterFlaxKit(FlaxKitDeferred, 0, 0); wait != DefaultFlaxKitBackoffMinutes*time.Minute {
		t.Errorf("no base = %s", wait)
	}
}

func TestDecidePhaseWithMuleHoldsAnAccountAtThePost(t *testing.T) {
	at := func(t tile.Tile, crafting int) freshstart.Snapshot {
		return freshstart.Snapshot{MainModal: -1, Tile: &t, Crafting: crafting, Fare: 60}
	}
	post := catherbyPost
	if tile.Chebyshev(post, Seers().Bank) <= freshstart.DestinationRadius {
		t.Fatal("the premise: the Catherby post is outside the Seers road's radius")
	}
	// Without the mule source, an account at Catherby with no coins goes pickpocketing for a fare.
	if got := DecidePhase(at(post, 10)); got == freshstart.PhaseArrived {
		t.Fatalf("premise: DecidePhase at the post = %s", got)
	}
	if got := DecidePhaseWithMule(at(post, 10), MuleFlax, post); got != freshstart.PhaseArrived {
		t.Errorf("at the post, fed by the mule = %s, want arrived", got)
	}
	near := tile.Tile{X: post.X + MulePostRadius, Z: post.Z}
	if got := DecidePhaseWithMule(at(near, 10), MuleFlax, post); got != freshstart.PhaseArrived {
		t.Errorf("at the post's edge = %s", got)
	}
	far := tile.Tile{X: post.X + MulePostRadius + 1, Z: post.Z}
	if got := DecidePhaseWithMule(at(far, 10), MuleFlax, post); got == freshstart.PhaseArrived {
		t.Error("past the post's radius is not the loop")
	}
	if got := DecidePhaseWithMule(at(post, 10), BankFlax, post); got != DecidePhase(at(post, 10)) {
		t.Error("every other source decides exactly as DecidePhase")
	}
	if got := DecidePhaseWithMule(at(post, 9), MuleFlax, post); got != DecidePhase(at(post, 9)) {
		t.Error("below crafting 10 the wool road decides")
	}
	if got := DecidePhaseWithMule(at(post, 10), MuleFlax, tile.Tile{}); got == freshstart.PhaseArrived {
		t.Error("no post known: no override")
	}
	upstairs := tile.Tile{X: post.X, Z: post.Z, Level: 1}
	if got := DecidePhaseWithMule(at(upstairs, 10), MuleFlax, post); got == freshstart.PhaseArrived {
		t.Error("another level is not the post")
	}
	bank := Seers().Bank
	if got := DecidePhaseWithMule(at(bank, 10), MuleFlax, post); got != freshstart.PhaseArrived {
		t.Errorf("the Seers bank is still the loop = %s", got)
	}
}

func TestHandoffStand(t *testing.T) {
	seers := tile.Tile{X: 2725, Z: 3491}
	if got := HandoffStand(MuleFlax, catherbyPost, seers); got != catherbyPost {
		t.Errorf("fed by the mule: meet it at its post, got %+v", got)
	}
	if got := HandoffStand(PickFlax, catherbyPost, seers); got != seers {
		t.Errorf("picking: the Seers stand as before, got %+v", got)
	}
	if got := HandoffStand(BankFlax, catherbyPost, seers); got != seers {
		t.Errorf("bank first: the Seers stand as before, got %+v", got)
	}
	if got := HandoffStand(MuleFlax, tile.Tile{}, seers); got != seers {
		t.Errorf("no post known: the Seers stand, got %+v", got)
	}
}

func TestFlaxKitSettings(t *testing.T) {
	d := DefaultSettings()
	if d.FlaxKitSize != 1000 || d.FlaxKitBackoffMinutes != 15 || d.FlaxSource != PickFlax {
		t.Errorf("defaults = %d/%d/%q", d.FlaxKitSize, d.FlaxKitBackoffMinutes, d.FlaxSource)
	}
	s := DefaultSettings()
	raw := `{"flaxSource":"Mule, then pick","flaxKitSize":500,"flaxKitBackoffMinutes":30,"mule":"mule","muleAt":500}`
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	s = s.Normalised()
	if s.FlaxSource != MuleFlax || s.FlaxKitSize != 500 || s.FlaxKitBackoffMinutes != 30 || s.Mule != "mule" || s.MuleAt != 500 {
		t.Errorf("decoded = %+v", s)
	}
	for in, want := range map[int]int{0: MinFlaxKitSize, 5: MinFlaxKitSize, 28: 28, 1000: 1000, 99999: MaxFlaxKitSize} {
		if got := (Settings{FlaxKitSize: in}).Normalised().FlaxKitSize; got != want {
			t.Errorf("flaxKitSize %d -> %d, want %d", in, got, want)
		}
	}
	for in, want := range map[int]int{-5: 1, 0: 1, 15: 15, 1000: 120} {
		if got := (Settings{FlaxKitBackoffMinutes: in}).Normalised().FlaxKitBackoffMinutes; got != want {
			t.Errorf("flaxKitBackoffMinutes %d -> %d, want %d", in, got, want)
		}
	}
}
