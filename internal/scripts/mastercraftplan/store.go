package mastercraftplan

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// THE STORAGE ROUND (a user request: "the mule can use masters bank for extra space").
//
// THE LOSS (live 5 Oct 2026). the mule, the MuleReceiver at the Catherby bank post, holds 265 distinct
// item rows of its bank's 288 (content 289 interface_bank/configs/bank.constant ^bank_total_slots; a
// bank row is one obj, and a deposit that would need a 289th is refused and the item stays in the
// depositor's pack). ScarabSwarm's loot hand-off keeps 48 rows free (consolidateMuleBankReserve), so
// past 240 rows the mule refuses every new loot TYPE, and fleet.log said so:
//
//	scarabswarm: consolidate - 1 loot type(s) held back, the mule's bank has no room for new types
//	(255 rows published, 48 kept free): Casket
//
// Seventy of its rows held five items or fewer, mostly odd gear and quest or kitchen leftovers
// (Mime top, Dragon longsword 2, Steel battleaxe 4, Rune scimitar, Ruby ring 5, Stew 5, Plank 3 ...):
// each costs a row of the bank as much as the hundreds of thousands of Oyster pearls do. the main account's own
// bank held 58 rows.
//
// WHAT THE ROUND DOES. When the mule's PUBLISHED bank holds at least craftStoreAtRows (235) rows,
// the main account asks the mule, in the one bundled kit every round uses, for the WHOLE of each cold, small row
// (lowest count first, at most 28 lines) and banks what arrives in its own bank, again until the
// published bank would be down to craftStoreToRows (215) rows, at most masterSupplyAsks kits a round.
// It runs after the gem, bow and pearl rounds (the owner's order: gems, bows, pearls, then the later
// rounds) and before the shop, superheat, arrow and herb rounds, and chains like them.
//
// WHICH ROWS MAY GO. A row moves only when every one of these holds (PlanStoreAsk):
//
//   - it is not Coins, and it is not PROTECTED (StoreProtected: what a fleet script asks the mule for
//     or MasterCrafter itself uses; storeprotect.go derives it from the kit and catalogue definitions
//     the scripts use, and lists the few items that cannot be derived);
//   - it is not HOT: the mule has not handed any of it out in the last craftStoreQuietDays (the mule
//     trade record, bot.Context.MuleOut), and the hand-out record reaches back at least
//     StoreMinCoverage, because a record that goes back a day says nothing about last week;
//   - it holds at most craftStoreMaxCount (50) items;
//   - the mule can hand it out at all: its name passes orderplan's grammar (letters, digits, space,
//     apostrophe and hyphen: "Waterskin(4)" and "Adam full helm (g)" never do, whatever their stock),
//     content lets players trade it (donorplan.Untradeable), the name is ONE obj (a name several objs
//     share cannot be told apart in the mule's bank), and one kit can carry it (below).
//
// THE KIT'S ROOM (orderplan.DecideKit): at most orderplan.KitLinesMax (28) lines and 28 slots in one
// trade, the donor's pack being empty (a kit ask banks it first). A line takes one slot when the item
// stacks or the row holds one item, and one slot when it has a bank note (the mule sends a line as
// notes when the kit would not otherwise fit, and always above 28 of a non-stackable); with neither
// it takes a slot an item, and above 28 it cannot cross at all (DeclineKitNoNote). A line of one
// takes its one slot whatever it is, so a kit of 28 single pieces of gear is full and fits.
//
// WHAT IT LEAVES BEHIND. Retrieval is not built: an item stored here stays in the main account's bank, and a
// later ask for it goes to the main account (the mule will answer "none in the bank"). ScarabSwarm's loot
// hand-off, which runs on the main account too between rounds, keeps the names this round stored (Ledger.Stored
// through scarabSwarmBot.extraKeep) or it would carry them straight back to the mule.
// the main account's own bank keeps StoreOwnReserve rows free for its working products.

// ModeStore is a storage round. It lives here with the rest of the round's decisions.
const ModeStore Mode = "store"

