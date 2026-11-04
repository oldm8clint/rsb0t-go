package fleetplan

// RecommendedKQStandoff is the npc_range a caster should hold from her FOOTPRINT: close enough
// that the engine actually delivers the cast, far enough that melee never joins her die.
//
// Measured with the kqdrain A/B at Magic 59 Fire Blast, every reading gated on a holder being
// alive:
//
//	npc_range 8   81 casts sent, ZERO accepted
//	npc_range 3   24 to 36 accepted
//	npc_range 1   41 accepted, and melee is on her die
//
// A cast from 8 is never delivered at all — the engine runs the interaction only from a tile that
// passes its own range test and will not walk the account there under clientRoutefinder, the same
// mechanism CLAUDE.md documents for OpLocU. Three is the cheapest tile that both casts and stays
// off the melee face, since KQStyleOddsFor only adds the third face at npc_range <= 1.
const RecommendedKQStandoff = 3

func Abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
