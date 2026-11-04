// Package lootchestfisherplan is LootChestFisher's own decisions: where the Taverley crystal
// chest is and where an account may stand to open it, how many keys a pack can hold the loot
// for, whether a run is worth making at all against what the account would earn fishing
// instead, and which phase of the loop it is in. It is pure: no client, actions, fleet or bot
// imports and no I/O, so every number below is testable without a server.
//
// The script it plans, in the owner's own words (2026-09-20):
//
//	"there should also be a script made for navigating to falador, after getting the keys from
//	mulereceiver and banking them for safe travel. then you can withdraw an amount that allows
//	you to bring all of the loot the crystal chest gives back to the closest falador bank.
//	after keys are all used up, the account should return to the mulereceiver to drop off all
//	loot and get anymore keys they may have aquired in the time it took us to open, if not we
//	should fall back to the progressive fisher until theres enough crystal keys to go make it
//	worth while looting them."
//
// Deliberately not modelled here, and why:
//   - The route itself. internal/webwalk owns long-distance planning, doors, gates and danger
//     zones; this package only names the tiles. The one geometric fact it does own is the
//     chest's legal standing tile, because that is a reach-strategy question internal/locreach
//     already answers and getting it wrong is silent (CLAUDE.md's OpLocU gotcha).
//   - The fishing fallback's own behaviour. It is a genuine hand-over to the registered
//     ProgressiveFisher bot, not a reimplementation; this package only decides when.
package lootchestfisherplan

// The drop table is content 289's own
// content/scripts/areas/area_taverly/scripts/crystal_chest.rs2, [label,crystal_chest_reward],
// transcribed branch for branch. It is here for ONE purpose — working out how much pack room
// an open can need, and what a key is worth — never to predict or attribute a drop. What the
// Loot panel shows is measured from the pack, like every other source (see internal/loot).
//
// The structure of that content, so the numbers below can be checked against it:
//
//	inv_add(inv, uncut_dragonstone, 1);         // EVERY open, before the branch
//	def_int $random = random(128);              // 0..127
//	if ($random < 34)  spinach roll + 2000 coins
//	else if ($random < 46)  50 air/water/earth/fire/body/mind + 10 chaos/death/cosmic/nature/law
//	else if ($random < 58)  2 rubies + 2 diamonds
//	else if ($random < 70)  3 runite bars
//	else if ($random < 80)  750 coins + one key half (either, 50/50)
//	else if ($random < 90)  150 noted iron ore
//	else if ($random < 100) 100 noted coal
//	else if ($random < 108) 5 raw swordfish + 1000 coins
//	else if ($random < 110) an adamant sq shield
//	else if ($random < 111) rune platelegs or plateskirt, by gender
//	// 111..127 — 17 of 128 — is nothing but the dragonstone.
//
// The key is consumed on every open, before any of this: inv_del(inv, crystal_key, 1) sits in
// [label,open_crystal_chest] above the reward call, so a chest never gives a key back for
// free and the 17/128 "nothing" branch really is a key spent for one dragonstone.

// RandomRange is content's own random(128).
const RandomRange = 128

// Obj ids, from content/pack/obj.pack. Named rather than inlined so a test can read.
const (
	UncutDragonstone = 1631
	Coins            = 995
	SpinachRoll      = 1969
	Ruby             = 1603
	Diamond          = 1601
	RuniteBar        = 2363
	KeyHalf1         = 985
	KeyHalf2         = 987
	CrystalKey       = 989
	NotedIronOre     = 441
	NotedCoal        = 454
	RawSwordfish     = 371
	AdamantSqShield  = 1183
	RunePlatelegs    = 1079
	RunePlateskirt   = 1093

	AirRune    = 556
	WaterRune  = 555
	EarthRune  = 557
	FireRune   = 554
	BodyRune   = 559
	MindRune   = 558
	ChaosRune  = 562
	DeathRune  = 560
	CosmicRune = 564
	NatureRune = 561
	LawRune    = 563
)

// Reward is one inv_add line: an obj id, how many, and whether the pack stacks it into one
// slot. Stacking is what decides pack room, and it is not guessable from the id — runes,
// coins and bank notes stack; gems, bars, fish, armour and key halves do not.
type Reward struct {
	ID     int
	Count  int
	Stacks bool
}

// Branch is one arm of the content's if/else chain. Weight is how many of the 128 rolls land
// in it.
type Branch struct {
	Name    string
	Weight  int
	Rewards []Reward
}

