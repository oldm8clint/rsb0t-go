package questplan

import (
	"strings"
	"testing"
)

// The live failure (2026-09-28): no mule essence and no Rune Mysteries, so the kit looped on a
// mining source Aubury refuses. Without the quest the market is the kit's only source (and with
// coins in the bank it is bought from).
func TestPipEssenceKitBuysFromTheMarketWhenRuneMysteriesIsNotDone(t *testing.T) {
	p := PlanProvision(pipEssenceKit(false), ProvisionState{
		Pack: map[int]int{ObjBronzePickaxe: 1}, Bank: map[int]int{CoinsID: 10000}, BankKnown: true, MuleKnown: true,
		FreeSlots: 26, Levels: map[string]int{"mining": 1}})
	if len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceMarket || p.Tasks[0].Source.Method != MethodMarketEssence || p.Tasks[0].Qty != 24 {
		t.Fatalf("plan %s", p.Summary())
	}
	// The coins are drawn first, budgeted at the ceiling: 24 x 150.
	if p.Phase != PhaseBank || p.ShopCoins != 24*PipEssenceUnitCeiling {
		t.Fatalf("phase %s, shop coins %d: %s", p.Phase, p.ShopCoins, p.Summary())
	}
	var drawn int
	for _, w := range p.Withdraw {
		if w.ID == CoinsID {
			drawn = w.Count
		}
	}
	if drawn < 24*PipEssenceUnitCeiling {
		t.Fatalf("drew %d coins for a %d gp lot", drawn, 24*PipEssenceUnitCeiling)
	}
}

// A failed market (Exhausted this round) leaves an account without Rune Mysteries BLOCKED: there
// is no mining line to fall through to, because Aubury refuses the teleport (live 14 Oct 2026:
// every such round walked to Aubury and back for nothing).
func TestPipEssenceMarketFailureBlocksTheKitWithoutRuneMysteries(t *testing.T) {
	kit := pipEssenceKit(false)
	p := PlanProvision(kit, ProvisionState{
		Pack: map[int]int{CoinsID: 10000}, BankKnown: true, MuleKnown: true, CoinsDrawn: true,
		FreeSlots: 26, Levels: map[string]int{"mining": 99},
		Exhausted: map[string]bool{SourceKey(kit[0], kit[0].Sources[0]): true}})
	if p.Phase != PhaseBlocked || len(p.Tasks) != 0 {
		t.Fatalf("plan %s", p.Summary())
	}
}

// Too poor for the lot's ceiling: the market is passed over (never a trip it cannot pay for), and
// without Rune Mysteries there is nothing after it, so the kit is blocked and says why.
func TestPipEssenceMarketTooPoorBlocksTheKitWithoutRuneMysteries(t *testing.T) {
	p := PlanProvision(pipEssenceKit(false), ProvisionState{
		Pack: map[int]int{CoinsID: 500}, Bank: map[int]int{CoinsID: 1000}, BankKnown: true, MuleKnown: true,
		FreeSlots: 26, Levels: map[string]int{"mining": 1}})
	if p.Phase != PhaseBlocked || p.ShopCoins != 0 ||
		!strings.Contains(strings.Join(p.Blocked, ";"), "market-essence (player market, at most 150 gp each): the pack and bank cannot pay") {
		t.Fatalf("phase %s: %q", p.Phase, p.Blocked)
	}
}

// A market the account cannot use at all (the stalls stand on another world, or its supply mode
// shops at no player market) is skipped up front and the BLOCKED line says so, not "failed".
func TestPipEssenceBlockedNamesAnUnavailableMarket(t *testing.T) {
	kit := pipEssenceKit(false)
	key := SourceKey(kit[0], kit[0].Sources[0])
	p := PlanProvision(kit, ProvisionState{
		Pack: map[int]int{CoinsID: 10000}, BankKnown: true, MuleKnown: true, FreeSlots: 26,
		Exhausted:   map[string]bool{key: true},
		Unavailable: map[string]string{key: "the Seers stalls do not stand on this world"}})
	joined := strings.Join(p.Blocked, ";")
	if p.Phase != PhaseBlocked || !strings.Contains(joined, "market-essence (player market, at most 150 gp each): the Seers stalls do not stand on this world") ||
		strings.Contains(joined, "failed this round") {
		t.Fatalf("phase %s: %q", p.Phase, p.Blocked)
	}
}

// With Rune Mysteries done the kit is a pickaxe and the mine: the market is not in it.
func TestPipEssenceKitMinesOnceRuneMysteriesIsDone(t *testing.T) {
	kit := pipEssenceKit(true)
	for _, l := range kit {
		for _, s := range l.Sources {
			if s.Kind == SourceMarket {
				t.Fatalf("a market source in the Rune Mysteries kit: %+v", l)
			}
		}
	}
	p := PlanProvision(kit, ProvisionState{
		Pack: map[int]int{ObjBronzePickaxe: 1}, BankKnown: true, MuleKnown: true, FreeSlots: 26, Levels: map[string]int{"mining": 1}})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Method != MethodMineEssence {
		t.Fatalf("plan %s", p.Summary())
	}
}

