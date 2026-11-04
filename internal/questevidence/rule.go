package questevidence

import (
	"fmt"
	"strings"
)

// Rule is the promotion evidence rule.
type Rule struct {
	// MinCompletions and MinAccounts: completions, on at least that many distinct accounts.
	MinCompletions, MinAccounts int
	// MaxDeaths counts both a death and a fight lost and walked away from (Evidence.Deaths).
	MaxDeaths int
	// MaxParksPerRun is the most parks per ladder run.
	MaxParksPerRun float64
}

// LeadRule is the rule the lead set on 15 October 2026 for promoting a quest into bot.ProvenQuests on
// live evidence: 10 or more completions on 10 or more distinct accounts since the module last changed,
// no deaths in it, and at most 0.5 parks per run.
var LeadRule = Rule{MinCompletions: 10, MinAccounts: 10, MaxDeaths: 0, MaxParksPerRun: 0.5}

// Verdict is a rule's answer for one quest. A quest passes only when every check ran and held: a check
// that could not run (no module-change point, no runs to divide parks by) is a failure, never a pass.
type Verdict struct {
	Pass     bool     `json:"pass"`
	Failures []string `json:"failures,omitempty"`
}

// Judge applies the rule to one quest's evidence.
func (r Rule) Judge(e Evidence) Verdict {
	var fail []string
	if !e.Bounded {
		fail = append(fail, "the module's last change is unknown (give -repo, or -changed "+e.Quest+"=<time>), so \"since the module changed\" is unbounded")
	}
	if e.Completions < r.MinCompletions {
		fail = append(fail, fmt.Sprintf("%d completion(s), need %d", e.Completions, r.MinCompletions))
	}
	if e.Accounts < r.MinAccounts {
		fail = append(fail, fmt.Sprintf("%d distinct account(s), need %d", e.Accounts, r.MinAccounts))
	}
	if e.Deaths() > r.MaxDeaths {
		fail = append(fail, fmt.Sprintf("%d death(s) (%d died, %d lost a fight), allowed %d", e.Deaths(), e.Died, e.LostFight, r.MaxDeaths))
	}
	if ppr, ok := e.ParksPerRun(); !ok {
		fail = append(fail, "no ladder runs in the logs since the change, so parks per run cannot be judged")
	} else if ppr > r.MaxParksPerRun {
		fail = append(fail, fmt.Sprintf("%.2f parks per run (%d parks in %d runs), allowed %.2f", ppr, e.Parks, e.Runs, r.MaxParksPerRun))
	}
	return Verdict{Pass: len(fail) == 0, Failures: fail}
}

// String is the verdict on one line.
func (v Verdict) String() string {
	if v.Pass {
		return "PASS"
	}
	return "FAIL: " + strings.Join(v.Failures, "; ")
}
