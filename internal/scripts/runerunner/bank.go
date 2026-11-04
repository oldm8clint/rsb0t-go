// Package runerunner is RuneRunner's I/O half: the bank reads and draws, the shop visits, the
// Mage Bank leg and the delivery, each a thin wrapper over primitives ScarabSwarm already proved
// live (scarabrunner.WalkTo, OpenBank, BuyFromKeeperPlanned, PrepareKit and the lever legs;
// donorrunner.Run for the trade). Every decision -- how much, where, whether -- is
// runerunnerplan's; the bot glue (internal/bot/runerunner.go) sequences them.
package runerunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// CoinsName is the coin object's display name.
const CoinsName = "Coins"

func logf(d *fleetrunner.Deps, format string, args ...interface{}) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func objName(d *fleetrunner.Deps, id int32) string {
	if d.Config == nil {
		return ""
	}
	if o, ok := d.Config.Obj(int(id)); ok {
		return o.Name
	}
	return ""
}

// waitUntil polls cond through the account's own Delay until it holds or timeout passes.
func waitUntil(d *fleetrunner.Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(150 * time.Millisecond) {
			return false
		}
	}
}

// BankRead is one reading of an open bank.
type BankRead struct {
	Coins int
	Runes fleetplan.RuneStock
}

// ReadBank reads an open bank's coins and runes.
func ReadBank(d *fleetrunner.Deps, bank *actions.Bank) BankRead {
	items := bank.Items()
	out := BankRead{Runes: fleetrunner.RuneCounts(d, items)}
	for _, it := range items {
		if it.ID == fleetrunner.CoinsID {
			out.Coins += int(it.Count)
		}
	}
	return out
}

// OpenBankAt walks to a bank on the wolf- and bear-avoiding webwalk and opens it
// (scarabrunner.OpenBank), then reads it. The caller closes the bank.
func OpenBankAt(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, BankRead, bool) {
	bank, ok := fleetrunner.OpenBank(d, at)
	if !ok || bank == nil {
		return nil, BankRead{}, false
	}
	return bank, ReadBank(d, bank), true
}

// CloseBank closes the bank and lets the close land.
func CloseBank(d *fleetrunner.Deps, bank *actions.Bank) {
	if bank == nil {
		return
	}
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
}

// DepositExcept puts every pack stack whose name is not in keep into the open bank, and leaves the
// account's travel kit alone (TravelKeeps: the rub jewellery above all, which a coins-only deposit
// banked every round until the 7 October 2026 travel wiring). With the bank open and the pack
// emptied it then draws the run staller's jewellery from the account's own bank when the pack
// holds none (scarabrunner.Travel.DrawRubItem).
func DepositExcept(d *fleetrunner.Deps, bank *actions.Bank, keep []string) int {
	keep = append(append([]string(nil), keep...), TravelKeeps(d)...)
	n := thieverrunner.DepositAllExcept(fleetrunner.Thiev(d), bank, keep)
	d.Travel.DrawRubItem(d, bank, nil)
	return n
}

// TravelKeeps is the names of the pack's items the account's Travel wants kept through a deposit
// (scarabrunner.Travel.AlchKeeps): the Games necklace or Ring of dueling, the walk alch kit while
// walk alch is on, the laws for a teleport. Nil with no Travel.
func TravelKeeps(d *fleetrunner.Deps) []string {
	if d == nil || d.Travel == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, it := range fleetrunner.PackItems(d) {
		key := strings.ToLower(it.Name)
		if seen[key] || !d.Travel.AlchKeeps(d, it.Name) {
			continue
		}
		seen[key] = true
		out = append(out, it.Name)
	}
	return out
}

// WithdrawCoins draws n coins from the open bank and reports how many arrived.
func WithdrawCoins(d *fleetrunner.Deps, bank *actions.Bank, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	before := fleetrunner.CoinsHeld(d)
	// The count rides behind the button blind and the draw is confirmed once (QuickWithdrawX), a round trip less than
	// WithdrawX waiting for the count dialog (twopertick-1012).
	if err := bank.QuickWithdrawX(fleetrunner.CoinsID, int32(n), thieverrunner.Waiter(fleetrunner.Thiev(d))); err != nil {
		return fleetrunner.CoinsHeld(d) - before, err
	}
	waitUntil(d, 4*time.Second, func() bool { return fleetrunner.CoinsHeld(d) >= before+n })
	return fleetrunner.CoinsHeld(d) - before, nil
}

