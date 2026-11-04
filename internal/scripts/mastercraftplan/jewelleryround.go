package mastercraftplan

// The jewellery round's order and its bank visits (jewellery.go has the rule and the content). Pure,
// like the rest of the plan, and written the way the chest round's is (chest.go NextChestStep): the
// next step is a function of where the account is and what it holds, so a step that fails is simply
// tried again from where it left the account, and a round that a restart interrupted carries on from
// whatever the pack and the bank hold.
//
// THE ORDER: ask the mule at its post; bank at the post (the notes the mule's answer arrives as turn
// into items, and the first load is drawn); then loads of
//
//	bank visit  every pack item banked, the load drawn: a Necklace mould, ten gold bars and ten
//	            Sapphires, the Cosmic and Water runes of the ten enchants, and the runes of the next
//	            teleport leg
//	to-furnace  a Falador Teleport from the post when one is castable and paid for, else the walk
//	craft       the furnace used with a gold bar, Make 10 (ten necklaces, three ticks each)
//	to-work     to the Falador west bank; the walk's poll casts Lvl-1 Enchant on the necklaces it carries as it
//	            goes, one a cast delay (jewellerywalk.go; a user request: "ring enchanting should be done
//	            while walking instead of while standing at the furnace")
//	enchant     whatever the walk left unenchanted, cast standing at the bank before the deposit, three ticks a
//	            cast (with craftWalkEnchant off, all of them, at the furnace, before the walk)
//
// until the batch is made (or the bank runs out of something); then home (the teleport, and the walk
// to Catherby) and the hand-over of the Games necklaces to the mule. A bank that holds Sapphire
// necklaces and no gold bars needs no furnace at all: the enchants are made where the account stands,
// the post's bank included, and the round never leaves it.
//
// The necklace mould is bought once (Rommik in Rimmington, 226 route cost from the Falador west bank)
// when none is in the pack or the bank, with coins drawn at a bank visit, and kept in the bank.

import (
	"sort"
	"strconv"
	"strings"
)

// JewelleryPlace is where the account is, as the round's steps see it.
type JewelleryPlace int

// The places.
const (
	// JewelleryAtOther is anywhere else: on a walk, or where a teleport landed.
	JewelleryAtOther JewelleryPlace = iota
	// JewelleryAtPost is the mule's post, with its bank (Catherby).
	JewelleryAtPost
	// JewelleryAtWorkBank is the Falador west bank.
	JewelleryAtWorkBank
	// JewelleryAtFurnace is beside the Falador furnace, a tile the engine accepts a use on.
	JewelleryAtFurnace
	// JewelleryAtShop is beside the keeper of the mould shop.
	JewelleryAtShop
)

// JewelleryStep is the next thing the round does.
type JewelleryStep string

// The steps.
const (
	// JewelleryStepAsk banks the pack at the post, reads the bank and asks the mule for the batch's
	// ingredients, the runes and the mould's coins.
	JewelleryStepAsk JewelleryStep = "ask"
	// JewelleryStepBank is a bank visit, at the post or the Falador west bank: every pack item is
	// banked and the next load drawn (PlanJewelleryDraws).
	JewelleryStepBank JewelleryStep = "bank"
	// JewelleryStepToWork goes to the Falador west bank.
	JewelleryStepToWork JewelleryStep = "to-work"
	// JewelleryStepToFurnace goes to the furnace: from the post a Falador Teleport when one is castable
	// and paid for, then the walk.
	JewelleryStepToFurnace JewelleryStep = "to-furnace"
	// JewelleryStepCraft makes the Sapphire necklaces of the pack's gold bars and Sapphires at the
	// furnace.
	JewelleryStepCraft JewelleryStep = "craft"
	// JewelleryStepEnchant casts Lvl-1 Enchant on every Sapphire necklace in the pack.
	JewelleryStepEnchant JewelleryStep = "enchant"
	// JewelleryStepShop buys a Necklace mould.
	JewelleryStepShop JewelleryStep = "shop"
	// JewelleryStepHome goes back to the mule's post: the teleport home when one is castable and paid
	// for, and the walk.
	JewelleryStepHome JewelleryStep = "home"
	// JewelleryStepHandover hands the Games necklaces owed to the mule.
	JewelleryStepHandover JewelleryStep = "handover"
	// JewelleryStepDone ends the round.
	JewelleryStepDone JewelleryStep = "done"
)

