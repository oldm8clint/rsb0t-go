// Package actionsim is TEST SUPPORT: a fake actions.Session that plays the server's side of a
// bank booth and a trade screen closely enough for the mule/donor kit code to be unit-tested
// without an engine. Nothing outside a _test.go file imports it.
//
// It models exactly what internal/actions reads and sends, no more: the bank main modal with a
// side panel whose inventory component offers "Deposit"; Withdraw 1/5/10/All/X (the count dialog)
// and Deposit 1/All; the Note/Item buttons and the bankcert varp; and a trade offer screen, a
// confirm screen and the close, with the partner's side scripted by the test. Every change is
// pushed to the client state as the server would push it, the pack to both the side panel and
// the fixed backpack component 3214.
package actionsim

import (
	"sync"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// The fake side panel's ids: a root and its one inventory child.
const (
	SideRoot = 9000
	SideInv  = 9001
)

// Components is an interface archive holding just the bank side panel.
func Components() archive.Components {
	return archive.Components{
		SideRoot: {ID: SideRoot, Children: []int{SideInv}},
		SideInv:  {ID: SideInv, LayerID: SideRoot, Type: archive.TypeInv, Iop: [5]string{"Deposit-1", "Deposit-5", "Deposit-10", "Deposit-All", "Deposit-X"}},
	}
}

// Stack is one inventory row.
type Stack struct {
	ID    int32
	Count int
}

// Objs says what the server knows about an item: whether it stacks, and its note/unnoted pair.
type Objs struct {
	Stackable func(id int32) bool
	// NoteOf is the note of an item (0: none); UnnoteOf the item of a note (0: not a note).
	NoteOf   func(id int32) int32
	UnnoteOf func(id int32) int32
}

// Session is the fake. The zero value is not usable; call New.
type Session struct {
	mu    sync.Mutex
	st    *gamestate.State
	objs  Objs
	pack  []Stack // 28 slots; ID 0 is empty
	bank  []Stack
	cert  bool
	xFor  int32 // the item a Withdraw-X is waiting on
	trade *Trade
	// The held.go half: the worn panel, where the player stands and the scene origin, the
	// ground items as [id, x, z], and the item-on-item rules a test scripted.
	worn    []Stack
	here    [2]int
	origin  [2]int
	ground  [][3]int
	combine map[[2]int32]int32
	// wearPos is SetWearPos's table: an item's worn slot.
	wearPos map[int32]int
	// bury is Bury's table: the experience of burying an item, in tenths of a point, and the stat it advances.
	bury               map[int32]int
	buryStat, buryXP10 int
	// shop is the open shop (shop.go), nil when none.
	shop *shopState
	// delayLeft is how many more ticks the player is delayed (p_delay): every op that checks
	// Player.delayed is refused until it runs out.
	delayLeft int
	// OnSend, when set, sees every packet after the simulator has handled it.
	OnSend func(b []byte)
	sent   [][]byte
	cheats anticheat.Counters
	// srvBank is the bank open on the server's side: the client's own view closes the moment it
	// sends CLOSE_MODAL, the server's when it reads it.
	srvBank bool
	// Ticked mode (SetTicked): packets wait in inbox until Tick reads at most UserEventsPerTick of
	// them, as the engine does; perTick records how many each tick read, maxBacklog the most that
	// were waiting when a tick began.
	ticked     bool
	inbox      [][]byte
	perTick    []int
	maxBacklog int
	// BoothOpens: an OPLOC opens the bank holding the bank rows (as a booth's Use-quickly does).
	BoothOpens bool
	// openReq and closeReq are a booth op and a CLOSE_MODAL read this tick (ticked mode), applied
	// after the reads; refused counts the packets the ticked engine refused.
	openReq, closeReq bool
	refused           int
	// chat is the engine's way with an open chat (ModelChat), nil for none.
	chat *chatModel
	// strictTrade makes a packet that clears the pending action (clearsPendingAction) close an open trade screen the way the engine does
	// (SetStrictTrade); strays counts the packets that did.
	strictTrade bool
	strays      int
	// splitMessages (SetSplitMessages) makes a Send that carries several packets hand each to the engine's handler in order, as one
	// websocket message read in one tick is; BeforeSend, when set, is called with every message before the simulator handles it (a test
	// uses it to have the partner act just ahead of a packet).
	splitMessages bool
	BeforeSend    func(b []byte)
}

// UserEventsPerTick is the engine's per-player USER_EVENT limit (NetworkPlayer.decodeIn).
const UserEventsPerTick = 5

// New is a session with the given pack (at most 28 rows, in slot order) and objs.
func New(objs Objs, pack []Stack) *Session {
	s := &Session{st: gamestate.New(), objs: objs, pack: make([]Stack, actions.BackpackCapacity)}
	// A simulated session delivers its ticks as fast as a test asks, which the view's lag gauge would read as a backlog being
	// drained: its view is current unless a test says otherwise (State().OverrideViewLag).
	s.st.OverrideViewLag(&gamestate.ViewLag{})
	copy(s.pack, pack)
	s.pushPack()
	return s
}

func (s *Session) State() *gamestate.State     { return s.st }
func (s *Session) Cheats() *anticheat.Counters { return &s.cheats }

// SetTicked switches the simulator to the engine's timing (engine 289 World.cycle,
// NetworkPlayer.decodeIn, Player.processQueues and processInteraction):
//
//   - a Send is one websocket message: its packets reach the engine's buffer together and wait
//     there until Tick;
//   - Tick reads packets in order until five user events were HANDLED; one the engine refuses (an
//     INV_BUTTON on a bank that is not open or a slot that does not hold the object) counts as a
//     client event instead (at most 20), so the tick reads on past it;
//   - after the reads, a CLOSE_MODAL read this tick closes the bank (the queue step), and then a
//     booth OPLOC read this tick opens it (the interaction step), so bank operations read in the
//     booth's own tick are refused;
//   - the tick ends with a PLAYER_INFO.
//
// Off (the default), every packet applies at once.
func (s *Session) SetTicked(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ticked = on
}

// clientPacketLen is every client packet the simulator splits a message into, opcode included.
var clientPacketLen = map[int]int{
	clientprot.INV_BUTTON1: 7, clientprot.INV_BUTTON2: 7, clientprot.INV_BUTTON3: 7, clientprot.INV_BUTTON4: 7, clientprot.INV_BUTTON5: 7,
	clientprot.OPLOC1: 7, clientprot.OPLOC2: 7, clientprot.OPLOC3: 7, clientprot.OPLOC4: 7, clientprot.OPLOC5: 7,
	clientprot.OPHELD1: 7, clientprot.OPHELD2: 7, clientprot.OPHELD3: 7, clientprot.OPHELD4: 7, clientprot.OPHELD5: 7,
	clientprot.OPHELDU: 13, clientprot.OPHELDT: 9, clientprot.OPOBJ1: 7, clientprot.OPOBJ2: 7, clientprot.OPOBJ3: 7, clientprot.OPOBJ4: 7, clientprot.OPOBJ5: 7,
	clientprot.RESUME_PAUSEBUTTON:   3,
	clientprot.RESUME_P_COUNTDIALOG: 5, clientprot.IF_BUTTON: 3, clientprot.CLOSE_MODAL: 1,
}

// splitMessage cuts a message into its packets; an opcode it does not know ends the split.
func splitMessage(b []byte) [][]byte {
	var out [][]byte
	for len(b) > 0 {
		n, ok := clientPacketLen[int(b[0])]
		if !ok || n > len(b) {
			return append(out, b)
		}
		out = append(out, b[:n])
		b = b[n:]
	}
	return out
}

// Tick runs one server tick in ticked mode and reports how many user events it handled.
func (s *Session) Tick() int {
	s.mu.Lock()
	if len(s.inbox) > s.maxBacklog {
		s.maxBacklog = len(s.inbox)
	}
	handled, refused := 0, 0
	for len(s.inbox) > 0 && handled < UserEventsPerTick && refused < 20 {
		b := s.inbox[0]
		s.inbox = s.inbox[1:]
		if s.handle(b) {
			handled++
		} else {
			refused++
			s.refused++
		}
	}
	if s.delayLeft > 0 {
		s.delayLeft--
	}
	if s.closeReq {
		s.closeReq = false
		if s.srvBank {
			s.srvBank = false
			s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
		}
	}
	if s.openReq {
		s.openReq = false
		s.openBank()
	}
	s.perTick = append(s.perTick, handled)
	s.feed(serverprot.PLAYER_INFO, func(p *packet.Packet) { p.P1(0) })
	s.mu.Unlock()
	return handled
}

// Delay delays the player for the next n ticks (a content script's p_delay: high alchemy is 3), in ticked
// mode: every held, inventory-button, loc and object op read meanwhile is refused.
func (s *Session) Delay(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delayLeft = n
}

// delayGated reports whether the engine's handler for the opcode refuses it while the player is delayed.
func delayGated(op int) bool {
	switch op {
	case clientprot.INV_BUTTON1, clientprot.INV_BUTTON2, clientprot.INV_BUTTON3, clientprot.INV_BUTTON4, clientprot.INV_BUTTON5,
		clientprot.OPHELD1, clientprot.OPHELD2, clientprot.OPHELD3, clientprot.OPHELD4, clientprot.OPHELD5, clientprot.OPHELDU,
		clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5, clientprot.OPLOCU,
		clientprot.OPOBJ1, clientprot.OPOBJ2, clientprot.OPOBJ3, clientprot.OPOBJ4, clientprot.OPOBJ5,
		clientprot.OPPLAYER1, clientprot.OPPLAYER2, clientprot.OPPLAYER3, clientprot.OPPLAYER4, clientprot.OPPLAYER5,
		clientprot.MOVE_GAMECLICK, clientprot.MOVE_MINIMAPCLICK:
		return true
	}
	return false
}

// clearsPendingAction reports whether the engine's handler for the opcode calls player.clearPendingAction() when it reads it: an op
// on a player (OpPlayerHandler) and a walk that is not the walk half of an op (MoveClickHandler: `if (!message.opClick)`). That is
// clearInteraction() plus closeModal(), and closeModal on the trade screen runs [if_close,trademain]: the offer goes back to its
// owner's pack and the partner's screen is closed with "Other player declined trade." So any such packet, from EITHER player,
// closes an open trade for both, and it is decoded the moment it is read, not when the op it names would run. An op on a loc is one too
// (OpLocHandler and OpLocUHandler: clearPendingAction after their checks), which is what a talisman used on the ruins does to a trade
// screen that is still open.
func clearsPendingAction(op int) bool {
	switch op {
	case clientprot.OPPLAYER1, clientprot.OPPLAYER2, clientprot.OPPLAYER3, clientprot.OPPLAYER4, clientprot.OPPLAYER5,
		clientprot.OPPLAYERT, clientprot.OPPLAYERU, clientprot.MOVE_GAMECLICK, clientprot.MOVE_MINIMAPCLICK,
		clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5, clientprot.OPLOCU:
		return true
	}
	return false
}

// Refused is how many packets the ticked engine refused (they did not count towards the five).
func (s *Session) Refused() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

// PerTick is how many user events each tick handled.
func (s *Session) PerTick() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.perTick...)
}

