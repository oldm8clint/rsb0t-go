package runerunnerplan

// The trawler gate (a user request: "acct should only join trawler if they have completed thier full shopping route to
// ensure shops are empty before going to the trawler"). While master's trawler block is on, the Runner's trawler crew
// (internal/bot/runner_trawler.go) asks the script before it takes the account; RuneRunner answers with TrawlerReady.
//
// The clean point between two rounds is the new round with its first station, the delivery to the mule, done: the last
// round's shops are empty and its goods are with the mule, and no shop of the new round has been visited. That is where the
// account joins. Mid-round it stays on its round, except that a round older than trawlerRoundHoldMaxMinutes (a stalled one)
// lets it join anyway. Once it has joined, trawlerStintMinutes bounds its stay: after that the gate says no for the rest
// of the round, so the next round is walked before the next stint (the crew's end-of-game leave hop restarts the script,
// which is when the gate is asked again).

import (
	"fmt"
	"time"
)

// The gate's settings.
const (
	// DefaultTrawlerStintMinutes is about one trawler game: joins measured 26-32 minutes to the first leave hop.
	DefaultTrawlerStintMinutes = 20
	MaxTrawlerStintMinutes     = 600
	// DefaultTrawlerRoundHoldMaxMinutes caps how long an unfinished round holds the account off the crew (a clean round
	// takes 45-65 minutes).
	DefaultTrawlerRoundHoldMaxMinutes = 180
	MinTrawlerRoundHoldMaxMinutes     = 10
	MaxTrawlerRoundHoldMaxMinutes     = 1440
)

// TrawlerVerdict is the gate's answer.
type TrawlerVerdict struct {
	Ready bool
	// Why is the reason, for the Runner's log line.
	Why string
	// Forced is a join past trawlerRoundHoldMaxMinutes of an unfinished round.
	Forced bool
}

// TrawlerReady reports whether the account may join the trawler crew now. stint 0 is "until the block ends"; holdMax 0 is
// no cap on the hold. heldSince is when the gate first said no this round (zero when it has not): holdMax counts from the
// latest of the round's start, heldSince and the end of this round's stint, so a block that starts late in an old round
// still gets the round's shops walked before a forced join.
func TrawlerReady(st State, now time.Time, stint, holdMax time.Duration, heldSince time.Time) TrawlerVerdict {
	if st.HopPending > 0 {
		return TrawlerVerdict{Why: fmt.Sprintf("a hop to world %d is under way", st.HopPending)}
	}
	if st.Trip.Active {
		return TrawlerVerdict{Why: "a Mage Bank trip to Lundail is under way"}
	}
	// held is the reason the round holds the account; a stalled round past holdMax joins anyway.
	holdStart := st.RoundStarted
	if heldSince.After(holdStart) {
		holdStart = heldSince
	}
	if stint > 0 && !st.TrawlerSince.IsZero() && st.TrawlerRound == st.Round {
		if end := st.TrawlerSince.Add(stint); end.After(holdStart) {
			holdStart = end
		}
	}
	held := func(why string) TrawlerVerdict {
		age := now.Sub(holdStart)
		if holdMax > 0 && !holdStart.IsZero() && age >= holdMax {
			return TrawlerVerdict{Ready: true, Forced: true, Why: fmt.Sprintf("%s, but it has held the crew %s (trawlerRoundHoldMaxMinutes %d): joining anyway",
				why, age.Truncate(time.Minute), int(holdMax/time.Minute))}
		}
		return TrawlerVerdict{Why: why}
	}
	if stint > 0 && !st.TrawlerSince.IsZero() && st.TrawlerRound == st.Round && now.Sub(st.TrawlerSince) >= stint {
		return held(fmt.Sprintf("the trawler stint of round=%d (trawlerStintMinutes %d) is over: the round's shops first",
			st.Round, int(stint/time.Minute)))
	}
	if st.Round == 0 && len(st.Ring) == 0 {
		return TrawlerVerdict{Ready: true, Why: "no round under way"}
	}
	if len(st.Ring) == 0 || st.Cursor >= len(st.Ring) {
		return held(fmt.Sprintf("round=%d is closed: the next round's delivery first", st.Round))
	}
	if st.Cursor <= 0 {
		return held(fmt.Sprintf("round=%d's delivery to the mule first", st.Round))
	}
	for _, s := range st.Ring[:st.Cursor] {
		if s.Kind != KindDeliver || !st.DoneThisRound(s.Key()) {
			return held(fmt.Sprintf("round=%d is under way at %s (%d of %d stations done)",
				st.Round, st.Ring[st.Cursor].Key(), len(st.Done), len(st.Ring)))
		}
	}
	return TrawlerVerdict{Ready: true, Why: fmt.Sprintf("round=%d done and round=%d's delivery made", st.Round-1, st.Round)}
}

// NoteTrawlerJoin records a join (the stint's clock) unless this round's stint is still running; changed is true when it
// wrote, so the caller saves.
func (st *State) NoteTrawlerJoin(now time.Time, stint time.Duration) (changed bool) {
	if !st.TrawlerSince.IsZero() && st.TrawlerRound == st.Round && (stint <= 0 || now.Sub(st.TrawlerSince) < stint) {
		return false
	}
	st.TrawlerRound, st.TrawlerSince = st.Round, now
	return true
}

// TrawlerStint is the trawlerStintMinutes setting as a duration.
func (s Settings) TrawlerStint() time.Duration {
	return time.Duration(s.TrawlerStintMinutes) * time.Minute
}

// TrawlerRoundHoldMax is the trawlerRoundHoldMaxMinutes setting as a duration.
func (s Settings) TrawlerRoundHoldMax() time.Duration {
	return time.Duration(s.TrawlerRoundHoldMaxMinutes) * time.Minute
}
