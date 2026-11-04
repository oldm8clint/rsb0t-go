package orderplan

import (
	"strings"
	"testing"
	"time"
)

// kitPolicy serves anything in stock, up to 1000 a line, with a generous per-donor cap of one
// order: the shape the live mule runs for the Scarab kit catalogue.
func kitPolicy() Policy {
	return Policy{
		Allowed:           []string{"acct1"},
		Open:              OpenStock{Enabled: true, Max: 1000, WindowMax: 100000},
		MaxOrdersPerDonor: 1,
		MaxUnitsPerItem:   100000,
	}
}

var kitFacts = map[string]KitFacts{
	"cake":      {HasNote: true},
	"shark":     {HasNote: true},
	"rope":      {HasNote: true},
	"fire rune": {Stackable: true},
	"mind rune": {Stackable: true},
	"coins":     {Stackable: true},
	"staff":     {HasNote: false},
}

func factsOf(item string) (KitFacts, bool) {
	f, ok := kitFacts[strings.ToLower(item)]
	return f, ok
}

func plenty(string) int { return 5000 }

func lineOf(t *testing.T, k KitDecision, item string) KitLineDecision {
	t.Helper()
	for _, l := range k.Lines {
		if strings.EqualFold(l.Item, item) {
			return l
		}
	}
	t.Fatalf("no line for %s in %+v", item, k.Lines)
	return KitLineDecision{}
}

// A kit that fits is handed over unnoted -- except FOOD, which always travels as notes (owner, 2 Oct
// 2026: one trade slot per food type; the donor banks the notes and withdraws food as it needs it).
func TestAKitThatFitsCrossesUnnotedExceptFood(t *testing.T) {
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Cake", Qty: 10}, {Item: "Fire rune", Qty: 300}, {Item: "Staff", Qty: 1}, {Item: "Rope", Qty: 2}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 20)
	if k.Serving() != 4 || k.Slots(factsOf) != 5 {
		t.Fatalf("serving %d slots %d, want 4 and 5 (cake as one note stack, rune stack, staff, two ropes): %+v", k.Serving(), k.Slots(factsOf), k.Lines)
	}
	if !lineOf(t, k, "Cake").Noted {
		t.Fatal("ten cakes crossed unnoted: food must travel as notes")
	}
	if lineOf(t, k, "Rope").Noted {
		t.Fatal("rope was noted though the kit fits unnoted (only food is always noted)")
	}
}

func TestFoodAlwaysTravelsAsNotesWhateverTheCount(t *testing.T) {
	for _, tc := range []struct {
		item string
		qty  int
		want bool
	}{
		{"Shark", 500, true}, {"Cake", 2, true}, {"Cake", 1, false}, {"Bass", 4, true},
		{"2/3 Cake", 5, true}, {"Rope", 10, false}, {"Chaos rune", 1000, false}, {"Staff", 1, false},
	} {
		if got := KitFoodAsNotes(tc.item, tc.qty); got != tc.want {
			t.Errorf("KitFoodAsNotes(%q, %d) = %v, want %v", tc.item, tc.qty, got, tc.want)
		}
	}
	// Food with no note form still crosses unnoted rather than being refused.
	facts := func(string) (KitFacts, bool) { return KitFacts{HasNote: false}, true }
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Cake", Qty: 5}}, kitPolicy(), NewLedger(time.Hour), plenty, facts, 20)
	if l := k.Lines[0]; l.Qty != 5 || l.Noted {
		t.Fatalf("a food with no bank note was %+v, want 5 unnoted", l)
	}
}

