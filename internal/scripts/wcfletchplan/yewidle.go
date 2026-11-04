package wcfletchplan

// How long a yew-rung account stands waiting, counted, and said every quarter hour.
//
// The only measure of idle the fleet log had was a count of "no yew in sight - waiting for one to
// grow back" lines, which are not seconds (one a pass, and a pass is two seconds plus whatever the
// chop took), say nothing of the lines the woodcutter's own warning repeats, and are not summed
// anywhere. 98,198 of them in 19.7 hours was the whole finding. IdleClock counts the wall time of the
// passes that found nothing to chop, per account, and IdleLedger adds the accounts up by world and
// camp, so one line a quarter hour says what the cohort lost and where.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// IdleReportEvery is how often an account reports its idle seconds, and the ledger flushes.
const IdleReportEvery = 15 * time.Minute

// StumpSpellLogAfter is how long an account must have waited in one unbroken spell for the log to say
// every yew at its camp is a stump.
const StumpSpellLogAfter = 20 * time.Second

// IdleKind is why a pass chopped nothing.
type IdleKind int

const (
	// IdleStumps: no tree of the species was standing in reach ("no-tree").
	IdleStumps IdleKind = iota
	// IdleExcluded: trees stood but every one was excluded (an Ent at it, no stand tile, a swing that
	// gave nothing: "all-blocked").
	IdleExcluded
)

// IdleClock is one account's idle time over the report window. The zero value is ready.
type IdleClock struct {
	start    time.Time
	stump    time.Duration
	excluded time.Duration
	spells   int
	inSpell  bool
	spell    time.Duration
}

// Pass records a pass that chopped nothing and took d.
func (c *IdleClock) Pass(kind IdleKind, d time.Duration, now time.Time) {
	c.begin(now)
	if d < 0 {
		d = 0
	}
	switch kind {
	case IdleExcluded:
		c.excluded += d
	default:
		c.stump += d
	}
	if !c.inSpell {
		c.inSpell, c.spell = true, 0
		c.spells++
	}
	c.spell += d
}

// Worked records a pass that did something else, which ends the spell of waiting.
func (c *IdleClock) Worked(now time.Time) {
	c.begin(now)
	c.inSpell, c.spell = false, 0
}

func (c *IdleClock) begin(now time.Time) {
	if c.start.IsZero() {
		c.start = now
	}
}

// Spell is how long the unbroken spell of waiting in progress has lasted (0 when not waiting).
func (c *IdleClock) Spell() time.Duration {
	if !c.inSpell {
		return 0
	}
	return c.spell
}

// Due reports whether the window is IdleReportEvery old at now.
func (c *IdleClock) Due(now time.Time) bool {
	return !c.start.IsZero() && now.Sub(c.start) >= IdleReportEvery
}

// IdleReport is one account's window.
type IdleReport struct {
	// Window is how long the window lasted; Stump and Excluded are the idle time inside it.
	Window, Stump, Excluded time.Duration
	// Spells is how many separate spells of waiting there were.
	Spells int
}

// Idle is Stump plus Excluded.
func (r IdleReport) Idle() time.Duration { return r.Stump + r.Excluded }

// Fraction is the share of the window spent waiting, 0 for an empty window.
func (r IdleReport) Fraction() float64 {
	if r.Window <= 0 {
		return 0
	}
	f := float64(r.Idle()) / float64(r.Window)
	if f > 1 {
		f = 1
	}
	return f
}

// Take returns the window ending at now and starts the next. A spell in progress carries over.
func (c *IdleClock) Take(now time.Time) IdleReport {
	c.begin(now)
	r := IdleReport{Window: now.Sub(c.start), Stump: c.stump, Excluded: c.excluded, Spells: c.spells}
	c.start, c.stump, c.excluded, c.spells = now, 0, 0, 0
	if c.inSpell {
		c.spells = 1
	}
	return r
}

// secs rounds a duration to whole seconds for a line.
func secs(d time.Duration) int { return int((d + 500*time.Millisecond) / time.Second) }

// Describe is the report as the first half of its log line.
func (r IdleReport) Describe() string {
	return fmt.Sprintf("%ds waiting = %.1f%% of %ds (%ds no yew standing in %d spell(s), %ds every yew excluded)",
		secs(r.Idle()), 100*r.Fraction(), secs(r.Window), secs(r.Stump), r.Spells, secs(r.Excluded))
}

// ---- the cohort's ledger ----

type ledgerRow struct {
	accounts           int
	window, stump, exc time.Duration
}

// IdleLedger adds the accounts' reports up by world, so one line a quarter hour speaks for the cohort.
type IdleLedger struct {
	mu    sync.Mutex
	since time.Time
	rows  map[int]*ledgerRow
}

// NewIdleLedger returns an empty ledger.
func NewIdleLedger() *IdleLedger { return &IdleLedger{rows: map[int]*ledgerRow{}} }

var sharedIdleLedger = NewIdleLedger()

// SharedIdleLedger is the process-wide ledger.
func SharedIdleLedger() *IdleLedger { return sharedIdleLedger }

// Add records one account's report for world at now and, when the ledger is IdleReportEvery old,
// flushes it: the cohort line and true. The report that tips the window over is counted in it.
func (l *IdleLedger) Add(world int, r IdleReport, now time.Time) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.since.IsZero() {
		l.since = now
	}
	row := l.rows[world]
	if row == nil {
		row = &ledgerRow{}
		l.rows[world] = row
	}
	row.accounts++
	row.window += r.Window
	row.stump += r.Stump
	row.exc += r.Excluded
	if now.Sub(l.since) < IdleReportEvery {
		return "", false
	}
	line := l.describeLocked(now.Sub(l.since))
	l.since = now
	l.rows = map[int]*ledgerRow{}
	return line, true
}

func (l *IdleLedger) describeLocked(span time.Duration) string {
	worlds := make([]int, 0, len(l.rows))
	var all ledgerRow
	for w, r := range l.rows {
		worlds = append(worlds, w)
		all.accounts += r.accounts
		all.window += r.window
		all.stump += r.stump
		all.exc += r.exc
	}
	sort.Ints(worlds)
	pct := func(r ledgerRow) float64 {
		if r.window <= 0 {
			return 0
		}
		return 100 * float64(r.stump+r.exc) / float64(r.window)
	}
	parts := make([]string, 0, len(worlds))
	for _, w := range worlds {
		r := l.rows[w]
		parts = append(parts, fmt.Sprintf("world %d %.1f%% (%d report(s), %ds idle of %ds)", w, pct(*r), r.accounts, secs(r.stump+r.exc), secs(r.window)))
	}
	return fmt.Sprintf("wcfletch: cohort idle over the last %d min: %.1f%% (%ds waiting of %ds account time, %d report(s)); %s",
		secs(span)/60, pct(all), secs(all.stump+all.exc), secs(all.window), all.accounts, strings.Join(parts, "; "))
}
