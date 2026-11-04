package questevidence

import (
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Attribution says where a build, or a moment, stands against the module's last change.
type Attribution int

const (
	// Unknown means it cannot be placed: a build the repository does not know, one made from a modified
	// tree, or a log file that named none. Unknown evidence is never counted, only reported.
	Unknown Attribution = iota
	// Before means it predates the module's last change: it ran the old module.
	Before
	// Since means it holds the module's last change.
	Since
)

// Window is "since the module last changed" for one quest.
type Window struct {
	// Bounded is false when nothing says where the module's last change is. Everything is counted so the
	// numbers can be read, but nothing can pass the rule: an unbounded window is not "all of history is
	// since the change".
	Bounded bool
	// Change describes the last change for the report (a commit and its time).
	Change string
	// Contains places a build against the change, by asking git (the build is the 40 hex digits, "+dirty"
	// already stripped). Nil in time mode.
	Contains func(build string) Attribution
	// From is the time floor of time mode (an explicit -changed or -since): evidence stamped at or after it
	// counts. Ignored when Contains is set.
	From time.Time
	// AllowDirty counts builds made from a modified tree, whose revision names the commit they started from
	// and not what they ran.
	AllowDirty bool
}

// classify places one piece of evidence: the build it ran on (empty for none) and when.
func (w Window) classify(build string, dirty bool, at time.Time) Attribution {
	if w.Contains != nil {
		if build == "" || build == "unknown" {
			return Unknown
		}
		if strings.HasSuffix(build, "+dirty") {
			dirty = true
			build = strings.TrimSuffix(build, "+dirty")
		}
		if dirty && !w.AllowDirty {
			return Unknown
		}
		return w.Contains(build)
	}
	if !w.From.IsZero() && at.Before(w.From) {
		return Before
	}
	return Since
}

// legacyFloor is when a completion that names no build (a store's seenComplete, stamped only with when the
// record last changed) becomes admissible: the first moment a build holding the change went live. ok is
// false when no log segment shows one, so no such completion can be placed.
func (w Window) legacyFloor(segments []Segment) (floor time.Time, build string, ok bool) {
	if w.Contains == nil {
		return w.From, "", true
	}
	for _, sg := range segments {
		if w.classify(sg.Build, sg.Dirty, sg.Start) == Since {
			return sg.Start, strings.TrimSuffix(sg.Build, "+dirty"), true
		}
	}
	return time.Time{}, "", false
}

// StoreFile is one account's quest side store.
type StoreFile struct {
	Account string
	Store   *questplan.Store
}

// Input is everything read: the log events and builds in time order, and the side stores.
type Input struct {
	events   map[string][]Event
	Segments []Segment
	Stores   []StoreFile
}

// NewInput indexes events by quest.
func NewInput(events []Event, segments []Segment, stores []StoreFile) *Input {
	in := &Input{events: map[string][]Event{}, Segments: segments, Stores: stores}
	for _, e := range events {
		in.events[e.Quest] = append(in.events[e.Quest], e)
	}
	return in
}

// Quests lists every quest id with any event or store record, sorted.
func (in *Input) Quests() []string {
	seen := map[string]bool{}
	for q := range in.events {
		seen[q] = true
	}
	for _, sf := range in.Stores {
		if sf.Store == nil {
			continue
		}
		for q := range sf.Store.Quests {
			seen[q] = true
		}
	}
	out := make([]string, 0, len(seen))
	for q := range seen {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

// BuildRow is one build's share of a quest's in-window evidence.
type BuildRow struct {
	Build       string    `json:"build"`
	First       time.Time `json:"first"`
	Runs        int       `json:"runs"`
	Completions int       `json:"completions"`
	Parks       int       `json:"parks"`
	Deaths      int       `json:"deaths"`
}

// Evidence is what one quest has done since its module last changed.
type Evidence struct {
	Quest  string `json:"quest"`
	Window Window `json:"-"`
	// Bounded and Change repeat the window for JSON.
	Bounded bool   `json:"bounded"`
	Change  string `json:"change,omitempty"`
	// FirstBuild and FirstAt are the first build holding the change that appears in the logs, and when it
	// went live (git mode).
	FirstBuild string    `json:"firstBuild,omitempty"`
	FirstAt    time.Time `json:"firstAt,omitempty"`

	// Completions is the completions counted and Accounts how many distinct accounts they came from. A quest
	// is finished once per account, so each account counts once: further "ended complete" lines for an
	// account that already completed it are Repeats (a loop, not evidence; the Barcrawl logs one tens of
	// thousands of times).
	Completions int      `json:"completions"`
	Accounts    int      `json:"accounts"`
	Repeats     int      `json:"repeats"`
	AccountList []string `json:"accountList,omitempty"`
	// Where they came from. An account seen in several is counted once, so these can sum to more than
	// Accounts.
	FromLog      int `json:"fromLog"`      // "[ladder] <id> ended complete" and the engine's COMPLETED line
	FromRecorded int `json:"fromRecorded"` // the store's Completions
	FromLegacy   int `json:"fromLegacy"`   // a store's seenComplete, placed by when its record last changed
	// Left out of the count, and why.
	Unearned     int `json:"unearned"`     // completions on the first decision: the quest was done before the run
	Unattributed int `json:"unattributed"` // evidence on a build that cannot be placed against the change
	LegacyBefore int `json:"legacyBefore"` // seenComplete records last changed before the change went live

	Runs      int `json:"runs"`
	Parks     int `json:"parks"`
	Timeouts  int `json:"timeouts"`
	Stopped   int `json:"stopped"`
	Died      int `json:"died"`
	LostFight int `json:"lostFight"`
	// DeathAccounts is how many accounts died or lost a fight.
	DeathAccounts int `json:"deathAccounts"`

	Builds []BuildRow `json:"builds,omitempty"`
}

// Deaths is the lead's "deaths": a death and a fight lost and walked away from both count.
func (e Evidence) Deaths() int { return e.Died + e.LostFight }

// ParksPerRun is parks over ladder runs; ok is false with no runs.
func (e Evidence) ParksPerRun() (float64, bool) {
	if e.Runs == 0 {
		return 0, false
	}
	return float64(e.Parks) / float64(e.Runs), true
}

// norm is the key an account is counted under: the game's name and the store directory's spelling (spaces
// are underscores there) are the same player, in either case.
func norm(account string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(account), " ", "_"))
}

type acctTally struct {
	display          string
	logComplete      int
	loggedCompleted  int
	recorded         int
	legacy           int
	hadAny           bool
	died, lostFights int
}

// Evaluate counts one quest's evidence inside the window.
func Evaluate(quest string, in *Input, w Window) Evidence {
	ev := Evidence{Quest: quest, Window: w, Bounded: w.Bounded, Change: w.Change}
	floor, floorBuild, floorOK := w.legacyFloor(in.Segments)
	if floorOK && floorBuild != "" {
		for _, sg := range in.Segments {
			if strings.TrimSuffix(sg.Build, "+dirty") == floorBuild {
				ev.FirstBuild, ev.FirstAt = floorBuild, sg.Start
				break
			}
		}
	}

	accts := map[string]*acctTally{}
	tally := func(account string) *acctTally {
		k := norm(account)
		a := accts[k]
		if a == nil {
			a = &acctTally{display: account}
			accts[k] = a
		}
		return a
	}
	rows := map[string]*BuildRow{}
	row := func(build string, at time.Time) *BuildRow {
		b := strings.TrimSuffix(build, "+dirty")
		if b == "" {
			b = "unknown"
		}
		r := rows[b]
		if r == nil {
			r = &BuildRow{Build: b, First: at}
			rows[b] = r
		}
		if at.Before(r.First) {
			r.First = at
		}
		return r
	}

	for _, e := range in.events[quest] {
		switch w.classify(e.Build, e.Dirty, e.At) {
		case Unknown:
			ev.Unattributed++
			continue
		case Before:
			continue
		}
		r := row(e.Build, e.At)
		switch e.Kind {
		case Run:
			ev.Runs++
			r.Runs++
		case Parked:
			ev.Parks++
			r.Parks++
		case Timeout:
			ev.Timeouts++
		case Stopped:
			ev.Stopped++
		case Died:
			ev.Died++
			r.Deaths++
			tally(e.Account).died++
		case LostFight:
			ev.LostFight++
			r.Deaths++
			tally(e.Account).lostFights++
		case Complete:
			tally(e.Account).logComplete++
			r.Completions++
		case Completed:
			if e.Ticks == 1 {
				ev.Unearned++
				continue
			}
			tally(e.Account).loggedCompleted++
		}
	}

	for _, sf := range in.Stores {
		if sf.Store == nil {
			continue
		}
		rec := sf.Store.Quests[quest]
		if rec == nil {
			continue
		}
		if len(rec.Completions) > 0 {
			for _, c := range rec.Completions {
				at := time.UnixMilli(c.At)
				switch w.classify(c.Build, false, at) {
				case Unknown:
					ev.Unattributed++
					continue
				case Before:
					continue
				}
				if !c.Earned() {
					ev.Unearned++
					continue
				}
				account := c.Account
				if account == "" {
					account = sf.Account
				}
				tally(account).recorded++
			}
			continue
		}
		if !rec.SeenComplete {
			continue
		}
		if !floorOK {
			ev.Unattributed++
			continue
		}
		if at := time.UnixMilli(rec.UpdatedAt); !floor.IsZero() && at.Before(floor) {
			ev.LegacyBefore++
			continue
		}
		tally(sf.Account).legacy++
	}

	deathAccts := 0
	for _, a := range accts {
		n := maxInt(maxInt(a.logComplete, a.loggedCompleted), maxInt(a.recorded, a.legacy))
		if a.died+a.lostFights > 0 {
			deathAccts++
		}
		if n == 0 {
			continue
		}
		ev.Completions++
		ev.Accounts++
		ev.Repeats += n - 1
		ev.AccountList = append(ev.AccountList, a.display)
		if a.logComplete > 0 || a.loggedCompleted > 0 {
			ev.FromLog++
		}
		if a.recorded > 0 {
			ev.FromRecorded++
		}
		if a.legacy > 0 {
			ev.FromLegacy++
		}
	}
	ev.DeathAccounts = deathAccts
	sort.Slice(ev.AccountList, func(i, j int) bool { return norm(ev.AccountList[i]) < norm(ev.AccountList[j]) })

	for _, r := range rows {
		ev.Builds = append(ev.Builds, *r)
	}
	sort.Slice(ev.Builds, func(i, j int) bool { return ev.Builds[i].First.Before(ev.Builds[j].First) })
	return ev
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
