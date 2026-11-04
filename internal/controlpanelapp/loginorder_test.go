package controlpanelapp

import (
	"path/filepath"
	"reflect"
	"testing"

	"rsb0t-go/internal/controlpanel"
)

// CR-4: the mule, the main account and the carriers are launched first, by the script each will run at this start; everyone else keeps the roster's
// order, and the roster itself is not reordered (the panel lists it as written).
func TestLoginOrderLaunchesTheMuleTheCrafterAndTheCarriersFirst(t *testing.T) {
	roster := []accountConfig{
		{Username: "a1", Script: "ScarabSwarm"},
		{Username: "a2", Script: "RuneCarrier"},
		{Username: "a3", Script: "ProgressiveFisher"},
		{Username: "a4", Script: "MasterCrafter"},
		{Username: "a5", Script: ""},
		{Username: "a6", Script: "RuneCarrier"},
		{Username: "a7", Script: "MuleReceiver"},
		{Username: "a8", Script: "ScarabSwarm"},
	}
	before := usernames(roster)
	got := usernames(loginOrder(roster, nil))
	want := []string{"a7", "a4", "a2", "a6", "a1", "a3", "a5", "a8"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("launch order %v, want %v", got, want)
	}
	if !reflect.DeepEqual(usernames(roster), before) {
		t.Fatalf("the roster was reordered: %v", usernames(roster))
	}
}

// A saved script assignment wins over the roster entry, so an account a panel switch made a carrier is launched as one, and one switched
// away from RuneCarrier is not.
func TestLoginOrderFollowsTheSavedAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), controlpanel.DefaultScriptAssignmentsFile)
	roster := []accountConfig{
		{Username: "Acct1", Script: "ProgressiveFisher"},
		{Username: "Acct2", Script: "RuneCarrier"},
		{Username: "Acct3", Script: "ProgressiveFisher"},
	}
	store := openScriptAssignments(path)
	settings := map[string]interface{}{"crafter": "main"}
	if _, err := store.Note("Acct3", "RuneCarrier", settings, settings, "ScarabSwarm", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Note("Acct2", "ProgressiveFisher", settings, settings, "RuneCarrier", nil); err != nil {
		t.Fatal(err)
	}
	got := usernames(loginOrder(roster, openScriptAssignments(path)))
	want := []string{"Acct3", "Acct1", "Acct2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("launch order %v, want %v", got, want)
	}
}

// A roster with none of them launches as written, and an empty one launches nobody.
func TestLoginOrderOfAnOrdinaryRosterIsTheRosterOrder(t *testing.T) {
	roster := []accountConfig{{Username: "x", Script: "ScarabSwarm"}, {Username: "y"}, {Username: "z", Script: "ProgressiveFisher"}}
	if got := usernames(loginOrder(roster, nil)); !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Fatalf("%v", got)
	}
	if got := loginOrder(nil, nil); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}
