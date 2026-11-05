package loot

// NOTABLE DROPS (a user request: the Notable drops ticker "appears just to include things from
// caskets and oyster pearls. for instance the dragon chainbody should show there as its a rare item
// from the npc").
//
// The ticker used to test a fixed list of eight obj ids (key halves, three gems, a talisman, pearls),
// which only the Casket, Oyster and Crystal chest sources ever paid, so an npc's dragon chainbody, a
// chest's rune armour or a random event's reward never qualified. The rule is now the same for every
// source: an item is notable when it is worth a lot, however it was got.
//
//   - one of it is worth at least NotableUnitGP (a dragon chainbody, rune platebody, a rune scimitar's
//     market median), or
//   - the stack in the drop is worth at least NotableStackGP (a hundred noted yew logs is not notable,
//     a thousand rune essence is not either, but 400 noted sharks is).
//
// Coins never qualify (they have a tile of their own), nor do the unvalued containers (a closed casket).
// The price is the caller's: internal/controlpanel passes the market price it resolves, so the rule has
// no price table of its own. The ids in the panel's fixed list stay notable at any price, because a key
// half or a cosmic talisman is rare whatever it trades for.
const (
	NotableUnitGP  = 20000
	NotableStackGP = 200000
)

// coinsObj is the cache's Coins obj.
const coinsObj = 995

// IsNotable reports whether count of an item worth unit gp each is a notable drop.
func IsNotable(id int, unit, count int64) bool {
	if id == coinsObj || IsUnvaluedContainer(id) || count <= 0 || unit <= 0 {
		return false
	}
	return unit >= NotableUnitGP || unit*count >= NotableStackGP
}
