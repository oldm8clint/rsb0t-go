package fisherrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fastopen"
)

// Openables are the Big net catches users want opened before a mule hand-off
// (2026-09-19): content's [opheld1,casket] gives one random reward (coins, uncut gems, a
// cosmic talisman, a key half) and [opheld1,oystershell] gives an Oyster pearl or an Empty
// oyster. Both are the item's first held op, "Open".
var Openables = []string{"Casket", "Oyster"}

// maxOpenRounds bounds OpenBankedOpenables: each round opens a pack's worth.
const maxOpenRounds = 20

// IsOpenable reports whether name is one of Openables, ignoring case.
func IsOpenable(name string) bool {
	for _, o := range Openables {
		if strings.EqualFold(o, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// OpenBankedOpenables withdraws every banked casket and oyster a pack at a time, opens them,
// and banks what they held, until none are left. keep names what stays in the pack when
// depositing (tools, food). reopen reopens the same bank after the pack is used. It returns
// the bank, open again, and how many it opened; ok is false when the bank could not be
// reopened, so the caller retries the hand-off later.
func OpenBankedOpenables(d *Deps, bank *actions.Bank, reopen func() (*actions.Bank, bool), keep []string) (*actions.Bank, int, bool) {
	opened := 0
	for round := 0; round < maxOpenRounds; round++ {
		if bankedOpenables(d, bank) == 0 {
			return bank, opened, true
		}
		if FreeSlots(d) == 0 {
			DepositExcept(d, bank, keep)
		}
		if err := SetNoteMode(d, bank, false); err != nil {
			d.logf("opening caskets/oysters: could not switch the bank to items: %v", err)
		}
		// The caskets and the oysters are drawn in ONE burst (twopertick-1012): two Withdraw-Alls, five user events a
		// tick at most and one confirm, where each was a click and a wait; a pack that fills takes what fits.
		var draws []actions.BankOp
		for _, id := range bank.RowIDsByName(d.Config, Openables) {
			draws = append(draws, actions.WithdrawAllOp(id))
		}
		if len(draws) > 0 && FreeSlots(d) > 0 {
			r, retried := bank.Visit(draws, Waiter(d), actions.BatchOptions{})
			d.logf("%s", actions.VisitLog("caskets and oysters", len(draws), r, retried))
		}
		_ = bank.Close()
		n := openHeldOpenables(d)
		opened += n
		var ok bool
		if bank, ok = reopen(); !ok {
			return nil, opened, false
		}
		DepositExcept(d, bank, keep)
		if n == 0 {
			d.logf("opening caskets/oysters: nothing opened this round — handing over what is left")
			return bank, opened, true
		}
	}
	return bank, opened, true
}

func bankedOpenables(d *Deps, bank *actions.Bank) int {
	n := 0
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && IsOpenable(obj.Name) && !obj.Noted() {
			n += int(it.Count)
		}
	}
	return n
}

// openHeldOpenables opens every casket and oyster in the pack as fast as the server allows:
// fastopen.Open, five a tick, one message a tick (the next click closes the last casket's reward
// box itself: engine OpHeldHandler clearPendingAction). Whatever that leaves goes the old way:
// click them all in one burst, then page through the reward boxes with Continue. Oysters show no
// box. Bounded by openablesTimeout.
func openHeldOpenables(d *Deps) int {
	heldNow := func() []gamestate.SlotItem { return heldOpenables(d) }
	held := len(heldNow())
	if held == 0 {
		return 0
	}
	if d.Loot != nil {
		before := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
		defer func() { recordOpenablesLoot(d, before) }()
	}
	// Five opens a tick, one message a tick, confirmed once (fastopen). What it could not open goes
	// the old way below.
	switch r := fastopen.Open(d.Session, fastopen.Delayer(d.Delay), PlayerPack, heldNow()); r.Reason {
	case "done", "stopped":
		return held - len(heldNow())
	default:
		d.logf("opening caskets/oysters: %d of %d still held after the fast open — opening them the old way", r.Left, r.Clicked)
	}
	for _, it := range heldNow() {
		if err := actions.Open(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("open %d in slot %d: %v", it.ID, it.Slot, err)
		}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	deadline := time.Now().Add(openablesTimeout)
	for time.Now().Before(deadline) && len(heldNow()) > 0 {
		if dlg.CanContinue() {
			_ = dlg.Continue()
		}
		if !d.Delay(150 * time.Millisecond) {
			// Stopped: no retry pass, and no further clicks. Before this returned, the
			// retry loop below sent Open for every casket still held (review, 2026-09-25).
			return held - len(heldNow())
		}
	}
	// A casket clicked while its predecessor's box was up may not have registered.
	if left := heldNow(); len(left) > 0 && time.Now().Before(deadline.Add(openablesTimeout)) {
		for _, it := range left {
			_ = actions.Open(d.Session, it.ID, it.Slot, PlayerPack)
			for i := 0; i < 20 && packSlotID(d, it.Slot) == it.ID; i++ {
				if dlg.CanContinue() {
					_ = dlg.Continue()
				}
				if !d.Delay(150 * time.Millisecond) {
					return held - len(heldNow())
				}
			}
		}
	}
	for i := 0; i < 5 && dlg.CanContinue(); i++ {
		_ = dlg.Continue()
		d.Delay(150 * time.Millisecond)
	}
	if main, _, chat := d.Session.State().Modals(); main != gamestate.NoModal || chat != gamestate.NoModal {
		_ = actions.CloseModal(d.Session)
	}
	return held - len(heldNow())
}

// openablesTimeout bounds one burst: a full pack of caskets is 28 reward boxes.
const openablesTimeout = 25 * time.Second

// recordOpenablesLoot reports what one burst yielded, measured as the pack's own change since
// before (taken just before the first click): how many caskets and oysters left the pack, and
// every item that arrived. It never adds a wait of its own.
func recordOpenablesLoot(d *Deps, before loot.Counts) {
	after := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	y := measureOpenables(before, after, func(id int) (string, bool) {
		obj, ok := d.Config.Obj(id)
		if !ok {
			return "", false
		}
		return obj.Name, obj.Noted()
	}, d.Junk)
	reportOpenables(d, y)
}

// reportOpenables sends one Drop per kind opened: the opens as its events, and the rewards the script
// keeps as its items.
func reportOpenables(d *Deps, y openablesYield) {
	for _, name := range Openables {
		if y.opened[name] <= 0 {
			continue
		}
		d.Loot(loot.Drop{Source: loot.Openable(name, y.sourceID[name]), Events: y.opened[name], Items: loot.Items(d.Config, y.gained[name])})
	}
}

// openablesYield is one burst's pack change, split per openable (keyed by its Openables name).
type openablesYield struct {
	opened   map[string]int
	sourceID map[string]int
	gained   map[string]loot.Counts
}

// measureOpenables is recordOpenablesLoot's pure core. A burst opens caskets and oysters
// together, so the rewards are split by what they are rather than by timing: an oyster only
// ever yields an item named for oysters (content's [opheld1,oystershell]: "Oyster pearl" or
// "Empty oyster"), and a casket never does ([opheld1,casket]: coins, uncut gems, a cosmic
// talisman or a key half). When only one kind was opened, everything that arrived is its.
// nameOf reports an obj's name and whether it is noted; an unknown id counts as a casket reward.
//
// junk is what the script throws away (Deps.Junk; nil for a script that banks everything): a reward
// it names is never recorded, because it never reaches the bank and is not what the account earns
// (a user request: "dropped empty oysters shouldnt be considered in the oyster loot table"). The
// opens still count, so the oyster card reads "so many opened, so many pearls" and the drop rate of a
// pearl stays the rate of a pearl.
func measureOpenables(before, after loot.Counts, nameOf func(id int) (string, bool), junk func(name string) bool) openablesYield {
	y := openablesYield{opened: map[string]int{}, sourceID: map[string]int{}, gained: map[string]loot.Counts{}}
	kind := func(id int) string {
		name, noted := nameOf(id)
		if noted {
			return ""
		}
		for _, o := range Openables {
			if strings.EqualFold(o, strings.TrimSpace(name)) {
				return o
			}
		}
		return ""
	}
	for id, n := range before {
		k := kind(id)
		if k == "" {
			continue
		}
		if left := n - after[id]; left > 0 {
			y.opened[k] += int(left)
			if _, ok := y.sourceID[k]; !ok || id < y.sourceID[k] {
				y.sourceID[k] = id
			}
		}
	}
	gained := loot.Gained(before, after, func(id int) bool {
		if kind(id) != "" {
			return true
		}
		name, _ := nameOf(id)
		return junk != nil && junk(name)
	})
	for id, n := range gained {
		// SPLIT BY WHAT THE REWARD IS, ALWAYS -- never by which openable happened to be opened
		// this burst.
		//
		// The old rule guessed from the counts and only fell back to the reward's name when
		// BOTH kinds had been opened. So a burst of oysters filed everything as Oyster and a
		// burst of caskets filed everything as Casket, whatever actually arrived -- and the
		// fleet owner watched the two cards show each other's loot (2026-09-22): "casket loot
		// panel has oyster loot in it and oyster has casket loot in it... ensuring it cant
		// happen".
		//
		// Reward identity cannot swap, and that is the whole point of using it. Content 289
		// keeps the two tables disjoint: [opheld1,oystershell] yields an Oyster pearl or an
		// Empty oyster, both named for oysters; [opheld1,casket] yields coins, uncut gems, a
		// cosmic talisman or a key half, and never anything oyster-named. So the name is not a
		// hint here, it is the identity -- and a test pins both real tables against it.
		to := openableFor(id, nameOf)
		// AND NEVER CREDIT A KIND THAT WAS NOT OPENED. Identity says which table a reward
		// belongs to; it does not say the reward came from this burst at all. Coins arriving
		// while only oysters were opened came from something else entirely -- a pickpocket, a
		// drop, a hand-out -- and belong to neither card.
		//
		// Dropping it is the honest answer and it is what the older rule was reaching for when
		// it said "a phantom casket". That rule got the principle right and the remedy wrong:
		// it handed the coins to the oysters instead, which is how the two cards came to show
		// each other's loot.
		if y.opened[to] == 0 {
			continue
		}
		if y.gained[to] == nil {
			y.gained[to] = loot.Counts{}
		}
		y.gained[to][id] = n
	}
	return y
}

// heldOpenables is the pack's unnoted openables.
func heldOpenables(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []gamestate.SlotItem
	for _, it := range inv.NonEmpty() {
		if openableID(d, int(it.ID)) {
			out = append(out, it)
		}
	}
	return out
}

// openableID reports whether an unnoted casket or oyster has this id. A var so a test can
// answer without a config archive.
var openableID = func(d *Deps, id int) bool {
	obj, ok := d.Config.Obj(id)
	return ok && IsOpenable(obj.Name) && !obj.Noted()
}

func packSlotID(d *Deps, slot int) int32 {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return -1
	}
	for _, it := range inv.NonEmpty() {
		if it.Slot == slot {
			return it.ID
		}
	}
	return -1
}

// openableFor names which openable a reward came out of, from the reward alone.
//
// See measureOpenables for why this must not consult what was opened. The two content tables
// are disjoint and only oyster rewards are named for oysters, so this is a decision rather than
// a guess. An unknown id is a casket reward: the casket table is the open-ended one (coins,
// gems, talismans, key halves), while the oyster table is exactly two items.
func openableFor(id int, nameOf func(id int) (string, bool)) string {
	name, _ := nameOf(id)
	if strings.Contains(strings.ToLower(name), "oyster") {
		return "Oyster"
	}
	return "Casket"
}
