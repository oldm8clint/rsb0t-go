package actions

// THE FAST BANK. One bank visit made the way MasterCrafter's quick bank (mastercraftrunner/quick.go)
// proved live on 4 October (a 15.5 s gem round down to 5.6 s): every deposit and withdrawal the
// visit needs goes out together the moment the bank shows with fresh inventories, five user events
// a tick, with no fixed pause and no confirm between operations, and the visit is confirmed ONCE at
// the end against the pack and the bank. What falls short is reported operation by operation, so
// the caller can retry or fall back to the old one-at-a-time path (bank.go, bank_more.go), which
// stays exactly as it was.
//
// The engine and content facts this rests on (engine = Engine-TS src, content = content/scripts):
//
//   - The engine reads at most five USER_EVENT packets a player a tick and leaves the rest in the
//     socket buffer for the next tick, in order (engine engine/entity/NetworkPlayer.ts decodeIn,
//     network/game/client/ClientGameProtCategory.ts USER_EVENT limit 5). INV_BUTTON, IF_BUTTON,
//     RESUME_P_COUNTDIALOG and CLOSE_MODAL are all USER_EVENT (network/game/client/model/*.ts).
//     Nothing is dropped for going over five, so the five-a-tick pacing below is a soft cap: a
//     burst that does land six in one tick still applies in order, one tick later.
//   - An INV_BUTTON is refused unless its component's interface is open (InvButtonHandler.ts:
//     isComponentVisible), the slot still holds that object (inv.hasAt), and the player is not
//     delayed. A refusal is silent, which is why the visit confirms at the end.
//   - The INV_BUTTON's script runs inside the handler, the same tick it is read
//     (InvButtonHandler.ts executeScript). Withdraw-X (option 5) reaches p_countdialog there and
//     suspends; a RESUME_P_COUNTDIALOG read after it in the same or a later tick resumes it
//     (ResumePCountDialogHandler.ts), so the count goes out right behind the button, with no wait
//     for the dialog to show. Until it is answered the player keeps protected access
//     (Player.ts executeScript: activePlayer.protect), and every other bank op would be refused,
//     so the pair is never split from its answer and nothing is sent between them.
//   - Withdraw-All (option 4) is @bank_withdraw(last_slot, ^max_32bit_int): as many as the pack
//     has room for, no count dialog (content interface_bank/scripts/bank.rs2,
//     bank_withdraw_request: inv_itemspace2). Withdraw 1/5/10 are options 1/2/3, no dialog.
//   - Deposit-All (option 4 on bank_side:inv) deposits every one of that object, wherever it is in
//     the pack (bank_deposit.rs2: inv_total), so one packet per object kind is enough.
//   - The bank is stackall and is only reorganised on open and close (bank.inv, bank.rs2
//     [label,openbank]/[label,closebank]); a withdrawal that empties a bank slot does not move the
//     others, and a deposit stacks onto its row or takes an empty slot. So the slots read when the
//     bank opened stay valid for the whole burst, as long as every deposit goes before every
//     withdrawal, which Batch enforces.
//   - CLOSE_MODAL only sets requestModalClose; the modal closes in the player's queue processing
//     of the same tick (CloseModalHandler.ts, Player.ts processQueues). [label,closebank] stops
//     transmitting the bank inventory (inv_stoptransmit), so a bank closed in the tick its last
//     operation ran never sends the client its final contents: a caller that needs a fresh bank
//     reading (MuleReceiver publishes it) confirms with the bank still open and closes afterwards,
//     which is what BatchOptions.Close does.
//   - The bank opens with if_openmain_side after inv_transmit of the pack and the bank, and
//     resets %bankcert to 0 (bank.rs2 [label,openbank]); the client keeps the last inventories it
//     was sent, so a fresh open is told apart by the inventory generations (MarkBankOpen).

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// BankMaxPerTick is how many user events the engine reads a player a tick.
const BankMaxPerTick = 5

const (
	// fastPoll is how often the fast bank looks at the session: well inside a tick.
	fastPoll = 10 * time.Millisecond
	// fastChunkWait is the longest a burst waits for the next server tick before sending its next
	// five anyway: the engine keeps what it has not read, in order.
	fastChunkWait = 900 * time.Millisecond
	// fastSettleTicks is how many server ticks after the last packet every result has certainly
	// arrived: read the next tick, its inventories flushed with that tick's PLAYER_INFO, and one
	// spare for a round trip up to about two ticks.
	fastSettleTicks = 3
	// fastConfirmTimeout bounds the confirm when no ticks are seen at all.
	fastConfirmTimeout = 3 * time.Second
	// fastStaleOpenGrace is how long an open bank whose inventories never refresh is waited on
	// before it is used as it is (the old fixed half-second pause, with margin).
	fastStaleOpenGrace = 1200 * time.Millisecond
)

