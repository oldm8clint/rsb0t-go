package runerunnerplan

import "fmt"

// What makes a rotation hop worth taking.
//
// Measured on 5 November 2026 (acct's logs): the East Ardougne block of each world is [PortKhazard, Aemad, Lundail], and its hop was
// judged by the Lundail runes alone ("world 1 expects 0 runes at [Lundail@1], under hopMinGain 200"; skipping [PortKhazard@1 Aemad@1
// Lundail@1]). Lundail's shelf is nearly always empty and the Wilderness kit was often short a blade, so over the day Aemad's shelf
// (500 Vials of water, 500 Bronze arrows, 20 Rope) was bought on world 3 alone (15 visits, none on worlds 1 and 2) and Port Khazard's
// four times. The ammo stops of a block were never counted toward its hop. HopWorthy counts them.

// AmmoHopMinCoins is the coins a block's ammo stops together must be worth for a hop to be made for them alone: two minutes of hop
// wait and login against the cheapest shelf (Aemad's whole shelf is about 2,900 coins).
const AmmoHopMinCoins = 2000

// AmmoHopStop is one ammo stop of a block: the coins it expects to spend there and the least its stop is made for (AmmoSite.MinVisit).
type AmmoHopStop struct {
	Key      string
	Coins    int
	MinVisit int
}

// HopWorthy says whether a block is worth its hop: its rune stations expect hopMinGain runes, or the ammo stops that would each be
// made (their coins at least their own MinVisit) add up to AmmoHopMinCoins. why says which, or why not.
func HopWorthy(runeWorth, hopMinGain int, ammo []AmmoHopStop) (bool, string) {
	if runeWorth >= hopMinGain {
		return true, fmt.Sprintf("%d runes expected (hopMinGain %d)", runeWorth, hopMinGain)
	}
	coins := 0
	for _, s := range ammo {
		if s.Coins >= max(s.MinVisit, 1) {
			coins += s.Coins
		}
	}
	if coins >= AmmoHopMinCoins {
		return true, fmt.Sprintf("%d runes expected, under hopMinGain %d, but the block's ammo stops are worth %d coins", runeWorth, hopMinGain, coins)
	}
	return false, fmt.Sprintf("%d runes expected, under hopMinGain %d, and its ammo stops are worth %d coins (need %d)", runeWorth, hopMinGain, coins, AmmoHopMinCoins)
}
