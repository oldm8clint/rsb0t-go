package gamestate

import (
	"fmt"
	"time"
)

// HOW OLD THE VIEW IS (ogresafe-1017, 17 Oct 2026).
//
// Everything a script reads from State (the player's tile, hitpoints, the NPCs around it) is the last PLAYER_INFO the
// client handled. The server runs on, and takes the account's clicks, whether or not the answer reaches the client. On
// 17 Oct 12:21:27 both local Psiphon tunnels hung for about 105 seconds: no byte came down for most of the fleet's
// proxied accounts, and when the tunnels came back the whole backlog arrived in about eight seconds (a hundred and
// seventy-odd ticks, "moved 10 tiles in one poll" on two dozen accounts at once). In between, a ScarabSwarm account on a
// safespot saw itself standing still, "stalled", clicked again from the tile it saw, learned that every exit of its
// safespot was blocked, started a 300-tile errand walk, and died in a pen of Ogres it had left on the strength of those
// clicks, with sixteen food in its pack and a view that said it was at full hitpoints on the safespot.
//
// Two states of the view are not the world's: SILENT (no tick for ViewStaleAfter: the view is the past, and nothing it
// says about the account's tile, hitpoints or surroundings is current) and CATCHING UP (ViewCatchUpTicks or more ticks in
// the last ViewWindow, where the server's own pace puts five: the backlog being read, in a stream or in clumps; the tile
// moves ten or thirty tiles a poll and a click started from it walks the account back along the way it came). The first
// ViewSettle after a silence counts as catching up too: the backlog follows the first tick back by milliseconds or by
// seconds. A script that reads either as the world acts on a picture that is seconds to minutes old, so every layer that
// sends clicks asks ViewLagging first and waits (webwalk.ViewClient, scarabrunner.ViewLagging).
//
// HOW LONG AGO THE VIEW LAST LAGGED (lageat-1018, 18 Oct 2026). ViewLag.Lagging says only whether the view is behind NOW. From
// 01:30 on 18 Oct the server lagged in episodes every ten to forty-five seconds on every route, and an account that is fed and
// healthy when an episode starts can be dead before it ends: at 04:41 a Paladin thief at 27 of 54 hitpoints, whose last tick was
// 04:41:36, read nothing for seven seconds and then fifteen ticks at once, and the Watchman its steal had raised hit it for 11, 9
// and 7 in between. Every eat rule above this package works from the hitpoints the view shows, so the margin each keeps has to
// cover what an episode can hide. ViewLag.Recent is that fact: the most ticks one episode hid in the last ViewLagMemory
// (RecentLag), kept per episode so one long silence is not forgotten by the short backlog that followed it.

const (
	// ViewStaleAfter is how long without a PLAYER_INFO the view counts as silent. A tick is 600 ms; a server hiccup of
	// a second or two is not a stall, five missed ticks is.
	ViewStaleAfter = 3 * time.Second
	// ViewWindow and ViewCatchUpTicks: this many ticks within the window is a backlog. The server's pace puts five (six at
	// a window's edge) in three seconds; a view that holds ten has been read ahead of the clock by two seconds or more.
	ViewWindow       = 3 * time.Second
	ViewCatchUpTicks = 10
	// ViewSettle is how long after a silence the view still counts as catching up whatever the window holds.
	ViewSettle = 2 * time.Second
	// viewMinTicks is how many ticks a session must have seen before its view is judged at all: a state that has had one
	// tick (a fresh login, a simulated session placed once) has no rhythm to be late against.
	viewMinTicks = 3
	// viewRing is how many tick times are kept; it must exceed ViewCatchUpTicks.
	viewRing = 16
	// ViewTick is the server's tick, which turns a silence into the ticks it hid.
	ViewTick = 600 * time.Millisecond
	// ViewLagMemory is how long after an episode the view still counts as having lagged recently. The incident's episodes came
	// 10 to 43 seconds apart (04:40:08, 04:40:30, 04:41:13, 04:41:23, 04:41:42, 04:41:55): the next one is more likely than not
	// inside a minute, and three minutes is four times the longest gap between two of those.
	ViewLagMemory = 3 * time.Minute
	// viewEpisodes is how many episodes are kept (the memory can hold far more than the worst one needs).
	viewEpisodes = 8
	// viewEpisodeMerge is how close two lagging readings must be to be one episode: the window a backlog is judged in plus the
	// settling after a silence, so the backlog that follows a silence is the same episode.
	viewEpisodeMerge = ViewWindow + ViewSettle
)

