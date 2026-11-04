package thieverrunner

import (
	"reflect"
	"strconv"
	"sync"
	"time"
)

// restockLogEvery is how often one account may say "waiting for the stall to restock".
//
// StealStall is polled every second or two while a stall is empty, and the Baker's stall is shared
// by up to a few dozen ScarabSwarm accounts at once. Live 30 Sep - 2 Oct that one line was
// 1,910,268 of the fleet's 10.9M ScarabSwarm log lines (17.5 percent, from 277 accounts), in a
// 1.4 GB log nobody can read for the lines that matter. Once every half minute per account, with
// the count since the last, says the same thing.
const restockLogEvery = 30 * time.Second

// restockForget is how long an account's rate-limit entry may sit unused before a new account's
// first line sweeps it away. The key is the account's session, and a session that has gone (a
// reconnect builds a new one) must not be kept alive by this map.
const restockForget = 10 * time.Minute

type restockSay struct {
	at      time.Time
	skipped int
}

var (
	restockMu    sync.Mutex
	restockSays  = map[any]*restockSay{} // keyed by the account's session
	restockClock = time.Now              // replaced by tests
)

// restockLine returns the line to log for a stall that is empty, or "" when this account said it
// less than restockLogEvery ago. key is the account's session (comparable); stall its display name.
func restockLine(key any, stall string) string {
	if key == nil || !reflect.TypeOf(key).Comparable() {
		return "waiting for the " + stall + " to restock" // nothing to key a limit on: say it, as before
	}
	now := restockClock()
	restockMu.Lock()
	defer restockMu.Unlock()
	s := restockSays[key]
	if s == nil {
		for k, old := range restockSays {
			if now.Sub(old.at) > restockForget {
				delete(restockSays, k)
			}
		}
		s = &restockSay{}
		restockSays[key] = s
	}
	if !s.at.IsZero() && now.Sub(s.at) < restockLogEvery {
		s.skipped++
		return ""
	}
	line := "waiting for the " + stall + " to restock"
	if s.skipped > 0 {
		line += " (" + strconv.Itoa(s.skipped+1) + " checks in " + now.Sub(s.at).Round(time.Second).String() + ")"
	}
	s.at, s.skipped = now, 0
	return line
}
