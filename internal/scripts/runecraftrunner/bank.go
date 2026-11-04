package runecraftrunner

import (
	"fmt"
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// OpenBank is how a visit reaches and opens a bank: scarabrunner.OpenBank (the webwalk, then the booth)
// unless a test replaced it.
var OpenBank = fleetrunner.OpenBank

// The kit's rune objects (content pack: waterrune 555, airrune 556, lawrune 563).
const (
	WaterRuneID = 555
	AirRuneID   = 556
	LawRuneID   = 563
)

// Want is what a bank visit loads into the pack.
type Want struct {
	// Talisman is the talisman object the trip needs in the pack: kept if it is there, drawn if not. 0 banks
	// every talisman.
	Talisman int32
	// Essence draws every Rune essence the pack has room for.
	Essence bool
	// Kit draws exactly these runes (a zero field draws none): the travel teleports'.
	Kit runecraftplan.Kit
	// Coins, when above 0, is the coins the pack holds after the visit: the stack in the pack is banked with the rest and exactly this
	// many are drawn (a Nature gate trip's fare home).
	Coins int
	// Keep lists objects that stay in the pack through the visit: the Natures a gate round carries.
	Keep []int32
	// Natures, when above 0, tops the Nature runes in the pack up to this many from the bank (what the bank holds, at most): the
	// campaign's keep, carried at the gate because there is no bank on Karamja (runecraft P4). The Natures the pack holds already
	// count, so a visit with Keep: Nature never draws a second stack.
	Natures int
	// Alch, when its Qty is above 0, draws that many noted strung bows (the dearest the bank holds first) for the gate's alchemy
	// (runecraftplan/gatealch.go). They come last, in note mode, and note mode is switched off again.
	Alch AlchWant
}

// AlchWant is the bows a visit draws as notes.
type AlchWant struct {
	// Qty is how many in all; Bows are the candidates with their ids (ResolveAlchBows), dearest first. NatureFloor, when above 0, keeps
	// the draw to the Natures the pack will hold after the visit less that floor: a bow needs a Nature rune to be cast away with.
	Qty         int
	Bows        []AlchBowIDs
	NatureFloor int
}

// AlchBowIDs is one bow: its strung object and that object's note.
type AlchBowIDs struct {
	// Name is the item's key (runecraftplan.AlchBow.Key); AskAs how a mule ask names it (the per-obj name of a bow, else Name).
	Name, AskAs   string
	Strung, Noted int32
}

// ResolveAlchBows finds the ids of the bows the gate casts on, strung and as notes, in the object config. A bow the config does not
// know, or whose strung obj it cannot tell from the unstrung one, is left out.
func ResolveAlchBows(cfg *cachecfg.Config) []AlchBowIDs {
	if cfg == nil {
		return nil
	}
	var out []AlchBowIDs
	// Everything the mule hands out to alch, dearest first (a user request: "the alch list should include any other item mule1
	// hands out to alch in order"): the fleet's list, a qualified name ("Yew longbow id66") naming its obj.
	items := append([]string(nil), fleetplan.DefaultAlchItems...)
	sort.SliceStable(items, func(i, j int) bool { return fleetplan.KnownAlchValue(items[i]) > fleetplan.KnownAlchValue(items[j]) })
	for _, key := range items {
		if fleetplan.NeverAlch(key) {
			continue
		}
		b := runecraftplan.AlchBow{Name: key}
		if base, id, ok := cachecfg.SplitQualified(key); ok {
			b = runecraftplan.AlchBow{Name: base, ID: id}
		}
		name := b.Name
		var item int32
		askAs := name
		switch {
		case b.ID > 0: // the obj named (an unstrung bow)
			item, askAs = int32(b.ID), b.Key()
		default:
			if ids, err := bowstringrunner.ResolveIDs(cfg, name); err == nil && ids.Strung != 0 {
				item, askAs = ids.Strung, bowstringplan.AskName(name, int(ids.Strung)) // a strung bow
				break
			}
			for _, id := range cfg.ObjIDs(name) { // an item whose name is its own
				if o, ok := cfg.Obj(id); ok && !o.Noted() {
					item = int32(id)
					break
				}
			}
		}
		if item == 0 {
			continue
		}
		var noted int32
		if o, ok := cfg.Obj(int(item)); ok && o.Stackable {
			noted = item // a stack of its own (a javelin): no note
		}
		for _, id := range cfg.ObjIDs(name) {
			if noted != 0 {
				break
			}
			if o, ok := cfg.Obj(id); ok && o.Noted() && o.CertLink == int(item) {
				noted = int32(id)
			}
		}
		if noted == 0 {
			continue
		}
		out = append(out, AlchBowIDs{Name: b.Key(), AskAs: askAs, Strung: item, Noted: noted})
	}
	return out
}

// AlchHeld is the noted strung bows the pack holds, by bow name.
func AlchHeld(d *fleetrunner.Deps, bows []AlchBowIDs) map[string]int {
	out := map[string]int{}
	for _, b := range bows {
		if n := PackCount(d, b.Noted); n > 0 {
			out[b.Name] = n
		}
	}
	return out
}

// TradeCapacity is the essence the pack could take in one trade right now: the slots besides the ones it fills with essence, less the
// slots its other items hold (the talisman, the Natures, the fare, a stack of bows). 25 at the gate with the usual three items, 24 with a
// stack of bows to alch.
func TradeCapacity(d *fleetrunner.Deps) int {
	occupied := runecraftplan.PackSlots - PackFree(d) - Essence(d)
	return runecraftplan.GateTradeMax(occupied)
}

// AlchStack is the noted strung bows in the pack, all kinds.
func AlchStack(d *fleetrunner.Deps, bows []AlchBowIDs) int {
	n := 0
	for _, b := range bows {
		n += PackCount(d, b.Noted)
	}
	return n
}

// quickable reports whether the visit can be the one-tick quick one: nothing to draw but essence, and the
// talisman (if one is wanted) already in the pack.
func (w Want) quickable(d *fleetrunner.Deps) bool {
	if w.Kit != (runecraftplan.Kit{}) || w.Coins > 0 || len(w.Keep) > 0 || w.Natures > 0 || w.Alch.Qty > 0 {
		return false
	}
	return w.Talisman == 0 || PackCount(d, w.Talisman) > 0
}

// VisitResult is one Visit.
type VisitResult struct {
	// Opened is the bank opening; Quick that the one-tick visit did it; QuickWhy why a quick attempt fell back.
	Opened, Quick bool
	QuickWhy      string
	// Err is why the visit fell short; "" when everything wanted was loaded.
	Err string
	// Bank is what the bank held when the visit left it: Essence, Talisman by altar key and the kit's runes;
	// Rows its distinct rows.
	Essence  int
	Talisman map[string]int
	Kit      runecraftplan.Kit
	Rows     int
	// BankNatures is the Nature runes the bank held when the visit left it (after any it drew).
	BankNatures int
	// AlchDrawn is the noted bows the pack holds after the visit, BankAlch the strung bows the bank still holds, by name.
	AlchDrawn int
	BankAlch  map[string]int
	// Pack is what the pack holds after: the essence, the kit runes, the talismans.
	PackEssence int
	PackKit     runecraftplan.Kit
	Held        map[string]bool
	// Deposited is how many kinds of item were banked.
	Deposited int
	Took      time.Duration
}

// keeps reports whether id is one of the objects a visit leaves in the pack.
func keeps(ids []int32, id int32) bool {
	for _, k := range ids {
		if k == id {
			return true
		}
	}
	return false
}

// countsOf reads a bank's rows into the visit's figures.
func countsOf(items []gamestate.SlotItem) (essence int, tal map[string]int, kit runecraftplan.Kit) {
	tal = map[string]int{}
	for _, it := range items {
		n := max(1, int(it.Count))
		switch int(it.ID) {
		case runecraftplan.EssenceID:
			essence += n
		case LawRuneID:
			kit.Law += n
		case WaterRuneID:
			kit.Water += n
		case AirRuneID:
			kit.Air += n
		default:
			if a, ok := runecraftplan.ByTalisman(int(it.ID)); ok {
				tal[a.Key] += n
			}
		}
	}
	return essence, tal, kit
}

// bankCount is how many of object id a bank's rows hold.
func bankCount(items []gamestate.SlotItem, id int32) int {
	n := 0
	for _, it := range items {
		if it.ID == id {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// NatureCount is the Nature runes among a bank's rows.
func NatureCount(items []gamestate.SlotItem) int {
	n := 0
	for _, it := range items {
		if it.ID == NatureID {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// Counts reads a bank's rows into essence, talismans by altar key and the kit's runes.
func Counts(items []gamestate.SlotItem) (essence int, talisman map[string]int, kit runecraftplan.Kit) {
	return countsOf(items)
}

func packKitOf(d *fleetrunner.Deps) runecraftplan.Kit {
	return runecraftplan.Kit{Law: PackCount(d, LawRuneID), Water: PackCount(d, WaterRuneID), Air: PackCount(d, AirRuneID)}
}

// Visit banks the pack at the bank by at (everything but the wanted talisman: the crafted runes, the
// last trip's leftovers, junk) and loads the trip: the talisman if it is not in the pack, the kit's runes,
// then every essence the pack has room for. q, when not nil, makes the visit the one-tick quick one
// (mastercraftrunner.QuickBank: the booth op, the deposit, the Withdraw-All and the close in one burst)
// whenever the account stands where the bank last opened and only essence is to be drawn, and learns the
// booth for the next time at every full visit.
//
// The full visit is one fast-bank burst (actions.Bank.Visit: every deposit, then every withdrawal, five user
// events a tick, confirmed once, anything short done again the one-at-a-time way).
func Visit(d *fleetrunner.Deps, at tile.Tile, want Want, q *mastercraftrunner.QuickBank) (res VisitResult) {
	started := time.Now()
	defer func() { res.Took = time.Since(started) }()
	if q != nil && want.quickable(d) && q.Ready(d) {
		before := Essence(d)
		qv := q.Visit(d, func(id int32) bool { return want.Talisman != 0 && id == want.Talisman }, func(bank []gamestate.SlotItem) []int32 {
			if e, _, _ := countsOf(bank); want.Essence && e+before > 0 {
				return []int32{runecraftplan.EssenceID}
			}
			return nil
		})
		if qv.Done {
			res.Opened, res.Quick = true, true
			ess, tal, kit := countsOf(qv.AtOpen)
			res.Essence, res.Talisman, res.Kit, res.Rows = ess+before-Essence(d), tal, kit, len(qv.AtOpen)
			if res.Essence < 0 {
				res.Essence = 0
			}
			res.PackEssence, res.PackKit, res.Held = Essence(d), packKitOf(d), Talismans(d)
			res.Deposited = len(qv.Deposited)
			return res
		}
		res.QuickWhy = qv.Err
		q.Forget()
	}
	bank, ok := OpenBank(d, at)
	if !ok || bank == nil {
		res.Err = fmt.Sprintf("could not open the bank at %d,%d from %s", at.X, at.Z, fleetrunner.HereIs(d))
		return res
	}
	res.Opened = true
	defer func() { _ = bank.Close() }()
	q.Learn(d, bank)
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))

	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] || (want.Talisman != 0 && it.ID == want.Talisman) || keeps(want.Keep, it.ID) {
			continue
		}
		seen[it.ID] = true
		ops = append(ops, actions.DepositAllOp(it.ID))
	}
	res.Deposited = len(ops)
	if want.Talisman != 0 && PackCount(d, want.Talisman) == 0 {
		ops = append(ops, actions.WithdrawOp(want.Talisman, 1))
	}
	if want.Coins > 0 {
		ops = append(ops, actions.WithdrawOp(CoinsID, int32(want.Coins)))
	}
	for _, k := range []struct {
		id int32
		n  int
	}{{LawRuneID, want.Kit.Law}, {WaterRuneID, want.Kit.Water}, {AirRuneID, want.Kit.Air}} {
		if k.n > 0 {
			ops = append(ops, actions.WithdrawOp(k.id, int32(k.n)))
		}
	}
	natDraw := 0
	if want.Natures > 0 {
		// Counted from the bank as it stood when the visit began: the deposits do not touch the Natures (they are in Keep or are
		// drawn here), and a short draw is what the bank has.
		if n := min(want.Natures-PackCount(d, NatureID), NatureCount(bank.Items())); n > 0 {
			ops = append(ops, actions.WithdrawOp(NatureID, int32(n)))
			natDraw = n
		}
	}
	if want.Essence {
		ops = append(ops, actions.WithdrawAllOp(runecraftplan.EssenceID))
	}
	if want.Alch.Qty > 0 {
		qty := want.Alch.Qty
		if want.Alch.NatureFloor > 0 {
			qty = min(qty, max(0, PackCount(d, NatureID)+natDraw-want.Alch.NatureFloor))
		}
		inBank := func(name string) int {
			for _, b := range want.Alch.Bows {
				if b.Name == name {
					return bankCount(bank.Items(), b.Strung)
				}
			}
			return 0
		}
		// The kit's items in their own order (dearest first), each up to what the bank holds.
		var draws []AlchBowIDs
		var counts []int
		for left, i := qty, 0; left > 0 && i < len(want.Alch.Bows); i++ {
			b := want.Alch.Bows[i]
			if n := min(left, inBank(b.Name)); n > 0 {
				draws, counts = append(draws, b), append(counts, n)
				left -= n
			}
		}
		if len(draws) > 0 {
			ops = append(ops, actions.NoteModeOp(true))
			for i, b := range draws {
				ops = append(ops, actions.WithdrawAsOp(b.Strung, int32(counts[i]), b.Noted))
			}
			ops = append(ops, actions.NoteModeOp(false))
		}
	}
	out, retried := bank.Visit(ops, w, actions.BatchOptions{})
	if out.Err != nil {
		res.Err = "bank visit: " + out.Err.Error()
		return res
	}
	if len(ops) > 0 {
		logf(d, "%s", actions.VisitLog("runecraft", len(ops), out, retried))
	}
	items := out.Bank
	if len(items) == 0 {
		items = bank.Items()
	}
	res.Essence, res.Talisman, res.Kit = func() (int, map[string]int, runecraftplan.Kit) { return countsOf(items) }()
	res.Rows = len(items)
	res.BankNatures = NatureCount(items)
	if len(want.Alch.Bows) > 0 {
		res.AlchDrawn = AlchStack(d, want.Alch.Bows)
		res.BankAlch = map[string]int{}
		for _, b := range want.Alch.Bows {
			if n := bankCount(items, b.Strung); n > 0 {
				res.BankAlch[b.Name] = n
			}
		}
	}
	res.PackEssence, res.PackKit, res.Held = Essence(d), packKitOf(d), Talismans(d)
	for _, sh := range out.Short {
		// Essence the bank did not hold, a talisman it did not hold: short is not an error of the visit, it is
		// what the bank has; the caller reads the result.
		if res.Err == "" && sh.Op.Kind == actions.BankOpDeposit {
			res.Err = fmt.Sprintf("%s (%d of %d)", sh.Op, sh.Got, sh.Want)
		}
	}
	return res
}
