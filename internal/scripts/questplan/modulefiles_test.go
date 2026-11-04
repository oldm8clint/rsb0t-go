package questplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the directory holding go.mod, found by walking up from the test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

// Every entry of ModuleFiles names a file (or a directory holding a .go file) that exists, so a rename
// cannot leave cmd/questpromote asking git about a path that was never the module.
func TestModuleFilesExist(t *testing.T) {
	root := repoRoot(t)
	for id, files := range ModuleFiles {
		if len(files) == 0 {
			t.Errorf("%s has no module files", id)
		}
		for _, f := range files {
			path := filepath.Join(root, filepath.FromSlash(f))
			if strings.HasSuffix(f, "/") {
				ents, err := os.ReadDir(path)
				if err != nil {
					t.Errorf("%s: directory %s: %v", id, f, err)
					continue
				}
				n := 0
				for _, e := range ents {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
						n++
					}
				}
				if n == 0 {
					t.Errorf("%s: directory %s holds no non-test .go file", id, f)
				}
				continue
			}
			if st, err := os.Stat(path); err != nil || st.IsDir() {
				t.Errorf("%s: %s is not a file (%v)", id, f, err)
			}
		}
	}
}
