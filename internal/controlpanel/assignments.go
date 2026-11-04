package controlpanel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Saved script assignments: a script the panel started on an account, kept in a file beside the
// process so it is started again after a restart instead of the roster's.
//
// Why this exists. A panel switch (POST /api/bot/stop, then /api/bot/start with a new script)
// lives in memory only. cmd/controlpanel auto-starts every account from the roster
// (fleet.config.json) at process start, so every restart put a switched account back on its
// roster script from wherever it stood -- 27 archived runs on 2026-09-22 would have undone a
// switch 27 times, from Ardougne, the Wilderness or the Kalphite lair. The roster is the owner's
// file and holds the shared password, so an agent cannot edit it. This file holds only
// {account: script, settings, when}: no credentials, nothing the roster does not already say.
//
// The rules, all in one place (Note below):
//
//   - A successful panel start of a script other than the roster's saves {script, the settings
//     the bot actually got}. So does the roster's script with settings that differ from the
//     roster's (an override of settings alone).
//   - A start of the roster's own script with settings OMITTED, or with settings equal to the
//     roster's, removes the entry: that is how an account is reverted. DELETE
//     /api/assignments?account= removes one without touching the running bot.
//   - A start that supplies no settings, or an empty object, on the script an account is
//     assigned to gets the SAVED settings, never the roster's and never {}. The panel's own
//     Restart button sends `settings || {}`, and {} is exactly what switches ScarabSwarm's mule
//     off (see FleetSource.StartBot), so a human click must not quietly wreck an assignment.
//   - At startup cmd/controlpanel starts an assigned account on its saved script and settings
//     instead of the roster's, for any script this build has registered; an unknown script is
//     logged and the roster wins for that start. The entry is kept either way.
//
// Turning it off is -script-assignments "" (or off), or renaming the file: every account then
// starts on its roster script, exactly as before this existed.

// DefaultScriptAssignmentsFile is where cmd/controlpanel keeps saved assignments, relative to its
// working directory (run/ for the live fleet). DefaultFakeScriptAssignmentsFile is the -fake
// default, so a simulated fleet never reads or writes the real one.
const (
	DefaultScriptAssignmentsFile     = "script-assignments.json"
	DefaultFakeScriptAssignmentsFile = "script-assignments-fake.json"
)

const (
	scriptAssignmentsVersion = 1
	// maxScriptAssignmentsBytes bounds the file read at startup. One entry is well under a
	// kilobyte (the bot start request itself is capped at maxBotRequestBytes), so this leaves
	// room for thousands of accounts while refusing to load something that is plainly not ours.
	maxScriptAssignmentsBytes = 4 << 20
)

// ScriptAssignment is one account's saved script.
type ScriptAssignment struct {
	// Account is the name the fleet knows the account by, exactly as the start named it.
	Account string `json:"account"`
	Script  string `json:"script"`
	// Settings is what the bot was started with: the resolved settings, never the literal
	// request body, so an omitted or {} request is saved as what actually ran.
	Settings map[string]interface{} `json:"settings"`
	// Since is when the account was put on this script with these settings (RFC3339, UTC). A
	// later start with the same pair changes nothing and writes nothing -- a whole-fleet
	// start-all must not rewrite the file once per account.
	Since string `json:"since"`
}

type scriptAssignmentsFile struct {
	V int `json:"v"`
	// Assignments is keyed by assignmentKey(account), so a start that spells a name with
	// different case or an underscore finds the same entry the game itself would.
	Assignments map[string]ScriptAssignment `json:"assignments"`
}

// ScriptAssignments is the saved-assignment store. A nil *ScriptAssignments is valid and
// disabled: every method answers "nothing saved" and saves nothing.
type ScriptAssignments struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]ScriptAssignment
	lastErr string
}

// assignmentKey is the name the game treats as the same account: case-insensitive, with an
// underscore and a space equal (a login name's display form uses either).
func assignmentKey(account string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(account), "_", " "))
}

