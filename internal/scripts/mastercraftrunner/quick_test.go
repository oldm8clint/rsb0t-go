package mastercraftrunner

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/tile"
)

// tickEngine plays the engine's side of a bank booth and of the chisel and bolt-tip uses TICK BY
// TICK, as engine 289 runs them (engine/src/engine/World.ts cycle, entity/NetworkPlayer.ts,
// entity/Player.ts), so the quick bank's and the pipeline's tick counts can be measured:
//
//   - client packets queue; each tick reads them in order until five user events were handled
//     (one that fails counts as a client event, at most 20), the rest wait (NetworkPlayer.decodeIn);
//   - OPLOC clears any pending action, closing a modal, and sets the interaction
//     (OpLocHandler); the booth opens the bank in the player's turn, after the reads, unless a
//     modal is open (processInteraction needs canAccess) or a later OPHELDU cleared it;
//   - INV_BUTTON is refused unless the bank interface is open (InvButtonHandler: isComponentVisible);
//     Deposit-All banks every one of the item, Withdraw-All fills the free slots (bank.rs2);
//   - CLOSE_MODAL closes the bank in the player's queue step, the same tick (CloseModalHandler,
//     Player.processQueues);
//   - OPHELDU is validated against the slots, clears the pending action, and runs at once: the cut
//     removes the lowest slot holding the item and adds the product to the lowest empty slot or its
//     stack (uncut_gem.rs2, bolts.rs2).
//   - OPHELDT (Superheat Item on iron ore) is validated against the slot, clears the pending action
//     (closing the bank), makes a steel bar with two coal in the pack (else an iron bar), and
//     p_delay(1)s the player: delayed until two ticks on, cleared in the player's turn, after the
//     reads. Every op handler refuses a packet read while the player is delayed.
//
// Every Delay is one tick; the server's output (modal, inventories, stats, PLAYER_INFO) is applied
// to the client at the end of it.
type tickEngine struct {
	st      *gamestate.State
	chat    *chatlog.Log
	cheats  anticheat.Counters
	pack    [28]struct{ id, n int32 }
	bank    []struct{ id, n int32 }
	pending [][]byte

	open, wantOpen, closeReq bool
	craftXP, fletchXP        int // tenths

	ticks, opens int
	// usesTick is the uses applied each tick, by tick; cutTicks the tick of every ruby cut, in order;
	// refused the events the engine refused.
	usesTick map[int]int
	cutTicks []int
	refused  int
	// ops is every user event the engine handled, in the order it read them: "withdraw-all:<obj>",
	// "withdraw-x:<obj>", "count:<n>", "deposit-all:<obj>", "use", "close" and "booth". A test of what a
	// load sends (its order, not only its tick count) reads it.
	ops []string

	// levelUps opens the level-up chat box when a cut gains a level (ADVANCESTAT from the engine
	// queue, which needs no modal open and runs before the interaction); chat is that box.
	levelUps, levelPending, chatBox bool
	levelUpsSeen                    int
	// countFor is the item a Withdraw-X's suspended script is waiting to draw (0: no count dialog).
	countFor int32

	// delayed and delayedUntil are a superheat cast's p_delay(1); magicXP and smithXP its XP
	// (tenths); castTicks the tick of every cast that landed; lag delays every frame that many
	// more ticks (a slow link), lagOf, when set, picks it frame by frame.
	delayed          bool
	delayedUntil     int
	magicXP, smithXP int
	castTicks        []int
	lag              int
	lagOf            func(frame int) int

	// The network: frames is what is in flight, each a message of packets that reaches the
	// engine's buffer whole at the start of tick at. lateFrame makes the frames numbered in it
	// arrive a tick late; splitFrame delivers the frame numbered in it in two halves, a tick apart
	// (a batch straddling a tick, which a real websocket frame never does); strayAt puts a stray
	// handled user event (a CLOSE_MODAL) ahead of everything at that tick.
	frames     []tFrame
	sent       int
	lateFrame  map[int]bool
	splitFrame map[int]bool
	strayAt    map[int]bool
	badBytes   int
}

type tFrame struct {
	at   int
	pkts [][]byte
}

const (
	tBoothX, tBoothZ, tBoothID = 3212, 3213, 2213
	tSideRoot, tSideInv        = 9000, 9001
)

