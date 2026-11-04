package controlpanelapp

import (
	"path/filepath"
	"testing"

	"rsb0t-go/internal/controlpanel"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/worlds"
)

// A restart of a cohort that was switched to ScarabSwarm with homeWorld 2 logs it in on world 2
// directly, instead of on the roster's world followed by a hop per account in the restart ramp.
func TestARestartLandsASavedHomeWorld2AssignmentOnWorld2(t *testing.T) {
	wm, err := worlds.New("wss://w1.example.test/", "https://w1.example.test/", map[int]worlds.Endpoints{
		2: {WSURL: "wss://w2.example.test/", HTTPBase: "https://w2.example.test/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr := fleet.New(fleet.Config{WSURL: "wss://w1.example.test/", HTTPBase: "https://w1.example.test/", Worlds: wm})
	t.Cleanup(mgr.CloseAll)

	path := filepath.Join(t.TempDir(), controlpanel.DefaultScriptAssignmentsFile)
	roster := accountConfig{Username: "Acct223", Script: "ProgressiveFisher", Settings: map[string]interface{}{"tool": "Big net"}}
	scarab := map[string]interface{}{"mule": "mule", "homeWorld": float64(2)}
	before := openScriptAssignments(path)
	if _, err := before.Note("Acct223", "ScarabSwarm", scarab, scarab, "ProgressiveFisher", roster.Settings); err != nil {
		t.Fatal(err)
	}
	after := openScriptAssignments(path)
	script, settings, _ := startupScript(roster, after, registeredFor("ScarabSwarm", "ProgressiveFisher"))
	if got := startupWorld(mgr, roster, script, settings); got != 2 {
		t.Fatalf("the saved ScarabSwarm assignment names homeWorld 2: logs in on world %d, want 2", got)
	}

	// The roster's own script (no homeWorld) keeps the roster's world, and so does an account
	// that starts nothing.
	script, settings, _ = startupScript(roster, nil, registeredFor("ScarabSwarm", "ProgressiveFisher"))
	if got := startupWorld(mgr, roster, script, settings); got != roster.World {
		t.Fatalf("no homeWorld: world %d, want the roster's %d", got, roster.World)
	}
	idle := accountConfig{Username: "idle", World: 1}
	if got := startupWorld(mgr, idle, "", map[string]interface{}{"homeWorld": float64(2)}); got != 1 {
		t.Fatalf("no script: world %d, want the roster's 1", got)
	}
	// A homeWorld the deployment does not list changes nothing.
	if got := startupWorld(mgr, roster, "ScarabSwarm", map[string]interface{}{"homeWorld": float64(3)}); got != roster.World {
		t.Fatalf("unlisted world 3: world %d, want the roster's %d", got, roster.World)
	}
}
