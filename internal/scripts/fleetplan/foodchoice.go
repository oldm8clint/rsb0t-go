package fleetplan

// FoodLadderFor is the food to ask for when the stockpile's contents are NOT known — best
// first, so a caller can try the next name if one is declined.
//
// WHY THIS EXISTS ALONGSIDE ChooseFoodToRequest. That function is the right answer and needs
// the source's actual counts; a donor in this fleet cannot currently see the mule's bank
// (internal/peerbank exists for exactly this and is not wired to any script yet), so asking
// blind is the only thing available today. Asking blind is survivable because the mule
// ANSWERS: an item it does not hold comes back as a decline with a reason, which is visible,
// rather than as silence.
//
// The order is the same rule ChooseFoodToRequest applies — never a heal above the account's
// maximum, and among what fits prefer what a stocked mule is likeliest to have deepest —
// with the live 2026-09-22 stockpile as the guide for that last part (Cake 91,380 and Bread
// 75,649 dwarf Shark 13,096).
func FoodLadderFor(maxHP int) []string {
	if maxHP < 1 {
		maxHP = 1
	}
	// Name, per-bite heal. Ordered by how deep the stockpile tends to be for a given tier.
	type opt struct {
		name string
		heal int
	}
	all := []opt{
		// 1 Nov 2026 (owner): "we have 1m + cooked sharks available on the mule so we should be using that over most
		// food": Shark first for every account it does not overheal.
		{"Shark", 20},
		// 3 Nov 2026 (owner): the trawler now brings Manta rays and Sea turtles in, which the mule cooks; they heal 22 and 21.
		{"Manta ray", 22}, {"Sea turtle", 21},
		{"Cake", 4}, {"Bread", 4}, {"Shrimps", 3},
		{"Tuna", 10}, {"Lobster", 12}, {"Bass", 13}, {"Swordfish", 14},
		// What the mule still held on 28 Sep once the fleet had eaten the ladder above (about 250k
		// Bread, Shark, Bass, Cake and Lobster in two days): 30,816 Chocolate slices and some
		// fish. Without them every food ask read "holds none" and accounts stole at the Baker's
		// stall with nothing to eat (543 "leaving the rung" retreats in 45 minutes).
		{"Chocolate slice", 5}, {"Cod", 7}, {"Mackerel", 6}, {"Herring", 5}, {"Sardine", 4}, {"Anchovies", 3},
		// 3 Oct: the mule held 10,165 Kebabs and Jugs of wine drop from Heroes and giants (owner).
		{"Kebab", 3}, {"Jug of wine", 11},
		// 4 Oct: random events hand out Spinach rolls and the mule holds them (owner).
		{"Spinach roll", 2},
	}
	var fits, over []string
	for _, o := range all {
		if o.heal <= maxHP {
			fits = append(fits, o.name)
		} else {
			over = append(over, o.name)
		}
	}
	// Anything that overheals goes last rather than being dropped: some food beats none.
	return append(fits, over...)
}

// DefaultCohortShare is how many accounts one mule is assumed to be feeding when nobody says
// otherwise. A hundred-account swarm is what this script is built for, so an account that
// cannot see the cohort size should behave as if it is one of many rather than the only one.
const DefaultCohortShare = 100

// FleetReserveFraction is the part of any stockpile that is never asked for, whatever the
// arithmetic says: a tenth. It is what stops the last few of an item being taken by whoever
// happens to ask last, leaving the next account — and every other script in the fleet, which
// draws on the same mule — with nothing at all.
const FleetReserveFraction = 10

// BlindShare is how many of an item to ask for when the stockpile's contents are NOT known.
//
// WHY IT IS NOT "just ask for what I want". Live on 2026-09-22, the first Stage 2 mule asks
// went out before the mule had published its bank, so they fell through to the unbounded
// want: acct76 took 150 chaos runes out of a fleet stock of 5,764 in one trade, while the
// accounts that asked a minute later — once the directory had a row — were correctly held to
// 53 each. A blind ask that takes a bounded share is the difference between the first few
// accounts draining a scarce rune and everybody getting some.
//
// The cap is deliberately crude, because a guess dressed up as arithmetic is still a guess:
// take at most BlindAskCap of anything unknown. It is sized as "enough to be worth the walk,
// small enough that a hundred of them do not empty a real stockpile" — a hundred accounts
// taking 50 is 5,000, which the chaos stock (5,764) survives and every abundant rune ignores.
//
// The moment the mule publishes, FairShare takes over and this stops being used.
func BlindShare(want int) int {
	if want <= 0 {
		return 0
	}
	if want > BlindAskCap {
		return BlindAskCap
	}
	return want
}

// BlindAskCap bounds any single ask made without knowing the stock. See BlindShare.
const BlindAskCap = 50

// FairShare is how many of an item one account may ask for out of a shared stockpile, so that
// "grab what I need" does not become "grab everything".
//
// The fleet owner's rule, 2026-09-22: "accounts should be able to grab the mules runes to
// speed up theiving process" — "but it should grab enough so theres some left for the fleet".
// Those are the two halves: take a real amount, and leave the rest standing.
//
// want is what the account would take if it were alone; stock is what the mule holds; cohort
// is how many accounts share it (<= 0 means DefaultCohortShare). The answer is the smallest
// of: what was wanted, an equal split of the spendable stock, and never more than the stock
// less its reserve. Zero is a real answer and means "ask for nothing" — a stockpile at or
// under its reserve has nothing to give.
//
// Deliberately NOT a percentage of what one account feels like taking: the whole failure mode
// is a hundred accounts each independently deciding their own need is reasonable. The divisor
// is the cohort, so the sum of everyone's fair share is the spendable stock by construction.
func FairShare(want, stock, cohort int) int {
	if want <= 0 || stock <= 0 {
		return 0
	}
	if cohort <= 0 {
		cohort = DefaultCohortShare
	}
	spendable := stock - stock/FleetReserveFraction
	if spendable <= 0 {
		return 0
	}
	share := spendable / cohort
	if share < 1 {
		// The stock cannot give every account even one. Rather than handing the first asker
		// the lot, give nothing: an item this thin is not worth a hand-off, and the caller
		// falls back to earning or buying it.
		return 0
	}
	// A share the stock cuts under FairShareMinLot is not worth a trade (a user request: "mule shouldnt
	// hand out only 1 nature rune at a time, more runes should be handed out at a time to ensure the mule can
	// still get time to cook"): live, 140 Nature runes split over the cohort went out one a trade. A thin
	// stock goes out in lots of FairShareMinLot to whoever asks first (the claims ledger keeps them from
	// over-claiming it), and nothing when it cannot cover a lot; a small want is still met in full.
	if share < want && share < FairShareMinLot {
		if spendable < FairShareMinLot {
			return 0
		}
		share = FairShareMinLot
	}
	if share > want {
		share = want
	}
	if share > spendable {
		share = spendable
	}
	return share
}

// FairShareMinLot is the least a stock-limited fair share hands out (FairShare).
const FairShareMinLot = 50

// MuleFoods is every food a Scarab account may be handed by the mule, any size: the union of
// FoodLadderFor's rungs.
var MuleFoods = []string{"Cake", "Bread", "Shrimps", "Tuna", "Lobster", "Bass", "Swordfish", "Shark", "Sea turtle", "Manta ray"}
