package actionsim

import (
	"sync"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// The click-flood test kit (docs/click_flood_audit_2026-10-15.md): a session that can be told a server tick has passed, can show a
// chat that stays up, and counts what the client sent per game tick, so a test can say "this loop sent at most one click a tick
// while the chat stayed up".

// AdvanceTick is one server tick as the client sees it (a PLAYER_INFO), in the default mode where packets apply at once. In
// SetTicked mode Tick does this and more; use that instead. A chat ModelChat is playing closes here, in the tick's output.
func (s *Session) AdvanceTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feed(serverprot.PLAYER_INFO, func(p *packet.Packet) { p.P1(0) })
	if m := s.chat; m != nil && m.closeAt != 0 && s.st.Tick() >= m.closeAt {
		m.closeAt = 0
		s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
	}
}

// chatModel is how the engine treats an open chat: a Continue finishes the script paused on its button (Player.executeScript closes
// the chat) and a click closes it (OpLocHandler, OpNpcHandler, OpObjHandler: clearPendingAction is closeModal), each in the next
// tick's output.
type chatModel struct {
	onContinue, onClick bool
	closeAt             int
}

// ModelChat plays the engine's way with an open chat (see chatModel): onContinue lets the pause button close it, onClick lets a
// click close it. With both off the chat stays up for ever. The close lands in the next AdvanceTick. Call it once, before the first
// packet; it chains any OnSend already set.
func (s *Session) ModelChat(onContinue, onClick bool) {
	m := &chatModel{onContinue: onContinue, onClick: onClick}
	s.mu.Lock()
	s.chat = m
	prev := s.OnSend
	s.mu.Unlock()
	s.OnSend = func(b []byte) {
		for _, op := range Opcodes(b) {
			isClick := false
			for _, c := range ClickOps {
				if c == op {
					isClick = true
					break
				}
			}
			s.mu.Lock()
			if m.closeAt == 0 && ((op == clientprot.RESUME_PAUSEBUTTON && m.onContinue) || (isClick && m.onClick)) {
				m.closeAt = s.st.Tick() + 1
			}
			s.mu.Unlock()
		}
		if prev != nil {
			prev(b)
		}
	}
}

// OpenChat shows chat interface id, as IF_OPENCHAT does.
func (s *Session) OpenChat(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feed(serverprot.IF_OPENCHAT, func(p *packet.Packet) { p.P2(uint16(id)) })
}

// CloseChat is the server's IF_CLOSE.
func (s *Session) CloseChat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feed(serverprot.IF_CLOSE, func(*packet.Packet) {})
}

// ClickOps is every opcode that is a click on something: a loc, an NPC, a ground item, a held item or a player, with their
// use-on forms and the held and inventory buttons.
var ClickOps = []int{
	clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5, clientprot.OPLOCU,
	clientprot.OPNPC1, clientprot.OPNPC2, clientprot.OPNPC3, clientprot.OPNPC4, clientprot.OPNPC5, clientprot.OPNPCU,
	clientprot.OPOBJ1, clientprot.OPOBJ2, clientprot.OPOBJ3, clientprot.OPOBJ4, clientprot.OPOBJ5, clientprot.OPOBJU,
	clientprot.OPHELD1, clientprot.OPHELD2, clientprot.OPHELD3, clientprot.OPHELD4, clientprot.OPHELD5, clientprot.OPHELDU,
	clientprot.OPPLAYER1, clientprot.OPPLAYER2, clientprot.OPPLAYER3, clientprot.OPPLAYER4, clientprot.OPPLAYER5,
	clientprot.INV_BUTTON1, clientprot.INV_BUTTON2, clientprot.INV_BUTTON3, clientprot.INV_BUTTON4, clientprot.INV_BUTTON5,
}

// UserOps is ClickOps plus the other packets the engine counts against its five-a-tick USER_EVENT read: walks, interface buttons,
// the pause button and CLOSE_MODAL.
var UserOps = append(append([]int(nil), ClickOps...),
	clientprot.MOVE_GAMECLICK, clientprot.MOVE_OPCLICK, clientprot.MOVE_MINIMAPCLICK,
	clientprot.IF_BUTTON, clientprot.RESUME_PAUSEBUTTON, clientprot.CLOSE_MODAL)