// tLen is every packet length the rounds send, opcode included (the sim turns the anti-cheat
// side-packets off, so a frame is nothing but these).
var tLen = map[int]int{
	clientprot.OPHELDU: 13, clientprot.OPLOC2: 7, clientprot.INV_BUTTON4: 7, clientprot.CLOSE_MODAL: 1,
	clientprot.INV_BUTTON5: 7, clientprot.RESUME_P_COUNTDIALOG: 5, clientprot.OPHELDT: 9,
}

// The ore and bars the sim superheats (content ids).
const (
	tIronOre  int32 = 440
	tIronBar  int32 = 2351
	tSteelBar int32 = 2353
)

// The bows the sim strings: ids of its own (bowstringrunner.ResolveIDs reads them from the cache).
const (
	tBowU, tBowStrung, tString int32 = 66, 855, 1777
	tBowXP                           = 750 // a Yew longbow's tenths
)

// Send is one message: its packets reach the engine's buffer together at the next tick.
func (e *tickEngine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		n, ok := tLen[int(b[0])]
		if !ok || n > len(b) {
			e.badBytes++
			pkts = append(pkts, b)
			break
		}
		pkts = append(pkts, b[:n])
		b = b[n:]
	}
	at := e.ticks + 1 + e.lag
	if e.lagOf != nil {
		at += e.lagOf(e.sent)
	}
	if e.lateFrame[e.sent] {
		at++
	}
	if e.splitFrame[e.sent] && len(pkts) > 2 {
		e.frames = append(e.frames, tFrame{at, pkts[:2]}, tFrame{at + 1, pkts[2:]})
	} else {
		e.frames = append(e.frames, tFrame{at, pkts})
	}
	e.sent++
	return nil
}
func (e *tickEngine) State() *gamestate.State     { return e.st }
func (e *tickEngine) Cheats() *anticheat.Counters { return &e.cheats }

func tg2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func stackable(id int32) bool {
	switch id {
	case pearlplan.TipsID, pearlplan.BoltID, pearlplan.PearlBoltID, mastercraftplan.NatureRuneID, mastercraftplan.FireRuneID:
		return true
	}
	return false
}

func (e *tickEngine) packTotal(id int32) int32 {
	var n int32
	for _, s := range e.pack {
		if s.id == id {
			n += s.n
		}
	}
	return n
}

func (e *tickEngine) packDel(id int32, n int32) {
	for i := range e.pack {
		if n <= 0 {
			return
		}
		if e.pack[i].id != id {
			continue
		}
		take := min(n, e.pack[i].n)
		e.pack[i].n -= take
		n -= take
		if e.pack[i].n == 0 {
			e.pack[i].id = 0
		}
	}
}

// packAdd adds n of id as content's inv_add does; it reports how many went in.
func (e *tickEngine) packAdd(id int32, n int32) int32 {
	if stackable(id) {
		for i := range e.pack {
			if e.pack[i].id == id {
				e.pack[i].n += n
				return n
			}
		}
		for i := range e.pack {
			if e.pack[i].id == 0 {
				e.pack[i].id, e.pack[i].n = id, n
				return n
			}
		}
		return 0
	}
	var in int32
	for i := range e.pack {
		if in == n {
			break
		}
		if e.pack[i].id == 0 {
			e.pack[i].id, e.pack[i].n = id, 1
			in++
		}
	}
	return in
}

func (e *tickEngine) bankAdd(id int32, n int32) {
	for i := range e.bank {
		if e.bank[i].id == id {
			e.bank[i].n += n
			return
		}
	}
	e.bank = append(e.bank, struct{ id, n int32 }{id, n})
}

func (e *tickEngine) bankTake(id int32, n int32) int32 {
	for i := range e.bank {
		if e.bank[i].id == id {
			take := min(n, e.bank[i].n)
			e.bank[i].n -= take
			return take
		}
	}
	return 0
}

