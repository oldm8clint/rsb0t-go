package controlpanel

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
)

// The Scarab settings object the rollout sends, trimmed: what matters is that it is NOT the
// fisher's and NOT {} (which switches ScarabSwarm's mule off).
func scarabSettings() map[string]interface{} {
	return map[string]interface{}{"mule": "mule", "maxStage": float64(6), "stage3Spell": "Wind Bolt"}
}

func fisherSettings() map[string]interface{} {
	return map[string]interface{}{"mule": "mule", "cheatSetup": false, "tool": "Big net"}
}

func openTestStore(t *testing.T) (*ScriptAssignments, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run", DefaultScriptAssignmentsFile)
	s, err := OpenScriptAssignments(path)
	if err != nil || s == nil {
		t.Fatalf("open %s: %v, %v", path, s, err)
	}
	return s, path
}

func TestScriptAssignmentsDisabledStoreSavesNothing(t *testing.T) {
	s, err := OpenScriptAssignments("")
	if s != nil || err != nil {
		t.Fatalf("empty path: %v, %v; want the nil disabled store", s, err)
	}
	if s.Enabled() {
		t.Fatal("nil store reports enabled")
	}
	if change, err := s.Note("a", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil); change != AssignmentNone || err != nil {
		t.Fatalf("Note on nil store: %v %v", change, err)
	}
	if _, ok := s.StartSettings("a", "ScarabSwarm", nil, "ProgressiveFisher"); ok {
		t.Fatal("nil store answered a start")
	}
}

// The rules at the top of assignments.go, one row each. Every row states what the store must
// hold AFTER the start, so a Note that never saved, or never removed, fails a row.
func TestScriptAssignmentNoteRules(t *testing.T) {
	for _, c := range []struct {
		name               string
		script             string
		supplied, resolved map[string]interface{}
		want               AssignmentChange
		wantScript         string // "" = no entry afterwards
	}{
		{"a switch to another script is saved", "ScarabSwarm", scarabSettings(), scarabSettings(), AssignmentSaved, "ScarabSwarm"},
		{"the roster script with settings omitted is a revert", "ProgressiveFisher", nil, fisherSettings(), AssignmentRemoved, ""},
		{"the roster script with the roster's own settings is a revert", "ProgressiveFisher", fisherSettings(), fisherSettings(), AssignmentRemoved, ""},
		{"the roster script with other settings is a settings override", "ProgressiveFisher", map[string]interface{}{"tool": "Harpoon"}, map[string]interface{}{"tool": "Harpoon"}, AssignmentSaved, "ProgressiveFisher"},
		{"the roster script started on {} is never worth replaying", "ProgressiveFisher", map[string]interface{}{}, map[string]interface{}{}, AssignmentRemoved, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openTestStore(t)
			// Every row starts from an account already switched, so a revert has something
			// to remove and a save has something to replace.
			if _, err := s.Note("Acct223", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", fisherSettings()); err != nil {
				t.Fatal(err)
			}
			change, err := s.Note("Acct223", c.script, c.supplied, c.resolved, "ProgressiveFisher", fisherSettings())
			if err != nil {
				t.Fatal(err)
			}
			wantChange := c.want
			if c.name == "a switch to another script is saved" {
				wantChange = AssignmentUnchanged // the same pair twice keeps its Since
			}
			if change != wantChange {
				t.Errorf("change = %q, want %q", change, wantChange)
			}
			got, ok := s.Get("Acct223")
			if c.wantScript == "" {
				if ok {
					t.Fatalf("entry still there after a revert: %+v", got)
				}
				return
			}
			if !ok || got.Script != c.wantScript || !sameSettings(got.Settings, c.resolved) {
				t.Fatalf("entry %+v (present %v), want %s with %v", got, ok, c.wantScript, c.resolved)
			}
		})
	}
}

func TestScriptAssignmentKeepsSinceForTheSamePairAndResetsItOnChange(t *testing.T) {
	s, path := openTestStore(t)
	clock := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.Note("jagex", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil)
	first, _ := s.Get("jagex")
	clock = clock.Add(time.Hour)
	info, _ := os.Stat(path)
	os.Chtimes(path, info.ModTime().Add(-time.Hour), info.ModTime().Add(-time.Hour))
	before, _ := os.Stat(path)
	if change, _ := s.Note("jagex", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil); change != AssignmentUnchanged {
		t.Fatalf("the same pair again: %q, want unchanged", change)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged start rewrote the file; a 266-account start-all would rewrite it 266 times")
	}
	again, _ := s.Get("jagex")
	if again.Since != first.Since {
		t.Fatalf("same pair: since %s -> %s; want it kept", first.Since, again.Since)
	}
	clock = clock.Add(time.Hour)
	braked := scarabSettings()
	braked["kqMaxLives"] = float64(3)
	s.Note("jagex", "ScarabSwarm", braked, braked, "ProgressiveFisher", nil)
	retuned, _ := s.Get("jagex")
	if retuned.Since == first.Since || retuned.Settings["kqMaxLives"] != float64(3) {
		t.Fatalf("a retune must save the new settings with a new since: %+v", retuned)
	}
}

