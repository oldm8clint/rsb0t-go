package lootchestfisherplan

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the geography, measured rather than remembered ----
//
// Every tile below came out of the baked static loc index and the collision pack
// (cmd/findloc, cmd/doordiag, internal/locreach), not from memory of the live game:
//
//	cmd/findloc 2900,3450,0 r=120   -> id=172 name="Closed chest" ops=[Open] at 2914,3452,0
//	                                   angle=2 shape=10
//	cachecfg.Loc(172)              -> Width 1, Length 1, ForceApproach 14, BlockWalk true
//	locreach.Tiles(that loc)       -> [2914,3451] then the footprint 2914,3452
//	cmd/findloc 2950,3370,0 r=60   -> "Bank booth" (Use-quickly) at 2945..2949,3367,0
//	                                   and the NEXT nearest at 3010,3354 (Falador east)
//	cmd/doordiag -x 2914 -z 3451   -> the chest's walkable component is 58 tiles,
//	                                   bbox x 2897..2916 z 3448..3452, and EXACTLY ONE door
//	                                   touches it: "Door" id=1530 at 2896,3450, whose east
//	                                   neighbour 2897,3450 is inside the component.
//
// The forceapproach number is the one that matters and the one that is silent when wrong.
// 14 is BlockWest|BlockSouth|BlockEast (8|4|2), i.e. approach from the north only — but that
// is before rotation. locreach.rotateFlags(angle 2, 14) is ((14<<2)&0xf)|(14>>2) = 8|3 = 11,
// which is BlockWest|BlockEast|BlockNorth and leaves SOUTH open. So the single legal tile to
// stand on is the one due south of the chest, 2914,3451, plus the chest's own footprint (which
// blocks walking, so it is not reachable in practice). Standing anywhere else and sending
// OPLOCU produces nothing at all, or an "I can't reach that!" a script that does not read
// MESSAGE_GAME never sees — see CLAUDE.md's OpLocU gotcha and internal/locreach's package doc.

var (
	// ChestTile is where the crystal chest loc stands.
	ChestTile = tile.Tile{X: 2914, Z: 3452, Level: 0}
	// ChestStand is the only tile a size-1 account may stand on to use a key on it.
	ChestStand = tile.Tile{X: 2914, Z: 3451, Level: 0}
	// HouseDoorOutside and HouseDoorInside are the two sides of the chest room's ONLY
	// entrance. The collision bake has the door closed, so a plain FindPath from Falador to
	// ChestStand answers "unreachable"; the walk has to cross this door.
	HouseDoorOutside = tile.Tile{X: 2896, Z: 3450, Level: 0}
	HouseDoorInside  = tile.Tile{X: 2897, Z: 3450, Level: 0}
	// FaladorBankStand is in front of the Falador WEST bank's booth row (2945..2949,3367).
	// It is the nearest bank to the chest by a wide margin: the east bank at 3010,3354 is
	// another ~65 tiles further, and there is no bank in Taverley at all.
	FaladorBankStand = tile.Tile{X: 2946, Z: 3369, Level: 0}
	// TaverleyGateEast and TaverleyGateWest are the two sides of the gate in Falador's west
	// wall (locs 1596/1597 at 2935,3450 and 2935,3451), which every route between the bank
	// and the chest crosses.
	TaverleyGateEast = tile.Tile{X: 2936, Z: 3451, Level: 0}
	TaverleyGateWest = tile.Tile{X: 2934, Z: 3451, Level: 0}
)

// BankToChestTiles and ChestToBankTiles are the measured one-way walking distance between the
// Falador west bank and the chest, as the collision pack's own A* counts tile steps across the
// three legs the two closed doors split the route into (93 + 40 + 17). Used only to reason
// about whether a trip pays for itself; nothing walks by this number.
const (
	BankToChestTiles = 150
	ChestToBankTiles = 150
)

// ---- how many keys to take out of the bank for one trip ----

// PackSlots is a full backpack.
const PackSlots = 28