// read handles one queued packet and reports whether it was a handled user event.
func (e *tickEngine) read(b []byte) bool {
	n := len(b)
	switch {
	case n >= 13 && int(b[n-13]) == clientprot.OPHELDU:
		b = b[n-13:]
		if e.delayed { // OpHeldUHandler: cannot interact while delayed
			return false
		}
		obj, slot, useObj, useSlot := int32(tg2(b, 1)), tg2(b, 3), int32(tg2(b, 7)), tg2(b, 9)
		if slot >= 28 || useSlot >= 28 {
			return false
		}
		// clearPendingAction: a use whose items are not in the named slots is refused but still
		// clears the pending action and closes a modal (OpHeldUHandler, the hasAt branches).
		e.wantOpen, e.open, e.chatBox, e.countFor = false, false, false, 0
		if e.pack[slot].id != obj || e.pack[useSlot].id != useObj {
			return false
		}
		e.usesTick[e.ticks]++
		switch {
		case obj == tBowU && useObj == tString: // bows.rs2 [label,string_bow]
			e.packDel(tBowU, 1)
			e.packDel(tString, 1)
			e.packAdd(tBowStrung, 1)
			before := levels.LevelFromXP(e.fletchXP / 10)
			e.fletchXP += tBowXP
			if e.levelUps && levels.LevelFromXP(e.fletchXP/10) > before {
				e.levelPending = true
			}
			e.cutTicks = append(e.cutTicks, e.ticks)
		case useObj == gemplan.ChiselID && obj == rubyU:
			e.packDel(rubyU, 1)
			e.packAdd(rubyCut, 1)
			before := levels.LevelFromXP(e.craftXP / 10)
			e.craftXP += 850
			if e.levelUps && levels.LevelFromXP(e.craftXP/10) > before {
				e.levelPending = true
			}
			e.cutTicks = append(e.cutTicks, e.ticks)
		case useObj == gemplan.ChiselID && (obj == pearlplan.PearlID || obj == pearlplan.PearlsID):
			k, _ := pearlplan.KindByID(obj)
			e.packDel(obj, 1)
			e.packAdd(pearlplan.TipsID, int32(k.Tips))
			before := levels.LevelFromXP(e.fletchXP / 10)
			e.fletchXP += pearlplan.XPTenths
			if e.levelUps && levels.LevelFromXP(e.fletchXP/10) > before {
				e.levelPending = true
			}
			e.cutTicks = append(e.cutTicks, e.ticks)
		case useObj == pearlplan.TipsID && obj == pearlplan.BoltID:
			c := min(int32(pearlplan.BoltsPerUse), e.packTotal(pearlplan.TipsID), e.packTotal(pearlplan.BoltID))
			e.packDel(pearlplan.TipsID, c)
			e.packDel(pearlplan.BoltID, c)
			e.packAdd(pearlplan.PearlBoltID, c)
			e.fletchXP += int(c) * pearlplan.XPTenths
		}
		e.ops = append(e.ops, "use")
		return true
	case n == 9 && int(b[0]) == clientprot.OPHELDT:
		// OpHeldTHandler, then superheat.rs2 on iron ore.
		obj, slot := int32(tg2(b, 1)), tg2(b, 3)
		if e.delayed || slot >= 28 || e.pack[slot].id != obj || obj != tIronOre || e.packTotal(mastercraftplan.NatureRuneID) < 1 {
			return false
		}
		e.wantOpen, e.open, e.chatBox, e.countFor = false, false, false, 0 // clearPendingAction
		e.packDel(tIronOre, 1)
		if e.packTotal(mastercraftplan.CoalID) > 1 {
			e.packDel(mastercraftplan.CoalID, 2)
			e.packAdd(tSteelBar, 1)
			e.smithXP += 175
		} else {
			e.packAdd(tIronBar, 1)
			e.smithXP += 125
		}
		e.packDel(mastercraftplan.NatureRuneID, 1)
		e.magicXP += mastercraftplan.SuperheatXPTenths
		e.delayed, e.delayedUntil = true, e.ticks+2
		e.castTicks = append(e.castTicks, e.ticks)
		return true
	case n >= 7 && int(b[n-7]) == clientprot.OPLOC2:
		b = b[n-7:]
		if e.delayed { // OpLocHandler: cannot interact while delayed
			return false
		}
		if tg2(b, 1) != tBoothX || tg2(b, 3) != tBoothZ || tg2(b, 5) != tBoothID {
			return false
		}
		e.open, e.chatBox, e.countFor = false, false, 0 // clearPendingAction closes a modal
		e.wantOpen = true
		e.ops = append(e.ops, "booth")
		return true
	case n >= 7 && int(b[n-7]) == clientprot.INV_BUTTON4:
		b = b[n-7:]
		obj, slot, com := int32(tg2(b, 1)), tg2(b, 3), tg2(b, 5)
		if e.delayed || !e.open { // InvButtonHandler: delayed, or the bank's components are not visible
			return false
		}
		switch com {
		case actions.BankWithdraw:
			if slot >= len(e.bank) || e.bank[slot].id != obj || e.bank[slot].n <= 0 {
				return false
			}
			free := int32(0)
			for _, s := range e.pack {
				if s.id == 0 {
					free++
				}
			}
			want := free
			if stackable(obj) {
				want = 1 << 30
			}
			got := e.bankTake(obj, want)
			e.packAdd(obj, got)
			e.ops = append(e.ops, fmt.Sprintf("withdraw-all:%d", obj))
		case tSideInv:
			if slot >= 28 || e.pack[slot].id != obj {
				return false
			}
			n := e.packTotal(obj)
			e.packDel(obj, n)
			e.bankAdd(obj, n)
			e.ops = append(e.ops, fmt.Sprintf("deposit-all:%d", obj))
		default:
			return false
		}
		return true
	case n == 7 && int(b[0]) == clientprot.INV_BUTTON5:
		// Withdraw-X: bank.rs2 @bank_withdraw stops at p_countdialog (the script suspended).
		obj, slot, com := int32(tg2(b, 1)), tg2(b, 3), tg2(b, 5)
		if e.delayed || !e.open || com != actions.BankWithdraw || slot >= len(e.bank) || e.bank[slot].id != obj || e.bank[slot].n <= 0 {
			return false
		}
		e.countFor = obj
		e.ops = append(e.ops, fmt.Sprintf("withdraw-x:%d", obj))
		return true
	case n == 5 && int(b[0]) == clientprot.RESUME_P_COUNTDIALOG:
		// ResumePCountDialogHandler: refused unless a script waits on the dialog; else it draws.
		if e.countFor == 0 {
			return false
		}
		want := int32(uint32(b[1])<<24 | uint32(b[2])<<16 | uint32(b[3])<<8 | uint32(b[4]))
		free := int32(0)
		for _, s := range e.pack {
			if s.id == 0 {
				free++
			}
		}
		e.packAdd(e.countFor, e.bankTake(e.countFor, min(want, free)))
		e.countFor = 0
		e.ops = append(e.ops, fmt.Sprintf("count:%d", want))
		return true
	case n >= 1 && int(b[n-1]) == clientprot.CLOSE_MODAL:
		e.closeReq = true
		e.ops = append(e.ops, "close")
		return true
	}
	return false
}