// OpenScriptAssignments loads path, or starts empty when it does not exist yet. An empty path
// returns nil, the disabled store. A file that cannot be parsed is moved aside to
// <path>.unreadable-<time> and the store starts empty, with an error saying so: refusing to
// start the whole fleet over it would be worse, and keeping it in place would let the next
// save overwrite the only copy of what it held.
func OpenScriptAssignments(path string) (*ScriptAssignments, error) {
	if path == "" {
		return nil, nil
	}
	s := &ScriptAssignments{path: path, now: time.Now, entries: map[string]ScriptAssignment{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("script assignments %s: %w (starting with none saved)", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxScriptAssignmentsBytes+1))
	f.Close()
	if err == nil {
		err = s.parse(data)
	}
	if err != nil {
		aside := fmt.Sprintf("%s.unreadable-%s", path, s.now().UTC().Format("20060102-150405"))
		if rerr := os.Rename(path, aside); rerr != nil {
			return s, fmt.Errorf("script assignments %s: %v, and could not move it aside: %v (starting with none saved; the next save will overwrite it)", path, err, rerr)
		}
		return s, fmt.Errorf("script assignments %s: %v (moved aside to %s; starting with none saved)", path, err, aside)
	}
	return s, nil
}

func (s *ScriptAssignments) parse(data []byte) error {
	if len(data) > maxScriptAssignmentsBytes {
		return fmt.Errorf("over the %d byte limit", maxScriptAssignmentsBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file scriptAssignmentsFile
	if err := dec.Decode(&file); err != nil {
		return fmt.Errorf("not valid: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the closing brace")
	}
	if file.V != scriptAssignmentsVersion {
		return fmt.Errorf("version %d, want %d", file.V, scriptAssignmentsVersion)
	}
	for key, a := range file.Assignments {
		if a.Account == "" || len(a.Account) > maxNameBytes || !cleanText(a.Account) {
			return fmt.Errorf("entry %q: account must be 1 to %d printable bytes", key, maxNameBytes)
		}
		if a.Script == "" || !cleanText(a.Script) {
			return fmt.Errorf("entry %q: script is required", key)
		}
		if assignmentKey(a.Account) != key {
			return fmt.Errorf("entry %q names account %q", key, a.Account)
		}
		s.entries[key] = a
	}
	return nil
}

// Enabled reports whether assignments are being saved at all.
func (s *ScriptAssignments) Enabled() bool { return s != nil }

// Path is the file assignments are saved to, "" when disabled.
func (s *ScriptAssignments) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Get returns account's saved assignment. The settings are a copy the caller may keep.
func (s *ScriptAssignments) Get(account string) (ScriptAssignment, bool) {
	if s == nil {
		return ScriptAssignment{}, false
	}
	s.mu.Lock()
	a, ok := s.entries[assignmentKey(account)]
	s.mu.Unlock()
	if ok {
		a.Settings = copySettings(a.Settings)
	}
	return a, ok
}

// List returns every saved assignment, sorted by account.
func (s *ScriptAssignments) List() []ScriptAssignment {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := make([]ScriptAssignment, 0, len(s.entries))
	for _, a := range s.entries {
		a.Settings = copySettings(a.Settings)
		out = append(out, a)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return assignmentKey(out[i].Account) < assignmentKey(out[j].Account) })
	return out
}

// scriptOf answers "which script is this account assigned" without copying any settings, for
// the per-second status path.
func (s *ScriptAssignments) scriptOf(account string) (script string, ok bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	a, ok := s.entries[assignmentKey(account)]
	s.mu.Unlock()
	return a.Script, ok
}

// LastError is the last save failure, "" once a save has succeeded since.
func (s *ScriptAssignments) LastError() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// StartSettings is the settings rule for a start that supplied none (nil) or an empty object:
// the saved settings when account is assigned to exactly this script. ok is false otherwise,
// and the caller applies its own rule (the roster's settings for nil, {} as given).
//
// One exception, so the revert rule stays one rule: the ROSTER's script with settings OMITTED is
// a revert (Note removes the entry), so it gets the roster's settings even when the account holds
// a settings-only assignment on that same script. An empty object is not a revert -- it is what
// the panel's Restart button sends for a stopped bot -- so it still gets the saved settings.
func (s *ScriptAssignments) StartSettings(account, script string, supplied map[string]interface{}, rosterScript string) (map[string]interface{}, bool) {
	if s == nil || len(supplied) > 0 {
		return nil, false
	}
	if supplied == nil && script == rosterScript {
		return nil, false
	}
	a, ok := s.Get(account)
	if !ok || a.Script != script {
		return nil, false
	}
	if a.Settings == nil {
		// Saved as nil (a script started with nothing at all): nil is what ran, so nil is
		// what runs, but a caller must still know the assignment answered.
		return nil, true
	}
	return a.Settings, true
}

// AssignmentChange says what Note did.
type AssignmentChange string

const (
	AssignmentSaved     AssignmentChange = "saved"
	AssignmentUnchanged AssignmentChange = "unchanged"
	AssignmentRemoved   AssignmentChange = "removed"
	AssignmentNone      AssignmentChange = "none"
)

// Note records a SUCCESSFUL start: account now runs script with resolved settings, which the
// request supplied as supplied (nil when it sent none). rosterScript and rosterSettings are what
// the roster configures for the account ("" and nil when it is not in the roster). See the rules
// at the top of this file.
func (s *ScriptAssignments) Note(account, script string, supplied, resolved map[string]interface{}, rosterScript string, rosterSettings map[string]interface{}) (AssignmentChange, error) {
	if s == nil {
		return AssignmentNone, nil
	}
	// Back on the roster's own script with the roster's settings, with nothing given (a revert),
	// or with nothing at all (an empty start is never worth replaying after a restart).
	backToRoster := script == rosterScript && (supplied == nil || len(resolved) == 0 || sameSettings(resolved, rosterSettings))
	key := assignmentKey(account)
	now := s.now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.entries[key]
	if backToRoster {
		if !had {
			return AssignmentNone, nil
		}
		delete(s.entries, key)
		if err := s.saveLocked(); err != nil {
			return AssignmentRemoved, err
		}
		return AssignmentRemoved, nil
	}
	if had && old.Script == script && sameSettings(old.Settings, resolved) {
		return AssignmentUnchanged, nil
	}
	s.entries[key] = ScriptAssignment{Account: account, Script: script, Settings: copySettings(resolved), Since: now}
	if err := s.saveLocked(); err != nil {
		return AssignmentSaved, err
	}
	return AssignmentSaved, nil
}

// Clear removes account's assignment; removed is false when it had none. The running bot is
// left alone -- this only decides what the NEXT process start runs.
func (s *ScriptAssignments) Clear(account string) (removed bool, err error) {
	if s == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := assignmentKey(account)
	if _, ok := s.entries[key]; !ok {
		return false, nil
	}
	delete(s.entries, key)
	return true, s.saveLocked()
}

// saveLocked writes beside the file and renames over it, as layoutStore.save does, so a crash
// mid-write leaves the previous file whole. s.mu must be held.
func (s *ScriptAssignments) saveLocked() (err error) {
	defer func() {
		if err != nil {
			s.lastErr = err.Error()
		} else {
			s.lastErr = ""
		}
	}()
	data, err := json.MarshalIndent(scriptAssignmentsFile{V: scriptAssignmentsVersion, Assignments: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".script-assignments-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, s.path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// sameSettings compares two settings objects as JSON, so 5 and 5.0 are equal and key order
// does not matter; nil and {} are equal.
func sameSettings(a, b map[string]interface{}) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// copySettings deep-copies a settings object through JSON, so neither the store nor its caller
// can change the other's map (a script is free to write into the map it was started with).
func copySettings(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return m
	}
	return out
}

// errAssignmentsOff answers a clear request on a process started without an assignments file.
var errAssignmentsOff = errors.New("saved script assignments are off on this control panel (-script-assignments is empty)")

// AssignmentControl is optional: a Source that keeps saved script assignments reports them at
// GET /api/assignments and lets DELETE /api/assignments remove one.
type AssignmentControl interface {
	ScriptAssignmentsView() AssignmentsView
	ClearScriptAssignment(account string) (removed bool, err error)
}

// AssignmentsView is GET /api/assignments' body.
type AssignmentsView struct {
	// Enabled is false when this process saves nothing (-script-assignments "").
	Enabled bool   `json:"enabled"`
	File    string `json:"file,omitempty"`
	// LastError is the last failed save; a start still succeeds when its save fails, so this
	// is where a full disk shows up.
	LastError   string               `json:"lastError,omitempty"`
	Assignments []AssignmentWithRole `json:"assignments"`
}

// AssignmentWithRole is one saved assignment with what the roster says beside it.
type AssignmentWithRole struct {
	ScriptAssignment
	// RosterScript is the roster's script for the account, "" when the roster starts none or
	// does not list the account at all (InRoster false).
	RosterScript string `json:"rosterScript"`
	InRoster     bool   `json:"inRoster"`
}

// viewOf builds AssignmentsView from a store and a roster lookup.
func viewOf(s *ScriptAssignments, roster func(account string) (RosterEntry, bool)) AssignmentsView {
	v := AssignmentsView{Enabled: s.Enabled(), File: s.Path(), LastError: s.LastError(), Assignments: []AssignmentWithRole{}}
	for _, a := range s.List() {
		row := AssignmentWithRole{ScriptAssignment: a}
		if roster != nil {
			if e, ok := roster(a.Account); ok {
				row.RosterScript, row.InRoster = e.Script, true
			}
		}
		v.Assignments = append(v.Assignments, row)
	}
	return v
}

// RosterEntry is what the roster file configures for one account.
type RosterEntry struct {
	Script   string
	Settings map[string]interface{}
}
