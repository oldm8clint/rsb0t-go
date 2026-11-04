package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/bowstringplan"
)

// The ids content 289 gives each bow: the unstrung obj and the strung one (the strung costs twice as
// much), as bowstringrunner.ResolveIDs reads them from the object config.
var bowObjs = map[string][2]int{
	"Yew shortbow": {68, 857}, "Yew longbow": {66, 855}, "Maple longbow": {62, 851}, "Maple shortbow": {64, 853},
	"Willow longbow": {58, 847}, "Willow shortbow": {60, 849}, "Oak longbow": {56, 845}, "Oak shortbow": {54, 843},
}

func testBowIDs(bow string) (int, int, bool) {
	ids, ok := bowObjs[bow]
	return ids[0], ids[1], ok
}

// The published bank as peerrunner.Holdings writes it for bows: the merged row of the name AND one row
// per obj under its qualified name.
func bank(rows map[string]int) Stock { return stockMap(rows) }

// THE BUG (live, 6 Oct 2026 16:41: "the mule's bank and this account's hold 36368 bow-and-string pairs
// ... a bows round is due"): the mule held 130,712 STRUNG Yew shortbows and no unstrung one, and the
// merged row of the name counted them all as bows to string, every few minutes, forever.
func TestBowsReadyDoesNotCountStrungBowsAsUnstrungOnes(t *testing.T) {
	set := DefaultSettings() // the owner's 20,000
	published := map[string]int{
		"Yew shortbow": 130712, "Yew shortbow id857": 130712, // strung only
		"Yew longbow": 7922 + 5519, "Yew longbow id66": 7922, "Yew longbow id855": 5519,
		"Maple longbow": 117044 + 55826, "Maple longbow id62": 117044, "Maple longbow id851": 55826,
		"Willow longbow": 413939 + 1508, "Willow longbow id58": 413939, "Willow longbow id847": 1508,
		"Bow string": 36368,
	}
	var led Ledger
	// Control: read by the merged name alone, as the code did, the mule is stocked for a round of Yew
	// shortbows no matter that none is unstrung.
	if r := BowsReady(set, 99, bank(published), &led); !r.Ready || r.Best.Name != "Yew longbow" {
		t.Fatalf("control (the estimate by name): %+v", r)
	}
	// Read from the per-obj rows: the unstrung bows are 7,922 Yew longbows, 117,044 Maple longbows and
	// 413,939 Willow longbows, and the Yew shortbows are none at all.
	r := BowsReadyFor(set, 99, bank(published), &led, testBowIDs)
	if r.Bows != 7922+117044+413939 || r.Strings != 36368 || r.Pairs != 36368 || r.Best.Name != "Yew longbow" {
		t.Fatalf("per-obj rows: %+v", r)
	}
	// With the longbows gone too, nothing is unstrung but willow and maple: Yew shortbows never count.
	only := map[string]int{"Yew shortbow": 130712, "Yew shortbow id857": 130712, "Bow string": 36368}
	r = BowsReadyFor(set, 99, bank(only), &led, testBowIDs)
	if r.Ready || r.Bows != 0 || r.Found || !strings.Contains(r.Why, "no unstrung bow") {
		t.Fatalf("only strung Yew shortbows held: %+v", r)
	}
	// And the same through Decide, which is what chose the round: ScarabSwarm stays.
	snap := Snapshot{Now: time.Date(2026, 10, 6, 16, 41, 0, 0, time.UTC), MuleHeard: true, MuleSameWorld: true, Stock: bank(only),
		FletchingLevel: 69, FletchingKnown: true, Ledger: &Ledger{}, BowIDs: testBowIDs} // under the Yew longbow's 70: Yew shortbows are strung
	if d := Decide(set, snap); d.Mode == ModeBows {
		t.Fatalf("a bow round was chosen for 130,712 strung bows: %+v", d)
	}
	snap.BowIDs = nil // the control: with no ids the merged row is read, as before
	if d := Decide(set, snap); d.Mode != ModeBows {
		t.Fatalf("control: the merged row reads as a round: %+v", d)
	}
}