func (e *tickEngine) tick() {
	e.ticks++
	if e.strayAt[e.ticks] {
		e.pending = append([][]byte{{byte(clientprot.CLOSE_MODAL)}}, e.pending...)
	}
	for len(e.frames) > 0 && e.frames[0].at <= e.ticks { // the network delivers in order
		e.pending = append(e.pending, e.frames[0].pkts...)
		e.frames = e.frames[1:]
	}
	user, client := 0, 0
	wasOpen := e.open
	for len(e.pending) > 0 && user < 5 && client < 20 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		if e.read(b) {
			user++
		} else {
			client++
			e.refused++
		}
	}
	if e.delayed && e.ticks >= e.delayedUntil { // processPlayers, after the reads
		e.delayed = false
	}
	if e.closeReq { // processQueues
		e.closeReq = false
		e.open, e.countFor = false, 0
	}
	if e.levelPending && !e.open && !e.chatBox { // processEngineQueue: ADVANCESTAT opens the box
		e.levelPending = false
		e.chatBox = true
		e.levelUpsSeen++
	}
	if e.wantOpen && !e.open && !e.chatBox && !e.delayed { // processInteraction (canAccess: no modal, not delayed)
		e.wantOpen = false
		e.open = true
		e.opens++
	}
	// output
	switch {
	case e.open && !wasOpen:
		e.st.Handle(serverprot.IF_OPENMAIN_SIDE, []byte{byte(actions.BankMainModal >> 8), byte(actions.BankMainModal & 0xff), byte(tSideRoot >> 8), byte(tSideRoot & 0xff)})
	case !e.open && wasOpen:
		e.st.Handle(serverprot.IF_CLOSE, nil)
	}
	e.push()
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *tickEngine) writeInv(com int, rows []struct{ id, n int32 }) {
	p := packet.NewWriter(512)
	p.P2(uint16(com))
	p.P2(uint16(len(rows)))
	for _, r := range rows {
		if r.id == 0 || r.n <= 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(r.id + 1))
		if r.n >= 255 {
			p.P1(255)
			p.P4(r.n)
		} else {
			p.P1(byte(r.n))
		}
	}
	e.st.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