// The owner's ruling: ONE trade of at most 28 slots. Twenty mixed lines, several of them
// multi-unit food, cannot fit unnoted; the biggest non-stackable lines go as notes until the kit
// fits, and nothing is dropped.
func TestATwentyLineKitIsNotedDownToOneTrade(t *testing.T) {
	lines := []KitLine{{Item: "Cake", Qty: 20}, {Item: "Shark", Qty: 15}, {Item: "Rope", Qty: 5}, {Item: "Fire rune", Qty: 500}, {Item: "Mind rune", Qty: 500}, {Item: "Coins", Qty: 3000}}
	for i := 0; i < 14; i++ {
		name := "Item" + string(rune('a'+i))
		kitFacts[strings.ToLower(name)] = KitFacts{HasNote: true}
		lines = append(lines, KitLine{Item: name, Qty: 1})
	}
	defer func() {
		for i := 0; i < 14; i++ {
			delete(kitFacts, strings.ToLower("Item"+string(rune('a'+i))))
		}
	}()
	k := DecideKit(time.Now(), "acct1", lines, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 0)
	if k.Serving() != 20 {
		t.Fatalf("serving %d of 20: %+v", k.Serving(), k.Lines)
	}
	if s := k.Slots(factsOf); s > KitLinesMax {
		t.Fatalf("the kit needs %d slots, more than one trade", s)
	}
	if !lineOf(t, k, "Cake").Noted || !lineOf(t, k, "Shark").Noted {
		t.Fatalf("the two big food lines should travel as notes: %+v", k.Lines)
	}
	if lineOf(t, k, "Rope").Noted {
		t.Fatal("rope needed no notes once the food was noted")
	}
}

// The donor's reported room binds before the screen's 28 does.
func TestTheDonorsRoomDecidesWhatIsNoted(t *testing.T) {
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Cake", Qty: 10}, {Item: "Fire rune", Qty: 50}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 5)
	if !lineOf(t, k, "Cake").Noted || k.Slots(factsOf) != 2 {
		t.Fatalf("10 cake into 5 free slots must travel as notes: %+v", k.Lines)
	}
}

// With no way to note it, a line that does not fit is cut, and lines at the END are dropped first.
func TestWhatCannotBeNotedIsCutFromTheEnd(t *testing.T) {
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Fire rune", Qty: 50}, {Item: "Staff", Qty: 1}, {Item: "Staff2", Qty: 6}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 4)
	if got := lineOf(t, k, "Staff2"); got.Qty != 2 || !strings.Contains(got.Reason, DeclineKitNoRoom) {
		t.Fatalf("the last line should be cut to the 2 slots left: %+v", got)
	}
	if k.Slots(factsOf) != 4 {
		t.Fatalf("slots %d, want 4", k.Slots(factsOf))
	}
}

// Each line is judged by Decide itself: a line the bank does not hold is declined with Decide's
// own words, and the rest of the kit is still served.
func TestAnEmptyLineIsDeclinedAndTheRestServed(t *testing.T) {
	stock := func(item string) int {
		if strings.EqualFold(item, "Shark") {
			return 0
		}
		return 100
	}
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Cake", Qty: 5}, {Item: "Shark", Qty: 5}}, kitPolicy(), NewLedger(time.Hour), stock, factsOf, 0)
	if l := lineOf(t, k, "Shark"); l.Qty != 0 || l.Reason != "none in the bank" {
		t.Fatalf("shark line %+v, want declined \"none in the bank\"", l)
	}
	if lineOf(t, k, "Cake").Qty != 5 {
		t.Fatal("cake line was not served")
	}
}

