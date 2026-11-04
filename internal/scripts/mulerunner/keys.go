package mulerunner

// The mule's key-half joining round: withdraw 14 of each half, use them on each other to make
// crystal keys, bank the keys (owner, 2026-09-20: "we can also make it that the mulereciever
// combines the key halves after doing casket openings withdrawing 14 of each half and using
// them on each other, i think 5 can be made per tick").
//
// It runs exactly one bounded round per call, like CookRound, and for the same reason: it must
// never hold the mule away from a trade request for long. One round is a bank open, one pack of
// halves, a handful of ticks of clicking and a bank close — a few seconds, not a few minutes.
// internal/bot/mule.go schedules it in the same quiet window the casket/oyster pass and the
// cooking round use, AFTER the openables pass, because opening caskets is what PRODUCES key
// halves ([opheld1,casket] hands one out) and doing it the other way round would leave the
// halves a pass just won sitting in the bank until the next quiet cycle.
//
// Two content facts this depends on, both read rather than assumed (see
// mulereceiverplan/keyhalves.go's own doc comment for the citations):
//
//   - content 289 names BOTH unnoted halves "Half of a key", so the two ids cannot be told
//     apart by name. ResolveKeyIDs takes every unnoted id sharing that name and requires
//     exactly two, refusing to guess if the content ever changes.
//   - the join has no player delay, and the engine accepts five OPHELDU packets per tick, so a
//     round fires them in bursts of mulereceiverplan.JoinsPerTick and re-reads the pack between
//     bursts.

import (
	"errors"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/mulereceiverplan"
)

// KeyHalfName and CrystalKeyName are the cache's own display names.
const (
	KeyHalfName    = "Half of a key"
	CrystalKeyName = "Crystal key"
)

// KeyIDs is the two unnoted half ids and the unnoted crystal key id, resolved live.
type KeyIDs struct {
	HalfA, HalfB int
	Key          int
	// HalfNoted and KeyNoted are the bank-note ids, so a round can tell a note apart from the
	// real thing (a note cannot be used on anything).
	HalfNoted map[int]bool
	KeyNoted  map[int]bool
}

// ResolveKeyIDs resolves the halves and the key through the cache, the way
// casketrunner.ResolveCasketIDs resolves "Casket": by name, taking every id, and classifying
// each by Obj.Noted. It reports false unless it found EXACTLY two distinct unnoted halves and
// at least one unnoted key id — anything else means the content is not what this code was
// written against, and guessing there would send packets content answers with nothing.
func ResolveKeyIDs(cfg *cachecfg.Config) (KeyIDs, bool) {
	if cfg == nil {
		return KeyIDs{}, false
	}
	out := KeyIDs{HalfA: -1, HalfB: -1, Key: -1, HalfNoted: map[int]bool{}, KeyNoted: map[int]bool{}}
	var halves []int
	for _, id := range cfg.ObjIDs(KeyHalfName) {
		obj, ok := cfg.Obj(id)
		if !ok {
			continue
		}
		if obj.Noted() {
			out.HalfNoted[id] = true
			continue
		}
		halves = append(halves, id)
	}
	for _, id := range cfg.ObjIDs(CrystalKeyName) {
		obj, ok := cfg.Obj(id)
		if !ok {
			continue
		}
		if obj.Noted() {
			out.KeyNoted[id] = true
			continue
		}
		if out.Key == -1 || id < out.Key {
			out.Key = id
		}
	}
	if len(halves) != 2 || out.Key == -1 {
		return out, false
	}
	// Lowest id first, so HalfA is content's keyhalf1 and HalfB its keyhalf2. Nothing depends
	// on which is which — [opheldu,keyhalf1] and [opheldu,keyhalf2] both reach join_keys — but
	// a stable order makes the logs and the tests readable.
	if halves[0] < halves[1] {
		out.HalfA, out.HalfB = halves[0], halves[1]
	} else {
		out.HalfA, out.HalfB = halves[1], halves[0]
	}
	return out, true
}

// KeyJoinResult is one round's outcome, for logging.
type KeyJoinResult struct {
	// Drawn is how many of each half was withdrawn, Joined how many crystal keys were
	// actually made (measured as the pack's own key count rising, never as clicks sent).
	Drawn  int
	Joined int
	// BankedHalves is how many of the scarcer half were still in the bank when the round
	// opened it: the round is due again while this is above one.
	BankedHalves int
	// Reason explains a round that made nothing.
	Reason string
	// Stopped is true when d.Delay asked to stop mid-round.
	Stopped bool
}