// JewelleryView is what NextJewelleryStep reads.
type JewelleryView struct {
	// Kind is what the batch makes; Pack and Bank are read As that kind (JewelleryHeld.As: the gem in
	// Sapphires, the made jewellery in Necklaces, the enchanted in Games, the second rune in Water).
	Kind  JewelleryKind
	Where JewelleryPlace
	// NearWork is whether the account stands on the Falador side of the run (the bank, the furnace
	// and the road between), wherever exactly.
	NearWork bool
	// Asked is set once the ask at the post was made this round, or none is wanted (a round that
	// carries on from the account's own bank).
	Asked bool
	// HandedOver is set once the hand-over at the end was made.
	HandedOver bool
	// Expired is the round's time used up: no more loads, home with what there is.
	Expired bool
	// Futile is set when something that should have moved the round on did not (a bank visit that
	// drew nothing for the work left, a craft or an enchant that made nothing): the round wraps up.
	Futile bool
	// ShopFailed is set when buying a mould failed: no mould is coming, so nothing is crafted.
	ShopFailed bool
	// CraftOK and EnchantOK are the levels, the members world and the spells not refused. EnchantOK
	// does not include the runes.
	CraftOK, EnchantOK bool
	// WaterPaid is whether a worn staff pays the kind's second rune (the Water rune, or the Air runes of
	// Lvl-2 Enchant).
	WaterPaid bool
	// Left is the Games necklaces the batch still has to make; Owed those made and not handed over.
	Left, Owed int
	// Pack and Bank are the round's items in each, the bank as last read (a bank visit reads it).
	Pack, Bank JewelleryHeld
	// PackOther is the pack's rows that are neither one of the round's items nor a rune or coins: the
	// notes the mule's answer arrives as, and whatever else was left.
	PackOther int
	// KeepSecond is the units of the kind's second rune the teleport home takes (JewelleryTravelRunes of
	// LegHome): they are carried, not left over.
	KeepSecond int
	// WalkEnchant is whether the walks of the round cast the enchant on the pieces they carry
	// (Settings.WalkEnchant): the pieces made at the furnace are enchanted on the way to the bank, and only
	// what is left on arrival is cast standing.
	WalkEnchant bool
}

func (v JewelleryView) mould() int { return v.Pack.Mould + v.Bank.Mould }

// walksToWorkBank is whether the account stands somewhere from which the round's next walk is the one to the
// Falador west bank: the furnace, the mould shop, or anywhere else on that side that is not a bank. A bank, and
// anywhere off the Falador side, enchants where it stands (the pieces there came from the bank itself).
func (v JewelleryView) walksToWorkBank() bool {
	switch v.Where {
	case JewelleryAtFurnace, JewelleryAtShop:
		return true
	case JewelleryAtOther:
		return v.NearWork
	}
	return false
}

// necklaces is the Sapphire necklaces already made, in the pack and the bank.
func (v JewelleryView) necklaces() int { return v.Pack.Necklaces + v.Bank.Necklaces }

// runesAnywhere is whether the pack and the bank together hold a cast's runes.
func (v JewelleryView) runesAnywhere() bool {
	return v.Kind.Casts(v.Pack.Add(v.Bank), v.WaterPaid) > 0
}

// canCraft is whether crafting is possible at all: allowed, and a mould held or still to be bought.
func (v JewelleryView) canCraft() bool {
	return v.CraftOK && (v.mould() > 0 || !v.ShopFailed)
}

// toCraft is the Sapphire necklaces still to make: what the gold bars and Sapphires (pack and bank)
// make of the batch's Left that no necklace already covers.
func (v JewelleryView) toCraft() int {
	if !v.canCraft() {
		return 0
	}
	return min(v.Pack.Pairs()+v.Bank.Pairs(), max(0, v.Left-v.necklaces()))
}

