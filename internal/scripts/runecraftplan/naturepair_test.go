package runecraftplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/naturepairplan"
)

// The gate shares its facts with naturepairplan (the TS NaturePair's pure layer, whose store, pier, ruins and request rate were
// hardened live): where it has the same fact, the two must agree, so a change to one is seen by the other.
func TestTheGateAgreesWithNaturePair(t *testing.T) {
	if JiminuaAt != naturepairplan.Store {
		t.Errorf("Jiminua %v, naturepairplan.Store %v", JiminuaAt, naturepairplan.Store)
	}
	if ArdougnePier.X != naturepairplan.Pier.X || ArdougnePier.Z != naturepairplan.Pier.Z {
		t.Errorf("Ardougne pier %v, naturepairplan.Pier %v", ArdougnePier, naturepairplan.Pier)
	}
	if Nature.Exit != naturepairplan.RuinsTile {
		t.Errorf("Nature exit %v, naturepairplan.RuinsTile %v", Nature.Exit, naturepairplan.RuinsTile)
	}
	k := naturepairplan.Karamja
	if Karamja.MinX != k.MinX || Karamja.MaxX != k.MaxX || Karamja.MinZ != k.MinZ || Karamja.MaxZ != k.MaxZ {
		t.Errorf("Karamja %+v, naturepairplan.Karamja %+v", Karamja, k)
	}
	if EssenceID != naturepairplan.EssenceID || Nature.Talisman != naturepairplan.Talisman || EssenceName != naturepairplan.Essence {
		t.Errorf("essence %d/%q, talisman %q against naturepairplan", EssenceID, EssenceName, Nature.Talisman)
	}
	if Nature.Level != naturepairplan.Level {
		t.Errorf("Nature level %d, naturepairplan.Level %d", Nature.Level, naturepairplan.Level)
	}
	// A carrier asks no faster than NaturePair's runner did (asking every tick cancels the window an open request makes).
	if GateRequestEvery < time.Duration(naturepairplan.RunnerAskMs)*time.Millisecond*2/3 {
		t.Errorf("a carrier asks every %s, NaturePair's runner every %dms", GateRequestEvery, naturepairplan.RunnerAskMs)
	}
	// The carrier's offer is what NaturePair's trade window carried; the master shape (talisman, notes, coins, runes) was one short.
	if CarrierOffer != naturepairplan.TradeCap || GateTradeMax(4) != naturepairplan.TradeCap-1 || GateTradeMax(GateKeepSlots) != naturepairplan.TradeCap {
		t.Errorf("offer %d, trade max %d/%d, naturepairplan.TradeCap %d", CarrierOffer, GateTradeMax(4), GateTradeMax(GateKeepSlots), naturepairplan.TradeCap)
	}
	// The fare is the ship's, each way.
	if GateFare < 2*naturepairplan.ShipFare {
		t.Errorf("the gate fare %d does not cover two crossings of %d", GateFare, naturepairplan.ShipFare)
	}
}