// ErrBankStopped is a fast bank visit cut short by a stop request.
var ErrBankStopped = errors.New("actions: stopped during the bank visit")

// BankOpKind is what one BankOp does.
type BankOpKind int

const (
	// BankOpDeposit banks every one of ID from the pack (Deposit-All).
	BankOpDeposit BankOpKind = iota + 1
	// BankOpWithdraw draws ID from the bank: Count 0 is Withdraw-All, any other count exactly that
	// many (the 1/5/10 buttons, else Withdraw-X with its count sent behind it).
	BankOpWithdraw
	// BankOpNoteMode sets the withdraw mode: Notes true draws bank notes.
	BankOpNoteMode
)

// BankOp is one operation of a fast bank visit.
type BankOp struct {
	Kind BankOpKind
	// ID is the pack object for a deposit, the bank row's object for a withdrawal.
	ID int32
	// Count is a withdrawal's exact count; 0 is Withdraw-All.
	Count int32
	// LandsAs is the pack object a withdrawal arrives as (the note, in note mode); 0 is ID.
	LandsAs int32
	// Notes is BankOpNoteMode's mode.
	Notes bool
}

// DepositAllOp is Deposit-All of id.
func DepositAllOp(id int32) BankOp { return BankOp{Kind: BankOpDeposit, ID: id} }

// WithdrawAllOp is Withdraw-All of id.
func WithdrawAllOp(id int32) BankOp { return BankOp{Kind: BankOpWithdraw, ID: id} }

// WithdrawOp is a withdrawal of exactly n of id (n <= 0 is Withdraw-All).
func WithdrawOp(id, n int32) BankOp {
	if n < 0 {
		n = 0
	}
	return BankOp{Kind: BankOpWithdraw, ID: id, Count: n}
}

// WithdrawAsOp is WithdrawOp arriving in the pack as landsAs (a note).
func WithdrawAsOp(id, n, landsAs int32) BankOp {
	op := WithdrawOp(id, n)
	op.LandsAs = landsAs
	return op
}

// NoteModeOp sets the withdraw mode to notes (true) or items.
func NoteModeOp(notes bool) BankOp { return BankOp{Kind: BankOpNoteMode, Notes: notes} }

func (op BankOp) landsAs() int32 {
	if op.LandsAs != 0 {
		return op.LandsAs
	}
	return op.ID
}

func (op BankOp) String() string {
	switch op.Kind {
	case BankOpDeposit:
		return fmt.Sprintf("deposit-all id=%d", op.ID)
	case BankOpWithdraw:
		if op.Count == 0 {
			return fmt.Sprintf("withdraw-all id=%d", op.ID)
		}
		return fmt.Sprintf("withdraw %d id=%d", op.Count, op.ID)
	case BankOpNoteMode:
		if op.Notes {
			return "note mode on"
		}
		return "note mode off"
	}
	return "unknown bank op"
}

// BankShort is one operation that did not fully happen.
type BankShort struct {
	Op BankOp
	// Want is what the operation should have moved, Got what the pack shows it moved.
	Want, Got int64
	Why       string
}

// BankBatch is what one fast bank visit did.
type BankBatch struct {
	// Sent is how many user events went out, Chunks in how many bursts (at most BankMaxPerTick
	// each).
	Sent, Chunks int
	// Short lists every operation that fell short; empty when the whole visit happened.
	Short []BankShort
	// Pack and Bank are the pack and the bank as the confirm read them (with the bank still open,
	// so both are fresh).
	Pack, Bank []gamestate.SlotItem
	// Closed is the bank closed after the confirm (BatchOptions.Close).
	Closed bool
	// Err is a visit that could not run (the bank not open) or was stopped.
	Err error
	// Took is the whole visit; Confirm the wait after the last packet.
	Took, Confirm time.Duration
}

// OK reports whether every operation happened.
func (r BankBatch) OK() bool { return r.Err == nil && len(r.Short) == 0 }

// ShortOf is the shortfall reported for op's object (Kind and ID), if any.
func (r BankBatch) ShortOf(kind BankOpKind, id int32) (BankShort, bool) {
	for _, s := range r.Short {
		if s.Op.Kind == kind && s.Op.ID == id {
			return s, true
		}
	}
	return BankShort{}, false
}

func (r BankBatch) String() string {
	if r.Err != nil {
		return fmt.Sprintf("fast bank: %v", r.Err)
	}
	s := fmt.Sprintf("fast bank: %d packet(s) in %d burst(s), confirmed in %s (%s in all)", r.Sent, r.Chunks, r.Confirm.Round(time.Millisecond), r.Took.Round(time.Millisecond))
	for _, sh := range r.Short {
		s += fmt.Sprintf("; %s short (%d of %d): %s", sh.Op, sh.Got, sh.Want, sh.Why)
	}
	return s
}

