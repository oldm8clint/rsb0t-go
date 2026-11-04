package questplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

var (
	testAemad = Source{Kind: SourceShop, Shop: ShopRef{Npc: "Aemad", Anchor: tile.Tile{X: 2614, Z: 3294}}, Price: 18}
	testBetty = Source{Kind: SourceShop, Shop: ShopRef{Npc: "Betty", Anchor: tile.Tile{X: 3014, Z: 3259}}, Price: 17}
	testRope  = KitLine{ID: 954, Name: "Rope", Qty: 1, Sources: []Source{testAemad}}
	testAir   = KitLine{ID: 556, Name: "Air rune", Qty: 6, Stackable: true, Sources: []Source{testBetty}}
	testShark = KitLine{ID: 385, Name: "Shark", Qty: 18}
	testKey   = KitLine{ID: 298, Name: "A key", Qty: 1, Untradeable: true}
)

func TestPlanProvisionReadsAnUnreadBankFirst(t *testing.T) {
	p := PlanProvision([]KitLine{testRope, testAir}, ProvisionState{FreeSlots: 28})
	if p.Phase != PhaseBank || len(p.Withdraw) != 0 || len(p.MuleAsk) != 0 || len(p.Tasks) != 0 {
		t.Fatalf("an unread bank must be read before any other rung: %+v", p)
	}
}

func TestPlanProvisionSatisfiedFromThePack(t *testing.T) {
	p := PlanProvision([]KitLine{testRope}, ProvisionState{Pack: map[int]int{954: 1}})
	if p.Phase != PhaseSatisfied {
		t.Fatalf("a held rope needs nothing: %+v", p)
	}
}

func TestPlanProvisionOwnBankBeforeTheMule(t *testing.T) {
	p := PlanProvision([]KitLine{testRope, testAir}, ProvisionState{
		BankKnown: true, Bank: map[int]int{954: 3, 556: 2},
		MuleKnown: true, Mule: map[int]int{954: 11, 556: 48890},
		FreeSlots: 28,
	})
	if p.Phase != PhaseBank {
		t.Fatalf("phase %s, want bank", p.Phase)
	}
	if len(p.Withdraw) != 2 || p.Withdraw[0].Count != 1 || p.Withdraw[1].Count != 2 {
		t.Fatalf("withdraw %+v", p.Withdraw)
	}
	// The bank covers the rope; the mule covers the remaining four airs, in the same plan.
	if len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != 556 || p.MuleAsk[0].Count != 4 {
		t.Fatalf("mule ask %+v", p.MuleAsk)
	}
}

// The owner's rule for an EMPTY mule: the mule is an accelerator, never a dependency. With a
// published bank that holds none of the kit, nothing is asked (so nothing is walked to) and
// every line falls to its shop.
func TestPlanProvisionEmptyMulePicksANonMuleSource(t *testing.T) {
	p := PlanProvision([]KitLine{testRope, testAir}, ProvisionState{
		BankKnown: true, Bank: map[int]int{995: 1000},
		MuleKnown: true, Mule: map[int]int{},
		FreeSlots: 28,
	})
	if len(p.MuleAsk) != 0 {
		t.Fatalf("asked an empty mule for %+v", p.MuleAsk)
	}
	if len(p.Tasks) != 2 || p.Tasks[0].Source.Kind != SourceShop || p.Tasks[1].Source.Kind != SourceShop {
		t.Fatalf("tasks %+v", p.Tasks)
	}
	if p.ShopCoins != 18+6*17 {
		t.Fatalf("shop coins %d", p.ShopCoins)
	}
	// The coins come out of the bank on the same visit, with half again for price rises.
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != CoinsID || p.Withdraw[0].Count != 180 {
		t.Fatalf("plan %+v", p)
	}
}

func TestPlanProvisionNoMuleAtAllIsTheSameAsAnEmptyOne(t *testing.T) {
	p := PlanProvision([]KitLine{testRope}, ProvisionState{
		BankKnown: true, Pack: map[int]int{995: 50}, FreeSlots: 20,
	})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Shop.Npc != "Aemad" {
		t.Fatalf("plan %+v", p)
	}
}

func TestPlanProvisionStockedMuleIsOneBundle(t *testing.T) {
	kit := []KitLine{testRope, testAir, testShark,
		{ID: 555, Name: "Water rune", Qty: 6, Stackable: true},
		{ID: 557, Name: "Earth rune", Qty: 6, Stackable: true}}
	p := PlanProvision(kit, ProvisionState{
		BankKnown: true, MuleKnown: true,
		Mule:      map[int]int{954: 11, 556: 100, 555: 100, 557: 100, 385: 18541},
		FreeSlots: 28,
	})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 5 {
		t.Fatalf("plan %+v", p)
	}
	slots := 0
	for _, c := range p.MuleAsk {
		for _, l := range kit {
			if l.ID == c.ID {
				slots += l.slots(c.Count)
			}
		}
	}
	if slots > MaxBundleSlots {
		t.Fatalf("bundle takes %d slots", slots)
	}
}