// KeysPerTrip is how many keys to withdraw for one Falador-bank-to-chest-and-back trip.
//
// It is not "as many as fit". The binding constraint is that the loot has to come back in the
// same pack, and the guard below refuses to open a chest that might not fit, so withdrawing
// more keys than the pack can hold the loot for simply carries them there and back again.
//
// Chosen by simulating the real drop table against a 28-slot pack with the real guard
// (200,000 seeded trips per K; this package's own test re-runs the same simulation at 20,000,
// which differs only in the third significant figure):
//
//	K=3  all 3 opened on 100.00% of trips, mean 3.00 opens
//	K=4  97.7%, mean 3.98
//	K=5  81.7%, mean 4.76
//	K=6  54.5%, mean 5.14   <- the plateau starts here
//	K=7  27.7%, mean 5.15
//	K=8   9.6%, mean 4.85
//	K=9   2.9%, mean 4.36
//
// Mean opens per trip plateaus at K=6..7 and falls away after, because every extra key is a
// slot the loot cannot use. Six is the smaller of the two plateau values, so it is the one
// that carries the fewest keys for the same result. A trip that ends with keys unopened loses
// nothing: they go straight back into the bank at the end of it.
const KeysPerTrip = 6

// KeysToWithdraw is how many keys to draw to TOP THE PACK UP to a full trip's worth, given how
// many the bank holds and how many are already in hand.
//
// Topping up rather than drawing a flat six matters because leftovers are normal: a trip ends
// when the pack runs out of ROOM, not when it runs out of keys, so an account routinely comes
// back holding two or three. Live on 2026-09-21 the fleet owner spotted spare104 leaving with
// two, and the log agreed -- "2 key(s) in hand, 53 in the bank" -- which buys a 170-tile round
// trip for two opens instead of six.
func KeysToWithdraw(banked, held int) int {
	if banked < 0 {
		banked = 0
	}
	if held < 0 {
		held = 0
	}
	short := KeysPerTrip - held
	if short <= 0 {
		return 0
	}
	if short > banked {
		return banked
	}
	return short
}

// CanOpenAnother reports whether there is certainly room for another chest's reward.
//
// "Certainly" is the whole point. Content's inv_add puts anything that will not fit on the
// floor, so a pack one slot short does not fail loudly — it just loses the drop. free is the
// pack's free slots right now and held reports what it already holds, which matters because
// the worst case shrinks as the stackable rewards accumulate (see WorstCaseNewSlots).
func CanOpenAnother(free int, held func(id int) int) bool {
	return free >= WorstCaseNewSlots(held)
}

// CanOpenWithKeyInHand is CanOpenAnother made exact for an open that still has its key in the pack:
// free is the pack's empty slots with that key counted as held.
//
// Content deletes the key BEFORE it adds anything -- [label,open_crystal_chest] runs
// inv_del(inv, crystal_key, 1) and only then calls [label,crystal_chest_reward] (areas/area_taverly/
// scripts/crystal_chest.rs2) -- and a key is one unstackable slot, so by the time the reward needs
// its room that slot is already free: the open is safe when free+1 covers the worst reward. Without
// the one slot, a trip stops one open early wherever the worst case is exactly one slot short, and the
// account walks back to the bank with a key in hand (the main account's chest runs, 5 October 2026, 21:35: five
// opened and one key carried to the bank and back). Over the real drop table, with the kit a chest
// round carries, the old rule opens all six keys on 54% of trips and this one on 63%.
func CanOpenWithKeyInHand(free int, held func(id int) int) bool {
	return free+1 >= WorstCaseNewSlots(held)
}

// ---- is a run worth making at all ----

// Economy is everything the "worth while" decision needs, all of it supplied by the caller so
// this package never pretends to know a price or a walking speed.
type Economy struct {
	// CoinsPerKey is the expected value of one chest opening (ExpectedCoinsPerKey).
	CoinsPerKey float64
	// FishPerHour and CoinsPerFish are what the account would earn if it kept fishing
	// instead — the real alternative, not zero.
	FishPerHour  float64
	CoinsPerFish float64
	// TravelMinutes is the round trip between the mule's post and the Falador bank: the
	// fixed cost an expedition has to pay once, however many keys it carries.
	TravelMinutes float64
	// TripMinutes is one Falador-bank-to-chest-and-back trip, and OpensPerTrip how many
	// chests it opens (the simulation's mean, not the number of keys drawn).
	TripMinutes  float64
	OpensPerTrip float64
}

