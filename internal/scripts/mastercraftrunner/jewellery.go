package mastercraftrunner

// The jewellery round's I/O (mastercraftplan/jewellery.go decides it and has the content facts, the
// sources of every id below). Four things are new here and nothing else:
//
//   - JewelleryKit: one bank visit that banks the whole pack and draws the next load, the way the
//     chest round's ChestKit does (every deposit in one batch, every withdrawal in another);
//   - FindFurnace, ApproachFurnace and MakeNecklaces: the furnace found BY NAME and option through the
//     loc index (never an id at a coordinate), a tile the engine accepts a use on (internal/locreach),
//     a gold bar used on it (OPLOCU: the furnace's Smelt option opens the smelting menu, not this
//     one), and Make 5 or Make 10 on the necklaces menu it opens;
//   - CastEnchant: Lvl-1 Enchant on every Sapphire necklace in the pack at the spell's own pace
//     (the old superheat cast loop's), the cast measured by the Games necklaces that arrive;
//   - BuyMould: a Necklace mould from the keeper of a crafting shop (combatrunner.BuyFrom).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The jewellery interface's ids (content pack/interface.pack: 4161=crafting_jewelry and
// 4239=crafting_jewelry:necklaces_inv), the slot of the Sapphire necklace in the necklaces menu
// (crafting_make_necklaces slot 1; slot 0 is the plain gold necklace), the crafting stat's index
// and the furnace's name and option (a build with several locs named "Furnace": only some carry the
// Smelt option, and only those answer a use).
const (
	JewelleryInterface = 4161
	NecklacesInv       = mastercraftplan.NecklacesInv
	MakeMenuSlot       = mastercraftplan.NecklaceMenuSlot
	CraftingStat       = 12
	FurnaceName        = "Furnace"
	FurnaceOp          = "Smelt"
)

// JewelleryOwn reads the round's items out of a bank reading, unnoted objects only.
func JewelleryOwn(banked []gamestate.SlotItem) mastercraftplan.JewelleryHeld {
	return mastercraftplan.JewelleryHeldIn(func(id int32) int { return int(actions.CountByID(banked, id)) })
}

// JewelleryPack reads the round's items out of the pack, unnoted objects only.
func JewelleryPack(d *fleetrunner.Deps) mastercraftplan.JewelleryHeld {
	return mastercraftplan.JewelleryHeldIn(func(id int32) int { return packCount(d, id) })
}

// JewelleryPackOther is the pack's rows that are neither one of the Games necklace round's items nor a
// rune nor coins: the notes the mule's answer arrives as, and whatever else was left.
func JewelleryPackOther(d *fleetrunner.Deps) int {
	return JewelleryPackOtherFor(d, mastercraftplan.KindNecklace)
}

// JewelleryPackOtherFor is JewelleryPackOther for a kind: the other kind's items count as other.
func JewelleryPackOtherFor(d *fleetrunner.Deps, kind mastercraftplan.JewelleryKind) int {
	sp := kind.Spec()
	known := map[int32]bool{
		mastercraftplan.GoldBarID: true, sp.GemID: true, sp.BaseID: true,
		sp.FinalID: true, sp.MouldID: true, mastercraftplan.CoinsID: true,
	}
	n := 0
	for _, it := range packItems(d) {
		if known[it.ID] {
			continue
		}
		if d.Config != nil {
			if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && strings.HasSuffix(strings.ToLower(o.Name), " rune") {
				continue
			}
		}
		n++
	}
	return n
}

// ---- the kit ----

// JewelleryKitResult is what a kit visit did.
type JewelleryKitResult struct {
	// Banked is the bank as it was read after the deposit; Drew what was drawn, by item name, as the
	// pack showed it.
	Banked []gamestate.SlotItem
	Drew   map[string]int
	// Took is the whole visit, from the open bank to the closed one.
	Took time.Duration
	Err  string
}