func TestPlanProvisionBundleNeverExceedsTwentyEightUnstacked(t *testing.T) {
	big := KitLine{ID: 385, Name: "Shark", Qty: 40}
	p := PlanProvision([]KitLine{big}, ProvisionState{
		BankKnown: true, MuleKnown: true, Mule: map[int]int{385: 1000}, FreeSlots: 28,
	})
	if len(p.MuleAsk) != 1 || p.MuleAsk[0].Count != 28 {
		t.Fatalf("mule ask %+v", p.MuleAsk)
	}
	// And it never asks for more than the pack can take.
	p = PlanProvision([]KitLine{big}, ProvisionState{
		BankKnown: true, MuleKnown: true, Mule: map[int]int{385: 1000}, FreeSlots: 5, Depositable: 3,
	})
	if len(p.MuleAsk) != 1 || p.MuleAsk[0].Count != 8 {
		t.Fatalf("mule ask with 8 slots of room %+v", p.MuleAsk)
	}
}

func TestPlanProvisionOneAskPerRound(t *testing.T) {
	p := PlanProvision([]KitLine{testRope}, ProvisionState{
		BankKnown: true, MuleKnown: true, MuleAsked: true, Mule: map[int]int{954: 11},
		Pack: map[int]int{995: 100}, FreeSlots: 28,
	})
	if len(p.MuleAsk) != 0 || p.Phase != PhaseSource {
		t.Fatalf("a spent ask must fall through to the shop: %+v", p)
	}
}

func TestPlanProvisionNeverAsksOrBuysAQuestItem(t *testing.T) {
	p := PlanProvision([]KitLine{testKey}, ProvisionState{
		BankKnown: true, MuleKnown: true, Mule: map[int]int{298: 5}, Pack: map[int]int{995: 1000},
	})
	if len(p.MuleAsk) != 0 || p.Phase != PhaseBlocked {
		t.Fatalf("plan %+v", p)
	}
	if !strings.Contains(p.Blocked[0], "quest item") {
		t.Fatalf("blocked reason %q", p.Blocked[0])
	}
}

func TestPlanProvisionGroundBeforeSkillBeforeShop(t *testing.T) {
	l := KitLine{ID: 1, Name: "Thing", Qty: 2, Sources: []Source{
		{Kind: SourceShop, Shop: ShopRef{Npc: "Keeper"}, Price: 5},
		{Kind: SourceSkill, Skill: "thieving", Level: 5, Method: "thieve-thing"},
		{Kind: SourceGround, Tile: tile.Tile{X: 1, Z: 1}},
	}}
	st := ProvisionState{BankKnown: true, Pack: map[int]int{995: 100}, Levels: map[string]int{"thieving": 10}}
	p := PlanProvision([]KitLine{l}, st)
	if p.Tasks[0].Source.Kind != SourceGround {
		t.Fatalf("first %+v", p.Tasks[0])
	}
	st.Exhausted = map[string]bool{SourceKey(l, l.Sources[2]): true}
	p = PlanProvision([]KitLine{l}, st)
	if p.Tasks[0].Source.Kind != SourceSkill {
		t.Fatalf("after the ground failed: %+v", p.Tasks[0])
	}
	st.Levels["thieving"] = 4
	p = PlanProvision([]KitLine{l}, st)
	if p.Tasks[0].Source.Kind != SourceShop {
		t.Fatalf("below the skill level: %+v", p.Tasks[0])
	}
}

func TestPlanProvisionBlockedWhenCoinsRunOut(t *testing.T) {
	p := PlanProvision([]KitLine{testRope}, ProvisionState{BankKnown: true, Pack: map[int]int{995: 3}})
	if p.Phase != PhaseBlocked {
		t.Fatalf("plan %+v", p)
	}
}

func TestPlanProvisionOptionalLinesNeverBlock(t *testing.T) {
	opt := KitLine{ID: 7, Name: "Nice to have", Qty: 1, Optional: true}
	p := PlanProvision([]KitLine{opt}, ProvisionState{BankKnown: true})
	if p.Phase != PhaseSatisfied {
		t.Fatalf("plan %+v", p)
	}
}