// The per-donor order cap is checked once for the whole kit: a delivered kit counts as ONE order,
// so it does not spend a one-order window on its first line and refuse the rest. CONTROL: the
// same lines recorded as separate orders do exhaust the cap.
func TestAKitCountsAsOneOrder(t *testing.T) {
	now := time.Now()
	pol := kitPolicy()
	pol.MaxOrdersPerDonor = 2
	led := NewLedger(time.Hour)
	led.RecordKit(now, "acct1", "k1", []ItemCount{{"Cake", 5}, {"Fire rune", 50}, {"Rope", 2}})
	if n := led.OrdersTo(now, "acct1"); n != 1 {
		t.Fatalf("a three-line kit counted as %d orders", n)
	}
	k := DecideKit(now, "acct1", []KitLine{{Item: "Cake", Qty: 5}}, pol, led, plenty, factsOf, 0)
	if k.Serving() != 1 {
		t.Fatalf("a second kit inside a two-order window was refused: %+v", k.Lines)
	}

	control := NewLedger(time.Hour)
	for _, it := range []string{"Cake", "Fire rune", "Rope"} {
		control.Record(now, "acct1", it, 1)
	}
	if n := control.OrdersTo(now, "acct1"); n != 3 {
		t.Fatalf("control: three separate orders counted as %d", n)
	}
}