// DefaultEconomy is the measured fleet as of 2026-09-20, for a caller with nothing better.
// Every field's provenance:
//
//   - CoinsPerKey 3250: ExpectedCoinsPerKey with every item valued at HIGH ALCHEMY. The
//     deliberate floor. Valuing the same table at the market's own medians gives 19,053, and
//     the Loot panel's own rule (any median, however thin the sample) gives 461,170 — but that
//     figure is two one-trade outliers, an earth rune priced from four trades at 43,790 each
//     and a diamond from five at 1.2M. The floor is the number worth deciding on.
//   - FishPerHour 206: the live fleet's measured rate per account.
//   - CoinsPerFish 200: a raw lobster's own traded median.
//   - TripMinutes 2: MEASURED on the local engine, not estimated. cmd/lootchesttest -mode
//     travel walked the Falador west bank to ChestStand in 36s and back in 36s, at combat 3
//     with the automatic danger zones on, opening both closed doors on the way (the Falador
//     west-wall gate and the chest room's single entrance). 72s of walking plus a bank trip
//     and the opens themselves rounds to two minutes.
//   - TravelMinutes 6: MEASURED, and it is the SHIP route, not the mountain. With the automatic
//     White Wolf den zones on there is no land route between Catherby and Falador at all for a
//     low-combat account; the wolf-free way is the ship chain, and webwalk plans it at cost 813
//     once a hops handler is installed. cmd/lootchesttest -mode longleg walked it at combat 3
//     in 2m51s, paying the Ardougne-to-Brimhaven fare out of the travel reserve, so the round
//     trip is about six minutes. The pack is banked before it anyway: the reserve is the only
//     thing a death on this leg can cost.
//   - OpensPerTrip 5.14: the simulation's own mean for KeysPerTrip.
var DefaultEconomy = Economy{
	CoinsPerKey:   3250,
	FishPerHour:   206,
	CoinsPerFish:  200,
	TravelMinutes: 6,
	TripMinutes:   2,
	OpensPerTrip:  5.14,
}

// CoinsPerChestHour is what an account that never runs out of keys would earn, for comparison
// with CoinsPerFishingHour. It is deliberately a ceiling rather than a forecast: the real
// constraint on this script is key SUPPLY, not how fast it can spend them, and the mule makes
// keys only as fast as the fleet's caskets produce halves.
func (e Economy) CoinsPerChestHour() float64 {
	if e.TripMinutes <= 0 {
		return 0
	}
	return 60 / e.TripMinutes * e.OpensPerTrip * e.CoinsPerKey
}

// CoinsPerFishingHour is what the account earns by not going at all.
func (e Economy) CoinsPerFishingHour() float64 { return e.FishPerHour * e.CoinsPerFish }

// SurplusPerTrip is what one chest trip earns OVER what the same minutes of fishing would.
// Negative means a trip loses money against fishing and the whole idea is off.
func (e Economy) SurplusPerTrip() float64 {
	gain := e.OpensPerTrip * e.CoinsPerKey
	forgone := e.TripMinutes / 60 * e.CoinsPerFishingHour()
	return gain - forgone
}

// TravelCost is what the expedition's fixed travel costs in forgone fishing.
func (e Economy) TravelCost() float64 {
	return e.TravelMinutes / 60 * e.CoinsPerFishingHour()
}

// BreakEvenKeys is the smallest number of keys that makes an expedition pay for its own
// travel, at these prices: enough trips that the surplus over fishing covers the round trip
// to Falador and back. It is never less than 1, and 0 means "never worth it at these prices"
// (a trip that does not beat fishing on its own can never be paid for by doing more of them).
func (e Economy) BreakEvenKeys() int {
	surplus := e.SurplusPerTrip()
	if surplus <= 0 {
		return 0
	}
	trips := 1
	for float64(trips)*surplus < e.TravelCost() {
		trips++
		if trips > 1000 {
			return 0
		}
	}
	keys := trips * KeysPerTrip
	if keys < 1 {
		keys = 1
	}
	return keys
}

