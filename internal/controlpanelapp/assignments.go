package controlpanelapp

import (
	"fmt"
	"log"
	"strings"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/controlpanel"
)

// Saved script assignments at process start (internal/controlpanel/assignments.go has the rules).

// openScriptAssignments opens the -script-assignments file. "", "off", "false" and "none" turn
// the feature off -- the rollback runbook spells it "-script-assignments=false", and a flag that
// silently created a file called "false" instead would be a trap. A file that cannot be read is
// reported and set aside by OpenScriptAssignments; the fleet starts anyway, from the roster.
func openScriptAssignments(path string) *controlpanel.ScriptAssignments {
	switch strings.ToLower(strings.TrimSpace(path)) {
	case "", "off", "false", "none":
		log.Printf("controlpanel: saved script assignments are OFF (-script-assignments %q): every account starts from the roster and panel switches last until the next restart", path)
		return nil
	}
	store, err := controlpanel.OpenScriptAssignments(path)
	if err != nil {
		log.Printf("controlpanel: WARNING: %v", err)
	}
	if store != nil {
		log.Printf("controlpanel: saving panel script starts to %s (%d saved now); they replace the roster's script at each start", store.Path(), len(store.List()))
	}
	return store
}

// scriptRegistered reports whether this build has a bot registered under name.
func scriptRegistered(name string) bool {
	for _, n := range bot.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// rosterLookup answers what the roster configures for an account, matched the way the game
// matches names (case-insensitive, underscore equals space), for FleetSource's assignment rules.
func rosterLookup(accounts []accountConfig) func(account string) (controlpanel.RosterEntry, bool) {
	byKey := make(map[string]controlpanel.RosterEntry, len(accounts))
	for _, a := range accounts {
		byKey[rosterKey(a.Username)] = controlpanel.RosterEntry{Script: a.Script, Settings: a.Settings}
	}
	return func(account string) (controlpanel.RosterEntry, bool) {
		e, ok := byKey[rosterKey(account)]
		return e, ok
	}
}

func rosterKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", " "))
}

// describeAssignments logs, once at start, how the saved file meets this roster: which entries
// will replace a roster script, which name an account this roster does not connect (kept, not
// deleted -- the roster may have been trimmed for one run), and which name a script this build
// does not have (the roster wins for those).
func describeAssignments(store *controlpanel.ScriptAssignments, roster func(string) (controlpanel.RosterEntry, bool)) {
	list := store.List()
	if len(list) == 0 {
		return
	}
	apply, absent, unknown := 0, []string{}, []string{}
	for _, a := range list {
		if _, ok := roster(a.Account); !ok {
			absent = append(absent, a.Account)
			continue
		}
		if !scriptRegistered(a.Script) {
			unknown = append(unknown, fmt.Sprintf("%s (%s)", a.Account, a.Script))
			continue
		}
		apply++
	}
	log.Printf("controlpanel: %d saved script assignment(s): %d replace a roster script at this start", len(list), apply)
	if len(absent) > 0 {
		log.Printf("controlpanel: %d saved assignment(s) name accounts this run does not connect, kept for a later run: %s", len(absent), strings.Join(absent, ", "))
	}
	if len(unknown) > 0 {
		log.Printf("controlpanel: WARNING: %d saved assignment(s) name a script this build has not registered, so those accounts start on their roster script: %s", len(unknown), strings.Join(unknown, ", "))
	}
}

// startupScript is what the process starts roster account a on: its saved assignment when it
// has one naming a registered script, else its roster entry. why is a log line when an
// assignment was involved at all, "" when the roster simply applies.
func startupScript(a accountConfig, store *controlpanel.ScriptAssignments, registered func(string) bool) (script string, settings map[string]interface{}, why string) {
	saved, ok := store.Get(a.Username)
	if !ok {
		return a.Script, a.Settings, ""
	}
	if !registered(saved.Script) {
		return a.Script, a.Settings, fmt.Sprintf("saved script assignment names %q, which this build has not registered; starting the roster's %q instead (the entry is kept)", saved.Script, a.Script)
	}
	return saved.Script, saved.Settings, fmt.Sprintf("starting %s from its saved script assignment (since %s) instead of the roster's %q", saved.Script, saved.Since, a.Script)
}

// startWorlder is the one fleet.Manager method startupWorld needs.
type startWorlder interface {
	StartWorld(roster int, settings map[string]interface{}) int
}

// startupWorld is the world a process start logs roster account a in on, chosen once startupScript
// has picked what it will run: the script's own homeWorld when it names one the deployment lists
// (fleet.Manager.StartWorld), else the roster's world. An account that starts no script keeps the
// roster's world, since nothing it runs has a home to ask for.
func startupWorld(mgr startWorlder, a accountConfig, script string, settings map[string]interface{}) int {
	if script == "" {
		return a.World
	}
	return mgr.StartWorld(a.World, settings)
}