// MaxBacklog is the most packets that were waiting when a tick began: more than
// UserEventsPerTick means a burst was sent ahead of the engine's per-tick read.
func (s *Session) MaxBacklog() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxBacklog
}

// SetBank replaces the bank's rows without opening it (for a booth that opens it later).
func (s *Session) SetBank(rows []Stack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bank = append([]Stack(nil), rows...)
}

// Sent is every packet sent so far.
func (s *Session) Sent() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.sent...)
}

func (s *Session) feed(op int, build func(*packet.Packet)) {
	p := packet.NewWriter(256)
	build(p)
	s.st.Handle(op, p.Data[:p.Pos])
}

func writeInv(p *packet.Packet, com int, rows []Stack) {
	p.P2(uint16(com))
	p.P2(uint16(len(rows)))
	for _, r := range rows {
		if r.ID == 0 || r.Count <= 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(r.ID + 1))
		if r.Count >= 255 {
			p.P1(255)
			p.P4(int32(r.Count))
		} else {
			p.P1(byte(r.Count))
		}
	}
}

func (s *Session) pushPack() {
	rows := append([]Stack(nil), s.pack...)
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.PlayerPack, rows) })
	if s.bankOpen() {
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, SideInv, rows) })
	}
	if s.trade != nil && s.trade.screen == actions.TradeOfferScreen {
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.TradeOfferInv, rows) })
	}
	if s.shop != nil {
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.ShopPack, rows) })
	}
}