// MinKeysDefault is BreakEvenKeys(DefaultEconomy) rounded to a whole trip, kept as a constant
// so the script has a default that does not depend on a live price fetch. The test asserts it
// still matches the arithmetic, so a change to the economy cannot silently leave it stale.
const MinKeysDefault = 6

// ---- the loop ----

// Phase is one step of the script's own cycle.
type Phase string

const (
	// PhaseHandoff is at the mule: give it everything looted, and ask for whatever keys it
	// has made since. Always the first thing after arriving home, and the only place keys
	// come from.
	PhaseHandoff Phase = "handoff"
	// PhaseFish hands the account back to the registered ProgressiveFisher bot because
	// there are not enough keys to be worth the trip.
	PhaseFish Phase = "fish"
	// PhaseBankForTravel deposits everything before a long walk, so a death on the way costs
	// nothing — the owner's own "banking them for safe travel". The bank is shared, so the
	// keys are waiting at the other end.
	PhaseBankForTravel Phase = "bank-for-travel"
	// PhaseTravelToFalador walks to the Falador west bank.
	PhaseTravelToFalador Phase = "travel-to-falador"
	// PhaseWithdrawKeys draws one trip's keys at the Falador bank.
	PhaseWithdrawKeys Phase = "withdraw-keys"
	// PhaseOpenChest walks to the chest and opens until the keys or the pack run out.
	PhaseOpenChest Phase = "open-chest"
	// PhaseBankLoot walks back to the Falador bank and deposits the haul.
	PhaseBankLoot Phase = "bank-loot"
	// PhaseTravelHome walks back to the mule once no keys are left anywhere.
	PhaseTravelHome Phase = "travel-home"
	// PhaseDrainBank draws the banked chest haul back out, a pack at a time, so the mule can
	// have it: the fleet owner's "make sure it hands over everything in the bank besides 500gp
	// and untradeables once it has looted all keys".
	//
	// A phase of its own rather than a flag on PhaseHandoff, because the two differ in the one
	// way that matters to the dispatcher: a hand-off gives away what is ALREADY in the pack and
	// finishes when the pack is empty, while this has to open a bank, draw a load, hand it over
	// and come back for the next one until the bank itself is empty. Folding them together
	// would have made "am I finished?" mean two different things in one branch.
	PhaseDrainBank Phase = "drain-bank"
)