// toEnchant is the casts still to make for the batch.
func (v JewelleryView) toEnchant() int {
	if !v.EnchantOK || !v.runesAnywhere() {
		return 0
	}
	return min(v.Left, v.necklaces()+v.toCraft())
}

// needMould is whether crafting is wanted and no mould is held anywhere.
func (v JewelleryView) needMould() bool { return v.toCraft() > 0 && v.mould() == 0 }

// dirty is whether the pack holds anything the round should bank before it ends.
//
// The second rune the way home's teleport uses is not dirt: for the Ring of dueling that rune is the Air the
// enchants take, and Camelot Teleport's five Air (Varrock's three) are drawn at the visit that ends the work and
// must stay in the pack, where counting them as dirt sent the round back to the bank eleven times (live, 7 Oct
// 2026 14:42). KeepSecond is how many of it the home leg carries.
func (v JewelleryView) dirty() bool {
	p := v.Pack
	second := max(0, p.Water-v.KeepSecond)
	return p.Games+p.Necklaces+p.Bars+p.Sapphires+p.Mould+p.Cosmic+second+v.PackOther > 0
}

// Working is whether the batch has anything left to make that this account can make now: the round
// is in its loads. Otherwise it is wrapping up (banking, going home, handing over).
func (v JewelleryView) Working() bool { return !v.Expired && !v.Futile && v.toEnchant() > 0 }

// JewelleryVisitFor is the draw request of a bank visit made in view v, with bankAfter the bank as it
// stands once the pack is banked: what is left to make (nothing when wrapping up), and the runes of
// the teleport the leg after the visit can cast, if any: Falador Teleport from the post when the
// next step is the furnace, the way home from the work bank when the round is over.
func JewelleryVisitFor(v JewelleryView, bankAfter JewelleryHeld, bankOf func(item string) int, magic int, paid map[string]bool) JewelleryVisit {
	working := v.Working()
	in := JewelleryVisit{
		Kind: v.Kind, Bank: bankAfter, BankOf: bankOf, CraftOK: v.CraftOK, EnchantOK: v.EnchantOK,
		WaterPaid: v.WaterPaid, ShopOK: !v.ShopFailed,
	}
	if working {
		in.Left = v.Left
	}
	switch {
	case v.Where == JewelleryAtPost && working && v.toCraft() > 0:
		in.Travel = JewelleryTravelRunes(LegToFalador, magic, paid)
	case v.Where == JewelleryAtWorkBank && !working:
		in.Travel = JewelleryTravelRunes(LegHome, magic, paid)
	}
	return in
}

// NextJewelleryStep is the round's order: see the file comment.
func NextJewelleryStep(v JewelleryView) JewelleryStep {
	switch {
	case v.HandedOver:
		return JewelleryStepDone
	case !v.Asked:
		return JewelleryStepAsk
	}
	atBank := v.Where == JewelleryAtPost || v.Where == JewelleryAtWorkBank
	if v.Working() {
		craftNow := 0
		if v.CraftOK && v.Pack.Mould > 0 {
			craftNow = min(v.Pack.Pairs(), max(0, v.Left-v.necklaces()))
		}
		enchantNow := v.EnchantOK && v.Pack.Necklaces > 0 && v.Kind.Casts(v.Pack, v.WaterPaid) > 0
		switch {
		case v.needMould() && v.Pack.Mould == 0 && v.Pack.Coins >= MouldCoins:
			return JewelleryStepShop
		case craftNow > 0:
			if v.Where == JewelleryAtFurnace {
				return JewelleryStepCraft
			}
			return JewelleryStepToFurnace
		case enchantNow:
			// a user request: "ring enchanting should be done while walking instead of while standing at the
			// furnace". From the furnace (or anywhere else on the Falador side that is not a bank) the next walk is
			// the one to the work bank, so take it with the pieces: its poll casts the enchants, and the step at the
			// bank casts what is left, before the deposit.
			if v.WalkEnchant && v.walksToWorkBank() {
				return JewelleryStepToWork
			}
			return JewelleryStepEnchant
		case atBank:
			return JewelleryStepBank
		case v.NearWork || v.toCraft() > 0 || v.Where == JewelleryAtFurnace || v.Where == JewelleryAtShop:
			return JewelleryStepToWork
		}
		return JewelleryStepHome
	}
	// Nothing more to make: bank what the pack holds where it can, go home, hand over.
	switch v.Where {
	case JewelleryAtPost:
		switch {
		case v.dirty():
			return JewelleryStepBank
		case v.Owed > 0:
			return JewelleryStepHandover
		}
		return JewelleryStepDone
	case JewelleryAtWorkBank:
		if v.dirty() {
			return JewelleryStepBank
		}
		return JewelleryStepHome
	case JewelleryAtFurnace, JewelleryAtShop:
		if v.dirty() {
			return JewelleryStepToWork
		}
		return JewelleryStepHome
	}
	if v.NearWork && v.dirty() {
		return JewelleryStepToWork
	}
	return JewelleryStepHome
}