// viewNow is the clock the view's ticks are stamped with; a test replaces it.
var viewNow = time.Now

// SetViewClock replaces the clock the view's ticks and reads are stamped with, and returns the function that puts the old one back.
// TEST SUPPORT, for a replay of a recorded lag outside this package: the minute of play around an incident delivered in milliseconds
// and stamped as its log saw it. Nothing else may be running against a State while it is swapped.
func SetViewClock(now func() time.Time) (restore func()) {
	prev := viewNow
	viewNow = now
	return func() { viewNow = prev }
}

// viewClock is when PLAYER_INFO ticks arrived. Guarded by State.mu like the rest of sceneTracker.
type viewClock struct {
	// count is the ticks seen, saturating at viewMinTicks.
	count int
	// ring holds the last viewRing tick times, head the next slot.
	ring [viewRing]time.Time
	head int
	last time.Time
	// silence is the last gap over ViewStaleAfter, settleUntil when its settling ends, and sinceSilence the ticks since it
	// ended (the first tick back counted).
	silence      time.Duration
	silenceEnded time.Time
	settleUntil  time.Time
	sinceSilence int
	// episodes is the last viewEpisodes lag episodes, epHead the next slot and epNewest the latest one written.
	episodes    [viewEpisodes]lagEpisode
	epHead      int
	epNewest    int
	haveEpisode bool
}

// lagEpisode is one stretch of lag: when it was last seen and the most ticks it hid.
type lagEpisode struct {
	at    time.Time
	ticks int
}

// ticksIn is how many server ticks a stretch of d holds, rounded up.
func ticksIn(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + ViewTick - 1) / ViewTick)
}

// episode records lag seen at now that hid ticks ticks: into the newest episode when it is close enough to be the same one, else
// as a new one.
func (v *viewClock) episode(now time.Time, ticks int) {
	if ticks <= 0 {
		return
	}
	if v.haveEpisode {
		if e := &v.episodes[v.epNewest]; now.Sub(e.at) <= viewEpisodeMerge {
			e.at = now
			e.ticks = max(e.ticks, ticks)
			return
		}
	}
	v.epNewest = v.epHead
	v.episodes[v.epHead] = lagEpisode{at: now, ticks: ticks}
	v.epHead = (v.epHead + 1) % viewEpisodes
	v.haveEpisode = true
}

// inWindow is how many ticks the ring holds within ViewWindow of now.
func (v viewClock) inWindow(now time.Time) int {
	n := 0
	for _, t := range v.ring {
		if !t.IsZero() && now.Sub(t) <= ViewWindow {
			n++
		}
	}
	return n
}

// note records a tick arriving at now.
func (v *viewClock) note(now time.Time) {
	if v.count < viewMinTicks {
		v.count++
	}
	if !v.last.IsZero() {
		if gap := now.Sub(v.last); gap > ViewStaleAfter {
			v.silence, v.silenceEnded, v.settleUntil, v.sinceSilence = gap, now, now.Add(ViewSettle), 0
			v.episode(now, ticksIn(gap)) // the ticks the server ran that no byte told the client of
		}
	}
	if v.sinceSilence < 1<<30 {
		v.sinceSilence++
	}
	v.ring[v.head] = now
	v.head = (v.head + 1) % viewRing
	v.last = now
	if v.count >= viewMinTicks {
		// A backlog read ahead of the clock: the view was this many ticks behind when its newest one arrived.
		if n := v.inWindow(now); n >= ViewCatchUpTicks {
			v.episode(now, n)
		}
	}
}

