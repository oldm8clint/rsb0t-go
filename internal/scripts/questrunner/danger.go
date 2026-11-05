package questrunner

import "time"

// dangerVerdict is what a dangerWatch says to do about hitpoints that keep falling while the quest
// makes no progress.
type dangerVerdict int

const (
	dangerNone     dangerVerdict = iota
	dangerWalkAway               // leave the spot: walk to the retreat bank
	dangerPark                   // walking away did not stop it (or it is critical): park the quest
)

// DangerParkFor is how long a quest that stood in a danger is skipped (live 6 Nov 2026: a Waterfall
// run stood at East Ardougne market with no food, hitpoints 11, 6, 5, 4, and died twice).
const DangerParkFor = 15 * time.Minute

// dangerWatch is the engine-wide fail-safe every quest inherits: a quest loop must never stand in a
// danger without eating, walking off or parking. It is fed once per engine tick with the hitpoints and
// whether the tick made no progress (a wait step, or a step that failed). It is pure.
type dangerWatch struct {
	stalled int // consecutive no-progress ticks
	drops   int // ticks inside the streak on which hitpoints fell
	start   int
	last    int
	walks   int // walk-aways already tried since the hitpoints were last healthy
}

// Note records one tick and returns the verdict.
func (w *dangerWatch) Note(hp, maxHP int, stalled bool) dangerVerdict {
	if hp <= 0 || maxHP <= 0 {
		return dangerNone // unknown reading
	}
	if hp*100 >= 80*maxHP {
		w.walks = 0
	}
	if !stalled {
		w.stalled, w.drops = 0, 0
		w.start, w.last = hp, hp
		return dangerNone
	}
	if w.stalled == 0 {
		w.start, w.last = hp, hp
	}
	w.stalled++
	if hp < w.last {
		w.drops++
	}
	w.last = hp

	lowHalf := hp*2 <= maxHP
	critical := hp*4 <= maxHP
	switch {
	case critical && w.drops >= 1 && w.stalled >= 2,
		lowHalf && w.drops >= 2 && w.stalled >= 3,
		w.drops >= 3 && w.stalled >= 8:
		if w.walks == 0 {
			w.walks++
			w.stalled, w.drops, w.start = 0, 0, hp
			return dangerWalkAway
		}
		return dangerPark
	}
	return dangerNone
}