// The {} trap: the panel's Restart button sends `settings || {}` for a stopped bot, and {} is
// what switches ScarabSwarm's mule off. An assigned account must get its SAVED settings.
func TestStartSettingsGivesTheSavedSettingsForNilAndEmpty(t *testing.T) {
	s, _ := openTestStore(t)
	s.Note("runescape", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil)
	for _, supplied := range []map[string]interface{}{nil, {}} {
		got, ok := s.StartSettings("runescape", "ScarabSwarm", supplied, "ProgressiveFisher")
		if !ok || got["mule"] != "mule" {
			t.Errorf("supplied %v: got %v ok=%v, want the saved Scarab settings", supplied, got, ok)
		}
	}
	if _, ok := s.StartSettings("runescape", "ScarabSwarm", map[string]interface{}{"mule": "other"}, "ProgressiveFisher"); ok {
		t.Error("explicit settings must win over the saved ones")
	}
	if _, ok := s.StartSettings("runescape", "ProgressiveFisher", nil, "ProgressiveFisher"); ok {
		t.Error("a revert to the roster script must not get the Scarab settings")
	}
	if _, ok := s.StartSettings("someone else", "ScarabSwarm", nil, "ProgressiveFisher"); ok {
		t.Error("an unassigned account answered")
	}
}

// A settings-only override on the roster's own script: {} keeps it, omitted is a revert.
func TestStartSettingsOnARosterScriptOverride(t *testing.T) {
	s, _ := openTestStore(t)
	harpoon := map[string]interface{}{"tool": "Harpoon"}
	s.Note("bass", "ProgressiveFisher", harpoon, harpoon, "ProgressiveFisher", fisherSettings())
	if got, ok := s.StartSettings("bass", "ProgressiveFisher", map[string]interface{}{}, "ProgressiveFisher"); !ok || got["tool"] != "Harpoon" {
		t.Errorf("{} on an override: %v %v, want the override", got, ok)
	}
	if _, ok := s.StartSettings("bass", "ProgressiveFisher", nil, "ProgressiveFisher"); ok {
		t.Error("omitted settings on the roster script is a revert and must get the roster's settings")
	}
}

// A second OpenScriptAssignments on the same path is what a process restart does.
func TestScriptAssignmentsSurviveReopeningTheFile(t *testing.T) {
	s, path := openTestStore(t)
	s.Note("spare 20", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil)
	s.Note("Acct221", "ScarabSwarm", scarabSettings(), scarabSettings(), "ProgressiveFisher", nil)
	s.Note("Acct221", "ProgressiveFisher", nil, fisherSettings(), "ProgressiveFisher", fisherSettings())

	again, err := OpenScriptAssignments(path)
	if err != nil {
		t.Fatal(err)
	}
	if list := again.List(); len(list) != 1 || list[0].Account != "spare 20" || list[0].Settings["mule"] != "mule" {
		t.Fatalf("after reopen: %+v; want only the unreverted account", list)
	}
	// The game treats case and underscore as the same name, so the lookup must too.
	if _, ok := again.Get("Spare_20"); !ok {
		t.Error("a differently spelled name did not find the entry")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "password") {
		t.Fatalf("the assignments file must hold no credential: %s", data)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".script-assignments-*.tmp")); len(leftovers) != 0 {
		t.Fatalf("atomic write left temp files: %v", leftovers)
	}
}