// BatchOptions tunes Batch.
type BatchOptions struct {
	// Close closes the bank once the confirm has read the pack and the bank.
	Close bool
	// ConfirmTimeout bounds the confirm when no server ticks are seen; 0 is fastConfirmTimeout.
	ConfirmTimeout time.Duration
}

// fastPacket is one burst entry: one or two user events that must go in the same tick.
type fastPacket struct {
	events int
	send   func() error
	op     int // index into the planned ops this packet serves, -1 for none
}

// fastCheck is the confirm's expectation for one planned op.
type fastCheck struct {
	op      BankOp
	before  int64 // the landing object's pack count before the burst (0 after a deposit of it)
	want    int64 // what a withdrawal should add; a deposit wants the pack to hold none
	all     bool  // Withdraw-All: as many as fit
	checked bool  // false: nothing to confirm (a note mode switch)
}

// Batch makes one fast bank visit on an already-open bank: every deposit (Deposit-All, one per
// object kind) goes first, then the note-mode switches and withdrawals in the order given, sent at
// most BankMaxPerTick user events a tick with no wait on anything the server does not need; then
// the pack and the bank are read once. A withdrawal of an object the bank did not hold when it
// opened is reported short without sending anything. The caller decides what to do with a short
// result (a retry, or the one-at-a-time path in bank.go and bank_more.go).
func (b *Bank) Batch(ops []BankOp, w Waiter, opt BatchOptions) (out BankBatch) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	if !b.IsOpen() {
		out.Err = ErrNotOnScreen
		return out
	}
	side := b.depositComponent()
	if side == -1 {
		out.Err = ErrNotOnScreen
		return out
	}
	st := b.s.State()
	r := b.plan(ops, b.s, side, items(st, side), b.Items())
	out.Short = append(out.Short, r.unplanned...)

	// Send: at most BankMaxPerTick user events a burst; the next burst waits for the next server
	// tick, or for this one's results to show (it was read, so the next cannot share its tick).
	lastTick, lastGen := st.Tick(), r.packGen()
	for i := 0; i < len(r.packets); {
		if !b.IsOpen() {
			out.Err = ErrNotOnScreen
			return out
		}
		if out.Chunks > 0 {
			tickAt, servedUpTo := lastTick, r.packets[i-1].op+1
			deadline := time.Now().Add(fastChunkWait)
			for st.Tick() <= tickAt && time.Now().Before(deadline) {
				if d, _ := r.allDone(servedUpTo); d {
					break
				}
				if !w.Delay(fastPoll) {
					out.Err = ErrBankStopped
					return out
				}
			}
		}
		lastTick, lastGen = st.Tick(), r.packGen()
		events := 0
		for i < len(r.packets) && events+r.packets[i].events <= BankMaxPerTick {
			if err := r.packets[i].send(); err != nil {
				out.Err = err
				return out
			}
			events += r.packets[i].events
			i++
		}
		out.Sent += events
		out.Chunks++
	}
	r.confirm(&out, w, opt, lastTick, lastGen)
	return out
}

// batchRun is one planned visit: what goes out, and what the confirm expects.
type batchRun struct {
	b       *Bank
	st      *gamestate.State
	side    int
	pack0   []gamestate.SlotItem
	ordered []BankOp
	checks  []fastCheck
	packets []fastPacket
	pairs   int
	// unplanned is every withdrawal of an object the bank view does not hold: short, nothing sent.
	unplanned []BankShort
}

