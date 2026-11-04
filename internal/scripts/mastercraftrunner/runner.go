// Package mastercraftrunner is the I/O half of MasterCrafter's gem round: the bank visits at the
// mule's post (deposit everything, read the bank, draw a chisel and a load of the best uncut gem),
// the cut itself (gemrunner.CutAll, five a tick), and drawing the products out as bank notes for
// the hand-over to the mule. The decisions are internal/scripts/mastercraftplan's; the bank, the
// walk and the trade are the ones ScarabSwarm, BowStringer and RuneRunner already proved
// (scarabrunner.OpenBank, runerunner.Deliver).
//
// Everything is by object id, never by name: strung and unstrung bows share a name, and so does
// every item with its bank note.
package mastercraftrunner

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// NoteOfID, when set, is the note of an item for DrawProducts instead of the object config's (a test whose
// simulator carries its own note rule and no config sets it).
var NoteOfID func(id int32) int32

// OpenBank is how a visit reaches and opens the bank at the mule's post: scarabrunner.OpenBank (the
// walk on the wolf- and bear-avoiding webwalk, then the booth) unless a test replaced it.
var OpenBank = fleetrunner.OpenBank

func logf(d *fleetrunner.Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// GemDeps adapts d to gemrunner's Deps, for the cut.
func GemDeps(d *fleetrunner.Deps) *gemrunner.Deps {
	return &gemrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: gemrunner.Delayer(d.Delay),
		Log: d.Log, Walker: d.Walker,
	}
}

// Visit is what one bank visit saw.
type Visit struct {
	// Opened is false when the bank could not be reached or opened.
	Opened bool
	Err    string
	// Banked is the bank's rows after the deposit (a copy: the bank closes with the visit).
	Banked []gamestate.SlotItem
}

// Count is how many of object id the bank held after the deposit.
func (v Visit) Count(id int32) int { return int(actions.CountByID(v.Banked, id)) }

// UncutCounts is the bank's uncut gems by gem key.
func UncutCounts(banked []gamestate.SlotItem) map[string]int {
	out := map[string]int{}
	for _, g := range gemplan.Gems {
		if n := int(actions.CountByID(banked, g.UncutID)); n > 0 {
			out[g.Key] = n
		}
	}
	return out
}

// CutGems is how many cut gems of every kind the bank held, and Crushed the crushed gemstones.
func CutGems(banked []gamestate.SlotItem) (cut, crushed int) {
	for _, g := range gemplan.Gems {
		cut += int(actions.CountByID(banked, g.CutID))
	}
	return cut, int(actions.CountByID(banked, gemplan.CrushedGemstoneID))
}

// GemVisit is a bank visit of a gem round.
type GemVisit struct {
	Visit
	// ChiselHeld is a chisel in the pack or the bank.
	ChiselHeld bool
	// Gem is the kind drawn and Drawn how many uncut ones are now in the pack (0: none drawn).
	Gem   gemplan.Gem
	Drawn int
	// NoChisel is set when a load was to be drawn and no chisel is held anywhere: nothing can be
	// cut until one is asked for.
	NoChisel bool
}

// GemBankVisit opens the bank at post, deposits the whole pack but the chisel (cut gems, crushed
// gemstones, notes the kit left: a bank takes a note back as the item), reads the bank, and, when
// pick names an uncut kind the bank holds, draws a chisel (if none is carried) and a load of that
// kind. pick nil only reads. The pack is then ready for gemrunner.CutAll.
func GemBankVisit(d *fleetrunner.Deps, post tile.Tile, pick func(uncut map[string]int) (gemplan.Gem, bool)) GemVisit {
	return GemBankVisitLearning(d, post, pick, nil)
}

// GemBankVisitLearning is GemBankVisit that teaches q, while the bank is open, what a quick visit
// needs (QuickBank.Learn).
func GemBankVisitLearning(d *fleetrunner.Deps, post tile.Tile, pick func(uncut map[string]int) (gemplan.Gem, bool), q *QuickBank) GemVisit {
	var v GemVisit
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		v.Err = fmt.Sprintf("could not open the bank at %d,%d", post.X, post.Z)
		return v
	}
	v.Opened = true
	defer func() { _ = bank.Close() }()
	q.Learn(d, bank)
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if len(bank.Inventory()) > 0 {
		keepChisel := func(it gamestate.SlotItem) bool { return it.ID != gemplan.ChiselID }
		if err := bank.BatchDepositAllMatching(keepChisel, w); err != nil {
			v.Err = "deposit: " + err.Error()
			return v
		}
	}
	v.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	haveChisel := bowstringrunner.PackCount(d, gemplan.ChiselID) > 0
	v.ChiselHeld = haveChisel || v.Count(gemplan.ChiselID) > 0
	if pick == nil {
		return v
	}
	gem, ok := pick(UncutCounts(v.Banked))
	if !ok {
		return v
	}
	if err := bank.SetNoteMode(false, w); err != nil {
		v.Err = "note mode off: " + err.Error()
		return v
	}
	if !haveChisel {
		if v.Count(gemplan.ChiselID) == 0 {
			v.Err, v.NoChisel = "no chisel in the pack or the bank", true
			return v
		}
		if err := bank.WithdrawOne(gemplan.ChiselID); err != nil {
			v.Err = "withdraw chisel: " + err.Error()
			return v
		}
	}
	n := min(mastercraftplan.GemLoad(), v.Count(gem.UncutID))
	if err := bank.WithdrawX(gem.UncutID, int32(n), w); err != nil {
		v.Err = "withdraw " + gem.UncutName + ": " + err.Error()
	}
	v.Gem, v.Drawn = gem, bowstringrunner.PackCount(d, gem.UncutID)
	return v
}