// JewelleryKit banks the whole pack, in an open bank, and draws what plan names from the bank as it
// then stands (draws with an ID: the caller has resolved a rune known by name), then closes it. Every
// deposit goes out in one batch and every withdrawal in another (actions.Bank.Batch); a withdrawal
// the batch did not confirm is made again one at a time.
func JewelleryKit(d *fleetrunner.Deps, bank *actions.Bank, plan func(banked []gamestate.SlotItem) []mastercraftplan.JewelleryDraw) (out JewelleryKitResult) {
	start := time.Now()
	out.Drew = map[string]int{}
	defer func() { out.Took = time.Since(start) }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	st := d.Session.State()
	if err := bank.SetNoteMode(false, w); err != nil {
		out.Err = "item mode: " + err.Error()
		return out
	}
	packBefore := len(bank.Inventory())
	bankGen := invGen(st, actions.BankWithdraw)
	if err := bank.BatchDepositInventory(w); err != nil {
		out.Err = "deposit: " + err.Error()
	}
	if packBefore > 0 {
		waitUntil(d, func() bool { return invGen(st, actions.BankWithdraw) > bankGen }, bankSettle)
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	var draws []mastercraftplan.JewelleryDraw
	for _, dr := range plan(out.Banked) {
		if dr.ID != 0 && dr.Count > 0 {
			draws = append(draws, dr)
		}
	}
	ops := make([]actions.BankOp, 0, len(draws))
	for _, dr := range draws {
		ops = append(ops, actions.WithdrawOp(dr.ID, int32(dr.Count)))
	}
	if len(ops) > 0 {
		res := bank.Batch(ops, w, actions.BatchOptions{})
		if res.Err != nil {
			out.Err = "withdraw: " + res.Err.Error()
		}
		for _, sh := range res.Short {
			if sh.Op.Kind != actions.BankOpWithdraw || sh.Why == "the bank holds none" || sh.Why == "the pack filled" {
				continue
			}
			if rest := int32(sh.Want - sh.Got); rest > 0 {
				if err := bank.WithdrawX(sh.Op.ID, rest, w); err != nil {
					logf(d, "mastercrafter: jewellery: withdrawing %d of item %d: %v", rest, sh.Op.ID, err)
				}
			}
		}
	}
	for _, dr := range draws {
		if n := packCount(d, dr.ID); n > 0 {
			out.Drew[dr.Name] += n
		}
	}
	_ = bank.Close()
	waitUntil(d, func() bool { return !bank.IsOpen() }, time.Second)
	return out
}

// ---- the furnace ----

// WalkFunc is the walk the furnace approach uses (the round's wolf-free WalkTo).
type WalkFunc func(dest tile.Tile, radius int, timeout time.Duration) bool

// FindFurnace is the nearest loc named FurnaceName with the Smelt option within radius of near.
func FindFurnace(d *fleetrunner.Deps, near tile.Tile, radius int) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Nearest(d.Config, locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: radius, Name: FurnaceName, Op: FurnaceOp}, live)
}

// furnaceLoc is the locreach model of a found furnace.
func furnaceLoc(d *fleetrunner.Deps, furn locindex.Match) (locreach.Loc, bool) {
	typ, ok := d.Config.Loc(furn.ID)
	if !ok {
		return locreach.Loc{}, false
	}
	return locreach.Loc{
		X: furn.X, Z: furn.Z, Level: furn.Level, Width: typ.Width, Length: typ.Length,
		Angle: furn.Angle, Shape: furn.Shape, ForceApproach: typ.ForceApproach,
	}, true
}

// FurnaceStands is the tiles the engine accepts a use on the furnace from, nearest to from first, the
// furnace's own footprint (solid) left out.
func FurnaceStands(d *fleetrunner.Deps, furn locindex.Match, from tile.Tile) []tile.Tile {
	l, ok := furnaceLoc(d, furn)
	if !ok {
		return nil
	}
	fx, fz, fw, fh := l.Footprint()
	var out []tile.Tile
	for _, t := range locreach.Tiles(l) {
		if t.X >= fx && t.X < fx+fw && t.Z >= fz && t.Z < fz+fh {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return tile.Chebyshev(out[i], from) < tile.Chebyshev(out[j], from) })
	return out
}