// plan turns ops into packets sent through to, against pack0 (the pack, by slot) and bank0 (the
// bank, by slot): deposits first, then everything else in order.
func (b *Bank) plan(ops []BankOp, to Session, side int, pack0, bank0 []gamestate.SlotItem) *batchRun {
	st := b.s.State()
	r := &batchRun{b: b, st: st, side: side, pack0: pack0}
	deposited := map[int32]bool{}
	for _, op := range ops {
		if op.Kind == BankOpDeposit && !deposited[op.ID] {
			deposited[op.ID] = true
			r.ordered = append(r.ordered, op)
		}
	}
	for _, op := range ops {
		if op.Kind != BankOpDeposit {
			r.ordered = append(r.ordered, op)
		}
	}
	withdrawnAs := map[int32]bool{}
	for _, op := range r.ordered {
		if op.Kind == BankOpWithdraw {
			withdrawnAs[op.landsAs()] = true
		}
	}

	cert, certKnown := st.VarpKnown(BankCertVarp)
	r.checks = make([]fastCheck, len(r.ordered))
	expected := map[int32]int64{} // landing object -> what earlier withdrawals in this batch add
	for i, op := range r.ordered {
		i, op := i, op
		r.checks[i] = fastCheck{op: op}
		switch op.Kind {
		case BankOpDeposit:
			slot, _, found := slotOf(pack0, op.ID)
			if !found {
				continue // nothing of it to bank: not a shortfall
			}
			r.checks[i].checked = !withdrawnAs[op.ID]
			r.packets = append(r.packets, fastPacket{events: 1, op: i, send: func() error { return InvButton(to, bankDepositAll, op.ID, slot, side) }})
		case BankOpNoteMode:
			want := 0
			button := BankItemButton
			if op.Notes {
				want, button = 1, BankNoteButton
			}
			if certKnown && cert == want {
				continue
			}
			cert, certKnown = want, true
			r.packets = append(r.packets, fastPacket{events: 1, op: i, send: func() error { return IfButton(to, button) }})
		case BankOpWithdraw:
			lands := op.landsAs()
			before := int64(CountByID(pack0, lands))
			if deposited[lands] {
				before = 0
			}
			before += expected[lands]
			slot, avail, found := slotOf(bank0, op.ID)
			r.checks[i].checked = true
			r.checks[i].before = before
			if !found || avail <= 0 {
				r.unplanned = append(r.unplanned, BankShort{Op: op, Want: int64(op.Count), Why: "the bank holds none"})
				r.checks[i].checked = false
				continue
			}
			take := int64(op.Count)
			if take == 0 || take > int64(avail) {
				take = int64(avail)
			}
			r.checks[i].want = take
			r.checks[i].all = op.Count == 0
			expected[lands] += take
			option := 0
			switch {
			case op.Count == 0:
				option = bankWithdrawAll
			case take == 1:
				option = bankWithdrawOne
			case take == 5:
				option = bankWithdrawFive
			case take == 10:
				option = bankWithdrawTen
			}
			if option != 0 {
				r.packets = append(r.packets, fastPacket{events: 1, op: i, send: func() error { return InvButton(to, option, op.ID, slot, BankWithdraw) }})
				continue
			}
			n := int32(take)
			r.pairs++
			r.packets = append(r.packets, fastPacket{events: 2, op: i, send: func() error {
				if err := InvButton(to, bankWithdrawX, op.ID, slot, BankWithdraw); err != nil {
					return err
				}
				return answerCountDialogBlind(to, n)
			}})
		}
	}
	return r
}

// packNow is the confirm's view of the pack: the bank's side panel while it is open (the view that
// follows the pack then), the backpack component otherwise.
func (r *batchRun) packNow() []gamestate.SlotItem {
	if r.b.IsOpen() {
		return items(r.st, r.side)
	}
	return items(r.st, PlayerPack)
}

func (r *batchRun) packGen() int {
	com := PlayerPack
	if r.b.IsOpen() {
		com = r.side
	}
	if inv := r.st.Inventory(com); inv != nil {
		return inv.Generation
	}
	return 0
}

func (r *batchRun) satisfied(c fastCheck, pack, bank []gamestate.SlotItem) (done, terminal bool, got int64) {
	if !c.checked {
		return true, true, 0
	}
	switch c.op.Kind {
	case BankOpDeposit:
		left := int64(CountByID(pack, c.op.ID))
		return left == 0, left == 0, 0
	case BankOpWithdraw:
		got = int64(CountByID(pack, c.op.landsAs())) - c.before
		if got < 0 {
			got = 0
		}
		if got >= c.want {
			return true, true, got
		}
		full := IsFull(pack)
		if c.all && got > 0 && (full || (len(bank) > 0 && CountByID(bank, c.op.ID) == 0)) {
			return true, true, got // as many as fit, or the row is empty
		}
		return false, full, got
	}
	return true, true, 0
}

func (r *batchRun) allDone(upTo int) (done, terminal bool) {
	pack, bank := r.packNow(), r.b.Items()
	done, terminal = true, true
	for i := 0; i < upTo && i < len(r.checks); i++ {
		d, t, _ := r.satisfied(r.checks[i], pack, bank)
		done = done && d
		terminal = terminal && (d || t)
	}
	return done, terminal
}

