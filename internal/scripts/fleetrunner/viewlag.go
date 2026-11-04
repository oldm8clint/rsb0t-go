package fleetrunner

import (
	"sync"
	"time"
)

// WHAT ScarabSwarm DOES WHEN ITS VIEW OF THE GAME IS NOT CURRENT (ogresafe-1017, 17 Oct 2026).
//
// gamestate.State.ViewLag says the view is silent (no tick for three seconds) or catching up on a backlog. The server
// runs on either way and takes every click the account sends, so a script that acts on the picture it has acts on a
// picture that is seconds to minutes old: that is how an account on a safespot "stalled", clicked again from the tile it
// saw, and walked a pen of Ogres on 17 Oct 12:22. So nothing that moves the account or starts something that will starts
// while the view lags: not a cast, not a walk, not an errand. The walker holds its own clicks (webwalk/lag.go); this is
// the rest, and the one place that says so in the log.

// lagMemo is what ViewLagging last told the log about one account.
type lagMemo struct {
	lagging bool
	since   time.Time
}

var lagMemos = struct {
	sync.Mutex
	m map[string]*lagMemo
}{m: map[string]*lagMemo{}}

// ViewLagging reports whether the account's view of the game is silent or catching up, and says so in the log once each time
// that changes: what is wrong when it starts, and how long it lasted and what the replay was when it ends.
func ViewLagging(d *Deps) bool {
	if d == nil || d.Session == nil {
		return false
	}
	st := d.Session.State()
	if st == nil {
		return false
	}
	l := st.ViewLag()
	key := SafeGapKey(d)
	lagMemos.Lock()
	m := lagMemos.m[key]
	if m == nil {
		m = &lagMemo{}
		lagMemos.m[key] = m
	}
	changed := m.lagging != l.Lagging
	var since time.Time
	if changed {
		m.lagging = l.Lagging
		if l.Lagging {
			m.since = time.Now()
		}
		since = m.since
	}
	lagMemos.Unlock()
	if changed {
		if l.Lagging {
			d.Logf("view lag: %s — the connection is not delivering the game; holding every cast, walk and errand until it does", l.Why())
		} else if l.Silence > 0 && l.SilenceEnded.After(since) {
			d.Logf("view lag over after %s: %d ticks have arrived since the %s of silence before them — resuming from the account's real tile",
				time.Since(since).Round(time.Second), l.SinceSilence, l.Silence.Round(time.Second))
		} else {
			d.Logf("view lag over after %s — resuming", time.Since(since).Round(time.Second))
		}
	}
	return l.Lagging
}