// ViewLag is the state of the view at one instant.
type ViewLag struct {
	// Lagging is Silent or CatchingUp: nothing the view says may be acted on as current.
	Lagging bool
	// Silent is true when no tick has arrived for ViewStaleAfter.
	Silent bool
	// CatchingUp is true while a backlog is being read (ViewCatchUpTicks in ViewWindow) or ViewSettle after a silence.
	CatchingUp bool
	// Age is the wall time since the last tick (0 before the first).
	Age time.Duration
	// InWindow is the ticks of the last ViewWindow.
	InWindow int
	// Silence is the last gap over ViewStaleAfter (0 for none yet), SilenceEnded when it ended and SinceSilence the ticks that
	// have arrived since, the first tick back included: what the view was missing and what it has read since.
	Silence      time.Duration
	SilenceEnded time.Time
	SinceSilence int
	// Recent is the worst episode of the last ViewLagMemory, whether or not the view is lagging now.
	Recent RecentLag
}

// RecentLag is what the view's recent lag episodes hid: the most ticks one of them hid, and how long ago it was last seen. The
// zero value is no lag within ViewLagMemory. An episode still going on counts, at the ticks it has hidden so far (Ago 0).
type RecentLag struct {
	Ticks int
	Ago   time.Duration
}

// Why says what is wrong with the view, for a log line; "" when nothing is.
func (l ViewLag) Why() string {
	switch {
	case l.Silent:
		return fmt.Sprintf("no game tick for %s", l.Age.Round(100*time.Millisecond))
	case l.CatchingUp && l.InWindow >= ViewCatchUpTicks:
		return fmt.Sprintf("%d ticks in the last %s: a backlog is being read", l.InWindow, ViewWindow)
	case l.CatchingUp:
		return fmt.Sprintf("the view has just come back after %s of silence", l.Silence.Round(time.Second))
	}
	return ""
}

// read is the view's state at now for an account that is in the game.
func (v viewClock) read(now time.Time) ViewLag {
	if v.last.IsZero() || v.count < viewMinTicks {
		return ViewLag{}
	}
	l := ViewLag{Age: now.Sub(v.last), Silence: v.silence, SilenceEnded: v.silenceEnded, SinceSilence: v.sinceSilence}
	l.InWindow = v.inWindow(now)
	l.Silent = l.Age > ViewStaleAfter
	l.CatchingUp = l.InWindow >= ViewCatchUpTicks || (!v.settleUntil.IsZero() && now.Before(v.settleUntil))
	l.Lagging = l.Silent || l.CatchingUp
	l.Recent = v.recent(now, ViewLagMemory)
	return l
}

// recent is the worst episode within the memory at now: the most ticks hidden (the newest among equals), and a silence in progress
// at the ticks it has hidden so far.
func (v viewClock) recent(now time.Time, within time.Duration) RecentLag {
	var r RecentLag
	for _, e := range v.episodes {
		if e.at.IsZero() {
			continue
		}
		ago := now.Sub(e.at)
		if ago > within {
			continue
		}
		if e.ticks > r.Ticks || (e.ticks == r.Ticks && ago < r.Ago) {
			r = RecentLag{Ticks: e.ticks, Ago: ago}
		}
	}
	if !v.last.IsZero() {
		if age := now.Sub(v.last); age > ViewStaleAfter {
			if t := ticksIn(age); t > r.Ticks {
				r = RecentLag{Ticks: t}
			}
		}
	}
	return r
}

// ViewLag reports how old the view is. An account that is not in the game, or has not seen viewMinTicks ticks yet, is not
// lagging.
func (s *State) ViewLag() ViewLag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.scene.viewOverride != nil {
		return *s.scene.viewOverride
	}
	if s.LoggedOut || s.scene.tick == 0 {
		return ViewLag{}
	}
	return s.scene.view.read(viewNow())
}

// OverrideViewLag makes ViewLag report l whatever the ticks say, until it is called again with nil. For the simulated sessions
// (which deliver ticks as fast as a test asks, and would read as a backlog) and for tests of code that reacts to a lagging view.
func (s *State) OverrideViewLag(l *ViewLag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l == nil {
		s.scene.viewOverride = nil
		return
	}
	cp := *l
	s.scene.viewOverride = &cp
}

// ViewLagging reports whether the view is silent or catching up (ViewLag.Lagging).
func (s *State) ViewLagging() bool { return s.ViewLag().Lagging }

// RecentViewLag is the worst lag episode of the last ViewLagMemory (ViewLag.Recent): what the eat rules keep a margin for.
func (s *State) RecentViewLag() RecentLag { return s.ViewLag().Recent }