// confirm reads the visit once: everything shows, or the results have certainly all arrived
// (fastSettleTicks after lastTick, the tick count when the last burst went out), or nothing more
// can arrive (a full pack), or the timeout; then it lists what fell short and closes on request.
func (r *batchRun) confirm(out *BankBatch, w Waiter, opt BatchOptions, lastTick, lastGen int) {
	st := r.st
	confirmStart := time.Now()
	timeout := opt.ConfirmTimeout
	if timeout <= 0 {
		timeout = fastConfirmTimeout
	}
	if len(r.packets) > 0 {
		deadline := confirmStart.Add(timeout)
		for {
			// A terminal state (a full pack) ends the wait once a server tick has passed since the
			// last burst; a session that sees no ticks at all (a simulator) settles on a pack update.
			done, terminal := r.allDone(len(r.checks))
			if done || (terminal && (st.Tick() > lastTick || (st.Tick() == 0 && r.packGen() > lastGen))) {
				break
			}
			if st.Tick() >= lastTick+fastSettleTicks || !time.Now().Before(deadline) {
				break
			}
			if !w.Delay(fastPoll) {
				out.Err = ErrBankStopped
				return
			}
		}
	}
	out.Confirm = time.Since(confirmStart)
	if r.pairs > 0 && st.CountDialogOpen() {
		// The P_COUNTDIALOG of a Withdraw-X answered blind arrives after the answer went out; the
		// server's dialog is already resumed, so the client's flag is stale.
		st.ClearDialogInput()
	}
	out.Pack, out.Bank = r.packNow(), r.b.Items()
	for _, c := range r.checks {
		done, _, got := r.satisfied(c, out.Pack, out.Bank)
		if done {
			continue
		}
		short := BankShort{Op: c.op, Want: c.want, Got: got}
		switch {
		case c.op.Kind == BankOpDeposit:
			short.Want, short.Got = int64(CountByID(r.pack0, c.op.ID)), int64(CountByID(r.pack0, c.op.ID)-CountByID(out.Pack, c.op.ID))
			short.Why = "the pack still holds it"
		case IsFull(out.Pack):
			short.Why = "the pack filled"
		default:
			short.Why = "not confirmed: sent, but the pack never showed it"
		}
		out.Short = append(out.Short, short)
	}
	if opt.Close {
		if err := r.b.Close(); err == nil {
			out.Closed = true
		}
	}
}

// answerCountDialogBlind sends RESUME_P_COUNTDIALOG without waiting for the dialog to show: the
// INV_BUTTON5 just sent ahead of it suspends the server's script at p_countdialog in the same
// tick it is read (see the file comment).
func answerCountDialogBlind(s Session, value int32) error {
	if value < 0 {
		value = 0
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.RESUME_P_COUNTDIALOG)
		w.P4(value)
	})
}

// ---- opening ----

// lastBankSide is the bank side panel's inventory component the last time any Bank resolved it
// (every session shares one interface archive), so a mark taken before the bank opens knows which
// inventory to watch. -1 until the first resolution.
var lastBankSide atomic.Int64

func init() { lastBankSide.Store(-1) }

// BankOpenMark is the bank's two inventories as they stood before a booth op. The client keeps
// the last inventories the server sent, so a bank that has just opened shows the previous visit's
// contents until the open's own inventories arrive; the mark tells the two apart.
type BankOpenMark struct {
	bankGen, sideGen int
	sideCom          int
}

// MarkBankOpen reads the mark. Take it just before the booth op.
func MarkBankOpen(s Session) BankOpenMark {
	st := s.State()
	m := BankOpenMark{sideCom: int(lastBankSide.Load())}
	if inv := st.Inventory(BankWithdraw); inv != nil {
		m.bankGen = inv.Generation
	}
	if m.sideCom >= 0 {
		if inv := st.Inventory(m.sideCom); inv != nil {
			m.sideGen = inv.Generation
		}
	}
	return m
}

// OpenedFresh reports whether the bank is open with both inventories sent since m was taken.
func (b *Bank) OpenedFresh(m BankOpenMark) bool {
	if !b.IsOpen() {
		return false
	}
	st := b.s.State()
	bank := st.Inventory(BankWithdraw)
	if bank == nil || bank.Generation <= m.bankGen {
		return false
	}
	side := b.depositComponent()
	if side < 0 {
		return false
	}
	inv := st.Inventory(side)
	if inv == nil {
		return false
	}
	if side == m.sideCom {
		return inv.Generation > m.sideGen
	}
	return inv.Generation > 0
}

// AwaitOpen waits, polling well inside a tick, for the bank to open with fresh inventories after a
// booth op sent once m was taken. A bank that opens but whose inventories never refresh is used as
// it is after fastStaleOpenGrace (fresh false), as the old fixed pause did. open is false when the
// bank did not open within timeout or a stop was requested.
func (b *Bank) AwaitOpen(m BankOpenMark, w Waiter, timeout time.Duration) (open, fresh bool) {
	w = waiterOr(w)
	deadline := time.Now().Add(timeout)
	var openedAt time.Time
	for {
		if b.OpenedFresh(m) {
			return true, true
		}
		now := time.Now()
		if b.IsOpen() {
			if openedAt.IsZero() {
				openedAt = now
			} else if now.Sub(openedAt) >= fastStaleOpenGrace {
				return true, false
			}
		} else {
			openedAt = time.Time{}
			if !now.Before(deadline) {
				return false, false
			}
		}
		if !w.Delay(fastPoll) {
			return false, false
		}
	}
}