// The item rule ranks the market after ground spawns and before the account's own skills,
// whatever order a kit lists them in.
func TestMarketRanksBeforeSkillAndAfterGround(t *testing.T) {
	if !(ruleRank(SourceGround) < ruleRank(SourceMarket) && ruleRank(SourceMarket) < ruleRank(SourceSkill) && ruleRank(SourceSkill) < ruleRank(SourceShop)) {
		t.Fatalf("item rule %v", ItemRule)
	}
	line := KitLine{ID: ObjRuneEssence, Name: "Rune essence", Qty: 5, Sources: []Source{
		{Kind: SourceSkill, Skill: "mining", Method: MethodMineEssence},
		{Kind: SourceMarket, Method: MethodMarketEssence, Price: 10},
	}}
	p := PlanProvision([]KitLine{line}, ProvisionState{Pack: map[int]int{CoinsID: 100}, BankKnown: true, FreeSlots: 28, Levels: map[string]int{"mining": 1}})
	if len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceMarket {
		t.Fatalf("plan %s", p.Summary())
	}
}

func TestValidateMarketSource(t *testing.T) {
	for _, done := range []bool{false, true} {
		for _, l := range pipEssenceKit(done) {
			if why := ValidateKitLine(l); why != "" {
				t.Fatal(why)
			}
		}
	}
	bad := KitLine{ID: 1, Name: "x", Qty: 1, Sources: []Source{{Kind: SourceMarket, Method: MethodMarketEssence}}}
	if ValidateKitLine(bad) == "" {
		t.Fatal("a market source with no price ceiling validated")
	}
}

func essenceTerms() MarketTerms {
	return MarketTerms{Order: PipEssenceMarkets, Item: "Rune essence", Want: 24, Coins: 24 * PipEssenceUnitCeiling,
		Fallback: PipEssenceUnitFallback, Ceiling: PipEssenceUnitCeiling,
		Listings: []MarketStock{
			// The 2026-09-28 reading.
			{Market: "Seers Market", Item: "Rune essence", Stock: 13567, LastSold: 100},
			{Market: "Seers Market", Item: "Rune axe", Stock: 49},
			{Market: "Tradevault", Item: "Rune essence", Stock: 430526, LastSold: 100},
			{Market: "Sil Stuff", Item: "Rune essence", Stock: 285191},
		}}
}

func TestPickMarketFirstStallAtItsListedPrice(t *testing.T) {
	pick, why := PickMarket(essenceTerms())
	if why != "" || pick != (MarketPick{Market: "Seers Market", Unit: 100, UnitCap: 125, Qty: 24, MaxTotal: 3000}) {
		t.Fatalf("%+v %q", pick, why)
	}
}

func TestPickMarketNextStallInOrder(t *testing.T) {
	tm := essenceTerms()
	tm.Skip = map[string]bool{"Seers Market": true}
	if pick, _ := PickMarket(tm); pick.Market != "Tradevault" {
		t.Fatalf("%+v", pick)
	}
	tm.Skip["Tradevault"] = true
	// Sil Stuff lists stock with no price: the fallback, capped.
	if pick, _ := PickMarket(tm); pick.Market != "Sil Stuff" || pick.Unit != PipEssenceUnitFallback || pick.UnitCap != 125 {
		t.Fatalf("%+v", pick)
	}
	tm.Skip["Sil Stuff"] = true
	if pick, why := PickMarket(tm); pick.Market != "" || !strings.Contains(why, "Sil Stuff: asked already") {
		t.Fatalf("%+v %q", pick, why)
	}
}

func TestPickMarketPriceCeiling(t *testing.T) {
	tm := essenceTerms()
	tm.Listings[0].LastSold = 400 // Seers over the ceiling: skipped
	tm.Listings[2].LastSold = 140 // Tradevault under it, cap clipped to the ceiling
	pick, _ := PickMarket(tm)
	if pick.Market != "Tradevault" || pick.Unit != 140 || pick.UnitCap != PipEssenceUnitCeiling {
		t.Fatalf("%+v", pick)
	}
	// A fallback over the ceiling is refused outright.
	tm.Fallback = 200
	if _, why := PickMarket(tm); !strings.Contains(why, "no sane price") {
		t.Fatal(why)
	}
}

func TestPickMarketStockAndListings(t *testing.T) {
	tm := essenceTerms()
	tm.Listings = tm.Listings[1:2] // Seers lists only an axe now; the others are not on the page
	pick, why := PickMarket(tm)
	// Seers' page is readable and has none; Tradevault's page was not read, so it is asked at
	// the fallback price.
	if pick.Market != "Tradevault" || pick.Unit != PipEssenceUnitFallback {
		t.Fatalf("%+v %q", pick, why)
	}
	tm = essenceTerms()
	tm.Listings[0].Stock = 10
	if pick, _ := PickMarket(tm); pick.Market != "Tradevault" {
		t.Fatalf("a stall with 10 was asked for 24: %+v", pick)
	}
	// No page at all: every stall at the fallback.
	tm = essenceTerms()
	tm.Listings = nil
	if pick, _ := PickMarket(tm); pick.Market != "Seers Market" || pick.Unit != PipEssenceUnitFallback {
		t.Fatalf("%+v", pick)
	}
}

func TestPickMarketQuantityFitsTheCoins(t *testing.T) {
	tm := essenceTerms()
	tm.Coins = 1000 // 8 at the 125 cap
	pick, _ := PickMarket(tm)
	if pick.Qty != 8 || pick.MaxTotal != 1000 {
		t.Fatalf("%+v", pick)
	}
	tm.Coins = 100
	if pick, why := PickMarket(tm); pick.Market != "" || !strings.Contains(why, "do not pay for one") {
		t.Fatalf("%+v %q", pick, why)
	}
	tm = essenceTerms()
	tm.Want = 0
	if pick, _ := PickMarket(tm); pick.Market != "" {
		t.Fatalf("%+v", pick)
	}
}