func (s *Session) pushBank() {
	rows := append([]Stack(nil), s.bank...)
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.BankWithdraw, rows) })
}

func (s *Session) bankOpen() bool { return s.srvBank }

// Pack is the pack's rows, empty slots dropped.
func (s *Session) Pack() []Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nonEmpty(s.pack)
}

// Remove takes up to n of id out of the pack, as a trade that carried them away would, and reports
// how many went. For a test that stands in for the partner taking what was offered.
func (s *Session) Remove(id int32, n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := s.takePack(id, n)
	s.pushPack()
	return got
}

// Give puts n of id into the pack as the server would (one stack for a stackable, a slot per unit
// otherwise) and reports how many went in. For a test that stands in for a content script adding
// an item, such as a range turning a raw fish into a cooked one.
func (s *Session) Give(id int32, n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := s.addPack(id, n)
	s.pushPack()
	return got
}

// Bank is the bank's rows.
func (s *Session) Bank() []Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nonEmpty(s.bank)
}

func nonEmpty(rows []Stack) []Stack {
	var out []Stack
	for _, r := range rows {
		if r.ID != 0 && r.Count > 0 {
			out = append(out, r)
		}
	}
	return out
}

// OpenBank opens the bank interface holding rows (in bank slot order), note mode off.
func (s *Session) OpenBank(rows []Stack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bank = append([]Stack(nil), rows...)
	s.openBank()
}

