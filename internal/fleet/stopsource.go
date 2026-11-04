package fleet

import "rsb0t-go/internal/stallwatch"

// stopSource says who asked an account's bot to stop through the fleet. It exists for one reader,
// the watchdog's safety net (watchdog_orphan.go): a bot an operator stopped is left stopped, and any
// other bot found stopped on an account in game is started again. Every path that stops a bot from
// outside the script records its source on the account (account.noteStopLocked) and every StartBot
// clears it, so the mark always describes the CURRENT run.
//
// The script ending itself is not a source at all: it never goes through the fleet. The runner
// records that separately (bot.Runner.StoppedFromOutside).
type stopSource int

const (
	// stopNone is no request through the fleet.
	stopNone stopSource = iota
	// stopOperator is Manager.StopBot and Manager.Disconnect: the panel's stop and disconnect,
	// bulk stop, a stream take-over, cmd/fleet's dashboard. The explicit ones.
	stopOperator
	// stopWatchdog is the watchdog's own restart and relogin. It starts the script again itself,
	// and if it cannot the safety net does.
	stopWatchdog
	// stopHop is a world hop's logout, which restarts the script itself after the new login.
	stopHop
)

// kind is the stallwatch name for the same four.
func (s stopSource) kind() stallwatch.StopKind {
	switch s {
	case stopOperator:
		return stallwatch.StopOperator
	case stopWatchdog:
		return stallwatch.StopWatchdog
	case stopHop:
		return stallwatch.StopHop
	}
	return stallwatch.StopNone
}

// noteStopLocked records that by asked the current bot to stop. a.mu must be held. An operator's
// stop is never downgraded by a later one: the watchdog stopping a bot that an operator had just
// stopped would otherwise turn "leave it alone" back into "start it again".
func (a *account) noteStopLocked(by stopSource) {
	if a.stopBy == stopOperator && by != stopOperator {
		return
	}
	a.stopBy = by
}