// FurnaceReached is whether the account stands where the engine accepts a use on the furnace.
func FurnaceReached(d *fleetrunner.Deps, furn locindex.Match) bool {
	l, ok := furnaceLoc(d, furn)
	if !ok {
		return false
	}
	x, z, level, ok := d.Session.State().Tile()
	if !ok || level != furn.Level {
		return false
	}
	reached, modelled := locreach.Reached(x, z, l)
	return modelled && reached
}

// ApproachFurnace walks the account onto a tile the engine accepts a use on the furnace from, the
// nearest one first (the engine runs a loc's [oplocu] trigger only from such a tile and, with
// clientRoutefinder on, never walks the account into position: internal/locreach has the live
// transcript). first is the time the walk to the nearest has; any other tile gets forty seconds. It
// reports whether the account got there.
func ApproachFurnace(d *fleetrunner.Deps, furn locindex.Match, walk WalkFunc, first time.Duration) bool {
	if FurnaceReached(d, furn) {
		return true
	}
	here := tile.Tile{}
	if x, z, level, ok := d.Session.State().Tile(); ok {
		here = tile.Tile{X: x, Z: z, Level: level}
	}
	stands := FurnaceStands(d, furn, here)
	if len(stands) == 0 {
		// No model of it: the furnace's own tile, close enough for the engine's own approach.
		return walk(tile.Tile{X: furn.X, Z: furn.Z, Level: furn.Level}, 2, first)
	}
	for i, to := range stands {
		timeout := first
		if i > 0 {
			timeout = 40 * time.Second
		}
		if walk(to, 0, timeout) {
			return true
		}
		if i >= 3 {
			break
		}
	}
	logf(d, "mastercrafter: jewellery: could not stand on any tile the engine accepts a use on the furnace at %d,%d from (at %s)", furn.X, furn.Z, fleetrunner.HereIs(d))
	return false
}

// necklaceMenu reads the open necklaces menu: the object in the Sapphire necklace's slot, ok false
// while the jewellery interface is not the open main modal or its inventory has not arrived or holds
// nothing there (no Sapphire, or no mould, in the pack: the interface then shows no necklace).
func necklaceMenu(d *fleetrunner.Deps) (obj int32, ok bool) {
	return jewelleryMenu(d, mastercraftplan.KindNecklace)
}

// jewelleryMenu is the open make menu's object in the kind's slot (the Sapphire necklace's in the
// necklaces menu, the Emerald ring's in the rings menu).
func jewelleryMenu(d *fleetrunner.Deps, kind mastercraftplan.JewelleryKind) (obj int32, ok bool) {
	sp := kind.Spec()
	st := d.Session.State()
	if main, _, _ := st.Modals(); main != JewelleryInterface {
		return 0, false
	}
	inv := st.Inventory(sp.MenuInv)
	if inv == nil {
		return 0, false
	}
	for _, it := range inv.NonEmpty() {
		if it.Slot == sp.MenuSlot {
			return it.ID, true
		}
	}
	return 0, false
}

// dismissChat presses "Click here to continue" on a chat dialog left open while no random event is on
// (a level-up's congratulations stops a use, and a short Make's message box too).
func dismissChat(d *fleetrunner.Deps) bool {
	if d.Components == nil || randomevents.Busy(d.Session) {
		return false
	}
	cd := actions.NewChatDialog(d.Session, d.Components)
	if !cd.IsOpen() || !cd.CanContinue() {
		return false
	}
	if err := cd.Continue(); err != nil {
		return false
	}
	waitUntil(d, func() bool { return !cd.IsOpen() || !cd.CanContinue() }, 1500*time.Millisecond)
	return true
}

// NecklaceResult is one MakeNecklaces.
type NecklaceResult struct {
	// Made is the Sapphire necklaces the pack gained, Uses the interface uses (a gold bar used on the
	// furnace and a Make button), XP the Crafting XP gained in displayed points, Ticks the server
	// ticks it spanned.
	Made, Uses, XP, Ticks int
	// Kind is what was made (the zero value, Sapphire necklaces, or Emerald rings): the log's words.
	Kind mastercraftplan.JewelleryKind
	// Reason is "done" (no gold bar or Sapphire left), "no-mould", "no-menu" (the interface would not
	// open, or showed no necklace), "reach" (the engine would not take the use from where the
	// account stands), "stalled", "held" or "stopped".
	Reason string
}