// Snapshot is everything DecidePhase reads, all of it a plain value.
type Snapshot struct {
	// AtMule and AtFalador are whether the account is already at each end of the long walk.
	AtMule, AtFalador bool
	// KeysInPack and KeysInBank are unnoted crystal keys.
	KeysInPack, KeysInBank int
	// NotedKeysInPack is crystal keys held as BANK NOTES, which the mule hands over so a whole
	// stock rides home in one stacked slot instead of 28 at a time. They cannot open a chest —
	// only KeysInPack can — but they are real keys the moment they are banked, so they count
	// toward TotalKeys and they force a bank trip before any travel. Reading them as zero was
	// the trap: an account that had just collected 400 of them would see no keys at all and go
	// back to fishing.
	NotedKeysInPack int
	// LootInPack is how many pack slots hold something that is neither a crystal key nor a
	// tool the script keeps (the haul).
	LootInPack int
	// PackFree is the free backpack slots.
	PackFree int
	// Held reports the pack's current count of an obj id, for the room guard. nil reads as
	// an empty pack.
	Held func(id int) int
	// MinKeys is the "worth while" threshold: below this many keys held anywhere, the
	// account fishes instead.
	MinKeys int
	// AtFaladorBank is whether the account is standing AT the Falador bank, as opposed to
	// merely somewhere in Falador. AtFalador covers the whole far end, and the bank and the
	// chest are 85 tiles apart inside it, so the two cannot be told apart without this.
	//
	// It exists for one decision: topping the keys up is FREE at the bank and costs a
	// 170-tile round trip anywhere else. See DecidePhase's first case.
	AtFaladorBank bool
	// Fishing is whether the delegated ProgressiveFisher is running the account right now.
	//
	// It exists because POSITION CANNOT DECIDE whether fishing may continue. The fisher picks
	// its own fishing area and walks itself there, and that area can be hundreds of tiles from
	// the mule: with fishingGuild on, an account at Fishing 68+ fishes the Fishing Guild, which
	// is ~220 tiles from the Catherby mule stand. See DecidePhase's rule 4.
	Fishing bool

	// Mule is the last thing this account heard about the MULE's own crystal-key bank, and
	// when it heard it — see MuleStock. It decides one thing only: whether walking to the mule
	// to collect keys is worth it (the owner's "only come back to the mule when they reach 100+
	// keys in the bank"). It never decides what this account does with keys it already holds.
	//
	// A zero value is "never heard", which is deliberately NOT the same as "heard, and it was
	// empty": the first is a reason to go and find out, the second is a reason to stay away.
	Mule MuleStock
	// Now is the instant the staleness of Mule is judged at. Zero reads every reading as stale,
	// which fails safe — the account fishes and probes rather than travelling on a number
	// nobody has dated.
	Now time.Time
	// Probe is the caller's own answer to ShouldProbe: it is time to go and find out what the
	// mule holds, because the reading is missing or stale and the blind-probe clock has come
	// round. It is passed in rather than computed here because the clock belongs to the caller
	// (it has to survive across Loop calls and be pushed back by every attempt), and because a
	// pure dispatcher that reads a wall clock cannot be tested.
	//
	// This is the EXIT from the unknown state, and the reason a mule that never answers cannot
	// strand the single account running this script: without it, "I do not know how many keys
	// the mule has" would be a permanent reason never to go and look.
	Probe bool
	// BankHaulRows is how many rows of the account's OWN bank are destined for the mule:
	// everything except the keys, the tools, the ship fare and content's own untradeables. It
	// drives PhaseDrainBank, and it is a REMEMBERED reading — a bank cannot be counted without
	// opening one — so a caller that has never had a bank open passes 0 and simply does not
	// drain until it has.
	BankHaulRows int
}

// WantsMule reports whether there is any reason at all to be standing at the mule: keys worth
// collecting, a banked haul to hand over, or a reading that has to be retaken.
//
// Gathered into one predicate because "should I walk to the mule?" is the exact question the
// fleet owner reported wrong twice — first "its trying to mule way too early", then an account
// that would not come back for 100+ waiting keys — and it was previously spread across three
// branches of DecidePhase that each answered a slightly different question.
//
// LootInPack is deliberately NOT one of the reasons, although it looks like the most obvious
// one. A pack with something in it is already handled where it belongs: the AtMule branch hands
// it over first, the in-between branch banks it before any long walk, and the AtFalador branch
// banks it at that end. Adding it here would have broken rule 4 in a way the existing tests
// caught immediately — a working fisher's pack is FULL OF FISH, so "loot in the pack" would
// have read as an errand and dragged the fisher home on every load.
func (s Snapshot) WantsMule() bool {
	return s.BankHaulRows > 0 || s.InterruptsFishing()
}