// Product is one thing handed to the mule: the object id it sits in the bank under and the name
// the trade screen shows it by.
type Product struct {
	ID   int32
	Name string
}

// GemProducts are the cut gems of every kind and the crushed gemstone.
func GemProducts() []Product {
	out := make([]Product, 0, len(gemplan.Gems)+1)
	for _, g := range gemplan.Gems {
		out = append(out, Product{ID: g.CutID, Name: g.Name})
	}
	return append(out, Product{ID: gemplan.CrushedGemstoneID, Name: gemplan.CrushedGemstone})
}

// ResolveBowIDs is bowstringrunner.ResolveIDs unless a test replaced it.
var ResolveBowIDs = bowstringrunner.ResolveIDs

// BowProducts are the strung bows of the named bows. A bow whose ids cannot be told apart is
// left out (and said so).
func BowProducts(d *fleetrunner.Deps, names []string) []Product {
	var out []Product
	for _, n := range names {
		ids, err := ResolveBowIDs(d.Config, n)
		if err != nil {
			logf(d, "mastercrafter: %s: %v — its strung bows cannot be handed over", n, err)
			continue
		}
		out = append(out, Product{ID: ids.Strung, Name: n})
	}
	return out
}

// Names is the distinct product names, in order.
func Names(products []Product) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range products {
		k := strings.ToLower(p.Name)
		if !seen[k] {
			seen[k] = true
			out = append(out, p.Name)
		}
	}
	return out
}

// Haul is what DrawProducts took out of the bank.
type Haul struct {
	Visit
	// Rows is how many product rows were drawn, Slots the pack slots they filled, Got the units
	// that arrived per product name.
	Rows, Slots int
	Got         map[string]int
}

// DrawProducts opens the bank at post, deposits the pack, switches withdrawals to notes (one pack
// slot per product however many there are: 500 cut gems cross as one stack) and draws every
// product the bank holds, by id. Notes are what RuneRunner's and the fishers' hand-overs cross
// too, and the mule banks them. It leaves the bank in item mode.
func DrawProducts(d *fleetrunner.Deps, post tile.Tile, products []Product) Haul {
	out := Haul{Got: map[string]int{}}
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		out.Err = fmt.Sprintf("could not open the bank at %d,%d", post.X, post.Z)
		return out
	}
	out.Opened = true
	defer func() { _ = bank.Close() }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if len(bank.Inventory()) > 0 {
		// One burst, a Deposit-All per kind five a tick, confirmed once (twopertick-1012; it was a click and a wait a kind).
		if err := bank.BatchDepositInventory(w); err != nil {
			out.Err = "deposit: " + err.Error()
			return out
		}
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	// EVERY PRODUCT IN ONE BURST (twopertick-1012): note mode on, a Withdraw-All per product the bank holds and note
	// mode off go out together, five user events a tick, and are confirmed once. It was a note-mode switch that waited
	// on the varp, a Withdraw-All and a poll at 150 ms for each of up to a dozen products, and a switch back.
	var ids []int32
	var drawn []Product
	for _, p := range products {
		if out.Count(p.ID) > 0 {
			ids = append(ids, p.ID)
			drawn = append(drawn, p)
		}
	}
	slotsBefore := packRows(d)
	noteOf := actions.NoteResolver(d.Config)
	if NoteOfID != nil {
		noteOf = NoteOfID
	}
	noteDraw, asked := bank.DrawAllAsNotes(ids, noteOf, actions.BackpackCapacity-slotsBefore, w)
	if asked > 0 {
		logf(d, "mastercrafter: %s", actions.VisitLog("hand-over note draw", asked+2, noteDraw, 0))
	}
	// The pack was emptied above, so what a draw added is read off the pack: the units of the product's note (a note
	// is one stack of n) and the slots it takes.
	for _, p := range drawn {
		if n := int(actions.CountByID(packItems(d), noteOf(p.ID))); n > 0 {
			out.Got[p.Name] += n
			out.Rows++
		}
	}
	out.Slots = packRows(d) - slotsBefore
	return out
}

// packUnits is every unit in the pack, a stack counted at its size; packRows the slots in use.
func packUnits(d *fleetrunner.Deps) int {
	n := 0
	for _, it := range packItems(d) {
		n += max(1, int(it.Count))
	}
	return n
}

func packRows(d *fleetrunner.Deps) int { return len(packItems(d)) }

func packItems(d *fleetrunner.Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// DepositAll banks the whole pack at post (notes go in as the item) and reports the bank.
func DepositAll(d *fleetrunner.Deps, post tile.Tile) Visit {
	var v Visit
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		v.Err = fmt.Sprintf("could not open the bank at %d,%d", post.X, post.Z)
		return v
	}
	v.Opened = true
	defer func() { _ = bank.Close() }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if len(bank.Inventory()) > 0 {
		if err := bank.BatchDepositInventory(w); err != nil {
			v.Err = "deposit: " + err.Error()
		}
	}
	v.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	return v
}

// CountProducts is how many units of the products a bank reading holds, by id, per product name.
func CountProducts(banked []gamestate.SlotItem, products []Product) map[string]int {
	out := map[string]int{}
	for _, p := range products {
		if n := int(actions.CountByID(banked, p.ID)); n > 0 {
			out[p.Name] += n
		}
	}
	return out
}

// Total sums a count map.
func Total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