// Table is the reward chain, in content's own order. The always-added uncut dragonstone is
// NOT in it: it is added before the branch and is accounted for separately, so a caller can
// never double-count it.
func Table() []Branch {
	return []Branch{
		{Name: "spinach roll and 2000 coins", Weight: 34, Rewards: []Reward{
			{SpinachRoll, 1, false}, {Coins, 2000, true}}},
		{Name: "rune bundle", Weight: 12, Rewards: []Reward{
			{AirRune, 50, true}, {WaterRune, 50, true}, {EarthRune, 50, true}, {FireRune, 50, true},
			{BodyRune, 50, true}, {MindRune, 50, true}, {ChaosRune, 10, true}, {DeathRune, 10, true},
			{CosmicRune, 10, true}, {NatureRune, 10, true}, {LawRune, 10, true}}},
		{Name: "2 rubies and 2 diamonds", Weight: 12, Rewards: []Reward{
			{Ruby, 2, false}, {Diamond, 2, false}}},
		{Name: "3 runite bars", Weight: 12, Rewards: []Reward{{RuniteBar, 3, false}}},
		// Content picks one half at random. Modelled as keyhalf1 for room purposes: both are
		// one unstackable slot, so which one it is cannot change the answer.
		{Name: "750 coins and a key half", Weight: 10, Rewards: []Reward{
			{Coins, 750, true}, {KeyHalf1, 1, false}}},
		{Name: "150 noted iron ore", Weight: 10, Rewards: []Reward{{NotedIronOre, 150, true}}},
		{Name: "100 noted coal", Weight: 10, Rewards: []Reward{{NotedCoal, 100, true}}},
		{Name: "5 raw swordfish and 1000 coins", Weight: 8, Rewards: []Reward{
			{RawSwordfish, 5, false}, {Coins, 1000, true}}},
		{Name: "adamant sq shield", Weight: 2, Rewards: []Reward{{AdamantSqShield, 1, false}}},
		// One roll in 128, and the gender switch picks legs or skirt. One unstackable slot
		// either way.
		{Name: "rune platelegs or plateskirt", Weight: 1, Rewards: []Reward{{RunePlatelegs, 1, false}}},
		{Name: "nothing but the dragonstone", Weight: 17},
	}
}

// NewSlotsFor is how many backpack slots branch b would newly occupy, given what the pack
// already holds (held reports a current count for an obj id; nil reads as an empty pack).
// The always-added uncut dragonstone is included, since it never stacks and arrives on every
// single open.
func NewSlotsFor(b Branch, held func(id int) int) int {
	n := 1 // the uncut dragonstone
	for _, r := range b.Rewards {
		if !r.Stacks {
			n += r.Count
			continue
		}
		if held == nil || held(r.ID) == 0 {
			n++
		}
	}
	return n
}

// WorstCaseNewSlots is the most slots ANY single open could need against this pack. A caller
// with at least this many free slots cannot lose a reward to a full pack, whatever the roll
// gives — content's inv_add drops the overflow on the floor, where nothing picks it up.
//
// It falls from 12 on an empty pack (the rune bundle's eleven rune types plus the dragonstone)
// to 6 once every rune is already held (the five raw swordfish plus the dragonstone), which is
// why the guard is recomputed before every open rather than fixed once.
func WorstCaseNewSlots(held func(id int) int) int {
	worst := 1
	for _, b := range Table() {
		if n := NewSlotsFor(b, held); n > worst {
			worst = n
		}
	}
	return worst
}

// TotalWeight is the sum of every branch's weight; it must equal RandomRange or the table has
// been transcribed wrong.
func TotalWeight() int {
	n := 0
	for _, b := range Table() {
		n += b.Weight
	}
	return n
}

// ExpectedCoinsPerKey is the expected value of one open, given a unit price for each obj id.
// price is the caller's own valuation (the control panel's PriceBook, high alchemy, whatever
// it can defend) and returns 0 for anything it cannot value, so a missing price understates
// rather than invents. The uncut dragonstone is counted once per open, as content adds it.
func ExpectedCoinsPerKey(price func(id int) int64) float64 {
	if price == nil {
		return 0
	}
	total := float64(price(UncutDragonstone))
	for _, b := range Table() {
		var sum int64
		for _, r := range b.Rewards {
			sum += price(r.ID) * int64(r.Count)
		}
		total += float64(b.Weight) / float64(RandomRange) * float64(sum)
	}
	return total
}