func (s *Session) openBank() {
	// [label,openbank] ~reorganize_inv(bank): the rows close up to the left, in order.
	s.bank = nonEmpty(s.bank)
	s.cert = false
	s.srvBank = true
	s.feed(serverprot.IF_OPENMAIN_SIDE, func(p *packet.Packet) { p.P2(actions.BankMainModal); p.P2(SideRoot) })
	s.feed(serverprot.VARP_SMALL, func(p *packet.Packet) { p.P2(actions.BankCertVarp); p.P1(0) })
	s.pushBank()
	s.pushPack()
}

func (s *Session) stackable(id int32) bool {
	if s.objs.Stackable == nil {
		return false
	}
	return s.objs.Stackable(id) || (s.objs.UnnoteOf != nil && s.objs.UnnoteOf(id) != 0)
}

func (s *Session) packCount(id int32) int {
	n := 0
	for _, r := range s.pack {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// addPack puts n of id in the pack as the server would: one stack for a stackable, a slot per
// unit otherwise; returns how many went in.
func (s *Session) addPack(id int32, n int) int {
	if n <= 0 {
		return 0
	}
	if s.stackable(id) {
		for i := range s.pack {
			if s.pack[i].ID == id {
				s.pack[i].Count += n
				return n
			}
		}
		for i := range s.pack {
			if s.pack[i].ID == 0 {
				s.pack[i] = Stack{ID: id, Count: n}
				return n
			}
		}
		return 0
	}
	added := 0
	for i := range s.pack {
		if added == n {
			break
		}
		if s.pack[i].ID == 0 {
			s.pack[i] = Stack{ID: id, Count: 1}
			added++
		}
	}
	return added
}

// takePack removes up to n of id from the pack; returns how many.
func (s *Session) takePack(id int32, n int) int {
	taken := 0
	for i := range s.pack {
		if taken == n {
			break
		}
		if s.pack[i].ID != id {
			continue
		}
		take := s.pack[i].Count
		if take > n-taken {
			take = n - taken
		}
		s.pack[i].Count -= take
		if s.pack[i].Count == 0 {
			s.pack[i] = Stack{}
		}
		taken += take
	}
	return taken
}

func (s *Session) bankAdd(id int32, n int) {
	for i := range s.bank {
		if s.bank[i].ID == id {
			s.bank[i].Count += n
			return
		}
	}
	s.bank = append(s.bank, Stack{ID: id, Count: n})
}

func (s *Session) withdraw(id int32, n int) {
	for i := range s.bank {
		if s.bank[i].ID != id || s.bank[i].Count <= 0 {
			continue
		}
		if n > s.bank[i].Count {
			n = s.bank[i].Count
		}
		lands := id
		if s.cert && s.objs.NoteOf != nil && s.objs.NoteOf(id) != 0 {
			lands = s.objs.NoteOf(id)
		}
		got := s.addPack(lands, n)
		s.bank[i].Count -= got
		return
	}
}

func (s *Session) deposit(id int32, n int) {
	got := s.takePack(id, n)
	if got == 0 {
		return
	}
	stored := id
	if s.objs.UnnoteOf != nil && s.objs.UnnoteOf(id) != 0 {
		stored = s.objs.UnnoteOf(id)
	}
	s.bankAdd(stored, got)
}

// Send handles one client packet as the server would, then passes it to OnSend.
func (s *Session) Send(build func(w *packet.Packet)) error {
	p := packet.NewWriter(64)
	build(p)
	b := append([]byte(nil), p.Data[:p.Pos]...)
	if pre := s.BeforeSend; pre != nil {
		pre(b)
	}
	s.mu.Lock()
	s.sent = append(s.sent, b)
	if s.ticked {
		s.inbox = append(s.inbox, splitMessage(b)...)
	} else if s.splitMessages {
		for _, pkt := range Split(b) {
			s.handle(pkt)
		}
	} else {
		s.handle(b)
	}
	on := s.OnSend
	s.mu.Unlock()
	if on != nil {
		on(b)
	}
	return nil
}

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

// at reports whether rows[slot] holds obj (the engine's inv.hasAt).
func at(rows []Stack, slot int, obj int32) bool {
	return slot >= 0 && slot < len(rows) && rows[slot].ID == obj && rows[slot].Count > 0
}

// handle applies one client packet and reports whether the engine would count it as handled.
func (s *Session) handle(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	op := int(b[0])
	if s.delayLeft > 0 && delayGated(op) {
		return false // OpHeldHandler, InvButtonHandler, OpLocHandler...: `if (player.delayed) return false`
	}
	if s.strictTrade && clearsPendingAction(op) && s.trade != nil {
		s.strays++
		s.closeTrade()
	}
	switch op {
	case clientprot.INV_BUTTON1, clientprot.INV_BUTTON2, clientprot.INV_BUTTON3, clientprot.INV_BUTTON4, clientprot.INV_BUTTON5:
		if len(b) < 7 {
			return false
		}
		option := map[int]int{clientprot.INV_BUTTON1: 1, clientprot.INV_BUTTON2: 2, clientprot.INV_BUTTON3: 3, clientprot.INV_BUTTON4: 4, clientprot.INV_BUTTON5: 5}[op]
		obj, slot, com := int32(g2(b, 1)), g2(b, 3), g2(b, 5)
		counts := map[int]int{1: 1, 2: 5, 3: 10, 4: 1 << 30}
		switch {
		case com == actions.BankWithdraw && s.bankOpen() && at(s.bank, slot, obj):
			if option == 5 {
				s.xFor = obj
				s.feed(serverprot.P_COUNTDIALOG, func(*packet.Packet) {})
				return true
			}
			s.withdraw(obj, counts[option])
		case com == SideInv && s.bankOpen() && at(s.pack, slot, obj):
			s.deposit(obj, counts[option])
		case com == actions.PlayerWorn && option == 1:
			return s.unequip(obj, slot)
		case s.shop != nil && (com == actions.ShopStock || com == actions.ShopPack):
			return s.shopButton(com, option, obj, slot)
		case com == actions.TradeOfferInv && s.trade != nil:
			n := counts[option]
			if option == 5 {
				s.xFor = -obj
				s.feed(serverprot.P_COUNTDIALOG, func(*packet.Packet) {})
				return true
			}
			s.trade.offer(s, obj, n)
			return true
		default:
			return false // the interface is not open, or the slot does not hold it: refused
		}
		s.pushBank()
		s.pushPack()
		return true
	case clientprot.RESUME_P_COUNTDIALOG:
		if len(b) < 5 {
			return false
		}
		n := int(int32(uint32(b[1])<<24 | uint32(b[2])<<16 | uint32(b[3])<<8 | uint32(b[4])))
		obj := s.xFor
		s.xFor = 0
		if obj < 0 && s.trade != nil {
			s.trade.offer(s, -obj, n)
			return true
		}
		if obj > 0 && s.bankOpen() {
			s.withdraw(obj, n)
			s.pushBank()
			s.pushPack()
			return true
		}
		return false
	case clientprot.IF_BUTTON:
		if len(b) < 3 {
			return false
		}
		com := g2(b, 1)
		switch {
		case (com == actions.BankNoteButton || com == actions.BankItemButton) && s.bankOpen():
			s.cert = com == actions.BankNoteButton
			v := byte(0)
			if s.cert {
				v = 1
			}
			s.feed(serverprot.VARP_SMALL, func(p *packet.Packet) { p.P2(actions.BankCertVarp); p.P1(v) })
		case s.trade != nil:
			s.trade.button(s, com)
		default:
			return false
		}
		return true
	case clientprot.CLOSE_MODAL:
		s.closeTrade()
		s.shop = nil
		if s.ticked {
			s.closeReq = true // the queue step, after this tick's reads
			return true
		}
		if s.srvBank {
			s.srvBank = false
			s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
		}
		return true
	case clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5:
		// OpLocHandler: clearPendingAction closes any modal at once; the booth opens the bank in
		// the interaction step, after this tick's reads.
		if s.srvBank {
			s.srvBank = false
			s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
		}
		if s.BoothOpens {
			if s.ticked {
				s.openReq = true
			} else {
				s.openBank()
			}
		}
		return true
	}
	return s.handleHeld(op, b)
}

// ---- trades ----

// closeTrade is the trade screen closing on the engine's side (trade.rs2 [if_close,trademain]): the offer goes back to the pack and
// the client is told. It does nothing when no trade is open. The caller holds the lock.
func (s *Session) closeTrade() {
	t := s.trade
	if t == nil {
		return
	}
	for _, r := range t.mine {
		s.addPack(r.ID, r.Count)
	}
	t.mine = nil
	s.trade = nil
	s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
	s.pushPack()
}

// PartnerClosesTrade is the partner's side closing the screen: what the engine does when the partner sends an op on a player or a
// walk while the screen is open (see clearsPendingAction). The offer returns to the pack and the screen closes.
func (s *Session) PartnerClosesTrade() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeTrade()
}

// SetStrictTrade makes this session close an open trade screen when it SENDS an op on a player or a walk, as the engine does for
// either player of a trade (clearsPendingAction). Off by default: the older trade tests send a second request while a screen they have
// just opened is still being looked at, which the engine would close.
func (s *Session) SetStrictTrade(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strictTrade = on
}

// SetSplitMessages makes the simulator, outside ticked mode, hand every packet of a message to the engine's handler in order (Split), as
// a websocket message read in one tick is, and not only the first: an Accept and an op written together are two packets, and the second
// finds the first's work done. Off by default, as the older tests send coalesced messages that only their first packet was ever read from.
func (s *Session) SetSplitMessages(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.splitMessages = on
}

// StrayCloses is how many times a packet this session sent closed an open trade screen (clearsPendingAction).
func (s *Session) StrayCloses() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.strays
}

