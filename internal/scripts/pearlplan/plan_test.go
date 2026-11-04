package pearlplan

import (
	"reflect"
	"strings"
	"testing"
)

func stock(m map[string]int) Stock {
	return func(item string) (int, bool) {
		for k, v := range m {
			if strings.EqualFold(k, item) {
				return v, true
			}
		}
		return 0, true
	}
}

// The content facts the round rests on (pearlplan's package comment cites each file).
func TestContentFacts(t *testing.T) {
	if Level != 41 || XPTenths != 32 || BoltsPerUse != 10 {
		t.Fatalf("level %d, XP %d tenths, %d bolts a use", Level, XPTenths, BoltsPerUse)
	}
	big, ok := KindByID(PearlsID)
	if !ok || big.Name != "Oyster pearls" || big.Tips != 24 || big.Note != 414 {
		t.Fatalf("%+v", big)
	}
	small, ok := KindByID(PearlNoteID)
	if !ok || small.Name != "Oyster pearl" || small.Tips != 6 || small.ID != 411 {
		t.Fatalf("a note finds its kind: %+v", small)
	}
	if Kinds[0].ID != PearlsID {
		t.Fatal("Oyster pearls (24 tips a use) must be cut first")
	}
	if _, ok := KindByID(TipsID); ok {
		t.Fatal("tips are not a pearl")
	}
}

func TestPlanAskAsksForEveryPearlAndEveryBolt(t *testing.T) {
	mule := HeldIn(stock(map[string]int{"Oyster pearl": 104000, "Oyster pearls": 2500, "Bolts": 612000, "Pearl bolttips": 9}))
	a := PlanAsk(mule)
	want := []Line{{Pearls, 2500}, {Pearl, 104000}, {Bolt, 612000}, {Tips, 9}} // the mule's tips ride along (2 Nov 2026)
	if !reflect.DeepEqual(a.Lines, want) || a.Pearls != 106500 || a.Bolts != 612000 {
		t.Fatalf("%+v", a)
	}
	if a := PlanAsk(Held{}); len(a.Lines) != 0 {
		t.Fatalf("an empty mule: %+v", a)
	}
	if a := PlanAsk(Held{Bolts: 50}); !reflect.DeepEqual(a.Lines, []Line{{Bolt, 50}}) {
		t.Fatalf("bolts only: %+v", a)
	}
}

// The owner's thresholds (6 Oct 2026): 20,000 oyster pearls in the two banks together are cut,
// 20,000 Bolts in the two banks together take the tips; what this account's own bank holds alone is
// a Resume.
func TestReady(t *testing.T) {
	const pearls, bolts = 20000, 20000
	mule := Held{Pearl: 15000, Bolts: 100}
	own := Held{Pearl: 4999}
	if r := Ready(pearls, bolts, 60, true, mule, own, true); r.Ready {
		t.Fatalf("19,999 pearls in the two banks is under 20,000: %+v", r)
	}
	own.Pearls = 1
	if r := Ready(pearls, bolts, 60, true, mule, own, true); !r.Ready || r.Resume || !strings.Contains(r.Why, "20000 oyster pearls together") {
		t.Fatalf("20,000 pearls of both kinds in the two banks: %+v", r)
	}
	if r := Ready(pearls, bolts, 40, true, mule, own, true); r.Ready || !strings.Contains(r.Why, "41") {
		t.Fatalf("Fletching 40: %+v", r)
	}
	if r := Ready(pearls, bolts, 0, false, mule, own, true); r.Ready {
		t.Fatalf("Fletching unknown: %+v", r)
	}
	// This account's own bank alone: a Resume, with or without the mule's bank.
	if r := Ready(pearls, bolts, 60, true, Held{}, Held{Pearl: 20000}, false); !r.Ready || !r.Resume {
		t.Fatalf("own pearls: %+v", r)
	}
	if r := Ready(pearls, bolts, 60, true, Held{}, Held{Tips: 600, Bolts: 20000}, false); !r.Ready || !r.Resume {
		t.Fatalf("own tips and own Bolts: %+v", r)
	}
	// The 13:41 round of 6 Oct 2026 (3,087,510 tips here, 55 pearls and 53 Bolts at the mule) is no
	// round any more.
	if r := Ready(pearls, bolts, 60, true, Held{Pearl: 55, Bolts: 53}, Held{Tips: 3087510}, true); r.Ready || !strings.Contains(r.Why, "under the threshold") {
		t.Fatalf("a handful of Bolts for the tips: %+v", r)
	}
	// Tips here and the Bolts of the two banks together.
	if r := Ready(pearls, bolts, 60, true, Held{Bolts: 15000}, Held{Tips: 600, Bolts: 5000}, true); !r.Ready || r.Resume {
		t.Fatalf("20,000 Bolts in the two banks: %+v", r)
	}
	if r := Ready(pearls, bolts, 60, true, Held{Bolts: 50000}, Held{}, true); r.Ready {
		t.Fatalf("Bolts and no tips anywhere must not start a round: %+v", r)
	}
	if r := Ready(pearls, bolts, 60, true, Held{}, Held{}, false); r.Ready || !strings.Contains(r.Why, "published") {
		t.Fatalf("no published bank: %+v", r)
	}
}

func TestBoltUses(t *testing.T) {
	for _, c := range []struct{ tips, bolts, want int }{{0, 5, 0}, {5, 0, 0}, {1, 1, 1}, {10, 10, 1}, {11, 100, 2}, {600000, 500000, 50000}} {
		if got := BoltUses(c.tips, c.bolts); got != c.want {
			t.Errorf("BoltUses(%d, %d) = %d, want %d", c.tips, c.bolts, got, c.want)
		}
	}
}

func TestRefusal(t *testing.T) {
	for said, want := range map[string]string{
		"You need a Fletching level of 41 to do that.":                         "refused",
		"You need a Fletching level of 41 to make that kind of bolt.":          "refused",
		"You need to be on a members' world to gain experience in Fletching.":  "members",
		"To use this item please login to a members' server.":                  "members",
		"You use your chisel to fletch small bolt tips.\nYou fletch 10 bolts.": "",
	} {
		if got, level := Refusal(said); got != want || (want == "refused" && level != 41) {
			t.Errorf("%q: %q %d, want %q", said, got, level, want)
		}
	}
}

func TestHeld(t *testing.T) {
	h := Held{Pearl: 10, Pearls: 2, Tips: 7, Bolts: 3}
	if h.PearlCount() != 12 || h.TipsToCome() != 10*6+2*24 {
		t.Fatalf("%+v", h)
	}
	if s := h.Add(Held{Pearl: 1, Tips: 1}); s != (Held{Pearl: 11, Pearls: 2, Tips: 8, Bolts: 3}) {
		t.Fatalf("%+v", s)
	}
}

// The rates the report quotes: a 27-pearl load of seven ticks, and fifty bolts a tick.
func TestRates(t *testing.T) {
	if got := TipXPPerHour(1); got != 857*27*32/10 {
		t.Fatalf("tips XP/h %d", got)
	}
	if got := BoltXPPerHour(); got != 6000*50*32/10 {
		t.Fatalf("bolts XP/h %d", got)
	}
}