// InterruptsFishing is the narrower half: the reasons worth taking a WORKING fisher off its
// spot for, which is a higher bar than the reasons worth walking to the mule when idle.
//
// Only two things clear it. Keys known to be waiting above the threshold are the point of the
// whole script. A due probe is the way out of never having heard a count — without it, an
// account that starts up knowing nothing would fish forever past a mule holding hundreds of
// keys, which is exactly what happened live on 2026-09-20 (417 banked, zero asks in 50
// minutes).
//
// A banked haul deliberately does NOT clear it. The chest loot in the bank keeps perfectly
// well, and the account passes the mule on its own between every chest run; interrupting a
// fishing session for bookkeeping would cost more than it collects. It also could not be told
// apart from the fisher's own banked catch, which this script must not touch.
// A DUE PROBE IS NO LONGER ONE OF THEM, and that is the change of 2026-09-23. A probe is a
// QUESTION, and the question is asked and answered over the wall bus -- the mule's own decline
// carries the number ("only 69 Crystal key banked, holding until 100"). The bus reaches it from
// anywhere, so the 200-tile journey contributes nothing to getting the answer.
//
// The fleet owner watched this happen on four consecutive builds: "spare104 is still walking
// over to the mule????? the mule doesnt have 100 keys?????". On every fresh login,
//
//	00:30:31 [spare104] started
//	00:30:31 [spare104] lootchest: walking to the mule (2809,3441)
//
// with an empty pack, no keys anywhere, and a mule holding 69 against a reserve of 100. It
// walked to learn that, then walked back.
//
// The exit from "never heard a count" that the comment above is right to insist on is
// preserved intact: internal/bot/lootchestfisher.go asks over the bus whenever the phase says
// the mule is wanted, and PhaseHandoff still fires on a probe when the account is already
// carrying something. What is removed is only the idea that the walk itself is part of asking.
func (s Snapshot) InterruptsFishing() bool {
	return s.Mule.WorthVisiting(s.Now)
}

// TotalKeys is every crystal key the account can get at, in the pack and in the bank.
// TotalKeys is every crystal key the account can get at: in the pack, in the pack as notes,
// and in the bank. Notes count because banking them turns them into ordinary keys.
func (s Snapshot) TotalKeys() int { return s.KeysInPack + s.NotedKeysInPack + s.KeysInBank }