func TestAnUnreadableAssignmentsFileIsSetAsideNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultScriptAssignmentsFile)
	if err := os.WriteFile(path, []byte(`{"v":1,"assignments":{"x":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenScriptAssignments(path)
	if err == nil || !strings.Contains(err.Error(), "moved aside") {
		t.Fatalf("err = %v, want a moved-aside error", err)
	}
	if !s.Enabled() || len(s.List()) != 0 {
		t.Fatalf("store after a bad file: enabled %v, %d entries; want an empty working store", s.Enabled(), len(s.List()))
	}
	aside, _ := filepath.Glob(path + ".unreadable-*")
	if len(aside) != 1 {
		t.Fatalf("set-aside copies: %v", aside)
	}
	if data, _ := os.ReadFile(aside[0]); string(data) != `{"v":1,"assignments":{"x":` {
		t.Fatalf("the set-aside copy changed: %q", data)
	}
}

func TestAnAssignmentsFileWithAWrongKeyIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultScriptAssignmentsFile)
	os.WriteFile(path, []byte(`{"v":1,"assignments":{"someone":{"account":"jagex","script":"ScarabSwarm","settings":{},"since":"","updated":""}}}`), 0o600)
	if _, err := OpenScriptAssignments(path); err == nil {
		t.Fatal("an entry keyed under another account's name was accepted")
	}
}

// fleetSourceWithAssignments is a FleetSource over a fixed snapshot with assignments on and a
// recording start, so the REAL StartBot rule (startWith) is what the test drives.
func fleetSourceWithAssignments(t *testing.T, snap []fleet.AccountStatus) (*FleetSource, *ScriptAssignments, *[]botCall) {
	t.Helper()
	s, _ := openTestStore(t)
	f := newFleetSource(func() []fleet.AccountStatus { return snap }, nil, time.Second)
	f.poll = 0
	roster := map[string]RosterEntry{"acct109": {Script: "ProgressiveFisher", Settings: fisherSettings()}}
	f.SetRosterSettings(func(account string) map[string]interface{} { return roster[strings.ToLower(account)].Settings })
	f.SetScriptAssignments(s, func(account string) (RosterEntry, bool) {
		e, ok := roster[strings.ToLower(account)]
		return e, ok
	}, func(string, ...interface{}) {})
	calls := &[]botCall{}
	return f, s, calls
}

func recorder(calls *[]botCall, err error) func(string, string, map[string]interface{}) error {
	return func(account, script string, settings map[string]interface{}) error {
		*calls = append(*calls, botCall{account, script, settings})
		return err
	}
}

// The whole panel sequence on one account: switch, a Restart-button {} start, a start-all nil
// start, then the revert -- each checked against what the bot was actually started with.
func TestFleetSourceStartRecordsReplaysAndReverts(t *testing.T) {
	a := blank("acct109")
	a.Connected = true
	a.Script = "ScarabSwarm"
	f, s, calls := fleetSourceWithAssignments(t, []fleet.AccountStatus{a})
	start := recorder(calls, nil)

	if err := f.startWith(start, "acct109", "ScarabSwarm", scarabSettings()); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Get("acct109"); !ok || got.Script != "ScarabSwarm" {
		t.Fatalf("the switch was not saved: %+v %v", got, ok)
	}
	for _, supplied := range []map[string]interface{}{{}, nil} {
		if err := f.startWith(start, "acct109", "ScarabSwarm", supplied); err != nil {
			t.Fatal(err)
		}
		last := (*calls)[len(*calls)-1]
		if last.Settings["mule"] != "mule" || last.Settings["stage3Spell"] != "Wind Bolt" {
			t.Fatalf("start with %v ran %v; want the SAVED Scarab settings, not {} and not the fisher's", supplied, last.Settings)
		}
	}
	accounts := f.Accounts(nil)
	if len(accounts) != 1 || accounts[0].AssignedScript != "ScarabSwarm" || accounts[0].RosterScript != "ProgressiveFisher" {
		t.Fatalf("status does not name the override: %+v", accounts)
	}

	if err := f.startWith(start, "acct109", "ProgressiveFisher", nil); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1]
	if last.Settings["tool"] != "Big net" {
		t.Fatalf("the revert ran %v; want the roster's fisher settings", last.Settings)
	}
	if _, ok := s.Get("acct109"); ok {
		t.Fatal("the revert left the assignment in place, so the next restart would put it back on Scarab")
	}
	if accounts := f.Accounts(nil); accounts[0].AssignedScript != "" || accounts[0].RosterScript != "" {
		t.Fatalf("status still names an override after the revert: %+v", accounts[0])
	}
}

// Without the store the same {} start passes {} straight through -- the behaviour on main
// before this change, and the control that shows the test above measures the store.
func TestFleetSourceWithoutAssignmentsPassesEmptySettingsThrough(t *testing.T) {
	f := newFleetSource(func() []fleet.AccountStatus { return nil }, nil, time.Second)
	f.SetRosterSettings(func(string) map[string]interface{} { return fisherSettings() })
	var calls []botCall
	if err := f.startWith(recorder(&calls, nil), "acct109", "ScarabSwarm", map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if len(calls[0].Settings) != 0 {
		t.Fatalf("with no store {} became %v", calls[0].Settings)
	}
}

func TestAFailedStartSavesNothing(t *testing.T) {
	f, s, calls := fleetSourceWithAssignments(t, nil)
	err := f.startWith(recorder(calls, errors.New("not connected yet")), "acct109", "ScarabSwarm", scarabSettings())
	if err == nil {
		t.Fatal("the start error was swallowed")
	}
	if _, ok := s.Get("acct109"); ok {
		t.Fatal("a start that failed was saved, so the next restart would run something that never ran")
	}
}

// The HTTP surface, over the fake fleet: a real POST /api/bot/start, then what GET
// /api/assignments, /api/status and /api/meta say, then DELETE.
func TestAssignmentsOverHTTP(t *testing.T) {
	src := NewFakeSource(4, 1)
	s, path := openTestStore(t)
	src.SetScriptAssignments(s)
	name := src.accounts[0].Name
	roster := src.accounts[0].Bot
	h := newHarness(t, Config{}, testOptions(), src)

	var meta metaBody
	decode(t, h.get("/api/meta"), &meta)
	if !meta.ScriptAssignments {
		t.Fatal("/api/meta does not advertise saved assignments")
	}
	res := h.postJSON("/api/bot/start", map[string]interface{}{"account": name, "script": "ScarabSwarm", "settings": scarabSettings()})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d %s", res.StatusCode, bodyText(t, res))
	}
	var view AssignmentsView
	decode(t, h.get("/api/assignments"), &view)
	if !view.Enabled || view.File != path || len(view.Assignments) != 1 {
		t.Fatalf("view %+v", view)
	}
	row := view.Assignments[0]
	if row.Account != name || row.Script != "ScarabSwarm" || row.RosterScript != roster || !row.InRoster || row.Settings["mule"] != "mule" {
		t.Fatalf("row %+v", row)
	}
	var st struct{ Accounts []AccountStatus }
	decode(t, h.get("/api/status"), &st)
	found := false
	for _, a := range st.Accounts {
		if a.Name == name {
			found = a.AssignedScript == "ScarabSwarm" && a.RosterScript == roster
		} else if a.AssignedScript != "" {
			t.Errorf("%s reports an assignment it does not have", a.Name)
		}
	}
	if !found {
		t.Fatal("/api/status does not name the assigned script")
	}

	if r := h.request(http.MethodDelete, "/api/assignments", nil, nil); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE with no account: %d", r.StatusCode)
	}
	var cleared assignmentClearBody
	decode(t, h.request(http.MethodDelete, "/api/assignments?account="+name, nil, nil), &cleared)
	if !cleared.OK || !cleared.Removed {
		t.Fatalf("clear %+v", cleared)
	}
	if again, _ := OpenScriptAssignments(path); len(again.List()) != 0 {
		t.Fatalf("the file still holds %v after DELETE", again.List())
	}
	if r := h.request(http.MethodPost, "/api/assignments", nil, nil); r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", r.StatusCode)
	}
}

func TestAssignmentsAnswer501WithoutAStore(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	if r := h.get("/api/assignments"); r.StatusCode != http.StatusNotImplemented {
		t.Fatalf("GET on a source without assignments: %d", r.StatusCode)
	}
	var meta metaBody
	decode(t, h.get("/api/meta"), &meta)
	if meta.ScriptAssignments {
		t.Fatal("meta advertises assignments with no store")
	}
}

// The fake fleet's start-up replay, which cmd/controlpanel runs under -fake after a restart.
func TestFakeSourceReplaysSavedAssignmentsAtStart(t *testing.T) {
	s, path := openTestStore(t)
	first := NewFakeSource(4, 1)
	first.SetScriptAssignments(s)
	name := first.accounts[1].Name
	if err := first.StartBot(name, "ScarabSwarm", scarabSettings()); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenScriptAssignments(path)
	if err != nil {
		t.Fatal(err)
	}
	second := NewFakeSource(4, 1)
	second.SetScriptAssignments(reopened)
	lines := second.ApplyScriptAssignments()
	if len(lines) != 1 || !strings.Contains(lines[0], name) {
		t.Fatalf("replay lines %v", lines)
	}
	for _, a := range second.Accounts(nil) {
		if a.Name != name {
			continue
		}
		if a.Bot != "ScarabSwarm" || a.BotState != "running" || a.Settings["mule"] != "mule" || a.AssignedScript != "ScarabSwarm" {
			t.Fatalf("after the replay: %+v", a)
		}
		return
	}
	t.Fatal("account missing")
}
