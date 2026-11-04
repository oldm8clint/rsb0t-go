package fleetplan

import "testing"

func TestAFareDrawCarriesTheFloat(t *testing.T) {
	kandarin, misthalin := KhazardTroopers.Stand, BarbarianVillage.Stand
	d := DecideFare(FareInput{Here: &kandarin, Dest: &misthalin})
	if d.Action != FareDraw || d.Draw != FareFloat || d.Need >= FareFloat {
		t.Fatalf("an empty purse across the water: %+v, want a FareDraw of the %d float", d, FareFloat)
	}
}