// joinSettleTimeout bounds the wait for one burst of joins to land. The engine runs the
// whole burst inside one or two ticks (no p_delay in join_keys), so this is generous.
const joinSettleTimeout = 3 * time.Second

// JoinKeyHalvesRound runs one bounded joining round at the mule's own post: open the bank,
// withdraw up to mulereceiverplan.HalvesPerTrip of each half, join them, bank the keys.
//
// It returns ok=false only when it could not run at all (no bank, no components, content it
// does not recognise); a round with nothing to do returns ok=true with Joined 0 and a Reason.
func JoinKeyHalvesRound(d *Deps) (KeyJoinResult, bool) {
	if d.Components == nil {
		return KeyJoinResult{Reason: "no interface archive configured"}, false
	}
	ids, ok := ResolveKeyIDs(d.Config)
	if !ok {
		return KeyJoinResult{Reason: "this content pack does not have two distinct key halves and a crystal key"}, false
	}
	bank, opened := openNearestBankBooth(d)
	if !opened {
		return KeyJoinResult{Reason: "could not open a bank booth"}, false
	}
	closed := false
	closeBank := func() {
		if !closed {
			closed = true
			_ = bank.Close()
		}
	}
	defer closeBank()

	bankA, bankB := bankCount(bank.Items(), ids.HalfA), bankCount(bank.Items(), ids.HalfB)
	res := KeyJoinResult{BankedHalves: mulereceiverplan.JoinableHalves(bankA, bankB)}
	if res.BankedHalves == 0 {
		res.Reason = "no matching pair of key halves in the bank"
		return res, true
	}

	// Go in with an empty pack: every free slot is another half. The pack's Deposit-Alls, the
	// switch to items and both halves' withdrawals go out in one burst (actions.Bank.Batch) and
	// are confirmed once against the bank's side panel; anything short is done the old way.
	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if !seen[it.ID] {
			seen[it.ID] = true
			ops = append(ops, actions.DepositAllOp(it.ID))
		}
	}
	draw := mulereceiverplan.WithdrawPerHalf(bankA, bankB, mulereceiverplan.PackSlots)
	if draw == 0 {
		res.Reason = "no room to withdraw a pair of halves"
		return res, true
	}
	ops = append(ops, actions.NoteModeOp(false), actions.WithdrawOp(int32(ids.HalfA), int32(draw)), actions.WithdrawOp(int32(ids.HalfB), int32(draw)))
	fast := bank.Batch(ops, handoutWaiter{d}, actions.BatchOptions{})
	if errors.Is(fast.Err, actions.ErrBankStopped) {
		res.Stopped = true
		return res, true
	}
	if !fast.OK() {
		d.logf("keys: %s — the rest the old way", fast)
		for _, sh := range fast.Short {
			if sh.Op.Kind == actions.BankOpDeposit {
				depositOneSlow(d, bank, sh.Op.ID)
			}
		}
		if bank.NoteMode() {
			if err := bank.SetNoteMode(false, handoutWaiter{d}); err != nil {
				d.logf("keys: switching the bank to items: %v", err)
			}
		}
		for _, id := range []int{ids.HalfA, ids.HalfB} {
			before := countByID(bank.Inventory(), int32(id))
			if before >= draw {
				continue
			}
			if err := bank.WithdrawX(int32(id), int32(draw-before), handoutWaiter{d}); err != nil {
				d.logf("keys: withdrawing %d of half %d: %v", draw-before, id, err)
				continue
			}
			if !waitFor(d, "key halves in the pack", 5*time.Second, func() bool { return countByID(bank.Inventory(), int32(id)) > before }) {
				if d.Stopped() {
					res.Stopped = true
					return res, true
				}
			}
		}
	}
	// The side panel is the view that follows the pack while the bank is open.
	res.Drawn = min2(countByID(bank.Inventory(), int32(ids.HalfA)), countByID(bank.Inventory(), int32(ids.HalfB)))
	closeBank()
	if res.Drawn == 0 {
		res.Reason = "the halves never arrived in the pack"
		return res, true
	}

	joined, stopped := joinHeldHalves(d, ids)
	res.Joined, res.Stopped = joined, stopped

	// Put the keys (and any odd half left over) back, so the next trade goes in with an empty
	// pack exactly as DepositAllInPack's own doc comment requires.
	if _, ok := DepositAllInPack(d); !ok {
		d.logf("keys: made %d crystal key(s) but could not bank them — they stay in the pack", joined)
	}
	if joined == 0 && res.Reason == "" {
		res.Reason = "the halves did not join"
	}
	return res, true
}