const (
	// BankRows is a bank's size in content 289 (^bank_total_slots, consolidateplan.MuleBankSlots).
	BankRows = 288
	// StoreMinCoverage is the least the mule's hand-out record must reach back for a store round to
	// trust it: a day. The record keeps the newest muleledger.DefaultKeep entries (about eleven days
	// of the live the mule); a fresh or short one still shows everything handed out in the time it
	// holds, and the quiet days then count only that far, never below this.
	StoreMinCoverage = 24 * time.Hour
	// StoreOwnReserve is the rows of this account's own bank a store round never fills: its products
	// (cut gems, bars, bows, arrows, herbs) wait there for their hand-over.
	StoreOwnReserve = 40
	// StoreRefusedFor is how long an item the mule refused for a reason that belongs to the item (not
	// an item it hands out, kept back, a name it cannot read, too many to trade with no note) stays
	// off the plan.
	StoreRefusedFor = 7 * 24 * time.Hour
)

// StoreRow is one item of the mule's published bank as the storage round weighs it. The caller
// reads the facts from the cache (bot: storeRowFor); the plan decides.
type StoreRow struct {
	// Item is the name the mule publishes and a kit asks for; Count the items of it in the bank.
	Item  string
	Count int
	// Tradeable is the caller's verdict that content lets players trade it and that the name is ONE
	// obj: false when the cache cannot say, the name is shared, or an obj of that name is
	// tradeable=no (donorplan.Untradeable).
	Tradeable bool
	// Stackable and HasNote are what the mule's slot plan (orderplan.KitFacts) reads of the item.
	Stackable, HasNote bool
}

// Slots is how many trade slots (and pack slots) the whole row takes in one kit, 0 when no kit can
// carry it (see THE KIT'S ROOM).
func (r StoreRow) Slots() int {
	switch {
	case r.Count < 1:
		return 0
	case r.Stackable, r.Count == 1, r.HasNote:
		return 1
	case r.Count > orderplan.NotesAbove:
		return 0 // would have to travel as notes, and has none (orderplan.DeclineKitNoNote)
	default:
		return r.Count
	}
}

// StoreInput is what a storage round reads.
type StoreInput struct {
	// Rows is the mule's published bank, one row per item name: nil when it has not published one.
	Rows []StoreRow
	// OwnRows is how many rows this account's own bank holds (0: not read yet, taken as empty; the
	// round reads it before it asks).
	OwnRows int
	// Hot says the mule has handed an item out within the quiet days; nil says the hand-out record is
	// not available, and every item is then in use. Covered is how far back that record reaches.
	Hot     func(item string) bool
	Covered time.Duration
	// Protected overrides StoreProtected (nil: StoreProtected); Skip is items to leave out whatever
	// they are (declined or moved earlier in the round, refused earlier and remembered).
	Protected, Skip func(item string) bool
}

func (in StoreInput) protected(item string) bool {
	if in.Protected != nil {
		return in.Protected(item)
	}
	return StoreProtected(item)
}

// StoreCensus counts why the mule's rows may not move, for the log: every row is counted once, under
// the first reason that holds.
type StoreCensus struct {
	// Total is the mule's rows; Movable those a kit may ask for.
	Total, Movable int
	// Protected, Hot, Big (over craftStoreMaxCount), Unservable (no kit can carry it, the mule cannot
	// read its name, or it is not tradeable), Skipped (left out by the caller) count the rest.
	Protected, Hot, Big, Unservable, Skipped int
}

// String is the census in one line.
func (c StoreCensus) String() string {
	return strconv.Itoa(c.Movable) + " of " + strconv.Itoa(c.Total) + " rows can move (" +
		strconv.Itoa(c.Protected) + " protected, " + strconv.Itoa(c.Hot) + " handed out lately, " +
		strconv.Itoa(c.Big) + " over the size limit, " + strconv.Itoa(c.Unservable) + " the mule cannot hand out, " +
		strconv.Itoa(c.Skipped) + " left out)"
}

// canHandOut says the mule's order grammar reads the name (orderplan.Parse: letters, digits, space,
// apostrophe, hyphen, at most orderplan.ItemNameMax long).
func canHandOut(item string) bool {
	o, err := orderplan.Parse(orderplan.Format(item, 1))
	return err == nil && strings.EqualFold(o.Item, strings.TrimSpace(item))
}

