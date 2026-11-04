package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Every Far pen over the water (Barbarian Village, Al-Kharid) is a walk over the ships from East Ardougne, where the Magic accounts bank: its fares are due before
// the walk is planned (DecideFare), or the walk is "unreachable" and the account asks for it every five seconds.
func TestEveryFarPenAcrossTheWaterTakesFaresFromTheArdougneBank(t *testing.T) {
	bank := tile.Tile{X: 2655, Z: 3283}
	far := 0
	for _, p := range TrainPens {
		// the rune pens' stands are underground: the walk's fares are judged on their banks
		stand := p.FareStand()
		if !p.Far || freshstart.LandmassOf(stand) == freshstart.LandKandarin { // the Yanille and Feldip ogres are Far pens on the home side
			continue
		}
		far++
		need := freshstart.FareToCross(&bank, &stand)
		if need != freshstart.DefaultFare {
			t.Errorf("%s: %d coins of fares from the Ardougne bank, want the whole default fare %d", p.Name, need, freshstart.DefaultFare)
		}
		// Empty-handed at the bank: the fares are drawn from it first; with the bank tried and no coin, earned.
		in := FareInput{Here: &bank, Dest: &stand, Thieving: 60}
		if dec := DecideFare(in); dec.Action != FareDraw {
			t.Errorf("%s: empty-handed at the bank DecideFare = %v, want draw", p.Name, dec.Action)
		}
		in.BankTried = true
		if dec := DecideFare(in); dec.Action != FareEarn {
			t.Errorf("%s: bank tried and empty DecideFare = %v, want earn", p.Name, dec.Action)
		}
		in.Coins = freshstart.DefaultFare
		if dec := DecideFare(in); dec.Action != FareNone {
			t.Errorf("%s: holding the fares DecideFare = %v, want none", p.Name, dec.Action)
		}
		// Already on the pen's home ground: nothing to cross.
		if need := freshstart.FareToCross(&p.Bank, &stand); need != 0 {
			t.Errorf("%s: %d coins of fares from its own bank, want none", p.Name, need)
		}
	}
	if far < 4 { // Barbarian Village, Al-Kharid, and the Dwarven Mine and the Ice dungeon (judged on their banks)
		t.Fatalf("%d Far pens across the water in TrainPens, want at least 4", far)
	}
}