func TestPlanProvisionWornLinesCountWhenWorn(t *testing.T) {
	amulet := KitLine{ID: 552, Name: "Ghostspeak amulet", Qty: 1, Worn: true, Untradeable: true}
	p := PlanProvision([]KitLine{amulet}, ProvisionState{Worn: map[int]int{552: 1}})
	if p.Phase != PhaseSatisfied {
		t.Fatalf("plan %+v", p)
	}
}

func TestValidateKitLine(t *testing.T) {
	cases := map[string]KitLine{
		"no object id":     {Name: "x", Qty: 1},
		"non-positive":     {ID: 1, Name: "x"},
		"no tile":          {ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: SourceGround}}},
		"no method":        {ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: SourceSkill}}},
		"no keeper":        {ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: SourceShop}}},
		"gets anyway":      {ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: SourceBank}}},
		"unknown source k": {ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: "gift"}}},
	}
	for want, l := range cases {
		got := ValidateKitLine(l)
		if got == "" || !strings.Contains(got, strings.TrimSuffix(want, " k")) {
			t.Errorf("%s: got %q", want, got)
		}
	}
	if got := ValidateKitLine(testRope); got != "" {
		t.Errorf("rope: %q", got)
	}
}

func TestStoreDecodesLenientlyAndParks(t *testing.T) {
	s, err := DecodeStore([]byte(`{"version":1,"future":{"x":1},"quests":{"waterfall":{"attempts":2,"unknownField":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Record("waterfall").Attempts != 2 {
		t.Fatalf("attempts lost: %+v", s.Quests["waterfall"])
	}
	if _, err := DecodeStore([]byte(`{"quests":`)); err == nil {
		t.Fatal("malformed JSON must be an error, never an empty store")
	}
	now := time.Unix(1_800_000_000, 0)
	s.NotePark("waterfall", "no progress", now)
	if parked, why := s.Parked("waterfall", now.Add(time.Minute)); !parked || why != "no progress" {
		t.Fatalf("parked %v %q", parked, why)
	}
	if parked, _ := s.Parked("waterfall", now.Add(6*time.Minute)); parked {
		t.Fatal("the first park backs off five minutes")
	}
	s.NotePark("waterfall", "again", now)
	if parked, _ := s.Parked("waterfall", now.Add(6*time.Minute)); !parked {
		t.Fatal("the second park backs off ten minutes")
	}
	s.ClearPark("waterfall", now)
	if parked, _ := s.Parked("waterfall", now); parked {
		t.Fatal("cleared")
	}
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeStore(b)
	if err != nil || back.Record("waterfall").Parks != 2 {
		t.Fatalf("round trip: %v %+v", err, back.Quests["waterfall"])
	}
}

// The second Waterfall lab run's strand: a pack that already covers the bare prices still goes
// back to the bank for the margin and the travel float, so the fares home are paid for.
func TestPlanProvisionDrawsTheFloatEvenWhenThePackCoversThePrices(t *testing.T) {
	p := PlanProvision([]KitLine{testRope, testAir}, ProvisionState{
		BankKnown: true, Bank: map[int]int{995: 3000}, Pack: map[int]int{995: 200},
		FreeSlots: 20, TravelFloat: 200,
	})
	want := 120 + 60 + 200 - 200
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != CoinsID || p.Withdraw[0].Count != want {
		t.Fatalf("plan %+v, want a %d-coin withdrawal", p, want)
	}
	// With the bank empty the bare prices are enough to go: no block, straight to the shops.
	p = PlanProvision([]KitLine{testRope, testAir}, ProvisionState{
		BankKnown: true, Bank: map[int]int{}, Pack: map[int]int{995: 200},
		FreeSlots: 20, TravelFloat: 200,
	})
	if p.Phase != PhaseSource || len(p.Blocked) != 0 {
		t.Fatalf("plan %+v", p)
	}
}

// Once the round has drawn its coins, a pack that still covers the remaining prices goes on to
// the next shop instead of back to the bank for the margin.
func TestPlanProvisionDoesNotTopUpTheFloatTwiceInARound(t *testing.T) {
	st := ProvisionState{BankKnown: true, Bank: map[int]int{995: 3000}, Pack: map[int]int{995: 110},
		FreeSlots: 20, TravelFloat: 200, CoinsDrawn: true}
	p := PlanProvision([]KitLine{testAir}, st)
	if p.Phase != PhaseSource {
		t.Fatalf("plan %+v", p)
	}
	st.Pack = map[int]int{995: 10}
	if p := PlanProvision([]KitLine{testAir}, st); p.Phase != PhaseBank {
		t.Fatalf("a pack short of the bare prices must still go to the bank: %+v", p)
	}
}
