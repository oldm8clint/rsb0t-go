package fleetrunner

import (
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// simNPCsFacing is simNPCs for NPCs that all face the local player in slot: an NPC_INFO whose new-vis entries carry an extended
// block with the face-entity mask (0x04) and the player's face id (slot + 32768).
func simNPCsFacing(s *gamestate.State, slot int, npcs ...simNPC) {
	fields := []bitField{{8, 0}}
	for _, n := range npcs {
		fields = append(fields, bitField{14, uint32(n.index)}, bitField{11, uint32(n.typeID)},
			bitField{5, uint32(n.dx & 0x1f)}, bitField{5, uint32(n.dz & 0x1f)}, bitField{1, 0}, bitField{1, 1})
	}
	fields = append(fields, bitField{14, 16383})
	data := bitPack((8+37*len(npcs)+14+7)/8, fields...)
	for range npcs {
		face := 32768 + slot
		data = append(data, 0x04, byte(face>>8), byte(face))
	}
	s.Handle(serverprot.NPC_INFO, data)
}

func eatsSent(sim *actionsim.Session) int { return len(sentOps(sim, clientprot.OPHELD1)) }