func (e *tickEngine) push() {
	e.writeInv(actions.PlayerPack, e.pack[:])
	if e.open {
		e.writeInv(tSideInv, e.pack[:])
		e.writeInv(actions.BankWithdraw, e.bank)
	}
	stat := func(id, tenths int) {
		xp := tenths / 10
		e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(id), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), 99})
	}
	stat(gemrunner.CraftingStat, e.craftXP)
	stat(bowstringrunner.FletchingStat, e.fletchXP)
	stat(MagicStat, e.magicXP)
	stat(SmithingStat, e.smithXP)
}

type tBit struct {
	n int
	v uint32
}

func tBitPack(total int, fields ...tBit) []byte {
	buf := make([]byte, total)
	pos := 0
	for _, f := range fields {
		n, v := f.n, f.v
		for n > 0 {
			free := 8 - pos&7
			take := min(n, free)
			chunk := (v >> uint(n-take)) & ((1 << uint(take)) - 1)
			buf[pos>>3] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

// newTickEngine is an account standing south of the booth at 3212,3212 with pack in its slots
// (chisel first) and bank in its bank, and the Deps a round runs on. Every Delay is one tick.
func newTickEngine(t *testing.T, pack, bank []struct{ id, n int32 }) (*tickEngine, *fleetrunner.Deps, *QuickBank) {
	t.Helper()
	e := &tickEngine{st: gamestate.New(), chat: chatlog.New(), usesTick: map[int]int{}}
	e.cheats.SetEnabled(false) // a frame is then nothing but the round's own packets
	copy(e.pack[:], pack)
	e.bank = append(e.bank, bank...)
	e.st.Handle(serverprot.REBUILD_NORMAL, []byte{400 >> 8, 400 & 0xff, 400 >> 8, 400 & 0xff}) // origin 3152,3152
	e.st.Handle(serverprot.PLAYER_INFO, tBitPack(5, tBit{1, 1}, tBit{2, 3}, tBit{2, 0}, tBit{7, 60}, tBit{7, 60}, tBit{1, 0}, tBit{1, 0}, tBit{8, 0}, tBit{11, 2047}))
	if x, z, _, ok := e.st.Tile(); !ok || x != 3212 || z != 3212 {
		t.Fatalf("tile %d,%d %v", x, z, ok)
	}
	e.st.Handle(serverprot.IF_SETICON, []byte{byte(3213 >> 8), byte(3213 & 0xff), 3})
	e.push()
	comps := archive.Components{
		3213:      {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214:      {ID: 3214, Type: archive.TypeInv, ObjOps: true},
		tSideRoot: {ID: tSideRoot, Children: []int{tSideInv}},
		tSideInv:  {ID: tSideInv, LayerID: tSideRoot, Type: archive.TypeInv, Iop: [5]string{"Deposit-1", "Deposit-5", "Deposit-10", "Deposit-All", "Deposit-X"}},
	}
	d := &fleetrunner.Deps{
		Session: e, Components: comps, Chat: e.chat,
		Delay: func(time.Duration) bool { e.tick(); return true },
	}
	q := &QuickBank{}
	q.Teach(locindex.Match{Loc: locindex.Loc{ID: tBoothID, X: tBoothX, Z: tBoothZ}, Op: 2, Distance: 1}, tile.Tile{X: 3212, Z: 3212}, tSideInv)
	return e, d, q
}

func load(id int32, n int) []struct{ id, n int32 } {
	out := []struct{ id, n int32 }{{gemplan.ChiselID, 1}}
	for i := 0; i < n; i++ {
		out = append(out, struct{ id, n int32 }{id, 1})
	}
	return out
}

// THE POINT: a load of 27 gems is SEVEN engine ticks, bank visit included: the deposit, the
// Withdraw-All and the close in one tick, the 27 cuts five a tick over the next six with the next
// booth op behind the last two, and the bank open in that same sixth tick.
func TestQuickBankGemLoadIsSevenTicks(t *testing.T) {
	ruby, _ := gemplan.GemByUncutID(rubyU)
	e, d, q := newTickEngine(t, load(rubyU, 27), []struct{ id, n int32 }{{rubyU, 270}})
	gd := GemDeps(d)
	pick := func(uncut map[string]int) (gemplan.Gem, bool) { return ruby, uncut[ruby.Key] > 0 }
	const loads = 5
	start := e.ticks
	cut := 0
	for i := 0; i < loads; i++ {
		r := gemrunner.CutAllThen(gd, ruby, func() { q.Prime(d) })
		if r.Reason != "done" || r.Cuts != 27 {
			t.Fatalf("load %d: %+v", i, r)
		}
		cut += r.Cuts
		v, qv := GemQuickVisit(d, q, pick)
		if !qv.Done || v.Drawn != 27 || v.Gem.Key != "ruby" || len(qv.Deposited) != 1 || qv.Deposited[0] != rubyCut {
			t.Fatalf("load %d: visit %+v / %+v", i, v, qv)
		}
	}
	per := float64(e.ticks-start) / loads
	if per != 7 {
		t.Fatalf("a load took %.1f ticks, want 7", per)
	}
	if e.opens != loads {
		t.Fatalf("the bank opened %d times for %d loads", e.opens, loads)
	}
	for tick, n := range e.usesTick {
		if n > 5 {
			t.Fatalf("tick %d applied %d uses, over the engine's five", tick, n)
		}
	}
	if got := e.bank[0].n; got != 270-27*loads {
		t.Fatalf("bank holds %d uncut, want %d", got, 270-27*loads)
	}
	if cut != 27*loads || e.craftXP != 850*27*loads {
		t.Fatalf("cut %d, XP %d", cut, e.craftXP)
	}
}

// Uses the engine dropped leave uncut gems among the cut ones: the visit banks them with the rest and
// draws a full 27 again.
func TestQuickBankAfterAPartialLoad(t *testing.T) {
	pack := []struct{ id, n int32 }{{gemplan.ChiselID, 1}}
	for i := 0; i < 25; i++ {
		pack = append(pack, struct{ id, n int32 }{rubyCut, 1})
	}
	pack = append(pack, struct{ id, n int32 }{rubyU, 1}, struct{ id, n int32 }{rubyU, 1})
	e, d, q := newTickEngine(t, pack, []struct{ id, n int32 }{{rubyU, 100}})
	ruby, _ := gemplan.GemByUncutID(rubyU)
	v, qv := GemQuickVisit(d, q, func(uncut map[string]int) (gemplan.Gem, bool) { return ruby, uncut[ruby.Key] > 0 })
	if !qv.Done || v.Drawn != 27 || e.packTotal(rubyCut) != 0 {
		t.Fatalf("%+v / %+v", v, qv)
	}
	if e.bank[0].n != 100+2-27 {
		t.Fatalf("bank %d uncut", e.bank[0].n)
	}
}

// A visit made from anywhere but the tile the bank last opened from is refused, for a full one.
func TestQuickBankOnlyFromTheStandTile(t *testing.T) {
	_, d, q := newTickEngine(t, load(rubyU, 0), nil)
	if !q.Ready(d) {
		t.Fatal("not ready on the stand tile")
	}
	q.stand.X++
	if q.Ready(d) {
		t.Fatal("ready off the stand tile")
	}
	if v := q.Visit(d, KeepChisel, nil); v.Done || v.Err == "" {
		t.Fatalf("%+v", v)
	}
}

// A bank that never opens (the booth op lost) ends the visit short, for the caller's full visit.
func TestQuickBankFallsShortWhenTheBankDoesNotOpen(t *testing.T) {
	e, d, q := newTickEngine(t, load(rubyU, 0), []struct{ id, n int32 }{{rubyU, 10}})
	q.booth.ID++ // the engine refuses the op
	v := q.Visit(d, KeepChisel, func([]gamestate.SlotItem) []int32 { return []int32{rubyU} })
	if v.Opened || v.Done || v.Err == "" {
		t.Fatalf("%+v", v)
	}
	if e.packTotal(rubyU) != 0 {
		t.Fatal("drew without a bank")
	}
}

// Pearls go the same way: 27 Oyster pearls a load, 24 tips each, seven ticks a load.
func TestQuickBankPearlLoads(t *testing.T) {
	e, d, q := newTickEngine(t, load(pearlplan.PearlsID, 27), []struct{ id, n int32 }{{pearlplan.PearlsID, 54}, {pearlplan.PearlID, 10}})
	gd := GemDeps(d)
	start := e.ticks
	cut, tips := 0, 0
	for i := 0; i < 4; i++ {
		lv := q.Visit
		kind, _ := pearlplan.KindByID(firstPearl(e))
		r := CutPearls(gd, kind, func() { q.Prime(d) })
		if r.Reason != "done" {
			t.Fatalf("load %d: %+v", i, r)
		}
		cut, tips = cut+r.Cut, tips+r.Tips
		if v := lv(d, KeepChisel, PearlDraw); !v.Done {
			t.Fatalf("load %d: %+v", i, v)
		}
	}
	if cut != 27+27+27+10 || tips != 81*24+10*6 {
		t.Fatalf("cut %d pearls into %d tips", cut, tips)
	}
	if e.ticks-start > 4*7 {
		t.Fatalf("four loads took %d ticks", e.ticks-start)
	}
	if e.fletchXP != cut*pearlplan.XPTenths {
		t.Fatalf("Fletching XP %d tenths, want %d", e.fletchXP, cut*pearlplan.XPTenths)
	}
	// Every tip was banked by the last visit; nothing left to cut.
	if PearlDraw(asSlots(e.bank)) != nil {
		t.Fatal("pearls left in the bank")
	}
}

func firstPearl(e *tickEngine) int32 {
	for _, s := range e.pack {
		if _, ok := pearlplan.KindByID(s.id); ok {
			return s.id
		}
	}
	return 0
}

func asSlots(rows []struct{ id, n int32 }) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for i, r := range rows {
		if r.n > 0 {
			out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: r.id, Count: r.n}})
		}
	}
	return out
}