// WithdrawItems withdraws every bank row of a listed item: the listed runes (each one stack, one
// pack slot) and, when the ring buys them, Staffs of air (which do not stack: one slot each, and
// Withdraw-All takes as many as the pack has room for). rows is how many bank rows there were to
// take, drawn how many pack slots now hold them, and got the counts that arrived.
func WithdrawItems(d *fleetrunner.Deps, bank *actions.Bank, items []string) (rows, drawn int, got map[string]int) {
	got = map[string]int{}
	listed := map[string]bool{}
	for _, r := range items {
		listed[strings.ToLower(r)] = true
	}
	// Every row is drawn in ONE burst (twopertick-1012): a Withdraw-All per row, five user events a tick, confirmed once,
	// where each was a click and a wait for the pack to show it (a tick or two a row). A row the burst did not draw is
	// drawn again one at a time.
	type row struct {
		id              int32
		name            string
		held, heldSlots int
	}
	var todo []row
	var ops []actions.BankOp
	for _, it := range bank.Items() {
		name := objName(d, it.ID)
		if !listed[strings.ToLower(name)] || it.Count <= 0 {
			continue
		}
		if o, ok := d.Config.Obj(int(it.ID)); ok && o.Noted() {
			continue
		}
		rows++
		todo = append(todo, row{id: it.ID, name: name, held: packCount(d, it.ID), heldSlots: packSlotsOf(d, it.ID)})
		ops = append(ops, actions.WithdrawAllOp(it.ID))
	}
	if len(ops) > 0 {
		r, retried := bank.Visit(ops, thieverrunner.Waiter(fleetrunner.Thiev(d)), actions.BatchOptions{})
		logf(d, "runerunner: %s", actions.VisitLog("delivery draw", len(ops), r, retried))
	}
	for _, t := range todo {
		if n := packCount(d, t.id) - t.held; n > 0 {
			got[t.name] += n
			drawn += packSlotsOf(d, t.id) - t.heldSlots
		}
	}
	return rows, drawn, got
}

// PackCounts is the pack's count of each named item (runes by stack, staffs by slot), matched
// ignoring case and keyed by the name as given.
func PackCounts(d *fleetrunner.Deps, names []string) map[string]int {
	want := map[string]string{}
	for _, n := range names {
		want[strings.ToLower(n)] = n
	}
	out := map[string]int{}
	for _, it := range packSlots(d) {
		if key, ok := want[strings.ToLower(objName(d, it.ID))]; ok {
			out[key] += int(it.Count)
		}
	}
	return out
}

func packSlotsOf(d *fleetrunner.Deps, id int32) int {
	n := 0
	for _, it := range packSlots(d) {
		if it.ID == id {
			n++
		}
	}
	return n
}

