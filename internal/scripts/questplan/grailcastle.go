package questplan

import "rsb0t-go/internal/scripts/tile"

// The Grail castle's bell. The castle stands on the Fisherman's side of the river and has no door a walk reaches: the only
// way in is [opheld1,grail_bell] in quest_grail.rs2,
//
//	~mesbox("Ting-a-ling-a-ling!");
//	if(npc_find(coord, grail_maiden, 4, 0) = true & inzone(0_43_73_0_0, 0_43_73_63_63, coord) = true) {
//	    ~chatnpc("<p,confused>Welcome to the Grail castle.|You should come inside,|it's cold out here.");
//	    p_teleport(0_43_73_9_20);
//	    ~mesbox("Somehow you are now inside the castle.");
//	    return;
//	}
//	mes("Nothing happens.");
//
// which asks two things of the account that the old flow never did. A maiden must stand within four tiles of it when the
// bell is rung (the four of them spawn inside the castle, at 2760,4688 2763,4690 2766,4680 and 2769,4686, and wander), and
// every one of the three boxes is a p_pausebutton: the script waits for the CONTINUE click, and a walk, a close or any
// other interface action drops it (Player.closeModal clears an activeScript paused on a button). The old flow rang the bell,
// waited six seconds for a landing that never came and walked off, so the teleport never ran; the walker, rightly, found
// the staircase unreachable from the ring tile (nine parks of "5 walks to 2760,4681 ended unreachable" in the 12-16 Oct
// 2026 logs, the last at 06:28 on 16 Oct, each after a Titan fight of twenty minutes that had been won).

// NpcGrailMaiden is the Grail Maiden's npc id (content/pack/npc.pack, fisher_realm.npc).
const NpcGrailMaiden = 210

// GrailBellEarshot is how near a maiden must be: npc_find's distance, the zone iterator's Chebyshev distance from the
// account.
const GrailBellEarshot = 4

// GrailCastleLanding is where the bell sets the account down (0_43_73_9_20), the ground floor's north-west room.
var GrailCastleLanding = tile.Tile{X: 2761, Z: 4692}

// GrailStairStand is the tile west of the castle's spiral staircase (loc 1738 at 2761,4680, a 2x2): the stand the baked
// graph's "Climb-up" edge starts from. The staircase lands on the first floor at 2762,4682.
var GrailStairStand = tile.Tile{X: 2760, Z: 4680}

// GrailBellLanded reports whether t is somewhere the bell's teleport leaves an account that rang it: behind the castle
// walls. A distance from the landing tile is no test: the ring stand (2763,4693) is two tiles from it, on the other side of
// the wall.
func GrailBellLanded(t tile.Tile) bool { return InGrailCastle(t) }

// GrailMaidenInEarshot reports whether a maiden on maiden hears a bell rung on me: npc_find(coord, grail_maiden, 4, 0)
// looks in a square of four tiles about the ringer, on its own level.
func GrailMaidenInEarshot(me, maiden tile.Tile) bool {
	return me.Level == maiden.Level && tile.Chebyshev(me, maiden) <= GrailBellEarshot
}

// The castle's ground floor: the tiles a walk cannot reach from the ring tile (GrailBellRing) once the doors the baked
// graph knows are counted, which is to say the rooms the bell is the only way into. Rows run from z 4692 (index 0) down to
// 4675, columns from x 2756 to 2780; 'I' marks an interior tile. Measured with the live planner over the real collision pack
// (questmods/grail_pack_test.go checks the table against it: every 'I' tile is unreachable from the ring tile, and every
// tile a walk from the landing reaches is an 'I'). The castle's floor plan interleaves with the ground outside, so no
// rectangle holds it.
var grailCastleRows = [...]string{
	"....III.III..............", // 4692
	"...IIIIIIIII.............", // 4691
	"..II.IIIII.I....I........", // 4690
	"..IIIIII.II.IIII.........", // 4689
	"..I.IIIIIIII.III.........", // 4688
	"..III.IIIIIIIIII.........", // 4687
	".IIIIIIIII.III.I...IIIII.", // 4686
	"I.I.III.I.I..IIIII.IIIIII", // 4685
	".IIIIIII.IIIIIIIIIII..II.", // 4684
	"..IIIIIII.IIIIIIII.I..III", // 4683
	"IIIIIIIIIIIIIIII...IIIII.", // 4682
	"IIIII..IIIIIIIII.........", // 4681
	".IIII..IIIIIIIII.........", // 4680
	".II.IIII.III.............", // 4679
	"IIII....III..............", // 4678
	"IIIIIIIIII...............", // 4677
	"IIIIIIIII................", // 4676
	".I.......................", // 4675
}

const (
	grailCastleMinX = 2756
	grailCastleMaxZ = 4692
)

// InGrailCastle reports whether t is on the castle's ground floor behind the walls the bell opens: the account that stands
// there has rung it already, and the bell is not rung again (the ring tile is unreachable from the inside, and an account
// whose quest was cut short between the bell and the staircase would otherwise walk to the Fisherman for ever).
func InGrailCastle(t tile.Tile) bool {
	if t.Level != 0 {
		return false
	}
	row := grailCastleMaxZ - t.Z
	col := t.X - grailCastleMinX
	if row < 0 || row >= len(grailCastleRows) || col < 0 || col >= len(grailCastleRows[row]) {
		return false
	}
	return grailCastleRows[row][col] == 'I'
}
