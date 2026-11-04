package mastercraftplan

// THE TWO PRODUCTS OF THE JEWELLERY ROUND (a user request: "rings of dueling for the fleet: make them
// (enchant emerald rings, as the Games necklaces are made in the jewellery round) and hand them to the
// mule; accounts can then use their teleports and rub them for run-energy stalls").
//
// The round makes one kind a batch. Both are the same work with different items, so the round's
// functions read a JewelleryKind for the items, the spell and the menu and keep one state machine
// (jewelleryround.go) over a holding read AS that kind (JewelleryHeld.As): the gem always sits in the
// Sapphires field, the made jewellery in Necklaces, the enchanted one in Games, the mould in Mould and
// the spell's second rune in Water, whatever the kind. The zero JewelleryKind is the necklace, so a
// ledger written before rings existed reads as it always did.
//
// THE CONTENT (content/scripts, branch 289):
//
//   - An Emerald ring (skill_crafting/configs/jewellery/jewellery.struct [emerald_ring], jewellery.rs2):
//     Crafting 27, 55 XP, one gold bar and one cut Emerald, made at a furnace by using a gold bar on it,
//     with a Ring mould in the pack. The interface is the same crafting_jewelry (4161); its rings menu is
//     crafting_jewelry:rings_inv (4233, pack/interface.pack) and the Emerald ring sits in slot 2 of
//     crafting_make_rings (slot 0 the gold ring, 1 the Sapphire ring), with the same Make, Make 5 and
//     Make 10 options: craft_gold is shared, so a ring comes every third tick like a necklace.
//   - The Ring mould is sold by the same two keepers as the Necklace mould (Rommik in Rimmington and
//     Dommik in Al Kharid: rimmington.inv and alkharid.inv stock2=ring_mould,4,100), 5 gp.
//   - Lvl-2 Enchant (skill_magic/configs/magic_spells.dbrow [magic_spell_enchant_level2]): Magic 27, not a
//     members spell, 3 Air and 1 Cosmic rune (a worn Staff of air pays the Air: magic.rs2
//     [proc,staff_runes]), 37 Magic XP (370 tenths), convertobj emerald_ring -> ring_of_dueling_8,
//     "Ring of dueling(8)" (members=yes: on a free world the cast says "You can only make that on a
//     members' server."). The component is magic:enchant_lvl2, 1165. enchant.rs2 is the same label for
//     every level: the ring is replaced in its slot and the cast ends in p_delay(1), so a cast every
//     third tick, as the Lvl-1 Enchant has.
//   - Ring of dueling(8) is obj 2552 (noted 2553), charges 8 (param=charges), "Rub" (iop4) teleports to the
//     Al Kharid Duel Arena or Castle Wars (general/scripts/enchanted_jewellry/ring_of_dueling.rs2), and
//     the run staller rubs it as a Games necklace (scarabplan.IsRubItem).

// JewelleryKind is what a jewellery batch makes. The zero value is the Games necklace.
type JewelleryKind string

// The kinds.
const (
	// KindNecklace is the Games necklace(8): gold bar + Sapphire, Lvl-1 Enchant.
	KindNecklace JewelleryKind = ""
	// KindRing is the Ring of dueling(8): gold bar + Emerald, Lvl-2 Enchant.
	KindRing JewelleryKind = "ring"
)

// The ring's items, by name and object id (content pack/obj.pack; the noted form sits at id+1).
const (
	CutEmerald         = "Emerald" // the cut gem: the uncut one is "Uncut emerald"
	CutEmeraldID int32 = 1605
	RingMould          = "Ring mould"
	RingMouldID  int32 = 1592
	EmeraldRing        = "Emerald ring"
	// EmeraldRingID is the real ring; MakeMenuRingID the rings menu's dummy of the same name that never
	// reaches a pack.
	EmeraldRingID  int32 = 1639
	MakeMenuRingID int32 = 1650
	// DuelingRingName and DuelingRingID are the enchant's product, ring_of_dueling_8.
	DuelingRingName       = "Ring of dueling(8)"
	DuelingRingID   int32 = 2552
	RuneAirID       int32 = 556
)

// The ring's numbers (see the file comment for their sources).
const (
	// EnchantRingSpell is the spell's name as actions.ItemSpellComponent knows it, EnchantRingLevel its
	// Magic level, EnchantRingXPTenths its XP in content's tenths and EnchantRingAir the Air runes a cast
	// takes (with one Cosmic).
	EnchantRingSpell    = "Lvl-2 Enchant"
	EnchantRingLevel    = 27
	EnchantRingXPTenths = 370
	EnchantRingAir      = 3
	// RingLevel and RingXPTenths are the Emerald ring's Crafting level and XP.
	RingLevel    = 27
	RingXPTenths = 550
)

// KindSpec is a kind's items, spell and menu.
type KindSpec struct {
	Kind JewelleryKind
	// Word is the kind in a log line ("necklace", "ring"); Product the enchanted item's short name
	// ("Games necklace", "Ring of dueling") and Plural its plural.
	Word, Product, Plural string
	// Mould, Gem, Base and Final are the items by name and object id: the mould kept, the cut gem, the
	// jewellery made at the furnace (Sapphire necklace, Emerald ring) and the enchanted one.
	Mould, Gem, Base, Final string
	MouldID, GemID, BaseID  int32
	FinalID                 int32
	// Spell is the enchant's name, SpellLevel its Magic level, SpellXPTenths its XP; Second is the rune
	// that goes with the Cosmic rune (Water for the necklace, Air for the ring), SecondID its object id
	// and SecondPer how many of it a cast takes.
	Spell               string
	SpellLevel, SpellXP int
	Second              string
	SecondID            int32
	SecondPer           int
	// CraftLevel and CraftXP are the Crafting level and XP of the base item.
	CraftLevel, CraftXP int
	// MenuInv is the interface component of the make menu's inventory, MenuSlot the slot of the base item
	// in it.
	MenuInv, MenuSlot int
}

