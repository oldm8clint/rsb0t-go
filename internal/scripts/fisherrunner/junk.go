package fisherrunner

// Dropping the junk catch mid-session, for BassOysterCaskets (fisherplan.BassOysterJunk). Only a Deps with
// Junk set and a Cycle ever drops anything; ProgressiveFisher leaves both nil.
//
// Dropping is now the drop cycle's job (cycle.go): the same held-op Drop of every junk slot, five a tick
// (content's [opheld5,_] has no delay), done together with the caskets and oysters opened, the key halves
// joined and the pearls cut. These two names are what FishUntil has always called, kept so that FishUntil's
// own text did not change when the cycle replaced the junk drop: a drop clears the player's interaction like
// any OPHELD on the backpack, which stops fishing, so FishUntil runs the cycle only when it is due (the pack
// nearly full, with something to do) and then re-clicks the spot it was on.

// junkDue reports whether a drop cycle is due: the pack is nearly full and holds something for it.
func junkDue(d *Deps) bool { return CycleDue(d) }

// dropJunkMidSession is FishUntil's drop: one drop cycle, with the raw fish it dropped taken off the
// session's target and count (cycleMidSession). It reports whether anything was done; FishUntil stops
// trying for the rest of the session when nothing was.
func dropJunkMidSession(d *Deps, lastCount, target *int) bool {
	return cycleMidSession(d, lastCount, target)
}
