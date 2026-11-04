package controlpanelapp

import (
	"path/filepath"
	"testing"

	"rsb0t-go/internal/controlpanel"
)

func registeredFor(names ...string) func(string) bool {
	return func(s string) bool {
		for _, n := range names {
			if n == s {
				return true
			}
		}
		return false
	}
}

// What a restart does: one process saves a panel switch, a NEW store opened on the same file
// (the next process) decides the auto-start. The roster entry is the fisher throughout.
func TestStartupScriptPrefersTheSavedAssignmentAfterARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), controlpanel.DefaultScriptAssignmentsFile)
	roster := accountConfig{Username: "Acct223", Script: "ProgressiveFisher", Settings: map[string]interface{}{"tool": "Big net"}}
	scarab := map[string]interface{}{"mule": "mule", "maxStage": float64(6)}

	before := openScriptAssignments(path)
	if script, _, why := startupScript(roster, before, registeredFor("ScarabSwarm", "ProgressiveFisher")); script != "ProgressiveFisher" || why != "" {
		t.Fatalf("no assignment yet: %q %q; want the roster, silently", script, why)
	}
	if _, err := before.Note("Acct223", "ScarabSwarm", scarab, scarab, "ProgressiveFisher", roster.Settings); err != nil {
		t.Fatal(err)
	}

	after := openScriptAssignments(path)
	script, settings, why := startupScript(roster, after, registeredFor("ScarabSwarm", "ProgressiveFisher"))
	if script != "ScarabSwarm" || settings["mule"] != "mule" || why == "" {
		t.Fatalf("after the restart: %q %v %q; want the saved Scarab assignment, logged", script, settings, why)
	}

	// A build that dropped the script falls back to the roster, and says so.
	script, settings, why = startupScript(roster, after, registeredFor("ProgressiveFisher"))
	if script != "ProgressiveFisher" || settings["tool"] != "Big net" || why == "" {
		t.Fatalf("unregistered script: %q %v %q; want the roster entry with a warning", script, settings, why)
	}
}

func TestScriptAssignmentsOffSpellings(t *testing.T) {
	for _, off := range []string{"", "off", "false", "FALSE", " none "} {
		if s := openScriptAssignments(off); s.Enabled() {
			t.Errorf("-script-assignments %q left the feature on (at %s)", off, s.Path())
		}
	}
	roster := accountConfig{Username: "a", Script: "ProgressiveFisher"}
	if script, _, _ := startupScript(roster, nil, registeredFor("ProgressiveFisher")); script != "ProgressiveFisher" {
		t.Fatalf("disabled store: %q", script)
	}
}

func TestRosterLookupMatchesNamesTheWayTheGameDoes(t *testing.T) {
	look := rosterLookup([]accountConfig{{Username: "spare 20", Script: "ProgressiveFisher"}})
	if e, ok := look("Spare_20"); !ok || e.Script != "ProgressiveFisher" {
		t.Fatalf("lookup: %+v %v", e, ok)
	}
	if _, ok := look("someone"); ok {
		t.Fatal("an unknown account was found")
	}
}
