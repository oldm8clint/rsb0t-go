package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// optionsFile is what rsb0t remembers between starts that is not in the roster. The server, the accounts, their scripts
// and settings are already in the roster, passwords in credentials.json, and what the panel changes in its own files.
const optionsFile = "options.json"

// savedOptions is optionsFile.
type savedOptions struct {
	// PanelArgs are the panel options last given after `--`; a start with none uses these.
	PanelArgs []string `json:"panelArgs,omitempty"`
	// SkipSetup starts the panel straight away instead of showing the start page.
	SkipSetup bool `json:"skipSetup,omitempty"`
}

// loadOptions reads optionsFile; a missing file is no options.
func loadOptions(home string) (savedOptions, error) {
	var o savedOptions
	b, err := os.ReadFile(filepath.Join(home, optionsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return savedOptions{}, fmt.Errorf("%s: %w", optionsFile, err)
	}
	return o, nil
}

// saveOptions writes optionsFile whole, through a temporary file, so a crash cannot leave half of it.
func saveOptions(home string, o savedOptions) error {
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(home, optionsFile)
	if err := os.WriteFile(p+".tmp", append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// rememberPanelArgs is the panel options for this start: those given, which are saved for next time, else the saved ones.
func rememberPanelArgs(home string, given []string) []string {
	for len(given) > 0 && given[0] == "--" {
		given = given[1:]
	}
	o, err := loadOptions(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t: the saved options are not used:", err)
		return given
	}
	if len(given) > 0 {
		o.PanelArgs = given
		if err := saveOptions(home, o); err != nil {
			fmt.Fprintln(os.Stderr, "rsb0t: these options will not be remembered:", err)
		}
		return given
	}
	if len(o.PanelArgs) > 0 {
		fmt.Printf("rsb0t: using your last panel options: %s (`rsb0t forget` clears them)\n", strings.Join(o.PanelArgs, " "))
	}
	return o.PanelArgs
}

// alreadyRunning reports whether something answers on addr: another rsb0t, in practice.
func alreadyRunning(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// chooseDataDir is the default data folder: an rsb0t-data already in the working directory, else one beside the program, so
// a double-click and a terminal start find the same remembered settings; else the user's config folder when the program's
// own folder cannot be written. exeDir "" means it is not known or not lasting (`go run` builds in a temporary folder).
func chooseDataDir(cwd, exeDir, configDir string) string {
	here := filepath.Join(cwd, "rsb0t-data")
	if isDir(here) || exeDir == "" {
		return here
	}
	beside := filepath.Join(exeDir, "rsb0t-data")
	if isDir(beside) || canWrite(exeDir) || configDir == "" {
		return beside
	}
	return filepath.Join(configDir, "rsb0t")
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func canWrite(dir string) bool {
	f, err := os.CreateTemp(dir, ".rsb0t-write-check-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// programDir is the folder of the running program, "" under the temporary folder.
func programDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	if rel, err := filepath.Rel(os.TempDir(), dir); err == nil && !strings.HasPrefix(rel, "..") {
		return ""
	}
	return dir
}