// TradeOpen reports whether a trade screen is open on the engine's side.
func (s *Session) TradeOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trade != nil
}

// Trade is the partner's side of one trade, scripted by the test.
type Trade struct {
	screen int
	// Partner is the name the screen shows.
	Partner string
	// Theirs is what the partner offers.
	Theirs []Stack
	mine   []Stack
	// AcceptOffer and AcceptConfirm: whether the partner accepts each screen once we do.
	AcceptOffer, AcceptConfirm bool
	// PartnerHoldsConfirm makes the partner's accept of the CONFIRM screen the test's to give (PartnerAcceptsConfirm), in either order
	// with ours, as trade.rs2 plays it: the side that accepts second completes the trade, and the screen of the side that accepted first
	// says "Waiting for other player." while the other's says "Other player has accepted.". AcceptConfirm is ignored then.
	PartnerHoldsConfirm bool
	// OnMine, when set, is called whenever our offer changes (for a partner reacting to it).
	OnMine func(mine []Stack)
	done   bool
	// ourConfirm and partnerConfirm are who has accepted the confirm screen (PartnerHoldsConfirm).
	ourConfirm, partnerConfirm bool
}

// OpenTrade opens the offer screen with t as the partner.
func (s *Session) OpenTrade(t *Trade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.screen = actions.TradeOfferScreen
	s.trade = t
	s.srvBank = false
	s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.TradeOfferScreen) })
	s.feed(serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(actions.TradePartner); p.PJStr("Trading With: " + t.Partner) })
	s.pushTrade()
	s.pushPack()
}

