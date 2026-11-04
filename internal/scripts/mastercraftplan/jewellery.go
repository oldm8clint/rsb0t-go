package mastercraftplan

// THE JEWELLERY ROUND (a user request: "we want tick manipulation and run stalling utilized to
// its fullest potential for ultimate exp perhour and no downtime spent walking"). ScarabSwarm accounts
// rub a carried Games necklace or Ring of dueling every third tick while they run, which cuts the
// run energy a walk costs by about 65% (scarabplan/runstall.go, the tick lab of 6 Oct), and each one
// asks the mule for one on a rider line (scarabplan.RubAskLine). Nothing in the fleet held any. This
// round makes them: the main account turns the mule's gold bars and cut sapphires into Sapphire necklaces at the
// Falador furnace, enchants them with Lvl-1 Enchant into Games necklaces(8), and hands them to the
// mule.
//
// WHEN (design ruling, 6 Oct 2026 13:40: "the necklace/ring crafting should only happen if the overall
// fleet has none left, remembering that the fleet will swap over to high alch stalls sooner rather
// than later"): the mule's published bank holds NO Games necklace and NO Ring of dueling of any charge,
// and it holds the ingredients. Each trigger makes one batch of Settings.JewelleryBatch (default 50;
// 0 turns the round off), hands it to the mule and stops until the mule is empty again. Demand falls
// by itself: an account keeps its one, and one at Magic 55 or more walk-alchs instead of rubbing.
// Necklaces half made in this account's own bank, and Games necklaces made and not yet handed over,
// are carried on whatever the mule holds (Resume).
//
// THE CONTENT (content/scripts, branch 289):
//
//   - Lvl-1 Enchant (skill_magic/configs/magic_spells.dbrow [magic_spell_enchant_level1]): Magic 7,
//     not a members spell, 1 Water and 1 Cosmic rune (a worn Staff of water pays the water: magic.rs2
//     [proc,staff_runes]), 17.5 Magic XP (175 tenths), convertobj sapphire_necklace ->
//     necklace_of_minigames_8, "Games necklace(8)" (members=yes: on a free world the cast says "You
//     can only make that on a members' server."). The component is magic:enchant_lvl1, 1155
//     (pack/interface.pack). enchant.rs2 [opheldt,magic:enchant_lvl1] replaces the necklace IN ITS
//     SLOT (inv_setslot), takes the runes, then p_delay(1): the engine drops a cast read while the
//     player is delayed, so one cast every third tick, the pace superheat has (superheat.go THE PACE).
//   - A Sapphire necklace (skill_crafting/scripts/jewellery/jewellery.rs2 + configs/jewellery/
//     jewellery.struct): Crafting 20, 55 XP, one gold bar and one cut Sapphire, made at a furnace by
//     USING a gold bar on it ([oplocu,_smithing_furnace] -> craft_gold_interface; the furnace's Smelt
//     option opens the smelting menu instead). The interface needs a Necklace mould in the pack and a
//     Sapphire for its slot: it fills the dummy inventory crafting_make_necklaces (slot 1 =
//     invis_necklace1, object 1668, which the cache names "Sapphire necklace" too) and shows it as
//     crafting_jewelry:necklaces_inv (component 4239) with the options Make, Make 5 and Make 10 and no
//     Make X. craft_gold closes the interface, delays two ticks and makes one necklace, then again
//     with a two-tick delay before each until the count (1, 5 or 10) or the bars or sapphires run out:
//     a necklace every third tick. A count that stops short of what was asked (the bars ran out)
//     ends in a "You don't have enough ... to make any more." message box, a chat dialog, so a load is
//     sized to the button that finishes it: exactly ten (jewelleryLoad) for Make 10.
//   - The Necklace mould costs 5 gp (moulds.obj) and is sold by Rommik in Rimmington (2946,3205) and
//     Dommik in Al Kharid (areas/area_rimmington and area_alkharid, craftingshop: stock 2, cost
//     multiplier 100%). It is bought once and kept in the bank.
//   - The Falador furnace is the loc named "Furnace" with a Smelt option at 2975,3368, a 3x3 loc that
//     takes a use from its west side (internal/locreach); the Falador west bank's booths stand 29 tiles
//     away, and the Falador Teleport (Magic 37) lands 14 tiles from it.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// ModeJewellery is a jewellery round.
const ModeJewellery Mode = "jewellery"

