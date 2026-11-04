package controlpanel

import (
	"fmt"
	"log"
)

// Saved script assignments under -fake: the same ScriptAssignments rules FleetSource follows,
// with the accounts NewFakeSource made (and the script each was made with) standing in for the
// roster. It exists so the whole path -- a panel start saved to disk, a real process restart,
// the assignment replayed and reported at /api/status -- can be shown with no game connection.

// SetScriptAssignments turns saved assignments on for this fake fleet. The fleet as it stands
// now is the "roster": call this straight after NewFakeSource, before anything is started.
func (f *FakeSource) SetScriptAssignments(store *ScriptAssignments) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assignments = store
	f.fakeRoster = make(map[string]string, len(f.accounts))
	for _, a := range f.accounts {
		f.fakeRoster[assignmentKey(a.Name)] = a.Bot
	}
}

// ApplyScriptAssignments is cmd/controlpanel's start-up replay for a fake fleet: every account
// with a saved assignment is put on it, running, with the saved settings -- what a real fleet's
// auto-start does for a roster account. Any script name is accepted, exactly as the fake's own
// StartBot accepts one (a real fleet refuses a script this build has not registered; that check
// lives in cmd/controlpanel's startupScript). It returns one line per account for the log.
func (f *FakeSource) ApplyScriptAssignments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var lines []string
	for i := range f.accounts {
		a, ok := f.assignments.Get(f.accounts[i].Name)
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] starting %s from its saved assignment (since %s) instead of the roster's %q", f.accounts[i].Name, a.Script, a.Since, f.accounts[i].Bot))
		f.accounts[i].Bot = a.Script
		f.accounts[i].Settings = a.Settings
		f.accounts[i].BotState = "running"
		f.accounts[i].BotError = ""
	}
	return lines
}

func (f *FakeSource) rosterScriptLocked(account string) string {
	return f.fakeRoster[assignmentKey(account)]
}

// assignedStartSettings is FleetSource.resolveStartSettings' rule for account i, minus the
// roster-settings fallback (a fake roster has no settings). f.mu is held.
func (f *FakeSource) assignedStartSettings(i int, script string, supplied map[string]interface{}) map[string]interface{} {
	if saved, ok := f.assignments.StartSettings(f.accounts[i].Name, script, supplied, f.rosterScriptLocked(f.accounts[i].Name)); ok {
		return saved
	}
	return supplied
}

// noteAssignment records a successful start of account i. f.mu is held.
func (f *FakeSource) noteAssignment(i int, script string, supplied, resolved map[string]interface{}) {
	name := f.accounts[i].Name
	change, err := f.assignments.Note(name, script, supplied, resolved, f.rosterScriptLocked(name), nil)
	switch {
	case err != nil:
		log.Printf("controlpanel: fake: [%s] saving the script assignment failed: %v", name, err)
	case change == AssignmentSaved:
		log.Printf("controlpanel: fake: [%s] script assignment saved: %s", name, script)
	case change == AssignmentRemoved:
		log.Printf("controlpanel: fake: [%s] script assignment removed: back on %s", name, script)
	}
}

// ScriptAssignmentsView implements AssignmentControl.
func (f *FakeSource) ScriptAssignmentsView() AssignmentsView {
	f.mu.Lock()
	roster := make(map[string]string, len(f.fakeRoster))
	for k, v := range f.fakeRoster {
		roster[k] = v
	}
	store := f.assignments
	f.mu.Unlock()
	return viewOf(store, func(account string) (RosterEntry, bool) {
		script, ok := roster[assignmentKey(account)]
		return RosterEntry{Script: script}, ok
	})
}

// ClearScriptAssignment implements AssignmentControl.
func (f *FakeSource) ClearScriptAssignment(account string) (bool, error) {
	f.mu.Lock()
	store := f.assignments
	f.mu.Unlock()
	if !store.Enabled() {
		return false, errAssignmentsOff
	}
	return store.Clear(account)
}