// SetTheirs replaces the partner's offer while the offer screen is open.
func (s *Session) SetTheirs(rows []Stack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trade == nil {
		return
	}
	s.trade.Theirs = append([]Stack(nil), rows...)
	s.pushTrade()
}

// TradeDone reports whether a trade completed (both confirm screens accepted).
func (s *Session) TradeDone(t *Trade) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return t.done
}

func (s *Session) pushTrade() {
	t := s.trade
	if t == nil {
		return
	}
	mine, theirs := append([]Stack(nil), t.mine...), append([]Stack(nil), t.Theirs...)
	if t.screen == actions.TradeOfferScreen {
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.TradeMyOffer, mine) })
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.TradeTheirOffer, theirs) })
	}
}

func (t *Trade) offer(s *Session, id int32, n int) {
	got := s.takePack(id, n)
	if got == 0 {
		return
	}
	merged := false
	for i := range t.mine {
		if t.mine[i].ID == id {
			t.mine[i].Count += got
			merged = true
		}
	}
	if !merged {
		t.mine = append(t.mine, Stack{ID: id, Count: got})
	}
	s.pushTrade()
	s.pushPack()
	if t.OnMine != nil {
		t.OnMine(append([]Stack(nil), t.mine...))
	}
}

// completeTrade is the second accept of the confirm screen: both_moveinv and if_close (trade.rs2), in the packet that made it, so the pack
// holds what was offered and the screen is gone before anything read after that packet. The caller holds the lock.
func (s *Session) completeTrade(t *Trade) {
	for _, r := range t.Theirs {
		s.addPack(r.ID, r.Count)
	}
	t.mine, t.done = nil, true
	s.trade = nil
	s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
	s.pushPack()
}