// KeyJewelleryBatch is the setting that sizes a batch: the Games necklaces one round makes, 0 for none
// (the round is off).
const KeyJewelleryBatch = "craftJewelleryBatch"

func init() { knownKeys[KeyJewelleryBatch] = true }

const (
	// DefaultJewelleryBatch is the Games necklaces one trigger makes (design ruling, 6 Oct 2026).
	DefaultJewelleryBatch = 50
	// MaxJewelleryBatch bounds the setting: the mule hands out at most a thousand of an item an order.
	MaxJewelleryBatch = 1000
)

// The items, by name and object id (content pack/obj.pack; the noted form of each sits at id+1).
const (
	GoldBar             = "Gold bar"
	GoldBarID     int32 = 2357
	CutSapphire         = "Sapphire" // the cut gem: the uncut one is "Uncut sapphire"
	CutSapphireID int32 = 1607
	NecklaceMould       = "Necklace mould"
	// NecklaceMouldID is the unnoted mould.
	NecklaceMouldID  int32 = 1597
	SapphireNecklace       = "Sapphire necklace"
	// SapphireNecklaceID is the real necklace; MakeMenuNecklaceID the make menu's dummy of the same
	// name that never reaches a pack.
	SapphireNecklaceID int32 = 1656
	MakeMenuNecklaceID int32 = 1668
	// GamesNecklaceName and GamesNecklaceID are the enchant's product, necklace_of_minigames_8.
	GamesNecklaceName       = "Games necklace(8)"
	GamesNecklaceID   int32 = 3853
	RuneCosmic              = "Cosmic rune"
	RuneCosmicID      int32 = 564
	RuneWaterID       int32 = 555
	// CoinsName and CoinsID are the mould's price.
	CoinsName       = "Coins"
	CoinsID   int32 = 995
)

// The numbers (see the file comment for their sources).
const (
	// EnchantSpell is the spell's name as actions.ItemSpellComponent knows it, EnchantLevel its Magic
	// level, EnchantXPTenths its XP in content's tenths and EnchantTicks the ticks one cast takes.
	EnchantSpell    = "Lvl-1 Enchant"
	EnchantLevel    = 7
	EnchantXPTenths = 175
	EnchantTicks    = 3
	// NecklaceLevel and NecklaceXPTenths are the Sapphire necklace's Crafting level and XP, and
	// NecklaceTicks the ticks one takes inside a Make-N.
	NecklaceLevel    = 20
	NecklaceXPTenths = 550
	NecklaceTicks    = 3
	// MakeButtons: the necklace make menu's three options, as INV_BUTTON numbers, and the counts they
	// make.
	MakeOne, MakeFive, MakeTen = 1, 5, 10
	// JewelleryLoad is the bars (and so the sapphires) one load carries: exactly one Make 10, which
	// ends silently where a shorter count would end in a message box.
	JewelleryLoad = MakeTen
	// MouldCoins is the coins a mould purchase carries (the mould costs 5).
	MouldCoins = 100
	// JewelleryPackSlots is the backpack's slots.
	JewelleryPackSlots = 28
	// JewelleryEnchantMaxLoad is the most sapphire necklaces an enchant-only load draws: the pack less
	// its two rune stacks and the teleport's rune stacks.
	JewelleryEnchantMaxLoad = JewelleryPackSlots - 6
)

// ---- where it is done ----

// JewelleryWorkBank is where the work banks: in front of the Falador west bank's booths
// (lootchestfisherplan.FaladorBankStand), 29 tiles from the furnace, which the round finds by name.
var JewelleryWorkBank = lootchestfisherplan.FaladorBankStand

