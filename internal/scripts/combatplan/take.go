package combatplan

// Judging a ground-item take.
//
// The obvious confirmation — "wait until the pack holds one more of it" — cannot tell the three
// outcomes apart, and a run that only has that predicate reports every one of them as the same
// timeout. A live CombatTrainer run logged exactly that: "took item 532 but the pack never
// gained it", which names the pickup as the culprit without any evidence that the pickup was
// what failed. It could equally have been another account taking the stack first, the stack
// despawning, or a click the engine accepted and never ran.
//
// So the verdict is taken from two readings, not one: what the pack holds, and whether the stack
// is still on the ground. The order matters in both directions.
//
// PackGained is checked FIRST because the two server messages race. Taking an item sends both an
// OBJ_DEL for the tile and an inventory update, and nothing guarantees the inventory half is
// decoded first. A judge that looked at the ground first would read its own successful pickup as
// "the stack vanished" on any tick where OBJ_DEL landed ahead of UPDATE_INV_PARTIAL.
//
// TakeGone needs a settle window for the same race seen from the other side: the stack being
// missing for one poll proves nothing, so it has to stay missing while the pack stays unchanged
// before the disappearance means somebody else got it.

// TakeObservation is one poll of the world after a Take was sent.
type TakeObservation struct {
	// PackGained is whether the pack now holds more of this item than it did before the click.
	PackGained bool
	// OnGround is whether the stack is still where we clicked it.
	OnGround bool
	// SinceMs is how long ago the Take went out.
	SinceMs int64
	// MissingForMs is how long the stack has been absent from the ground, 0 while it is there.
	MissingForMs int64
}

// TakeVerdict is what one observation proves about a Take.
type TakeVerdict string

const (
	// TakeWaiting means nothing is settled yet: keep polling.
	TakeWaiting TakeVerdict = "waiting"
	// TakeDone means the pack really gained the item.
	TakeDone TakeVerdict = "taken"
	// TakeGone means the stack left the ground without reaching our pack: another account or
	// an NPC took it, or it despawned. The item is lost, but nothing is broken.
	TakeGone TakeVerdict = "gone"
	// TakeTimedOut means the stack is still lying there and the pack never changed, which is
	// the only outcome that points at the click itself.
	TakeTimedOut TakeVerdict = "timed-out"
)

// JudgeTake decides what one observation proves. settleMs is how long a stack must stay missing
// before its absence counts, and timeoutMs how long the whole take gets.
func JudgeTake(o TakeObservation, settleMs, timeoutMs int64) TakeVerdict {
	if o.PackGained {
		return TakeDone
	}
	if !o.OnGround && o.MissingForMs >= settleMs {
		return TakeGone
	}
	if o.SinceMs >= timeoutMs {
		return TakeTimedOut
	}
	return TakeWaiting
}