// ---- the blind open ----
//
// THE BLIND OPEN brings a bank visit up to MasterCrafter's six-tick gem stream
// (mastercraftrunner/fastgems.go, mastercraftplan/fast.go): the booth op and the deposits go out
// together, aligned so the deposits are read the tick after the bank opens, with no round trip
// between the click and the deposits. The engine facts (engine 289):
//
//   - The engine reads a player's packets until five user events were HANDLED; one its handler
//     refuses does not count towards the five, so the tick reads on past it
//     (engine/entity/NetworkPlayer.ts read: userLimit++ only when the handler succeeded).
//   - The booth opens the bank in the player's interaction step, after every packet of the tick was
//     read (World.processPlayers: processQueues, then processInteraction), so a deposit read in the
//     booth's own tick finds the bank shut and is refused (InvButtonHandler: isComponentVisible) and,
//     being refused, does not stop the read.
//   - So the booth op must be the fifth handled event of its tick. Four CLOSE_MODALs go ahead of it
//     (CloseModalHandler always succeeds and only sets requestModalClose, applied in the queue step,
//     which runs BEFORE the interaction step: they close nothing the booth then opens), all five in
//     ONE message, which the engine buffers whole (web.ts client.buffer), so a tick never reads half
//     of it. The deposits follow as the next messages, five to a message, and are read the next tick
//     with the bank open.
//   - OPLOC itself closes any open modal at once (OpLocHandler: clearPendingAction).
//
// What can put it off the tick boundary: a stray handled event already waiting in the buffer, an
// account that is not yet in reach of the booth (it walks a step and the bank opens a tick late),
// or a delayed account (the OPLOC is refused). Each shows as deposits the pack still holds once
// the bank shows, and those go through Batch: the visit then costs what the paced path costs.

// BankBooth is the booth a blind open clicks: the loc op it answers to (Use-quickly) and the loc.
type BankBooth struct{ Op, X, Z, LocID int }

// blindPad is how many handled user events go ahead of the booth op in its message.
const blindPad = BankMaxPerTick - 1

// collector gathers packets and sends them as one message.
type collector struct {
	s      Session
	builds []func(w *packet.Packet)
}

func (c *collector) Send(build func(w *packet.Packet)) error {
	c.builds = append(c.builds, build)
	return nil
}
func (c *collector) State() *gamestate.State     { return c.s.State() }
func (c *collector) Cheats() *anticheat.Counters { return c.s.Cheats() }
func (c *collector) flush() error {
	builds := c.builds
	c.builds = nil
	if len(builds) == 0 {
		return nil
	}
	return c.s.Send(func(w *packet.Packet) {
		for _, f := range builds {
			f(w)
		}
	})
}

// BlindOpen is what OpenBankBlind did.
type BlindOpen struct {
	// Bank is the open bank, nil when it never opened.
	Bank *Bank
	// Blind is the booth op and the deposits sent ahead together; Fresh the open's own inventories
	// arriving (AwaitOpen).
	Blind, Fresh bool
	// Landed is how many deposit kinds the blind burst banked; Batch the confirm, with whatever the
	// burst missed sent again through Batch once the bank showed.
	Landed int
	Batch  BankBatch
	// OpenTook is the booth op to the bank showing, Took the whole visit.
	OpenTook, Took time.Duration
}

// OpenBankBlind opens the bank at booth and banks every one of each object in deposits (Deposit-All
// per kind): OpenBankBlindOps with deposits only.
func OpenBankBlind(s Session, comps archive.Components, booth BankBooth, deposits []int32, w Waiter, timeout time.Duration) BlindOpen {
	ops := make([]BankOp, 0, len(deposits))
	for _, id := range deposits {
		ops = append(ops, DepositAllOp(id))
	}
	return OpenBankBlindOps(s, comps, booth, ops, w, timeout)
}

// predictedBank is the bank as the next open will show it: the last bank this session was sent,
// compacted to the left in order, which is what the open's ~reorganize_inv(bank) does (content
// general/scripts/misc/inv_procs.rs2 reorganize_inv, bank.rs2 [label,openbank]). It is right as
// long as this account's bank changed only through visits whose final contents the client saw;
// a wrong slot is refused by the engine (inv.hasAt) and comes back through the paced retry.
func predictedBank(st *gamestate.State) []gamestate.SlotItem {
	inv := st.Inventory(BankWithdraw)
	if inv == nil {
		return nil
	}
	rows := inv.NonEmpty()
	for i := range rows {
		rows[i].Slot = i
	}
	return rows
}