// JewelleryFurnaceRadius is how far from the work bank the furnace is searched for (the Falador one is
// 29 tiles away, the next nearest furnace hundreds).
const JewelleryFurnaceRadius = 45

// MouldShop is a crafting shop that sells the Necklace mould: its name, its keeper (the NPC the Trade
// option is clicked on) and the keeper's spawn tile.
type MouldShop struct {
	Name, Keeper string
	Stand        tile.Tile
}

// MouldShops are the shops that stock the mould, the nearest the work bank first: Rommik in Rimmington
// (map square 46,50 local 2,5: 226 route cost from the Falador west bank) and Dommik in Al Kharid (map
// square 51,49 local 58,58). Each keeps two (content craftingshop and craftingshop2, stock3).
func MouldShops() []MouldShop {
	return []MouldShop{
		{Name: "Rommik's Crafty Supplies", Keeper: "Rommik", Stand: tile.Tile{X: 2946, Z: 3205}},
		{Name: "Dommik's Crafting Store", Keeper: "Dommik", Stand: tile.Tile{X: 3322, Z: 3194}},
	}
}

// ---- what a place holds ----

// JewelleryHeld is the round's items in one place (the pack, or the bank), unnoted objects only.
type JewelleryHeld struct {
	Bars      int `json:"bars,omitempty"`
	Sapphires int `json:"sapphires,omitempty"`
	Necklaces int `json:"necklaces,omitempty"`
	Games     int `json:"games,omitempty"`
	Mould     int `json:"mould,omitempty"`
	Cosmic    int `json:"cosmic,omitempty"`
	Water     int `json:"water,omitempty"`
	Coins     int `json:"coins,omitempty"`
	// The ring's items (jewellerykind.go): cut Emeralds, Emerald rings, Rings of dueling(8), the Ring
	// mould and Air runes. As(kind) reads a holding as one kind.
	Emeralds     int `json:"emeralds,omitempty"`
	EmeraldRings int `json:"emeraldRings,omitempty"`
	Duelings     int `json:"duelings,omitempty"`
	RingMould    int `json:"ringMould,omitempty"`
	Air          int `json:"air,omitempty"`
}

// Pairs is the necklaces the bars and sapphires make.
func (h JewelleryHeld) Pairs() int { return max(0, min(h.Bars, h.Sapphires)) }

// Add is the two holdings together.
func (h JewelleryHeld) Add(o JewelleryHeld) JewelleryHeld {
	return JewelleryHeld{
		Bars: h.Bars + o.Bars, Sapphires: h.Sapphires + o.Sapphires, Necklaces: h.Necklaces + o.Necklaces, Games: h.Games + o.Games,
		Mould: h.Mould + o.Mould, Cosmic: h.Cosmic + o.Cosmic, Water: h.Water + o.Water, Coins: h.Coins + o.Coins,
		Emeralds: h.Emeralds + o.Emeralds, EmeraldRings: h.EmeraldRings + o.EmeraldRings, Duelings: h.Duelings + o.Duelings,
		RingMould: h.RingMould + o.RingMould, Air: h.Air + o.Air,
	}
}

// JewelleryHeldIn counts the round's items out of a count-by-object-id function.
func JewelleryHeldIn(count func(id int32) int) JewelleryHeld {
	return JewelleryHeld{
		Bars: count(GoldBarID), Sapphires: count(CutSapphireID), Necklaces: count(SapphireNecklaceID),
		Games: count(GamesNecklaceID), Mould: count(NecklaceMouldID), Cosmic: count(RuneCosmicID),
		Water: count(RuneWaterID), Coins: count(CoinsID),
		Emeralds: count(CutEmeraldID), EmeraldRings: count(EmeraldRingID), Duelings: count(DuelingRingID),
		RingMould: count(RingMouldID), Air: count(RuneAirID),
	}
}

// ---- the mule's demand ----