// antiLen is the length, opcode included, of the fixed-size anti-cheat side packets that ride in front of a click.
var antiLen = map[int]int{
	clientprot.ANTICHEAT_OPLOGIC1: 5, clientprot.ANTICHEAT_OPLOGIC2: 3, clientprot.ANTICHEAT_OPLOGIC3: 5,
	clientprot.ANTICHEAT_OPLOGIC4: 2, clientprot.ANTICHEAT_OPLOGIC5: 2, clientprot.ANTICHEAT_OPLOGIC6: 3,
	clientprot.ANTICHEAT_OPLOGIC7: 5, clientprot.ANTICHEAT_OPLOGIC8: 2, clientprot.ANTICHEAT_OPLOGIC9: 4,
}

// extraLen is the length, opcode included, of the click packets splitMessage (the ticked engine's own table) does not know.
var extraLen = map[int]int{
	clientprot.OPNPC1: 3, clientprot.OPNPC2: 3, clientprot.OPNPC3: 3, clientprot.OPNPC4: 3, clientprot.OPNPC5: 3, clientprot.OPNPCU: 9,
	clientprot.OPLOCU: 13, clientprot.OPOBJU: 13,
	clientprot.OPPLAYER1: 3, clientprot.OPPLAYER2: 3, clientprot.OPPLAYER3: 3, clientprot.OPPLAYER4: 3, clientprot.OPPLAYER5: 3,
}

// packetLen is the length, opcode included, of the packet at the start of b, and whether it could be sized.
func packetLen(b []byte) (op, n int, ok bool) {
	op = int(b[0])
	switch {
	case op == clientprot.ANTICHEAT_CYCLELOGIC2, op == clientprot.MOVE_GAMECLICK, op == clientprot.MOVE_OPCLICK, op == clientprot.MOVE_MINIMAPCLICK:
		if len(b) < 2 {
			return op, 0, false
		}
		return op, 2 + int(b[1]), true
	case antiLen[op] > 0:
		return op, antiLen[op], true
	case clientPacketLen[op] > 0:
		return op, clientPacketLen[op], true
	case extraLen[op] > 0:
		return op, extraLen[op], true
	case op == clientprot.NO_TIMEOUT:
		return op, 1, true
	}
	return op, 0, false
}

// Opcodes lists the opcodes of the packets in one websocket message, in order. A packet it cannot size ends the list.
func Opcodes(b []byte) []int {
	var out []int
	for len(b) > 0 {
		op, n, ok := packetLen(b)
		out = append(out, op)
		if !ok || n >= len(b) {
			return out
		}
		b = b[n:]
	}
	return out
}

// Split cuts one websocket message into its packets, opcode first, in order: a coalesced walk and click is two. A packet it cannot size,
// or one the message is too short to hold, ends the split and is returned whole as the last element.
func Split(b []byte) [][]byte {
	var out [][]byte
	for len(b) > 0 {
		_, n, ok := packetLen(b)
		if !ok || n > len(b) {
			return append(out, b)
		}
		out = append(out, b[:n])
		b = b[n:]
	}
	return out
}

// Clicks counts the packets a session sent, by opcode and by the game tick the client had seen when it sent them.
type Clicks struct {
	mu     sync.Mutex
	s      *Session
	byTick map[int]map[int]int // tick -> opcode -> count
}

// RecordClicks starts counting what s sends. It chains any OnSend already set.
func (s *Session) RecordClicks() *Clicks {
	c := &Clicks{s: s, byTick: map[int]map[int]int{}}
	prev := s.OnSend
	s.OnSend = func(b []byte) {
		tick := s.st.Tick()
		c.mu.Lock()
		for _, op := range Opcodes(b) {
			if c.byTick[tick] == nil {
				c.byTick[tick] = map[int]int{}
			}
			c.byTick[tick][op]++
		}
		c.mu.Unlock()
		if prev != nil {
			prev(b)
		}
	}
	return c
}

// Count is how many packets with one of ops were sent in all.
func (c *Clicks) Count(ops ...int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.byTick {
		for _, op := range ops {
			n += m[op]
		}
	}
	return n
}

// MaxPerTick is the most packets with one of ops sent within one game tick, and which tick that was.
func (c *Clicks) MaxPerTick(ops ...int) (most, tick int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for t, m := range c.byTick {
		n := 0
		for _, op := range ops {
			n += m[op]
		}
		if n > most {
			most, tick = n, t
		}
	}
	return most, tick
}

// Ticks is how many distinct game ticks saw at least one of ops sent.
func (c *Clicks) Ticks(ops ...int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.byTick {
		for _, op := range ops {
			if m[op] > 0 {
				n++
				break
			}
		}
	}
	return n
}