// The unstrung bows of a real round are counted, exactly, however many strung ones the name hides.
func TestBowsReadyCountsTheUnstrungRowExactly(t *testing.T) {
	set := orderSettings()
	published := map[string]int{
		"Maple longbow": 400 + 5000, "Maple longbow id62": 400, "Maple longbow id851": 5000, "Bow string": 1000,
	}
	var led Ledger
	led.NoteStrungDelivered("Maple longbow", 5000) // the estimate would agree here ...
	r := BowsReadyFor(set, 55, bank(published), &led, testBowIDs)
	if !r.Ready || r.Bows != 400 || r.Pairs != 400 {
		t.Fatalf("%+v", r)
	}
	// ... and it does not need the ledger at all: a ledger that knows nothing reads the same.
	if r := BowsReadyFor(set, 55, bank(published), &Ledger{}, testBowIDs); r.Bows != 400 {
		t.Fatalf("without the ledger: %+v", r)
	}
	// A bank that holds only strung bows of a name has a per-obj row for the strung obj and none for
	// the unstrung one: exact zero, not "unknown".
	if n, exact := bowstringplan.MuleUnstrung(bank(map[string]int{"Maple longbow": 70, "Maple longbow id851": 70}), "Maple longbow", 62, 851, 0); n != 0 || !exact {
		t.Fatalf("strung only: %d exact=%v", n, exact)
	}
	// A bank with nothing of the bow at all is exactly none.
	if n, exact := bowstringplan.MuleUnstrung(bank(map[string]int{}), "Maple longbow", 62, 851, 0); n != 0 || !exact {
		t.Fatalf("nothing: %d exact=%v", n, exact)
	}
}

// Only when a bank carries no per-obj row of a bow at all is the merged count the estimate, less the
// strung bows this account handed the mule, as before.
func TestBowsReadyFallsBackToTheEstimateWhenNoPerObjRowExists(t *testing.T) {
	set := orderSettings()
	merged := map[string]int{"Maple longbow": 300, "Bow string": 1000}
	var led Ledger
	if r := BowsReadyFor(set, 55, bank(merged), &led, testBowIDs); !r.Ready || r.Bows != 300 {
		t.Fatalf("merged row only: %+v", r)
	}
	led.NoteStrungDelivered("Maple longbow", 280)
	if r := BowsReadyFor(set, 55, bank(merged), &led, testBowIDs); r.Ready || r.Bows != 20 {
		t.Fatalf("with 280 strung at the mule: %+v", r)
	}
	if n, exact := bowstringplan.MuleUnstrung(bank(merged), "Maple longbow", 62, 851, 280); n != 20 || exact {
		t.Fatalf("MuleUnstrung fallback: %d exact=%v", n, exact)
	}
	// A name whose ids cannot be resolved falls back too.
	if r := BowsReadyFor(set, 55, bank(merged), &Ledger{}, func(string) (int, int, bool) { return 0, 0, false }); r.Bows != 300 {
		t.Fatalf("unresolved ids: %+v", r)
	}
	// No published bank at all: nothing is ready and the reason says so.
	if r := BowsReadyFor(set, 55, nil, &led, testBowIDs); r.Ready || r.Why == "" {
		t.Fatalf("unpublished: %+v", r)
	}
}

// AdjustedStock hands the kit planner the unstrung count under a bow's plain name AND under the
// qualified name of its unstrung obj, and leaves every other name alone.
func TestAdjustedStockAnswersUnstrungCountsFromTheirRows(t *testing.T) {
	published := map[string]int{
		"Yew shortbow": 130712, "Yew shortbow id857": 130712, "Yew longbow": 9000, "Yew longbow id66": 7922, "Yew longbow id855": 1078,
		"Bow string": 5, "Uncut ruby": 7,
	}
	adj := AdjustedStock(bank(published), &Ledger{}, testBowIDs)
	for name, want := range map[string]int{
		"Yew shortbow": 0, "Yew shortbow id68": 0, // no unstrung Yew shortbow is held
		"Yew longbow": 7922, "Yew longbow id66": 7922,
		"Yew longbow id855": 1078, // the strung obj's own row is stock's own answer
		"Bow string":        5, "Uncut ruby": 7,
	} {
		if n, ok := adj(name); !ok || n != want {
			t.Errorf("%q = %d ok=%v, want %d", name, n, ok, want)
		}
	}
}
