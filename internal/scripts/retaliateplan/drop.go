package retaliateplan

import "sort"

// DropPicker chooses the log a Drop clear is aimed at.
//
// An inventory Drop only clears the player's interaction if the server accepts it: OpHeldHandler refuses a slot that no
// longer holds the item BEFORE it clears anything (engine OpHeldHandler.ts, validSlot/hasAt, then clearPendingAction). A
// write is made from a snapshot of the pack that is a few ticks old (the lead), so a log that an earlier, still unread Drop
// is about to remove must not be aimed at again. The picker therefore never offers a slot it aimed at within the last
// lead+3 ticks, and offers nothing (the caller then presses the controls-tab button instead, which needs no item) when no
// other log is in the snapshot. A log that is in the snapshot exists on the server already, so no other age check is needed.
type DropPicker struct {
	aimed map[int]int
}

// DropCand is one log in the pack snapshot.
type DropCand struct {
	Slot int
	ID   int32
}

// NewDropPicker returns an empty picker.
func NewDropPicker() *DropPicker { return &DropPicker{aimed: map[int]int{}} }

// Pick returns the log to drop for a write made at tick now with the given lead, and records it as aimed. ok is false when no
// log is free to aim at.
func (p *DropPicker) Pick(logs []DropCand, now, lead int) (DropCand, bool) {
	hold := lead + 3
	free := make([]DropCand, 0, len(logs))
	for _, c := range logs {
		if t, was := p.aimed[c.Slot]; was && now-t < hold {
			continue
		}
		free = append(free, c)
	}
	if len(free) == 0 {
		return DropCand{}, false
	}
	// the highest slot first: new logs land in the lowest empty slot, so the highest is the oldest in a steady pack
	sort.Slice(free, func(i, j int) bool { return free[i].Slot > free[j].Slot })
	pick := free[0]
	p.aimed[pick.Slot] = now
	for s, t := range p.aimed {
		if now-t > 4*hold {
			delete(p.aimed, s)
		}
	}
	return pick, true
}