// DecidePhase is the script's own dispatcher.
//
// The order encodes three rules that matter, in the order they matter:
//  1. Never walk carrying something. Every long leg is preceded by a bank, because the route
//     crosses White Wolf Mountain and a death with a full pack loses the lot.
//  2. Never strand loot at the far end. Loot in the pack at Falador goes into the bank before
//     anything else; loot in the pack at the mule goes to the mule.
//  3. Never sit idle. With too few keys to be worth a trip, the account fishes — it does not
//     wait at the mule for the mule to make more.
//  4. Never drag a working fisher home. Once the delegated fisher is genuinely running, only
//     the key count ends it, never where the account happens to be standing.
//
// Rule 4 is the one that is easy to get wrong, and it was: PhaseFish used to be reachable only
// under `if s.AtMule`, so anything outside muleStandRadius (8 tiles) fell through to the "in
// between" branch and returned PhaseTravelHome. That silently contradicts rule 3 — the fisher
// walks to its own fishing area by design, so the moment it did its job the dispatcher decided
// the account was lost and walked it back. Live on 2026-09-20, `spare104` (Fishing 80, so
// fishingGuild sends it to the Fishing Guild ~220 tiles away) spent 29.5 minutes at ZERO XP in
// every skill doing exactly this: 10 walks started, 7 timed out, 0 arrivals, closest approach
// 9 tiles, a full Catherby<->Guild round trip every ~4.5 minutes. The 120s walk timeout it
// looked like was only an accelerant — a walk that ARRIVED would have been sent home by the
// very next dispatch just the same. At radius 8 this also clipped ordinary Catherby fishing
// (the spots sit outside 8 tiles of the bank); it merely survived there because the yank was
// short enough to look like normal banking.
func DecidePhase(s Snapshot) Phase {
	min := s.MinKeys
	if min < 1 {
		min = MinKeysDefault
	}

	if s.AtFalador {
		// Far end. Bank what was looted, spend what is left, and only go home when there is
		// genuinely nothing more to open.
		switch {
		case s.AtFaladorBank && s.LootInPack == 0 && s.KeysInPack < KeysPerTrip && s.KeysInBank > 0:
			// LEAVE THE BANK WITH A FULL TRIP, NEVER A PARTIAL ONE.
			//
			// This has to come first, and the reason is the bug it fixes. A trip ends when the
			// PACK runs out of room, not when the keys run out, so the account routinely banks
			// its haul with one or two keys still in hand. The moment that haul is banked the
			// pack is empty -- so the "keep opening while there is room" case below is true
			// again, and the account walked 85 tiles back to the chest carrying its leftovers
			// while the rest of its keys sat in the bank it was standing at.
			//
			// Live on 2026-09-21, and the log reads it out plainly: spare104 banked at 21:44:57
			// with "1 key(s) in hand, 36 in the bank", walked to the chest, opened ONE, and
			// walked back. A 170-tile round trip for a single open, six times over.
			//
			// Guarded on LootInPack == 0 so rule 2 still wins: the haul is banked by
			// PhaseBankLoot first (which also joins looted key halves and banks the coins,
			// neither of which the withdraw step does), and this fires on the pass after.
			return PhaseWithdrawKeys
		case s.KeysInPack > 0 && CanOpenAnother(s.PackFree, s.Held):
			// KEEP OPENING WHILE THERE IS CERTAINLY ROOM, and note that this case must come
			// BEFORE the loot one. It used to come after, so any reward at all sent the
			// account to the bank before "is there room for another?" was ever asked --
			// which turned a six-key trip into six round trips of about 85 tiles each
			// between the chest at 2914,3451 and the Falador bank. Observed live
			// 2026-09-21: spare104 drew six keys, opened ONE, and walked back to the bank
			// with five still in the pack.
			//
			// Reordering is safe precisely because CanOpenAnother is a worst-case check
			// (see its own doc comment: content's inv_add drops anything that will not fit
			// on the floor, so "probably fits" is not good enough). It only says yes when
			// the next reward certainly fits, so nothing is risked by asking it first.
			return PhaseOpenChest
		case s.LootInPack > 0:
			// Out of room, or out of keys. Bank the haul; coming back for more is the next
			// decision's job, not this one's.
			return PhaseBankLoot
		// The top-up used to sit HERE, below the open case, which is exactly why it never
		// fired: with the haul banked there is always room, so the open case won every time.
		// It is now the first case above, gated on standing at the bank.
		case s.KeysInPack > 0:
			// A full trip's worth already, or none left in the bank to add: bank the haul
			// (which is what filled the pack) and come back.
			return PhaseBankLoot
		case s.KeysInBank > 0:
			return PhaseWithdrawKeys
		default:
			return PhaseTravelHome
		}
	}

	// Rule 4. Deliberately below the AtFalador branch so rule 2 still wins: loot must never be
	// stranded at the far end, whatever else is running. Everywhere else, a running fisher owns
	// the account's position and its own pack — the fish it is carrying are its business, so
	// this also stops the "in between" branch below reading them as a haul to bank, and stops
	// the AtMule branch treating them as chest loot to hand over. Chest loot is unaffected: it
	// only exists after a Falador trip, and the fisher is stopped for the whole of one.
	//
	// The `!InterruptsFishing()` half was added 2026-09-21 and is the other side of the same
	// coin. Rule 4 as written made a running fisher immune to being taken off its spot for ANY
	// reason below the key threshold — which is right for its own catch and its own position,
	// and wrong for the one thing this script exists to do. Keys the mule is holding above the
	// threshold are worth the interruption, and so is finding out whether it has any; nothing
	// else is. See Snapshot.InterruptsFishing.
	if s.Fishing && s.TotalKeys() < min && !s.InterruptsFishing() {
		return PhaseFish
	}

	if s.AtMule {
		// SPEND THE KEYS BEFORE HANDING ANYTHING OVER.
		//
		// This branch used to hand over the moment LootInPack was above zero, which sent the
		// account to the mule with a pack of unopened crystal keys -- "trying to mule way too
		// early", as the fleet owner put it. Two things made that worse than a wasted trip.
		// The keys are on fisherplan.NeverHandOver, so MuleLoad offers NOTHING and the trade
		// opens and closes empty ("closed without anything crossing ... offered=0", live
		// 2026-09-21). And LootInPack counts the coin surplus, so a few spare coins alone were
		// enough to keep choosing a hand-off that could never carry anything.
		//
		// The cycle the owner described is: collect keys, open every one, THEN hand the haul
		// over. So keys in hand win over a hand-off, and the hand-off happens when they are
		// spent -- which is also when there is actually something to give.
		// Only once there are enough keys for a worthwhile run: min is the whole point of the
		// threshold, and going to Falador with two keys is the 170-tile round trip this script
		// already avoids elsewhere. Below it the account fishes, which is what FEEDS the key
		// supply -- caskets become halves, the mule joins them, and the keys come back here.
		// GIVE BEFORE YOU TAKE. This case used to sit BELOW the "enough keys, go to Falador"
		// one, and the fleet owner named the consequence exactly: "also no handover of loot
		// from the chest when grabbing more keys". An account that arrived at the mule holding
		// the haul AND already holding enough keys -- which is the ordinary end of a trip that
		// ran out of pack room rather than out of keys -- skipped the hand-off entirely and
		// set off back to Falador with the chest loot still in its pack, where the next thing
		// that could take it was a bank 150 tiles away.
		//
		// The owner's own wording is one exchange, not two trips: "if the mule has over 100keys
		// those should be traded IN RETURN FOR defi3lds bank". Giving first is also what makes
		// taking possible, and that is not merely tidy: the mule serves ONE trade partner at a
		// time and the fleet measures 1.51 trades a minute against 5.0 donors a minute, so a
		// meeting that does half the job costs a whole slot on the fleet's scarcest resource.
		// Handing the haul over first empties the pack, which is exactly the room the keys
		// coming the other way need.
		if s.LootInPack > 0 {
			return PhaseHandoff
		}
		// Keys in hand beat everything below: bank them and go spend them.
		if s.KeysInPack > 0 || s.NotedKeysInPack > 0 {
			return PhaseBankForTravel
		}
		if s.KeysInBank >= min {
			return PhaseTravelToFalador
		}
		// COLLECT THE KEYS, and only while there are genuinely keys to collect. WorthVisiting
		// is the owner's "over 100" and Probe is the way out of never having heard a number --
		// see Snapshot.Probe. A hand-off with an empty pack is how this script asks: the trade
		// half finds nothing to give and the ask half runs anyway, which is the same meeting
		// with one of its two trades skipped rather than a different phase.
		if s.Mule.WorthVisiting(s.Now) || s.Probe {
			return PhaseHandoff
		}
		// NOTHING LEFT TO OPEN, SO EMPTY THE BANK. The owner: "make sure it hands over
		// everything in the bank besides 500gp and untradeables once it has looted all keys".
		// Guarded on having no keys anywhere, so a run that still has chests to open is never
		// interrupted to do bookkeeping.
		if s.BankHaulRows > 0 && s.TotalKeys() == 0 {
			return PhaseDrainBank
		}
		return PhaseFish
	}

	// In between: on the road, or somewhere else entirely. Keep going to whichever end the
	// keys make sense at, and bank first if carrying anything.
	if s.LootInPack > 0 || s.KeysInPack > 0 || s.NotedKeysInPack > 0 {
		return PhaseBankForTravel
	}
	if s.TotalKeys() >= min {
		return PhaseTravelToFalador
	}
	// THE DEFAULT IS WORK, NOT A WALK TO THE MULE. This used to return PhaseTravelHome
	// unconditionally, which is the other half of "its trying to mule way too early": an
	// account with no keys and nothing to give walked the whole ship chain back to Catherby to
	// stand in front of a mule and ask a question it already knew the answer to. WantsMule is
	// the list of reasons that justify the trip, and every one of them is a real errand --
	// something to give, a bank to drain, keys known to be waiting, or a reading that has gone
	// stale and has to be retaken.
	if s.WantsMule() {
		return PhaseTravelHome
	}
	return PhaseFish
}

// HandoffDue reports whether a hand-off with the mule is worth starting: there is loot to give,
// or the account is out of keys and the mule may have made more.
func HandoffDue(s Snapshot) bool {
	return s.LootInPack > 0 || s.TotalKeys() < 1
}