// joinHeldHalves fires OPHELDU at every pair in the pack, JoinsPerTick at a time, re-reading
// the pack between bursts, and returns how many crystal keys really arrived.
//
// Success is measured as the key count rising, never as clicks sent — the same rule
// completeTrade uses for receipt. A burst whose keys never arrive ends the round rather than
// clicking forever: the realistic causes (the backpack component not visible to the server, a
// content change) do not fix themselves inside one round.
func joinHeldHalves(d *Deps, ids KeyIDs) (int, bool) {
	eq := actions.NewEquipment(d.Session, d.Components)
	com := eq.InventoryComponent()
	if com == -1 {
		d.logf("keys: the server has not set this account's backpack tab up — every use-on-item would be refused silently")
		return 0, false
	}
	made := 0
	for round := 0; round < mulereceiverplan.PackSlots; round++ {
		pairs := mulereceiverplan.PairHalves(heldHalves(d, ids), ids.HalfA, ids.HalfB, mulereceiverplan.JoinsPerTick)
		if len(pairs) == 0 {
			return made, false
		}
		before := packCountOf(d, int32(ids.Key))
		sent := 0
		for _, p := range pairs {
			// Target then used item, the order OPHELDU's own payload takes.
			if err := actions.OpHeldU(d.Session, int32(p.To.ID), p.To.Slot, com, int32(p.From.ID), p.From.Slot, com); err != nil {
				d.logf("keys: OpHeldU: %v", err)
				break
			}
			sent++
		}
		if sent == 0 {
			return made, false
		}
		if !waitFor(d, "key halves to join", joinSettleTimeout, func() bool { return packCountOf(d, int32(ids.Key)) > before }) {
			if d.Stopped() {
				return made, true
			}
			d.logf("keys: sent %d join(s) and no crystal key arrived — stopping this round", sent)
			return made, false
		}
		// Let the rest of the burst land before re-reading, so the next burst is planned
		// against a settled pack rather than a half-applied one.
		if !d.Delay(600 * time.Millisecond) {
			return made + packCountOf(d, int32(ids.Key)) - before, true
		}
		made += packCountOf(d, int32(ids.Key)) - before
	}
	return made, false
}

// Stopped reports whether a zero-length delay says a stop was requested. Deps has no direct
// cancellation signal, and every sibling helper asks the Delayer the same way.
func (d *Deps) Stopped() bool { return !d.Delay(0) }

func heldHalves(d *Deps, ids KeyIDs) []mulereceiverplan.HalfSlot {
	var out []mulereceiverplan.HalfSlot
	for _, it := range packItems(d) {
		id := int(it.ID)
		if id == ids.HalfA || id == ids.HalfB {
			out = append(out, mulereceiverplan.HalfSlot{Slot: it.Slot, ID: id})
		}
	}
	return out
}

func bankCount(items []gamestate.SlotItem, id int) int {
	n := 0
	for _, it := range items {
		if int(it.ID) == id {
			n += int(it.Count)
		}
	}
	return n
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ReadBankTotals opens the nearest bank booth, reads every row into an obj-id -> count map and
// closes it again. Exported for a harness that must measure the mule's bank before and after
// something: a reading taken without a real open hands back gamestate's last snapshot, so two
// "snapshots" around a change can both be the same one (docs/trustworthy_harnesses.md, trap 2).
func ReadBankTotals(d *Deps) (map[int]int64, bool) {
	bank, ok := openNearestBankBooth(d)
	if !ok {
		return nil, false
	}
	out := map[int]int64{}
	for _, it := range bank.Items() {
		c := int64(it.Count)
		if c < 1 {
			c = 1
		}
		out[int(it.ID)] += c
	}
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
	return out, true
}