// chargeNames is "<base>(8)" down to "<base>(1)".
func chargeNames(base string) []string {
	out := make([]string, 0, 8)
	for c := 8; c >= 1; c-- {
		out = append(out, base+"("+strconv.Itoa(c)+")")
	}
	return out
}

// JewelleryDemandNames are the items the fleet's run stalling takes from the mule, every charge of
// both (scarabplan.RubItems): the Games necklace first.
func JewelleryDemandNames() []string {
	return append(chargeNames("Games necklace"), chargeNames("Ring of dueling")...)
}

// JewelleryDemandHeld is how many Games necklaces and Rings of dueling, of any charge, the mule's
// published bank holds.
func JewelleryDemandHeld(stock Stock) int {
	n := 0
	for _, name := range JewelleryDemandNames() {
		n += stockOf(stock, name)
	}
	return n
}

// ---- the ledger ----

// JewelleryLedger is what the jewellery round remembers across rounds and restarts (Ledger.Jewellery).
type JewelleryLedger struct {
	// Owed is the Games necklaces made and not yet handed to the mule, by bank object id: a failed
	// hand-over, or a restart before it, is made good by the next round. Only what this round made is
	// owed: a necklace this account carries for its own run stalling is its own.
	Owed map[string]int `json:"owed,omitempty"`
	// Target, Crafted and Enchanted are the batch under way: the Games necklaces (or Rings of dueling,
	// Kind) it is to make, the Sapphire necklaces (Emerald rings) made and the Games necklaces (Rings
	// of dueling) made so far. Target 0 is no batch.
	Target    int `json:"target,omitempty"`
	Crafted   int `json:"crafted,omitempty"`
	Enchanted int `json:"enchanted,omitempty"`
	// Kind is what the batch under way makes: "" for Games necklaces (every ledger before 7 Oct 2026),
	// "ring" for Rings of dueling.
	Kind JewelleryKind `json:"kind,omitempty"`
	// Bank is this account's bank's round items as a bank visit last read them: a restarted script has
	// not opened the bank yet, and the batch it resumes needs to know what is there.
	Bank JewelleryHeld `json:"bank,omitempty"`
	// PauseUntil holds jewellery rounds off after one that found nothing to do for a reason that will
	// not pass by itself.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Lifetime counters: rounds, Sapphire necklaces made, Games necklaces made and handed over,
	// moulds bought.
	Rounds       int `json:"rounds,omitempty"`
	Made         int `json:"made,omitempty"`
	Handed       int `json:"handed,omitempty"`
	MouldsBought int `json:"mouldsBought,omitempty"`
}

// AddOwed records n of the bank object id as owed to the mule.
func (l *JewelleryLedger) AddOwed(id, n int) {
	if n <= 0 || id <= 0 {
		return
	}
	if l.Owed == nil {
		l.Owed = map[string]int{}
	}
	l.Owed[strconv.Itoa(id)] += n
}