// OpenBankBlindOps opens the bank at booth and makes the visit ops (as Batch would: deposits
// first, then note switches and withdrawals in order). The account must already stand where the
// booth can be used without a step: the walk is the caller's. When the ops can be sent blind (the
// bank's side panel is known from an earlier open, the bank is not open, no other caller holds the
// account's movement, and something is to be sent) the booth op goes out as the fifth handled user
// event of its message and the ops follow as the next messages, five events each, read from the
// tick after the open; deposits name the backpack's slots, withdrawals the bank's slots as the
// last bank this session saw predicts them (predictedBank). Otherwise the booth op goes out alone
// and the ops follow the open through Batch. Either way the visit is confirmed once with the bank
// open (its readings are fresh when this returns), and whatever the blind burst fell short of goes
// out again through Batch, against the bank as it opened.
func OpenBankBlindOps(s Session, comps archive.Components, booth BankBooth, ops []BankOp, w Waiter, timeout time.Duration) (out BlindOpen) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	st := s.State()
	bank := NewBank(s, comps)
	mark := MarkBankOpen(s)
	side := int(lastBankSide.Load())
	c := &collector{s: s}
	var r *batchRun
	if side >= 0 && !bank.IsOpen() && !MovementHeldBy(s) && len(ops) > 0 {
		r = bank.plan(ops, c, side, items(st, PlayerPack), predictedBank(st))
	}
	out.Blind = r != nil && len(r.packets) > 0
	events := 0
	if out.Blind {
		for i := 0; i < blindPad; i++ {
			_ = c.Send(func(w *packet.Packet) { w.P1Enc(clientprot.CLOSE_MODAL) })
		}
		if err := OpLoc(c, booth.Op, booth.X, booth.Z, booth.LocID); err != nil {
			out.Batch.Err = err
			return out
		}
		if err := c.flush(); err != nil {
			out.Batch.Err = err
			return out
		}
		// The ops, five user events a message, a Withdraw-X never parted from its count: the
		// engine buffers each message whole, so no tick reads half of one.
		inMsg := 0
		for _, p := range r.packets {
			if inMsg+p.events > BankMaxPerTick {
				if err := c.flush(); err != nil {
					out.Batch.Err = err
					return out
				}
				inMsg = 0
			}
			if err := p.send(); err != nil {
				out.Batch.Err = err
				return out
			}
			inMsg += p.events
			events += p.events
		}
		if err := c.flush(); err != nil {
			out.Batch.Err = err
			return out
		}
		out.Batch.Sent, out.Batch.Chunks = events, (events+BankMaxPerTick-1)/BankMaxPerTick
	} else if err := OpLoc(s, booth.Op, booth.X, booth.Z, booth.LocID); err != nil {
		out.Batch.Err = err
		return out
	}
	open, fresh := bank.AwaitOpen(mark, w, timeout)
	out.OpenTook, out.Fresh = time.Since(start), fresh
	if !open {
		out.Batch.Err = ErrNotOnScreen
		return out
	}
	out.Bank = bank
	if len(ops) == 0 {
		out.Batch.Pack, out.Batch.Bank = bank.Inventory(), bank.Items()
		return out
	}
	if !out.Blind {
		out.Batch = bank.Batch(ops, w, BatchOptions{})
		return out
	}
	// The ops were read from the tick after the open, five a tick: the last of them in the tick
	// whose PLAYER_INFO brings the count to openTick+ticks.
	ticks := (events + BankMaxPerTick - 1) / BankMaxPerTick
	confirmed := out.Batch
	confirmed.Short = append(confirmed.Short, r.unplanned...)
	r.confirm(&confirmed, w, BatchOptions{}, st.Tick()+ticks-1, r.packGen())
	if errors.Is(confirmed.Err, ErrBankStopped) {
		out.Batch = confirmed
		return out
	}
	out.Landed = len(r.checks) - len(confirmed.Short)
	if len(confirmed.Short) == 0 {
		out.Batch = confirmed
		return out
	}
	// The paced retry of what the blind burst missed, against the bank as it opened.
	var retry []BankOp
	noted := false
	for _, sh := range confirmed.Short {
		switch op := sh.Op; {
		case op.Kind == BankOpDeposit:
			retry = append(retry, DepositAllOp(op.ID))
		case op.Kind == BankOpWithdraw && sh.Why != "the pack filled":
			if sh.Why != "the bank holds none" && op.Count > 0 {
				op.Count = int32(sh.Want - sh.Got)
				if op.Count <= 0 {
					continue
				}
			}
			noted = noted || (op.LandsAs != 0 && op.LandsAs != op.ID)
			retry = append(retry, op)
		}
	}
	if len(retry) == 0 {
		out.Batch = confirmed
		return out
	}
	if noted {
		retry = append(append([]BankOp{NoteModeOp(true)}, retry...), NoteModeOp(false))
	}
	again := bank.Batch(retry, w, BatchOptions{})
	again.Sent += confirmed.Sent
	again.Chunks += confirmed.Chunks
	for _, sh := range confirmed.Short {
		if sh.Op.Kind == BankOpWithdraw && sh.Why == "the pack filled" {
			again.Short = append(again.Short, sh)
		}
	}
	out.Batch = again
	return out
}

