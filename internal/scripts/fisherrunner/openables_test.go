package fisherrunner

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/packet"
)

// Content 289 obj ids, as the lab config archive names them.
var testObjNames = map[int]string{
	405: "Casket", 406: "Casket", 407: "Oyster", 408: "Oyster", 409: "Empty oyster", 411: "Oyster pearl",
	995: "Coins", 1623: "Uncut sapphire", 985: "Half of a key", 303: "Small fishing net", 379: "Lobster",
}

func testNameOf(id int) (string, bool) { return testObjNames[id], id == 406 || id == 408 }

func TestMeasureOpenablesSplitsAMixedBurst(t *testing.T) {
	before := loot.Counts{405: 3, 407: 4, 406: 10, 995: 100, 303: 1, 379: 2}
	after := loot.Counts{406: 10, 995: 260, 1623: 1, 985: 1, 411: 3, 409: 1, 303: 1, 379: 1}
	y := measureOpenables(before, after, testNameOf, nil)
	if y.opened["Casket"] != 3 || y.opened["Oyster"] != 4 {
		t.Fatalf("opened %v, want 3 caskets and 4 oysters (noted caskets never count)", y.opened)
	}
	if y.sourceID["Casket"] != 405 || y.sourceID["Oyster"] != 407 {
		t.Fatalf("source ids %v", y.sourceID)
	}
	if want := (loot.Counts{995: 160, 1623: 1, 985: 1}); !reflect.DeepEqual(y.gained["Casket"], want) {
		t.Fatalf("casket rewards %v, want %v", y.gained["Casket"], want)
	}
	if want := (loot.Counts{411: 3, 409: 1}); !reflect.DeepEqual(y.gained["Oyster"], want) {
		t.Fatalf("oyster rewards %v, want %v", y.gained["Oyster"], want)
	}
}

func TestMeasureOpenablesNeverCreditsAKindItDidNotOpen(t *testing.T) {
	// Only oysters opened, and coins arrived. This test used to assert the coins were the
	// OYSTERS' -- "even an oddly named arrival is theirs, not a phantom casket's". The
	// principle was right and the remedy was wrong: refusing to invent a casket is correct,
	// but handing coins to the oysters is how the two cards came to show each other's loot,
	// which the fleet owner saw live on 2026-09-22.
	//
	// Coins arriving while only oysters were opened came from something else -- a pickpocket, a
	// drop, a hand-out -- so they belong to NEITHER card and are dropped.
	y := measureOpenables(loot.Counts{407: 2}, loot.Counts{411: 1, 409: 1, 995: 5}, testNameOf, nil)
	if y.opened["Casket"] != 0 || len(y.gained["Casket"]) != 0 {
		t.Fatalf("a casket source appeared with no casket opened: %+v", y)
	}
	if want := (loot.Counts{411: 1, 409: 1}); !reflect.DeepEqual(y.gained["Oyster"], want) {
		t.Fatalf("oyster rewards %v, want %v — coins are not an oyster's to claim", y.gained["Oyster"], want)
	}
	// Nothing opened (a burst that registered no click): nothing to report.
	if y := measureOpenables(loot.Counts{405: 2}, loot.Counts{405: 2}, testNameOf, nil); len(y.opened) != 0 || len(y.gained) != 0 {
		t.Fatalf("an unchanged pack reported %+v", y)
	}
}

func TestOpenablesAreCasketsAndOystersOnly(t *testing.T) {
	for _, n := range []string{"Casket", "casket", "Oyster", " oyster "} {
		if !IsOpenable(n) {
			t.Errorf("%q should be opened before a hand-off", n)
		}
	}
	for _, n := range []string{"Oyster pearl", "Empty oyster", "Raw bass", "Big fishing net", ""} {
		if IsOpenable(n) {
			t.Errorf("%q must not be opened", n)
		}
	}
}

// The two cards showed each other's loot, live on 2026-09-22. The old rule guessed from which
// openable had been opened and only consulted the reward when BOTH had been — so a burst of
// oysters filed everything as Oyster and a burst of caskets filed everything as Casket.
//
// This asserts the fix is structural, which is what users asked for ("ensuring it cant
// happen"): every reward in content 289's two tables lands on its own card, whatever was opened
// alongside it.
func TestACasketRewardCanNeverLandOnTheOysterCard(t *testing.T) {
	// Content 289: [opheld1,oystershell] yields exactly these two, and [opheld1,casket] yields
	// coins, uncut gems, a cosmic talisman or a key half — nothing oyster-named.
	oysterRewards := map[int]string{411: "Oyster pearl", 413: "Empty oyster"}
	casketRewards := map[int]string{995: "Coins", 1623: "Uncut sapphire", 1621: "Uncut emerald",
		1619: "Uncut ruby", 1617: "Uncut diamond", 1454: "Cosmic talisman", 985: "Half of a key"}

	nameOf := func(id int) (string, bool) {
		if n, ok := oysterRewards[id]; ok {
			return n, false
		}
		if n, ok := casketRewards[id]; ok {
			return n, false
		}
		return "", false
	}

	for id, name := range oysterRewards {
		if got := openableFor(id, nameOf); got != "Oyster" {
			t.Errorf("%s (%d) filed under %q — an oyster's own reward must never reach the casket card", name, id, got)
		}
	}
	for id, name := range casketRewards {
		if got := openableFor(id, nameOf); got != "Casket" {
			t.Errorf("%s (%d) filed under %q — a casket reward must never reach the oyster card", name, id, got)
		}
	}
	// An id the cache cannot name is a casket reward: that table is the open-ended one.
	if got := openableFor(99999, nameOf); got != "Casket" {
		t.Errorf("an unknown reward filed under %q, want Casket", got)
	}
}

// sendCounter counts every packet a fisher sends.
type sendCounter struct {
	*fakeSession
	// sends counts the clicks sent, not the messages: the fast opener sends five to a message.
	sends int
}

func (s *sendCounter) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	for b := w.Data[:w.Pos]; len(b) > 0; {
		n := 7 // an OPHELD: the opcode, obj, slot, component
		if len(b) < n {
			n = len(b)
		}
		s.sends++
		b = b[n:]
	}
	return nil
}

// TestOpeningStopsClickingWhenStopped: the first wait of the burst reports a stop. The burst's
// own five clicks are already out, but nothing more may follow; the retry pass used to click
// every casket still held a second time.
func TestOpeningStopsClickingWhenStopped(t *testing.T) {
	was := openableID
	openableID = func(_ *Deps, id int) bool { return id == 405 }
	t.Cleanup(func() { openableID = was })

	f := &sendCounter{fakeSession: newFakeSession()}
	setPackForEatTest(t, f.fakeSession, [2]int32{405, 1}, [2]int32{405, 1}, [2]int32{405, 1}, [2]int32{405, 1}, [2]int32{405, 1})
	d := &Deps{Session: f, Delay: func(time.Duration) bool { return false }}
	if opened := openHeldOpenables(d); opened != 0 {
		t.Fatalf("opened = %d, want 0 (nothing left the pack)", opened)
	}
	if f.sends != 5 {
		t.Fatalf("sent %d packets after a stop, want only the burst's 5", f.sends)
	}
}
