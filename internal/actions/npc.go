package actions

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

var opNpcOpcodes = [5]int{
	clientprot.OPNPC1,
	clientprot.OPNPC2,
	clientprot.OPNPC3,
	clientprot.OPNPC4,
	clientprot.OPNPC5,
}

// OpNpc ports the OP_NPC1..5 branch of doAction: option is 1-based like the menu's own
// numbering. Ported from interactWithLoc's sibling in Client.ts (~line 8882) minus the
// client-side tryMove approach — callers are expected to already be within interaction
// range (e.g. via internal/pathfind + Walk), the same assumption trade's live proof
// already validated for player targets.
func OpNpc(s Session, option int, npcIndex int) error {
	if option < 1 || option > 5 {
		return errBadOption
	}
	if interactionHeld(s) {
		return nil // the Guardian holds the account's input while it handles an event (movehold.go)
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(byte(opNpcOpcodes[option-1]))
		w.P2(uint16(npcIndex))
	})
}
