package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Options given are saved and come back on a start with none; a start with others replaces them.
func TestRememberPanelArgs(t *testing.T) {
	home := t.TempDir()
	if got := rememberPanelArgs(home, nil); len(got) != 0 {
		t.Fatalf("nothing saved yet: %v", got)
	}
	first := []string{"-listen", "127.0.0.1:9000"}
	if got := rememberPanelArgs(home, append([]string{"--"}, first...)); !reflect.DeepEqual(got, first) {
		t.Fatalf("given: %v", got)
	}
	if got := rememberPanelArgs(home, nil); !reflect.DeepEqual(got, first) {
		t.Fatalf("remembered: %v", got)
	}
	second := []string{"-watchdog", "act"}
	rememberPanelArgs(home, second)
	if got := rememberPanelArgs(home, []string{"--"}); !reflect.DeepEqual(got, second) {
		t.Fatalf("replaced: %v", got)
	}
}

// Saving panel options keeps the start-page choice.
func TestSavedOptionsKeepEachOther(t *testing.T) {
	home := t.TempDir()
	if err := saveOptions(home, savedOptions{SkipSetup: true}); err != nil {
		t.Fatal(err)
	}
	rememberPanelArgs(home, []string{"-listen", "127.0.0.1:9000"})
	o, err := loadOptions(home)
	if err != nil || !o.SkipSetup || len(o.PanelArgs) != 2 {
		t.Fatalf("%+v %v", o, err)
	}
}

// A broken options file does not stop a start.
func TestBrokenOptionsFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, optionsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := rememberPanelArgs(home, nil); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestChooseDataDir(t *testing.T) {
	cwd, exe, cfg := t.TempDir(), t.TempDir(), t.TempDir()
	if got := chooseDataDir(cwd, exe, cfg); got != filepath.Join(exe, "rsb0t-data") {
		t.Errorf("beside the program: %s", got)
	}
	if got := chooseDataDir(cwd, "", cfg); got != filepath.Join(cwd, "rsb0t-data") {
		t.Errorf("no lasting program folder: %s", got)
	}
	if err := os.Mkdir(filepath.Join(cwd, "rsb0t-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir(cwd, exe, cfg); got != filepath.Join(cwd, "rsb0t-data") {
		t.Errorf("a folder already here stays in use: %s", got)
	}
}
