package actions

import (
	"fmt"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// OpPlayer sends the server-selected player option, preserving native side packets.
func OpPlayer(s Session, option, index int) error {
	if option < 1 || option > 5 || index < 0 || index >= 2047 {
		return fmt.Errorf("invalid player option or index")
	}
	if option == 4 {
		return OpPlayer4(s, int32(index))
	}
	opcodes := [...]byte{clientprot.OPPLAYER1, clientprot.OPPLAYER2, clientprot.OPPLAYER3, clientprot.OPPLAYER4, clientprot.OPPLAYER5}
	return s.Send(func(w *packet.Packet) {
		if option == 1 {
			s.Cheats().BeforeOpPlayer1(w)
		}
		w.P1Enc(opcodes[option-1])
		w.P2(uint16(index))
	})
}

// OpPlayerU uses the held item on a visible player (native OPPLAYERU layout).
func OpPlayerU(s Session, index, obj, slot, com int) error {
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(clientprot.OPPLAYERU)
		w.P2(uint16(index))
		w.P2(uint16(obj))
		w.P2(uint16(slot))
		w.P2(uint16(com))
	})
}