// Describe is a one-line account of a MakeNecklaces for a log.
func (r NecklaceResult) Describe() string {
	return fmt.Sprintf("%d %s(s) from %d use(s) of the furnace over %d tick(s), +%d Crafting XP (%s)", r.Made, r.Kind.Spec().Base, r.Uses, r.Ticks, r.XP, r.Reason)
}

// makeButton is the Make button for n pairs left: Make 10 for ten or more, Make 5 for five to nine,
// else Make 1, and the count it makes. A count that is used up ends silently where a count the bars
// or Sapphires run short of ends in a message box.
func makeButton(n int) (option, per int) {
	switch {
	case n >= mastercraftplan.MakeTen:
		return 3, mastercraftplan.MakeTen
	case n >= mastercraftplan.MakeFive:
		return 2, mastercraftplan.MakeFive
	}
	return 1, mastercraftplan.MakeOne
}

// The pace of a Make: a necklace every NecklaceTicks, the first after the same, and a margin.
const (
	necklaceTick   = 600 * time.Millisecond
	necklaceMargin = 5 * time.Second
	// necklaceStall is how long a Make may go without a new necklace before it is taken as stopped.
	necklaceStall = 8 * time.Second
	// menuWait is how long the interface has to open after a gold bar is used on the furnace.
	menuWait  = 3 * time.Second
	menuTries = 3
)

// MakeNecklaces uses the pack's gold bars on the furnace and takes the Make button that finishes the
// load, over and over, until the pack holds no gold bar or no Sapphire. The account must stand where
// the engine accepts a use (ApproachFurnace). hold runs before every use.
func MakeNecklaces(d *fleetrunner.Deps, furn locindex.Match, hold func() bool) (res NecklaceResult) {
	return MakeJewellery(d, mastercraftplan.KindNecklace, furn, hold)
}

