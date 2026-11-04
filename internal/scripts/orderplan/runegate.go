package orderplan

// THE MULE'S RUNE POOL (a user request): "we should have the mule stack up 20k of runes before it starts
// handing out 1k of each to the accounts requesting it, this will save the mule from continuously being traded".
//
// The thieves' half is consolidateplan/runekeep.go: a thief holds a pocketed rune until it has 10,000 and then
// hands the mule all but a ration, so the pool grows in lots of thousands. This is the mule's half. For the runes
// a gate names (Chaos, Death and Blood by default, the ones the pickpockets pay), the mule hands NONE out while it
// holds fewer than Floor of that rune, and at most Max of it in one hand-out once it holds Floor or more. The gate
// is a gate and not a reserve: at 20,000 held it serves a thousand, and stays shut from 19,000 until the stack is
// built back up, which is the "stack up" users asked for.
//
// It applies to a rune served by the OPEN path (the order for an item the catalogue is silent about, the way every
// kit's runes are served). A rune an operator put in the catalogue (handoutItems) is on the terms it was given: a
// catalogue entry always wins, as everywhere in this package. It replaces anyKeepBack for a gated rune, which
// would otherwise be a second, contradictory floor.
//
// The asking side reads the same rule (RuneGate.Stock, RuneGate.Cap) so an account never walks to a mule that will
// decline it, and never asks for more than a hand-out gives.
//
// Pure.

import "strings"

// The rune pool's defaults.
const (
	// DefaultRuneFloor is the 20,000 of a gated rune the mule must hold before it hands any out.
	DefaultRuneFloor = 20_000
	// DefaultRuneMax is the 1,000 of one rune a single hand-out gives.
	DefaultRuneMax = 1_000
)

// RuneGate is the pooled-rune rule. The zero value is off.
type RuneGate struct {
	// Floor is how many of a gated rune the mule must already hold before it hands any of it out; 0 or less is
	// off (handoutRuneFloor).
	Floor int
	// Max is the most of one gated rune one hand-out gives (handoutRuneMax); 0 leaves the open path's own ceiling.
	Max int
	// Runes are the gated rune names (handoutRunes), matched ignoring case and surrounding space.
	Runes []string
}

// DefaultRuneGate is the owner's rule for the runes the pickpockets pay.
func DefaultRuneGate() RuneGate {
	return RuneGate{Floor: DefaultRuneFloor, Max: DefaultRuneMax, Runes: []string{"Chaos rune", "Death rune", "Blood rune"}}
}

// Active reports whether the rule gates anything.
func (g RuneGate) Active() bool { return g.Floor > 0 && len(g.Runes) > 0 }

// Covers reports whether item is a gated rune.
func (g RuneGate) Covers(item string) bool {
	if !g.Active() {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(item))
	for _, r := range g.Runes {
		if strings.ToLower(strings.TrimSpace(r)) == want && want != "" {
			return true
		}
	}
	return false
}

// narrow puts a gated rune's open-path entry on the gate's terms: the gate's floor (inclusive, a gate and never a
// reserve) and the lower of the entry's per-order ceiling and the gate's.
func (g RuneGate) narrow(h Handout) Handout {
	if !g.Covers(h.Item) {
		return h
	}
	if g.Max > 0 && (h.Max <= 0 || g.Max < h.Max) {
		h.Max = g.Max
	}
	h.Floor, h.FloorKeepsBack, h.FloorInclusive = g.Floor, false, true
	return h
}

// Holds reports whether a mule holding held of a gated rune hands it out.
func (g RuneGate) Holds(held int) bool { return held >= g.Floor }

// FloorOf is how many of item the mule must hold before it hands any out: Floor for a gated rune, 0 for anything else.
func (g RuneGate) FloorOf(item string) int {
	if g.Covers(item) {
		return g.Floor
	}
	return 0
}

// Cap is the most of item one hand-out gives: n, cut to Max for a gated rune.
func (g RuneGate) Cap(item string, n int) int {
	if g.Covers(item) && g.Max > 0 && n > g.Max {
		return g.Max
	}
	return n
}

// Stock is a mule's published bank as the asking side should read it under the gate: a gated rune the mule holds
// fewer than Floor of reads as none (known, zero), because the mule will decline an order for it; everything else
// is as published. nil stays nil (no reading), and an inactive gate returns stock unchanged.
func (g RuneGate) Stock(stock func(item string) (int, bool)) func(item string) (int, bool) {
	if stock == nil || !g.Active() {
		return stock
	}
	return func(item string) (int, bool) {
		n, ok := stock(item)
		if ok && g.Covers(item) && !g.Holds(n) {
			return 0, true
		}
		return n, ok
	}
}