func packSlots(d *fleetrunner.Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

func packCount(d *fleetrunner.Deps, id int32) int {
	n := 0
	for _, it := range packSlots(d) {
		if it.ID == id {
			n += int(it.Count)
		}
	}
	return n
}

// PackRunes is the listed runes in the pack.
func PackRunes(d *fleetrunner.Deps) fleetplan.RuneStock { return fleetrunner.PackRunes(d) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *fleetrunner.Deps) int { return thieverrunner.FreeSlots(fleetrunner.Thiev(d)) }

// SlotsWanted is how many new pack slots a purchase of these runes needs: one per rune not
// already stacked in the pack.
func SlotsWanted(d *fleetrunner.Deps, runes []string) int {
	have := PackRunes(d)
	n := 0
	for _, r := range runes {
		if have[r] <= 0 {
			n++
		}
	}
	return n
}

// MakeRoom opens the bank at `at` and deposits everything except coins and the listed runes, so
// the pack has room for a visit's new stacks. It reports whether the bank opened.
func MakeRoom(d *fleetrunner.Deps, at tile.Tile, runes []string) bool {
	bank, _, ok := OpenBankAt(d, at)
	if !ok {
		return false
	}
	keep := append([]string{CoinsName}, runes...)
	DepositExcept(d, bank, keep)
	CloseBank(d, bank)
	return true
}

// TopUp opens the bank at `at`, deposits everything except coins and the listed runes, and draws
// `coins` more. It returns the bank reading (before the draw) and the coins that arrived.
func TopUp(d *fleetrunner.Deps, at tile.Tile, runes []string, coins int) (BankRead, int, bool) {
	bank, read, ok := OpenBankAt(d, at)
	if !ok {
		return BankRead{}, 0, false
	}
	defer CloseBank(d, bank)
	DepositExcept(d, bank, append([]string{CoinsName}, runes...))
	got, err := WithdrawCoins(d, bank, coins)
	if err != nil {
		logf(d, "runerunner: drawing %d coins at %d,%d: %v", coins, at.X, at.Z, err)
	}
	return read, got, true
}

// BankCounts is an open bank's count of each named item (unnoted), keyed by the name as given.
func BankCounts(d *fleetrunner.Deps, bank *actions.Bank, names []string) map[string]int {
	want := map[string]string{}
	for _, n := range names {
		want[strings.ToLower(n)] = n
	}
	out := map[string]int{}
	for _, it := range bank.Items() {
		key, ok := want[strings.ToLower(objName(d, it.ID))]
		if !ok {
			continue
		}
		if o, found := d.Config.Obj(int(it.ID)); found && o.Noted() {
			continue
		}
		out[key] += int(it.Count)
	}
	return out
}

// PackSlots is how many pack slots hold any of the named items.
func PackSlots(d *fleetrunner.Deps, names []string) int {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	n := 0
	for _, it := range packSlots(d) {
		if want[strings.ToLower(objName(d, it.ID))] {
			n++
		}
	}
	return n
}

// noteOf is the bank-note obj of item id, 0 when it has none (scarabswarm_consolidate.go's consolidateNoteOf).
func noteOf(d *fleetrunner.Deps, id int32) int32 {
	if d == nil || d.Config == nil {
		return 0
	}
	o, ok := d.Config.Obj(int(id))
	if !ok {
		return 0
	}
	for _, cand := range d.Config.ObjIDs(o.Name) {
		if co, ok := d.Config.Obj(cand); ok && co.Noted() && co.CertLink == int(id) {
			return int32(cand)
		}
	}
	return 0
}

// WithdrawNoted draws every bank row of the named unstackable parts as bank notes, one pack slot a stack whatever the count, and
// returns the counts that arrived by name. A pack takes 24 single Ropes, so a delivery of loose ones moved one pack a round
// whatever the bank held (the mule held 37 on 4 Nov 2026); a note stack crosses in one trade slot and the mule's bank takes it
// unnoted. A part with no note form is left for WithdrawItems. Note mode is put back to Item before it returns.
func WithdrawNoted(d *fleetrunner.Deps, bank *actions.Bank, names []string) map[string]int {
	got := map[string]int{}
	if len(names) == 0 || bank == nil || d == nil || d.Config == nil {
		return got
	}
	listed := map[string]bool{}
	for _, n := range names {
		listed[strings.ToLower(strings.TrimSpace(n))] = true
	}
	type row struct {
		id, note int32
		name     string
		count    int32
	}
	var rows []row
	for _, it := range bank.Items() {
		if it.Count <= 0 {
			continue
		}
		o, ok := d.Config.Obj(int(it.ID))
		if !ok || o.Noted() || !listed[strings.ToLower(o.Name)] {
			continue
		}
		if note := noteOf(d, it.ID); note != 0 {
			rows = append(rows, row{id: it.ID, note: note, name: o.Name, count: it.Count})
		}
	}
	if len(rows) == 0 {
		return got
	}
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if err := bank.SetNoteMode(true, w); err != nil {
		logf(d, "runerunner: note mode for the delivery: %v: the Ropes are drawn as single items", err)
		return got
	}
	defer func() {
		if err := bank.SetNoteMode(false, w); err != nil {
			logf(d, "runerunner: putting the bank back to Item mode: %v", err)
		}
	}()
	for _, r := range rows {
		before := packCount(d, r.note)
		if err := bank.WithdrawXAs(r.id, r.count, r.note, w); err != nil {
			logf(d, "runerunner: drawing %d %s as notes: %v", r.count, r.name, err)
		}
		if n := packCount(d, r.note) - before; n > 0 {
			got[r.name] += n
		}
	}
	return got
}