// String is a one-line account of the open for a log.
func (o BlindOpen) String() string {
	mode := "paced"
	if o.Blind {
		mode = "blind"
	}
	s := fmt.Sprintf("bank open (%s): open in %s, %d op(s) done blind, %s in all", mode, o.OpenTook.Round(time.Millisecond), o.Landed, o.Took.Round(time.Millisecond))
	if o.Batch.Sent > 0 || o.Batch.Err != nil || len(o.Batch.Short) > 0 {
		s += "; then " + o.Batch.String()
	}
	return s
}

// ---- drop-in fast versions of the one-at-a-time calls ----

// awaitSide waits briefly for the side panel's inventory to exist at all (it can land a moment
// after the modal opens), the same allowance DepositAllMatching makes, but without waiting on an
// empty pack that has already arrived.
func (b *Bank) awaitSide(w Waiter) {
	if _, inv := b.InventorySnapshot(); inv != nil {
		return
	}
	w.DelayUntil(func() bool {
		_, inv := b.InventorySnapshot()
		return inv != nil || !b.IsOpen()
	}, bankSideReadyWait)
}

// BatchDepositAllMatching is DepositAllMatching made fast: Deposit-All on every matching kind at
// once, confirmed once; whatever the batch left behind goes through DepositAllMatching, one at a
// time, as before.
func (b *Bank) BatchDepositAllMatching(match func(gamestate.SlotItem) bool, w Waiter) error {
	w = waiterOr(w)
	if !b.IsOpen() {
		return ErrNotOnScreen
	}
	b.awaitSide(w)
	var ops []BankOp
	seen := map[int32]bool{}
	for _, it := range b.Inventory() {
		if !seen[it.ID] && match(it) {
			seen[it.ID] = true
			ops = append(ops, DepositAllOp(it.ID))
		}
	}
	if len(ops) == 0 {
		return nil
	}
	r := b.Batch(ops, w, BatchOptions{})
	if r.OK() {
		return nil
	}
	if errors.Is(r.Err, ErrBankStopped) {
		return r.Err
	}
	return b.DepositAllMatching(match, w)
}

// BatchDepositInventory is DepositInventory made fast (BatchDepositAllMatching on everything).
func (b *Bank) BatchDepositInventory(w Waiter) error {
	return b.BatchDepositAllMatching(func(gamestate.SlotItem) bool { return true }, w)
}

// QuickWithdrawX is WithdrawX with the count sent behind the button (no wait for the dialog) and
// one confirm: a round trip less than WithdrawX for any count but 1, 5 and 10. What it falls short
// of is drawn by WithdrawX, as before.
func (b *Bank) QuickWithdrawX(objID, n int32, w Waiter) error {
	return b.quickWithdraw(WithdrawOp(objID, n), w)
}

// QuickWithdrawXAs is WithdrawXAs (landsAsID arriving, a note in note mode) made the same way.
func (b *Bank) QuickWithdrawXAs(objID, n, landsAsID int32, w Waiter) error {
	return b.quickWithdraw(WithdrawAsOp(objID, n, landsAsID), w)
}

func (b *Bank) quickWithdraw(op BankOp, w Waiter) error {
	w = waiterOr(w)
	if op.Count <= 0 {
		return nil
	}
	r := b.Batch([]BankOp{op}, w, BatchOptions{})
	if r.Err != nil {
		return r.Err
	}
	short, isShort := r.ShortOf(BankOpWithdraw, op.ID)
	if !isShort {
		return nil
	}
	switch {
	case short.Why == "the bank holds none":
		return ErrItemNotFound
	case short.Got > 0 && IsFull(r.Pack):
		return nil // WithdrawX also ends when the pack fills after some arrived
	}
	rest := int32(short.Want - short.Got)
	if rest <= 0 {
		return nil
	}
	if op.LandsAs != 0 && op.LandsAs != op.ID {
		return b.WithdrawXAs(op.ID, rest, op.LandsAs, w)
	}
	return b.WithdrawX(op.ID, rest, w)
}