// storeCandidates is the rows a kit may ask for, lowest count first, ties by name, and the census
// of every row.
func storeCandidates(set Settings, in StoreInput) ([]StoreRow, StoreCensus) {
	var out []StoreRow
	var c StoreCensus
	seen := map[string]bool{}
	for _, r := range in.Rows {
		name := strings.TrimSpace(r.Item)
		key := strings.ToLower(name)
		if name == "" || r.Count < 1 || seen[key] {
			continue
		}
		seen[key] = true
		c.Total++
		switch {
		case strings.EqualFold(name, "Coins") || in.protected(name):
			c.Protected++
		case in.Skip != nil && in.Skip(name):
			c.Skipped++
		case in.Hot == nil || in.Hot(name):
			c.Hot++
		case r.Count > set.StoreMaxCount:
			c.Big++
		case !r.Tradeable || r.Slots() == 0 || !canHandOut(name):
			c.Unservable++
		default:
			c.Movable++
			r.Item = name
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count < out[j].Count
		}
		return strings.ToLower(out[i].Item) < strings.ToLower(out[j].Item)
	})
	return out, c
}

// StoreTotal is the rows the mule's published bank holds (what StoreAtRows and StoreToRows count).
func StoreTotal(rows []StoreRow) int {
	n := 0
	seen := map[string]bool{}
	for _, r := range rows {
		key := strings.ToLower(strings.TrimSpace(r.Item))
		if key != "" && r.Count > 0 && !seen[key] {
			seen[key] = true
			n++
		}
	}
	return n
}

// PlanStoreAsk is ONE kit for a storage round: the whole of each cold, small, tradeable row, lowest
// count first (ties by name), enough to bring the mule's bank down to set.StoreToRows, no more than
// fits this account's own bank (BankRows less StoreOwnReserve less the rows it holds), at most
// orderplan.KitLinesMax lines and as many slots. A row that does not fit the slots left is passed
// over for a smaller one. Nothing when the bank is already at or under StoreToRows, and nothing
// without a hand-out record that reaches back StoreMinCoverage.
func PlanStoreAsk(set Settings, in StoreInput) []Line {
	if in.Hot == nil || in.Covered < StoreMinCoverage {
		return nil // never move what nothing can vouch is unused
	}
	cands, _ := storeCandidates(set, in)
	want := min(StoreTotal(in.Rows)-set.StoreToRows, BankRows-StoreOwnReserve-max(0, in.OwnRows), orderplan.KitLinesMax)
	if want <= 0 {
		return nil
	}
	var lines []Line
	slots := 0
	for _, r := range cands {
		if len(lines) >= want {
			break
		}
		if w := r.Slots(); slots+w <= orderplan.KitLinesMax {
			lines = append(lines, Line{Item: r.Item, Qty: r.Count})
			slots += w
		}
	}
	return lines
}

// StoreReadiness is whether a storage round is worth starting.
type StoreReadiness struct {
	Ready bool
	// Rows is the mule's published rows, Move how many rows the round expects to move (one kit's
	// worth is what it asks first; the round asks again), Census says why the others stay.
	Rows, Move int
	Census     StoreCensus
	Why        string
}

// StoreReady reads the mule's published bank: a round starts when it holds at least set.StoreAtRows
// rows, the hand-out record is there and reaches back StoreMinCoverage, and there is a row to move.
// StoreMinMove is the fewest rows a storage round starts for (unless fewer bring the bank down to StoreToRows): one trade
// carries up to orderplan.KitLinesMax rows, and a round of one row was a trip to the mule for two Rune arrows.
const StoreMinMove = 10

