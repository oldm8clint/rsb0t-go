package swarmrally

import (
	"fmt"
	"sync"
)

// The process-wide registry.
//
// Every account in this project is a goroutine in ONE process, which is the same fact
// internal/mulebus's doc comment leans on: "the transport collapses to a shared, thread-safe,
// in-memory pub-sub". A rally is the same shape of problem. Scripts running under
// cmd/controlpanel do not get a Rally handed to them — bot.Context carries a *mulebus.Bus and
// nothing else of this kind — and threading a new field through Context would touch a file
// three other sessions are working in.
//
// So a rally is looked up BY NAME. The name is a setting on the script, which means two
// different cohorts (say a Kalphite Queen swarm and some future boss) can each have their own
// without knowing about each other, and an account that names no rally gets none at all.
var (
	sharedMu sync.Mutex
	shared   = map[string]*Rally{}
)

// Shared returns the rally called name, creating it from p the first time. Later callers get
// the SAME rally and p is ignored — the first account through the door sets the rule, and a
// later one that disagreed would otherwise silently split the cohort in two.
//
// The returned bool says whether this call created it, so a caller can log the policy once.
func Shared(name string, p Policy) (*Rally, bool, error) {
	if name == "" {
		return nil, false, fmt.Errorf("a rally needs a name; an empty one would pool every script in the process")
	}
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if r, ok := shared[name]; ok {
		return r, false, nil
	}
	r, err := New(p, nil)
	if err != nil {
		return nil, false, err
	}
	shared[name] = r
	return r, true, nil
}

// Lookup returns an existing rally, or nil. It never creates one, so a reader (a panel, a log
// line) cannot accidentally open a gather that no account asked for.
func Lookup(name string) *Rally {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	return shared[name]
}

// Names lists the open rallies, for a panel or a status line.
func Names() []string {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	out := make([]string, 0, len(shared))
	for k := range shared {
		out = append(out, k)
	}
	return out
}

// Forget drops a rally. It exists for tests and for a harness that runs several cohorts in one
// process; a live fleet never calls it, because a forgotten rally that accounts are still
// heartbeating into would silently become a second, empty one.
func Forget(name string) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	delete(shared, name)
}