// A donor off the allow list, or past its cap, is refused on every line with the same reason.
func TestADonorLevelRefusalCoversEveryLine(t *testing.T) {
	now := time.Now()
	led := NewLedger(time.Hour)
	led.Record(now, "acct1", "Knife", 1)
	k := DecideKit(now, "acct1", []KitLine{{Item: "Cake", Qty: 5}, {Item: "Fire rune", Qty: 50}}, kitPolicy(), led, plenty, factsOf, 0)
	for _, l := range k.Lines {
		if l.Qty != 0 || l.Reason != "already handed you one recently" {
			t.Fatalf("line %+v, want refused for the donor's cap", l)
		}
	}
	k = DecideKit(now, "stranger", []KitLine{{Item: "Cake", Qty: 5}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 0)
	if k.Serving() != 0 || k.Lines[0].Reason != "not on the allow list" {
		t.Fatalf("a stranger's kit: %+v", k.Lines)
	}
}

func TestNormaliseKitMergesAndBounds(t *testing.T) {
	got, bad := NormaliseKit([]KitLine{{Item: "Cake", Qty: 2}, {Item: " cake ", Qty: 3}, {Item: "Rope", Qty: 0}, {Item: "Fire rune", Qty: 5}})
	if bad != "" || len(got) != 2 || got[0].Qty != 5 || got[1].Item != "Fire rune" {
		t.Fatalf("got %+v %q", got, bad)
	}
	var long []KitLine
	for i := 0; i < KitLinesMax+1; i++ {
		long = append(long, KitLine{Item: "Item" + string(rune('A'+i%26)) + string(rune('a'+i/26)), Qty: 1})
	}
	if _, bad := NormaliseKit(long); bad != DeclineKitTooLong {
		t.Fatalf("29 lines: %q", bad)
	}
	if _, bad := NormaliseKit(nil); bad != DeclineKitEmpty {
		t.Fatalf("empty kit: %q", bad)
	}
}

// A non-stackable line above a pack travels as notes, as a single order does; one with no note
// form is refused rather than half-sent.
func TestABigNonStackableLineIsNotedOrRefused(t *testing.T) {
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Cake", Qty: 40}, {Item: "Staff", Qty: 30}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 0)
	if l := lineOf(t, k, "Cake"); !l.Noted || l.Qty != 40 {
		t.Fatalf("40 cake: %+v", l)
	}
	if l := lineOf(t, k, "Staff"); l.Qty != 0 || l.Reason != DeclineKitNoNote {
		t.Fatalf("30 staffs with no note: %+v", l)
	}
}

func TestDecideReceiveKit(t *testing.T) {
	want := []KitLine{{Item: "Cake", Qty: 10}, {Item: "Fire rune", Qty: 100}}
	base := KitReceiveView{Screen: ScreenOffer, Partner: "mule", Mule: "mule", Want: want, Planned: true}
	cases := []struct {
		name   string
		theirs []ItemCount
		mine   []ItemCount
		screen Screen
		act    ReceiveAction
	}{
		{"nothing yet", nil, nil, ScreenOffer, ReceiveWait},
		{"half the kit placed", []ItemCount{{"Cake", 10}}, nil, ScreenOffer, ReceiveWait},
		{"the whole kit", []ItemCount{{"Cake", 10}, {"Fire rune", 100}}, nil, ScreenOffer, ReceiveAccept},
		{"notes read by name", []ItemCount{{"cake", 10}, {"Fire rune", 100}}, nil, ScreenOffer, ReceiveAccept},
		{"an item not in the kit", []ItemCount{{"Cake", 10}, {"Bones", 1}}, nil, ScreenOffer, ReceiveDecline},
		{"more than wanted", []ItemCount{{"Cake", 11}}, nil, ScreenOffer, ReceiveDecline},
		{"asked to give", []ItemCount{{"Cake", 10}}, []ItemCount{{"Coins", 1}}, ScreenOffer, ReceiveDecline},
		{"a subset on the confirm screen", []ItemCount{{"Cake", 10}}, nil, ScreenConfirm, ReceiveConfirm},
	}
	for _, c := range cases {
		v := base
		v.Theirs, v.Mine, v.Screen = c.theirs, c.mine, c.screen
		if got, why := DecideReceiveKit(v); got != c.act {
			t.Errorf("%s: %v (%s), want %v", c.name, got, why, c.act)
		}
	}
	v := base
	v.Partner = "Someone"
	v.Theirs = []ItemCount{{"Cake", 10}, {"Fire rune", 100}}
	if got, _ := DecideReceiveKit(v); got != ReceiveDecline {
		t.Error("another partner's screen must be declined")
	}
	// Before the mule's plan is heard, the ask is the ceiling and a partial offer is acceptable.
	v = base
	v.Planned = false
	v.Theirs = []ItemCount{{"Cake", 4}}
	if got, _ := DecideReceiveKit(v); got != ReceiveAccept {
		t.Error("without a plan, a valid partial offer is accepted")
	}
}

// A donor that banks what it gets asks for its lines as notes (MasterCrafter's storage round; the
// a user request: "the mule should hand over items noted"). A non-stackable with a note form then
// crosses noted although the kit would fit unnoted; a stackable, and an item with no note form,
// cross as they are; a line asked noted once in a merged pair stays asked noted.
func TestADonorMayAskForNotes(t *testing.T) {
	k := DecideKit(time.Now(), "acct1", []KitLine{
		{Item: "Rope", Qty: 4, Noted: true}, {Item: "Staff", Qty: 1, Noted: true}, {Item: "Fire rune", Qty: 50, Noted: true},
	}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 20)
	if r := lineOf(t, k, "Rope"); !r.Noted || r.Qty != 4 {
		t.Fatalf("four ropes asked as notes: %+v", r)
	}
	if s := lineOf(t, k, "Staff"); s.Noted || s.Qty != 1 {
		t.Fatalf("a staff has no note form and still crosses: %+v", s)
	}
	if f := lineOf(t, k, "Fire rune"); f.Noted || f.Qty != 50 {
		t.Fatalf("a stackable crosses as itself: %+v", f)
	}
	if k.Slots(factsOf) != 3 {
		t.Fatalf("slots %d, want 3 (one a line): %+v", k.Slots(factsOf), k.Lines)
	}
	// Unasked, the same ropes cross unnoted (TestAKitThatFitsCrossesUnnotedExceptFood).
	if lineOf(t, DecideKit(time.Now(), "acct1", []KitLine{{Item: "Rope", Qty: 4}}, kitPolicy(), NewLedger(time.Hour), plenty, factsOf, 20), "Rope").Noted {
		t.Fatal("ropes noted although nobody asked")
	}
	got, bad := NormaliseKit([]KitLine{{Item: "Rope", Qty: 2}, {Item: "rope", Qty: 3, Noted: true}})
	if bad != "" || len(got) != 1 || got[0].Qty != 5 || !got[0].Noted {
		t.Fatalf("merged %+v %q", got, bad)
	}
}