// Spec is the kind's items.
func (k JewelleryKind) Spec() KindSpec {
	if k == KindRing {
		return KindSpec{
			Kind: KindRing, Word: "ring", Product: "Ring of dueling", Plural: "Rings of dueling",
			Mould: RingMould, Gem: CutEmerald, Base: EmeraldRing, Final: DuelingRingName,
			MouldID: RingMouldID, GemID: CutEmeraldID, BaseID: EmeraldRingID, FinalID: DuelingRingID,
			Spell: EnchantRingSpell, SpellLevel: EnchantRingLevel, SpellXP: EnchantRingXPTenths,
			Second: RuneAir, SecondID: RuneAirID, SecondPer: EnchantRingAir,
			CraftLevel: RingLevel, CraftXP: RingXPTenths,
			MenuInv: RingsInv, MenuSlot: RingMenuSlot,
		}
	}
	return KindSpec{
		Kind: KindNecklace, Word: "necklace", Product: "Games necklace", Plural: "Games necklaces",
		Mould: NecklaceMould, Gem: CutSapphire, Base: SapphireNecklace, Final: GamesNecklaceName,
		MouldID: NecklaceMouldID, GemID: CutSapphireID, BaseID: SapphireNecklaceID, FinalID: GamesNecklaceID,
		Spell: EnchantSpell, SpellLevel: EnchantLevel, SpellXP: EnchantXPTenths,
		Second: RuneWater, SecondID: RuneWaterID, SecondPer: 1,
		CraftLevel: NecklaceLevel, CraftXP: NecklaceXPTenths,
		MenuInv: NecklacesInv, MenuSlot: NecklaceMenuSlot,
	}
}

// The menus' components and slots (content pack/interface.pack: 4239 crafting_jewelry:necklaces_inv,
// 4233 crafting_jewelry:rings_inv; jewellery.rs2: the Sapphire necklace is slot 1 of
// crafting_make_necklaces, the Emerald ring slot 2 of crafting_make_rings).
const (
	NecklacesInv     = 4239
	NecklaceMenuSlot = 1
	RingsInv         = 4233
	RingMenuSlot     = 2
)

// String is the kind's name for a log.
func (k JewelleryKind) String() string { return k.Spec().Word }

// Kinds is both kinds, the ring first (the owner asks for rings of dueling, 7 Oct 2026).
func Kinds() []JewelleryKind { return []JewelleryKind{KindRing, KindNecklace} }

// BatchOf is the setting's batch for the kind: the Games necklaces or Rings of dueling one round makes.
func (s Settings) BatchOf(k JewelleryKind) int {
	if k == KindRing {
		return s.RingBatch
	}
	return s.JewelleryBatch
}

// KeyRingBatch is the setting that sizes a Ring of dueling batch: the rings one round makes, 0 for none.
const KeyRingBatch = "craftRingBatch"

// DefaultRingBatch is the Rings of dueling one trigger makes (a user request), the necklace batch's
// size.
const DefaultRingBatch = 50

// KeyOf is the setting that sizes the kind's batch.
func KeyOf(k JewelleryKind) string {
	if k == KindRing {
		return KeyRingBatch
	}
	return KeyJewelleryBatch
}

// As is the holding read as the kind: the gem in Sapphires, the base item in Necklaces, the enchanted
// one in Games, the mould in Mould and the spell's second rune in Water, whatever the kind. Fields the
// kind has no use for are cleared (the other kind's items are not this batch's).
func (h JewelleryHeld) As(k JewelleryKind) JewelleryHeld {
	if k == KindRing {
		return JewelleryHeld{
			Bars: h.Bars, Sapphires: h.Emeralds, Necklaces: h.EmeraldRings, Games: h.Duelings,
			Mould: h.RingMould, Cosmic: h.Cosmic, Water: h.Air, Coins: h.Coins,
		}
	}
	return JewelleryHeld{
		Bars: h.Bars, Sapphires: h.Sapphires, Necklaces: h.Necklaces, Games: h.Games,
		Mould: h.Mould, Cosmic: h.Cosmic, Water: h.Water, Coins: h.Coins,
	}
}

// Total is the holding's round items of both kinds, as one number (a ledger test: anything at all).
func (h JewelleryHeld) Total() int {
	return h.Bars + h.Sapphires + h.Necklaces + h.Games + h.Mould + h.Cosmic + h.Water + h.Coins +
		h.Emeralds + h.EmeraldRings + h.Duelings + h.RingMould + h.Air
}

// Casts is how many enchants the runes of the holding pay for, with the second rune counted as the
// kind's (a worn staff paying it: secondPaid) and the Cosmic rune always.
func (k JewelleryKind) Casts(h JewelleryHeld, secondPaid bool) int {
	n := h.Cosmic
	if !secondPaid {
		n = min(n, h.Water/max(1, k.Spec().SecondPer))
	}
	return max(0, n)
}

// DemandNames are the item names of the kind's finished jewellery, every charge.
func (k JewelleryKind) DemandNames() []string {
	if k == KindRing {
		return chargeNames("Ring of dueling")
	}
	return chargeNames("Games necklace")
}

// DemandHeld is how many of the kind's finished jewellery, of any charge, the mule's published bank
// holds.
func (k JewelleryKind) DemandHeld(stock Stock) int {
	n := 0
	for _, name := range k.DemandNames() {
		n += stockOf(stock, name)
	}
	return n
}
