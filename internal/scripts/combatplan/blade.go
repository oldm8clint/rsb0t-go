package combatplan

// The blade gate: what a run should do when the rung it is walking to needs a bladed weapon
// WORN rather than carried.
//
// Why this is a pure rule with three answers instead of a boolean, and why it is worth a file:
// a live run on 2026-09-20 reached Defence 1 -> 25 in 18m44s, was killing Giants at 21m48s, and
// then died on the two-answer version of this question. A mime random event displaced it 5,109
// tiles; on the next Loop the trainer asked "is a blade worn, and failing that is one carried",
// got false to both, and returned a terminal error that ended the window.
//
// The account had its scimitar the whole time. What it did not have was a readable backpack.
// The mime's entry proc calls ~macro_events_set_tabs (content, macro_events.rs2:218), which is
// if_settab(null, 3) and if_settab(null, 4) — the backpack and worn-items tabs. The server sends
// IF_SETICON with 65535, gamestate stores -1 for that tab, and actions.Equipment resolves its
// container through exactly that tab, so Worn() and Inventory() BOTH return nil. Every pack and
// worn read in the process comes back empty until [mapzoneexit,0_31_74] restores the tabs with
// ~initalltabs.
//
// So the rule is: an unreadable container is not an empty one. "I cannot see" and "it is not
// there" are different answers and only the second is evidence. A run that cannot see waits; a
// run that can see and genuinely has no blade is given a bounded number of passes to buy or
// withdraw one before it is allowed to stop, because that is a real, permanent condition and a
// run that can never cross the web should say so rather than loop forever.

// BladeVerdict is what the caller should do about the blade this pass.
type BladeVerdict int

const (
	// BladeReady means a bladed weapon is in the right hand and the route is open.
	BladeReady BladeVerdict = iota
	// BladeWait means try again next pass. Either the containers could not be read, or the
	// blade is genuinely missing but the run has passes left to fix it.
	BladeWait
	// BladeStop means the containers were readable and held no blade for long enough that the
	// condition is permanent rather than transient.
	BladeStop
)

func (v BladeVerdict) String() string {
	switch v {
	case BladeReady:
		return "ready"
	case BladeWait:
		return "wait"
	default:
		return "stop"
	}
}

// BladeFailLimit is how many consecutive READABLE passes with no blade a run gets before it is
// allowed to stop.
//
// Twenty rather than one because every cheap way out of the state takes more than one pass: the
// gear step has to reach a keeper and buy, a bank trip has to walk to a booth and withdraw, and
// a death drops the account at Lumbridge holding nothing until it re-gears. One pass turned a
// recoverable event into a permanent stop once already. Unreadable passes never count toward it,
// so an event that strips the tabs for minutes cannot exhaust it at all.
const BladeFailLimit = 20

// BladeGate decides the blade question from three readings and a counter.
//
//   - readable: the backpack and worn panels both resolved to a real component this pass.
//   - wielding: a bladed weapon is already in the right hand.
//   - wore: the caller tried to put one on out of the pack and it landed.
//
// It returns the verdict and the new consecutive-failure count, which the caller holds across
// passes. The count is deliberately reset by any success and left ALONE by an unreadable pass:
// a reading that proves nothing must not push a run toward stopping.
func BladeGate(readable, wielding, wore bool, fails, limit int) (BladeVerdict, int) {
	if wielding || wore {
		return BladeReady, 0
	}
	if !readable {
		return BladeWait, fails
	}
	fails++
	if limit > 0 && fails >= limit {
		return BladeStop, fails
	}
	return BladeWait, fails
}