// MakeJewellery is MakeNecklaces for a kind: with a Ring mould and cut Emeralds it makes Emerald rings
// from the rings menu (jewellery.rs2 craft_gold is the same label, a ring every third tick).
func MakeJewellery(d *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, furn locindex.Match, hold func() bool) (res NecklaceResult) {
	sp := kind.Spec()
	res.Kind = kind
	st := d.Session.State()
	made0 := packCount(d, sp.BaseID)
	_, xp0 := statOf(d, CraftingStat)
	tick0 := st.Tick()
	defer func() {
		res.Made = max(0, packCount(d, sp.BaseID)-made0)
		_, xp := statOf(d, CraftingStat)
		res.XP, res.Ticks = xp-xp0, st.Tick()-tick0
	}()
	for {
		if hold != nil && !hold() {
			res.Reason = "held"
			return res
		}
		bars, gems := packCount(d, mastercraftplan.GoldBarID), packCount(d, sp.GemID)
		switch {
		case packCount(d, sp.MouldID) == 0:
			res.Reason = "no-mould"
			return res
		case bars == 0 || gems == 0:
			res.Reason = "done"
			return res
		}
		option, per := makeButton(min(bars, gems))
		dismissChat(d)
		// The gold bar used on the furnace opens the menu; a use read while the last Make's delay is
		// running is dropped without a word, so it is sent again.
		var obj int32
		opened := false
		for try := 0; try < menuTries && !opened; try++ {
			if try > 0 && !d.Delay(900*time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
			slot := -1
			for _, it := range packItems(d) {
				if it.ID == mastercraftplan.GoldBarID {
					slot = it.Slot
					break
				}
			}
			if slot < 0 {
				res.Reason = "done"
				return res
			}
			at := time.Now()
			if err := actions.OpLocU(d.Session, furn.X, furn.Z, furn.ID, mastercraftplan.GoldBarID, slot, fleetrunner.PlayerPack); err != nil {
				logf(d, "mastercrafter: jewellery: OpLocU(gold bar on the furnace): %v", err)
				res.Reason = "stalled"
				return res
			}
			res.Uses++
			// The menu, or the engine's answer that it will not take the use from here.
			waitUntil(d, func() bool {
				_, ok := jewelleryMenu(d, kind)
				return ok || mastercraftplan.NecklaceRefusal(d.Chat.Since(at)) != ""
			}, menuWait)
			if o, ok := jewelleryMenu(d, kind); ok {
				obj, opened = o, true
			} else if mastercraftplan.NecklaceRefusal(d.Chat.Since(at)) == "reach" {
				res.Reason = "reach"
				return res
			}
		}
		if !opened {
			res.Reason = "no-menu"
			return res
		}
		before := packCount(d, sp.BaseID)
		if err := actions.InvButton(d.Session, option, obj, sp.MenuSlot, sp.MenuInv); err != nil {
			logf(d, "mastercrafter: jewellery: Make %d: %v", per, err)
			res.Reason = "stalled"
			return res
		}
		// The necklaces arrive one every third tick; the Make is over when all of them have, or when no
		// new one has come for a while.
		deadline := time.Now().Add(time.Duration(per+2)*time.Duration(mastercraftplan.NecklaceTicks)*necklaceTick + necklaceMargin)
		last, lastAt := before, time.Now()
		for time.Now().Before(deadline) {
			if now := packCount(d, sp.BaseID); now > last {
				last, lastAt = now, time.Now()
			}
			if last-before >= per || time.Since(lastAt) > necklaceStall {
				break
			}
			if !d.Delay(quickPoll) {
				res.Reason = "stopped"
				return res
			}
		}
		if last == before {
			res.Reason = "stalled"
			return res
		}
		// The delay that follows a necklace is over a tick after the last one landed.
		if !d.Delay(650 * time.Millisecond) {
			res.Reason = "stopped"
			return res
		}
	}
}

// ---- the enchant ----

// EnchantResult is one CastEnchant.
type EnchantResult struct {
	// Casts is the casts that landed (the Games necklaces rising), Sent the casts sent, XP the Magic XP
	// gained in displayed points, Ticks the server ticks it spanned.
	Casts, Sent, XP, Ticks int
	// Kind is what was enchanted (Games necklaces, or Rings of dueling): the log's words.
	Kind mastercraftplan.JewelleryKind
	// Reason is "done" (no Sapphire necklace left), "level", "runes", "members", "item", "stalled",
	// "held" or "stopped".
	Reason string
}

// Describe is a one-line account of a CastEnchant for a log.
func (r EnchantResult) Describe() string {
	return fmt.Sprintf("%d %s(s) from %d cast(s) over %d tick(s), +%d Magic XP (%s)", r.Casts, r.Kind.Spec().Product, r.Sent, r.Ticks, r.XP, r.Reason)
}

// enchantLand is how long a cast has to show its Games necklace before it is taken as dropped.
const enchantLand = 4 * 600 * time.Millisecond

// CastEnchant casts Lvl-1 Enchant on the pack's Sapphire necklaces one cast at a time, at the spell's
// own pace (the spell's p_delay(1) after each, as superheat's, and the engine dropping a cast it reads while the
// player is delayed): a cast, its Games necklace seen in the pack, then the next sent once the client
// has seen two more server ticks, so it is read on the third, the first tick the engine takes one. A
// cast that does not land is sent again a tick later and the gap is widened by a tick for the rest of
// the run. hold runs before every cast.
func CastEnchant(d *fleetrunner.Deps, hold func() bool) (res EnchantResult) {
	return CastEnchantKind(d, mastercraftplan.KindNecklace, hold)
}

// CastEnchantKind is CastEnchant for a kind: Lvl-2 Enchant on the pack's Emerald rings, a Ring of
// dueling(8) for each, at the same pace (the same enchant.rs2 label and p_delay(1)).
func CastEnchantKind(d *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, hold func() bool) (res EnchantResult) {
	sp := kind.Spec()
	res.Kind = kind
	st := d.Session.State()
	games0 := packCount(d, sp.FinalID)
	_, xp0 := statOf(d, MagicStat)
	tick0 := st.Tick()
	at := time.Now()
	defer func() {
		res.Casts = max(0, packCount(d, sp.FinalID)-games0)
		_, xp := statOf(d, MagicStat)
		res.XP, res.Ticks = xp-xp0, st.Tick()-tick0
	}()
	gap := mastercraftplan.EnchantTicks - 1
	misses := 0
	landedAt := -1
	for {
		if hold != nil && !hold() {
			res.Reason = "held"
			return res
		}
		if why := mastercraftplan.EnchantRefusal(d.Chat.Since(at)); why != "" {
			res.Reason = why
			return res
		}
		// A level-up's dialog (17.5 XP a cast: a level now and then) left open makes the engine refuse a
		// cast without a word (a script that needs protected access): close it.
		dismissChat(d)
		slot := -1
		for _, it := range packItems(d) {
			if it.ID == sp.BaseID {
				slot = it.Slot
				break
			}
		}
		if slot < 0 {
			res.Reason = "done"
			return res
		}
		if landedAt >= 0 {
			wait := time.Now().Add(time.Duration(gap) * 650 * time.Millisecond)
			for st.Tick() < landedAt+gap && time.Now().Before(wait) {
				if !d.Delay(10 * time.Millisecond) {
					res.Reason = "stopped"
					return res
				}
			}
		}
		before := packCount(d, sp.FinalID)
		if err := actions.CastOnItem(d.Session, sp.Spell, sp.BaseID, slot, fleetrunner.PlayerPack); err != nil {
			logf(d, "mastercrafter: jewellery: enchant: %v", err)
			res.Reason = "stalled"
			return res
		}
		res.Sent++
		deadline := time.Now().Add(enchantLand)
		for packCount(d, sp.FinalID) <= before && time.Now().Before(deadline) {
			// A refused cast says so within a tick: no need to wait out the whole landing time.
			if why := mastercraftplan.EnchantRefusal(d.Chat.Since(at)); why != "" {
				res.Reason = why
				return res
			}
			if !d.Delay(10 * time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
		}
		if packCount(d, sp.FinalID) > before {
			landedAt = st.Tick()
			misses = 0
			continue
		}
		if misses++; misses >= 3 {
			if why := mastercraftplan.EnchantRefusal(d.Chat.Since(at)); why != "" {
				res.Reason = why
			} else {
				res.Reason = "stalled"
			}
			return res
		}
		gap++
		landedAt = st.Tick()
	}
}

// ---- the mould ----

// BuyMould buys a Necklace mould, with the coins in the pack, from the keeper of the shop: the
// walk to its stand, the Trade, the buy, confirmed against the pack. It returns why when none was
// bought.
func BuyMould(d *fleetrunner.Deps, shop mastercraftplan.MouldShop) (bought bool, why string) {
	return BuyMouldFor(d, mastercraftplan.KindNecklace, shop)
}

// BuyMouldFor is BuyMould for a kind: the same keepers sell the Ring mould (rimmington.inv and
// alkharid.inv, stock2=ring_mould,4,100).
func BuyMouldFor(d *fleetrunner.Deps, kind mastercraftplan.JewelleryKind, shop mastercraftplan.MouldShop) (bought bool, why string) {
	site := combatplan.ShopSite{Shop: shop.Name, Keeper: shop.Keeper, Stand: shop.Stand}
	res, err := combatrunner.BuyFrom(fleetrunner.Combat(d), site, kind.Spec().Mould, 1)
	if err == nil && res.Bought > 0 {
		return true, ""
	}
	var parts []string
	if err != nil {
		parts = append(parts, err.Error())
	}
	if res.Said != "" {
		parts = append(parts, shop.Keeper+": "+res.Said)
	}
	if len(parts) == 0 {
		parts = append(parts, shop.Keeper+" sold none")
	}
	return false, strings.Join(parts, "; ")
}
