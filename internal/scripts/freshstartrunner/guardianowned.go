package freshstartrunner

import (
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// guardianOwned reports whether a loc is inside the walls of a place whose way out the Guardian plans for itself
// (Rogues' Castle: randomevents.StrandCompoundAt). The script's own recoveries (the door it opens "to clear the
// way", the chain of doors out of a "sealed pocket", the stair, the stile, the crumbling wall) all rank what is
// near, and from open ground in the deep Wilderness the whole of the castle is near: its home is "unreachable" on
// the raw pack from anywhere north of the ditch, so every one of those recoveries fires, picks a castle door and
// walks the account back into the courtyards and rooms the Guardian had just got it out of. On 6 October 2026 a
// ProgressiveCrafter and four ProgressiveWCFletch/Fisher accounts did that for two hours, opening the east room's
// door, the east chamber's door and each other's leaves. A loc the Guardian owns is not a candidate.
func guardianOwned(m locindex.Match) bool {
	if _, ok := randomevents.StrandCompoundAt(m.X, m.Z, m.Level); ok {
		return true
	}
	return leverOwned(m)
}

// leverOwned reports whether a loc is inside the Draynor Manor basement, whose doors answer to six levers and whose way out is
// the quest module's to plan (questmods/ernestmaze.go: the levers' state is read off the server, never from the pack, which
// draws every door shut). The script's recoveries cannot: a door hunt in the basement picks a door that is locked ("timed out
// waiting for the door to open", "the step through the door" again and again), and the stair search finds the ladder up by its
// name and cannot walk to it ("has no reachable approach tile from here") four walks and a minute at a time. Live 19 Oct 2026,
// two Stage 5 accounts did both for hours, 172 ladder tries between them, while the module's own search was never asked.
func leverOwned(m locindex.Match) bool {
	return questplan.InErnestBasement(tile.Tile{X: m.X, Z: m.Z, Level: m.Level})
}
