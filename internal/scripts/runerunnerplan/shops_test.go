package runerunnerplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/marketbulkplan"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// Test 1: Betty is port_sarim.inv [magicshop] and port_sarim.npc [betty], line for line.
func TestBettyMatchesContent(t *testing.T) {
	if Betty.NPCID != 583 || Betty.Keeper != "Betty" || Betty.SellMultiplier != 1000 || Betty.Delta != 10 {
		t.Fatalf("Betty = npc %d keeper %q %d/%d, want 583 Betty 1000/10", Betty.NPCID, Betty.Keeper, Betty.SellMultiplier, Betty.Delta)
	}
	want := []fleetplan.StockLine{
		{Rune: "Fire rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Water rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Air rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Earth rune", Base: 1000, RateTicks: 10, Cost: 4},
		{Rune: "Mind rune", Base: 1000, RateTicks: 10, Cost: 3},
		{Rune: "Body rune", Base: 1000, RateTicks: 10, Cost: 3},
		{Rune: "Chaos rune", Base: 1000, RateTicks: 100, Cost: 15},
		{Rune: "Death rune", Base: 1000, RateTicks: 150, Cost: 30},
	}
	if len(Betty.Lines) != len(want) {
		t.Fatalf("Betty has %d lines, want %d", len(Betty.Lines), len(want))
	}
	for i, l := range want {
		if Betty.Lines[i] != l {
			t.Errorf("line %d = %+v, want %+v", i, Betty.Lines[i], l)
		}
	}
	if BettyStand != (tile.Tile{X: 3012, Z: 3259}) {
		t.Errorf("BettyStand = %v, want 3012,3259 (m47_50 local 4,59)", BettyStand)
	}
}

// Test 2: Betty's parameters are Aubury's, so every mind and chaos price agrees at every stock.
func TestBettyPricesLikeAubury(t *testing.T) {
	for _, r := range []string{"Mind rune", "Chaos rune"} {
		for stock := 0; stock <= 1000; stock++ {
			if b, a := Betty.PriceAt(r, stock), fleetplan.Aubury.PriceAt(r, stock); b != a {
				t.Fatalf("%s at stock %d: Betty %d, Aubury %d", r, stock, b, a)
			}
		}
	}
}

// The bank tiles are other packages' proven tiles; this fails if one of them moves.
func TestBankTilesMatchTheirSources(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want tile.Tile
	}{
		{"Catherby", CatherbyBank, marketbulkplan.CatherbyPost},
		{"East Ardougne", ArdougneBank, thieverplan.ArdougneBank},
		{"Draynor", DraynorBank, fleetplan.DraynorBank},
		{"Varrock East", VarrockEastBank, thieverplan.VarrockEastBank},
		{"Yanille", YanilleBank, thieverplan.YanilleBank},
	} {
		if c.got != c.want {
			t.Errorf("%s bank %v, its source says %v", c.name, c.got, c.want)
		}
	}
}

func TestSiteRegistry(t *testing.T) {
	for _, key := range BuiltSites() {
		s, ok := Site(key)
		if !ok || !s.Built || s.Key != key {
			t.Fatalf("Site(%q) = %+v, %v", key, s, ok)
		}
		if !Eligible(s, 1) {
			t.Errorf("%s should be open at Magic 1", key)
		}
	}
	if s, _ := Site("aubury"); s.Shop.Keeper != "Aubury" || s.Stand != fleetplan.AuburyStand || s.Bank != VarrockEastBank {
		t.Errorf("Aubury site %+v", s)
	}
	if s, _ := Site("Betty"); s.Bank != DraynorBank || s.HoldTTL != BettyHoldTTL || s.Wilderness {
		t.Errorf("Betty site %+v", s)
	}
	if s, _ := Site("Lundail"); !s.Wilderness || s.Approach != ArdougneBank || s.Bank != (tile.Tile{}) {
		t.Errorf("Lundail site %+v", s)
	}
	g, ok := Site("WizardsGuild")
	if !ok || !g.Built || g.MinMagic != 66 || g.Wilderness {
		t.Fatalf("WizardsGuild = %+v, %v", g, ok)
	}
	if g.Stand != fleetplan.WizardsGuildStand || g.Approach != fleetplan.WizardsGuildStand ||
		g.Bank != YanilleBank || g.Shop.Keeper != "Magic Store owner" || g.Shop.NPCID != 461 {
		t.Errorf("WizardsGuild site %+v", g)
	}
	if Eligible(g, 65) || !Eligible(g, 66) || !Eligible(g, 99) {
		t.Error("the guild door opens at the CURRENT Magic level 66, not below")
	}
	if _, ok := Site("Zaff"); ok {
		t.Error("Zaff is not a rune shop")
	}
}

func TestCanonicalRune(t *testing.T) {
	if n, ok := CanonicalRune("mind RUNE"); !ok || n != "Mind rune" {
		t.Errorf("CanonicalRune = %q, %v", n, ok)
	}
	if n, ok := CanonicalRune("Law rune"); !ok || n != "Law rune" {
		t.Errorf("Law rune is Lundail's: %q, %v", n, ok)
	}
	if n, ok := CanonicalRune("blood rune"); !ok || n != "Blood rune" {
		t.Errorf("Blood rune is the Wizards' Guild's: %q, %v", n, ok)
	}
	if _, ok := CanonicalRune("Soul rune"); ok {
		t.Error("no modelled shop line sells soul runes")
	}
}

// EligibleShops is what a round is planned over: the guild only from the CURRENT Magic 66.
func TestEligibleShopsGateTheGuildOnMagic(t *testing.T) {
	all := DefaultSites()
	if got := strings.Join(EligibleShops(all, 65), ","); got != "Aubury,Betty,Lundail" {
		t.Errorf("Magic 65: %s", got)
	}
	if got := strings.Join(EligibleShops(all, 0), ","); got != "Aubury,Betty,Lundail" {
		t.Errorf("Magic unknown: %s", got)
	}
	if got := strings.Join(EligibleShops(all, 66), ","); got != "Aubury,Betty,Lundail,WizardsGuild" {
		t.Errorf("Magic 66: %s", got)
	}
	if got := EligibleShops([]string{SiteWizardsGuild}, 65); len(got) != 0 {
		t.Errorf("a guild-only runner at 65: %v", got)
	}
}