// ---- a bank visit ----

// JewelleryDraw is one item a bank visit draws. ID is 0 for a rune known only by name (the teleport's
// law, fire and air runes), which the caller resolves through the cache.
type JewelleryDraw struct {
	ID    int32
	Name  string
	Count int
}

// JewelleryVisit is what PlanJewelleryDraws reads: the bank as it stands once the pack is banked.
type JewelleryVisit struct {
	// Kind is what the batch makes; Bank is read As that kind.
	Kind JewelleryKind
	Bank JewelleryHeld
	// BankOf counts the bank's items by name (the teleport's runes); nil is none.
	BankOf func(item string) int
	// Left is the Games necklaces the batch still has to make (0: none, wrapping up).
	Left int
	// CraftOK and EnchantOK are the levels, the members world and the spells not refused.
	CraftOK, EnchantOK bool
	// WaterPaid is whether a worn staff pays the Water rune.
	WaterPaid bool
	// ShopOK is whether a mould may still be bought (no failed purchase this round).
	ShopOK bool
	// Travel is the runes of the next leg's teleport, by name (JewelleryTravelRunes): drawn as far as
	// the bank holds them, after the enchants' own.
	Travel map[string]int
}

// PlanJewelleryDraws is what one bank visit draws, the pack having just been banked: the next load.
//
//   - Sapphire necklaces in the bank are enchanted first (an enchant load draws as many as the pack
//     has slots for, the batch has left and the runes cover);
//   - else a crafting load: the mould, and ten gold bars with ten Sapphires (fewer when the batch or
//     the bank has fewer, or the runes cover fewer enchants), or, when no mould is held and one may be
//     bought, the coins for it instead of the mould;
//   - the runes of the load's enchants, exactly (Cosmic, and Water unless a worn staff pays it);
//   - the next leg's teleport runes, all of them or none: a teleport short of one rune is not cast, so
//     drawing the others would only cost slots.
//
// The result fits the pack (the biggest load, twenty necklaces or ten pairs, leaves room for every
// stack). A visit with nothing left to make (Left 0) draws the teleport's runes alone.
func PlanJewelleryDraws(in JewelleryVisit) []JewelleryDraw {
	b := in.Bank
	sp := in.Kind.Spec()
	runeCap := in.Kind.Casts(b, in.WaterPaid)
	left := in.Left
	if !in.EnchantOK {
		left = 0
	}
	var necklaces, bars, mould, coins int
	switch {
	case left > 0 && b.Necklaces > 0:
		necklaces = min(left, b.Necklaces, runeCap, JewelleryEnchantMaxLoad)
	case left > 0 && in.CraftOK:
		if n := min(JewelleryLoad, left, b.Pairs(), runeCap); n > 0 {
			switch {
			case b.Mould > 0:
				bars, mould = n, 1
			case in.ShopOK && b.Coins >= MouldCoins:
				bars, coins = n, MouldCoins
			}
		}
	}
	// The load fits the pack with a stack of every rune the visit can draw (it always does at these
	// sizes: this is the check that would catch a change to them).
	for 2+len(in.Travel)+boolInt(coins > 0)+boolInt(mould > 0)+necklaces+2*bars > JewelleryPackSlots && necklaces+bars > 0 {
		if necklaces > 0 {
			necklaces--
		} else {
			bars--
		}
	}
	enchants := necklaces + bars
	stack := map[string]int{}
	if enchants > 0 {
		stack[RuneCosmic] = enchants
		if !in.WaterPaid {
			stack[sp.Second] = enchants * sp.SecondPer
		}
	}
	have := func(name string) int {
		switch name {
		case sp.Second:
			return b.Water
		case RuneCosmic:
			return b.Cosmic
		}
		if in.BankOf != nil {
			return in.BankOf(name)
		}
		return 0
	}
	travel := make([]string, 0, len(in.Travel))
	covered := len(in.Travel) > 0
	for name, want := range in.Travel {
		travel = append(travel, name)
		if want <= 0 || have(name)-stack[name] < want {
			covered = false
		}
	}
	sort.Strings(travel)
	if covered {
		for _, name := range travel {
			stack[name] += in.Travel[name]
		}
	}
	var out []JewelleryDraw
	add := func(id int32, name string, n int) {
		if n > 0 {
			out = append(out, JewelleryDraw{ID: id, Name: name, Count: n})
		}
	}
	add(sp.MouldID, sp.Mould, mould)
	add(GoldBarID, GoldBar, bars)
	add(sp.GemID, sp.Gem, bars)
	add(sp.BaseID, sp.Base, necklaces)
	add(RuneCosmicID, RuneCosmic, stack[RuneCosmic])
	add(sp.SecondID, sp.Second, stack[sp.Second])
	add(CoinsID, CoinsName, coins)
	for _, name := range travel {
		if name != RuneCosmic && name != sp.Second {
			add(0, name, stack[name])
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// JewelleryLoaded is whether a draw list is a load of work, Sapphire necklaces to enchant or gold bars
// to craft, and not the teleport's runes alone: a visit that was to draw a load and drew none is
// futile (JewelleryView.Futile), whatever else it drew.
func JewelleryLoaded(draws []JewelleryDraw) bool {
	for _, d := range draws {
		if d.Count > 0 && (d.ID == GoldBarID || d.ID == SapphireNecklaceID || d.ID == EmeraldRingID) {
			return true
		}
	}
	return false
}

// JewelleryDrawCounts is a draw list as counts by item name, for a log.
func JewelleryDrawCounts(draws []JewelleryDraw) string {
	parts := make([]string, 0, len(draws))
	for _, d := range draws {
		parts = append(parts, strconv.Itoa(d.Count)+" "+d.Name)
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// ---- what the server says ----

// EnchantRefusal reads the cast's chat for content's refusals (magic.rs2 [proc,check_spell_requirements]
// and enchant.rs2): "level", "runes", "members" or "item"; "" for none.
func EnchantRefusal(said string) string {
	switch {
	case strings.Contains(said, "Your Magic level is not high enough"):
		return "level"
	case strings.Contains(said, "You do not have enough") && strings.Contains(said, "Runes to cast this spell"):
		return "runes"
	case strings.Contains(said, "members' server"):
		return "members"
	case strings.Contains(said, "This spell can only be cast on"):
		return "item"
	}
	return ""
}

// NecklaceRefusal reads the chat after a use of a gold bar on the furnace for the refusals that will
// not pass by themselves: "level" (jewellery.rs2 craft_gold: the Crafting level, a message box) and
// "reach" (the engine's own "I can't reach that!"); "" for none.
func NecklaceRefusal(said string) string {
	switch {
	case strings.Contains(said, "You need at least level") && strings.Contains(said, "Crafting"):
		return "level"
	case strings.Contains(said, "I can't reach that"):
		return "reach"
	}
	return ""
}