func StoreReady(set Settings, in StoreInput) StoreReadiness {
	var r StoreReadiness
	if !set.StoreOverflow {
		r.Why = "the storage round is off (" + KeyStoreOverflow + ")"
		return r
	}
	if in.Rows == nil {
		r.Why = "the mule has not published its bank"
		return r
	}
	r.Rows = StoreTotal(in.Rows)
	if r.Rows < set.StoreAtRows {
		r.Why = "the mule's bank has " + strconv.Itoa(r.Rows) + " rows, under " + KeyStoreAtRows + " " + strconv.Itoa(set.StoreAtRows)
		return r
	}
	if in.Hot == nil {
		r.Why = "the mule's hand-out record is not available, so nothing can be called unused"
		return r
	}
	if in.Covered < StoreMinCoverage {
		r.Why = "the mule's hand-out record reaches back only " + in.Covered.Round(time.Minute).String() + ", under the " + StoreMinCoverage.String() + " a storage round needs"
		return r
	}
	_, r.Census = storeCandidates(set, in)
	room := BankRows - StoreOwnReserve - max(0, in.OwnRows)
	r.Move = min(r.Rows-set.StoreToRows, r.Census.Movable, max(0, room))
	switch {
	case room <= 0:
		r.Why = "this account's bank holds " + strconv.Itoa(in.OwnRows) + " rows, with no room to keep " + strconv.Itoa(StoreOwnReserve) + " free"
	case r.Move <= 0:
		r.Why = "the mule's bank has " + strconv.Itoa(r.Rows) + " rows but no row can move: " + r.Census.String()
	case r.Move < min(StoreMinMove, r.Rows-set.StoreToRows):
		// a user request: "master is currently asking the mule for 1 item kits ... it should consolidate all asks into 1
		// ask as there are 28 inventory slots". A round waits until a trade's worth of rows can move together.
		r.Why = "the mule's bank has " + strconv.Itoa(r.Rows) + " rows but only " + strconv.Itoa(r.Move) + " can move, under the " +
			strconv.Itoa(StoreMinMove) + " a storage round's trade waits for: " + r.Census.String()
	default:
		r.Ready = true
		r.Why = "the mule's bank has " + strconv.Itoa(r.Rows) + " rows (" + KeyStoreAtRows + " " + strconv.Itoa(set.StoreAtRows) + "): " +
			strconv.Itoa(r.Move) + " to move to " + strconv.Itoa(set.StoreToRows) + "; " + r.Census.String()
	}
	return r
}

// ---- what the ledger remembers of it ----

// NoteStored records n units of item moved into this account's bank by a storage round, and keeps
// the item off every later plan's refused list (it was served after all).
func (l *Ledger) NoteStored(item string, n int) {
	key := strings.ToLower(strings.TrimSpace(item))
	if key == "" || n <= 0 {
		return
	}
	if l.Stored == nil {
		l.Stored = map[string]int{}
	}
	l.Stored[key] += n
	delete(l.StoreRefused, key)
}

// StoredNames is every item name a storage round has moved into this account's bank, sorted,
// lower case: the keep list ScarabSwarm's loot hand-off is given on this account.
func (l *Ledger) StoredNames() []string {
	if l == nil || len(l.Stored) == 0 {
		return nil
	}
	out := make([]string, 0, len(l.Stored))
	for k := range l.Stored {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NoteStoreRefused records that the mule refused item for a reason that belongs to the item, at
// now; the plan leaves it out for StoreRefusedFor.
func (l *Ledger) NoteStoreRefused(item string, now time.Time) {
	key := strings.ToLower(strings.TrimSpace(item))
	if key == "" {
		return
	}
	if l.StoreRefused == nil {
		l.StoreRefused = map[string]time.Time{}
	}
	l.StoreRefused[key] = now
}

// StoreRefusedNow reports whether item is still off the plan at now, forgetting refusals that have
// run out (so the file does not grow with every item the mule ever refused).
func (l *Ledger) StoreRefusedNow(item string, now time.Time) bool {
	if l == nil || len(l.StoreRefused) == 0 {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(item))
	at, ok := l.StoreRefused[key]
	if !ok {
		return false
	}
	if now.Sub(at) >= StoreRefusedFor {
		delete(l.StoreRefused, key)
		return false
	}
	return true
}

// StoreRefusalIsTheItem says a mule's decline of ONE kit line belongs to the item and will not pass
// by itself: the mule does not hand it out, keeps it back, cannot read its name, or it is more than
// a trade carries with no note to carry it as. "none in the bank" (the published row was stale),
// the per-item window limit, the donor's order cap and "no room" belong to the moment instead.
func StoreRefusalIsTheItem(reason string) bool {
	r := strings.ToLower(reason)
	for _, sub := range []string{
		"not an item i hand out",
		strings.ToLower(orderplan.DeclineKeptBack),
		strings.ToLower(string(orderplan.ErrItemChars)),
		strings.ToLower(string(orderplan.ErrItemTooLong)),
		strings.ToLower(orderplan.DeclineKitNoNote),
	} {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}