// Tipping bolts: five uses a tick on the same two stacks, ten bolts a use: fifty a tick.
func TestMakeBoltsFiftyATick(t *testing.T) {
	pack := []struct{ id, n int32 }{{gemplan.ChiselID, 1}, {pearlplan.TipsID, 1200}, {pearlplan.BoltID, 1000}}
	e, d, _ := newTickEngine(t, pack, nil)
	gd := GemDeps(d)
	holds := 0
	r := MakeBolts(gd, func() bool { holds++; return true })
	if r.Reason != "done" || r.Made != 1000 || r.Uses != 100 {
		t.Fatalf("%+v", r)
	}
	if busy := len(e.usesTick); busy != 20 {
		t.Fatalf("%d ticks did the work, want 20 (1000 bolts, 50 a tick)", busy)
	}
	if e.packTotal(pearlplan.TipsID) != 200 || e.packTotal(pearlplan.BoltID) != 0 || e.packTotal(pearlplan.PearlBoltID) != 1000 {
		t.Fatalf("pack: %d tips, %d bolts, %d pearl bolts", e.packTotal(pearlplan.TipsID), e.packTotal(pearlplan.BoltID), e.packTotal(pearlplan.PearlBoltID))
	}
	if e.fletchXP != 1000*pearlplan.XPTenths || r.XP != 3200 {
		t.Fatalf("XP %d tenths, result %d", e.fletchXP, r.XP)
	}
	if holds < 20 {
		t.Fatalf("hold ran %d times, want every tick", holds)
	}
}

// hold saying stop ends the run at once.
func TestMakeBoltsStopsWhenHeld(t *testing.T) {
	pack := []struct{ id, n int32 }{{pearlplan.TipsID, 1200}, {pearlplan.BoltID, 1000}}
	_, d, _ := newTickEngine(t, pack, nil)
	n := 0
	r := MakeBolts(GemDeps(d), func() bool { n++; return n <= 3 })
	if r.Reason != "held" || r.Uses != 15 {
		t.Fatalf("%+v", r)
	}
}