// PartnerAcceptsConfirm is the partner's accept of the confirm screen of a trade with PartnerHoldsConfirm: if this side has accepted
// already, the trade completes; if not, this side's screen says "Other player has accepted." and its own accept completes the trade.
// Nothing happens off the confirm screen.
func (s *Session) PartnerAcceptsConfirm(t *Trade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trade != t || t.screen != actions.TradeConfirmScreen {
		return
	}
	if t.ourConfirm {
		s.completeTrade(t)
		return
	}
	t.partnerConfirm = true
	s.feedText(actions.TradeConfirmStatus, "Other player has accepted.")
}

func (s *Session) feedText(com int, text string) {
	s.feed(serverprot.IF_SETTEXT, func(p *packet.Packet) { p.P2(uint16(com)); p.PJStr(text) })
}

func (t *Trade) button(s *Session, com int) {
	switch {
	case com == actions.TradeAcceptOffer && t.screen == actions.TradeOfferScreen:
		if !t.AcceptOffer {
			return
		}
		t.screen = actions.TradeConfirmScreen
		t.ourConfirm, t.partnerConfirm = false, false
		s.feed(serverprot.IF_OPENMAIN, func(p *packet.Packet) { p.P2(actions.TradeConfirmScreen) })
		// The engine resets the status line right after the open and before the offers are transmitted (trade.rs2).
		s.feedText(actions.TradeConfirmStatus, "Are you sure you want to make this trade?")
		mine, theirs := append([]Stack(nil), t.mine...), append([]Stack(nil), t.Theirs...)
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.TradeConfirmMineSmall, mine) })
		s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) { writeInv(p, actions.TradeConfirmTheirsSmall, theirs) })
	case com == actions.TradeAcceptConfirm && t.screen == actions.TradeConfirmScreen:
		if t.PartnerHoldsConfirm {
			if !t.partnerConfirm {
				t.ourConfirm = true
				s.feedText(actions.TradeConfirmStatus, "Waiting for other player.")
				return
			}
		} else if !t.AcceptConfirm {
			return
		}
		s.completeTrade(t)
	case com == actions.TradeDecline:
		for _, r := range t.mine {
			s.addPack(r.ID, r.Count)
		}
		t.mine = nil
		s.trade = nil
		s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
		s.pushPack()
	}
}