// OwedIDs is the owed necklaces by object id, ids ascending.
func (l *JewelleryLedger) OwedIDs() (ids []int, owed map[int]int) {
	owed = map[int]int{}
	for k, n := range l.Owed {
		id, err := strconv.Atoi(k)
		if err != nil || n <= 0 {
			continue
		}
		owed[id] += n
	}
	for id := range owed {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, owed
}

// OwedTotal is every owed unit.
func (l *JewelleryLedger) OwedTotal() int {
	if l == nil {
		return 0
	}
	n := 0
	for _, v := range l.Owed {
		if v > 0 {
			n += v
		}
	}
	return n
}

// Settle takes what crossed to the mule (by object id) off what is owed, and caps the rest at what
// the bank holds (banked nil: not read, nothing capped): a necklace that is no longer there cannot be
// handed over, and owing it would ask for it forever.
func (l *JewelleryLedger) Settle(handed map[int]int, banked func(id int) int) {
	_, owed := l.OwedIDs()
	out := map[string]int{}
	for id, n := range owed {
		n -= max(0, handed[id])
		if banked != nil {
			n = min(n, max(0, banked(id)))
		}
		if n > 0 {
			out[strconv.Itoa(id)] = n
		}
	}
	if len(out) == 0 {
		out = nil
	}
	l.Owed = out
}

// Under reports whether a batch is under way: some of its Games necklaces are still to make.
func (l *JewelleryLedger) Under() bool { return l != nil && l.Target > l.Enchanted }

// Left is the Games necklaces the batch under way still has to make.
func (l *JewelleryLedger) Left() int {
	if l == nil {
		return 0
	}
	return max(0, l.Target-l.Enchanted)
}

// StartBatch begins a batch of target Games necklaces: the counts start again.
func (l *JewelleryLedger) StartBatch(target int) { l.StartKindBatch(KindNecklace, target) }

// StartKindBatch begins a batch of target pieces of the kind: the counts start again.
func (l *JewelleryLedger) StartKindBatch(kind JewelleryKind, target int) {
	l.Target, l.Crafted, l.Enchanted, l.Kind = max(0, target), 0, 0, kind
}

// ClearBatch ends the batch under way (whatever of it was not made is not coming).
func (l *JewelleryLedger) ClearBatch() {
	l.Target, l.Crafted, l.Enchanted, l.Kind = 0, 0, 0, KindNecklace
}

// ---- the decision ----

// JewelleryPlan is what a new batch is sized to.
type JewelleryPlan struct {
	// Kind is what the batch makes.
	Kind JewelleryKind
	// N is the Games necklaces (Rings of dueling) the batch makes: the setting's batch, at most what the
	// mule holds of each ingredient (gold bars, cut Sapphires (Emeralds), Cosmic runes, and Water runes
	// (three Air runes a ring) unless a worn staff pays them). Gold, Sapphires, Cosmic and Water are the
	// mule's stocks as read, Sapphires being the kind's gem and Water its second rune.
	N                              int
	Gold, Sapphires, Cosmic, Water int
	// Why is the reason N is 0, "" otherwise.
	Why string
}

// PlanJewelleryBatch sizes a new batch of Games necklaces from the mule's published bank.
func PlanJewelleryBatch(set Settings, mule Stock, waterPaid bool) JewelleryPlan {
	return PlanKindBatch(KindNecklace, set, mule, waterPaid)
}

// PlanKindBatch sizes a new batch of the kind from the mule's published bank (secondPaid: a worn staff
// pays the kind's second rune).
func PlanKindBatch(kind JewelleryKind, set Settings, mule Stock, secondPaid bool) JewelleryPlan {
	sp := kind.Spec()
	p := JewelleryPlan{
		Kind: kind, Gold: stockOf(mule, GoldBar), Sapphires: stockOf(mule, sp.Gem),
		Cosmic: stockOf(mule, RuneCosmic), Water: stockOf(mule, sp.Second),
	}
	if mule == nil {
		p.Why = "the mule has not published its bank"
		return p
	}
	batch := set.BatchOf(kind)
	water := p.Water / sp.SecondPer
	if secondPaid {
		water = batch
	}
	p.N = min(batch, p.Gold, p.Sapphires, p.Cosmic, water)
	if p.N > 0 {
		return p
	}
	var missing []string
	for _, m := range []struct {
		item string
		n    int
		ok   bool
	}{{GoldBar, p.Gold, true}, {sp.Gem, p.Sapphires, true}, {RuneCosmic, p.Cosmic, true}, {sp.Second, water, !secondPaid}} {
		if m.ok && m.n <= 0 {
			missing = append(missing, m.item)
		}
	}
	p.Why = "the mule holds no " + strings.Join(missing, ", no ")
	return p
}

// JewelleryOK reports whether this account may make Games necklaces at all: a members world (the
// necklace is a members item), Magic for the enchant and Crafting for the necklace, both known.
func JewelleryOK(set Settings, magic, crafting int) (bool, string) {
	return JewelleryOKFor(KindNecklace, set, magic, crafting)
}

// JewelleryOKFor is JewelleryOK for a kind: a Ring of dueling needs Magic 27 (Lvl-2 Enchant) and
// Crafting 27 (an Emerald ring).
func JewelleryOKFor(kind JewelleryKind, set Settings, magic, crafting int) (bool, string) {
	sp := kind.Spec()
	switch {
	case !set.MembersWorld:
		return false, sp.Product + "s are members items (" + KeyMembersWorld + " is off)"
	case magic < sp.SpellLevel:
		return false, "Magic " + strconv.Itoa(magic) + " is under the " + strconv.Itoa(sp.SpellLevel) + " " + sp.Spell + " needs"
	case crafting < sp.CraftLevel:
		return false, "Crafting " + strconv.Itoa(crafting) + " is under the " + strconv.Itoa(sp.CraftLevel) + " " + articled(sp.Base) + " needs"
	}
	return true, ""
}

// articled is "a Sapphire necklace" or "an Emerald ring".
func articled(name string) string {
	if name != "" && strings.ContainsRune("AEIOU", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}

// JewelleryReady decides a jewellery round. In this order:
//
//  1. Games necklaces made and not yet handed to the mule are owed: the round is a hand-over (always,
//     whatever craftJewelleryBatch says: they are the mule's).
//  2. This account's own bank holds Sapphire necklaces and the runes to enchant them: the round
//     enchants them (Resume). Off with the batch setting at 0.
//  3. A batch is under way (the ledger) and the bank still holds its materials, or has not been read
//     since the script started: the round carries it on (Resume).
//  4. The mule's published bank holds no Ring of dueling of any charge, and the ingredients for at
//     least one: a new batch of Rings of dueling (the a user request; craftRingBatch).
//  5. The mule's published bank holds no Games necklace and no Ring of dueling of any charge, and the
//     ingredients for at least one: a new batch of Games necklaces (the owner's rule of 6 Oct).
//
// Each kind needs its own Magic and Crafting levels (JewelleryOKFor); a kind that cannot be made
// gives way to the other. It reads nothing a mule that is not here would own: the caller has checked
// the mule is heard.
func JewelleryReady(set Settings, s Snapshot, led *Ledger) (Decision, string) {
	jl := &led.Jewellery
	if n := jl.OwedTotal(); n > 0 {
		if until, held := led.BackedOff(ModeDeliver, s.Now); held {
			return Decision{}, strconv.Itoa(n) + " Games necklace(s) and Ring(s) of dueling are owed to the mule but its hand-overs are backing off until " + until.Format("15:04")
		}
		return Decision{Mode: ModeJewellery, Resume: true, Why: strconv.Itoa(n) + " Games necklace(s) and Ring(s) of dueling made earlier are still owed to the mule"}, ""
	}
	if set.JewelleryBatch <= 0 && set.RingBatch <= 0 {
		return Decision{}, "jewellery is off (" + KeyJewelleryBatch + " and " + KeyRingBatch + " are 0)"
	}
	var whys []string
	// What this account's own bank holds, a batch under way first: its kind carries on before any other.
	kinds := Kinds()
	if jl.Under() && jl.Kind != kinds[0] {
		kinds = []JewelleryKind{jl.Kind, kinds[0]}
	}
	usable := map[JewelleryKind]bool{}
	for _, kind := range kinds {
		if set.BatchOf(kind) <= 0 {
			continue
		}
		if ok, why := JewelleryOKFor(kind, set, s.MagicLevel, s.CraftingLevel); !ok {
			whys = append(whys, why)
			continue
		}
		usable[kind] = true
		sp := kind.Spec()
		own := s.OwnJewellery.As(kind)
		runes := kind.Casts(own, s.SecondPaid(kind)) > 0
		under := jl.Under() && jl.Kind == kind
		switch {
		case own.Necklaces > 0 && runes:
			return Decision{Mode: ModeJewellery, Kind: kind, Resume: true, Why: "this account's bank holds " + strconv.Itoa(own.Necklaces) + " " + sp.Base + "(s) to enchant"}, ""
		case under && !s.OwnJewelleryKnown:
			return Decision{Mode: ModeJewellery, Kind: kind, Resume: true, Why: "a batch of " + strconv.Itoa(jl.Target) + " " + sp.Plural + " is under way (" + strconv.Itoa(jl.Enchanted) + " made) and the bank has not been read since the script started"}, ""
		case under && (own.Pairs() > 0 || own.Necklaces > 0) && runes:
			return Decision{Mode: ModeJewellery, Kind: kind, Resume: true, Why: "a batch of " + strconv.Itoa(jl.Target) + " " + sp.Plural + " is under way (" + strconv.Itoa(jl.Enchanted) + " made) and this account's bank holds its materials"}, ""
		}
	}
	if len(usable) == 0 {
		if len(whys) == 0 {
			return Decision{}, "jewellery is off (" + KeyJewelleryBatch + " and " + KeyRingBatch + " are 0)"
		}
		return Decision{}, strings.Join(whys, "; ")
	}
	if s.Stock == nil {
		return Decision{}, "the mule has not published its bank"
	}
	// A new batch. Rings of dueling when the mule holds none; Games necklaces when it holds no
	// necklace and no ring (the owner's rule of 6 Oct).
	var notes []string
	if usable[KindRing] {
		if n := KindRing.DemandHeld(s.Stock); n > 0 {
			notes = append(notes, "the mule holds "+strconv.Itoa(n)+" Ring(s) of dueling")
		} else if p := PlanKindBatch(KindRing, set, s.Stock, s.SecondPaid(KindRing)); p.N > 0 {
			return Decision{Mode: ModeJewellery, Kind: KindRing, Jewellery: p, Why: "the mule holds no Ring of dueling; a batch of " + strconv.Itoa(p.N) + " is " +
				strconv.Itoa(p.Gold) + " gold bars, " + strconv.Itoa(p.Sapphires) + " Emeralds and " + strconv.Itoa(p.Cosmic) + " Cosmic runes deep (batch " + strconv.Itoa(set.RingBatch) + ")"}, ""
		} else {
			notes = append(notes, "the mule holds no Ring of dueling and cannot fund a batch: "+p.Why)
		}
	}
	if usable[KindNecklace] {
		if n := JewelleryDemandHeld(s.Stock); n > 0 {
			return Decision{}, joinWhy("the mule holds "+strconv.Itoa(n)+" Games necklace(s) and Ring(s) of dueling", notes)
		}
		p := PlanJewelleryBatch(set, s.Stock, s.SecondPaid(KindNecklace))
		if p.N <= 0 {
			return Decision{}, joinWhy("the mule holds none and cannot fund a batch: "+p.Why, notes)
		}
		return Decision{Mode: ModeJewellery, Jewellery: p, Why: "the mule holds no Games necklace or Ring of dueling; a batch of " + strconv.Itoa(p.N) + " is " +
			strconv.Itoa(p.Gold) + " gold bars, " + strconv.Itoa(p.Sapphires) + " Sapphires and " + strconv.Itoa(p.Cosmic) + " Cosmic runes deep (batch " + strconv.Itoa(set.JewelleryBatch) + ")"}, ""
	}
	return Decision{}, strings.Join(append(notes, whys...), "; ")
}

// joinWhy is a reason with the other kind's reasons after it.
func joinWhy(first string, notes []string) string {
	if len(notes) == 0 {
		return first
	}
	return first + "; " + strings.Join(notes, "; ")
}

// SecondPaid is whether a worn staff pays the kind's second rune (a Staff of water the Water rune, a
// Staff of air the Air rune): WaterPaid for the necklace, AirPaid for the ring.
func (s Snapshot) SecondPaid(kind JewelleryKind) bool {
	if kind == KindRing {
		return s.AirPaid
	}
	return s.WaterPaid
}

// ---- the teleports that shorten the legs ----

// JewelleryLeg is a leg of the round a teleport can shorten.
type JewelleryLeg int

// The legs.
const (
	// LegToFalador is the mule's post to the Falador furnace and bank: Falador Teleport (Magic 37).
	LegToFalador JewelleryLeg = iota + 1
	// LegHome is the Falador side back to the post: Camelot Teleport (Magic 45) or Varrock Teleport
	// (Magic 25), ChestHomeSpellFrom.
	LegHome
)

// JewelleryLegSpell is the teleport that shortens the leg at this Magic level, "" for none.
func JewelleryLegSpell(leg JewelleryLeg, magic int) string {
	switch leg {
	case LegToFalador:
		if fal, _ := ChestCastable(magic); fal {
			return FaladorTeleport
		}
	case LegHome:
		return ChestHomeSpellFrom(magic, true)
	}
	return ""
}

// JewelleryTravelRunes is the runes of one cast of the leg's teleport, by name, none of a rune a worn
// staff pays for (paid: RunesPaidByStaff). Empty when no teleport shortens the leg.
func JewelleryTravelRunes(leg JewelleryLeg, magic int, paid map[string]bool) map[string]int {
	out := map[string]int{}
	spell := JewelleryLegSpell(leg, magic)
	if spell == "" {
		return out
	}
	for name, n := range ChestSpellRunes(spell) {
		if !paid[name] {
			out[name] += n
		}
	}
	return out
}

// jewelleryTravelNeed is the runes of both legs' teleports, by name.
func jewelleryTravelNeed(magic int, paid map[string]bool) map[string]int {
	out := map[string]int{}
	for _, leg := range []JewelleryLeg{LegToFalador, LegHome} {
		for name, n := range JewelleryTravelRunes(leg, magic, paid) {
			out[name] += n
		}
	}
	return out
}

// ---- the ask ----

// PlanJewelleryAsk is the kit a new batch asks the mule for at its post: the gold bars and cut
// Sapphires of the Sapphire necklaces still to make (none of this account's own, which stay its own
// whatever the bank holds), the Cosmic and Water runes of the enchants and of the teleports'
// legs that this account's bank is short of (no Water when a worn staff pays it), and coins for the
// mould when none is held anywhere and the bank is short of them. Only what the mule publishes, at
// most its stock. own is this account's bank by item name.
func PlanJewelleryAsk(n int, magic int, paid map[string]bool, mule Stock, own JewelleryHeld, ownOf func(item string) int, haveMould bool) []Line {
	return PlanKindAsk(KindNecklace, n, magic, paid, mule, own, ownOf, haveMould)
}

// PlanKindAsk is PlanJewelleryAsk for a kind: own is this account's bank read As the kind, the gems are
// the kind's (Emeralds for rings) and its second rune is asked at its count a cast (three Air a ring).
func PlanKindAsk(kind JewelleryKind, n int, magic int, paid map[string]bool, mule Stock, own JewelleryHeld, ownOf func(item string) int, haveMould bool) []Line {
	if ownOf == nil {
		ownOf = func(string) int { return 0 }
	}
	sp := kind.Spec()
	var lines []Line
	add := func(item string, want int) {
		if want = min(want, stockOf(mule, item)); want > 0 {
			lines = append(lines, Line{Item: item, Qty: want})
		}
	}
	craft := max(0, n-own.Necklaces)
	add(GoldBar, craft)
	add(sp.Gem, craft)
	need := map[string]int{RuneCosmic: n}
	if !paid[sp.Second] {
		need[sp.Second] = n * sp.SecondPer
	}
	for name, c := range jewelleryTravelNeed(magic, paid) {
		need[name] += c
	}
	names := make([]string, 0, len(need))
	for name := range need {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add(name, need[name]-ownOf(name))
	}
	if !haveMould && own.Coins < MouldCoins {
		add(CoinsName, 2*MouldCoins)
	}
	return lines
}
